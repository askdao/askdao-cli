# askdao-cli — KOL 本地 Agent 引导工具

> Go 单二进制 CLI。在 KOL 项目目录下扫描技术栈、推断框架、生成 Anthropic Managed Agents 配置草稿。
> askdao-cli 是 AskDAO 在你本机运行的开源部分 —— 信任锚点。

技术栈：Go 1.26 + anchore/syft + go-enry/enry + moby/buildkit (dockerfile parser) + anthropic-sdk-go；桌面 app（`cmd/askdao-studio`）+ Wails v2

---

<directory>
cmd/askdao/ - CLI 入口（main.go router + common.go 共享 helper）+ 用户命令（auth login/status/logout · agent edit/deploy）；v0.8 命令精简，审阅入口收敛为 agent edit 本地 Web 工作台
cmd/askdao-studio/ - Wails v2 桌面 app（M1 建设中，issue #64）—— askdao-cli 第二入口，AssetServer.Handler 复用 webstudio 显示 studio.html（零 sidecar/零前端构建链）；桌面功能经 StudioData.Desktop flag 隔离不碰 CLI
internal/ - 业务实现（auth/ 凭据 + Device Code Flow 客户端 · types/ 双 schema · scanner/ 确定性扫描 + harness 感知双 scope · providers/ 框架推断 · pipeline/ L1-L4 编排 · recommender/ L4 LLM + /cli/recommend 客户端 · render/ 审阅卡片 · webstudio/ 本地 Web 工作台(127.0.0.1 + go:embed) · observe/ --observe 临时 hook 生命周期(写/合并/清理 settings.local.json，零残留三件套) · deploy/ conductor /cli/deploy 客户端 + skill zip 打包 · selfupdate/ askdao update 自升级引擎(GitHub Releases latest + checksum 校验 + 原子换装)）
install/ - 一键安装脚本（install.sh / install.ps1 / install.cmd），经 askdao.ai rewrite 反向代理分发，canonical 在本仓（开源可审计）
docs/ - 设计文档与调研报告（design.md = Phase 1 静态流水线主稿 + observe-layer-design.md = v0.8+ Observe 层方向 + cli-auth-device-flow.md（OAuth 2.0 Device Code Flow）+ HANDOFF.md + investigations/ 子目录：2 份实现底座 spike + 3 份 Agent Session 观测报告）
</directory>

<config>
go.mod - Go module 定义（github.com/askdao/askdao-cli）
Makefile - build / install / test / lint / clean / snapshot 标准目标
.goreleaser.yml - GoReleaser v2 发布管线（windows/darwin/linux × amd64/arm64 + checksums；version 经 ldflags -X main.version 注入）
.github/workflows/release.yml - tag v* 触发：go test → goreleaser release 挂 GitHub Release
.github/workflows/desktop-build.yml - 桌面 app（cmd/askdao-studio）per-OS matrix（macos/windows）wails build 编译验证（PR/dispatch 触发）
LICENSE - MIT
.gitignore - Go 标准忽略规则
</config>

---

## 设计哲学

1. **本地隐私**：扫描全在用户机器跑，不上传任何文件内容（与云端方案的根本区别）
2. **确定性优先**：L1-L3 用工业标准库（syft/enry），LLM 只做最后一步推荐 + reason
3. **借车不造车**：syft 解决"manifest 都说了什么包"问题，nixpacks providers 解决"框架推断 + 系统包反向映射"问题，**askdao-cli 自己只写 25%**
4. **review-and-edit 而非 from-scratch**：KOL 体验是审阅推荐草稿，不是空白模板

---

## 命令骨架

```
askdao auth login [--server url] [--name device] [--no-browser]  # OAuth 2.0 Device Code Flow（RFC 8628）+ 落 ~/.config/askdao/credentials.json (0600)；成功后自动跑 askdao-mcp setup（fail-soft 不影响 login 结果）
askdao auth status                         # 显示当前登录身份；未登录 exit 1
askdao auth logout                         # 删除本地 credentials（不撤销服务端 token，撤销走 web UI v2）
askdao mcp setup [--print]                 # M2：调 conductor /cli/mcp-credentials 取 gateway URL+token，自动写本机 Claude Code(~/.claude.json upsert) + Codex(~/.codex/config.toml 追加 + ASKDAO_MCP_TOKEN env，Win 走 setx)；--print 只输出 snippet 不落盘
askdao agent edit [--dir path] [--harness id] [--no-ui] [--force] [--observe]
                                           # v0.8 核心命令：扫描(或加载已有 askdao-agent.yml)+ 重扫拿 skill/MCP 候选 → 开本地 Web 工作台审阅/编辑 spec+Agent profile、按 scope 勾选 skill/MCP → Save 或一站式 Deploy。--no-ui 只写草稿退出(CI/headless)；--observe 临时挂 PreToolUse hook 预勾真实 claude session 实际激活的 skill/MCP
askdao agent deploy [--dir path] [--harness id] [--force] [--confirm-downgrade]
                                           # 读 <dir>/askdao-agent.yml 原始字节 + 打包 custom_local skill 整目录 + 经 Conductor /cli/deploy 推到 Anthropic Managed Agents（**v0.7.1 起 update-mode**：同 yaml.metadata.name 重 deploy → in-place update Anthropic agent + env，复用 agent_id/group_id；改 name → fork 新 agent；**降级确认闸**：yaml 省略 visibility = 保持线上现值；对已上架服务的 agent 显式 visibility: private → 危险警告 + 交互确认（或 --confirm-downgrade），降级会让订阅者/展示页失访且改回公开需平台重审）
askdao update [--force]                    # 自升级到最新 GitHub Release（checksum 校验 + 原子换装；-dev 构建拒绝，--force 越过/同版本重装）；首装走 install/ 一键脚本
askdao version                             # 打印版本
askdao help                                # 顶层帮助（子命令用 askdao <cmd> --help 看 flag）
```

> **v0.8 命令精简**：旧 `detect` / `bundle` / `agent init` / `agent show`（及 `agent validate` 计划项）已**移除**——其价值（扫描报告 / 上传清单预览 / 卡片审阅）全收进 `agent edit` 本地 Web 工作台。系统未上线，无向后兼容包袱，不保留旧命令。

**产物布局**（v0.7 起项目根扁平化 + `.askdao/` 工具空间）：
- `<root>/askdao-agent.yml` — KOL 唯一编辑对象（项目宣言文件，含 `persona.system_prompt` literal block 完整内容）
- `<root>/.askdao/recommendation.yml` — diff baseline（deploy 用作 KOL 改动检测）
- `<root>/.askdao/detection.json` — 确定性扫描结果（每次 `agent edit` 重生成）

deploy 的 token 解析顺序：`ASKDAO_CONDUCTOR_TOKEN + ASKDAO_CONDUCTOR_URL` 同时设置 → 用 env（CI / 一次性覆盖）；否则读 `credentials.json`（`askdao auth login` 的产物）；都没有 → 报错并提示登录。两个 env 必须成对，单设一个明确报错（防止误配置静默降级）。设计稿 [`docs/cli-auth-device-flow.md`](docs/cli-auth-device-flow.md) §6.3。

**Skill 上传分类规则**（v0.7 起所有 custom skill 一律上传，Anthropic Managed Agents 无公共 skill registry，custom skill 必须随包上传）：所有 `<skillDir>/<name>/` 目录递归打包（含 SKILL.md + scripts/ + assets/ + references/ 等所有子文件 + 二进制透传）。`<root>/.agents/skills/` / `<root>/.claude/skills/` / 自定义 path 的上级在 ZipDir 时被 `filepath.Rel` 切掉 —— **harness 中性 invariant**：Anthropic 端只看到 `<skillName>/SKILL.md` 形态（design.md §9.14）。vendored 与原生只是 bundle UI 的 inline origin tag（`skill (repo-native)` / `skill (vendored: <source> @ <hash>)`），不改变上传行为。**打包 ignore 过滤（`ZipDir`）**：默认排除普遍安全/无关项 —— 目录 `node_modules` / `.git` / `.svn` / `.hg` / `__pycache__` / `.venv` / `venv`（整棵 `filepath.SkipDir`），文件 `.DS_Store` / `Thumbs.db` / `desktop.ini` / 编辑器 swap & backup（`*.swp` / `*.swo` / `*~`）+ **安全关键的 dotenv（`.env` / `.env.*`）**（skill 是 prompt 包几乎不需 .env，默认排除杜绝密钥误传，确需的模板用 `.askdaoignore` 的 `!` 反向纳入）；`SKILL.md` 永远保留；`CLAUDE.md` / `skills-lock.json` / manifest 默认随包。项目特有的构建输出目录（`output*` / `input` 等）由 **`.askdaoignore`**（syntax 同 gitignore，`#` 注释 / 尾 `/` 目录模式 / `!` 反向纳入；放 skill 目录根）兜底排除 —— 用精确名而非前缀通配做默认排除，避免误伤合法命名的 skill 子目录（如 `output_templates`）。该过滤同时保护落 S3 的真源 blob 与上传 Anthropic 的文件列表（二者同源于这份 zip）。

---

## 与 askdao-cloud 的关系

- **独立仓库 + 独立发版**（KOL 本地工具必须独立 repo + 开源 = 信任锚点）
- 与服务端共享 `AgentSpec` schema 契约（CI diff 校验对齐，避免双写漂移）
- 设计文档：[`docs/design.md`](docs/design.md)（含两份 spike 报告：[`docs/investigations/syft-spike-for-askdao-cli.md`](docs/investigations/syft-spike-for-askdao-cli.md) + [`docs/investigations/nixpacks-provider-pattern.md`](docs/investigations/nixpacks-provider-pattern.md)）

---

## 状态

Phase 1（detect / agent init / show / deploy 骨架 + L1-L4 流水线 + render UX）已交付（issue #1-8）；后续加了 `bundle` 命令 + detection 的 archetype / 部署清单（lockfile-pinned skill 走引用重装、其余随包上传，零 LLM；issue #19）。M4 补完 `agent deploy` —— 接对外端点 `POST /api/v1/cli/deploy`（`multipart/form-data` + custom skill zip 上传 + `409 kol_profile_required` 隐式补全 + blocking-warning gating，仅 `action=rejected` 阻断、severity 不再 gate）。**v0.7.1 deploy update-mode** —— 服务端按 `(owner, yaml.metadata.name)` 去重，同 name 重 deploy → in-place update（复用既有 agent/env）而非堆同名 agent；cli 端 `DeployResponse` 加 `Created` / `PreviousManagedVersion`，终端区分 `Created new agent.` vs `Updated existing agent (vN → vN+1).`。**v0.8 observe** —— `agent edit --observe` 用 Claude Code PreToolUse hook 观测真实 session 实际激活的 skill/MCP（spike 实测确认子代理工具调用冒泡到主 PreToolUse），工作台叠加证据高亮 + 一键收窄；新增 `internal/observe`（临时 settings.local.json 零残留生命周期：defer cleanup + 启动 SweepStale 自检 + 整文件备份还原）+ webstudio `/api/observe` 端点 + `OnReady(port)` 回调。其他设计真相源：[`docs/design.md`](docs/design.md) + [`docs/HANDOFF.md`](docs/HANDOFF.md)。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
