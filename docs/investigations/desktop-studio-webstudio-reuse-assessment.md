# Desktop Studio — webstudio 复用可行性与隔离扩展评估

- **日期**: 2026-07-04
- **背景**: askdao-cli 计划新增第二个入口 `cmd/askdao-studio`（Wails 桌面 app，issue #64），面向无 CLI 经验的用户。落地前核查「在现有 `internal/webstudio` 上扩展能否满足桌面 app 的功能，且不破坏 CLI `askdao agent edit`」——早期设想过独立 React+Vite 前端，核查确认现状是 vanilla 单 HTML 工作台，故改走复用路线。
- **对象**: `internal/webstudio`（server.go / api.go / studio.html）、`cmd/askdao/edit.go`、`internal/{pipeline,auth,deploy,recommender}` 复用接口、构建链路。
- **方法**: 一手 Read 核心文件 + 全景核查交叉印证，接口签名逐条对源。
- **结论**: 可行。向导审阅大头现成复用；桌面新功能经 `Desktop` flag + 桌面专属回调/路由隔离，CLI 路径零变更（`StudioData.Observe` 是现成先例）。唯一需碰的共享代码是 `deployFromDir` 加 confirm 参数（向后兼容）。

> 本文只记 askdao-cli 自身技术核查。桌面 app 的产品设计、交互取舍、跨仓/服务端计划归私有 orchestrator 仓，本仓不引其内部坐标（遵 `docs/CLAUDE.md` 开源仓边界）。

---

## §1 webstudio 现状功能全景

`internal/webstudio` 是 `askdao agent edit` 拉起的本地工作台：`Serve(Options)` 绑 `127.0.0.1:0` 随机端口，`go:embed studio.html`（vanilla 单页，inline CSS/JS，无 npm/框架，~1650 行）+ `logo.png`，阻塞至用户点 Deploy/Done。

**前端 4 步向导**（studio.html）：
1. Identity — name / display_name / description / category / output language（`metadata.language`）/ theme color / avatar（icon 网格）/ visibility
2. Persona — model_class（→ `model_preferences` 映射）/ system_prompt
3. Skills & Tools — Skills / MCP / Secrets 三 Tab，按 scope 分组勾选；`--observe` 叠加真实激活项
4. Review — 汇总 + 选中项 chips + Deploy / Save&finish

**配套**：AI 信心度徽标 + hover 理由 tooltip；统一结果对话框（success/error × terminal，Deploy 含可点击 group 链接）；输入校验；主题色实时预览。

**路由**（server.go `buildMux`）：`/`(HTML) `/logo.png` `/api/spec`(GET StudioData) `/api/save` `/api/deploy` `/api/done` `/api/observe`(GET/POST)。

**回调解耦**（cmd 注入，webstudio 不依赖 pipeline/deploy）：`Options{Data *StudioData, OnSave, OnDeploy, OnReady, NoBrowser}` — server.go:42；`OnSave func(*types.AgentSpec) error` / `OnDeploy func(*types.AgentSpec) (*DeployResult, error)` / `OnReady func(port int)`。

---

## §2 桌面功能对照现状

| 桌面功能 | webstudio 现状 | 扩展方式 | 碰 CLI? |
|---|---|---|---|
| 向导 + 审阅编辑器 | ✅ 完整已有 | 复用，几乎零改；拖文件夹入口由桌面壳提供 | 否 |
| 错误 GUI 化（409 三类） | ⚠️ `studioDeployError`（edit.go:277-306）已把 kol_profile / blocking_warnings / visibility_downgrade 三类转文案；降级现为「提示去 CLI `--confirm-downgrade`」（studio 无交互确认） | 扩展 `deployFromDir` 加 confirm 参数 + 桌面前端确认框 + 桌面 OnDeploy 透传 | 见 §4（向后兼容） |
| SKILL.md 行内校验 | ⚠️ 校验在 deploy 层 fail-fast（`packageSkills` → `scanner.ParseSkillFrontmatter`，deploy.go:469），前端无行内 UI | 前端加校验 UI + 小端点跑 ParseSkillFrontmatter | 否（桌面隔离 + 新端点） |
| 登录 / 账号 | ❌ 在 CLI `auth login` 命令 | Wails Go 侧调 `auth.NewDeviceFlow`/`auth.Load` + 登录 UI | 否（新代码） |
| 自更新 | ❌ 在 CLI `update` 命令 | Wails 壳调 `internal/selfupdate` | 否（新代码） |
| 内嵌 AI 助手侧栏 | ❌ 无 | `Desktop` flag + 新路由 `/api/assistant` + 前端侧栏 | 否（隔离） |
| 部署后测试聊天 | ❌ 无（deploy 成功给 group 链接） | `Desktop` flag + 新路由 `/api/chat` + 前端聊天区 | 否（隔离） |

---

## §3 隔离扩展方案（不破坏 CLI 的机制）

**现成先例**：`StudioData.Observe bool`（api.go:29）——cmd 的 `--observe` 置位（edit.go:82 `data.Observe = *observeMode`），前端据此显示观测区并轮询 `/api/observe`，未开则前端完全无感。这是「cmd 置位 flag → 前端条件渲染 → CLI 不启用即隔离」的活样板。

**桌面照搬**：
1. `StudioData.Desktop bool`（新字段）——桌面壳生成 StudioData 时置 `true`；CLI `edit.go` 不设（默认 `false`）。
2. studio.html 按 `data.desktop` 条件渲染桌面专属区块（助手侧栏、测试聊天、SKILL 行内校验、降级确认框）。CLI（`desktop=false`）看不到。
3. `Options` 加桌面专属回调（nil 时不启用/不注册对应路由）。CLI `edit.go` 不注入 → 回调 nil → 桌面路由不生效。

**保证**：CLI `agent edit` 走 `edit.go` 原路径，**`edit.go` 一行不改，`askdao agent edit` 行为 100% 不变**。回归由「现有 `internal/webstudio` + `cmd/askdao` 测试全绿」守护。

---

## §4 唯一需碰的共享代码：deployFromDir 向后兼容扩展

`deployFromDir(ctx, dir, harness, force)`（deploy.go:521）签名**只有 `force`，不含 `ConfirmVisibilityDowngrade`**——被 `edit.go` 的 `OnDeploy`（edit.go:123）复用。桌面要支持「应用内确认可见性降级」须让它能透传 confirm。

**做法（向后兼容）**：新增 `deployFromDirWithConfirm(ctx, dir, harness, force, confirmDowngrade bool)`，令旧 `deployFromDir` 委托它（`confirmDowngrade=false`）。CLI 现有调用零改动；桌面 OnDeploy 走新函数、捕获 `deploy.ErrVisibilityDowngradeConfirm`（client.go:141，含 `Detail.AgentName/CurrentVisibility`）后弹应用内确认、带 `confirmDowngrade=true` 重发。这是已知的桌面前置。

---

## §5 复用接口锚点

**扫描** `internal/pipeline`：`func Run(ctx, Options) (*Result, error)` — pipeline.go:80（Options.Root 必填 / LLM nil 跳过推荐；Result.Detection/Recommendation/AgentSkills/Warnings）。纯 Go 无 CLI 耦合（除 syft 子进程软降级）。

**数据契约** `internal/webstudio`：`func BuildStudioData(spec, det, harnessLabel, restorePrior) *StudioData` — api.go:89。桌面复用它，额外置 `Desktop=true`。

**认证** `internal/auth`：
- `NewDeviceFlow(serverURL, clientName) *DeviceFlow` — device.go:73；`.Start(ctx)` — :84；`.PollUntilApproved(ctx, deviceCode, interval, deadline)` — :173
- `Load() (*Credentials, error)` — credentials.go:77（缺失返 `ErrNoCredentials`）；`Save` :109 / `Delete` :172 / `Path` :65
- **无 `IsLoggedIn` 布尔**：登录态用 `auth.Load()` + `errors.Is(err, auth.ErrNoCredentials)`（样板 auth.go:149 / deploy.go:419）
- 凭证 `$XDG_CONFIG_HOME/askdao/credentials.json`（win `%AppData%` / mac `~/Library/Application Support`），0600 — configDir credentials.go:189

**部署** `internal/deploy`：`NewClient(baseURL) *Client`（`AuthToken` 字段）— client.go:182；`(c *Client) Deploy(ctx, DeployInput) (*DeployResponse, error)` — client.go:198；`DeployInput{Force, ConfirmVisibilityDowngrade, ...}` — client.go:31；typed errors `ErrKolProfileRequired`/`ErrVisibilityDowngradeConfirm`/`ErrBlockingWarnings` — client.go:114/141/160；`classifyConflict` — client.go:292。cmd 层 `deployFromDir` — deploy.go:521 / 共享 `packageSkills` — deploy.go:469 / `resolveServerAndToken` — deploy.go:405。对外端点 `POST /api/v1/cli/deploy`（multipart）。

**LLM 推荐** `internal/recommender`：`LLMClient` 接口 — llm.go:29；`ConductorClient`（对外 `POST /api/v1/cli/recommend`）— llm.go:80/90；**离线 `MockClient`** — llm.go:141 + `DefaultMockRecommend` — llm.go:157；`chooseLLMClient()` — common.go:46。未登录也能跑通扫描审阅。

**cmd 入口范式**：`cmd/askdao/main.go:21` 手写 switch router（无 cobra），stdlib `flag`。Wails 用自己的 `wails.Run(...)` 作 main，不套 router，直接调同样 internal 包。

---

## §6 构建链路现实（Wails 集成风险）

- **`.goreleaser.yml`**（仓库根，`.yml` 非 `.yaml`）：单 build `id: askdao`，`main: ./cmd/askdao`，`CGO_ENABLED=0`，`-trimpath`，`goos:[windows,darwin,linux] × goarch:[amd64,arm64]`，单 ubuntu runner 交叉编译。
- **`.github/workflows/`：只有 `release.yml`**（`on: push tags v*`）。**无 PR/push CI 门禁**。
- **⚠ Wails 产物无法 drop-in**：Wails 需 `CGO_ENABLED=1` + 各 OS 原生 GUI 工具链（mac Xcode / win WebView2+gcc / linux gtk+webkit2gtk），**不能交叉编译**。给 goreleaser 加 wails build 不可行；须走 **per-OS matrix runner** 的独立 `wails build` job（新建 workflow）。
- **go.mod**：无 wails 依赖；直接依赖仅 `BurntSushi/toml`、`go-enry/v2`、`moby/buildkit`、`yaml.v3`；webstudio HTTP server 纯 stdlib `net/http`。
- **本地约束**：开发机无 go、无 wails，不能 build/run Wails GUI。验证分工——纯 Go 改动（webstudio flag / deployFromDir）→ docker `golang:1.26` go vet+test；Wails 编译打包 → per-OS matrix CI；GUI 真机运行 → 有 go+wails 环境的机器或 CI artifact。

---

## §7 结论与阶段分解（askdao-cli 侧）

复用 webstudio 可行：向导审阅现成，桌面新功能经 flag/回调/路由隔离，CLI 零破坏，唯一共享改动向后兼容。

**阶段分解**（每阶段独立 commit，验证方式已标）：
1. webstudio `Desktop` flag 地基 + `deployFromDir` 向后兼容扩展 — docker go test + 现有测试证明不破 CLI
2. Wails 壳（`cmd/askdao-studio` webview 指向内部 `webstudio.Serve`）+ per-OS matrix CI build — CI 验编译打包
3. 桌面登录 UI（Wails 调 device flow）
4. 拖文件夹 → `pipeline.Run` 接入向导
5. 可见性降级应用内确认（用 §4 扩展）
6. 内嵌 AI 助手侧栏（`Desktop` flag + `/api/assistant`；助手后端支点见内部设计）
7. 部署后测试聊天（`/api/chat`）
8. SKILL.md 行内校验 + 桌面自更新
