package pipeline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/askdao/askdao-cli/internal/recommender"
	"github.com/askdao/askdao-cli/internal/types"
)

// hermeticOpts builds an Options that runs against a temp tree without ever
// invoking syft (we feed canned package data via SyftRunner) and with HOME
// pointed at an empty dir so harness probes return ✗.
func hermeticOpts(root, home string, llm recommender.LLMClient) Options {
	return Options{
		Root:      root,
		AgentName: "demo",
		HomeDir:   home,
		LLM:       llm,
		SyftRunner: func(_ context.Context, _ []string) ([]byte, error) {
			return []byte(`{"artifacts":[
				{"name":"fastapi","version":"0.135.1","type":"python"},
				{"name":"sqlalchemy","version":"2.0.48","type":"python"},
				{"name":"pytest","version":"9.0.2","type":"python"}
			]}`), nil
		},
	}
}

func writeFixture(t *testing.T, root string) {
	t.Helper()
	mustMkdir(t, root)
	mustMkdir(t, filepath.Join(root, ".github", "workflows"))
	must(t, os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte(`
[project]
name = "demo"
requires-python = ">=3.12"

[dependency-groups]
dev = ["pytest"]
`), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "main.py"), []byte("from fastapi import FastAPI\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, ".github", "workflows", "deploy.yml"), []byte("name: deploy\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, ".env.example"), []byte("ANTHROPIC_API_KEY=\nGITHUB_TOKEN=\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{
		"mcpServers": {"github": {"type": "url", "url": "https://api.githubcopilot.com/mcp/"}}
	}`), 0o644))
}

func TestPipeline_DetectOnly_NoLLM(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root)

	res, err := Run(context.Background(), hermeticOpts(root, t.TempDir(), nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Recommendation != nil {
		t.Errorf("expected no recommendation when LLM is nil")
	}

	det := res.Detection
	// Languages.
	if len(det.DetectedLanguages) == 0 {
		t.Error("expected at least one language detected")
	}
	// Packages: fastapi prod, pytest dev.
	prodCount, devCount := 0, 0
	for _, p := range det.DetectedPackages["pip"] {
		if p.IsProd {
			prodCount++
		} else {
			devCount++
		}
	}
	if prodCount < 2 {
		t.Errorf("expected ≥2 prod pip pkgs (fastapi, sqlalchemy), got %d", prodCount)
	}
	if devCount < 1 {
		t.Errorf("expected pytest to be marked dev, got %d dev pkgs", devCount)
	}

	// Frameworks: FastAPI should be detected.
	hasFastAPI := false
	for _, f := range det.DetectedFrameworks {
		if f.Name == "FastAPI" {
			hasFastAPI = true
		}
	}
	if !hasFastAPI {
		t.Errorf("FastAPI not detected: %+v", det.DetectedFrameworks)
	}

	// Policy: deploy.yml present → bash override.
	if len(det.DetectedToolRiskHints.ProductionSignals) == 0 {
		t.Error("policy did not pick up deploy.yml signal")
	}
	hasBashOverride := false
	for _, o := range det.DetectedToolRiskHints.ToolOverridesRecommended {
		if o.Tool == "bash" {
			hasBashOverride = true
		}
	}
	if !hasBashOverride {
		t.Error("expected bash tool override under production signal")
	}

	// Secrets: ANTHROPIC_API_KEY required + GITHUB_TOKEN cross-linked to github MCP.
	gotSecrets := map[string]types.DetectedRequiredSecret{}
	for _, s := range det.DetectedRequiredSecrets {
		gotSecrets[s.Name] = s
	}
	if !gotSecrets["ANTHROPIC_API_KEY"].Required {
		t.Error("ANTHROPIC_API_KEY should be required")
	}
	if gh := gotSecrets["GITHUB_TOKEN"]; gh.UsedByGuess == nil || gh.UsedByGuess.MCPServer != "github" {
		t.Errorf("GITHUB_TOKEN cross-link missing: %+v", gh)
	}
}

func TestPipeline_WithMockLLM_ProducesAgentSpec(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root)

	mock := &recommender.MockClient{}
	res, err := Run(context.Background(), hermeticOpts(root, t.TempDir(), mock))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Recommendation == nil {
		t.Fatal("expected recommendation when LLM is set")
	}
	spec := res.Recommendation.Spec
	if spec.APIVersion != types.AgentSpecAPIVersion {
		t.Errorf("apiVersion = %q", spec.APIVersion)
	}
	if spec.Metadata.Name != "demo" {
		t.Errorf("metadata.name = %q", spec.Metadata.Name)
	}
	// MockClient picks shell.permission=ask_for_dangerous when prod signals
	// are present — this is the contract pipeline + recommender share.
	if got := spec.Capabilities.Shell.Permission; got != "ask_for_dangerous" {
		t.Errorf("shell.permission = %q, want ask_for_dangerous", got)
	}
}

func TestPipeline_SyftAbsentDoesNotFail(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root)

	opts := Options{
		Root:    root,
		HomeDir: t.TempDir(),
		// No SyftRunner; runSyft will check PATH and degrade with a warning.
	}
	res, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run should not error when syft is missing: %v", err)
	}
	hasSyftWarning := false
	for _, w := range res.Warnings {
		if len(w) >= 4 && w[:4] == "syft" {
			hasSyftWarning = true
		}
	}
	// Either syft IS on PATH (CI runner) or we get a soft warning. Both are OK.
	if !hasSyftWarning {
		if _, err := exec.LookPath("syft"); err != nil {
			t.Errorf("syft missing but no warning surfaced: %+v", res.Warnings)
		}
	}
	// Languages should still be populated either way.
	if len(res.Detection.DetectedLanguages) == 0 {
		t.Error("language detection should run regardless of syft availability")
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
