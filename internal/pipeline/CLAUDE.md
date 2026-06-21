# internal/pipeline/
> L2 | 父级: ../../CLAUDE.md

唯一 orchestration 层 —— 把 scanner / dev_filter / providers / policy / recommender 五件事串成 `pipeline.Run(ctx, opts)`。`askdao detect`（LLM=nil）和 `askdao agent init --auto`（LLM≠nil）共享这个入口。

## 成员清单

- **pipeline.go** — `Run(ctx, Options) (*Result, error)` 是唯一入口。Options 字段：`Root` / `Excludes` / `AgentName` / `PreferredHarness` / `LLM`（nil 跳过推荐）/ `SyftRunner`（注入测试 fake）/ `HomeDir`（harness probe override）/ `IncludeEvals`（部署清单是否含 skill `evals/`）。Result：`Detection` / `ProviderPlans` / `Recommendation` / **`AgentSkills`（v0.7 起 deterministic 构造的 skills 段）** / `Warnings`。流程：
  1. Scanner phase：DetectLanguages / DetectRuntimes / runSyft（缺 syft 时软降级）/ ParseDockerfile / DetectMCPConfigs / DetectSkills / DetectRequiredSecrets / DetectHarnessSignals
  2. ApplyDevFilter — manifest 重标 syft 输出的 IsProd
  3. Provider phase：每个 provider 跑 Detect → Plan，命中的合并到 Detection.DetectedFrameworks / DetectedExternalServices；apt 列表通过 mergeAptHints 合并 provider plans + Dockerfile 抽取去重
  3b. Archetype + DeploymentPayload：`InferArchetype`（需 skills + languages + frameworks 都就绪）→ `DetectDeploymentPayload`（用 archetype 决定要不要剔 input/data 目录），payload warns 并入 Result.Warnings
  4. Policy phase：InferToolRiskHints 写到 Detection.DetectedToolRiskHints
  5. ScanInfo 装配（root / 时长 / 排除）
  **5b. Deterministic skills builder（v0.7）**：`BuildAgentSpecSkills(det)` 填到 `Result.AgentSkills`，cmd-layer 后用它覆盖 LLM 输出的 spec.Skills
  6. Optional LLM phase：发 RecommendRequest（含 Detection + ProviderSummary + Policy），收 RecommendResponse
- **skills_builder.go** — `BuildAgentSpecSkills(det) []types.Skill` deterministic 构造 agent.yml.skills 段：每个 DetectedSkill → `{type: custom_local, path: filepath.Dir(s.Source)}`（path 是相对项目根的 skill 目录路径）；每个 ImpliedAnthropicSkill 去重 → `{type: builtin, provider: anthropic, id: skillID}`。稳定排序。**信任边界原则**：LLM 适合软字段（model_class / system_prompt / persona / reasoning_*）；skill 引用是确定性事实字段，由 builder 取代 LLM 自由发挥（design.md §9.13，硬字段确定性填充、软字段才交 LLM）。
- **skills_builder_test.go** — 4 用例覆盖：1 原生 + 2 vendored 全产 custom_local（path 指目录非 SKILL.md）/ implied xlsx 产 builtin / duplicate SkillID 去重 / 空 DetectedSkills 安全返 nil。
- **pipeline_test.go** — 三个核心测试：
  - `TestPipeline_DetectOnly_NoLLM` — fixture 项目（pyproject.toml + main.py + deploy.yml + .env.example + .mcp.json）跑全管线，断言：FastAPI 框架命中 / pytest 标 dev / deploy.yml 触发 bash 覆盖 / GITHUB_TOKEN 跨链到 github MCP
  - `TestPipeline_WithMockLLM_ProducesAgentSpec` — 同 fixture + MockClient，AgentSpec 出来后 shell.permission=ask_for_dangerous（policy + recommender 契约）
  - `TestPipeline_SyftAbsentDoesNotFail` — syft 不在 PATH 时不 fail，软警告 + 其他扫描照常工作

## 设计约束

- **soft-fail over hard-fail**：syft 缺失 / dev_filter 错 / policy 错都进 `Warnings` 而非 `error`，让 KOL 看到部分结果而非整体失败。`error` 只用于真正"无法继续"的场景（root 不存在 / app index 失败）。
- **不持状态**：每个 Run 独立。所有 IO 走调用方传入的 ctx + opts，不读 process-global config。
- **LLM 通过接口注入**：`Options.LLM recommender.LLMClient`，cmd-layer 决定用 ConductorClient 还是 MockClient。pipeline 不识别具体实现。
- **dedupe is here, not in providers**：多个 provider 可能输出同一框架（不太可能，但 future-proof）/ 同一外部服务，pipeline 去重保证 Detection 不重复。
- **apt merge 跨源**：mergeAptHints 把 provider plans 的 apt + Dockerfile 抽取的 ExtractedAptPackages 合并去重。Dockerfile 抽取作为补充 evidence，reason 标注来源。
- **provider 顺序固定**：Python → Node → Go → Rust（与 issue #4/#5 落盘顺序一致）。多 provider 都 Detect 命中时，Plan 全跑，框架/服务全合并。

## 后续 issue 挂载点

- 对外端点 `POST /api/v1/cli/recommend` 已上线；cmd 层 `chooseLLMClient()` 通过 `ASKDAO_CONDUCTOR_URL` env 切真实 ConductorClient（否则离线 MockClient）
- 长尾框架 / 外部服务规则增量加在 internal/providers，pipeline 不需改

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
