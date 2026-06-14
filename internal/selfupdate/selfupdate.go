// Package selfupdate implements `askdao update` — in-place binary upgrade
// from GitHub Releases.
//
// [INPUT]: 依赖 GitHub releases/latest 302 重定向解析版本（不碰 api.github.com，避 60/hr 匿名限流）
//
//	与 GoReleaser 资产命名 askdao_{ver}_{os}_{arch}.{tar.gz|zip} + checksums.txt；stdlib only
//	(net/http + archive/{zip,tar} + compress/gzip + crypto/sha256)
//
// [OUTPUT]: 对外提供 Updater（New / Run）—— 查 latest、下载校验、原子换装当前可执行文件
// [POS]: internal/ 的自升级引擎，被 cmd/askdao/update.go 消费；
//
//	与 install/ 脚本共享同一资产命名契约（脚本管首装，本包管后续升级）
//
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
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrUpToDate is returned by Run when the running version already matches the
// latest release and force is false.
var ErrUpToDate = errors.New("already up to date")

// Updater drives one self-update cycle. Zero-value fields fall back to the
// real GitHub endpoints; tests point DownloadBase at an httptest server
// and ExePath at a scratch file.
type Updater struct {
	Repo         string       // owner/name, default "askdao/askdao-cli"
	DownloadBase string       // default "https://github.com"
	Client       *http.Client // default http.DefaultClient
	Out          io.Writer    // progress output, default os.Stdout
	ExePath      string       // binary to replace, default os.Executable()
	GOOS         string       // default runtime.GOOS
	GOARCH       string       // default runtime.GOARCH
}

func New() *Updater { return &Updater{} }

func (u *Updater) repo() string { return defaultStr(u.Repo, "askdao/askdao-cli") }
func (u *Updater) dl() string   { return defaultStr(u.DownloadBase, "https://github.com") }
func (u *Updater) goos() string { return defaultStr(u.GOOS, runtime.GOOS) }
func (u *Updater) arch() string { return defaultStr(u.GOARCH, runtime.GOARCH) }

func (u *Updater) client() *http.Client {
	if u.Client != nil {
		return u.Client
	}
	return http.DefaultClient
}

func (u *Updater) out() io.Writer {
	if u.Out != nil {
		return u.Out
	}
	return os.Stdout
}

func defaultStr(v, d string) string {
	if v != "" {
		return v
	}
	return d
}

// Latest returns the newest release version (without the leading "v").
//
// It reads the tag from the github.com/<repo>/releases/latest 302 redirect
// (Location: .../releases/tag/vX.Y.Z) instead of querying api.github.com — the
// anonymous API is rate-limited to 60/hr/IP (brutal behind shared CGNAT), the
// github.com redirect is not.
func (u *Updater) Latest(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/%s/releases/latest", u.dl(), u.repo())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	// Capture the 302 instead of following it: copy the client so the
	// no-follow policy doesn't leak into download requests.
	c := *u.client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Do(req)
	if err != nil {
		return "", fmt.Errorf("query latest release: %w", err)
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("query latest release: %s returned %s (no redirect)", url, resp.Status)
	}
	v := strings.TrimPrefix(path.Base(loc), "v")
	if v == "" || v == "." || v == "/" {
		return "", errors.New("could not resolve latest version from redirect")
	}
	return v, nil
}

// AssetName returns the GoReleaser archive name for a version on this
// Updater's platform — must stay in lockstep with .goreleaser.yml.
func (u *Updater) AssetName(version string) string {
	ext := "tar.gz"
	if u.goos() == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("askdao_%s_%s_%s.%s", version, u.goos(), u.arch(), ext)
}

// Run performs the full cycle: resolve latest, compare against
// currentVersion, download + checksum-verify the platform archive, and swap
// the running binary in place. force re-installs even when versions match.
func (u *Updater) Run(ctx context.Context, currentVersion string, force bool) error {
	exe, err := u.exePath()
	if err != nil {
		return err
	}
	cleanupOld(exe) // stale .old from a previous Windows update

	latest, err := u.Latest(ctx)
	if err != nil {
		return err
	}
	if latest == strings.TrimPrefix(currentVersion, "v") && !force {
		return fmt.Errorf("%w (v%s)", ErrUpToDate, latest)
	}

	asset := u.AssetName(latest)
	base := fmt.Sprintf("%s/%s/releases/download/v%s", u.dl(), u.repo(), latest)
	fmt.Fprintf(u.out(), "Downloading askdao v%s (%s/%s) ...\n", latest, u.goos(), u.arch())

	archive, err := u.fetch(ctx, base+"/"+asset)
	if err != nil {
		return err
	}
	sums, err := u.fetch(ctx, base+"/checksums.txt")
	if err != nil {
		return err
	}
	if err := verifyChecksum(archive, sums, asset); err != nil {
		return err
	}

	binName := "askdao"
	if u.goos() == "windows" {
		binName = "askdao.exe"
	}
	binary, err := extractBinary(archive, asset, binName)
	if err != nil {
		return err
	}
	if err := apply(binary, exe, u.goos()); err != nil {
		return err
	}
	fmt.Fprintf(u.out(), "Updated %s: v%s -> v%s\n", exe, strings.TrimPrefix(currentVersion, "v"), latest)
	return nil
}

func (u *Updater) exePath() (string, error) {
	if u.ExePath != "" {
		return u.ExePath, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate current executable: %w", err)
	}
	return filepath.EvalSymlinks(exe)
}

func (u *Updater) fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// verifyChecksum checks data against the GoReleaser checksums.txt line
// "<sha256hex>  <asset>".
func verifyChecksum(data, checksums []byte, asset string) error {
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt has no entry for %s", asset)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", asset, want, got)
	}
	return nil
}

// extractBinary pulls binName out of a .zip or .tar.gz archive.
func extractBinary(archive []byte, asset, binName string) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", asset, err)
		}
		for _, f := range zr.File {
			if f.Name == binName {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("%s not found in %s", binName, asset)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", asset, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == binName && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in %s", binName, asset)
}

// apply swaps the binary at exePath with newBinary. The temp file lives in
// the same directory so the final rename stays on one filesystem. On Windows
// a running exe cannot be overwritten but CAN be renamed: move it aside to
// .old first; the leftover is deleted best-effort (or by the next update).
func apply(newBinary []byte, exePath, goos string) error {
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".askdao-update-*")
	if err != nil {
		return fmt.Errorf("write new binary: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err := tmp.Write(newBinary); err != nil {
		tmp.Close()
		return fmt.Errorf("write new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	if goos == "windows" {
		old := exePath + ".old"
		os.Remove(old)
		if err := os.Rename(exePath, old); err != nil {
			return fmt.Errorf("move old binary aside: %w", err)
		}
		if err := os.Rename(tmpName, exePath); err != nil {
			os.Rename(old, exePath) // best-effort rollback
			return fmt.Errorf("install new binary: %w", err)
		}
		os.Remove(old)
		return nil
	}
	if err := os.Rename(tmpName, exePath); err != nil {
		return fmt.Errorf("install new binary: %w", err)
	}
	return nil
}

func cleanupOld(exePath string) {
	os.Remove(exePath + ".old")
}
