# docs/
> L2 | 父级: ../CLAUDE.md

设计文档 + 调研报告。`design.md` = Phase 1 静态扫描流水线设计稿；`observe-layer-design.md` = v0.8+ Observe 层（运行时观测驱动 spec 生成）新方向；调研产物按主题落 `investigations/` 子目录。`handoff-{A,C}-*.md` = v0.8 第二步并行线的**会话启动文件**（自包含，供各开独立窗口驱动）。

> **开源仓边界**：本仓文档只描述 askdao-cli 自身。L2/L3 对齐服务端时只引公开契约（端点路径 + 字段名），不引 conductor 私有仓内部坐标（文件路径/类名/表名/alembic 迁移号/内部 ADR 编号/私有 PR 号）。跨私有仓交接与内部计划归私有 orchestrator 仓。

## 成员清单

- **HANDOFF.md** — 新会话上手 / 上下文切换的入口文档（current status / quick start / document map / decisions / pitfalls）
- **handoff-A-observe-hook.md** — v0.8 第二步**线 A** 会话启动文件（纯 askdao-cli）：`agent edit --observe` 用 Claude Code hooks 观测真实 session 实际激活的 skill/MCP 自动预勾选。spike 已验证完全可行（直接捕获 `Skill` tool + `mcp__*`），含背景/工作思路/复用点/起步。自包含
- **handoff-C-harness-paths.md** — v0.8 第二步**线 C** 会话启动文件（纯 askdao-cli）：补全 `harness_scope.go` 的 Codex/Cursor user-scope skill/MCP 路径。含完整调研 + patch（Codex skill=`~/.agents/skills`、Cursor MCP=`~/.cursor/mcp.json`、修现状 `.agent` marker bug、Codex MCP TOML 拆独立 issue）。自包含
- **design.md** — askdao-cli 静态扫描流水线 + 中间格式 schema + CLI 设计：四层流水线（syft → dev-filter → providers → LLM）、detection.json schema、harness-neutral 中间格式 yaml schema、Dockerfile 兼容字段、KOL 审阅卡片 UX、部署清单 skill 分类、Phase 1-3 路线图
- **observe-layer-design.md** — askdao-cli **Observe 层**设计（v0.8+ 新方向）：从"静态扫描猜测环境"转向"运行时观测记录"——用 Claude Code / Codex hooks 让 Agent 在真实环境跑一遍项目，从观测数据（实际执行的命令 / 加载的包 / 写入的文件 / 绑定的端口）生成 `askdao-agent.yml`。核心：三层可见性框架（上下文/执行/运行时内部）+ 四条观测路径，原静态 4 层降级为 cold-start baseline，Observe→Spec 是开源空白地带（SlimToolkit 为唯一容器级先例）。前置三份调研见 `investigations/`（报告 A/B/C）。**⚠️ 立足点已经内部评审纠正**：场景锁定 skill_pipeline + Anthropic MVP，四观测路径 + §6 重造 schema 作废，仅吸收 Web 工作台 + provenance + skill 相关性筛选
- **cli-auth-device-flow.md** — `askdao auth login` 鉴权设计稿（OAuth 2.0 Device Code Flow / RFC 8628）：浏览器绑定的一条命令登录，产出长效 CLI token 落 `~/.config/askdao/credentials.json`（0600），替代手工复制 session token 的死亡 UX，对标 gh / gcloud / flyctl auth login。含完整时序图 + 服务端鉴权端点契约 + token 解析优先级（env 成对 > credentials.json）
- **kol-quickstart-windows.md** — 面向 KOL 本人的 Windows 上手指南（onboarding S6 产物）：§1 一行命令安装 `irm https://askdao.ai/install.ps1 | iex`（手动下载 + PATH 流程降级为备用方案；升级用 `askdao update`）→ askdao.ai 注册 + 订阅模式激活 → `auth login`（**内置 askdao-mcp 自动配置**）→ 验证连接（claude `/mcp` / codex 提问）+ `mcp setup`/`--print` 备用 → 本地调试（链 askdao-mcp-reference.md）→ `agent edit`/`deploy` → 常见问题 + Windows 路径速查
- **askdao-mcp-reference.md** — askdao-mcp 工具参考（面向 Skill 编写者）：6 能力域工具全清单（listenhub / elevenlabs / google+openai 文生图 / market / sec）+ 各域关键参数/典型链路 + **Skill 编写注意事项 9 条**（产物 URL 时效 图10min/音频60min 立即消费、异步两形态、先查资源不硬编码 voice/speaker、配额预查、错误是文本非异常、凭证零出现、SKILL.md description 触发语义、数据边界免责、组合范式）。quickstart §4/§5 链入
- **WxArticle-Project-To-Claude-Plugin.md** — 外部参考文章存档：从 install.sh/symlink 迁移到 Claude Code Plugin 的实录（三套 manifest fan-out + CI 校验 + SessionStart hook + description 触发原则）。§9.11 Plugin 调研的野生验证案例，已落档 design.md §9.11「野生案例验证」小节
- **investigations/** - spike 报告与外部技术参考（详见子目录 CLAUDE.md）

## 文档来源

`design.md` 与 `investigations/` 下早期两份报告（syft-spike + nixpacks-pattern）于早期整体迁移到此处 —— 这些产物只服务 askdao-cli，归属本仓库。`observe-layer-design.md` 与 `investigations/` 下三份 Agent Session 观测报告为本仓直接产出。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
