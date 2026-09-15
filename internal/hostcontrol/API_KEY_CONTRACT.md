# Write-only API-key integration

Constructor is unchanged. New service methods:

- `InspectAPIKey(ctx, protocol.HostAPIKeyInspectRequest) (protocol.HostAPIKeyStatusResponse, error)`
- `SetAPIKey(ctx, protocol.HostAPIKeySetRequest) (protocol.HostAPIKeyStatusResponse, error)`

DTOs are in `pkg/protocol/rpc_host_api_key.go`. Input fields for set are
`provider_id`, `expected_revision`, `secret`, `confirm_replace`. Response fields
are only provider ID, API-key capability, replacement-consent requirement,
opaque revision, redacted local status, checked_locally and future_runtime.
No key is returned or exported. No OAuth/login/refresh/delete operation exists.

Inspect permits ChatGPT to report `api_key_supported:false`; Set rejects it.
Other provider IDs must be supported built-ins or locally configured compatible
profiles. The global config file must exist and be locally readable. An absent
auth file has revision `missing` and can be created by an explicit set. Corrupt,
oversized, nonregular or symlinked auth files are unavailable, never overwritten.
Existing profile entries OR an effective environment credential require explicit
`confirm_replace:true`; anonymous Zen does not require replacement consent.

Errors are the existing fixed classes plus `ErrReplaceConfirmationRequired`.
Context cancellation/deadline errors remain recognizable. Auth-file revisions
are whole-file metadata revisions, not hashes of secret content. Every legacy
atomic auth writer changes the inode and invalidates outstanding revisions.
Unrelated profile mutations also require rereading and explicitly retrying.

The method itself is callable only by explicitly trusted same-user local
transports. Public transport owns the mandatory numeric-loopback TLS + CSRF gate
**before body parsing**; HTTP must not accept submitted keys. Request objects
are secret input: never log/echo/record their JSON. Printf-style formatting is
redacted, but JSON serialization intentionally remains possible for trusted RPC.
Responses always state `future_runtime`; restart existing workers to consume
changed credentials. Configured means present locally, never remotely verified.

Implementation and gofmt complete. The parent-authorized focused check and
complete normal tests for all three affected packages passed, including late
defaults/status tests and API-key tests:

```sh
GOMAXPROCS=2 go test -p 1 ./internal/auth ./internal/config ./internal/hostcontrol -run 'TestHost|TestManager' -count=1
GOMAXPROCS=2 go test -p 1 ./internal/auth ./internal/config ./internal/hostcontrol -count=1
```

Legacy filestore.go is unchanged. Added config.ReadManagerConfiguredProviderIDs
as a strict existing-config capability view rather than changing existing
read-default behavior. All auth mutations share the existing auth.json.lock.

`TestHostKeyExternalLegacySameContentInodeInvalidatesCAS` runs an actual
independent legacy FileStore.Put process, restores the previous mtime and
verifies unchanged bytes/size yet changed inode invalidates the old revision.
`TestHostKeyLegacyLockWaitCancellation` verifies the new writer waits behind
the existing FileStore lock and honors cancellation without touching auth.
Cross-process and concurrent new-writer CAS tests also pass. No production fix
was needed during these checks. Race checks await a separate parent test slot.
