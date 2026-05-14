# askdao-cli Auth — OAuth 2.0 Device Code Flow

> **Status**: design v0.1 — 2026-05-13
> **Scope**: replace the `ASKDAO_CONDUCTOR_TOKEN` env-var auth shim with a one-command browser-bound login (`askdao auth login`) that yields a long-lived CLI token, persisted at `~/.config/askdao/credentials.json` (0600).
> **Why**: 手工从浏览器 cookie 复制 session token 对 KOL 来说是死亡线 UX。OAuth 2.0 Device Code Flow（RFC 8628）是 `gh auth login` / `gcloud auth login` / `flyctl auth login` 同款工业标准，无需本地端口、跨 SSH 友好、KOL 见过同类交互不发慌。
> **Trust anchor**: askdao-cli 是 AskDAO 体系**唯一开源对外**的子项目，鉴权层必须达到工业标准。

---

## 1. Sequence

```
KOL terminal                askdao-cli              conductor (api.askdao.ai)         web (askdao.ai)
─────────────                ──────────              ─────────────────────────         ────────────────
$ askdao auth login
                             POST /api/v1/cli/auth/device
                             { client_name }
                                          ────────►
                                                    { device_code, user_code,
                                                      verification_uri,
                                                      verification_uri_complete,
                                                      expires_in: 900,
                                                      interval: 5 }
                                          ◄────────
   Your code: HJKL-4892
   Open https://askdao.ai/cli/auth?code=HJKL-4892
                             open browser  ───────────────────────────────────────►
                                                                                       GET /cli/auth?code=HJKL-4892
                                                                                       (better-auth gate)
                                                                                            ▼
                                                                                       sign in if needed
                                                                                            ▼
                                                                                       show user_code prominently
                                                                                       "If this matches your terminal,
                                                                                        click Authorize"
                                                                                            ▼
                                                                                       [Authorize] clicked
                                                                                            │
                                                                                            ▼
                                                                                       POST /api/v1/cli/auth/device/approve
                                                                                       { user_code, name? }
                                                                                       (auth: better-auth session)
                                                                                                                 ◄────
                                                                                       UPDATE cli_device_auth
                                                                                         SET status='approved',
                                                                                             user_id, approved_at
                                                                                                                 ────►
                                                                                       200 { approved: true }
   POST /api/v1/cli/auth/device/token  (every 5s, max 15min)
   { device_code }
                                          ────────►
                                                    while status=pending: 400 authorization_pending
                                                    status=approved →
                                                      generate access_token (cli_xxx)
                                                      INSERT cli_token (token_hash, user_id, name)
                                                      UPDATE cli_device_auth SET status='consumed'
                                                      200 { access_token, user_id, user_email }
                                          ◄────────
   write ~/.config/askdao/credentials.json (0600)
   ✓ Logged in as sam@askdao.ai
```

**Anti-phishing invariant**: the same `user_code` must appear in both the terminal *and* the web page. KOL is trained to compare before clicking Authorize. A malicious site cannot guess the code (≥ 32 bits entropy, restricted to non-ambiguous alphabet).

---

## 2. Constants

| Constant                    | Value                            | Why                                            |
| --------------------------- | -------------------------------- | ---------------------------------------------- |
| `device_code` length        | 32 random bytes → URL-safe b64   | ~256 bits entropy, opaque to user              |
| `user_code` format          | `XXXX-XXXX` from `BCDFGHJKLMNPQRSTVWXZ23456789` (20 chars) | 20⁸ ≈ 25.6 G combinations, no 0/O/1/I/l |
| `device_code` prefix        | `dev_`                           | grep-friendly                                  |
| `access_token` length       | 32 random bytes → URL-safe b64   | matches device_code entropy                    |
| `access_token` prefix       | `cli_`                           | middleware dispatch key                        |
| `expires_in` (device flow)  | 900 s (15 min)                   | OAuth recommendation                           |
| polling `interval`          | 5 s                              | OAuth recommendation                           |
| token storage               | SHA-256 hex (64 chars) in DB     | DB leak ≠ token leak; matches `gh` convention  |
| `access_token` lifetime     | ∞ (until user revokes in web UI) | KOL self-use mode; revoke is the off-switch    |

---

## 3. PG schema (alembic 027 — assumed next free)

```sql
CREATE TABLE cli_device_auth (
    id               BIGSERIAL PRIMARY KEY,
    device_code_hash CHAR(64)    NOT NULL UNIQUE,                     -- SHA-256(device_code)
    user_code        VARCHAR(9)  NOT NULL,                            -- 'XXXX-XXXX'
    status           VARCHAR(16) NOT NULL DEFAULT 'pending',          -- pending|approved|consumed|expired|denied
    user_id          VARCHAR(64) REFERENCES users(id) ON DELETE CASCADE,
    scope            VARCHAR(64) NOT NULL DEFAULT 'agent.deploy',
    client_name      VARCHAR(128),                                    -- 'askdao-cli/0.1.0 darwin/arm64'
    device_name      VARCHAR(128),                                    -- user-supplied via --name, default hostname
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at       TIMESTAMPTZ NOT NULL,                            -- created_at + 15min
    approved_at      TIMESTAMPTZ,
    consumed_at      TIMESTAMPTZ
);
CREATE UNIQUE INDEX cli_device_auth_pending_user_code_uniq
    ON cli_device_auth (user_code) WHERE status = 'pending';         -- collision-free among live codes
CREATE INDEX cli_device_auth_expires_idx
    ON cli_device_auth (expires_at) WHERE status = 'pending';

CREATE TABLE cli_token (
    id           BIGSERIAL PRIMARY KEY,
    user_id      VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   CHAR(64)    NOT NULL UNIQUE,                          -- SHA-256(access_token)
    name         VARCHAR(128),                                          -- 'macbook-pro' or hostname
    scope        VARCHAR(64) NOT NULL DEFAULT 'agent.deploy',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,                                          -- NULL = perpetual
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX cli_token_user_active_idx
    ON cli_token (user_id) WHERE revoked_at IS NULL;
```

**Plaintext token lifecycle**:
1. generated in-memory by the `/device/token` polling handler when status transitions `approved → consumed`
2. SHA-256 hashed → INSERT cli_token (token_hash) inside the same transaction that flips `cli_device_auth.status='consumed'`
3. plaintext returned in HTTP response **exactly once** — never re-readable from the DB

This means a DB compromise cannot extract any usable CLI token, and an unconsumed approval cannot be replayed (consumed_at gate).

---

## 4. API contract

### 4.1 `POST /api/v1/cli/auth/device`

**Auth**: none (public — anyone can request a code).

**Request body**:
```json
{ "client_name": "askdao-cli/0.1.0 darwin/arm64" }
```

**Response 201**:
```json
{
  "device_code":                "dev_aB3kQ_xY...",
  "user_code":                  "HJKL-4892",
  "verification_uri":           "https://askdao.ai/cli/auth",
  "verification_uri_complete":  "https://askdao.ai/cli/auth?code=HJKL-4892",
  "expires_in":                 900,
  "interval":                   5
}
```

### 4.2 `POST /api/v1/cli/auth/device/token`

**Auth**: none — `device_code` is the bearer of authority.

**Request body**:
```json
{ "device_code": "dev_aB3kQ_xY..." }
```

**Response 200** (status was `approved`, now transitions to `consumed`):
```json
{
  "access_token": "cli_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "token_type":   "Bearer",
  "user_id":      "usr_abc123",
  "user_email":   "sam@askdao.ai"
}
```

**Response 400** `authorization_pending` — KOL has not approved yet. CLI keeps polling.

**Response 400** `slow_down` — (reserved; not implemented v1) CLI should add 5s to its interval.

**Response 410** `expired_token` — `expires_at < now()` and status was `pending`.

**Response 410** `access_denied` — KOL clicked Cancel/Reject (reserved; not implemented v1).

**Response 410** `already_consumed` — `device_code` was already redeemed once. CLI should stop and report misuse.

**Response 404** `invalid_device_code` — unknown / malformed device_code.

All errors follow `{ "error": "<code>", "error_description": "<human>" }` (RFC 6749 §5.2).

### 4.3 `POST /api/v1/cli/auth/device/approve`

**Auth**: better-auth session (cookie) — the human in the browser.

**Request body**:
```json
{ "user_code": "HJKL-4892", "device_name": "macbook-pro" }
```

**Response 200**: `{ "approved": true }`. Status transitions `pending → approved` and `user_id`, `device_name`, `approved_at` are filled. The CLI's *next* poll will return the access_token.

**Response 404** `invalid_user_code` — no `pending` row with this code.

**Response 410** `expired_user_code` — row exists but `expires_at < now()` (transition to `expired` lazily).

### 4.4 `GET /api/v1/cli/auth/device/lookup?code=HJKL-4892`

**Auth**: better-auth session.

Read-only metadata for the approval page UI (so it can show `client_name`, `device_name` hint, `expires_at` countdown). Returns `{ client_name, suggested_device_name, expires_at }` or 404 if invalid/expired.

---

## 5. Auth middleware change (conductor)

Current `app.auth.middleware.get_current_user` resolves better-auth session from cookie. Extend it to short-circuit on `Authorization: Bearer cli_...`:

```python
async def get_current_user(request, db) -> RequestContext:
    bearer = _extract_bearer(request)
    if bearer and bearer.startswith("cli_"):
        return await _resolve_cli_token(bearer, db)   # new
    return await _resolve_better_auth(request, db)    # existing
```

`_resolve_cli_token`:
1. SHA-256(bearer)
2. SELECT * FROM cli_token WHERE token_hash=? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())
3. UPDATE last_used_at = now() (best-effort, fire-and-forget if it adds latency)
4. Return RequestContext(user_id=row.user_id, source='cli_token', token_id=row.id)

Existing endpoints keep working unchanged; the CLI just gains a parallel access path.

---

## 6. CLI surface

### 6.1 `~/.config/askdao/credentials.json`

```json
{
  "version":      1,
  "server":       "https://api.askdao.ai",
  "user_id":      "usr_abc123",
  "user_email":   "sam@askdao.ai",
  "access_token": "cli_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "created_at":   "2026-05-13T12:00:00Z"
}
```

Path resolves via XDG: `$XDG_CONFIG_HOME/askdao/credentials.json` → fall back to `~/.config/askdao/credentials.json` → on Windows `%AppData%\askdao\credentials.json`. Permissions: 0600 file, 0700 parent dir, created on demand.

### 6.2 Commands

```
askdao auth login    [--name <device-name>] [--server <url>]
askdao auth status
askdao auth logout
```

- `login` — runs the device flow; opens default browser via `open` / `xdg-open` / `cmd /c start`; prints `user_code` to stderr; polls `/device/token`; writes credentials on success.
- `status` — prints server + user_email + token age + credentials path; exit 1 if not logged in.
- `logout` — deletes the credentials file; does NOT revoke server-side (revoke is a web concern).

### 6.3 Token resolution order in `askdao agent deploy` and any future authed command

```
1. $ASKDAO_CONDUCTOR_TOKEN env var          ← CI / one-off override (matches aws/gcloud)
2. ~/.config/askdao/credentials.json        ← interactive default
3. error: "Run `askdao auth login` first."
```

Server URL resolution:
```
1. --server flag                            ← rarely used
2. $ASKDAO_CONDUCTOR_URL env var            ← CI override
3. credentials.json `server` field          ← bound at login time
4. https://api.askdao.ai                    ← compiled default
```

**Decision rationale (env first, not last)**: matches aws/gcloud/kubectl convention — explicitly-set env always wins. KOL with stable login never sees env; CI / temp-account-switch users set env once. The previous task description said "credentials first, env fallback" but env-first is the industry default and removes one footgun (a stale credentials.json silently overriding a CI env). Open for revisit if 哥 prefers credentials-first.

---

## 7. Web UI (`askdao-ai-web`)

`/cli/auth?code=HJKL-4892`:

1. If `code` query param absent → show "Open this page only from your terminal" with brief explainer.
2. If not signed in → redirect `/sign-in?next=/cli/auth?code=HJKL-4892`.
3. Once signed in, fetch `GET /api/v1/cli/auth/device/lookup?code=...` to render the confirmation card:

   ```
   ┌─────────────────────────────────────────┐
   │ Authorize device                         │
   │                                          │
   │ A device wants to act on your behalf at  │
   │ askdao.ai. If this code matches what     │
   │ your terminal shows, click Authorize.    │
   │                                          │
   │       ╭─────────────────╮               │
   │       │   HJKL-4892     │   ← 4xl mono   │
   │       ╰─────────────────╯               │
   │                                          │
   │ askdao-cli/0.1.0 darwin/arm64            │
   │ Device name: [macbook-pro          ]     │
   │ Expires in 14:23                         │
   │                                          │
   │   [ Cancel ]      [ Authorize device ]   │
   └─────────────────────────────────────────┘
   ```

4. On Authorize click → `POST /api/v1/cli/auth/device/approve` → on 200 swap to success state ("You can close this window and return to your terminal").
5. On 410 / 404 → error state with link back to `/sign-in` or "run `askdao auth login` again".

No new better-auth fields; the page is gated by the existing session and posts approval as the signed-in user.

---

## 8. Cross-repo PR plan

| Repo                          | Branch                          | Scope                                                                                            | PR order |
| ----------------------------- | ------------------------------- | ------------------------------------------------------------------------------------------------ | -------- |
| `askdao-cloud-conductor`      | `feature/cli-device-auth`       | alembic 027, 4 endpoints, middleware extension, tests, GEB                                       | 1 (merge & deploy first — contract source) |
| `askdao-cli`                  | `feature/auth-device-flow`      | `internal/auth/` (credentials + device-flow HTTP), `cmd/askdao/auth.go` (3 commands), deploy.go rewires token resolution, tests, GEB | 2 |
| `askdao-ai-web`               | `feature/cli-auth-page`         | `/cli/auth` page (signed-in guard + confirmation card + approve fetch), GEB                       | 3 |

Each PR self-contained; PR #2 / #3 can be opened in parallel with PR #1, but must wait for PR #1 to merge + deploy before E2E smoke.

---

## 9. Out of scope (v1) — tracked for v2

- Web UI to **list / revoke** existing CLI tokens (only the backend `revoked_at` column is in place). Until v2 ships, manual SQL revoke.
- `askdao auth refresh` — tokens are perpetual, so no refresh; if compromised, revoke + relogin.
- `slow_down` rate-limiting on `/device/token` polling — fixed 5s interval is enough for one CLI at a time.
- "Deny" button in the web UI — Cancel just navigates away (status stays pending until expiry).
- Per-scope tokens — single `agent.deploy` scope hardcoded; multi-scope arrives when we ship more authed CLI commands.
- HMAC / signed device_code — not needed when token_hash already gates DB lookups; entropy + UNIQUE constraint suffice.

---

## 10. Decision log

| Decision                                                         | Rationale                                                                                  |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Device Code Flow over loopback callback                          | KOL on SSH / remote dev / locked-down work laptop has no local port; device flow is portable |
| Token hashed in DB (SHA-256), plaintext only on response         | DB leak ≠ token leak; matches `gh auth` convention; cheap to implement                     |
| user_code charset excludes `0OIl1`                                | KOL might dictate it over the phone or read it on small screens                            |
| `cli_` / `dev_` prefixes                                          | Grep-friendly; middleware dispatches by prefix in O(1)                                      |
| Plaintext token never persists past the single poll response     | Eliminates the "approved but unconsumed" replay window                                     |
| env-var > credentials.json (not the other way)                   | Matches aws/gcloud/kubectl; lets CI override without erasing local config                  |
| Tokens perpetual by default, revoke via web UI                   | KOL self-use mode — TTL adds friction without preventing the realistic attack              |
| `/device/lookup` is a separate read endpoint                     | Keeps `/approve` POST-only and idempotent-friendly; web page can show metadata pre-action  |
