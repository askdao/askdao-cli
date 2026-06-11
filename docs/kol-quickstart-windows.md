# AskDAO 创作者快速上手指南（Windows）

> v0.4 | 2026-06-11 | 面向对象：使用 Windows + Claude Code / Codex 的 KOL/Builder
>
> 走完本指南，你将完成：安装 askdao 命令行工具 → 让你本地的 Agent 项目接入 askdao-mcp 工具集 → 本地调试确认 → 一条命令部署到 askdao.ai，供你的订阅者使用。
> 全程约 20 分钟。遇到问题直接联系平台方（Sam）。
>
> v0.4 变更：§1 补 PowerShell `.\` 前缀说明 + 用户级 PATH 正确写法（真机验证反馈）。v0.3 变更：§1 增补 Release 自助下载命令（gh release download）。v0.2 变更：`askdao auth login` 已内置 askdao-mcp 自动配置，不再需要平台方单独交付 MCP token，原手工配置步骤降级为备用方案。

---

## 0. 准备清单

开始前确认你已有：

- [ ] Windows 10/11（PowerShell 可用）
- [ ] Claude Code 或 Codex 至少装好一个，并且你的 Agent 项目能正常跑
- [ ] 平台方交付的 `askdao.exe`（或私有 GitHub Release 下载权限）

> MCP 访问凭证**不需要**单独申请——第 3 步登录时平台自动下发并配置。

## 1. 安装 askdao.exe

两种获取方式任选其一：

**A. 平台方直发**：拿到 `askdao.exe` 后直接跳到下面的 PATH 设置。

**B. 从 GitHub Release 自助下载**（已被邀请为仓库协作者时；后续升级也走这条）：

```powershell
winget install GitHub.cli          # 装 gh（已装可跳过）
gh auth login                      # 浏览器登录 GitHub（一次性）
gh release download -R askdao/askdao-cli --pattern "*windows_amd64*"
Expand-Archive askdao_*_windows_amd64.zip -DestinationPath .
```

解压后先在 exe 所在目录就地验证（**PowerShell 必须加 `.\` 前缀**，直接敲 `askdao` 会报 CommandNotFoundException——这是 PowerShell 的规则，不是安装坏了）：

```powershell
cd askdao_*_windows_amd64    # Expand-Archive 默认解到同名子目录
.\askdao.exe --help
```

然后把 `askdao.exe` 放到固定目录并加入用户 PATH：

```powershell
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\askdao\bin" | Out-Null
Move-Item .\askdao.exe "$env:LOCALAPPDATA\askdao\bin\askdao.exe" -Force
[Environment]::SetEnvironmentVariable("Path",
  [Environment]::GetEnvironmentVariable("Path","User") + ";$env:LOCALAPPDATA\askdao\bin", "User")
```

**重开一个终端窗口**（PATH 改动只对新终端生效），验证：

```powershell
askdao --help
```

## 2. 注册 askdao.ai 并激活创作者资料

1. 打开 https://askdao.ai 注册账号（支持 Google/GitHub/邮箱）
2. 打开 **https://askdao.ai/dashboard/subscription**，选择一档**订阅模式**并点击「激活订阅模式」按钮——这一步是部署的硬前提
   - **免费**：任何人访问你的主页即可订阅
   - **访问密码**：订阅者需输入你设置的口令才能加入（口令可随时改）
   - 付费模式 Phase 2 开放
3. 打开 **https://askdao.ai/dashboard/profile**，补全展示名、头像、个人简介（订阅者会看到）

> 第 2 步不做的话，后面 `agent deploy` 会报错并附上激活页链接——注意：只填 profile 页**不够**，必须在 subscription 页激活过订阅模式。

## 3. 登录命令行（自动完成 askdao-mcp 配置）

```powershell
askdao auth login
```

浏览器自动弹出授权页，确认后登录完成。**登录成功会自动配置 askdao-mcp**——从平台取回接入凭证，写好 Claude Code 和 Codex 的本机配置（含 `ASKDAO_MCP_TOKEN` 环境变量），终端输出类似：

```
✓ Logged in as you@example.com. Token saved to ...
→ Setting up askdao-mcp for your local harnesses …
✓ Claude Code: askdao-mcp configured in ~/.claude.json
✓ Codex: askdao-mcp configured in ~/.codex/config.toml
```

**重开一个终端窗口**让环境变量生效。

- 浏览器没弹出来？用 `askdao auth login --no-browser`，按提示手动打开链接并输入配对码
- 登录状态验证：`askdao auth status`（凭证在 `%APPDATA%\askdao\credentials.json`）

## 4. 验证 askdao-mcp 已连上

askdao-mcp 是平台的工具网关——播客生成、语音合成、文生图、美股行情、SEC 财报等 19 个工具，完整清单与用法见 **[askdao-mcp 工具参考](askdao-mcp-reference.md)**。第 3 步已自动配置，这里只需验证：

- **Claude Code**：任意项目里启动 `claude`，输入 `/mcp`，应看到 `askdao-mcp` 已连接，工具列表含 `listenhub_*`、`elevenlabs_*`、`sec_*` 等
- **Codex**：启动 `codex`，问它"列出 askdao-mcp 可用的工具"

### 自动配置失败 / 想重跑？

```powershell
askdao mcp setup            # 随时重跑自动配置
askdao mcp setup --print    # 只输出手工配置片段，不写任何文件
```

`--print` 会给出三段内容：Claude Code 的 `claude mcp add` 一行命令、Codex 的 `config.toml` 片段、`ASKDAO_MCP_TOKEN` 环境变量设置——照贴即可，适用于自动配置不可用的极端情况。

> Codex 项目补充：askdao 工具能直接读取 Codex 配置（`%USERPROFILE%\.codex\config.toml` 与项目内 `.codex/config.toml`），`agent edit` 可自动发现 askdao-mcp。仅 v0.9 之前的老版本 askdao.exe 需要在项目根放 `.mcp.json` 兼容写法（token 走 `${ASKDAO_MCP_TOKEN}` 环境变量展开，可安全进 git）。

## 5. 本地调试你的 Agent

用你自己的 harness 正常跑 Agent 项目，确认它能真实调用 askdao-mcp 的工具（比如让它查一只股票行情、生成一段语音）。**调试满意了再进入下一步**——部署后线上跑的就是这套行为。

如果你的 Agent 通过 **Skill** 编排这些工具，编写前请读 [askdao-mcp 工具参考](askdao-mcp-reference.md) 的「Skill 编写注意事项」一节——产物 URL 时效、异步任务模式、配额查询等都有讲究。

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
| `askdao` 报 CommandNotFoundException | 两种情况：还没做 §1 的 PATH 步骤（在 exe 目录用 `.\askdao.exe` 可先验证）；或 PATH 设置后没重开终端 |
| `deploy` 报 "profile isn't set up" | 去 https://askdao.ai/dashboard/subscription 激活一次订阅模式（见第 2 步）；只填 profile 页不解除此限制 |
| 登录后看到 `! askdao-mcp setup skipped: ...` | 登录本身成功；按提示原因处理后跑 `askdao mcp setup` 重试（如本机未装任何 harness、平台侧暂未配置） |
| `ASKDAO_CONDUCTOR_TOKEN is set but ... URL is not` | 你设置过环境变量覆盖，要么成对设置，要么清掉走 `auth login` 凭证 |
| `/mcp` 显示 askdao-mcp 连接失败 / 401 | 终端没重开（环境变量未生效）或配置被改动；`echo $env:ASKDAO_MCP_TOKEN` 确认非空，再跑 `askdao mcp setup` |
| `agent edit` 扫不到 askdao-mcp | 跑一次 `askdao mcp setup`；老版本 askdao.exe 对 Codex 项目需项目根 `.mcp.json`（见第 4 节补充） |
| 某 MCP 标"stdio 不可部署" | 正常——本机进程型 server 线上不支持，取消勾选即可 |
| 浏览器始终弹不出来 | 所有需要浏览器的命令都支持手动路径：`auth login --no-browser`；`agent edit --no-ui` |

### 路径速查（Windows）

| 内容 | 位置 |
|------|------|
| CLI 登录凭证 | `%APPDATA%\askdao\credentials.json` |
| Claude Code 用户级 MCP 配置 | `%USERPROFILE%\.claude.json` |
| Codex MCP 配置 | `%USERPROFILE%\.codex\config.toml` |
| MCP token 环境变量 | 用户环境变量 `ASKDAO_MCP_TOKEN`（`mcp setup` 自动设置） |

---

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
