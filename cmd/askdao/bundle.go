package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/askdao/askdao-cli/internal/pipeline"
	"github.com/askdao/askdao-cli/internal/render"
)

// runBundle implements `askdao bundle [path]`. It previews the deployment
// payload — the explicit list of files that would be uploaded when the agent
// is deployed, with everything deliberately left out and reasons. It does NOT
// package or upload anything (that's what `askdao agent deploy` does).
//
// v0.7: every custom skill (repo-native and vendored alike) appears in
// WILL UPLOAD with an inline origin tag. The previous `--bundle-skill` flag
// is gone — there is no "reference, don't upload" branch to opt out of.
func runBundle(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("bundle", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "Print the deployment_payload as JSON")
	showWarnings := fs.Bool("warnings", false, "Print all warnings in full")
	noEvals := fs.Bool("no-evals", false, "Drop skill evals/ subdirectories from the payload")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}

	res, err := pipeline.Run(ctx, pipeline.Options{
		Root:         root,
		IncludeEvals: !*noEvals,
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
