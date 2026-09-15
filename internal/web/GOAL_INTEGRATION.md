# Parent integration contract (temporary handoff)

Child-owned new Go files: `runtime_goal.go`, `runtime_goal_events.go`,
`goal_http.go`, `runtime_goal*_test.go`, `goal_http_test.go`.

Core DTO consumed from `pkg/protocol/rpc_goal_run.go`. Awaiting
`clientrpc.Event.GoalRunCompleted *protocol.RPCGoalRunCompleted` and
`protocol.AgentEvent.GoalRunID string`. Typed RPC rejection expected
`goal_run_rejected`; align if core names differently. `goal_run` capability.

## Required existing edits

- `RuntimeSnapshot.Goal *RuntimeGoal json:"goal"`; `liveRuntime.goal runtimeGoalState`.
- Snapshot clone: `s.Goal = s.Goal.clone()`.
- `drain`, while eventMu+mu held: first `buffered, valid := r.bufferGoalEventLocked(event)`;
  only if `!buffered && valid`, call existing message-edit buffer.
- First line of `consumeEvent`: `if r.consumeGoalEvent(event) { return }`.
  Helper filters run/root/epoch, handles whole-run completion; existing text,
  permissions, tools, plan and usage projectors remain unchanged.
- `retireTurnCancel`: AFTER clearing `cancelTaskToken` and checking exact
  instance/session/token, call `r.finishGoalRunLocked()` before current idle
  cancellation cleanup. Helper leaves busy and running latched until cancel task
  retired. Existing `publishLocked()` in idle path then publishes completion.
  Persist recovery after retirement when finished (cannot call while holding mu).
- `queueActiveLocked`: return false for `r.goal.pending || r.goal.active`.
- `mutateQueue`: reject pending/active goal before any queue mutation, including
  review removal, because this run never owns prompt queue authority.
- History prepare AND commit: `r.goalBlocksHistoryLocked()` is an admission guard
  under mu before any mutation (including preparation invalidation). Core must
  also reject nonterminal-goal editing even if manager hasn't inspected it yet.
- Switch and committed message revision reset `r.goal = runtimeGoalState{}` and
  `snapshot.Goal = nil`, then refresh new branch/session read-only. Normal Prompt
  can clear completed goal-run state, NOT durable goal projection.
- Open and Switch: call `r.refreshGoal()` AFTER existing abort, verified info,
  session binding and history load. This is a capability-gated goal_inspect read;
  DO NOT remove existing abort (it durably defers saved goals on reopen).
  After successful revision commit, goal projection should be nil/read-only
  refreshed against its new branch.
- Runtime HTTP dispatch after shared auth/CSRF/project lookup:
  `if goalAction(action) { s.runtimeGoalAction(ctx,w,r,project); return }`.
- Existing pageData feature flag if frontend needs it: typed `RuntimeGoalBackend`.
  The child doesn't own pageData, render templates, or browser app.

## Public backend/UI contract

`RuntimeGoal` JSON: session_id, branch_id, tip_id, goal_id, objective, status,
blocked_reason, deferred, token_budget (nullable), tokens_used,
budget_remaining (nullable), estimated_costs, goal_run_id, running.
An inspected absent goal is a nonnil projection with status `none`, empty goal_id,
and exact branch/tip. No read creates or resumes a goal.

Typed POST actions (shared CSRF + cookie required):

- `goal-inspect`: instance_id, session_id, branch_id (required field, empty permits
  current branch discovery). Returns RuntimeSnapshot, never starts execution.
- `goal-start`: instance_id, session_id, branch_id, expected_tip_id,
  expected_goal_id (required, empty means asserted absence), objective,
  optional token_budget (if supplied, positive). Calls core action `create`.
- `goal-resume`: same bindings/CAS, no objective/budget, nonempty expected_goal_id.

All duplicate authority or unexpected form fields rejected; authority in query
rejected. Create cannot replace nonterminal goal. Resume cannot revive terminal
goal. Existing current provider/model/permission side effects unchanged.

After whole-run completion, UI should perform a read-only goal-inspect before
presenting next explicit Resume/Create: current goal tip/deferred are durable
inspection properties, not recoverable from turn_done or completion envelope.
No automatic execution or retry permitted.

## Tests

New network-free subprocess fixture has independent
`SNOW_WEB_GOAL_TEST_CHILD=1` entrypoint, and removes existing CHILD env.
New tests cover preACK buffer/root filtering, two turn owner reset, completed
before ACK, cancellation task retirement and gaps, privacy/count/bytes bounds,
clone ownership, POST identity/CSRF/duplicates/no read replay, held queue and stale
binding pre-mutation guards, lost HTTP vs RPC ACK, fast completion.

Existing generic worker fixtures emit NewRPCReady and may inherit newly added
capabilities. If default capabilities now include goal_run, generic fixtures
must OMIT it (or implement goal_inspect); otherwise new refreshGoal sees a
claimed capability returning no typed response. This is fixture drift, not a
reason to remove activation abort or fail-open malformed inspections.

## Newly observed core contract mismatches (parent relay urgent)

- Optional-budget mismatch RESOLVED: core and web both allow omitted create
  budget and require any supplied budget to be positive.
- `App.InspectGoal` currently requires nonempty BranchID; no existing
  RuntimeSnapshot or RPCSessionInfo exposes active branch ID. Initial read-only
  discovery therefore needs `goal_inspect` to accept empty branch (still exact
  session bound) and return authoritative active BranchID, or parent must add a
  reliable read-only branch-discovery RPC before inspection. NO guessed `main`.
  All create/resume mutations retain strict nonempty reviewed branch/tip CAS.
- `handleGoalRun` currently returns generic errors; web fail-closes generic
  rejection by canceling worker. Core must map PREMUTATION rejection to fixed
  `goal_run_rejected`; uncertain durable writes remain untyped/unknown and close.

## Worker flags (critical existing integration)

Current worker `--tools read,glob,grep,write,edit,bash,ask_user` omits native goal
capabilities. Core `GoalRunReadyAdmitted` requires get_goal/create_goal/update_goal.
Parent must coordinate narrowly adding these tools and the new managed-explicit-
goals policy flag to this worker launch; don't alter provider/model/permission
arguments or drop no-plugins/no-mcp/no-skills/no-subagents restrictions.


## Verification status at child handoff

Formatted all child-owned Go files. `go test ./internal/web -run TestGoal -count=1`
currently cannot build because `clientrpc.Event.GoalRunCompleted` is not yet
added by parent/core integration. `AgentEvent.GoalRunID` is now present.
No tests claimed passed; no installation, commits, or port 7331 activity.


## Latest child verification (parent has added client envelope)

`go test ./internal/web -run TestGoal -count=1` now COMPILES, but event/ACK/cancel
fixtures fail because the parent has not wired the existing hooks listed above.
No need for child to edit existing files. The exact hook instructions above
remain current. Missing consumed-goal hook causes foreign runs to enter legacy
projection and completion envelopes to be ignored. Fix is parent integration,
not weakening fixtures. HTTP routing also still needs the goalAction dispatch.

## Additional authorized client fixture

Added `pkg/agentclient/rpc/goal_completion_test.go` (new file only, per parent).
`go test ./pkg/agentclient/rpc -run TestGoal -count=1` PASSED: typed terminal
separation, malformed request/run/goal/status binding failure, duplicate JSON
rejection, and correlation retained across two individual turns.

Core now allows omitted budgets; web is aligned: optional create budget, any
supplied value strictly positive; resume accepts existing unlimited goals.
