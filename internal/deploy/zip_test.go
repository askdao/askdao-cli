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

// TestZipDir_IgnoreFilter 守护安全过滤：默认排除 dotenv / node_modules / .git / 编辑器垃圾，
// .askdaoignore 兜底排除项目特有路径；SKILL.md 与合法 skill 文件保留。
func TestZipDir_IgnoreFilter(t *testing.T) {
	src := t.TempDir()
	// 应保留
	mustWrite(t, filepath.Join(src, "SKILL.md"), "---\nname: my-skill\n---\nhi\n")
	mustWrite(t, filepath.Join(src, "assets", "logo.png"), "PNG")
	// 默认排除（安全 / 垃圾）
	mustWrite(t, filepath.Join(src, ".env"), "SECRET=shhh")
	mustWrite(t, filepath.Join(src, ".env.production"), "TOKEN=leak")
	mustWrite(t, filepath.Join(src, "node_modules", "left-pad", "index.js"), "module.exports={}")
	mustWrite(t, filepath.Join(src, ".git", "config"), "[core]")
	mustWrite(t, filepath.Join(src, "notes.swp"), "vim junk")
	// .askdaoignore 兜底排除 + 反向纳入
	mustWrite(t, filepath.Join(src, ".askdaoignore"), "build/\nsecret_notes.md\n!assets/logo.png\n")
	mustWrite(t, filepath.Join(src, "build", "out.txt"), "artifact")
	mustWrite(t, filepath.Join(src, "secret_notes.md"), "private")

	data, err := ZipDir(src, "my-skill")
	if err != nil {
		t.Fatalf("ZipDir: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	got := map[string]bool{}
	for _, f := range zr.File {
		got[f.Name] = true
	}

	wantPresent := []string{"my-skill/SKILL.md", "my-skill/assets/logo.png"}
	for _, n := range wantPresent {
		if !got[n] {
			t.Errorf("expected %q present, zip = %v", n, keys(got))
		}
	}
	wantAbsent := []string{
		"my-skill/.env",
		"my-skill/.env.production",
		"my-skill/node_modules/left-pad/index.js",
		"my-skill/.git/config",
		"my-skill/notes.swp",
		"my-skill/.askdaoignore",
		"my-skill/build/out.txt",
		"my-skill/secret_notes.md",
	}
	for _, n := range wantAbsent {
		if got[n] {
			t.Errorf("expected %q EXCLUDED, but it was packed", n)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
