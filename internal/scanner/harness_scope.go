// [INPUT]: 依赖标准库 os / path/filepath / strings
// [OUTPUT]: 对外提供 ScanScopeOpts；包内提供 activeHarnesses / harnessForProjectDir / dirExists
// [POS]: internal/scanner 的 harness 感知层 —— 按工作目录的 harness 特征（.claude→Claude / .agent→Codex）
//
//	决定扫哪些 user 根目录的全局 skill/MCP；被 skills_dir.go / mcp_config.go 消费
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package scanner

import (
	"path/filepath"
	"strings"
)

// ScanScopeOpts carries the home directory used for user-scope (global) skill
// and MCP discovery. HomeDir is injectable so tests can point at a temp dir;
// when empty, user-scope scanning is skipped (project scope still runs).
type ScanScopeOpts struct {
	HomeDir string
}

// harnessConvention describes one agent harness's on-disk footprint: the marker
// directory that, when present in the project root, says "this is a <harness>
// workspace" — so its user-level (global) skills/MCP become relevant — plus the
// home-relative locations of those global skills and MCP config files.
type harnessConvention struct {
	name          string   // "claude" | "codex" | "cursor"
	markerDirs    []string // project-root markers; presence activates user-scope scan
	userSkillDirs []string // home-relative global skill dirs
	userMCPFiles  []string // home-relative global MCP config files
}

// harnessConventions drives harness-aware user-scope scanning. Claude is fully
// specified. Codex/Cursor user-level paths are intentionally nil: their exact
// global skill/MCP conventions are not yet documented in harness-design. The
// marker detection is in place so Phase 2 (codex.go) only needs to fill the
// paths once confirmed — no structural change required.
var harnessConventions = []harnessConvention{
	{
		name:          "claude",
		markerDirs:    []string{".claude"},
		userSkillDirs: []string{".claude/skills"}, // ~/.claude/skills
		userMCPFiles:  []string{".claude.json"},   // ~/.claude.json
	},
	{
		name:       "codex",
		markerDirs: []string{".agent"}, // Codex workspace marker
		// TODO(phase2): confirm Codex global skill dir + MCP config location
		// (~/.codex/...) against harness-design before enabling user-scope.
		userSkillDirs: nil,
		userMCPFiles:  nil,
	},
	{
		name:          "cursor",
		markerDirs:    []string{".cursor"},
		userSkillDirs: nil, // TODO: confirm Cursor global skill convention
		userMCPFiles:  nil,
	},
}

// activeHarnesses returns the harness conventions whose marker directory exists
// in the project root — i.e. which harnesses' user-level skills/MCP are relevant
// to this workspace. A project with .claude/ pulls in ~/.claude global skills;
// a project with .agent/ (Codex) pulls in its global equivalents. This keeps an
// unrelated harness's globals out of the picker (a pure Codex project won't list
// every ~/.claude skill).
func activeHarnesses(root string) []harnessConvention {
	var out []harnessConvention
	for _, h := range harnessConventions {
		for _, m := range h.markerDirs {
			if dirExists(filepath.Join(root, m)) {
				out = append(out, h)
				break
			}
		}
	}
	return out
}

// harnessForProjectDir maps a project-relative skill/MCP candidate path to the
// harness it belongs to ("" = generic / not harness-specific).
func harnessForProjectDir(rel string) string {
	switch {
	case hasPathPrefix(rel, ".claude"):
		return "claude"
	case hasPathPrefix(rel, ".agent"):
		return "codex"
	case hasPathPrefix(rel, ".cursor"):
		return "cursor"
	default:
		return ""
	}
}

// hasPathPrefix reports whether p equals prefix or sits under prefix/.
func hasPathPrefix(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}
