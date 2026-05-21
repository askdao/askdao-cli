# askdao-cli Observe 层：运行时观测驱动的 Agent 环境规格生成

> ⚠️ **2026-05-21 立足点纠正（务必先读）**：本稿在不了解代码 + 误判场景为通用 Code App 下提出。评审后锁定真实立足点 = **Skills Pipeline + Anthropic Managed Agents MVP**。**四条观测路径（import 解析 / snapshot / tracer 注入 / 包安装命令解析）+ §6 重造 schema 均作废**；实际吸收的只有「本地 Web 工作台 + 字段级 provenance + skill 相关性筛选」，且 observe 从"推断包依赖"重定向为"观测实际激活的 skill/MCP"。纠正详情 + v0.8 落地见 [`review-observe-pivot-2026-05-21.md`](./review-observe-pivot-2026-05-21.md)。下文 §三~§六的包推断设计**仅作历史参考**。

> **文档性质**：多轮讨论汇总 + 技术方案 + 实施计划
> **快照时间**：2026-05-20
> **前置文档**：askdao-cli-overview.md（v0.7.1, 13K+ 行 Go）
> **参考调研**：
> - 深度研报 A [`investigations/Runtime Observability and Session Hooking for AI Coding Agents_ An Engineering Assessment for askdao-cli.md`](./investigations/Runtime Observability and Session Hooking for AI Coding Agents_ An Engineering Assessment for askdao-cli.md)
> - 调研报告 B [`investigations/Open Source and Native Instrumentation for Agent Session Observability.md`](./investigations/Open Source and Native Instrumentation for Agent Session Observability.md)
> - 调研报告 C《面向 Claude Code 与 Codex 的 Agent Session 审计与观测研究报告》[`investigations/面向 Claude Code 与 Codex 的 Agent Session 审计与观测研究报告.md`](./investigations/面向 Claude Code 与 Codex 的 Agent Session 审计与观测研究报告.md)
> - Anthropic Managed Agents API 文档（harness-design/claude-managed-agents-docs/docs 目录下的 agent-setup.md / environments.md / cloud-containers.md / mcp-connector.md）
> - OpenAI Agents SDK 文档（harness-design/openai-agents-sdk-docs目录下的 zh-CN-full.md，覆盖 01-12 章）

---

## 一、核心洞察：从"猜测 Agent 需要什么"到"记录 Agent 实际用了什么"

### 1.1 问题根源

askdao-cli 的现有 4 层静态流水线（syft 扫依赖 → dev_filter → nixpacks 式框架推断 → LLM 推荐）在 7 个版本的迭代中反复遭遇同一个根本困难：**静态扫描无法可靠推断完整的运行时环境**。

典型的失败模式：

- `psycopg2` 出现在 `requirements.txt` 里，但 syft 扫不出它依赖 `libpq-dev` 系统包
- `Dockerfile` 里的多阶段构建、USER、EXPOSE 等复杂写法需要逐版本扩展 parser
- lockfile 里有 200 个包，其中 160 个是 dev 依赖，过滤规则每个框架都不一样
- KOL 的项目可能根本没有 lockfile，只有一堆 `.py` 文件和零散的 `import`

每一次修正都是"把一个假设替换成一个事实"（overview.md §五 原话），但假设的长尾是无穷的。

### 1.2 思路转变

新方案不再猜测，而是**观测**：让 AI Agent（Claude Code 或 Codex CLI）在真实环境中跑一遍项目，通过 hooks 记录它实际执行了什么命令、加载了哪些包、修改了哪些文件，然后从观测数据中生成 `askdao-agent.yml`。

这和传统 APM / observability 领域的思路一致——不看配置文件里"声明"了什么，看运行时"发生"了什么。

### 1.3 关键发现：Observe → Spec 是一个空白地带

三份独立的调研报告从不同路径得出同一结论：

> **所有现有开源项目停留在"捕获并展示数据"（dashboard / replay / trace viewer），没有人把 Agent 的观测数据翻译成部署规格（Dockerfile / requirements.txt / askdao-agent.yml）。**

最接近的精神先驱是 **SlimToolkit/DockerSlim**（CNCF Sandbox, 20K+ stars）——它观测容器进程树的运行时行为，生成精简镜像 + 逆向工程的 Dockerfile。但它的观测对象是容器，不是 AI Agent session。

这意味着 askdao-cli 做的是一个真正的创新。核心逻辑必须自己写，不能依赖任何现有项目的代码。

### 1.4 三层可见性框架（来自报告 C 的核心分析模型）

报告 C 将"为什么纯扫描不行"分解为三层递进的可见性 gap，清晰解释了每一层观测手段存在的必要性：

| 层 | 要回答的问题 | 纯扫描 | hooks / OTel | 需要的补充 |
|---|---|---|---|---|
| **上下文层** | 这次任务引发了哪些 tool/API/permission 流程 | 弱 | **强** | — |
| **执行层** | Bash 里实际跑了什么命令，结果是什么 | 弱 | **强**（PreToolUse/PostToolUse） | — |
| **运行时内部层** | python 进程内 import 了哪些模块、拉起了哪些子进程 | 弱 | **弱到中** | **必须补 runtime instrumentation** |

这三层是递进关系，每一层回答的问题不同，不能互相替代。askdao-cli 的四条观测路径（§三）恰好覆盖了这三层：路径 ①④ 覆盖执行层，路径 ② 覆盖上下文层（环境基线），路径 ③ 覆盖运行时内部层。

---

## 二、开源生态调研结论

### 2.1 项目成熟度分级

经过两份报告的交叉验证，将调研到的项目按成熟度分为三级：

**第一级：生产级基础设施（可作为架构依赖）**

| 项目 | Stars | 价值 |
|------|-------|------|
| Claude Code 官方 hooks 机制 | N/A（Anthropic 维护） | askdao-cli observe 层的底层原语，29 个 lifecycle events |
| OpenTelemetry Collector Contrib | 30K+ | 遥测管道标准，协议转换 / 脱敏 / 路由 |
| SigNoz | 20K+ | OTEL 原生、ClickHouse 后端、可自托管的一体化 observability |
| Langfuse | 19K+ | LLM trace 检视 / eval / prompt 管理 |
| SlimToolkit | 20K+ | "observe runtime → emit spec" 的架构思路参考 |

**第二级：有价值的代码参考（学习材料，不作为架构依赖）**

| 项目 | 看什么 |
|------|--------|
| TechNickAI/claude_telemetry | 最薄的 Claude Code SDK hook 包装方式；适合借鉴 headless/CI 场景的观测打包 |
| DazzleML/claude-session-logger | 最小化 hook 脚本写法（`log-command.py`） |
| pydantic/claude-code-logfire-plugin | transcript JSONL 解析 + OTLP 发送模式；学习 Claude Code 插件化打包与 trace 映射 |
| Siddhant-K-code/agent-trace | "strace for AI agents"；回放、审计、行为漂移分析的 UX 参考；event model 和 replay 设计值得研究 |
| liaohch3/claude-tap | 跨 Agent（Claude/Codex/Gemini/Cursor 等 9 种 CLI）的 proxy 捕获方案；最适合做"API 面可见 vs 本地执行面可见"的边界对照 |
| waynesutton/codex-sync-plugin | Codex 侧 `~/.codex/sessions/*.jsonl` 解析参考 |

> **两个项目需特别注意**（来自报告 C 的补充评估）：
> - **disler/claude-code-hooks-multi-agent-observability**：架构参考价值高（`Hook Scripts → HTTP POST → Bun Server → SQLite → WebSocket → Vue Client`），但仓库**未声明开源许可证**（有公开 issue 要求补充），且存在 hook 解析错误阻断 Bash 的已知 bug。只做思路参考，不纳入产品依赖。
> - **badlogic/lemmy/apps/claude-trace**：能看到 Claude Code 隐藏的 system prompt 和 raw API 数据，但实现方式是注入 `fetch()` 拦截器——Claude Code 切到 native binary 安装后已出现失效 issue。适合研究/逆向观察，不适合做稳定底座。

**第三级：过于早期，不宜作为重要参考**

| 项目 | 原因 |
|------|------|
| tobilg/ai-observer | ~209 stars，刚发布，个人项目 |
| Mizune/llm-cli-telemetry | 极少 stars，非常早期 |

> **注意**：报告 B 将 AI Observer 和 llm-cli-telemetry 列为"最强直接答案"，报告 C 将 AI Observer 列为"最适合作为统一本地观测后端"。经验证，这两个项目的 GitHub stars 极少、成熟度不足，不宜作为 askdao-cli 的架构支柱。它们的技术思路可以浏览，但不应该承担参考原型的角色。askdao-cli 的 observe 层不需要通用 observability 后端——它自己就是 spec 生成器，数据只在本地临时存在。

### 2.2 Claude Code vs Codex CLI：原生能力对比

两份报告高度一致的结论：**Claude Code 的 hook 覆盖面远优于 Codex CLI。**

| 维度 | Claude Code | Codex CLI |
|------|-------------|-----------|
| Hook 事件数 | 29 个（SessionStart 到 SessionEnd 全链路） | 6 个公开事件 |
| Bash 命令捕获 | `PreToolUse(Bash)` 拿到完整 command + `PostToolUse` 拿到 exit_code 和 output | 可以，但 `unified_exec` 新路径未被完全覆盖 |
| 文件操作捕获 | `PostToolUse(Write\|Edit\|MultiEdit)` 拿到 file_path 和内容 | 部分支持 |
| Hook transport | HTTP type（天然适配 Go HTTP server）+ command type | 仅 command type（需要 shim 中转） |
| OTEL 数据 | 有 tool_details flag，可导出 bash command、file path、MCP tool name | 故意不包含命令内容，只有 tool name |
| 已知 gap | 无重大 gap | `read_file`/`grep`/`web_search` 等不发 hook events；`codex exec`/`codex mcp-server` 曾有 OTEL gap |

**决策：Phase 1 聚焦 Claude Code，Codex CLI 作为 Phase 2。** 不仅因为 Claude Code hook 更丰富，还因为 HTTP hook type 天然适配 askdao-cli 的 Go 架构。

> **⚠ Codex `updatedInput` 争议（三份报告存在矛盾）**：报告 A 查到 Codex 的 `output_parser.rs` 返回 "PreToolUse hook returned unsupported updatedInput"（GitHub issue #18491 也在请求此功能）。报告 C 声称 Codex 的 PreToolUse 已经支持 `updatedInput` 改写。两者不可能同时为真——可能源于不同版本的差异，也可能是报告 C 将"已解析但跳过"误判为"已支持"。**Phase 2 开始前必须在目标 Codex 版本上实测验证。** 如果 `updatedInput` 确实可用，Codex 侧的 tracer 注入就不需要 command shim 中转。

### 2.3 原生能力能否单独解决问题？

三份报告的共同回答：**不能。**

Claude Code 的 hooks + OTEL 能捕获到 tool 调用、bash 命令、文件操作、permission 决策、MCP 活动。但它们**不会**自动产出：

- 包清单（哪些包被实际加载了）
- import 依赖图（哪些 import 被执行了）
- 完整的执行环境快照（环境变量、系统包版本、toolchain 版本）

这些必须通过 hook 脚本、tracer 注入或 OS 级 instrumentation 补充。这也正是 askdao-cli observe 层要做的事情。

> **报告 C 的精确表述**："官方只把可见性定义到 prompt / tool / API / hook / subprocess trace context 这一层，并没有任何进程内 module/import 级别的语义。Claude Code 原生可以很好地审计'工具边界'，但不能原生审计'工具内部进程究竟 import 了什么包、又拉起了什么隐藏子进程'。"

### 2.4 双 Harness 对照：为什么中间格式不能偏向任何一方

askdao-cli 的设计哲学（overview.md §2.4）明确要求 harness-neutral 中间格式。对照 Anthropic Managed Agents API 和 OpenAI Agents SDK 的实际接口，两者的抽象差异非常大：

| 概念 | Anthropic Managed Agents | OpenAI Agents SDK (Sandbox) |
|------|--------------------------|----------------------------|
| 系统 prompt 字段名 | `agent.system` | `agent.instructions` |
| 运行时隔离 | 固定 Ubuntu 22.04 云容器，不可选 base image | **多种 Provider**：E2B、Docker、Cloudflare、Modal、Vercel、Unix-local 等，可选 image |
| 包预装机制 | `environment.config.packages.{apt,pip,npm,...}` 声明式预装 | **无预装概念**。通过 `Shell` capability 在运行时安装 |
| 环境变量 | 不在 Environment API 里，走 Vault 资源 | `Manifest.environment`（一等公民，直接声明） |
| 端口暴露 | 无端口概念（只有 unrestricted/limited 网络模式） | Sandbox 文档明确支持端口暴露 |
| Agent + 运行环境的关系 | **两个独立 API 资源**（`/v1/agents` + `/v1/environments`） | **一体化**：`SandboxAgent` 包含 `default_manifest` |
| 文件注入 | Agent toolset 自带文件操作 | `Manifest.entries`：File、Dir、LocalFile、GitRepo、S3Mount 等 |
| 多 Agent | `callable_agents` | `handoffs` + Agent-as-Tool |
| 网络控制 | `networking: {type: unrestricted\|limited, allowed_hosts: [...]}` | Provider 级别配置 |

**关键推论**：

- **`base_image` 在 Anthropic 侧无用（固定 Ubuntu 22.04），在 OpenAI 侧有用**（Docker Provider 可选 image，E2B 有 template）。保留。
- **`env_vars` 在 Anthropic 侧走 Vault，在 OpenAI 侧走 Manifest.environment**。保留，Adapter 各自翻译。
- **`exposed_ports` 在 Anthropic 侧无对应，在 OpenAI 侧是 Sandbox 能力**。保留，Anthropic Adapter 忽略。
- **`packages` 在 Anthropic 侧是声明式预装，在 OpenAI 侧需转换为 Shell 安装脚本**。中间格式只描述"需要什么包"，不规定"怎么装"。
- **YAML 不应拆成 Anthropic 的 `agent` + `environment` 两块**——这是 Anthropic 特有的资源分离，OpenAI 的 `SandboxAgent` 是一体化的。

> **设计原则：中间格式描述语义意图（Agent 需要什么），Adapter 负责翻译成目标 harness 的具体 API 调用。** 同一份 `askdao-agent.yml`，AnthropicAdapter 拆成两个 API 资源，OpenAIAdapter 合并进一个 SandboxAgent。

---

## 三、四条观测路径与交叉校验

### 3.1 路径全景

讨论中明确了四条互补的观测路径，各有覆盖面和盲区：

**路径 ①：Write/Edit hooks → 源文件 import 静态解析**

- 机制：Claude Code 的 `PostToolUse(Write|Edit|Read)` hooks 捕获到 Agent 写/改的源文件内容，对其做 import 解析
- Python 用 `ast.parse` → `Import`/`ImportFrom` 节点，过滤 stdlib，剩余即第三方依赖
- Node 用 regex 匹配 `require('x')` / `import x from 'x'`
- Go 用 regex 匹配 `import "x"` 或 `go/parser`
- 覆盖：Agent 写过或编辑过的文件中的依赖
- 盲区：Agent 没动过的已有文件中的依赖
- 成本：几乎为零（数据已经通过 hooks 拿到了，只需加一层解析）

**路径 ②：SessionStart/End 全量快照 + diff**

- 机制：在 `SessionStart` hook 时执行 `pip freeze` / `npm ls --json` / `go list -m all` / `dpkg -l` 等命令，记录完整的环境状态；在 `SessionEnd` 时再跑一次，diff 出本次 session 新增的包
- 覆盖：session 期间新安装的所有包（不管通过什么命令装的）
- 盲区：环境里早就存在的包——diff 只看增量
- 附加价值：SessionStart 的全量快照可以与路径 ① 的 import 解析交叉验证。例如 import 解析发现 `flask`，全量快照确认 `flask==3.0.2` 已安装 → 高置信度依赖记录
- 成本：一次性命令，极低

**路径 ③：Import tracer 注入（ground truth）**

- 机制：在 `PreToolUse(Bash)` hook 中，当检测到命令是 `python ...` 时，注入 `PYTHONPATH=.askdao:$PYTHONPATH`，使 Python 进程启动时加载一个 `sitecustomize.py`。该脚本通过 `atexit.register` 在进程退出时 dump `sys.modules` 中所有来自 `site-packages` 的模块名和版本到 JSONL 文件
- Node.js 等价方案：`NODE_OPTIONS='--require .askdao/trace-require.js'`，monkey-patch `Module._load` 记录所有 require 路径
- Go：编译型语言，import 在编译时确定，用 `go list -deps` 解析即可
- 覆盖：进程**实际加载**的每一个第三方模块（runtime ground truth）
- 盲区：只覆盖被执行的代码路径。如果某个 import 在未触发的 if 分支里，不会被记录
- 成本：需要在 PreToolUse 中改写命令（加环境变量前缀），轻微介入 Agent 执行

**路径 ④：Command parser（bash 命令解析）**

- 机制：解析 `PreToolUse(Bash)` 中的 `tool_input.command`，用正则匹配 `apt-get install`、`pip install`、`npm install` 等包安装命令
- 同时解析 `PostToolUse(Bash)` 中的 `exit_code` + `output`，通过错误信息反向推断系统依赖（如 `pg_config executable not found` → 需要 `libpq-dev`）
- 覆盖：session 中执行的所有包安装命令
- 盲区：非标准安装路径（`curl | bash`、手动 `cp` 等）；以前就装好的包
- 成本：极低

### 3.2 交叉校验矩阵

```
① + ② = "源码声明了 flask" + "环境有 flask==3.0.2"  → 高置信度依赖
③ alone = "进程真的 import 了 flask"                  → ground truth
② + ④  = "session 前没有 libpq-dev" + "agent 装了它"  → 系统包依赖
① + ③  = "源码声明了 numpy" + "进程确实加载了 numpy"    → 双重确认

置信度排序：③ > (①+②) > ④ > ② alone
```

### 3.3 关于路径 ③ 的关键问题与决策

**问题：Import tracer 注入是否始终影响 Agent 执行？**

**不是。** Claude Code 的 hooks 配置在 `.claude/settings.json`，在 session 启动时快照读取。askdao-cli 只在 `askdao observe` 时写入临时 hook 配置，session 结束后清理。正常使用 Claude Code 时没有任何 askdao hooks 存在，零影响。

**问题：这种注入做法是否常见？**

**在 Agent 领域无先例，但在传统 observability 领域是标准做法。** 行业术语叫 "auto-instrumentation"。成熟先例包括：

| 工具 | 注入方式 | 使用场景 |
|------|----------|----------|
| OpenTelemetry Python | `sitecustomize.py` 或 `opentelemetry-instrument` 命令 | 生产环境 APM |
| Datadog APM | `ddtrace-run python app.py` | 生产环境 |
| New Relic | `newrelic-admin run-program python app.py` | 生产环境 |
| Node.js `--require` | `node --require ./tracer.js app.js` | 生产环境 |
| Java `-javaagent` | `java -javaagent:otel.jar -jar app.jar` | 生产环境 |

这些工具全部跑在**生产环境**，比 askdao-cli 的"本地观测一次"场景要求严苛得多。

**问题：调研的项目里有人这么做吗？**

**没有。** 两份报告覆盖的所有项目都停留在"被动记录 Agent 发了什么命令"。没有任何项目追踪 Agent 启动的子进程里实际加载了哪些包。

**决策：路径 ③ 一次到位，不分阶段。** 理由：侵入性只在 `askdao observe` 期间存在，采用的是工业界成熟的 auto-instrumentation 标准做法，对 Agent 行为无影响，KOL 群体（工程师）对此概念不陌生。

---

## 四、跨平台问题与解决方案

### 4.1 问题识别

KOL 可能在 macOS、Windows 或 Linux 上运行 Claude Code / Codex CLI。这带来五个层面的问题。

**问题 1（最严重）：观测环境 ≠ 部署环境**

KOL 在 macOS 上跑 `askdao observe`，Agent 使用 macOS 的包管理器（`brew install libpq`）。但 `askdao-agent.yml` 的部署目标是 Linux（Managed Agents / E2B），需要的是 `apt-get install -y libpq-dev`。Windows 上类似，Agent 可能用 `choco install`、`winget install` 等。

如果 command parser 只处理 `apt-get`，在 macOS 和 Windows 上就完全失效。

**问题 2：快照命令不通用**

`dpkg -l`（Linux only）/ `brew list`（macOS only）/ `choco list`（Windows only）。

**问题 3：Agent 在不同 OS 上的行为差异**

同一个项目，Claude Code 在不同平台上可能使用不同的命令语法（bash vs PowerShell）、不同的路径分隔符、不同的环境激活方式。

**问题 4：sitecustomize.py 注入路径**

环境变量注入语法在 bash / PowerShell / CMD 之间不同。

**问题 5：localhost 绑定**

IPv6 优先的环境下 `127.0.0.1` 可能 ECONNREFUSED（overview.md 已记录过此坑）。

### 4.2 解决方案

**关键洞察：语言级包管理器是跨平台一致的，系统级包才有映射问题。**

`pip install flask`、`npm install express`、`cargo add serde` 在所有平台上完全一样。askdao-cli 的四条观测路径中，路径 ①②③ 都只涉及语言级包——它们天然跨平台。只有路径 ④ 的系统包部分有问题。

**采用策略：语言包 → 系统包反向推断（策略 C + A fallback）**

不去观测 `brew install` / `choco install` / `apt-get install`，而是从语言包反向推断系统依赖：

```
观测到 pip install psycopg2    →  已知需要 apt: libpq-dev
观测到 npm install sharp        →  已知需要 apt: libvips-dev
观测到 pip install Pillow       →  已知需要 apt: libjpeg-dev, zlib1g-dev
观测到 import cv2（via tracer） →  已知需要 apt: libgl1-mesa-glx
```

这张"语言包 → 系统包"的映射表是确定性的、跨平台的、可以硬编码的。askdao-cli 完全不需要关心 KOL 用什么操作系统——因为 `pip install psycopg2` 在所有平台上长一样，而它需要 `libpq-dev` 这件事也是确定的。

对于映射表覆盖不到的 edge case，交给 L4 LLM 层，带上 `target_os: ubuntu-22.04` 上下文做翻译。

**快照命令的跨平台适配**：

```go
switch runtime.GOOS {
case "darwin":
    systemPkgCmd = "brew list --formula"
case "linux":
    systemPkgCmd = "dpkg -l"
case "windows":
    systemPkgCmd = "choco list --local-only"
}
// 语言级快照命令跨平台一致，无需 switch
pipFreezeCmd = "pip freeze"
npmListCmd   = "npm ls --json --depth=0"
goListCmd    = "go list -m all"
```

**tracer 注入的跨平台适配**：

```go
// PreToolUse hook 中改写命令时，按 OS 选择注入语法
switch runtime.GOOS {
case "windows":
    // PowerShell 语法
    rewritten = fmt.Sprintf(`$env:PYTHONPATH="%s;$env:PYTHONPATH"; %s`, tracerDir, originalCmd)
default:
    // bash 语法
    rewritten = fmt.Sprintf(`PYTHONPATH=%s:$PYTHONPATH %s`, tracerDir, originalCmd)
}
```

**localhost 绑定**：统一绑 `127.0.0.1`（IPv4 确定性），不使用 `localhost`。

---

## 五、技术架构

### 5.1 整体流程

```
askdao-cli observe ./myapp [--agent claude|codex]
│
├─ 1. 检测 OS + 已安装的语言运行时
│
├─ 2. 生成 hook 配置
│     ├─ Claude: 写入 .claude/settings.local.json（HTTP hooks → localhost:PORT）
│     └─ Codex:  写入 .codex/hooks.toml（command hooks → askdao-hook-shim）
│
├─ 3. 写入 tracer 脚本（从 go:embed 释放）
│     ├─ .askdao/sitecustomize.py     （Python import tracer）
│     ├─ .askdao/trace-require.js     （Node.js import tracer）
│     └─ .askdao/askdao-hook-shim     （仅 Codex 需要，command → HTTP 中转）
│
├─ 4. 启动 Go HTTP Recorder Server（localhost:PORT）
│
├─ 5. 执行 SessionStart 全量快照
│     ├─ pip freeze / npm ls --json / go list -m all
│     ├─ 语言运行时版本（python --version / node --version / go version）
│     ├─ git rev-parse HEAD + branch + dirty state
│     └─ 白名单环境变量（PATH, VIRTUAL_ENV, CONDA_PREFIX 等，NEVER 值，只要名字）
│
├─ 6. exec claude（或 codex），附带引导 prompt：
│     "Set up this project for production, install everything it needs,
│      run the dev server, and verify it starts."
│
│     Agent 运行期间，Recorder 持续接收 hook events：
│     ├─ PreToolUse(Bash)   → command parser（解析 install 命令）
│     │                     → tracer 注入（检测到 python/node 命令时加 env var 前缀）
│     ├─ PostToolUse(Bash)  → exit code + error message → 反向依赖推断
│     ├─ PostToolUse(Write|Edit) → 源文件 import 静态解析
│     └─ 所有事件            → 写入 .askdao/observations.jsonl
│
├─ 7. Session 结束，执行 SessionEnd 全量快照，与 SessionStart diff
│
├─ 8. 收集 tracer 输出（.askdao/imports-*.jsonl）
│
├─ 9. 合并器（merger）
│     ├─ static scan（现有 L1-L3 流水线）       → source: static
│     ├─ import tracer 输出                      → source: tracer    [最高置信度]
│     ├─ 源文件 import 解析 + 全量快照交叉       → source: observed
│     ├─ snapshot diff（新增包）                  → source: snapshot
│     ├─ command parser（install 命令）           → source: command
│     ├─ 语言包 → 系统包反向映射表               → source: inferred
│     └─ LLM 推荐（L4，处理 edge case）          → source: llm
│
├─ 10. 本地审阅 UI（HTTP server 角色切换）
│     ├─ server 从 "hook receiver" 切换为 "review UI server"
│     ├─ 自动打开浏览器 http://127.0.0.1:PORT/review
│     ├─ 用户在页面上审阅 / 修改 / 确认 spec
│     └─ POST /api/spec/confirm → 触发生成
│
├─ 11. 生成 askdao-agent.yml（每个字段带 provenance 标记）
│
└─ 12. 清理：删除 .askdao/ 临时目录、hook 配置、tracer 脚本、关闭 server
```

### 5.2 Go 模块结构

```
askdao-cli (Go binary)
│
├─ cmd/observe.go               ← 入口：orchestrate 整个 observe 流程
│
├─ pkg/hookserver/              ← Go HTTP server，接收 Claude Code POST 的 JSON
│   ├─ server.go                ← net/http server，绑 127.0.0.1:random-port
│   ├─ handlers.go              ← /event/pre, /event/post, /event/end 路由
│   ├─ review.go                ← /review, /api/spec, /api/spec/confirm 路由（session 结束后启用）
│   └─ types.go                 ← Claude Code hook event JSON schema 定义
│
├─ pkg/hookconfig/              ← 生成 .claude/settings.local.json 或 .codex/hooks.toml
│   ├─ claude.go
│   └─ codex.go
│
├─ pkg/parser/                  ← 解析 bash 命令中的包安装指令
│   ├─ apt.go                   ← apt-get install / apt install
│   ├─ pip.go                   ← pip install / pip3 install / uv add / poetry add
│   ├─ npm.go                   ← npm install / yarn add / pnpm add / bun add
│   ├─ cargo.go                 ← cargo add
│   ├─ gomod.go                 ← go install / go get
│   └─ errors.go                ← 错误信息 → 系统包反向推断表
│
├─ pkg/importscan/              ← 解析源文件中的 import 语句
│   ├─ python.go                ← ast-level regex 或 go-tree-sitter
│   ├─ node.go                  ← require/import from
│   └─ golang.go                ← Go import blocks
│
├─ pkg/snapshot/                ← SessionStart/End 全量快照 + diff
│   ├─ capture.go               ← 执行 pip freeze / npm ls 等，解析输出
│   ├─ diff.go                  ← 两个快照 slice 做 diff
│   └─ platform.go              ← runtime.GOOS 分支：brew/dpkg/choco
│
├─ pkg/tracer/                  ← import tracer 注入逻辑
│   ├─ inject.go                ← PreToolUse 中检测 python/node 命令 → 改写
│   └─ collect.go               ← SessionEnd 时解析 tracer 输出 JSONL
│
├─ pkg/syspkg/                  ← 语言包 → 系统包反向映射表
│   └─ mapping.go               ← psycopg2 → libpq-dev, sharp → libvips-dev, ...
│                                  注意：不硬排除预装包，预装排除在 Adapter 端做
│
├─ pkg/merger/                  ← 合并所有 source → askdao-agent.yml
│   ├─ merge.go                 ← 按置信度排序 + 冲突解决
│   ├─ provenance.go            ← 生成 _provenance 块（独立于可传输字段）
│   └─ schema.go                ← harness-neutral AgentSpec 结构体定义
│                                  identity / runtime / tools / skills / mcp_servers / _provenance
│
└─ embed/
    ├─ tracers/                 ← 非 Go，go:embed 嵌入资源
    │   ├─ sitecustomize.py     ← Python import tracer（~30 行）
    │   ├─ trace-require.js     ← Node.js import tracer（~30 行）
    │   └─ askdao-hook-shim     ← Codex command→HTTP 中转（~30 行 shell/Go）
    └─ ui/
        └─ review.html          ← 自包含审阅页面（inline CSS + JS，无外部依赖）
```

所有核心逻辑是 Go。tracer 脚本是**被观测应用的语言**——不是 askdao-cli 的运行时依赖，而是 `go:embed` 嵌入的静态资源，observe 时写到目标目录，结束后清理。

### 5.3 Claude Code Hook 配置示例

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [{
          "type": "http",
          "url": "http://127.0.0.1:7788/event/pre",
          "timeout": 10
        }]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Bash|Write|Edit|MultiEdit|Read",
        "hooks": [{
          "type": "http",
          "url": "http://127.0.0.1:7788/event/post",
          "timeout": 30
        }]
      }
    ],
    "SessionEnd": [
      {
        "hooks": [{
          "type": "http",
          "url": "http://127.0.0.1:7788/event/end"
        }]
      }
    ]
  }
}
```

关键行为：

- Hook 在 session 启动时快照读取，session 中间改配置不生效
- 非 2xx 响应 / 连接失败 / 超时 → 非阻塞，Agent 继续执行
- exit code 2 在 PreToolUse 中可阻止调用（askdao-cli 的纯观测模式不用此机制）
- HTTP hooks 只在 Claude Code 可用；Codex CLI 只支持 command type，需要 shim 中转

### 5.4 Python Import Tracer 实现

```python
# embed/tracers/sitecustomize.py
# 嵌入到 askdao-cli 二进制，observe 时释放到 .askdao/sitecustomize.py
import atexit, sys, json, os

def _dump_imports():
    mods = {}
    for name, mod in sys.modules.items():
        f = getattr(mod, '__file__', None) or ''
        if 'site-packages' in f:
            mods[name.split('.')[0]] = getattr(mod, '__version__', None)
    out = os.path.join(os.environ.get('ASKDAO_TRACE_DIR', '/tmp'), 'askdao-imports.jsonl')
    with open(out, 'a') as fh:
        fh.write(json.dumps({
            'tool_use_id': os.environ.get('ASKDAO_TOOL_USE_ID'),  # 跨层关联键
            'traceparent': os.environ.get('TRACEPARENT'),          # OTel trace 关联（扩展预留）
            'pid': os.getpid(),
            'modules': mods,
        }) + '\n')

atexit.register(_dump_imports)
```

> **跨层关联设计（来自报告 C 的数据模型启发）**：报告 C 提出了 `process_exec` 和 `dependency_observation` 两个实体，通过 `parent_tool_use_id` 关联到 `tool_call`。我们采纳这个设计——在注入 tracer 时，通过 `ASKDAO_TOOL_USE_ID` 环境变量把 Claude Code 的 `tool_use_id` 传递给子进程，使 tracer 输出能精确关联到"这些 import 是在哪次 Bash tool call 中产生的"。同时预留 `TRACEPARENT` 字段，为将来与 Claude Code 的 OTel trace tree 对接做准备。

注入方式（在 PreToolUse hook handler 的 Go 代码中）：

```go
// 检测到 python 命令时，改写为加 PYTHONPATH + 关联 ID 前缀
if isPythonCommand(cmd) {
    switch runtime.GOOS {
    case "windows":
        cmd = fmt.Sprintf(
            `$env:PYTHONPATH="%s;$env:PYTHONPATH"; $env:ASKDAO_TRACE_DIR="%s"; $env:ASKDAO_TOOL_USE_ID="%s"; %s`,
            tracerDir, traceOutputDir, toolUseID, cmd)
    default:
        cmd = fmt.Sprintf(
            `PYTHONPATH=%s:$PYTHONPATH ASKDAO_TRACE_DIR=%s ASKDAO_TOOL_USE_ID=%s %s`,
            tracerDir, traceOutputDir, toolUseID, cmd)
    }
    return hookResponse{Decision: "allow", UpdatedInput: map[string]string{"command": cmd}}
}
```

### 5.5 错误信息 → 系统包反向推断表

```go
// pkg/parser/errors.go
var errorToSysPkg = map[string][]string{
    "pg_config executable not found":       {"libpq-dev"},
    "fatal error: Python.h":                {"python3-dev"},
    "fatal error: ffi.h":                   {"libffi-dev"},
    "fatal error: openssl/ssl.h":           {"libssl-dev"},
    "fatal error: zlib.h":                  {"zlib1g-dev"},
    "fatal error: jpeglib.h":               {"libjpeg-dev"},
    "Package 'cairo' not found":            {"libcairo2-dev"},
    "command not found: cmake":             {"cmake"},
    "vips/vips8 not found":                 {"libvips-dev"},
    "No package 'gobject-introspection-1.0'": {"libgirepository1.0-dev"},
    // ... 扩展为 100+ 条映射
}
```

### 5.6 语言包 → 系统包映射表

```go
// pkg/syspkg/mapping.go
var langPkgToApt = map[string][]string{
    // Python 包 → apt 系统依赖
    "psycopg2":       {"libpq-dev"},
    "psycopg2-binary": {"libpq-dev"},
    "mysqlclient":    {"libmysqlclient-dev"},
    "Pillow":         {"libjpeg-dev", "zlib1g-dev", "libfreetype6-dev"},
    "lxml":           {"libxml2-dev", "libxslt1-dev"},
    "pycairo":        {"libcairo2-dev"},
    "pygit2":         {"libgit2-dev"},
    "opencv-python":  {"libgl1-mesa-glx", "libglib2.0-0"},
    "cryptography":   {"libssl-dev", "libffi-dev"},

    // Node 包 → apt 系统依赖
    "sharp":          {"libvips-dev"},
    "canvas":         {"libcairo2-dev", "libjpeg-dev", "libpango1.0-dev", "libgif-dev"},
    "bcrypt":         {"python3"},  // uses node-gyp + python
    "sqlite3":        {"libsqlite3-dev"},

    // 通用工具
    "playwright":     {"libnss3", "libatk-bridge2.0-0", "libdrm2", "libgbm1"},
}
```

此表是确定性的、跨平台的、可以开源让社区贡献。覆盖 100-200 个常见映射后，剩余 edge case 由 L4 LLM 层处理。

### 5.7 本地审阅 UI（HTTP server 角色复用）

#### 动机

askdao-cli 的现有审阅体验在 CLI 中完成（`askdao agent show` 的"中等详情卡片"）。overview.md 记录过这个钟摆问题：v0.4 的 230 行太重、v0.5 的 35 行又太糊。CLI 审阅的根本矛盾在于**信息量和交互性不可兼得**——终端里要么一次展示所有字段（用户滚到眼花），要么分段交互（用户来回答 yes/no 很烦）。

observe 层产出的 spec 比静态扫描产出的更丰富（每个字段带 provenance + evidence），在 CLI 里展示会更拥挤。而此时我们已经有一个跑在 `127.0.0.1:PORT` 的 Go HTTP server——它在 session 期间接收 hook events，session 结束后只需切换路由角色即可服务审阅页面，**零额外基础设施成本**。

#### 服务端角色切换

```go
// observe session 进行中：hook receiver
mux.HandleFunc("/event/pre",  hookPreHandler)
mux.HandleFunc("/event/post", hookPostHandler)
mux.HandleFunc("/event/end",  hookEndHandler)

// session 结束后：review UI server
//go:embed ui/review.html
var reviewHTML []byte

mux.HandleFunc("/review", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/html")
    w.Write(reviewHTML)
})
mux.HandleFunc("/api/spec", func(w http.ResponseWriter, r *http.Request) {
    // GET: 返回当前生成的 spec JSON（含 provenance + evidence）
    json.NewEncoder(w).Encode(currentSpec)
})
mux.HandleFunc("/api/spec/confirm", func(w http.ResponseWriter, r *http.Request) {
    // POST: 接收用户修改后的 spec，写 askdao-agent.yml，通知主 goroutine 退出
    var edited AgentSpec
    json.NewDecoder(r.Body).Decode(&edited)
    writeYAML(edited)
    confirmCh <- struct{}{}  // 解除 CLI 端的等待
})
```

CLI 端在 session 结束后打印一行并自动打开浏览器：

```go
fmt.Printf("✓ Observation complete. Review your agent spec at:\n\n")
fmt.Printf("  http://127.0.0.1:%d/review\n\n", port)
fmt.Printf("Waiting for your confirmation...\n")

// macOS: open, Linux: xdg-open, Windows: start
openBrowser(fmt.Sprintf("http://127.0.0.1:%d/review", port))

<-confirmCh  // 阻塞直到用户在页面上点击确认
```

#### 前端页面设计

审阅页面是一个自包含的 HTML 文件（inline CSS + JS），`go:embed` 嵌入到二进制，无 npm build、无 node_modules、无外部 CDN 依赖。页面启动时 `fetch('/api/spec')` 获取当前 spec 数据并渲染。

核心交互能力：

| 能力 | CLI 做不到 | 审阅 UI 怎么做 |
|------|-----------|---------------|
| **Provenance 可视化** | 颜色在终端有限 | 每个依赖旁用颜色标签：🟢 tracer（最高置信）、🔵 observed、🟡 snapshot、🟠 llm（需人工确认） |
| **按置信度分组** | 分组展示在终端很别扭 | 先展示 `source: llm` 的（最需要看），折叠 `source: tracer` 的（几乎不用改） |
| **Evidence 详情** | 占用大量屏幕空间 | hover/click ℹ️ 图标弹出 evidence（如 "app.py:3 → from flask import Flask"） |
| **Inline 编辑** | 需要 vim/nano | 直接在页面上改版本号、删除不需要的包、添加遗漏的包 |
| **YAML 预览** | 来回切换文件 | 实时 YAML preview panel，编辑即时反映 |

页面结构：

```
┌──────────────────────────────────────────────────────┐
│  askdao · Agent Spec Review                          │
│  Project: homework-spelling · Session: sess_abc      │
├──────────────────────────────────────────────────────┤
│                                                      │
│  Environment                                         │
│  ┌─ Base Image: python:3.12-slim             [edit]  │
│  │                                                   │
│  ├─ System Packages (2)                        [+]   │
│  │  🟠 libpq-dev    inferred ℹ️                 [x]  │
│  │  🔵 ffmpeg       command  ℹ️                 [x]  │
│  │                                                   │
│  ├─ Python Packages (4)                        [+]   │
│  │  🟢 flask==3.0.2      tracer  ℹ️             [x]  │
│  │  🟢 psycopg2==2.9.9   tracer  ℹ️             [x]  │
│  │  🔵 gunicorn==21.2.0   observed ℹ️           [x]  │
│  │  🟡 celery==5.3.6      snapshot ℹ️           [x]  │
│  │                                                   │
│  ├─ Env Vars (2)                               [+]   │
│  │  🔵 DATABASE_URL    observed ℹ️                    │
│  │  🔵 OPENAI_API_KEY  command  ℹ️                    │
│  │                                                   │
│  ├─ Ports: 8000                                      │
│  │                                                   │
│  Persona  [editable textarea]                        │
│  Skills / MCP Servers  [collapsible]                 │
│                                                      │
├──────────────────────────────────────────────────────┤
│  [Confirm & Generate]          [Export YAML Preview]  │
└──────────────────────────────────────────────────────┘
```

#### Fallback：CLI 审阅仍然可用

审阅 UI 不是唯一路径。添加 `--no-ui` flag 保留纯 CLI 审阅流程：

```bash
# 默认：自动打开浏览器审阅
askdao observe ./myapp --agent claude

# 纯 CLI 模式（CI/headless 环境）
askdao observe ./myapp --agent claude --no-ui
```

`--no-ui` 时退回到现有的 CLI 卡片式审阅 + `$EDITOR` 打开 YAML 文件的流程。

---

## 六、`askdao-agent.yml` 输出格式（harness-neutral + provenance）

中间格式描述**语义意图**（Agent 需要什么），不绑定任何特定 harness 的 API 结构。Conductor 端的 Adapter 负责翻译。

```yaml
apiVersion: askdao.ai/v1
kind: AgentSpec
metadata:
  name: homework-spelling
  generated_by: askdao-cli/0.8.0
  observe_session_id: "sess_abc123"

# ── Agent 身份 ──
# Anthropic → agent.system / agent.model
# OpenAI   → agent.instructions / agent.model
identity:
  model: claude-sonnet-4-6
  persona: |
    You are a homework spelling tutor. You help students
    identify and correct spelling errors in their writing.
  description: "A spelling tutor agent for homework help"

# ── 运行时环境（Adapter 各自翻译） ──
runtime:
  # Anthropic → 忽略（固定 Ubuntu 22.04）
  # OpenAI   → DockerSandboxClient(image=...) 或 E2B template
  base_image: python:3.12-slim

  # Anthropic → config.packages.apt
  # OpenAI   → Manifest setup_script: "apt-get install -y ..."
  system_packages:
    - libpq-dev
    - ffmpeg

  # Anthropic → config.packages.{pip,npm,cargo,gem,go}
  # OpenAI   → Manifest setup_script: "pip install ..."
  language_packages:
    pip:
      - flask==3.0.2
      - psycopg2==2.9.9
      - gunicorn==21.2.0
      - celery==5.3.6
    npm: []
    # cargo, gem, go 按需填充

  # Anthropic → Vault 资源（值由 KOL 在 deploy 时填入）
  # OpenAI   → Manifest.environment
  env_vars:
    - DATABASE_URL
    - OPENAI_API_KEY

  # Anthropic → 忽略（无端口概念）
  # OpenAI   → Sandbox 端口暴露
  exposed_ports:
    - 8000

  # Anthropic → config.networking
  # OpenAI   → Provider 级别配置
  networking:
    mode: unrestricted
    allowed_hosts: []

# ── 能力声明 ──
tools: []                # Anthropic → tools array; OpenAI → tools array
skills: []               # Anthropic → agent.skills; OpenAI → Skills capability
mcp_servers: []          # 两侧都支持 MCP

# ── observe provenance（审阅辅助，不上传到任何 API） ──
_provenance:
  "pip:flask==3.0.2":
    source: tracer
    evidence: "sys.modules['flask'] at process exit"
  "pip:psycopg2==2.9.9":
    source: tracer
    evidence: "sys.modules['psycopg2'] at process exit"
  "pip:gunicorn==21.2.0":
    source: observed
    evidence: "app.py:1 'import gunicorn' + pip freeze match"
  "pip:celery==5.3.6":
    source: snapshot
    evidence: "pip freeze diff: +celery==5.3.6"
  "apt:libpq-dev":
    source: inferred
    evidence: "psycopg2 in tracer output → known apt dep"
  "apt:ffmpeg":
    source: command
    evidence: "PostToolUse: apt-get install -y ffmpeg"
  "env:DATABASE_URL":
    source: observed
    evidence: "config.py:12 os.environ['DATABASE_URL']"
  "env:OPENAI_API_KEY":
    source: command
    evidence: "PreToolUse: echo $OPENAI_API_KEY"
  "port:8000":
    source: observed
    evidence: "app.py:45 app.run(port=8000)"
```

### 6.1 Adapter 翻译示例

**AnthropicAdapter** 拿到此 YAML 后拆成两个 API 调用：

```
POST /v1/agents
  name:        metadata.name
  model:       identity.model
  system:      identity.persona
  description: identity.description
  tools:       tools + mcp_toolset entries
  mcp_servers: mcp_servers
  skills:      skills

POST /v1/environments
  name:     metadata.name + "-env"
  config:
    type: cloud
    packages:
      apt: runtime.system_packages
      pip: runtime.language_packages.pip
      npm: runtime.language_packages.npm
    networking: runtime.networking

POST /v1/vaults
  为 runtime.env_vars 中的每个变量创建凭证占位

忽略：runtime.base_image, runtime.exposed_ports
```

**OpenAIAdapter**（Phase 2）拿到此 YAML 后合并为一个 SandboxAgent：

```
SandboxAgent(
  name:         metadata.name
  model:        identity.model  →  映射到 OpenAI 模型名
  instructions: identity.persona
  tools:        tools + mcp_servers 映射
  capabilities: [Shell(), Filesystem()]
  default_manifest: Manifest(
    entries: { "setup.sh": File(content=生成的安装脚本) }
    environment: { var: "<placeholder>" for var in runtime.env_vars }
  )
)

DockerSandboxClient(image=runtime.base_image)
或 E2BSandboxClient(template=...)

setup.sh 内容：
  apt-get update && apt-get install -y libpq-dev ffmpeg
  pip install flask==3.0.2 psycopg2==2.9.9 ...
```

### 6.2 Provenance 的设计原则

provenance 信息放在 `_provenance` 独立块（下划线前缀表示"元数据，不传输到 API"），而不是 inline 在每个字段里。原因：

- 可传输字段（`runtime.language_packages.pip`）的格式必须是简洁的字符串列表，与两个 API 的原生格式对齐
- provenance 只在审阅 UI 中展示，Adapter 不读取
- KOL 确认后，`_provenance` 块可以安全丢弃或归档

### 6.3 预装清单的处理方式

Anthropic 容器（Ubuntu 22.04）预装了 Python 3.12+、Node.js 20+、Go 1.22+、git、curl、jq、make、cmake、ripgrep 等。但 OpenAI 的不同 Provider（E2B / Docker / Cloudflare）预装内容各不相同。

**决策**：`pkg/syspkg` 映射表**不硬排除**任何预装包。observe 层忠实记录观测到的所有依赖。预装排除在 **Adapter 端**做——AnthropicAdapter 持有一份 cloud-containers.md 的预装清单，生成 `config.packages` 时减去已预装的项；OpenAIAdapter 根据目标 Provider 的 base image 做类似排除。

`_provenance` 中可以标注 `"note": "Anthropic 云容器已预装"` 供审阅参考，但不影响 YAML 的可传输字段。

---

## 七、安全与隐私

### 7.1 核心原则

延续 askdao-cli "本地隐私"设计哲学：

- **Observe 全在本地跑**——Hook server 绑 127.0.0.1，数据写本地 `.askdao/` 目录
- **不上传任何观测数据**——observations.jsonl 只用于本地生成 askdao-agent.yml
- **环境变量只记名字不记值**——`env_vars` 里只有 `DATABASE_URL`，永远不存 `postgres://user:pass@...`
- **tracer 脚本只记模块名和版本**——不记文件内容、不记函数调用、不记 I/O

### 7.2 需要脱敏的数据类型

| 数据 | 风险 | 处理 |
|------|------|------|
| bash 命令中的 token/密码 | `curl -H "Authorization: Bearer sk-..."` | hook handler 正则脱敏，只保留命令结构 |
| 环境变量值 | `export DB_URL=postgres://...` | 只记变量名，丢弃值 |
| Git remote URL | 可能含用户名/组织拓扑 | hash 处理或不记录 |
| 源文件内容 | Agent 写的代码可能含 secrets | import 解析后立即丢弃原始内容 |
| tracer 输出 | 只含模块名/版本 | 已经是最小信息集 |

### 7.3 环境变量白名单

快照层只捕获白名单内的环境变量，绝不 dump 整个 `os.Environ()`：

```go
var envAllowlist = []string{
    "PATH", "HOME", "USER", "SHELL",
    "VIRTUAL_ENV", "CONDA_PREFIX", "CONDA_DEFAULT_ENV",
    "NPM_CONFIG_PREFIX", "NODE_ENV", "GOPATH", "GOROOT",
    "PYTHONPATH", "PYTHONHOME",
    "LANG", "LC_ALL",
}
```

### 7.4 数据保留策略（来自报告 C 的冷热分层建议）

虽然 askdao-cli 的观测数据目前只在本地临时存在（observe 结束后清理），但如果将来扩展到 team-grade 的持久化观测（如 AskDAO 平台级的 Agent 审计），应遵循以下分层保留策略：

| 数据类型 | 敏感度 | 建议保留期 |
|---------|--------|-----------|
| 结构化 metadata（session / tool / decision / duration / cost） | 低 | 30-90 天 |
| tool 名称、成功/失败、cwd、repo、tool_use_id、model、permission source | 低 | 30-90 天 |
| package snapshot / git metadata / env allowlist 快照 | 低 | 30-90 天 |
| Bash command 明文、file path、MCP 参数 | 中 | 3-7 天 |
| prompt / tool output 全文 | 高 | 3-7 天（opt-in） |
| raw API request/response body | 极高 | 仅调试期开启，不持久化 |

**红线**（三份报告一致）：

- **不要**默认长期保存完整 prompt、完整 tool output、完整 raw API request/response
- **要**默认保存结构化 metadata
- **可按需打开** Bash command 明文和 MCP 参数体

> 这与 Claude Code 和 Codex CLI 的官方默认策略对齐：两者都将 prompt content、tool details、tool content、raw API bodies 设为显式 opt-in（`OTEL_LOG_USER_PROMPTS`、`OTEL_LOG_TOOL_DETAILS`、`OTEL_LOG_TOOL_CONTENT`、`OTEL_LOG_RAW_API_BODIES` / Codex 的 `log_user_prompt = false`）。

---

## 八、已知局限

### 8.1 观测覆盖有限

Agent 在一次 session 中不一定执行所有代码路径。如果某个 import 在未触发的 if 分支里、某个依赖只在特定 feature flag 下用到，observation 不会捕获到。

**缓解措施**：

- 静态扫描（L1-L3）作为 floor——它能看到 lockfile 里声明的所有包，不管是否被执行
- 引导 prompt 可以指导 Agent 尽量覆盖更多路径（"run tests", "exercise all endpoints"）
- 未来可支持多次 observe session 聚合（不同 prompt → union specs）

### 8.2 非确定性

Agent 可能本次用 `pip install`，下次用 `uv add`。同一个包可能被不同名字引用（`psycopg2` vs `psycopg2-binary`）。

**缓解措施**：parser 层做包名归一化（如 `psycopg2-binary` → `psycopg2` family），保留所有 surface form 作为 evidence。

### 8.3 Codex CLI 的 hook 覆盖 gap

Codex CLI 的 `unified_exec` 新路径未被 hooks 完全覆盖，`codex exec` 和 `codex mcp-server` 的 OTEL 也曾有 gap。

**缓解措施**：Phase 2 实现 Codex 支持前，先验证当前版本（≥0.125）的实际覆盖情况。要求 Codex ≥0.125 作为最低版本。

### 8.4 tracer 的代码路径限制

Import tracer 只记录进程实际加载的模块。如果一个包被 `pip install` 了但没有被 import（比如只用了它的 CLI 工具 `flask run`），tracer 看不到。

**缓解措施**：与路径 ② snapshot diff 交叉验证——snapshot 能看到它被安装了，tracer 看不到它被 import。两者取 union。

### 8.5 `transcript_path` 不是稳定接口（来自报告 C + Codex 官方文档）

Claude Code 和 Codex CLI 都在 hook payload 中提供 `transcript_path` 字段，指向 session 的 JSONL transcript 文件。但 Codex 官方文档明确声明：**transcript 格式不是稳定接口，可能在任何版本变更。** Claude Code 的 transcript 格式虽然目前较稳定，也没有给出向后兼容承诺。

**决策**：askdao-cli 不依赖 transcript 文件的解析。我们通过 hooks 直接拿结构化 JSON 数据（`tool_input`、`tool_response`、`exit_code` 等），这些字段有文档化的 schema。`transcript_path` 只作为调试参考写入 `observations.jsonl`，不参与 spec 生成的任何逻辑。

### 8.6 Claude Code detailed tracing 仍有 beta 面

Claude Code 的精细化 trace spans（`claude_code.hook` span、per-tool `execution` sub-span）需要 beta tracing 设置才能启用，交互式 session 下可能还需要 allowlisting。span names 和 attributes 可能在版本间变化。

**决策**：askdao-cli 的主数据通路是 hooks（稳定），不是 OTEL traces（beta）。traces 仅作为可选的补充诊断信号。

### 8.7 proxy 型方案不等于 runtime ground truth

claude-tap、claude-trace 等 proxy/拦截型工具能看到 API 面的完整数据（system prompt、conversation history、tool schemas），但它们记录的是"网络层发生了什么"，不是"本地执行面发生了什么"。import graph、hidden subprocess、动态依赖下载在 API 面完全不可见。

**对 askdao-cli 的含义**：这些工具是研究和边界对照的好帮手，但不能替代我们的 hooks + tracer 方案。

---

## 九、实施计划

### 阶段划分

基于讨论共识：**不做分阶段发布，四条观测路径一次到位。** 但工程实施上分两个 phase（按 Agent 支持范围划分，不是按功能裁剪）。

### Phase 1：Claude Code 完整 observe 能力（v0.8）

**工期估算：3-4 周**

| 周次 | 交付 |
|------|------|
| W1 | `pkg/hookserver`（Go HTTP server + Claude Code event handlers）+ `pkg/hookconfig/claude.go`（生成 .claude/settings.local.json） |
| W1 | `pkg/snapshot`（SessionStart/End 全量快照 + diff，含跨平台 `runtime.GOOS` 分支） |
| W2 | `pkg/parser`（apt/pip/npm/cargo/go 命令解析 + 错误信息反向推断表） |
| W2 | `pkg/importscan`（Python/Node/Go 源文件 import 静态解析） |
| W2 | `pkg/tracer`（sitecustomize.py + trace-require.js embed + 注入逻辑 + 输出收集） |
| W3 | `pkg/syspkg`（语言包→系统包映射表，初始 100+ 条目） |
| W3 | `pkg/merger`（合并器 + provenance 标记 + askdao-agent.yml 输出） |
| W3 | `cmd/observe.go`（完整流程编排 + 清理逻辑） |
| W3 | `embed/ui/review.html`（本地审阅页面）+ `pkg/hookserver/review.go`（server 角色切换 + /review、/api/spec 路由） |
| W4 | 在 20 个代表性项目上端到端测试（Django+PG, FastAPI+Redis, Next.js+Prisma, Streamlit+ML 等） |
| W4 | 跨平台验证（macOS + Linux，Windows 如有条件则验证） |

**验收标准**：

- 在 20 个测试项目上，生成的 `askdao-agent.yml` 经 Conductor 部署后一次构建成功率 ≥ 80%
- 输出 YAML 符合 harness-neutral AgentSpec schema（identity / runtime / tools / skills / mcp_servers / _provenance）
- `_provenance` 块包含每个字段的 source + evidence
- macOS 和 Linux 上均可正常运行
- observe 结束后零残留（.askdao/ 目录 + hook 配置全部清理）
- 审阅 UI 支持 provenance 颜色标签、evidence 弹窗、inline 编辑、YAML 预览
- `--no-ui` flag 退回 CLI 审阅流程（CI/headless 环境可用）

### Phase 2：Codex CLI 支持 + 社区反馈迭代（v0.9）

**工期估算：2-3 周**

| 任务 | 说明 |
|------|------|
| `pkg/hookconfig/codex.go` | 生成 .codex/hooks.toml（command type） |
| `embed/tracers/askdao-hook-shim` | command → HTTP 中转 shim（如 updatedInput 可用则可简化） |
| Codex `updatedInput` 实测 | 在目标 Codex 版本上验证 PreToolUse 是否真正支持命令改写（报告 A 与报告 C 存在矛盾） |
| Codex hook 覆盖度验证 | 确认 ≥0.125 的实际覆盖情况，特别关注 `unified_exec` 路径 |
| `pkg/syspkg/mapping.go` 扩展 | 根据 Phase 1 测试结果补充映射表 |
| 多 session 聚合模式 | `askdao observe --rounds 3 --prompts ./prompts/`，union specs |

---

## 十、与 askdao-cli 现有架构的关系

### Observe 层是 L1-L4 的补充，不是替代

```
现有 4 层静态流水线（保留）：
L1  syft        →  扫出所有依赖包
L2  dev_filter  →  过滤开发依赖 / 探测 MCP / skills / 密钥
L3  providers   →  nixpacks 式框架推断 + apt 反向映射
L4  LLM         →  推荐 + reasoning

新增 observe 层：
O1  hook events  →  command parser + 错误反向推断
O2  tracer       →  import ground truth
O3  snapshot     →  环境全量 + diff
O4  importscan   →  源文件 import 解析

合并器决定最终 spec（harness-neutral AgentSpec）：
- 两者一致 → 直接采用，置信度最高
- observe 有而 static 无 → 采用 observe（典型案例：libpq-dev）
- static 有而 observe 无 → 保留 static 但标记 "未被运行时验证"
- 冲突 → 以 observe 为准（runtime truth > 声明式推断），但展示 diff 给 KOL 审阅

输出格式遵循 §2.4 的 harness-neutral 原则：
- identity（model, persona）→ 两个 harness 各自映射字段名
- runtime（packages, env_vars, ports, networking）→ Adapter 决定如何配置
- _provenance → 审阅辅助，不传输到任何 API
```

### 使用方式

```bash
# 方式 1：只跑静态扫描（现有行为，不变）
askdao agent init --auto

# 方式 2：静态扫描 + 运行时观测（新增）
askdao observe ./myapp --agent claude
# observe 结束后自动调用 init --auto，但 merger 会合并观测数据

# 方式 3：只跑观测，不跑静态（适用于没有 lockfile 的项目）
askdao observe ./myapp --agent claude --skip-static
```

---

## 十一、三份调研报告的交叉验证总结

三份独立调研从不同角度审视了同一个问题空间。以下是它们的共识点和分歧点，供后续开发参考。

### 11.1 完全一致的结论（高置信度）

| 结论 | 报告 A | 报告 B | 报告 C |
|------|--------|--------|--------|
| Claude Code hooks 是最强的原生观测原语 | ✅ | ✅ | ✅ |
| Codex CLI hook 覆盖面显著窄于 Claude Code | ✅ | ✅ | ✅ |
| Codex OTEL 故意不包含命令内容 | ✅ | ✅ | ✅ |
| 原生 hooks/OTEL 不能单独解决"实际用了什么包" | ✅ | ✅ | ✅ |
| 必须补充 runtime instrumentation（import shim / shell wrapper） | ✅ | ✅ | ✅ |
| 现有开源项目均停留在观测/展示层 | ✅ | ✅ | ✅ |
| "observe → spec" 是空白地带 | ✅ | ✅ | ✅（未显式说，但其建议止于观测） |
| SessionStart 环境快照优于离线全盘扫描 | ✅ | ✅ | ✅ |
| eBPF/auditd 应作为可选增强而非默认 | ✅ | ✅ | ✅ |

### 11.2 存在分歧需注意的点

| 分歧 | 报告 A | 报告 B | 报告 C | 本方案决策 |
|------|--------|--------|--------|-----------|
| AI Observer 是否适合做统一后端 | 未评估 | 推荐为首选 | 推荐为首选 | **不采纳**（~209 stars，过于早期；askdao-cli 不需要通用后端） |
| Codex 是否支持 `updatedInput` 改写 | 查到不支持（PR #18491） | 未涉及 | 声称已支持 | **Phase 2 开始前实测验证** |
| `transcript_path` 是否可靠 | 未特别强调 | 未涉及 | 明确警告不稳定 | **不依赖 transcript 解析** |

### 11.3 askdao-cli 的差异化定位

三份报告全部聚焦于**观测**（observe / audit / replay）。askdao-cli 独有的价值在于在观测之上多走了一步——**从观测数据自动生成可部署的 Agent 规格**。这个 "observe → spec" 翻译层包括：

- `pkg/parser`：bash 命令 → spec patch 翻译
- `pkg/syspkg`：语言包 → 系统包反向映射
- `pkg/merger`：多源合并 + provenance 标记
- harness-neutral `askdao-agent.yml`（identity / runtime / _provenance）
- 跨平台（macOS/Windows 观测环境 → 多种部署目标）的包名翻译

这些模块在整个开源生态中无对应物，是 askdao-cli 的核心 IP。

### 11.4 双 Harness API 对照后的重要修正

对照 Anthropic Managed Agents API 和 OpenAI Agents SDK 的实际接口后，修正了此前纯基于调研报告得出的部分建议：

| 此前建议 | 修正 | 原因 |
|---------|------|------|
| 删除 `base_image` 字段 | **保留** | Anthropic 固定不可选，但 OpenAI Docker/E2B Provider 可选 |
| 将 `env_vars` 改名为 `required_secrets` | **保留原名** | Anthropic 走 Vault，OpenAI `Manifest.environment` 直接支持 |
| 删除 `exposed_ports` 字段 | **保留** | Anthropic 无端口概念，OpenAI Sandbox 支持 |
| 将 YAML 拆成 `agent` + `environment` 两块 | **不拆** | Anthropic 特有的资源分离，OpenAI 是一体化 SandboxAgent |
| `pkg/syspkg` 硬排除预装包 | **不硬排除** | 预装清单因 harness/Provider 而异，排除由 Adapter 端做 |

> **教训**：只看一个 harness 的 API 做设计决策，会不自觉地把中间格式变成该 harness 的方言。overview.md §2.4 的设计哲学——"如果 yaml 字段直接绑 Anthropic SDK，叙事会坍缩"——在这次对照中被再次验证。
