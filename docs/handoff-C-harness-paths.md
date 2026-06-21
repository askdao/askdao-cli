# Handoff C — Codex/Cursor harness 路径补全

> v0.8 第二步 · **线 C**（纯 askdao-cli，小而确定）。调研 + 完整 patch 已就绪，本文件**自包含**：全新窗口读完即可 review + 落地。

---

## 0. 项目背景（全新窗口必读）

**askdao-cli 是什么**：AskDAO 体系内唯一开源的子项目（信任锚点）。Go 1.26 单二进制 CLI，KOL 在本地项目目录跑一行命令 → 扫描技术栈 → 生成 Anthropic Managed Agents 配置 → 经 Conductor 部署。设计哲学：本地隐私（只读元数据不上传文件内容）、确定性优先、review-and-edit。

**立足点（已锁定）**：场景 = Skills Pipeline；第一期 = Anthropic Managed Agents MVP。

**当前代码状态**：分支 `main`，第一步（`agent edit` Web 工作台）已 merge（#32 #33）。架构：`internal/{types,scanner,...}` + `cmd/askdao`。

**工作纪律（GEB 分形文档协议）**：改代码必须同步文档——L3 文件头 INPUT/OUTPUT/POS、L2 目录 CLAUDE.md，固定带 `[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md`。**改代码不同步文档 = 未完成。**

**验证习惯**：`cd askdao-cli && go test ./... && make build`。

**沟通**：中文沟通，英文代码，简洁直接。

---

## 1. 这条线是什么

补全 `internal/scanner/harness_scope.go` 的 **Codex / Cursor user-scope skill/MCP 路径**（现为 nil TODO），让 KOL 装在 user 根目录的全局 skill/MCP 能被 Web 工作台扫到并勾选。当前只有 Claude Code 实装。

**为什么要做**：KOL 的 skill/MCP 常装在 user 根目录（不在项目里），打包必须能选到；harness 取决于工作目录特征（有 `.claude` 走 Claude / 有 `.codex` 走 Codex / 有 `.cursor` 走 Cursor）。当前 Anthropic MVP 以 Claude Code 为主，本线价值在**未来 harness 扩展**——小、确定、可独立交付。

## 2. 调研结论（已完成，必读）

### 2.1 现状理解（已核对代码）

`harness_scope.go` 的核心结构 `harnessConvention`：`name` / `markerDirs []string` / `userSkillDirs []string` / `userMCPFiles []string`，路径均 home-relative。Claude 模板已填全。消费链路：`activeHarnesses(root)` 按 root 下 marker 目录是否存在门控 user-scope 扫描；`DetectSkills`/`DetectMCPConfigs` 遍历 `userSkillDirs`/`userMCPFiles` 拼 `HomeDir + rel`；`harnessForProjectDir(rel)` 把 project 候选路径前缀映射到 harness 标签。

### 2.2 路径核对表

| harness | project skill dir | user skill dir | project MCP | user MCP | marker | 来源/置信度 |
|---|---|---|---|---|---|---|
| **claude**（现状） | `.claude/skills`, `.agents/skills` | `~/.claude/skills` | `.mcp.json` | `~/.claude.json` | `.claude` | 已实装/确定 |
| **codex** | `.agents/skills` | `~/.agents/skills` | `.codex/config.toml` (TOML) | `~/.codex/config.toml` (TOML) | `.codex` | 官方 [codex/skills](https://developers.openai.com/codex/skills) + [codex/mcp](https://developers.openai.com/codex/mcp) / **确定** |
| **cursor** | 无 skill 概念（rules `.cursor/rules/*.mdc`，非 SKILL.md） | **无**（user rules 存 Cursor settings DB，非文件系统） | `.cursor/mcp.json` (JSON) | `~/.cursor/mcp.json` (JSON) | `.cursor` | [Cursor docs](https://cursor.com/docs/rules) / **确定**（MCP）；skill 确定无此概念 |
| **cowork** | — | — | — | — | — | 桌面通用 Agent App 非 CLI harness，**无文件系统约定，建议暂不支持** |

### 2.3 关键判别

- **Codex MCP 是 TOML 格式 `[mcp_servers.<id>]` 表**（不是 JSON `mcpServers`）。现有 `mcp_config.go` 的 `readMCPConfig` 是 **JSON-only**，无法解析 TOML —— 这是真正的额外工作量（见 §4）。
- **Cursor / Claude MCP 都是 JSON `mcpServers` 同形**，可直接复用 `readMCPConfig`。
- **现状 bug**：旧代码 marker 写 `.agent`（单数，Codex 根本不创建该目录），且 `harnessForProjectDir` 永远命不中真实的 `.agents/skills` → Codex project skill 此前丢失 harness 标签。patch 一并修复。
- **Codex skill 路径版本漂移**：官方文档已迁到 `.agents/skills`，2026-04 第三方教程仍写 `~/.codex/skills`。**采信官方 + 跨 harness 通用约定 `.agents/skills`**。

## 3. 完整 patch（review 后落地）

> 这是 C 调研产出的 patch。请 **review 后落地**——特别留意 L3 头注释里 `dirExists` 等措辞按实际核对（`dirExists` 是包内非导出 helper，定义在别处，本文件只调用）。

### Patch A — `internal/scanner/harness_scope.go`（整文件替换）

```go
// [INPUT]: 依赖标准库 path/filepath / strings
// [OUTPUT]: 对外提供 ScanScopeOpts；包内提供 activeHarnesses / harnessForProjectDir / hasPathPrefix
// [POS]: internal/scanner 的 harness 感知层 —— 按工作目录的 harness 特征（.claude→Claude / .codex→Codex / .cursor→Cursor）
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
//
// userMCPFiles holds only files our JSON parser (readMCPConfig) understands —
// i.e. the canonical {"mcpServers": {...}} shape. Codex's MCP config is TOML
// ([mcp_servers.<id>] tables) and is therefore NOT listed here yet; wiring it
// up needs a TOML reader, not just a path (see codexMCPNote below).
type harnessConvention struct {
	name          string   // "claude" | "codex" | "cursor"
	markerDirs    []string // project-root markers; presence activates user-scope scan
	userSkillDirs []string // home-relative global skill dirs (SKILL.md form)
	userMCPFiles  []string // home-relative global MCP config files (JSON mcpServers shape only)
}

// harnessConventions drives harness-aware user-scope scanning.
//
//   - claude: fully specified. Marker .claude; global skills ~/.claude/skills;
//     global MCP ~/.claude.json (JSON mcpServers).
//   - codex: skills confirmed at ~/.agents/skills (SKILL.md form — the same
//     cross-harness dir already in SkillDirCandidates for project scope). Marker
//     is the .codex config dir. Global MCP lives in ~/.codex/config.toml as TOML
//     [mcp_servers.<id>] tables — intentionally NOT in userMCPFiles because the
//     current readMCPConfig only parses JSON; a TOML reader is required (codexMCPNote).
//   - cursor: has NO skill concept (it uses rules: .cursor/rules/*.mdc project +
//     user rules stored in Cursor's settings DB, not a filesystem dir) — so
//     userSkillDirs stays nil. Global MCP ~/.cursor/mcp.json IS canonical JSON
//     mcpServers and is wired up directly.
//
// Sources:
//
//	codex skills    https://developers.openai.com/codex/skills  (~/.agents/skills, .agents/skills)
//	codex mcp/toml  https://developers.openai.com/codex/mcp     (~/.codex/config.toml, [mcp_servers.<id>])
//	cursor mcp      https://cursor.com/docs/  (~/.cursor/mcp.json JSON mcpServers)
//	cursor rules    https://cursor.com/docs/rules  (no SKILL.md concept; .cursor/rules/*.mdc)
//
// cowork (Claude Cowork) is deliberately absent: it is a desktop general-agent
// app, not a terminal CLI harness, and exposes no documented project marker /
// skill dir / MCP file convention to scan. Revisit if that changes.
var harnessConventions = []harnessConvention{
	{
		name:          "claude",
		markerDirs:    []string{".claude"},
		userSkillDirs: []string{".claude/skills"}, // ~/.claude/skills
		userMCPFiles:  []string{".claude.json"},   // ~/.claude.json
	},
	{
		name:          "codex",
		markerDirs:    []string{".codex"},         // Codex config dir (was wrongly ".agent")
		userSkillDirs: []string{".agents/skills"}, // ~/.agents/skills (official codex skills path)
		// codexMCPNote: global MCP is ~/.codex/config.toml in TOML
		// ([mcp_servers.<id>]); readMCPConfig is JSON-only, so we cannot list it
		// here without a TOML parser. Tracked as a separate work item.
		userMCPFiles: nil,
	},
	{
		name:          "cursor",
		markerDirs:    []string{".cursor"},
		userSkillDirs: nil,                         // Cursor has no SKILL.md concept (uses rules)
		userMCPFiles:  []string{".cursor/mcp.json"}, // ~/.cursor/mcp.json (JSON mcpServers)
	},
}

// activeHarnesses returns the harness conventions whose marker directory exists
// in the project root — i.e. which harnesses' user-level skills/MCP are relevant
// to this workspace. A project with .claude/ pulls in ~/.claude global skills;
// a project with .codex/ (Codex) pulls in ~/.agents/skills; a project with
// .cursor/ pulls in ~/.cursor/mcp.json. This keeps an unrelated harness's
// globals out of the picker (a pure Codex project won't list every ~/.claude skill).
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
// harness it belongs to ("" = generic / not harness-specific). ".agents" is
// Codex's (and the OpenAI Agents SDK's) cross-harness skill dir; ".codex" is
// Codex's config/MCP dir — both map to codex.
func harnessForProjectDir(rel string) string {
	switch {
	case hasPathPrefix(rel, ".claude"):
		return "claude"
	case hasPathPrefix(rel, ".agents"), hasPathPrefix(rel, ".codex"):
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
```

> **落地前自检**：上面用到的 `dirExists` 必须已在 `internal/scanner` 包内（旧 `harness_scope.go` 或 `skills_dir.go`/`mcp_config.go`）有定义。若旧文件里 `dirExists` 原本定义在 `harness_scope.go`，整文件替换会删掉它 —— 那就把 `dirExists` 保留进新文件，或确认它在别处。**先 `grep -rn "func dirExists" internal/scanner/` 确认归属再替换。**

### Patch B — `internal/scanner/CLAUDE.md`（L2 文档同步，GEB 要求）

替换 **harness_scope.go** 那一条成员清单：

旧：
```
- **harness_scope.go** — v0.8 harness 感知层。… codex marker `.agent`、cursor `.cursor` 的 user 路径留 nil，TODO 待 Phase 2 核对 …
```

新：
```
- **harness_scope.go** — v0.8 harness 感知层。`ScanScopeOpts{HomeDir}` + `harnessConventions` 表：**claude** marker `.claude` → user `~/.claude/skills` + `~/.claude.json`；**codex** marker `.codex` → user skill `~/.agents/skills`（官方 codex skills 路径，SKILL.md 形态，与 SkillDirCandidates 的 `.agents/skills` 同源），user MCP 留 nil —— Codex MCP 是 `~/.codex/config.toml` 的 TOML `[mcp_servers.<id>]` 表，`readMCPConfig` 仅解析 JSON，接它需新增 TOML 解析（独立 issue，见 `codexMCPNote`）；**cursor** marker `.cursor` → user skill nil（Cursor 无 SKILL.md 概念，只有 `.cursor/rules/*.mdc` + settings DB 里的 user rules），user MCP `~/.cursor/mcp.json`（JSON `mcpServers` 同形，直接复用）。**cowork** 不入表 —— Claude Cowork 是桌面通用 Agent App 非 CLI harness，无文件系统约定。`activeHarnesses(root)` 按工作目录 marker 门控 user 扫描；`harnessForProjectDir(rel)` 把 `.claude`/`.agents`/`.codex`/`.cursor` 前缀映射到 harness 标签。被 skills_dir.go / mcp_config.go 消费。
```

同时把 `mcp_config.go` 那条成员清单里 user scope 描述补 Cursor（现只写了 claude）：

旧片段：`user scope 由 activeHarnesses(root) 门控——root 有 .claude/ 才扫 ~/.claude.json …`
新片段：`user scope 由 activeHarnesses(root) 门控——root 有 .claude/ 扫 ~/.claude.json（Harness=claude）、有 .cursor/ 扫 ~/.cursor/mcp.json（Harness=cursor，JSON 同形）；Codex 的 ~/.codex/config.toml 是 TOML，暂未接（需 TOML 解析）。均标 Scope=user。`

## 4. 注意点 / 决策

1. **Codex MCP TOML 解析是独立工作量**（不是填路径）：要扫 `~/.codex/config.toml` + `.codex/config.toml`，需新增 TOML 解析（项目已依赖 `github.com/BurntSushi/toml` 可复用），把 `[mcp_servers.<id>]`（含 `url`/`command`/`args`/`bearer_token_env_var`）映射到 `MCPServerConfig`，并在 conventions 表区分 JSON/TOML 两类（或 `readMCPConfig` 按扩展名分派）。**建议拆独立 issue，本 PR 先接 Codex skill + Cursor MCP（JSON/目录可直达的）。**
2. **Codex marker 取 `.codex`**（保守）：`.agents/skills` 已被 `SkillDirCandidates` 在 project scope 覆盖，marker 用 `.codex` 已足够触发 user scope。若想"项目里有 `.agents/skills` 也算 Codex workspace"，可把 `markerDirs` 设 `{".codex", ".agents"}`——按需确认。
3. **建议补 `harness_scope_test.go`**（当前不存在）：覆盖 `.codex/` marker → `~/.agents/skills` user skill 命中、`.cursor/` marker → `~/.cursor/mcp.json` user MCP 命中两条新路径。

## 5. 验证 / 分支 / 起步

- 验证：`go test ./...`（含新 `harness_scope_test.go`）+ `make build`。
- 分支：`feature/codex-cursor-harness-paths`（自取），PR → `main`。
- 起步：① `grep -rn "func dirExists" internal/scanner/` 确认 helper 归属 ② review + 落地 Patch A/B ③ 决定 Codex MCP TOML 是否本 PR 做（建议拆）④ 补测试。
- GEB：改 `harness_scope.go` 同步 L3 头 + `internal/scanner/CLAUDE.md`。

---

## 开窗 opening line（复制即用）

```
做 Codex/Cursor harness 路径补全这条线（v0.8 第二步 C）。
先读 askdao-cli/docs/handoff-C-harness-paths.md（含调研结论 + 完整 patch），然后 review 落地。
```
