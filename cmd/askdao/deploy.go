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

// runDeploy implements `askdao agent deploy [--harness id]`. Phase 1 stops at
// the diff preview because conductor #11 (the deploy endpoint) isn't yet
// available — we still surface a useful preview so KOLs can review their
// edits before the endpoint lands.
func runDeploy(_ context.Context, args []string) int {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	harness := fs.String("harness", "", "Override preferred_harness from agent.yml")
	dir := fs.String("dir", ".", "Agent directory containing agent.yml")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	editedPath := filepath.Join(*dir, "agent.yml")
	originalPath := filepath.Join(*dir, ".askdao", "recommendation.yml")

	edited, err := readSpec(editedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deploy: %v\n", err)
		return 1
	}
	original, err := readSpec(originalPath)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"deploy: original recommendation snapshot missing at %s\n"+
				"        run `askdao agent init <name> --auto` first to seed it.\n",
			originalPath)
		return 1
	}

	r := render.New()
	fmt.Println("→ Reading", editedPath)
	diffs := render.DiffAgentSpec(original, edited)
	if len(diffs) == 0 {
		fmt.Println("→ No fields changed since last recommendation.")
	} else {
		fmt.Printf("→ You modified %d field(s) since the last recommendation:\n\n", len(diffs))
		render.RenderDiff(r, diffs)
	}

	target := edited.PreferredHarness
	if *harness != "" {
		target = *harness
	}
	if target == "" {
		target = "anthropic_managed_agents"
	}

	conductor := os.Getenv("ASKDAO_CONDUCTOR_URL")
	if conductor == "" {
		fmt.Println()
		fmt.Println("✗ deploy: ASKDAO_CONDUCTOR_URL is not set.")
		fmt.Println("  Conductor endpoint #11 is not yet wired in Phase 1; deploy is a stub.")
		fmt.Println("  Set ASKDAO_CONDUCTOR_URL=https://... once the endpoint ships, then re-run.")
		fmt.Printf("  Target harness would be: %s\n", target)
		return 3
	}

	// When ASKDAO_CONDUCTOR_URL is set, Phase 1 still doesn't push — we just
	// print what would happen. Real push lives behind conductor #11.
	fmt.Println()
	fmt.Printf("✗ deploy: conductor recommend reachable at %s, but the deploy\n", conductor)
	fmt.Println("  endpoint is not part of Phase 1. The diff above is for your review;")
	fmt.Println("  no spec was pushed.")
	return 3
}

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
