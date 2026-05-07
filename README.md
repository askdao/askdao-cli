# askdao-cli

> Local CLI for AskDAO — bootstrap AI agents from your project directory by auto-detecting tech stack and generating Anthropic Managed Agents config.

**Status:** Pre-alpha. Design phase. APIs unstable.

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
| `askdao agent init <name> --auto` | planned | Scan current directory and generate `agent.yml` draft |
| `askdao detect [path]` | planned | Print detection report without creating an agent |
| `askdao agent validate` | planned | Validate `agent.yml` schema |
| `askdao agent deploy` | planned | Push agent + environment to Anthropic Managed Agents |
| `askdao agent regenerate` | planned | Re-scan and diff against existing yaml |

See [docs/design.md](docs/design.md) for the full design.

## Project layout

```
askdao-cli/
├── cmd/askdao/             # CLI entry point
├── internal/               # Implementation (TBD)
│   ├── scanner/            # syft / enry / dockerfile parser wrappers
│   ├── providers/          # nixpacks-style framework providers
│   ├── recommender/        # LLM-driven yaml generation
│   └── types/              # detection.json + agent.yml schemas
├── docs/                   # Design documents
│   ├── design.md           # Main design draft
│   └── investigations/     # Spike reports informing the design
└── Makefile
```

## Status

This is part of the [AskDAO](https://askdao.ai) ecosystem — a platform that helps subject-matter experts (KOLs) deliver AI-assisted services to their audience across IM channels.

`askdao-cli` is the local tool KOLs run on their own machine to define and deploy their agents.

## License

MIT — see [LICENSE](LICENSE).
