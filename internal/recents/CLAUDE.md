# internal/recents/
> L2 | 父级: ../../CLAUDE.md

桌面 studio 的「最近项目」本地便利态持久化（`recent-projects.json`，与 auth 同配置目录 `$XDG_CONFIG_HOME/askdao/` 或 `os.UserConfigDir()/askdao/`）。让多项目工作台记住最近打开的项目 + 每项目上次部署记录，跨 app 重启。**桌面-only**：被 `cmd/askdao-studio/app.go` 消费；CLI `agent edit` 不触碰。

## 成员清单

- **recents.go** — `File{Version, Projects[]}` / `Project{Dir, Label, LastOpenedAt, Deploy}` / `DeployRecord{AgentID, MetadataName, Created, PreviousManagedVersion, GroupLink, AnthropicAgentID, LastDeployedAt}` + `SchemaVersion=1` + `Load` / `Save` / `Path` + `(*File).Touch`（MRU upsert 前插 + `maxEntries=12` 淘汰，保留既有 Deploy）/ `Find` / `SetDeploy`（缺则建）/ `Remove`（幂等）/ `normalize`（`filepath.Clean` + best-effort `EvalSymlinks` → `/foo`、`/foo/`、软链折叠成一条 dedup key）。复用 `auth.ConfigDir()` 定位配置目录；原子写（MkdirAll 0700 + CreateTemp + Chmod 0600 + Rename）镜像 `auth.Save` 但**不重构**它。**关键差异**：读失败/损坏/版本漂移一律优雅降级为空 `File`（便利态非身份，绝不 brick 工作台），区别于 `auth.Load` 的硬报错。
- **recents_test.go** — Save/Load round-trip（含 `DeployRecord` + `*int` 版本）/ 缺文件·损坏 JSON·future version 三态优雅空 / Touch MRU 顺序 + 再触前移 / `maxEntries` 淘汰 / 再触保留 Deploy / SetDeploy 缺则建 / normalize `/foo` vs `/foo/` dedup / Remove 幂等 / 0600 文件 + 0700 目录（POSIX）。用 `t.Setenv("XDG_CONFIG_HOME", t.TempDir())` 隔离（镜像 credentials_test）。

## 设计约束

- **便利态非身份**：`recent-projects.json` 是可丢弃的 UI 便利缓存（最近项目 + 部署速览），不是真相源。任何读失败静默降级为空，绝不阻塞工作台；这是与 `internal/auth` credentials（硬报错）的有意分野。
- **dir 为键 vs (owner,name) 身份漂移**：`Project.Dir`（normalize 后）是 dedup key，但部署身份是服务端的 `(owner, metadata.name)`。故 `DeployRecord.MetadataName` 存 deploy 当时的 name —— 改名后列表仍显「deployed as <旧名>」，把漂移暴露出来而非静默错配。
- **桌面-only**：只 `cmd/askdao-studio` import；CLI `agent edit` 路径零触碰。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
