package observe

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallCleanup_NoPriorSettings(t *testing.T) {
	root := t.TempDir()
	path := settingsFile(root)

	cleanup, err := Install(root, 12345)
	if err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("settings not written: %v", err)
	}
	s := string(raw)
	if !strings.Contains(s, "/api/observe") || !strings.Contains(s, "12345") {
		t.Errorf("settings missing observe hook bound to port: %s", s)
	}
	if !strings.Contains(s, `"Skill"`) || !strings.Contains(s, `"mcp__.*"`) {
		t.Errorf("settings missing both matchers: %s", s)
	}

	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("cleanup should remove the file we created, stat err=%v", err)
	}
}

func TestInstallCleanup_PreservesPriorSettings(t *testing.T) {
	root := t.TempDir()
	path := settingsFile(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{
  "permissions": {
    "allow": ["Bash(ls:*)"]
  },
  "hooks": {
    "PreToolUse": [
      {"matcher": "Write", "hooks": [{"type": "command", "command": "echo hi"}]}
    ]
  }
}
`)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	cleanup, err := Install(root, 999)
	if err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	s := string(raw)
	if !strings.Contains(s, "/api/observe") {
		t.Errorf("observe rule not appended: %s", s)
	}
	if !strings.Contains(s, "Bash(ls:*)") {
		t.Errorf("user permissions lost: %s", s)
	}
	if !strings.Contains(s, `"Write"`) || !strings.Contains(s, "echo hi") {
		t.Errorf("user hook lost: %s", s)
	}

	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(path)
	if !bytes.Equal(restored, original) {
		t.Errorf("cleanup not byte-exact:\n got: %q\nwant: %q", restored, original)
	}
}

func TestSweepStale(t *testing.T) {
	root := t.TempDir()
	path := settingsFile(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulate a crashed run: observe rules left behind beside a user rule.
	leftover := `{
  "hooks": {
    "PreToolUse": [
      {"matcher": "Write", "hooks": [{"type": "command", "command": "echo hi"}]},
      {"matcher": "Skill", "hooks": [{"type": "http", "url": "http://127.0.0.1:5/api/observe", "timeout": 10}]},
      {"matcher": "mcp__.*", "hooks": [{"type": "http", "url": "http://127.0.0.1:5/api/observe", "timeout": 10}]}
    ]
  }
}`
	if err := os.WriteFile(path, []byte(leftover), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SweepStale(root); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if strings.Contains(s, "/api/observe") {
		t.Errorf("sweep left observe rules behind: %s", s)
	}
	if !strings.Contains(s, `"Write"`) {
		t.Errorf("sweep removed the user's own rule: %s", s)
	}

	// Idempotent / safe edge cases.
	if err := SweepStale(root); err != nil {
		t.Fatalf("sweep on already-clean file: %v", err)
	}
	if err := SweepStale(t.TempDir()); err != nil {
		t.Fatalf("sweep when no file exists should be a no-op: %v", err)
	}
}

// TestSweepStale_OnlyObserveRules confirms an empty PreToolUse is pruned, not left
// as {"hooks":{"PreToolUse":[]}} noise.
func TestSweepStale_OnlyObserveRules(t *testing.T) {
	root := t.TempDir()
	path := settingsFile(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	only := `{"hooks":{"PreToolUse":[{"matcher":"Skill","hooks":[{"type":"http","url":"http://127.0.0.1:5/api/observe"}]}]}}`
	if err := os.WriteFile(path, []byte(only), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SweepStale(root); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	s := string(raw)
	if strings.Contains(s, "PreToolUse") || strings.Contains(s, "hooks") {
		t.Errorf("empty containers not pruned: %s", s)
	}
}
