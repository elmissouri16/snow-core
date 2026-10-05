# ChatGPT authentication

Use a ChatGPT/Codex subscription with Snow through OAuth rather than an OpenAI
API key. This guide covers browser and device login, supported credential
imports, token refresh, status checks, the authenticated model catalog, and
account selection.

> **Note:** ChatGPT product authentication can change independently of Snow.
> Run `snow auth check chatgpt` to inspect your current login without refreshing
> it.

## On this page

- [Supported credential shape](#supported-credential-shape)
- [Source import locations](#source-import-locations)
- [Login](#login)
- [Named accounts and manual selection](#named-accounts-and-manual-selection)
- [Quota observations and continuity](#quota-observations-and-continuity)
- [Refresh, locking, and rotation](#refresh-locking-and-rotation)
- [Status checks and JWT metadata](#status-checks-and-jwt-metadata)
- [Authenticated model catalog](#authenticated-model-catalog)
- [Codex inference and SSE](#codex-inference-and-sse)
- [Compatibility boundary](#compatibility-boundary)
- [Related documents](#related-documents)

## Supported credential shape

Snow recognizes ChatGPT/Codex subscription credentials stored under the
`chatgpt` key in `~/.snow/auth.json` for the legacy `default` account:

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

Named accounts use `chatgpt/account/<name>` keys with the same credential shape.
The alias is local routing metadata, distinct from the upstream workspace
`accountId`; tokens remain exclusively in the credential store.

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

## Named accounts and manual selection

Save an additional login without replacing the default credential:

```sh
snow login chatgpt --account work
snow auth check chatgpt --account work
snow accounts chatgpt                 # also: snow auth accounts chatgpt
snow auth use chatgpt work            # global preferred account
snow --provider chatgpt --account work
snow logout chatgpt --account work
```

Aliases use 1–64 lowercase ASCII letters or digits, with internal `.`, `_`, or
`-`; the first and last characters must be letters or digits. `default` keeps
the legacy `chatgpt` entry. Login rejects the same upstream identity saved under
a different alias; aliases are not separate subscriptions merely because their
names differ. Enumeration discovers stored aliases directly, with no config
registry. Named login persists its credential without selecting it as preferred
or fetching the default account's model catalog.

Runtime selection is explicit `--account`, then global
`chatgpt_accounts.preferred`, then `default`. Management commands without
`--account` always target `default`, even when another account is preferred.
`auth check`, `accounts`, and `auth accounts` are local-only: they neither
refresh tokens nor discover models. CLI listings show stored aliases only,
including `default` only if its credential exists.

In a ChatGPT TUI, `/account` opens the stored-account picker and
`/account NAME` selects an alias for this runtime only. Opening the picker makes
no network request. Selecting an account invalidates its old catalog and starts
background discovery for the selected account only; unrelated configured
providers are not contacted by account selection. Selection requires an idle turn
and no active subagents. `/login` and `/logout` operate on the currently selected
ChatGPT alias. To save another account entirely from the TUI, use
`/login chatgpt NAME`, choose browser or device authorization, and sign into the
other ChatGPT account. The chooser shows the target alias. Saving it does not
select it, replace `default`, or change the preferred account; duplicate upstream
identities remain rejected. Use the CLI to remove a different named account.

In `/account`, highlight an alias and press **p** to make it preferred, or use
`/account prefer NAME`. This saves the global preference for future runs without
changing the current runtime or bypassing history binding. The picker shows
`selected` and `preferred` separately. `auth use` is the equivalent CLI action.
An alias called `backup` is only a name, not an automatic failover role.

For example, with one login already saved as `default`:

```text
/login chatgpt backup       # authorize the other account; default is untouched
/account prefer default    # default is primary for future runs
/account                   # Enter selects; p makes the highlighted alias preferred
```

Selecting an account in the TUI is one action. If a saved conversation contains
private continuity tied to another account, Snow automatically starts a fresh
account-bound context with a **bounded plain-text working-state checkpoint** of
the readable conversation and tool evidence. Your task context carries over;
account-private continuation does not. No separate `/new` command is needed.
The earlier session retains its exact history in `/sessions`, and the TUI reports
the handoff. This is a working-context checkpoint, not an exact transcript replay:
thinking, images, private blocks, and old account binding are not copied.

Logging into a replacement identity under the **selected alias** in the TUI uses
the same handoff before the next prompt. Same-identity token rotation leaves the
conversation intact; saving another alias remains a detached login, without
switching. If credentials changed outside this TUI, reselect the desired alias
(including `/account default` when default was replaced) to request the handoff.
Unsaved `--no-session` history is not automatically discarded or written to disk;
that case still requires an explicit fresh-session choice.

Dedicated web-manager and public SDK/RPC account-management controls remain
deferred. `snow --mode web` rejects account override flags; shared runtime
continuity guards still apply to its workers.

## Quota observations and continuity

Local authentication state (`configured`, `expired`, `missing`, or `invalid`)
is not a quota balance. The TUI account picker separately reports quota failures
observed by this runtime for that credential identity, with a reset time only
when the server supplies a validated Unix-seconds value. Snow does not probe
accounts for balances, estimate remaining credit, or persist these observations
as subscription entitlements.

A structured HTTP 429 with `error.type=usage_limit_reached` is a terminal usage
limit, not temporary throttling. Valid `error.resets_at` is interpreted as Unix
epoch seconds; malformed optional reset metadata is ignored. Generic 429
throttling remains retryable. Recognition follows official Codex source pinned
at [447eac3b81183b32c1a09f5fba9617abfacbebe3](https://github.com/openai/codex/tree/447eac3b81183b32c1a09f5fba9617abfacbebe3),
not message-text guesses.

**Automatic account failover is not implemented.** Nonempty
`--account-fallback` and `chatgpt_accounts.fallback` are rejected. Applicable
upstream usage permission and cross-account encrypted-continuity behavior have
not been established; inaccessible terms/help pages are not evidence that
current terms prohibit or permit rotation. Review applicable provider rules
before using multiple accounts, and choose accounts manually.

Encrypted ChatGPT reasoning continuity cannot safely transfer to a different
known identity. Snow persists an account-identity binding before requests and
checks it for inference and compaction, including resumed, branched, and forked
sessions. Shared account selection rejects incompatible continuity rather than
deleting or rewriting history. The TUI handles that specific rejection by
atomically selecting the requested alias with a fresh account-bound context and
a local text-only handoff checkpoint, leaving the previous durable session saved.
The summary requires no request to the exhausted or previous account. Other
admission, storage, or credential errors
remain failures, not reasons to create a new session. CLI callers can start a
fresh session with `snow --provider chatgpt --account NAME`. Legacy opaque
ChatGPT history without a binding is allowed only for `default` with a known
identity; unknown identity fails closed.

Children snapshot the account selected at creation. Direct child forks copy a
known parent binding; inherited opaque continuity without verified provenance
(including legacy restored or recursive children) fails closed. Use
`fork_turns=none` for an independent child rather than transferring that state.

## Refresh, locking, and rotation

Runtime resolution goes through Snow's generic auth service, which delegates
token exchange to the ChatGPT OAuth driver. Tokens that expire within five
minutes, including tokens already expired while Snow was idle, are refreshed
before access-token validation under an account-scoped cross-process refresh
lock, while the global auth-store lock is held only for the fresh read and
compare-and-swap write. This keeps unrelated credential operations responsive
during network I/O and atomically persists rotated refresh tokens.

A pre-stream HTTP 401 forces one guarded refresh and one retry, while reusing a
newer credential already rotated by another process. Permanent
`invalid_grant`-class failures require login; transient and network failures
remain retryable and secret-free.

The auth store lock is `~/.snow/auth.json.lock`; account refresh serialization
uses a hashed `auth.json.<account-key-hash>.refresh.lock`. Both are mode `0600`.

## Status checks and JWT metadata

Check the stored ChatGPT login without refreshing it:

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
`GET /backend-api/codex/models?client_version=0.160.0` discovery. Raw records
are cached for 15 minutes under
`~/.snow/cache/chatgpt-models/<origin-and-identity-hash>.json` with versioned
origin/workspace/user-identity metadata, ETags, and mode `0600`. Distinct users
in the same workspace cannot share inventories or conditional ETags. Version-1
workspace-only caches are invalidated; token rotation for the same identity
preserves version-2 caches.

Opening `/model` reloads an expired ChatGPT catalog in the background. Press
**Ctrl+R** in the picker to force discovery immediately. Cache reads do not
extend the 15-minute lifetime; a failed refresh keeps only a compatible
same-account cache and becomes retryable after one minute.

The `client_version` value is Snow's pinned Codex catalog compatibility level,
not Snow's release number. OpenAI can omit newer models for older clients:
GPT-6 Astra requires a newer catalog contract than Snow's former `0.147.0`
request. Upgrading this contract invalidates older cached catalogs. New model
IDs returned by the account catalog appear automatically; a release requiring
a newer client contract can still require a Snow update. Account and workspace
availability remain controlled by OpenAI.

Only records with `visibility=list` or unset visibility are shown;
`supported_in_api=false` does not hide subscription-only models. Snow maps each
model's advertised inference effort (`minimal`, `low`, `medium`, `high`,
`xhigh`, or `max`) into normalized protocol thinking levels; `off` remains the
explicit no-reasoning selection. Codex may also advertise `ultra`, but upstream
defines it as a host preset that enables proactive multi-agent behavior rather
than a valid Responses `reasoning.effort`. Snow therefore does not expose it as
a ChatGPT thinking level or send it to inference.

Authenticated account catalogs are authoritative: a model missing from the
selected account is not merged back from the bundled snapshot. An explicitly
requested or active unavailable model is rejected, not replaced during account
selection or catalog refresh. Select another model explicitly. Requests use the
selected account's advertised capabilities even when both accounts offer the
same model ID, and reject a withdrawn reasoning effort rather than repair it.
If that model's advertised context window changed, reselect it with `/model`
before continuing: Snow refuses dispatch until the agent's fixed-context and
compaction budgets have been revalidated against the new window.
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
- [Install Snow and run your first prompt](getting-started.md) — installation
  and provider setup
