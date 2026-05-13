# askdao-cli

> Local CLI for AskDAO — bootstrap AI agents from your project directory by auto-detecting tech stack and generating (then deploying) Anthropic Managed Agents config.

**Status:** Pre-alpha — Phase 1 implemented (detect / init / show / deploy); APIs may still change.

---

## What it does

Run one command in your project directory and get a ready-to-review agent specification:

```bash
$ askdao agent init my-agent --auto

→ Scanning ./ ...
→ Detected: Python 3.12 + FastAPI + SQLAlchemy + PostgreSQL
→ Detected 28 production deps (filtered out 14 dev deps)
→ Inferred system packages: libpq-dev, gcc, libjpeg-dev
→ Calling LLM for system_prompt + reasoning ...

✓ Generated my-agent/agent.yml (draft)
✓ Saved my-agent/.askdao/detection.json (provenance)
```

Instead of staring at a blank template, you start by reviewing a draft.

Once you're happy with `agent.yml` (and have written any `skills/<name>/SKILL.md`), deploy it:

```bash
$ export ASKDAO_CONDUCTOR_URL=https://api.askdao.ai
$ export ASKDAO_CONDUCTOR_TOKEN=<your session token>
$ askdao agent deploy --dir my-agent

→ Reading my-agent/agent.yml
→ Packaged 1 custom skill(s): my-skill
→ Deploying to https://api.askdao.ai (harness=anthropic_managed_agents) ...

⚠  The conductor needs your KOL profile filled in before deploying.
   KOL bio (one line, optional — press Enter to skip): I build research agents
→ Setting KOL profile: kol_join_mode=free, bio="I build research agents"
✓ KOL profile saved.
→ Retrying deploy ...

✓ Deployed.

  agent_id:    agt_…
  anthropic:   agent=agent_…  environment=env_…
  group_id:    grp_…
  group link:  https://askdao.ai/k/<you>/g/grp_…

  Skills:
    • my-skill  →  managed skill_…@v0.1.0  (viking://resources/skills/private/<you>/skill_…/v0.1.0/)
```

(Pass `--bio "…"` to skip the prompt; `--force` to deploy despite HIGH-severity translation warnings.)

## Why

Configuring a Managed Agent runtime — picking the right model, listing OS packages, writing system prompts — is high-friction. `askdao-cli` reduces that to a review-and-edit step by leveraging:

- [`anchore/syft`](https://github.com/anchore/syft) for deterministic dependency scanning (30+ package managers)
- [`go-enry/enry`](https://github.com/go-enry/enry) for byte-level language detection (~500 languages)
- Provider patterns ported from [`railwayapp/nixpacks`](https://github.com/railwayapp/nixpacks) for framework inference and reverse mapping (e.g. `psycopg → libpq-dev`)
- LLM only for the final fuzzy step (recommendation + reasoning), not for scanning

## Install

```bash
# from source
git clone https://github.com/askdao/askdao-cli.git
cd askdao-cli && make install
```

Pre-built binaries will be published once the API stabilizes.

## Commands

| Command | Status | Description |
|---------|--------|-------------|
| `askdao detect [path]` | ready | Print the detection report without creating an agent |
| `askdao agent init <name> [--auto]` | ready | Create an agent skeleton; `--auto` scans the project and pre-fills `agent.yml` |
| `askdao agent show <name>` | ready | Render an agent's spec (mid-density card, or `--full` / focused views) |
| `askdao agent deploy [--dir path] [--force] [--bio …]` | ready | Package custom skills + push the agent (and an environment) to Anthropic Managed Agents via Conductor |
| `askdao agent validate` | planned | Validate `agent.yml` schema |
| `askdao agent regenerate` | planned | Re-scan and diff against existing yaml |

`agent deploy` reads `ASKDAO_CONDUCTOR_URL` + `ASKDAO_CONDUCTOR_TOKEN` (both required); `agent init --auto` reads `ASKDAO_CONDUCTOR_URL` (optional — falls back to a local mock recommender).

See [docs/design.md](docs/design.md) for the full design.

## Project layout

```
askdao-cli/
├── cmd/askdao/             # CLI entry point + commands (detect / agent init|show|deploy)
├── internal/
│   ├── scanner/            # syft / enry / dockerfile parser wrappers
│   ├── providers/          # nixpacks-style framework providers
│   ├── pipeline/           # L1-L4 orchestration (scan → providers → policy → LLM)
│   ├── recommender/        # policy heuristics + conductor /cli/recommend client
│   ├── render/             # KOL review UX (mid-density card, diffs, warnings)
│   ├── deploy/             # conductor /cli/deploy client + skill-dir zip packaging
│   └── types/              # detection.json + agent.yml schemas
├── docs/                   # design.md (main draft) + HANDOFF.md + investigations/
└── Makefile
```

## Status

This is part of the [AskDAO](https://askdao.ai) ecosystem — a platform that helps subject-matter experts (KOLs) deliver AI-assisted services to their audience across IM channels.

`askdao-cli` is the local tool KOLs run on their own machine to define and deploy their agents.

## License

MIT — see [LICENSE](LICENSE).
