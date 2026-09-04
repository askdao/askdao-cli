# cmd/askdao/
> L2 | 父级: ../../CLAUDE.md

CLI 入口与用户命令（auth · mcp · agent edit/deploy · update）的实装。只做参数解析 + IO + 调用 internal/ 各层；审阅入口收敛为 `agent edit` 本地 Web 工作台。实现细节见各文件头注释；历史变更见 git log / PR。

## 成员清单

- **main.go** — subcommand router（stdlib flag + 手写 dispatch，无第三方 CLI 框架）
- **common.go** — edit/deploy 共享 helper（文件名常量 / ensureAskdaoDir / defaultAgentName / chooseLLMClient）
- **auth.go** — auth login/status/logout（Device Code Flow 编排 + 凭据落盘；登录成功自动配置 askdao-mcp，fail-soft）
- **mcp.go** — mcp setup：取 gateway 凭证写本机 Claude Code(.claude.json upsert) / Codex(config.toml 追加)，幂等 + 原子写 + malformed 拒绝 clobber；--print 零写入
- **update.go** — 自升级命令壳（-dev 构建拒绝，--force 越过）
- **edit.go** — agent edit 核心命令：loadOrScan（加载或扫描生成草稿 + 确定性硬字段覆盖）→ webstudio.Serve（OnSave/OnDeploy 回调注入）；--observe 观测流程；--no-ui 降级写草稿退出；模型目录注入
- **deploy.go** — agent deploy：读原始 yaml 字节 + diff 显示 + deployflow 装配 → 409 三义分流（KOL 资料引导 / blocking-warning gating / 降级确认 prompt）+ update-mode 与 schedule 警示输出
- **\*\_test.go** — cmd/deploy/mcp/skill 打包各 e2e 与回归（httptest 假服务端）

## 设计约束

- **stdlib only**：不引 cobra；手写 router 简单稳定二进制小
- **KOL Profile 归云端**：遇 `kol_profile_required` 统一引导去订阅模式页（URL 优先服务端下发），不本地 prompt/PATCH
- **deploy 发原始 yaml 字节**：不 Marshal 往返，保留注释/顺序/未知字段（服务端 forward-compat）
- **危险默认值绝不靠沉默达成**：降级确认 stdin 非 TTY/EOF/非 yes 一律拒绝并指引 `--confirm-downgrade`
- **退出码**：0 成功 / 1 业务错 / 2 用法错 / 3 conductor 未配置（deploy）

## 端到端验证

`make build && ./askdao agent edit --dir <proj> --no-ui` → 写 askdao-agent.yml + `.askdao/`；默认起 127.0.0.1 工作台；deploy e2e 验 multipart + skill zip + update-mode 输出。

## 已知限制

- syft 不在 PATH 时 packages 列表空（软警告提示安装）
- 远端 ID 不写回 yaml `status:`

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
