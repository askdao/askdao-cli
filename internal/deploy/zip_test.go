package deploy

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestZipDir_RootsEntriesUnderRootName(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "SKILL.md"), "---\nname: my-skill\n---\nhi\n")
	mustWrite(t, filepath.Join(src, "scripts", "run.py"), "print('hi')\n")
	mustWrite(t, filepath.Join(src, ".DS_Store"), "junk")

	data, err := ZipDir(src, "my-skill")
	if err != nil {
		t.Fatalf("ZipDir: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		got[f.Name] = string(b)
	}

	gotNames := make([]string, 0, len(got))
	for n := range got {
		gotNames = append(gotNames, n)
	}
	sort.Strings(gotNames)
	want := []string{"my-skill/SKILL.md", "my-skill/scripts/run.py"}
	if len(gotNames) != len(want) {
		t.Fatalf("entries = %v, want %v (.DS_Store must be skipped)", gotNames, want)
	}
	for i := range want {
		if gotNames[i] != want[i] {
			t.Errorf("entry[%d] = %q, want %q", i, gotNames[i], want[i])
		}
	}
	if got["my-skill/SKILL.md"] != "---\nname: my-skill\n---\nhi\n" {
		t.Errorf("SKILL.md content = %q", got["my-skill/SKILL.md"])
	}
	if got["my-skill/scripts/run.py"] != "print('hi')\n" {
		t.Errorf("run.py content = %q", got["my-skill/scripts/run.py"])
	}
}

func TestZipDir_MissingDir(t *testing.T) {
	if _, err := ZipDir(filepath.Join(t.TempDir(), "does-not-exist"), "x"); err == nil {
		t.Error("ZipDir on a missing directory should error")
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
