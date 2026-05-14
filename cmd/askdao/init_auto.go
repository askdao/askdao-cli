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

// runInit implements `askdao agent init <name> [--auto]`. Without --auto it
// emits a blank skeleton (plan/06 §4.2 baseline). With --auto it runs the L1-L4
// pipeline, renders the mid-density review card, and prompts the KOL through
// the [A/E/R/S/D/F/M/W/P/Q] menu before writing files.
func runInit(ctx context.Context, args []string) int {
	name, rest, ok := splitNameAndFlags(args)
	if !ok {
		fmt.Fprintln(os.Stderr, "askdao agent init: missing <name>")
		return 2
	}
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	auto := fs.Bool("auto", false, "Run scanner + LLM pipeline and pre-fill agent.yml")
	from := fs.String("from", ".", "Project root to scan (default: cwd)")
	harness := fs.String("harness", "", "Override preferred_harness (anthropic_managed_agents | openai_agents_sdk | auto)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	if !*auto {
		return writeSkeleton(name)
	}

	llm := chooseLLMClient()
	res, err := pipeline.Run(ctx, pipeline.Options{
		Root:             *from,
		AgentName:        name,
		PreferredHarness: *harness,
		LLM:              llm,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "askdao agent init: %v\n", err)
		return 1
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	if res.Recommendation == nil {
		fmt.Fprintln(os.Stderr, "askdao agent init: recommender returned no spec")
		return 1
	}

	r := render.New()
	in := buildSummaryInput(res)
	render.RenderSummary(r, in)
	fmt.Println()

	return interactiveLoop(name, res, in, os.Stdin)
}

// writeSkeleton produces the empty plan/06 §4.2 layout when --auto is absent.
func writeSkeleton(name string) int {
	if err := os.MkdirAll(filepath.Join(name, "skills"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Join(name, "resources"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}
	yml := fmt.Sprintf(`apiVersion: askdao.ai/v1
kind: AgentSpec
metadata:
  name: %s
  version: 0.1.0
  visibility: private
  persona_file: persona.md
persona:
  model_class: balanced
  model_preferences:
    - { provider: anthropic, id: claude-sonnet-4-6 }
  system_prompt: ""
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
	if err := os.WriteFile(filepath.Join(name, "agent.yml"), []byte(yml), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(name, "persona.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}
	fmt.Printf("✓ Created agent skeleton at ./%s/\n", name)
	fmt.Println("  Edit agent.yml + persona.md, then run `askdao agent init", name, "--auto` to refresh recommendations.")
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

// buildSummaryInput collects the bits SummaryInput needs that aren't present
// in AgentSpec — dev/dep counters, filtered MCP servers, persona note.
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
		PersonaFileNote:    "empty — KOL to write",
		Harness:            "Anthropic Managed Agents",
	}
}

// interactiveLoop drives the [A/E/R/S/D/F/M/W/P/Q] menu from design.md §3.1.
// Reader stdin is parameterized so tests can feed scripted input.
func interactiveLoop(name string, res *pipeline.Result, in render.SummaryInput, stdin io.Reader) int {
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
			return writeAgentDir(name, res, true /* draft */)
		}
		switch strings.ToUpper(strings.TrimSpace(line)) {
		case "A":
			return writeAgentDir(name, res, false)
		case "Q":
			return writeAgentDir(name, res, true)
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
			fmt.Println("  (E)dit not yet wired — exit with [A] then edit the written agent.yml.")
			fmt.Println()
		default:
			fmt.Println("  Unknown choice. Type one of [A/E/R/S/D/F/M/W/P/Q].")
		}
	}
}

// writeAgentDir creates ./<name>/, drops agent.yml + persona.md +
// .askdao/{detection.json,recommendation.yml}. The recommendation.yml snapshot
// is what `agent deploy` diffs against later.
func writeAgentDir(name string, res *pipeline.Result, draft bool) int {
	dir := name
	askdaoDir := filepath.Join(dir, ".askdao")
	if err := os.MkdirAll(askdaoDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Join(dir, "skills"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}

	yml, err := yaml.Marshal(&res.Recommendation.Spec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init: yaml marshal: %v\n", err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.yml"), yml, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}
	// recommendation.yml is the frozen snapshot — never edited.
	if err := os.WriteFile(filepath.Join(askdaoDir, "recommendation.yml"), yml, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "init: %v\n", err)
		return 1
	}

	personaPath := filepath.Join(dir, "persona.md")
	if _, err := os.Stat(personaPath); os.IsNotExist(err) {
		_ = os.WriteFile(personaPath, []byte("# "+name+"\n\nKOL persona — describe how this agent should behave.\n"), 0o644)
	}

	detJSON, _ := json.MarshalIndent(res.Detection, "", "  ")
	_ = os.WriteFile(filepath.Join(askdaoDir, "detection.json"), detJSON, 0o644)

	if draft {
		fmt.Printf("✓ Saved draft at ./%s/  (run again with [A] to mark approved)\n", name)
	} else {
		fmt.Printf("✓ Approved and wrote ./%s/agent.yml + persona.md\n", name)
		fmt.Println("  Next: review persona.md, then `askdao agent deploy`")
	}
	return 0
}
