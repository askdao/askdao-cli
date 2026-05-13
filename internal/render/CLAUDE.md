# internal/render/
> L2 | 父级: ../../CLAUDE.md

KOL 审阅 UX 渲染层 —— v0.5 中等详情卡片（mid-density，7 块顶层 ~80-90 行屏幕空间）+ inline reasoning + 入口扩展。所有渲染走 `Renderer{Out, Color, Width}`，cmd-layer 切 `--no-color` / pipe to file 时传 `NewPlain(w)` 拿干净文本。

## 成员清单

- **colors.go** — `Renderer` 结构（Out / Color / Width）+ ANSI 常量（red / green / yellow / blue / grey / bold / dim）+ `New()` / `NewPlain(w)` 构造器 + `section(title)` 画 `═══` 双线包夹标题。手写一小撮 ANSI，不引第三方 dep。
- **lists.go** — `RenderItemList(r, items, opts)` 双列布局 + 前 N 截断 + "... and M more" 尾行（design.md §3.1 字段三档分类的"列计数 + 入口"档位实现）；`RenderKVList` 给 PERSONA / RUNTIME 块的 `Label : Value` 对齐；`JoinTruncated` 给 inline 上下文（capability scopes 等）。`MoreEntry` 字段拼出 `[D] view all   [F] view filtered` 这种入口提示。
- **reasoning.go** — `RenderReason(r, reason, opts)` 输出 `↳ Why: ...` + 多行连续对齐 + 可选 `Confidence: 0.NN`（design.md §3.1 inline reasoning 形态）；`RenderReasoningTrace` 给 `[R] view full reasoning trace` 命令（编号 + 多条决策）。
- **warnings.go** — `TranslationWarning` 结构（Field / Action / Reason / Severity / FallbackAttempted；render 包不强依赖 conductor schema，cmd-layer 自己拼装）+ 严重度三级（HIGH / MEDIUM / LOW）+ `WarningView` 二选（ViewSummary 默认折叠 MEDIUM/LOW 成计数行 / ViewAll 全展开）。`CountSummary` 给 header 用。**HIGH 永远 verbatim**，MEDIUM/LOW 默认折叠是 acceptance criterion 的关键约束。
- **diff.go** — `DiffAgentSpec(a, b)` 显式字段级 walker（不走 reflection）覆盖 KOL 实际编辑的字段（metadata / persona / capabilities / mcp_servers / workspace.packages / preferred_harness）。MCP 用名字做 union diff（add/remove/modify）。`RenderDiff` 输出 design.md §3.5 deploy preview 形态：`field.path:` / `-  before` / `+  after` 三行块。
- **summary.go** — `RenderSummary(r, SummaryInput)` 七块顺序：PERSONA / SKILLS / MCP SERVERS / CAPABILITIES / RUNTIME / SUBSCRIBER ONBOARDING / TRANSLATION WARNINGS。`SummaryInput` 装 AgentSpec 之外的额外材料（dev/prod 计数、被过滤的 MCP server 列表、translation warnings、PersonaFileNote 等）—— 这些不是 AgentSpec 的字段但卡片要展示，cmd-layer 在 detection.json + provider plan + adapter feedback 之间合成。
- **payload.go** — `RenderPayload(r, root, DeploymentPayload, ProjectArchetype, full)` 渲染部署清单。`full=true`（`askdao bundle`）：archetype 行 + evidence + `WILL UPLOAD`（每条 include，目录附 immediate-children 摘要如 `SKILL.md, scripts/ (3)`）+ `SKILL REFERENCES`（vendored skill 的 source @ shorthash + resolvable）+ `EXCLUDED`（每条 + reason）+ ignore sources 行。`full=false`（`askdao detect --summary` 末尾）：三行精简（archetype / N files X KB will upload · M refs · K excluded / "run `askdao bundle`"）。`humanSize` 渲字节、`childSummary` 走一层 ReadDir。
- **\*\_test.go** — 五份 unit 测试 + 一份 golden file 视觉回归（`TestRenderSummary_StableLayout`）。运行 `UPDATE_GOLDEN=1 go test ./internal/render -run StableLayout` 重新生成 golden。`TestRenderSummary_SectionSmoke` 是补丁测试 —— 单条 section 漏内容时直接报哪条而不是丢整 golden 给人看。
- **testdata/golden_summary.txt** — design.md §3.1 fixture 渲染 snapshot；layout 漂移（多一空行、字段顺序变、字段宽度调整）即报错。

## 设计约束

- **不引第三方渲染 dep**：手写 ANSI + 简单 `═══` 边框够用。`r3labs/diff` 等也不引 —— `DiffAgentSpec` 的显式 walker 让 path 字符串可控（KOL 友好），reflection-based diff 反而难调输出顺序。
- **Renderer 不持状态**：所有渲染函数收 `*Renderer` 做 first arg。tests 用 `bytes.Buffer + NewPlain(&buf)` 抓输出做 golden 比对。
- **Color=false 是 first-class**：`color()` helper 在 `Color=false` 时直接返回原字符串。所有测试都跑 plain 模式确保 layout 不依赖 ANSI 字符宽度。
- **HIGH warnings 永远 verbatim**：design.md §3.1 强约束。MEDIUM / LOW 默认折叠成 `MEDIUM (1) · LOW (0)   [W] see all` 一行；用户敲 `[W]` 切 ViewAll 才展开。
- **TranslationWarning 自带类型**：render 不依赖 conductor 端 schema（Phase 1 还没定）—— 在本包定义结构，cmd-layer 自己组装。
- **Truncation 三档**（design.md §3.1）：必列具体（Skills / MCP / Vault / 关键路径 / Tool overrides / apt libs）→ 全列；列计数 + 入口（28 个 pip / 14 dev filtered / 网络白名单）→ `RenderItemList` + 入口提示；展开 reasoning（model 选 / tool override / skill 推 / warning）→ `RenderReason` inline。
- **不做 TUI / 多语言 / pipe-friendly JSON 模式**：out of scope 明确，留 Phase 3。

## 依赖

仅标准库（`fmt` / `io` / `sort` / `strings` / `os` / `reflect`）+ `internal/types`。

## 字段输出对应

| 输出位置 | 函数 | 备注 |
|---|---|---|
| `init --auto` 后的中等详情卡片 | `RenderSummary` | 7 块完整 |
| `agent show <name>` 默认视图 | `RenderSummary` | 同上 |
| `agent show --reasoning` / `[R]` | `RenderReasoningTrace` | 全 decisions 编号列表 |
| `agent show --warnings` / `[W]` | `RenderTranslationWarnings(view=ViewAll)` | HIGH+MEDIUM+LOW 全展开 |
| `agent deploy` diff preview | `DiffAgentSpec` + `RenderDiff` | KOL 改 yaml 后 pre-deploy 看变更（vs `.askdao/recommendation.yml`） |
| `agent deploy` translation report | `RenderTranslationWarnings` | conductor `/cli/deploy` 返回的 `translation_report` —— HIGH-block 路径 `ViewAll`，部署成功后摘要 `ViewSummary`（cmd 层把 conductor 小写 enum 转 `render.Severity*`） |

## 消费方（cmd 层）

- `agent init --auto`（issue #8）：detection.json + RecommendResponse + 交互菜单（A/E/R/S/D/F/M/W/P/Q）→ `RenderSummary` + `RenderReasoningTrace` + `RenderTranslationWarnings`；本包是渲染 primitives，cmd 是 orchestrator
- `agent show`（issue #8）：复用 `RenderSummary` / `RenderReasoningTrace` / `RenderTranslationWarnings`（`--full` 直 pipe 原 yaml）
- `agent deploy`（M4）：`DiffAgentSpec` + `RenderDiff` 做 pre-deploy diff（vs `.askdao/recommendation.yml`）；`RenderTranslationWarnings` 渲染 conductor `/cli/deploy` 返回的 `translation_report`（HIGH-block → `ViewAll` + exit 1；部署成功 → `ViewSummary` 折叠 MEDIUM/LOW）

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
