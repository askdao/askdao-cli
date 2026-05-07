# askdao-cli Environment Bootstrap (`askdao agent init --auto`)

> **Scope**: plan/06-deploy-cli.md §4.2 `askdao agent init` 命令的智能化补强 ——
> 让 KOL 在自己项目目录下跑一行命令，自动产出 `agent.yml` 草稿（含 Anthropic environment 配置），而不是从空白模板手填。
>
> **Status**: Design draft — pending review
> **Date**: 2026-05-06
> **Owner**: Sam
> **Aligns with**: memory `project_askdao_cli_design_pivot_2026_05_05.md`（Go + 借鉴 Oz Environment 一等抽象）

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
┌──────────────────────────────────────────────┐
│  L1 · syft (Go library)                       │  确定性扫描
│  → 1000+ 包+版本（含传递依赖）                 │
└──────────────┬───────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────┐
│  L2 · dev-filter                              │  manifest 二次解析
│  pyproject.toml / package.json 区分 dev/prod  │
│  → ~50 个生产直接依赖                          │
└──────────────┬───────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────┐
│  L3 · providers (移植 nixpacks)               │  框架推断 + 系统包反向映射
│  detect()/Plan() 每语言一个 provider          │
│  → detected_frameworks + apt_pkgs             │
└──────────────┬───────────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────────┐
│  L4 · LLM 推荐器（调 conductor 后端）         │  模糊推断
│  把 detection.json 喂 LLM 生成 yaml + reason  │
└──────────────┬───────────────────────────────┘
               │
               ▼
            agent.yml + .askdao/detection.json
```

**层间分离原则**：L1-L3 全确定性，离线可跑，零成本；L4 才调 LLM。

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
→ Inferred system packages: libpq-dev, gcc
→ Calling LLM for system_prompt + reasoning ...

✓ Generated my-agent/agent.yml (draft)
✓ Saved my-agent/.askdao/detection.json (provenance)

Next:
  cd my-agent && vim agent.yml persona.md
  askdao agent validate
  askdao agent deploy
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

---

## 5. agent.yml schema（L4 LLM 输出 + 与 plan/06 §5 兼容）

`init --auto` 最终落盘的文件，是 plan/06 §5 AgentSpec yaml 的**自动填充版**。新增三个字段（紫色标注），其余完全兼容 plan/06：

```yaml
# my-agent/agent.yml (auto-generated draft, KOL should review/edit)
name: my-agent
version: 0.1.0
visibility: private
expertise_level: pro
domain:
  - backend-engineering         # ← LLM 从 frameworks 反推
  
model: claude-opus-4-7           # ← LLM 推荐（多步推理 → opus）
persona_file: persona.md
group_name: "My Agent Group"

# ============================================================
# 🆕 environment 块（plan/06 当前没有，本设计新增）
# 直接对齐 Anthropic environment_create_params
# ============================================================
environment:
  config:
    type: cloud
    
    packages:
      type: packages
      pip:
        # 来自 detected_packages.pip[is_prod=true]
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
      type: limited                       # 默认保守
      allow_mcp_servers: true
      allow_package_managers: false
      allowed_hosts:
        # 从 detected_env_files + detected_external_services 反推
        - api.anthropic.com

# ============================================================
# tools / skills / resources（plan/06 §5 已有，自动填）
# ============================================================
tools:
  - agent_toolset_20260401      # 标配
  
skills:                          # init --auto 不自动创建 Skill；KOL 后续手加
  []

resources:
  - path: ./resources/README.md  # 仅自动复制 README 作为初始 resource
    tag: project-overview

# ============================================================
# 🆕 provenance 块（透明度 + 让 KOL 知道为什么这么推）
# ============================================================
provenance:
  detection_report: .askdao/detection.json
  
  reasoning_summary: |
    项目是 Python 3.12 FastAPI 后端 + 一些 TypeScript 工具脚本。已识别 28 个生产
    Python 依赖（自动过滤 14 个 dev 依赖）；apt 包基于 asyncpg(psycopg) 推断需要
    libpq-dev + gcc。建议 model=opus-4-7 因为后端业务包含数据库迁移 + 多步推理。
    networking=limited 保守策略，仅放行 Anthropic API。
  
  reasoning_decisions:
    - decision: "选 claude-opus-4-7 而非 sonnet"
      reason: "后端 + 数据库迁移逻辑复杂，opus 多步推理更稳"
      confidence: 0.78
    - decision: "过滤 pytest/mypy/ruff/black"
      reason: "dev 依赖运行时不需要，省 environment 体积"
      confidence: 0.99
  
  generated_at: 2026-05-06T14:23:11Z
  generator_version: askdao-cli/0.1.0

# ============================================================
# memory / guardrails（plan/06 §5 已有）
# ============================================================
memory:
  fact_extraction: enabled
  episode_summary: enabled

guardrails:
  credential_filter: enabled
  kol_memory_redact: enabled
```

**对 plan/06 §5 的兼容性**：
- 旧字段（name/version/visibility/expertise_level/model/tools/skills/resources/memory/guardrails）100% 保留
- 新增 `environment` 块 —— 这是把 plan/06 漏掉的 Anthropic environment 字段第一次明确化
- 新增 `provenance` 块 —— 纯元数据，deploy 时不上送，可手动删掉无影响

---

## 6. Go 工程量估算

| 模块 | 来源 | 估算 |
|-----|------|------|
| `internal/scanner/syft.go` | wrap `anchore/syft` | 80 行 |
| `internal/scanner/enry.go` | wrap `go-enry/enry` | 50 行 |
| `internal/scanner/dockerfile.go` | wrap `moby/buildkit` parser | 80 行 |
| `internal/scanner/dev_filter.go` | manifest 主文件 dev/prod 区分（Python/Node/Rust） | 200 行 |
| `internal/scanner/runtimes.go` | `.nvmrc` / `.python-version` etc. 解析 | 80 行 |
| `internal/providers/provider.go` | Provider interface + App/Env 抽象 | 120 行 |
| `internal/providers/python.go` | 移植 nixpacks python.rs | 250 行 |
| `internal/providers/node.go` | 移植 nixpacks node/mod.rs | 300 行 |
| `internal/providers/go.go` | 移植 | 150 行 |
| `internal/providers/rust.go` | 移植 | 150 行 |
| `internal/providers/apt_map.go` | 反向映射表（数据为主） | 100 行 |
| `internal/recommender/llm.go` | 调 conductor LLM endpoint 生成 yaml | 200 行 |
| `internal/types/detection.go` | detection.json schema | 150 行 |
| `internal/types/agent_yml.go` | agent.yml schema（与 conductor pydantic 对齐） | 250 行 |
| `cmd/askdao/init_auto.go` | 命令实现 | 150 行 |
| `cmd/askdao/detect.go` | 命令实现 | 80 行 |
| 总计（Phase 1） | | **~2400 行 Go** |

包括基础测试，**Phase 1 估算 3-4 周可落地**（一个工程师全职）。

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

## 9. 待哥拍板的关键决策

1. **L4 LLM 调用走哪条路**？
   - **(A) BYOK**：askdao-cli 直连 Anthropic，KOL 用自己 key。隐私强但需 KOL 配 key。
   - **(B) Conductor 中转**：askdao-cli → conductor 后端 → Anthropic。简单，conductor 承担流量。
   - **倾向**：**B**（conductor 中转），因为 Phase 1 KOL 大概率还没自己的 Anthropic key。

2. **syft 的接入方式**？
   - **(A) spawn CLI 进程读 stdout JSON**：MVP 快，跟进 syft release 零成本。askdao-cli 安装时单独装 syft。
   - **(B) 直接 Go library import**：用户装 askdao-cli 即可用，但二进制 ~80 MB。
   - **倾向**：**Phase 1 用 A，Phase 2 评估 B**（取决于哥对二进制大小的容忍度）。

3. **`environment` 是否也写进 plan/06 §5 的 AgentSpec 主 schema**？
   - 若是 → conductor 的 pydantic 模型也要加这个块，agent_spec 表新增列存 environment 配置
   - 若否 → environment 块只是 askdao-cli 落盘的辅助配置，deploy 时单独走另一条 API
   - **倾向**：**写进主 schema**，让 yaml 完全自包含，KOL 一眼看清"我的 Agent 跑在什么环境里"。

---

## 10. 落地路径（建议下一步）

1. **本设计文档评审 → ADR 编号**：编入 `harness-design/primitives/06-environment-bootstrap.md`（如果哥同意 environment 一等抽象的方向）
2. **更新 plan/06**：把 §4.2 改写为带 `--auto` 路径；§5 AgentSpec 加 environment + provenance 块
3. **更新 plan/01**：alembic 加 `agent_spec.environment_config` JSONB 列（若决策 9.3 = 主 schema）
4. **GitHub Issue 拆分**：按 §6 工程量切成 ~6 个 task，进 askdao-cli repo（独立仓库，按 memory `feedback_kol_local_tool_must_be_oss.md`）

---

## 附录 · 参考资料

- 上游 spike 报告：[`investigations/syft-spike-for-askdao-cli.md`](./investigations/syft-spike-for-askdao-cli.md)
- 上游 spike 报告：[`investigations/nixpacks-provider-pattern.md`](./investigations/nixpacks-provider-pattern.md)
- Warp Oz 设计参考：`harness-design/warp-oz-docs/cloud-agents/environments.md`（同 org 私有仓库）
- Anthropic SDK environment schema：`anthropic/types/beta/environment_create_params.py`（SDK 公开源码）
- 相关 memory：`project_askdao_cli_design_pivot_2026_05_05.md`
