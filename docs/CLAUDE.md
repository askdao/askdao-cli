# docs/
> L2 | 父级: ../CLAUDE.md

设计文档 + 调研报告。`design.md` = Phase 1 静态扫描流水线设计稿；`observe-layer-design.md` = v0.8+ Observe 层（运行时观测驱动 spec 生成）新方向；调研产物按主题落 `investigations/` 子目录。`handoff-{A,B,C}-*.md` = v0.8 第二步三条并行线的**会话启动文件**（自包含，供哥各开一个独立物理窗口驱动）。

## 成员清单

- **HANDOFF.md** — 新会话上手 / 上下文切换的入口文档（current status / quick start / document map / decisions / pitfalls）
- **handoff-A-observe-hook.md** — v0.8 第二步**线 A** 会话启动文件（纯 askdao-cli）：`agent edit --observe` 用 Claude Code hooks 观测真实 session 实际激活的 skill/MCP 自动预勾选。spike 已验证完全可行（直接捕获 `Skill` tool + `mcp__*`），含背景/工作思路/复用点/起步。自包含，供哥开独立窗口
- **handoff-B-theme-and-kolprofile.md** — v0.8 第二步**线 B/D** 会话启动文件（**跨三仓** cli+conductor+web）：主题色跨仓贯通（theme.go Palette token 三端对齐 → conductor Metadata 镜像 + Group 带色 → web `/k/{kol}/g/{group}` 渲染）+ kol_profile 握手改"引导去 askdao.ai"。自包含
- **handoff-C-harness-paths.md** — v0.8 第二步**线 C** 会话启动文件（纯 askdao-cli）：补全 `harness_scope.go` 的 Codex/Cursor user-scope skill/MCP 路径。含完整调研 + patch（Codex skill=`~/.agents/skills`、Cursor MCP=`~/.cursor/mcp.json`、修现状 `.agent` marker bug、Codex MCP TOML 拆独立 issue）。自包含
- **design.md** — askdao-cli `init --auto` 的完整设计（v0.5 卡片 UX + v0.6 §9.10 部署 payload/archetype（PR #19 已交付）+ §9.11 Plugin 机制调研（Claude Code/Codex Plugin 对 askdao-cli 三个层面的影响，待决策））：四层流水线（syft → dev-filter → providers → LLM）、detection.json schema、harness-neutral 中间格式 yaml schema、Dockerfile 兼容 5 字段、KOL 中等详情卡片 UX、部署清单 lockfile-driven skill 分类、Phase 1-3 路线图
- **observe-layer-design.md** — askdao-cli **Observe 层**设计（v0.8+ 新方向，2026-05-20）：从"静态扫描猜测环境"转向"运行时观测记录"——用 Claude Code / Codex hooks 让 Agent 在真实环境跑一遍项目，从观测数据（实际执行的命令 / 加载的包 / 写入的文件 / 绑定的端口）生成 `askdao-agent.yml`。核心：三层可见性框架（上下文/执行/运行时内部）+ 四条观测路径，原静态 4 层降级为 cold-start baseline，Observe→Spec 是开源空白地带（SlimToolkit 为唯一容器级先例）。前置三份调研见 `investigations/`（报告 A/B/C）。**⚠️ 2026-05-21 立足点已纠正**：场景锁定 skill_pipeline + Anthropic MVP，四观测路径 + §6 重造 schema 作废，仅吸收 Web 工作台 + provenance + skill 相关性筛选 —— 见 `review-observe-pivot-2026-05-21.md`
- **review-2026-05-06.md** — v0.2 review（Anthropic 三资源模型重审）
- **review-v0.3-2026-05-06.md** — v0.3 review（harness-neutral 中间格式）
- **review-v0.4-2026-05-06.md** — v0.4 review（Dockerfile 兼容性补强）
- **review-v0.5-2026-05-06.md** — v0.5 review（中等详情卡片 UX）
- **review-observe-pivot-2026-05-21.md** — observe-layer-design.md 立足点纠正 + v0.8 实施评审：锁定 skill_pipeline + Anthropic MVP；作废四观测路径 + §6 重造 schema；吸收 Web 工作台（`askdao agent edit`）+ 字段级 provenance + harness 感知双 scope skill 筛选；命令精简（agent edit/deploy）+ Agent 主题色。含 M1-M5 已实施 + 第二步待续清单
- **m4-deploy-walkthrough.md** — `askdao agent deploy` 端到端 runbook：cli build / token 取 + URL-decode / agent init 骨架 / SKILL.md 手写 / agent.yml 3 处编辑（system_prompt 不读 persona.md gotcha）/ deploy 命令 + conductor 后端 sync_skill_zip + adapter + 事务建 Agent↔Group 全链路；含 cli stdout / `/api/v1/agents` / PG 验证 checklist。2026-05-14 prod e2e 验证产物
- **cli-auth-device-flow.md** — `askdao auth login` 鉴权设计稿（OAuth 2.0 Device Code Flow / RFC 8628，v0.1 2026-05-13）：浏览器绑定的一条命令登录，产出长效 CLI token 落 `~/.config/askdao/credentials.json`（0600），替代手工复制 session token 的死亡 UX，对标 gh / gcloud / flyctl auth login。含完整时序图 + conductor 端 `cli_device_auth` / `cli_token` 表 + token 解析优先级（env 成对 > credentials.json）
- **investigations/** - spike 报告与外部技术参考（详见子目录 CLAUDE.md）

## 文档来源

`design.md` 与 `investigations/` 下早期两份报告（syft-spike + nixpacks-pattern）原存放于 `harness-design/designs/` 与 `harness-design/investigations/`，于 2026-05-06 整体迁移到此处 —— 这些产物只服务 askdao-cli，归属本仓库更合理。`observe-layer-design.md` 与 `investigations/` 下三份 Agent Session 观测报告（2026-05-20）为本仓直接产出。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
