# askdao-cli M4 Deploy Walkthrough

> 一次完整的 `askdao agent deploy` 端到端流程，从 cli build 到 conductor 后端落库的全部步骤。
> 来源：M4 Issue 1-7 上线后第一次 prod e2e 验证（2026-05-14）。
> 适用：第一次跑 cli 部署、debug、写新的 walkthrough/runbook 时参考。

---

## 0. 前置 env

```sh
# Mac 没 go，docker 跨编译 darwin/amd64
cd ~/workspace/askdao/askdao-cloud/askdao-cli
docker run --rm -v "$PWD":/src -w /src golang:1.26-alpine \
  sh -c 'GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o askdao github.com/askdao/askdao-cli/cmd/askdao'
# 产物：./askdao（Mach-O 64-bit x86_64, ~26 MB）
./askdao --version   # askdao-cli 0.1.0-dev
```

## 1. 拿 conductor session token

```sh
# 从 prod web 浏览器 DevTools → Application → Cookies → __Secure-better-auth.session_token
# auth middleware (app/auth/middleware.py:33) Bearer 不做 URL-decode，所以要先 unquote
# （%2F → /, %3D → =）。cookie 路径走 starlette 自动 decode，bearer 走的不一样。
RAW='<复制的 cookie value（含 %2F / %3D）>'
DECODED=$(printf '%s' "$RAW" | python3 -c 'import sys, urllib.parse; print(urllib.parse.unquote(sys.stdin.read()), end="")')
printf '%s' "$DECODED" > /tmp/askdao-token && chmod 600 /tmp/askdao-token

# sanity
curl -sS -H "Authorization: Bearer $(cat /tmp/askdao-token)" https://api.askdao.ai/api/v1/auth/me
# {"id":"<your_user_id>","name":"...","email":"...",...}
```

## 2. 建 agent 骨架

```sh
mkdir -p /tmp/m4-test && cd /tmp/m4-test
~/workspace/askdao/askdao-cloud/askdao-cli/askdao agent init my-agent
# ✓ Created agent skeleton at ./my-agent/
```

生成目录：

```
my-agent/
├── agent.yml      # 默认 spec：name=my-agent / model=claude-sonnet-4-6 / skills=[] / persona_file=persona.md
├── persona.md     # 只有 "# my-agent" 标题
├── resources/     # 空
└── skills/        # 空
```

## 3. 手写 custom skill

`agent init`（不带 `--auto`）不会自动建 skill 骨架；手 mkdir：

```sh
mkdir -p my-agent/skills/hello-skill
```

写 `my-agent/skills/hello-skill/SKILL.md` —— YAML frontmatter + body；`name` 受 conductor 正则 `^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$` 约束（`app/core/skill_format.py:17`）：

```markdown
---
name: hello-skill
description: M4 e2e verification skill — returns a fixed token to confirm the custom-skill pipeline (...) is fully wired.
---

# Hello Skill

When the user greets you / asks to verify the skill is loaded → reply with this token on its own line:

ASKDAO_HELLO_TOKEN_42

Then briefly acknowledge in one sentence.
```

## 4. 改 agent.yml 引用 skill + 写 system_prompt

**关键 gotcha**：conductor `AnthropicAdapter` 只读 `persona.system_prompt`，**不**读 `persona_file: persona.md`（`app/agents/adapters/anthropic_adapter.py:322`：`if spec.persona.system_prompt: agent_params["system"] = ...`）—— 系统提示要写进 yaml，不是 persona.md。

对 `my-agent/agent.yml` 做 3 处编辑：

```yaml
metadata:
  name: my-agent
  description: "M4 e2e test agent — verifies custom skill upload + invocation"   # ← 加
  version: 0.1.0
  ...

persona:
  ...
  system_prompt: |                                                                # ← 替换空串
    You are the M4 e2e test agent for AskDAO. You have one custom skill: `hello-skill`,
    which you should consult whenever the user greets you or asks you to verify the
    skill pipeline. Follow the skill's instructions exactly. Keep responses brief.

skills:                                                                            # ← 替换空数组
  - type: custom_local
    path: ./skills/hello-skill
```

## 5. 本地验证（不发请求）

```sh
~/workspace/askdao/askdao-cloud/askdao-cli/askdao agent show my-agent
```

预期输出（节选）：

```
PERSONA
  Name           : my-agent
  Description    : M4 e2e test agent — verifies custom skill upload + invocation
  Model class    : balanced
  Primary model  : claude-sonnet-4-6 (standard)  [anthropic]
  Persona file   : persona.md
  System prompt  : 243 chars

SKILLS  (1)
  ✓ hello-skill  (custom local)
    Path: ./skills/hello-skill
    Will be uploaded on deploy.

CAPABILITIES (Tool permissions)
  shell          ✓ allow
  filesystem     ✓ allow
  web            ✓ allow
  code_execution ✓ allow

RUNTIME  (Anthropic Managed Agents environment)
  Networking: limited
  Workdir: /app
```

只在本地解析 yaml；不调 conductor。

## 6. Deploy

```sh
export ASKDAO_CONDUCTOR_URL=https://api.askdao.ai
export ASKDAO_CONDUCTOR_TOKEN="$(cat /tmp/askdao-token)"

~/workspace/askdao/askdao-cloud/askdao-cli/askdao agent deploy \
  -dir my-agent \
  -bio "M4 e2e test KOL — automated deployment verification"
```

`-bio` 是为应对 conductor 端 `kol_join_mode IS NULL` 时返的 `409 kol_profile_required`：cli 收到这个 409 会用 `-bio` 值 PATCH `/api/v1/users/me/kol-profile`（`kol_join_mode=free` + `kol_bio=<-bio 值>`）然后重跑 POST。如果你 web 上已经设过 KOL 资料，cli 不会触发 409 path，`-bio` 闲置无害。

### cli 干的事（按 `cmd/askdao/deploy.go` + `internal/deploy/client.go`）

1. 读 `my-agent/agent.yml` **原始字节**（不 re-marshal —— 决议 9.5，保 KOL 编辑原样）
2. 枚举 `skills[*]` 里 `type==custom_local` 的项 → `internal/deploy.ZipDir()` 把 `./skills/hello-skill/` 压成 zip（顶层 `hello-skill/SKILL.md` + 其他文件）
3. `multipart/form-data` POST `https://api.askdao.ai/api/v1/cli/deploy` —— `Authorization: Bearer` + form fields：`agent_yml`（原文）+ 每个 skill 一个 zip part（field 名 `skill_zip__<skill_name>`）
4. 收 conductor 响应：`agent_id` / `anthropic_agent_id` / `anthropic_environment_id` / `group_id` / `group_link` / `skills[*]`（resolved `anthropic_skill_id` + `ov_content_uri`）/ `translation_report`
5. 若返 `409 kol_profile_required` → 用 `-bio` 值 PATCH `/api/v1/users/me/kol-profile` → 重跑 POST
6. 若 `translation_report.has_blocking()`（HIGH-severity warning）且无 `-force` → exit 1
7. 渲染结果到 stdout

### 预期输出（实测）

```
→ Reading my-agent/agent.yml
→ Packaged 1 custom skill(s): hello-skill
→ Deploying to https://api.askdao.ai (harness=anthropic_managed_agents) ...

✓ Deployed.

  agent_id:    agt_9e00e69c00814f12b9ed9e7f8ed9ffa6
  anthropic:   agent=agent_01SjtWCxSocX7NYvnASxChiz  environment=env_01FBW2gPURh3DvG3QeTDav7P
  group_id:    grp_822d2568a8c26274ee43cf01
  group link:  https://askdao.ai/k/<your_user_id>/g/grp_822d2568a8c26274ee43cf01

  Skills:
    • ./skills/hello-skill  →  managed skill_01QwbrxWDTZMwJTA3EVbniFn@1778687633093623
      (viking://resources/skills/private/<your_user_id>/skill_7cbf8e0249f5c94e/v0.1.0/)

  ⚠️  TRANSLATION WARNINGS  (anthropic_managed_agents)
    MEDIUM (0) · LOW (2)   [W] see all
```

### conductor 后端做了啥（响应里看不到，PG / Anthropic 验证）

- **`app/skills/sync.py:sync_skill_zip`** —— 把 skill zip 写 OV 真源 `viking://resources/skills/private/<owner>/<skill_id>/v0.1.0/`（OV 是真源）+ 同步上传到 Anthropic Managed Skills（beta `skills-2025-10-02`）→ 拿到 `skill_01Qwbrx...` + 版本（Anthropic 端整数戳）→ 写 `skill_registry` 表（scope_id=owner_id, skill_name, version, managed_skill_id, content_uri, visibility=private）（ADR-P15 三层 URI / P16 Shadowing / P18 OV→Managed 单向 sync）
- **`app/agents/adapters/anthropic_adapter.py`** —— `BETAS=["managed-agents-2026-04-01"]` + `_build_skills(resolved_skills=...)`（M4 Issue 2 改：`custom_local` 从 `resolved_skills` map 拿 `anthropic_skill_id` 回填到 `agent_params.skills`）→ Anthropic `client.beta.agents.environments.create(...)` 跟 `agents.create(...)` 都带 `betas=[...]` header
- **`app/api/cli.py:deploy_agent_spec`**（M4 Issue 3 端点）—— 事务里：写 `agent_spec`（含 resolved skills）+ 自动建 `Group`（`grp_<sha1(agent_id)[:24]>`，**M4 Issue 7：`ai_open_to_members=True`**）+ 写 owner `GroupMember(role="owner")` + 回填 `agent_spec.group_id`（alembic 024 加 unique index 防漂移）（ADR-P21）

---

## 验证 checklist（deploy 后）

### A. cli stdout

- `✓ Deployed.`
- `agent_id` / `anthropic agent/environment` / `group_id` / `group link` / skill resolved `anthropic_skill_id`+`viking://` URI 全有
- `translation_report` 没 HIGH（否则会 exit 1）

### B. conductor API（用 owner token）

```sh
TOK=$(cat /tmp/askdao-token)
curl -sS -H "Authorization: Bearer $TOK" https://api.askdao.ai/api/v1/agents
# 期望：新 agent name=my-agent / is_active=True / group_id=grp_... / managed_agent_id=agent_... /
#       skills[0] 含 anthropic_skill_id + anthropic_skill_version + ov_content_uri + path + type
```

### C. PG（ECS Exec 进 conductor 容器 + asyncpg inline）

```sql
-- 1. group with ai_open_to_members=TRUE（Issue 7）
SELECT id, agent_id, owner_id, ai_open_to_members, name
FROM "group" WHERE agent_id LIKE 'agt_%' ORDER BY id;

-- 2. owner group_member 行（Issue 3 事务）
SELECT group_id, user_id, role
FROM group_member WHERE group_id = '<group_id from above>';

-- 3. skill_registry 行（Issue 1 三层 URI 落地）
SELECT scope_id, skill_name, version, managed_skill_id, content_uri, visibility
FROM skill_registry WHERE scope_id = '<owner_user_id>';
```

inline asyncpg 模板（适配 ECS Exec session 提前 EOF 的限制 —— 短 SQL 优先）：

```sh
TASK_ID=$(aws ecs list-tasks --cluster askdao \
  --service-name AskdaoConductor-ServiceD69D759B-GGEaoluhSjsE \
  --region us-east-1 --query 'taskArns[0]' --output text | awk -F/ '{print $NF}')

SCRIPT='
import os, asyncio, asyncpg
async def main():
    conn = await asyncpg.connect(
        host=os.environ["DB_HOST"], port=int(os.environ["DB_PORT"]),
        user=os.environ["DB_USERNAME"], password=os.environ["DB_PASSWORD"],
        database="askdao",
    )
    rows = await conn.fetch(...)  # 你的 SQL
    for r in rows: print(dict(r))
    await conn.close()
asyncio.run(main())
'
ENCODED=$(printf '%s' "$SCRIPT" | base64)
aws ecs execute-command --cluster askdao --task "$TASK_ID" --container conductor \
  --interactive --command "sh -c \"echo $ENCODED | base64 -d | python3\"" \
  --region us-east-1 2>&1 | tail -30
```

---

## 全链路要点

- **cli 不直调 Anthropic**，全部经 conductor 中转（决策 9.1，design.md §9）
- **deploy 这一刻 cli 端只发 1-3 个 HTTP 请求**：1 个 multipart POST `/cli/deploy` + 可能 1 个 PATCH kol-profile + 可能 1 个 retry POST
- **OV 是 skill 真源**，Managed Skills 是单向 mirror；视性 `skill_registry` 表是查询索引；adapter 翻译时从 `resolved_skills` map 拿 anthropic_skill_id（ADR-P15/P16/P18）
- **Agent ↔ Group 一一绑**：deploy 自动建虚拟 Group + owner Membership + 回填 `agent_spec.group_id`（alembic 024 unique index 强约束）
- **`ai_open_to_members=True`** 是 deploy 默认（M4 Issue 7 + alembic 025）—— 订阅者能在 group context 跟 KOL agent 对话，否则 `check_group_or_raise` 挡 403

---

## 不在本 walkthrough 范围

- `agent init --auto --from <dir>`（带 L1-L4 流水线 + 交互审阅菜单的完整 KOL 引导流）
- ~~`agent deploy` 幂等 re-deploy（ADR-P19 P2 未实装）~~ **已实装 2026-05-19**：conductor `/cli/deploy` 加 lookup-then-create-or-update（dedup key = `(owner_id, yaml.metadata.name)` KOL scope，alembic 029 partial unique 背书）。命中既有 row → in-place `environments.update` + `agents.update`（乐观锁 retry once），复用 agent_id/group_id；不命中走原 create。cli 终端区分 `Created new agent.` vs `Updated existing agent (vN → vN+1).`。详 `docs/update-mode-handoff.md`
- Failure recovery（Anthropic API fail → `agent_spec.is_active=False` + 保留 Group，需手动 cleanup）
- 订阅者侧（M4 Issue 5 `subscribe()/cancel()` 自动 join/leave KOL Agent Group）+ Web group chat 路由（M4 Issue 6）

---

## 参考

- 设计真相源：[design.md](./design.md) §3 命令骨架 + §5 yaml schema + §9 决策记录
- HANDOFF：[HANDOFF.md](./HANDOFF.md) 当前 status + M4 各 Issue 实装位置
- ADR：[`harness-design/primitives/04-agent-deployment-pipeline.md`](../../harness-design/primitives/04-agent-deployment-pipeline.md)（ADR-P15/P16/P18/P19/P20/P21）
- conductor 端 entrypoint：`app/api/cli.py:deploy_agent_spec` / `app/skills/sync.py:sync_skill_zip` / `app/agents/adapters/anthropic_adapter.py`
