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

// runDetect implements `askdao detect [path]`. Prints detection.json to stdout
// (jsonl-friendly so KOLs / scripts can pipe it). When --summary is set, prints
// a short human-readable digest instead.
func runDetect(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	summary := fs.Bool("summary", false, "Print short human-readable digest instead of JSON")
	pretty := fs.Bool("pretty", true, "Pretty-print JSON output")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}

	res, err := pipeline.Run(ctx, pipeline.Options{Root: root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "detect: %v\n", err)
		return 1
	}

	for _, w := range res.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}

	if *summary {
		printDetectSummary(res)
		return 0
	}

	enc := json.NewEncoder(os.Stdout)
	if *pretty {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(res.Detection); err != nil {
		fmt.Fprintf(os.Stderr, "detect: encode: %v\n", err)
		return 1
	}
	return 0
}

// printDetectSummary emits the short form shown in design.md §3.2:
//
//	Languages: Python 67% · TypeScript 26% · Shell 7%
//	Frameworks: FastAPI (conf=0.95) · SQLAlchemy (conf=0.92)
//	Production deps: 28 pip / 12 npm
//	System pkgs: libpq-dev gcc libjpeg-dev
//	Lockfiles: uv.lock pnpm-lock.yaml
//	Harness signals: claude-code ✓ codex ✗ cursor ✗
func printDetectSummary(res *pipeline.Result) {
	d := res.Detection

	fmt.Print("Languages: ")
	if len(d.DetectedLanguages) == 0 {
		fmt.Println("(none detected)")
	} else {
		for i, l := range d.DetectedLanguages {
			if i > 0 {
				fmt.Print(" · ")
			}
			fmt.Printf("%s %.0f%%", l.Language, l.Percentage)
			if i >= 3 {
				fmt.Print(" · …")
				break
			}
		}
		fmt.Println()
	}

	fmt.Print("Frameworks: ")
	if len(d.DetectedFrameworks) == 0 {
		fmt.Println("(none detected)")
	} else {
		for i, f := range d.DetectedFrameworks {
			if i > 0 {
				fmt.Print(" · ")
			}
			fmt.Printf("%s (conf=%.2f)", f.Name, f.Confidence)
		}
		fmt.Println()
	}

	prodPip := 0
	prodNpm := 0
	for _, p := range d.DetectedPackages["pip"] {
		if p.IsProd {
			prodPip++
		}
	}
	for _, p := range d.DetectedPackages["npm"] {
		if p.IsProd {
			prodNpm++
		}
	}
	fmt.Printf("Production deps: %d pip / %d npm\n", prodPip, prodNpm)

	fmt.Print("System pkgs: ")
	if len(d.InferredAptPackages) == 0 {
		fmt.Println("(none inferred)")
	} else {
		for i, a := range d.InferredAptPackages {
			if i > 0 {
				fmt.Print(" ")
			}
			fmt.Print(a.Name)
		}
		fmt.Println()
	}

	fmt.Print("Harness signals: ")
	hs := d.DetectedHarnessSignals
	fmt.Printf("claude-code %s · codex %s · cursor %s · gemini-cli %s\n",
		check(hs.ClaudeCode.Installed), check(hs.Codex.Installed),
		check(hs.Cursor.Installed), check(hs.GeminiCLI.Installed))

	render.RenderPayload(render.New(), d.Scan.Root, d.DeploymentPayload, d.Archetype, false)
}

func check(b bool) string {
	if b {
		return "✓"
	}
	return "✗"
}
