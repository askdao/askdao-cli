// [INPUT]: 标准库 + internal/auth（Credentials / Load / ErrNoCredentials）+ internal/deploy（Client / DeployInput / ZipDir / Err* 类型）+ internal/render（Diff / TranslationWarnings）+ internal/scanner（ParseSkillFrontmatter — deploy 前置校验）+ internal/types（AgentSpec）+ gopkg.in/yaml.v3
// [OUTPUT]: runDeploy — `askdao agent deploy` 命令实装；packageSkills / deployFromDir / resolveSkillDir — skill 打包真相源（CLI + 工作台共用）
// [POS]: cmd/askdao 的 deploy 子命令；读 <dir>/askdao-agent.yml 原文 + 经 packageSkills 按 skill.path（project 相对 / 绝对 / ~ / Scope=="user"）
//
//	统一解析 + 递归打 zip（harness 中性 invariant）→ 经 internal/deploy.Client 上传 conductor /cli/deploy；处理
//	kol_profile_required 时引导去 askdao.ai/dashboard/subscription（kol_join_mode 在订阅模式页设置，KOL profile 归云端）+ blocking-warning gating（仅 REJECTED 阻断，severity 不 gate）+ 结果打印。Token / server URL 解析顺序见 resolveServerAndToken
//	(env > credentials.json > error)，对齐 docs/cli-auth-device-flow.md §6.3.
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/askdao/askdao-cli/internal/auth"
	"github.com/askdao/askdao-cli/internal/deploy"
	"github.com/askdao/askdao-cli/internal/render"
	"github.com/askdao/askdao-cli/internal/scanner"
	"github.com/askdao/askdao-cli/internal/types"
)

// runDeploy implements `askdao agent deploy [--dir path] [--harness id] [--force]`:
// reads <dir>/askdao-agent.yml (sent verbatim), packages every custom_local
// skill directory into a zip via the shared packageSkills (recursively
// including SKILL.md + scripts/ + assets/ + references/ etc., harness-neutral
// by virtue of filepath.Base) — the same source of truth the web studio's
// deployFromDir uses, so CLI and studio resolve project / absolute / ~ /
// user-scope skill paths identically — POSTs everything to the conductor
// /cli/deploy endpoint, runs the kol_profile_required handshake when needed,
// gates on blocking (REJECTED / deploy-fatal) translation warnings (override
// with --force), and prints the resulting agent / group / skill IDs.
func runDeploy(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	dir := fs.String("dir", ".", "KOL project root containing askdao-agent.yml")
	harness := fs.String("harness", "", "Override preferred_harness from askdao-agent.yml")
	force := fs.Bool("force", false, "Deploy even if the translation report has blocking (deploy-fatal) warnings")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	agentYAMLPath := filepath.Join(*dir, askdaoAgentFileName)
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

	conductorURL, token, authErr := resolveServerAndToken()
	if authErr != nil {
		fmt.Println()
		fmt.Println("✗ deploy:", authErr)
		fmt.Println("  Either run `askdao auth login`, or set ASKDAO_CONDUCTOR_URL + ASKDAO_CONDUCTOR_TOKEN for CI / one-off use.")
		return 3
	}

	// Package each custom_local skill's directory via the shared packageSkills —
	// the single source of truth also used by the web studio's deployFromDir.
	// It resolves project-relative, absolute, ~-prefixed, and Scope=="user"
	// (global) skill paths uniformly (see resolveSkillDir), and applies the
	// harness-neutral invariant (zip top dir = filepath.Base, design.md §9.14:
	// Anthropic only ever sees `tts/SKILL.md` regardless of the on-disk
	// .claude/ or .agents/ prefix). Keeping a separate inline loop here once
	// caused the CLI to mishandle global skills the studio packaged fine.
	skillZips, err := packageSkills(*dir, &spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "deploy:", err)
		return 1
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
	printDeployProgress(conductorURL, harnessID, len(skillZips))

	resp, derr := cl.Deploy(ctx, in)
	if derr != nil {
		var kpr *deploy.ErrKolProfileRequired
		if errors.As(derr, &kpr) {
			// KOL profile lives in the askdao.ai cloud (not the local CLI). The
			// 409 gate is specifically kol_join_mode IS NULL, set on the dashboard
			// subscription page (mode picker + activate button) — the profile page
			// only writes name/image/bio and won't clear the gate. The URL is
			// server-authoritative (detail.setup_url, M4); the hardcoded value is
			// only the fallback for older conductors. Mirrors edit.go's
			// studioDeployError so both entry points agree.
			fmt.Println()
			fmt.Println("⚠  Your Builder profile isn't set up yet.")
			fmt.Printf("   Pick a subscription mode at %s, then deploy again.\n", kolProfileSetupURL(kpr))
			return 1
		}
	}
	if derr != nil {
		var bw *deploy.ErrBlockingWarnings
		if errors.As(derr, &bw) {
			fmt.Println()
			fmt.Println("✗ deploy: the translation report has blocking (deploy-fatal) warnings:")
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

// kolProfileSetupURL resolves the page that clears the kol_profile_required
// gate: server-handed detail.setup_url first, hardcoded fallback for older
// conductors. Shared by runDeploy and edit.go's studioDeployError.
func kolProfileSetupURL(kpr *deploy.ErrKolProfileRequired) string {
	if kpr != nil && kpr.Detail.SetupURL != "" {
		return kpr.Detail.SetupURL
	}
	return "https://askdao.ai/dashboard/subscription"
}

func printDeployResult(resp *deploy.DeployResponse) {
	fmt.Println()
	// Update-mode: the server's response.created tells us whether this deploy
	// created a fresh agent or updated the existing one in place. When updating
	// we also print the version bump (e.g. "v1 → v2") so the KOL can confirm the
	// change landed without diving into the Anthropic dashboard.
	if resp.Created {
		fmt.Println("✓ Created new agent.")
	} else if resp.PreviousManagedVersion != nil {
		fmt.Printf("✓ Updated existing agent (v%d → v%d).\n",
			*resp.PreviousManagedVersion, *resp.PreviousManagedVersion+1)
	} else {
		fmt.Println("✓ Updated existing agent.")
	}
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
		// Prelude so the ⚠️ section is not mistaken for a deploy failure.
		// Anything REJECTED (deploy-fatal) would have already 409-blocked the
		// deploy upstream; at this point the agent is live and these are
		// advisory only (this adapter is fail-soft and never rejects).
		fmt.Println("ℹ The following non-blocking warnings were flagged during translation.")
		fmt.Println("  Your agent is live and ready to use — these are advisory notes.")
		fmt.Println()
		// Batch CLI: use ViewAll because the [W] interactive prompt in
		// ViewSummary is dead — the process exits after this print.
		render.RenderTranslationWarnings(render.New(), resp.TranslationReport.Harness, warnings, render.ViewAll)
	}
	// Trailing confirmation so the user has a clear "done" signal regardless
	// of whether warnings are present.
	fmt.Println()
	if resp.GroupLink != "" {
		fmt.Printf("✓ Deploy complete. Open %s to chat.\n", resp.GroupLink)
	} else {
		fmt.Println("✓ Deploy complete.")
	}
}

// printDeployProgress prints expected scope + duration before the POST to
// /cli/deploy. The actual HTTP call takes 10-25s while conductor uploads
// each skill to Anthropic Managed Skills + creates an environment + agent.
// Without this prelude the user sees "Deploying ..." then nothing for half
// a minute.
func printDeployProgress(conductorURL, harnessID string, skillCount int) {
	if skillCount > 0 {
		fmt.Printf("→ Deploying to %s (harness=%s) — uploading %d skill(s) + creating Anthropic agent/environment, typically 15-25s ...\n",
			conductorURL, harnessID, skillCount)
	} else {
		fmt.Printf("→ Deploying to %s (harness=%s) — creating Anthropic agent/environment, typically 5-10s ...\n",
			conductorURL, harnessID)
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

// resolveServerAndToken picks the conductor URL + bearer token for deploy.
//
// Precedence (docs/cli-auth-device-flow.md §6.3 — env-first, matches
// aws/gcloud/kubectl):
//
//  1. $ASKDAO_CONDUCTOR_TOKEN + $ASKDAO_CONDUCTOR_URL (both required if either
//     is set) — CI / one-off override
//  2. credentials.json from `askdao auth login` — interactive default
//  3. error — caller prints the actionable hint
//
// The two env vars travel as a pair: explicitly setting only one is almost
// certainly a misconfiguration and silently falling back to credentials.json
// would be more confusing than the error.
func resolveServerAndToken() (string, string, error) {
	envToken := strings.TrimSpace(os.Getenv("ASKDAO_CONDUCTOR_TOKEN"))
	envURL := strings.TrimSpace(os.Getenv("ASKDAO_CONDUCTOR_URL"))

	if envToken != "" && envURL != "" {
		return envURL, envToken, nil
	}
	if envToken != "" && envURL == "" {
		return "", "", errors.New("ASKDAO_CONDUCTOR_TOKEN is set but ASKDAO_CONDUCTOR_URL is not")
	}
	if envURL != "" && envToken == "" {
		return "", "", errors.New("ASKDAO_CONDUCTOR_URL is set but ASKDAO_CONDUCTOR_TOKEN is not")
	}

	creds, err := auth.Load()
	if err != nil {
		if errors.Is(err, auth.ErrNoCredentials) {
			return "", "", errors.New("not logged in")
		}
		return "", "", err
	}
	return creds.Server, creds.AccessToken, nil
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

// resolveSkillDir resolves a custom_local skill's on-disk directory. Project
// scope (relative path) joins dir; user scope (Scope=="user", an absolute path,
// or a ~-prefixed path) resolves against the home dir / filesystem so global
// skills picked in the web studio package correctly.
func resolveSkillDir(dir string, s types.Skill) (string, error) {
	path := s.Path
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, filepath.FromSlash(path[2:])), nil
	}
	if filepath.IsAbs(path) || s.Scope == "user" {
		return filepath.FromSlash(path), nil
	}
	return filepath.Join(dir, filepath.FromSlash(path)), nil
}

// packageSkills zips every custom_local skill referenced by spec into a
// name→zip map, applying the harness-neutral invariant (zip top dir =
// filepath.Base). Shared by `agent deploy` (CLI) and the web studio's
// OnDeploy. Returns a descriptive error on a missing dir / SKILL.md / name
// collision / incomplete SKILL.md frontmatter (name + description are how
// the model decides to activate a skill — a skill missing them deploys
// fine but never triggers, so we fail fast here instead).
func packageSkills(dir string, spec *types.AgentSpec) (map[string][]byte, error) {
	skillZips := map[string][]byte{}
	fmNames := map[string]string{} // frontmatter name → yaml path (collision detection)
	for _, s := range spec.Skills {
		if s.Type != "custom_local" {
			continue
		}
		if s.Path == "" {
			return nil, fmt.Errorf("a custom_local skill entry is missing 'path'")
		}
		skillName := filepath.Base(filepath.Clean(s.Path))
		if skillName == "" || skillName == "." || skillName == "/" {
			return nil, fmt.Errorf("custom_local skill path %q: cannot resolve a skill name", s.Path)
		}
		skillDir, err := resolveSkillDir(dir, s)
		if err != nil {
			return nil, err
		}
		if fi, serr := os.Stat(skillDir); serr != nil || !fi.IsDir() {
			return nil, fmt.Errorf("custom_local skill %q: directory not found at %s", s.Path, skillDir)
		}
		skillMD := filepath.Join(skillDir, "SKILL.md")
		if _, serr := os.Stat(skillMD); serr != nil {
			return nil, fmt.Errorf("custom_local skill %q: %s has no SKILL.md", s.Path, skillDir)
		}
		fmName, fmDesc := scanner.ParseSkillFrontmatter(skillMD)
		if fmName == "" {
			return nil, fmt.Errorf("custom_local skill %q: SKILL.md frontmatter must declare 'name' (add a leading `---` block with name + description)", s.Path)
		}
		if fmDesc == "" {
			return nil, fmt.Errorf("custom_local skill %q: SKILL.md frontmatter must declare 'description' — it is the trigger instruction the model matches against; without it the skill never activates", s.Path)
		}
		if prev, dup := fmNames[fmName]; dup {
			return nil, fmt.Errorf("skill frontmatter name collision %q (declared by both %s and %s)", fmName, prev, s.Path)
		}
		fmNames[fmName] = s.Path
		zb, zerr := deploy.ZipDir(skillDir, skillName)
		if zerr != nil {
			return nil, fmt.Errorf("packaging skill %q: %w", s.Path, zerr)
		}
		if _, dup := skillZips[skillName]; dup {
			return nil, fmt.Errorf("skill name collision %q (two paths share a basename)", skillName)
		}
		skillZips[skillName] = zb
	}
	return skillZips, nil
}

// deployFromDir reads <dir>/askdao-agent.yml, packages its custom_local skills,
// and POSTs to conductor /cli/deploy. It performs no interactive prompting —
// callers handle typed errors (*deploy.ErrKolProfileRequired,
// *deploy.ErrBlockingWarnings). Used by the web studio's one-stop OnDeploy.
func deployFromDir(ctx context.Context, dir, harnessOverride string, force bool) (*deploy.DeployResponse, error) {
	agentYAML, err := os.ReadFile(filepath.Join(dir, askdaoAgentFileName))
	if err != nil {
		return nil, err
	}
	var spec types.AgentSpec
	if err := yaml.Unmarshal(agentYAML, &spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", askdaoAgentFileName, err)
	}
	skillZips, err := packageSkills(dir, &spec)
	if err != nil {
		return nil, err
	}
	var detection []byte
	if d, derr := os.ReadFile(filepath.Join(dir, ".askdao", "detection.json")); derr == nil {
		detection = d
	}
	harnessID := harnessOverride
	if harnessID == "" {
		harnessID = spec.PreferredHarness
	}
	if harnessID == "" {
		harnessID = "anthropic_managed_agents"
	}
	conductorURL, token, err := resolveServerAndToken()
	if err != nil {
		return nil, err
	}
	cl := deploy.NewClient(conductorURL)
	cl.AuthToken = token
	return cl.Deploy(ctx, deploy.DeployInput{
		AgentYAML: agentYAML,
		Detection: detection,
		HarnessID: harnessID,
		Force:     force,
		SkillZips: skillZips,
	})
}
