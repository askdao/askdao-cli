# askdao-cli Agent Bootstrap (`askdao agent init --auto`)

> **Scope**: plan/06-deploy-cli.md §4.2 `askdao agent init` 命令的智能化补强 ——
> 让 KOL 在自己项目目录下跑一行命令，自动产出 `agent.yml` 草稿（含 Anthropic Managed Agents 三资源完整配置），而不是从空白模板手填。
>
> **Version**: v0.2 (2026-05-06)
> **Status**: Design draft — pending review
> **Owner**: Sam
> **Aligns with**: memory `project_askdao_cli_design_pivot_2026_05_05.md`（Go + 借鉴 Oz Environment 一等抽象）

---

## ChangeLog

### v0.2 · 2026-05-06 — Anthropic 三资源模型重构

哥指出 v0.1 在 Anthropic Managed Agents 抽象层有结构性偏差。重读官方文档后做以下修订（细节见 [`review-2026-05-06.md`](./review-2026-05-06.md)）：

- **§5 agent.yml schema 重写为三块布局**：`agent` (Anthropic Agent 资源) + `environment` (Anthropic Environment 资源) + `vault_hints` (订阅者 onboarding 引导)。v0.1 把所有字段塞 `environment` 块的设计被推翻 —— Agent 才是富资源，Environment 只是容器配置
- **§4 detection.json 增加 4 个探查字段**：`detected_mcp_configs` / `detected_skills` / `detected_required_secrets` / `detected_tool_risk_hints`
- **§6 工程量估算调整为 ~2760 行 Go**（+360）：4 个新 scanner 模块（mcp_config / skills_dir / secrets_hint / policy 推断）
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

askdao-cli 的 `init --auto` 应该 1:1 对齐这个 UX，但**输出对象从 Docker image 换成 Anthropic Managed Agents environment YAML**。

### 1.3 与 plan/06 现有方向的衔接

本设计**不替代** plan/06 §4.2-§4.4，是它的**前置增强**：

```
[新增] askdao agent init --auto <name>     # 扫描当前目录 → 生成 agent.yml 草稿
       └─ KOL 修订 agent.yml + persona.md
[已有] askdao agent validate                # plan/06 §4.3
[已有] askdao agent deploy                  # plan/06 §4.4
```

---

## 2. 系统架构（四层流水线）

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
│  • policy.infer() 生产信号 → tool permission_policy        │
│  → detected_frameworks + apt_pkgs + tool_risk_hints        │
└──────────────┬───────────────────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────────────────┐
│  L4 · LLM 推荐器（调 conductor 后端）                      │  模糊推断
│  把 detection.json 喂 LLM 生成 3 块 yaml + reasoning       │
│  • Block 1 · agent (model + system + tools + skills + …)  │
│  • Block 2 · environment (packages + networking)           │
│  • Block 3 · vault_hints (required_credentials)            │
└──────────────┬───────────────────────────────────────────┘
               │
               ▼
            agent.yml (3 blocks) + .askdao/detection.json
```

**层间分离原则**：L1-L3 全确定性，离线可跑，零成本；L4 才调 LLM。
**Anthropic 资源映射**：yaml 的三块对应 Anthropic Managed Agents 的三个 API 资源（Agent / Environment / 加 Vault），deploy 命令按顺序调三个 API。

---

## 3. 命令骨架（plan/06 §4.2 增量）

### 3.1 `askdao agent init <name> [--auto] [--from <path>]`

无 `--auto`：保持现状（生成空目录骨架）。

加 `--auto`：触发扫描流水线 +生成填好的 `agent.yml` 草稿：

```bash
$ cd ~/WorkSpace/my-fastapi-project
$ askdao agent init my-agent --auto

→ Scanning ./ ...
→ Detected: Python 3.12 + FastAPI + SQLAlchemy + PostgreSQL
→ Detected 28 production deps (filtered out 14 dev deps)
→ Detected 1 MCP config (.mcp.json: github)
→ Detected 0 custom skills + recommending 1 anthropic skill (xlsx)
→ Detected 2 required secrets from .env.example
→ Detected production deploy signal → bash/write set to always_ask
→ Inferred system packages: libpq-dev, gcc
→ Calling LLM (via conductor) for system_prompt + reasoning ...

✓ Generated my-agent/agent.yml (draft, 3 blocks: agent / environment / vault_hints)
✓ Saved my-agent/.askdao/detection.json (provenance)

Next:
  cd my-agent && vim agent.yml persona.md
  askdao agent validate
  askdao agent deploy   # 3 API calls: environment.create → agent.create → write conductor
```

`--from <path>` 允许从非 cwd 的目录扫（KOL 项目散在多目录时）。

### 3.2 `askdao detect [path]`（仅诊断）

不创建 agent 目录，只跑 L1-L3 + 打印 detection report。用于 KOL 想"先看看有什么"的探索路径。

```bash
$ askdao detect ./
Languages: Python 67% · TypeScript 26% · Shell 7%
Frameworks: FastAPI (conf=0.95) · SQLAlchemy (conf=0.92)
Production deps: 28 pip / 12 npm
System pkgs: libpq-dev gcc libjpeg-dev
Lockfiles: uv.lock pnpm-lock.yaml
```

### 3.3 `askdao agent regenerate`（init 后再扫）

KOL 项目演进后想刷新 yaml 推荐。读 `.askdao/detection.json` 做 diff，提示哪些字段变了。

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
    "base_image": "python:3.12-slim",
    "exposed_ports": [8000]
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
| `detected_dockerfile` | Dockerfile AST 解析 | `moby/buildkit` parser |
| `detected_external_services` | 依赖+import+.env 三源交叉 | 自己写（30 行规则） |
| `detected_env_files` | dotenv 解析（仅读 keys，不读 values） | 自己写 |
| `inferred_apt_packages` | nixpacks 反向映射表 | 移植 |
| `repository_layout` | 工作区文件检测 | 自己写 |
| **🆕 `detected_mcp_configs`** | `.mcp.json` / `claude_desktop_config.json` JSON 解析 + Anthropic 兼容性检测 | 自己写（~60 行 Go） |
| **🆕 `detected_skills`** | 5+ 个 skill dir 约定（`.claude/skills/` / `.agents/skills/` ...）+ 依赖反推内置 skill | 自己写（~80 行 Go） |
| **🆕 `detected_required_secrets`** | `.env.example` keys + service mapping → MCP server 反查 | 自己写（~120 行 Go） |
| **🆕 `detected_tool_risk_hints`** | 生产信号检测（deploy.yml / prod config）+ 用户数据信号 → permission_policy | 自己写（~100 行 Go） |

---

## 5. agent.yml schema（v0.2 三块布局）

`init --auto` 最终落盘的 yaml 与 Anthropic Managed Agents **三资源 1:1 映射**：

```
┌─────────────────────────────────────────────────────────────┐
│  agent.yml                                                   │
│                                                              │
│  ┌────────────┐    ┌──────────────┐    ┌────────────────┐  │
│  │ Block 1    │    │ Block 2      │    │ Block 3        │  │
│  │ agent      │    │ environment  │    │ vault_hints    │  │
│  └─────┬──────┘    └──────┬───────┘    └────────┬───────┘  │
└────────┼──────────────────┼─────────────────────┼──────────┘
         │                  │                     │
         ▼                  ▼                     ▼
   POST /v1/agents    POST /v1/environments    KOL 引导订阅者
   → agent_id +       → environment_id          填 vault → vault_id
     version
                                                (session 时三件套绑定)
```

完整示例：

```yaml
# my-agent/agent.yml (auto-generated draft, KOL should review/edit)

# 顶层 metadata（plan/06 §5 已有，保留）
name: my-agent
version: 0.1.0
visibility: private
expertise_level: pro
domain:
  - backend-engineering          # LLM 从 frameworks 反推
group_name: "My Agent Group"
persona_file: persona.md         # plan/06 长篇 persona（聚合进 agent.system 或独立资源）

# ============================================================
# Block 1 · Anthropic Agent 资源 → POST /v1/agents
# ============================================================
agent:
  # —— Model
  model:
    id: claude-opus-4-6           # LLM 推断（多步推理 → opus）
    speed: standard               # 默认 standard；KOL 可改 fast 走 premium pricing

  description: "..."              # KOL 给订阅者看的 agent 介绍

  # —— System prompt（LLM 生成，KOL 应审阅）
  system: |
    You are an AI assistant for {KOL name}, helping with backend engineering
    topics including FastAPI, SQLAlchemy migrations, and Postgres ops.
    
    Code execution rules:
    - Use the bash/write tools to create deliverable files in /mnt/session/outputs/
    - When users ask about migrations, always suggest a dry-run first
    ...

  # —— Tools 配置（按 detected_tool_risk_hints 自动设 policy）
  tools:
    - type: agent_toolset_20260401
      default_config:
        enabled: true
        permission_policy: { type: always_allow }   # KOL 服务订阅者场景默认
      configs:
        # 检测到生产部署文件，对 bash/write 单独要求 ask
        - { name: bash,  permission_policy: { type: always_ask } }
        - { name: write, permission_policy: { type: always_ask } }

    # 来自 detected_mcp_configs（仅含 anthropic_compatible=true 的）
    - type: mcp_toolset
      mcp_server_name: github

  # —— MCP servers（来自 detected_mcp_configs）
  mcp_servers:
    - { type: url, name: github, url: "https://api.githubcopilot.com/mcp/" }

  # —— Skills（detected_skills + LLM 推荐内置）
  skills:
    # detected_skills.implied_anthropic_skills
    - { type: anthropic, skill_id: xlsx }      # detected pandas+openpyxl → xlsx
    # detected_skills 里的 custom_local（deploy 时上传到 Anthropic）
    - { type: custom, skill_id: skill_xxx, version: latest }   # 由 deploy 命令上传后回填

  # —— Metadata（业务标签）
  metadata:
    askdao.kol_id: "kol_sam"
    askdao.pricing_tier: "paid"
    askdao.cli_version: "0.1.0"

# ============================================================
# Block 2 · Anthropic Environment 资源 → POST /v1/environments
# ============================================================
environment:
  name: my-agent-env             # 必须 org+workspace 内唯一
  description: "FastAPI + Postgres runtime"

  config:
    type: cloud
    
    packages:
      # 来自 detected_packages.{pip,npm,...}[is_prod=true]
      pip:
        - fastapi==0.135.1
        - sqlalchemy==2.0.48
        - asyncpg==0.31.0
        - alembic==1.18.4
        - anthropic==0.97.0
        # ... 28 项（已自动过滤 pytest/mypy/ruff 等 dev 依赖）
      apt:
        # 来自 inferred_apt_packages
        - libpq-dev
        - gcc
        - libjpeg-dev
    
    networking:
      type: limited
      allow_mcp_servers: true
      allow_package_managers: false
      allowed_hosts:
        # 从 detected_external_services + detected_required_secrets 反推
        - api.anthropic.com

# ============================================================
# Block 3 · Vault hints（不调 Anthropic API）
# 订阅者首次 onboarding 时，KOL 平台引导填这些 secrets 进 vault
# ============================================================
vault_hints:
  required_credentials:
    - name: GITHUB_TOKEN
      purpose: "MCP github server 认证"
      used_by: { mcp_server: github }
      from: .env.example
      required: true
    - name: ANTHROPIC_API_KEY
      purpose: "Anthropic API 调用"
      used_by: { agent: true }
      from: .env.example
      required: true                 # 但 conductor 通常代收，订阅者不必单独填
      note: "If subscribers use platform-managed billing, this is auto-injected"

  optional_credentials:
    - name: STRIPE_API_KEY
      purpose: "可选支付集成（仅当 KOL 启用了订阅订单 skill）"
      from: .env.example
      required: false

# ============================================================
# Provenance（v0.1 已有，保留）
# ============================================================
provenance:
  detection_report: .askdao/detection.json
  
  reasoning_summary: |
    项目是 Python 3.12 FastAPI 后端 + Postgres。检测到生产部署 workflow，
    所以 agent.tools 对 bash/write 设 always_ask；其他工具默认 always_allow。
    Skills 推荐 xlsx（基于 pandas+openpyxl 依赖）。
    MCP server github 需要订阅者填 GITHUB_TOKEN 进 vault。
  
  reasoning_decisions:
    - decision: "model=claude-opus-4-6"
      reason: "复杂后端业务多步推理"
      confidence: 0.78
    - decision: "tools.bash + tools.write 设 always_ask"
      reason: "检测到 .github/workflows/deploy.yml + config/production.toml — 高风险信号"
      confidence: 0.85
    - decision: "推荐 anthropic skill xlsx"
      reason: "依赖 pandas + openpyxl 强暗示报表场景"
      confidence: 0.82
  
  generated_at: 2026-05-06T14:23:11Z
  generator_version: askdao-cli/0.2.0

# ============================================================
# Memory / Guardrails（plan/06 §5 已有，保留）
# 注：这两块是 conductor 业务字段，不上送 Anthropic
# ============================================================
memory:
  fact_extraction: enabled
  episode_summary: enabled

guardrails:
  credential_filter: enabled
  kol_memory_redact: enabled

# ============================================================
# Status（首次 init 为空；apply 后回写）
# ============================================================
status:
  last_applied_at: null
  remote_agent_id: null            # 例如 "agt_xyz789"
  remote_agent_version: null       # 🆕 Agent 版本钉（v0.2 新加）
  remote_environment_id: null      # 例如 "env_abc123"
  vault_setup_complete: false      # 🆕 订阅者 vault 是否已配齐
  drift_detected: false
```

**对 plan/06 §5 的兼容性**：
- 旧字段（name/version/visibility/expertise_level/group_name/persona_file/memory/guardrails）100% 保留
- **重大重构**：旧的扁平字段（model / tools / skills / resources）全部移入 `agent` block 内
- **新增** `agent` / `environment` / `vault_hints` 三块顶层布局
- **新增** `provenance` / `status` 块作为元数据
- 需配套修订 `plan/06 §5` 描述（落地路径 §10 已列出）

**与 conductor 的衔接**：
- `askdao agent deploy` 按顺序调三个 API：先 environment.create → 再 agent.create（引用 mcp_servers 但不带 vault）→ 写回 PG agent_spec 表
- conductor PG `agent_spec` 表需 alembic 017 加 2 列：`managed_agent_version: int` + `vault_hints_json: jsonb`
- vault 不在 deploy 时创建，KOL onboarding 订阅者时由 conductor 引导填 vault

---

## 6. Go 工程量估算

| 模块 | 来源 | 估算 |
|-----|------|------|
| `internal/scanner/syft.go` | wrap `anchore/syft` | 80 行 |
| `internal/scanner/enry.go` | wrap `go-enry/enry` | 50 行 |
| `internal/scanner/dockerfile.go` | wrap `moby/buildkit` parser | 80 行 |
| `internal/scanner/dev_filter.go` | manifest 主文件 dev/prod 区分（Python/Node/Rust） | 200 行 |
| `internal/scanner/runtimes.go` | `.nvmrc` / `.python-version` etc. 解析 | 80 行 |
| **🆕 `internal/scanner/mcp_config.go`** | `.mcp.json` / `claude_desktop_config.json` JSON 解析 + Anthropic 兼容性检测（type=url 过滤） | 60 行 |
| **🆕 `internal/scanner/skills_dir.go`** | 5+ 个 skill dir 约定扫描（`.claude/skills/` / `.agents/skills/` ...）+ 依赖反推内置 skill 推荐 | 80 行 |
| **🆕 `internal/scanner/secrets_hint.go`** | `.env.example` keys 抽取 + service-to-secret 映射 + MCP server 反查 | 120 行 |
| `internal/providers/provider.go` | Provider interface + App/Env 抽象 | 120 行 |
| `internal/providers/python.go` | 移植 nixpacks python.rs | 250 行 |
| `internal/providers/node.go` | 移植 nixpacks node/mod.rs | 300 行 |
| `internal/providers/go.go` | 移植 | 150 行 |
| `internal/providers/rust.go` | 移植 | 150 行 |
| `internal/providers/apt_map.go` | 反向映射表（数据为主） | 100 行 |
| **🆕 `internal/recommender/policy.go`** | tool permission_policy 启发式（生产信号检测 → bash/write 单独 always_ask） | 100 行 |
| `internal/recommender/llm.go` | 调 conductor LLM endpoint 生成三块 yaml + reasoning | 250 行（v0.1: 200, +50 因为多了 agent block 推断） |
| `internal/types/detection.go` | detection.json schema（含 4 个新字段） | 200 行（v0.1: 150, +50） |
| `internal/types/agent_yml.go` | agent.yml schema（三块布局，与 conductor pydantic 对齐） | 350 行（v0.1: 250, +100 因 Agent block 字段全） |
| `cmd/askdao/init_auto.go` | 命令实现 | 150 行 |
| `cmd/askdao/detect.go` | 命令实现 | 80 行 |
| 总计（Phase 1） | | **~2950 行 Go** |

包括基础测试，**Phase 1 估算仍在 3-4 周区间**（一个工程师全职；4 个新模块都是数据驱动 + 简单解析，比 nixpacks providers 移植轻）。

---

## 7. Phase 1 MVP vs Phase 2

### Phase 1 MVP（修身期够用）

- ✅ L1：syft 调 CLI 进程模式（不 import）
- ✅ L2：Python (uv/poetry/pip-tools) + Node (npm/pnpm/yarn) 两个生态的 dev/prod 过滤
- ✅ L3：移植 nixpacks 4 个 provider（python/node/go/rust）+ apt 反向映射表
- ✅ L4：调 conductor 现有 `_managed_agents_stream` 生成 system_prompt + reasoning（用 KOL 自己的 Anthropic key 还是 conductor 中转 → 见 §9 决策项）
- ✅ `askdao agent init --auto` + `askdao detect` 两个命令
- ✅ 保守 networking（type=limited，只放 anthropic.com）

### Phase 2 增强

- ⏳ L1：syft 改 Go library 直接 import（去外部依赖）
- ⏳ L3：扩展到 Java/Ruby/PHP/Elixir
- ⏳ Monorepo 多 workspace 支持（pnpm-workspace / cargo workspaces）
- ⏳ `askdao agent regenerate` diff 模式
- ⏳ Skill 自动初始化（从 README 抽核心能力建议初始 Skill）
- ⏳ Secret in code 扫描（gitleaks 集成）
- ⏳ 增量扫描 + cache（大仓库提速）

---

## 8. 与 plan/06 已有 ADR 的关系

| plan/06 ADR | 本设计的关系 |
|------------|------------|
| §4.2 `agent init` 空骨架 | **扩展**：加 `--auto` 模式，骨架基础上自动填 |
| §4.3 `agent validate` | **复用**：自动生成的 yaml 也走同一个 validator |
| §4.4 `agent deploy` 事务三件套 | **不变**：deploy 仍读 yaml，本设计只影响"yaml 怎么来的" |
| §5 AgentSpec yaml schema | **新增 environment + provenance 两个 block**，需要更新 conductor pydantic AgentSpec 模型 |
| §6.1 CLI 框架（Typer） | **冲突**：本设计假设 askdao-cli 用 Go（按 memory pivot）。若仍用 Python 则全部估算翻倍（Python 没有 syft/enry/nixpacks 同等成熟生态） |

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

---

## 10. 落地路径（建议下一步）

1. **本设计文档 v0.2 评审 → ADR 编号**：编入 `harness-design/primitives/`（待 ADR 体系扩展，可能编为 `06-agent-bootstrap.md` 涵盖三资源整合而非单独 environment）
2. **更新 plan/06**：
   - §4.2 改写为带 `--auto` 路径
   - §5 AgentSpec 重写为三块布局（agent / environment / vault_hints）
3. **更新 plan/01 + alembic**：
   - alembic 017 加 2 列：`managed_agent_version` + `vault_hints_json`
   - 同步更新 plan/02 conductor 业务字段说明
4. **更新 plan/03 + Conductor**：
   - 加 `POST /api/v1/cli/recommend` endpoint（决策 9.1）
   - 现有 ManagedAgentsClient 适配 yaml 三块布局
5. **GitHub Issue 拆分**：按 §6 工程量切成 ~7 个 task（多了 4 个 scanner + policy 推断器），进 askdao-cli repo

---

## 附录 · 参考资料

- v0.2 设计 review 详细记录：[`review-2026-05-06.md`](./review-2026-05-06.md)
- 上游 spike 报告：[`investigations/syft-spike-for-askdao-cli.md`](./investigations/syft-spike-for-askdao-cli.md)
- 上游 spike 报告：[`investigations/nixpacks-provider-pattern.md`](./investigations/nixpacks-provider-pattern.md)
- Anthropic Managed Agents 官方文档（v0.2 重读后引用，同 org 私有仓库）：
  - `harness-design/claude-managed-agents-docs/docs/managed-agents/agent-setup.md`
  - `harness-design/claude-managed-agents-docs/docs/managed-agents/environments.md`
  - `harness-design/claude-managed-agents-docs/docs/managed-agents/skills.md`
  - `harness-design/claude-managed-agents-docs/docs/managed-agents/mcp-connector.md`
  - `harness-design/claude-managed-agents-docs/docs/managed-agents/vaults.md`
  - `harness-design/claude-managed-agents-docs/api/python/managed-agents/agents/create.md`
  - `harness-design/claude-managed-agents-docs/api/python/managed-agents/environments/create.md`
- Warp Oz 设计参考：`harness-design/warp-oz-docs/cloud-agents/environments.md`（同 org 私有仓库）
- Anthropic SDK environment schema：`anthropic/types/beta/environment_create_params.py`（SDK 公开源码）
- 相关 memory：`project_askdao_cli_design_pivot_2026_05_05.md`
