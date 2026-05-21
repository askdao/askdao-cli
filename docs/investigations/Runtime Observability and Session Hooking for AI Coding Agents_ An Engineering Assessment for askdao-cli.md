# Runtime Observability and Session Hooking for AI Coding Agents: An Engineering Assessment for askdao-cli

## TL;DR

- **Claude Code's hook system is the single best primitive available today for "observe what the agent actually does and turn it into a runtime spec."** `PreToolUse`/`PostToolUse` hooks on the Bash matcher receive the full, verbatim shell command (`tool_input.command`) plus the exit code and output on stdin — meaning you can deterministically detect every `apt-get install`, `pip install`, `npm install`, env-var read, and missing-library error the agent produces. Codex CLI exposes the same pattern but with materially weaker tool coverage and an OTEL stream that intentionally omits command strings.
- **No existing open-source project does the specific thing askdao-cli wants** — i.e., take a Claude Code/Codex session trace and synthesize a Dockerfile, `requirements.txt`, or runtime spec from it. The closest projects (claude-trace, claude_telemetry, pydantic/claude-code-logfire-plugin, DazzleML/claude-session-logger, waynesutton/codex-sync-plugin, disler/claude-code-hooks-multi-agent-observability, Siddhant-K-code/agent-trace) capture the raw data you'd need, but stop at dashboards/replay. The "observe-to-spec" half is open green-field — and SlimToolkit/DockerSlim is the only well-known runtime-observation→Dockerfile precedent, applied to containers rather than agents.
- **Recommended architecture for askdao-cli v0.8+: keep the static 4-layer as the "cold-start" baseline, and add a Claude Code Hooks Recorder that wraps a focused "exercise the app" session, then merges observed artifacts (installed packages, env vars accessed, files written, ports bound) into the spec.** Static catches what the agent never runs; observation catches the long tail (`libpq-dev`, GPU drivers, system tools) that static will never reliably see.

---

## Key Findings

### 1. Claude Code's hook surface is *exactly* what an observe-then-spec pipeline needs

Claude Code v2.x ships 29 distinct lifecycle hook events per Anthropic's official Claude Code Hooks reference (code.claude.com/docs/en/hooks): SessionStart, Setup, UserPromptSubmit, UserPromptExpansion, PreToolUse, PermissionRequest, PermissionDenied, PostToolUse, PostToolUseFailure, PostToolBatch, Notification, SubagentStart, SubagentStop, TaskCreated, TaskCompleted, Stop, StopFailure, TeammateIdle, InstructionsLoaded, ConfigChange, CwdChanged, FileChanged, WorktreeCreate, WorktreeRemove, PreCompact, PostCompact, Elicitation, ElicitationResult, and SessionEnd. The relevant ones for runtime-environment capture:

- **`PreToolUse` with `matcher: "Bash"`** — fires before every shell command the agent dispatches. The hook script receives a JSON blob on stdin containing `tool_input.command` (the full shell command verbatim), `tool_input.description`, `cwd`, `session_id`, and `tool_use_id`. This is what lets every public hook example block `rm -rf` or rewrite `pip install` to `uv add`. The same JSON gives you everything you need to detect a package install.
- **`PostToolUse` with the same matcher** — fires after execution and the input now also includes `tool_response.exit_code`, `tool_response.output`, and (since v2.1.119) `duration_ms`. Failure signals are first-class: a `pip install psycopg` followed by a `PostToolUse` with a "Error: pg_config executable not found" string in the output is exactly the signal that surfaces the `libpq-dev` requirement statically.
- **`PostToolUse` with `matcher: "Write|Edit|MultiEdit"`** — captures every file the agent creates or modifies, with the file path in `tool_input.file_path`. This catches generated `.env.example`, `Dockerfile`, `requirements.txt`, `package.json` changes.
- **`SessionStart` / `SessionEnd`** — bracket every session and can persist environment variables via `$CLAUDE_ENV_FILE`. Ideal for stamping a unique trace ID and finalizing the spec.
- **HTTP hooks** (since v2.1.x): instead of shelling out, point the hook at `http://localhost:PORT/event` and POST JSON. This is the right transport for a Go daemon — askdao-cli is already a Go binary, so it can register itself as the HTTP hook target and consume events directly without spawning Python.

Configuration lives in `.claude/settings.json` (project) or `~/.claude/settings.json` (user). Hooks are snapshotted at session start, so config edits don't hot-reload — askdao-cli would write the hook config before launching `claude`. Per Anthropic's own docs, exit code 2 in `PreToolUse` blocks the call; anything else is non-blocking — for pure observation, return 0 with no JSON and let the agent run.

### 2. Claude Code's native OpenTelemetry is *complementary* but not sufficient

Claude Code's built-in telemetry (enabled with `CLAUDE_CODE_ENABLE_TELEMETRY=1`) emits three signals over OTLP: metrics, log-style events, and (in beta) traces. The relevant log events:

- `claude_code.tool_result` — fired on every tool completion with `tool_name`, `success`, and (only when `OTEL_LOG_TOOL_DETAILS=1`) `tool_parameters.bash_command`, `tool_parameters.full_command`, `tool_parameters.mcp_server_name`, `tool_parameters.mcp_tool_name`, plus `tool_input` with file paths/URLs/search patterns.
- `claude_code.tool_decision` — fired on every permission decision with `source` (config/hook/user_permanent/user_temporary/user_abort/user_reject).
- `claude_code.api_request` / `api_error` / `user_prompt` — model calls, errors, and user prompts (the last redacted unless `OTEL_LOG_USER_PROMPTS=1`).
- `claude_code.code_edit_tool_decision` metric — accept/reject counts broken down by `language` (TypeScript, Python, etc.).
- Traces (beta) nest `llm_request` and tool spans under a `claude_code.tool` parent and propagate W3C trace context via `TRACEPARENT` into the CLI subprocess, so an external trace context can be the root span.

The catch is that OTEL is **passive observation** — it cannot block, modify, or inject context. It also doesn't capture environment variable *reads* (only what gets logged), and it does not surface the *exit code* of bash commands as cleanly as the `PostToolUse` hook does. **For askdao-cli's use case, hooks are the primary capture mechanism; OTEL is a secondary corroboration stream and a debugging aid.**

### 3. Codex CLI's hook surface is similar but materially weaker today (May 2026)

OpenAI's Codex CLI's hooks engine became stable in v0.124.0 (April 23, 2026), per the official Codex changelog (developers.openai.com/codex/changelog). The changelog entry declaring stability reads verbatim: *"Hooks are now stable, can be configured inline in config.toml and managed requirements.toml, and can observe MCP tools as well as apply_patch and long-running Bash sessions."* The schema is intentionally Claude-compatible: `PreToolUse`, `PostToolUse`, `PermissionRequest`, `UserPromptSubmit`, `SessionStart`, `Stop`. The CLI also sets `CLAUDE_PLUGIN_ROOT` for compatibility with Claude plugins.

But the coverage gaps are real and current:

- **Until PR #18391, `apply_patch` did not emit hook events.** Today it does, but `tool_name` was hardcoded to `"Bash"` historically — newer versions emit handler-supplied names like `apply_patch`. Verify your installed Codex version (≥0.125 emits proper names).
- **PreToolUse `updatedInput` rewrites are explicitly rejected**: `output_parser.rs` returns "PreToolUse hook returned unsupported updatedInput" — so you can observe and block, but not rewrite.
- **Many built-in tools still don't emit hooks** as of issue #20204: `read_file`, `grep`, `list_dir`, `view_image`, image generation, web search, and most multi-agent sub-tools all fall back to the trait default `None` and never call into the hook runtime. For environment capture this matters less (you mostly care about shell + edits), but it's worth knowing.
- **Codex CLI's OTEL `[otel]` exporter deliberately omits command contents.** The official "Advanced Configuration" doc (developers.openai.com/codex/config-advanced) states verbatim: *"If a metric includes the `tool` field, it reflects the internal tool used (for example, `apply_patch` or `shell`) and doesn't contain the actual shell command or patch codex is trying to apply."* Events are namespaced `codex.*` — `codex.conversation_starts`, `codex.api_request`, `codex.sse_event`, `codex.tool_decision`, `codex.tool_result` (an "output snippet" only, not the command). Metrics are aggregate counters/histograms with `tool` + `success` labels (e.g. `codex.tool.call`, `codex.tool.call.duration_ms`). **So for Codex, hooks are the only way to capture the actual `apt-get install` string — OTEL won't give it to you.**

### 4. Open-source projects worth studying — the data-capture side

Every project in scope captures *some* observational data. None of them generate a runtime spec, but most expose data you'd reuse.

| Project | Capture mechanism | Granularity | Useful for askdao-cli? |
|---|---|---|---|
| **`pydantic/claude-code-logfire-plugin`** | Hook-driven; a single Python `scripts/log-event.py` (stdlib only) runs on every Claude Code lifecycle event and on `Stop` parses `transcript_path` JSONL to extract per-API-call data, emits OTLP/HTTP JSON to Logfire. Produces one trace per session with child spans per LLM call. | Full session trace, conversation history, tokens, cost. No per-tool-call spans. | Pattern reference; transcript parser is reusable. |
| **`TechNickAI/claude_telemetry`** (the `claudia` CLI) | Wraps the `claude` binary and uses the Claude Agent SDK's async hooks to intercept tool calls before forwarding to any OTEL backend (Logfire/Datadog/Honeycomb/Grafana). Captures **tool calls, token counts, costs, execution traces**. | Per-tool-call: tool name, input, output. SDK-level. | Closest existing template; MIT-licensed; uses the SDK rather than the CLI hook config. |
| **`DazzleML/claude-session-logger`** | Hook-driven (`hooks/scripts/log-command.py` + `run-hook.mjs`). Writes plaintext logs to `~/.claude/sesslogs/{session-name}__{session-id}_{user}/` with separate `.sesslog`, `.shell`, `.tasks` files and a symlink to the transcript. | Tool calls, timestamps, shell output, task ops. Local files only. | Best example of the *minimal* hook-only logger pattern. |
| **`waynesutton/codex-sync-plugin`** (`codex-sync` npm) | Uses Codex's `notify` hook to trigger sync at end-of-turn, then parses `~/.codex/sessions/*.jsonl` to extract project path, model, tool calls, token counts. Sends to Convex (OpenSync dashboard). | Per-session: tool calls (configurable), thinking traces off by default, no file contents. | Reference for the Codex side of the same pattern. |
| **`badlogic/lemmy/apps/claude-trace`** (a.k.a. `@mariozechner/claude-trace`) | Different mechanism: monkey-patches `global.fetch` via `--require interceptor-loader.js` to intercept raw HTTP traffic to api.anthropic.com. Logs to `.claude-trace/*.jsonl` + self-contained HTML report including system prompts, tool definitions, tool outputs, thinking blocks. | **Maximum** — full request/response bodies, hidden system prompt, all tool calls. | Highest-fidelity data source; works without configuring hooks. |
| **`liaohch3/claude-tap`** | Local HTTP proxy + trace viewer that sits in front of Claude Code, Codex CLI, Gemini CLI, Cursor CLI, OpenCode, Kimi, Pi, Hermes Agent. Captures system prompts, conversation history, tool schemas, tool calls, streaming responses, token usage, request diffs. | Same as claude-trace, but works across all agents via forward-proxy injection. | Best option if you want a single capture path that works with Claude *and* Codex. |
| **`disler/claude-code-hooks-multi-agent-observability`** | Pure hook system; ships scripts for all 12+ hook events that forward to an observability server. Supports MCP tool detection (`mcp_server`, `mcp_tool_name`), agent swim-lane filtering, pulse charts. | Per-event, all 12+ hook types, MCP-aware. | Best end-to-end *example* of a production hook pipeline. |
| **`Siddhant-K-code/agent-trace`** ("strace for AI agents") | `agent-strace setup` injects hooks for Claude Code; captures user prompts, assistant responses, every tool call (Bash, Edit, Write, Read, Agent, Grep, Glob, WebFetch, WebSearch, MCP). Generates self-contained HTML viewer + ASCII timeline. | Per-tool-call with arguments and durations. | Strong UX reference; the name itself signals the analogy you'd lean on. |
| **`ColeMurray/claude-code-otel`** | Docker-compose stack: Claude Code → OTEL Collector → Prometheus (metrics) + Loki (events/logs) → Grafana. | Whatever Claude Code's native OTEL emits. | Reference for self-hosting; not directly useful for spec generation. |
| **`anthropics/claude-code` (official)** | Reference implementation; hook schema docs live at `code.claude.com/docs/en/hooks`. | N/A — this is the source. | Authoritative. |

**Verdict:** every piece of the data-collection pipeline askdao-cli needs is open-sourced somewhere. The piece nobody has built is **parser + spec emitter** — i.e., translate "agent ran `apt-get install -y libpq-dev && pip install psycopg2`" into a structured patch to `askdao-agent.yml`.

### 5. The "observe then spec" pattern — prior art and the open gap

A systematic search for projects that consume agent traces and emit Dockerfiles/runtime specs returned **no direct hits as of May 2026**. The closest precedents are container-level, not agent-level:

- **SlimToolkit / DockerSlim** — `slimtoolkit/slim` was accepted to CNCF on May 17, 2023 at the Sandbox maturity level (per cncf.io/projects/slimtoolkit/) and remains at Sandbox as of May 2026. It runs a container, observes via dynamic analysis + ELF dependency resolution which files the app loads, and emits (a) a minified `.slim` image, (b) auto-generated seccomp/AppArmor profiles, and (c) a reverse-engineered `Dockerfile.reversed` (formerly `Dockerfile.fat`). `slim info` / `slim xray` can produce just the reversed Dockerfile without the minification. This is the canonical "observe runtime → emit Dockerfile + security spec" precedent and it's the right mental model for askdao-cli's new layer.
- **Stencila/strace shrinker** (2017 blog post and Julia Evans's 2020 "Why strace doesn't work in Docker") — straces inside a Docker container, records every file actually opened, then assembles a minimal image. Same shape, lower-level signal. Requires CAP_SYS_PTRACE or a seccomp profile that allows ptrace.
- **`jupyter/repo2docker`** — implements the Reproducible Execution Environment Specification (REES); scans for `requirements.txt`, `environment.yml`, `Project.toml`, `apt.txt`, `Pipfile.lock`, `runtime.txt`, `DESCRIPTION`, `postBuild`, etc. and generates a Dockerfile. This is **static-file → spec**, the same shape as askdao-cli's current 4-layer.
- **`nixpacks` (Railway) / `railpack`** — file-system detection → Dockerfile via Nix. Static. Railway launched Railpack in beta on March 4, 2026 (blog.railway.com/p/introducing-railpack): *"Today we're excited to release Railpack — the next iteration of the Railway builder, developed from the ground up and based on everything we've learned from building over 14 million apps with Nixpacks."* The nixpacks GitHub repo (github.com/railwayapp/nixpacks) now displays: *"⚠️ Maintenance Mode: This project is currently in maintenance mode and is not under active development. We recommend using Railpack as a replacement."*
- **Buildpacks (Paketo/Heroku)** — same pattern.

None of these consume agent telemetry. None know what `pip install` was actually run during a session. **The "observe a Claude Code session, emit `askdao-agent.yml`" pipeline is unbuilt territory.**

### 6. Pattern: bash command → spec patch

The data you need is structured and parseable. From a `PreToolUse(Bash)` JSON payload, regex/AST-extract:

| Observed command | Spec patch |
|---|---|
| `apt-get install -y libpq-dev curl` | Append `libpq-dev`, `curl` to `system_packages` |
| `pip install psycopg2 fastapi==0.115.6` | Add to `pip_requirements` (pin where version present) |
| `npm install --save express@^4` / `pnpm add ...` | Add to `npm_dependencies`; record lockfile type |
| `uv add pydantic` | Same, but record `uv` as the package manager |
| `export DATABASE_URL=...` / `echo $OPENAI_API_KEY` | Append to `required_env_vars` (the *name*; never the value) |
| `python -m playwright install chromium` | Record post-install hook |
| `curl https://... \| bash` | Flag for human review (not auto-include) |
| `ports.bind(8000)` observed via `Write` to `Dockerfile`/source | Add to `exposed_ports` |

From `PostToolUse(Bash)` with non-zero exit code and an error string, run an inverse lookup table: `"pg_config executable not found"` → recommend `libpq-dev`; `"Microsoft Visual C++ 14.0 or greater"` → flag Windows-only; `"fatal error: Python.h"` → recommend `python3-dev`. This is exactly the "edge case" surface area the existing static pipeline misses, and the LLM recommendation layer (step 4 of the current 4-layer) can be reused to handle anything not in the table.

---

## Details

### Architecture sketch for askdao-cli's "observe → spec" layer

```
askdao-cli observe ./myapp --agent claude
          │
          ├─ writes .claude/settings.local.json with HTTP hooks pointing at localhost:PORT
          ├─ writes .codex/hooks.json (TOML inline or hooks.json) similarly
          ├─ starts a Go HTTP server (the Recorder) on localhost:PORT
          │
          ├─ exec's `claude` (or `codex`) with a focused prompt like:
          │    "Set up this project, install all dependencies, run the dev server,
          │     and exercise the main code paths so we can record the environment."
          │
          ▼
   ┌─────────────────────────────────────────────────────────────┐
   │  Recorder (Go HTTP server)                                  │
   │   • PreToolUse(Bash)  → parse command → emit ObservedEvent  │
   │   • PostToolUse(Bash) → parse exit/output → ObservedEvent   │
   │   • PostToolUse(Write|Edit) → file diff → ObservedEvent     │
   │   • SessionEnd        → flush + close                       │
   │   • UserPromptSubmit  → tag prompt boundaries               │
   └─────────────────────────────────────────────────────────────┘
          │
          ▼
   parser/aggregator (Go)
   - apt/pip/npm/uv/yarn/pnpm/cargo/go/brew/snap command grammars
   - env-var-name extractor (NEVER values; redact)
   - port-binding detector (Write to Dockerfile/code looking for ports)
   - failure→hint lookup table (libpq-dev, python3-dev, build-essential, ...)
          │
          ▼
   merger (existing dev_filter logic + new observation merger)
          │
          ▼
   LLM recommendation layer (existing) — now with observed evidence
          │
          ▼
   askdao-agent.yml  (with "source: observed" / "source: static" / "source: llm")
```

The merger should track *provenance* per spec entry. A field with `source: observed` is high-confidence; a field with `source: static-then-llm` is the existing v0.7 behavior. When both agree, the merge is trivial; when they conflict, prefer observation but surface the conflict in the diff that the KOL approves.

### Why Go + HTTP hooks specifically

Claude Code hook handlers can be `type: "command"` (spawned subprocess), `type: "http"` (POST JSON to a URL), `type: "prompt"` (Claude-evaluated), or `type: "agent"` (subagent-evaluated). For a Go CLI, the HTTP handler is the cleanest fit:

```json
{
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash",
        "hooks": [{ "type": "http", "url": "http://127.0.0.1:7788/event/pre",
                    "timeout": 10 }] }
    ],
    "PostToolUse": [
      { "matcher": "Bash|Write|Edit|MultiEdit",
        "hooks": [{ "type": "http", "url": "http://127.0.0.1:7788/event/post",
                    "timeout": 30 }] }
    ],
    "SessionEnd": [
      { "hooks": [{ "type": "http", "url": "http://127.0.0.1:7788/event/end" }] }
    ]
  }
}
```

The hook receives the event JSON as the POST body with `Content-Type: application/json`. Non-2xx responses, connection failures, and timeouts are non-blocking — execution continues. This means if askdao-cli's recorder crashes mid-session, Claude Code continues uninterrupted; you just lose the tail of the trace.

For Codex CLI, only `type: "command"` is supported today — `prompt` and `agent` handlers are parsed but skipped, and `async` is parsed but skipped. So for Codex, askdao-cli must shell out to a small `command` shim that POSTs to itself. The shim is ~30 lines of any language; Go can embed a static binary that the install step drops alongside the main CLI.

### Codex CLI: capture path with hooks

```toml
# ~/.codex/hooks.toml or inline in ~/.codex/config.toml
[[hooks.PreToolUse]]
matcher = "^(Bash|shell|apply_patch)$"
[[hooks.PreToolUse.hooks]]
type = "command"
command = "askdao-hook-shim pre"
timeout = 10

[[hooks.PostToolUse]]
matcher = "^(Bash|shell|apply_patch)$"
[[hooks.PostToolUse.hooks]]
type = "command"
command = "askdao-hook-shim post"
timeout = 30
```

Two known limitations to mitigate:
1. **Project-local hooks load only when the project is trusted.** First run will prompt the user; document this.
2. **Hook coverage is incomplete** — `read_file`, `grep`, image generation, web_search, and many multi-agent sub-tools don't emit hook events (issue #20204). For a runtime-environment spec, this is largely OK; what you care about is `Bash` and `apply_patch`, both of which now fire reliably as of Codex v0.125+.

### Limitations and threats to validity

1. **Coverage is bounded by the session.** The agent might not exercise GPU code, or might never reach a feature flag that requires a new package. Mitigations: use a multi-session aggregator (run "install/run/test" three times with different prompts and union the specs); explicitly prompt for full coverage; pair with the existing static scan as a floor.
2. **Non-determinism.** The agent might choose `pip install` once and `uv add` the next time. The parser should normalize to canonical package names (e.g., resolve `psycopg2-binary` → `psycopg2` family) and keep both surface forms as evidence.
3. **Privacy and secrets.** Bash commands routinely contain credentials (`curl -H "Authorization: $TOKEN"`, env exports). The recorder must redact: never persist a token value, only the variable *name*. Anthropic's own OTEL guidance is explicit: prompts and tool args may contain secrets; redact at the collector. Apply the same rule here — redact in the hook itself, before persistence.
4. **Hook is local-user-trust.** Claude Code hooks run with the user's full permissions, no sandbox. Anthropic's docs warn explicitly that misconfigured hooks can delete files. The shim must be minimal, well-reviewed, and signed/checksummed in the askdao-cli release artifact.
5. **The "what counts as a system dependency" question is fuzzy at the edges.** `curl` is on every base image; `libpq-dev` is not. The parser needs a base-image inventory (per chosen `base_image:` in `askdao-agent.yml`) and only emit `system_packages` for things genuinely absent from the base.
6. **Two agents, two hook configs.** Maintaining parity between Claude Code's hook schema and Codex CLI's hook schema is ongoing work; the latter is younger and the coverage gaps (apply_patch, MCP, image gen) are still being filled by upstream PRs.
7. **Claude Code's OTEL traces are still in beta.** Anthropic's docs state span names and attributes may change between releases; don't make traces the primary capture path.
8. **Anthropic clarified its ToS in February 2026 and enforced it on April 4, 2026 to require API-key auth for the Agent SDK.** Anthropic updated its legal docs on February 19–20, 2026 to explicitly require API keys for the Agent SDK (per The Register, Feb 20, 2026: "Anthropic clarifies ban on third-party tool access to Claude"), and full enforcement for subscription OAuth tokens in third-party harnesses was enacted on April 4, 2026 at 3 PM ET. The current Claude Code legal doc (code.claude.com/docs/en/legal-and-compliance) states verbatim: *"Developers building products or services that interact with Claude's capabilities, including those using the Agent SDK, should use API key authentication."* Read-only observation (hooks, OTEL, transcript parsing) is unaffected, but if askdao-cli later wants to *drive* the agent programmatically, it must use API keys, not subscription auth.

### Comparative reference: what each layer captures

| Capability | Static (current v0.7) | Claude Code Hooks | Claude Code OTEL | Codex Hooks | Codex OTEL | strace |
|---|---|---|---|---|---|---|
| Detect declared deps in lockfiles | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Detect `apt-get install` ran at runtime | ❌ | ✅ verbatim command | ✅ with `OTEL_LOG_TOOL_DETAILS=1` | ✅ verbatim command | ❌ (tool name only) | ✅ syscall-level |
| Detect `pip install x` ran at runtime | ❌ | ✅ | ✅ | ✅ | ❌ | ✅ |
| Detect env-var name accessed | partial (grep code) | ✅ via command parsing | partial | ✅ | ❌ | ✅ |
| Detect failed install with error | ❌ | ✅ exit_code + output | ✅ | ✅ | partial | ✅ |
| Detect file created by agent | ❌ | ✅ Write/Edit hooks | partial | partial | ❌ | ✅ |
| Block/modify in-flight | ❌ | ✅ PreToolUse | ❌ | partial (no rewrites) | ❌ | ❌ |
| Cost/latency telemetry | ❌ | ❌ | ✅ | ❌ | ✅ aggregate | ❌ |
| Works in CI/headless | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ (root or seccomp) |

**Recommendation: Claude Code Hooks is the primary; Codex Hooks is the parity target; OTEL is for cost dashboards and corroboration; strace is the fallback if the agent fully escapes hook coverage.**

---

## Recommendations

### Stage 1 (v0.8, 2-3 weeks): Claude Code Hooks Recorder, MVP

1. Add `askdao-cli observe ./project` subcommand that:
   - Generates `.claude/settings.local.json` with HTTP hooks pointed at a local Go server (`PreToolUse`/`PostToolUse` on `Bash` and `Write|Edit|MultiEdit`, plus `SessionEnd`).
   - Starts the Go server on a random localhost port.
   - Exec's `claude` with an opinionated bootstrap prompt: *"Set up this project for production, install everything it needs, run the dev server, and verify it starts. Use real package managers, not mocks. Tell me when you're done."*
   - On `SessionEnd`, writes `.askdao/observations.jsonl` and a human-readable `.askdao/observations.md`.
2. Ship a parser that handles `apt-get`, `apt`, `pip`, `pip3`, `uv add`, `uv pip install`, `pipx`, `poetry add`, `npm install`/`i`, `yarn add`, `pnpm add`, `bun add`, `cargo add`, `go install`, `gem install`, `brew install`, `dnf install`, `apk add`, plus the failure-message→system-package lookup table.
3. **Benchmark to advance to Stage 2:** on a curated suite of 20 representative KOL projects (Django + Postgres, FastAPI + Redis, Next.js + Prisma, Streamlit + ML, etc.), the observed spec must produce a successfully-building image ≥80% of the time *without* the existing static layer running. If <80%, the failure modes go into the lookup table and the threshold is re-evaluated.

### Stage 2 (v0.9, 3-4 weeks): merge with static; add Codex parity

4. Merge observed evidence into the existing 4-layer with per-field provenance (`source: observed | static | llm | user`).
5. Add the Codex CLI hook shim (`askdao-hook-shim`) and `.codex/hooks.toml` generator; require Codex ≥0.125 (so `apply_patch` emits proper `tool_name`).
6. Add an interactive `askdao-cli diff` step that shows the KOL exactly what the observation added/changed vs. the static spec, before writing `askdao-agent.yml`. This is the trust-building step.
7. **Benchmark to advance to Stage 3:** observed-merged spec must reduce build-failure rate by ≥50% vs. v0.7 static-only on the same suite, and the KOL-edit rate on the generated `askdao-agent.yml` must drop by ≥30%.

### Stage 3 (v1.0, 4-6 weeks): multi-session aggregation, secrets safety, telemetry

8. Multi-session run mode: `askdao-cli observe --rounds 3 --prompts ./prompts/` runs the same observation with N different prompts (install, run, test) and unions the specs, surfacing inconsistencies.
9. Hardened redaction layer (per Anthropic's OTEL guidance + a custom secret-pattern matcher) with unit tests on common leak shapes.
10. Optional opt-in: forward anonymized observation summaries (no commands, no env values — just `{package: psycopg2, lang: python, base: python:3.12-slim}` counts) to AskDAO to improve the failure→hint lookup table across all KOLs.
11. **Benchmark to advance past v1.0:** the LLM recommendation layer's "I had to guess" rate drops below 10% on the suite; mean time from `askdao init` to first successful agent deploy drops below 5 minutes.

### What would change these recommendations

- **If Codex CLI implements `updatedInput` for PreToolUse** (issue #18491): the same architecture can do *active* spec enforcement (e.g., auto-rewrite `pip install` to `uv add` to match the project's existing lockfile), not just observation.
- **If Anthropic stabilizes traces out of beta** and adds a span attribute for the *exit code* of `Bash` calls: the OTEL path becomes viable as the primary capture instead of hooks. As of May 2026, this is not the case.
- **If a competing project ships "agent-trace → Dockerfile"** (someone will, this is open territory): consider whether askdao-cli's value is in the trace→spec layer or in the AskDAO platform integration. If the former, ship fast; if the latter, integrate with whoever wins.
- **If the KOL community pushes back on running their projects "for real" to record them** (citing privacy/cost): fall back to a "dry-run mode" where Claude Code is instructed to describe what it *would* run, hooks still fire on the descriptive Bash calls (they're real shell calls in plan mode), but no network/install side effects occur. This is lower-fidelity but acceptable for sensitive projects.

---

## Caveats

- **All capabilities verified are correct as of mid-May 2026.** Both Claude Code (v2.1.x line) and Codex CLI (v0.124–0.128 range) are evolving on a weekly cadence; specific event names, JSON shapes, and matcher semantics will shift. Treat the schema as semi-stable but pin to a tested Claude/Codex CLI version in the askdao-cli install check.
- **"Hooks run with the user's full permissions, no sandbox"** — this is Anthropic's stated security model. The shim is in the critical security path of any askdao-cli user; treat its code like you'd treat the Go binary itself (signed releases, reproducible builds, minimal dependencies).
- **The 80% / 50% / 30% / 10% benchmark numbers are starting points**, not validated thresholds. Run the v0.8 pilot, gather real numbers, and adjust. The published industry data on agent-driven environment generation is thin; AskDAO will be one of the first to produce it.
- **Privacy boundary**: hook payloads contain everything the agent ran. If askdao-cli ever sends observation data off-device (Stage 3 step 10), the consent and redaction story must be airtight and opt-in; treat it like SIEM-class data. Anthropic's docs explicitly warn that `tool_parameters` in their own OTEL stream can leak secrets if commands include them — the same warning applies here.
- **One inconsistency to flag:** Codex's documentation and Anthropic's documentation both use `PreToolUse`/`PostToolUse` naming, but their JSON schemas differ in subtle ways (Codex uses `tool_name` matched by regex against handler names like `apply_patch`/`shell`; Claude uses tool names like `Bash`/`Edit`/`Write`). Don't assume schema parity — write two adapter layers.
- **The "no existing project does observe→spec for agents" finding is a literature/search result, not a proof.** It is possible a small project exists that isn't surfaced by GitHub/web search in May 2026. Before public launch of askdao-cli v0.8, do a final 30-minute fresh search to confirm the gap is still open.