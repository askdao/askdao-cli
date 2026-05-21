# docs/investigations/
> L2 | 父级: ../CLAUDE.md

调研报告。两类：① **实现底座验证** —— 外部技术/开源项目能否作 askdao-cli 实现依赖；② **Observe 层观测调研** —— v0.8+ 运行时观测驱动 spec 生成的可行性与开源生态，支撑 [`../design-new.md`](../design-new.md)。

## 成员清单

### 实现底座验证

- **syft-spike-for-askdao-cli.md** — anchore/syft 1.44.0 在 conductor 仓库实测：核心 Python/npm/cargo 依赖识别准确，单次扫描 < 5 秒；唯一 gap 是 dev/prod 不区分需二次过滤层。**结论：直接采用为 L1-L2 包扫描器**。
- **nixpacks-provider-pattern.md** — 通读 railwayapp/nixpacks Provider trait + python.rs / node.rs，抽出 Go 移植抽象（Provider 接口 4 方法 + App/Env 抽象 5 方法），重点摘录"依赖→系统包"反向映射表（psycopg→libpq-dev、puppeteer→12 个 apt 包等）。**结论：移植 4 个核心 provider（python/node/go/rust），~1000 行 Go**。

### Observe 层观测调研（design-new.md 前置 · 互为 A/B/C 三视角）

- **Runtime Observability and Session Hooking for AI Coding Agents_ An Engineering Assessment for askdao-cli.md** — 报告 A（工程评估 · EN）：Claude Code 29 个 lifecycle hook 是 observe→spec 的最佳原语（`PreToolUse`/`PostToolUse` Bash matcher 拿到 verbatim command + exit_code + output，可确定性捕获每次 apt/pip/npm install 与缺库报错）；Codex hook 覆盖更弱且 OTEL 故意省命令内容；"观测数据→部署 spec" 是无人做过的空白地带，SlimToolkit/DockerSlim 是唯一容器级先例。**结论：v0.8+ 保留静态 4 层作 cold-start baseline + 加 Claude Code Hooks Recorder**。
- **Open Source and Native Instrumentation for Agent Session Observability.md** — 报告 B（开源生态 · EN）：8 个项目对比（AI Observer / llm-cli-telemetry / claude_telemetry / RyanTech00 / OTel Collector Contrib / SigNoz / Langfuse），核心区分"自身捕获 agent 语义" vs "仅作遥测目的地"。**推荐混合架构：native hooks/OTel（语义事件）+ 轻量 snapshot 层（包/env/git 状态）+ 严格环境补 eBPF/auditd（宿主真相）**。
- **面向 Claude Code 与 Codex 的 Agent Session 审计与观测研究报告.md** — 报告 C（审计观测 · ZH）：三层可见性框架（上下文层 / 执行层 / 运行时内部层）论证为何纯扫描不够；Claude Code（29 hook events + HTTP hook + OTEL tool_details）官方能力远优于 Codex（6 events + OTEL 省命令）；纯本地扫描只适合作 SessionStart 基线，真实依赖与运行时来源须补进程级或语言级 runtime instrumentation。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
