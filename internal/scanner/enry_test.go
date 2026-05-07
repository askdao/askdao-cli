package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLanguages_TempTree(t *testing.T) {
	root := t.TempDir()

	mustWrite(t, filepath.Join(root, "main.go"),
		"package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"hi\") }\n")
	mustWrite(t, filepath.Join(root, "lib", "util.go"),
		"package lib\n\nfunc Add(a, b int) int { return a + b }\n")
	mustWrite(t, filepath.Join(root, "script.py"),
		"def hello():\n    print('hi')\n\nhello()\n")

	// Excluded directory should not contribute.
	mustWrite(t, filepath.Join(root, "excluded", "noisy.go"),
		"package excluded\n\nfunc x() {}\n")

	// node_modules is auto-skipped — it must not influence percentages.
	mustWrite(t, filepath.Join(root, "node_modules", "noise.js"),
		"console.log('skip');\n")

	got, err := DetectLanguages(root, []string{"./excluded/**"})
	if err != nil {
		t.Fatalf("DetectLanguages: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least one detected language")
	}

	langs := map[string]float64{}
	for _, e := range got {
		langs[e.Language] = e.Percentage
	}
	if _, ok := langs["Go"]; !ok {
		t.Errorf("Go not detected: %+v", got)
	}
	if _, ok := langs["Python"]; !ok {
		t.Errorf("Python not detected: %+v", got)
	}
	if _, ok := langs["JavaScript"]; ok {
		t.Errorf("JavaScript should have been skipped via node_modules guard, got %+v", got)
	}

	var sum float64
	for _, e := range got {
		sum += e.Percentage
	}
	if sum < 99.0 || sum > 100.5 {
		t.Errorf("percentages should sum to ~100, got %.2f", sum)
	}
}

func TestDetectLanguages_EmptyRoot(t *testing.T) {
	root := t.TempDir()
	got, err := DetectLanguages(root, nil)
	if err != nil {
		t.Fatalf("DetectLanguages: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty tree → expected zero languages, got %+v", got)
	}
}

func TestDetectLanguages_RootRequired(t *testing.T) {
	if _, err := DetectLanguages("", nil); err == nil {
		t.Fatal("expected error for empty root")
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
