# internal/deployflow/
> L2 | 父级: ../../CLAUDE.md

部署编排层 —— deploy 装配单源（Prepare / Deploy / ResolveServerAndToken）+ skill 打包编排（枚举 + frontmatter 校验 + zip）。CLI 与桌面共用，杜绝双写漂移。放此包而非 `internal/deploy`：deploy 是 stdlib-only 客户端域，编排（依赖 types/scanner）归此层。实现细节见各文件头注释；历史变更见 git log / PR。

## 成员清单

- **deploy.go** — `Prepare(dir, harnessOverride)` + `(*Prepared).Deploy(...)` + `ResolveServerAndToken`（env pair > credentials.json > error）；CLI / web studio / 桌面三入口共用
- **skills.go** — `PackageSkills`（frontmatter name/description 必填 + 跨 skill 唯一 fail-fast → ZipDir）+ `ResolveSkillDir`（`~` 展开 / 绝对 / user scope / project 相对四分支）+ `UpsertSkillFrontmatter`（行级写回，供桌面一键补全）
- **skills_test.go** — 四分支表驱动 + frontmatter 校验/补全 round-trip

## 设计约束

- **编排层可 import 业务包**（deploy/scanner/types），反向无依赖，无循环
- **单源反漂移**：skill 路径四分支解析是唯一正确处理（user 全局 skill 是绝对路径，裸 Join 会拼坏）；打包与校验/补全共用同一解析口径
- **独立成包**：桌面与 CLI 是不同 `package main`，复用必须落 internal

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
