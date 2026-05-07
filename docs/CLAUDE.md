# docs/
> L2 | 父级: ../CLAUDE.md

设计文档 + 调研报告。Phase 1 设计稿位于 `design.md`，调研产物按主题落 `investigations/` 子目录。

## 成员清单

- **design.md** — askdao-cli `init --auto` 的完整设计：四层流水线（syft → dev-filter → providers → LLM）、detection.json schema、agent.yml schema、Phase 1 MVP 范围、~2400 行 Go 工程量估算、3 个待决策项
- **investigations/** - spike 报告与外部技术参考（详见子目录 CLAUDE.md）

## 文档来源

`design.md` 与 `investigations/` 下两份报告原存放于 `harness-design/designs/` 与 `harness-design/investigations/`，于 2026-05-06 整体迁移到此处 —— 这些产物只服务 askdao-cli，归属本仓库更合理。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
