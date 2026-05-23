# askdao-cli Agent Bootstrap (`askdao agent init --auto`)

> **Scope**: plan/06-deploy-cli.md §4.2 `askdao agent init` 命令的智能化补强 ——
> 让 KOL 在自己项目目录下跑一行命令，自动产出 **harness-neutral 中间格式** 的 agent spec 草稿，
> conductor 端按 KOL 偏好的 runtime（Anthropic Managed Agents / OpenAI Agents SDK / ...）转换为对应 API 调用。
>
> **Version**: v0.5 (2026-05-06)
> **Status**: Design draft — pending review
> **Owner**: Sam
> **Aligns with**: memory `project_askdao_cli_design_pivot_2026_05_05.md`（Go + 借鉴 Oz Environment 一等抽象）；archived `harness-selection-analysis.md`（多 harness 路径）

---

## ChangeLog

### v0.5 · 2026-05-06 — KOL 审阅 UX 中等详情卡片

哥指出 v0.4 yaml 字段非常丰富（230+ 行）让 KOL 直接确认心智负担过大；第一版纯摘要（35 行）又被批评"过于摘要、模糊"丢失关键文件路径 / skill / dep 等具体内容。详细分析见 [`review-v0.5-2026-05-06.md`](./review-v0.5-2026-05-06.md)：

- **核心决策**：采用**中等详情卡片**（mid-density，7 块顶层结构 / ~80-90 行屏幕空间）+ inline reasoning（`↳ Why:` 引导符）+ 入口扩展（D/F/M/W/P 子命令查看更多细节）
- **7 块结构**：PERSONA / SKILLS / MCP SERVERS / CAPABILITIES / RUNTIME / SUBSCRIBER ONBOARDING / TRANSLATION WARNINGS
- **字段三档分类**：必列具体（Skills / MCP / Vault credentials / 关键文件路径 / Tool overrides / apt libs）；列计数+入口（28 个 Python deps 列前 8；dev deps 计数）；展开 reasoning（Model 选择 / Tool override / Skill 推荐 / Translation warning）
- **§3 命令骨架修订**：`init --auto` 改交互式 [A/E/R/S/D/F/M/W/P/Q]；`deploy` 加 diff preview（KOL 改了 yaml 后显示与推荐版本的差异）；新增 `askdao agent show <name> [--full|--reasoning|--warnings]`
- **§6.1 askdao-cli 加 7 个 render 模块**：~870 行 Go（summary / reasoning / diff / warnings / lists / show 命令 / init+deploy 改造）
- **§9 决策记录加 9.9**（中等详情 UX 选定）

### v0.4 · 2026-05-06 — Dockerfile 兼容性补强（选项 B）

哥追问"v0.3 是否考虑了 Dockerfile 常用内容和写法的兼容？"。承认 v0.3 在这点上覆盖不够（仅识别 + 抽 base_image，丢失多阶段构建、自定义镜像、复杂 RUN 链、USER 切换、EXPOSE 端口等常见模式）。详细分析见 [`review-v0.4-2026-05-06.md`](./review-v0.4-2026-05-06.md)：

- **§4 detection.json `detected_dockerfile` 字段扩展**：新增 `stages` / `run_commands` / `users` / `workdir` / `env_vars` / `cmd` / `entrypoint` / `build_args` / `extracted_apt_packages` / `extracted_pip_packages` / `extracted_setup_commands`，Dockerfile 完整 AST + 提取产物
- **§5 yaml `workspace` 块加 5 个新字段**：`base_image` / `setup_commands` / `users` / `workdir` / `exposed_ports`。Anthropic adapter 视为不支持并输出警告；OpenAI adapter（Phase 2）真正消费
- **§5 加 5.5 Translation Report 子章节**：定义 adapter 输出格式（`translation_warnings: [{field, action, reason, severity, fallback_attempted}]`）
- **§6.1 askdao-cli 端 `dockerfile.go` 行数 80 → 200 行**（用 `moby/buildkit` parser 完整解析）
- **§6.2 conductor 端加 ~100 行**：AnthropicAdapter 加 translation_report 输出 + `extracted_*` 字段兜底合并到 packages
- **§9 决策记录加 9.7**（Dockerfile 选项 B）+ **9.8**（不做 GPU 声明 —— 哥确认 AskDAO 不跑 ML 类任务）
- **未做的字段（哥确认）**：`resources.gpu` / `dockerfile.path` 直挂 / `build_args` build-time secrets / `volumes` / `healthcheck` / `labels`

### v0.3 · 2026-05-06 — Harness-neutral 中间格式重构

哥指出 askdao-cli 是开源项目，yaml 字段直接绑定 Anthropic SDK 字段名（即使三块布局正确）也会让对外叙事坍缩为"我们只为 Anthropic 服务"。重审两套 harness（Anthropic Managed Agents + OpenAI Agents SDK）后做以下修订（细节见 [`review-v0.3-2026-05-06.md`](./review-v0.3-2026-05-06.md)）：

- **§5 yaml 完全重写为 harness-neutral 中间格式**：顶层 8 个 harness-neutral 块（metadata / persona / capabilities / mcp_servers / custom_tools / skills / workspace / vault_hints）+ `apiVersion: askdao.ai/v1` + `kind: AgentSpec` + harness 独有特性进 `harness_specific:` escape hatch
- **§2 系统架构加 conductor adapter 层**：L4 LLM 输出中间格式，conductor 端 AnthropicAdapter / OpenAIAdapter 转译到具体 harness API
- **§4 detection.json 加 `detected_harness_signals`**：探查用户机器是否已装 Claude Code / Codex / Cursor，影响 harness 推荐
- **§6 工程量分两侧**：askdao-cli 端 ~3000 行 Go（v0.2 + 字段重命名）；conductor 端 Phase 1 AnthropicAdapter ~1 周；Phase 2 OpenAIAdapter ~2000 行 Python / 4-6 周
- **§7 三阶段路线图**：Phase 1 中间格式 + 单 adapter（不阻塞 askdao-cli MVP）/ Phase 2 OpenAIAdapter（开源前必做）/ Phase 3 更多 harness
- **§9 决策记录加 9.5**（中间格式选择）+ **9.6**（分阶段切分）

### v0.2 · 2026-05-06 — Anthropic 三资源模型重构

哥指出 v0.1 在 Anthropic Managed Agents 抽象层有结构性偏差。重读官方文档后做以下修订（细节见 [`review-2026-05-06.md`](./review-2026-05-06.md)）：

- **§5 agent.yml schema 重写为三块布局**：`agent` (Anthropic Agent 资源) + `environment` (Anthropic Environment 资源) + `vault_hints` (订阅者 onboarding 引导)。v0.1 把所有字段塞 `environment` 块的设计被推翻 —— Agent 才是富资源，Environment 只是容器配置
- **§4 detection.json 增加 4 个探查字段**：`detected_mcp_configs` / `detected_skills` / `detected_required_secrets` / `detected_tool_risk_hints`
- **§6 工程量估算调整为 ~2950 行 Go**（+550）：4 个新 scanner 模块（mcp_config / skills_dir / secrets_hint / policy 推断）
- **§9 决策回填**：决策 9.1（LLM 走 Conductor）、9.2（syft 走 CLI 进程）哥已确认；决策 9.3 拆为 3a (yaml 三块布局) + 3b (conductor PG agent_spec 加 2 列：`managed_agent_version` + `vault_hints_json`)

### v0.1 · 2026-05-05 — 初稿

四层流水线（syft → dev-filter → providers → LLM）+ detection.json + agent.yml 双 schema + 工程量估算。原存放于 `harness-design/designs/`。

---

## 1. 动机与定位

### 1.1 为什么需要

plan/06 §4.2 当前定义 `askdao agent init <name>` 的产物是**空目录骨架**：

```
my-agent/
├── agent.yml         # 主 spec — KOL 手填
├── persona.md        # KOL 手写
├── skills/
└── resources/
```

KOL 体验：盯着空白 `agent.yml` 不知怎么填 model / tools / packages / system prompt。"10 分钟从零上线"承诺难兑现。

### 1.2 借鉴 Warp Oz 的 `/create-environment` 模式

Warp 给工程师的 IDE 体验是：跑 `/create-environment` → Warp 扫仓库 → 自动推荐 Docker image + setup commands → 用户确认即可。这个模式本质是「**把模糊的"配 environment" 变成"审阅推荐"**」。

askdao-cli 的 `init --auto` 应该 1:1 对齐这个 UX，但**输出对象不是单一 harness 的配置，而是 harness-neutral 中间格式**（见 §1.3）。

### 1.3 Harness-neutral 中间格式（v0.3 核心修订）

askdao-cli **是开源项目**（face 工程师社区）。这意味着：

- 工程师社区有人用 **Claude Code**（背后 Anthropic Managed Agents）
- 也有人用 **OpenAI Codex**（背后 OpenAI Agents SDK）
- 未来还有 LangGraph / Vercel OA / 本地 Ollama 等更多 harness

如果 yaml 字段直接对齐 Anthropic SDK（即使三块布局正确），开源出去就是在告诉社区"我们只为 Anthropic 服务"。

**v0.3 的根本决策**：askdao-cli 输出的是 **harness-neutral 中间格式**（`apiVersion: askdao.ai/v1` + `kind: AgentSpec`），由 conductor 端 adapter 翻译成具体 harness API：

```
askdao-cli                        conductor
─────────                         ─────────
detection.json                    AnthropicAdapter
   ↓                              → POST /v1/agents (Anthropic API)
LLM 推荐                          → POST /v1/environments
   ↓                              → ...
中间格式 yaml         ────►       OpenAIAdapter           (Phase 2)
(harness-neutral)                 → SandboxAgent + Manifest + Runner.run()
                                  → ...
                                  
                                  其他 adapter            (Phase 3)
                                  → LangGraph / Vercel OA / ...
```

**Phase 1**（即将做）：中间格式 + 仅 AnthropicAdapter（保持当前 conductor M0-M2 路径）  
**Phase 2**（开源前必做）：加 OpenAIAdapter，conductor 端支持双 runtime  
**Phase 3**（中长期）：加更多 harness

详细路线图见 §7。

### 1.4 与 plan/06 现有方向的衔接

本设计**不替代** plan/06 §4.2-§4.4，是它的**前置增强**：

```
[新增] askdao agent init --auto <name>     # 扫描当前目录 → 生成 agent.yml 草稿
       └─ KOL 修订 agent.yml + persona.md
[已有] askdao agent validate                # plan/06 §4.3
[已有] askdao agent deploy                  # plan/06 §4.4
```

---

## 2. 系统架构（askdao-cli 四层流水线 + conductor adapter 层）

### 2.1 askdao-cli 端（用户本地，离线为主）

```
┌──────────────────────────────────────────────────────────┐
│  L1 · syft (CLI 进程)                                     │  确定性扫描
│  → 1000+ 包+版本（含传递依赖）                              │
└──────────────┬───────────────────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────────────────┐
│  L2 · dev-filter + 4 个补充 scanner                        │  manifest 二次解析
│  • dev_filter (pyproject.toml / package.json dev/prod)    │
│  • mcp_config_scan (.mcp.json / claude_desktop_config)    │
│  • skills_dir_scan (.claude/skills / .agents/skills ...)  │
│  • secrets_hint (.env.example keys → vault hints)         │
│  → ~50 prod deps + mcp_servers + skills + secrets         │
└──────────────┬───────────────────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────────────────┐
│  L3 · providers (移植 nixpacks) + policy 推断器             │  启发式
│  • providers.detect()/Plan() 每语言一个 provider           │
│  • policy.infer() 生产信号 → permission_policy             │
│  • harness_signals (detect Claude Code / Codex 已装)       │
│  → detected_frameworks + apt_pkgs + tool_risk + harness    │
└──────────────┬───────────────────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────────────────┐
│  L4 · LLM 推荐器（调 conductor 后端）                      │  模糊推断
│  把 detection.json 喂 LLM 生成中间格式 yaml + reasoning    │
│  • metadata + persona + capabilities + mcp_servers         │
│  • custom_tools + skills + workspace + vault_hints         │
│  • preferred_harness (基于 harness_signals 推断)           │
│  • harness_specific (escape hatch)                         │
└──────────────┬───────────────────────────────────────────┘
               │
               ▼
       agent.yml (中间格式 v1) + .askdao/detection.json
```

**层间分离原则**：L1-L3 全确定性，离线可跑，零成本；L4 才调 LLM。  
**输出**：harness-neutral 中间格式，与具体 SDK 字段解耦。

### 2.2 conductor 端（云端 deploy 流程）

```
agent.yml (中间格式)
       │
       ▼
┌─────────────────────────────────────────────┐
│  conductor: AgentSpec 验证 + adapter 路由     │
│  根据 yaml.preferred_harness 选择 adapter    │
└──────┬───────────────────────┬──────────────┘
       │                       │
       ▼                       ▼ (Phase 2)
┌─────────────────┐    ┌─────────────────┐
│ AnthropicAdapter│    │ OpenAIAdapter   │
│ (Phase 1)       │    │ (Phase 2)       │
└────┬────────────┘    └────┬────────────┘
     │                      │
     ▼                      ▼
┌──────────────────┐    ┌──────────────────────────────┐
│ POST /v1/agents  │    │ SandboxAgent + Manifest      │
│ POST /v1/        │    │ + Capabilities + Runner.run()│
│   environments   │    │ (in-process Python loop)     │
└──────────────────┘    └──────────────────────────────┘
```

**层间分离原则**：askdao-cli 不知道也不应知道 yaml 最终落到哪个 SDK；conductor 端 adapter 是唯一的"翻译边界"。  
**Phase 切分**：Phase 1 仅 AnthropicAdapter；Phase 2 加 OpenAIAdapter；Phase 3 更多 harness。

---

## 3. 命令骨架（plan/06 §4.2 增量）

### 3.1 `askdao agent init <name> [--auto] [--from <path>] [--harness <id>]`

无 `--auto`：保持现状（生成空目录骨架）。

加 `--auto`：触发扫描流水线 + LLM 推荐 + **交互式中等详情卡片审阅**（v0.5）：

```bash
$ cd ~/WorkSpace/my-fastapi-project
$ askdao agent init my-agent --auto

→ Scanning ./ ...
→ Detected: Python 3.12 + FastAPI + SQLAlchemy + PostgreSQL
→ Detected 28 prod deps (14 dev deps filtered)
→ Detected 1 MCP config (.mcp.json: github)
→ Detected 1 custom skill + recommending 1 builtin (xlsx)
→ Detected 2 required secrets from .env.example
→ Detected production deploy signal → shell permission=ask_for_dangerous
→ Detected harness signals: claude-code ✓ codex ✗
→ Calling LLM (via conductor) for system_prompt + reasoning ...

✓ Generated draft for "my-agent"

═══════════════════════════════════════════════════════════════
 PERSONA
═══════════════════════════════════════════════════════════════

  Name           : my-agent
  Description    : Backend engineering assistant
  Model class    : high_reasoning
  Primary model  : Claude Opus 4.6 (standard speed)
                   ↳ Why: FastAPI + Alembic migration logic complex
                          Confidence: 0.78
  Fallback       : gpt-5.4, claude-sonnet-4-6
  Persona file   : persona.md  (empty — KOL to write)
  System prompt  : 380 chars  [P] view


═══════════════════════════════════════════════════════════════
 SKILLS  (2)
═══════════════════════════════════════════════════════════════

  ✓ xlsx  (Anthropic builtin)
    Why : detected pandas + openpyxl in your dependencies
    Conf: 0.85

  ✓ portfolio-analyzer  (custom local)
    Path: ./skills/portfolio-analyzer/SKILL.md  (4.2 KB)
    Will be uploaded to Anthropic on deploy.


═══════════════════════════════════════════════════════════════
 MCP SERVERS  (1 active, 1 filtered)
═══════════════════════════════════════════════════════════════

  ✓ github
    URL  : https://api.githubcopilot.com/mcp/
    Type : url (Anthropic compatible ✓)
    Token: GITHUB_TOKEN  (from .env.example)

  ⊘ filesystem  (filtered out)
    Type : stdio (not supported by Anthropic)
    [M] see filtered + alternatives


═══════════════════════════════════════════════════════════════
 CAPABILITIES (Tool permissions)
═══════════════════════════════════════════════════════════════

  shell          ⚠️  ask_for_dangerous
                 ↳ Why: .github/workflows/deploy.yml + production.toml
                        suggest production-touching code
  filesystem     ✓  allow (scopes: ./output, ./tmp)
  web            ✓  allow
  code_execution ✓  allow


═══════════════════════════════════════════════════════════════
 RUNTIME  (Anthropic Managed Agents environment)
═══════════════════════════════════════════════════════════════

  Python production deps (28 total, 14 dev filtered):
    • fastapi==0.135.1          • alembic==1.18.4
    • sqlalchemy==2.0.48         • anthropic==0.97.0
    • asyncpg==0.31.0            • pydantic==2.12.5
    • uvicorn==0.36.0            • httpx==0.28.0
    ... and 20 more   [D] view all   [F] view 14 filtered

  System libs (apt, all 3):
    • libpq-dev   ↳ asyncpg/psycopg need postgres headers
    • gcc         ↳ Python C extensions compile
    • libjpeg-dev ↳ Pillow image processing

  Networking: limited
    Allowed hosts: api.anthropic.com, api.openai.com,
                   api.githubcopilot.com  (3 total)
  Workdir: /app


═══════════════════════════════════════════════════════════════
 SUBSCRIBER ONBOARDING  (Vault hints)
═══════════════════════════════════════════════════════════════

  Required (1):
    🔑 GITHUB_TOKEN
       Used by: MCP server "github"
       From   : .env.example
       Subscribers need to provide their own.

  Optional: none


═══════════════════════════════════════════════════════════════
 ⚠️  TRANSLATION WARNINGS  (Anthropic Managed Agents)
═══════════════════════════════════════════════════════════════

  HIGH (2):
    • workspace.base_image = "pytorch/pytorch:2.1.0-cuda12.1"
      → IGNORED. Anthropic uses fixed cloud image.
      → Phase 2 OpenAIAdapter supports custom image.

    • workspace.setup_commands (2 commands)
      → PARTIALLY IGNORED. apt/pip names extracted; raw commands lost.
      [W] see commands

  MEDIUM (1) · LOW (0)   [W] see all


═══════════════════════════════════════════════════════════════
 ACTIONS
═══════════════════════════════════════════════════════════════

  [A] Approve and deploy        [P] View persona / system prompt
  [E] Edit yaml in $EDITOR       [D] View all 28 Python deps
  [R] View full reasoning trace  [F] View filtered dev deps
  [S] Show full yaml in pager    [W] View all warnings
                                 [M] View filtered MCP

  [Q] Quit (saved as draft)

>
```

**字段三档分类原则**（v0.5 核心设计）：

| 档位 | 字段 | 处理方式 |
|------|------|---------|
| **必列具体** | Skills / MCP / Vault credentials / 关键文件路径 / Tool overrides / apt libs | 每条逐项展开 |
| **列计数 + 入口** | 28 个 Python deps（前 8 + and 20 more）/ dev deps 计数 / 网络白名单（前 5） | "[D] view all" 入口 |
| **展开 reasoning** | Model 选择 / Tool override / Skill 推荐 / Translation warning | inline `↳ Why:` 引导符 |

`--from <path>` 允许从非 cwd 的目录扫。

`--harness <id>`（可选）显式指定 `preferred_harness`，覆盖 LLM 自动推断：
- `anthropic_managed_agents`（Phase 1 唯一可用 adapter）
- `openai_agents_sdk`（Phase 2 启用；当前 `init` 接受但 `deploy` 会拒绝）
- `auto`（默认，按 detected_harness_signals 推断）

### 3.2 `askdao detect [path]`（仅诊断）

不创建 agent 目录，只跑 L1-L3 + 打印 detection report。用于 KOL 想"先看看有什么"的探索路径。

```bash
$ askdao detect ./
Languages: Python 67% · TypeScript 26% · Shell 7%
Frameworks: FastAPI (conf=0.95) · SQLAlchemy (conf=0.92)
Production deps: 28 pip / 12 npm
System pkgs: libpq-dev gcc libjpeg-dev
Lockfiles: uv.lock pnpm-lock.yaml
Harness signals: claude-code ✓ codex ✗ cursor ✗
```

### 3.3 `askdao agent show <name> [--full|--reasoning|--warnings|--persona|--deps|--mcp]`（v0.5 新增）

显示已创建 agent 的中等详情卡片。子选项控制详情深度：

- 无选项 → 默认显示 7 块中等详情卡片（同 init --auto 后的视图）
- `--full` → 完整 yaml（pipe 友好；同 `[S]` 键）
- `--reasoning` → 只显示 provenance.reasoning_decisions（同 `[R]` 键）
- `--warnings` → 只显示 translation warnings（同 `[W]` 键）
- `--persona` → 只显示 system_prompt + persona_file 内容（同 `[P]` 键）
- `--deps` → 完整 Python deps 列表（同 `[D]` 键）
- `--mcp` → MCP 完整列表 + 被过滤的（同 `[M]` 键）

### 3.4 `askdao agent regenerate`（init 后再扫）

KOL 项目演进后想刷新 yaml 推荐。读 `.askdao/detection.json` 做 diff，提示哪些字段变了。

### 3.5 `askdao agent deploy [--harness <id>]`

按 yaml 的 `preferred_harness`（或命令行 `--harness` 覆盖）选择 conductor 端 adapter：

- **AnthropicAdapter**（Phase 1 + 之后）：environment.create → agent.create → 写回 conductor PG
- **OpenAIAdapter**（Phase 2 启用）：上传 manifest 到 conductor → conductor 内存实例化 SandboxAgent → 写回 conductor PG

**v0.5 加 diff preview**：KOL 改了 yaml 后，deploy 时显示与原推荐版本的差异：

```
$ askdao agent deploy

→ Reading my-agent/agent.yml ...
→ You modified 2 fields since the last recommendation:

  persona.model_preferences[0].id:
    -  claude-opus-4-6
    +  claude-sonnet-4-6
    
  capabilities.shell.permission:
    -  ask_for_dangerous
    +  always_allow

→ Impact on translation report:
  • OK: both fields supported by anthropic_managed_agents
  • Warning (medium): always_allow + production deploy detected
    → subscribers won't be asked before bash runs

  [A] Approve and deploy   [E] Edit again   [Q] Cancel
```

deploy 失败的常见情形：
- `preferred_harness=openai_agents_sdk` 但 conductor 部署版本 < Phase 2 → 报错并提示切换
- 中间格式里有某 harness 不支持的字段（如 OpenAI 不支持 Anthropic `fast_mode`）→ adapter 输出 translation report，KOL 决定继续或修改

---

## 4. detection.json schema（L1-L3 产物）

确定性结果，离线可重现。落盘到 `<agent_dir>/.askdao/detection.json`。

```json
{
  "schema_version": "askdao/detection/v1",
  "generated_at": "2026-05-06T14:23:11Z",
  "generator_version": "askdao-cli/0.1.0",
  
  "scan": {
    "root": "/Users/sunmu/WorkSpace/my-fastapi-project",
    "is_git_repo": true,
    "git_remote": "github.com/sunmu/my-fastapi-project",
    "total_files": 1247,
    "excluded_paths": ["./openviking/**", "node_modules/**", ".git/**"],
    "scan_duration_ms": 1832
  },

  "detected_languages": [
    { "language": "Python", "bytes": 145239, "percentage": 67.4, "files": 47 },
    { "language": "TypeScript", "bytes": 56812, "percentage": 26.4, "files": 23 }
  ],

  "detected_runtimes": [
    { "kind": "python", "version": "3.12",
      "source": "pyproject.toml", "constraint": ">=3.12,<3.14" },
    { "kind": "node", "version": "22",
      "source": ".nvmrc", "constraint": "22" }
  ],

  "detected_manifests": [
    { "manifest": "pyproject.toml", "package_manager": "uv",
      "lockfile": "uv.lock",
      "direct_prod_deps": 28, "direct_dev_deps": 14, "transitive_deps": 247 }
  ],

  "detected_packages": {
    "pip": [
      { "name": "fastapi", "version": "0.135.1", "is_prod": true },
      { "name": "sqlalchemy", "version": "2.0.48", "is_prod": true },
      { "name": "pytest", "version": "9.0.2", "is_prod": false },
      ...
    ],
    "npm": [...]
  },

  "detected_frameworks": [
    { "name": "FastAPI", "confidence": 0.95,
      "evidence": ["uses_dep:fastapi", "import_pattern:from fastapi import"] },
    { "name": "SQLAlchemy", "confidence": 0.92, "evidence": [...] },
    { "name": "Alembic", "confidence": 0.88, "evidence": [...] }
  ],

  "detected_dockerfile": {
    "exists": true,
    "path": "./Dockerfile",

    // —— 完整 AST（v0.4 新增）
    "stages": [
      { "from": "node:20", "as": "builder",
        "commands": [
          { "instruction": "WORKDIR", "value": "/app" },
          { "instruction": "COPY", "value": ". ." },
          { "instruction": "RUN", "value": "npm ci && npm run build" }
        ] },
      { "from": "python:3.12-slim", "as": null,
        "commands": [
          { "instruction": "WORKDIR", "value": "/app" },
          { "instruction": "COPY", "value": "--from=builder /app/dist /app" },
          { "instruction": "RUN", "value": "apt-get update && apt-get install -y libpq-dev gcc && rm -rf /var/lib/apt/lists/*" },
          { "instruction": "RUN", "value": "pip install --no-cache-dir -r requirements.txt" },
          { "instruction": "USER", "value": "appuser" },
          { "instruction": "EXPOSE", "value": "8000" },
          { "instruction": "CMD", "value": "[\"uvicorn\", \"app.main:app\", \"--host\", \"0.0.0.0\"]" }
        ] }
    ],
    "final_stage_name": null,        // 多阶段时 --target 默认值；最后一段无 AS 时为 null
    "base_image": "python:3.12-slim",  // 最终阶段的 FROM
    
    // —— 各 instruction 抽出的字段
    "run_commands": [
      "npm ci && npm run build",
      "apt-get update && apt-get install -y libpq-dev gcc && rm -rf /var/lib/apt/lists/*",
      "pip install --no-cache-dir -r requirements.txt"
    ],
    "users": [
      { "name": "appuser", "uid": null, "gid": null }   // 从 RUN useradd / USER 抽
    ],
    "workdir": "/app",                 // 最终 WORKDIR
    "env_vars": {                      // ENV 抽出（不含 secrets）
      "PYTHONUNBUFFERED": "1"
    },
    "exposed_ports": [8000],
    "cmd": ["uvicorn", "app.main:app", "--host", "0.0.0.0"],
    "entrypoint": null,
    "build_args": [],                  // ARG 列表
    
    // —— 自动提取产物（喂给 yaml workspace）
    "extracted_apt_packages": ["libpq-dev", "gcc"],          // 从 RUN apt-get install
    "extracted_pip_packages": [],                              // 从 RUN pip install（仅明确写出包名的）
    "extracted_setup_commands": [                              // 无法归到 packages 的 RUN
      "rm -rf /var/lib/apt/lists/*"
    ],
    
    // —— v0.4 标记 Anthropic 兼容性
    "anthropic_compatible_warnings": [
      { "field": "stages", "issue": "multi-stage build not supported; only final stage's packages can be migrated" },
      { "field": "users", "issue": "USER appuser ignored; Anthropic runs as fixed user" },
      { "field": "exposed_ports", "issue": "EXPOSE 8000 ignored; no port preview" }
    ]
  },

  "detected_external_services": [
    { "service": "PostgreSQL", "confidence": 0.95,
      "evidence": ["uses_dep:asyncpg", "env_key:DATABASE_URL"] },
    { "service": "Anthropic API", "confidence": 0.99,
      "evidence": ["uses_dep:anthropic", "env_key:ANTHROPIC_API_KEY"] }
  ],

  "detected_env_files": [
    { "path": ".env.example",
      "declared_keys": ["ANTHROPIC_API_KEY", "DATABASE_URL"] }
  ],

  "inferred_apt_packages": [
    { "name": "libpq-dev", "reason": "Python deps psycopg2/asyncpg need postgres headers" },
    { "name": "gcc", "reason": "Python C extensions compile" },
    { "name": "libjpeg-dev", "reason": "Pillow image processing" }
  ],

  "repository_layout": {
    "layout": "single",
    "workspaces": []
  },

  "detected_mcp_configs": [
    { "source": ".mcp.json",
      "servers": [
        { "name": "github", "type": "url",
          "url": "https://api.githubcopilot.com/mcp/",
          "anthropic_compatible": true },
        { "name": "filesystem", "type": "stdio",
          "command": "mcp-server-filesystem",
          "anthropic_compatible": false,
          "warning": "Anthropic Managed Agents only supports type=url; stdio MCP cannot be deployed" }
      ] }
  ],

  "detected_skills": [
    { "source": ".claude/skills/portfolio-analyzer/SKILL.md",
      "skill_name": "portfolio-analyzer",
      "kind": "custom_local",
      "size_bytes": 4231 },
    { "implied_anthropic_skills": [
        { "skill_id": "xlsx",
          "reason": "detected dependency: pandas + openpyxl",
          "confidence": 0.85 }
      ] }
  ],

  "detected_required_secrets": [
    { "name": "ANTHROPIC_API_KEY",
      "from": ".env.example",
      "purpose_guess": "Anthropic API authentication",
      "used_by_guess": null,
      "required": true },
    { "name": "GITHUB_TOKEN",
      "from": ".env.example",
      "purpose_guess": "GitHub MCP server authentication",
      "used_by_guess": { "mcp_server": "github" },
      "required": true },
    { "name": "DATABASE_URL",
      "from": ".env.example",
      "purpose_guess": "PostgreSQL connection string (project-internal, not for agent runtime)",
      "used_by_guess": null,
      "required": false,
      "note": "Likely development-only, may not need to enter vault" }
  ],

  "detected_tool_risk_hints": {
    "production_signals": [
      { "signal": "AWS deploy workflow detected", "evidence": ".github/workflows/deploy.yml" },
      { "signal": "Production env file referenced", "evidence": "config/production.toml" }
    ],
    "user_data_signals": [],
    "recommended_default_policy": "always_allow",
    "tool_overrides_recommended": [
      { "tool": "bash", "policy": "always_ask",
        "reason": "Production deploy detected; bash should require approval" },
      { "tool": "write", "policy": "always_ask",
        "reason": "Production deploy detected; write should require approval" }
    ]
  },

  "detected_harness_signals": {
    "claude_code": {
      "installed": true,
      "evidence": ["~/.claude/ exists", "~/.claude/skills/ has 3 SKILL.md"]
    },
    "codex": {
      "installed": false,
      "evidence": []
    },
    "cursor": {
      "installed": false,
      "evidence": []
    },
    "gemini_cli": {
      "installed": false,
      "evidence": []
    },
    "recommended_harness": "anthropic_managed_agents",
    "recommendation_reason": "Claude Code is the primary local harness on this machine; Anthropic Managed Agents is the natural cloud counterpart"
  }
}
```

**字段来源对应表**：

| 字段 | 来源 | 工具 |
|------|------|------|
| `detected_languages` | enry 字节级语言识别 | `go-enry/enry/v2` |
| `detected_runtimes` | `.nvmrc` / `.python-version` / `rust-toolchain.toml` 解析 | 自己写 |
| `detected_manifests` | manifest 主文件解析（dev/prod 区分） | 自己写 |
| `detected_packages` | syft + dev-filter 标注 | `anchore/syft` + 自己写 |
| `detected_frameworks` | nixpacks-port providers 启发式 | 移植 |
| `detected_dockerfile` | Dockerfile **完整 AST**：stages / RUN / USER / WORKDIR / ENV / EXPOSE / CMD / ENTRYPOINT / ARG + 提取产物（apt/pip/setup_commands） | `moby/buildkit` parser + 自己写抽取规则（v0.4 升级） |
| `detected_external_services` | 依赖+import+.env 三源交叉 | 自己写（30 行规则） |
| `detected_env_files` | dotenv 解析（仅读 keys，不读 values） | 自己写 |
| `inferred_apt_packages` | nixpacks 反向映射表 | 移植 |
| `repository_layout` | 工作区文件检测 | 自己写 |
| **🆕 `detected_mcp_configs`** | `.mcp.json` / `claude_desktop_config.json` JSON 解析 + Anthropic 兼容性检测 | 自己写（~60 行 Go） |
| **🆕 `detected_skills`** | 5+ 个 skill dir 约定（`.claude/skills/` / `.agents/skills/` ...）+ 依赖反推内置 skill | 自己写（~80 行 Go） |
| **🆕 `detected_required_secrets`** | `.env.example` keys + service mapping → MCP server 反查 | 自己写（~120 行 Go） |
| **🆕 `detected_tool_risk_hints`** | 生产信号检测（deploy.yml / prod config）+ 用户数据信号 → permission_policy | 自己写（~100 行 Go） |
| **🆕 `detected_harness_signals`** | 探查 `~/.claude/` / `~/.codex/` / `~/.cursor/` 等 dir 是否存在 + 推荐 harness | 自己写（~60 行 Go） |

---

## 5. agent.yml schema（v0.3 中间格式 · harness-neutral）

`init --auto` 最终落盘的 yaml 是 **askdao 自定义中间格式**（`apiVersion: askdao.ai/v1` + `kind: AgentSpec`），不直接对齐任何 harness SDK 的字段命名。conductor 端的 adapter 负责翻译到具体 harness API。

### 5.1 顶层结构（harness-neutral 8 块）

```
agent.yml (apiVersion: askdao.ai/v1)
├── metadata           # name / description / visibility / domain / ...
├── persona            # model_class + model_preferences + system_prompt
├── capabilities       # shell / filesystem / web / code_execution（语义 + 权限）
├── mcp_servers        # 标准 MCP 协议（两边直通）
├── custom_tools       # name + schema + handler（adapter 翻译执行机制）
├── skills             # builtin / custom_local / git_repo
├── workspace          # packages + mounts + networking + environment_vars
├── vault_hints        # 订阅者 onboarding 必填 secrets
├── preferred_harness  # auto / anthropic_managed_agents / openai_agents_sdk
├── fallback_harnesses # Phase 2 启用
└── harness_specific   # escape hatch: { anthropic: {...}, openai: {...} }
```

**与 Anthropic / OpenAI SDK 的映射**（conductor 端 adapter 完成）：

```
askdao agent.yml                  AnthropicAdapter        OpenAIAdapter
─────────────────                 ────────────────        ─────────────
metadata, persona       ───►      Agent.name/system        SandboxAgent.name/instructions
capabilities            ───►      tools[*].configs         Capabilities + tools list
mcp_servers             ───►      mcp_servers (1:1)        mcp_servers (1:1)
custom_tools            ───►      tools[type=custom]       function_tool decorators
skills                  ───►      skills[type=anthropic]   Skills capability
                                  + custom upload          + GitRepo/LocalDir
workspace.packages      ───►      Environment.packages     Manifest setup commands
workspace.mounts        ───►      ✗ 不支持，translation     Manifest GitRepo/S3Mount
                                    report 警告
workspace.networking    ───►      Environment.networking   provider-specific
vault_hints             ───►      Vault per-user 资源       Manifest.environment + provider
preferred_harness       ───►      adapter 路由              adapter 路由
harness_specific.openai ───►      ✗ 忽略                    sandbox_provider / compaction
harness_specific.anthropic ───►   fast_mode / callable      ✗ 忽略
```

### 5.2 完整 yaml 示例

```yaml
# my-agent/agent.yml (auto-generated draft, KOL should review/edit)
apiVersion: askdao.ai/v1
kind: AgentSpec

# ============================================================
# metadata（业务标识，与 harness 无关）
# ============================================================
metadata:
  name: my-agent
  description: "..."                 # KOL 给订阅者看的介绍
  version: 0.1.0
  visibility: private                # private | shared | public（spec/02 §1.2 line 48；askdao-cli#28）
  expertise_level: pro
  domain:
    - backend-engineering            # LLM 从 frameworks 反推
  group_name: "My Agent Group"
  persona_file: persona.md           # 长篇 persona（plan/06 已有概念）
  labels:
    askdao.kol_id: "kol_sam"
    askdao.pricing_tier: "paid"

# ============================================================
# persona · 模型 + 角色（语义层）
# ============================================================
persona:
  model_class: high_reasoning        # high_reasoning / balanced / fast / multimodal / coding
  model_preferences:                 # 顺序优先；adapter 选第一个其 runtime 支持的
    - { provider: anthropic, id: claude-opus-4-6, speed: standard }
    - { provider: openai,    id: gpt-5.4 }
    - { provider: anthropic, id: claude-sonnet-4-6 }   # fallback
  
  system_prompt: |
    You are an AI assistant for {KOL name}, helping with backend engineering
    topics including FastAPI, SQLAlchemy migrations, and Postgres ops.
    
    Execution rules:
    - Use shell/filesystem tools to create deliverable files in ./output/
    - When users ask about migrations, always suggest a dry-run first
    ...

# ============================================================
# capabilities · 语义化能力 + 权限策略
# adapter 翻译：Anthropic 端 → tools[*].configs；OpenAI 端 → Capabilities
# ============================================================
# capabilities 是 hard field —— 由 askdao-cli recommender.DefaultCapabilities()
# 确定性生成、不交 LLM 即兴（§9.13，同 skills）。scopes 是固定的 harness-neutral
# 词表（非自由文本）：Anthropic 当前忽略 scopes（工具配置无 scoping 原语，adapter
# emit IGNORED warning），未来 harness（OpenAI/E2B）可强制。
capabilities:
  shell:
    enabled: true
    permission: always_allow         # 有 production signals 时收紧为 ask_for_dangerous
    scopes: [read, write, execute]
  filesystem:
    enabled: true
    permission: always_allow
    scopes: [read, write]
  web:
    enabled: true                    # web search + web fetch
    permission: always_allow
    scopes: [fetch]
  code_execution:
    enabled: true                    # python / sandbox 执行
    permission: always_allow
    scopes: [javascript, shell]

# ============================================================
# mcp_servers · 标准 MCP 协议（两边直通）
# ============================================================
mcp_servers:
  - name: github
    type: url
    url: "https://api.githubcopilot.com/mcp/"
  # 注：detected_mcp_configs 中 type=stdio 的 server 在此处过滤掉
  # （Anthropic 不支持 stdio；OpenAI 端可在 harness_specific.openai 启用）

# ============================================================
# custom_tools · schema 中性，handler 由 adapter 适配
# ============================================================
custom_tools: []                     # MVP 空；KOL 后续可手加

# ============================================================
# skills · 多源
# ============================================================
skills:
  - type: builtin
    provider: anthropic              # 当前仅 anthropic 提供 builtin skills
    id: xlsx                         # detected pandas+openpyxl → xlsx
  
  - type: custom_local
    path: ./skills/portfolio-analyzer
    # adapter 行为：Anthropic 端 → 上传到 Anthropic Skills；
    #              OpenAI 端 → 物化进 sandbox manifest

# ============================================================
# workspace · 运行时环境配置（替代 v0.2 environment）
# ============================================================
workspace:
  # —— v0.4 新增：Dockerfile 兼容字段（OpenAI adapter 消费；Anthropic adapter 忽略 + 警告）
  base_image: null                   # 自定义基础镜像；如 "python:3.12-slim" / "pytorch/pytorch:2.1-cuda12"
                                     # null 表示用 adapter 默认
                                     # Anthropic 端：忽略 + warn (cloud type 强制默认镜像)
                                     # OpenAI 端：DockerSandboxClient(image=...) 参数
  
  workdir: /app                      # 工作目录；两边都用
  
  setup_commands: []                 # 命令式编译/配置补充
  # 示例（来自 detected_dockerfile.extracted_setup_commands）：
  # - "git clone https://github.com/foo/native-lib && cd native-lib && cmake . && make install"
  # Anthropic 端：忽略 + warn（仅声明式 packages 支持；可能从命令里抽 apt/pip 兜底）
  # OpenAI 端：进 Manifest setup phase 顺序执行
  
  users: []                          # OS users / groups 创建
  # 示例（来自 detected_dockerfile.users）：
  # - { name: appuser, uid: 1000, gid: 1000 }
  # Anthropic 端：忽略 + warn（runs as fixed user）
  # OpenAI 端：Manifest.users / groups
  
  exposed_ports: []                  # 端口暴露（用于 web service 预览）
  # 示例：[8000, 5432]
  # Anthropic 端：忽略 + warn（no port preview）
  # OpenAI 端：exposed ports capability + SandboxClient port forwarding
  
  startup_command: null              # ENTRYPOINT/CMD 参考；agent runtime 通常不需要
                                     # 留位为后续 long-running service 类 agent 用

  # —— packages（两边都能转）
  packages:
    pip:
      - fastapi==0.135.1
      - sqlalchemy==2.0.48
      - asyncpg==0.31.0
      - alembic==1.18.4
      - anthropic==0.97.0
      # ... 已自动过滤 pytest/mypy/ruff 等 dev 依赖
    apt:
      - libpq-dev                    # 来自 inferred_apt_packages + dockerfile.extracted_apt_packages
      - gcc
      - libjpeg-dev
  
  mounts: []                         # OpenAI SDK Manifest 用；Anthropic adapter 跳过
  # 示例（Phase 2 OpenAI 端可用）：
  # - { type: git_repo, repo: "owner/agent-data", ref: main, dest: ./repo }
  # - { type: s3, bucket: kol-bucket, key: data/, dest: ./data, mode: ro }
  
  networking:
    mode: limited                    # limited / unrestricted
    allowed_hosts:
      - api.anthropic.com
      - api.openai.com
    allow_mcp_servers: true          # 是否放行 mcp_servers 列表里的 url
    allow_package_managers: false    # 运行时是否允许 pip install / npm install
  
  environment_vars:                  # 启动时注入（不含 secrets）
    LOG_LEVEL: info

# ============================================================
# vault_hints · 订阅者 onboarding 时引导填的 secrets
# ============================================================
vault_hints:
  required_credentials:
    - name: GITHUB_TOKEN
      purpose: "MCP github server 认证"
      used_by: { mcp_server: github }
      from: .env.example
      required: true
    - name: ANTHROPIC_API_KEY
      purpose: "LLM 调用（若 KOL 走 BYOK 而非 platform 代收）"
      used_by: { agent: true }
      from: .env.example
      required: false
      note: "If subscribers use platform-managed billing, this is auto-injected"
  
  optional_credentials: []

# ============================================================
# preferred_harness · 部署目标 runtime
# ============================================================
preferred_harness: anthropic_managed_agents
# 当前 Phase 1 唯一可用 adapter；Phase 2 可改为 openai_agents_sdk

fallback_harnesses: []
# Phase 2 启用：例如 [openai_agents_sdk] 表示首选失败时退到此

# ============================================================
# harness_specific · escape hatch（中间格式无法表达的特性）
# adapter 只读自己 provider 那一块
# ============================================================
harness_specific:
  anthropic:
    fast_mode: false                 # speed=fast 走 premium pricing
    callable_agents: []              # multi-agent 编排（research preview）
    metadata:                        # Anthropic Agent.metadata 直传
      askdao.kol_id: "kol_sam"
  
  openai:
    sandbox_provider: docker         # docker / e2b / modal / unix_local / daytona / vercel
    compaction:
      enabled: true
      trigger_at_tokens: 100000

# ============================================================
# memory / guardrails · conductor 业务字段（不上送任何 harness）
# ============================================================
memory:
  fact_extraction: enabled
  episode_summary: enabled

guardrails:
  credential_filter: enabled
  kol_memory_redact: enabled

# ============================================================
# provenance · 透明度
# ============================================================
provenance:
  detection_report: .askdao/detection.json
  
  reasoning_summary: |
    项目是 Python 3.12 FastAPI 后端 + Postgres。检测到 Claude Code 已装于此机器，
    推荐 anthropic_managed_agents 作为 preferred_harness。LLM 推断 model_class=
    high_reasoning（多步推理），首选 claude-opus-4-6。检测到生产部署文件，
    capabilities.shell.permission=ask_for_dangerous（adapter 翻译为 bash 工具
    单独 always_ask）。
  
  reasoning_decisions:
    - decision: "preferred_harness=anthropic_managed_agents"
      reason: "Claude Code installed locally; Anthropic 是自然延伸"
      confidence: 0.85
    - decision: "model_preferences[0]=claude-opus-4-6"
      reason: "FastAPI + Alembic 迁移逻辑复杂，需要 high_reasoning"
      confidence: 0.78
    - decision: "capabilities.shell.permission=ask_for_dangerous"
      reason: "检测到 .github/workflows/deploy.yml + config/production.toml"
      confidence: 0.85
  
  generated_at: 2026-05-06T14:23:11Z
  generator_version: askdao-cli/0.3.0

# ============================================================
# status · apply 后回写
# ============================================================
status:
  last_applied_at: null
  active_harness: null               # apply 时记录实际使用的 adapter
  remote_ids:
    # 各 harness 各自的远端 ID（adapter 写入对应键）
    anthropic_agent_id: null         # 例如 "agt_xyz789"
    anthropic_agent_version: null    # versioned
    anthropic_environment_id: null   # 例如 "env_abc123"
    openai_session_state_id: null    # OpenAI 端无 versioned agent，存 RunState ref
  vault_setup_complete: false
  drift_detected: false
```

### 5.3 与 plan/06 §5 的兼容性

- 旧字段（visibility / expertise_level / group_name / persona_file / memory / guardrails）保留，移入 `metadata` / 顶层
- **结构性变更**：旧的扁平字段全部进 `persona` / `capabilities` / `workspace` 等中间格式块
- conductor 端 pydantic AgentSpec 模型需重写为中间格式 schema（不再直接对应 Anthropic SDK 字段）
- plan/06 §5 整段示例 yaml 需要重写

### 5.4 与 conductor 的衔接（Phase 1 / Phase 2）

**Phase 1**（仅 AnthropicAdapter）：
- `askdao agent deploy` → conductor 收 yaml → AnthropicAdapter 翻译：
  - 调 `POST /v1/environments`（来自 `workspace`）
  - 调 `POST /v1/agents`（来自 `persona` + `capabilities` + `mcp_servers` + `custom_tools` + `skills` + `harness_specific.anthropic`）
  - 写回 `agent_spec` 表
- conductor PG `agent_spec` 表需 alembic 017 加列：
  - `managed_agent_version: int NOT NULL DEFAULT 1`
  - `vault_hints_json: jsonb`
  - `runtime_id: text NOT NULL DEFAULT 'anthropic_managed_agents'`（Phase 2 用）
- vault 不在 deploy 时创建，KOL onboarding 订阅者时引导填

**Phase 2**（加 OpenAIAdapter）：
- `askdao agent deploy --harness openai_agents_sdk` → conductor 收 yaml → OpenAIAdapter 翻译：
  - 解析 `persona` → SandboxAgent.instructions / model_preferences[0..n]
  - 解析 `capabilities` → Capabilities 列表
  - 解析 `workspace` → Manifest（packages 转 setup commands；mounts 直接转 GitRepo/S3Mount；users / workdir / exposed_ports / setup_commands 直传）
  - 解析 `harness_specific.openai.sandbox_provider` → 选 SandboxClient
  - 写回 `agent_spec` 表（`runtime_id='openai_agents_sdk'`）
- chat.py 三岔路径（managed_agents / openai_sdk / sandbox_template）

### 5.5 Translation Report（v0.4 新增）

每次 `askdao agent deploy` 时，conductor 端 adapter 把无法承载的字段输出为 translation report，KOL 在 deploy 输出 + status 文件中可见。

```json
{
  "harness": "anthropic_managed_agents",
  "translation_warnings": [
    {
      "field": "workspace.base_image",
      "value": "pytorch/pytorch:2.1.0-cuda12.1-cudnn8-runtime",
      "action": "ignored",
      "reason": "Anthropic Managed Agents uses fixed cloud container image; custom base image not supported",
      "severity": "high"
    },
    {
      "field": "workspace.setup_commands",
      "count": 3,
      "action": "partial",
      "reason": "Anthropic Managed Agents only supports declarative packages; imperative setup commands cannot run",
      "severity": "high",
      "fallback_attempted": "extracted apt/pip names from commands and merged into workspace.packages"
    },
    {
      "field": "workspace.users",
      "count": 1,
      "action": "ignored",
      "reason": "Anthropic Managed Agents runs as fixed user",
      "severity": "medium"
    },
    {
      "field": "workspace.exposed_ports",
      "value": [8000],
      "action": "ignored",
      "reason": "Anthropic Managed Agents does not support port preview; use OpenAI SDK + DockerSandboxClient for that",
      "severity": "low"
    }
  ]
}
```

**KOL 看到 high severity warning 时的处置**：
1. 接受降级：忽略警告继续 deploy
2. 切换 harness：改为 `--harness openai_agents_sdk`（Phase 2 起可用）
3. 修改 yaml：删掉无法承载的字段

**adapter 行为约定**：
- `severity: high` —— 必显示，KOL 必须明确确认才能 deploy
- `severity: medium` —— 默认显示，KOL 可加 `--quiet-medium` 隐藏
- `severity: low` —— 仅 `--verbose` 模式显示
- `action: ignored` —— 字段完全无效
- `action: partial` —— 部分能力降级（如 setup_commands 抽出 apt/pip 名）
- `fallback_attempted` —— adapter 已尝试的兜底动作（透明告诉 KOL）

---

## 6. 工程量估算（分 askdao-cli + conductor 两侧）

### 6.1 askdao-cli 端（Go）

| 模块 | 来源 | 估算 |
|-----|------|------|
| `internal/scanner/syft.go` | wrap `anchore/syft` | 80 行 |
| `internal/scanner/enry.go` | wrap `go-enry/enry` | 50 行 |
| `internal/scanner/dockerfile.go` | wrap `moby/buildkit` parser + 完整 AST 抽取（stages / RUN / USER / WORKDIR / ENV / EXPOSE / CMD / ENTRYPOINT / ARG）+ extracted_apt/pip/setup_commands 提取规则 | 200 行（v0.3: 80，**+120 因 v0.4 升级**） |
| `internal/scanner/dev_filter.go` | manifest 主文件 dev/prod 区分（Python/Node/Rust） | 200 行 |
| `internal/scanner/runtimes.go` | `.nvmrc` / `.python-version` etc. 解析 | 80 行 |
| `internal/scanner/mcp_config.go` | `.mcp.json` / `claude_desktop_config.json` 解析 + 标记 anthropic_compatible | 60 行 |
| `internal/scanner/skills_dir.go` | 5+ 个 skill dir 约定扫描 + 依赖反推 builtin skill | 80 行 |
| `internal/scanner/secrets_hint.go` | `.env.example` keys 抽取 + service-to-secret 映射 | 120 行 |
| **🆕 `internal/scanner/harness_signals.go`** | 探查 `~/.claude/` / `~/.codex/` / `~/.cursor/` 推荐 harness | 60 行 |
| `internal/providers/provider.go` | Provider interface + App/Env 抽象 | 120 行 |
| `internal/providers/python.go` | 移植 nixpacks python.rs | 250 行 |
| `internal/providers/node.go` | 移植 nixpacks node/mod.rs | 300 行 |
| `internal/providers/go.go` | 移植 | 150 行 |
| `internal/providers/rust.go` | 移植 | 150 行 |
| `internal/providers/apt_map.go` | 反向映射表（数据为主） | 100 行 |
| `internal/recommender/policy.go` | tool permission_policy 启发式 | 100 行 |
| `internal/recommender/llm.go` | 调 conductor LLM endpoint 生成中间格式 yaml | 280 行（v0.2: 250，+30 因 model_preferences 等多源选择） |
| `internal/types/detection.go` | detection.json schema（含 5 个新字段） | 220 行（v0.2: 200, +20 加 harness_signals） |
| **🆕 `internal/types/agent_spec.go`** | 中间格式 yaml schema（apiVersion + 8 块 + escape hatch） | 400 行（v0.2 agent_yml.go: 350，**重写**为中间格式） |
| `cmd/askdao/init_auto.go` | 命令实现（带 `--harness` + **v0.5 交互式 [A/E/R/S/D/F/M/W/P/Q]**） | 230 行（v0.4: 180, +50） |
| `cmd/askdao/detect.go` | 命令实现 | 80 行 |
| `cmd/askdao/deploy.go` | 命令实现（带 `--harness`，调 conductor adapter + **v0.5 diff preview**） | 200 行（v0.4: 150, +50） |
| **🆕 `cmd/askdao/show.go`** | show 命令（subcmd D/F/M/W/P 分发） | 120 行 |
| **🆕 `internal/render/summary.go`** | 中等详情卡片渲染器（7 块 + box drawing + 截断策略） | 320 行 |
| **🆕 `internal/render/reasoning.go`** | inline reasoning（`↳ Why:` 引导符 + confidence 颜色） | 100 行 |
| **🆕 `internal/render/diff.go`** | yaml diff（`github.com/r3labs/diff` 集成） | 100 行 |
| **🆕 `internal/render/warnings.go`** | translation warnings 渲染（severity 颜色分组） | 80 行 |
| **🆕 `internal/render/lists.go`** | 通用 "前 N + and M more" 列表渲染器 | 100 行 |
| askdao-cli 端总计 | | **~4280 行 Go** |

vs v0.4（~3410 行）：增量 ~870 行（5 个 render 模块 + show 命令 + init/deploy 改造）。
vs v0.3（~3290 行）：累计增量 ~990 行。
vs v0.2（~2950 行）：累计增量 ~1330 行。

**askdao-cli 端 Phase 1 工期估算**：仍在 3-4 周区间（render 模块都是数据驱动 + Go 模板字符串，比 scanner / provider 移植轻）。

### 6.2 conductor 端 · Phase 1（AnthropicAdapter）

| 模块 | 内容 | 估算 |
|-----|------|------|
| `app/agents/spec.py` | AgentSpec pydantic 模型（中间格式） | 400 行（含 validation） |
| `app/agents/adapters/anthropic_adapter.py` | 中间格式 → Anthropic Agent + Environment + skill upload + **v0.4: extracted_* 字段兜底合并到 packages + translation_report 输出** | 500 行（v0.3: 400, +100 因 v0.4 加合并 + report） |
| `app/agents/adapters/translation_report.py` | adapter 共用：lossy translation 警告格式 | 100 行 |
| `app/api/cli.py` | `POST /api/v1/cli/recommend` + `POST /api/v1/cli/deploy` | 200 行 |
| 测试 | | 200 行 |
| alembic 017 | 加 `managed_agent_version` + `vault_hints_json` + `runtime_id` 列 | 50 行 |
| conductor 端 Phase 1 总计 | | **~1450 行 Python**（v0.3: 1350, +100 因 v0.4 anthropic_adapter 合并 + report） |

**conductor 端 Phase 1 工期估算**：~1-2 周（adapter 是核心，其他都是 boilerplate）。

### 6.3 conductor 端 · Phase 2（OpenAIAdapter）

| 模块 | 内容 | 估算 |
|-----|------|------|
| `app/agents/adapters/openai_adapter.py` | 中间格式 → SandboxAgent + Manifest + Capabilities | 500 行 |
| `app/openai_sdk/client.py` | wrap `Runner.run_streamed()`，对齐现有 SSE 输出 | 400 行 |
| `app/openai_sdk/session.py` | 实现 `Session` Protocol 对接 OpenViking MemoryProvider | 200 行 |
| `app/openai_sdk/sandbox_router.py` | 把 Manifest 落到 E2B / Docker / UnixLocal 选择 | 300 行 |
| `app/api/chat_openai.py` | OpenAI 路径的流式 endpoint（chat.py 三岔之一） | 400 行 |
| `app/artifacts/sweeper_openai.py` | 文件系统型 artifact 回收（不是 Files API） | 300 行 |
| 测试 + 集成 | | ~400 行 |
| conductor 端 Phase 2 总计 | | **~2500 行 Python** |

**conductor 端 Phase 2 工期估算**：4-6 周（含与现有 chat.py 的三岔重构 + 测试）。

### 6.4 总览

| 阶段 | 范围 | 工期 |
|-----|------|------|
| Phase 1 | askdao-cli ~4280 行 Go + conductor ~1450 行 Python | 5-6 周（两条流水线并行；v0.4 + v0.5 增量都在区间内消化） |
| Phase 2 | conductor ~2500 行 Python（OpenAIAdapter） | 4-6 周 |
| Phase 3 | 更多 harness（按需） | 不在当前估算 |

---

## 7. 三阶段路线图

### Phase 1 · 中间格式 + 单 adapter（即将做，不阻塞 askdao-cli MVP）

**askdao-cli 端**：
- ✅ L1：syft 调 CLI 进程模式
- ✅ L2：Python (uv/poetry/pip-tools) + Node (npm/pnpm/yarn) dev/prod 过滤
- ✅ L3：nixpacks 移植 4 provider（python/node/go/rust）+ apt 反向映射
- ✅ L4：调 conductor LLM endpoint 生成中间格式 yaml
- ✅ 四个命令：`init --auto` / `detect` / `deploy` / `show`（v0.5 加 show）
- ✅ 5 个 scanner 含 `harness_signals.go`
- ✅ Dockerfile 完整 AST 解析（v0.4 升级；含 stages / RUN / USER / WORKDIR / EXPOSE / extracted_*）
- ✅ yaml 即中间格式 + 5 个 workspace Dockerfile 兼容字段（base_image / setup_commands / users / workdir / exposed_ports）
- ✅ `preferred_harness` 仅 `anthropic_managed_agents`
- ✅ **v0.5 中等详情卡片 UX**：7 块顶层（Persona / Skills / MCP / Capabilities / Runtime / Onboarding / Warnings）+ inline reasoning（`↳ Why:`）+ 入口扩展（[A/E/R/S/D/F/M/W/P/Q]）
- ✅ **v0.5 deploy diff preview**：KOL 改 yaml 后显示与原推荐的差异 + 对 translation_report 的影响
- ✅ **v0.7.1 deploy update-mode**（2026-05-19，ADR-P19 从 P2 升 P0 落地）：conductor `/cli/deploy` 加 lookup-then-create-or-update 分支。Dedup key = `(owner_id, yaml.metadata.name)`，KOL scope。命中既有 row → `environments.update` + `agents.update`（乐观锁 retry once）in-place，复用 agent_id/group_id/created_at；不命中走原 create。alembic 029 partial unique index 同时防并发 deploy race。cli `DeployResponse` 加 `created` / `previous_managed_version`，终端区分 `Created new agent.` vs `Updated existing agent (vN → vN+1).`。详 `docs/update-mode-handoff.md`

**conductor 端**：
- ✅ AgentSpec pydantic 模型（中间格式 + workspace 5 字段）
- ✅ AnthropicAdapter（中间格式 → Anthropic 三资源 API）
- ✅ AnthropicAdapter 加 translation_report 输出（Dockerfile 字段忽略警告）+ extracted_apt/pip 兜底合并
- ✅ alembic 017 加 3 列（`managed_agent_version` + `vault_hints_json` + `runtime_id`）
- ✅ `POST /api/v1/cli/recommend` + `POST /api/v1/cli/deploy`（含 translation_report 返回）

**Phase 1 总工期**：5-6 周（两条流水线并行；v0.4 + v0.5 增量都在区间内消化）

### Phase 2 · 加 OpenAIAdapter（开源前必做）

**conductor 端**：
- ⏳ OpenAIAdapter（中间格式 → SandboxAgent + Manifest）
- ⏳ OpenAI adapter 真正消费 v0.4 加的 5 个 workspace 字段（base_image → DockerSandboxClient image / setup_commands → Manifest setup phase / users → Manifest.users / workdir → Manifest workdir / exposed_ports → exposed ports capability）
- ⏳ `app/openai_sdk/` 全新建（client / session / sandbox_router / chat_handler / artifact_sweeper）
- ⏳ chat.py 三岔（managed_agents / openai_sdk / sandbox_template）
- ⏳ 多 sandbox provider 支持（Phase 2.1: docker / unix_local；Phase 2.2: e2b / modal）

**askdao-cli 端**：
- ⏳ yaml `preferred_harness` 加 `openai_agents_sdk` 选项
- ⏳ deploy 命令 `--harness openai_agents_sdk` 实测打通
- ⏳ Translation report 渲染（adapter 返 lossy 警告时友好显示）

**Phase 2 总工期**：4-6 周（conductor 端是大头）

### Phase 3 · 演化（中长期）

- ⏳ askdao-cli 端：
  - L1：syft 改 Go library 直接 import（去外部依赖）
  - L3：扩展到 Java/Ruby/PHP/Elixir
  - Monorepo 多 workspace 支持（pnpm-workspace / cargo workspaces）
  - `askdao agent regenerate` diff 模式
  - Skill 自动初始化（从 README 抽核心能力建议初始 Skill）
  - Secret-in-code 扫描（gitleaks 集成）
  - **Dockerfile 选项 C 直挂能力**（`workspace.dockerfile.path` + `target_stage` + `build_args`）
  - **多阶段构建 target_stage 选择**
  - **build_args / build-time secrets**
  - **volumes / healthcheck / labels** 字段（按 KOL 反馈）
- ⏳ conductor 端：
  - 加更多 harness（LangGraph / Vercel OA / 本地 Ollama）
  - 中间格式演化到 v2（`apiVersion: askdao.ai/v2`）

---

## 8. 与 plan/06 已有 ADR 的关系

| plan/06 ADR | 本设计的关系 |
|------------|------------|
| §4.2 `agent init` 空骨架 | **扩展**：加 `--auto` 模式，骨架基础上自动填中间格式 yaml |
| §4.3 `agent validate` | **扩展**：validator 校验中间格式 schema（apiVersion + 8 块） |
| §4.4 `agent deploy` 事务三件套 | **重构**：deploy 通过 conductor adapter 路由到具体 harness API；旧 plan/06 §4.4 步骤需要重写 |
| §5 AgentSpec yaml schema | **完全重写为中间格式**：8 块顶层 + harness_specific escape hatch；plan/06 §5 yaml 示例需替换 |
| §6.1 CLI 框架（Typer） | **冲突**：本设计假设 askdao-cli 用 Go（按 memory pivot）。Python 没有 syft/enry/nixpacks 同等成熟生态 |
| plan/03 multi-runtime ADR | **明确为 Phase 2**：alembic `agent_spec.runtime_id` 列 + chat.py 三岔；本设计的 Phase 2 即对应这条 ADR |

---

## 9. 决策记录

### 9.1 L4 LLM 调用走哪条路？✅ 已定（v0.2）

- **(A) BYOK**：askdao-cli 直连 Anthropic，KOL 用自己 key
- **(B) Conductor 中转**：askdao-cli → conductor 后端 → Anthropic ← **选定**
- 理由：Phase 1 KOL 大概率还没 Anthropic key；conductor 已有 ManagedAgentsClient 可复用，零额外工程量
- 实现要点：conductor 加一个 endpoint `POST /api/v1/cli/recommend`（或类似），把 detection.json 转发给 Anthropic + 返回 yaml 草稿

### 9.2 syft 的接入方式？✅ 已定（v0.2）

- **(A) spawn CLI 进程读 stdout JSON** ← **Phase 1 选定**
- **(B) 直接 Go library import** — Phase 2 再评估
- 理由：MVP 快，跟进 syft release 零成本；二进制大小（~80 MB if library）不是 Phase 1 关注点
- askdao-cli 安装文档需注明依赖：用户先装 `brew install syft`

### 9.3a yaml schema 与 Anthropic 三资源对齐 ✅ 已定（v0.2）

- v0.1 提案"是否进 AgentSpec 主 schema"是错问题
- v0.2 决定：yaml **顶层有 3 个独立 block**（agent / environment / vault_hints），1:1 对应 Anthropic Agent / Environment / Vault 三资源
- 理由：清晰映射 API 调用边界；避免抽象层混淆；KOL 一眼看清三件套
- 实现要点：见 §5 重写后的 schema

### 9.3b conductor PG `agent_spec` 表新增 2 列 ✅ 已定（v0.2）

- 新增 **`managed_agent_version: int NOT NULL DEFAULT 1`** —— Agent 是 versioned 资源，必须 pin
- 新增 **`vault_hints_json: jsonb`** —— 存 yaml 的 vault_hints block，订阅者 onboarding 时引导填 vault
- 实现：alembic 017（命名建议 `017_managed_agent_version_and_vault_hints.py`），M3 阶段配套 askdao-cli MVP 一起上

### 9.4 后续待讨论项（v0.2 review 中浮现）

详见 [`review-2026-05-06.md`](./review-2026-05-06.md) §8：

1. Vault hints 是否拆出独立 `vault_hints.yml`？（当前选择：内嵌同 yaml）
2. Tool permission_policy 启发式具体规则？（当前选择：生产信号 + 工具危险等级二维矩阵，待细化）
3. Custom skills 探查到后是直传 Anthropic 还是先存 OpenViking？（涉及 conductor skill 上传管线）
4. 探查到 stdio MCP 怎么处理？（当前选择：标记 `anthropic_compatible: false` 并提醒 KOL）

### 9.5 yaml 输出格式：harness-neutral 中间格式 ✅ 已定（v0.3）

- v0.2 仍 1:1 对齐 Anthropic SDK 字段命名（即使三块布局正确）；askdao-cli 是开源项目，对外暴露这种 yaml 等于宣告"只服务 Anthropic"
- v0.3 决定：yaml **顶层 8 块全 harness-neutral**（`metadata / persona / capabilities / mcp_servers / custom_tools / skills / workspace / vault_hints`）+ `apiVersion: askdao.ai/v1` + `kind: AgentSpec` + harness 独有特性进 `harness_specific:` escape hatch
- 理由：yaml 字段不应暗示单一 harness；当前 Phase 1 只支持 Anthropic 是实施权宜，Phase 2 起支持 OpenAI Codex / 更多
- 实现要点：见 §5 重写后的 schema；conductor 端 AgentSpec pydantic 模型完全重写为中间格式

### 9.6 多 harness 支持分三阶段 ✅ 已定（v0.3）

- **Phase 1**（即将做）：中间格式 + AnthropicAdapter，单 harness 选项
- **Phase 2**（开源前必做）：OpenAIAdapter + chat.py 三岔 + alembic 加 `runtime_id` 列
- **Phase 3**（中长期）：更多 harness（LangGraph / Vercel OA / Ollama）
- 理由：Phase 1 不阻塞 askdao-cli MVP；Phase 2 给开源前留足时间；Phase 3 是演化空间
- 实现要点：详见 §6.2 / §6.3 工程量分项；conductor 端 plan/03 配套修订（M3+ 新增 OpenAI runtime 章节）

### 9.7 Dockerfile 兼容采用选项 B（5 字段中等修订）✅ 已定（v0.4）

- v0.3 仅识别 + 抽 base_image，丢失多阶段、自定义镜像、复杂 RUN 链、USER 切换、EXPOSE 端口等模式
- v0.4 决定：workspace 加 5 字段（`base_image` / `setup_commands` / `users` / `workdir` / `exposed_ports`），Anthropic adapter 输出 translation_report，OpenAI adapter Phase 2 真正消费
- 理由：Anthropic 故意做得很薄注定承载不全，但中间格式应按 OpenAI 上限留位；选项 C 直挂 Dockerfile 心智复杂度太高，留 Phase 3
- 实现要点：详见 §4 detection.json 升级 + §5.5 Translation Report 子章节 + §6.1 dockerfile.go 升级（80 → 200 行）+ §6.2 anthropic_adapter 加合并 + report（+100 行）

### 9.8 不做 GPU 资源声明 ✅ 已定（v0.4）

- 哥决定：AskDAO 不跑 ML 类任务，不需要 `workspace.resources.gpu` 字段
- 理由：聚焦 KOL 知识/服务场景，GPU 是远期事；当前 Anthropic 也完全不支持 GPU
- 影响：v0.4 yaml 不加 `resources.gpu` / `resources.memory_mb` 等资源字段；Phase 3 重新评估时再讨论

### 9.9 KOL 审阅 UX 采用「中等详情卡片 + 入口扩展」 ✅ 已定（v0.5）

- v0.4 完整 yaml 230+ 行让 KOL 心智负担过大；第一版纯摘要（35 行）丢失关键文件路径 / skill / dep 等具体内容
- v0.5 决定：**中等详情卡片**（mid-density，7 块顶层 / ~80-90 行）+ inline reasoning（`↳ Why:`）+ 入口扩展（[D/F/M/W/P]）
- **7 块顶层结构**：PERSONA / SKILLS / MCP SERVERS / CAPABILITIES / RUNTIME / SUBSCRIBER ONBOARDING / TRANSLATION WARNINGS
- **字段三档分类**：
  - 必列具体（Skills / MCP / Vault credentials / 关键文件路径 / Tool overrides / apt libs）
  - 列计数 + 入口（Python deps 前 8 + and N more / dev deps 计数 / 网络白名单前 5）
  - 展开 reasoning（Model 选择 / Tool override / Skill 推荐 / Translation warning）
- **inline reasoning 风格**：`↳ Why:` 引导符 + confidence 数字
- **deploy 加 diff preview**：KOL 改 yaml 后显示与原推荐的差异 + 对 translation_report 的影响
- 实现要点：`internal/render/` 5 个新模块（summary / reasoning / diff / warnings / lists）+ `cmd/askdao/show.go` + init/deploy 改造（共 ~870 行 Go）
- Phase 切分：归 Phase 1（不拆 Phase 1.5）

---

### 9.10 部署 Payload 清单 + 项目原型识别（确定性，零 LLM）✅ 已定（v0.7 修正）

> **v0.7 修正**（2026-05-14）：v0.6 的 lockfile-driven 分类规则建立在错误假设之上——以为 Anthropic Managed Agents 有"从 lockfile 重装"的能力。调研 `../../harness-design/investigations/managed-agents-skill-installation.md` §4.1 反映**不存在公共 skill registry**，所有 custom skill 必须 `POST /v1/skills` 上传到调用方组织。本节相应改写。

**动机**：以 `homework-spelling`（一个 skill-centric 内容流水线：input PDF → output HTML）为标尺，发现 askdao-cli 答不出 KOL 上云时最实际的问题——「这个目录部署到云端，到底该打包上传哪些文件？」`.agents/skills/` 下有 14 个外部 skill + 1 个本地原创 skill，该剔除的（`node_modules`/`output`/`input`/`.DS_Store`）和该纳入的（每一个 skill 整目录 / `CLAUDE.md` / `skills-lock.json`）混在一起。

**两层确定性能力**（都跑在 `askdao detect` / `askdao bundle` 里，`LLM=nil`）：

1. **`Detection.deployment_payload`** —— `internal/scanner/payload.go`：
   - **所有 custom skill 一律上传**（v0.7 修正）：每个 `<skillDir>/<name>/` 整目录递归打包进 `Includes`（含 SKILL.md + scripts/ + assets/ + references/ 等所有子文件 + 二进制透传）。vendored（lockfile-pinned）与 repo-原生 **从上传角度无区别** —— Anthropic Managed Agents 无公共 registry，无法 "reinstall from reference"。
   - **vendored 标签作 UI 元信息**：`DetectedSkill.LockedSource` + `LockedHash` 携带来源 + lockfile hash；`PayloadEntry.Reason` 携带 origin tag（`"repo-native"` 或 `"vendored: <source> @ <short-hash>"`），bundle UI inline 渲染。**不影响上传行为**。
   - **harness 中性 invariant**：deploy 时 `ZipDir(skillAbsDir, filepath.Base(skill.path))` —— KOL 项目里 skill 实际存放的上级路径（`.claude/skills/` / `.agents/skills/` 等）由 `filepath.Rel(srcDir, path)` 切掉，Anthropic 端只看到 `<skillName>/SKILL.md` 形态。详见 §9.14。
   - **ignore 规则链**：`builtin`（编辑器/OS 垃圾、可重装依赖缓存、build 输出、**`.env*`/`*.pem`/`*.key` 永不上传**）→ `.gitignore` → `.dockerignore` → `.askdaoignore`（新约定，syntax 同 gitignore，`!pattern` 反向纳入）。复用 `glob.go` 的 `compileGlobs`/`matchAny`。
   - **正向识别**：`CLAUDE.md`/`AGENTS.md`/`README` → agent_doc；`package.json`/`go.mod`/`skills-lock.json`/`Dockerfile`/… → manifest。
   - **明确剔除并给理由**（写进 `Excludes`）：`output*`/`*-old`/`*-bak` → generated；`input`/`data`/`samples`/`tmp` → 仅 archetype==skill_pipeline 时剔；`.github` → CI 配置。
   - **不做 drift 检测**：`computedHash` 是 lockfile 工具的归一化 hash，不是裸文件 sha256，无法可靠复算 —— `LockedHash` 仅作 UI 显示。

2. **`Detection.archetype`** —— `internal/scanner/archetype.go`：纯函数。本地原创 skill 数 = pipeline 信号；service framework / 后端语言占比 >50% = app 信号。两者都有 → `mixed`；只 pipeline → `skill_pipeline`；只 app → `code_app`；都无 → `unknown`。让 detect 知道「这目录的 agent 本体是那个本地 skill，不是一个服务」，并据此调整 payload 的剔除策略。

**暴露**：`askdao bundle [path]`（独立命令，输出 `WILL UPLOAD`（含每个 skill 的 origin tag）/ `EXCLUDED` 两段 + `--json`/`--warnings`/`--no-evals`）+ `askdao detect --summary` 末尾追加精简三行 + `askdao detect`（非 summary）的 JSON 多两个顶层字段。

**v0.7 已删除**：`SkillReferences` 字段 + `SkillRef` struct（数据模型）；`--bundle-skill` flag（force-inline 概念消失）；"SKILL REFERENCES" section（bundle UI）。

**不做（划界）**：`askdao bundle` 只预览不打包/不上传（真上传走 `agent deploy`）；不实现 `.gitignore` 全套语义。

**实现量（含 v0.7 修正）**：`internal/scanner/{payload,archetype}.go` + `skills_dir.go` 扩展 + `internal/render/payload.go` + `cmd/askdao/bundle.go` + types 新字段 + 测试，约 ~1000 行 Go。Phase 切分：归 Phase 1。已交付：PR #19（v0.6 初版）+ PR _TBD_（v0.7 修正）。

---

### 9.11 Plugin 机制的影响（Claude Code Plugin / Codex Plugin）⏳ 待决策（v0.6 调研）

**背景**：2025-2026 这一波，两个最主流的本地 AI Agent 运行环境几乎同时定义了同一种「打包格式」——一个目录 + 一个 manifest，里面装 `skills/` / `.mcp.json` / `agents/` / `hooks/`（Claude 还有 `bin/` / `.lsp.json` / `monitors/` / `settings.json`），通过 git-repo 形态的 marketplace 分发。

| | Claude Code Plugin | OpenAI Codex Plugin |
|---|---|---|
| manifest | `.claude-plugin/plugin.json`（`name`/`description`/`version`/`author`/`homepage`/`repository`/`license` + 可声明依赖）| `.codex-plugin/plugin.json`（同上 + `keywords` + 组件指针 `skills`/`mcpServers`/`apps`/`hooks` + `interface{displayName,category,logo,screenshots,defaultPrompt,brandColor,...}`）|
| 组件目录（plugin 根）| `skills/<name>/SKILL.md`、`commands/`(legacy)、`agents/`、`hooks/hooks.json`、`.mcp.json`、`.lsp.json`、`monitors/monitors.json`、`bin/`、`settings.json` | `skills/<name>/SKILL.md`、`.app.json`、`.mcp.json`、`hooks/hooks.json`、`assets/` |
| marketplace 清单 | `.claude-plugin/marketplace.json`（`owner/repo` / git URL / 本地路径 / 远程 URL）| `.agents/plugins/marketplace.json`（`source`：`local`/`git-subdir`/`git`；带 `policy.installation`/`policy.authentication`）|
| 安装 | `/plugin marketplace add owner/repo` → `/plugin install name@marketplace`，skill 命名空间化 `/plugin-name:skill` | Codex app 目录 / CLI `/plugins`；`@plugin-name` 调用；config 在 `~/.codex/config.toml` |
| 版本 | `plugin.json.version` 显式，否则 git commit SHA | `plugin.json.version` |
| 脚手架 | `plugin-dev` 插件 | 内置 `$plugin-creator` skill |

**关键洞察**：askdao-cli 的本职——「扫 KOL 项目目录 → 推断出可部署的 agent → 打包」——和这套 plugin 格式在结构上高度重叠。`homework-spelling` 那种 `.agents/skills/` + `skills-lock.json` + `CLAUDE.md` 的目录，离一个 Claude Code plugin 只差一个 `.claude-plugin/plugin.json`。领域收敛出了事实标准，askdao-cli 在它正中央。

**三个层面的影响**：

1. **入口侧（检测）—— plugin manifest 是「权威来源」，胜过启发式。** 项目里有 `.claude-plugin/plugin.json` / `.codex-plugin/plugin.json` → 这个项目本身就是一个 plugin，manifest 直接给出 name/version/bundle 了哪些 skills·agents·hooks·MCP/声明了哪些依赖，不用猜；有 `.claude-plugin/marketplace.json` / `.agents/plugins/marketplace.json` → 这是一个 marketplace（plugin 仓库），N 个子目录各是一个 plugin；plugin manifest 声明的**依赖** = §9.10「在 lockfile 里 = 引用、不在 = 打包」规则的泛化。影响面：scanner 加 `LoadPluginManifest`（`skills_dir.go` 旁）、archetype 加 `plugin_package` / `plugin_marketplace`（置信度近 1.0）、`payload.go` 在 plugin archetype 下直接用 manifest 定义清单、`detection.go` 加 `DetectedPluginManifest` sub-type。**这一档是 §9.10 工作的自然延伸——纯增量、低风险、确定性、零 LLM。**

2. **出口侧 —— askdao-cli 可以生成 plugin，而不只是 `askdao.ai/v1` AgentSpec。** 设想新命令 `askdao plugin export [--target claude-code|codex|both]`：把 detection / AgentSpec 转成标准 plugin 目录（`.claude-plugin/plugin.json` + `skills/<name>/SKILL.md` + `.mcp.json` + `agents/` + `hooks/hooks.json` + Claude 可加 `bin/`·`settings.json`）。AgentSpec → plugin 的字段映射几乎逐项对得上（`metadata`→manifest 头、`skills`→`skills/`、`mcp_servers`→`.mcp.json`、`persona.system_prompt`→ 一个根 skill 或 instruction、Codex `interface{...}`→ `metadata.labels`+`domain`+ 新展示块）。战略价值：让 KOL 的 agent 能装进任何人的 Claude Code/Codex，而不只是 AskDAO 云——和「askdao-cli = AskDAO 体系唯一对外开源子项目 = 信任锚点」的定位高度一致。分发模型也跟着多一条：除「部署到 AskDAO 云」外，还有「push 这个 plugin 到一个 git-repo marketplace，Claude Code/Codex 用户 `/plugin install` 就能装」；AskDAO 可以自己托管一个 marketplace。改动面中等：新命令 + 一个文件发射器（`internal/export` 或扩 `internal/render`）+ AgentSpec↔plugin 映射表（进 §5）。不碰 conductor。

3. **架构层 —— `AgentSpec` 目标矩阵多两列；askdao-cloud 发行模型要重想。** §5.1 的映射现在该再加 `ClaudeCodePluginEmitter` / `CodexPluginEmitter` 两列（文件发射器，不是 API 调用器）。要点：**Plugin ≠ Managed Agent**——前者扩展本地 CLI，后者是云端自治 agent，是两种 runtime；但 plugin 可以 bundle `agents/`（subagent 定义），所以一个 KOL 的「agent」可呈现为 (a) 云端 Managed Agent（现在的目标）、(b) subscriber 本地装的 Claude Code plugin、(c) Codex plugin——`AgentSpec` 作为 harness-neutral 中间格式正好横跨这几种。`workspace.*` / `vault_hints` 在 plugin 目标下大多无处安放（plugin 不管 runtime、没有 per-user vault），用 v0.4 已有的 translation_report 机制报告「这些字段被忽略」即可。**不建议让 plugin 格式取代 `askdao.ai/v1`**——后者的价值恰恰在于 harness-neutral，能同时映射云端 agent 和本地 plugin 两类目标，plugin 只覆盖后者。askdao-cloud 侧的开放问题（需上层拍板）：① AskDAO 要不要自己托管 plugin marketplace？② conductor 要不要能反向 ingest 一个 plugin → 起一个 Managed Agent（plugin 作为 agent 的另一种输入源）？③ KOL onboarding 主路径是「装 askdao-cli → `agent init` → deploy 到云」还是「`plugin export` → push 到 marketplace」，还是并存？

**推荐路线（分阶段）**：

| 阶段 | 内容 | 风险 | 何时 |
|---|---|---|---|
| 0 | 本节（落档，待决策）| 无 | 已做 |
| 1 | scanner 加 plugin-manifest 检测（影响①）：`LoadPluginManifest` / archetype `plugin_package`·`plugin_marketplace` / payload 用 manifest 直接定义 / `DetectedPluginManifest`。确定性、零 LLM，§9.10 的延伸。 | 低 | 确认方向后可作为下一个 PR |
| 2 | `askdao plugin export`（影响②）：detection/AgentSpec → Claude Code plugin 目录（+ Codex）。先定 AgentSpec↔plugin 映射表（进 §5）。 | 中 | 阶段 1 之后；先在 askdao-cloud 起 design issue 讨论分发模型 |
| 3 | 架构层（影响③）：AgentSpec 目标矩阵 +2 列、askdao-cloud 发行模型决策、conductor 要不要 PluginAdapter / plugin-ingest。跨 askdao-cli + askdao-cloud + conductor。 | 高（战略）| 阶段 2 落地、跑过几个真实 KOL plugin 之后 |

---

### 9.12 Agent 项目布局：单文件宣言 + `.askdao/` 工具空间 ✅ 已定（v0.7）

**动机**：v0.6 设计中 `askdao agent init <name>` 在 KOL 项目内创建 `<name>/` 子目录，所有 CLI 产物（agent.yml / persona.md / .askdao/recommendation.yml / detection.json）都在那个子目录里。哥实测时撞到 `~/WorkSpace/homework-spelling/homework-spelling/` 自指路径迷惑，且 `agent.yml` 隐藏在子目录里，KOL 不易直观感知"这是我项目的 agent 声明"。

**决策（v0.7）**：扁平化产物布局 + 命名区分"我的"vs"工具的"。

```
KOL 项目根/
├── askdao-agent.yml            ← KOL 唯一编辑对象（项目宣言文件）
│                                 类比 Cargo.toml / package.json / Dockerfile
│                                 含 persona.system_prompt literal block 完整内容
├── .askdao/                    ← 工具空间（隐藏）
│   ├── recommendation.yml      ← diff baseline（deploy 用作 KOL 改动检测）
│   └── detection.json          ← 确定性扫描结果（每次 init 重生成）
├── .agents/skills/             ← KOL 已有
├── skills-lock.json            ← KOL 已有
├── package.json                ← KOL 项目原有文件
└── ...
```

**心智模型**："**根的一个文件是 KOL 的（要 commit、要编辑），`.askdao/` 是工具的（可随时 rm 重生成）**。"

**关键决策点**：
1. `agent.yml` → `askdao-agent.yml`：带工具前缀防命名空间撞库（与 `pyproject.toml` / `tsconfig.json` 同款规约）。
2. 不放进 `.askdao/`：核心声明文件按业界惯例应在项目根（IDE 默认可见 / commit 自然 / review-and-edit UX 主张兑现）。隐藏目录是给"工具产物"用的，类比 `.github/` / `.vscode/`。
3. 一个项目 = 一个 agent：`agent init [name]` name 参数可选（默认项目目录名），仅写入 `metadata.name`，不影响磁盘布局。
4. **不再生成 `persona.md`**（详见 §9.15）。

**影响**：`cmd/askdao/{init_auto,deploy,show,bundle}.go` 全部改读写新路径；`internal/types/agent_spec.go` 加 `askdaoAgentFileName` / `askdaoDirName` 常量；测试 fixture 重写。哥实测扁平化后命令行体验直观了一档。

---

### 9.13 信任边界 in L1-L4（哪些字段 LLM 适合 vs 不适合）✅ 已定（v0.7）

**动机**：冒烟测试中两次撞到"LLM 越界进入确定性字段"的同款问题：

1. **`metadata.domain` 标量当 list** —— LLM 写 `domain: "education"`，pydantic 严格拒绝。修法：conductor 端引入 `normalize_llm_agent_spec` 在 `model_validate` 之前吸收常见 LLM 错位（标量↔list、enum 大小写等）。
2. **`skills` 段乱写** —— LLM 看到 `detected_skills` 把 14 个 lockfile-pinned 都误抄成 `custom_local` 内联，且 `path` 指 SKILL.md 文件而非目录。修法：deterministic builder 取代 LLM 的 skills 段输出。

**根因**：把**确定性事实**（"哪些 skill 在 lockfile 里"、"domain 字段必须是 list"）丢给**概率系统**（LLM）去决定，是架构错位。

**信任边界原则**：

| 字段类型 | 适合 LLM | 不适合 LLM |
|---|---|---|
| **软字段**（设计决策、风格、解释）| `persona.system_prompt` / `provenance.reasoning_*` / `metadata.description` / `model_class` / `expertise_level` | — |
| **硬字段**（schema 强约束、ground truth、确定性事实）| — | `skills[]` / `metadata.domain` 类型 / `metadata.version` 格式 / `capabilities.*.permission` enum / `mcp_servers[].type` enum |

**实现模式**：硬字段一律由 askdao-cli / conductor 后处理填充或规整：
1. **LLM 端 prompt 加约束**（belt）：明确告诉模型"omit this field" / "must be JSON array" / "lowercase enum"
2. **server 端 normalizer**（suspenders）：`normalize_llm_agent_spec` + `data.pop("skills", None)` 兜底剥
3. **CLI 端 deterministic builder**（最终防线）：`BuildAgentSpecSkills(det)` 强覆盖 LLM 输出

**Phase 1 实例**：
- `app/agents/llm_normalizer.py`（conductor，PR #44）—— 处理 `metadata.domain` 等
- `internal/pipeline/skills_builder.go`（askdao-cli，PR _TBD_）—— deterministic 构造 skills 段

**未来扩展**：当 LLM 在 `version` / `permission` / `domain` 三个字段又自由发挥时（迟早），把它们纳入同款 normalizer 而不是改 prompt 求模型服从。

#### Normalizer 规则集 audit checklist（v0.7.1 补，prod 撞到第三次后立的 process gate）

normalizer 历史上演化是被动反应式的：每撞一次新错位就加一类规则。这意味着每次 AgentSpec 加新字段时，**必须**对照下面这张四类形态表过一遍，确认 normalizer 是否需要新规则——而不是等 prod 502 再补。

| Schema 类型 | 已知 LLM 错位形态 | normalizer 规则 / tuple |
|---|---|---|
| `list[str]` | scalar string（"education" 而非 ["education"]）| `_SINGLETON_STRING_LISTS` → `_wrap_str_to_list` |
| `list[<model>]` | single dict（{...} 而非 [{...}]） | `_SINGLETON_OBJECT_LISTS` → `_wrap_object_to_list` |
| `dict[str, str]` | list of strings（["a","b"] 而非 {"a":"","b":""}）| `_LIST_TO_DICT_KEYS` → `_list_to_dict_with_empty_values`（v0.7.1） |
| Enum string | 大小写错（"Private" / "ALWAYS_ALLOW"）| `_LOWERCASE_STRINGS` → `_lowercase_string` |
| `bool` | "true"/"false" 字符串 | （pydantic 默认接受，暂不需 normalizer） |
| 嵌套结构（`Optional[X]`、union） | （未撞过，留观察）| — |

**新加字段时的 process gate**（写进 conductor `app/agents/spec.py` L3 头部的镜像约束）：
1. 字段类型属于上表前 4 类之一？ → 加进对应 tuple，并补单元测试
2. 字段是 dict[str, str]？ → **加进 `_LIST_TO_DICT_KEYS`**
3. 字段是 enum string（注释里 ∈ {...}）？ → 加进 `_LOWERCASE_STRINGS` + prompt 加约束
4. `tests/test_llm_normalizer.py::test_rule_paths_resolve_against_spec_or_known_extensions` 跑一遍守护漂移

撞墙记录（每条都该是教训而不是事后补丁）：

| 时间 | 字段 | 形态 | 修复 PR |
|---|---|---|---|
| 2026-05-13 | `metadata.domain` | scalar → list | conductor #44 |
| 2026-05-14 | `skills[]` | LLM 自由发挥（dict, dict, dict...） | askdao-cli #25 deterministic builder + conductor #50 prompt OMIT |
| 2026-05-14 | `metadata.labels` | list → dict | conductor #55（本节由此触发补的 checklist） |

---

### 9.14 Skill 上传分层协议 + harness 中性 invariant ✅ 已定（v0.7）

**动机**：`harness-design/investigations/managed-agents-skill-installation.md` §1.2.1/1.2.2 反映 Anthropic `/v1/skills` 接受 multipart 多文件原生上传（不接受 zip）。最初考虑把这条协议传染到全链路（CLI 直接 multipart 给 conductor），但这是反向复杂化 —— CLI 要写大量 walk + multipart 代码，conductor 要重写接收逻辑，OpenViking 备份要折腾。

**决策**：**分层协议** —— Conductor 作 anti-corruption layer，把 Anthropic 协议怪癖封装在内部。

```
askdao-cli                    Conductor                    Anthropic
─────────                     ─────────                    ─────────
  打包 zip per skill           解 zip                       multipart 多 part
  ───── multipart/form ────►    │                            ↑
       (skill_files: zip)       │ ─── SDK files=[...] ────────┘
                                 (anti-corruption layer)
```

- **CLI ↔ Conductor**: zip per skill（简单内部协议，PR #21 已实装跑通）
- **Conductor ↔ Anthropic**: multipart 多 part（按 Anthropic §1.2.1 原生协议；conductor `sync_skill_zip` 已在解 zip + SDK `files=[...]` 调用）
- **设计原则**：Anthropic 协议变化（未来支持 zip / 改 endpoint / 改 beta header）→ 只动 conductor，CLI 零改动

**Harness 中性 invariant**（关键）：

KOL 项目里 skill 实际存放的上级路径不进入 zip。CLI `ZipDir(srcDir, rootName)` 内部用 `filepath.Rel(srcDir, path)` 算相对路径，物理上不可能含上级目录：

```go
// cmd/askdao/deploy.go:
skillAbsDir := filepath.Join(*dir, s.Path)             // e.g. <root>/.agents/skills/tts
skillName := filepath.Base(filepath.Clean(s.Path))     // "tts" —— harness-specific 上级被切掉
zb, _ := deploy.ZipDir(skillAbsDir, skillName)         // zip 内顶层 = "tts/"
```

| KOL 项目里 skill 的实际存放位置 | zip 顶层目录 |
|---|---|
| `.claude/skills/tts/` | `tts/` |
| `.agents/skills/tts/` | `tts/` |
| `skills/tts/`（KOL 自定义） | `tts/` |
| `vendor/marswaveai/tts/`（更怪的） | `tts/` |

无论 KOL 用哪个 harness 习惯，Anthropic 端始终只看到 `tts/SKILL.md` 形态。**这条 invariant 由 `ZipDir` 实现保证 —— 物理上不可能泄露上级目录**。

**测试覆盖**：`internal/deploy/zip_test.go` + `cmd/askdao/deploy_test.go::TestDeploy_EndToEnd_HappyPath` 验证 zip 内 entry 形态。

---

### 9.15 persona 单一真相源（删 persona.md / persona_file）✅ 已定（v0.7）

**动机**：v0.6 设计中 agent 的 persona 有两种表达路径：

1. `AgentSpec.Persona.SystemPrompt`（yaml 字段直接写 prompt 内容）
2. `AgentSpec.Metadata.PersonaFile`（指向外部 `.md` 文件）

两条路径并存就一定有"应该用哪条"的歧义，且 init --auto 写 `<name>/persona.md` 文件、deploy 时可能要再读注入 yaml —— 故障域多一层（路径错配 / 文件丢失 / 内容不同步）。

**决策（v0.7）**：**合并到单一字段** —— 删 `Metadata.PersonaFile` + 不再生成 persona.md，所有 prompt 内容在 `Persona.SystemPrompt` yaml literal block (`|`) 内。

```yaml
persona:
  model_class: balanced
  model_preferences:
    - {provider: anthropic, id: claude-sonnet-4-6}
  system_prompt: |
    You are a spelling homework generator for a 5th grader.

    ## Your responsibilities
    1. Read the teacher's spelling list (PDF or photo in input/)
    2. Generate one polished HTML study page in output/
    ...
```

**关键观察**：
- YAML literal block 内任意 markdown 字符无 escape 压力（包括 `:` / `"` / `---` 等）
- 现代 IDE（VS Code / IntelliJ）对 yaml 多行字符串内的 markdown 多有 inject 高亮
- 长 prompt（5000+ 字）虽然让 yaml 文件变胖，但 prompt 段放 yaml 末尾，前面的结构化字段仍清晰

**收益**：
- schema 简化：删 `PersonaFile` 字段（双侧 askdao-cli + conductor）
- 故障域消失："yaml 引用 .md，.md 丢了怎么办"这条不存在了
- 心智简化：KOL **只**面对 `askdao-agent.yml` 一个编辑对象（与 §9.12 项目布局哲学完全一致）
- diff 干净：`askdao-agent.yml` 单文件 diff baseline，KOL 改 prompt 跟改 capabilities 一样的轨迹

**未来 follow-up（不在 v0.7）**：加 `askdao agent edit` 命令打开 `$EDITOR` 跳到 system_prompt 字段（针对 IDE 不友好的终端 KOL）。当前靠 `vim askdao-agent.yml` 跳定位足够。

---

## 10. 落地路径

### Phase 1（即将做）

1. **本设计文档 v0.4 评审 → ADR 编号**：编入 `harness-design/primitives/`（建议 `07-harness-neutral-agent-spec.md` 含 v0.3 中间格式 + v0.4 Dockerfile 兼容子条款）
2. **更新 plan/06**：
   - §4.2 改写为带 `--auto` + `--harness` 路径
   - §5 AgentSpec yaml schema 完全重写为中间格式 8 块（含 v0.4 workspace 5 个 Dockerfile 兼容字段）
   - §6.1 CLI 框架：明确 Go（按 memory pivot）
3. **更新 plan/01 + alembic**：
   - alembic 017 加 3 列：`managed_agent_version` + `vault_hints_json` + `runtime_id`
   - 同步更新 plan/02 conductor 业务字段
4. **更新 plan/03 + Conductor**：
   - 新增 `app/agents/spec.py`（中间格式 pydantic）+ `app/agents/adapters/anthropic_adapter.py`
   - AnthropicAdapter 实现 v0.4 translation_report 输出 + extracted_apt/pip 兜底合并
   - 加 `POST /api/v1/cli/recommend` + `POST /api/v1/cli/deploy`（含 translation_report 返回）
   - 现有 `ManagedAgentsClient` 改造为 Anthropic adapter 的下游消费者
5. **GitHub Issue 拆分**：按 §6 工程量切成 8-10 个 task：
   - askdao-cli 端：6 个（5 个 scanner / 4 个 provider / recommender / cmd × 4 含 show / **render UX 5 模块**）
   - conductor 端：3 个（spec + anthropic_adapter / cli endpoint / alembic）

### Phase 2（开源前必做）

6. **新增 ADR**：`harness-design/primitives/08-multi-harness-runtime.md`（OpenAIAdapter + sandbox provider 选择）
7. **plan/03 加章节**：M-OpenAI 阶段（`app/openai_sdk/` 新建 + chat.py 三岔重构）
8. **askdao-cli 端**：deploy 命令 `--harness openai_agents_sdk` 通路打通
9. **GitHub Issue 拆分**：~6 个 task（openai_adapter / 5 个 openai_sdk 子模块 / chat 三岔 / 测试）

---

## 附录 · 参考资料

### v0.5 design review
- [`review-v0.5-2026-05-06.md`](./review-v0.5-2026-05-06.md) — KOL 审阅 UX 中等详情卡片（7 块结构 + inline reasoning + 入口扩展）

### v0.4 design review
- [`review-v0.4-2026-05-06.md`](./review-v0.4-2026-05-06.md) — Dockerfile 兼容性补强（选项 B：5 字段；不做 GPU 声明）

### v0.3 design review
- [`review-v0.3-2026-05-06.md`](./review-v0.3-2026-05-06.md) — 中间格式可行性 + 字段重叠度评估 + 三阶段路线图

### v0.2 design review
- [`review-2026-05-06.md`](./review-2026-05-06.md) — Anthropic 三资源模型重审

### 上游 spike 报告
- [`investigations/syft-spike-for-askdao-cli.md`](./investigations/syft-spike-for-askdao-cli.md)
- [`investigations/nixpacks-provider-pattern.md`](./investigations/nixpacks-provider-pattern.md)

### Anthropic Managed Agents 官方文档（同 org 私有仓库）
- `harness-design/claude-managed-agents-docs/docs/managed-agents/overview.md`
- `harness-design/claude-managed-agents-docs/docs/managed-agents/agent-setup.md`
- `harness-design/claude-managed-agents-docs/docs/managed-agents/environments.md`
- `harness-design/claude-managed-agents-docs/docs/managed-agents/skills.md`
- `harness-design/claude-managed-agents-docs/docs/managed-agents/mcp-connector.md`
- `harness-design/claude-managed-agents-docs/docs/managed-agents/vaults.md`
- `harness-design/claude-managed-agents-docs/api/python/managed-agents/agents/create.md`
- `harness-design/claude-managed-agents-docs/api/python/managed-agents/environments/create.md`

### OpenAI Agents SDK 官方文档（v0.3 重读后引用，同 org 私有仓库）
- `harness-design/openai-agents-sdk-docs/01-agents-sdk-overview.md`
- `harness-design/openai-agents-sdk-docs/03-agent-definitions.md`
- `harness-design/openai-agents-sdk-docs/04-models-and-providers.md`
- `harness-design/openai-agents-sdk-docs/06-sandbox-agents.md`
- `harness-design/openai-agents-python/` — Python SDK 源码

### Warp Oz 设计参考（同 org 私有仓库）
- `harness-design/warp-oz-docs/cloud-agents/environments.md`

### 历史选型分析
- `harness-design/archived-version/harness-selection-analysis.md` — 原始多 harness 推荐路径

### 相关 memory
- `project_askdao_cli_design_pivot_2026_05_05.md` — Go + Oz Environment 一等抽象 + multi-runtime ADR
