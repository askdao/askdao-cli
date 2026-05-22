// [INPUT]: internal/pipeline（Run）+ internal/webstudio（Serve / BuildStudioData / DefaultThemeForCategory）
//
//   - internal/deploy（DeployResponse / Err* 类型）+ internal/observe（Install / SweepStale）+ internal/types（AgentSpec / Detection）+ yaml；
//     复用同包 helper：chooseLLMClient / readSpec / resolveServerAndToken / deployFromDir / ensureAskdaoDir /
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
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/askdao/askdao-cli/internal/deploy"
	"github.com/askdao/askdao-cli/internal/observe"
	"github.com/askdao/askdao-cli/internal/pipeline"
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
	force := fs.Bool("force", false, "Deploy despite HIGH-severity translation warnings")
	observeMode := fs.Bool("observe", false, "Arm temporary hooks to pre-select skills/MCP a real claude session uses")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	home, _ := os.UserHomeDir()

	spec, det, code := loadOrScan(ctx, *dir, *harness, home)
	if spec == nil {
		return code
	}

	// Default the theme palette token from the category when unset.
	if spec.Metadata.ThemeColor == "" {
		spec.Metadata.ThemeColor = webstudio.DefaultThemeForCategory(spec.Metadata.Category)
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

	data := webstudio.BuildStudioData(spec, det, "Anthropic Managed Agents")
	data.Observe = *observeMode

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
		OnDeploy: func(edited *types.AgentSpec) (string, error) {
			if err := writeAgentSpec(*dir, edited); err != nil {
				return "", err
			}
			resp, derr := deployFromDir(ctx, *dir, *harness, *force)
			if derr != nil {
				return "", studioDeployError(derr)
			}
			return deployResultLine(resp), nil
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

// loadOrScan returns the spec to edit plus the detection backing the studio's
// skill/MCP candidate lists. An existing askdao-agent.yml is loaded verbatim
// (with a fresh scan for candidates); otherwise the full pipeline synthesizes a
// draft and the baseline (.askdao/) is written. Returns (nil, nil, code) on error.
func loadOrScan(ctx context.Context, dir, harness, home string) (*types.AgentSpec, *types.Detection, int) {
	agentPath := filepath.Join(dir, askdaoAgentFileName)
	if existing, err := readSpec(agentPath); err == nil {
		fmt.Fprintln(os.Stderr, "→ Loaded existing", agentPath, "— re-scanning for skill/MCP candidates ...")
		res, _ := pipeline.Run(ctx, pipeline.Options{Root: dir, LLM: nil, HomeDir: home})
		var det *types.Detection
		if res != nil {
			det = res.Detection
		}
		return existing, det, 0
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
		return nil, nil, 1
	}
	if res.Recommendation == nil {
		fmt.Fprintln(os.Stderr, "edit: recommender returned no spec")
		return nil, nil, 1
	}
	// Deterministic skills builder overwrites the LLM's spec.Skills (project
	// scope only); user-scope globals stay opt-in candidates in the studio.
	res.Recommendation.Spec.Skills = res.AgentSkills
	spec := &res.Recommendation.Spec
	writeBaseline(dir, spec, res.Detection)
	return spec, res.Detection, 0
}

// writeAgentSpec writes the KOL-editable askdao-agent.yml at the project root.
func writeAgentSpec(dir string, spec *types.AgentSpec) error {
	if err := ensureAskdaoDir(dir); err != nil {
		return err
	}
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
		return fmt.Errorf("your KOL profile isn't set up yet — complete it at https://askdao.ai/workspace, then deploy again")
	}
	var bw *deploy.ErrBlockingWarnings
	if errors.As(derr, &bw) {
		return fmt.Errorf("deploy blocked by HIGH-severity translation warnings — fix the spec or re-run edit with --force")
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
	if resp.GroupLink != "" {
		s += " · " + resp.GroupLink
	}
	return s
}
