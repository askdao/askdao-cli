// [INPUT]: 标准库 + internal/deploy（Client / DeployInput / ZipDir / Err* 类型）+ internal/render（Diff / TranslationWarnings）+ internal/types（AgentSpec）+ gopkg.in/yaml.v3
// [OUTPUT]: runDeploy — `askdao agent deploy` 命令实装
// [POS]: cmd/askdao 的 deploy 子命令；读 <dir>/agent.yml 原文 + 打包 custom_local skill 目录 → 经 internal/deploy.Client 上传 conductor /cli/deploy；处理 kol_profile_required 握手 + HIGH-warning gating + 结果打印
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/askdao/askdao-cli/internal/deploy"
	"github.com/askdao/askdao-cli/internal/render"
	"github.com/askdao/askdao-cli/internal/types"
)

// runDeploy implements `askdao agent deploy [--dir path] [--harness id] [--force] [--bio text]`:
// reads <dir>/agent.yml (sent verbatim), packages each custom_local skill
// directory into a zip, POSTs everything to the conductor /cli/deploy endpoint,
// runs the kol_profile_required handshake when needed, gates on HIGH-severity
// translation warnings (override with --force), and prints the resulting
// agent / group / skill IDs.
func runDeploy(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	dir := fs.String("dir", ".", "Agent directory containing agent.yml")
	harness := fs.String("harness", "", "Override preferred_harness from agent.yml")
	force := fs.Bool("force", false, "Deploy even if the translation report has HIGH-severity warnings")
	bio := fs.String("bio", "", "KOL bio — used if the conductor asks you to set up your KOL profile (skips the interactive prompt)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	agentYAMLPath := filepath.Join(*dir, "agent.yml")
	agentYAML, err := os.ReadFile(agentYAMLPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deploy: %v\n", err)
		return 1
	}
	var spec types.AgentSpec
	if err := yaml.Unmarshal(agentYAML, &spec); err != nil {
		fmt.Fprintf(os.Stderr, "deploy: parse %s: %v\n", agentYAMLPath, err)
		return 1
	}
	fmt.Println("→ Reading", agentYAMLPath)

	// Optional diff preview against the frozen recommendation snapshot
	// (`init --auto` writes it; a from-scratch agent.yml won't have one).
	if original, derr := readSpec(filepath.Join(*dir, ".askdao", "recommendation.yml")); derr == nil {
		diffs := render.DiffAgentSpec(original, &spec)
		if len(diffs) == 0 {
			fmt.Println("→ No fields changed since the last recommendation.")
		} else {
			fmt.Printf("→ You modified %d field(s) since the last recommendation:\n\n", len(diffs))
			render.RenderDiff(render.New(), diffs)
		}
	}

	conductorURL := strings.TrimSpace(os.Getenv("ASKDAO_CONDUCTOR_URL"))
	if conductorURL == "" {
		fmt.Println()
		fmt.Println("✗ deploy: ASKDAO_CONDUCTOR_URL is not set.")
		fmt.Println("  Set ASKDAO_CONDUCTOR_URL=https://api.askdao.ai (and ASKDAO_CONDUCTOR_TOKEN) and re-run.")
		return 3
	}
	token := strings.TrimSpace(os.Getenv("ASKDAO_CONDUCTOR_TOKEN"))
	if token == "" {
		fmt.Println()
		fmt.Println("✗ deploy: ASKDAO_CONDUCTOR_TOKEN is not set.")
		fmt.Println("  deploy needs authentication — set ASKDAO_CONDUCTOR_TOKEN to your session token and re-run.")
		return 3
	}

	// Package each custom_local skill directory (<dir>/skills/<basename(path)>/).
	skillZips := map[string][]byte{}
	for _, s := range spec.Skills {
		if s.Type != "custom_local" {
			continue
		}
		key := s.Path
		if key == "" {
			key = s.ID
		}
		if key == "" {
			fmt.Fprintln(os.Stderr, "deploy: a custom_local skill entry is missing 'path' (and 'id')")
			return 1
		}
		name := filepath.Base(strings.TrimRight(filepath.ToSlash(key), "/"))
		if name == "" || name == "." {
			fmt.Fprintf(os.Stderr, "deploy: custom_local skill %q: invalid path\n", key)
			return 1
		}
		skillDir := filepath.Join(*dir, "skills", name)
		if fi, serr := os.Stat(skillDir); serr != nil || !fi.IsDir() {
			fmt.Fprintf(os.Stderr,
				"deploy: custom_local skill %q: directory not found at %s\n"+
					"        create %s with a SKILL.md (+ any scripts), then re-run.\n",
				key, skillDir, skillDir)
			return 1
		}
		if _, serr := os.Stat(filepath.Join(skillDir, "SKILL.md")); serr != nil {
			fmt.Fprintf(os.Stderr, "deploy: custom_local skill %q: %s does not contain a SKILL.md\n", key, skillDir)
			return 1
		}
		zb, zerr := deploy.ZipDir(skillDir, name)
		if zerr != nil {
			fmt.Fprintf(os.Stderr, "deploy: packaging skill %q: %v\n", key, zerr)
			return 1
		}
		skillZips[name] = zb
	}

	// Optional detection.json — sent if present.
	var detection []byte
	if d, derr := os.ReadFile(filepath.Join(*dir, ".askdao", "detection.json")); derr == nil {
		detection = d
	}

	harnessID := *harness
	if harnessID == "" {
		harnessID = spec.PreferredHarness
	}
	if harnessID == "" {
		harnessID = "anthropic_managed_agents"
	}

	cl := deploy.NewClient(conductorURL)
	cl.AuthToken = token
	in := deploy.DeployInput{
		AgentYAML: agentYAML,
		Detection: detection,
		HarnessID: harnessID,
		Force:     *force,
		SkillZips: skillZips,
	}

	if n := len(skillZips); n > 0 {
		fmt.Printf("→ Packaged %d custom skill(s): %s\n", n, strings.Join(sortedKeys(skillZips), ", "))
	}
	fmt.Printf("→ Deploying to %s (harness=%s) ...\n", conductorURL, harnessID)

	resp, derr := cl.Deploy(ctx, in)
	if derr != nil {
		var kpr *deploy.ErrKolProfileRequired
		if errors.As(derr, &kpr) {
			if !setupKolProfile(ctx, cl, kpr, *bio) {
				return 1
			}
			fmt.Println("→ Retrying deploy ...")
			resp, derr = cl.Deploy(ctx, in)
		}
	}
	if derr != nil {
		var bw *deploy.ErrBlockingWarnings
		if errors.As(derr, &bw) {
			fmt.Println()
			fmt.Println("✗ deploy: the translation report has HIGH-severity warnings:")
			fmt.Println()
			render.RenderTranslationWarnings(render.New(), bw.Report.Harness, toRenderWarnings(bw.Report), render.ViewAll)
			fmt.Println("  Fix agent.yml, or re-run with --force to deploy anyway.")
			return 1
		}
		fmt.Println()
		fmt.Println("✗ deploy:", derr)
		return 1
	}

	printDeployResult(resp)
	return 0
}

// setupKolProfile runs the kol_profile_required handshake: print the conductor's
// hint, resolve a bio (--bio flag, else an interactive one-line prompt — empty
// is fine since kol_bio is optional), then PATCH the KOL profile with
// kol_join_mode=free. Returns false (and prints to stderr) on failure.
func setupKolProfile(ctx context.Context, cl *deploy.Client, req *deploy.ErrKolProfileRequired, bioFlag string) bool {
	fmt.Println()
	fmt.Println("⚠  The conductor needs your KOL profile filled in before deploying.")
	if req.Detail.Hint != "" {
		fmt.Println("  ", req.Detail.Hint)
	}
	bio := strings.TrimSpace(bioFlag)
	if bio == "" {
		fmt.Print("   KOL bio (one line, optional — press Enter to skip): ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		bio = strings.TrimSpace(line)
	}
	if bio != "" {
		fmt.Printf("→ Setting KOL profile: kol_join_mode=free, bio=%q\n", bio)
	} else {
		fmt.Println("→ Setting KOL profile: kol_join_mode=free")
	}
	if err := cl.SetupKol(ctx, deploy.KolProfilePatch{KolJoinMode: "free", KolBio: bio}); err != nil {
		fmt.Fprintf(os.Stderr, "deploy: failed to set KOL profile: %v\n", err)
		return false
	}
	fmt.Println("✓ KOL profile saved.")
	return true
}

func printDeployResult(resp *deploy.DeployResponse) {
	fmt.Println()
	fmt.Println("✓ Deployed.")
	fmt.Println()
	fmt.Printf("  agent_id:    %s\n", resp.AgentID)
	fmt.Printf("  anthropic:   agent=%s  environment=%s\n", resp.AnthropicAgentID, resp.AnthropicEnvironmentID)
	if resp.GroupID != "" {
		fmt.Printf("  group_id:    %s\n", resp.GroupID)
	}
	if resp.GroupLink != "" {
		fmt.Printf("  group link:  %s\n", resp.GroupLink)
	}
	if len(resp.Skills) > 0 {
		fmt.Println()
		fmt.Println("  Skills:")
		for _, sk := range resp.Skills {
			fmt.Printf("    • %s\n", formatSkillRef(sk))
		}
	}
	if warnings := toRenderWarnings(resp.TranslationReport); len(warnings) > 0 {
		fmt.Println()
		render.RenderTranslationWarnings(render.New(), resp.TranslationReport.Harness, warnings, render.ViewSummary)
	}
}

// formatSkillRef renders one entry of DeployResponse.skills. custom_local skills
// carry the managed-skill mapping (anthropic_skill_id / version / ov_content_uri);
// builtin / git_repo entries just echo their type + identifier.
func formatSkillRef(sk map[string]interface{}) string {
	str := func(k string) string {
		if v, ok := sk[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	switch str("type") {
	case "custom_local":
		label := str("path")
		if label == "" {
			label = str("id")
		}
		out := label
		if mid := str("anthropic_skill_id"); mid != "" {
			out += "  →  managed " + mid
			if ver := str("anthropic_skill_version"); ver != "" {
				out += "@" + ver
			}
		}
		if uri := str("ov_content_uri"); uri != "" {
			out += "  (" + uri + ")"
		}
		return out
	case "builtin":
		if id := str("id"); id != "" {
			if p := str("provider"); p != "" {
				return fmt.Sprintf("%s  (builtin %s)", id, p)
			}
			return fmt.Sprintf("%s  (builtin)", id)
		}
		return "builtin"
	default:
		t := str("type")
		for _, k := range []string{"path", "repo", "id"} {
			if v := str(k); v != "" {
				return fmt.Sprintf("%s  (%s)", v, t)
			}
		}
		return t
	}
}

// toRenderWarnings converts the conductor's TranslationReport (lowercase enum
// values, fallback_attempted as an optional description string) into the render
// package's portable TranslationWarning slice.
func toRenderWarnings(tr deploy.TranslationReport) []render.TranslationWarning {
	out := make([]render.TranslationWarning, 0, len(tr.TranslationWarnings))
	for _, w := range tr.TranslationWarnings {
		var sev render.Severity
		switch strings.ToLower(w.Severity) {
		case "high":
			sev = render.SeverityHigh
		case "low":
			sev = render.SeverityLow
		default:
			sev = render.SeverityMedium
		}
		out = append(out, render.TranslationWarning{
			Field:             w.Field,
			Action:            strings.ToUpper(w.Action),
			Reason:            w.Reason,
			Severity:          sev,
			FallbackAttempted: w.FallbackAttempted != nil && strings.TrimSpace(*w.FallbackAttempted) != "",
		})
	}
	return out
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// readSpec parses an agent.yml-style file into an AgentSpec — used for the
// deploy diff preview against .askdao/recommendation.yml.
func readSpec(path string) (*types.AgentSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s types.AgentSpec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &s, nil
}
