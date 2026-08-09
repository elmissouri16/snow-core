# ChatGPT authentication research

## What snow supports

`snow` recognizes ChatGPT/Codex subscription credentials stored under the
`chatgpt` key in `auth.json`:

```json
{
  "chatgpt": {
    "type": "oauth",
    "access": "<access token>",
    "refresh": "<refresh token>",
    "expires": 1735689600,
    "accountId": "<ChatGPT workspace id>"
  }
}
```

The side-effect-free check is available from Go as
`internal/provider/chatgpt.CheckAuth` and from the CLI:

```bash
snow auth check chatgpt
```

The check never sends or prints token material. It verifies that the stored
credential is OAuth with an access token, reports expiration, and extracts the
ChatGPT account and optional plan claims. Token exchange may derive missing
metadata from `id_token`, but Snow never persists the raw ID token.

Use `snow login chatgpt` for loopback browser PKCE login,
`snow login chatgpt --device-code` for the 15-minute device flow, or
`--no-open` to print the browser URL without launching it. The browser flow
validates state and PKCE and accepts a complete pasted callback URL as a
headless fallback, including when callback port 1455 is occupied. In the TUI,
`/login` → `chatgpt` offers browser login (with device fallback),
device-code login, and these compatible local import sources:

- Codex: `~/.codex/auth.json` (`tokens.access_token`, `refresh_token`, `account_id`)
- Pi: `~/.pi/agent/auth.json` (`openai-codex` OAuth entry)
- OpenCode: `$XDG_DATA_HOME/opencode/auth.json` or
  `~/.local/share/opencode/auth.json` (`openai` OAuth entry)

Selecting a source requires a ChatGPT account ID, validates and atomically
imports it as Snow's `chatgpt` credential. Refreshable expired imports are
accepted and refreshed before use.
Tokens are never shown in the picker. `snow auth check chatgpt` remains strictly
side-effect-free; runtime resolution refreshes tokens that expire within five
minutes under a cross-process auth-store lock and atomically persists rotated
refresh tokens. A pre-stream HTTP 401 forces one guarded refresh and one retry,
while reusing a newer credential already rotated by another process. Permanent
`invalid_grant`-class failures require login; transient/network failures remain
retryable and secret-free.

`/model` uses authenticated `GET /backend-api/codex/models?client_version=0.147.0`
discovery. Raw records are cached for 15 minutes under
`~/.snow/cache/chatgpt-models/<origin-and-account-hash>.json` with versioned
origin/account metadata, ETags, and mode `0600`.
Only `visibility=list` entries are shown; `supported_in_api=false` does not hide
subscription-only models. Snow maps low/medium/high reasoning and intentionally
omits xhigh/max/ultra. A same-account stale cache, then a bundled snapshot,
keeps startup usable offline. The auth store lock is `~/.snow/auth.json.lock`
and is also `0600`.

## Research findings

### pi

Pi models ChatGPT Plus/Pro as the `openai-codex` OAuth provider:

- Provider registration: [`packages/ai/src/providers/openai-codex.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/ai/src/providers/openai-codex.ts)
- OAuth flow and token refresh: [`packages/ai/src/auth/oauth/openai-codex.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/ai/src/auth/oauth/openai-codex.ts)
- Side-effect-free auth availability check and later refresh: [`packages/ai/src/models.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/ai/src/models.ts)
- Auth format and provider documentation: [`packages/coding-agent/docs/providers.md`](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/providers.md)

The important separation is `checkAuth` versus runtime resolution: pi considers
an OAuth entry configured without refreshing it during the check; resolution
refreshes expired credentials while holding the credential-store lock.

Pi's current Codex flow uses (the client ID is public configuration, not a
secret, but may change):

- client ID `app_EMoamEEZ73f0CkXaXp7hrann`
- issuer `https://auth.openai.com`
- PKCE browser login at `/oauth/authorize`
- token exchange and refresh at `/oauth/token`
- optional device flow at `/api/accounts/deviceauth/usercode` and
  `/api/accounts/deviceauth/token`
- access-token namespace `https://api.openai.com/auth`, field `chatgpt_account_id`
- ChatGPT Codex requests at `https://chatgpt.com/backend-api/codex/responses`
  with `Authorization`, `chatgpt-account-id`, and `originator` headers

### Official OpenAI Codex

The official Codex repository uses the same device-code endpoints and a
15-minute polling window:

- [`codex-rs/login/src/device_code_auth.rs`](https://github.com/openai/codex/blob/main/codex-rs/login/src/device_code_auth.rs)
- [`codex-rs/login/src/auth/manager.rs`](https://github.com/openai/codex/blob/main/codex-rs/login/src/auth/manager.rs)

It also treats refresh-token failures as a re-login condition and persists
rotated access/refresh tokens atomically. Snow distinguishes permanent token
rejection from transient endpoint/network failure rather than forcing a login
for every refresh error.

### OpenCode

The current [OpenCode Go repository](https://github.com/opencode-ai/opencode)
does not implement ChatGPT subscription OAuth. Its provider adapters load API
keys and use provider-specific HTTP clients. Therefore it is useful as a
provider-interface reference, but not as a source for ChatGPT OAuth behavior.

## Compatibility boundary

ChatGPT subscription OAuth remains separate from OpenAI API-key authentication.
Snow uses the OAuth client and product-backend routes exercised by official
Codex, not the public API-key `/v1/models` contract. Those routes can change;
constants, wire records, cache parsing, and the bundled fallback remain isolated
in `internal/provider/chatgpt`. Authenticated requests accept redirects only when scheme and host are exactly
unchanged, reject URL userinfo, and therefore block HTTPS downgrade and
cross-origin bearer forwarding. Vision-capable catalog records are backed by
validated image data-URI encoding. Completed encrypted reasoning items are
persisted only as non-rendered opaque continuity blocks and replayed before
function calls/outputs; encrypted payloads are never emitted as UI/log events.
