# Nixpacks Provider Pattern · askdao-cli 移植笔记

> 通读 railwayapp/nixpacks 的 Provider trait + python.rs + node/mod.rs，
> 抽出可移植到 Go 的接口与启发式
> Date: 2026-05-06

## TL;DR · 直接照搬什么

1. **Provider trait（4 方法）**：`name() / detect() / get_build_plan() / metadata()` —— Go interface 直译
2. **App / Environment 抽象（5 方法）**：`includes_file / read_file / find_match / find_files / get_config_variable` —— ~50 行 Go 实现
3. **Package Manager 自动选优先级**：lockfile 强信号 > manifest 弱信号 > 文件名兜底
4. **依赖 → 系统包反向映射表**：nixpacks 工程化沉淀的领域知识，**直接抄进 askdao-cli 的 apt 推断器**

## 不照搬什么

1. **Phase 模型（setup/install/start）整套不抄**：nixpacks 输出 BuildPlan → Dockerfile，askdao-cli 输出 Anthropic environment.packages 声明，只抄 detect 阶段，不抄 install/start。
2. **Nix archive 概念不抄**：nixpacks 用 Nix 锁定基础包版本，Anthropic environment 用 cloud type，包版本走 pip/npm 自己的 spec。
3. **Provider 30 个全要不要**：Phase 1 只移植 python + node + go + rust 四个核心，其他按需补。

---

## 1. Provider Trait（直译 Go）

**Rust 原版**（`src/providers/mod.rs:30`）:

```rust
pub trait Provider: Send + Sync {
    fn name(&self) -> &str;
    fn detect(&self, app: &App, env: &Environment) -> Result<bool>;
    fn get_build_plan(&self, app: &App, env: &Environment) -> Result<Option<BuildPlan>>;
    fn metadata(&self, app: &App, env: &Environment) -> Result<ProviderMetadata>;
}
```

**Go 移植**（askdao-cli `internal/providers/provider.go`）:

```go
type Provider interface {
    Name() string
    Detect(app *App, env *Env) (bool, error)
    Plan(app *App, env *Env) (*FrameworkPlan, error)   // 注意：不是 BuildPlan，是简化版
    Metadata(app *App, env *Env) (Metadata, error)
}

// askdao-cli 的 FrameworkPlan 比 nixpacks BuildPlan 简单很多
type FrameworkPlan struct {
    Frameworks   []string                 // ["FastAPI", "Alembic"]
    SystemPkgs   map[string][]string      // {"apt": ["libpq-dev"]}
    Runtime      RuntimeHint              // {Kind: "python", Version: "3.12"}
    EntryPoint   string                   // 推断的启动命令（仅做参考，不上 environment）
    Confidence   float64                  // 0.0-1.0
    Evidence     []Evidence               // 给 reason 字段做证据
}
```

---

## 2. App / Environment 抽象

**Rust 原版用法**（python.rs 高频调用）:

```rust
app.includes_file("requirements.txt")              // 文件存在性
app.includes_file("pyproject.toml")
app.read_file(".tool-versions")?                   // 读文件
app.find_match(&regex, "/**/*.py")?                // glob+grep
app.find_files("/**/*.py").unwrap()                // glob 找文件
env.get_config_variable("PYTHON_PACKAGE_MANAGER")  // 读环境变量
```

**Go 移植**（薄层）：

```go
type App struct {
    rootPath string
    fileSet  map[string]bool   // 已扫文件名缓存
}

func (a *App) IncludesFile(rel string) bool                  { ... }
func (a *App) ReadFile(rel string) ([]byte, error)           { ... }
func (a *App) FindMatch(re *regexp.Regexp, glob string) bool { ... }
func (a *App) FindFiles(glob string) []string                { ... }

type Env struct {
    config map[string]string  // 用户在 askdao.yml 或环境变量里覆盖的值
}

func (e *Env) GetConfigVariable(key string) (string, bool) { ... }
```

**关键决策**：file scan 一次性建索引而不是每次 IO，~5K 文件项目第一次扫 < 50 ms。

---

## 3. Package Manager 自动选（Python 实例）

**Rust 优先级链**（python.rs:81-99）:

```
1. requirements.txt              → pip + requirements.txt
2. pyproject.toml + poetry.lock  → poetry
3. pyproject.toml + pdm.lock     → pdm
4. pyproject.toml + uv.lock      → uv          ← conductor 走这条
5. pyproject.toml (无 lock)      → setuptools
6. Pipfile                       → pipenv
7. （都没）                      → NoInstallation
```

**Go 移植**：直接 1:1 翻译，5 行 switch。

**Node 类似**（[node/mod.rs](https://github.com/railwayapp/nixpacks/blob/main/src/providers/node/mod.rs)）:
```
package-lock.json → npm
yarn.lock         → yarn
pnpm-lock.yaml    → pnpm
bun.lockb         → bun
（manifest only） → npm 兜底
```

---

## 4. 依赖 → 系统包反向映射表 ⭐ 最大宝藏

这是 nixpacks 沉淀多年的工程经验，直接抄进 askdao-cli。

### Python 反向映射（python.rs:240-268）

| Python 依赖 | 触发系统包 | 用途 |
|------------|-----------|------|
| `psycopg2` / `psycopg` | `postgresql_16.dev` | postgres C 扩展 |
| `mysqlclient` (Django+MySQL) | `libmysqlclient.dev` | mysql C 扩展 |
| `cairo` | `cairo` lib | 图形渲染 |
| `pydub` | `ffmpeg-headless` | 音频处理 shell out |
| `pdf2image` | `poppler_utils` | PDF 解析 shell out |

兜底：所有 Python 项目都装 `gcc + zlib + stdenv.cc.cc.lib`（C 扩展通用编译依赖）

### Node 反向映射（node/mod.rs:144-173）

| Node 依赖 | 触发 apt 包 | 用途 |
|----------|-----------|------|
| `prisma` | `openssl` | DB 连接 TLS |
| `sharp` | `gcc-unwrapped` libs | 图片处理原生 |
| `puppeteer` | `libnss3 / libatk1.0-0 / libatk-bridge2.0-0 / libcups2 / libgbm1 / libasound2t64 / libpangocairo-1.0-0 / libxss1 / libgtk-3-0 / libxshmfence1 / libglu1 / chromium`（12 个）| Chromium headless |
| `canvas` | `libuuid + libGL` | 图形原生 |

### askdao-cli 适配

把 nixpacks 的 Nix 包名转换为 Anthropic environment 支持的 **apt 包名**：

```go
var pythonDepToAptPkgs = map[string][]string{
    "psycopg2":     {"libpq-dev", "gcc"},
    "psycopg":      {"libpq-dev", "gcc"},
    "mysqlclient":  {"libmysqlclient-dev", "gcc"},
    "pydub":        {"ffmpeg"},
    "pdf2image":    {"poppler-utils"},
    "cairo":        {"libcairo2-dev"},
    "Pillow":       {"libjpeg-dev", "zlib1g-dev"},  // askdao-cli 自加
    "lxml":         {"libxml2-dev", "libxslt-dev"}, // askdao-cli 自加
}

var npmDepToAptPkgs = map[string][]string{
    "prisma":     {"openssl"},
    "sharp":      {"libvips-dev"},
    "puppeteer":  {"libnss3", "libatk1.0-0", "libatk-bridge2.0-0", /* ...12 项 */ "chromium"},
    "canvas":     {"libuuid1", "libgl1"},
    "playwright": {"libnss3", "libatk1.0-0", /* ...类似 puppeteer */},
}
```

**这张表是 askdao-cli 最有差异化价值的工程沉淀**。每发现新的"装不上"案例就往里加。Phase 1 起码要有这两张表的 80%。

---

## 5. uses_dep() 的实现策略

nixpacks 用 `uses_dep("django")` 判断"项目实际用了 django"（不仅仅是 manifest 里写了）：

```rust
fn uses_dep(app: &App, dep: &str) -> Result<bool> {
    // 1. 看 pyproject.toml / requirements.txt 等 manifest
    // 2. + 看 .py 文件里有 import django
}
```

**askdao-cli 的实现**：syft 已经给了 manifest 层面的事实（66 个 Python 包名 + 版本）。
- Phase 1：直接 `for pkg in syft.packages: if pkg.name == "django"` 即可
- Phase 2：可选地加 import 扫描（防止 manifest 写了 django 但项目实际不用）

---

## 6. 启发式样例（Django + PostgreSQL）

nixpacks python.rs 探查 Django 的逻辑很值得抄：

```rust
fn is_django(app: &App, _env: &Environment) -> Result<bool> {
    let has_manage = app.includes_file("manage.py");
    let imports_django = uses_dep(app, "django")?;
    Ok(has_manage && imports_django)   // 必须双重证据
}

fn is_using_postgres(app: &App, _env: &Environment) -> Result<bool> {
    let re = Regex::new(r"django.db.backends.postgresql").unwrap();
    let uses_pg = app.find_match(&re, "/**/*.py")?
        || uses_dep(app, "psycopg2")?
        || uses_dep(app, "psycopg")?;
    Ok(uses_pg)
}
```

这种**多源证据 + OR/AND 组合**的启发式比单看 manifest 准得多。askdao-cli L4 框架推断就走这套模式：

```go
type FrameworkRule struct {
    Name      string
    EvidenceFns []EvidenceFn  // 每条返回 (matched bool, weight float, why string)
    MinScore  float64          // 总分阈值
}

// FastAPI 规则示例
{
    Name: "FastAPI",
    EvidenceFns: [
        UsesNpmDep("fastapi", weight=0.5),
        ImportPattern(`from fastapi import`, weight=0.4),
        FilePattern(`uvicorn`, "/**/*.py", weight=0.1),
    ],
    MinScore: 0.5,
}
```

---

## 7. Phase 1 移植清单

| Provider | 优先级 | Source | 工程量估算 |
|---------|------|--------|----------|
| **python** | P0 | `python.rs` (882 行) | ~250 行 Go |
| **node**   | P0 | `node/mod.rs` (1228 行) | ~300 行 Go |
| **go**     | P0 | `go.rs` | ~150 行 Go |
| **rust**   | P0 | `rust.rs` | ~150 行 Go |
| java/ruby/php/elixir | P1 | 各对应 .rs | 后续按需 |
| 其他 16 种 | P2 | - | 不做 |

**Phase 1 总移植工作量估算 ~1000 行 Go**。加 syft / enry 的 wrap，全部 askdao-cli 扫描器层 ~1500 行 Go 可落。

---

## 8. 与 syft 的分工

```
┌────────────────────────────────────────────┐
│  syft   (扫包)                              │
│  → 输出 1000+ 包名+版本 (含传递依赖)         │
└──────────────┬─────────────────────────────┘
               │
               ▼
┌────────────────────────────────────────────┐
│  askdao-cli dev-filter                      │
│  读 manifest 主文件区分 dev / prod          │
│  → 输出 ~50 个 prod 直接依赖                │
└──────────────┬─────────────────────────────┘
               │
               ▼
┌────────────────────────────────────────────┐
│  askdao-cli providers (移植 nixpacks)        │
│  detect() → 这是 python/node/rust 项目      │
│  Plan() → 框架推断 + 系统包反向映射         │
│  → 输出 detection.json 的 detected_*  块   │
└──────────────┬─────────────────────────────┘
               │
               ▼
┌────────────────────────────────────────────┐
│  L4 LLM 推荐器                              │
│  把 detection.json 喂 LLM 生成 yaml          │
└────────────────────────────────────────────┘
```

每层职责清晰：
- **syft**：把"manifest 都说了什么包"问题解决
- **dev-filter**：把"哪些是生产依赖"问题解决
- **providers**：把"这个项目用什么框架/需要什么系统包"问题解决
- **LLM**：把"翻译成 reason 文案 + 决策细节"问题解决

---

## 下一步

整合本文 + syft spike 报告 → 写 [`../design.md`](../design.md)。
