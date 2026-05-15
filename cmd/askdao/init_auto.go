package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/askdao/askdao-cli/internal/pipeline"
	"github.com/askdao/askdao-cli/internal/recommender"
	"github.com/askdao/askdao-cli/internal/render"
	"github.com/askdao/askdao-cli/internal/types"
)

// askdaoAgentFileName is the filename of the project-declaration file written
// at the KOL project root. v0.7 renamed `agent.yml` → `askdao-agent.yml` so
// the file announces its tool origin (parallel to `Cargo.toml`/`package.json`).
const askdaoAgentFileName = "askdao-agent.yml"

// askdaoDirName is the hidden tool-products directory next to the project
// declaration. Contains recommendation.yml + detection.json only — no
// persona.md (persona is fully embedded in askdao-agent.yml.persona.system_prompt
// as of v0.7).
const askdaoDirName = ".askdao"

// runInit implements `askdao agent init [name] [--auto] [--from path]`.
// Without --auto it emits a blank skeleton at the project root. With --auto it
// runs the L1-L4 pipeline, renders the mid-density review card, and prompts
// the KOL through the [A/E/R/S/D/F/M/W/P/Q] menu before writing files.
//
// `name` is optional; if absent we use the basename of the project root.
func runInit(ctx context.Context, args []string) int {
	name, rest, hasName := splitNameAndFlags(args)
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	auto := fs.Bool("auto", false, "Run scanner + LLM pipeline and pre-fill askdao-agent.yml")
	from := fs.String("from", ".", "Project root to scan (default: cwd)")
	harness := fs.String("harness", "", "Override preferred_harness (anthropic_managed_agents | openai_agents_sdk | auto)")
	if hasName {
		if err := fs.Parse(rest); err != nil {
			return 2
		}
	} else {
		if err := fs.Parse(args); err != nil {
			return 2
		}
	}

	if name == "" {
		name = defaultAgentName(*from)
	}

	if !*auto {
		return writeSkeleton(*from, name)
	}

	llm := chooseLLMClient()
	usingMock := isMockLLM(llm)

	// Phase milestones — `pipeline.Run` is a 10-20s black box, mostly spent
	// waiting on the LLM. The pipeline itself is not yet phase-observable
	// (follow-up: Options.OnPhase callback); for now we at least make it
	// clear what step is in progress so a stall is debuggable.
	fmt.Fprintln(os.Stderr, "→ Scanning project (languages / deps / Dockerfile / MCP / skills) ...")
	fmt.Fprintln(os.Stderr, "→ Inferring frameworks + building deployment payload ...")
	if usingMock {
		fmt.Fprintln(os.Stderr, "→ Using offline MockClient (no conductor URL or credentials) — recommendation will be deterministic stub.")
	} else {
		fmt.Fprintln(os.Stderr, "→ Calling LLM via conductor for recommendation (typically 10-20s) ...")
	}

	res, err := pipeline.Run(ctx, pipeline.Options{
		Root:             *from,
		AgentName:        name,
		PreferredHarness: *harness,
		LLM:              llm,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "✓ Recommendation received.")
	fmt.Fprintln(os.Stderr)
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	if res.Recommendation == nil {
		fmt.Fprintln(os.Stderr, "askdao agent init: recommender returned no spec")
		return 1
	}

	// Deterministic skills builder overwrites whatever the LLM emitted. Same
	// philosophy as the metadata.domain normalizer — fields backed by scanner
	// ground truth must not be at the LLM's mercy. See design.md §9.13.
	res.Recommendation.Spec.Skills = res.AgentSkills

	r := render.New()
	in := buildSummaryInput(res)
	render.RenderSummary(r, in)
	fmt.Println()

	return interactiveLoop(*from, name, res, in, os.Stdin)
}

// defaultAgentName returns the agent name to use when the user did not supply
// a positional argument: the basename of the absolute project root. Falls
// back to "agent" if the path resolves to "/" or similar.
func defaultAgentName(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "agent"
	}
	base := filepath.Base(abs)
	if base == "" || base == "/" || base == "." {
		return "agent"
	}
	return base
}

// writeSkeleton emits the minimum-viable askdao-agent.yml at the project root
// for the no-`--auto` path. The KOL fills the persona.system_prompt block and
// adds skills entries manually before running deploy.
func writeSkeleton(root, name string) int {
	if err := ensureAskdaoDir(root); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}
	yml := fmt.Sprintf(`apiVersion: askdao.ai/v1
kind: AgentSpec
metadata:
  name: %s
  version: 0.1.0
  visibility: private
persona:
  model_class: balanced
  model_preferences:
    - { provider: anthropic, id: claude-sonnet-4-6 }
  system_prompt: |
    Describe how this agent should behave.
capabilities:
  shell:          { enabled: true,  permission: allow }
  filesystem:     { enabled: true,  permission: allow }
  web:            { enabled: true,  permission: allow }
  code_execution: { enabled: true,  permission: allow }
mcp_servers: []
custom_tools: []
skills: []
workspace:
  base_image: ""
  workdir: /app
  startup_command: ""
  networking: { mode: limited, allow_mcp_servers: true, allow_package_managers: false }
vault_hints: {}
preferred_harness: anthropic_managed_agents
`, name)
	agentPath := filepath.Join(root, askdaoAgentFileName)
	if err := os.WriteFile(agentPath, []byte(yml), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}
	fmt.Printf("✓ Created %s\n", agentPath)
	fmt.Println("  Edit persona.system_prompt + add skills, then run `askdao agent init --auto` to populate from scan.")
	return 0
}

// chooseLLMClient picks the conductor client when we have both a server URL
// and a bearer token; otherwise falls back to the in-process MockClient so
// `init` works offline (deterministic mock recommendations) without a live
// conductor.
//
// Token / URL resolution mirrors `resolveServerAndToken` in deploy.go:
//
//  1. env ASKDAO_CONDUCTOR_URL + ASKDAO_CONDUCTOR_TOKEN (both required as a
//     pair; matches aws/gcloud convention for CI / one-off override)
//  2. credentials.json written by `askdao auth login`
//  3. nothing → MockClient (offline mode)
//
// We deliberately do NOT fall back to MockClient on partial env (only URL
// set, no token) — that almost certainly indicates misconfiguration the user
// wants to know about, so we let `recommend` 401 and the caller surfaces the
// error verbatim.
func chooseLLMClient() recommender.LLMClient {
	url, token, err := resolveServerAndToken()
	if err != nil {
		return &recommender.MockClient{}
	}
	c := recommender.NewConductorClient(url)
	c.AuthToken = token
	return c
}

// isMockLLM tells whether chooseLLMClient fell back to the offline mock —
// used by the init milestone output to tell the user that the LLM step
// will not actually hit a network (avoids a misleading "calling LLM…" line
// when the run is going to be deterministic and fast).
func isMockLLM(c recommender.LLMClient) bool {
	_, ok := c.(*recommender.MockClient)
	return ok
}

// buildSummaryInput collects the bits SummaryInput needs that aren't present
// in AgentSpec — dev/dep counters, filtered MCP servers.
func buildSummaryInput(res *pipeline.Result) render.SummaryInput {
	det := res.Detection

	prodPip, devPip := 0, 0
	for _, p := range det.DetectedPackages["pip"] {
		if p.IsProd {
			prodPip++
		} else {
			devPip++
		}
	}

	var filtered []types.MCPServerConfig
	for _, cfg := range det.DetectedMCPConfigs {
		for _, s := range cfg.Servers {
			if !s.AnthropicCompatible {
				filtered = append(filtered, s)
			}
		}
	}

	return render.SummaryInput{
		Spec:               &res.Recommendation.Spec,
		ReasoningSummary:   res.Recommendation.ReasoningSummary,
		ReasoningDecisions: res.Recommendation.ReasoningDecisions,
		TotalProdPipDeps:   prodPip,
		TotalDevPipDeps:    devPip,
		FilteredMCPServers: filtered,
		Harness:            "Anthropic Managed Agents",
	}
}

// interactiveLoop drives the [A/E/R/S/D/F/M/W/P/Q] menu from design.md §3.1.
// Reader stdin is parameterized so tests can feed scripted input.
func interactiveLoop(root, name string, res *pipeline.Result, in render.SummaryInput, stdin io.Reader) int {
	br := bufio.NewReader(stdin)
	r := render.New()

	for {
		fmt.Println("─── ACTIONS ───────────────────────────────────────────────────")
		fmt.Println("  [A] Approve and write files     [P] View persona / system prompt")
		fmt.Println("  [E] Edit yaml in $EDITOR        [D] View all pip deps")
		fmt.Println("  [R] View full reasoning trace   [F] View filtered (dev) deps")
		fmt.Println("  [S] Show full yaml in pager     [W] View all warnings")
		fmt.Println("                                  [M] View filtered MCP")
		fmt.Println("  [Q] Quit (saved as draft)")
		fmt.Print("> ")

		line, err := br.ReadString('\n')
		if err != nil {
			fmt.Println()
			return writeAgentDir(root, name, res, true /* draft */)
		}
		switch strings.ToUpper(strings.TrimSpace(line)) {
		case "A":
			return writeAgentDir(root, name, res, false)
		case "Q":
			return writeAgentDir(root, name, res, true)
		case "R":
			fmt.Println()
			render.RenderReasoningTrace(r, res.Recommendation.ReasoningDecisions)
			fmt.Println()
		case "S":
			fmt.Println()
			yml, _ := yaml.Marshal(&res.Recommendation.Spec)
			fmt.Println(string(yml))
		case "P":
			fmt.Println()
			fmt.Println(res.Recommendation.Spec.Persona.SystemPrompt)
			fmt.Println()
		case "D":
			fmt.Println()
			for _, p := range res.Recommendation.Spec.Workspace.Packages.Pip {
				fmt.Println("  •", p)
			}
			fmt.Println()
		case "F":
			fmt.Println()
			any := false
			for _, p := range res.Detection.DetectedPackages["pip"] {
				if !p.IsProd {
					fmt.Printf("  • %s==%s\n", p.Name, p.Version)
					any = true
				}
			}
			if !any {
				fmt.Println("  (no dev deps detected)")
			}
			fmt.Println()
		case "M":
			fmt.Println()
			for _, m := range in.FilteredMCPServers {
				fmt.Printf("  ⊘ %s  (type=%s)\n", m.Name, m.Type)
			}
			if len(in.FilteredMCPServers) == 0 {
				fmt.Println("  (no filtered MCP servers)")
			}
			fmt.Println()
		case "W":
			fmt.Println()
			render.RenderTranslationWarnings(r, in.Harness, in.Warnings, render.ViewAll)
			fmt.Println()
		case "E":
			fmt.Println("  (E)dit not yet wired — exit with [A] then edit the written askdao-agent.yml.")
			fmt.Println()
		default:
			fmt.Println("  Unknown choice. Type one of [A/E/R/S/D/F/M/W/P/Q].")
		}
	}
}

// writeAgentDir drops the four CLI products at the project root + .askdao/:
//
//   - <root>/askdao-agent.yml             — KOL edit target (project declaration)
//   - <root>/.askdao/recommendation.yml   — frozen snapshot; agent deploy uses
//     it as diff baseline
//   - <root>/.askdao/detection.json       — deterministic scan result
//
// No persona.md is written: the persona's system_prompt lives entirely inside
// askdao-agent.yml.persona.system_prompt (yaml literal block).
func writeAgentDir(root, name string, res *pipeline.Result, draft bool) int {
	if err := ensureAskdaoDir(root); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}
	askdaoDir := filepath.Join(root, askdaoDirName)

	yml, err := yaml.Marshal(&res.Recommendation.Spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init: yaml marshal: %v\n", err)
		return 1
	}
	agentPath := filepath.Join(root, askdaoAgentFileName)
	if err := os.WriteFile(agentPath, yml, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}
	// recommendation.yml is the frozen snapshot — never edited; deploy diffs
	// the KOL-edited askdao-agent.yml against this baseline.
	if err := os.WriteFile(filepath.Join(askdaoDir, "recommendation.yml"), yml, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}

	detJSON, _ := json.MarshalIndent(res.Detection, "", "  ")
	_ = os.WriteFile(filepath.Join(askdaoDir, "detection.json"), detJSON, 0o644)

	if draft {
		fmt.Printf("✓ Saved draft (askdao-agent.yml + .askdao/) — re-run with [A] to mark approved\n")
	} else {
		fmt.Printf("✓ Approved and wrote %s\n", agentPath)
		fmt.Println("  Next: review persona.system_prompt, then `askdao agent deploy`")
	}
	return 0
}

// ensureAskdaoDir creates <root>/.askdao if absent. The .askdao directory
// holds non-edit tool artifacts (recommendation.yml diff baseline +
// detection.json scan dump); the askdao-agent.yml itself lives at root.
func ensureAskdaoDir(root string) error {
	return os.MkdirAll(filepath.Join(root, askdaoDirName), 0o755)
}
