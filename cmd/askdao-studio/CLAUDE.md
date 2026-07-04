# cmd/askdao-studio/
> L2 | 父级: ../../CLAUDE.md

AskDAO Studio 桌面 app（Wails v2 main）—— askdao-cli 第二入口，面向无 CLI 经验用户。**复用 `internal/webstudio`**：`AssetServer.Handler = webstudio.Handler(...)`，`Assets:nil` 令所有 GET 走 webstudio 的 mux，桌面窗口直接显示 studio.html + `/api/*` + `/logo.png`，零 sidecar、零前端构建链。桌面专属功能经 `StudioData.Desktop` flag 隔离，CLI `agent edit` 不受影响。issue #64。

## 成员清单

- **main.go** — Wails main：`wails.Run(&options.App{Title/Width/Height/MinWidth/MinHeight, AssetServer:{Handler: webstudio.Handler(app.StudioOptions())}, OnStartup, Bind:[app]})`。AssetServer.Handler 复用 webstudio 的 http 表面（`Assets:nil` → 所有 GET 转发到它；见 [../../internal/webstudio/CLAUDE.md](../../internal/webstudio/CLAUDE.md)）。
- **app.go** — `App`（Wails bound-method 宿主 + `ctx`）+ `NewApp` / `startup(ctx)` + `StudioOptions`（桌面注入 webstudio 的 `Data`/`OnSave`/`OnDeploy`）。**阶段2 骨架**：`placeholderSpec` 占位 StudioData + `Desktop=true` + stub 回调；扫描/部署真实接线（拖文件夹 → `pipeline.Run` → `BuildStudioData`；`OnDeploy` → `deployFromDirWithConfirm`）在后续阶段。
- **wails.json** — Wails 项目配置。`frontend:install`/`frontend:build` 空 → `wails build` 不碰前端（无 npm 构建链，只期望 AssetServer.Handler）。`outputfilename: AskDAO Studio`。

## 设计约束

- **本地建不了**：Wails 需 CGO + 各 OS 原生 GUI 工具链（mac Xcode / win WebView2 / linux gtk+webkit2gtk），不能交叉编译。编译打包只在 per-OS matrix CI（[`../../.github/workflows/desktop-build.yml`](../../.github/workflows/desktop-build.yml)：macos-latest `darwin/universal` + windows-latest `windows/amd64`）；GUI 真机运行靠有 go+wails 环境的机器或 CI artifact。开发机（无 go/wails）只能 build/test 非 studio 包。
- **零新后端 + 复用 CLI 核心**：桌面能力全经 `internal/`（pipeline/deploy/auth/webstudio/...）+ Conductor 现有 endpoint，不新增服务端。
- **不破坏 CLI**：桌面新增经 `Desktop` flag + 桌面专属回调隔离，`cmd/askdao` 与 `internal/webstudio` 的 CLI 路径零变更（现有测试全绿守护）。

## 里程碑（M1）

阶段1 webstudio `Desktop` flag 地基 ✅ → **阶段2 Wails 壳 + CI（本目录骨架）** → 登录 UI / 拖文件夹接扫描 / 可见性降级应用内确认 / 内嵌助手侧栏 / 测试聊天 / SKILL 校验+自更新（后续，各独立 commit）。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
