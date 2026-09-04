// [INPUT]: 标准库 + internal/deploy（Err* 类型）+ internal/deployflow（Prepare/Deploy/ResolveServerAndToken 装配单源）+ internal/render（Diff / TranslationWarnings）+ internal/types（AgentSpec）+ gopkg.in/yaml.v3
// [OUTPUT]: runDeploy — `askdao agent deploy` 命令实装（装配走 internal/deployflow.Prepare+Deploy 单源，CLI / studio / 桌面共用）+ deployOpenLink（回执落点单源，agent 页优先、存量 group 链接兜底；edit.go 共用）
// [POS]: cmd/askdao 的 deploy 子命令；读 <dir>/askdao-agent.yml 原文 + 经 internal/deployflow.PackageSkills 按 skill.path（project 相对 / 绝对 / ~ / Scope=="user"）
//
//	统一解析 + 递归打 zip（harness 中性 invariant）→ 经 internal/deploy.Client 上传 conductor /cli/deploy；处理
//	kol_profile_required 时引导去 askdao.ai/dashboard/subscription（kol_join_mode 在订阅模式页设置，KOL profile 归云端）+ blocking-warning gating（仅 REJECTED 阻断，severity 不 gate）
//	+ visibility 降级确认闸（409 visibility_downgrade_requires_confirm → promptVisibilityDowngrade 危险警告 + stdin Y/N，yes 重发带确认字段；--confirm-downgrade 非交互通道）+ 结果打印。
//	Token / server URL 解析走 deployflow.ResolveServerAndToken (env > credentials.json > error)，对齐 docs/cli-auth-device-flow.md §6.3.
//
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
	"github.com/askdao/askdao-cli/internal/deployflow"
	"github.com/askdao/askdao-cli/internal/render"
	"github.com/askdao/askdao-cli/internal/types"
)

// runDeploy implements `askdao agent deploy [--dir path] [--harness id] [--force]`:
// assembles the bundle via deployflow.Prepare (the single source the web studio
// and desktop share), prints diff preview / progress, POSTs via deployflow
// Deploy, handles the kol_profile_required handshake and the visibility
// downgrade prompt, and prints the resulting agent id and page link.
func runDeploy(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	dir := fs.String("dir", ".", "KOL project root containing askdao-agent.yml")
	harness := fs.String("harness", "", "Override preferred_harness from askdao-agent.yml")
	force := fs.Bool("force", false, "Deploy even if the translation report has blocking (deploy-fatal) warnings")
	confirmDowngrade := fs.Bool("confirm-downgrade", false, "Acknowledge taking an approved shared/public agent private (subscribers and showcase pages lose access; going public again requires re-review)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// 装配单源（deployflow.Prepare）：读 yaml + PackageSkills + detection + harness 默认链。
	p, err := deployflow.Prepare(*dir, *harness)
	if err != nil {
		fmt.Fprintln(os.Stderr, "deploy:", err)
		return 1
	}
	fmt.Println("→ Reading", filepath.Join(*dir, askdaoAgentFileName))

	// Optional diff preview against the frozen recommendation snapshot
	// (`init --auto` writes it; a from-scratch agent.yml won't have one).
	// 有意先于凭据检查：未登录用户也能看到自己改了什么。
	if original, derr := readSpec(filepath.Join(*dir, ".askdao", "recommendation.yml")); derr == nil {
		diffs := render.DiffAgentSpec(original, p.Spec)
		if len(diffs) == 0 {
			fmt.Println("→ No fields changed since the last recommendation.")
		} else {
			fmt.Printf("→ You modified %d field(s) since the last recommendation:\n\n", len(diffs))
			render.RenderDiff(render.New(), diffs)
		}
	}

	conductorURL, token, authErr := deployflow.ResolveServerAndToken()
	if authErr != nil {
		fmt.Println()
		fmt.Println("✗ deploy:", authErr)
		fmt.Println("  Either run `askdao auth login`, or set ASKDAO_CONDUCTOR_URL + ASKDAO_CONDUCTOR_TOKEN for CI / one-off use.")
		return 3
	}

	if n := len(p.SkillZips); n > 0 {
		fmt.Printf("→ Packaged %d custom skill(s): %s\n", n, strings.Join(sortedKeys(p.SkillZips), ", "))
	}
	printDeployProgress(conductorURL, p.HarnessID, len(p.SkillZips))

	confirm := *confirmDowngrade
	resp, derr := p.Deploy(ctx, conductorURL, token, *force, confirm)
	if derr != nil {
		// Visibility downgrade gate: the yaml explicitly sets visibility: private
		// on an agent that is approved and publicly serving. Warn about both
		// consequences (immediate loss of access for subscribers/showcase, and a
		// fresh platform review to go public again), then ask for an explicit
		// yes before retrying with the confirmation field set.
		var vdc *deploy.ErrVisibilityDowngradeConfirm
		if errors.As(derr, &vdc) {
			if !promptVisibilityDowngrade(vdc) {
				return 1
			}
			confirm = true
			resp, derr = p.Deploy(ctx, conductorURL, token, *force, confirm)
		}
	}
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

// promptVisibilityDowngrade warns about taking a live approved shared/public
// agent private and asks for an explicit yes. Returns true only on a confirmed
// interactive "y"/"yes". When stdin is not a terminal (CI, pipes) it refuses
// and points at --confirm-downgrade — a destructive default must never be
// reachable by silence.
func promptVisibilityDowngrade(e *deploy.ErrVisibilityDowngradeConfirm) bool {
	name := e.Detail.AgentName
	if name == "" {
		name = "this agent"
	}
	cur := e.Detail.CurrentVisibility
	if cur == "" {
		cur = "shared/public"
	}
	fmt.Println()
	fmt.Printf("⚠  DANGER: %q is currently %s and approved — it is serving users right now.\n", name, cur)
	fmt.Println("   Taking it private will:")
	fmt.Println("     • immediately cut off subscribers and its public showcase pages")
	fmt.Printf("     • require a fresh platform review (back to pending) to become %s again\n", cur)
	fmt.Println("   Tip: omit `visibility` in askdao-agent.yml to keep the current setting.")
	if !stdinIsTerminal() {
		fmt.Println()
		fmt.Println("✗ deploy: refusing to downgrade without confirmation (stdin is not a terminal).")
		fmt.Println("  Re-run with --confirm-downgrade to acknowledge, or drop `visibility: private` from askdao-agent.yml.")
		return false
	}
	fmt.Print("\n   Take it private anyway? [y/N] ")
	// EOF (e.g. stdin is /dev/null — a char device, so it passes the terminal
	// check) reads an empty line and falls through to the refusal below: a
	// destructive default must never be reachable by silence.
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	fmt.Println("✗ deploy: cancelled — agent visibility unchanged.")
	fmt.Println("  To proceed deliberately, re-run with --confirm-downgrade; or drop `visibility: private` from askdao-agent.yml (omitted = keep current).")
	return false
}

// stdinIsTerminal reports whether stdin is an interactive terminal (char
// device). Under pipes / CI it is not, and interactive prompts must not block.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
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
	if resp.AgentURL != "" {
		fmt.Printf("  agent page:  %s\n", resp.AgentURL)
	}
	// Legacy group fields: only agents deployed before groups were retired
	// still carry them, so nothing is printed for a fresh agent.
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
	// #303: frequent-schedule cost warning — advisory only, never blocks the
	// deploy (product decision: no hard frequency floor, guardrail lives in
	// the UI). Printed prominently so a "*/5 * * * *" doesn't slip by unseen.
	if resp.ScheduleWarning != "" {
		fmt.Println()
		fmt.Printf("⚠️  SCHEDULE COST WARNING: %s\n", resp.ScheduleWarning)
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
	if link := deployOpenLink(resp); link != "" {
		fmt.Printf("✓ Deploy complete. Open %s to chat.\n", link)
	} else {
		fmt.Println("✓ Deploy complete.")
	}
}

// deployOpenLink is the single place that decides which URL a deploy hands
// back. The agent's own page is the destination; GroupLink is only a fallback
// for agents deployed before groups were retired server-side.
func deployOpenLink(resp *deploy.DeployResponse) string {
	if resp.AgentURL != "" {
		return resp.AgentURL
	}
	return resp.GroupLink
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
