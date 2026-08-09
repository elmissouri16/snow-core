# Persistent Thread Goals

Snow can attach one persisted objective to each saved session branch. Goals are
not available in ephemeral `--no-session` sessions; mutating goal APIs return a
persisted-session error there.

Statuses are `active`, `paused`, `blocked`, `usage_limited`, `budget_limited`,
and `complete`. `complete` and `budget_limited` are terminal. Pause is valid only
from active; resume is valid only from paused, blocked, or usage-limited. Editing
rotates the goal ID, reactivates the objective, preserves its accumulated
usage/budget, and makes in-flight updates for the old objective stale.

Active goals continue through private serial turns in Default mode. Plan mode
cancels and joins automatic goal work, never launches goal turns, and never
charges planning work. Returning to Default explicitly resumes an active,
non-deferred goal. User prompts may interrupt automatic work and, if the goal is
still active afterward, continuation resumes exactly once.

## Accounting and stopping

Provider usage events inside one request are cumulative snapshots, so Snow
charges only that request's final snapshot. Final snapshots are summed across
tool-result requests in the logical turn. Accounting happens once, before any
terminal error classification. Exact budget crossing atomically changes the
goal to `budget_limited`; a successful crossing gets one final budget-summary
turn. If the crossing request itself fails, Snow preserves budget precedence
but does not issue a provider request already known to be impossible.

Elapsed usage is in-process monotonic work time, including sub-second remainder
carried across turns. Process downtime is not charged. Goal IDs are optimistic
stale-write guards. Objective replacement and status transitions are
compare-and-swap operations, while SQLite accounting uses one atomic update
even when the same database is opened by multiple handles.

Provider/tool/context/accounting failures immediately stop an active goal as
`blocked`, or `usage_limited` for provider quota exhaustion. Accounting errors
are returned to the caller and never permit another autonomous turn. This host error classification is
distinct from a model declaring an external blocker. Three automatic turns
with no text or tool progress conservatively pause the goal and emit an error;
they do not falsely claim that the blocked audit succeeded. Automatic requests
also yield briefly between turns, preventing an immediate-response provider
from hot-spinning even while useful text/tool work continues.

`Abort` cancels and joins any admitted turn. If goal work was active—even in the
small window before its first provider call—it persists a continuation
deferral. A deferred active goal stays idle across reopen until explicit
continue/resume or a new user prompt clears that deferral.

## TUI

```text
/goal
/goal ship and verify the parser
/goal --budget 20000 ship and verify the parser
/goal edit revised objective
/goal pause
/goal resume
/goal clear
```

Creating a replacement while an unfinished goal exists requires explicit
confirmation. A failed edit/replacement restarts the unchanged prior goal when
it had been eligible for automatic work. Empty-ID clear is a compare-and-swap
no-op only when no goal exists; it can never wildcard-delete a concurrently
created goal. Pause, edit, and clear remain usable while automatic work runs.
Resume/session/branch/fork/compaction/shutdown paths cancel and join owned work
before changing state. Compaction resumes only a goal that was already running;
it cannot bypass surface readiness, and aborting compaction persists deferral. A fork gets an independent managed objective file, so
clearing either branch cannot remove the other's objective. Restored paused,
blocked, and usage-limited goals display resume guidance.

## SDK and RPC

The SDK exposes `Goal`, `CreateGoal`/`SetGoal`, `EditGoal`, `PauseGoal`,
`ResumeGoal`, `ClearGoal`, `ContinueGoal`, `ReadyGoals`, and a context-aware
`Abort`. SDK construction deliberately does **not** start persisted automatic
work before the embedding host can subscribe. The host should subscribe, emit
or inspect `StateEvent`, then call `ReadyGoals`; that publishes the initial goal
snapshot and starts only an active, non-deferred Default-mode goal.

RPC commands are `goal_get`, `goal_set`/`goal_create`, `goal_edit`,
`goal_pause`, `goal_resume`, `goal_clear`, and `goal_continue`; successful
responses use `data`. Print/JSON, RPC, and TUI install event observers before
they signal goal readiness.

Agent events are delivered by one ordered dispatcher rather than on provider or
tool worker goroutines. Subscriber payloads are deep copies. A subscriber can
therefore invoke prompt/model/mode/goal/close controls without holding goal
locks or waiting on the worker that is invoking it. A dispatcher-reentrant
prompt that requests manual input fails that tool call fast unless an in-process
SDK handler can answer it; this prevents waiting for an event that the same
dispatcher would have to deliver.

## Model contract and privacy

Direct `get_goal`, `create_goal`, and `update_goal` tools are registered
normally. Models may set only `complete` or `blocked`. Completion requires a
direct evidence audit. A model-side blocked update has a mechanical minimum of
three goal turns and the steering prompt requires the **same** true external
blocker on all three; blocker identity remains a prompt-audited semantic claim,
matching Codex's contract. Resume and objective edit reset that audit.
Pause/resume/clear and limit states remain host-controlled.

The current objective is synthesized on every goal-bearing provider request as
trusted host-generated internal context and serialized as trailing
**user-role** input. It is never a system/developer/assistant message and is not
persisted as visible conversation text. Goal tool previews are private;
permission, routing, and session-summary events omit objective text.

Objectives over 8 KiB **by byte length** are atomically materialized under
`SNOW_HOME/goals/<session>/<goal-id>/goal-objective.md`. Directories are real
(non-symlink) `0700` directories and the regular file is `0600`; objectives are
bounded to 128 KiB. Reads, writes, renames, and cleanup use Go's descriptor-
anchored `os.Root` API and remove only the expected file plus an empty owner
directory, closing symlink-swap/recursive-delete races. The persisted public goal contains a short reference, but
the controller resolves only its own goal-ID-owned, root-confined file and
injects the actual escaped text privately. The model does not need broad
`SNOW_HOME` read or shell access. Replacement/edit/clear remove only validated
owned files; forged reference-shaped objective text cannot trigger deletion.
Snow does not invent image attachments.
