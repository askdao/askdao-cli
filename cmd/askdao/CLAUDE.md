# cmd/askdao/
> L2 | 父级: ../../CLAUDE.md

CLI 入口与四个用户可见命令的实装。这里只做参数解析 + IO + 调用 pipeline / render；业务逻辑都在 internal/。

## 成员清单

- **main.go** — subcommand router。无第三方 CLI 框架；用 stdlib `flag` + 手写 dispatch。一级命令 `detect / bundle / agent / version / help`；`agent` 二级 dispatch 到 `init / show / deploy`。
- **argparse.go** — `splitNameAndFlags(args)` helper：把 agent `<name>` 位置参数从 flag 流里挑出来。stdlib `flag.Parse` 在第一个非 flag 参数后停止解析，所以 `init my-agent --auto` 会丢 `--auto` —— 这个 helper 通过 `flagsWithValue` 白名单（`--from / --harness / --dir`）正确跳过 value 形式 flag，让位置参数任意位置都能识别。
- **detect.go** — `askdao detect [path] [--summary] [--pretty]`。默认 JSON 输出整个 Detection（含 v0.6 的 `archetype` + `deployment_payload`）；`--summary` 走 design.md §3.2 短摘要（Languages / Frameworks / Production deps / System pkgs / Harness signals）+ 末尾追加 `render.RenderPayload(..., full=false)` 的精简部署清单三行（archetype + N files X KB / M refs / K excluded + "run `askdao bundle`"）。
- **bundle.go** — `askdao bundle [path] [--json] [--warnings] [--no-evals] [--bundle-skill a,b]`。跑 `pipeline.Run`（LLM=nil）只看 `deployment_payload`：`render.RenderPayload(..., full=true)` 打 WILL UPLOAD（含每个目录的 immediate-children 摘要）/ SKILL REFERENCES / EXCLUDED 三段；`--json` 吐 `{archetype, deployment_payload}`；`--no-evals` → `Options.IncludeEvals=false`；`--bundle-skill` 逗号分隔 → `Options.ForceBundleSkills`（把 vendored skill 从引用改成随包上传）。**只预览不打包/不上传**（真上传等 conductor #11 deploy endpoint）。
- **init_auto.go** — `askdao agent init <name> [--auto] [--from path] [--harness id]`。
  - 无 `--auto`：写 plan/06 §4.2 空骨架
  - 有 `--auto`：跑 pipeline → 渲染中等详情卡片 → `interactiveLoop` 展示 [A/E/R/S/D/F/M/W/P/Q] 菜单。`A` / `Q` 写文件，前者标 approved 后者标 draft；其余分支输出对应详情后回菜单。所有写入：`<name>/agent.yml`（KOL 编辑副本）+ `<name>/.askdao/recommendation.yml`（冻结快照，deploy 用作 diff baseline）+ `<name>/.askdao/detection.json` + `<name>/persona.md`（已存在则不覆盖）+ `<name>/skills/`
  - LLM 选择走 `chooseLLMClient`：`ASKDAO_CONDUCTOR_URL` 环境变量在则用 `ConductorClient`，否则 `MockClient`。conductor #11 endpoint 落地后无需改代码，KOL set 一下 env 就切真实推荐。
- **show.go** — `askdao agent show <name> [--full|--reasoning|--warnings|--persona|--deps|--mcp]`。读 `<name>/agent.yml`，根据 flag 切五个聚焦视图或默认中等详情卡片。`--full` 直 pipe 原 yaml（cat-like 友好）。
- **deploy.go** — `askdao agent deploy [--harness id] [--dir path]`。读 agent.yml 与 recommendation.yml，跑 `render.DiffAgentSpec` 显示 KOL 改动 vs 原推荐。Phase 1 在 `ASKDAO_CONDUCTOR_URL` 没设时硬错（设有也只显示 diff，不真推 —— conductor #11 没有 deploy endpoint）。
- **\*\_test.go** — `argparse_test.go` 6 case 覆盖 splitNameAndFlags 主路径；`cmd_test.go` 跑端到端：`detect --summary` / `bundle 默认 + --json`（含 skill-lock 的 mini repo，断言 WILL UPLOAD/SKILL REFERENCES/EXCLUDED 段 + node_modules/output 被排除）/ `show 默认` / `show --full` / `show 缺 dir` / `deploy 无 conductor URL 错出` / `deploy 含 diff 显示 before/after`。`captureStdout` 是临时 redirect 的 helper，goroutine drain 防止 buffer 阻塞。

## 设计约束

- **stdlib only**：不引 cobra / kingpin / urfave/cli。手写 router 简单稳定，且让二进制小（~10MB Go binary 已足够 KOL 接受）。
- **agent 子命令统一前缀**：`agent init / show / deploy`。设计稿 §3 也是这个层级，KOL 心智模型对齐 git/kubectl 风格。
- **interactive only via --auto**：`init` 默认非交互（写空骨架就退出），仅 `--auto` 走 [A/E/R/S/D/F/M/W/P/Q] 菜单 —— 自动化流水线（CI / 脚本）能用 `init my-agent` 拿模板而不被卡 stdin。
- **Conductor URL 环境变量**：`ASKDAO_CONDUCTOR_URL` + `ASKDAO_CONDUCTOR_TOKEN`。无配置走 MockClient（开发期友好）。
- **deploy 是 stub**：明确告诉 KOL "Phase 1 deploy 是 stub" 而非装作能用 —— 比静默失败诚实。
- **error 退出码约定**：0 成功 / 1 业务错（找不到文件、yaml 解析挂等）/ 2 用法错（缺位置参数、flag 错）/ 3 conductor 端未就绪。

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

## 已知 Phase 1 限制（design.md §7 路线）

- `[E] edit yaml in $EDITOR` 暂未 wire（提示 KOL 用 `[A]` 退出后手编辑）
- `[S] full yaml in pager` 直接打印不分页（对 KOL 项目通常够；后续可加 less wrap）
- syft 不在 PATH 时 packages 列表空（warnings 提示安装）
- conductor #11 deploy endpoint 未上线，deploy 命令仅显示 diff

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
