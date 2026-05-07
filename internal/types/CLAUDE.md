# internal/types/
> L2 | 父级: ../../CLAUDE.md

askdao-cli pipeline 的两份 schema 真相源。所有上层模块（scanner / providers / recommender / render / cmd）依赖此包定义的结构契约；conductor 端 adapter 通过镜像 schema 解析 deploy 上送的 yaml。

## 成员清单

- **detection.go** — `Detection` schema (detection.json L1-L3 产物)。含 v0.4 完整 Dockerfile AST + v0.3 五个新字段（mcp_configs / skills / required_secrets / tool_risk_hints / harness_signals）。`DetectionSchemaVersion = "askdao/detection/v1"` 是版本戳。JSON tag only。
- **agent_spec.go** — `AgentSpec` schema (agent.yml L4 输出 · harness-neutral 中间格式)。`apiVersion: askdao.ai/v1` + 八块顶层（metadata / persona / capabilities / mcp_servers / custom_tools / skills / workspace / vault_hints）+ `harness_specific` escape hatch + memory/guardrails/provenance/status conductor 业务字段。同时提供 yaml + json tags。`AgentSpecAPIVersion` / `AgentSpecKind` 是版本戳。
- **detection_test.go** — Detection JSON marshal→unmarshal→DeepEqual round-trip 测试 + schema_version 钉死。
- **agent_spec_test.go** — AgentSpec YAML round-trip 测试（基于 testdata/valid_agent.yml）+ strict 模式（KnownFields=true）拒收 invalid fixture + apiVersion/kind 钉死。
- **testdata/valid_agent.yml** — 合法 fixture，覆盖全部八块。约定：`omitempty` 字段空值省略不写，避免 round-trip 把 `[]` 转为 nil 触发 DeepEqual 假阴。
- **testdata/invalid_agent.yml** — 非法 fixture，含未知顶层字段 `mystery_field`，strict 解码必须 reject。

## 设计约束

- 仅 schema + tags，不带 `Validate()` / builder。校验逻辑留给 scanner / recommender / cmd 层。
- 区分 nullable scalar 用 `*T`（如 `FinalStageName *string`、`LastAppliedAt *time.Time`）；nullable slice/map 用裸类型，nil 即 null。
- 异构列表（如 `DetectedSkill` 同时承载 custom_local skill 与 implied builtin skill 两种形态）通过 `omitempty` 字段 union 实现，避免自定义 unmarshaler。

## 版本演进协议

- bump `DetectionSchemaVersion` 或 `AgentSpecAPIVersion` 必须同步：
  1. 更新 conductor 端镜像 pydantic 模型
  2. 更新本目录的 fixture
  3. 更新 design.md §4 / §5
- 字段增删：先在本包加，再加测试，再让上层适配。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
