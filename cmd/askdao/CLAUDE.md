# cmd/askdao/
> L2 | 父级: ../../CLAUDE.md

CLI 入口与用户命令（**auth login/status/logout · agent edit/deploy**）的实装。这里只做参数解析 + IO + 调用 pipeline / webstudio / deploy / auth；业务逻辑都在 internal/。**v0.8 命令精简**：核心审阅入口从一堆 CLI 子命令（init/show/detect/bundle + [A/E/R/S/D/F/M/W/P/Q] 字符菜单）收敛为单一 `agent edit` 本地 Web 工作台；旧命令的价值（扫描报告 / 上传清单 / 卡片审阅）收进工作台视图。

## 成员清单

- **main.go** — subcommand router。无第三方 CLI 框架；stdlib `flag` + 手写 dispatch。一级命令 `auth / agent / version / help`；`agent` 二级 dispatch 到 `edit / deploy`；`auth` 二级 dispatch 到 `login / status / logout`。
- **common.go** — 共享 helper（edit/deploy 共用）：`askdaoAgentFileName`(`askdao-agent.yml`) / `askdaoDirName`(`.askdao`) 常量 + `ensureAskdaoDir` + `defaultAgentName`（项目根 basename） + `chooseLLMClient`（env/credentials → ConductorClient，否则离线 MockClient；partial env 报错不静默 mock）。
- **auth.go** — `askdao auth login [--server url] [--name device-name] [--no-browser]` / `status` / `logout`。login 编排 OAuth 2.0 Device Code Flow（RFC 8628）：`auth.NewDeviceFlow(...).Start` → 打印 `user_code` + 开浏览器（`open`/`xdg-open`/`cmd /c start`）→ `PollUntilApproved` → `auth.Save` 落 `~/.config/askdao/credentials.json`（0600）。server URL 解析 `--server` > `$ASKDAO_CONDUCTOR_URL` > `auth.DefaultServerURL`。`status` 输出身份，无凭据 exit 1；`logout` 删本地。设计稿 [../../docs/cli-auth-device-flow.md](../../docs/cli-auth-device-flow.md) §6。
- **edit.go** — `askdao agent edit [--dir path] [--harness id] [--no-ui] [--force] [--observe]`。**v0.8 核心命令**：`runEdit` → `loadOrScan`（存在 `askdao-agent.yml` 则加载 + 重扫拿 skill/MCP 候选，返回 `loaded=true`；否则跑 `pipeline.Run` 生成草稿 + `writeBaseline` 落 `.askdao/`，`loaded=false`；**两分支都覆盖 `spec.Capabilities = recommender.DefaultCapabilities(detRiskHints(det))`** —— capabilities 是 hard field 不交 LLM 即兴，§9.13 同 skills）→ `loaded` 透传 `BuildStudioData(...,restorePrior=loaded)`：**编辑已有 yaml 时忠实回显 KOL 上次勾选的 skill/MCP（含 stdio MCP），全新草稿走默认策略** → 默认主题色（`webstudio.DefaultThemeForCategory`）→ 启 `webstudio.Serve`（`OnSave`=`writeAgentSpec` 写 yaml；`OnDeploy`=写 yaml + `deployFromDir` + `studioDeployError` 把 kol_profile 错转「去 askdao.ai/workspace 补填」引导 + `deployResultLine`）。**`--observe`**：观测真实 claude session 预勾真正用到的 skill/MCP —— 进流程先 `observe.SweepStale` 清残留，`webstudio.Serve` 的 `OnReady(port)` 回调里 `observe.Install` 写临时 PreToolUse hook（指向 `/api/observe`）+ `printObserveGuide` 引导 KOL 另开终端跑 `claude`，`defer` cleanup 字节级还原（零残留三件套见 [../../internal/observe/CLAUDE.md](../../internal/observe/CLAUDE.md)）。`--no-ui` 降级为扫描+写草稿退出（CI/headless）。复用同包：`chooseLLMClient` / `readSpec` / `resolveServerAndToken` / `deployFromDir` / `ensureAskdaoDir` / `defaultAgentName`。
- **deploy.go** — `askdao agent deploy [--dir path] [--harness id] [--force]`。读 `<dir>/askdao-agent.yml` **原始字节**（不 re-marshal）+ 解析；`.askdao/recommendation.yml` 存在则先 `render.DiffAgentSpec` 显示 KOL 改动；skill 打包统一经 **`packageSkills`**（**v0.8.1 起 runDeploy 删内联循环改调此函数，与工作台共用同一真相源 —— 消除分叉**）：每个 `custom_local` skill 先过 **frontmatter 前置校验**（`scanner.ParseSkillFrontmatter`：`name`/`description` 必填 + frontmatter name 跨 skill 唯一 —— description 是模型触发 skill 的语义匹配指令，缺了部署成功但永不激活，故 fail-fast；源自 design.md §9.11 野生案例验证），再经 `internal/deploy.ZipDir` 打 zip（harness 中性：`filepath.Base` 切上级路径）；经 `deploy.Client.Deploy` 以 `multipart/form-data` POST conductor `/api/v1/cli/deploy`。`409 kol_profile_required`（ADR-P21）→ **引导去 askdao.ai/workspace**（KOL profile 归云端，不再 prompt bio / 自动 PATCH，与 edit 的 `studioDeployError` 一致）；`ErrBlockingWarnings`（blocking = `TranslationAction.REJECTED`，severity 不 gate）→ `RenderTranslationWarnings(ViewAll)` + `--force` gating。**v0.7.1 update-mode**：`DeployResponse.Created` / `PreviousManagedVersion` 区分 `Created` vs `Updated (vN→vN+1)`。**复用函数（CLI + 工作台共用）**：`packageSkills(dir, spec)`（枚举 + zip custom_local skill，含 `resolveSkillDir` —— project 相对路径 join dir，**user scope 的绝对/`~`/`Scope=="user"` 路径直接解析**，供工作台勾选的全局 skill 打包；**`runDeploy` 与 `deployFromDir` 均经此**）+ `deployFromDir(ctx, dir, harness, force)`（读 yaml + packageSkills + Deploy，无交互，返回 typed error；被 `edit` 的一站式 OnDeploy 复用）。helper：`printDeployResult` / `printDeployProgress` / `formatSkillRef` / `toRenderWarnings` / `resolveServerAndToken`（env 成对 > credentials.json > error）/ `readSpec`。
- **\*\_test.go** — `cmd_test.go`：`edit --no-ui`（写 askdao-agent.yml + detection.json）/ `deploy 无 conductor URL 错出` / `deploy 含 diff 显示 before/after`；`deploy_test.go`（`httptest` 假 conductor）：e2e happy（验 multipart + skill zip）/ `409 kol_profile_required` → 引导去 askdao.ai/workspace + exit 1（不 retry、不调 /kol-profile PATCH）/ blocking-warning gating ±`--force`（mock REJECTED warning 触发 409）/ 缺 skill 目录 exit 1。`skill_package_test.go`（**v0.8.1 守 skill 打包真相源**）：`TestResolveSkillDir` 表驱动锁四分支（`~/`展开 / 绝对路径 / `Scope=="user"` 相对 / project 相对 join dir）+ `TestDeploy_UserScopeAbsolutePath` e2e 回归（yaml 含全局 skill 绝对路径 + `scope: user` → `runDeploy` 经 packageSkills 正确打包，验旧内联循环 `filepath.Join(dir, abs)` 拼错路径的 bug 已修）+ `TestPackageSkills_FrontmatterValidation`（frontmatter 缺 name / 缺 description / name 撞名的拒绝路径 + 完整 frontmatter 通过）。`captureStdout` 临时 redirect helper。

## 设计约束

- **stdlib only**：不引 cobra / kingpin。手写 router 简单稳定，二进制小。
- **命令收敛到 agent edit / deploy**（v0.8）：审阅/编辑/发布走 `agent edit`（本地 Web 工作台，go:embed 单页，见 [../../internal/webstudio/CLAUDE.md](../../internal/webstudio/CLAUDE.md)）；命令行直接发布走 `agent deploy`（手动 / CI / 手编 yaml 用户）。系统未上线，无向后兼容包袱，砍掉 init/show/detect/bundle 不保留。
- **KOL Profile 不在本地 CLI/工作台**：归 askdao.ai 云端 web（认证后未填则云端补）；`edit` 与 `deploy`（CLI）遇 `kol_profile_required` **统一引导去 https://askdao.ai/workspace**（profile tab），不再 prompt bio / 自动 PATCH —— KOL 在云端表单设 `kol_join_mode` 后重跑 deploy。
- **Conductor env / token 解析**：`ASKDAO_CONDUCTOR_URL` + `ASKDAO_CONDUCTOR_TOKEN`。edit 时 token 可选（无配置 → MockClient 离线生成草稿）；deploy 时必填，解析 env 成对（单设一个报错）> credentials.json > error。
- **deploy 发原始 askdao-agent.yml 字节**：不 yaml.Marshal 往返，保留 KOL 注释 / 字段顺序 / 未知字段（conductor `extra=ignore` forward-compat）。
- **error 退出码**：0 成功 / 1 业务错 / 2 用法错 / 3 conductor URL/token 未配置（deploy）。

## 端到端验证

`make build && ./askdao agent edit --dir <proj> --no-ui` → 扫描 + 写 `askdao-agent.yml`（含 `theme_color` 默认 token + `skills` 仅 project scope）+ `.askdao/{recommendation.yml,detection.json}`。

`./askdao agent edit --dir <proj>`（默认）→ 启 `127.0.0.1:随机端口` Web 工作台 + 开浏览器 → 审阅/编辑 profile/persona/主题色 + 按 scope 分组勾选 skill/MCP → Save / 一站式 Deploy。

`deploy` e2e：`ASKDAO_CONDUCTOR_URL=… ASKDAO_CONDUCTOR_TOKEN=… ./askdao agent deploy --dir <proj>` → 上传 skill zip → `agent_id` / `group link` / update-mode `Created`/`Updated`。

## 已知限制

- syft 不在 PATH 时 packages 列表空（软警告提示安装）。
- Codex user-scope skill（`~/.agents/skills`）与 MCP（`~/.codex/config.toml` TOML）均已接入；Cursor 非目标 harness 已移除。
- 远端 ID 不写回 `agent.yml` `status:`（P2）。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
