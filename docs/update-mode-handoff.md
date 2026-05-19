# askdao-cli + conductor · Update-Mode Deploy Handoff

> 新会话上手文档：把 `askdao agent deploy` 从"每次 create 新 Anthropic agent"改成"首次 create，后续 update"。
>
> Last updated: 2026-05-18 (handed off for next-day implementation)
>
> Author context: handed off after a long debug session for listenhub MCP + audio-proxy revert (PR mcp-gateway#5 reverted, #6 stateless kept). All PRs in conductor/mcp-gateway up to today are merged. **mcp-gateway 上次 deploy 未做** —— 哥说后续统一部署。

---

## TL;DR

| | |
|---|---|
| **问题** | `askdao agent deploy` 每次都调 `agents.create` → Anthropic 后台堆同名 agent (homework-spelling-mcp / `-01` / `-02` / `-03` / ...) 难调试 |
| **解** | conductor `/cli/deploy` 加 "lookup-then-create-or-update" 分支。dedup key = `(user_id, agent_yaml.metadata.name)` |
| **预计** | conductor 1 PR (~150 行 + 测试)，askdao-cli **可能** 0 改动（更可能 1 个小 PR 加 `--force-new` flag）|
| **前置已就绪** | Anthropic `agents.update(id, version, ...)` 支持 tools / system / mcp_servers 全量替换；in-place version 已实测（vault permission_policy fix 那次手工调过，version 1→2 成功）|

---

## Why now

哥手动管理 prod 时连续看到：

```
agent_011Az1n... homework-spelling-mcp
agent_01P4qSw... homework-spelling-mcp-01
agent_01LRDd6... homework-spelling-mcp-02
agent_01TMruT... homework-spelling-mcp-03
```

每个对应同一份 `askdao-agent.yml` 的一次微调 deploy。Anthropic 后台堆积 + 调试不知道当前 active 是哪个 + memory 浪费。

askdao-cli `docs/design.md` ADR-P19 ("re-deploy /diff") 标了 P2，今天升 P0。

---

## Design

### Dedup key

`(owner_id, agent_yaml.metadata.name)` — owner_id 来自 `RequestContext.user_id`，name 来自 yaml metadata。

**关键决策**：用 `name` 不用 yaml 里某个 stable ID。理由：
- 哥已经习惯改 `name` 字面值（"-01" / "-02"）来区分版本 — 这跟"我想覆盖同一个 agent"语义冲突，但**这就是哥决定要保留同一个 agent 的意图载体**。一个 agent.yml 改名 = KOL 显式说"我要新建一个"
- agent.yml 不需要新加字段，零 schema 改动
- askdao-cli 零改动（如果 KOL 不想换 agent 就别改 name）

### API contract changes

`POST /api/v1/cli/deploy`（conductor）行为变化：

```
当前：
  收 agent_yaml + skills
  → AnthropicAdapter.adapt(spec)
  → client.beta.agents.create(**agent_params)    ← always create
  → 写 agent_spec 表（新 PK）

改造后：
  收 agent_yaml + skills
  → AnthropicAdapter.adapt(spec)
  → query: SELECT * FROM agent_spec
           WHERE owner_id=ctx.user_id AND name=spec.metadata.name AND is_active=true
  → if hit:
      retrieve current Anthropic version: agents.retrieve(existing.managed_agent_id)
      agents.update(existing.managed_agent_id, version=current.version, **agent_params)
      UPDATE agent_spec SET ... WHERE pk = existing.pk   ← in-place
  → if miss:
      agents.create(**agent_params)
      INSERT agent_spec ...                              ← original path
  → return DeployResponse (agent_id 字段 = existing or new)
```

**响应不变** — `DeployResponse.anthropic_agent_id` 既可以是已存在的也可以是新建的，cli 端无需区分。

### Lookup query needs an index

`agent_spec` 表当前可能没有 `(owner_id, name, is_active)` 的索引。看一下 `app/models/db.py` 的 `AgentSpec` 类 + alembic migrations head；如果没有，加 alembic 029 加 partial unique index：

```sql
CREATE UNIQUE INDEX uq_agent_spec_owner_name_active
  ON agent_spec (owner_id, name)
  WHERE is_active = true;
```

partial 是因为历史 deactivated agent 可以同名共存。这条 unique 也防并发 race（两个 cli 同时 deploy 同名 agent 时第二个会被 PG 拒）。

### Version conflict handling

`agents.update(version=N)` 是乐观锁。如果两个 cli 同时 update 同一 agent：
1. 第一个 update 把 version 1→2
2. 第二个用 stale version=1 → Anthropic 返 400 "version mismatch"
3. conductor 应该 retry 一次：`agents.retrieve` 拿新 version → `agents.update` 用新值
4. 仍冲突 → 返 503 给 cli "concurrent deploy detected, retry"

修身期 n=1 KOL，并发概率 ~0，retry 一次就够。

### askdao-cli 改不改？

**默认不改**。yaml 改 name → 新建 agent；不改 name → update。语义自然。

可选加 `--force-new` flag 让 cli 显式要求新建（哥未来想 fork 一个新 agent 时用）。建议**第一版不做**，让 yaml metadata.name 作为唯一控制点 — 工作量更小验收更快。

---

## Implementation plan

### Step 1 (conductor) — adapter return shape

`app/agents/adapters/anthropic_adapter.py` 的 `AnthropicAdapter.adapt()` 当前返 `AnthropicAdapterOutput(environment_params, agent_params, vault_hints, translation_report)`。`agent_params` 是 `agents.create` body。

**check**：`agent_params` 是否能直接 spread 进 `agents.update(id, version, **params)`？看 update API 的字段集（`harness-design/claude-managed-agents-docs/api/python/managed-agents/agents/update.md`）—— `name` / `model` / `system` / `tools` / `mcp_servers` / `description` / `metadata` 都支持。**预计可以原样复用** `agent_params`。

若 `agent_params` 含 update 不支持的字段（比如某 create-only 字段），按需 filter。

### Step 2 (conductor) — agent_spec lookup helper

在 `app/agents/registry.py` 加：

```python
async def find_active_by_owner_name(
    db: AsyncSession, owner_id: str, name: str
) -> AgentSpec | None:
    result = await db.execute(
        select(AgentSpec)
        .where(
            AgentSpec.owner_id == owner_id,
            AgentSpec.name == name,
            AgentSpec.is_active == True,  # noqa
        )
        .limit(1)
    )
    return result.scalar_one_or_none()
```

（注意 `AgentSpec.name` 字段是否存在 — 也许叫 `display_name` 或别的；看 `db.py` 实际字段名。）

### Step 3 (conductor) — alembic 029 partial unique index

模板见 conductor `alembic/versions/028_user_managed_vault_id.py`（PR #65 写过的）。一行 `op.create_index(...)`。

### Step 4 (conductor) — /cli/deploy lookup-or-update branch

`app/api/cli.py` 的 `deploy_agent_spec` 当前流程（M4 ADR-P21 三件套）：
1. KOL profile 校验
2. sync skills to OV + Managed
3. `AnthropicAdapter.adapt(spec)`
4. `client.beta.agents.create(**output.agent_params)` ← 改这里
5. PG 事务建 agent_spec + Agent↔Group + owner GroupMembership
6. commit

改造：

```python
existing = await find_active_by_owner_name(db, ctx.user_id, spec.metadata.name)
if existing and existing.managed_agent_id:
    # update path
    current = await client.beta.agents.retrieve(existing.managed_agent_id)
    try:
        result = await client.beta.agents.update(
            existing.managed_agent_id,
            version=current.version,
            **output.agent_params,
        )
    except anthropic.BadRequestError as e:
        if "version" in str(e).lower():
            # one retry on version conflict
            current = await client.beta.agents.retrieve(existing.managed_agent_id)
            result = await client.beta.agents.update(
                existing.managed_agent_id,
                version=current.version,
                **output.agent_params,
            )
        else:
            raise
    # in-place PG update — keep agent_spec PK + group_id stable
    existing.managed_agent_version = str(result.version)
    existing.skills = [...]  # rewrite from output.resolved_skills
    # ... other mutable fields
    await db.commit()
    agent_id = existing.agent_id
    anthropic_agent_id = existing.managed_agent_id
else:
    # original create path (current code, mostly untouched)
    ...
```

注意：
- **不要新建** Agent↔Group / GroupMembership，复用旧的
- skills 同步可能也要 update path：之前 sync 过的 custom_local skill 可能 `skill_registry` 已经有 entry，需要 idempotent 处理（看 `app/skills/sync.py:sync_skill_zip` 是否本身就 idempotent — 大概率是，因为它走 `viking://resources/skills/...{version}/` 三层路径，version bump 时不冲突）

### Step 5 (conductor) — tests

`tests/test_cli_deploy.py`（或同名文件）现有 create 路径测试。加：
- test_deploy_update_existing_agent：先 create，再 deploy 同 name → 验证只调一次 create 一次 update，agent_id 不变
- test_deploy_update_version_conflict_retries：mock `agents.update` 第一次返 version mismatch 第二次成功
- test_deploy_create_different_name：deploy 不同 name → 新建 agent

### Step 6 (askdao-cli) — 可能 0 改动

CLI 端 `cmd/askdao/deploy.go` 的 `Client.Deploy()` 拿 `DeployResponse` 不关心 agent 是 new 还是 reuse。**预计零改动**。

可选：终端输出区分 "✓ Created new agent: agt_xxx" vs "✓ Updated existing agent: agt_xxx"，需要 conductor 在 DeployResponse 加 `created: bool` 字段。建议第一版不加，避免协议改动。

### Step 7 — GEB doc sync

按之前模式：
- conductor L1 head bump (alembic 028 → 029) + 当前 head footnote
- `app/agents/CLAUDE.md` L2 + `app/agents/adapters/CLAUDE.md` L2 描述 update path
- `app/api/CLAUDE.md` L2 描述 cli.py deploy_agent_spec 新分支
- `app/models/CLAUDE.md` L2 alembic 029 entry

### Step 8 — 部署

跟之前 PR 一样：
- conductor 走 GHA `Deploy Conductor` workflow（含 alembic 029 自动跑 + ECS rolling）
- askdao-cli 如果有改动：本地 `make build && install`（KOL 工具，不部署）

---

## Test plan (manual e2e)

哥的 `homework-spelling-mcp-03` 当前是 active，agent_id = `agent_01TMruTc8GTVLoKQdphMyfkm`（看 2026-05-19 02:15 创建时间）。验证步骤：

1. **不改 yaml metadata.name (`homework-spelling-mcp-03`)**，跑 `askdao agent deploy`
   - 期望：response 含 `agent_id = agent_01TMruTc8GTVLoKQdphMyfkm`（**同一个**）
   - Anthropic 后台 version 从当前值 +1（manually retrieve 看 `agents.versions.list`）
   - PG `agent_spec` 表行数不变，managed_agent_version 字段 bump

2. **改 yaml metadata.name = `homework-spelling-mcp-04`**，跑 `askdao agent deploy`
   - 期望：response 含 **新的** `agent_id = agt_...`
   - Anthropic 后台多一个 `homework-spelling-mcp-04` agent
   - PG `agent_spec` 表多一行

3. **并发 race**（可选，难重现）：两个 terminal 同时跑 `askdao agent deploy` 同 name → 至少一个成功，第二个要么 version retry 成功要么 503

---

## Risks / open questions

1. **agent_spec.name 字段是否存在？** 我没 grep 验证。如果叫 `display_name` 或没 dedicated 字段（agent_yaml 整体存 jsonb），lookup helper 要按实际字段名写。先 `grep -n "class AgentSpec" app/models/db.py` 看完整 ORM 定义
2. **managed_agent_version 字段是否存在？** alembic 020 之前加过 `managed_agent_version`，应该有。double check
3. **`agents.update` 不支持 update environment** — 如果 yaml `workspace.networking` / `packages` 改了，需要单独走 `environments.update`（哥的 vault unblock 那次手工 update 过 env，知道 API 支持）。本 PR 是否一并实现？建议**第一版只做 agent.update，environment 改动用户先重 deploy 新 agent**（简化）— 或同时实现 env update 让所有 yaml 变更 in-place 生效（更彻底但工作量翻倍）。**Recommend: 简化版本先做，env update 单独跟进**
4. **alembic 028 实测**：之前 PR conductor #65 加的 `user.managed_vault_id` 是否 prod 已 migrated？哥说"前面有变更再统一部署"暗示 mcp-gateway revert 还没部署，但 conductor PR #65 + #67 应该早就 prod 跑了（用 vault id 已经在跑了）。**确认 alembic head**：prod 是不是已经是 028？看 `Run alembic migrations` 步骤是 idempotent，新 head 029 不会重跑 028
5. **`UPDATE` 不动 group 关系**：现有 agent ↔ group 1:1 映射（M4 ADR-P21）— update 路径完全不动 group_id / GroupMember，复用旧绑定。这是对的（KOL 期望同一群继续聊同一 agent）

---

## Reference links / context anchors

- 上次 in-place agent.update 实证（vault permission_policy fix）：`agent_011Az1nrj4uTawM9CNcnm9iL` version 1 → 2 in-place 成功（PATCH /v1/agents/{id} → 200 + updated_at + version bumped）
- ADR-P19 "re-deploy /diff" 原始定义：`askdao-cli/docs/design.md` 搜 "ADR-P19"
- conductor `/cli/deploy` 当前实现：`app/api/cli.py:deploy_agent_spec`
- Anthropic agents.update API：`harness-design/claude-managed-agents-docs/api/python/managed-agents/agents/update.md`
- 之前 PR #65 (vault) 是 conductor 端类似规模的 PR，可参考代码组织和测试覆盖密度

---

## Pickup checklist (start here tomorrow)

- [x] 读完本 doc
- [x] `git -C ~/WorkSpace/askdao-cloud/askdao-cloud-conductor pull --ff-only && grep -n "class AgentSpec" app/models/db.py` 看 ORM 实际字段名（Step 1-2 前置）
- [x] 按 Step 1-7 顺序实施 — 与 cli 端字段消费拆成两 PR
- [x] 跑 `pytest tests/` 全过（conductor 779 测试 / cli `go test ./...` 全过）
- [x] 开 PR：[conductor#72](https://github.com/askdao/askdao-cloud-conductor/pull/72) + askdao-cli PR（见 commit `feature/agent-deploy-update-mode-response`）
- [ ] `aws ecs list-services --cluster askdao` 看 prod 当前 conductor task def revision，PR merge 后 `./infra/scripts/deploy.sh conductor` 部署 + 验 `/health/deep`
- [ ] manual e2e 跑 3 个 case（不改 name / 改 name / 改 env 字段）
- [ ] **第一次 update 成功的那一刻**：去 Anthropic 后台手动 archive 历史堆积的 `homework-spelling-mcp` / `-01` / `-02` / `-03`，留 `-03` 作为当前唯一 active（之前 update target）

---

## ✅ 实施完成 2026-05-19

**Conductor PR**：[#72 feat(deploy): in-place update mode for /cli/deploy (ADR-P19)](https://github.com/askdao/askdao-cloud-conductor/pull/72) — alembic 029 partial unique index + `registry.find_active_by_owner_name` helper + `/cli/deploy` lookup-or-update 分支 + `DeployResponse` 加 `created` / `previous_managed_version` + 5 个新测试。哥决策 **方案 B**：environment 一并 update（不是 handoff 推荐的简化方案 A），符合 trust-upstream-declaration 设计哲学；不加 `--force-new` flag（yaml `metadata.name` 是唯一控制点 + KOL scope dedup，语义自然完备）；加 `created` bool 字段（cli 终端 UX 区分 Created vs Updated v1→v2）。

**askdao-cli PR**：`feature/agent-deploy-update-mode-response` 分支 — Go `DeployResponse` struct 加 `Created` / `PreviousManagedVersion` 字段 + `printDeployResult` 区分输出 `Created new agent.` vs `Updated existing agent (vN → vN+1).` + GEB 文档同步（L1 CLAUDE.md / cmd/askdao/CLAUDE.md / internal/deploy/CLAUDE.md / design.md / HANDOFF.md / m4-deploy-walkthrough.md / 本文件）。

**Plan 文件**：`/Users/sunmu/.claude/plans/askdao-cli-docs-update-mode-handoff-md-sparkling-lynx.md`
