# Handoff A — observe hook 预勾（`askdao agent edit --observe`）

> v0.8 第二步 · **线 A**（纯 askdao-cli，实验性能力，spike 已验证可行）。
> 这是一份**自包含**的会话启动文件：全新窗口读完本文件即可开工，不依赖任何历史上下文。
> 配套（按需读）：`docs/HANDOFF.md` v0.8 段。

---

## 0. 项目背景（全新窗口必读）

**askdao-cli 是什么**：AskDAO 在本机运行的开源部分（信任锚点）。Go 1.26 单二进制 CLI，KOL 在本地项目目录跑一行命令 → 扫描技术栈 → 生成 Anthropic Managed Agents 配置 → 经服务端部署。设计哲学：本地隐私（扫描不上传文件内容）、确定性优先（L1-L3 用工业库，LLM 只做最后推荐）、review-and-edit（审阅推荐草稿而非空白模板）。

**立足点（已锁定，不再调研）**：
- 场景 = **Skills Pipeline 为主**（产品目标用户）
- 第一期 = **Anthropic Managed Agents MVP**（固定 Ubuntu 22.04 云容器，预装齐全）

**当前代码状态**：
- 分支 `main`。第一步（v0.8 `agent edit` 本地 Web 工作台）**已 merge**（#32 #33）。
- 命令集 = `auth login/status/logout` + `agent edit` + `agent deploy`（init/show/detect/bundle 已砍）。
- 架构：`internal/{types,scanner,providers,pipeline,recommender,render,webstudio,deploy}` + `cmd/askdao`。

**工作纪律（GEB 分形文档协议）**：改代码必须同步文档——L3 文件头 INPUT/OUTPUT/POS、L2 目录 CLAUDE.md 成员清单、必要时 L1。L2/L3 固定带 `[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md`。**改代码不同步文档 = 任务未完成。**

**验证习惯**：`cd askdao-cli && go test ./... && make build`。UI 改动用 mock server + playwright 截图。

**沟通**：中文沟通，英文代码，简洁直接。

---

## 1. 这条线是什么

observe 重定向后**唯一未兑现的核心能力**。用 Claude Code hooks 观测 KOL 跑一次真实 session 时**实际激活了哪些 skill / 调用了哪些 MCP**，然后在 Web 工作台自动**预勾选**这些真正用到的项（替代第一步的"手动全勾"），从根上解决 skill 过度包含——一个 Agent 实际只用 `.claude/skills/` 下的一部分。

## 2. 目标

`askdao agent edit --observe` → 写临时 hook → 引导 KOL 跑一次代表性场景 → 收集激活的 skill/MCP → 工作台预勾 → 零残留清理。定位为"**在默认全勾之上做收窄建议**"的叠加层，不是唯一真相。

## 3. spike 结论（已验证，开工前必读全文）

读 `docs/investigations/observe-hook-skill-activation-spike.md`。一句话：**完全可行，直接捕获，无需间接推断。这条线该做，且成本很低。** 关键发现（官方文档 + 本机真实 transcript 双重实证）：

- **skill 激活就是一次工具调用**。`Skill` 是 Claude Code 一等内置工具，skill 名明文落在 `tool_input.skill`。实证：`{"name":"Skill","input":{"skill":"content-generator"}}`（正是 skill_pipeline 真实样例）。`PreToolUse(matcher:"Skill")` 直接拿到。
- **MCP tool 形态固定 `mcp__<server>__<tool>`**。实证 `mcp__askdao-voice__elevenlabs_text_to_speech`，正则一拆即得 server 名 `askdao-voice`，对齐 webstudio 现有 `MCPCandidate.Name` 预勾。
- **HTTP hook 直发 `127.0.0.1` 受官方支持**，且 `internal/webstudio` server **已存在**（第一步落地，注入式 handler）。加一个 `POST /api/observe` 端点即兼任 hook receiver，不必写文件轮询。

## 4. 工作思路（来自 spike "最省怎么做"）

1. webstudio 加 `POST /api/observe`（`server.go`），接收 hook 上报的激活项。
2. `agent edit --observe`（`edit.go` 加 flag）：写临时 `.claude/settings.local.json`，`PreToolUse` 匹配 `Skill` + MCP tool，hook command 用 `curl` 发 `127.0.0.1:<port>/api/observe`。
3. 引导 KOL 自然跑一次 Claude（代表性场景）。
4. server 收集激活 skill（`tool_input.skill`）+ MCP（`mcp__<server>__<tool>` 拆 server）。
5. 工作台预勾这些；**没跑到的不强制取消勾选**（observe 是叠加层）。
6. **零残留三件套**：append 带标记 + 整文件备份 + 启动时自检残留 + 进程侧 `defer` 清理。**不能只依赖 SessionEnd**——askdao-cli 自身被 kill 时 hook 不触发，必须进程 defer + 启动自检兜底。

## 5. 关注要点 / 坑

- **开工前唯一要补的一次实测**（spike R3）：带子代理（Subagent）的 session，确认子代理内的 skill/MCP 调用是否冒泡到主 `PreToolUse`。其余已被双重证据锁定。**先确认这个再大改。**
- 边界：KOL 手打 `/skillname` 斜杠命令绕过 `PreToolUse`（走 prompt 展开路径）。observe 引导本就是"自然跑"非手动点名，影响极小；要兜底再挂 `UserPromptExpansion`，MVP 不必。
- Plan B：transcript 离线解析（spike 实证就是用它跑出来的），留作 HTTP 路径出问题的兜底，**不作主路径**。
- webstudio 架构约束：回调注入解耦（`OnSave`/`OnDeploy` 由 cmd 注入，webstudio 不依赖 pipeline/deploy）。observe 端点要遵循同样约束。

## 6. 复用点（file:line）

- `internal/webstudio/server.go` — 加 `/api/observe`；现有 `buildMux` / `Serve` / 随机端口 `127.0.0.1:0`。
- `internal/webstudio/api.go` — `SkillCandidate` / `MCPCandidate` 的预勾态字段；现有默认勾选规则（project 全勾 / user opt-in）。
- `cmd/askdao/edit.go` — `runEdit` 加 `--observe` flag。

## 7. 验证

- `go test ./...`（新增 webstudio observe 端点测试 + 临时 hook 写入/清理测试）
- `make build`
- 零残留 e2e：`--observe` 跑完确认 `.claude/settings.local.json` 恢复原状（含已有 settings 的合并/还原）

## 8. 分支 / 起步

- 分支：`feature/observe-hook-preselect`（自取），PR → `main`，一 issue 一分支（DEV-FLOW）。
- 起步：① 读 spike 报告全文 ② 确认 R3 子代理冒泡实测 ③ 实现 `/api/observe` → `--observe` → 预勾。
- GEB：改 `server.go`/`api.go`/`edit.go` 同步 L3 头 + `internal/webstudio/CLAUDE.md` + `cmd/askdao/CLAUDE.md`。

---

## 开窗 opening line（复制即用）

```
做 askdao-cli 的 observe hook 预勾这条线（v0.8 第二步 A）。
先读 askdao-cli/docs/handoff-A-observe-hook.md（含背景+spike结论+工作思路），
再读 docs/investigations/observe-hook-skill-activation-spike.md，然后开始。
```
