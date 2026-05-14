# cmd/askdao/
> L2 | 父级: ../../CLAUDE.md

CLI 入口与用户命令（auth login/status/logout · detect · bundle · agent init/show/deploy）的实装。这里只做参数解析 + IO + 调用 pipeline / render / deploy / auth；业务逻辑都在 internal/。

## 成员清单

- **main.go** — subcommand router。无第三方 CLI 框架；用 stdlib `flag` + 手写 dispatch。一级命令 `auth / detect / bundle / agent / version / help`；`agent` 二级 dispatch 到 `init / show / deploy`；`auth` 二级 dispatch 到 `login / status / logout`。
- **auth.go** — `askdao auth login [--server url] [--name device-name] [--no-browser]` / `status` / `logout`。login 编排 OAuth 2.0 Device Code Flow（RFC 8628）：`auth.NewDeviceFlow(server, "askdao-cli/<ver> <os>/<arch>").Start` → 打印 `user_code` + 打开浏览器（`open` / `xdg-open` / `cmd /c start`）→ `PollUntilApproved` → `auth.Save(&Credentials{...})` 落 `~/.config/askdao/credentials.json`（0600）。server URL 解析 `--server` > `$ASKDAO_CONDUCTOR_URL` > `auth.DefaultServerURL`（compiled-in `https://api.askdao.ai`）。错误码映射到 UX hint（expired/denied/already_consumed）。`status` 输出 email + user_id + server + 创建时间 + 文件路径，无凭据 exit 1。`logout` 删本地，提示服务端仍生效。设计稿 [../../docs/cli-auth-device-flow.md](../../docs/cli-auth-device-flow.md) §6。
- **argparse.go** — `splitNameAndFlags(args)` helper：把 agent `<name>` 位置参数从 flag 流里挑出来。stdlib `flag.Parse` 在第一个非 flag 参数后停止解析，所以 `init my-agent --auto` 会丢 `--auto` —— 这个 helper 通过 `flagsWithValue` 白名单（`--from / --harness / --dir`）正确跳过 value 形式 flag，让位置参数任意位置都能识别。
- **detect.go** — `askdao detect [path] [--summary] [--pretty]`。默认 JSON 输出整个 Detection（含 v0.6 的 `archetype` + `deployment_payload`）；`--summary` 走 design.md §3.2 短摘要（Languages / Frameworks / Production deps / System pkgs / Harness signals）+ 末尾追加 `render.RenderPayload(..., full=false)` 的精简部署清单三行（archetype + N files X KB / M refs / K excluded + "run `askdao bundle`"）。
- **bundle.go** — `askdao bundle [path] [--json] [--warnings] [--no-evals] [--bundle-skill a,b]`。跑 `pipeline.Run`（LLM=nil）只看 `deployment_payload`：`render.RenderPayload(..., full=true)` 打 WILL UPLOAD（含每个目录的 immediate-children 摘要）/ SKILL REFERENCES / EXCLUDED 三段；`--json` 吐 `{archetype, deployment_payload}`；`--no-evals` → `Options.IncludeEvals=false`；`--bundle-skill` 逗号分隔 → `Options.ForceBundleSkills`（把 vendored skill 从引用改成随包上传）。**只预览不打包/不上传**（真上传等 conductor #11 deploy endpoint）。
- **init_auto.go** — `askdao agent init <name> [--auto] [--from path] [--harness id]`。
  - 无 `--auto`：写 plan/06 §4.2 空骨架
  - 有 `--auto`：跑 pipeline → 渲染中等详情卡片 → `interactiveLoop` 展示 [A/E/R/S/D/F/M/W/P/Q] 菜单。`A` / `Q` 写文件，前者标 approved 后者标 draft；其余分支输出对应详情后回菜单。所有写入：`<name>/agent.yml`（KOL 编辑副本）+ `<name>/.askdao/recommendation.yml`（冻结快照，deploy 用作 diff baseline）+ `<name>/.askdao/detection.json` + `<name>/persona.md`（已存在则不覆盖）+ `<name>/skills/`
  - LLM 选择走 `chooseLLMClient`：`ASKDAO_CONDUCTOR_URL` 环境变量在则用 `ConductorClient`（POST conductor `/api/v1/cli/recommend`，已上线），否则 `MockClient`（离线确定性 mock）。KOL set 一下 env 就切真实推荐，无需改代码。
- **show.go** — `askdao agent show <name> [--full|--reasoning|--warnings|--persona|--deps|--mcp]`。读 `<name>/agent.yml`，根据 flag 切五个聚焦视图或默认中等详情卡片。`--full` 直 pipe 原 yaml（cat-like 友好）。
- **deploy.go** — `askdao agent deploy [--dir path] [--harness id] [--force] [--bio text]`。读 `<dir>/agent.yml` **原始字节**（不 re-marshal）+ 解析；`.askdao/recommendation.yml` 存在则先跑 `render.DiffAgentSpec` 显示 KOL 改动 vs 原推荐（不存在则跳过）；每个 `type==custom_local` skill 把 `<dir>/skills/<basename(path)>/` 经 `internal/deploy.ZipDir` 打成 zip；经 `internal/deploy.Client.Deploy` 以 `multipart/form-data` POST conductor `/api/v1/cli/deploy`（`ASKDAO_CONDUCTOR_TOKEN` Bearer）。`409 kol_profile_required`（ADR-P21）→ `setupKolProfile`：打印 hint + 取 bio（`--bio` 或交互 prompt）+ `Client.SetupKol(kol_join_mode=free)` + 重跑一次；`ErrBlockingWarnings`（`translation_report` 有 HIGH）→ `render.RenderTranslationWarnings(ViewAll)` + `--force` 提示 + exit 1（带 `--force` 跳过 gating）。成功打印 `agent_id` / anthropic agent+environment id / `group_id` / `group link` / `Skills:`（含 `→ managed skill_…@ver (viking://…)`）+ 折叠的 `translation_report`（`ViewSummary`）。`ASKDAO_CONDUCTOR_URL` / `ASKDAO_CONDUCTOR_TOKEN` 没设 → stdout 提示 + exit 3。helper：`setupKolProfile` / `printDeployResult` / `formatSkillRef` / `toRenderWarnings`（conductor 小写 enum → `render.SeverityHigh/Medium/Low`）。
- **\*\_test.go** — `argparse_test.go` 6 case 覆盖 splitNameAndFlags 主路径；`cmd_test.go`：`detect --summary` / `bundle 默认 + --json`（含 skill-lock 的 mini repo，断言 WILL UPLOAD/SKILL REFERENCES/EXCLUDED 段 + node_modules/output 被排除）/ `show 默认` / `show --full` / `show 缺 dir` / `deploy 无 conductor URL 错出` / `deploy 含 diff 显示 before/after`；`deploy_test.go`（`httptest` 假 conductor）：`deploy 无 token` / e2e happy（验 multipart 字段 + skill file part 是合法 zip 含 `<name>/SKILL.md`）/ `409 kol_profile_required` → SetupKol → 重跑（断言 deploy 调 2 次、PATCH 1 次）/ HIGH-warning gating ±`--force` / 缺 skill 目录 exit 1。`captureStdout` / `captureStderr` 是临时 redirect 的 helper，goroutine drain 防止 buffer 阻塞。

## 设计约束

- **stdlib only**：不引 cobra / kingpin / urfave/cli。手写 router 简单稳定，且让二进制小（~10MB Go binary 已足够 KOL 接受）。
- **agent 子命令统一前缀**：`agent init / show / deploy`。设计稿 §3 也是这个层级，KOL 心智模型对齐 git/kubectl 风格。
- **interactive only via --auto / deploy 的 kol-profile prompt**：`init` 默认非交互（写空骨架就退出），仅 `--auto` 走 [A/E/R/S/D/F/M/W/P/Q] 菜单；`deploy` 仅在 `409 kol_profile_required` 且无 `--bio` 时 prompt 一行 bio（可空，非交互环境 EOF → 空 bio）—— 自动化流水线能用 `init my-agent` / `deploy --bio ""` 不被卡 stdin。
- **Conductor env**：`ASKDAO_CONDUCTOR_URL` + `ASKDAO_CONDUCTOR_TOKEN`。recommend 时 token 可选（无配置则 `init` 走 MockClient）；**deploy 时**：解析顺序 env 同时设置 → 用 env（CI 覆盖，**必须成对**：单设一个明确报错防误配置）→ 退回读 `credentials.json`（`askdao auth login` 的产物）→ 都没有则提示登录。conductor `/cli/deploy` 走 `get_current_user` 鉴权（接受 cli_* token 或 better-auth session）。
- **deploy 发原始 agent.yml 字节**：不 `yaml.Marshal(spec)` 往返 —— 保留 KOL 注释 / 字段顺序 / Go struct 未知字段（conductor `spec.py` `extra=ignore` forward-compat）；解析出的 spec 只用于枚举 `custom_local` skills + diff 预览。
- **error 退出码约定**：0 成功 / 1 业务错（找不到文件、yaml 解析挂、缺 skill 目录、conductor 返回错误等）/ 2 用法错（缺位置参数、flag 错）/ 3 conductor URL/token 未配置（deploy 时）。

## 端到端验证

`make build && ./askdao detect --summary .` 在本仓库实测输出（需 syft 在 PATH 时还会有 packages）：

```
Languages: Go 100% · Makefile 0%
Frameworks: (none detected)
Production deps: 0 pip / 0 npm
System pkgs: gcc pkg-config
Harness signals: claude-code ✓ · codex ✗ · cursor ✗ · gemini-cli ✗
```

`./askdao agent init smoke-agent --auto --from /tmp/demo`（在 demo 目录有 package.json + next 依赖）渲染完整 7 块卡片 + 写出 `smoke-agent/{agent.yml, persona.md, .askdao/{detection.json, recommendation.yml}}`。

`deploy` e2e：`ASKDAO_CONDUCTOR_URL=https://api.askdao.ai ASKDAO_CONDUCTOR_TOKEN=<token> ./askdao agent deploy --dir my-agent`（`my-agent/agent.yml` 含 `skills: [{type: custom_local, path: my-skill}]` + `my-agent/skills/my-skill/SKILL.md`）→ 若 `kol_join_mode IS NULL` 报 `kol_profile_required` → prompt bio（或 `--bio`）→ PATCH kol-profile → 重跑 → 上传 skill zip → 输出 `agent_id` / `group_id` / `group link` / `Skills: my-skill → managed skill_…@v0.1.0 (viking://…)`。

## 已知 Phase 1 限制（design.md §7 路线）

- `[E] edit yaml in $EDITOR` 暂未 wire（提示 KOL 用 `[A]` 退出后手编辑）
- `[S] full yaml in pager` 直接打印不分页（对 KOL 项目通常够；后续可加 less wrap）
- syft 不在 PATH 时 packages 列表空（warnings 提示安装）
- `agent show --warnings` 暂无数据可显示（translation_report 来自 `agent deploy` 的响应，未写回 agent.yml）
- `agent deploy` 不做幂等 re-deploy（conductor `/cli/deploy` 每次新建 agent + group；re-deploy `/diff` 走 ADR-P19，P2）；远端 ID 不写回 `agent.yml` `status:`（P2）

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
