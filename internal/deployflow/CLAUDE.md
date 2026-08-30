# internal/deployflow/
> L2 | 父级: ../../CLAUDE.md

部署编排层 —— deploy 装配单源（Prepare/Deploy/ResolveServerAndToken）+ skill 打包（枚举 spec 的 `custom_local` skill → SKILL.md frontmatter 前置校验 → 调 `internal/deploy.ZipDir` 打 zip）。**放此包而非 `internal/deploy`**：deploy 是 stdlib-only 的 HTTP+zip 客户端域（[../deploy/CLAUDE.md](../deploy/CLAUDE.md) 明确「连 internal/types 都不 import」），而 skill 打包是编排（依赖 `types.AgentSpec` + `scanner.ParseSkillFrontmatter`），归编排层。CLI（`cmd/askdao agent deploy`）与桌面（`cmd/askdao-studio` OnDeploy）**单源共用**，杜绝双写漂移。

## 成员清单

- **deploy.go** — `Prepare(dir, harnessOverride) → *Prepared`（读 yaml + PackageSkills + detection + harness 默认链）+ `(*Prepared).Deploy(ctx, url, token, force, confirmDowngrade)` + `ResolveServerAndToken`（env pair > credentials.json > error）。CLI runDeploy / web studio OnDeploy / 桌面 App.deploy 三入口共用，杜绝装配漂移（桌面此前缺 Detection/降级闸/env override/harness 默认）

- **skills.go** — `PackageSkills(dir, spec) (map[string][]byte, error)` / `ResolveSkillDir(dir, skill)`（四分支：`~/` 展开 home / 绝对路径原样 / `Scope=="user"` 相对原样 / project 相对 join dir —— 工作台勾选的全局 user-scope skill 走绝对或 Scope 分支；**导出**供桌面 skill-validate/skill-fix 定位与 deploy gate 同一 SKILL.md）/ `UpsertSkillFrontmatter(skillMDPath, name, description)`（行级 upsert 写回 SKILL.md frontmatter：缺 `--- --- ` 块则前插、有键就地更新、空参数跳过，保留 body 与其余键；供桌面一键补全 + 后续 AI 助手写 SKILL.md 清同一 gate）（均对外）；包内 `quoteYAMLValue`（含 `:`/`#`/引号才加引号，对齐 `ParseSkillFrontmatter` 剥单层引号的宽松读法）。`PackageSkills`：遍历 `spec.Skills` 的 custom_local → `ResolveSkillDir` → 校验目录 + `SKILL.md` 存在 + frontmatter `name`/`description` 必填（description 是模型触发指令，缺则部署成功但永不激活，fail-fast）+ frontmatter name 跨 skill 唯一 → `deploy.ZipDir` 打 zip（harness 中性 invariant：zip 顶层目录 = `filepath.Base`）。
- **skills_test.go** — `TestResolveSkillDir`（四分支表驱动）+ `TestPackageSkills_FrontmatterValidation`（缺 name / 缺 description / name 撞名拒绝 + 完整 frontmatter 通过）+ `TestUpsertSkillFrontmatter`（无块前插 / 缺字段补全 / 已有键就地更新不重复 / 含冒号值加引号 / 空参数 no-op，经 `scanner.ParseSkillFrontmatter` round-trip 证补全后必过 deploy gate）。`runDeploy` 的 e2e 回归（`TestDeploy_UserScopeAbsolutePath`）在 cmd/askdao 覆盖 deploy 全链路。

## 设计约束

- **编排层可 import 业务包**：与 `internal/deploy`（stdlib-only 的 HTTP+zip 域）纪律不同 —— deployflow 是编排，import `internal/{deploy,scanner,types}` 组装 skill 打包。**无循环**：deploy / scanner / types 都不 import deployflow。
- **单源反漂移**：CLI 与桌面共用 `PackageSkills` + `ResolveSkillDir`，避免各自复刻四分支 —— 全局 user-scope skill 是绝对路径，`filepath.Join(dir, absPath)` 会拼坏成 "directory not found"，四分支解析是唯一正确处理。桌面 skill-validate/skill-fix 也经 `ResolveSkillDir` 定位，与 deploy 打包同一解析口径。
- **独立成包**：Go 不允许跨 `package main` import，桌面（`cmd/askdao-studio`）与 CLI 复用 skill 打包必须落 `internal`；deploy 纪律不收 types/scanner，编排逻辑归此包。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
