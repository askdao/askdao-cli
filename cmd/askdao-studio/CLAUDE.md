# cmd/askdao-studio/
> L2 | 父级: ../../CLAUDE.md

AskDAO Studio 桌面 app（Wails v2 main）—— askdao-cli 第二入口，面向无 CLI 经验用户。复用 `internal/webstudio`：`AssetServer.Handler` 直接挂 webstudio 的 mux，零 sidecar、零前端构建链。桌面专属功能经 `StudioData.Desktop` flag 隔离，CLI `agent edit` 无感。实现细节见各文件头注释；历史变更见 [../../CHANGELOG.md](../../CHANGELOG.md)。

## 成员清单

- **main.go** — Wails main（AssetServer.Handler 复用 webstudio；Mac.TitleBar 必须显式标准标题栏——否则全宽顶栏盖住红绿灯窗口关不掉）
- **app.go** — App 宿主：把桌面回调注入 webstudio——扫描三段（pick/run/cancel）/ save / deploy（deployflow 单源）/ chat 与 assistant（SSE 透传，官方助手 id 经 env 接缝解析，未配降级不误聊）/ 外链桥接 / skill 校验与补全；登录经 internal/auth 与 CLI 共用 credentials.json
- **wails.json** — Wails 项目配置（frontend build 全空，不碰前端）
- **build/appicon.png** — app 图标源图（wails build 自动生成 icns）

## 设计约束

- **本地建不了**：Wails 需 CGO + 各 OS 原生 GUI 工具链，不能交叉编译；编译只在 per-OS matrix CI（desktop-build.yml）
- **零新后端 + 复用 CLI 核心**：桌面能力全经 internal/ + Conductor 现有 endpoint
- **不破坏 CLI**：桌面新增经 Desktop flag + 专属回调隔离，CLI 路径零变更（现有测试守护）

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
