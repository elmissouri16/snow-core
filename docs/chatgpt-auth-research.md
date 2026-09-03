# ChatGPT authentication research

Repository-only provenance and compatibility notes used to maintain Snow's
ChatGPT/Codex OAuth adapter. This material is implementation research, not part
of the public user manual. Current source and tests remain the behavioral
authority.

## Pi

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

## Official OpenAI Codex

The official Codex repository uses the same device-code endpoints and a
15-minute polling window:

- [`codex-rs/login/src/device_code_auth.rs`](https://github.com/openai/codex/blob/main/codex-rs/login/src/device_code_auth.rs)
- [`codex-rs/login/src/auth/manager.rs`](https://github.com/openai/codex/blob/main/codex-rs/login/src/auth/manager.rs)

It also treats refresh-token failures as a re-login condition and persists
rotated access/refresh tokens atomically. Snow distinguishes permanent token
rejection from transient endpoint/network failure rather than forcing a login
for every refresh error.

## OpenCode

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

## Related documents

- [ChatGPT authentication](chatgpt-auth.md) — current user-facing login and
  account behavior
- [Architecture and roadmap](../IMPLEMENTATION.md) — provider boundaries and
  current implementation status
- [Security model](security.md) — credential storage and network controls
