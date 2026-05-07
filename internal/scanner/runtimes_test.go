package scanner

import (
	"path/filepath"
	"testing"
)

func TestDetectRuntimes_AllSources(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "pyproject.toml"), `
[project]
name = "demo"
requires-python = ">=3.12,<3.14"
`)
	mustWrite(t, filepath.Join(root, ".nvmrc"), "v22\n")
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/demo\n\ngo 1.23.4\n")
	mustWrite(t, filepath.Join(root, "rust-toolchain.toml"), `
[toolchain]
channel = "1.83.0"
`)

	got, err := DetectRuntimes(root)
	if err != nil {
		t.Fatalf("DetectRuntimes: %v", err)
	}
	byKind := map[string]string{}
	for _, r := range got {
		byKind[r.Kind] = r.Version
	}
	if byKind["python"] != "3.12" {
		t.Errorf("python version = %q, want 3.12 (from constraint)", byKind["python"])
	}
	if byKind["node"] != "22" {
		t.Errorf("node version = %q, want 22 (v-prefix stripped)", byKind["node"])
	}
	if byKind["go"] != "1.23.4" {
		t.Errorf("go version = %q", byKind["go"])
	}
	if byKind["rust"] != "1.83.0" {
		t.Errorf("rust version = %q", byKind["rust"])
	}
}

func TestDetectRuntimes_PythonVersionFallback(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".python-version"), "3.11.5")
	got, err := DetectRuntimes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "python" || got[0].Version != "3.11.5" {
		t.Errorf("got %+v", got)
	}
	if got[0].Source != ".python-version" {
		t.Errorf("source = %q", got[0].Source)
	}
}

func TestDetectRuntimes_ToolVersions(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".tool-versions"),
		"# managed by asdf\nnodejs 20.10.0\npython 3.12.1\n")
	got, err := DetectRuntimes(root)
	if err != nil {
		t.Fatal(err)
	}
	byKind := map[string]string{}
	for _, r := range got {
		byKind[r.Kind] = r.Version
	}
	if byKind["node"] != "20.10.0" || byKind["python"] != "3.12.1" {
		t.Errorf("got %+v", byKind)
	}
}

func TestDetectRuntimes_Empty(t *testing.T) {
	root := t.TempDir()
	got, err := DetectRuntimes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}
