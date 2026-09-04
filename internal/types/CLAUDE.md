# internal/types/
> L2 | 父级: ../../CLAUDE.md

askdao-cli pipeline 的两份 schema 真相源。所有上层模块依赖此包定义的结构契约；服务端镜像同一 schema（CI diff 校验对齐）。字段语义细节见各文件头注释；历史演进见 git log / PR。

## 成员清单

- **detection.go** — `Detection` schema（detection.json L1-L3 产物）：packages/languages/dockerfile AST/runtimes/mcp_configs/skills/required_secrets/tool_risk_hints/harness_signals；DetectedSkill 与 DetectedMCPConfig 各带 Scope（project|user）+ Harness 支撑双 scope 扫描；`UnknownSecretPurpose` 常量供 scanner 标记 + recommender 排除
- **agent_spec.go** — `AgentSpec` schema（askdao-agent.yml，harness-neutral 中间格式）：八块顶层 + harness_specific + memory/wiki/guardrails/outcomes/schedule/provenance/status 服务端业务块（`*bool` 三态声明即生效语义）+ 订阅者身份层 metadata（display_name/avatar/theme_color/category/language）；yaml+json 双 tags
- **model_catalog.go** — 模型目录镜像类型（三档 class 视图 + models 白名单条目）+ 离线兜底目录（无 concrete id——二进制不含模型 id）
- **detection_test.go / agent_spec_test.go** — round-trip + strict 拒收 + 版本戳钉死
- **testdata/** — 合法/非法 yaml fixture

## 设计约束

- 仅 schema + tags，不带 Validate()/builder；校验留给上层
- nullable scalar 用 `*T`；nullable slice/map 用裸类型 nil 即 null
- 异构列表用 omitempty union 字段实现，避免自定义 unmarshaler

## 版本演进协议

bump `DetectionSchemaVersion` / `AgentSpecAPIVersion` 必须同步：服务端镜像（CI diff）→ 本目录 fixture → design.md 对应节。字段增删先在本包加、再加测试、再让上层适配。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
