# internal/auth/
> L2 | 父级: ../CLAUDE.md

askdao-cli 身份层 —— OAuth 2.0 Device Code Flow（RFC 8628）客户端 + 本地凭据持久化。`askdao auth login` 调本包驱动整条 device flow；`askdao agent deploy` 通过 `Load` 读凭据。设计稿 [../../docs/cli-auth-device-flow.md](../../docs/cli-auth-device-flow.md)。

## 成员清单

- **credentials.go** — `Credentials{}` 结构 + `Save` / `Load` / `Delete` / `Path` + `DefaultServerURL` / `ErrNoCredentials` sentinel。XDG-aware 路径解析（`$XDG_CONFIG_HOME/askdao/credentials.json` → `os.UserConfigDir()/askdao/`）；POSIX 上文件 0600 + 父目录 0700；原子写入（write-to-temp + rename）防止半写。schema version 1 锁字段顺序与必填项。
- **credentials_test.go** — Save/Load roundtrip / 缺失文件返 `ErrNoCredentials` / 损坏 JSON / future schema version 拒绝 / 0600 模式不变量（POSIX）/ Delete 幂等。所有用例用 `t.Setenv("XDG_CONFIG_HOME", t.TempDir())` 隔离配置目录。
- **device.go** — `DeviceFlow{}` HTTP 客户端：`Start` / `Poll` / `PollUntilApproved`；OAuth `error` 字段映射到 5 个 sentinel（`ErrAuthorizationPending` / `ErrExpiredToken` / `ErrAccessDenied` / `ErrAlreadyConsumed` / `ErrInvalidDeviceCode`）；轮询循环遇 `pending` 等下一 tick，遇任何 terminal 错误立即返回；deadline 既覆盖 select 间隙也覆盖 in-flight Poll（两者都规整化成 `ErrExpiredToken`，UX 一致）。
- **device_test.go** — `httptest.Server` mock conductor：Start happy/502 / Poll 5 个错误码全覆盖 + Success / `PollUntilApproved` 多次 pending → token / 超时变 `ErrExpiredToken` / terminal 错误立即上抛。

## 设计约束

- **AccessToken 不可恢复**：明文 token 只在 `auth login` 兑换瞬间存在；后续生命周期完全靠 `credentials.json` 持有。文件丢失 → 必须 `auth login` 重来；服务端只存 SHA-256 hash（conductor `cli_token` 表）。
- **device flow 状态机不在 CLI 侧**：本包是无状态 HTTP 客户端；`pending → approved → consumed` 等转换全在 conductor。CLI 只 own 命令行 UX + 凭据落盘。
- **`server` 字段绑定时机**：login 时把当时用的 conductor URL 写进 `credentials.Server`，deploy 默认从该字段读 —— 防止用户后来换 URL 但 token 已绑老 URL 时出现"看似合理实则错配"的故障模式。env `ASKDAO_CONDUCTOR_URL` 仍可显式覆盖（必须跟 `_TOKEN` 成对）。
- **`auth logout` 仅本地**：服务端撤销需要登录态 + 列表 UI，留给 web v2。本地清空已足以阻止本机继续使用 token；如怀疑泄露应该走 web UI revoke。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
