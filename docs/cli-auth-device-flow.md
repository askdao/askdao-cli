# askdao-cli Auth — OAuth 2.0 Device Code Flow

> **Status**: design v0.1 — 2026-05-13
> **Scope**: replace the `ASKDAO_CONDUCTOR_TOKEN` env-var auth shim with a one-command browser-bound login (`askdao auth login`) that yields a long-lived CLI token, persisted at `~/.config/askdao/credentials.json` (0600).
> **Why**: 手工从浏览器 cookie 复制 session token 对用户来说是死亡线 UX。OAuth 2.0 Device Code Flow（RFC 8628）是 `gh auth login` / `gcloud auth login` / `flyctl auth login` 同款工业标准，无需本地端口、跨 SSH 友好。
> **Trust anchor**: askdao-cli 是 AskDAO 体系唯一开源对外的子项目，鉴权层必须达到工业标准。

---

## 1. Sequence

```
user terminal                askdao-cli              server (api.askdao.ai)            web (askdao.ai)
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
                                                                                       (signed-in gate)
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
                                                                                       (auth: signed-in session)
                                                                                            ▼
                                                                                       persist approval (→ approved)
                                                                                       200 { approved: true }
   POST /api/v1/cli/auth/device/token  (every 5s, max 15min)
   { device_code }
                                          ────────►
                                                    while status=pending: 400 authorization_pending
                                                    on approved →
                                                      issue access_token (cli_xxx),
                                                      mark approval consumed
                                                    200 { access_token, user_id, user_email }
                                          ◄────────
   write ~/.config/askdao/credentials.json (0600)
   ✓ Logged in
```

**Anti-phishing invariant**: the same `user_code` must appear in both the terminal *and* the web page. The user is trained to compare before clicking Authorize. A malicious site cannot guess the code (≥ 32 bits entropy, restricted to non-ambiguous alphabet).

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
| token storage               | SHA-256 hex in DB                | DB leak ≠ token leak; matches `gh` convention  |
| `access_token` lifetime     | ∞ (until user revokes in web UI) | self-use mode; revoke is the off-switch        |

---

## 3. Server-side storage (overview)

The server keeps two records: one tracking the **device-flow state** (the pending → approved → consumed lifecycle of each `user_code`, with a short expiry), and one storing **issued CLI tokens** (only a SHA-256 hash — never plaintext).

**Plaintext token lifecycle**:
1. generated in-memory when the device-flow status transitions `approved → consumed`
2. SHA-256 hashed and persisted in the same transaction that consumes the approval
3. plaintext returned in the HTTP response **exactly once** — never re-readable from storage

This means a storage compromise cannot extract any usable CLI token, and a consumed approval cannot be replayed.

---

## 4. API surface (client view)

The CLI talks to four endpoints. Full request/response contracts live with the server implementation; the client-relevant shape:

- `POST /api/v1/cli/auth/device` — start a flow (auth: none, public). Returns `{ device_code, user_code, verification_uri, verification_uri_complete, expires_in, interval }`.
- `POST /api/v1/cli/auth/device/token` — poll with `{ device_code }` (auth: none — the `device_code` bears authority). `400 authorization_pending` until approved, then `200 { access_token, token_type, user_id, user_email }`. Terminal errors: expired / already-consumed / invalid device_code.
- `POST /api/v1/cli/auth/device/approve` — the browser approves with `{ user_code, device_name? }` (auth: signed-in session).
- `GET /api/v1/cli/auth/device/lookup?code=...` — read-only metadata for the approval page UI (auth: signed-in session).

All errors follow `{ "error": "<code>", "error_description": "<human>" }` (RFC 6749 §5.2).

---

## 5. Bearer token recognition

The server's auth layer recognizes `Authorization: Bearer cli_...` and resolves it via the CLI-token path (SHA-256 lookup + expiry / revocation check), otherwise falls back to the normal signed-in session. Existing endpoints keep working unchanged; the CLI gains a parallel access path.

---

## 6. CLI surface

### 6.1 `~/.config/askdao/credentials.json`

```json
{
  "version":      1,
  "server":       "https://api.askdao.ai",
  "user_id":      "usr_abc123",
  "user_email":   "you@example.com",
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

**Decision rationale (env first, not last)**: matches aws/gcloud/kubectl convention — explicitly-set env always wins. A user with a stable login never sees env; CI / temp-account-switch users set env once. Env-first removes one footgun (a stale credentials.json silently overriding a CI env).

---

## 7. Web UI (`askdao.ai`)

`/cli/auth?code=HJKL-4892`:

1. If `code` query param absent → show "Open this page only from your terminal" with brief explainer.
2. If not signed in → redirect `/sign-in?next=/cli/auth?code=HJKL-4892`.
3. Once signed in, fetch the lookup endpoint to render the confirmation card:

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

4. On Authorize click → approve endpoint → on 200 swap to success state ("You can close this window and return to your terminal").
5. On error → error state with link back to `/sign-in` or "run `askdao auth login` again".

The page is gated by the existing session and posts approval as the signed-in user.

---

## 8. Rollout

The flow spans three places, server contract first:

- **server** — the four endpoints + Bearer-token recognition in the auth layer
- **askdao-cli** — `internal/auth/` (credentials + device-flow HTTP), `cmd/askdao/auth.go` (3 commands), deploy token-resolution rewire
- **askdao.ai web** — the `/cli/auth` confirmation page (signed-in guard + confirmation card + approve)

---

## 9. Out of scope (v1) — tracked for v2

- Web UI to **list / revoke** existing CLI tokens (only the backend revocation column is in place). Until v2, revoke is manual.
- `askdao auth refresh` — tokens are perpetual, so no refresh; if compromised, revoke + relogin.
- `slow_down` rate-limiting on `/device/token` polling — fixed 5s interval is enough for one CLI at a time.
- "Deny" button in the web UI — Cancel just navigates away (status stays pending until expiry).
- Per-scope tokens — single `agent.deploy` scope hardcoded; multi-scope arrives when we ship more authed CLI commands.
- HMAC / signed device_code — not needed when the token hash already gates lookups; entropy + uniqueness suffice.

---

## 10. Decision log

| Decision                                                         | Rationale                                                                                  |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Device Code Flow over loopback callback                          | A user on SSH / remote dev / locked-down laptop has no local port; device flow is portable |
| Token hashed in storage (SHA-256), plaintext only on response    | storage leak ≠ token leak; matches `gh auth` convention; cheap to implement                |
| user_code charset excludes `0OIl1`                                | the user might dictate it over the phone or read it on small screens                       |
| `cli_` / `dev_` prefixes                                          | grep-friendly; middleware dispatches by prefix in O(1)                                      |
| Plaintext token never persists past the single poll response     | eliminates the "approved but unconsumed" replay window                                     |
| env-var > credentials.json (not the other way)                   | matches aws/gcloud/kubectl; lets CI override without erasing local config                  |
| Tokens perpetual by default, revoke via web UI                   | self-use mode — TTL adds friction without preventing the realistic attack                  |
| `/device/lookup` is a separate read endpoint                     | keeps `/approve` POST-only and idempotent-friendly; the web page can show metadata pre-action |
