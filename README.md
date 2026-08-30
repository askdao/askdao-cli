# askdao-cli

> Turn the AI agent project on your machine into a live service your subscribers can chat with — scan locally, review and edit in a local web studio, deploy to [askdao.ai](https://askdao.ai) with one command.

**Status:** v0.1.1 — pre-1.0, the surface is still evolving with creator feedback.

> 🛡 **Open source on purpose.** askdao-cli is the part of AskDAO that runs on *your* machine, so it is fully open source — you can audit exactly what it does before running it. Scanning happens locally; your file contents never leave your machine. Details in [Privacy & security](#privacy--security).

---

## Install

One command — it downloads the latest release, verifies the checksum, installs the binary, and handles `PATH`:

**macOS / Linux / WSL**

```bash
curl -fsSL https://askdao.ai/install.sh | bash
```

**Windows (PowerShell)**

```powershell
irm https://askdao.ai/install.ps1 | iex
```

**Windows (CMD)**

```bat
curl -fsSL https://askdao.ai/install.cmd -o install.cmd && install.cmd && del install.cmd
```

Upgrade any time with `askdao update` — no need to re-run the script. The scripts live in [`install/`](install/), audit them before piping to your shell. Prefer not to pipe? Download an archive from [Releases](https://github.com/askdao/askdao-cli/releases) and put the binary on your `PATH`, or build from source ([For developers](#for-developers)).

---

## Get started

Three steps take a local project to a deployed agent.

**1. Log in** — opens your browser once. Login also auto-configures the **askdao-mcp** toolkit (podcasts, TTS, image generation, market data …) for your local Claude Code / Codex, so you can use the same tools while debugging that your agent will use in production:

```bash
askdao auth login
```

**2. Review in the studio** — run inside the project you want to turn into an agent:

```bash
cd path/to/your-agent-project
askdao agent edit
```

Your browser opens a **local studio** (`127.0.0.1`, nothing uploaded) that walks you through a 4-step wizard instead of a blank YAML template:

1. **Identity** — name, description, category, theme color
2. **Persona** — model class + system prompt
3. **Skills & Tools** — tick exactly which skills / MCP servers travel with the agent, from both your project and your global (`~/.claude` etc.) scope
4. **Review** — confirm the final selection, then deploy or save

Every AI-suggested default carries a confidence badge — hover it for the reasoning, then keep or override. The result is a single file you own: `askdao-agent.yml` at your project root (commit it).

**3. Deploy** — the studio's final button, or from the terminal:

```bash
askdao agent deploy
```

You get a link like `https://askdao.ai/k/<you>/g/...` — open it and chat with your live agent. Re-deploying the same agent name **updates it in place**; no duplicates.

> Windows user? The step-by-step walkthrough is [docs/kol-quickstart-windows.md](docs/kol-quickstart-windows.md).

---

## Commands

| Command | Description |
|---|---|
| `askdao auth login [--no-browser]` | Browser-bound login (OAuth 2.0 Device Code Flow); then auto-configures askdao-mcp for local harnesses |
| `askdao auth status` / `logout` | Show identity / delete local credentials |
| `askdao mcp setup [--print]` | Re-run the askdao-mcp configuration for Claude Code / Codex; `--print` emits copy-paste snippets instead of writing files |
| `askdao agent edit [--dir path] [--no-ui] [--observe]` | Scan + open the local studio to review, edit, and deploy; `--no-ui` writes a draft `askdao-agent.yml` and exits (CI / headless) |
| `askdao agent deploy [--dir path] [--force] [--confirm-downgrade]` | Package selected skills + push `askdao-agent.yml` to askdao.ai |
| `askdao update [--force]` | Self-update to the latest release (checksum-verified) |
| `askdao version` | Print version |

---

## How it works

The scan is **deterministic first, AI last**:

- [`anchore/syft`](https://github.com/anchore/syft) reads your dependency manifests (30+ package managers); [`go-enry/enry`](https://github.com/go-enry/enry) detects languages; framework inference follows [`railwayapp/nixpacks`](https://github.com/railwayapp/nixpacks) provider patterns
- Skill / MCP discovery is harness-aware and dual-scope: project-local plus your global config, each item tagged so you choose what ships instead of bundling everything
- The LLM only proposes the fuzzy fields (description, system prompt, category) with visible reasoning — it never decides hard fields like the skill list

Files written, all at your project root:

```
your-project/
├── askdao-agent.yml          ← your single edit target (commit this)
└── .askdao/
    ├── recommendation.yml    ← baseline; deploy shows a diff of what you changed
    └── detection.json        ← raw scan output
```

`askdao agent deploy` prints the diff against the recommendation baseline, packages each selected skill directory, and pushes everything to askdao.ai, where the platform provisions the cloud runtime and injects production credentials server-side — tokens on your machine are for local debugging only and are never uploaded.

---

## Privacy & security

| Concern | Behavior |
|---|---|
| Source code upload | The scan only emits package names, dependency counts, and language percentages — file contents never leave your machine; only skills you explicitly tick are uploaded at deploy |
| `.env` files | Never read for values; only `.env.example`-style keys are inspected. `.env*` / `*.pem` / `*.key` are hard-excluded from every upload regardless of `.gitignore` |
| Credentials | The studio only declares credential *hints* (name / purpose); actual secret values are never entered, stored, or uploaded here |
| Web studio | Bound to `127.0.0.1` on a random port, fully self-contained |
| CLI tokens | OAuth 2.0 Device Code Flow (RFC 8628); stored at `~/.config/askdao/credentials.json` (0600); the server keeps only a SHA-256 hash |
| Skill packaging | Your on-disk layout (`.claude/skills/` vs `.agents/skills/`) is stripped at zip time — the platform only ever sees `<skillName>/SKILL.md` |

---

## For developers

**Build from source** (Go 1.26+):

```bash
git clone https://github.com/askdao/askdao-cli.git
cd askdao-cli && make install     # or: go install ./cmd/askdao (Windows has no make)
```

Source builds report a `-dev` version. Releases are cut by pushing a `v*` tag (GoReleaser builds 6 platform archives + checksums; `make snapshot` dry-runs locally).

For CI or a custom backend, `ASKDAO_CONDUCTOR_URL` + `ASKDAO_CONDUCTOR_TOKEN` (set as a pair) override the credentials from `auth login`; `askdao auth login --server <url>` points the whole flow at a local server.

**Layout**: `cmd/askdao/` (CLI entry + commands) · `internal/` (scanner, providers, pipeline, recommender, webstudio, deploy, selfupdate, auth, types) · `install/` (one-liner install scripts, served via askdao.ai) · `docs/` (design + decision log).

**Contributing**: issues + PRs welcome. Start with [`docs/design.md`](docs/design.md) (architecture + decision log §9) and [`docs/HANDOFF.md`](docs/HANDOFF.md) (current status); the trust-boundary philosophy there explains why hard fields are never LLM-decided.

---

## License

MIT — see [LICENSE](LICENSE).
