# askdao-cli · Project Handoff

> 新会话上手 / 上下文切换的入口文档。读完这一页就能继续工作。
>
> Last updated: 2026-05-21

---

## v0.8 — `agent edit` 本地 Web 工作台

> observe-layer-design.md 评审纠偏后启动。立足点 = **Skills Pipeline + Anthropic Managed Agents MVP**。
> 纠偏全过程 + 决策见 [`review-observe-pivot-2026-05-21.md`](./review-observe-pivot-2026-05-21.md)。

### ✅ 第一步已完成（已 commit + push）

- **commit** `0a49333`（38 files, +1806/-1062），分支 `feature/agent-edit-web-studio` 已 push 到 `origin`，**PR 未开**（入口 `https://github.com/askdao/askdao-cli/pull/new/feature/agent-edit-web-studio`）。
- 全测试绿（`go test ./...`）+ `make build` 通过。二进制 `./askdao`（git-ignored）。
- plan：`~/.claude/plans/anthropic-managed-pure-pixel.md`。

**交付内容**：
- **命令精简**：审阅入口从 init/show + `[A/E/R/S/...]` 字符菜单收敛为 `askdao agent edit` 本地 Web 工作台；命令集 = `auth` + `agent edit/deploy`，删 init/show/detect/bundle/argparse。
- **webstudio**（`internal/webstudio/`）：`127.0.0.1` 随机端口 + go:embed 自包含单页；写 yaml/deploy 由 cmd 注入 `OnSave`/`OnDeploy` 回调解耦（不依赖 pipeline/deploy）。
- **harness 感知双 scope 扫描**（`internal/scanner/harness_scope.go`）：`.claude/`→扫 `~/.claude/skills`+`~/.claude.json`；`DetectedSkill`/`DetectedMCPConfig` 加 `Scope`/`Harness`；`archetype` 跳过 user scope；`skills_builder` 默认只产 project scope（修复 skill 过度包含）。
- **schema**：`metadata.category` + `theme_color`（预设色板 token，`webstudio/theme.go`）+ `Skill.Scope`。
- **deploy 复用**：`cmd/askdao/deploy.go` 的 `deployFromDir`/`packageSkills`（含 user-scope skill 的绝对/`~` 路径解析）。

**前端 4 轮打磨后的最终形态**（`internal/webstudio/studio.html`，Kami 设计系统）：
全宽 topbar（屏幕左上 AskDAO logo + "AskDAO Agent"）+ 主体/footer 限宽 920 居中聚焦；4 步向导（Identity → Persona → Skills & Tools → Review）；accent 实时跟随 theme_color；Step1 description 多行 + Visibility/Theme 各自独立 card；Step3 **Skills / MCP servers / Secrets 三 tab**（harness 准确名 Claude Code/OpenAI Codex/...；vault_hints 行编辑）；信心度徽标穿插推荐字段（body 级 fixed tooltip，hover 显理由）；Step4 Review 列出选中项**名称 chips**。

### ⏳ 第二步：4 项待续（起点待与哥对齐）

> **重要**：抛"从哪项开始"的选择题时哥选择 clarify，**说明他对第二步的范围/优先级/是否并行另有想法**。新窗口**第一件事先问哥**想澄清什么、第二步怎么排，**不要盲目开做**。下面是我对 4 项的理解（供对齐用）：

- **A. observe hook 预勾（`agent edit --observe`）** — 纯 askdao-cli，**实验性，需先 spike**。用 Claude Code hooks 观测一次真实 session 实际激活的 skill/MCP → 自动预勾选（替代纯手动勾，兑现 observe 重定向后的核心价值）。机制：写临时 `.claude/settings.local.json`（PostToolUse → HTTP hook 指向 webstudio server，server 兼 hook receiver）→ 引导跑一次 → 收集激活项预勾 → 清理（零残留）。**前置 spike**：先验证 Claude Code hooks 能否拿到 *skill 激活* 事件（可能只有 Bash/Edit/MCP tool 事件，skill 激活或需从 MCP tool 调用 / transcript 间接推断）。线索：`harness-design/investigations/` 三份 observe 报告 + `harness-design/claude-managed-agents-docs` hooks 文档。
- **B. 主题色跨仓贯通** — 跨 conductor + askdao-ai-web，确定性高。让 `theme_color`/`category` 在订阅者端真正生效（现 cli 存了下游没用）。conductor `spec.py` 镜像字段 + `/cli/deploy` 持久化 + Group 带色；askdao-ai-web Group 页 `/k/{kol}/g/{group}` 按 token 渲染（对齐 `data-agent-tone`）。**theme_color 是 token 名，三端共用同一张 token→hex 表，源是 `webstudio/theme.go`**。conductor 改动走 long-running 分支（见 memory `feedback_branch_strategy_concurrent_devs`）。
- **C. Codex/Cursor/Cowork harness 路径补全** — 纯 askdao-cli，小。`harness_scope.go` 的 `harnessConventions` 里 codex/cursor 的 `userSkillDirs`/`userMCPFiles` 现为 nil（TODO），cowork 未加 marker。需核对各自全局 skill/MCP 约定（Codex 工作目录 marker = `.agent`，user 根目录/MCP 配置待查 `harness-design/openai-agents-sdk-docs` + codex 文档）。当前 Anthropic MVP 以 Claude Code 为主，价值在未来 harness。
- **D. kol_profile 云端引导** — 依赖云端就绪。`edit` 的 OnDeploy 遇 `kol_profile_required` 已返回"去 askdao.ai 补填"文案；但 `deploy.go` 的 CLI `runDeploy` 仍是旧 `setupKolProfile`（CLI prompt bio + PATCH）。哥定调 KOL profile 归 askdao.ai 云端 → CLI 握手也应改引导（去掉/降级 CLI bio prompt）。依赖 askdao.ai 云端 KOL profile 表单就绪。

### 新窗口怎么继续

1. 读本段（HANDOFF v0.8）+ [`review-observe-pivot-2026-05-21.md`](./review-observe-pivot-2026-05-21.md) + plan `~/.claude/plans/anthropic-managed-pure-pixel.md`。
2. `cd askdao-cli && git checkout feature/agent-edit-web-studio`（已是当前分支）。
3. **先与哥对齐第二步起点/范围/优先级**（见上方"重要"），再动手。
4. 关键文件：`internal/webstudio/{server,api,theme,studio.html,CLAUDE.md}` · `internal/scanner/harness_scope.go` · `cmd/askdao/{edit,common,deploy,main}.go` · `internal/types/{agent_spec,detection}.go` · `internal/pipeline/{pipeline,skills_builder}.go`。
5. 验证习惯：改后 `go test ./...` + `make build`；UI 改动用 `/tmp/mockserve.py` 起 mock + playwright 截图验证（本会话用过的模式，mock 注入 StudioData JSON + serve studio.html/logo.png）。

---

## v0.7 Corrections（2026-05-14，本次会话）

冒烟测试中暴露并修正了四条根基级问题，详见 design.md 新增 §9.12-9.15：

1. **§9.10 lockfile-driven skill 分类（v0.6）建立在错误假设上** —— 以为 Anthropic Managed Agents 有"从 lockfile 重装"的能力。调研 [`../../harness-design/investigations/managed-agents-skill-installation.md`](../../harness-design/investigations/managed-agents-skill-installation.md) §4.1 反映**不存在公共 skill registry**，所有 custom skill 必须 `POST /v1/skills` 上传。修法：**所有 custom skill 一律上传**，vendored vs 原生只是 bundle UI 的 inline origin tag。删除 `SkillReferences` 字段 + `SkillRef` struct + `--bundle-skill` flag + "SKILL REFERENCES" UI section。
2. **§9.12 Agent 项目布局扁平化** —— v0.6 的 `<name>/` 子目录撞自指路径（`~/WorkSpace/homework-spelling/homework-spelling/`）。新布局：`<root>/askdao-agent.yml`（KOL 唯一编辑对象，项目宣言文件）+ `<root>/.askdao/{recommendation.yml,detection.json}`（工具空间）。一个项目 = 一个 agent。
3. **§9.13 信任边界 in L1-L4** —— LLM 越界进入确定性字段是同款病：先撞 `metadata.domain` 标量（已修 normalizer），再撞 `skills` 段乱写（本次修 deterministic builder）。原则：**硬字段 = 模式约束 / ground truth / enum** 一律 deterministic 填充覆盖；**软字段 = 设计决策 / 解释 / persona** 才让 LLM 自由发挥。
4. **§9.14 Skill 上传分层协议 + harness 中性 invariant** —— CLI ↔ Conductor zip per skill（简单内部协议），Conductor ↔ Anthropic multipart 多 part（按 §1.2.1 原生协议）。Conductor 作 anti-corruption layer。Harness 中性 invariant：`filepath.Base(s.Path)` 切掉 `.claude/skills/` / `.agents/skills/` 等上级，Anthropic 端只看到 `tts/SKILL.md` 形态。
5. **§9.15 persona 单一真相源** —— 删 `Metadata.PersonaFile` 字段 + 不再生成 persona.md，所有 prompt 在 `Persona.SystemPrompt` literal block 内。schema 简化 + 故障域消失 + KOL 心智简化。

**PR 拆分**：
- askdao-cli PR _TBD_ —— §9.10 修正 + §9.12-15 全部代码改动（约 ~1200 行净改动）
- askdao-cloud-conductor PR _TBD_ —— spec.py 删 `persona_file` + LLM prompt 改 OMIT skills + adapter `git_repo` wording 同步（约 ~70 行）

---

## Current Status

**Phase 1 已实装 · M4 Deploy CLI 补完中（Issue 4 = `agent deploy` 去 stub，已完成）**

| 项 | 状态 |
|---|------|
| 设计文档 (design.md) | v0.5 定稿 + v0.6 决策记录（§9.10 部署 Payload + §9.11 Plugin 机制调研）；§3 命令骨架 + §5 yaml schema + §9 决策记录是真相源 |
| Phase 1 代码（askdao-cli #1-8） | 已交付 —— `internal/{types,scanner,providers,pipeline,recommender,render}` + `cmd/askdao`（`detect` / `agent init` / `agent show` / `agent deploy`）|
| conductor 端（原 #15-17） | 已 merge —— `app/agents/{spec,adapters}` + `app/api/cli.py`（`POST /api/v1/cli/recommend` + `POST /api/v1/cli/deploy`）|
| M4 Deploy CLI 补完 — conductor Issue 1-3 | 已 merge（alembic 024）—— `skill_registry` 三层 URI（`viking://resources/skills/{visibility}/{scope_id}/{skill_id}/{version}/`）+ OV→Managed 单向 sync（`app/skills/`）+ `/cli/deploy` 改 `multipart/form-data` + 事务建 Agent↔Group + owner GroupMembership + `409 kol_profile_required` + `groups.create_group_with_owner`（ADR-P15/P16/P18/P21）|
| M4 Deploy CLI 补完 — askdao-cli Issue 4 | ✅ 本次 —— `agent deploy` 去 stub，接 conductor `/cli/deploy`（multipart + custom skill zip 上传 + KOL 资料隐式补全 + HIGH-warning gating），新 `internal/deploy/` 包；GitHub issue [#20](https://github.com/askdao/askdao-cli/issues/20)|
| 部署 Payload 清单 + 项目原型识别（v0.6）| ✅ 已交付 PR #19 —— `askdao bundle [path]`（上传清单：lockfile-driven skill 分类 + ignore 链 + 排除给理由）+ `Detection.archetype` / `deployment_payload` + `detect --summary` 加段。设计见 design.md §9.10 |
| Plugin 机制调研（Claude Code Plugin / Codex Plugin）| ⏳ 待决策 —— design.md §9.11：三个层面影响（入口侧 plugin-manifest 检测 / 出口侧 `askdao plugin export` / 架构层 AgentSpec 目标矩阵 + askdao-cloud 发行模型）+ 分阶段推荐路线。本轮只落档不动代码，等上层确认方向 |
| 待办 | M4 Issue 5（conductor `app/users/kol_subscription.py:subscribe()` 自动 join KOL 旗下所有 Agent Group）+ Issue 6（askdao-ai-web：「成为 KOL」表单 + `/k/{kolId}/g/{groupId}` Group 对话路由 + dashboard 去 mock）；plugin 机制阶段 1（scanner 加 plugin-manifest 检测，design.md §9.11）等定夺 |

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

deploy 流程（ADR-P15/P16/P18/P19/P21）：① conductor 检测 `user.kol_join_mode IS NULL` → `409 {"detail":{"reason":"kol_profile_required","fields":["kol_join_mode","kol_bio"],"hint":"..."}}` → CLI prompt bio（或 `--bio`）→ `PATCH /api/v1/users/me/kol-profile`（`kol_join_mode=free`）→ 重跑 deploy ② 每个 `custom_local` skill 经 `sync_skill_zip` 上传 ③ `AnthropicAdapter.adapt` 把 `resolved_skills` 回填到 `agent_params.skills`；`translation_report.has_blocking()` 且 `force≠true` → `409` 带 `translation_report` → CLI `--force` 才过 ④a **update-mode 分支（ADR-P19，2026-05-19 落地）**：按 `(owner_id, yaml.metadata.name)` 查 `registry.find_active_by_owner_name`（alembic 029 partial unique）→ 决定 create / update 路径 ④b 命中 → `environments.update` + `agents.update`（乐观锁 `version` int，BadRequestError "version" retry once）；不命中 → 原 `environments.create` + `agents.create` ⑤ update 路径 in-place mutate `agent_spec` row（复用 agent_id/group_id/managed_*_id/created_at），不重建 Agent↔Group；create 路径 PG 事务：写 `agent_spec`（带 `skills`）+ 自动建 `Group`（无 `group_id` 时，`grp_<sha1(agent_id)[:24]>`）+ owner `GroupMember` + 回填 `agent_spec.group_id` ⑥ 回传 `agent_id` / `anthropic_agent_id` / `anthropic_environment_id` / `group_id` / `group_link` / `skills` / `translation_report` / **`created` (bool)** / **`previous_managed_version` (int|None)**；cli 终端区分 `Created new agent.` vs `Updated existing agent (vN → vN+1).`。详 `docs/update-mode-handoff.md`

ADR 锚点：`harness-design/primitives/04-agent-deployment-pipeline.md`（ADR-P15 skill 三层 URI / P16 Shadowing / P18 OV 真源 + 单向 sync / P19 re-deploy update mode / P21 deploy 事务三件套 + KOL 资料隐式补全）+ `plan/06-deploy-cli.md` §4.4。env：`agent deploy` 需 `ASKDAO_CONDUCTOR_URL` + `ASKDAO_CONDUCTOR_TOKEN`（都必填）。

未做（P2）：远端 ID 写回 `agent.yml` `status:`；`askdao profile setup-kol/show-kol` 独立子命令；`askdao agent validate`；`askdao agent init` 生成 skill 骨架；`~/.askdao/config.yaml` 配置文件。

---

## Document Map

```
askdao-cli/
├── docs/
│   ├── HANDOFF.md ⭐ 你在这（一次性切换入口）
│   ├── design.md   ⭐ 设计真相源 (v0.5 + v0.6 决策记录 §9.10/§9.11)
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
