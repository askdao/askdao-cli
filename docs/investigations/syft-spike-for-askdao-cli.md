# Syft Spike for askdao-cli

> 验证 anchore/syft 是否可作为 askdao-cli L1-L2 包扫描器的底座
> Date: 2026-05-06
> Target: 一个大型真实后端仓库（含大体积 vendored submodule）

## TL;DR

**结论：直接采用 Syft 作为 askdao-cli L1-L2 扫描器，不再自写 manifest parser。**

Syft 1.44.0 在一个大型真实后端仓库实测：核心依赖 100% 识别准确，输出字段完整可直接喂下游推荐器。唯一 gap 是 dev/prod 不区分，askdao-cli 需要做二次过滤层。

---

## 安装与运行

```bash
brew install syft     # 78 MB，一次性
syft dir:. --exclude './vendor/**' -o json --quiet > sbom.json
```

性能：扫一个大型仓库 + 其大体积 vendored submodule (~1.5 GB / 1.2 万文件) 全量 < 5 秒。

---

## 关键验证项

### ✓ 1. Python uv.lock 完整识别

测试仓用 uv 而非 poetry/pip-tools。Syft `python-package-cataloger` 直接支持 `python-uv-lock-entry` 格式：

| 关键依赖 | 识别 | 版本 | Source |
|---------|------|------|--------|
| fastapi | ✓ | 0.135.1 | uv.lock |
| sqlalchemy | ✓ | 2.0.48 | uv.lock |
| anthropic | ✓ | 0.97.0 | uv.lock |
| alembic | ✓ | 1.18.4 | uv.lock |
| pydantic | ✓ | 2.12.5 | uv.lock |
| asyncpg | ✓ | 0.31.0 | uv.lock |
| pytest | ✓ | 9.0.2 | uv.lock | ← dev 依赖也扫到，未做区分
| mypy | ✓ | 1.19.1 | uv.lock | ← dev
| ruff | ✓ | 0.15.6 | uv.lock | ← dev

→ **排除大体积 vendored submodule 后共 66 个 Python 包，全部命中。**

### ✓ 2. GitHub Actions 也被识别

`/.github/workflows/deploy.yml` 里 4 个 action 自动识别（type=`github-action`，metadataType=`github-actions-use-statement`）：
- `actions/checkout@v4`
- `aws-actions/amazon-ecr-login@v2`
- `aws-actions/configure-aws-credentials@v4`
- `docker/setup-buildx-action@v3`

→ 这条 askdao-cli 暂时不用，但能用来推断"项目部署到 AWS"这种语境。

### ✓ 3. Submodule 噪声可控

不加 `--exclude` 时：1229 个 artifact（vendored submodule 1159 + 主仓 70）。
加 `--exclude './vendor/**'` 后：70 个 artifact（66 Python + 4 Actions）。

→ askdao-cli 必须默认尊重 `.gitmodules` / `.gitignore`，submodule 不算"项目主依赖"。

---

## 字段完整度（fastapi 包样例）

```json
{
  "id": "dcabd7d0cb5aa8c4",
  "name": "fastapi",
  "version": "0.135.1",
  "type": "python",
  "foundBy": "python-package-cataloger",
  "locations": [{"path": "/uv.lock", "evidence": "primary"}],
  "language": "python",
  "purl": "pkg:pypi/fastapi@0.135.1",
  "metadataType": "python-uv-lock-entry",
  "metadata": {
    "index": "https://pypi.org/simple",
    "dependencies": [
      {"name": "annotated-doc", "optional": false},
      {"name": "pydantic", "optional": false},
      {"name": "starlette", "optional": false},
      ...
    ]
  },
  "cpes": [...]   // 12 个 CPE 给安全扫描，askdao-cli 暂不用
}
```

足够直接转译为 Anthropic environment 的 `packages.pip = ["fastapi==0.135.1", ...]`。

---

## 已知 Gap & askdao-cli 的对应策略

### Gap 1 · dev vs prod 不区分

Syft 从 lockfile 拉所有依赖（包括 dev），但 lockfile 本身（uv.lock / poetry.lock / package-lock.json）通常不带 dev 标记。

**对应策略**：askdao-cli 在 syft 之上加**二次过滤层**，读 manifest 主文件（`pyproject.toml` / `package.json`）的 dev 声明：
- `pyproject.toml` → `[dependency-groups].dev` (uv) / `[tool.poetry.group.dev.dependencies]` (poetry) / `[project.optional-dependencies].dev`
- `package.json` → `devDependencies`
- `Cargo.toml` → `[dev-dependencies]`

这层不复杂（每种 ~30 行 Go），但必须有 —— 否则 `pytest/mypy/ruff` 这种工具会被误推上生产 Anthropic environment。

### Gap 2 · `distro` 字段在 dir 模式空

`syft dir:.` 输出 `"distro": {}`。distro 推断只在 `syft image:...` 模式才有意义（探查容器内 OS）。

**对应策略**：askdao-cli 不依赖 distro 字段，OS 选择走 Anthropic environment 默认（cloud type）。如未来要选基础镜像，从 `Dockerfile` 的 `FROM` 字段抽（用 `moby/buildkit` parser）。

### Gap 3 · 框架推断不在 Syft 职责内

Syft 只识别"包"，不推断"框架"。比如装了 `fastapi` 不代表项目用 FastAPI（可能只是间接依赖）。

**对应策略**：askdao-cli 加 L4 推断层，复用 nixpacks providers 的启发式 + LLM 二次校验（见下一阶段任务）。

---

## 推荐使用方式

### 模式 A · CLI 调用（spike / 早期 MVP）

直接 spawn `syft` 进程读 stdout JSON。优点：升级跟进 syft release 零成本。缺点：依赖 PATH 上有 syft binary。

### 模式 B · Go library import（最终生产）

```go
import "github.com/anchore/syft/syft"

src, _ := syft.GetSource(ctx, "/path/to/project", &syft.GetSourceConfig{
    Excludes: []string{"./vendor/**"},
})
sbom, _ := syft.CreateSBOM(ctx, src, nil)
for pkg := range sbom.Artifacts.Packages.Enumerate() {
    // pkg.Type, pkg.Name, pkg.Version, pkg.PURL, pkg.Locations, pkg.Metadata
}
```

优点：用户装 askdao-cli 即可用，零外部依赖。缺点：syft 二进制依赖较重（最终 askdao-cli ~80 MB），且 syft 的 Go module 锁定较紧，跟其他依赖可能有版本冲突。

→ **推荐**：MVP 用模式 A，正式版评估模式 B。决策依据是 askdao-cli 安装包大小可接受度。

---

## 与 Warp/Oz 路径的对照

| 维度 | Warp/Oz | askdao-cli |
|------|---------|----------|
| 包扫描在哪跑 | 服务器（用 GitHub App token 拉 manifest） | 用户本地（直接读文件，零网络） |
| 工具 | 自研 + GitHub Languages API | Syft（开源工业标准） |
| 隐私边界 | 客户端只发 owner/repo 标识符 | 客户端就在用户机器上，不需要发任何东西 |
| 输出形态 | Docker image 推荐 | Anthropic environment.packages 声明 |

askdao-cli 的隐私模型其实**比 Warp 更强**（本地 read，零上送），是 Wrapper 形态的天然优势。

---

## 下一步

1. 任务 #2：通读 nixpacks `src/providers/python.rs`，抽出 Provider trait 移植到 Go 的接口
2. 任务 #3：把本 spike 结论 + nixpacks 学到的抽象，整合写入 [`../design.md`](../design.md)
