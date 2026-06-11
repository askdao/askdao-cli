// Package auth implements askdao-cli's authentication primitives:
//   - on-disk credentials (this file)
//   - OAuth 2.0 Device Code Flow client (device.go)
//
// [INPUT]: 依赖标准库 encoding/json + os + path/filepath + runtime
// [OUTPUT]: 对外提供
//   - Credentials{} struct + Load() / Save() / Delete() + Path() helper
//   - DefaultServerURL 常量
//
// [POS]: internal/auth — askdao-cli 的本地身份持久化层；deploy / 其他需要 token
//
//	的命令通过 Load 读取。设计稿 docs/cli-auth-device-flow.md §6.1。
//	XDG-aware：$XDG_CONFIG_HOME/askdao/credentials.json 优先；fallback
//	~/.config/askdao/credentials.json；Windows 走 %AppData%\askdao\。
//	文件权限 0600，父目录 0700 —— 防止同机其他用户读到 token。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// DefaultServerURL is compiled into the binary as the production conductor
// host. CLI flags / env override take precedence; see the token-resolution
// table in docs/cli-auth-device-flow.md §6.3.
const DefaultServerURL = "https://api.askdao.ai"

// credentialsFileName is the on-disk filename within the askdao config dir.
const credentialsFileName = "credentials.json"

// credentialsSchemaVersion is bumped whenever the on-disk JSON layout
// changes. Loaders refuse newer schemas to avoid silently misreading fields.
const credentialsSchemaVersion = 1

// Credentials is the on-disk view of a successful `askdao auth login`.
//
// AccessToken is the plaintext bearer the CLI sends to conductor as
// `Authorization: Bearer cli_...`. It is hashed (SHA-256) on the server
// side; if this file is lost the user must re-run `askdao auth login`,
// there is no recovery path.
type Credentials struct {
	Version     int       `json:"version"`
	Server      string    `json:"server"`
	UserID      string    `json:"user_id"`
	UserEmail   string    `json:"user_email"`
	AccessToken string    `json:"access_token"`
	CreatedAt   time.Time `json:"created_at"`
}

// ErrNoCredentials is returned by Load when the credentials file is absent.
// Callers distinguish "not logged in" from "corrupted credentials" by
// errors.Is-ing against this sentinel.
var ErrNoCredentials = errors.New("auth: no credentials on disk (run `askdao auth login`)")

// Path returns the absolute on-disk path the credentials would live at,
// without creating the file or its parent. Useful for `askdao auth status`
// output and tests.
func Path() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, credentialsFileName), nil
}

// Load reads credentials from disk. Returns ErrNoCredentials if the file
// does not exist; any other read / parse failure surfaces as a wrapped
// error. A successful Load gives credentials suitable for use as a bearer
// token in subsequent CLI commands.
func Load() (*Credentials, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoCredentials
		}
		return nil, fmt.Errorf("auth: read %s: %w", p, err)
	}
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("auth: parse %s: %w (delete and re-login if corrupted)", p, err)
	}
	if c.Version != credentialsSchemaVersion {
		return nil, fmt.Errorf(
			"auth: %s has schema version %d (expected %d); re-run `askdao auth login`",
			p, c.Version, credentialsSchemaVersion,
		)
	}
	if c.AccessToken == "" || c.Server == "" {
		return nil, fmt.Errorf("auth: %s is missing required fields; re-run `askdao auth login`", p)
	}
	return &c, nil
}

// Save writes credentials to disk atomically (write-to-temp + rename) with
// 0600 permission. The parent directory is created at 0700 if needed.
// Atomic write guards against half-written files when the CLI is killed
// mid-flight.
func Save(c *Credentials) error {
	if c.Version == 0 {
		c.Version = credentialsSchemaVersion
	}
	if c.Version != credentialsSchemaVersion {
		return fmt.Errorf("auth: cannot save schema version %d (expected %d)", c.Version, credentialsSchemaVersion)
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	if c.AccessToken == "" || c.Server == "" {
		return errors.New("auth: cannot save credentials with empty access_token or server")
	}

	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("auth: mkdir %s: %w", dir, err)
	}
	// On Unix, MkdirAll honors umask — narrow the perms explicitly so we are
	// not at the mercy of a permissive default.
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dir, 0o700)
	}

	final := filepath.Join(dir, credentialsFileName)
	tmp, err := os.CreateTemp(dir, credentialsFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("auth: temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	// Ensure cleanup if anything below fails before the rename.
	defer func() {
		_ = os.Remove(tmpPath) // no-op once rename succeeded
	}()

	encoded, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		tmp.Close()
		return fmt.Errorf("auth: marshal credentials: %w", err)
	}
	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		return fmt.Errorf("auth: write %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("auth: close %s: %w", tmpPath, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0o600); err != nil {
			return fmt.Errorf("auth: chmod %s: %w", tmpPath, err)
		}
	}
	if err := os.Rename(tmpPath, final); err != nil {
		return fmt.Errorf("auth: rename to %s: %w", final, err)
	}
	return nil
}

// Delete removes the credentials file. Returns ErrNoCredentials if it was
// already gone (so callers can choose to silently no-op or report).
func Delete() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoCredentials
		}
		return fmt.Errorf("auth: delete %s: %w", p, err)
	}
	return nil
}

// configDir picks the platform-appropriate config directory and appends
// our app folder. Honors $XDG_CONFIG_HOME on Unix; uses os.UserConfigDir
// (which already encodes the right Windows / macOS defaults) as fallback.
func configDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "askdao"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("auth: cannot resolve config dir: %w", err)
	}
	return filepath.Join(base, "askdao"), nil
}
