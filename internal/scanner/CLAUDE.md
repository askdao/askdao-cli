# internal/scanner/
> L2 | 父级: ../../CLAUDE.md

L1-L2 流水线确定性扫描器：包扫描（syft 进程）+ 语言识别（enry）+ Dockerfile AST（buildkit parser）。三件套都返回 `internal/types.Detection` 的 sub-types，由上层 cmd 装配落 `.askdao/detection.json`。

## 成员清单

- **syft.go** — `ScanPackages(ctx, root, opts)` spawn `syft dir:<root> -o syft-json --quiet`（决策 9.2 模式 A）。`SyftRunner` 是可注入命令运行器，单测用 fake 喂 canned JSON。`DefaultSyftExcludes = ["./openviking/**"]` 来自 spike §3 实测（OpenViking submodule 1159 噪声 artifact）。所有包默认 `IsProd=true`，issue #3 的 manifest dev/prod parser 才会标 false。`mapSyftTypeToEcosystem` 把 syft type 字符串归一到 detection.json 的 ecosystem key（pip / npm / cargo / go / github_actions / ...）；未知 type 透传，避免静默丢信息。
- **enry.go** — `DetectLanguages(root, excludes)` 走 `filepath.WalkDir` + `go-enry/v2` 字节级语言识别。模拟 Linguist 的过滤：vendor / documentation / configuration / generated 全 skip；`Programming` + `Markup` 类语言入统，`Data`/`Prose` 排除。单文件最多读 16 KiB（对齐 Linguist 采样行为）。auto-skip 目录：`.git node_modules .venv venv __pycache__ dist build target .next .nuxt`。
- **dockerfile.go** — `ParseDockerfile(path)` 用 `moby/buildkit/frontend/dockerfile/parser` 出完整 AST（v0.4 升级，design.md §4）。多阶段：每个 FROM 起新 stage，BaseImage = 最后 FROM，FinalStageName = 最后 stage 的 AS（无 AS 则 nil）。RUN 抽取：split on `&&` → 按子片段 `apt(-get) install` / `pip install` 正则匹配 → 入 `ExtractedAptPackages` / `ExtractedPipPackages`；其余进 `ExtractedSetupCommands`，但 `apt-get update` / `rm -rf /var/lib/apt` / `rm -rf /var/cache` 视为 housekeeping 过滤。Anthropic warnings 触发条件：多 stage / USER / EXPOSE / ENTRYPOINT。
- **glob.go** — `compileGlobs` + `matchAny` 实现 syft 风格 `./openviking/**` 双星号前缀匹配；普通 glob 走 `path.Match`，同时尝试 basename 匹配。
- **\*\_test.go** — 三个 scanner 单测覆盖主路径：syft 用 fake runner（不依赖 PATH 上的 syft 二进制；同时含 integration 用 `exec.LookPath("syft")` t.Skip 跳过）；enry 在 t.TempDir 写多语言文件验证占比；dockerfile 含 multi_stage / single_stage / missing 三场景。
- **testdata/** — Dockerfile fixtures：`multi_stage.Dockerfile`（Node→Python builder/runner，含 USER / EXPOSE / apt+pip 提取 + git clone 走 setup_commands）；`single_stage.Dockerfile`（最小 ENV 空格形式 + 单 pip）。

## 设计约束

- **决策 9.2 模式 A**：syft 走 CLI 进程而非 import library；接口里通过 `SyftRunner` 注入 fake 让单测离线可跑。模式 B（library import）等 askdao-cli 安装包大小可接受度评估后再切。
- **buildkit ENV pair 形式**：`ENV K=V` 与 `ENV K V` 在 buildkit AST 都规范为 `Next` 链交替 key/value，最末挂 `=` 或空字符串作 terminator。`parseEnvArgs` 按 (k, v) 对扫，跳过空 / `=` key。原以 `=` in arg 区分两种形式的实现是错的（已修）。
- **dev/prod 边界**：本目录只产出 `IsProd=true` 的原始包列表；dev/prod 二次过滤的 manifest parser（pyproject.toml `[dependency-groups].dev` / `package.json devDependencies` / `Cargo.toml [dev-dependencies]`）属 issue #3 范围，会基于本输出二次标注。
- **隐私**：scanner 全本地跑，不读任何 `.env` / 不上传内容。`DetectedEnvFile` 只采 keys，由 issue #3 的 secrets scanner 填充，本目录不涉及。
- **错误容忍**：enry walk 遇 fs.Err / 权限错忽略单个文件；syft 进程错把 stderr 拼到 error 上下文便于诊断；Dockerfile 不存在返回 `&{Exists:false}`，nil error，让上层选择是否写入 detection.json。

## 依赖

- `github.com/anchore/syft` — 不 import，作为外部 CLI 进程调用（决策 9.2）
- `github.com/go-enry/go-enry/v2` v2.9.6 — 语言识别 + Linguist 过滤
- `github.com/moby/buildkit/frontend/dockerfile/parser` v0.29.0 — Dockerfile AST

## 字段来源映射（detection.json）

| Detection 字段 | 本目录函数 | 备注 |
|---|---|---|
| `detected_packages` | `ScanPackages` | map[ecosystem][]Package；IsProd 全 true，等 issue #3 重标 |
| `detected_languages` | `DetectLanguages` | 字节级聚合 + 百分比 round 到 2 位小数 |
| `detected_dockerfile` | `ParseDockerfile` | v0.4 全 AST + extracted_* + anthropic warnings |

## 后续 issue 挂载点

- issue #3 manifest parser 会读 `detected_manifests` 配合本目录 `detected_packages` 重标 dev/prod
- issue #4 providers 用 `detected_packages` + `detected_dockerfile.extracted_apt_packages` 反向推 inferred_apt
- issue #6 recommender 把这里所有产物拼成 detection.json 喂 LLM

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
