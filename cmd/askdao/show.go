package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/askdao/askdao-cli/internal/render"
	"github.com/askdao/askdao-cli/internal/types"
)

// runShow implements `askdao agent show <name> [flags]`. The default view is
// the same mid-density card init produces; flags drill into one section.
func runShow(_ context.Context, args []string) int {
	name, rest, ok := splitNameAndFlags(args)
	if !ok {
		fmt.Fprintln(os.Stderr, "askdao agent show: missing <name>")
		return 2
	}
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	full := fs.Bool("full", false, "Print the complete agent.yml")
	reasoning := fs.Bool("reasoning", false, "Print provenance.reasoning_decisions only")
	warnings := fs.Bool("warnings", false, "Print TranslationWarnings only (none until the agent is deployed — they live in agent deploy's response, not agent.yml)")
	persona := fs.Bool("persona", false, "Print persona.md + system_prompt only")
	deps := fs.Bool("deps", false, "Print full pip / npm dep lists")
	mcp := fs.Bool("mcp", false, "Print MCP servers (active + filtered)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	specPath := filepath.Join(name, "agent.yml")
	data, err := os.ReadFile(specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "show: read %s: %v\n", specPath, err)
		return 1
	}
	var spec types.AgentSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		fmt.Fprintf(os.Stderr, "show: parse agent.yml: %v\n", err)
		return 1
	}

	r := render.New()

	switch {
	case *full:
		fmt.Print(string(data))
		return 0

	case *reasoning:
		if spec.Provenance == nil || len(spec.Provenance.ReasoningDecisions) == 0 {
			fmt.Println("(no reasoning decisions recorded — was init --auto used?)")
			return 0
		}
		render.RenderReasoningTrace(r, spec.Provenance.ReasoningDecisions)
		return 0

	case *warnings:
		fmt.Println("(translation warnings come from `agent deploy`; conductor adapter not yet wired)")
		return 0

	case *persona:
		fmt.Println(spec.Persona.SystemPrompt)
		fmt.Println()
		personaPath := filepath.Join(name, "persona.md")
		if pdata, err := os.ReadFile(personaPath); err == nil {
			fmt.Println("─── persona.md ─────────────────────────────────────────")
			fmt.Print(string(pdata))
		}
		return 0

	case *deps:
		fmt.Println("Python:")
		for _, p := range spec.Workspace.Packages.Pip {
			fmt.Println("  •", p)
		}
		if len(spec.Workspace.Packages.Npm) > 0 {
			fmt.Println("\nNode:")
			for _, p := range spec.Workspace.Packages.Npm {
				fmt.Println("  •", p)
			}
		}
		return 0

	case *mcp:
		for _, s := range spec.MCPServers {
			fmt.Printf("✓ %s  type=%s  url=%s\n", s.Name, s.Type, s.URL)
		}
		if len(spec.MCPServers) == 0 {
			fmt.Println("(no MCP servers configured)")
		}
		return 0

	default:
		// Default: mid-density card. Filtered MCP / dev counts come from
		// .askdao/detection.json when present; we degrade gracefully if the
		// snapshot is missing.
		in := render.SummaryInput{
			Spec:    &spec,
			Harness: "Anthropic Managed Agents",
		}
		if spec.Provenance != nil {
			in.ReasoningSummary = spec.Provenance.ReasoningSummary
			in.ReasoningDecisions = spec.Provenance.ReasoningDecisions
		}
		render.RenderSummary(r, in)
		return 0
	}
}
