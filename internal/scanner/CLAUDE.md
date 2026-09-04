# internal/scanner/
> L2 | 父级: ../../CLAUDE.md

L1-L3 流水线确定性扫描器（syft / enry / dockerfile 底座 + dev_filter / runtimes / mcp_config / skills_dir / secrets_hint / harness_signals / harness_scope）。所有函数返回 `internal/types.Detection` 的 sub-types，由 pipeline 装配落 `.askdao/detection.json`。实现细节见各文件头注释；历史变更见 git log / PR。

## 成员清单

- **syft.go** — 包扫描：spawn syft CLI 出 JSON（SyftRunner 可注入供单测离线）；所有包默认 IsProd=true 交 dev_filter 重标
- **enry.go** — 语言统计（Linguist 式 vendor/docs/generated 过滤，只计 Programming+Markup）
- **dockerfile.go** — Dockerfile AST 解析 + RUN 抽取 apt/pip + 兼容警告
- **glob.go** — syft 风格 `./pattern/**` glob 匹配
- **dev_filter.go** — 按 manifest 把 dev/test 包翻成 IsProd=false（pip 三 flavor / npm / cargo；PEP 503 归一）
- **runtimes.go** — 五类版本 pin 文件解析 + `.tool-versions` fallback
- **mcp_config.go** — MCP 配置发现（project + user 双 scope，JSON/TOML 按扩展名分派；远程 url 归一判兼容，stdio 不可部署出 warning）
- **skills_dir.go** — skill 目录发现（四候选目录 + frontmatter 解析 + bundle 体积 + lockfile 关联 + builtin 反向推断；user scope 仅作工作台 opt-in 候选不进默认 spec）
- **secrets_hint.go** — 凭证提示（只读 `.env.example` 类样板、只采 key 不读 value；MCP 反查关联；未识别的 key 标 UnknownSecretPurpose）
- **harness_signals.go** — HOME 目录 harness 痕迹探查 + 推荐 harness
- **harness_scope.go** — harness 感知层（claude/codex/cowork 三 harness 的 marker 门控 + user scope 路径模型），被 skills_dir 与 mcp_config 消费

## 设计约束

- **syft 走 CLI 进程而非 import library**；SyftRunner 注入让单测离线可跑
- **dev/prod 边界两步分离**：syft 产原始列表，dev_filter 按 manifest 重标——扫描与 manifest 解读解耦
- **隐私**：全本地跑；绝不读真 `.env`，只采样板 key 不读 value
- **错误容忍**：单文件/单源失败跳过不 fail 整个扫描；缺 manifest 静默跳过
- **PEP 503 归一**：pip dep 名比对统一 lowercase + `_`/`.` → `-`

## 依赖

- syft（外部 CLI 进程，不 import）· go-enry/v2 · moby/buildkit dockerfile parser · BurntSushi/toml

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
