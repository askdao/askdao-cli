# Handoff B — 主题色跨仓贯通 + kol_profile 云端引导

> v0.8 第二步 · **线 B/D**（**跨三仓**：askdao-cli + askdao-cloud-conductor + askdao-ai-web）。
> 建议在 `askdao-cloud` 根目录开窗（能同时看到三个子仓）。
> 这是一份**自包含**的会话启动文件：全新窗口读完即可开工。

---

## 0. 项目背景（全新窗口必读）

**askdao-cli 是什么**：AskDAO 体系内唯一开源的子项目（信任锚点）。Go 1.26 单二进制 CLI，KOL 在本地项目目录跑一行命令 → 扫描技术栈 → 生成 Anthropic Managed Agents 配置 → 经 Conductor 部署。

**整体架构**（本线涉及全部三层）：
- **askdao-cli**（Go）— 本地工具，生成 `askdao-agent.yml`（harness-neutral schema `askdao.ai/v1`）。
- **askdao-cloud-conductor**（Python/FastAPI）— 云端中枢，`/cli/deploy` 接收 yaml + adapter 翻译成 Anthropic Managed Agent，建 Agent↔Group。仓库 `askdao-cloud-conductor/`。
- **askdao-ai-web**（Next.js）— 订阅者前端，Group 对话页 `/k/{kol}/g/{group}`。仓库 `askdao-ai-web/`。

**立足点（已锁定）**：场景 = Skills Pipeline；第一期 = Anthropic Managed Agents MVP。

**当前代码状态**：askdao-cli 在 `main`，第一步（`agent edit` Web 工作台）已 merge（#32 #33）。

**工作纪律（GEB 分形文档协议）**：改代码必须同步文档——L3 文件头、L2 目录 CLAUDE.md、必要时 L1，固定带 `[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md`。**改代码不同步文档 = 未完成。**

**验证习惯**：cli `go test ./... && make build`；conductor `pytest`；web 起页面看渲染。

**沟通**：中文沟通，英文代码，简洁直接，每次以"哥"开头。

---

## 1. 这条线是什么

收口第一步留下的两条"悬空尾巴"：

- **B 主题色**：第一步把 `metadata.category` / `theme_color` 写进了 cli schema，但下游 conductor 没镜像、askdao-ai-web 没渲染——**cli 存了下游没用，是个悬空承诺**。
- **D kol_profile 握手**：`agent edit` 的 OnDeploy 已引导"去 askdao.ai 补填"，但 `deploy.go` 的 CLI `runDeploy` 仍跑旧的 CLI prompt bio——**与"KOL profile 归云端"定调直接矛盾，半坏状态**。

做完这两项，第一步才算真正完整、对订阅者可见。

## 2. 目标

- **B**：KOL 在工作台选的品牌色，在订阅者 Group 页 `/k/{kol}/g/{group}` 真正渲染出来。
- **D**：CLI deploy 的 `kol_profile_required` 握手从"prompt bio"改为"引导去 askdao.ai 补填"。

## 3. 工作思路 — B 主题色（核心：token 三端对齐）

**真相源** = askdao-cli `internal/webstudio/theme.go` 的 `Palette`：8 个 token + `categoryDefault`（category→token 映射）+ `DefaultThemeForCategory`。

```
Palette: sunset #FF6B35 / ocean #2E86DE / forest #27AE60 / berry #8E44AD
         amber #F39C12 / teal #16A085 / rose #E84393 / slate #475569
```

**铁律：`theme_color` 存 token 名（如 "sunset"）不是 hex。三端共用同一张 token→hex 表，任何一端硬编码 hex 都是 bug。**

1. **conductor**（`app/agents/spec.py:33 class Metadata`）：镜像加 `category` + `theme_color` 可选字段（pydantic）。`/cli/deploy`（`app/api/cli.py`）持久化这俩字段。Agent→Group 时把 `theme_color` 写到 Group 实体（订阅者查 Group 拿色）。
2. **askdao-ai-web**（`src/app/[locale]/k/[kolId]/g/[groupId]/page.tsx`）：按 Group 的 `theme_color` token 渲染品牌色，对齐 Kami 的 `data-agent-tone`（设计 token 在 `src/config/style/theme.css`）。需要一张 token→hex 表（镜像 theme.go 的 Palette）。
3. **⚠️ 留给这个窗口和哥决策**：token→hex 表三端如何对齐——
   - 方案 a：各端各维护镜像 + CI diff 校验（对齐现有 AgentSpec schema 的 CI diff 模式）。
   - 方案 b：单一来源生成（如 cli 导出 JSON，conductor/web 引用）。
   - 倾向 a（简单、与现有 cli↔conductor schema 对齐约定一致），但请先和哥确认。

## 4. 工作思路 — D kol_profile（次要，跟随处理）

1. `cmd/askdao/deploy.go:189 setupKolProfile` 现在 = CLI prompt bio（`bufio` 读 stdin）+ PATCH `kol_join_mode=free`。
2. 改为：遇 `*deploy.ErrKolProfileRequired` 时**不再 prompt bio**，打印"请去 askdao.ai 补填 KOL 资料"引导（对齐 `edit` 的 `studioDeployError` 处理，见 `cmd/askdao/CLAUDE.md`）。
3. 依赖 askdao.ai 云端 KOL profile 表单就绪。若未就绪：D 先把 CLI 引导文案改好（指向 askdao.ai 具体页面），表单本身是 askdao-ai-web 侧另一件事，可拆 issue。

## 5. 关注要点 / 坑

- conductor Metadata 加字段走**版本演进协议**（见 `internal/types/CLAUDE.md`：conductor 镜像 pydantic + fixture + design.md §4/§5）。但 `category`/`theme_color` 是 omitempty forward-compat，**不 bump** `AgentSpecAPIVersion`。
- conductor 改动走 **long-running 分支 `feature/askdao-cli-integration`**（memory `feedback_branch_strategy_concurrent_devs`：多人并发仓库用 long-running feature branch + merge sync，别直接切短分支撞别人）。
- conductor v0.7 已删 `persona_file`（`spec.py:45-50` 注释说明），加字段时别碰 persona 逻辑。
- `theme_color` = token 名（非 hex），反复强调，三端一致。
- D 是"次要、跟随处理"，别喧宾夺主，主体是 B。

## 6. 复用点（file:line）

| 端 | 位置 | 用途 |
|---|------|------|
| askdao-cli | `internal/webstudio/theme.go:22 Palette` | token→hex 真相源 |
| askdao-cli | `internal/types/agent_spec.go:74 Category / :79 ThemeColor` | schema 字段（已存在） |
| askdao-cli | `cmd/askdao/deploy.go:189 setupKolProfile` | D 改造点 |
| conductor | `app/agents/spec.py:33 class Metadata` | 镜像加字段 |
| conductor | `app/api/cli.py`（deploy 端点）/ `app/api/groups.py create_group_with_owner` | 持久化 + Group 带色 |
| web | `src/app/[locale]/k/[kolId]/g/[groupId]/page.tsx` | 订阅者 Group 页渲染 |
| web | `src/config/style/theme.css`（`data-agent-tone`） | Kami 主题对齐 |

## 7. 验证

- cli: `go test ./...`
- conductor: `pytest`（Metadata round-trip + deploy 持久化 theme_color）
- web: 起页面看 theme 渲染
- **e2e**：deploy 一个带 `theme_color` 的 agent → 订阅者 Group 页显示对应品牌色

## 8. 分支 / 起步

- 分支：conductor `feature/askdao-cli-integration`；web 新分支；cli 新分支（D）。各自 PR。
- 起步：① 读 `theme.go`（token 表）+ conductor `spec.py` Metadata + web group 页现状 ② 和哥定 token→hex 三端对齐方案 ③ conductor 镜像字段 + 持久化 → web 渲染 → cli D 文案。

---

## 开窗 opening line（复制即用）

```
哥，我做主题色跨仓贯通 + kol_profile 云端引导这条线（v0.8 第二步 B/D，跨三仓）。
先读 askdao-cli/docs/handoff-B-theme-and-kolprofile.md，我在 askdao-cloud 根目录开的窗。
```
