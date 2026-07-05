# internal/deployflow/
> L2 | 父级: ../../CLAUDE.md

部署编排层 —— skill 打包（枚举 spec 的 `custom_local` skill → SKILL.md frontmatter 前置校验 → 调 `internal/deploy.ZipDir` 打 zip）。**放此包而非 `internal/deploy`**：deploy 是 stdlib-only 的 HTTP+zip 客户端域（[../deploy/CLAUDE.md](../deploy/CLAUDE.md) 明确「连 internal/types 都不 import」），而 skill 打包是编排（依赖 `types.AgentSpec` + `scanner.ParseSkillFrontmatter`），归编排层。CLI（`cmd/askdao agent deploy`）与桌面（`cmd/askdao-studio` OnDeploy）**单源共用**，杜绝双写漂移。

## 成员清单

- **skills.go** — `PackageSkills(dir, spec) (map[string][]byte, error)`（对外）+ `resolveSkillDir(dir, skill)`（包内四分支：`~/` 展开 home / 绝对路径原样 / `Scope=="user"` 相对原样 / project 相对 join dir —— 工作台勾选的全局 user-scope skill 走绝对或 Scope 分支）。`PackageSkills`：遍历 `spec.Skills` 的 custom_local → `resolveSkillDir` → 校验目录 + `SKILL.md` 存在 + frontmatter `name`/`description` 必填（description 是模型触发指令，缺则部署成功但永不激活，fail-fast）+ frontmatter name 跨 skill 唯一 → `deploy.ZipDir` 打 zip（harness 中性 invariant：zip 顶层目录 = `filepath.Base`）。
- **skills_test.go** — `TestResolveSkillDir`（四分支表驱动）+ `TestPackageSkills_FrontmatterValidation`（缺 name / 缺 description / name 撞名拒绝 + 完整 frontmatter 通过）。`runDeploy` 的 e2e 回归（`TestDeploy_UserScopeAbsolutePath`）在 cmd/askdao 覆盖 deploy 全链路。

## 设计约束

- **编排层可 import 业务包**：与 `internal/deploy`（stdlib-only 的 HTTP+zip 域）纪律不同 —— deployflow 是编排，import `internal/{deploy,scanner,types}` 组装 skill 打包。**无循环**：deploy / scanner / types 都不 import deployflow。
- **单源反漂移**：CLI 与桌面共用 `PackageSkills`，避免各自复刻 `resolveSkillDir` 四分支 —— 全局 user-scope skill 是绝对路径，`filepath.Join(dir, absPath)` 会拼坏成 "directory not found"，四分支解析是唯一正确处理。
- **独立成包**：Go 不允许跨 `package main` import，桌面（`cmd/askdao-studio`）与 CLI 复用 skill 打包必须落 `internal`；deploy 纪律不收 types/scanner，编排逻辑归此包。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
