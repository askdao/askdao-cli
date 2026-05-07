# Design Review v0.5 · 2026-05-06

> 触发：哥指出 v0.4 yaml 字段非常丰富（200+ 行），让 KOL 直接确认这个文件
> 心智负担过大。问应该做摘要还是直接看 yaml。
>
> 第一轮提议：纯摘要卡片（35 行）—— 哥批评"过于摘要、模糊，不利于一些重要文件
> 或路径的确认"。
>
> 第二轮调整：**中等详情**（mid-density）—— 字段按"KOL 决策视角"分三档，
> 必列具体内容的就列，可归纳的归纳 + 入口扩展。
>
> Reviewer: Sam
> Decision: 中等详情 7 块结构；inline reasoning 用 `↳ Why:` 引导符；归 Phase 1
> Outcome: design.md 升 v0.5，加交互式 UX + 7 render 模块 + show 命令

---

## 1. 起源问题（哥的原话）

> "现在 agent.yml 文件的内容字段非常丰富。我们填好了让 KOL 去确认，
> 感觉对他的心理认知负担也是比较大的。"

v0.4 完整 yaml 现在 230+ 行，10+ 顶层块。让 KOL 直接审：

| 受众 | 问题 |
|-----|------|
| 普通 KOL（非工程师） | 不懂术语；30 分钟读不完；放弃 |
| 工程师 KOL | 能读懂但烦；要花 30 分钟才能审；和"10 分钟上线"承诺冲突 |

---

## 2. 第一轮提议被批评

第一版给的"纯摘要卡片"（35 行）：

```
🐍 Runtime
   • 28 Python packages from your project        ← 模糊
   • 3 system libs (libpq-dev, gcc, libjpeg-dev)

📦 Skills
   • Excel (Anthropic builtin)                   ← 太短
```

**哥批评的核心**：
> "一些关键的目录里面的文件、重要文件，还有重要的 Skill，
> 还是应该给出来具体的内容，这样好让这个 KOL 来进行确认。
> 总之，我觉得如果完全是摘要的、模糊的说明的话，不利于有
> 一些重要文件或路径的确认。"

**问题本质**：
- "28 Python packages" 是黑盒 → KOL 没法判断推荐器有没有出错
- KOL 要看到 fastapi/sqlalchemy 这种熟悉的依赖名字才能信任推荐
- 关键文件路径（persona.md / .claude/skills/* / Dockerfile / .env.example）必须可见，KOL 想验证 askdao-cli 扫到了正确的文件

---

## 3. 正确的设计原则 · 字段三档分类

不是"摘要 vs 全文"二选一，而是按 **KOL 的决策视角** 把字段分三档：

### 档 1 · 必列具体内容（KOL 必须能逐条看清）

| 字段 | 为什么必须列 |
|-----|-------------|
| **Skills 完整列表** | 每个 skill 影响 agent 能力；KOL 必须知道是 builtin 还是 custom + 来源路径 |
| **核心生产依赖（前 8-15 个）** | 看到 fastapi/sqlalchemy 这种熟悉名字才能信任推荐器 |
| **MCP Servers 完整列表** | name + url + 关联 token；漏掉一个就少一个能力 |
| **Vault credentials 完整列表** | 每个 secret 的用途和来源 —— 订阅者要填的东西 |
| **关键文件路径** | persona.md / .claude/skills/ / .env.example / Dockerfile —— KOL 想验证扫到了正确文件 |
| **Tool permission 的 override** | 哪几个工具被设 always_ask + 触发 evidence |
| **System packages（apt 等）** | 通常 < 10 个，全列也不多 |

### 档 2 · 列计数 + 入口（KOL 想看才看）

| 字段 | 处理方式 |
|-----|---------|
| 完整 28 个 Python 依赖 | 列前 8 个 + "and 20 more [press D]" |
| dev deps 被过滤的清单 | "14 dev deps filtered [press F]" |
| 网络白名单全部 | 列前 5 个 + "...and N more" |
| 完整 yaml | "[S] show full" 入口 |

### 档 3 · 展开 reasoning（KOL 需要理解 why）

| 字段 | 必带 reasoning |
|-----|--------------|
| Model 选择 | 为什么 opus 而不是 sonnet（含 confidence） |
| Tool always_ask override | 触发的 evidence 文件路径 |
| Skill 推荐 | 为什么推荐这个 builtin |
| Translation warning | 哪个字段被忽略 + 影响 + 替代方案 |

---

## 4. 中等详情卡片设计

### 4.1 7 块顶层结构（哥确认）

```
PERSONA      → 模型 / 角色 / persona.md / system_prompt
SKILLS       → 完整列表（builtin + custom_local + git_repo）
MCP SERVERS  → 完整列表（含被过滤的 stdio MCP 提示）
CAPABILITIES → tool permissions + override reasoning
RUNTIME      → 核心 deps + apt + networking + workdir
ONBOARDING   → 完整 vault credentials 列表
WARNINGS     → translation warnings 按 severity 分组
```

### 4.2 inline reasoning 风格（哥确认 ↳ Why: 引导符）

```
shell          ⚠️  ask_for_dangerous
               ↳ Why: .github/workflows/deploy.yml + production.toml
                      suggest production-touching code
```

### 4.3 "前 N 个" 默认值（哥确认）

| 字段 | 默认显示 | 入口键 |
|-----|---------|------|
| Python production deps | 前 8 个 | [D] view all |
| Python dev deps（已过滤） | 计数 | [F] view filtered |
| apt libs | 全列（< 10 个） | — |
| 网络白名单 | 前 5 个 | inline 显示 |
| 翻译警告 | high 全列 + medium/low 计数 | [W] view all |

### 4.4 完整示例

```
$ askdao agent init my-agent --auto

→ Scanning ./ ...
→ Calling LLM via conductor ...

✓ Generated draft for "my-agent"

═══════════════════════════════════════════════════════════════
 PERSONA
═══════════════════════════════════════════════════════════════

  Name           : my-agent
  Description    : Backend engineering assistant
  Model class    : high_reasoning
  Primary model  : Claude Opus 4.6 (standard speed)
                   ↳ Why: FastAPI + Alembic migration logic complex
                          Confidence: 0.78
  Fallback       : gpt-5.4, claude-sonnet-4-6
  Persona file   : persona.md  (empty — KOL to write)
  System prompt  : 380 chars  [P] view


═══════════════════════════════════════════════════════════════
 SKILLS  (2)
═══════════════════════════════════════════════════════════════

  ✓ xlsx  (Anthropic builtin)
    Why : detected pandas + openpyxl in your dependencies
    Conf: 0.85

  ✓ portfolio-analyzer  (custom local)
    Path: ./skills/portfolio-analyzer/SKILL.md  (4.2 KB)
    Will be uploaded to Anthropic on deploy.


═══════════════════════════════════════════════════════════════
 MCP SERVERS  (1 active, 1 filtered)
═══════════════════════════════════════════════════════════════

  ✓ github
    URL  : https://api.githubcopilot.com/mcp/
    Type : url (Anthropic compatible ✓)
    Token: GITHUB_TOKEN  (from .env.example)

  ⊘ filesystem  (filtered out)
    Type : stdio (not supported by Anthropic)
    [M] see filtered + alternatives


═══════════════════════════════════════════════════════════════
 CAPABILITIES (Tool permissions)
═══════════════════════════════════════════════════════════════

  shell          ⚠️  ask_for_dangerous
                 ↳ Why: .github/workflows/deploy.yml + production.toml
                        suggest production-touching code
  filesystem     ✓  allow (scopes: ./output, ./tmp)
  web            ✓  allow
  code_execution ✓  allow


═══════════════════════════════════════════════════════════════
 RUNTIME  (Anthropic Managed Agents environment)
═══════════════════════════════════════════════════════════════

  Python production deps (28 total, 14 dev filtered):
    • fastapi==0.135.1          • alembic==1.18.4
    • sqlalchemy==2.0.48         • anthropic==0.97.0
    • asyncpg==0.31.0            • pydantic==2.12.5
    • uvicorn==0.36.0            • httpx==0.28.0
    ... and 20 more   [D] view all   [F] view 14 filtered

  System libs (apt, all 3):
    • libpq-dev   ↳ asyncpg/psycopg need postgres headers
    • gcc         ↳ Python C extensions compile
    • libjpeg-dev ↳ Pillow image processing

  Networking: limited
    Allowed hosts: api.anthropic.com, api.openai.com,
                   api.githubcopilot.com  (3 total)

  Workdir: /app


═══════════════════════════════════════════════════════════════
 SUBSCRIBER ONBOARDING  (Vault hints)
═══════════════════════════════════════════════════════════════

  Required (1):
    🔑 GITHUB_TOKEN
       Used by: MCP server "github"
       From   : .env.example
       Subscribers need to provide their own.

  Optional: none


═══════════════════════════════════════════════════════════════
 ⚠️  TRANSLATION WARNINGS  (Anthropic Managed Agents)
═══════════════════════════════════════════════════════════════

  HIGH (2):
    • workspace.base_image = "pytorch/pytorch:2.1.0-cuda12.1"
      → IGNORED. Anthropic uses fixed cloud image.
      → Phase 2 OpenAIAdapter supports custom image.

    • workspace.setup_commands (2 commands)
      → PARTIALLY IGNORED. apt/pip names extracted; raw commands lost.
      [W] see commands

  MEDIUM (1) · LOW (0)   [W] see all


═══════════════════════════════════════════════════════════════
 ACTIONS
═══════════════════════════════════════════════════════════════

  [A] Approve and deploy        [P] View persona / system prompt
  [E] Edit yaml in $EDITOR       [D] View all 28 Python deps
  [R] View full reasoning trace  [F] View filtered dev deps
  [S] Show full yaml in pager    [W] View all warnings
                                 [M] View filtered MCP

  [Q] Quit (saved as draft)

>
```

---

## 5. 屏幕空间估算

| 版本 | 行数 |
|------|------|
| v0.4 完整 yaml（当前） | ~230 行 |
| v0.5 第一版纯摘要 | ~35 行（被哥否决） |
| v0.5 中等详情卡片 | **~80-90 行** ← 选定 |

虽然比纯摘要长一倍多，但：
- 信息密度合理（每行都是 KOL 该看的）
- 仍然 < 一屏半（终端 24 行 × 2-3 屏滚动）
- KOL 1-2 分钟扫完 vs 读完整 yaml 15 分钟

---

## 6. v0.5 工程量增量（askdao-cli 端）

| 模块 | 内容 | 估算 |
|-----|------|------|
| **🆕 `internal/render/summary.go`** | 卡片渲染器（7 块各自的渲染 + box drawing + 截断策略） | 320 行 |
| **🆕 `internal/render/reasoning.go`** | inline reasoning（`↳ Why:` 引导符 + confidence 颜色） | 100 行 |
| **🆕 `internal/render/diff.go`** | yaml diff（`github.com/r3labs/diff`） | 100 行 |
| **🆕 `internal/render/warnings.go`** | translation warnings 渲染（severity 颜色分组） | 80 行 |
| **🆕 `internal/render/lists.go`** | 通用 "前 N + and M more" 渲染器 | 100 行 |
| **🆕 `cmd/askdao/show.go`** | show 命令实现（subcmd D/F/M/W/P 分发） | 120 行 |
| `cmd/askdao/init_auto.go` | 改交互式（[A/E/R/S/D/F/M/W/P/Q]） | +50 行 |
| `cmd/askdao/deploy.go` | 加 diff preview | +50 行 |
| askdao-cli v0.5 增量总计 | | **~870 行 Go** |

vs v0.4（~3410 行）：增量 ~870 行。  
askdao-cli 端 v0.5 总计：**~4280 行 Go**。

工期影响：仍在 3-4 周区间内（render 模块都是数据驱动 + Go 模板字符串，比 scanner / provider 移植轻）。

---

## 7. 决策记录

### 9.9 KOL 审阅 UX 采用「中等详情卡片 + 入口扩展」 ✅ 已定（v0.5）

- v0.4 让 KOL 看完整 yaml（230+ 行）心智负担过大
- 第一版纯摘要（35 行）丢失关键文件路径 / skill / dep 等具体内容，KOL 无法验证
- v0.5 决定：**中等详情卡片**（7 块顶层结构 / ~80-90 行）+ inline reasoning（`↳ Why:`）+ 入口扩展（D/F/M/W/P）
- 7 块结构（哥确认）：PERSONA / SKILLS / MCP / CAPABILITIES / RUNTIME / ONBOARDING / WARNINGS
- "前 N 个" 默认值（哥确认）：Python deps 前 8；apt < 10 全列；网络白名单前 5
- 实现要点：`internal/render/` 5 个新模块（summary / reasoning / diff / warnings / lists）+ `cmd/askdao/show.go`
- 工程量：+870 行 Go（在 askdao-cli 3410 → 4280 区间内）
- Phase 切分：归 Phase 1（哥确认；不拆 Phase 1.5）

---

## 8. 修订动作清单（落进 design.md）

- [x] 顶部 ChangeLog 加 v0.5 章节
- [x] §3 命令骨架：`init --auto` 改交互式 [A/E/R/S/D/F/M/W/P/Q]；`deploy` 加 diff preview；新增 `askdao agent show`
- [x] §6.1 askdao-cli 工程量加 7 个 render 模块（~870 行）+ 总计更新
- [x] §6.4 总览同步
- [x] §7 Phase 1 描述加 "交互式审阅 UX"
- [x] §9 决策记录加 9.9
- [x] §10 落地路径加：UX 模块归 Phase 1
- [x] 附录加 v0.5 review 引用

---

## 9. 后续待讨论项

1. **Persona file（persona.md）的初始化**：v0.5 显示 "empty — KOL to write"。是否要 `askdao agent init --auto` 时让 LLM 也生成一份 persona.md 草稿？还是留 KOL 自写？
2. **`askdao agent show` 输出在 pipe 场景**：摘要带 box drawing + 颜色不利于 grep / awk。是否加 `--no-color` / `--format=json` 选项？
3. **`[E] Edit` 后的 validate 失败处理**：KOL 改 yaml 改坏了怎么办？回到原推荐 vs 让 KOL 继续改？
4. **多语言**：摘要里现在是英文（match 开源对外）；中文 KOL 是否要 `--lang=zh` 选项？倾向：Phase 3 再做。
