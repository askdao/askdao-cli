# internal/providers/
> L2 | 父级: ../../CLAUDE.md

L3 启发式层 —— 移植 railwayapp/nixpacks 的 Provider trait 抽象 + python.rs / node/mod.rs / go.rs / rust.rs 的探测逻辑，外加 askdao-cli 自加的 dep→apt 反向映射表。issue #4 落 Python + Node 两个 P0 provider 与共享底座；issue #5 落 Go + Rust 两个长尾 provider，至此 Phase 1 计划的 4 个 P0 provider 全齐。

## 成员清单

- **provider.go** — `Provider` 接口（`Name / Detect / Plan / Metadata` 四方法，对齐 nixpacks `mod.rs:30`）+ `App` / `Env` 抽象。`App` 一次性 walk 项目根并缓存 file index（`shouldSkipProviderDir` 跳 `.git node_modules .venv venv __pycache__ dist build target .next .nuxt`），后续 `IncludesFile` / `FindFiles` / `FindMatch` 全 O(1) 或 O(n) 但无 IO。`Env` 把 OS environ + 用户覆盖合并。`HasDep(pkgs, ecosystem, name)` 是所有 provider 的 `uses_dep` 等价物，按 PEP 503 归一对比 prod-only 包。`FrameworkPlan` 是 nixpacks `BuildPlan` 的精简版 —— 不含 install/start phase（askdao-cli 输出声明式 `environment.packages`，不组 Dockerfile recipe）。
- **apt_map.go** — `InferAptPackages(pkgs)` 双表反向映射：`pythonAptMap`（移植 nixpacks python.rs:240-268 + askdao-cli 自加 Pillow / lxml / cryptography / pycurl / weasyprint）+ `npmAptMap`（puppeteer 12 个 apt deps、playwright 10 个、prisma / sharp / canvas）。仅 IsProd=true 的包参与；同 apt 包多 dep 触发时保留首条 reason。**Rust 的 cargoAptMap 不在这里 —— 放在 rust.go 里**（语言私表保持就近原则，避免本文件膨胀）。
- **python.go** — `PythonProvider` 实现 Provider 接口。`Detect` 触发于 `main.py / manage.py / requirements.txt / pyproject.toml / Pipfile / setup.py`。`SelectPackageManager` 走 nixpacks python.rs:81-99 的 lockfile 强信号优先级链（`uv.lock > poetry.lock > pdm.lock > Pipfile.lock|Pipfile > requirements.txt > pyproject.toml`），`PYTHON_PACKAGE_MANAGER` env var 一票覆盖。`pythonFrameworkRules` 走多源证据 + 加权打分（spike §6）：每条规则有 `probes` 列表 + `minScore` 阈值，`uses_dep` + `import_pattern` + `file_present` 三类信号组合。覆盖 FastAPI / Django / Flask / SQLAlchemy / Alembic / Pandas。`detectExternalServices` 反查 PostgreSQL / MySQL / Redis / Anthropic / OpenAI。
- **node.go** — `NodeProvider` 类似结构。`Detect` 看 `package.json`。`SelectPackageManager` 走 `bun.lockb > pnpm-lock.yaml > yarn.lock > npm 兜底`，`NODE_PACKAGE_MANAGER` env var 覆盖。`nodeFrameworkRules` 覆盖 Next.js / Nuxt / Express / NestJS / Vite / React。`hasNpmDep` 优先用 syft-derived `Pkgs` 列表（看到 lockfile 真相），缺失时退到直接读 `package.json` —— 让 provider 在没跑 syft 的场景也能工作。`detectExternalServices` 反查 pg / postgres / mysql2 / mysql / ioredis / redis / @anthropic-ai/sdk / openai。
- **go.go** — `GoProvider` 实现 Provider 接口。`Detect` 触发于 `go.mod` 或 `go.work`（workspace 也算 Go 项目）。Plan 用 `goDirectiveRe` 抽 `go X.Y.Z` 出 Runtime.Version；`cgoImportRe` 扫 `import "C"` 命中则 apt 加 `gcc + pkg-config`；`buildTagRe` 扫 `//go:build foo && bar` 留首个作为 evidence。`detectExternalServices` 直读 go.mod 文本匹配 `lib/pq` / `jackc/pgx` / `go-sql-driver/mysql` / `redis/go-redis` / `anthropics/anthropic-sdk-go`。Metadata 在 `go.work` 存在时记 `workspace=go.work`。**没有 PackageManager 概念**：Go 只有官方 toolchain，nixpacks go.rs 也没分流。
- **rust.go** — `RustProvider` 实现 Provider 接口。`Detect` 看 `Cargo.toml`。Runtime.Version 三级回退：`rust-toolchain.toml [toolchain].channel > rust-toolchain 单行 > Cargo.toml [package].rust-version`。`cargoAptMap` 是 Rust 私表，覆盖最常见的 `*-sys` crate（openssl-sys / pq-sys / mysqlclient-sys / libsqlite3-sys / zstd-sys / libxml / alsa-sys）→ 对应 apt headers。`inferRustAptPackages` 仅 IsProd=true 参与，同 apt 包去重保留首条 reason，自带 `sortInferred` 简单排序避免再多引入 `sort` import。`detectExternalServices` 反查 diesel / sqlx / tokio-postgres / redis crate。`Cargo.toml [workspace].members` 存在时记入 evidence。Metadata：`package_manager=cargo`，`toolchain_pinned=true`（当 rust-toolchain* 任一文件存在）。
- **\*\_test.go** — 接口 conformance 编译期断言（`var _ Provider = (*XxxProvider)(nil)`）+ App/Env 单测；Python 7 个 PM case + env override + FastAPI/Django plan + 空项目；Node 5 个 PM case + Next.js plan + puppeteer 12 apt + package.json fallback；Go runtime 抽取 + cgo 触发 + go.mod ext-svc 反查 + go.work workspace + rust-only 反例；Rust runtime 三级回退 + sys-crate 反向 apt + workspace evidence + go-only 反例；apt_map 去重/dev 跳过/PEP 503/混合。
- **testdata/** — 以 t.TempDir 写 fixture 为主，无静态目录。

## 设计约束

- **不抄 install/start phase**：nixpacks 终态是 Dockerfile recipe，askdao-cli 终态是 Anthropic environment 声明 + agent.yml。`FrameworkPlan.EntryPoint` 字段保留但仅作参考，不会上 environment。
- **provider stateful with read-only fields**：`Pkgs` 字段必须在调 Plan 前由 caller 设置（issue #8 的 `cmd/askdao detect` 编排器负责）。这避免 provider 自己 spawn syft —— 一次扫描多 provider 共用。
- **multi-source evidence + 加权打分**：单看 manifest 容易误判（manifest 里写了 fastapi 但项目实际只是间接依赖）；加 import 扫描 + 配置文件存在性，多于一项才算数。spike §6 详细论证。
- **PEP 503 归一**：pip dep 比对走 `normalizeDepName("pip", x)`：lowercase + `_` → `-` + `.` → `-`。和 internal/scanner 同规则；目前各包独立实现避免 import cycle，未来可抽公共 util。
- **Node 双轨 dep 探测**：`hasNpmDep` 先看 syft 输出，再 fallback 读 package.json。让 provider 单独可用（不强依赖 #2 scanner 的输出），同时正常流程下又能用 lockfile 真相做更准的判断。
- **Go / Rust 不强求 Pkgs**：Go provider 完全不消费 syft 输出（runtime 抽取 + cgo 扫描就够）；Rust provider 仅在 sys-crate 反向 apt 时用 `Pkgs["cargo"]`，nil 时跳过整张表 —— `Plan` 仍能工作只是少了 apt hint。

## 依赖

无新增三方依赖（`go.go` / `rust.go` 复用了 issue #3 引入的 `BurntSushi/toml`）；用标准库 `regexp` / `encoding/json` + `internal/types`。

## 字段输出对应（detection.json）

| Detection 字段 | provider 函数 | 备注 |
|---|---|---|
| `detected_frameworks` | `Plan.Frameworks` | 多源证据加权（Python/Node 主路径） |
| `inferred_apt_packages` | `Plan.SystemPkgs["apt"]` | 来自 InferAptPackages（Python/Node）+ inferRustAptPackages（Rust）+ cgo 触发（Go） |
| `detected_external_services` | `Plan.ExternalSvc` | dep+import 交叉证据 |

provider 不直接写 `detected_runtimes` —— 那是 internal/scanner.DetectRuntimes 的活；provider 只产 `RuntimeHint` 给推荐器做交叉校验。

## 后续 issue 挂载点

- issue #6 recommender 把 provider 输出 + scanner 输出合成 detection.json，喂 LLM
- issue #8 `cmd/askdao detect` 调度多 provider parallel 跑 Detect，再 Plan 命中的那些
- 长尾 provider（Java / Ruby / PHP / Elixir）按需 Phase 3 加；接口已稳定，加新 provider = 新文件 + 新规则表

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
