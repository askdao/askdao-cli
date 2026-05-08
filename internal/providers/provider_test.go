package providers

import (
	"os"
	"path/filepath"
	"testing"
)

// All providers must satisfy the Provider interface — this is the trait
// consistency test the issue calls out.
func TestProviderInterface(t *testing.T) {
	var _ Provider = (*PythonProvider)(nil)
	var _ Provider = (*NodeProvider)(nil)
}

func TestApp_IncludesAndRead(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "manage.py"), "import django\n")
	mustWriteFile(t, filepath.Join(root, "src", "main.py"), "from fastapi import FastAPI\n")
	// Excluded by shouldSkipProviderDir; should not appear in the index.
	mustWriteFile(t, filepath.Join(root, "node_modules", "noisy.js"), "x")

	app, err := NewApp(root)
	if err != nil {
		t.Fatal(err)
	}
	if !app.IncludesFile("manage.py") {
		t.Error("expected manage.py in index")
	}
	if !app.IncludesFile("src/main.py") {
		t.Error("expected src/main.py in index")
	}
	if app.IncludesFile("node_modules/noisy.js") {
		t.Error("node_modules should be skipped by the index walker")
	}

	data, err := app.ReadFile("manage.py")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "import django\n" {
		t.Errorf("ReadFile content mismatch: %q", string(data))
	}
}

func TestApp_FindFilesAndFindMatch(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.py"), "from fastapi import FastAPI")
	mustWriteFile(t, filepath.Join(root, "sub", "b.py"), "import os")
	mustWriteFile(t, filepath.Join(root, "c.txt"), "ignore me")

	app, err := NewApp(root)
	if err != nil {
		t.Fatal(err)
	}
	pys := app.FindFiles("**/*.py")
	if len(pys) != 2 {
		t.Errorf("expected 2 .py files, got %d (%v)", len(pys), pys)
	}
}

func TestEnv_ConfigOverride(t *testing.T) {
	env := NewEnv(map[string]string{"PYTHON_PACKAGE_MANAGER": "uv"})
	v, ok := env.GetConfigVariable("PYTHON_PACKAGE_MANAGER")
	if !ok || v != "uv" {
		t.Errorf("override missed: ok=%v v=%q", ok, v)
	}
	if _, ok := env.GetConfigVariable("DEFINITELY_NOT_SET_ANYWHERE_X"); ok {
		t.Error("nonexistent var should return ok=false")
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
