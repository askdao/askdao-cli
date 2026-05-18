# askdao-cli

> Local CLI for AskDAO — bootstrap AI agents from your project directory by auto-detecting tech stack, then deploy to Anthropic Managed Agents through Conductor.

**Status:** v0.7 — `auth login`, `detect`, `bundle`, `agent init/show/deploy` all shipping; APIs may still change before 1.0.

> 🛡 **Trust anchor.** `askdao-cli` is the **only open-source piece** of the AskDAO platform. All project scanning runs locally — file contents never leave your machine; only the resulting `detection.json` summary (no source code, no env values) is sent to Conductor when you run `agent init --auto`. Auth uses OAuth 2.0 Device Code Flow (RFC 8628); CLI tokens are SHA-256 hashed server-side.

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

# 4. Scan and generate a draft (interactive review at the end)
askdao agent init --auto

# 5. Preview exactly what would be uploaded (skills + agent docs + manifests)
askdao bundle

# 6. Deploy to Anthropic Managed Agents
askdao agent deploy
```

That's the full happy path. Everything between steps 4 and 6 is local edits to `askdao-agent.yml` at your project root.

---

## What it does

Run one command in your project root and get a ready-to-review agent specification:

```bash
$ askdao agent init --auto
→ Scanning project (languages / deps / Dockerfile / MCP / skills) ...
→ Inferring frameworks + building deployment payload ...
→ Calling LLM via conductor for recommendation (typically 10-20s) ...
✓ Recommendation received.

  [mid-density review card with persona, skills, capabilities, runtime, vault hints]

─── ACTIONS ─────────────────────────────────────────────────
  [A] Approve and write files     [P] View persona / system prompt
  [E] Edit yaml in $EDITOR        [D] View all pip deps
  [R] View full reasoning trace   [F] View filtered (dev) deps
  [S] Show full yaml in pager     [W] View all warnings
                                  [M] View filtered MCP
  [Q] Quit (saved as draft)
> A

✓ Approved and wrote ./askdao-agent.yml
  Next: review persona.system_prompt, then `askdao agent deploy`
```

Files written, all at your project root:

```
your-project/
├── askdao-agent.yml          ← your single edit target (commit this)
├── .askdao/
│   ├── recommendation.yml    ← diff baseline (commit for review history)
│   └── detection.json        ← deterministic scan output (gitignore optional)
├── .agents/skills/           ← your existing skill tree (untouched)
├── package.json              ← your project files (untouched)
└── ...
```

Instead of staring at a blank template, you start by reviewing a draft. Edit `askdao-agent.yml.persona.system_prompt`, fine-tune capabilities and vault hints, then deploy:

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

✓ Deployed.

  agent_id:    agt_aa4c3329...
  anthropic:   agent=agent_01G8...  environment=env_014X...
  group_id:    grp_c1aeb63d...
  group link:  https://askdao.ai/k/<you>/g/grp_c1aeb63d...

  Skills:
    • .agents/skills/listenhub   →  managed skill_014rn2…@1778…  (viking://resources/skills/private/…)
    • .agents/skills/listenhub-cli  →  managed skill_015Pu…@1778…
    • .agents/skills/spelling-homework-generator  →  managed skill_01J26…@1778…
    • .agents/skills/tts  →  managed skill_0142c2…@1778…

ℹ The following non-blocking warnings were flagged during translation.
  Your agent is live and ready to use — these are advisory notes.

  MEDIUM (3):
    • workspace.base_image = IGNORED  → Anthropic uses fixed cloud image
    • ...
  LOW (1):
    • ...

✓ Deploy complete. Open https://askdao.ai/k/<you>/g/grp_c1aeb63d... to chat.
```

First-time KOLs hit a `409 kol_profile_required` handshake — the CLI prompts for an optional bio and retries automatically. Pass `--bio "…"` to skip the prompt; `--force` to deploy despite HIGH-severity translation warnings.

---

## Why

Configuring a Managed Agent runtime — picking the right model, listing OS packages, writing system prompts, declaring skills — is high-friction. `askdao-cli` reduces that to a **review-and-edit** step (not from-scratch) by leveraging:

- [`anchore/syft`](https://github.com/anchore/syft) for deterministic dependency scanning (30+ package managers)
- [`go-enry/enry`](https://github.com/go-enry/enry) for byte-level language detection (~500 languages)
- Provider patterns ported from [`railwayapp/nixpacks`](https://github.com/railwayapp/nixpacks) for framework inference and reverse mapping (e.g. `psycopg → libpq-dev`)
- LLM only for the final fuzzy step (recommendation + reasoning); **never** for scanning or for fields with schema constraints (see [design.md §9.13](docs/design.md))

Hard fields like `skills[]` and `metadata.labels` are filled deterministically from the local scan; the LLM does not get to vote on them. This is the trust-boundary principle — LLM = probabilistic; AgentSpec = strict contract; one designated normalizer absorbs the gap.

---

## Install

```bash
# from source (Go 1.26+ required)
git clone https://github.com/askdao/askdao-cli.git
cd askdao-cli && make install
```

Pre-built binaries will be published once the API stabilizes (v1.0 target).

---

## Commands

| Command | Status | Description |
|---|---|---|
| `askdao auth login [--server url] [--no-browser]` | ready | Browser-bound OAuth 2.0 Device Code Flow; saves a long-lived token at `~/.config/askdao/credentials.json` (0600) |
| `askdao auth status` | ready | Show currently-logged-in identity; exit 1 if not logged in |
| `askdao auth logout` | ready | Delete local credentials (server-side revoke is via the web UI; coming v2) |
| `askdao detect [path]` | ready | Print the deterministic detection report without creating an agent (offline, no LLM) |
| `askdao bundle [path]` | ready | Preview the deployment payload — which files would be uploaded, with sizes + skill origin tags |
| `askdao agent init [name] [--auto]` | ready | Generate `askdao-agent.yml` at the project root; `--auto` scans the project and runs the L1-L4 LLM pipeline |
| `askdao agent show [flags]` | ready | Render the agent spec (mid-density card, `--full` for raw yaml, focused views via `--persona` / `--deps` / `--mcp` / `--warnings` / `--reasoning`) |
| `askdao agent deploy [--dir path] [--force] [--bio …]` | ready | Package custom skills + push to Anthropic Managed Agents via Conductor |
| `askdao agent validate` | planned | Validate `askdao-agent.yml` schema |
| `askdao agent regenerate` | planned | Re-scan and diff against existing yaml |

### Token resolution

`agent deploy` and `agent init --auto` resolve the conductor URL + bearer token in this order:

1. `ASKDAO_CONDUCTOR_URL` + `ASKDAO_CONDUCTOR_TOKEN` env vars (must be set as a pair) — for CI / one-off overrides
2. `~/.config/askdao/credentials.json` — from `askdao auth login` (the default path)
3. Error with a setup hint

This mirrors `aws` / `gcloud` / `kubectl` conventions (explicit env always wins). For local-dev pointing at a custom conductor, use `askdao auth login --server http://localhost:8000`.

---

## Project layout

```
askdao-cli/
├── cmd/askdao/             # CLI entry + commands (auth / detect / bundle / agent init|show|deploy)
├── internal/
│   ├── auth/               # OAuth 2.0 Device Code Flow + credentials.json (0600, XDG-aware)
│   ├── scanner/            # syft / enry / dockerfile parser + payload classifier
│   ├── providers/          # nixpacks-style framework providers (Python / Node / Go / Rust)
│   ├── pipeline/           # L1-L4 orchestration + deterministic skills builder
│   ├── recommender/        # policy heuristics + conductor /cli/recommend client
│   ├── render/             # KOL review UX (mid-density card, payload, diff, warnings)
│   ├── deploy/             # conductor /cli/deploy client + skill-dir zip packaging
│   └── types/              # detection.json + askdao-agent.yml schemas
├── docs/
│   ├── design.md           # complete design + decision log (§9)
│   ├── cli-auth-device-flow.md   # OAuth 2.0 Device Code Flow design
│   └── HANDOFF.md          # context-switch entry point + v0.7 corrections log
└── Makefile
```

See [`docs/design.md`](docs/design.md) for the full architecture (10 sections + 15+ design decisions). [`docs/HANDOFF.md`](docs/HANDOFF.md) is the entry point for fresh agent sessions / new contributors.

---

## Privacy + security model

| Concern | Behavior |
|---|---|
| Source code upload | Local scan only emits package names + dep counts + language byte percentages — **no file contents leave your machine** until you explicitly `agent deploy` |
| `.env` files | Never read for values. Only `.env.example` / `.env.sample` are inspected, and only keys are extracted (never RHS values) |
| `.env*` / `*.pem` / `*.key` | Hard-coded `builtinIgnore`; never enter the upload payload regardless of `.gitignore` state |
| CLI auth tokens | OAuth 2.0 Device Code Flow (RFC 8628); plaintext token returned exactly once on `auth login`, stored at `~/.config/askdao/credentials.json` (0600); server side stores only SHA-256(token) |
| Phishing resistance | `user_code` shown in both terminal AND web page; alphabet `BCDFGHJKLMNPQRSTVWXZ23456789` (no 0/O/1/I/l, no vowels) — KOL compares before clicking Authorize |
| Skill upload origin | The on-disk path of your skill (`.claude/skills/` vs `.agents/skills/`) is **stripped at zip time** — Anthropic only ever sees `<skillName>/SKILL.md` (harness-neutral invariant, design.md §9.14) |

---

## Status

This is part of the [AskDAO](https://askdao.ai) ecosystem — a platform that helps subject-matter experts (KOLs) deliver AI-assisted services to their audience across IM channels (Telegram, WhatsApp, iMessage, web).

`askdao-cli` is the local tool KOLs run on their own machine to define and deploy their agents.

**Roadmap**:

- ✅ v0.6 — `detect`, `bundle`, `agent init/show/deploy` shipping; deterministic L1-L3 scanner; lockfile-aware deployment payload classification.
- ✅ v0.7 — OAuth Device Code Flow `auth login`; flat project layout (`askdao-agent.yml` at root + `.askdao/` for tool products); Skill upload model corrected (no public registry on Anthropic Managed Agents; all custom skills upload via `POST /v1/skills`); persona consolidated into yaml literal block; trust-boundary normalizer + audit checklist.
- 🚧 v0.8 (planned) — `agent validate` + `agent regenerate`; web UI for listing / revoking CLI tokens; spinner progress during long deploy POSTs.

---

## Contributing

This repo is in pre-1.0; the surface is still evolving with KOL feedback (currently dogfooding with [`homework-spelling`](https://github.com/sunmu01/homework-spelling) and similar real projects). Issues + PRs welcome — please read [`docs/design.md`](docs/design.md) §9 (decision log) first to understand the trust-boundary philosophy before proposing changes to the L1-L4 pipeline.

---

## License

MIT — see [LICENSE](LICENSE).
