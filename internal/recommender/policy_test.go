package recommender

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInferToolRiskHints_ProductionDeploy(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".github", "workflows", "deploy.yml"),
		"name: deploy\non: push\n")
	mustWrite(t, filepath.Join(root, "config", "production.toml"),
		"[server]\nhost = \"prod.example.com\"\n")

	hints, err := InferToolRiskHints(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(hints.ProductionSignals) < 2 {
		t.Errorf("expected ≥2 production signals, got %+v", hints.ProductionSignals)
	}
	// Acceptance criterion: shell.permission must be flipped to ask_for_dangerous
	// when production is detected. The recommender uses tool overrides for
	// bash + write rather than flipping the default policy itself.
	gotOverrides := map[string]string{}
	for _, o := range hints.ToolOverridesRecommended {
		gotOverrides[o.Tool] = o.Policy
	}
	if gotOverrides["bash"] != "always_ask" {
		t.Errorf("bash should be always_ask under production, got %+v", gotOverrides)
	}
	if gotOverrides["write"] != "always_ask" {
		t.Errorf("write should be always_ask under production, got %+v", gotOverrides)
	}
	if hints.RecommendedDefaultPolicy != "always_allow" {
		t.Errorf("default policy should remain always_allow with overrides; got %q",
			hints.RecommendedDefaultPolicy)
	}
}

func TestInferToolRiskHints_NoSignals(t *testing.T) {
	hints, err := InferToolRiskHints(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(hints.ProductionSignals) != 0 {
		t.Errorf("clean tree should have zero production signals: %+v", hints)
	}
	if len(hints.ToolOverridesRecommended) != 0 {
		t.Errorf("no signals → no overrides expected, got %+v", hints.ToolOverridesRecommended)
	}
	if hints.RecommendedDefaultPolicy != "always_allow" {
		t.Errorf("default = %q, want always_allow", hints.RecommendedDefaultPolicy)
	}
}

func TestInferToolRiskHints_GlobAndDirSignals(t *testing.T) {
	root := t.TempDir()
	// Glob pattern: deploy-prod.yml in workflows.
	mustWrite(t, filepath.Join(root, ".github", "workflows", "deploy-prod.yml"), "")
	// Dir signal: terraform/ existing.
	if err := os.MkdirAll(filepath.Join(root, "terraform"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Top-level *.tf glob.
	mustWrite(t, filepath.Join(root, "main.tf"), "")
	// User-data signal: data/ dir.
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}

	hints, _ := InferToolRiskHints(root)

	prodSignals := map[string]bool{}
	for _, s := range hints.ProductionSignals {
		prodSignals[s.Signal] = true
	}
	if !prodSignals["AWS deploy workflow detected"] {
		t.Errorf("missing deploy-*.yml signal: %+v", hints.ProductionSignals)
	}
	if !prodSignals["Terraform manifests directory present"] {
		t.Errorf("missing terraform dir signal: %+v", hints.ProductionSignals)
	}
	if !prodSignals["Terraform .tf files at project root"] {
		t.Errorf("missing *.tf glob signal: %+v", hints.ProductionSignals)
	}

	if len(hints.UserDataSignals) == 0 {
		t.Errorf("data/ dir should produce a user-data signal: %+v", hints.UserDataSignals)
	}
}

func TestInferToolRiskHints_RootRequired(t *testing.T) {
	if _, err := InferToolRiskHints(""); err == nil {
		t.Error("expected error for empty root")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
