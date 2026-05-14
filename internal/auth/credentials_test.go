// [INPUT]: 依赖 testing + os + path/filepath + time + runtime
// [OUTPUT]: TestCredentials* — Save / Load / Delete roundtrip; sentinel error
//          on missing file; corruption detection; schema-version refuse;
//          0600 mode invariant (POSIX-only sub-case)
// [POS]: internal/auth/ — unit-test layer for credentials.go; isolates the
//        config dir via t.Setenv("XDG_CONFIG_HOME", t.TempDir()) so tests
//        never touch the developer's real $HOME.
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package auth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// isolatedConfigDir routes the askdao config dir to a per-test tempdir.
func isolatedConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// On macOS / Windows os.UserConfigDir wins over XDG_CONFIG_HOME only
	// when XDG is empty — explicitly setting it forces our credentials.go
	// path to the configDir() XDG branch, giving us deterministic placement.
	return dir
}

func TestSaveLoadRoundtrip(t *testing.T) {
	root := isolatedConfigDir(t)

	c := &Credentials{
		Server:      "https://api.test",
		UserID:      "usr_abc",
		UserEmail:   "sam@askdao.ai",
		AccessToken: "cli_xxxxxx",
	}
	if err := Save(c); err != nil {
		t.Fatalf("Save: %v", err)
	}

	expected := filepath.Join(root, "askdao", "credentials.json")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("Save: file not at expected path %s: %v", expected, err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.AccessToken != c.AccessToken || got.UserEmail != c.UserEmail {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if got.Version != credentialsSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", credentialsSchemaVersion, got.Version)
	}
	if got.CreatedAt.IsZero() {
		t.Fatalf("CreatedAt should be auto-stamped by Save")
	}
}

func TestSaveAtomic0600Mode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("0600 mode is POSIX-only; Windows uses ACLs")
	}
	_ = isolatedConfigDir(t)

	if err := Save(&Credentials{
		Server: "https://api.test", AccessToken: "cli_x", UserEmail: "a@b.c", UserID: "u",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode = %o, want 0600", mode)
	}
	// Parent dir should be 0700.
	pi, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if mode := pi.Mode().Perm(); mode != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", mode)
	}
}

func TestLoadReturnsErrNoCredentialsWhenMissing(t *testing.T) {
	_ = isolatedConfigDir(t)

	_, err := Load()
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("expected ErrNoCredentials, got %v", err)
	}
}

func TestLoadRejectsFutureSchemaVersion(t *testing.T) {
	_ = isolatedConfigDir(t)

	// Hand-write a credentials file with version 999.
	path, _ := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"version": 999, "server": "x", "access_token": "y", "user_id": "u", "user_email": "e", "created_at": "2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "schema version 999") {
		t.Fatalf("expected schema-version error, got %v", err)
	}
}

func TestLoadRejectsCorruptedJSON(t *testing.T) {
	_ = isolatedConfigDir(t)

	path, _ := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestSaveRejectsEmptyFields(t *testing.T) {
	_ = isolatedConfigDir(t)
	err := Save(&Credentials{Server: "x"}) // missing access_token
	if err == nil {
		t.Fatal("expected error on empty access_token")
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	_ = isolatedConfigDir(t)
	if err := Save(&Credentials{Server: "x", AccessToken: "cli_x", UserID: "u", UserEmail: "e"}); err != nil {
		t.Fatal(err)
	}
	if err := Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := Load(); !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("after Delete, expected ErrNoCredentials, got %v", err)
	}
}

func TestDeleteReturnsErrNoCredentialsWhenMissing(t *testing.T) {
	_ = isolatedConfigDir(t)
	err := Delete()
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("expected ErrNoCredentials, got %v", err)
	}
}

// Sanity: confirm Path is stable across two calls in the same process; tests
// that bake the file location into expectations rely on it.
func TestPathStable(t *testing.T) {
	_ = isolatedConfigDir(t)
	a, _ := Path()
	b, _ := Path()
	if a != b {
		t.Fatalf("Path drift: %s vs %s", a, b)
	}
}

func TestCreatedAtPreservedIfSetExplicitly(t *testing.T) {
	_ = isolatedConfigDir(t)
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := Save(&Credentials{
		Server: "x", AccessToken: "cli_x", UserID: "u", UserEmail: "e", CreatedAt: fixed,
	}); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.CreatedAt.Equal(fixed) {
		t.Fatalf("CreatedAt = %v, want %v", c.CreatedAt, fixed)
	}
}
