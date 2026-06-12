# Windows KOL Onboarding 计划（短期 + 中期）

> v0.2 | 2026-06-11 | 状态：M1/M2/M3 已交付，M4 核心已交付，**M5 开源已兑现（2026-06-11）**
>
> **2026-06-11 重大更新**：askdao-cli 仓库已转 public（开源前检查：gitleaks 全历史零泄露 + issue/PR 正文评论扫描干净）。一键安装上线——`irm https://askdao.ai/install.ps1 | iex`（Windows）/ `curl -fsSL https://askdao.ai/install.sh | bash`（Unix），脚本 canonical 在 `install/`，经 askdao.ai rewrite 反代分发；新增 `askdao update` 自升级命令。**S1（outside collaborator）随之作废**——仓库公开后无需邀请。
>
> 目标：让第一个使用 Windows 的外部 KOL/Builder 完成「安装 askdao-cli → 本地 Agent 项目适配 askdao-mcp → 调试确认 → 部署到 askdao.ai」全链路。
> 短期方案以**人工运维链路**让单个 KOL 本周跑通；中期方案把人工环节逐项自动化，支撑第 2~N 个 KOL 自助 onboarding。

---

## 0. 现状盘点（2026-06-10 调查结论）

| 维度 | 现状 | 判断 |
|------|------|------|
| 代码跨平台 | 路径全走 `os.UserConfigDir()`（Win 落 `%APPDATA%\askdao\`）+ `filepath.FromSlash`；chmod 仅 POSIX；浏览器拉起 Win 用 `cmd /c start` fail-soft（`auth.go:215`） | ✅ ready |
| 命令集 | `auth login/status/logout` + `agent edit/deploy`，token 解析 env 成对 > credentials.json | ✅ ready |
| Deploy 管线 | `POST /api/v1/cli/deploy` 已在生产 api.askdao.ai，含 skill sync + in-place update + 409 引导补全 KOL 资料 | ✅ ready |
| MCP 扫描门控 | 远程 http/sse 归一 `type: url`，`anthropic_compatible` 门控 + stdio 警示完整 | ✅ ready |
| 二进制分发 | 无 CI / 无 GoReleaser / 无预编译二进制 / 无安装脚本 | ❌ missing |
| 仓库可见性 | `askdao/askdao-cli` private；org 默认成员权限 **write**（不可加 org member） | ⚠️ 需配置 |
| Codex MCP 扫描 | 仅 skill 路径已接，`~/.codex/config.toml` MCP TOML 未接（已拆独立 issue，见 handoff-C） | ⚠️ partial |
| gateway token 发放 | `GATEWAY_TOKENS_JSON` 静态 env，手工加 entry + 重启容器 | ⚠️ 手工 |
| KOL onboarding 文档 | 无端到端指南，无 Windows 说明 | ❌ missing |
| Windows 真机验证 | 从未做过 | ❌ missing |

**关键架构事实**：部署后的 Managed Agent 由 Conductor 经 Credential Vault 在**服务端**注入 mcp.askdao.ai 凭证；KOL 本机的 MCP 配置只服务两件事——本地调试时 harness 能调到 askdao-mcp 工具、`agent edit` 扫描时能发现该 server 并写进 agent.yml。

---

## 1. 短期方案（目标：本周，单个 KOL 跑通）

原则：**零开发或最小开发，人工链路可接受**。每一步都是运维操作 + 文档，唯一可能的代码改动是无（交叉编译用现有源码）。

### S1. 仓库访问 — Outside Collaborator（半小时）～~已作废 (2026-06-11)~~

> **作废**：仓库已转 public（M5 兑现），任何人可匿名 clone / 下载 Releases，无需邀请协作者。以下保留为历史记录。

不公开仓库，只给这一个 KOL 单仓 Read 权限：

```bash
gh api -X PUT repos/askdao/askdao-cli/collaborators/<github-username> -f permission=pull
```

- 他收邀请邮件接受后：可 clone、可读源码（信任锚点）、可下载本仓私有 Releases
- **禁止**加为 org member（org `default_repository_permission=write`，会获得全 org 写权限）
- 后续移除：`gh api -X DELETE repos/askdao/askdao-cli/collaborators/<username>`

### S2. 二进制分发 — 交叉编译直发（半小时）

```bash
cd askdao-cli
GOOS=windows GOARCH=amd64 go build -o dist/askdao.exe ./cmd/askdao
```

- 产物直接发给他（安全渠道），或 `gh release create` 挂到私有 Release（协作者可见，他后续可 `gh release download -R askdao/askdao-cli` 自助升级）
- 告知放置位置建议：`%LOCALAPPDATA%\askdao\bin\askdao.exe` 并加入 PATH
- 纯 stdlib 无 cgo，交叉编译无额外依赖；syft 为可选依赖，缺失时仅包扫描为空，不阻塞

### S3. 平台账号与 KOL 资料（KOL 自助，10 分钟）

1. askdao.ai 注册（better-auth，无邀请码机制）
2. 首次 deploy 若未补全资料会返 409 + hint —— 提前引导他在 Web 端补全 `kol_join_mode`（free/invitation/paid）+ `kol_bio`
3. `askdao auth login`（OAuth Device Code Flow，浏览器拉起失败时走 `--no-browser` 手动输码），token 落 `%APPDATA%\askdao\credentials.json`

### S4. MCP gateway token 发放（运维手工，半小时）

1. Secrets Manager `askdao/conductor` 的 `GATEWAY_TOKENS_JSON` 追加专属 entry：
   ```json
   {"id": "kol-<name>", "token": "<256bit 随机>", "scopes": ["mcp"]}
   ```
2. 重启 mcp-gateway 容器（ECS force new deployment）使 env 生效
3. token 经安全渠道交付 KOL
4. **凭证审计**：entry 的 `id` 带 KOL 名，便于日后吊销（删 entry + 重启）

### S5. KOL 本机 askdao-mcp 配置（一行命令）

写进 onboarding 文档，KOL 自己执行：

```bash
claude mcp add --transport http askdao-mcp https://mcp.askdao.ai/mcp \
  --header "Authorization: Bearer <token>" --scope user
```

- `--scope user` 落 `~/.claude.json`（Win 即 `%USERPROFILE%\.claude.json`），秘密不进项目 git
- 此后 `askdao agent edit` 扫描自动发现该 server，识别为 `type: url` + `anthropic_compatible: true`，默认勾选进 agent.yml
- **Codex 用户注意**：短期内 cli 不扫 `~/.codex/config.toml` 的 MCP；workaround 是在项目根放 `.mcp.json`（project scope）声明同一 server，cli 的 project-scope 扫描可发现。Codex 本地调试的 MCP 配置需按 Codex 自身文档另行配置

### S6. Onboarding 文档（半天）✅ 已落地 2026-06-10

`docs/kol-quickstart-windows.md` 已写就，面向 KOL 本人（非工程背景可读），覆盖：

1. 获取并放置 `askdao.exe`（PATH 设置截图级说明）
2. askdao.ai 注册 + KOL 资料补全
3. `askdao auth login`（含 `--no-browser` 备选路径）
4. `claude mcp add` 配置 askdao-mcp（S5 命令）
5. 本地 Agent 项目调试确认（用他自己的 Claude Code 跑通对 askdao-mcp 工具的调用）
6. `askdao agent edit` 审阅生成 agent.yml → `askdao agent deploy`
7. 路径速查表（`%APPDATA%\askdao\credentials.json` 等）与常见报错（409 资料未补全 / token 不成对 / stdio MCP 不可部署警示）

### S7. Windows 真机全链路验证（半天，发给 KOL 前必做）

在 Windows 真机或 VM 上自己完整走一遍 S2→S6，重点盯：

- [ ] `askdao.exe` 双击/PowerShell 均可执行，PATH 生效
- [ ] `auth login` 浏览器拉起（`cmd /c start`）+ credentials 写入 `%APPDATA%`
- [ ] `claude mcp add` 后 `agent edit` 扫描能发现 askdao-mcp 且勾选正确
- [ ] skill zip 打包路径分隔符正确（`filepath.FromSlash` 真机回归）
- [ ] Web Studio（`agent edit` 的本地工作台）在 Windows 浏览器表现正常
- [ ] `agent deploy` 端到端成功，conductor 侧 Agent/Group 创建正确

### 短期方案验收标准

> KOL 在自己的 Windows 机器上，按 quickstart 文档独立完成：安装 → 登录 → 本地调试调通 askdao-mcp 工具 → deploy 成功 → 其他用户可在 askdao.ai 订阅使用该 Agent。全程除 token 交付外无需平台方介入。

---

## 2. 中期方案（目标：第 2~N 个 KOL 自助化）

原则：**把短期方案中每个人工环节变成产品能力**。按依赖与收益排序。

### M1. 发布工程 — GoReleaser + GitHub Actions（约 1 天）

替代 S2 的手工交叉编译：

- `.goreleaser.yml`：windows/amd64 + darwin/arm64 + linux/amd64（顺手覆盖全平台）
- `.github/workflows/release.yml`：tag push 触发，产出挂私有 Release
- checksums + 版本号注入（`-ldflags -X main.version=`）
- 私有阶段协作者经 `gh release download` 获取；为日后开源预留 scoop manifest / `install.ps1` 的目录占位

### M2. `askdao mcp setup` — MCP 配置自动注入（约 1 天，跨 cli + conductor）✅ 第一步已落地 2026-06-10

替代 S4 交付后的 S5 手工命令，KOL 体验收敛为三行：`auth login` → `mcp setup` → 开始调试。
conductor 侧 endpoint（PR conductor#149）+ cli 侧命令均已实现；token 动态化（第二步）保持触发条件待命。
部署前置：Secrets Manager `askdao/conductor` 需手工 put `MCP_GATEWAY_CLI_TOKEN`（CDK 注入已就位，askdao-cloud@4c5801e）。

**conductor 侧**：新增 `GET /api/v1/cli/mcp-credentials`

- 鉴权：复用 `cli_*` token（与 deploy 同一链路）
- 返回：`{gateway_url, token}`
- 实现分两步走：
  - **第一步（单 KOL~少量 KOL）**：conductor env 配一个面向 KOL 的 gateway token（或从既有 secret 读），endpoint 原样下发——不改 gateway
  - **第二步（触发条件：KOL 数 > 3 或出现吊销需求）**：token 动态化——gateway 的校验从静态 `GATEWAY_TOKENS_JSON` 改为回调 conductor 校验（或 conductor 签发 JWT、gateway 本地验签）。凭证的发放与校验收敛到 conductor 唯一真相源，与 per-KOL vault（alembic 028 伏笔）同一条演化线

**cli 侧**：新增 `askdao mcp setup` 命令

- 调上述 endpoint 取 `{gateway_url, token}`
- 复用 `harness_scope.go` 的 harness 检测
- claude → 写 `~/.claude.json` 的 `mcpServers`（user scope）
- codex → 写 `~/.codex/config.toml` 的 `mcp_servers`（依赖 M3 的 TOML 能力）
- 幂等：已存在同名 entry 时更新而非重复追加；`--print` 模式只输出配置不落盘（供手工粘贴）

### M3. Codex MCP TOML 支持（约 1 天，已有独立 issue）✅ 扫描侧已落地 2026-06-10

`internal/scanner` 已接入 Codex TOML MCP 扫描：`readMCPConfig` 按扩展名分派 JSON/TOML，project scope 扫 `.codex/config.toml`、user scope 扫 `~/.codex/config.toml`（`.codex`/`.agents` marker 门控）。M2 的 codex 写入能力随 `askdao mcp setup` 交付。

### M4. KOL onboarding 产品化（按需，约 1~2 天）✅ 核心已落地 2026-06-10

- ✅ 订阅模式三档完整实现（web PR #122 + conductor PR #150）：免费(free) / 访问密码(invitation，含订阅侧密码 gate + alembic 041 口令存储) / 付费(Phase 2 disabled)；NULL 态显式「激活订阅模式」按钮根治 radio 死点击致 409 无法解除的 bug
- ✅ My Agents 页 `<ActivationNotice>` 激活引导（新用户首落页）
- ✅ deploy 409 引导链路根治：conductor 下发 `detail.setup_url`，CLI/工作台优先渲染（cli PR #59），硬编码降级为 fallback
- 未做（保持按需）：独立注册后 onboarding 引导页（下载 cli → auth login 三步 checklist）——现有 EmptyState CLI 引导 + ActivationNotice 已覆盖主动线

### M5. 开源决策点（非工程，战略）— ✅ 已兑现（2026-06-11）

哥拍板提前兑现（一键安装的硬前提：脚本无法带凭证，二进制必须匿名可达）：

- ✅ 公开前检查：gitleaks 全历史 98 commits 零泄露；全部 issue/PR 正文 + 评论扫描无密钥形态；LICENSE MIT
- ✅ `gh repo edit --visibility public` 完成，匿名下载 v0.1.0 Release 资产验证通过（issue #48 关闭）
- ✅ 一键安装上线：`install/` 三脚本（sh/ps1/cmd，checksum 校验 + PATH 处理）+ askdao.ai rewrite 反代（`https://askdao.ai/install.{sh,ps1,cmd}`）
- ✅ `askdao update` 自升级命令（internal/selfupdate，原子换装 + Windows .old 策略）
- 未做：scoop/winget/homebrew 包管理器渠道（一键脚本已覆盖主流，按需激活）

### 中期方案验收标准

> 新 KOL 无需平台方任何人工操作：自助注册 → 下载安装（一行 PowerShell）→ `askdao auth login` → `askdao mcp setup` → 调试 → `askdao agent deploy`。平台方仅在审核/吊销时介入。

---

## 3. 优先级与时间线

| 序 | 事项 | 阶段 | 估时 | 依赖 |
|----|------|------|------|------|
| 1 | S1 仓库协作者 + S2 交叉编译直发 | 短期 | 1 小时 | — |
| 2 | S4 gateway token 手工发放 | 短期 | 半小时 | — |
| 3 | S6 quickstart 文档 | 短期 | 半天 | S1-S5 定稿 |
| 4 | S7 Windows 真机验证 | 短期 | 半天 | S2/S6 |
| 5 | M1 GoReleaser + release workflow | 中期 | 1 天 | — |
| 6 | M2 `askdao mcp setup` + conductor endpoint（第一步） | 中期 | 1 天 | — |
| 7 | M3 Codex MCP TOML | 中期 | 1 天 | 视 KOL harness 提级 |
| 8 | M2 第二步 token 动态化 | 中期后段 | 2 天 | KOL 数 > 3 触发 |
| 9 | M4 onboarding 产品化 / M5 开源 | 按需 | — | 战略决策 |

## 4. 风险与开放问题

1. ~~第一个 KOL 的 harness 未确认~~ **已确认（2026-06-10）：Claude Code 和 Codex 都用**。短期靠双轨覆盖：Codex 本地调试走 `config.toml` `bearer_token_env_var`（官方支持远程 HTTP MCP），cli 扫描走项目根 `.mcp.json` env 展开 workaround（quickstart §4.4）。M3（Codex MCP TOML 扫描）优先级提升，但不阻塞短期方案；Codex 远程 MCP 真机连通性纳入 S7 验证项
2. **Windows 真机从未验证**——代码正确 ≠ 真机走通，S7 是发给 KOL 前的硬门槛
3. **gateway token 是共享静态凭证**——短期单 KOL 风险可控（专属 entry 可吊销），但泄露即全量 mcp 工具可用；M2 第二步前不给超过个位数 KOL 发放
4. ~~**私有 Release 下载要求 KOL 安装 gh CLI 并登录**~~ **已解除（2026-06-11）**：仓库公开 + 一键安装脚本，安装一行命令、升级 `askdao update`，零 gh 依赖

---

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
