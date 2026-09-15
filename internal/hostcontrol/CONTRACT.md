# Runtime-free host control integration

`New(Options{ConfigPath, AuthPath}) (*Service, error)` touches no files. Empty paths
select config.DefaultPaths. Methods:

- `GetDefaults(context.Context, protocol.HostDefaultsRequest) (protocol.HostDefaultsResponse, error)`
- `UpdateDefaults(context.Context, protocol.HostDefaultsUpdateRequest) (protocol.HostDefaultsResponse, error)`
- `ProviderStatus(context.Context) (protocol.HostProviderStatusResponse, error)`

Transport DTOs: `pkg/protocol/rpc_host_control.go`. Scope `global` prohibits CWD;
`project` requires existing directory CWD and canonicalizes symlinks. Both scopes
read/write ONLY operator global config.json; project files are never inspected.
Global/project response and patch objects are mutually exclusive. Provider/model
is one atomic pair; set requires a nonempty model (1..256 bytes). An empty
effective model on GET means no explicit runtime model selection; this service
does not resolve provider model defaults or promise catalog availability.
Thinking enum: off|minimal|low|medium|high|xhigh|max|ultra. Summary:
off|auto|concise|detailed. Verbosity: low|medium|high.
Set requires value; reset prohibits value. Omitted operations leave values alone.
CAS `revision` required on updates; stale means re-read and explicitly retry.
`applies_to` is always `future_runtime`: **not** a new conversation created in an
existing worker, no live worker reload promise.

Errors are fixed sentinels (`ErrInvalidRequest`, `ErrUnavailable`,
`ErrRevisionConflict`) or context cancellation/deadline errors; never forward
raw config/auth parse errors. Status contains only bounded safe provider IDs,
state configured|expired|unavailable, fixed reason codes and checked_locally.
No auth refresh/login, provider construction or network capability. The separately
approved write-only API-key methods and mandatory transport restrictions are
documented in [API_KEY_CONTRACT.md](API_KEY_CONTRACT.md).

Verification checkpoint: parent-authorized focused `TestHost|TestManager` and
complete normal tests passed for auth, config and hostcontrol, with
`GOMAXPROCS=2 go test -p 1 ... -count=1`. This includes all late defaults/status
and API-key additions. No active build/test subprocess remains; race checks
await a separate parent test slot.

Auth-store corruption/oversize/nonregular/symlink conditions produce
`unavailable` / `auth_store_unavailable`, never a missing/anonymous claim. When
the store is readable, valid stored credentials precede env fallback; invalid
(non-Valid) entries permit fallback exactly as auth.Service does. The legacy
built-ins share OPENCODE_API_KEY only for Go/Zen and OPENAI_API_KEY only for
openai-compatible; named profiles do not inherit those keys. ChatGPT is locally
inspected OAuth only (including seconds/milliseconds/JWT expiry); no refresh or
account-claim extraction. Missing Zen is configured / anonymous_access.

Config and auth roots reject final-root symlinks, files reject symlinks and
nonregular types, and opened root/file identities are pinned and verified.
OS ancestor aliases such as macOS /var are supported outside the selected root.
Cross-process writes share config.json.lock with existing config mutators, use
nonblocking advisory-lock polling with context cancellation and never remove or
steal old locks. Bounded 4 MiB raw-object edits preserve unknown sections and
fields; project_selections is capped at 4096. Atomic replacement uses a pinned
same-parent 0600 temp file, file sync, inode-checked rename and parent sync.
