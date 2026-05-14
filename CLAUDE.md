# askdao-cli — KOL 本地 Agent 引导工具

> Go 单二进制 CLI。在 KOL 项目目录下扫描技术栈、推断框架、生成 Anthropic Managed Agents 配置草稿。
> AskDAO 体系内**唯一对外开源的子项目**（其他子仓全私有）—— 信任锚点。

技术栈：Go 1.26 + anchore/syft + go-enry/enry + moby/buildkit (dockerfile parser) + anthropic-sdk-go

---

<directory>
cmd/askdao/ - CLI 入口（main.go router）+ 用户命令（auth login/status/logout · detect · bundle · agent init/show/deploy）
internal/ - 业务实现（auth/ 凭据 + Device Code Flow 客户端 · types/ 双 schema · scanner/ 确定性扫描 · providers/ 框架推断 · pipeline/ L1-L4 编排 · recommender/ L4 LLM + /cli/recommend 客户端 · render/ 审阅卡片 · deploy/ conductor /cli/deploy 客户端 + skill zip 打包）
docs/ - 设计文档与调研报告（design.md 主稿 + cli-auth-device-flow.md（OAuth 2.0 Device Code Flow）+ HANDOFF.md + investigations/ 子目录两份 spike 报告）
</directory>

<config>
go.mod - Go module 定义（github.com/askdao/askdao-cli）
Makefile - build / install / test / lint / clean 标准目标
LICENSE - MIT
.gitignore - Go 标准忽略规则
</config>

---

## 设计哲学

1. **本地隐私**：扫描全在用户机器跑，不上传任何文件内容（与云端方案的根本区别）
2. **确定性优先**：L1-L3 用工业标准库（syft/enry），LLM 只做最后一步推荐 + reason
3. **借车不造车**：syft 解决"manifest 都说了什么包"问题，nixpacks providers 解决"框架推断 + 系统包反向映射"问题，**askdao-cli 自己只写 25%**
4. **review-and-edit 而非 from-scratch**：KOL 体验是审阅推荐草稿，不是空白模板

---

## 命令骨架

```
askdao auth login [--server url] [--no-browser]  # OAuth 2.0 Device Code Flow（RFC 8628）+ 落 ~/.config/askdao/credentials.json (0600)
askdao auth status                         # 显示当前登录身份；未登录 exit 1
askdao auth logout                         # 删除本地 credentials（不撤销服务端 token，撤销走 web UI v2）
askdao detect [path]                       # 打印 detection report（含 archetype + 部署清单），不创建 agent
askdao bundle [path]                       # 预览部署清单：会上传哪些文件（skill 整目录 + origin tag）/ 哪些被排除
askdao agent init [name] [--auto]          # 在项目根创建 askdao-agent.yml + .askdao/（--auto 跑 L1-L4 流水线 + 交互审阅）
askdao agent show [flags] [--dir path]     # 渲染 agent spec（中等详情卡片 / 聚焦视图）
askdao agent deploy [--dir path] [--force] # 打包 custom skill 整目录 + 经 Conductor /cli/deploy 推到 Anthropic Managed Agents
askdao agent validate                      # 校验 askdao-agent.yml（计划，未实装）
```

**产物布局**（v0.7 起项目根扁平化 + `.askdao/` 工具空间）：
- `<root>/askdao-agent.yml` — KOL 唯一编辑对象（项目宣言文件，含 `persona.system_prompt` literal block 完整内容）
- `<root>/.askdao/recommendation.yml` — diff baseline（deploy 用作 KOL 改动检测）
- `<root>/.askdao/detection.json` — 确定性扫描结果（每次 init 重生成）

deploy 的 token 解析顺序：`ASKDAO_CONDUCTOR_TOKEN + ASKDAO_CONDUCTOR_URL` 同时设置 → 用 env（CI / 一次性覆盖）；否则读 `credentials.json`（`askdao auth login` 的产物）；都没有 → 报错并提示登录。两个 env 必须成对，单设一个明确报错（防止误配置静默降级）。设计稿 [`docs/cli-auth-device-flow.md`](docs/cli-auth-device-flow.md) §6.3。

**Skill 上传分类规则**（v0.7 起所有 custom skill 一律上传，Anthropic Managed Agents 无公共 registry —— 详见 `../harness-design/investigations/managed-agents-skill-installation.md`）：所有 `<skillDir>/<name>/` 目录递归打包（含 SKILL.md + scripts/ + assets/ + references/ 等所有子文件 + 二进制透传）。`<root>/.agents/skills/` / `<root>/.claude/skills/` / 自定义 path 的上级在 ZipDir 时被 `filepath.Rel` 切掉 —— **harness 中性 invariant**：Anthropic 端只看到 `<skillName>/SKILL.md` 形态（design.md §9.14）。vendored 与原生只是 bundle UI 的 inline origin tag（`skill (repo-native)` / `skill (vendored: <source> @ <hash>)`），不改变上传行为。`node_modules` / `output*` / `input` / 编辑器&OS 垃圾 / `.env*` 等自动排除并给理由；`CLAUDE.md` / `skills-lock.json` / manifest 自动纳入。`.askdaoignore`（syntax 同 gitignore，`!` 反向纳入）做兜底覆盖。

---

## 与 askdao-cloud 的关系

- **独立仓库 + 独立发版**（按 memory `feedback_kol_local_tool_must_be_oss.md`：KOL 本地工具必须独立 repo + 开源 = 信任锚点）
- 与 `askdao-cloud-conductor` 共享 `AgentSpec` schema（CI diff 校验对齐，避免双写漂移）
- 设计文档：[`docs/design.md`](docs/design.md)（含两份 spike 报告：[`docs/investigations/syft-spike-for-askdao-cli.md`](docs/investigations/syft-spike-for-askdao-cli.md) + [`docs/investigations/nixpacks-provider-pattern.md`](docs/investigations/nixpacks-provider-pattern.md)）

---

## 状态

Phase 1（detect / agent init / show / deploy 骨架 + L1-L4 流水线 + render UX）已交付（issue #1-8）；后续加了 `bundle` 命令 + detection 的 archetype / 部署清单（lockfile-pinned skill 走引用重装、其余随包上传，零 LLM；issue #19）。M4 补完 `agent deploy` —— 接 conductor `POST /api/v1/cli/deploy`（`multipart/form-data` + custom skill zip 上传 + `409 kol_profile_required` 隐式补全 + HIGH-warning gating），见 [`docs/HANDOFF.md`](docs/HANDOFF.md)。设计真相源：[`docs/design.md`](docs/design.md)。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
