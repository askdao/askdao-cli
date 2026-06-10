# AskDAO 创作者快速上手指南（Windows）

> v0.1 | 2026-06-10 | 面向对象：使用 Windows + Claude Code / Codex 的 KOL/Builder
>
> 走完本指南，你将完成：安装 askdao 命令行工具 → 让你本地的 Agent 项目接入 askdao-mcp 工具集 → 本地调试确认 → 一条命令部署到 askdao.ai，供你的订阅者使用。
> 全程约 30 分钟。遇到问题直接联系平台方（Sam）。

---

## 0. 准备清单

开始前确认你已有：

- [ ] Windows 10/11（PowerShell 可用）
- [ ] Claude Code 或 Codex 至少装好一个，并且你的 Agent 项目能正常跑
- [ ] 平台方交付给你的两样东西：
  - `askdao.exe`（或私有 GitHub Release 下载权限）
  - **MCP 访问 token**（一串 `mcp_` 或随机字符，下文称 `<MCP_TOKEN>`）

## 1. 安装 askdao.exe

把 `askdao.exe` 放到固定目录并加入 PATH（PowerShell 执行）：

```powershell
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\askdao\bin" | Out-Null
Move-Item .\askdao.exe "$env:LOCALAPPDATA\askdao\bin\askdao.exe"
[Environment]::SetEnvironmentVariable("Path", $env:Path + ";$env:LOCALAPPDATA\askdao\bin", "User")
```

**重开一个终端窗口**，验证：

```powershell
askdao --help
```

## 2. 注册 askdao.ai 并补全创作者资料

1. 打开 https://askdao.ai 注册账号（支持 Google/GitHub/邮箱）
2. 进入个人设置，补全**创作者资料**：加入方式（`kol_join_mode`：free/invitation/paid）+ 个人简介（`kol_bio`）

> 这一步不做的话，后面 `agent deploy` 会报 409 并提示你回来补全。

## 3. 登录命令行

```powershell
askdao auth login
```

- 浏览器会自动弹出授权页，确认后终端显示登录成功
- 浏览器没弹出来？用 `askdao auth login --no-browser`，按提示手动打开链接并输入屏幕上的配对码
- 凭证保存在 `%APPDATA%\askdao\credentials.json`，验证：`askdao auth status`

## 4. 配置 askdao-mcp（关键步骤）

askdao-mcp 是平台的工具网关（播客生成、语音合成、文生图、美股行情、SEC 财报等），地址 `https://mcp.askdao.ai/mcp`。你需要让本地的 Claude Code / Codex 连上它，调试确认后再部署。

### 4.1 先把 token 设为用户环境变量（一次性）

```powershell
[Environment]::SetEnvironmentVariable("ASKDAO_MCP_TOKEN", "<MCP_TOKEN>", "User")
```

设完**重开终端**生效。两个 harness 都引用这个变量，token 不会进任何代码仓库。

### 4.2 Claude Code 用户

```powershell
claude mcp add --transport http askdao-mcp https://mcp.askdao.ai/mcp --header "Authorization: Bearer ${ASKDAO_MCP_TOKEN}" --scope user
```

验证：在任意项目里启动 `claude`，输入 `/mcp`，应能看到 `askdao-mcp` 已连接，工具列表含 `listenhub_*`、`elevenlabs_*`、`sec_*` 等。

### 4.3 Codex 用户

编辑 `%USERPROFILE%\.codex\config.toml`，追加：

```toml
[mcp_servers.askdao-mcp]
url = "https://mcp.askdao.ai/mcp"
bearer_token_env_var = "ASKDAO_MCP_TOKEN"
```

验证：启动 `codex`，问它"列出 askdao-mcp 可用的工具"。

### 4.4 Codex 开发的项目：额外在项目根放一个 `.mcp.json`

askdao 工具目前扫描不到 Codex 的 TOML 配置。在你的 Agent 项目根目录创建 `.mcp.json`（这个文件可以进 git，token 走环境变量展开，不会泄露）：

```json
{
  "mcpServers": {
    "askdao-mcp": {
      "type": "http",
      "url": "https://mcp.askdao.ai/mcp",
      "headers": { "Authorization": "Bearer ${ASKDAO_MCP_TOKEN}" }
    }
  }
}
```

Claude Code 开发的项目不需要这一步（4.2 的 user 配置就能被发现）。

## 5. 本地调试你的 Agent

用你自己的 harness 正常跑 Agent 项目，确认它能真实调用 askdao-mcp 的工具（比如让它查一只股票行情、生成一段语音）。**调试满意了再进入下一步**——部署后线上跑的就是这套行为。

> 注意：部署后平台会在服务端自动注入 mcp.askdao.ai 的访问凭证，你本机的 token 只用于本地调试，不会被上传。

## 6. 审阅并部署

在 Agent 项目根目录：

```powershell
askdao agent edit
```

- 浏览器打开本地工作台，展示扫描结果：你的 skill、MCP server、系统提示词等
- 确认 `askdao-mcp` 已被勾选（远程 URL 类型默认勾选）
- 如果看到某个 MCP 标了警告"stdio 不可部署"——那是只能跑在你本机的 server，线上不支持，属正常提示
- 审阅确认后保存，生成 `askdao-agent.yml`

然后部署：

```powershell
askdao agent deploy
```

成功后终端会输出 Agent 地址。同名再次 deploy 即为更新（in-place）。

## 7. 验证上线

1. 打开 askdao.ai，进入你的创作者主页，确认 Agent 已出现
2. 用另一个账号（或让朋友）订阅并对话，确认工具调用正常

## 常见问题

| 现象 | 原因与解法 |
|------|-----------|
| `deploy` 报 409 | 创作者资料未补全，回 askdao.ai 设置页补 `kol_join_mode` + `kol_bio`（见第 2 步） |
| `ASKDAO_CONDUCTOR_TOKEN is set but ... URL is not` | 你设置过环境变量覆盖，要么成对设置，要么清掉走 `auth login` 凭证 |
| `/mcp` 显示 askdao-mcp 连接失败 / 401 | token 没设对或终端没重开；`echo $env:ASKDAO_MCP_TOKEN` 确认非空 |
| `agent edit` 扫不到 askdao-mcp | Codex 项目需要项目根 `.mcp.json`（见 4.4） |
| 某 MCP 标"stdio 不可部署" | 正常——本机进程型 server 线上不支持，取消勾选即可 |
| 浏览器始终弹不出来 | 所有需要浏览器的命令都支持手动路径：`auth login --no-browser`；`agent edit --no-ui` |

### 路径速查（Windows）

| 内容 | 位置 |
|------|------|
| CLI 登录凭证 | `%APPDATA%\askdao\credentials.json` |
| Claude Code 用户级 MCP 配置 | `%USERPROFILE%\.claude.json` |
| Codex MCP 配置 | `%USERPROFILE%\.codex\config.toml` |
| MCP token 环境变量 | 用户环境变量 `ASKDAO_MCP_TOKEN` |

---

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
