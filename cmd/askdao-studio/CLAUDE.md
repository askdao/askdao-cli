# cmd/askdao-studio/
> L2 | 父级: ../../CLAUDE.md

AskDAO Studio 桌面 app（Wails v2 main）—— askdao-cli 第二入口，面向无 CLI 经验用户。**复用 `internal/webstudio`**：`AssetServer.Handler = webstudio.Handler(...)`，`Assets:nil` 令所有 GET 走 webstudio 的 mux，桌面窗口直接显示 studio.html + `/api/*` + `/logo.png`，零 sidecar、零前端构建链。桌面专属功能经 `StudioData.Desktop` flag 隔离，CLI `agent edit` 不受影响。

## 成员清单

- **main.go** — Wails main：`wails.Run(&options.App{Title/Width/Height/MinWidth/MinHeight, AssetServer:{Handler: webstudio.Handler(app.StudioOptions())}, Mac:{TitleBar: mac.TitleBarDefault()}, OnStartup, Bind:[app]})`。AssetServer.Handler 复用 webstudio 的 http 表面（`Assets:nil` → 所有 GET 转发到它；见 [../../internal/webstudio/CLAUDE.md](../../internal/webstudio/CLAUDE.md)）。**`Mac.TitleBar` 必须显式设标准标题栏**——否则 wails 默认 webview 占满窗口顶部，webstudio 的全宽顶栏(`<header>`，为浏览器 tab 设计)盖住 macOS 红绿灯，窗口关不掉。
- **app.go** — `App`（Wails bound-method 宿主 + `ctx` + 登录态 + 扫描项目态）+ `NewApp` / `startup(ctx)` + `StudioOptions`（把桌面回调 + 动态 `StudioData` 注入 webstudio）。桌面业务全复用 `internal` 核心包，方法即 webstudio 回调：`spec`（返回当前 `currentData`；未扫描时是 `NeedsScan=true` 的 placeholder，驱动 studio.html 选文件夹覆盖层）/ `scan`（`runtime.OpenDirectoryDialog` 选文件夹 → `pipeline.Run` + `llmClient`（登录→`recommender.ConductorClient` / 否则 `MockClient`）→ `BuildStudioData` 换下 placeholder）/ `save`（`syncNetworking` + 写 `<dir>/askdao-agent.yml`）/ `deploy`（读 yaml + **`deployflow.PackageSkills` 单源** + `auth.Load` 拿 server/token + `deploy.Client.Deploy`，组装镜像 cmd/askdao 的 deployFromDir）。登录经 `internal/auth` device flow：`authState`/`startLogin`（+`openBrowser` 拉起验证页）/`loginPoll`/`logout`，与 CLI 共用 `credentials.json`（桌面/CLI 登录互通）。全部经 `StudioData.Desktop` + 桌面专属回调隔离，CLI `agent edit` 无感。**同 `package main` 含 main.go（依赖 wails GUI 工具链），本地建不了、只 CI 编译**。
- **wails.json** — Wails 项目配置。`frontend:install`/`frontend:build` 空 → `wails build` 不碰前端（无 npm 构建链，只期望 AssetServer.Handler）。`outputfilename: AskDAO Studio`。

## 设计约束

- **本地建不了**：Wails 需 CGO + 各 OS 原生 GUI 工具链（mac Xcode / win WebView2 / linux gtk+webkit2gtk），不能交叉编译。编译打包只在 per-OS matrix CI（[`../../.github/workflows/desktop-build.yml`](../../.github/workflows/desktop-build.yml)：macos-latest `darwin/universal` + windows-latest `windows/amd64`）；GUI 真机运行靠有 go+wails 环境的机器或 CI artifact。开发机（无 go/wails）只能 build/test 非 studio 包。
- **零新后端 + 复用 CLI 核心**：桌面能力全经 `internal/`（pipeline/deploy/auth/webstudio/...）+ Conductor 现有 endpoint，不新增服务端。
- **不破坏 CLI**：桌面新增经 `Desktop` flag + 桌面专属回调隔离，`cmd/askdao` 与 `internal/webstudio` 的 CLI 路径零变更（现有测试全绿守护）。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
