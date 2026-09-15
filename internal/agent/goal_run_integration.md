# Shared guard completion update

Items 1/2/6 below are NOW IMPLEMENTED by child per parent authorization. Changed configuration.go, session_context.go, compaction.go (RunMailbox admission), queue_control.go, and App sessions_goals.go legacy methods. Added goal_run_controls_test.go in agent/App. Request setters participate in admission; owner blocks modes/provider/model/request settings/session+branch+manual compaction+queue+mailbox-run without implicit Stop. Legacy mode unchanged. SessionIdentityAdmitted marks explicit owner busy across gaps. Direct queue events carry GoalRunID. Managed legacy Create/Edit/Pause/Resume/Clear/Continue all reject before writes/Stop; Stop/Abort still cancel and join.

Focused goal-run tests PASS, focused race PASS, full agent/App/RPC suite PASS on latest single run, go vet PASS, diff check PASS. New guards tested before Release and in simulated nondeferred native gap; goal identity, branch/tip, owner, and deferred state stay unchanged on rejection. Legacy suite passed.

**Parent/version owner blocker discovered:** TestBranchRestoreTargetModeIsBoundAndAppliedWithoutExecution is flaky at branch_restore_test.go:356. Reproduced ~15 failures in `go test ./internal/app -run '^TestBranchRestoreTargetModeIsBoundAndAppliedWithoutExecution$' -count=30`. Fixture SetMode(Default) starts empty legacy ContinueGoal worker; restore immediately observes autoRunning and correctly rejects. Version-owned fixture should join a.Agent.WaitGoal after mode switch, not weaken admission. Recorded new bugs.md entry (latest BUG ID). Child did NOT edit version files. Single suite pass does not clear this repeatable flake.

---

# Explicit goal-run core handoff to parent

## Implemented and verified

- New `GoalRunHandle` reserves `autoRunning` synchronously; stable ID/Done/error result across every native turn, retry, compaction boundary, and budget report. Release is the provider-start gate; Cancel reaches gaps/compaction and persists deferral. Abort/Close join the owner.
- Extracted `runAutomaticGoal` from ContinueGoal; legacy and explicit both use it and the original internalTurn/run loop. No invented user prompt, parallel turn loop, or WaitGoal wrapper.
- Managed ContinueGoal is a no-op. Normal prompts reject active explicit owner and do not bind/account an existing goal.
- Schema exposure guard in lifecycle_run.go; central dispatch guard in tool_admission.go (model AND plugin calls, before preflight/permission). create_goal denied always under managed policy; get/update only on the exact owned goal.
- Parent provided Options fields, CLI wiring and AgentEvent.GoalRunID field. **Child added event GoalRunID normalization in configuration.go's existing publish RLock block per parent request; do not duplicate.** Native/compaction root events inherit the same run ID, native TurnIDs remain distinct.
- RPC handles occupy existing cancel/promptDone/promptWG lifecycle. ACK failure cancels+joins without provider work, completion bounded-drains native events after runtime cleanup, then emits exactly one goal_run_completed (not prompt_completed). Real Serve EOF and Abort covered.
- App Start/Inspect hold stateMu then admission. Admission rejects Plan, queued/recovered/review/delivering work, busy owner, active subagents, wrong session/branch/tip/goal identity, unfinished replacement, terminal/exhausted resume. Persistence-attempt failures are unknown and safe deferred with no provider start.
- Parent-approved budget correction: token_budget OPTIONAL; nil unlimited, explicit nonpositive rejected. Resume preserves original budget and requires remaining budget when bounded.
- Inspect allows omitted/empty branch_id for current-branch discovery with exact session_id; mutations still require concrete exact branch/tip/goal assertions.

## Remaining PARENT integration (before worker tool/profile expansion)

1. Shared controls must reject explicit owner across gaps: settings/model/mode/session/branch/manual compaction may currently stopAutomatic/preempt or check only running. Treat goalRun != nil as busy. Only native autoCompactGoalBoundary remains permitted. Shared configuration enqueueRootInput also rejects goalRun != nil (native turns disable queueAccepting, but explicit owner check closes every bypass).
2. Legacy App CreateGoal/EditGoal/PauseGoal/ResumeGoal/ClearGoal/ContinueGoal: call Agent.ManagedGoalMutationAllowedAdmitted() under existing admission before stop/write. Managed policy rejects them; legacy unchanged. Stop/Abort permitted. No generic RPC goal mutation tunnel; minimum explicit controls are create/resume/inspect/Abort.
3. RPC error mapping: errors.Is(err, app.ErrGoalRunOutcomeUnknown) -> protocol.RPCGoalRunUnknownErrorCode (`goal_run_unknown`); errors.Is(err, app.ErrGoalRunRejected) -> protocol.RPCGoalRunRejectedErrorCode (`goal_run_rejected`). UNKNOWN first. Both exported sentinels/constants exist. All pre-write failures wrap rejected; attempted-write and ACK-loss failures wrap unknown, never rejected.
4. In Serve on original input frame: if isGoalControlCommand(req.Type), call validateGoalControlFrame([]byte(line)); on error write response with rpcErrorCode(err) and continue. Helpers live in child-owned goal_run_validation.go. Typed Request alone cannot reject explicitly empty extra envelope fields. Duplicate/unknown params and required assertions are already validated in child handlers.
5. RPC routes goal_run/goal_inspect and root schema variants are present from parent. **New TestGoalRunStrictSchemaContract currently exposes generic response oneOf overlap:** typed goal_run/goal_inspect ACKs also match generic success response because response.schema.json's command.not.enum (~1539) lacks the new names. Exclude goal_run and goal_inspect there (check other new typed process/branch names too). Request schema checks already pass. Child does not edit schema roots.
6. Queue-control direct a.bus.Publish bypass is separate from normalized publish. Managed goals cannot own ordinary queued work; if all root queue snapshots must carry active run ID, set event.GoalRunID under existing a.mu in publishQueueControlEventLocked. Child did not touch shared queue_control.go.
7. Audit public bug tracking/docs in parent scope. Legacy WaitGoal's mutable autoDone and swallowed worker errors remain legacy contract limitations; explicit handle fixes new surface without redefining legacy WaitGoal.

## Exact public/transport contract

App.StartGoalRun(ctx, protocol.RPCGoalRunParams) (*agent.GoalRunHandle,error)
App.InspectGoal(ctx,protocol.RPCGoalInspectParams) (protocol.RPCGoalInspection,error)
Handle: ID(), GoalID(), Release(), Cancel(), Done(), Wait(ctx), GoalStatus() (status available after Done).

- goal_run params: action create|resume; session_id; branch_id; expected_tip_id (present empty asserts empty tip); expected_goal_id (present empty asserts absent goal). Create requires objective, optional positive token_budget. Resume requires exact nonempty expected_goal_id and accepts no objective/budget fields.
- ACK data: {goal_run_id,goal_id,session_id,branch_id}.
- Terminal: {type:"goal_run_completed",request_id,goal_run_id,goal_id,status:"finished"|"canceled"|"failed",goal_status,error?}. Finished means execution ended, NOT semantic goal completion.
- goal_inspect params: session_id, branch_id?; result {session_id,branch_id,tip_id,goal:null|ThreadGoal,deferred,budget_remaining?,goal_run_id?}.
- New goal-run.schema.json defs: goal_runParams, goal_inspectParams, accepted, inspection, completed.

## Verification

Ran modern Go CLI list fully before edits (and uuid explanation), gofmt, focused checks.

Passed on REAL checkout after event assignment:
- go test ./internal/agent ./internal/app ./internal/rpc -run '^TestGoalRun' -count=1 (before adding schema-root regression test described above)
- go test -race ./internal/agent ./internal/app ./internal/rpc -run '^TestGoalRun' -count=1 (before schema-root regression test)
- Full internal/agent and internal/app suites.
- go vet ./internal/agent ./internal/app ./internal/rpc
- scoped git diff --check.

Full RPC baseline remains blocked by unrelated parent in-flight TestProtocolCommandInventoryHasDispatcherCase: process_control_list/logs/stop not recognized as direct dispatcher cases. Newly-added focused schema regression is blocked by parent generic-response exclusion above. No installs, commits, or port 7331 use.
