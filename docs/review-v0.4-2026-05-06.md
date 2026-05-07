# Design Review v0.4 · 2026-05-06

> 触发：哥追问"v0.3 是否考虑了 Dockerfile 常用内容和写法的兼容？"
> v0.3 在 Dockerfile 兼容上**覆盖不够**：只在 detection.json 识别 + 抽 base_image，
> yaml workspace 块只能装 packages，丢失了多阶段构建、自定义 base image、
> 复杂 RUN 链、USER 切换、EXPOSE 端口等常见模式。
>
> Reviewer: Sam
> Decision: 选项 B（中等修订）；不做 GPU 声明；先改文档，再开 Phase 1
> Outcome: design.md 升 v0.4，workspace 加 5 个字段 + translation report

---

## 1. v0.3 当前 Dockerfile 处理能力（事实）

**只能做到**：
- detection.json `detected_dockerfile` 字段：识别存在 + 抽 `base_image` + `exposed_ports`
- yaml `workspace.packages.{pip,apt,npm,...}` ：声明式包列表

**仅此而已**。本质上 v0.3 假设"Dockerfile 里只是装包"，把其他内容当噪音丢掉。这对简单项目够用，但工程师社区项目大多 Dockerfile 复杂得多。

---

## 2. v0.3 无法表达的 Dockerfile 模式（7 项）

按出现频率排序：

### 2.1 多阶段构建（90% 后端 Dockerfile 在用）
```dockerfile
FROM node:20 AS builder
COPY . .
RUN npm ci && npm run build

FROM node:20-slim
COPY --from=builder /app/dist /app
CMD ["node", "/app/server.js"]
```
v0.3 完全无法表达 builder/runner 分离。

### 2.2 自定义基础镜像（GPU / 特殊预装）
- `pytorch/pytorch:2.1.0-cuda12.1-cudnn8-runtime`
- `nvcr.io/nvidia/pytorch:24.01-py3`
- 公司私有 registry 定制镜像

v0.3 没有 `base_image` 字段。

### 2.3 复杂 RUN 链（系统级编译/配置）
```dockerfile
RUN apt-get update && apt-get install -y \
      build-essential libssl-dev \
    && curl -sSL https://install.python-poetry.org | python3 - \
    && rm -rf /var/lib/apt/lists/*

RUN git clone https://github.com/foo/native-lib && \
    cd native-lib && cmake . && make install
```
v0.3 的 `packages.apt` 只能列包名，无法表达**命令式编译步骤**。

### 2.4 USER / 用户切换
```dockerfile
RUN useradd -m -u 1000 appuser
USER appuser
WORKDIR /home/appuser
```
v0.3 没有 user/group 字段。OpenAI SDK Manifest 实际有 `users / groups`。

### 2.5 EXPOSE 端口 + ENTRYPOINT/CMD
v0.3 没有 ports 字段。Web service 的"端口预览"能力（OpenAI SDK 支持）完全无法表达。

### 2.6 Build args / build-time secrets
```dockerfile
ARG GITHUB_TOKEN
RUN --mount=type=secret,id=mypip pip install --extra-index-url ...
```
v0.3 vault_hints 只管运行时，不管构建时。

### 2.7 WORKDIR / VOLUME / LABEL / HEALTHCHECK
低优先级但常见。v0.3 完全没字段。

---

## 3. Anthropic vs OpenAI 在 Dockerfile 维度的承载能力

**关键事实**：Anthropic Managed Agents 有硬性限制，许多 Dockerfile 模式**根本不支持**。

| Dockerfile 模式 | Anthropic Managed Agents | OpenAI Agents SDK |
|----------------|-------------------------|------------------|
| 自定义 base image | ❌ **完全不支持**（强制 `config.type: cloud`） | ✅ DockerSandboxClient `image` 参数 |
| 多阶段构建 | ❌ 不支持 | ⚠️ 用户自己 build 后塞 image |
| GPU / CUDA | ❌ 不支持 | ⚠️ 取决于 SandboxClient（Modal / Daytona 可能支持） |
| 复杂 RUN 链 | ❌ 仅声明式 packages | ✅ Manifest setup commands |
| USER 切换 | ❌ 不支持 | ✅ `Manifest.users / groups` |
| EXPOSE 端口预览 | ❌ 不支持 | ✅ exposed ports capability |
| Build-time secrets | ❌ 不支持 | ⚠️ provider-specific |
| WORKDIR | ❌ 隐含 | ✅ Manifest 隐含 |

### 关键洞察

**Anthropic Managed Agents 故意做得很薄**。askdao-cli 的中间格式无论怎么设计，对 Anthropic 那侧都注定承载不全 —— 这是 **adapter 层的固有损失**，不是设计失误。

但 OpenAI SDK 那侧承载力强很多。中间格式**应该按 OpenAI 的上限留位**，让 Anthropic adapter 输出 translation report 警告即可。

---

## 4. 三选项对比 + 选定方案

### 选项 A · 最小修订（仅承认局限）
- detection.json 完整 dump Dockerfile 内容
- yaml 不变，只在 provenance 加警告
- 优点：1-2 天完成
- 缺点：复杂 Dockerfile 项目体验差，OpenAI adapter 也用不上

### 选项 B · 中等修订 ⭐ 选定（哥确认）
为 OpenAI adapter 留位，Anthropic adapter 主动忽略：

```yaml
workspace:
  base_image: python:3.12-slim         # 🆕 OpenAI 用，Anthropic 忽略
  base_image_hint: python_3_12         # 🆕 Anthropic 推 packages family
  packages: { pip, apt, ... }
  setup_commands:                      # 🆕 命令式补充
    - "git clone ... && make install"
  users:                               # 🆕 OpenAI 用
    - { name: appuser, uid: 1000 }
  workdir: /app                        # 🆕 两边都用
  exposed_ports: [8000]                # 🆕 OpenAI 用做 preview
  startup_command: null                # 留位
  mounts: []
  networking: {...}
  environment_vars: {...}
```

**5 个新字段**（base_image / setup_commands / users / workdir / exposed_ports），其余次要字段 Phase 3 再加。

### 选项 C · 完整修订（直接挂 Dockerfile）
```yaml
workspace:
  dockerfile:
    path: ./Dockerfile
    target_stage: runner
    build_args: { ... }
```
- 优点：完全保真
- 缺点：Anthropic adapter 用不上；KOL 心智复杂；askdao-cli 端要完整解析整个 Dockerfile

---

## 5. 选定方案细节

### 5.1 v0.4 加的 5 个 workspace 字段

| 字段 | 用途 | Anthropic adapter 行为 | OpenAI adapter 行为 |
|-----|------|----------------------|--------------------|
| `base_image` | 自定义镜像名 | ❌ 忽略 + warn | ✅ DockerSandboxClient `image=` 参数 |
| `setup_commands` | 命令式编译/配置步骤 | ❌ 忽略 + warn（packages 兜底） | ✅ 进 Manifest setup phase |
| `users` | OS users / groups 创建 | ❌ 忽略 + warn | ✅ `Manifest.users / groups` |
| `workdir` | 工作目录 | ⚠️ 隐含支持（默认 /workspace）| ✅ Manifest 隐含 |
| `exposed_ports` | 端口暴露 | ❌ 忽略 + warn | ✅ exposed ports capability |

### 5.2 不加的字段（哥已确认）

- ❌ `resources.gpu` —— 不跑 ML 类任务，不需要 GPU 声明
- ❌ `dockerfile.path` 直挂 Dockerfile —— 选项 C 思路，Phase 3 再考虑
- ❌ `build_args` / build-time secrets —— v0.3 / v0.4 范围外，未来按需
- ❌ `volumes` / `healthcheck` / `labels` —— Phase 3 按需

### 5.3 detection.json 的 detected_dockerfile 升级

v0.3 字段（保留）：
- `exists` / `path` / `base_image` / `exposed_ports`

v0.4 新增：
- `stages: []` —— 多阶段列表，含每阶段 `from / as / commands`
- `final_stage_name` —— `--target` 默认值（多阶段时选最后一段）
- `run_commands: []` —— 所有 RUN 命令的列表（含完整命令字符串）
- `users: []` —— `useradd / USER` 抽出的用户
- `workdir` —— 最终 WORKDIR
- `env_vars: { ... }` —— ENV 抽出的非 secret 环境变量
- `cmd` / `entrypoint` —— CMD / ENTRYPOINT（参考用，agent runtime 不需要）
- `build_args: []` —— ARG 列表
- `extracted_apt_packages: []` —— 从 RUN apt-get install 抽出的 apt 包名（喂 workspace.packages.apt）
- `extracted_pip_packages: []` —— 从 RUN pip install 抽出的 pip 包名
- `extracted_setup_commands: []` —— 无法归到 packages 的 RUN 命令（喂 workspace.setup_commands）

### 5.4 Translation report

每次 `askdao agent deploy` 时，conductor 端 adapter 输出 translation report，格式：

```json
{
  "harness": "anthropic_managed_agents",
  "translation_warnings": [
    {
      "field": "workspace.base_image",
      "value": "pytorch/pytorch:2.1.0-cuda12.1-cudnn8-runtime",
      "action": "ignored",
      "reason": "Anthropic Managed Agents uses fixed cloud container image; custom base image not supported",
      "severity": "high"
    },
    {
      "field": "workspace.setup_commands",
      "count": 3,
      "action": "ignored",
      "reason": "Anthropic Managed Agents only supports declarative packages; imperative setup commands cannot run",
      "severity": "high",
      "fallback_attempted": "extracted apt/pip names from commands and merged into workspace.packages"
    },
    {
      "field": "workspace.users",
      "count": 1,
      "action": "ignored",
      "reason": "Anthropic Managed Agents runs as fixed user",
      "severity": "medium"
    },
    {
      "field": "workspace.exposed_ports",
      "value": [8000],
      "action": "ignored",
      "reason": "Anthropic Managed Agents does not support port preview; use OpenAI SDK + DockerSandboxClient for that",
      "severity": "low"
    }
  ]
}
```

KOL 看到 high severity warning 时可以选择：
- 忽略警告继续 deploy（接受降级）
- 切换到 `--harness openai_agents_sdk`（Phase 2 起可用）
- 修改 yaml 删掉这些字段

---

## 6. Phase 切分（按哥确认）

### Phase 1（即将做）
- ✅ workspace 加 5 字段（base_image / setup_commands / users / workdir / exposed_ports）
- ✅ Anthropic adapter 输出 translation_report（高 severity 字段警告）
- ✅ detection.json `detected_dockerfile` 扩展为完整 Dockerfile AST
- ✅ askdao-cli 端 `internal/scanner/dockerfile.go` 升级（80 → 200 行）
- ✅ AnthropicAdapter 实现 `extracted_apt_packages` 等字段的兜底合并

### Phase 2（OpenAIAdapter 启用时）
- ⏳ OpenAI adapter 真正消费 base_image / setup_commands / users / workdir / exposed_ports
- ⏳ exposed_ports → SandboxClient port preview 能力

### Phase 3（中长期，按需）
- ⏳ `workspace.dockerfile` 直挂能力（选项 C，仅 OpenAI / Docker SandboxClient 路径）
- ⏳ multi-stage build target_stage 选择
- ⏳ build_args / build-time secrets
- ⏳ volumes / healthcheck / labels

---

## 7. 工作安排（哥确认）

> "1. 先把文档修正了；2. 修正以后，我们再开始 Phase 1 的开发"

意味着：
1. **本次 v0.4 文档修订** = 优先动作
2. v0.4 之后即开 Phase 1（不再有 v0.5 大改的预期）
3. 任何 Phase 1 实施过程中浮现的问题进 v0.5 / v0.6 增量修订

---

## 8. 修订动作清单（落进 design.md）

- [x] 顶部 ChangeLog 加 v0.4 章节
- [x] §4 detection.json `detected_dockerfile` 字段扩展（含完整 Dockerfile AST + extracted_*）
- [x] §5 yaml workspace 块加 5 字段 + translation report 子章节
- [x] §6 askdao-cli 端 `dockerfile.go` 工程量升 80→200 行
- [x] §6.2 AnthropicAdapter 加 translation_report 输出 + extracted_* 字段兜底合并逻辑（~100 行 Python）
- [x] §7 Phase 1 / Phase 2 / Phase 3 的工作描述微调
- [x] §9 决策记录加 9.7（Dockerfile 兼容范围）+ 9.8（不做 GPU 声明）
- [x] §10 落地路径加：plan/06 §5 yaml schema 同步加这 5 字段

---

## 9. 决策记录

### 9.7 Dockerfile 兼容采用选项 B（5 字段中等修订）✅ 已定（v0.4）

- v0.3 仅识别 + 抽 base_image，丢失多阶段、自定义镜像、复杂 RUN、USER 等模式
- v0.4 决定：workspace 加 5 字段（base_image / setup_commands / users / workdir / exposed_ports），Anthropic adapter 输出 translation_report，OpenAI adapter Phase 2 真正消费
- 理由：Anthropic 注定承载不全，但中间格式应按 OpenAI 上限留位；选项 C 直挂 Dockerfile 心智复杂度太高，留 Phase 3
- 影响：design.md §4 + §5 + §6 + §7 修订；plan/06 §5 同步

### 9.8 不做 GPU 资源声明 ✅ 已定（v0.4）

- 哥决定：AskDAO 不跑 ML 类任务，不需要 `workspace.resources.gpu` 字段
- 理由：聚焦 KOL 知识/服务场景，GPU 是远期事
- 影响：v0.4 yaml 不加 gpu 字段；Phase 3 重新评估时再讨论

---

## 10. 后续待讨论项

1. AnthropicAdapter 的 `extracted_apt_packages` 兜底合并规则：是否要去重（用户 yaml 里已有的 apt 包不重复加）？冲突处理（用户写 `nginx==1.20`，Dockerfile RUN 装 `nginx==1.18`，谁优先）？
2. OpenAI adapter 接 `setup_commands` 时是否要做命令安全检查（拒绝 `rm -rf /` 之类）？还是信任 KOL？
3. `workspace.workdir` 默认值：Anthropic 是 `/workspace`，OpenAI Manifest 是 workspace-relative —— 中间格式默认值应该是什么？
