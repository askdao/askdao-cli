# CHANGELOG

> **Frozen as of 2026-09-05; no further entries.** See git log and pull requests for changes. This file is kept as a record of milestones before 2026-09-05.

历史变更与版本演进 footnote（时间倒序）。GEB 规则：L1/L2 CLAUDE.md 只述现状，变更史（版本号 / issue 号 / 演进经过）一律落本文件；更细的历史见 git log 与各 PR。

## 2026-09-04

- **deploy 回执落点改 Agent 主页**（issue #98）：服务端不再为新 agent 创建聊天群，`DeployResponse` 随之新增 `agent_url`（Agent 主页，恒非空）。CLI 终端与 studio 成功卡片一律用它作「Open …」落点（`deployOpenLink` 单源，`cmd/askdao/deploy.go`）；`group_id` / `group_link` 降为遗留位——只有更早部署的 agent 才带值，有值才打印、且在无 `agent_url` 时兜底，存量输出不变。studio 结果对话框按钮文案「Open group page」→「Open agent page」。`metadata.group_name` 服务端已不读，字段保留只为存量 yaml 仍可解析。

## 2026-08-30

- **B10 cron 求解器单源化**（issue #93 第二期）：studio.js 删 101 行手写 croniter 复刻（cronFieldSet/cronNextRuns/wallInstants 等，零测试裸奔），「Next:」预览与 <15min 费用警示改调 conductor `POST /cli/cron-preview`（经 webstudio 新增 `/api/cron-preview` 本地代理 + `OnCronPreview` 回调，CLI 与桌面都注入）；离线/未登录/非法 cron 隐藏预览行（describeCron/schedFromCron 纯映射保留本地）。static_assets_test 守护求解器符号不回潮。

- **B11 文档瘦身**（askdao-cloud /simplify 审查）：全部 CLAUDE.md 按 FATAL-005/006 规则精简；清除对已删代码（archetype / deployment_payload）的文档残留；本文件随本次瘦身新建。
- **P2 结构收敛**（PR #95，issue #93）：新增 `internal/deployflow`（deploy 装配单源，修桌面版四处漂移）+ `internal/browser`；studio.html 拆分为 html/css/js 三文件各自 go:embed；recommender HTTP 样板收敛为泛型 `doJSON[T]`/`fetchOr[T]`。
- **P1 纯删**（PR #94，issue #93）：删 `internal/render/{summary,payload,reasoning,lists}.go`（v0.8 命令精简后零调用残留，约 700 行）+ `scanner/{payload,archetype}.go` 及 `types` 的 `Archetype`/`DeploymentPayload`（服务端零消费）；修帮助文本与 flag 脱节（--bio 已不存在等）。

## 更早（摘要）

- **v0.8**：命令精简——detect / bundle / agent init / agent show 移除，审阅入口收敛为 `agent edit` 本地 Web 工作台；`--observe` 观测层（PreToolUse hook 预勾真实激活的 skill/MCP）；harness 感知双 scope 扫描（claude/codex/cowork）。
- **v0.7.1**：deploy update-mode——服务端按 (owner, name) 去重，同 name 重 deploy 原地更新（`Created`/`PreviousManagedVersion` 区分输出）。
- **v0.7**：产物布局扁平化（askdao-agent.yml 进项目根 + `.askdao/` 工具空间）；所有 custom skill 一律上传（删 SkillReferences 引用重装机制）。
- **Phase 1**（issue #1-8）：L1-L4 静态流水线（scanner/providers/pipeline/recommender/render）+ deploy 骨架交付。
