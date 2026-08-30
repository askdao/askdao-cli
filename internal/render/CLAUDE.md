# internal/render/
> L2 | 父级: ../../CLAUDE.md

deploy 终端输出渲染层：pre-deploy diff 与 translation warnings。渲染走 `Renderer{Out, Color, Width}`，`NewPlain(w)` 出干净文本供测试/管道。

## 成员清单

- **colors.go** — `Renderer` 结构 + ANSI 常量 + `New()`/`NewPlain(w)` 构造器 + `section(title)` 标题框
- **diff.go** — `DiffAgentSpec(a, b)` 显式字段级 walker + `RenderDiff`（deploy preview 三行块形态）
- **warnings.go** — `TranslationWarning` 结构 + 严重度三级 + `RenderTranslationWarnings`（ViewSummary 折叠 / ViewAll 全展开；HIGH 永远 verbatim）
- **\*\_test.go / testdata/** — unit 测试 + golden 视觉回归

## 设计约束

- 不引第三方渲染 dep（手写 ANSI；diff 用显式 walker 保 path 字符串可控）
- Renderer 不持状态；Color=false 是 first-class（测试全跑 plain 模式）
- 消费方仅 `cmd/askdao/deploy.go`（diff preview + translation report 渲染）

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
