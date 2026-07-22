package recents

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// isolate points recents (via auth.ConfigDir) at a per-test temp config dir and
// returns the resolved askdao dir. Mirrors credentials_test's isolation.
func isolate(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	return filepath.Join(base, "askdao")
}

func dirs(f *File) []string {
	out := make([]string, len(f.Projects))
	for i, p := range f.Projects {
		out[i] = p.Dir
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSaveLoadRoundTrip(t *testing.T) {
	isolate(t)
	v := 2
	now := time.Now().UTC().Truncate(time.Second)
	f := &File{Version: SchemaVersion, Projects: []Project{{
		Dir: "/a", Label: "A", LastOpenedAt: now,
		Deploy: &DeployRecord{
			AgentID: "agt_1", MetadataName: "a", Created: true,
			PreviousManagedVersion: &v, GroupLink: "https://g", LastDeployedAt: now,
		},
	}}}
	if err := Save(f); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Projects) != 1 || got.Projects[0].Dir != "/a" {
		t.Fatalf("round-trip dir mismatch: %+v", got)
	}
	d := got.Projects[0].Deploy
	if d == nil || d.AgentID != "agt_1" || d.MetadataName != "a" || !d.Created {
		t.Fatalf("deploy record not preserved: %+v", d)
	}
	if d.PreviousManagedVersion == nil || *d.PreviousManagedVersion != 2 {
		t.Fatalf("PreviousManagedVersion not preserved: %+v", d.PreviousManagedVersion)
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	isolate(t)
	f, err := Load()
	if err != nil {
		t.Fatalf("Load on missing should not error: %v", err)
	}
	if len(f.Projects) != 0 || f.Version != SchemaVersion {
		t.Fatalf("want empty File, got %+v", f)
	}
}

func TestLoadCorruptReturnsEmpty(t *testing.T) {
	root := isolate(t)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, fileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load()
	if err != nil {
		t.Fatalf("Load on corrupt should not error (convenience cache): %v", err)
	}
	if len(f.Projects) != 0 {
		t.Fatalf("corrupt file should degrade to empty, got %+v", f)
	}
}

func TestLoadFutureVersionReturnsEmpty(t *testing.T) {
	root := isolate(t)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"version":999,"projects":[{"dir":"/x"}]}`)
	if err := os.WriteFile(filepath.Join(root, fileName), body, 0o600); err != nil {
		t.Fatal(err)
	}
	f, _ := Load()
	if len(f.Projects) != 0 {
		t.Fatalf("unknown schema version should degrade to empty, got %+v", f)
	}
}

func TestTouchMRUOrder(t *testing.T) {
	f := &File{Version: SchemaVersion}
	f.Touch("/a", "")
	f.Touch("/b", "")
	f.Touch("/c", "")
	if got := dirs(f); !eq(got, []string{"/c", "/b", "/a"}) {
		t.Fatalf("insert order = %v, want [/c /b /a]", got)
	}
	f.Touch("/a", "") // re-touch moves to front
	if got := dirs(f); !eq(got, []string{"/a", "/c", "/b"}) {
		t.Fatalf("re-touch order = %v, want [/a /c /b]", got)
	}
}

func TestTouchEvictsBeyondMax(t *testing.T) {
	f := &File{Version: SchemaVersion}
	for i := 0; i < maxEntries+3; i++ {
		f.Touch(filepath.Join("/p", fmt.Sprint(i)), "")
	}
	if len(f.Projects) != maxEntries {
		t.Fatalf("len = %d, want maxEntries %d", len(f.Projects), maxEntries)
	}
	if want := filepath.Join("/p", fmt.Sprint(maxEntries+2)); f.Projects[0].Dir != want {
		t.Fatalf("front = %q, want most-recent %q", f.Projects[0].Dir, want)
	}
}

func TestTouchPreservesDeploy(t *testing.T) {
	f := &File{Version: SchemaVersion}
	f.Touch("/a", "A")
	f.SetDeploy("/a", DeployRecord{AgentID: "agt_x", MetadataName: "a"})
	f.Touch("/b", "")
	f.Touch("/a", "A2") // re-touch must not drop the deploy record
	p := f.Find("/a")
	if p == nil || p.Deploy == nil || p.Deploy.AgentID != "agt_x" {
		t.Fatalf("deploy record lost on re-touch: %+v", p)
	}
	if p.Label != "A2" {
		t.Fatalf("label not updated on re-touch: %q", p.Label)
	}
}

func TestSetDeployCreatesWhenAbsent(t *testing.T) {
	f := &File{Version: SchemaVersion}
	f.SetDeploy("/never-touched", DeployRecord{AgentID: "agt_y", MetadataName: "y"})
	p := f.Find("/never-touched")
	if p == nil || p.Deploy == nil || p.Deploy.AgentID != "agt_y" {
		t.Fatalf("SetDeploy on absent dir did not create entry: %+v", p)
	}
}

func TestNormalizeDedup(t *testing.T) {
	f := &File{Version: SchemaVersion}
	f.Touch("/foo", "")
	f.Touch("/foo/", "") // trailing slash collapses to same key
	if len(f.Projects) != 1 {
		t.Fatalf("/foo and /foo/ should dedup, got %d entries: %v", len(f.Projects), dirs(f))
	}
	if f.Projects[0].Dir != "/foo" {
		t.Fatalf("normalized dir = %q, want /foo", f.Projects[0].Dir)
	}
}

func TestRemoveIdempotent(t *testing.T) {
	f := &File{Version: SchemaVersion}
	f.Touch("/a", "")
	f.Remove("/a")
	if f.Find("/a") != nil {
		t.Fatalf("entry not removed")
	}
	f.Remove("/a") // second remove is a no-op, must not panic
}

func TestSavePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	root := isolate(t)
	if err := Save(&File{Version: SchemaVersion}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	fi, err := os.Stat(filepath.Join(root, fileName))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("file perm = %o, want 600", fi.Mode().Perm())
	}
	di, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir perm = %o, want 700", di.Mode().Perm())
	}
}
