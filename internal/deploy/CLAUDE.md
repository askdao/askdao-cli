# internal/deploy/
> L2 | 父级: ../../CLAUDE.md

`agent deploy` 的服务端客户端层 —— KOL 的 yaml（+ detection + custom skill zip）以 multipart POST 到 `POST /api/v1/cli/deploy`，需要时 PATCH KOL 资料。只做「HTTP 客户端 + skill 目录打包」两件事；命令行编排在 `cmd/askdao/deploy.go`。实现细节见各文件头注释；历史变更见 git log / PR。

## 成员清单

- **client.go** — Client.Deploy（multipart 构造 + 409 三义分类为专用 error 类型）+ SetupKol（PATCH 资料）+ DeployResponse 等契约类型 + 路径/超时常量
- **zip.go** — `ZipDir`：skill 目录递归打包（单一顶层目录形态 + ignore 过滤：默认排除 + `.askdaoignore` 兜底 + SKILL.md 硬保留）
- **client_test.go / zip_test.go** — happy/409 判别/非 2xx/ignore 过滤守护

## 设计约束

- **stdlib only**：连 `internal/types` 都不 import——只收发 `[]byte` 与通用 map，yaml 解析归 cmd 层
- **agent_yaml 发原始字节**：不 Marshal 往返，保留注释/顺序/未知字段
- **skill part 必须带 filename**（CreateFormFile）——服务端按 file part 识别，缺 filename 判为字符串 400
- **409 三义靠 detail.reason 区分**：kol_profile_required / visibility_downgrade_requires_confirm / blocking-warnings（仅 action=rejected 阻断，severity 不 gate）；解析失败退化通用错误带 body
- **timeout 180s**：deploy 服务端同步串行三步，比 recommend 的 90s 宽

## 与服务端契约的对齐点

只引公开契约（端点路径 + 字段名 + 响应形态），靠 CI diff 校验防漂移：DeployResponse / TranslationReport（小写 enum）/ 409 各 detail 形态 / skill zip 格式（zip 内 SKILL.md + frontmatter name + 单一顶层目录）。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
