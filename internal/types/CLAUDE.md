# internal/types/
> L2 | 父级: ../../CLAUDE.md

askdao-cli pipeline 的两份 schema 真相源。所有上层模块（scanner / providers / recommender / render / cmd）依赖此包定义的结构契约；服务端通过镜像同一 schema 契约解析 deploy 上送的 yaml（CI diff 校验对齐）。

## 成员清单

- **detection.go** — `Detection` schema (detection.json L1-L3 产物)。含 v0.4 完整 Dockerfile AST + v0.3 五个新字段（mcp_configs / skills / required_secrets / tool_risk_hints / harness_signals）+ v0.6 两个新字段（`Archetype ProjectArchetype` = code_app/skill_pipeline/mixed/unknown + confidence + evidence；`DeploymentPayload` = `Includes`/`Excludes` 两份 `PayloadEntry` 清单 + `TotalBytes`/`TotalFiles`/`IgnoreSources`）。**v0.7 删 `SkillReferences` 字段 + `SkillRef` struct** —— Anthropic Managed Agents 无公共 registry，所有 custom skill 一律进 Includes 上传；vendored 标签由 `DetectedSkill.LockedSource` + `LockedHash` 携带，bundle UI inline 渲染。`DetectedSkill` 字段：`Description` / `BundleBytes` / `BundleFiles` / `LockedSource` / `LockedHash` / `IsLocalOriginal`（全 omitempty）。`DetectionSchemaVersion = "askdao/detection/v1"` 是版本戳。JSON tag only。**v0.8 给 `DetectedSkill` + `DetectedMCPConfig` 各加 `Scope`（project|user）+ `Harness`（claude|codex）** —— 支撑 harness 感知双 scope 扫描（工作目录 `.claude`→扫 `~/.claude`、`.agent`→Codex）+ 工作台按 scope 分组展示。
- **agent_spec.go** — `AgentSpec` schema (askdao-agent.yml L4 输出 · harness-neutral 中间格式)。`apiVersion: askdao.ai/v1` + 八块顶层（metadata / persona / capabilities / mcp_servers / custom_tools / skills / workspace / vault_hints）+ `harness_specific` escape hatch + memory/guardrails/provenance/status 服务端业务字段。同时提供 yaml + json tags。`AgentSpecAPIVersion` / `AgentSpecKind` 是版本戳。**v0.7 删 `Metadata.PersonaFile` 字段** —— persona 内容全在 `Persona.SystemPrompt` literal block 内，单一真相源。`Skill.Type` 三个值：`builtin` / `custom_local` / `git_repo`（最后一个 harness-agnostic 保留，Anthropic Adapter HIGH-warning skip）。`Skill.Path`（custom_local 用）= **相对 KOL 项目根的 skill 目录路径**（如 `.agents/skills/tts`），deploy 时 `ZipDir` 递归打包目录全部内容；harness 中性 invariant：filepath.Base 切掉 `.claude/`/`.agents/` 等上级，Anthropic 端只看到 `tts/SKILL.md` 形态。**v0.8 `Metadata` 加订阅者身份层（agent-bound）`Category` + `ThemeColor` + `DisplayName` + `Avatar`：ThemeColor 预设色板 token 非自由 hex；DisplayName 中文展示名，与 Name（运行时标识+dedup key）解耦、改它不动 dedup；Avatar 单 string 前缀（""=默认 / `icon:<lucide>` / url，颜色复用 ThemeColor）；四者跨仓贯通订阅者端 Group 页 / 广场。`Skill` 加 `Scope`（project|user，user 层全局 skill 的 `Path` 为绝对/`~` 前缀，deploy 据此解析）。**
- **detection_test.go** — Detection JSON marshal→unmarshal→DeepEqual round-trip 测试 + schema_version 钉死。
- **agent_spec_test.go** — AgentSpec YAML round-trip 测试（基于 testdata/valid_agent.yml）+ strict 模式（KnownFields=true）拒收 invalid fixture + apiVersion/kind 钉死。
- **testdata/valid_agent.yml** — 合法 fixture，覆盖全部八块。约定：`omitempty` 字段空值省略不写，避免 round-trip 把 `[]` 转为 nil 触发 DeepEqual 假阴。
- **testdata/invalid_agent.yml** — 非法 fixture，含未知顶层字段 `mystery_field`，strict 解码必须 reject。

## 设计约束

- 仅 schema + tags，不带 `Validate()` / builder。校验逻辑留给 scanner / recommender / cmd 层。
- 区分 nullable scalar 用 `*T`（如 `FinalStageName *string`、`LastAppliedAt *time.Time`）；nullable slice/map 用裸类型，nil 即 null。
- 异构列表（如 `DetectedSkill` 同时承载 custom_local skill 与 implied builtin skill 两种形态）通过 `omitempty` 字段 union 实现，避免自定义 unmarshaler。
- `DeploymentPayload` 是「上传清单」的真相源：`PayloadEntry.Path` 目录以 `/` 结尾、`Bytes`/`Files` 为递归总和。**v0.7 起所有 custom skill 一律进 Includes**（vendored 与 repo-原生 同等对待），origin 信息携带在 `PayloadEntry.Reason`（`"repo-native"` 或 `"vendored: <source> @ <hash>"`）。服务端按这份清单打包上送，不再做"按 references 重装"。

## 版本演进协议

- bump `DetectionSchemaVersion` 或 `AgentSpecAPIVersion` 必须同步：
  1. 服务端镜像模型同步更新（私有仓，CI diff 校验）
  2. 更新本目录的 fixture
  3. 更新 design.md §4 / §5
- 字段增删：先在本包加，再加测试，再让上层适配。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
