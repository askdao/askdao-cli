# askdao-cli

> Local CLI for AskDAO — turn a project directory into a deployable AI agent: it
> scans your tech stack, skills, and MCP servers, then lets you review, edit, and
> deploy the agent in a local web studio.

**Status:** Pre-alpha — `agent edit` (local web studio) + `agent deploy` implemented; APIs may still change.

---

## What it does

Run one command in your project directory:

```bash
$ askdao agent edit

→ Scanning ./ (tech stack · skills · MCP servers · required secrets) ...
→ Generating a draft agent spec ...
→ Agent studio at http://127.0.0.1:53017/   (opening your browser)
```

Your browser opens a local **agent studio** — a 4-step wizard that walks you
through everything, instead of leaving you to fill in a blank YAML template:

1. **Identity** — name, description, category, visibility, and a theme color
2. **Persona** — model class + system prompt
3. **Skills & Tools** — tick the skills / MCP servers / secrets to include
   (project-local and your global `~/.claude` ones, grouped by scope)
4. **Review** — confirm the exact items, then **Deploy** (or Save as a draft)

Everything runs locally (`127.0.0.1`); your files are scanned on your machine
and never uploaded. Editing is a review-and-tick step, not a from-scratch one.

Prefer the terminal, CI, or hand-editing? `askdao agent edit --no-ui` writes the
draft `askdao-agent.yml` and exits; you can edit it and `askdao agent deploy` directly:

```bash
$ export ASKDAO_CONDUCTOR_URL=https://api.askdao.ai
$ export ASKDAO_CONDUCTOR_TOKEN=<token>      # or just: askdao auth login
$ askdao agent deploy

→ Reading askdao-agent.yml
→ Packaged 1 custom skill(s)
→ Deploying to https://api.askdao.ai (harness=anthropic_managed_agents) ...
✓ Created new agent.
  agent_id:   agt_…   group link: https://askdao.ai/k/<you>/g/grp_…
```

(Re-deploying the same `metadata.name` updates the agent in place; `--force` overrides HIGH-severity translation warnings.)

## Why

Configuring a Managed Agent runtime — picking the model, listing OS packages,
choosing which skills travel with the agent, writing the system prompt — is
high-friction. `askdao-cli` turns it into review-and-edit by leveraging:

- [`anchore/syft`](https://github.com/anchore/syft) for deterministic dependency scanning (30+ package managers)
- [`go-enry/enry`](https://github.com/go-enry/enry) for byte-level language detection (~500 languages)
- Provider patterns ported from [`railwayapp/nixpacks`](https://github.com/railwayapp/nixpacks) for framework inference and reverse mapping (e.g. `psycopg → libpq-dev`)
- **harness-aware, dual-scope skill/MCP discovery** — a `.claude/` project pulls in both project-local and your global `~/.claude` skills (each tagged by scope + harness), so you choose exactly what ships instead of bundling everything
- the **LLM only for the final fuzzy step** (recommendation + reasoning, routed through Conductor), never for scanning

## Install

```bash
# from source
git clone https://github.com/askdao/askdao-cli.git
cd askdao-cli && make install
```

Pre-built binaries will be published once the API stabilizes.

## Commands

| Command | Description |
|---------|-------------|
| `askdao auth login [--server url] [--no-browser]` | Browser-bound login (OAuth 2.0 Device Code Flow, RFC 8628) → `~/.config/askdao/credentials.json` |
| `askdao auth status` / `logout` | Show the current identity / clear local credentials |
| `askdao agent edit [--dir path] [--no-ui] [--force]` | Scan the project (or load an existing `askdao-agent.yml`) and open the local web studio to review / edit / deploy. `--no-ui` writes a draft and exits |
| `askdao agent deploy [--dir path] [--harness id] [--force]` | Package custom skills + push `askdao-agent.yml` to Anthropic Managed Agents via Conductor |

`agent deploy` resolves `ASKDAO_CONDUCTOR_URL` + `ASKDAO_CONDUCTOR_TOKEN` from the
environment (both required as a pair) or from `credentials.json` written by
`askdao auth login`.

See [docs/HANDOFF.md](docs/HANDOFF.md) for the current status and [docs/design.md](docs/design.md) for the full design.

## Project layout

```
askdao-cli/
├── cmd/askdao/             # CLI entry + commands (auth · agent edit/deploy)
├── internal/
│   ├── scanner/            # syft / enry / dockerfile + harness-aware dual-scope skill/MCP scan
│   ├── providers/          # nixpacks-style framework providers
│   ├── pipeline/           # L1-L4 orchestration (scan → providers → policy → LLM)
│   ├── recommender/        # policy heuristics + conductor /cli/recommend client
│   ├── render/             # CLI render helpers (deploy diff / translation warnings)
│   ├── webstudio/          # local web studio — 127.0.0.1 server + go:embed single-page wizard
│   ├── deploy/             # conductor /cli/deploy client + skill-dir zip packaging
│   ├── auth/               # OAuth 2.0 Device Code Flow + credentials
│   └── types/              # detection.json + askdao-agent.yml schemas
├── docs/                   # design.md + HANDOFF.md + investigations/
└── Makefile
```

## Status

This is part of the [AskDAO](https://askdao.ai) ecosystem — a platform that helps
subject-matter experts (KOLs) deliver AI-assisted services to their audience
across IM channels.

`askdao-cli` is the local, open-source tool KOLs run on their own machine to
define and deploy their agents.

## License

MIT — see [LICENSE](LICENSE).
