# internal/recommender/
> L2 | 父级: ../../CLAUDE.md

L4 推荐器 —— askdao-cli 的"模糊推断"边界。L1-L3 全确定性，本目录开始进入策略启发式 + LLM 调用。按决策 9.1 LLM 走 conductor 中转（不走 BYOK），所以本目录只有"策略 heuristic"和"HTTP 客户端"两件事。

## 成员清单

- **policy.go** — `InferToolRiskHints(root)` 输出 `types.DetectedToolRiskHints`。三类信号源：
  - `productionPathPatterns` 文件存在即触发：`.github/workflows/deploy.yml` / `release.yml` / `.env.production` / `production.toml` / `pulumi.yaml` / `Chart.yaml` 等
  - `productionDirPatterns` 目录存在即触发：`terraform` / `k8s` / `kubernetes` / `helm` / `deploy`
  - `productionGlobs` 文件名 glob：`*.tf` / `.github/workflows/deploy-*.yml` / `.github/workflows/release-*.yml`
  - `userDataPatterns` 文件 / 目录混合：`users.csv` / `customers.csv` / `data` / `datasets`
  
  默认 policy 永远是 `always_allow`；产生 `ProductionSignals` 时往 `ToolOverridesRecommended` 加两条 `bash` + `write` → `always_ask`。这与 design.md §4 的示例对齐 —— policy 层不直接翻 default，让 LLM 后续基于 evidence 加 nuance。
- **fs.go** — 极薄包装 `os.Stat` / `os.ReadDir`，把 `os` import 局限在一处，让 policy.go 的依赖更干净。变量 `osStat` 留作测试可替换钩子。
- **llm.go** — `LLMClient` 接口 + 两实现：
  - `ConductorClient`：HTTP POST `/api/v1/cli/recommend`，bearer 鉴权可选，默认 90s 超时（覆盖 LLM tail latency）。响应解析后会校验 `apiVersion == askdao.ai/v1`，避免 conductor 端意外升级 schema 静默通过。**另加 `FetchModelClasses`（GET `/api/v1/cli/model-classes`）+ 模块级 `FetchModelClassesOrFallback(ctx, baseURL, token)`：拉 model 档目录喂 studio 第二步选择器，conductor 不可达/未登录时降级 `types.FallbackModelClasses()`（仅 slug/label/blurb、无 concrete id → 二进制不含模型 id，真 id 部署时由 conductor 从 model_class 解析）。另加 `FetchAppConfig`（GET `/api/v1/cli/config`）+ 模块级 `FetchStudioAssistantID(ctx, baseURL, token)`：读回桌面内嵌助手的 agent id（服务端常量），任何失败/未登录返 `""` 让侧栏降级静态帮助（同 FetchModelClassesOrFallback 优雅蓝本）。**
  - `MockClient`：`Override` 函数指针注入；nil 时走 `DefaultMockRecommend`，按请求材料拼一份"足够合法"的 AgentSpec —— 让开发期 / 单测在没有在线 conductor 时也能联调（`ASKDAO_CONDUCTOR_URL` 未设时 cmd 层 `chooseLLMClient` 默认走它），并作为 conductor `/cli/recommend` 集成测试的参考实现。
  
  `RecommendRequest` 携带 `Detection` + `ProviderSummary` slice + `Policy` + `AgentName` + `PreferredHarness`。`ProviderSummary` 是 internal/providers 的 `FrameworkPlan` 的 JSON-friendly 投影 —— 不直接 import providers 包是为了避免跟 conductor 端 mock server 引入跨包循环。
  
  `DefaultMockRecommend` 内部 helper：
  - `extractCompatibleMCPServers` 只透传 `AnthropicCompatible=true` 的 server（stdio 自动过滤）
  - `buildWorkspace` 从 detection 抽 prod-only pip / npm 包名，apt 列表去重合并 provider summary + detection.InferredAptPackages
  - `BuildVaultHints`（导出）**过滤非凭证**（跳过 `PurposeGuess == types.UnknownSecretPurpose` 的配置参数，不进 vault_hints）后按 required 拆 RequiredCredentials / OptionalCredentials；UsedByGuess.MCPServer 写成 `map[string]interface{}{"mcp_server": ...}` 对齐 schema 自由形态。被 `cmd/askdao/edit.go` + `cmd/askdao-studio/app.go` 作确定性硬字段覆写复用（治 mock + conductor 两路径）
  - `harnessFor` 优先级：`req.PreferredHarness` > `Detection.DetectedHarnessSignals.RecommendedHarness` > 兜底 `anthropic_managed_agents`
  - `Persona` 只设 `ModelClass:"balanced"`，**不再硬编码 `ModelPreferences` 具体 model id**（真 id 由 conductor 从 model_class 解析 / studio 选择器从 catalog 填），杜绝客户端硬编码模型 id

- **capabilities.go** — `DefaultCapabilities(policy)` 确定性生成 capabilities（hard field §9.13，同 skills 不交 LLM 即兴）：4 槽固定 `enabled=true` + 规范 scopes 词表（shell:read/write/execute · filesystem:read/write · web:fetch · code_execution:javascript/shell）+ permission（shell 按 production signals 收紧为 `ask_for_dangerous`，其余 `always_allow`）。被 `MockClient`（llm.go）+ `cmd/askdao/edit.go` loadOrScan 两分支覆盖 `spec.Capabilities`。Anthropic adapter 忽略 scopes，未来 harness 可用。
- **\*\_test.go** — `policy_test.go` 覆盖 production deploy + glob + dir + 用户数据 + 空树 + 错误边界；`llm_test.go` 覆盖 MockClient 默认输出 + Override 注入 + ConductorClient happy-path（用 `httptest.NewServer` 反向喂 `DefaultMockRecommend` 验证序列化往返）+ 非 2xx 错误带 body + apiVersion 校验 + 空 BaseURL。

## 设计约束

- **决策 9.1**：LLM 走 conductor 中转。本目录绝不直接 import `anthropic-sdk-go` / OpenAI SDK。所有"模糊推断"都委托给 conductor `POST /api/v1/cli/recommend`（已上线）。
- **不依赖 internal/providers**：用 `ProviderSummary` 中间结构传递必要字段，避免跨包循环（conductor 端 mock 不需要 providers 包参与）。
- **mock-first 设计**：开发期 / 单测全用 `MockClient`，集成测试切 `ConductorClient`。这让本仓库的 CI 不依赖 conductor 在线。
- **policy 层只标 override 不翻 default**：`RecommendedDefaultPolicy` 永远 `always_allow`，是为了把"是否要全局收紧权限"的决策权留给 LLM —— LLM 看到 evidence 列表会更有 nuance。policy 层只做"显然要 ask 的工具"硬约束。
- **apiVersion 钉死校验**：响应里 `Spec.APIVersion != AgentSpecAPIVersion` 直接 reject，避免 conductor 端意外升 schema 静默吞数据。

## 依赖

- 标准库 `net/http` / `encoding/json` / `context`
- `internal/types`（schema 真相源）

无新增三方依赖。

## 字段输出对应

| 输出位置 | 函数 | 备注 |
|---|---|---|
| `Detection.DetectedToolRiskHints` | `policy.InferToolRiskHints` | 由 issue #8 cmd 装配进 detection.json |
| `AgentSpec` 全字段 | `LLMClient.Recommend` | conductor 端 LLM 推断；mock 走 DefaultMockRecommend |
| `Provenance.ReasoningSummary` + `Provenance.ReasoningDecisions` | `LLMClient.Recommend` | KOL 中等详情卡片的 inline reasoning 来源 |

## 相关 issue / 后续

- issue #7：render 用 `RecommendResponse` 渲染中等详情卡片（已交付）
- issue #8：`cmd/askdao agent init --auto` 编排 scanner → providers → policy → llm → render（已交付）
- ConductorClient 的 BaseURL/token 目前从 `ASKDAO_CONDUCTOR_URL` / `ASKDAO_CONDUCTOR_TOKEN` env 读（cmd 层 `chooseLLMClient` / `deploy.go`）；`~/.askdao/config.yaml` 配置文件留 P2
- M4：`internal/deploy/` 复用同样的「HTTP 客户端到 conductor」模式接 `/cli/deploy`（见 `internal/deploy/CLAUDE.md`）

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
