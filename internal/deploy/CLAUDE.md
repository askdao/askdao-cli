# internal/deploy/
> L2 | 父级: ../../CLAUDE.md

`askdao agent deploy` 的 conductor 客户端层 —— 把 KOL 编辑好的 `agent.yml`（+ 可选 `detection.json` + 每个 `custom_local` skill 的目录 zip）以 `multipart/form-data` POST 到 conductor `POST /api/v1/cli/deploy`，并在需要时 PATCH KOL 资料。本目录只做「HTTP 客户端 + skill 目录打包」两件事；命令行编排（flag 解析 / 交互 prompt / 输出渲染）在 `cmd/askdao/deploy.go`。

## 成员清单

- **client.go** — `Client{BaseURL, HTTPClient, AuthToken}` + 两个方法：
  - `Deploy(ctx, DeployInput) (*DeployResponse, error)`：构 `multipart/form-data`（text field `agent_yaml`（必填，原始字节不 re-marshal）+ 可选 `detection`/`harness_id`/`force` + 每个 skill 一个 **file part**，field 名 = `DeployInput.SkillZips` 的 key = `custom_local` skill `path` 的 basename），带 `Authorization: Bearer`，POST `DefaultDeployPath`。409 经 `classifyConflict` 解析 FastAPI `{"detail":{...}}`：`reason=="kol_profile_required"` → `*ErrKolProfileRequired`；带 `translation_report` → `*ErrBlockingWarnings`；其它（含 `detail` 是 str）→ 退化成通用错误带 body。其它非 2xx → 通用错误带 status + 截断 body。
  - `SetupKol(ctx, KolProfilePatch) error`：PATCH `DefaultKolProfilePath`（JSON body），带 Bearer；非 2xx → 错误带 body。
  - 类型：`DeployInput`（`AgentYAML []byte` / `Detection []byte` / `HarnessID` / `Force` / `SkillZips map[string][]byte`）、`DeployResponse`（镜像 conductor `app.api.cli.DeployResponse`：`agent_id` / `anthropic_agent_id` / `anthropic_environment_id` / `group_id` / `group_link` / `skills []map[string]interface{}` / `translation_report`）、`TranslationReport` / `TranslationWarning`（镜像 `app.agents.adapters.TranslationReport`；`severity`/`action` 是**小写** enum value —— conductor `use_enum_values=True`）、`KolProfilePatch`（CLI 只发 `kol_join_mode` + 可选 `kol_bio` / `name` / `image`）、`KolProfileRequired`（409 `detail` 的 `reason`/`fields`/`hint`）。
  - 错误：`ErrKolProfileRequired{Detail KolProfileRequired}` / `ErrBlockingWarnings{Report TranslationReport}` —— 调用方用 `errors.As` 判别（指针 receiver 的 `Error()`）。
  - 常量：`DefaultDeployPath = "/api/v1/cli/deploy"`、`DefaultKolProfilePath = "/api/v1/users/me/kol-profile"`、`DefaultTimeout = 180 * time.Second`（skill sync + Anthropic environment/agent.create 都在一个同步请求里，比 recommend 的 90s 更宽）。
- **zip.go** — `ZipDir(srcDir, rootName string) ([]byte, error)`：`archive/zip` + `filepath.WalkDir` 递归打包 `srcDir`，每个文件 entry 名 = `rootName + "/" + ToSlash(rel)`（zip 内顶层目录 = `rootName/`，目录 entry 隐式由文件路径派生）；跳过 `.DS_Store`。形态对齐 conductor `app/skills/sync.py:_zip_files_for_managed` 期望的「单一顶层目录 + 内含 `SKILL.md`」。
- **\*\_test.go** — `client_test.go`：`Deploy` happy（`httptest.NewServer` 解析 multipart 验 `agent_yaml`/`harness_id`/`force` text field + `my-skill` file part 是合法 zip 含 `my-skill/SKILL.md`，回 `DeployResponse`）+ 409 两种 `detail`（kol_profile_required / blocking-warnings）经 `errors.As` 判别 + 409 str-detail 退化通用 + 非 2xx 带 body + 空 BaseURL + `SetupKol`（PATCH path + JSON body + Bearer）+ `SetupKol` 非 2xx。`zip_test.go`：`ZipDir` 打临时目录 → `archive/zip` 读回，断言 entry 名带 `rootName/` 前缀、内容一致、`.DS_Store` 被跳；缺目录报错。

## 设计约束

- **stdlib only**：`net/http` / `mime/multipart` / `encoding/json` / `archive/zip` / `errors` 等，不引第三方 HTTP / zip 库；与 `internal/recommender`（决策 9.1：HTTP 客户端域）依赖纪律一致，连 `internal/types` 都不 import —— 只收发 `[]byte`（`agent_yaml`）+ 通用 `DeployResponse`/map，yaml 解析与「转 `render.TranslationWarning`」由 `cmd` 层做。
- **`agent_yaml` 发原始字节**：`DeployInput.AgentYAML` 是 KOL 编辑后的 `agent.yml` 原文 bytes，**不**经 `yaml.Marshal` 往返 —— 保留注释 / 字段顺序，避免 Go struct 不认识的新字段被丢（conductor `spec.py` `extra="ignore"` forward-compat）。
- **skill file part 用 file field（带 filename）**：`mw.CreateFormFile(name, name+".zip")`（含 `Content-Disposition: ...; filename=...`）—— conductor 端 `request.form().get(skill_name)` 必须拿到 starlette `UploadFile`（有 `.read()`），text field 会被它判 `isinstance(str)` 报 400。
- **409 双义**：conductor `/cli/deploy` 对「KOL 资料未填」和「translation_report 有 HIGH warning」都返 409，靠 `detail.reason` 区分；`classifyConflict` 解析失败（`detail` 是 str 或其它形态）→ 返 nil 让 `Deploy` 退化成通用错误（body 原样带出）。FastAPI 标准 body 形态 `{"detail": ...}`。
- **timeout 180s**：deploy 是「sync skill 到 OV+Managed Skills + 建 Anthropic environment + agent」三步串行的同步请求；recommend 是单次 LLM 调用 90s 够，deploy 留 180s。

## 依赖

仅标准库 + （间接）conductor `/api/v1/cli/deploy` + `/api/v1/users/me/kol-profile` 的契约（镜像 conductor `app/api/cli.py:DeployResponse` / 409 `detail` 形态 + `app/agents/adapters/translation_report.py`）。无新增三方依赖。

## 字段输出对应

| 输出位置 | 函数 | 备注 |
|---|---|---|
| `agent deploy` 主请求 | `Client.Deploy` | multipart：`agent_yaml`(text) + `detection`/`harness_id`/`force`(text, 可选) + 每个 custom_local skill 一个 file part |
| `agent deploy` 触发的 KOL 资料补全 | `Client.SetupKol` | PATCH `/users/me/kol-profile`，CLI 固定发 `kol_join_mode="free"` + 可选 `kol_bio` |
| `custom_local` skill 上传内容 | `ZipDir` | `<dir>/skills/<basename(path)>/` → zip（顶层目录 = `<basename(path)>/`） |
| 部署结果（`agent_id` / `group_link` / `skills` / `translation_report`） | `DeployResponse` | `cmd` 层渲染到终端；`translation_report` 转 `render.TranslationWarning` 走 `RenderTranslationWarnings` |

## 与 conductor 的对齐点

- `DeployResponse` ↔ conductor `app/api/cli.py:DeployResponse`（同名字段一一对应）
- `TranslationReport` / `TranslationWarning` ↔ conductor `app/agents/adapters/translation_report.py`（`severity`/`action` 小写 enum value）
- 409 `kol_profile_required` `detail` ↔ conductor `cli.py:deploy_agent_spec` 第 ① 步
- 409 blocking-warnings `detail.translation_report` ↔ conductor `cli.py` `output.translation_report.has_blocking() and not force` 分支
- skill zip 格式 ↔ conductor `app/core/skill_format.py:validate_skill_zip`（zip 内有 `SKILL.md` + frontmatter `name:`）+ `app/skills/sync.py:_zip_files_for_managed`（单一顶层目录原样保留）

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
