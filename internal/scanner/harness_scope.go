// [INPUT]: 依赖标准库 os / path/filepath / runtime / strings；包内 dirExists（harness_signals.go）+ fileExists（runtimes.go）
// [OUTPUT]: 对外提供 ScanScopeOpts；包内提供 activeHarnesses / harnessForProjectDir / hasPathPrefix / appDataDir
// [POS]: internal/scanner 的 harness 感知层 —— 覆盖 claude(CLI) / codex(CLI) / cowork(GUI app) 三 harness。
//
//	按工作目录 marker（CLI）或 app 安装痕迹（GUI）决定扫哪些 user 根目录的全局 skill/MCP；
//	被 skills_dir.go / mcp_config.go 消费
//
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package scanner

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ScanScopeOpts carries the home directory used for user-scope (global) skill
// and MCP discovery. HomeDir is injectable so tests can point at a temp dir;
// when empty, user-scope scanning is skipped (project scope still runs).
type ScanScopeOpts struct {
	HomeDir string
}

// pathBase says which root a user-scope path resolves against. CLI harnesses
// (Claude Code, Codex) keep their global config under the home dir on every OS;
// GUI apps (Cowork) keep it under the per-OS application-data dir, which differs
// by platform — that split is the whole reason this enum exists.
type pathBase int

const (
	baseHome    pathBase = iota // ~/            (CLI: ~/.claude, ~/.agents/skills)
	baseAppData                 // platform app-data (GUI: <app-data>/Claude/...)
)

// userPath is one home-injectable user-scope location: rel resolved under base.
type userPath struct {
	base pathBase
	rel  string
}

// resolve turns a userPath into an absolute path under the (injectable) home.
func (p userPath) resolve(home string) string {
	if p.base == baseAppData {
		return filepath.Join(appDataDir(home), p.rel)
	}
	return filepath.Join(home, p.rel)
}

// appDataDir returns the per-user application-data root for the running OS,
// rooted at the injectable home so tests can point at a temp dir.
//
//   - macOS:   ~/Library/Application Support
//   - Windows: %APPDATA% (falls back to ~/AppData/Roaming when the env is unset)
//   - Linux:   $XDG_CONFIG_HOME (falls back to ~/.config)
//
// On darwin there is no APPDATA env, so it is pure home-derivation — keeping
// tests deterministic when a fake HOME is injected. Known limitation: a Windows
// MSIX/Store install of Claude Desktop virtualizes config under
// AppData\Local\Packages\Claude_<id>\LocalCache\Roaming\Claude\, which the
// standard %APPDATA% path below does not see — only the .exe install is covered.
func appDataDir(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support")
	case "windows":
		if ad := os.Getenv("APPDATA"); ad != "" {
			return ad
		}
		return filepath.Join(home, "AppData", "Roaming")
	default: // linux & friends — XDG base dir spec
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			return x
		}
		return filepath.Join(home, ".config")
	}
}

// harnessConvention describes one agent harness's on-disk footprint. CLI
// harnesses declare markerDirs (project-root dirs that say "this is a <harness>
// workspace", activating their global skills/MCP). GUI-app harnesses leave
// markerDirs nil — they have no per-project footprint — and instead self-gate on
// whether the user actually has the app configured (see active).
//
// userMCPFiles holds only files our JSON parser (readMCPConfig) understands —
// the canonical {"mcpServers": {...}} shape. Codex's MCP config is TOML
// ([mcp_servers.<id>] tables) and is therefore NOT listed yet; wiring it up
// needs a TOML reader, not just a path (see codexMCPNote).
type harnessConvention struct {
	name          string     // "claude" | "codex" | "cowork"
	markerDirs    []string   // project-root markers (CLI); nil for GUI apps
	userSkillDirs []userPath // global skill dirs (SKILL.md form)
	userMCPFiles  []userPath // global MCP config files (JSON mcpServers shape only)
}

// harnessConventions drives harness-aware user-scope scanning.
//
//   - claude (CLI): marker .claude → ~/.claude/skills + ~/.claude.json.
//   - codex (CLI): markers .codex and .agents (the latter is the cross-harness
//     skills dir already in SkillDirCandidates, so a project carrying only
//     .agents/skills still counts as a Codex workspace). Global skills at
//     ~/.agents/skills (SKILL.md form, official Codex skills path). Global MCP
//     lives in ~/.codex/config.toml as TOML — intentionally NOT in userMCPFiles
//     because readMCPConfig only parses JSON; a TOML reader is required (codexMCPNote).
//   - cowork (GUI app = Claude Desktop's agentic mode): NO project marker. Global
//     MCP is claude_desktop_config.json under the per-OS app-data dir (canonical
//     JSON mcpServers). NB: DXT-packaged extensions (Claude Extensions/) are a
//     separate, mostly-stdio MCP source not scanned here — they rarely port to
//     Managed Agents (url-only), so the mcpServers map is the right target.
//     userSkillDirs stays nil: Cowork's user-authored skills
//     live in ephemeral per-session dirs that are deleted on cleanup (not
//     scannable), and any persistent skills it runs come from ~/.claude/skills,
//     already covered by the claude harness.
//
// Sources:
//
//	codex skills    https://developers.openai.com/codex/skills  (~/.agents/skills)
//	codex mcp/toml  https://developers.openai.com/codex/mcp     (~/.codex/config.toml, [mcp_servers.<id>])
//	cowork mcp      ~/Library/Application Support/Claude/claude_desktop_config.json (macOS); %APPDATA%\Claude\... (Windows)
//	cowork skills   github.com/anthropics/claude-code/issues/31422 (ephemeral session dirs, deleted on cleanup)
//
// Cursor is deliberately absent: it is not a target harness for askdao-cli.
var harnessConventions = []harnessConvention{
	{
		name:          "claude",
		markerDirs:    []string{".claude"},
		userSkillDirs: []userPath{{baseHome, ".claude/skills"}}, // ~/.claude/skills
		userMCPFiles:  []userPath{{baseHome, ".claude.json"}},   // ~/.claude.json
	},
	{
		name:          "codex",
		markerDirs:    []string{".codex", ".agents"},            // .codex config dir + cross-harness .agents
		userSkillDirs: []userPath{{baseHome, ".agents/skills"}}, // ~/.agents/skills (official codex skills path)
		// codexMCPNote: global MCP is ~/.codex/config.toml in TOML
		// ([mcp_servers.<id>]); readMCPConfig is JSON-only, so we cannot list it
		// here without a TOML parser. Tracked as a separate work item.
		userMCPFiles: nil,
	},
	{
		name:          "cowork",
		markerDirs:    nil, // GUI app: no project marker; self-gates on config existence
		userSkillDirs: nil, // ephemeral session skills (deleted on cleanup) / shared ~/.claude/skills
		userMCPFiles:  []userPath{{baseAppData, "Claude/claude_desktop_config.json"}},
	},
}

// activeHarnesses returns the harness conventions relevant to this workspace —
// i.e. whose user-level skills/MCP should feed the picker. CLI harnesses are
// gated by their project marker; GUI-app harnesses by whether the user has the
// app configured. This keeps an unrelated harness's globals out of the picker (a
// pure Codex project won't list every ~/.claude skill). Assumes home != ""
// (callers only reach here for user-scope scanning).
func activeHarnesses(root, home string) []harnessConvention {
	var out []harnessConvention
	for _, h := range harnessConventions {
		if h.active(root, home) {
			out = append(out, h)
		}
	}
	return out
}

// active reports whether this harness is relevant to the workspace at root.
func (h harnessConvention) active(root, home string) bool {
	// CLI harness: declared by project marker dirs; presence in root activates.
	if len(h.markerDirs) > 0 {
		for _, m := range h.markerDirs {
			if dirExists(filepath.Join(root, m)) {
				return true
			}
		}
		return false
	}
	// GUI-app harness (no project marker, e.g. Cowork): self-gates on whether the
	// user actually has it configured — any resolved user path exists.
	for _, p := range h.userMCPFiles {
		if fileExists(p.resolve(home)) {
			return true
		}
	}
	for _, p := range h.userSkillDirs {
		if dirExists(p.resolve(home)) {
			return true
		}
	}
	return false
}

// harnessForProjectDir maps a project-relative skill/MCP candidate path to the
// harness it belongs to ("" = generic / not harness-specific). ".agents" is
// Codex's (and the OpenAI Agents SDK's) cross-harness skill dir; ".codex" is
// Codex's config/MCP dir — both map to codex.
func harnessForProjectDir(rel string) string {
	switch {
	case hasPathPrefix(rel, ".claude"):
		return "claude"
	case hasPathPrefix(rel, ".agents"), hasPathPrefix(rel, ".codex"):
		return "codex"
	default:
		return ""
	}
}

// hasPathPrefix reports whether p equals prefix or sits under prefix/.
func hasPathPrefix(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}
