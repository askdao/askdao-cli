# internal/scanner/
> L2 | 父级: ../../CLAUDE.md

L1-L3 流水线确定性扫描器。issue #2 起底（syft / enry / dockerfile），issue #3 补齐六位（dev_filter / runtimes / mcp_config / skills_dir / secrets_hint / harness_signals），v0.6 补部署清单两位（archetype / payload）。所有函数都返回 `internal/types.Detection` 的 sub-types，由上层 cmd 装配落 `.askdao/detection.json`。

## 成员清单

### Issue #2 — L1-L2 扫描底座

- **syft.go** — `ScanPackages(ctx, root, opts)` spawn `syft dir:<root> -o syft-json --quiet`（决策 9.2 模式 A）。`SyftRunner` 是可注入命令运行器，单测用 fake 喂 canned JSON。`DefaultSyftExcludes` 默认空（caller 经 `SyftOptions.Excludes` 传项目特定排除，如大体积 vendored submodule）。所有包默认 `IsProd=true`，由 `dev_filter.go` 二次重标。`mapSyftTypeToEcosystem` 把 syft type 字符串归一到 detection.json ecosystem key。
- **enry.go** — `DetectLanguages(root, excludes)` 走 `filepath.WalkDir` + `go-enry/v2`，模拟 Linguist 的 vendor / docs / config / generated 过滤；只统计 `Programming` + `Markup`。单文件读 16 KiB 上限。
- **dockerfile.go** — `ParseDockerfile(path)` 用 `moby/buildkit/frontend/dockerfile/parser` 出 v0.4 完整 AST。RUN 抽取走 `&&` 分片 + 正则识别 apt-get/pip install；其余进 `ExtractedSetupCommands`，apt-get update / rm -rf housekeeping 过滤。多 stage / USER / EXPOSE / ENTRYPOINT 触发 Anthropic 兼容警告。
- **glob.go** — `compileGlobs` + `matchAny` 实现 syft 风格 `./pattern/**` 双星号前缀匹配。

### Issue #3 — L2-L3 补充扫描

- **dev_filter.go** — `ApplyDevFilter(root, pkgs)` 原地 mutate syft 输出，把 manifest 主文件中标 dev/test 的包翻成 `IsProd=false`。pip 端覆盖 uv `[dependency-groups].{dev,test,tests,lint,type,typing,docs,bench}` + Poetry `[tool.poetry.group.dev.dependencies]` + PEP 621 `[project.optional-dependencies]` 三种 flavor。npm 端读 `devDependencies` + `optionalDependencies`。cargo 端读 `[dev-dependencies]` + `[build-dependencies]`。`normalizeDepName` 按 PEP 503 归一（pip 把 `_` / `.` 折成 `-`，所有 ecosystem 全 lowercase）。`parsePEP508Name` 把 `requests[security]>=2,<3 ; python_version<'3.10'` 抽到 `requests`。manifest 不存在 → 静默跳过该 ecosystem。
- **runtimes.go** — `DetectRuntimes(root)` 解析五类 pin 文件：pyproject.toml `[project].requires-python`（含 constraint 范围）+ `.python-version` fallback / `.nvmrc`（自动 strip `v` 前缀）/ go.mod 的 `go X.Y.Z` 行 / `rust-toolchain.toml` 的 `[toolchain].channel` + `rust-toolchain` 单行 fallback。`.tool-versions`（asdf）作为 fallback 补未识别的 runtime —— 同 kind 不重复。
- **mcp_config.go** — `DetectMCPConfigs(root, opts)` project scope 扫 `.mcp.json` + `.codex/config.toml`（标 `Scope=project`；`.cursor/mcp.json` 随 Cursor 移除，`claude_desktop_config.json` 归位到 cowork user scope）。**格式按扩展名分派（`parseMCPServers`）**：`.toml` → Codex `[mcp_servers.<id>]` 表（BurntSushi/toml；`url` 远程 / `command`+`args` 拼成 stdio Command；`bearer_token_env_var` 是鉴权管线不参与传输判定，不解码；无关顶层 key 忽略），其余 → 规范 JSON `{"mcpServers": {name: {type, url, command}}}` 形态；type 缺失时按 url/command 推断。`AnthropicCompatible`：**有 URL 且非 stdio 即可部署**——远程传输（Claude Code/Cowork 标 `http`/`sse`，Anthropic spec 称 `url`）一律归一为 `type=url` 判兼容；只有 stdio（本地子进程）不可部署、输出固定 warning（"... only supports remote (url) MCP servers; stdio ..."）。malformed JSON/TOML 静默跳过该源（不 fail 整个扫描）。**v0.8 双 scope**：user scope 由 `harness_scope.go` `activeHarnesses(root, opts.HomeDir)` 门控并按 `userPath.resolve(home)` 定位——root 有 `.claude/` 扫 `~/.claude.json`（`Harness=claude`）、root 有 `.codex/` 或 `.agents/` 扫 `~/.codex/config.toml`（`Harness=codex`，TOML）、装了 Cowork（`<app-data>/Claude/claude_desktop_config.json` 存在）扫之（`Harness=cowork`）。均标 `Scope=user`。`readMCPConfig` helper 复用解析逻辑。
- **skills_dir.go** — `DetectSkills(root, pkgs)` 扫四个候选目录（`SkillDirCandidates`：`.claude/skills` / `.agents/skills` / `skills` / `agents/skills`；`.cursor/skills` 随 Cursor 移除），凡 `<dir>/<name>/SKILL.md` 存在即一条 `kind=custom_local` skill；每条补 `ParseSkillFrontmatter`（**导出**：无 YAML dep，行级解析 `--- ... ---` 取 `name` / `description`；cmd/askdao 的 packageSkills deploy 前置校验共用同一解析器）、`dirSize`（整个 skill 子树字节数 + 文件数 → `BundleBytes` / `BundleFiles`）、以及 `LoadSkillsLock` 的关联：name 在 `skills-lock.json` 里 → `LockedSource`/`LockedHash` 非空 + `IsLocalOriginal=false`（vendored 外部依赖），否则 `IsLocalOriginal=true`（repo 原生）。`LoadSkillsLock(root)` 找 `skills-lock.json`（root + 各候选目录及父级），解析 `{version, skills:{name:{source,computedHash}}}` → name→`SkillsLockEntry` map（scanner-internal 类型，含 Name/Source/LockedHash 三字段），缺失/malformed 返回 nil（不 fail）。`hashFile` = SKILL.md 的 hex sha256（payload 层只记录不据此判 drift —— lockfile 工具的 computedHash 不是裸文件 hash，无法可靠复算）。然后跑 `inferBuiltinSkills`：依规则表反向推 builtin（pandas+openpyxl→xlsx / PyPDF2→pdf / pdfplumber→pdf / python-docx→docx），同 skill_id 去重保留高 confidence。返回的最后一条记录用 `ImpliedAnthropicSkills` union 字段（detection.go 的异构 list 形态）。**v0.8 签名加 `ScanScopeOpts` + 双 scope**：project scope 扫 `SkillDirCandidates`（标 `Scope=project`，`harnessForProjectDir` 推 Harness）；user scope 由 `harness_scope.go` `activeHarnesses(root, opts.HomeDir)` 门控、按 `userPath.resolve(home)` 扫各 harness 全局 skill（claude `~/.claude/skills`、codex `~/.agents/skills`；cowork 无 user skill）（标 `Scope=user`，Source 用绝对路径供 deploy 定位）。抽 `scanSkillBase` helper 复用枚举逻辑。user scope skill **不进** `BuildAgentSpecSkills` 默认 spec.Skills（仅作工作台 opt-in 候选，避免把 KOL 全局 skill 库自动打包）。
- **secrets_hint.go** — `DetectRequiredSecrets(root, mcpConfigs)` 读 `.env.example` / `.env.sample` / `.env.template`，**只采 key 不读 value**（`readEnvKeys` 内部丢弃等号右侧）。规则表分两层：精确后缀（`ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `GITHUB_TOKEN` / `DATABASE_URL` ...）+ 宽匹配子串（`_API_KEY` / `_TOKEN` / `_SECRET` / `PASSWORD` / `CREDENTIAL`）。MCP 反查：当 `GITHUB_TOKEN` + 名含 "github" 的 MCP server 同时存在 → 填 `UsedByGuess.MCPServer`。未匹配规则的 key 输出 `purpose_guess=types.UnknownSecretPurpose`（"Unknown — KOL should review and decide"）+ `required=false`，被 `recommender.BuildVaultHints` 据此排除出 vault_hints（配置参数非凭证）。多文件 dedupe 保留首次出现。
- **harness_signals.go** — `DetectHarnessSignals(opts)` 探查 HOME 目录下的 `~/.claude/` / `~/.codex/` / `~/.cursor/`（macOS 也试 `Library/Application Support/Cursor`）/ `~/.gemini/` 痕迹。`HarnessProbeOpts.HomeDir` 注入让单测用 t.TempDir 假 HOME。Claude Code 的 evidence 含 `~/.claude/skills/` 下 SKILL.md 计数。推荐顺序：claude_code → anthropic_managed_agents；codex → openai_agents_sdk（Phase 2 路径，目前 deploy 仍走 anthropic）；都没装 → 默认 anthropic_managed_agents。
- **harness_scope.go** — v0.8 harness 感知层，覆盖 **claude(CLI) / codex(CLI) / cowork(GUI app)** 三 harness。`ScanScopeOpts{HomeDir}` + `userPath{base,rel}` 路径模型（`baseHome`=`~/`，`baseAppData`=平台 app-data，由 `appDataDir(home)` 按 GOOS 解析：mac `Library/Application Support`、win `%APPDATA%`、linux `~/.config`）+ `harnessConventions` 表：**claude** marker `.claude` → `~/.claude/skills` + `~/.claude.json`；**codex** marker `.codex`+`.agents`（项目仅有 `.agents/skills` 也算 Codex workspace）→ user skill `~/.agents/skills`（官方路径），user MCP `~/.codex/config.toml`（TOML `[mcp_servers.<id>]`，由 `readMCPConfig` 扩展名分派解析）；**cowork**（Claude Desktop agentic 模式）无 project marker → user skill nil（ephemeral session 目录会被清理删除，见 anthropics/claude-code#31422；持久 skill 复用 `~/.claude/skills`），user MCP `<app-data>/Claude/claude_desktop_config.json`（JSON `mcpServers` 同形）。**Cursor 已彻底移除**（非目标 harness）。`activeHarnesses(root, home)` 双激活（`active(root,home)`）：CLI 按 project marker 门控、GUI app 按 config 是否存在自洽门控；`harnessForProjectDir(rel)` 把 `.claude`/`.agents`/`.codex` 前缀映射到 harness 标签。新增 `appDataDir`/`hasPathPrefix`，复用 `dirExists`(harness_signals.go)/`fileExists`(runtimes.go)。被 skills_dir.go / mcp_config.go 消费。

### v0.6 — 项目原型 + 部署清单（确定性，零 LLM）


## 设计约束

- **决策 9.2 模式 A**：syft 走 CLI 进程而非 import library；`SyftRunner` 注入让单测离线可跑。
- **buildkit ENV pair 形式**：`ENV K=V` 与 `ENV K V` 在 buildkit AST 都规范为 `Next` 链交替 key/value，末挂 `=` 或空 token。`parseEnvArgs` 按 (k, v) 对扫，跳过空 / `=` key。
- **dev/prod 边界**：`syft.go` 产出原始包列表（IsProd=true），`dev_filter.go` 在装配 detection.json 时按 manifest 重标。两步分离让"扫一次包"和"读一次 manifest"逻辑解耦，便于单测。
- **隐私**：scanner 全本地跑。`secrets_hint.go` 只读 `.env.example` / `.env.sample` / `.env.template`，绝不读真 `.env`；只采 key 不读 value。
- **错误容忍**：enry walk 遇 fs.Err / 权限错忽略单文件；syft 进程错把 stderr 拼到 error 上下文；Dockerfile 不存在返回 `&{Exists:false}` + nil error；malformed `.mcp.json` 跳过该源不 fail 整个扫描；缺 manifest 不 fail；缺 HOME 才 fail。
- **PEP 503 归一**：pip dep name 比对全走 `normalizeDepName("pip", x)`：lowercase + `_` → `-` + `.` → `-`。其他 ecosystem 仅 lowercase。

## 依赖

- `github.com/anchore/syft` — 不 import，外部 CLI 进程调用
- `github.com/go-enry/go-enry/v2` v2.9.6 — 语言识别
- `github.com/moby/buildkit/frontend/dockerfile/parser` v0.29.0 — Dockerfile AST
- `github.com/BurntSushi/toml` v1.6.0 — pyproject.toml / Cargo.toml / rust-toolchain.toml 解析

## 字段来源映射（detection.json）

| Detection 字段 | 函数 | 备注 |
|---|---|---|
| `detected_packages` | `ScanPackages` + `ApplyDevFilter` | syft 出原始 list；dev_filter 翻 IsProd |
| `detected_languages` | `DetectLanguages` | 字节级聚合 + 百分比 round 2 位小数 |
| `detected_dockerfile` | `ParseDockerfile` | v0.4 全 AST + extracted_* + warnings |
| `detected_runtimes` | `DetectRuntimes` | 五类 pin 文件 + .tool-versions fallback |
| `detected_mcp_configs` | `DetectMCPConfigs` | project `.mcp.json`+`.codex/config.toml` + user scope(claude `~/.claude.json` / codex `~/.codex/config.toml` TOML / cowork app-data) + 远程(url/http/sse)→url 归一兼容标记 |
| `detected_skills` | `DetectSkills` | 五候选目录 + frontmatter description + bundle 体积 + lockfile 关联 + builtin 反向推 |
| `detected_required_secrets` | `DetectRequiredSecrets` | env sample only；MCP 反查 cross-link |
| `detected_harness_signals` | `DetectHarnessSignals` | HOME 探查 + 推荐 harness |
| `archetype` | `InferArchetype` | code_app / skill_pipeline / mixed / unknown + confidence + evidence |
| `deployment_payload` | `DetectDeploymentPayload` | includes / excludes / skill_references / 总体积 / ignore_sources（builtin + 各 ignore 文件）|

## 后续 issue 挂载点

- issue #4 providers 用 `detected_packages` (prod-only) + `detected_dockerfile.extracted_apt_packages` 反向推 `inferred_apt_packages`
- issue #6 recommender 把这里所有产物拼成 detection.json 喂 LLM
- issue #8 `cmd/askdao detect` 做装配编排：parallel 调本目录所有函数 → 合成 Detection struct → JSON 落盘

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
