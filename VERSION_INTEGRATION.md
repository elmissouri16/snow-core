# Version integration handoff — SOURCE FINAL

Shared schema ownership RELEASED to manager_protocol_integration:
`request.schema.json`, `response.schema.json`, `output.schema.json`,
`agent-event.schema.json`, and `pkg/protocol/schema_test.go`.
Version request/response references and representative requests are complete.
No further shared-schema writes from message_edit_core.

## Final contract

Contract: `pkg/protocol/rpc_branch_versions.go`.
Capability `branch_versions`; branches_page, branch_messages_page,
branch_restore_prepare, branch_restore_commit as documented in those DTOs.

`RPCBranchRestoreCommitted.Mode` is typed `protocol.CollaborationMode`, JSON
`mode`, required by the version schema. Restore applies the existing target
branch's saved mode to the Agent before ACK. Target mode is authorization-bound
and checked again atomically before selection, even when the tip is unchanged.
Provider/model/permission remain current. ACK thinking matches effective target
mode reasoning. No provider request, skill activation, tool or goal run occurs.

`RPCBranchMessagesPage` includes optional `history_tools` and
`history_tools_truncated`. Projection inspects the complete bounded exact-tip
history before selecting page owners, so tool outcomes remain correct when the
result lies beyond the page boundary. Only public tool previews are exposed;
frame trimming removes associated owner metadata. Restore ACK uses the same
public projection with a <=64-message suffix and a <=2-MiB conservative frame.

Session version APIs bound history to 100,000 entries/32 MiB; page limits are
100 branches / 64 messages. SQLite preflight now returns an ancestor-presence
sentinel instead of potentially unbounded raw parent identity. Version reads
and selection avoid the legacy unbounded branch/history selectors.

Prepare/commit reject active execution/subagents, nonterminal source/target
goals, and pending/review/recovered queue state. Separate, action-bound restore
tokens expire after two minutes, are bounded to 64, and are single-use. Store
selection binds both branch/tip pairs under the atomic writer reservation.
Durable probes distinguish definitive no-change rejection from uncertainty;
confirmed target caches and plugin projections reconcile without replay.

## Verification completed

PASS:
- go test ./internal/app ./internal/session ./internal/rpc -run 'BranchRestore|BranchVersion' -count=1
- go test -race ./internal/app ./internal/session ./internal/rpc -run 'BranchRestore|BranchVersion' -count=1
  App 4.670s; session 5.008s; RPC 12.456s.
- Scoped git diff --check across owned packages.
- Owned package Go-file line counts: all <=1,000 lines.

RPC tests validate actual version request/output schemas; cross-page public tool
pairing/privacy; read-only preview; exact target-mode/settings restore ACK;
strict frames; token replay; bounded output and oversized identity rejection.
App tests include Memory/SQLite saved-mode restoration and mode-stale rejection,
queue/active guards, target goals, preflight and ACK failures, and ambiguous
store outcomes without execution. Session tests cover exact selection, bounded
reads, cancellation and oversized inactive history/metadata/preflight identity.

Earlier broad owned-package gate failed concurrent goal/process correlation,
policy, dispatcher/schema integration tests. It was not rerun per parent's
bounded-finishing instruction. Parent owns actual-worker/browser, broad gates,
canonical documentation/bug tracking and install. No install/commit performed.
