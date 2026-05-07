# Design Review v0.3 · 2026-05-06

> 触发：哥指出 askdao-cli 是开源项目，将来面向工程师社区（既有 Claude Code 用户也有 OpenAI Codex 用户）。
> v0.2 yaml 仍然是 Anthropic Managed Agents 字段对齐的格式 —— 即使三块布局正确，**字段命名仍暗示单一 harness**。
> 哥提出：askdao-cli 应输出**中间格式**（harness-neutral），conductor 端按目标 runtime 转换。
>
> Reviewer: Sam
> Status: 决策已对齐，design.md 升 v0.3
> Outcome: yaml 重写为 harness-neutral 中间格式 + 三阶段路线图

---

## 1. 起源回顾

### 1.1 历史决策（archived 分析）

`harness-design/archived-version/harness-selection-analysis.md` 原始推荐路径是：
- **核心 Harness 用 OpenAI Agents SDK**（Python 原生 + 循环可控 + 多模型）
- **Sandbox 内继续用 Claude Agent SDK**
- Claude Managed Agents 当时**不用**，作为 "Premium Tier 执行后端" 预留

### 1.2 当前现状

conductor 实际跑的是 Claude Managed Agents（M0-M2 + 候选 B #28），这是**为了快速起步的 pivot**。原始多 harness 路径并未取消，只是延后。

### 1.3 这次重审的本质

哥的提议**不是设计反复**，而是：
- **askdao-cli 的开源属性**让 yaml 暴露在外部工程师面前
- yaml 字段命名一旦绑定单一 harness，**对外的开源叙事就坍缩了**：开源工具暗示"我们只为 Anthropic 服务"
- 即使 conductor 端 Phase 1 仍只跑 Anthropic adapter，**yaml 字段也应当 harness-neutral**

---

## 2. 两套 harness 抽象层对比（事实层）

| 维度 | Anthropic Managed Agents | OpenAI Agents SDK |
|------|-------------------------|------------------|
| **控制平面** | Anthropic 云端（黑盒循环，SSE 流） | 你的 Python 进程（你看得见每一步） |
| **Agent 实体** | 服务端持久化资源（versioned，agent_id） | 进程内对象（每次 `Runner.run()` 实例化） |
| **Environment** | 独立资源（packages + networking） | `SandboxAgent.default_manifest` ——含 `File/Dir/LocalFile/GitRepo/S3Mount/...environment/users/groups` |
| **Sandbox** | 单一 cloud container（黑盒） | `SandboxClient` 多 provider（UnixLocal / Docker / E2B / Modal / Daytona / Vercel / Cloudflare ...） |
| **Session** | 服务端有状态资源 | `Session` Protocol（自实现，可对接 OpenViking） |
| **工具系统** | 内置 `agent_toolset_20260401`（8 固定）+ `mcp_toolset` + `custom` | `function_tool` + `hosted_tools` + MCP + `Capabilities`（Shell/Filesystem/Skills/Memory/Compaction） |
| **Tool 权限** | `always_allow / always_ask` 声明式 | guardrails + approvals 代码层 |
| **Skills** | Anthropic 托管 + custom 上传 | `Skills` capability + `GitRepo/LocalDir` 在 sandbox 内物化 |
| **Secrets** | `Vault`（per-user 独立资源） | `Manifest.environment` + provider-native secret stores |
| **多模型** | ❌ 仅 Anthropic | ✅ LiteLLM / any-llm / 100+ |
| **执行哲学** | "你给 spec，云端给你 SSE 流" | "你写 Runner.run()，循环在你进程里" |

### 关键洞察 3 条

1. **OpenAI SDK 的 Manifest 比 Anthropic Environment 厚得多**：能挂 GitRepo / S3 / LocalDir、能定义 OS users/groups —— Anthropic Environment 完全没有。
2. **Anthropic 的 Vault 比 OpenAI 的 secrets 重**：独立 per-user 资源 vs. manifest.environment + provider native。
3. **服务端 vs 客户端循环是根本分歧**：conductor 端实现两个 runtime 工程量**不对称** —— Anthropic 已 90% 现成，OpenAI SDK 全新建（~2000 行 Python）。

---

## 3. 字段重叠度评估

中间格式不是简单字段映射，而是**领域语义层**抽象：

| 字段 | Anthropic | OpenAI SDK | 中间格式难度 |
|-----|-----------|-----------|-----------|
| name / description / metadata | ✅ | ✅ | 🟢 直接映射 |
| model 选择 | ✅ | ✅ | 🟡 模型 ID 不同（claude-opus-4-6 vs gpt-5.4），需引入 `model_class` 语义层 |
| system prompt / instructions | ✅ | ✅ | 🟢 直接映射 |
| MCP servers | ✅ | ✅ | 🟢 都遵循 MCP 标准协议 |
| 内置工具启用 | toolset 选 8 个 | Capabilities 装 5 类 | 🟡 工具命名不同（bash vs Shell），但语义对齐 |
| Tool permission | always_allow / always_ask | guardrails / approvals 函数 | 🟡 转换可行但 OpenAI 端要写 boilerplate |
| Custom tools | 客户端执行 + event 回调 | `function_tool` 装饰器同步执行 | 🟠 语义可对齐（name + schema），实现机制需 adapter 各自适配 |
| Packages 列表 | `config.packages.{pip,npm,...}` | Manifest.environment 不直接管包，靠 image + 启动脚本 | 🟠 OpenAI 端要把 packages 转成 manifest setup 命令 |
| Networking | unrestricted/limited + allowed_hosts | provider-specific（每个 SandboxClient 各管） | 🟠 OpenAI 端实现因 provider 而异 |
| Skills | anthropic id / custom upload | Skills capability + GitRepo/LocalDir | 🟠 加载机制完全不同 |
| Vault / secrets | 独立 Vault 资源 | Manifest.environment + provider | 🟠 抽象层只能描述"需要哪些 secrets"，由 adapter 决定怎么注入 |
| 长任务 / 自动压缩 | 服务端自动 | Compaction capability + RunState 序列化 | 🟠 完全不同的恢复机制，可能不强行抽象 |

**总览**：~70% 字段语义可对齐 / ~25% 需 adapter 翻译 / ~5% 是某一边独有需 escape hatch。

---

## 4. 中间格式设计原则

1. **对齐"工程师设计 agent 时关心的领域语义"**，不是任何一边 API 的字段映射
2. **字段命名体现意图而非实现**（如 `model_class: high_reasoning` 比 `model: gpt-5.4` 更适合中间格式）
3. **Harness 特有特性不强行抽象**，留 escape hatch（`harness_specific:` block）
4. **转换是 lossy-aware 的**：adapter 必须能告诉用户 "我把你的 X 字段降级处理了，因为 OpenAI SDK 没有对应能力"
5. **`apiVersion: askdao.ai/v1` 从一开始就埋好**：未来加 harness（LangGraph / Vercel OA）走 v2 schema

---

## 5. v0.2 vs v0.3 yaml 顶层结构对比

### v0.2 结构

```yaml
metadata
agent:                          # 字段名暗示 Anthropic Agent 资源
  model: { id: claude-opus-4-6, speed: standard }
  system: ...
  tools:
    - type: agent_toolset_20260401   # ← Anthropic 专用 type 字面值
      configs: [...]                  # ← Anthropic 专用结构
    - type: mcp_toolset
      mcp_server_name: github
  mcp_servers: [...]
  skills: [...]
environment:                     # 字段名暗示 Anthropic Environment 资源
  config:
    type: cloud                  # ← Anthropic 专用枚举
    packages: { pip, apt, ... }
    networking: { type: limited, ... }
vault_hints: [...]
```

**问题**：每个块名 + 多个字段都是 Anthropic SDK 的 1:1 字段名/枚举值。开源出去工程师一看就知道这是为 Anthropic 写的。

### v0.3 结构

```yaml
apiVersion: askdao.ai/v1         # ← 自有版本
kind: AgentSpec                  # ← 自有 kind
metadata: ...

persona:                         # ← 语义层（不是 "agent"）
  model_class: high_reasoning    # ← 语义化
  model_preferences:             # ← 多模型偏好序列
    - { provider: anthropic, id: claude-opus-4-6, speed: standard }
    - { provider: openai, id: gpt-5.4 }
  system_prompt: ...

capabilities:                    # ← 语义化能力（不是 "tools"）
  shell: { enabled, permission: ask_for_dangerous }
  filesystem: { enabled, permission, scopes }
  web: { enabled, permission }
  code_execution: { enabled, permission }

mcp_servers: [...]               # ← 标准 MCP，两边直通
custom_tools: [...]              # ← schema 中性

skills:                          # ← 多源
  - { type: builtin, provider: anthropic, id: xlsx }
  - { type: custom_local, path: ... }
  - { type: git_repo, repo: ..., ref: ... }

workspace:                       # ← 替代 "environment"，更广义
  packages: { pip, apt, ... }
  mounts: [...]                  # ← OpenAI SDK 用，Anthropic adapter 跳过
  networking: { mode, allowed_hosts }
  environment_vars: { ... }

vault_hints: [...]               # ← 不变

preferred_harness: anthropic_managed_agents   # ← Phase 1 单选
fallback_harnesses: []                        # ← Phase 2 启用

harness_specific:                # ← escape hatch
  anthropic: { fast_mode, callable_agents, ... }
  openai: { sandbox_provider, compaction, ... }
```

**改进**：
- 顶层 8 个块名全 harness-neutral
- 每个内部字段尽量用语义化命名
- harness 独有特性集中到 `harness_specific:` block
- `apiVersion + kind` 让中间格式自带版本演化能力

---

## 6. 三阶段路线图

### Phase 1 · v0.3 现在做（不阻塞 askdao-cli MVP）

**askdao-cli 端**：
- yaml 改为中间格式风格（顶层 + 字段命名）
- 但 `preferred_harness` 仅 `anthropic_managed_agents` 单选
- detection.json 加 `detected_harness_signals`（探查用户机器有没有 claude code / codex / cursor 等已装）
- 工程量：在 v0.2 基础上 ~1 周

**conductor 端**：
- 实现 AnthropicAdapter（中间格式 → Anthropic 三资源 API）
- 不动现有 ManagedAgentsClient，加一层翻译
- 工程量：~1 周

**总 Phase 1 增量**：~2 周（vs v0.2 增量是 0 周，因为本来就要做 conductor 端 deploy 流程）

### Phase 2 · 开源前必做（OpenAI SDK adapter）

**conductor 端**：
- 新增 `app/openai_sdk/` 模块（~2000 行 Python）：
  - `client.py` — 包装 `Runner.run_streamed()`，对齐现有 SSE 输出格式（~400 行）
  - `session.py` — 实现 `Session` Protocol 对接 OpenViking MemoryProvider（~200 行）
  - `sandbox_adapter.py` — 把 Manifest 落到 E2B / Docker / UnixLocal（~300 行）
  - `chat_handler_openai.py` — 第二条流式管道（~400 行）
  - `artifact_sweeper_openai.py` — 文件系统型 artifact 回收（~300 行）
  - 测试 + 集成（~400 行）
- alembic 加 `agent_spec.runtime_id: text` 列（默认 `anthropic_managed_agents`）
- chat.py 三岔（managed-agents / openai-sdk / sandbox-template）

**askdao-cli 端**：
- yaml 的 `preferred_harness` 加 `openai_agents_sdk` 选项
- `askdao agent deploy --harness <id>` CLI 参数
- 工程量：~1 周

**总 Phase 2 工作量**：~4-6 周（conductor 端是大头）

### Phase 3 · 中长期

- 加更多 harness（LangGraph / Vercel OA / 本地 Ollama 等）
- 中间格式演化到 v2（兼容 v1 但能表达更多语义）

---

## 7. 风险与缓解

### 风险 1 · 抽象层泄露
中间格式定义太细（保留每边独有特性），变成"两套 schema 的并集"。  
**缓解**：抽象停在领域语义；独有特性进 `harness_specific:` 块，且只对高级 KOL 暴露。

### 风险 2 · OpenAI SDK 路径还没跑通
askdao-cli MVP 不能等 OpenAI SDK 路径上线。  
**缓解**：Phase 1 单 adapter 解耦，先发 askdao-cli。

### 风险 3 · 翻译损失（lossy translation）
某些字段单边独有 → adapter 转换时丢失。  
**缓解**：adapter 输出 "translation report"，明确告诉 KOL 哪些字段被降级。

### 风险 4 · 中间格式版本演化
未来加 harness，中间格式要扩。  
**缓解**：`apiVersion: askdao.ai/v1` 从一开始就埋好。

---

## 8. v0.3 设计原则确认（哥已同意）

✅ **中间格式字段命名风格**：
- 顶层 8 块：`metadata / persona / capabilities / mcp_servers / custom_tools / skills / workspace / vault_hints`
- 加 `apiVersion: askdao.ai/v1` + `kind: AgentSpec`
- escape hatch：`harness_specific: { anthropic, openai }`

✅ **Phase 1 / Phase 2 切分**：
- Phase 1 = 中间格式 + AnthropicAdapter（不阻塞 askdao-cli MVP）
- Phase 2 = OpenAIAdapter（开源前必做）
- Phase 3 = 更多 harness（中长期）

---

## 9. 修订动作清单（落进 design.md）

- [x] 顶部 ChangeLog 加 v0.3 章节
- [x] §1 动机加"开源属性 + harness-neutral"动机
- [x] §2 系统架构图加 conductor adapter 层（L4 输出中间格式 → adapter → harness API）
- [x] §3 命令骨架：`deploy --harness` 参数；输出文案
- [x] §4 detection.json 加 `detected_harness_signals` 字段
- [x] §5 yaml schema 完全重写为中间格式（顶层 8 块）
- [x] §6 工程量分两侧（askdao-cli + conductor），加 Phase 2 估算
- [x] §7 三阶段（Phase 1 / 2 / 3）
- [x] §8 与 plan/06 关系：明确 alembic 加 `runtime_id` 列（Phase 2）
- [x] §9 决策记录加 9.5（中间格式选择）+ 9.6（分阶段切分）
- [x] §10 落地路径分阶段更新

---

## 10. 决策记录

### 9.5 yaml 输出格式：harness-neutral 中间格式 ✅ 已定（v0.3）

- **决定**：yaml 顶层 8 块 + 字段命名 harness-neutral；`apiVersion: askdao.ai/v1` 从一开始埋好；harness 独有特性进 `harness_specific:` escape hatch
- **理由**：askdao-cli 是开源项目，yaml 字段不应暗示单一 harness；当前选 Anthropic 是实施权宜，未来要支持 OpenAI Codex / 更多 harness
- **影响**：design.md §5 完全重写

### 9.6 多 harness 支持分三阶段 ✅ 已定（v0.3）

- **Phase 1**（即将做）：中间格式 + AnthropicAdapter，单 harness 选项
- **Phase 2**（开源前必做）：OpenAIAdapter + chat.py 三岔 + alembic 加 `runtime_id` 列
- **Phase 3**（中长期）：更多 harness
- **理由**：Phase 1 不阻塞 askdao-cli MVP；Phase 2 给开源前留足时间；Phase 3 是演化空间
- **影响**：design.md §6 工程量 + §7 范围分两阶段；conductor 端 plan/03 需配套修订（M3+ 新增 OpenAI runtime 章节）
