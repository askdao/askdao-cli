# docs/
> L2 | 父级: ../CLAUDE.md

设计文档 + 调研报告。Phase 1 设计稿位于 `design.md`，调研产物按主题落 `investigations/` 子目录。

## 成员清单

- **HANDOFF.md** — 新会话上手 / 上下文切换的入口文档（current status / quick start / document map / decisions / pitfalls）
- **design.md** — askdao-cli `init --auto` 的完整设计（v0.5 卡片 UX + v0.6 §9.10 部署 payload/archetype（PR #19 已交付）+ §9.11 Plugin 机制调研（Claude Code/Codex Plugin 对 askdao-cli 三个层面的影响，待决策））：四层流水线（syft → dev-filter → providers → LLM）、detection.json schema、harness-neutral 中间格式 yaml schema、Dockerfile 兼容 5 字段、KOL 中等详情卡片 UX、部署清单 lockfile-driven skill 分类、Phase 1-3 路线图
- **review-2026-05-06.md** — v0.2 review（Anthropic 三资源模型重审）
- **review-v0.3-2026-05-06.md** — v0.3 review（harness-neutral 中间格式）
- **review-v0.4-2026-05-06.md** — v0.4 review（Dockerfile 兼容性补强）
- **review-v0.5-2026-05-06.md** — v0.5 review（中等详情卡片 UX）
- **investigations/** - spike 报告与外部技术参考（详见子目录 CLAUDE.md）

## 文档来源

`design.md` 与 `investigations/` 下两份报告原存放于 `harness-design/designs/` 与 `harness-design/investigations/`，于 2026-05-06 整体迁移到此处 —— 这些产物只服务 askdao-cli，归属本仓库更合理。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
