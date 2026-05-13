package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/askdao/askdao-cli/internal/pipeline"
	"github.com/askdao/askdao-cli/internal/render"
)

// runBundle implements `askdao bundle [path]`. It previews the deployment
// payload — the explicit list of files that would be uploaded when the agent
// is deployed, the vendored skills re-installed from their lockfile sources,
// and everything deliberately left out with reasons. It does NOT package or
// upload anything (that waits on conductor #11's deploy endpoint).
func runBundle(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("bundle", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "Print the deployment_payload as JSON")
	showWarnings := fs.Bool("warnings", false, "Print all warnings in full")
	noEvals := fs.Bool("no-evals", false, "Drop skill evals/ subdirectories from the payload")
	bundleSkill := fs.String("bundle-skill", "", "Comma-separated vendored skills to ship inline instead of as references")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}

	var force []string
	if *bundleSkill != "" {
		for _, s := range strings.Split(*bundleSkill, ",") {
			if s = strings.TrimSpace(s); s != "" {
				force = append(force, s)
			}
		}
	}

	res, err := pipeline.Run(ctx, pipeline.Options{
		Root:              root,
		IncludeEvals:      !*noEvals,
		ForceBundleSkills: force,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bundle: %v\n", err)
		return 1
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(struct {
			Archetype         any `json:"archetype"`
			DeploymentPayload any `json:"deployment_payload"`
		}{res.Detection.Archetype, res.Detection.DeploymentPayload}); err != nil {
			fmt.Fprintf(os.Stderr, "bundle: encode: %v\n", err)
			return 1
		}
		return 0
	}

	r := render.New()
	render.RenderPayload(r, root, res.Detection.DeploymentPayload, res.Detection.Archetype, true)

	if len(res.Warnings) > 0 {
		fmt.Println()
		if *showWarnings {
			for _, w := range res.Warnings {
				fmt.Println("⚠ " + w)
			}
		} else {
			fmt.Printf("⚠ %d warning(s) — run `askdao bundle --warnings` to see them\n", len(res.Warnings))
		}
	}
	return 0
}
