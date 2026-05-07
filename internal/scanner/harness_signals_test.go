package scanner

import (
	"path/filepath"
	"testing"
)

func TestDetectHarnessSignals_ClaudeCode(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".claude", "skills", "foo", "SKILL.md"), "# foo\n")
	mustWrite(t, filepath.Join(home, ".claude", "skills", "bar", "SKILL.md"), "# bar\n")

	got, err := DetectHarnessSignals(HarnessProbeOpts{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	if !got.ClaudeCode.Installed {
		t.Error("expected claude_code installed")
	}
	if got.Codex.Installed || got.Cursor.Installed || got.GeminiCLI.Installed {
		t.Errorf("only claude_code should be installed, got %+v", got)
	}
	if got.RecommendedHarness != "anthropic_managed_agents" {
		t.Errorf("recommended = %q", got.RecommendedHarness)
	}
	// Evidence should mention the 2 skill files we planted.
	if len(got.ClaudeCode.Evidence) < 2 {
		t.Errorf("expected ~/.claude/ skills evidence, got %+v", got.ClaudeCode.Evidence)
	}
}

func TestDetectHarnessSignals_CodexOnly(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".codex", "marker"), "")

	got, err := DetectHarnessSignals(HarnessProbeOpts{HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Codex.Installed {
		t.Error("expected codex installed")
	}
	if got.RecommendedHarness != "openai_agents_sdk" {
		t.Errorf("recommended = %q, want openai_agents_sdk", got.RecommendedHarness)
	}
}

func TestDetectHarnessSignals_NothingInstalled(t *testing.T) {
	got, err := DetectHarnessSignals(HarnessProbeOpts{HomeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got.RecommendedHarness != "anthropic_managed_agents" {
		t.Errorf("default should be anthropic_managed_agents, got %q", got.RecommendedHarness)
	}
}
