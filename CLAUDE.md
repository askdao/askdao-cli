# askdao-cli — KOL 本地 Agent 引导工具

> Go 单二进制 CLI。在 KOL 项目目录下扫描技术栈、推断框架、生成 Anthropic Managed Agents 配置草稿。
> AskDAO 体系内**唯一对外开源的子项目**（其他子仓全私有）—— 信任锚点。

技术栈：Go 1.26 + anchore/syft + go-enry/enry + moby/buildkit (dockerfile parser) + anthropic-sdk-go

---

<directory>
cmd/askdao/ - CLI 入口（main.go router）+ 五个用户命令（detect / bundle / agent init / agent show / agent deploy）
internal/ - 业务实现（types/ 双 schema · scanner/ 确定性扫描 · providers/ 框架推断 · pipeline/ L1-L4 编排 · recommender/ L4 LLM + /cli/recommend 客户端 · render/ 审阅卡片 · deploy/ conductor /cli/deploy 客户端 + skill zip 打包）
docs/ - 设计文档与调研报告（design.md 主稿 + HANDOFF.md + investigations/ 子目录两份 spike 报告）
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
askdao detect [path]                       # 打印 detection report（含 archetype + 部署清单），不创建 agent
askdao bundle [path]                       # 预览部署清单：上云会打包哪些文件 / 哪些 skill 走引用重装 / 哪些被排除
askdao agent init <name> [--auto]          # 创建 agent 目录骨架（--auto 跑 L1-L4 流水线 + 交互审阅）
askdao agent show <name> [--full|...]      # 渲染 agent spec（中等详情卡片 / 聚焦视图）
askdao agent deploy [--dir path] [--force] # 打包 custom skill + 经 Conductor /cli/deploy 推到 Anthropic Managed Agents
askdao agent validate                      # 校验 agent.yml（计划，未实装）
```

部署清单分类规则（确定性，零 LLM）：**在 `skills-lock.json` 里的 skill = 外部依赖 → 产引用，云端 `skill install` 重拉（npm ci 语义）；不在 lockfile 里的 = repo 原生 → 随包上传文件**。`node_modules` / `output*` / `input` / 编辑器&OS 垃圾 / `.env*` 等自动排除并给理由；`CLAUDE.md` / `skills-lock.json` / manifest 自动纳入。`.askdaoignore`（syntax 同 gitignore，`!` 反向纳入）做兜底覆盖。`askdao bundle --bundle-skill <name>` 把某外部 skill 强制改成随包上传。

---

## 与 askdao-cloud 的关系

- **独立仓库 + 独立发版**（按 memory `feedback_kol_local_tool_must_be_oss.md`：KOL 本地工具必须独立 repo + 开源 = 信任锚点）
- 与 `askdao-cloud-conductor` 共享 `AgentSpec` schema（CI diff 校验对齐，避免双写漂移）
- 设计文档：[`docs/design.md`](docs/design.md)（含两份 spike 报告：[`docs/investigations/syft-spike-for-askdao-cli.md`](docs/investigations/syft-spike-for-askdao-cli.md) + [`docs/investigations/nixpacks-provider-pattern.md`](docs/investigations/nixpacks-provider-pattern.md)）

---

## 状态

Phase 1（detect / agent init / show / deploy 骨架 + L1-L4 流水线 + render UX）已交付（issue #1-8）；后续加了 `bundle` 命令 + detection 的 archetype / 部署清单（lockfile-pinned skill 走引用重装、其余随包上传，零 LLM；issue #19）。M4 补完 `agent deploy` —— 接 conductor `POST /api/v1/cli/deploy`（`multipart/form-data` + custom skill zip 上传 + `409 kol_profile_required` 隐式补全 + HIGH-warning gating），见 [`docs/HANDOFF.md`](docs/HANDOFF.md)。设计真相源：[`docs/design.md`](docs/design.md)。

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
