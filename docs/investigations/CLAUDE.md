# docs/investigations/
> L2 | 父级: ../CLAUDE.md

工程化决策依据 —— 每篇围绕一个外部技术或开源项目，验证其能否作为 askdao-cli 实现底座。

## 成员清单

- **syft-spike-for-askdao-cli.md** — anchore/syft 1.44.0 在 conductor 仓库实测：核心 Python/npm/cargo 依赖识别准确，单次扫描 < 5 秒；唯一 gap 是 dev/prod 不区分需二次过滤层。**结论：直接采用为 L1-L2 包扫描器**。
- **nixpacks-provider-pattern.md** — 通读 railwayapp/nixpacks Provider trait + python.rs / node.rs，抽出 Go 移植抽象（Provider 接口 4 方法 + App/Env 抽象 5 方法），重点摘录"依赖→系统包"反向映射表（psycopg→libpq-dev、puppeteer→12 个 apt 包等）。**结论：移植 4 个核心 provider（python/node/go/rust），~1000 行 Go**。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
