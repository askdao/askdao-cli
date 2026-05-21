# observe-layer-design.md 立足点纠正 + v0.8 实施评审（2026-05-21）

> 配套 [`observe-layer-design.md`](./observe-layer-design.md) 的评审落档。记录该设计稿被纠偏的过程、锁定的立足点、吸收/作废清单，以及据此落地的 v0.8 第一步实现。

## 背景

`observe-layer-design.md`（原 design-new.md）是在**不了解 askdao-cli 实际代码**、且把场景**误判为通用 Code App** 的前提下提出的。经三轮 grill 纠偏。

## 立足点（哥锁定，不再调研）

- **场景 = Skills Pipeline 为主** —— 产品目标用户人群锁定。
- **第一期 = Anthropic Managed Agents MVP** —— 固定 Ubuntu 22.04 云容器，预装齐全。

## 决策清单

### 作废（误判 Code App 的产物）

| 项 | 处置 | 原因 |
|---|---|---|
| 四条观测路径（import 解析 / snapshot diff / tracer 注入 / 包安装命令解析） | **作废** | 服务"代码应用复现运行时"；skill_pipeline + 固定容器下无意义 |
| §6 重造 harness-neutral schema（identity/runtime/_provenance） | **作废** | 现有 `types.AgentSpec`（8 块）已是 harness-neutral 且生产验证；重造更弱且不兼容 conductor adapter |
| 语言包→系统包映射 / 错误反推表 | **作废** | 现有 `internal/providers` 已做（nixpacks 反向映射） |
| 引导 Agent 实跑"run dev server" | **作废** | 与低摩擦哲学冲突；最需要的项目恰恰最跑不起来 |

### 吸收（与包推断解耦、对 skill_pipeline 成立）

| 项 | 落地 |
|---|---|
| 本地 Web 工作台（HTTP server + go:embed 自包含 HTML） | `internal/webstudio/` + `askdao agent edit` |
| 字段级 provenance / 来源可解释性 | 复用现有 `types.Provenance`；工作台按 scope/origin 标 badge |
| 隐私规范（本地 127.0.0.1 / 只记名不记值） | webstudio 绑 127.0.0.1，数据不出本地 |
| **Skill 相关性筛选**（哥提出的真问题：静态扫描过度包含） | harness 感知 + project/user 双 scope 扫描 + 工作台按 scope 分组勾选（project 默认全勾 / user opt-in） |

### 新增（哥反馈衍生）

- **harness 感知扫描**：工作目录有 `.claude/` → 连带扫 `~/.claude/skills` + `~/.claude.json`；有 `.agent/`（Codex）→ 连带扫其 user 根目录（路径 Phase 2 核对）。
- **Agent 视觉品牌**：`metadata.category` + `metadata.theme_color`（预设色板 token），跨仓贯通订阅者端 Group 页。
- **命令精简**：砍 init/show/detect/bundle，核心收敛为 `agent edit`（Web 工作台）+ `agent deploy`。
- **KOL Profile 归 askdao.ai 云端**（本地工作台只管 Agent）；deploy 遇 `kol_profile_required` 引导去云端。

## 已实施（v0.8 第一步 · 本仓 askdao-cli）

- **M1** schema：`Metadata.Category`/`ThemeColor` + `Skill.Scope` + `DetectedSkill`/`DetectedMCPConfig` 的 `Scope`/`Harness`。
- **M4a** scanner：`internal/scanner/harness_scope.go` + `DetectSkills`/`DetectMCPConfigs` 双 scope；archetype 跳过 user scope；skills_builder 默认只产 project scope。
- **M2** webstudio：`server.go`/`api.go`/`theme.go`/`studio.html` + httptest 单测。
- **M3** cmd：`edit.go` + `common.go`；删 init/show/detect/bundle/argparse；main.go 路由 + help 精简。
- 全测试绿（`go test ./...`）+ `make build` 通过；`agent edit --no-ui` 端到端验证写出正确 yaml（skills 仅 project scope，user 全局 skill 留候选池）。

## 待续（第二步）

- Codex / Cursor 的 user-scope skill/MCP 路径核对（harness_scope.go TODO）。
- observe hook 预勾（`agent edit --observe`：Claude Code hooks 观测实际激活的 skill/MCP 自动预勾选）。
- 主题色跨仓贯通：conductor `spec.py` 镜像 + askdao-ai-web `/k/{kol}/g/{group}` 渲染。
- `kol_profile_required` 引导去 askdao.ai 的云端流程对接。
