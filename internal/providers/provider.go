// Package providers ports the relevant subset of railwayapp/nixpacks's provider
// abstraction so askdao-cli can do framework inference + dep→apt reverse mapping
// without re-implementing nixpacks's full BuildPlan model. See
// docs/investigations/nixpacks-provider-pattern.md §1-§2.
//
// [INPUT]: 项目根目录 + 可选环境变量覆盖
// [OUTPUT]: Provider.Plan 产出 FrameworkPlan，喂 detection.json 的
//
//	detected_frameworks / inferred_apt_packages / detected_external_services
//
// [POS]: L3 启发式层；L1-L2 scanner 输出 + L4 LLM 之间的桥梁
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package providers

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/askdao/askdao-cli/internal/types"
)

// Provider mirrors nixpacks's `Provider` trait (mod.rs:30) but trims the
// install/setup/start phase model — askdao-cli's downstream is a declarative
// `environment.packages` payload, not a Dockerfile recipe.
type Provider interface {
	// Name is a stable provider id ("python", "node", ...).
	Name() string

	// Detect reports whether this provider thinks the project is one of its
	// supported language/runtime kinds. Multiple providers may match; the
	// caller decides priority.
	Detect(app *App, env *Env) (bool, error)

	// Plan returns the inferred frameworks / system packages / runtime hint.
	// Should only be called after Detect returns true; nil result is allowed
	// when there's nothing actionable to report.
	Plan(app *App, env *Env) (*FrameworkPlan, error)

	// Metadata exposes per-provider tags (e.g. selected package manager) that
	// L4 LLM can include in `provenance.detection_summary`.
	Metadata(app *App, env *Env) (Metadata, error)
}

// FrameworkPlan is askdao-cli's slimmed counterpart to nixpacks BuildPlan. The
// EntryPoint is informational only — it is NEVER pushed to environment specs
// (Anthropic Managed Agents owns the runtime entrypoint).
type FrameworkPlan struct {
	Frameworks  []types.DetectedFramework
	SystemPkgs  map[string][]types.InferredAptPackage // keyed by manager: "apt"
	Runtime     RuntimeHint
	EntryPoint  string
	Confidence  float64
	Evidence    []Evidence
	ExternalSvc []types.DetectedExternalService
}

// RuntimeHint is the provider's best read on the language runtime; the
// canonical version pin still comes from internal/scanner.DetectRuntimes.
type RuntimeHint struct {
	Kind    string
	Version string
}

// Evidence is one human-readable proof tying a heuristic decision to a file or
// dependency. Surfaces in detection.json detected_frameworks[].evidence.
type Evidence struct {
	Marker string // "uses_dep:fastapi" | "import_pattern:from fastapi import"
	Weight float64
}

// Metadata is a free-form key/value bag for provider-internal tags.
type Metadata map[string]string

// App is the read-only view of a project root that providers query against. A
// single App instance caches a directory walk so repeated IncludesFile /
// FindFiles calls are cheap (nixpacks-pattern §2: ~50ms first scan on 5K-file
// projects).
type App struct {
	root      string
	once      sync.Once
	files     []string          // forward slashes, relative to root
	fileSet   map[string]bool   // for O(1) IncludesFile
	readCache map[string]string // memoize ReadFile contents (small files only)
	readMu    sync.Mutex
}

// NewApp constructs an App rooted at the given path. The first method call
// triggers a single filesystem walk; subsequent calls reuse the index.
func NewApp(root string) (*App, error) {
	if root == "" {
		return nil, errors.New("providers: root must be non-empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &App{root: abs, readCache: map[string]string{}}, nil
}

// Root returns the absolute root path.
func (a *App) Root() string { return a.root }

func (a *App) ensureIndexed() {
	a.once.Do(func() {
		a.fileSet = map[string]bool{}
		_ = filepath.WalkDir(a.root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, relErr := filepath.Rel(a.root, path)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if shouldSkipProviderDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			a.files = append(a.files, rel)
			a.fileSet[rel] = true
			return nil
		})
	})
}

// shouldSkipProviderDir mirrors the auto-skip set used by enry — providers
// don't care about node_modules, .venv, or build outputs either.
func shouldSkipProviderDir(name string) bool {
	switch name {
	case ".git", "node_modules", ".venv", "venv", "__pycache__",
		"dist", "build", "target", ".next", ".nuxt":
		return true
	}
	return false
}

// IncludesFile reports whether the file (relative to root, forward slashes)
// exists in the project tree.
func (a *App) IncludesFile(rel string) bool {
	a.ensureIndexed()
	return a.fileSet[filepath.ToSlash(rel)]
}

// ReadFile reads a file relative to root. Return values are NOT cached for
// large files; callers should keep the slice short-lived.
func (a *App) ReadFile(rel string) ([]byte, error) {
	abs := filepath.Join(a.root, filepath.FromSlash(rel))
	return os.ReadFile(abs)
}

// FindMatch returns true if any file matching glob also matches re. Walks the
// indexed file list; on first hit returns immediately.
func (a *App) FindMatch(re *regexp.Regexp, glob string) bool {
	for _, f := range a.FindFiles(glob) {
		data, err := a.ReadFile(f)
		if err != nil {
			continue
		}
		if re.Match(data) {
			return true
		}
	}
	return false
}

// FindFiles returns relative paths matching glob. Glob syntax follows the
// `path.Match` rules with one extension: a leading `/**/` is treated as
// "anywhere under root".
func (a *App) FindFiles(glob string) []string {
	a.ensureIndexed()
	pattern := strings.TrimPrefix(glob, "/")
	var out []string
	for _, f := range a.files {
		if matchProviderGlob(pattern, f) {
			out = append(out, f)
		}
	}
	return out
}

// matchProviderGlob handles the nixpacks-style `**/*.py` notation by stripping
// the recursive marker and matching against the basename suffix.
func matchProviderGlob(pattern, path string) bool {
	if strings.HasPrefix(pattern, "**/") {
		suffix := strings.TrimPrefix(pattern, "**/")
		ok, _ := filepath.Match(suffix, filepath.Base(path))
		return ok
	}
	if strings.Contains(pattern, "**") {
		// Naive: replace ** with *, match basename.
		pattern = strings.ReplaceAll(pattern, "**", "*")
		ok, _ := filepath.Match(pattern, filepath.Base(path))
		return ok
	}
	ok, _ := filepath.Match(pattern, path)
	return ok
}

// Env carries user-facing overrides (e.g. PYTHON_PACKAGE_MANAGER) that take
// precedence over heuristic auto-selection.
type Env struct {
	config map[string]string
}

// NewEnv builds an Env from the OS environment plus any explicit overrides.
func NewEnv(overrides map[string]string) *Env {
	cfg := map[string]string{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			cfg[kv[:i]] = kv[i+1:]
		}
	}
	for k, v := range overrides {
		cfg[k] = v
	}
	return &Env{config: cfg}
}

// GetConfigVariable mirrors nixpacks's API; second return reports presence.
func (e *Env) GetConfigVariable(key string) (string, bool) {
	v, ok := e.config[key]
	return v, ok
}

// HasDep reports whether `name` (case-insensitively, PEP 503 normalized for
// pip / lowercase for others) is among the prod packages of the given
// ecosystem. Used by every provider's `uses_dep` checks.
func HasDep(pkgs map[string][]types.Package, ecosystem, name string) bool {
	for _, p := range pkgs[ecosystem] {
		if !p.IsProd {
			continue
		}
		if normalizeDepName(ecosystem, p.Name) == normalizeDepName(ecosystem, name) {
			return true
		}
	}
	return false
}

// normalizeDepName mirrors the helper in internal/scanner; duplicated here to
// avoid a cross-package import cycle for a 4-line function.
func normalizeDepName(ecosystem, name string) string {
	low := strings.ToLower(name)
	if ecosystem == "pip" {
		low = strings.ReplaceAll(low, "_", "-")
		low = strings.ReplaceAll(low, ".", "-")
	}
	return low
}
