# internal/pipeline/
> L2 | 父级: ../../CLAUDE.md

唯一 orchestration 层 —— 把 scanner / dev_filter / providers / policy / recommender 串成 `pipeline.Run(ctx, opts)`。`agent edit` 的扫描路径（LLM 可为 nil）走这个入口。实现细节见文件头注释；历史变更见 [../../CHANGELOG.md](../../CHANGELOG.md)。

## 成员清单

- **pipeline.go** — `Run(ctx, Options) (*Result, error)` 唯一入口：Scanner phase → ApplyDevFilter → Provider phase（框架/外部服务合并 + apt 跨源去重）→ Policy phase → ScanInfo 装配 → 确定性 skills builder → 可选 LLM phase
- **skills_builder.go** — `BuildAgentSpecSkills(det)`：确定性构造 yaml skills 段（custom_local 按目录路径 / implied builtin 去重）；硬字段确定性填充、软字段才交 LLM 的信任边界
- **pipeline_test.go / skills_builder_test.go** — 全管线 fixture 测试（含 syft 缺失软降级）+ builder 用例

## 设计约束

- **soft-fail over hard-fail**：syft 缺失 / 单 phase 错进 Warnings 而非 error，KOL 看到部分结果
- **不持状态**：每个 Run 独立，IO 走 ctx + opts
- **LLM 经接口注入**：pipeline 不识别具体实现
- **dedupe 在 pipeline 不在 providers**；provider 顺序固定 Python → Node → Go → Rust

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
