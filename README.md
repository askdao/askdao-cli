# askdao-cli

> Local CLI for AskDAO — turn a project directory into a deployable AI agent: scan the tech stack, skills, and MCP servers, then review, edit, and deploy the agent in a **local web studio**, all the way to Anthropic Managed Agents through Conductor.

**Status:** v0.8 — `auth login` + `agent edit` (local web studio) + `agent deploy` shipping; APIs may still change before 1.0.

> 🛡 **Trust anchor.** `askdao-cli` is the **only open-source piece** of the AskDAO platform. All project scanning runs locally — file contents never leave your machine; only the resulting `detection.json` summary (no source code, no env values) is sent to Conductor when you run `agent edit`. Auth uses OAuth 2.0 Device Code Flow (RFC 8628); CLI tokens are SHA-256 hashed server-side.

---

## Quickstart

```bash
# 1. Install (Go 1.26+)
git clone https://github.com/askdao/askdao-cli.git
cd askdao-cli && make install

# 2. Log in (browser-bound, one-time)
askdao auth login
#   Your code: HJKL-4892
#   Open https://askdao.ai/cli/auth?code=HJKL-4892
#   ✓ Logged in as you@example.com. Token saved to ~/.config/askdao/credentials.json.

# 3. cd into the project you want to turn into an agent
cd ~/WorkSpace/my-spelling-pipeline

# 4. Scan + open the local web studio to review, edit, and deploy
askdao agent edit
```

That's the full happy path — the studio walks you through a 4-step wizard and deploys at the end. Prefer the terminal/CI? `askdao agent edit --no-ui` writes a draft `askdao-agent.yml` you can hand-edit, then `askdao agent deploy`.

---

## What it does

Run one command in your project root:

```bash
$ askdao agent edit

→ Scanning ./ (tech stack · skills · MCP servers · required secrets) ...
→ Generating a draft agent spec ...
→ Agent studio at http://127.0.0.1:53017/   (opening your browser)
```

Your browser opens a local **agent studio** — a 4-step wizard that walks you through everything, instead of leaving you to fill in a blank YAML template:

1. **Identity** — name, description, category, visibility, and a theme color (the brand color subscribers see on the agent's group page)
2. **Persona** — model class + system prompt
3. **Skills & Tools** — tick the skills / MCP servers / secrets to include, across two scopes: project-local (`<root>/.claude/skills`) and your global `~/.claude` ones, each tagged by scope + harness. Only what you tick travels with the agent
4. **Review** — confirm the exact selected items by name, then **Deploy** (or Save as a draft)

Each AI-recommended field carries an inline confidence badge — hover it for the reasoning behind the default, so you decide what to accept while you edit.

Files written, all at your project root:

```
your-project/
├── askdao-agent.yml          ← your single edit target (commit this)
├── .askdao/
│   ├── recommendation.yml    ← diff baseline (commit for review history)
│   └── detection.json        ← deterministic scan output (gitignore optional)
├── .claude/skills/           ← your existing skill tree (untouched)
└── ...
```

Everything runs locally (`127.0.0.1`); your files are scanned on your machine and never uploaded. The terminal path (`--no-ui` + `agent deploy`) prints a diff against the recommendation baseline and the full deploy result:

```bash
$ askdao agent deploy
→ Reading askdao-agent.yml
→ You modified 1 field(s) since the last recommendation:
    persona.system_prompt:
      - You are a helpful homework assistant...
      + You are a helpful homework assistant specialising in elementary spelling...

→ Packaged 4 custom skill(s): listenhub, listenhub-cli, spelling-homework-generator, tts
→ Deploying to https://api.askdao.ai (harness=anthropic_managed_agents) —
  uploading 4 skill(s) + creating Anthropic agent/environment, typically 15-25s ...

✓ Created new agent.

  agent_id:    agt_aa4c3329...
  anthropic:   agent=agent_01G8...  environment=env_014X...
  group_id:    grp_c1aeb63d...
  group link:  https://askdao.ai/k/<you>/g/grp_c1aeb63d...

  Skills:
    • .claude/skills/listenhub   →  managed skill_014rn2…@1778…  (viking://resources/skills/private/…)
    • .claude/skills/tts          →  managed skill_0142c2…@1778…

✓ Deploy complete. Open https://askdao.ai/k/<you>/g/grp_c1aeb63d... to chat.
```

Re-deploying the same `metadata.name` **updates the agent in place** (`Updated existing agent (v1 → v2)`) instead of stacking duplicates. Pass `--force` to deploy despite blocking (deploy-fatal) translation warnings. Note: a custom `base_image` and other Managed-Agent-irrelevant fields are dropped with only a low-severity note — they never block the deploy. KOL profile setup is handled on the askdao.ai web — the studio links you there if it's not done yet.

---

## Why

Configuring a Managed Agent runtime — picking the model, listing OS packages, choosing which skills travel with the agent, writing the system prompt — is high-friction. `askdao-cli` reduces that to a **review-and-edit** step (not from-scratch) by leveraging:

- [`anchore/syft`](https://github.com/anchore/syft) for deterministic dependency scanning (30+ package managers)
- [`go-enry/enry`](https://github.com/go-enry/enry) for byte-level language detection (~500 languages)
- Provider patterns ported from [`railwayapp/nixpacks`](https://github.com/railwayapp/nixpacks) for framework inference and reverse mapping (e.g. `psycopg → libpq-dev`)
- **harness-aware, dual-scope skill/MCP discovery** — a `.claude/` project pulls in both project-local and your global `~/.claude` skills, each tagged by scope + harness, so you choose exactly what ships instead of bundling everything
- the LLM only for the final fuzzy step (recommendation + reasoning); **never** for scanning or for fields with schema constraints (see [design.md §9.13](docs/design.md))

Hard fields like `skills[]` and `metadata.labels` are filled deterministically from the local scan; the LLM does not get to vote on them. This is the trust-boundary principle — LLM = probabilistic; AgentSpec = strict contract; one designated normalizer absorbs the gap.

---

## Install

**Pre-built binaries** (windows / darwin / linux × amd64 / arm64) ship with every
[GitHub Release](https://github.com/askdao/askdao-cli/releases). While the repo is
private, collaborators download via the `gh` CLI:

```bash
gh release download -R askdao/askdao-cli --pattern '*windows_amd64*'
```

On Windows, unzip and put `askdao.exe` somewhere on your `PATH`
(e.g. `%LOCALAPPDATA%\askdao\bin`). See
[docs/kol-quickstart-windows.md](docs/kol-quickstart-windows.md) for the full
Windows onboarding walkthrough.

**From source** (Go 1.26+ required):

```bash
git clone https://github.com/askdao/askdao-cli.git
cd askdao-cli && make install
```

Releases are cut by pushing a `v*` tag — GoReleaser builds all six
platform archives + checksums (`.goreleaser.yml` +
`.github/workflows/release.yml`); `make snapshot` dry-runs the pipeline locally.

---

## Commands

| Command | Status | Description |
|---|---|---|
| `askdao auth login [--server url] [--no-browser]` | ready | Browser-bound OAuth 2.0 Device Code Flow; saves a long-lived token at `~/.config/askdao/credentials.json` (0600) |
| `askdao auth status` | ready | Show currently-logged-in identity; exit 1 if not logged in |
| `askdao auth logout` | ready | Delete local credentials (server-side revoke is via the web UI; coming v2) |
| `askdao agent edit [--dir path] [--no-ui] [--force]` | ready | Scan the project (or load an existing `askdao-agent.yml`) and open the local web studio to review / edit / deploy. `--no-ui` writes a draft and exits (CI / headless) |
| `askdao agent deploy [--dir path] [--harness id] [--force]` | ready | Package custom skills + push `askdao-agent.yml` to Anthropic Managed Agents via Conductor |
| `askdao agent validate` | planned | Validate `askdao-agent.yml` schema |

> v0.8 simplified the command surface: the old `detect` / `bundle` / `agent init` / `agent show` (CLI character-menu review) collapsed into the single `agent edit` web studio — their views (scan report, upload manifest, spec card) now live as panels in the studio.

### Token resolution

`agent edit` and `agent deploy` resolve the conductor URL + bearer token in this order:

1. `ASKDAO_CONDUCTOR_URL` + `ASKDAO_CONDUCTOR_TOKEN` env vars (must be set as a pair) — for CI / one-off overrides
2. `~/.config/askdao/credentials.json` — from `askdao auth login` (the default path)
3. Error with a setup hint (`agent edit` falls back to an offline mock recommender when neither is set)

This mirrors `aws` / `gcloud` / `kubectl` conventions (explicit env always wins). For local-dev pointing at a custom conductor, use `askdao auth login --server http://localhost:8000`.

---

## Project layout

```
askdao-cli/
├── cmd/askdao/             # CLI entry + commands (auth · agent edit/deploy · common helpers)
├── internal/
│   ├── auth/               # OAuth 2.0 Device Code Flow + credentials.json (0600, XDG-aware)
│   ├── scanner/            # syft / enry / dockerfile parser + harness-aware dual-scope skill/MCP scan + payload classifier
│   ├── providers/          # nixpacks-style framework providers (Python / Node / Go / Rust)
│   ├── pipeline/           # L1-L4 orchestration + deterministic skills builder
│   ├── recommender/        # policy heuristics + conductor /cli/recommend client
│   ├── render/             # CLI render helpers (deploy diff, translation warnings)
│   ├── webstudio/          # local web studio — 127.0.0.1 server + go:embed single-page 4-step wizard
│   ├── deploy/             # conductor /cli/deploy client + skill-dir zip packaging
│   └── types/              # detection.json + askdao-agent.yml schemas
├── docs/
│   ├── design.md           # original L1-L4 design + decision log (§9)
│   ├── observe-layer-design.md     # v0.8 direction (corrected — see review below)
│   ├── review-observe-pivot-2026-05-21.md  # v0.8 scope-correction + implementation review
│   ├── cli-auth-device-flow.md     # OAuth 2.0 Device Code Flow design
│   └── HANDOFF.md          # context-switch entry point + status log
└── Makefile
```

See [`docs/HANDOFF.md`](docs/HANDOFF.md) for the current status and entry point for fresh sessions, and [`docs/design.md`](docs/design.md) for the full architecture + decision log.

---

## Privacy + security model

| Concern | Behavior |
|---|---|
| Source code upload | Local scan only emits package names + dep counts + language byte percentages — **no file contents leave your machine** until you explicitly deploy |
| `.env` files | Never read for values. Only `.env.example` / `.env.sample` are inspected, and only keys are extracted (never RHS values) |
| `.env*` / `*.pem` / `*.key` | Hard-coded `builtinIgnore`; never enter the upload payload regardless of `.gitignore` state |
| Vault credentials | The studio only declares credential **hints** (name / purpose / required) — actual secret values are never entered or stored here; subscribers provide them during onboarding |
| Web studio | Bound to `127.0.0.1` on a random port; serves a self-contained `go:embed` page; data stays on your machine |
| CLI auth tokens | OAuth 2.0 Device Code Flow (RFC 8628); plaintext token returned once on `auth login`, stored at `~/.config/askdao/credentials.json` (0600); server stores only SHA-256(token) |
| Phishing resistance | `user_code` shown in both terminal AND web page; alphabet `BCDFGHJKLMNPQRSTVWXZ23456789` (no 0/O/1/I/l, no vowels) — compare before clicking Authorize |
| Skill upload origin | The on-disk path of your skill (`.claude/skills/` vs `.agents/skills/`) is **stripped at zip time** — Anthropic only ever sees `<skillName>/SKILL.md` (harness-neutral invariant, design.md §9.14) |

---

## Status

This is part of the [AskDAO](https://askdao.ai) ecosystem — a platform that helps subject-matter experts (KOLs) deliver AI-assisted services to their audience across IM channels (Telegram, WhatsApp, iMessage, web).

`askdao-cli` is the local, open-source tool KOLs run on their own machine to define and deploy their agents.

**Roadmap**:

- ✅ v0.7 — OAuth Device Code Flow `auth login`; flat project layout (`askdao-agent.yml` at root + `.askdao/` for tool products); Skill upload model corrected (all custom skills upload via `POST /v1/skills`, no public registry); persona consolidated into a yaml literal block; trust-boundary normalizer.
- ✅ v0.8 — `agent edit` **local web studio** (4-step wizard, Kami design system); command surface simplified to `auth` + `agent edit/deploy`; harness-aware **dual-scope** skill/MCP discovery (project + global `~/.claude`) fixing skill over-inclusion; agent theme color; vault_hints editing; inline AI-confidence badges; in-place re-deploy (update mode).
- 🚧 next — Codex/Cursor global-scope paths; `agent edit --observe` (hook-driven auto-tick of skills actually used in a session); theme color rendered subscriber-side (conductor + askdao-ai-web); `agent validate`.

---

## Contributing

This repo is pre-1.0; the surface is still evolving with KOL feedback (currently dogfooding with [`homework-spelling`](https://github.com/sunmu01/homework-spelling) and similar real projects). Issues + PRs welcome — please read [`docs/design.md`](docs/design.md) §9 (decision log) and [`docs/review-observe-pivot-2026-05-21.md`](docs/review-observe-pivot-2026-05-21.md) first to understand the trust-boundary philosophy and the v0.8 direction.

---

## License

MIT — see [LICENSE](LICENSE).
