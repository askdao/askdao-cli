# Observe Spike：Claude Code hooks 能否捕获「skill 激活 / MCP tool 调用」

> 任务：验证 `askdao agent edit --observe` 的核心未知 —— Claude Code 的 hooks 系统能否观测 KOL 真实 session 里**实际激活了哪些 skill / 调用了哪些 MCP tool**，用于在 webstudio 工作台「预勾选」真正用到的项，替代「手动全勾」。
> 范围：纯调研，无生产代码改动。
> 日期：2026-05-21 · 验证基准：Claude Code v2.1.x line（官方 hooks reference + 本机真实 transcript 一手实证）

---

## 1. 可行性结论

**完全可行，且是直接捕获，无需间接推断。** 这条线（plan §M4c `--observe` 预勾）该做，且实现成本很低。

三个支撑点，均有官方文档 + 本机一手实证双重确认：

1. **`Skill` 是 Claude Code 的一等内置工具**，与 `Bash`/`Edit`/`Write` 并列。skill 激活时会产生一次 `tool_name == "Skill"` 的工具调用，`PreToolUse` / `PostToolUse` 可用 `matcher: "Skill"` 直接捕获，且 **skill 名明文落在 `tool_input.skill` 字段**。
2. **MCP tool 调用的 `tool_name` 形态固定为 `mcp__<server>__<tool>`**，正则一拆即得 server 名与 tool 名。
3. **HTTP hook 支持指向 `http://127.0.0.1:PORT`**，且现有 `internal/webstudio` server 已具备注入式 handler 架构，加一个 hook receiver 端点即可兼任接收器。

唯一边界（非阻塞）：KOL **手打 `/skillname` 斜杠命令**会绕过 `PreToolUse`（这是 prompt 展开路径，不是工具调用路径）。对 askdao 场景影响极小 —— 详见 §3 与 §7。

一手实证（本机 `~/.claude/projects/<project>/*.jsonl`，正是 skill_pipeline 真实样例）：

```json
{"type":"tool_use","name":"Skill","input":{"skill":"content-generator"}}
{"type":"tool_use","name":"Skill","input":{"skill":"browse","args":"Open file://..."}}
{"type":"tool_use","name":"mcp__askdao-voice__elevenlabs_text_to_speech","input":{"text":...,"voice_id":...}}
{"type":"tool_use","name":"mcp__askdao-voice__elevenlabs_search_voices","input":{}}
```

即：`content-generator`（KOL 自定义 skill）、`browse`（内置 skill）、`askdao-voice`（MCP server）三类全部以结构化、可识别的形态出现在工具调用流里。这正是 webstudio 要预勾的全部对象类型。

---

## 2. Claude Code hook 事件类型清单

来源：官方 [Hooks reference](https://code.claude.com/docs/en/hooks)（code.claude.com/docs/en/hooks）。完整 30+ 事件，下表只列与 observe-skill 相关的核心子集 + 关键边界事件。

| 事件 | 触发时机 | matcher 过滤维度 | payload 关键字段 | 能否区分 skill vs MCP vs 普通 tool |
|---|---|---|---|---|
| `PreToolUse` | 工具执行**前** | tool name（含 `Skill` / `mcp__*` / `Bash` 等） | `tool_name`、`tool_input`、`tool_use_id` + 公共字段 | **能**：`tool_name=="Skill"` → skill；`tool_name` 前缀 `mcp__` → MCP；其余为内置 tool |
| `PostToolUse` | 工具执行**成功后** | tool name | `tool_name`、`tool_input`、`tool_response` + 公共字段 | 同上 |
| `PostToolUseFailure` | 工具执行**失败后** | tool name | + `error`、`is_interrupt`、`duration_ms` | 同上（含失败的 skill/MCP 调用） |
| `UserPromptSubmit` | 用户提交 prompt、处理前 | 无 | prompt 文本 + 公共字段 | 用于标记「代表性场景」回合边界 |
| `UserPromptExpansion` | 斜杠命令展开时 | 命令名 | 展开的命令 | **`/skillname` 走这里**，不是 PreToolUse（见 §3 边界） |
| `ConfigChange` | 配置文件变化 | 含 `skills` 维度 | 变化类型 | skill **配置** 层面事件，非激活事件（无用，见 §3） |
| `SessionStart` | 会话开始/恢复 | `startup`/`resume`/`clear`/`compact` | `source`、`model`、`cwd`、`transcript_path` | 用于盖基线 + 拿 transcript_path |
| `SessionEnd` | 会话终止 | 终止原因 | 终止原因 + 公共字段 | **无 decision control，纯 side-effect** → 清理 hook 的天然落点（§6） |
| `Stop` | Claude 一轮回答结束 | 无 | 公共字段 | 可作「一轮观测结束、flush 预勾」信号 |
| `SubagentStart`/`SubagentStop` | 子代理生命周期 | agent type | agent type | 子代理内的 skill/MCP 调用是否冒泡到主 hook 需实测（§7 风险） |

**公共字段**（所有事件 stdin JSON 都带）：`session_id`、`transcript_path`、`cwd`、`permission_mode`、`hook_event_name`。

**command-type hook 的 payload 投递方式**：完整 JSON 经 **stdin** 传给 hook 进程；hook 可向 stdout 返回结构化 JSON、向 stderr 写日志。`PreToolUse` 返回 exit code 2 可阻断调用；observe 是纯观测，**返回 0 不返回任何 JSON 即可放行**。

---

## 3. skill 激活捕获路径（直接捕获）

**可直接捕获，配置如下。**

skill 激活在机器相就是一次工具调用：Claude 决定用某 skill → 发起 `tool_name=="Skill"` 的 tool_use → `tool_input.skill` 是 skill 名（自定义或内置同形）。`PreToolUse(matcher:"Skill")` 在工具执行前即拿到该 payload。

配置（写进临时 `.claude/settings.local.json`）：

```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Skill",
        "hooks": [{ "type": "http", "url": "http://127.0.0.1:PORT/api/observe", "timeout": 10 }] }
    ]
  }
}
```

webstudio 收到的 payload 里取 `tool_input.skill` 即 skill 名，与 §1 实证一致：

```
PreToolUse → tool_name="Skill", tool_input={"skill":"content-generator"}
```

> 实证字段名以本机真实 transcript 为准：**`tool_input.skill`**（不是 `skill_name`）。官方 hooks doc 未公布 Skill 工具的 input schema，但 transcript 的 tool_use block 与 hook 的 `tool_input` 同源，字段一致。实现时仍应做防御：`skill` 缺失则回落读 `name`/首个字符串值。

### 边界：`/skillname` 斜杠命令绕过 PreToolUse

官方原文："`PreToolUse` hook matching the `Skill` tool fires only when Claude calls the tool, but typing `/skillname` directly bypasses `PreToolUse`."

含义：
- **Claude 自主激活 skill（从 description 判断该用）→ 走 Skill 工具 → PreToolUse 捕获 ✅** —— 这是 askdao 关心的主路径（KOL 跑代表性场景，让 Claude 自然选 skill）。
- **KOL 手打 `/skillname` 强制激活 → 走 `UserPromptExpansion` 展开路径 → PreToolUse 不触发 ❌**。

对 askdao 场景的实际影响：**极小且可补救**。observe 的引导话术本就是「用代表性场景跑一次，让 Agent 自然工作」，不是「手动逐个 `/skill` 点名」。若要兜底，再挂一个 `UserPromptExpansion`（matcher = 命令名）hook 即可把斜杠激活也收进来 —— 但 MVP 不必做。

---

## 4. MCP tool 激活捕获路径（直接捕获）

**可直接捕获。** MCP tool 的 `tool_name` 官方固定形态 `mcp__<server>__<tool>`（例 `mcp__memory__create_entities`、`mcp__github__search_repositories`），matcher 支持正则。

配置：

```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "mcp__.*",
        "hooks": [{ "type": "http", "url": "http://127.0.0.1:PORT/api/observe", "timeout": 10 }] }
    ]
  }
}
```

webstudio 端从 `tool_name` 反推：`strings.SplitN(name[5:], "__", 2)` → `[server, tool]`。**server 名**就是要去与 `MCPCandidate.Name` 匹配并预勾的键。实证：`mcp__askdao-voice__elevenlabs_text_to_speech` → server=`askdao-voice`、tool=`elevenlabs_text_to_speech`，而 webstudio 现有 `MCPCandidate.Name` 正是 server 名 `askdao-voice`，可直接对齐预勾。

> 注意：一个 MCP server 通常暴露多个 tool。observe 只要观到 **任一** `mcp__<server>__*` 调用，就把该 server 整体预勾（webstudio 的勾选粒度是 server 不是 tool，见 `api.go MCPCandidate`）。

> 合并 matcher：`Skill` 与 `mcp__.*` 可写两条独立 PreToolUse 规则指向同一端点，receiver 按 `tool_name` 分流即可。

---

## 5. 数据回传机制（HTTP 直连，复用现有 webstudio server）

**用 HTTP hook 直发 localhost，不需要写临时文件 + 轮询。**

官方确认 HTTP hook 支持 localhost URL，config 形如：

```json
{ "type": "http", "url": "http://127.0.0.1:PORT/api/observe", "timeout": 30,
  "headers": { "Authorization": "Bearer $TOK" }, "allowedEnvVars": ["TOK"] }
```

hook 把事件 JSON 作为 POST body（`Content-Type: application/json`）发到该 URL；非 2xx / 连接失败 / 超时**均为非阻塞** —— receiver 崩了 Claude 也继续，只丢尾部观测。

落地路径（最省）：

1. webstudio `server.go` 已绑 `127.0.0.1:随机端口` 且有 `buildMux` 注入式 handler。`--observe` 时新增端点 `POST /api/observe`：
   ```go
   // 伪代码
   mux.HandleFunc("/api/observe", func(w, r) {
       var p struct{ ToolName string `json:"tool_name"`; ToolInput map[string]any `json:"tool_input"` }
       json.NewDecoder(r.Body).Decode(&p)
       switch {
       case p.ToolName == "Skill":         observed.AddSkill(p.ToolInput["skill"])
       case strings.HasPrefix(p.ToolName,"mcp__"): observed.AddMCPServer(serverOf(p.ToolName))
       }
       w.WriteHeader(200)
   })
   ```
2. observed 集合用并发安全的 `map[string]struct{}`（hook 可能并行 POST）。
3. KOL 跑完代表性场景（或工作台「停止观测」按钮）→ webstudio 把 observed 集合应用到 `SkillCandidate.Checked` / `MCPCandidate.Checked`：**观到的预勾、未观到的标灰**。

> 安全：observe 端口已是 127.0.0.1 本地随机端口；如需更严，hook config 带一个一次性 bearer token（`allowedEnvVars`）由 `--observe` 启动时注入环境，receiver 校验。MVP 可省。

> 关于 transcript 离线解析：HTTP hook 是实时上策；transcript（`~/.claude/projects/.../*.jsonl`）作为**离线兜底/对照**完全可行（§1 实证就是直接解析它拿到的），但 Claude 的 transcript 格式非稳定契约，不作主路径。见 §8。

---

## 6. 零残留方案（临时 settings.local.json 写入 + 清理）

`.claude/settings.local.json` 是 Claude Code 项目级 local 配置（gitignored，优先级高于 settings.json）。observe 的写入/恢复纪律：

**写入前**
1. 记录原始状态：文件不存在 → 记 `existed=false`；存在 → 读出原始字节 `original`（**整文件备份到内存或 `.askdao/settings.local.json.bak`**）。
2. 合并而非覆盖：若已存在 `hooks.PreToolUse`，**append** 我们的两条规则（带可识别标记，如 url 含约定路径 `/api/observe`），不动 KOL 原有 hook。

**清理时机：双保险**
- **正常**：观测结束（KOL 点「停止观测」或 webstudio session 退出）→ 移除我们 append 的规则；`existed=false` 则整删文件，否则写回 `original`。
- **兜底**：Claude session 自身的 `SessionEnd` hook（无 decision control，纯 side-effect，官方确认可靠触发）也可挂一个清理命令 —— 但更稳的是 askdao-cli 进程侧 `defer cleanup()` + 启动时检测残留（上次崩溃留下的标记规则）自动清。**不要只依赖 SessionEnd**：若 askdao-cli 自身被 kill，hook config 可能残留，故启动自检是必须的。

> 重要坑：hooks 在 **session 启动时快照**，运行中改 settings 不热重载。因此必须 **先写 settings.local.json，再 exec/引导启动 `claude`**。observe 的 `claude` 必须是 askdao-cli 写完配置后新拉起的 session，不能往已运行的 session 注入。

> 合并复杂度提醒：settings.local.json 可能已有 KOL 自己的 hooks/permissions。安全做法是只在 `hooks.PreToolUse` 数组里 append 带标记的对象、清理时按标记精确移除，**绝不整体重写**用户其他键。

---

## 7. 风险与坑

| # | 风险 | 影响 | 缓解 |
|---|---|---|---|
| R1 | `/skillname` 斜杠激活绕过 PreToolUse | 手动点名的 skill 漏勾 | 引导用「自然场景」而非手打 `/skill`；可选挂 `UserPromptExpansion` 兜底（MVP 不做） |
| R2 | `Skill` 的 `tool_input` 字段名官方未公布 schema | 取错 key → 拿不到 skill 名 | 实证为 `tool_input.skill`；实现做防御回落（缺失则读 name/首字符串） |
| R3 | 子代理（Subagent）内的 skill/MCP 调用是否冒泡到主 PreToolUse | 子代理用的 skill 可能漏 | **需实测**：若漏，补 `SubagentStart/Stop` 或解析 transcript 兜底 |
| R4 | hooks session 启动快照、不热重载 | 配置写晚了不生效 | 先写 settings.local.json 再起 claude（§6） |
| R5 | settings.local.json 已存在 KOL 自有 hook | 覆盖会毁用户配置 | append + 标记 + 精确移除，整文件备份（§6） |
| R6 | askdao-cli 自身崩溃 → hook config 残留 | 污染 KOL 项目，"零残留"破功 | 启动自检残留标记 + 自动清理，不只靠 SessionEnd |
| R7 | hook payload 字段随版本演进 | 未来 Claude 改 schema | pin 测过的 Claude 版本；receiver 容错解析 |
| R8 | 观测覆盖 = session 实际走过的路径 | 没跑到的 skill 不会被预勾 | 这是 observe 的本性（按 §M4b 默认仍全勾打底，observe 只做「收窄建议」叠加层，不做唯一真相） |
| R9 | `mcp__.*` matcher 也会捕获非目标 MCP（如 askdao 自己的 voice server） | 噪声 | receiver 只对齐 `MCPCandidate` 已发现的 server，未知 server 忽略 |

最关键的不确定项是 **R3（子代理冒泡）**：开工前用一个带子代理的 session 实测一次即可定论。其余均已通过官方文档 + 本机 transcript 双重确认。

---

## 8. 若不可行的替代方案（实际不需要，但留作 Plan B）

主路径已确认可行，本节仅为完整性。如果未来 HTTP hook 出问题或想离线分析：

**Plan B — transcript 离线解析**：`SessionStart` payload 给 `transcript_path`（`~/.claude/projects/<slug>/<session>.jsonl`）。observe 结束后直接解析该文件，遍历每行 `message.content[]` 里 `type=="tool_use"` 的 block，按 §1 同样的逻辑抽 `name=="Skill"` 的 `input.skill` 与 `name=~mcp__` 的 server。**本 spike 的 §1 实证就是用这个方法跑出来的**，所以可行性已被证明。代价：① transcript 格式非稳定契约，需容错；② 离线，丢失实时预勾的交互感。

**Plan C — command hook + 写文件**：若环境禁 HTTP hook，改 `type:"command"` 跑一个极小 shim（curl 回 localhost，或 append 一行到 `.askdao/observed.jsonl`），webstudio 轮询该文件。比 HTTP 多一层进程 + 文件 IO，无必要。

---

## 附：开工清单（直接可执行）

1. `cmd/askdao/edit.go` 加 `--observe` flag（或工作台内「运行实测」按钮）。
2. `webstudio/server.go` `buildMux` 加 `POST /api/observe`；新增并发安全 observed 集合 + `applyObserved(data)`。
3. 新增 `internal/observe/settings.go`：写/合并/清理临时 `.claude/settings.local.json`（带标记 append + 整文件备份 + 启动自检残留）。
4. `--observe` 流程：写 settings → 起 webstudio（拿 PORT）→ 把 PORT 注入 settings 的 hook url → 引导 KOL 起 `claude` 跑代表性场景 → 收集 → 预勾 → 清理（defer + SessionEnd 双保险）。
5. 开工前实测一次 **R3 子代理冒泡**，定 SubagentStart 是否要补。

---

## 出处

- 官方 Hooks reference：https://code.claude.com/docs/en/hooks （事件清单、`Skill` 工具、`mcp__<server>__<tool>` 形态、HTTP hook config、SessionEnd 无 decision control、stdin payload）
- 官方 Hooks guide：https://code.claude.com/docs/en/hooks-guide （MCP matcher 正则）
- 本机一手实证 transcript：`~/.claude/projects/<project>/<session>.jsonl`（真实 `Skill` + `mcp__askdao-voice__*` tool_use payload）
- 既有调研报告 A：`docs/investigations/Runtime Observability and Session Hooking for AI Coding Agents_ ...md`（hook 工程层 + HTTP hook + 非阻塞语义）
- 既有调研报告 C：`docs/investigations/面向 Claude Code 与 Codex 的 Agent Session 审计与观测研究报告.md`（公共字段、tool boundary 边界、transcript 非稳定契约）
- plan §M4c：`~/.claude/plans/anthropic-managed-pure-pixel.md`（预设 observe 预勾机制）
- 现有 webstudio：`internal/webstudio/server.go` + `api.go`（注入式 handler + SkillCandidate/MCPCandidate.Checked 预勾位）
