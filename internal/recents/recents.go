// [INPUT]: 依赖标准库 encoding/json + fmt + os + path/filepath + runtime + time；internal/auth 的 ConfigDir
// [OUTPUT]: 对外提供 File / Project / DeployRecord + SchemaVersion + Load / Save / Path + (File).Touch / Find / SetDeploy / Remove
// [POS]: internal/recents — 桌面 studio「最近项目」本地便利态持久化（recent-projects.json，与 auth 同配置目录）。
//
//	镜像 auth 的原子写，但读失败 / 损坏 / 版本漂移一律优雅降级为空列表（便利态非身份，绝不 brick 工作台）。
//	被 cmd/askdao-studio/app.go 消费（多项目工作台）；CLI agent edit 不触碰。
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package recents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/askdao/askdao-cli/internal/auth"
)

// SchemaVersion is bumped whenever the on-disk JSON layout changes. A file with
// a different version is treated as absent (graceful downgrade — this is a
// convenience cache, never authoritative state).
const SchemaVersion = 1

// fileName is the on-disk filename within the askdao config dir.
const fileName = "recent-projects.json"

// maxEntries caps the MRU list; entries beyond this are evicted on Touch.
const maxEntries = 12

// DeployRecord is what the desktop remembers about a project's last deploy, so a
// reopened project can show "deployed as X (vN)" without a server round-trip.
//
// MetadataName is the metadata.name at deploy time — deploy identity is
// (owner, name), so it is stored alongside AgentID: if the draft's name later
// changes, the list still reflects the agent this dir was actually deployed as
// (the (owner,name) drift becomes visible rather than silently wrong).
type DeployRecord struct {
	AgentID                string    `json:"agent_id"`
	MetadataName           string    `json:"metadata_name"`
	Created                bool      `json:"created"`
	PreviousManagedVersion *int      `json:"previous_managed_version,omitempty"`
	GroupLink              string    `json:"group_link,omitempty"`
	AnthropicAgentID       string    `json:"anthropic_agent_id,omitempty"`
	LastDeployedAt         time.Time `json:"last_deployed_at"`
}

// Project is one entry in the recent-projects list. Dir is the normalized
// absolute path (the dedup key); Label caches a display name; Deploy is the
// last-deploy record (nil until first deploy).
type Project struct {
	Dir          string        `json:"dir"`
	Label        string        `json:"label,omitempty"`
	LastOpenedAt time.Time     `json:"last_opened_at"`
	Deploy       *DeployRecord `json:"deploy,omitempty"`
}

// File is the on-disk document: a versioned, MRU-ordered project list
// (index 0 = most recently opened).
type File struct {
	Version  int       `json:"version"`
	Projects []Project `json:"projects"`
}

// Path returns the absolute on-disk path of the recents file without creating
// it. Shares auth's config dir so all askdao local state lives under one root.
func Path() (string, error) {
	dir, err := auth.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load reads the recents file. Unlike auth.Load, ANY failure (absent,
// unreadable, corrupt JSON, unknown schema version) returns an empty File with
// no error — this is a convenience cache and must never block the workbench.
func Load() (*File, error) {
	empty := &File{Version: SchemaVersion}
	p, err := Path()
	if err != nil {
		return empty, nil
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return empty, nil
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return empty, nil
	}
	if f.Version != SchemaVersion {
		return empty, nil
	}
	return &f, nil
}

// Save writes the recents file atomically (write-to-temp + rename) at 0600,
// creating the parent dir at 0700. Mirrors auth.Save's atomicity so a killed
// write never leaves a half-written file.
func Save(f *File) error {
	if f.Version == 0 {
		f.Version = SchemaVersion
	}
	dir, err := auth.ConfigDir()
	if err != nil {
		return fmt.Errorf("recents: resolve config dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("recents: mkdir %s: %w", dir, err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dir, 0o700)
	}
	final := filepath.Join(dir, fileName)
	tmp, err := os.CreateTemp(dir, fileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("recents: temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once rename succeeds

	encoded, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		tmp.Close()
		return fmt.Errorf("recents: marshal: %w", err)
	}
	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		return fmt.Errorf("recents: write %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("recents: close %s: %w", tmpPath, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0o600); err != nil {
			return fmt.Errorf("recents: chmod %s: %w", tmpPath, err)
		}
	}
	if err := os.Rename(tmpPath, final); err != nil {
		return fmt.Errorf("recents: rename to %s: %w", final, err)
	}
	return nil
}

// Find returns the entry for dir (matched on normalized path), or nil. The
// returned pointer is invalidated by any later Touch/SetDeploy/Remove that
// reslices Projects — read it, don't hold it across mutations.
func (f *File) Find(dir string) *Project {
	if idx := f.indexOf(normalize(dir)); idx >= 0 {
		return &f.Projects[idx]
	}
	return nil
}

// Touch upserts dir to the front of the MRU list, stamping LastOpenedAt=now and
// (if non-empty) Label, preserving any existing Deploy record. Entries beyond
// maxEntries are evicted. Returns the (now front) entry.
func (f *File) Touch(dir, label string) *Project {
	nd := normalize(dir)

	var p Project
	if idx := f.indexOf(nd); idx >= 0 {
		p = f.Projects[idx]
		f.Projects = append(f.Projects[:idx], f.Projects[idx+1:]...)
	}
	p.Dir = nd
	p.LastOpenedAt = time.Now().UTC()
	if label != "" {
		p.Label = label
	}
	f.Projects = append([]Project{p}, f.Projects...)
	if len(f.Projects) > maxEntries {
		f.Projects = f.Projects[:maxEntries]
	}
	return &f.Projects[0]
}

// SetDeploy attaches rec to dir's entry (creating a front entry if dir is not
// yet tracked, e.g. a deploy right after a scan that predates recents).
func (f *File) SetDeploy(dir string, rec DeployRecord) {
	r := rec
	nd := normalize(dir)
	if idx := f.indexOf(nd); idx >= 0 {
		f.Projects[idx].Deploy = &r
		return
	}
	f.Projects = append([]Project{{Dir: nd, LastOpenedAt: time.Now().UTC(), Deploy: &r}}, f.Projects...)
	if len(f.Projects) > maxEntries {
		f.Projects = f.Projects[:maxEntries]
	}
}

// Remove drops dir's entry if present. Idempotent.
func (f *File) Remove(dir string) {
	if idx := f.indexOf(normalize(dir)); idx >= 0 {
		f.Projects = append(f.Projects[:idx], f.Projects[idx+1:]...)
	}
}

func (f *File) indexOf(normalizedDir string) int {
	for i := range f.Projects {
		if normalize(f.Projects[i].Dir) == normalizedDir {
			return i
		}
	}
	return -1
}

// normalize produces a stable dedup key: filepath.Clean (drops trailing
// separators, resolves . / ..) plus best-effort symlink resolution so /foo,
// /foo/ and a symlinked path collapse to one entry. Falls back to the cleaned
// path when the dir does not exist on disk.
func normalize(dir string) string {
	c := filepath.Clean(dir)
	if resolved, err := filepath.EvalSymlinks(c); err == nil {
		return resolved
	}
	return c
}

// Normalize exposes the dedup-key normalization so callers (the desktop App's
// in-memory project list) key projects the same way the on-disk list does.
func Normalize(dir string) string { return normalize(dir) }
