// [INPUT]: 依赖 selfupdate.{Updater, verifyChecksum, extractBinary}; httptest 假 GitHub
// [OUTPUT]: cases — AssetName 平台映射 / checksum 校验±/ zip+tar.gz 提取 /
//
//	Run e2e（httptest：latest→下载→校验→换装）/ up-to-date / --force 同版本重装
//
// [POS]: internal/selfupdate 的单测，全程不出网、不碰真实可执行文件
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func targzWith(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{{"LICENSE", []byte("mit")}, {name, content}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipWith(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		name string
		body []byte
	}{{"LICENSE", []byte("mit")}, {name, content}} {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	zw.Close()
	return buf.Bytes()
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestAssetName(t *testing.T) {
	cases := []struct{ goos, goarch, version, want string }{
		{"windows", "amd64", "0.2.0", "askdao_0.2.0_windows_amd64.zip"},
		{"darwin", "arm64", "0.2.0", "askdao_0.2.0_darwin_arm64.tar.gz"},
		{"linux", "amd64", "1.0.0", "askdao_1.0.0_linux_amd64.tar.gz"},
	}
	for _, c := range cases {
		u := &Updater{GOOS: c.goos, GOARCH: c.goarch}
		if got := u.AssetName(c.version); got != c.want {
			t.Errorf("AssetName(%s/%s, %s) = %q, want %q", c.goos, c.goarch, c.version, got, c.want)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("binary-bytes")
	sums := []byte(fmt.Sprintf("%s  askdao_0.2.0_linux_amd64.tar.gz\n%s  other.zip\n", sha(data), sha([]byte("x"))))
	if err := verifyChecksum(data, sums, "askdao_0.2.0_linux_amd64.tar.gz"); err != nil {
		t.Fatalf("valid checksum rejected: %v", err)
	}
	if err := verifyChecksum([]byte("tampered"), sums, "askdao_0.2.0_linux_amd64.tar.gz"); err == nil {
		t.Fatal("tampered data accepted")
	}
	if err := verifyChecksum(data, sums, "missing.tar.gz"); err == nil {
		t.Fatal("missing asset entry accepted")
	}
}

func TestExtractBinary(t *testing.T) {
	want := []byte("new-binary")
	got, err := extractBinary(targzWith(t, "askdao", want), "a.tar.gz", "askdao")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("tar.gz extract = %q, %v", got, err)
	}
	got, err = extractBinary(zipWith(t, "askdao.exe", want), "a.zip", "askdao.exe")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("zip extract = %q, %v", got, err)
	}
	if _, err := extractBinary(targzWith(t, "notme", want), "a.tar.gz", "askdao"); err == nil {
		t.Fatal("missing binary in archive accepted")
	}
}

// fakeGitHub serves releases/latest + download assets for one version.
func fakeGitHub(t *testing.T, version string, asset string, archive []byte) *httptest.Server {
	t.Helper()
	sums := fmt.Sprintf("%s  %s\n", sha(archive), asset)
	mux := http.NewServeMux()
	mux.HandleFunc("/askdao/askdao-cli/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/askdao/askdao-cli/releases/tag/v"+version, http.StatusFound)
	})
	mux.HandleFunc("/askdao/askdao-cli/releases/download/v"+version+"/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/askdao/askdao-cli/releases/download/v"+version+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sums)
	})
	return httptest.NewServer(mux)
}

func newTestUpdater(t *testing.T, srv *httptest.Server, exe string) *Updater {
	t.Helper()
	return &Updater{
		DownloadBase: srv.URL,
		Out:          &bytes.Buffer{},
		ExePath:      exe,
		GOOS:         "linux",
		GOARCH:       "amd64",
	}
}

func TestRunUpdatesBinary(t *testing.T) {
	newBin := []byte("v0.2.0-binary")
	srv := fakeGitHub(t, "0.2.0", "askdao_0.2.0_linux_amd64.tar.gz", targzWith(t, "askdao", newBin))
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "askdao")
	if err := os.WriteFile(exe, []byte("v0.1.0-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := newTestUpdater(t, srv, exe)
	if err := u.Run(context.Background(), "0.1.0", false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := os.ReadFile(exe)
	if !bytes.Equal(got, newBin) {
		t.Fatalf("binary not replaced: %q", got)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm() != 0o755 {
		t.Fatalf("binary not executable: %v", fi.Mode())
	}
}

func TestRunUpToDate(t *testing.T) {
	srv := fakeGitHub(t, "0.1.0", "askdao_0.1.0_linux_amd64.tar.gz", targzWith(t, "askdao", []byte("same")))
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "askdao")
	if err := os.WriteFile(exe, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := newTestUpdater(t, srv, exe)
	err := u.Run(context.Background(), "0.1.0", false)
	if !errors.Is(err, ErrUpToDate) {
		t.Fatalf("want ErrUpToDate, got %v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "current" {
		t.Fatal("binary touched despite up-to-date")
	}

	// --force reinstalls the same version
	if err := u.Run(context.Background(), "0.1.0", true); err != nil {
		t.Fatalf("forced Run: %v", err)
	}
	got, _ = os.ReadFile(exe)
	if string(got) != "same" {
		t.Fatalf("forced reinstall did not replace binary: %q", got)
	}
}

func TestRunRejectsBadChecksum(t *testing.T) {
	asset := "askdao_0.2.0_linux_amd64.tar.gz"
	archive := targzWith(t, "askdao", []byte("new"))
	mux := http.NewServeMux()
	mux.HandleFunc("/askdao/askdao-cli/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/askdao/askdao-cli/releases/tag/v0.2.0", http.StatusFound)
	})
	mux.HandleFunc("/askdao/askdao-cli/releases/download/v0.2.0/"+asset, func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	mux.HandleFunc("/askdao/askdao-cli/releases/download/v0.2.0/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", sha([]byte("other-bytes")), asset)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	exe := filepath.Join(t.TempDir(), "askdao")
	if err := os.WriteFile(exe, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := newTestUpdater(t, srv, exe)
	if err := u.Run(context.Background(), "0.1.0", false); err == nil {
		t.Fatal("bad checksum accepted")
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "current" {
		t.Fatal("binary replaced despite bad checksum")
	}
}
