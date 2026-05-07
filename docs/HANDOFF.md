# askdao-cli · Project Handoff

> 新会话上手 / 上下文切换的入口文档。读完这一页就能继续工作。
>
> Last updated: 2026-05-07

---

## Current Status

**Phase 1 立项完成 · 尚未开始编码**

| 项 | 状态 |
|---|------|
| 设计文档 (design.md) | v0.5 定稿（1366 行）|
| Review 文档 | 4 份（v0.2 / v0.3 / v0.4 / v0.5）|
| Spike 报告 | 2 份（syft / nixpacks-pattern）|
| GitHub Issues | 11 个 ready（askdao-cli #1-8 + conductor #15-17）|
| GitHub Project | #3 已加全部 11 个 issue |
| conductor feature 分支 | `feature/askdao-cli-integration` 已建 |
| askdao-cli 代码 | 仅骨架（main.go hello world）|

---

## Quick Start for New Session

要继续 Phase 1 开发，按以下顺序：

1. **读这份 HANDOFF.md** — 你现在在这
2. **读 [`design.md`](./design.md)** — v0.5 设计真相源（重点看 §3 命令骨架 + §5 yaml schema + §9 决策记录）
3. **看 GitHub Project #3** — `gh project view 3 --owner askdao` 看任务板状态
4. **从 issue #1 / #15 开始**：
   - askdao-cli #1（Foundation schemas）—— `gh issue view 1 --repo askdao/askdao-cli`
   - conductor #15（alembic 017）—— `gh issue view 15 --repo askdao/askdao-cloud-conductor`
   - 这两个无 dependency，可并行
5. **conductor 端必读**：所有 conductor 相关工作必须 base on `feature/askdao-cli-integration` 分支（不直接到 main）

```bash
# conductor 端开干前
cd /Users/sunmu/WorkSpace/askdao-cloud/askdao-cloud-conductor
git fetch origin
git checkout feature/askdao-cli-integration
git pull
# 从这里再开 issue 自己的 feature 分支
git checkout -b feature/issue-15-alembic-017
```

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
哥，我要继续 askdao-cli Phase 1 的工作。

请先读 docs/HANDOFF.md，
然后再读 docs/design.md 进入语境。

我准备从 issue #1 (Foundation: schemas) 开始。
```

或者哥要从其他 issue 起步，把上面的 `#1` 换掉即可。

---

## What This Document is NOT

- **不是** 决策真相源（那是 design.md + reviews）
- **不是** 长期维护文档（每次 Phase 完成可以重写或归档）
- **是** 上下文切换辅助 + Phase 1 启动状态快照
