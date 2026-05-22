# internal/observe/
> L2 | 父级: ../../CLAUDE.md

`agent edit --observe` 的临时 hook 配置生命周期管理。把 Claude Code 的 `PreToolUse` hook（matcher `Skill` + `mcp__.*`）临时注入项目级 `.claude/settings.local.json`，让 KOL 跑真实 claude session 时把激活的 skill/MCP 上报到 webstudio `/api/observe` 端点；session 后**零残留**还原。webstudio 是 receiver，本模块是 hook 配置的写入/清理方，两者经 HTTP（`127.0.0.1:<port>/api/observe`）解耦。

## 成员清单

- **settings.go** — `Install(projectRoot, port) (cleanup, err)` / `SweepStale(projectRoot) error`。`Install` 读现有 settings 整文件备份 → 在 `hooks.PreToolUse` **append** 两条带标记规则（标记 = url 含 `/api/observe`），**绝不重写用户其他键**；`cleanup` 字节级还原（existed 写回 original / 否则删文件）；`SweepStale` 启动自检移除上次崩溃残留的标记规则。包内 helper：`rule`(造 PreToolUse 条目) / `preToolUse` / `setPreToolUse`(空容器自动 prune) / `stripObserveRules` / `isObserveRule`(按 url 标记识别) / `writeSettings`。
- **settings_test.go** — 无 prior settings 的 install→cleanup（建文件→删文件）/ 有 prior settings 的 append→字节级还原+不丢用户键 / SweepStale 清残留+留用户规则+空容器 prune+无文件 no-op。

## 设计约束

- **零残留三件套**：① 进程侧 `defer cleanup`（正常退出）② 启动 `SweepStale` 自检（兜底崩溃/Ctrl-C，因 askdao-cli 被 kill 时 hook 不触发，spike R6）③ 整文件备份还原（不依赖增量解析的精确性）。
- **项目级非用户级**：写 `<projectRoot>/.claude/settings.local.json`（gitignored，优先级高于 settings.json），projectRoot = `agent edit` 的 `--dir`，**不是** home（spike §6）。
- **只 append 带标记，不整体重写**：合并进已有 `hooks.PreToolUse` 数组，清理时按 url 标记精确移除，保住 KOL 自有 hooks/permissions（spike §6）。
- **先写配置再起 claude**：hooks 在 session 启动时快照、运行中不热重载（spike R4）。`Install` 由 webstudio `OnReady(port)` 回调触发，确保配置在 KOL 起 claude 前就位。
- **单向依赖**：observe 不依赖 webstudio/pipeline/deploy；webstudio 也不依赖 observe（cmd 层经 `OnReady` 注入串联）。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
