# askdao-cli — KOL 本地 Agent 引导工具

> Go 单二进制 CLI。在 KOL 项目目录下扫描技术栈、推断框架、生成 Agent 配置草稿并部署。
> askdao-cli 是 AskDAO 在你本机运行的开源部分 —— 信任锚点。

技术栈：Go 1.26 + anchore/syft + go-enry/enry + moby/buildkit (dockerfile parser)；桌面 app（`cmd/askdao-studio`）Wails v2。

实现细节见各目录 L2 与文件头注释；变更史见 git log 与 PR；2026-09-05 前的 milestone 化石在 [CHANGELOG.md](./CHANGELOG.md)（已冻结）。

---

<directory>
cmd/askdao/ - CLI 入口（router + 共享 helper）+ 用户命令（auth · mcp setup · agent edit/deploy · update）
cmd/askdao-studio/ - Wails v2 桌面 app：AssetServer.Handler 复用 webstudio（零 sidecar/零前端构建链），桌面功能经 Desktop flag 隔离
internal/auth/ - 凭据存取 + Device Code Flow 客户端
internal/types/ - 双 schema 真相源（detection.json + askdao-agent.yml，服务端镜像 CI diff 对齐）
internal/scanner/ - L1-L3 确定性扫描（syft/enry/dockerfile + harness 感知双 scope）
internal/providers/ - 框架推断启发式（nixpacks Provider 模式移植）
internal/pipeline/ - L1-L4 编排唯一入口
internal/recommender/ - L4 策略启发式 + conductor LLM HTTP 客户端
internal/render/ - deploy diff + translation warnings 终端渲染
internal/webstudio/ - 本地 Web 工作台（127.0.0.1 + go:embed 三文件前端）
internal/observe/ - --observe 临时 hook 生命周期（零残留三件套）
internal/deploy/ - conductor /cli/deploy HTTP 客户端 + skill zip 打包（stdlib-only）
internal/chat/ - conductor /chat SSE 流式客户端（桌面测试聊天，dumb pipe）
internal/deployflow/ - deploy 装配单源（Prepare/Deploy/凭证解析 + skill 打包编排，CLI/桌面共用）
internal/selfupdate/ - askdao update 自升级引擎（GitHub Releases + checksum + 原子换装）
install/ - 一键安装脚本（经 askdao.ai 反代分发，canonical 在本仓）
docs/ - 设计文档与调研报告（design.md 主稿 + auth/observe 设计 + investigations/）
</directory>

<config>
README.md - 面向用户的仓库说明（安装 / 命令 / 隐私口径）
go.mod - Go module 定义（github.com/askdao/askdao-cli）
Makefile - build / install / test / lint / clean / snapshot 标准目标
.goreleaser.yml - GoReleaser v2 发布管线（多平台 + checksums；version 经 ldflags 注入）
.github/workflows/release.yml - tag v* 触发 go test → goreleaser release
.github/workflows/desktop-build.yml - 桌面 app per-OS wails build 编译验证
LICENSE - MIT
.gitignore - Go 标准忽略规则
</config>

---

## 设计哲学

1. **本地隐私**：扫描全在用户机器跑，不上传任何文件内容
2. **确定性优先**：L1-L3 用工业标准库（syft/enry），LLM 只做最后一步推荐 + reason
3. **借车不造车**：syft 解决包识别、nixpacks providers 解决框架推断，askdao-cli 自己只写 25%
4. **review-and-edit 而非 from-scratch**：KOL 体验是审阅推荐草稿，不是空白模板

---

## 命令骨架

```
askdao auth login [--server url] [--name device] [--no-browser]  # Device Code Flow + 落 credentials.json；成功后自动配置本机 askdao-mcp（fail-soft）
askdao auth status / logout
askdao mcp setup [--print]                 # 取 gateway 凭证写本机 Claude Code / Codex 配置；--print 只输出 snippet
askdao agent edit [--dir path] [--harness id] [--no-ui] [--force] [--observe]
                                           # 核心命令：扫描或加载已有 yaml → 本地 Web 工作台审阅/编辑 → Save 或一站式 Deploy
askdao agent deploy [--dir path] [--harness id] [--force] [--confirm-downgrade]
                                           # 读 askdao-agent.yml + 打包 custom skill → conductor /cli/deploy（update-mode 同 name 原地更新；降级确认闸）
askdao update [--force]                    # 自升级最新 GitHub Release
askdao version / help
```

**产物布局**（项目根扁平化 + `.askdao/` 工具空间）：
- `<root>/askdao-agent.yml` — KOL 唯一编辑对象（项目宣言文件）
- `<root>/.askdao/recommendation.yml` — diff baseline（deploy 改动检测）
- `<root>/.askdao/detection.json` — 确定性扫描结果（每次 edit 重生成）

deploy 的 token 解析：env 成对（`ASKDAO_CONDUCTOR_TOKEN`+`URL`，单设一个报错）> `credentials.json` > 报错提示登录。细节见 `docs/cli-auth-device-flow.md`。

**Skill 上传规则**：所有 custom skill 目录递归打包（Anthropic 无公共 registry）；zip 内为「单一顶层目录 + SKILL.md」的 harness 中性形态（上级路径切掉）；打包 ignore 过滤默认排除 node_modules/.git/__pycache__ 等 + 安全关键 dotenv（`.env`/`.env.*`），项目特有排除走 `.askdaoignore`（gitignore 语法）。细节见 `internal/deploy/CLAUDE.md`。

---

## 与 askdao-cloud 的关系

- **独立仓库 + 独立发版**（KOL 本地工具必须独立 repo + 开源 = 信任锚点）
- 与服务端共享 `AgentSpec` schema 契约（CI diff 校验对齐，避免双写漂移）
- 设计文档：[`docs/design.md`](docs/design.md)

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
