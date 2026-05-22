package scanner

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestAppDataDir_PerOS(t *testing.T) {
	home := t.TempDir()
	// Force the home-fallback branch so the result is deterministic across CI OSes.
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	var want string
	switch runtime.GOOS {
	case "darwin":
		want = filepath.Join(home, "Library", "Application Support")
	case "windows":
		want = filepath.Join(home, "AppData", "Roaming")
	default:
		want = filepath.Join(home, ".config")
	}
	if got := appDataDir(home); got != want {
		t.Errorf("appDataDir(%q) = %q, want %q", home, got, want)
	}
}

func TestUserPath_Resolve(t *testing.T) {
	home := t.TempDir()
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	if got, want := (userPath{baseHome, ".claude/skills"}).resolve(home),
		filepath.Join(home, ".claude/skills"); got != want {
		t.Errorf("baseHome resolve = %q, want %q", got, want)
	}
	if got, want := (userPath{baseAppData, "Claude/claude_desktop_config.json"}).resolve(home),
		filepath.Join(appDataDir(home), "Claude/claude_desktop_config.json"); got != want {
		t.Errorf("baseAppData resolve = %q, want %q", got, want)
	}
}

func TestActiveHarnesses(t *testing.T) {
	t.Setenv("APPDATA", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	has := func(hs []harnessConvention, name string) bool {
		for _, h := range hs {
			if h.name == name {
				return true
			}
		}
		return false
	}

	t.Run("codex marker .codex", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, ".codex", "config.toml"), "x")
		if !has(activeHarnesses(root, t.TempDir()), "codex") {
			t.Error(".codex/ marker should activate codex")
		}
	})

	t.Run("codex marker .agents (no .codex)", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, ".agents", "skills", "x", "SKILL.md"), "# x\n")
		if !has(activeHarnesses(root, t.TempDir()), "codex") {
			t.Error(".agents/ marker should activate codex (decision: project with only .agents/skills counts as Codex)")
		}
	})

	t.Run("claude marker", func(t *testing.T) {
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, ".claude", "settings.json"), "{}")
		if !has(activeHarnesses(root, t.TempDir()), "claude") {
			t.Error(".claude/ marker should activate claude")
		}
	})

	t.Run("cowork: markerless, gated by app config existence", func(t *testing.T) {
		root := t.TempDir() // deliberately no project marker
		home := t.TempDir()
		// Before the config exists, Cowork must NOT activate.
		if has(activeHarnesses(root, home), "cowork") {
			t.Error("cowork must not activate without its config file")
		}
		// Create Cowork's app-data MCP config → now it self-gates active.
		mustWrite(t, filepath.Join(appDataDir(home), "Claude", "claude_desktop_config.json"), "{}")
		if !has(activeHarnesses(root, home), "cowork") {
			t.Error("cowork should activate once claude_desktop_config.json exists")
		}
	})

	t.Run("empty root + no app → none", func(t *testing.T) {
		if got := activeHarnesses(t.TempDir(), t.TempDir()); len(got) != 0 {
			t.Errorf("expected no active harnesses, got %+v", got)
		}
	})
}

func TestHarnessForProjectDir(t *testing.T) {
	cases := map[string]string{
		".claude/skills": "claude",
		".agents/skills": "codex",
		".codex":         "codex",
		".codex/config":  "codex",
		"skills":         "",
		".cursor/skills": "", // Cursor removed → generic
	}
	for rel, want := range cases {
		if got := harnessForProjectDir(rel); got != want {
			t.Errorf("harnessForProjectDir(%q) = %q, want %q", rel, got, want)
		}
	}
}

func TestHasPathPrefix(t *testing.T) {
	type tc struct {
		p, prefix string
		want      bool
	}
	for _, c := range []tc{
		{".claude", ".claude", true},
		{".claude/skills", ".claude", true},
		{".claudex", ".claude", false}, // sibling, not under
		{"skills", ".claude", false},
	} {
		if got := hasPathPrefix(c.p, c.prefix); got != c.want {
			t.Errorf("hasPathPrefix(%q, %q) = %v, want %v", c.p, c.prefix, got, c.want)
		}
	}
}
