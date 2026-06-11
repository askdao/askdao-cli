# askdao-mcp 工具参考（面向 Skill 编写者）

> v1.0 | 2026-06-11 | 面向对象：为 Agent 编写 Skill 的 KOL/Builder
>
> askdao-mcp 是 AskDAO 平台的统一工具网关（`https://mcp.askdao.ai/mcp`），向你的 Agent 平铺暴露 **19 个工具、6 大能力域**。本地调试与线上部署用的是**同一个网关**：本机凭证由 `askdao auth login` 自动配置，线上凭证由平台在服务端注入——你的 Skill 不需要、也不应该关心任何鉴权细节。

---

## 能力总览

| 能力域 | 工具前缀 | 工具数 | 一句话 |
|--------|---------|-------|--------|
| 播客生成 | `listenhub_*` | 8 | 文本/网址 → 单人或双人对话播客（含两段式审稿流） |
| 语音合成 | `elevenlabs_*` | 4 | 单人精品配音，情感标签与停顿可控 |
| 文生图 | `google_*` / `openai_*` | 2 | 两种模型风格的图片生成，支持参考图保持角色一致 |
| 美股行情 | `market_*` | 2 | 实时报价 + 历史 OHLCV（Yahoo Finance） |
| SEC 财报 | `sec_*` | 3 | 美股公司档案、申报文件、年报关键财务（SEC EDGAR） |

所有工具的返回都是 **markdown 文本**（含产物 URL），错误也以可读文本返回（`isError` 标记），Agent 可以直接阅读并决定重试或降级。

---

## 一、播客生成（listenhub_*，8 个）

### 一次性生成（最常用）

| 工具 | 作用 | 关键参数 |
|------|------|---------|
| `listenhub_get_speakers` | 列出可用主播（ID/姓名/语言/性别/试听 URL） | `language`（默认 `zh`） |
| `listenhub_create_podcast` | 一步生成播客（文稿+音频），**自动轮询到完成**（可能数分钟） | `query` 或 `sources`（文本/URL 数组）、`speakers`（1-2 个，**用姓名即可**自动解析 ID）、`mode`、`language` |
| `listenhub_get_podcast_status` | 即时查询单集状态/音频 URL/文稿（不轮询） | `episodeId`（**完整 24 字符**） |

`mode` 三档（决定成片时长与风格）：`quick` 3-5 分钟（新闻速递/简介）/ `deep` 8-15 分钟（深度解析）/ `debate` 5-10 分钟（对谈/问答）。

### 两段式生成（需要审稿/改稿时用）

| 工具 | 作用 |
|------|------|
| `listenhub_create_podcast_text_only` | 第一段：只生成文稿（参数同 create_podcast，`language` 必填，`speakerIds` 同样支持姓名） |
| `listenhub_generate_podcast_audio` | 第二段：对已有文稿的单集生成音频；可传 `customScripts`（content+speakerId 数组）**整体覆盖**文稿——这是修改文稿的唯一途径，没有单独的改稿工具 |

### 朗读与配额

| 工具 | 作用 |
|------|------|
| `listenhub_create_flowspeech` | 文本/URL 直接转朗读音频；`mode: smart`（AI 润色修语法）/ `direct`（原文照读）；自动轮询到完成 |
| `listenhub_get_flowspeech_status` | 即时查询 FlowSpeech 状态 |
| `listenhub_get_user_subscription` | 查询 ListenHub 订阅与 credit 余量 |

**典型链路**：`get_speakers` → `create_podcast`（一次性）；或 `get_speakers` → `create_podcast_text_only` → 人工/Agent 审稿 → `generate_podcast_audio(customScripts)`（两段式）。

---

## 二、语音合成（elevenlabs_*，4 个）

| 工具 | 作用 | 关键参数 |
|------|------|---------|
| `elevenlabs_text_to_speech` | 单人语音合成，返回网关托管的**音频 URL（约 60 分钟有效）** | `text`（≤5000 字符）、`voice_id`（**必须**先经 search_voices 获取）、`model_id`、`voice_settings`、`previous_text`/`next_text`（跨段连读时保持韵律连贯） |
| `elevenlabs_search_voices` | 列出账号可用音色（voice_id/名称/语言/性别/试听） | 无参数 |
| `elevenlabs_list_models` | 列出 TTS 模型及能力（语言数/风格支持） | 无参数 |
| `elevenlabs_check_subscription` | 查询字符配额用量与重置日期 | 无参数 |

表现力控制要点：

- **文本内联标记**：`<break time="1.5s"/>` 控制停顿；`[laughs]` `[whispers]` `[sighs]` 等情感标签（**用 `eleven_v3` 模型**效果最佳）
- **`model_id` 选型**：`eleven_v3`（情感标签最强）/ `eleven_multilingual_v2`（默认，29 语种）/ `eleven_turbo_v2_5` / `eleven_flash_v2_5`（低延迟）
- **`voice_settings`**：`stability` 越低情感起伏越大、越高越平稳；`speed` 0.7-1.2

**与 ListenHub 的分工**：要"播客节目"（自动写稿、对谈结构）用 listenhub；要"精确控制每一句怎么读"（讲故事、配音、拼写朗读）用 elevenlabs。

---

## 三、文生图（2 个）

| 工具 | 模型 | 强项 | 关键参数 |
|------|------|------|---------|
| `google_generate_image` | Google Gemini 3.1 Flash Image (Nano Banana 2) | 照片级真实感、图内文字渲染、提示词还原度 | `prompt`（≤4000 字符）、`aspect_ratio`（1:1/16:9/9:16/21:9 等 14 种）、`image_size`（512/1K/2K/4K）、`reference_image` |
| `openai_generate_image` | OpenAI GPT Image 2 | 指令遵循、图内文字、构图；**透明背景**（logo/贴纸，需 png/webp） | `prompt`、`size`（1024 方/横/竖/auto）、`quality`、`background`、`output_format`、`reference_image` |

两个工具的返回都是**网关托管的图片 URL，约 10 分钟有效——生成后立即下载/保存**。

**角色一致性**：把上一次生成返回的 `image_url` 原样传入 `reference_image`（两个工具互通），可让同一角色贯穿多张图——典型用法是先生成"角色设定图"，后续每个场景都引用它。注意引用时原图必须仍在 10 分钟有效期内，多场景连续生成要一气呵成。

---

## 四、美股行情（market_*，2 个，只读、零密钥）

| 工具 | 作用 | 关键参数 |
|------|------|---------|
| `market_get_quote` | 实时报价：现价/日涨跌/日内区间/52 周高低/成交量/交易所 | `ticker`（如 `"NVDA"`） |
| `market_get_historical` | 历史 OHLCV：区间摘要（起止价/总回报/区间高低/均量）+ 近期 K 线 | `ticker`、`range`（1mo~max/ytd，默认 1y）、`interval`（1d/1wk/1mo） |

数据源 Yahoo Finance，有约 15 分钟延迟，**不可用于实盘交易决策类承诺**——Skill 文案里建议注明"行情仅供参考"。

---

## 五、SEC 财报（sec_*，3 个，只读、零密钥）

| 工具 | 作用 | 关键参数 |
|------|------|---------|
| `sec_get_company` | 公司档案：法定名/CIK/交易所/SIC 行业/财年截止/总部 | `ticker` |
| `sec_search_filings` | 检索申报文件：类型/日期/报告期/原文 URL | `ticker`、`form_type`（10-K/10-Q/8-K/S-1/DEF 14A，空=全部）、`count`（≤50） |
| `sec_get_financials` | 最新 10-K 关键财务：营收/毛利/营业利润/净利/资产负债/现金流，**各项含同比** | `ticker` |

仅覆盖**美股上市公司**。`sec_get_financials` 返回的是压缩摘要（刻意不返回 10-K 原文，避免撑爆上下文）；需要原文时用 `sec_search_filings` 拿到 URL 让 Agent 自行抓取阅读。

---

## Skill 编写注意事项

### 1. 产物 URL 是临时的——立即消费

图片 URL 约 **10 分钟**、音频 URL 约 **60 分钟**有效。Skill 流程必须设计成"**生成 → 立即下载/落盘/交付**"，绝不把 URL 当持久产物写进文件或留到流程末尾再用。`reference_image` 链式生成同理——多张图要连续生成，不能隔很久再引用。

### 2. 异步任务的两种形态，按需选

- `listenhub_create_podcast` / `create_flowspeech` **自带轮询**，调用会阻塞数分钟直到完成——Skill 里不需要再写轮询循环
- 两段式（`text_only` → `generate_podcast_audio`）适合**需要审稿**的场景；中间用 `get_*_status` 即时查询。`episodeId` 必须**完整复制 24 字符**，截断是最常见的失败原因

### 3. 先查资源，再做生成

语音/播客类工具依赖前置查询：`elevenlabs_text_to_speech` 的 `voice_id` 必须来自 `search_voices`；listenhub 的 speaker 推荐**直接用姓名**（工具会自动解析，比 ID 不易错）。Skill 步骤里把"查可用资源"写成第一步，不要硬编码 voice_id/speakerId——账号音色库会变。

### 4. 配额意识

生成类工具消耗第三方配额（ListenHub credit / ElevenLabs 字符数）。批量任务的 Skill 建议第一步先调 `*_subscription` / `check_subscription` 查余量，不足时提前告知用户而不是中途失败。

### 5. 错误是文本，不是异常

所有工具失败都返回可读的错误文本（如 `Tool xxx failed: ...`、`Validation error: ...`）。在 Skill 中明确指导 Agent：读错误文本判断原因——参数问题就修正重试，配额/服务问题就降级或告知用户，**不要无脑原样重试**。

### 6. 工具名直接写，凭证完全不管

Skill 指令中直接使用工具名（如 `listenhub_create_podcast`），本地与线上一致。**绝不在 Skill 中出现任何 token、API key 或网关 URL**——本地凭证由 `askdao auth login` 配置，线上由平台注入；Skill 里出现凭证既是安全隐患也根本不会生效。

### 7. SKILL.md 元数据决定 Skill 能否被触发

frontmatter 的 `name` 与 `description` 是部署硬校验（缺失会被 `askdao agent deploy` 拒绝），且 **`description` 是模型决定"何时激活这个 Skill"的唯一语义线索**——写清触发场景（"当用户要求生成播客/双人对谈音频时使用"），不要只写功能名词。

### 8. 数据类工具的边界写进 Skill

`market_*`/`sec_*` 只覆盖美股、行情有延迟、财务是年报摘要。如果你的 Skill 做投研类输出，把这些边界写进指令（如"数据来自 SEC 年报与 Yahoo 行情，仅供研究参考"），让 Agent 的输出自带免责语境。

### 9. 组合范式参考

- **内容转播客**：用户给文章/链接 → `get_speakers` → `create_podcast(mode=deep)` → 交付音频
- **审稿播客**：`text_only` → Agent 按用户风格改稿 → `generate_podcast_audio(customScripts)`
- **配音故事**：分段文本 → 逐段 `text_to_speech`（用 `previous_text`/`next_text` 保持韵律）→ 立即下载拼接
- **绘本/连环画**：先生成角色设定图 → 各场景 `reference_image` 引用 → 每张立即保存
- **个股速览**：`market_get_quote` + `sec_get_financials` + `sec_search_filings(8-K)` 拼一页摘要

---

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
