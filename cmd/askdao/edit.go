// [INPUT]: internal/pipeline（Run）+ internal/webstudio（Serve / BuildStudioData / DefaultThemeForCategory）
//
//   - internal/deploy（DeployResponse / Err* 类型）+ internal/observe（Install / SweepStale）+ internal/types（AgentSpec / Detection）+ internal/recommender（DefaultCapabilities / BuildVaultHints / FetchModelClassesOrFallback）+ yaml；
//     复用同包 helper：chooseLLMClient / readSpec / resolveServerAndToken / ensureAskdaoDir /
//     defaultAgentName / askdaoAgentFileName / askdaoDirName
//
// [OUTPUT]: runEdit — `askdao agent edit` 命令实装
// [POS]: cmd/askdao 的核心命令 —— 扫描/加载 → 本地 Web 工作台审阅编辑（--observe 观测真实 session 预勾真正用到的 skill/MCP）→ 一站式发布；取代旧的 init/show CLI 审阅
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/askdao/askdao-cli/internal/deploy"
	"github.com/askdao/askdao-cli/internal/deployflow"
	"github.com/askdao/askdao-cli/internal/observe"
	"github.com/askdao/askdao-cli/internal/pipeline"
	"github.com/askdao/askdao-cli/internal/recommender"
	"github.com/askdao/askdao-cli/internal/types"
	"github.com/askdao/askdao-cli/internal/webstudio"
)

// runEdit implements `askdao agent edit [--dir path] [--harness id] [--no-ui] [--force] [--observe]`.
// It loads an existing askdao-agent.yml (or scans + generates a draft when
// absent), opens the local web studio for review/edit/skill-selection, and lets
// the KOL Save or one-stop Deploy. --no-ui writes a draft and exits (CI/headless).
// --observe arms temporary Claude Code hooks so the studio pre-selects the
// skills/MCP a real claude session actually activates.
func runEdit(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	dir := fs.String("dir", ".", "KOL project root containing (or to hold) askdao-agent.yml")
	harness := fs.String("harness", "", "Override preferred_harness (anthropic_managed_agents | ...)")
	noUI := fs.Bool("no-ui", false, "Skip the browser; scan and write a draft only (CI/headless)")
	force := fs.Bool("force", false, "Deploy despite blocking (deploy-fatal) translation warnings")
	observeMode := fs.Bool("observe", false, "Arm temporary hooks to pre-select skills/MCP a real claude session uses")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	home, _ := os.UserHomeDir()

	spec, det, loaded, code := loadOrScan(ctx, *dir, *harness, home)
	if spec == nil {
		return code
	}

	// Default the theme palette token + avatar icon from the category when unset.
	// Deterministic backstop for the identity layer: the recommend LLM is asked
	// to fill these, but cli guarantees a sensible non-empty avatar/theme so a
	// deployed agent always renders with an icon + brand color (not a bare
	// initial). display_name is intentionally NOT defaulted here — empty falls
	// back to name in the frontend (display_name || name || agent_id).
	if spec.Metadata.ThemeColor == "" {
		spec.Metadata.ThemeColor = webstudio.DefaultThemeForCategory(spec.Metadata.Category)
	}
	if spec.Metadata.Avatar == "" {
		spec.Metadata.Avatar = webstudio.DefaultAvatarForCategory(spec.Metadata.Category)
	}

	if *noUI {
		if err := writeAgentSpec(*dir, spec); err != nil {
			fmt.Fprintln(os.Stderr, "edit:", err)
			return 1
		}
		fmt.Printf("✓ Wrote draft %s (--no-ui).\n", filepath.Join(*dir, askdaoAgentFileName))
		fmt.Println("  Edit it, then `askdao agent deploy`, or re-run `askdao agent edit` for the studio.")
		return 0
	}

	// loaded ⇒ editing an existing yaml: restore the KOL's saved skill/MCP
	// selection verbatim. A fresh draft uses the default selection policy.
	data := webstudio.BuildStudioData(spec, det, spec.PreferredHarness, loaded)
	data.Observe = *observeMode
	// Model-class catalog for the step-2 selector: fetched from conductor so the
	// concrete model ids aren't baked into this binary. Degrades to a minimal
	// bundled fallback offline (token is optional for edit).
	editURL, editToken, _ := deployflow.ResolveServerAndToken()
	// 三档视图按 spec 的 harness 取（离线回退用）；白名单 `models[]` 取两 harness 合集
	// （cloud#84：Studio 下拉框选模型即切 harness；空 = 离线/旧 conductor → 前端回退三档）
	data.ModelCatalog = recommender.FetchModelClassesOrFallback(ctx, editURL, editToken, spec.PreferredHarness)
	data.Models = recommender.FetchModelCatalogOrEmpty(ctx, editURL, editToken)

	// --observe arms temporary hooks bound to the studio port (set in OnReady, once
	// the port is known) and tears them down on the way out. SweepStale first clears
	// anything a previously-killed run left behind; defer cleanup handles the normal
	// exit. A Ctrl-C'd process is caught by the next run's SweepStale (spike R6).
	var observeCleanup func() error
	if *observeMode {
		if err := observe.SweepStale(*dir); err != nil {
			fmt.Fprintln(os.Stderr, "edit: observe sweep:", err)
		}
		defer func() {
			if observeCleanup != nil {
				if err := observeCleanup(); err != nil {
					fmt.Fprintln(os.Stderr, "edit: observe cleanup:", err)
				}
			}
		}()
	}

	err := webstudio.Serve(webstudio.Options{
		Data: data,
		// cron「Next:」预览走 conductor 权威求解（B10）；离线/未登录返 nil 隐藏预览行。
		// 凭证与 model catalog 同一份（token 对 edit 可选）。
		OnCronPreview: func(cron, tz string) *recommender.CronPreview {
			return recommender.FetchCronPreviewOrNil(ctx, editURL, editToken, cron, tz)
		},
		OnReady: func(port int) {
			if !*observeMode {
				return
			}
			cleanup, err := observe.Install(*dir, port)
			if err != nil {
				fmt.Fprintln(os.Stderr, "edit: observe install:", err)
				return
			}
			observeCleanup = cleanup
			printObserveGuide(*dir)
		},
		OnSave: func(edited *types.AgentSpec) error {
			return writeAgentSpec(*dir, edited)
		},
		OnDeploy: func(edited *types.AgentSpec) (*webstudio.DeployResult, error) {
			if err := writeAgentSpec(*dir, edited); err != nil {
				return nil, err
			}
			// Studio 里选的模型决定 harness（写进 yaml 的 preferred_harness）；--harness 只
			// 作打开 Studio 时的初始种子，不再在部署时覆盖 KOL 在页面上的选择（cloud#84）。
			url, tok, aerr := deployflow.ResolveServerAndToken()
			if aerr != nil {
				return nil, studioDeployError(aerr)
			}
			prep, perr := deployflow.Prepare(*dir, "")
			if perr != nil {
				return nil, studioDeployError(perr)
			}
			resp, derr := prep.Deploy(ctx, url, tok, *force, false)
			if derr != nil {
				return nil, studioDeployError(derr)
			}
			return &webstudio.DeployResult{
				Message:         deployResultLine(resp),
				AgentURL:        resp.AgentURL,
				GroupLink:       resp.GroupLink,
				AgentID:         resp.AgentID,
				Created:         resp.Created,
				ScheduleWarning: resp.ScheduleWarning,
			}, nil
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "edit:", err)
		return 1
	}
	return 0
}

// printObserveGuide tells the KOL to drive a real claude session in a second
// terminal so the studio can pre-select the skills/MCP actually activated.
func printObserveGuide(dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	fmt.Println("\n● Observe mode — temporary PreToolUse hooks are armed.")
	fmt.Println("  In a SECOND terminal, run a representative scenario in this project:")
	fmt.Printf("      cd %s && claude\n", abs)
	fmt.Println("  Skills / MCP servers light up in the studio as they activate.")
	fmt.Println("  Hooks are removed automatically when you Deploy / finish / Ctrl-C.")
}

// loadOrScan returns the spec to edit, the detection backing the studio's
// skill/MCP candidate lists, and whether an existing yaml was loaded. An existing
// askdao-agent.yml is loaded verbatim (with a fresh scan for candidates) and
// loaded=true so the studio restores the saved selection; otherwise the full
// pipeline synthesizes a draft (loaded=false, default selection) and the baseline
// (.askdao/) is written. Returns (nil, nil, false, code) on error.
func loadOrScan(ctx context.Context, dir, harness, home string) (*types.AgentSpec, *types.Detection, bool, int) {
	agentPath := filepath.Join(dir, askdaoAgentFileName)
	if existing, err := readSpec(agentPath); err == nil {
		fmt.Fprintln(os.Stderr, "→ Loaded existing", agentPath, "— re-scanning for skill/MCP candidates ...")
		res, _ := pipeline.Run(ctx, pipeline.Options{Root: dir, LLM: nil, HomeDir: home})
		var det *types.Detection
		if res != nil {
			det = res.Detection
		}
		// capabilities is a hard field — always deterministic, never KOL/LLM-edited
		// (studio has no capabilities UI). Overwrite even on load so a stale yaml
		// (LLM free-text scopes) is normalised on the next edit.
		existing.Capabilities = recommender.DefaultCapabilities(detRiskHints(det))
		// --harness seeds the Studio's initial runtime; the model picked in the
		// Studio (written back as preferred_harness) is what deploy uses.
		if harness != "" {
			existing.PreferredHarness = harness
		}
		return existing, det, true, 0
	}

	fmt.Fprintln(os.Stderr, "→ Scanning project + generating a draft (LLM 10-20s if a conductor is configured) ...")
	res, err := pipeline.Run(ctx, pipeline.Options{
		Root:             dir,
		AgentName:        defaultAgentName(dir),
		PreferredHarness: harness,
		LLM:              chooseLLMClient(),
		HomeDir:          home,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "edit:", err)
		return nil, nil, false, 1
	}
	if res.Recommendation == nil {
		fmt.Fprintln(os.Stderr, "edit: recommender returned no spec")
		return nil, nil, false, 1
	}
	// Deterministic builders overwrite the LLM's free-text values for hard fields
	// (design.md §9.13): project-scope skills + capabilities. User-scope skills
	// stay opt-in candidates in the studio.
	res.Recommendation.Spec.Skills = res.AgentSkills
	res.Recommendation.Spec.Capabilities = recommender.DefaultCapabilities(detRiskHints(res.Detection))
	res.Recommendation.Spec.VaultHints = recommender.BuildVaultHints(res.Detection)
	spec := &res.Recommendation.Spec
	writeBaseline(dir, spec, res.Detection)
	return spec, res.Detection, false, 0
}

// detRiskHints safely extracts the risk-hint policy from a (possibly nil) detection.
func detRiskHints(det *types.Detection) types.DetectedToolRiskHints {
	if det == nil {
		return types.DetectedToolRiskHints{}
	}
	return det.DetectedToolRiskHints
}

// syncNetworkingFromMCP derives workspace.networking fields from mcp_servers:
// allow_mcp_servers mirrors whether any MCP server is configured, and
// allowed_hosts includes each MCP server's hostname (deduped, preserving
// any pre-existing hosts).
func syncNetworkingFromMCP(spec *types.AgentSpec) {
	hasMCP := len(spec.MCPServers) > 0
	spec.Workspace.Networking.AllowMCPServers = hasMCP

	if !hasMCP {
		return
	}

	existing := map[string]bool{}
	for _, h := range spec.Workspace.Networking.AllowedHosts {
		existing[h] = true
	}
	for _, srv := range spec.MCPServers {
		if srv.URL == "" {
			continue
		}
		u, err := url.Parse(srv.URL)
		if err != nil || u.Hostname() == "" {
			continue
		}
		host := u.Hostname()
		if !existing[host] {
			spec.Workspace.Networking.AllowedHosts = append(spec.Workspace.Networking.AllowedHosts, host)
			existing[host] = true
		}
	}
}

// writeAgentSpec writes the KOL-editable askdao-agent.yml at the project root.
func writeAgentSpec(dir string, spec *types.AgentSpec) error {
	if err := ensureAskdaoDir(dir); err != nil {
		return err
	}
	syncNetworkingFromMCP(spec)
	yml, err := yaml.Marshal(spec)
	if err != nil {
		return fmt.Errorf("yaml marshal: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, askdaoAgentFileName), yml, 0o644)
}

// writeBaseline writes the frozen diff baseline (.askdao/recommendation.yml) and
// the scan dump (.askdao/detection.json) once, right after a fresh draft is
// synthesized. Best-effort: failures are non-fatal to the studio session.
func writeBaseline(dir string, spec *types.AgentSpec, det *types.Detection) {
	if err := ensureAskdaoDir(dir); err != nil {
		return
	}
	askdaoDir := filepath.Join(dir, askdaoDirName)
	if yml, err := yaml.Marshal(spec); err == nil {
		_ = os.WriteFile(filepath.Join(askdaoDir, "recommendation.yml"), yml, 0o644)
	}
	if det != nil {
		if detJSON, err := json.MarshalIndent(det, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(askdaoDir, "detection.json"), detJSON, 0o644)
		}
	}
}

// studioDeployError maps deploy's typed errors to KOL-facing studio messages.
// KOL-profile setup is delegated to the askdao.ai web (not the local studio).
func studioDeployError(derr error) error {
	var kpr *deploy.ErrKolProfileRequired
	if errors.As(derr, &kpr) {
		// The 409 gate is kol_join_mode IS NULL — set on the dashboard
		// subscription page (mode picker + activate button), NOT the profile
		// page (which only writes name/image/bio). URL is server-handed
		// (detail.setup_url) with a hardcoded fallback for older conductors.
		return fmt.Errorf("Your Builder profile isn't set up yet — pick a subscription mode at %s, then deploy again", kolProfileSetupURL(kpr))
	}
	var bw *deploy.ErrBlockingWarnings
	if errors.As(derr, &bw) {
		return fmt.Errorf("deploy blocked by deploy-fatal (rejected) translation warnings — fix the spec or re-run edit with --force")
	}
	var vdc *deploy.ErrVisibilityDowngradeConfirm
	if errors.As(derr, &vdc) {
		// The studio has no interactive confirm; the deploy CLI owns the
		// acknowledged-downgrade path. Most of the time this 409 is a mistake
		// (a stale `visibility: private` line), so lead with the fix.
		name := vdc.Detail.AgentName
		if name == "" {
			name = "this agent"
		}
		cur := vdc.Detail.CurrentVisibility
		if cur == "" {
			cur = "shared/public"
		}
		return fmt.Errorf("deploy blocked: %s is live (%s, approved) — `visibility: private` would cut off subscribers and showcase pages, and going %s again needs a fresh platform review. Remove `visibility: private` from askdao-agent.yml (omitted = keep current), or run `askdao agent deploy --confirm-downgrade` to proceed deliberately", name, cur, cur)
	}
	return derr
}

// deployResultLine renders a one-line deploy summary for the studio status bar.
func deployResultLine(resp *deploy.DeployResponse) string {
	verb := "Updated"
	if resp.Created {
		verb = "Created"
	}
	s := fmt.Sprintf("%s agent %s", verb, resp.AgentID)
	if link := deployOpenLink(resp); link != "" {
		s += " · " + link
	}
	return s
}
