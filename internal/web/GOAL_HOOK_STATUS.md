# Goal hook status — parent handoff

Implemented authorized existing hooks in runtime_events.go, runtime_cancel.go,
runtime_queue.go, runtime_message_edit.go, runtime_workflow.go.

PASS: `go test ./internal/web -run TestGoal -count=1`.

Full `go test ./internal/web ./pkg/agentclient/rpc -count=1`: RPC PASS, web FAIL
because generic existing worker fixtures inherit new goal_run ready capability
but do not implement goal_inspect. Open's now-wired refreshGoal therefore rejects
unsupported/malformed inspection. Parent-owned runtime_worker_test.go fixture
must remove goal_run from advertised capabilities (or implement full inspection).
Same adjustment needed in queue/edit/workflow fixtures. Child can adjust only
owned queue/edit/workflow fixtures; parent runtime_worker_test.go remains yours.

## Still outside child authorized scope

- `runtime_message_revision_prepare.go`: under mu, add goalBlocksHistoryLocked()
  to initial !eligible/CancelRequested guard BEFORE clearing prior preparation;
  and final publication guard. This file was not among authorized glob edits.
- Parent `runtime.go` Prompt: clear retired runtimeGoalState on fresh normal
  prompt admission (not durable snapshot.Goal projection).
- Version restore owner: reset `r.goal=runtimeGoalState{}` and `snapshot.Goal=nil`
  on branch restore authority rotation; then read-only `refreshGoal()` after
  verified active binding. Add goalBlocksHistoryLocked guard before restore
  preparation AND commit, so nonterminal goals cannot be evaded by branch restore.
- Parent worker flags: add managed-goal policy and get_goal/create_goal/update_goal
  tools; generic runtime_worker_test.go fixed --tools expectation must follow.

No new-test expectations were weakened to obtain PASS. Optional budget is the
actual agreed core contract; it remains optional in web.

## Superseding status after parent follow-up

Generic fixture capability drift fixed (queue/edit/workflow/main/version): omitted
only unsupported goal_run. Main fixture expected args now EXACTLY matches parent
fixed goal+process profile and requires --managed-explicit-goals.

All web and client tests PASSED before the latest ACK/CAS additions:
`go test ./internal/web ./pkg/agentclient/rpc -count=1`.
`go test -race ./internal/web -run TestGoal -count=1` also PASSED.
Two full race suite invocations timed out at 120 seconds (not claimed passed).

Parent-requested publishLocked VersionsEnabled refresh added in actual owning
file `runtime_stream.go` (not runtime_events.go): r.refreshVersionsLocked().

Latest ACK/CAS implementation now waiting ONLY for parent RuntimeSnapshot
GoalRunACK field; see GOAL_ACK_CAS_CONTRACT.md. All RunGoal callers and HTTP tests
aligned to wrapper+required expected_revision. Fast preACK completion transport
test now validates immutable receipt then awaits authoritative event drain;
Call response and drain are independent goroutines, so immediate idle was a
flaky fixture assumption, not a guaranteed wire-ACK projection contract.

## FINAL verified child status (supersedes blockers above)

Parent ACK snapshot field present. Child defensive ACK clone added.
Authorized runtime_message_revision_prepare.go now guards BOTH before preparation
invalidation and final publication. Added regression proving paused goal rejects
prepare+commit without invalidating a previously prepared edit or dispatching RPC.
Added receipt clone ownership regression.

Latest commands ALL PASS:
- go test -race ./internal/web -run 'TestGoal|TestRuntimeTurnCancel' -count=1 -timeout=95s
- go test ./internal/web ./pkg/agentclient/rpc -count=1 -timeout=80s
- go vet ./internal/web ./pkg/agentclient/rpc

Parent still owns version restore guards/reset/refresh and ordinary Prompt reset;
no child edits to those parent-owned files. No install, commit, or port 7331 use.
