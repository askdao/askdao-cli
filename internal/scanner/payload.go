// [INPUT]: 依赖 标准库 (fmt, os, path/filepath, sort, strings), internal/types 的 Detection / DeploymentPayload / PayloadEntry,
//
//	同包 glob.go 的 compileGlobs/matchAny、skills_dir.go 的 SkillDirCandidates、archetype.go 的判定结果（det.Archetype）
//
// [OUTPUT]: 对外提供 DetectDeploymentPayload —— 把项目目录分类成上传清单 / 排除清单，返回 payload + 软警告。
//
//	v0.7 起所有 custom skill（含 vendored 的 lockfile-pinned）一律打包上传（Anthropic Managed Agents 无公共 registry，
//	详见 harness-design/investigations/managed-agents-skill-installation.md）；vendored 仅作 metadata 标签由 bundle UI 渲染。
//
// [POS]: internal/scanner 的部署清单生成器；pipeline 在 skills + archetype 就绪后调；产物给 `askdao bundle` / `askdao detect` 渲染
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/askdao/askdao-cli/internal/types"
)

// PayloadOptions tunes DetectDeploymentPayload.
type PayloadOptions struct {
	// IncludeEvals keeps a skill's evals/ subdirectory in the upload payload.
	// Default false omits it (test fixtures don't need to travel to the cloud).
	IncludeEvals bool
}

// largePayloadBytes is the soft ceiling above which we warn — Anthropic skill
// upload has size limits and a multi-MB bundle is usually a sign something
// generated/vendored slipped in.
const largePayloadBytes = 50 << 20 // 50 MiB

// builtinIgnore is the always-on exclude list: editor/OS junk, re-installable
// dependency caches, build output, and — critically — secrets files.
var builtinIgnore = []ignoreRule{
	{".git", "version control metadata"},
	{".hg", "version control metadata"},
	{".svn", "version control metadata"},
	{".DS_Store", "macOS Finder metadata"},
	{"Thumbs.db", "Windows Explorer metadata"},
	{"*.swp", "editor swap file"},
	{"*.swo", "editor swap file"},
	{"*~", "editor backup file"},
	{".idea", "IDE settings"},
	{".vscode", "IDE settings"},
	{"node_modules", "re-installable from package manifest"},
	{"__pycache__", "Python bytecode cache"},
	{".pytest_cache", "test runner cache"},
	{"*.pyc", "Python bytecode"},
	{".mypy_cache", "type checker cache"},
	{".ruff_cache", "linter cache"},
	{"dist", "build output"},
	{"build", "build output"},
	{".next", "build output"},
	{".turbo", "build cache"},
	{"target", "build output (Rust/Java)"},
	{".venv", "Python virtualenv — re-creatable"},
	{"venv", "Python virtualenv — re-creatable"},
	{".cache", "tooling cache"},
	{".askdao", "askdao-cli local metadata"},
	{".env", "secrets file — never uploaded"},
	{".env.local", "secrets file — never uploaded"},
	{".env.*.local", "secrets file — never uploaded"},
	{"*.pem", "private key — never uploaded"},
	{"*.key", "private key — never uploaded"},
	{"id_rsa", "private key — never uploaded"},
}

type ignoreRule struct {
	pattern string
	reason  string
}

// agentDocNames are repo-root files that are part of the agent's identity /
// operating manual and should always travel with it.
var agentDocNames = map[string]string{
	"CLAUDE.md":  "agent operating manual",
	"AGENTS.md":  "agent operating manual",
	"AGENT.md":   "agent operating manual",
	"persona.md": "agent persona",
	"README.md":  "project README",
	"README.rst": "project README",
	"README":     "project README",
}

// manifestNames are dependency/build manifests worth shipping so the cloud can
// reconstruct the environment.
var manifestNames = map[string]bool{
	"skills-lock.json": true, "package.json": true, "package-lock.json": true,
	"pnpm-lock.yaml": true, "yarn.lock": true, "pyproject.toml": true,
	"requirements.txt": true, "uv.lock": true, "poetry.lock": true,
	"go.mod": true, "go.sum": true, "Cargo.toml": true, "Cargo.lock": true,
	"Makefile": true, "Dockerfile": true, ".dockerignore": true,
	"LICENSE": true, "LICENSE.txt": true, "LICENSE.md": true,
}

// generatedDirNames are directory base-names that hold regenerable output.
var generatedDirNames = map[string]bool{
	"output": true, "outputs": true, "out": true, "generated": true,
}

// userDataDirNames are directory base-names that look like per-run input data
// rather than agent payload — excluded only when the archetype is a pipeline.
var userDataDirNames = map[string]bool{
	"input": true, "inputs": true, "data": true, "samples": true,
	"tmp": true, "temp": true, "fixtures": true,
}

// DetectDeploymentPayload classifies the project under root into the upload
// manifest. det must already carry DetectedSkills, DetectedLanguages and
// Archetype (the pipeline fills those first). Returns the payload plus any soft
// warnings (never an error for a well-formed directory).
func DetectDeploymentPayload(root string, det *types.Detection, opts PayloadOptions) (types.DeploymentPayload, []string) {
	var warns []string
	pl := types.DeploymentPayload{}

	// --- 1. Ignore rules: builtin → .gitignore → .dockerignore → .askdaoignore.
	type ignoreMatcher struct {
		m      []globMatcher
		reason string
	}
	var ignore []ignoreMatcher
	addIgnore := func(pattern, reason string) {
		m, _ := compileGlobs([]string{pattern})
		ignore = append(ignore, ignoreMatcher{m: m, reason: reason})
	}
	for _, r := range builtinIgnore {
		addIgnore(r.pattern, r.reason)
	}
	ignoreSources := []string{"builtin"}
	var negPats []string
	for _, f := range []string{".gitignore", ".dockerignore", ".askdaoignore"} {
		pos, neg, ok := readIgnoreFile(filepath.Join(root, f))
		if !ok {
			continue
		}
		ignoreSources = append(ignoreSources, f)
		for _, p := range pos {
			addIgnore(p, "matched "+f)
		}
		negPats = append(negPats, neg...)
	}
	pl.IgnoreSources = ignoreSources
	negMatchers, _ := compileGlobs(negPats)
	matchedIgnore := func(name string) (string, bool) {
		if matchAny(negMatchers, name) {
			return "", false
		}
		for _, im := range ignore {
			if matchAny(im.m, name) {
				return im.reason, true
			}
		}
		return "", false
	}

	// --- 2. Skill units: each <skillDir>/<name>/ is one logical entry.
	// v0.7: every detected skill (repo-native AND vendored) ships inline as
	// files; Anthropic Managed Agents has no "reinstall from registry" path.
	// Vendored vs native is a UI-only distinction carried on DetectedSkill
	// metadata (LockedSource / LockedHash) and rendered as an origin tag in
	// the bundle output.
	skillUnitPaths := map[string]bool{}      // rel paths (slash) of skill dirs already classified
	skillContainerRoots := map[string]bool{} // top-level component of every skill-dir candidate
	for _, c := range SkillDirCandidates {
		skillContainerRoots[firstSegment(c)] = true
	}
	for _, s := range det.DetectedSkills {
		if s.SkillName == "" || s.Source == "" {
			continue // implied-builtin placeholder
		}
		dirRel := filepath.ToSlash(filepath.Dir(s.Source)) // e.g. .agents/skills/foo
		skillUnitPaths[dirRel] = true
		dirAbs := filepath.Join(root, filepath.FromSlash(dirRel))

		// Origin tag rendered inline by bundle UI as `skill (<tag>)`. Format
		// stays compact so the row stays scannable.
		var originTag string
		if s.IsLocalOriginal {
			originTag = "repo-native"
		} else {
			short := s.LockedHash
			if len(short) > 7 {
				short = short[:7]
			}
			if short == "" {
				originTag = fmt.Sprintf("vendored: %s", s.LockedSource)
			} else {
				originTag = fmt.Sprintf("vendored: %s @ %s", s.LockedSource, short)
			}
		}
		addSkillDirEntry(&pl.Includes, dirRel, dirAbs, "skill", originTag, opts.IncludeEvals)
	}

	// --- 3. Top-level entries.
	entries, _ := os.ReadDir(root)
	pipeline := det.Archetype.Kind == "skill_pipeline"
	for _, e := range entries {
		name := e.Name()
		abs := filepath.Join(root, name)

		// Skill containers (e.g. .agents, .claude, skills): their relevant
		// content is the skill units handled above. Surface any non-skill
		// leftovers under them only as an informational exclude — for v1 we
		// just skip the container wholesale.
		if e.IsDir() && skillContainerRoots[name] {
			// If this container holds skill units we already classified them.
			// Anything else under it (harness config etc.) is intentionally
			// not part of the agent payload.
			continue
		}

		// Ignore rules (builtin + ignore files), honoring negation.
		if reason, ok := matchedIgnore(name); ok {
			kind := "junk"
			if name == "node_modules" || strings.HasPrefix(name, ".venv") || name == "venv" {
				kind = "vendored"
			}
			pl.Excludes = append(pl.Excludes, dirOrFileEntry(name, abs, e.IsDir(), kind, reason))
			continue
		}

		lower := strings.ToLower(name)

		// Positive recognition.
		if r, ok := agentDocNames[name]; ok && !e.IsDir() {
			pl.Includes = append(pl.Includes, dirOrFileEntry(name, abs, false, "agent_doc", r))
			continue
		}
		if manifestNames[name] && !e.IsDir() {
			pl.Includes = append(pl.Includes, dirOrFileEntry(name, abs, false, "manifest", "build/dependency manifest"))
			continue
		}

		// Exclude-by-name heuristics for directories.
		if e.IsDir() {
			if generatedDirNames[lower] || strings.HasSuffix(lower, "-old") || strings.HasSuffix(lower, "-bak") || strings.HasSuffix(lower, ".bak") {
				pl.Excludes = append(pl.Excludes, dirOrFileEntry(name, abs, true, "generated", "generated artifact directory"))
				continue
			}
			if userDataDirNames[lower] {
				if pipeline {
					pl.Excludes = append(pl.Excludes, dirOrFileEntry(name, abs, true, "user_data", "input/data — re-supplied per run, not part of the agent"))
					continue
				}
			}
			if name == ".github" {
				pl.Excludes = append(pl.Excludes, dirOrFileEntry(name, abs, true, "other", "CI configuration — not part of the agent runtime"))
				continue
			}
		}

		// Default: ship it.
		kind := "source"
		reason := "project source"
		if e.IsDir() {
			reason = "project directory"
		} else if isLikelyConfigFile(name) {
			kind = "manifest"
			reason = "project config"
		}
		pl.Includes = append(pl.Includes, dirOrFileEntry(name, abs, e.IsDir(), kind, reason))
	}

	// --- 4. Negation can resurrect a concrete file inside an excluded dir.
	for _, np := range negPats {
		resolveNegation(root, np, &pl)
	}

	// --- 5. Totals (from Includes only).
	dedupEntries(&pl.Includes)
	dedupEntries(&pl.Excludes)
	sort.Slice(pl.Includes, func(i, j int) bool { return pl.Includes[i].Path < pl.Includes[j].Path })
	sort.Slice(pl.Excludes, func(i, j int) bool { return pl.Excludes[i].Path < pl.Excludes[j].Path })
	for _, e := range pl.Includes {
		pl.TotalBytes += e.Bytes
		pl.TotalFiles += e.Files
	}
	if pl.TotalBytes > largePayloadBytes {
		warns = append(warns, fmt.Sprintf("deployment payload is %s — large for a skill bundle; check for generated/vendored content that slipped in", humanBytes(pl.TotalBytes)))
	}
	return pl, warns
}

// --- helpers ---------------------------------------------------------------

func firstSegment(p string) string {
	p = filepath.ToSlash(p)
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}

func dirOrFileEntry(name, abs string, isDir bool, kind, reason string) types.PayloadEntry {
	if isDir {
		b, f := dirSize(abs)
		return types.PayloadEntry{Path: filepath.ToSlash(name) + "/", Bytes: b, Files: f, Kind: kind, Reason: reason}
	}
	var b int64
	if info, err := os.Stat(abs); err == nil {
		b = info.Size()
	}
	return types.PayloadEntry{Path: filepath.ToSlash(name), Bytes: b, Files: 1, Kind: kind, Reason: reason}
}

// addSkillDirEntry adds a skill directory to includes, optionally minus evals/.
func addSkillDirEntry(includes *[]types.PayloadEntry, dirRel, dirAbs, kind, reason string, includeEvals bool) {
	b, f := dirSize(dirAbs)
	if !includeEvals {
		if eb, ef := dirSize(filepath.Join(dirAbs, "evals")); ef > 0 {
			b -= eb
			f -= ef
			reason += " (evals/ excluded)"
		}
	}
	if b < 0 {
		b = 0
	}
	if f < 0 {
		f = 0
	}
	*includes = append(*includes, types.PayloadEntry{Path: dirRel + "/", Bytes: b, Files: f, Kind: kind, Reason: reason})
}

// readIgnoreFile parses a gitignore-style file. Returns positive patterns,
// negated (`!`) patterns, and whether the file existed. Comments and blanks
// are skipped; trailing slashes are stripped (we match on base names).
func readIgnoreFile(path string) (pos, neg []string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		neg0 := strings.HasPrefix(line, "!")
		line = strings.TrimPrefix(line, "!")
		line = strings.TrimPrefix(line, "/")
		line = strings.TrimSuffix(line, "/")
		if line == "" {
			continue
		}
		if neg0 {
			neg = append(neg, line)
		} else {
			pos = append(pos, line)
		}
	}
	return pos, neg, true
}

// resolveNegation: if a negation pattern names a concrete existing file under
// root, make sure it appears in Includes (even if its parent directory was
// excluded). Best-effort, file-level only.
func resolveNegation(root, pat string, pl *types.DeploymentPayload) {
	clean := filepath.FromSlash(strings.TrimPrefix(pat, "./"))
	abs := filepath.Join(root, clean)
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return
	}
	rel := filepath.ToSlash(clean)
	for _, e := range pl.Includes {
		if e.Path == rel {
			return
		}
	}
	pl.Includes = append(pl.Includes, types.PayloadEntry{
		Path: rel, Bytes: info.Size(), Files: 1, Kind: "source",
		Reason: "force-included via .askdaoignore",
	})
}

func dedupEntries(es *[]types.PayloadEntry) {
	seen := map[string]bool{}
	out := (*es)[:0]
	for _, e := range *es {
		if seen[e.Path] {
			continue
		}
		seen[e.Path] = true
		out = append(out, e)
	}
	*es = out
}

func isLikelyConfigFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".toml", ".yaml", ".yml", ".ini", ".cfg", ".conf":
		return true
	}
	switch name {
	case ".nvmrc", ".python-version", ".tool-versions", "rust-toolchain", ".mcp.json":
		return true
	}
	return false
}

// humanBytes renders a byte count like "84 KB" / "1.2 MB" for messages.
func humanBytes(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
