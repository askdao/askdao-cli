# askdao-cli · Project Handoff

> 新会话上手 / 上下文切换的入口文档。读完这一页就能继续工作。
>
> Last updated: 2026-05-13

---

## Current Status

**Phase 1 已实装 · M4 Deploy CLI 补完中（Issue 4 = `agent deploy` 去 stub，已完成）**

| 项 | 状态 |
|---|------|
| 设计文档 (design.md) | v0.5 定稿（1366 行，§3 命令骨架 + §5 yaml schema + §9 决策记录是真相源）|
| Phase 1 代码（askdao-cli #1-8） | 已交付 —— `internal/{types,scanner,providers,pipeline,recommender,render}` + `cmd/askdao`（`detect` / `agent init` / `agent show` / `agent deploy`）|
| conductor 端（原 #15-17） | 已 merge —— `app/agents/{spec,adapters}` + `app/api/cli.py`（`POST /api/v1/cli/recommend` + `POST /api/v1/cli/deploy`）|
| M4 Deploy CLI 补完 — conductor Issue 1-3 | 已 merge（alembic 024）—— `skill_registry` 三层 URI（`viking://resources/skills/{visibility}/{scope_id}/{skill_id}/{version}/`）+ OV→Managed 单向 sync（`app/skills/`）+ `/cli/deploy` 改 `multipart/form-data` + 事务建 Agent↔Group + owner GroupMembership + `409 kol_profile_required` + `groups.create_group_with_owner`（ADR-P15/P16/P18/P21）|
| M4 Deploy CLI 补完 — askdao-cli Issue 4 | ✅ 本次 —— `agent deploy` 去 stub，接 conductor `/cli/deploy`（multipart + custom skill zip 上传 + KOL 资料隐式补全 + HIGH-warning gating），新 `internal/deploy/` 包；GitHub issue [#20](https://github.com/askdao/askdao-cli/issues/20)|
| 待办 | M4 Issue 5（conductor `app/users/kol_subscription.py:subscribe()` 自动 join KOL 旗下所有 Agent Group）+ Issue 6（askdao-ai-web：「成为 KOL」表单 + `/k/{kolId}/g/{groupId}` Group 对话路由 + dashboard 去 mock）|

---

## Quick Start for New Session

1. **读这份 HANDOFF.md** — 你现在在这
2. **读 [`design.md`](./design.md)** — v0.5 设计真相源（重点看 §3 命令骨架 + §5 yaml schema + §9 决策记录）
3. **读 L2 CLAUDE.md** — `cmd/askdao/CLAUDE.md`（命令实装）+ `internal/deploy/CLAUDE.md`（deploy 客户端）+ `internal/{recommender,render,pipeline}/CLAUDE.md`
4. **看 GitHub issues** — askdao-cli #1-8（Phase 1，已 closed）+ #20（M4 Issue 4）；M4 整体见 askdao-cloud-conductor 侧 + harness-design `primitives/04-agent-deployment-pipeline.md`（ADR-P15/P16/P18/P21）
5. **下一步**：M4 Issue 5（conductor 侧 `subscribe()` 自动 join Group）/ Issue 6（askdao-ai-web）—— 各自独立分支 / PR（DEV-FLOW：一 issue 一分支）

---

## M4 — Deploy CLI 补完（`agent deploy` ↔ conductor `/cli/deploy`）

`agent deploy` 把 KOL 编辑好的 `agent.yml`（+ 可选 `detection.json` + 每个 `custom_local` skill 的目录 zip）以 `multipart/form-data` POST 到 conductor `POST /api/v1/cli/deploy`：

| 端 | 实装位置 |
|---|---|
| askdao-cli | `internal/deploy/`（`Client.Deploy` 构 multipart / `Client.SetupKol` PATCH kol-profile / `ZipDir` 打包 skill 目录 / `Err{KolProfileRequired,BlockingWarnings}`）+ `cmd/askdao/deploy.go`（编排：读 agent.yml 原始字节 → 可选 diff → 枚举 + 打包 `custom_local` skills → Deploy → `409 kol_profile_required` 交互/`--bio` 补全 + 重跑 → HIGH-warning gating（`--force`）→ 打印结果） |
| conductor | `app/api/cli.py:deploy_agent_spec`（multipart）+ `app/skills/sync.py:sync_skill_zip`（OV 三层真源 → Managed Skills 副本 → `skill_registry`）+ `app/agents/adapters/anthropic_adapter.py:adapt(spec, detection, resolved_skills)` + `app/api/groups.py:create_group_with_owner`；alembic 023（`skill_registry`）+ 024（`agent_spec.group_id` unique index） |

deploy 流程（ADR-P15/P16/P18/P21）：① conductor 检测 `user.kol_join_mode IS NULL` → `409 {"detail":{"reason":"kol_profile_required","fields":["kol_join_mode","kol_bio"],"hint":"..."}}` → CLI prompt bio（或 `--bio`）→ `PATCH /api/v1/users/me/kol-profile`（`kol_join_mode=free`）→ 重跑 deploy ② 每个 `custom_local` skill 经 `sync_skill_zip` 上传 ③ `AnthropicAdapter.adapt` 把 `resolved_skills` 回填到 `agent_params.skills`；`translation_report.has_blocking()` 且 `force≠true` → `409` 带 `translation_report` → CLI `--force` 才过 ④ Anthropic Beta `environments/agents.create` ⑤ PG 事务：写 `agent_spec`（带 `skills`）+ 自动建 `Group`（无 `group_id` 时，`grp_<sha1(agent_id)[:24]>`）+ owner `GroupMember` + 回填 `agent_spec.group_id` ⑥ 回传 `agent_id` / `anthropic_agent_id` / `anthropic_environment_id` / `group_id` / `group_link` / `skills` / `translation_report`。

ADR 锚点：`harness-design/primitives/04-agent-deployment-pipeline.md`（ADR-P15 skill 三层 URI / P16 Shadowing / P18 OV 真源 + 单向 sync / P21 deploy 事务三件套 + KOL 资料隐式补全）+ `plan/06-deploy-cli.md` §4.4。env：`agent deploy` 需 `ASKDAO_CONDUCTOR_URL` + `ASKDAO_CONDUCTOR_TOKEN`（都必填）。

未做（P2）：re-deploy 走 conductor `/diff`（ADR-P19）；远端 ID 写回 `agent.yml` `status:`；`askdao profile setup-kol/show-kol` 独立子命令；`askdao agent validate`；`askdao agent init` 生成 skill 骨架；`~/.askdao/config.yaml` 配置文件。

---

## Document Map

```
askdao-cli/
├── docs/
│   ├── HANDOFF.md ⭐ 你在这（一次性切换入口）
│   ├── design.md   ⭐ 设计真相源 (v0.5 / 1366 行)
│   ├── review-2026-05-06.md       # v0.2 review (Anthropic 三资源模型)
│   ├── review-v0.3-2026-05-06.md  # v0.3 review (中间格式)
│   ├── review-v0.4-2026-05-06.md  # v0.4 review (Dockerfile 兼容性)
│   ├── review-v0.5-2026-05-06.md  # v0.5 review (中等详情卡片 UX)
│   └── investigations/
│       ├── syft-spike-for-askdao-cli.md
│       └── nixpacks-provider-pattern.md
├── CLAUDE.md          # L1 项目宪法
├── README.md          # 英文，对外门面
├── cmd/askdao/main.go # 入口骨架
├── go.mod / Makefile / LICENSE / .gitignore
```

**外部相关文档**（同 org 私有仓库）：
- `harness-design/claude-managed-agents-docs/` — Anthropic Managed Agents 官方文档
- `harness-design/openai-agents-sdk-docs/` — OpenAI Agents SDK 文档
- `harness-design/warp-oz-docs/` — Warp Oz 设计参考
- `harness-design/archived-version/harness-selection-analysis.md` — 历史 harness 选型分析
- `askdao-cloud/orchestrator/ALWAYS/RESOURCE-MAP.yml` — 全局资源索引

---

## Phase 1 Issue 索引

详见 [`reference_phase1_issues.md`](../../../.claude/projects/-Users-sunmu-WorkSpace-askdao-cloud/memory/reference_phase1_issues.md)（在你的 auto memory 里）。简版：

### askdao-cli 端 · 8 issue（target main，每个走自己短分支）

| # | 标题 | 行数 | Deps |
|---|------|------|------|
| [#1](https://github.com/askdao/askdao-cli/issues/1) | Foundation: schemas | 620 | 无 |
| [#2](https://github.com/askdao/askdao-cli/issues/2) | Scanner: syft + enry + Dockerfile AST | 430 | #1 |
| [#3](https://github.com/askdao/askdao-cli/issues/3) | Scanner: 6 supplementary | 600 | #1, #2 |
| [#4](https://github.com/askdao/askdao-cli/issues/4) | Providers: Python + Node | 770 | #1, #3 |
| [#5](https://github.com/askdao/askdao-cli/issues/5) | Providers: Go + Rust | 300 | #4 |
| [#6](https://github.com/askdao/askdao-cli/issues/6) | Recommender: policy + LLM | 380 | #1, #3, #4, conductor #17 (soft) |
| [#7](https://github.com/askdao/askdao-cli/issues/7) | Render UX: 5 modules | 700 | #1 |
| [#8](https://github.com/askdao/askdao-cli/issues/8) | Commands: init/detect/deploy/show | 630 | #1-#7 |

### conductor 端 · 3 issue（target `feature/askdao-cli-integration`）

| # | 标题 | 行数 | Deps |
|---|------|------|------|
| [#15](https://github.com/askdao/askdao-cloud-conductor/issues/15) | alembic 017 multi-runtime cols | 50 | 无 |
| [#16](https://github.com/askdao/askdao-cloud-conductor/issues/16) | AgentSpec + AnthropicAdapter + TranslationReport | 1200 | #15 |
| [#17](https://github.com/askdao/askdao-cloud-conductor/issues/17) | CLI endpoints | 400 | #16 |

### 推荐工作排序

- **Week 1**：#1（foundation） + conductor #15（alembic）—— 两侧 unblock
- **Week 2-3**：askdao-cli #2/#3/#4/#7 并行；conductor #16
- **Week 4**：askdao-cli #5/#6（#6 用 mock conductor 解耦）；conductor #17
- **Week 5-6**：askdao-cli #8 端到端整合；conductor 总 PR `feature/askdao-cli-integration` → `main`

---

## Decisions Made (Quick Reference)

| # | 决策 | 选择 |
|---|------|------|
| 9.1 | LLM 调用走哪条路 | B · 通过 Conductor 中转 |
| 9.2 | syft 接入方式 | A · spawn CLI 进程（Phase 1）|
| 9.3a | yaml schema 布局 | 三块 → v0.3 升级为 8 块 harness-neutral |
| 9.3b | conductor agent_spec 表 | 加 3 列：`managed_agent_version` + `vault_hints_json` + `runtime_id` |
| 9.5 | yaml 格式 | harness-neutral 中间格式（apiVersion: askdao.ai/v1） |
| 9.6 | 多 harness 支持分阶段 | Phase 1 单 adapter / Phase 2 OpenAI / Phase 3 更多 |
| 9.7 | Dockerfile 兼容 | 选项 B · workspace 加 5 字段 + Anthropic 端 translation report |
| 9.8 | GPU 声明 | 不做（AskDAO 不跑 ML 任务） |
| 9.9 | KOL 审阅 UX | 中等详情卡片 7 块 + inline reasoning + 入口扩展 |

---

## Architecture Summary

```
askdao-cli (Go)                    conductor (Python)
─────────────                       ─────────────────
                                    
本地扫描（用户机器）                   云端 adapter
                                    
L1 syft → packages                  AnthropicAdapter (Phase 1)
L2 dev_filter / mcp / skills /        ↓ POST /v1/agents
   secrets / harness signals          ↓ POST /v1/environments
L3 nixpacks providers + apt_map →     
L4 LLM (via conductor) →            OpenAIAdapter (Phase 2)
                                      ↓ SandboxAgent + Manifest
agent.yml (askdao.ai/v1)              ↓ Runner.run_streamed()
   8 顶层块：metadata / persona /
   capabilities / mcp_servers /     更多 adapter (Phase 3)
   custom_tools / skills /            ↓ LangGraph / Vercel OA / ...
   workspace / vault_hints
```

---

## Known Pitfalls (本次会话踩到的坑)

工程小坑（避免重复浪费时间）：

1. **`gh api` 创建 issue 含换行 body** → 必须用 `--body-file file.md`，不能 `-f body="...\n..."`（已有 memory `feedback_gh_api_multiline_body.md`）
2. **`gh label create` 颜色** → 不能带 `#` 前缀，直接传 `0e8a16`
3. **zsh 的 `rm` / `cp` 默认带 `-i` 别名** → 删除/覆盖会卡住等输入。用绝对路径绕过：`/bin/rm -f` / `/bin/cp -f`
4. **GitHub project board 里某个 issue 已存在** → `gh project item-add` 失败时不影响其他，可忽略
5. **中文括号 vs 西文括号** → Edit 工具要求精确匹配，old_string 复制时注意 `（` vs `(`
6. **harness-design 里 designs/ 和 investigations/ 的 askdao-cli 文档已迁出** → 现统一在 askdao-cli/docs/ 下
7. **conductor `_session_cleanup_loop` 已支持 managed_agents 路径**（候选 B #28 完成）—— 引用 conductor CLAUDE.md 时这个事实是当前状态

---

## Recommended Opening Line for Next Session

复制这段开新会话即可：

```
哥，我要继续 askdao-cli / M4 Deploy CLI 的工作。

请先读 docs/HANDOFF.md（重点 "Current Status" + "M4 — Deploy CLI 补完"），
再读 cmd/askdao/CLAUDE.md + internal/deploy/CLAUDE.md。

下一步是 M4 Issue 5（conductor subscribe() 自动 join Group）/ Issue 6（askdao-ai-web）。
```

---

## What This Document is NOT

- **不是** 决策真相源（那是 design.md + reviews + harness-design `primitives/04`）
- **不是** 长期维护文档（每次 Phase / milestone 完成可以重写或归档）
- **是** 上下文切换辅助 + 当前状态快照（Phase 1 已实装 + M4 Deploy CLI 进度）
