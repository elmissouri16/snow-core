# ChatGPT authentication

Snow authenticates ChatGPT/Codex subscriptions through OAuth, not an OpenAI API
key. This document describes the supported credential shape, local import
sources, login flows, token refresh, status checks, the authenticated model
catalog, and the hardened Codex inference boundary.

> **Note:** These are the OAuth client and product-backend routes exercised by
> official Codex. They can change; constants, wire records, cache parsing, and
> the bundled fallback remain isolated in `internal/provider/chatgpt`.

## On this page

- [Supported credential shape](#supported-credential-shape)
- [Source import locations](#source-import-locations)
- [Login](#login)
- [Refresh, locking, and rotation](#refresh-locking-and-rotation)
- [Status checks and JWT metadata](#status-checks-and-jwt-metadata)
- [Authenticated model catalog](#authenticated-model-catalog)
- [Codex inference and SSE](#codex-inference-and-sse)
- [Research notes](#research-notes)
- [Compatibility boundary](#compatibility-boundary)
- [Related documents](#related-documents)

## Supported credential shape

Snow recognizes ChatGPT/Codex subscription credentials stored under the
`chatgpt` key in `~/.snow/auth.json`:

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

`type` must be `oauth`, and `access` must be present. `refresh`, `expires`
(Unix epoch seconds), and `accountId` are optional but expected for refreshable
and account-scoped sessions.

## Source import locations

The TUI picker discovers compatible local credentials in this order: OpenCode,
Pi, then Codex.

| Source | Paths | Entry keys |
|---|---|---|
| OpenCode | `$XDG_DATA_HOME/opencode/auth.json`, `~/.local/share/opencode/auth.json`, or `~/.opencode/auth.json` | `openai`, `openai-codex`, or `chatgpt` OAuth entry |
| Pi | `~/.pi/agent/auth.json` | `openai-codex` or `chatgpt` OAuth entry |
| Codex | `~/.codex/auth.json` | `tokens.access_token`, `refresh_token`, `account_id` |

Snow does not import an OpenAI API-key-only Codex login as ChatGPT OAuth; the
Codex source must be a `chatgpt` auth mode. The lower-level importer remains
available for explicit compatibility use.

## Login

### Browser PKCE login

Run:

```sh
snow login chatgpt
```

This starts a loopback browser PKCE flow. The browser flow validates state and
PKCE and accepts a complete pasted callback URL as a headless fallback,
including when callback port 1455 is occupied. Use `--no-open` to print the
browser URL without launching it:

```sh
snow login chatgpt --no-open
```

### Device-code login

Run:

```sh
snow login chatgpt --device-code
```

The device flow uses the official Codex device-auth endpoints with a 15-minute
polling window.

### TUI picker

In the TUI, `/login` opens a centered authentication card. Choosing `chatgpt`
keeps known local account/workspace IDs, unrestricted browser login (with device
fallback), device-code login, and authorization progress in that same card. Esc
from the account/method list returns to the provider list when it was opened from
there. Esc during authorization cancels that attempt and restores the
account/method list instead of dismissing the complete login flow.

The picker groups duplicate source entries by account ID and displays source
names without tokens. Selecting a known account starts a fresh Snow browser
OAuth flow with the official Codex `allowed_workspace_id` restriction and
validates the returned token claim before saving it. Snow never copies
OpenCode, Pi, or Codex token material in this TUI flow. Unrestricted
browser/device login remains available when a new or different account is
intended.

Snow uses official Codex scopes and the same form-encoded token refresh
contract used by Codex, Pi, and OpenCode. Tokens are never shown in the picker.

## Refresh, locking, and rotation

Runtime resolution goes through Snow's generic auth service, which delegates
token exchange to the ChatGPT OAuth driver. Tokens that expire within five
minutes are refreshed under a provider-specific cross-process refresh lock,
while the global auth-store lock is held only for the fresh read and
compare-and-swap write. This keeps unrelated credential operations responsive
during network I/O and atomically persists rotated refresh tokens.

A pre-stream HTTP 401 forces one guarded refresh and one retry, while reusing a
newer credential already rotated by another process. Permanent
`invalid_grant`-class failures require login; transient and network failures
remain retryable and secret-free.

The auth store lock is `~/.snow/auth.json.lock`; provider refresh serialization
uses a hashed `auth.json.<provider-hash>.refresh.lock`. Both are mode `0600`.

## Status checks and JWT metadata

The side-effect-free check is available from Go as
`internal/provider/chatgpt.CheckAuth` and from the CLI:

```sh
snow auth check chatgpt
```

The check never sends or prints token material. It verifies that the stored
credential is OAuth with an access token, reports expiration, and extracts the
ChatGPT account and optional plan claims from the access-token JWT. Token
exchange may derive missing metadata from `id_token`, but Snow never persists
the raw ID token.

## Authenticated model catalog

`/model` uses authenticated
`GET /backend-api/codex/models?client_version=0.147.0` discovery. Raw records
are cached for 15 minutes under
`~/.snow/cache/chatgpt-models/<origin-and-account-hash>.json` with versioned
origin/account metadata, ETags, and mode `0600`.

Only records with `visibility=list` or unset visibility are shown;
`supported_in_api=false` does not hide subscription-only models. Snow maps each
model's advertised inference effort (`minimal`, `low`, `medium`, `high`,
`xhigh`, or `max`) into normalized protocol thinking levels; `off` remains the
explicit no-reasoning selection. Codex may also advertise `ultra`, but upstream
defines it as a host preset that enables proactive multi-agent behavior rather
than a valid Responses `reasoning.effort`. Snow therefore does not expose it as
a ChatGPT thinking level or send it to inference.

Authenticated account catalogs are authoritative: a model missing from the
selected account is not merged back from the bundled snapshot, and an
unavailable active model is replaced by a compatible account model.
Authenticated sessions fall back only to a same-account cache; they never inject
a bundled model after account discovery fails. The adapter retains a bundled
snapshot for isolated transport fallback and tests, but Snow's authenticated
runtime does not publish ChatGPT models to the TUI, SDK, RPC, or subagent
inventories before a usable OAuth credential resolves. Logout removes the
account catalog from those inventories immediately.

## Codex inference and SSE

Codex inference requests attach one SHA-256 affinity value derived from the Snow
session, active branch, and request purpose as `prompt_cache_key`, `session-id`,
and `x-client-request-id`; raw session and branch IDs never leave the process.

The request identifies `User-Agent: snow`, explicitly enables automatic and
parallel tool selection (execution inside Snow remains serial), and omits
sampling/output-limit fields unsupported by the subscription path. JSON bodies
of at least 32 KiB use zstd compression.

The adapter classifies network failures, HTTP 408/425/5xx, immediate stream
overload/truncation, and temporary 429 throttling with bounded retry advice.
The central agent policy owns cancellation-aware exponential backoff and honors
`Retry-After`, preventing adapter and goal attempt counts from multiplying.
HTTP 402, a second 401, authentication/validation errors, and hard quota are
terminal. After normalized activity, recovery is a new durable continuation,
never a replay of streamed tool calls.

Responses/SSE parsing bounds individual and aggregate events, tool-call
count/identities/arguments, retained reasoning, error codes, and request IDs.
A limit violation emits one normalized stream error and stops parsing. A stream
must terminate with `response.completed`, `response.done`,
`response.incomplete`, or `[DONE]`; clean EOF without one is an error rather
than a successful partial answer.

Vision-capable catalog records are backed by validated image data-URI encoding.
Completed encrypted reasoning items are persisted only as non-rendered opaque
continuity blocks and replayed before function calls/outputs; encrypted
payloads are never emitted as UI or log events.

## Research notes

### Pi

Pi models ChatGPT Plus/Pro as the `openai-codex` OAuth provider:

- Provider registration: [`packages/ai/src/providers/openai-codex.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/ai/src/providers/openai-codex.ts)
- OAuth flow and token refresh: [`packages/ai/src/auth/oauth/openai-codex.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/ai/src/auth/oauth/openai-codex.ts)
- Side-effect-free auth availability check: [`packages/ai/src/models.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/ai/src/models.ts)
- Auth format and provider documentation: [`packages/coding-agent/docs/providers.md`](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/providers.md)

Pi separates `checkAuth` from runtime resolution: the check considers an OAuth
entry configured without refreshing it, while resolution refreshes expired
credentials while holding the credential-store lock.

Pi's current Codex flow uses the following endpoints and values (the client ID
is public configuration, not a secret, but may change):

- client ID `app_EMoamEEZ73f0CkXaXp7hrann`;
- issuer `https://auth.openai.com`;
- PKCE browser login at `/oauth/authorize`;
- token exchange and refresh at `/oauth/token`;
- optional device flow at `/api/accounts/deviceauth/usercode` and
  `/api/accounts/deviceauth/token`;
- access-token namespace `https://api.openai.com/auth`, field
  `chatgpt_account_id`;
- ChatGPT Codex requests at `https://chatgpt.com/backend-api/codex/responses`
  with `Authorization`, `chatgpt-account-id`, and `originator` headers.

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

Current OpenCode (`anomalyco/opencode`) implements ChatGPT Plus/Pro OAuth in
[`packages/opencode/src/plugin/openai/codex.ts`](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/plugin/openai/codex.ts).
It stores the credential under `openai`, extracts `accountId` from OAuth tokens,
refreshes with a form-encoded `/oauth/token` request, rewrites Responses calls
to `https://chatgpt.com/backend-api/codex/responses`, and sends bearer,
`ChatGPT-Account-Id`, `originator`, user-agent, and session headers. Its OAuth
model filter explicitly allows `gpt-5.3-codex-spark`.

The backend still applies account entitlements: a Spark-capable OpenCode account
and a different browser-selected Snow account are not interchangeable merely
because both report a ChatGPT Plus plan. Snow therefore uses the discovered
account ID only as an official OAuth workspace restriction, obtains its own
token, and rejects a login whose returned claim belongs to a different account.

## Compatibility boundary

ChatGPT subscription OAuth remains separate from OpenAI API-key authentication.
Snow uses the OAuth client and product-backend routes exercised by official
Codex, not the public API-key `/v1/models` contract.

Authenticated requests accept redirects only when scheme and host are exactly
unchanged, reject URL userinfo, and therefore block HTTPS downgrade and
cross-origin bearer forwarding.

## Related documents

- [Security](security.md) — credential handling and network boundaries
- [Configuration](configuration.md) — provider and auth storage paths
- [Using Snow](using-snow.md) — login, provider, and model workflows
- [README](../README.md) — quick start and provider overview
