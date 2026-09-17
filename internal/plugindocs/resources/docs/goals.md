# Persistent Thread Goals

A Thread Goal gives one session branch a continuing objective. Snow can keep
working through private serial turns until the goal completes, pauses, becomes
blocked, reaches a usage limit, or reaches its optional token budget.

## Create a goal

Use the TUI command with an optional token budget:

```text
/goal ship and verify the parser
/goal --budget 20000 ship and verify the parser
```

A goal belongs only to the active branch and requires a persisted session.
Durable sessions restore it after a restart; Thread Goals are unavailable with
`--no-session`. Replacing an unfinished goal requires confirmation.

Goals continue only in Default collaboration mode. Automatic goal turns cannot
ask for interactive user input; if the agent needs information, it should mark
the goal blocked and explain what is missing.

## Understand goal status

| Status | Meaning |
|---|---|
| `active` | Eligible for automatic continuation |
| `paused` | Stopped until you resume it |
| `blocked` | Waiting for missing information or another condition |
| `usage_limited` | Stopped by provider or runtime usage limits |
| `budget_limited` | Reached the goal token budget; terminal |
| `complete` | Objective finished; terminal |

Transient provider failures use bounded retries. Cancellation, a provider
quota, or a budget limit stops further continuation. Three consecutive empty
turns, or three identical responses with no successful tool work beyond
`get_goal`, pause the goal. This detects repeated output, not every form of
semantic non-progress. Resuming resets the repetition check.

## Control a goal

Use these TUI commands:

```text
/goal
/goal edit revised objective
/goal replace revised objective
/goal pause
/goal resume
/goal clear
```

- `/goal` shows the current objective, status, usage, and budget.
- `edit` revises the objective while preserving accumulated usage and the
  budget.
- `replace` discards the unfinished goal and creates a new goal with fresh
  accounting and no token budget.
- `pause` stops automatic work without deleting the goal.
- `resume` restarts an eligible paused, blocked, or usage-limited goal.
- `clear` removes the goal from the branch.

Pressing Ctrl+C or Esc during automatic goal work aborts the turn and defers
continuation. Use `/goal resume` when you are ready to continue.

## Use goals in the local web manager

The local manager exposes a narrower explicit workflow than the TUI commands.
Activate a conversation and enable **Goal** in the composer. This is a local
draft-mode toggle: it performs no inspection and starts no work. Type the objective
in the same message box, then submit using **Start goal run** (or Ctrl/⌘+Enter).
There is no separate objective editor, creation dialog, or consent checkbox.
Submitting explicitly authorizes one goal run with the current model and session
permissions. Goal objectives currently accept plain text, not attachments.

Before admitting work, Snow inspects the exact live worker, durable session,
branch, tip, goal identity, model, permissions, thinking and revision. The browser
sends `expected_revision` only to the manager as a compare-and-swap guard for the
reviewed snapshot; it is not part of the strict core `goal_run` RPC parameters. A
changed draft or scope cancels that submission instead of sending stale text or
retargeting it. On confirmed admission, only an unchanged objective draft is
cleared and Goal mode turns off. Concurrently typed text and unverified
submissions are kept;
no failed or uncertain write is automatically replayed. Toggling Goal off by
itself never clears text, cancels a run, or resumes a saved goal.

An existing goal's objective and run state appear above the composer. **Details**
(or **Thread goal** in the conversation menu) opens read-only status and explicit
Resume controls. For a new goal, Details also offers an optional positive token
budget; objective entry still stays in the composer. Resume retains its separate
exact-target review and confirmation. Activation, passive reads, branch restore
and reconnection keep saved goals deferred; none automatically resume work.
Start does not replace an unfinished goal, and the manager has no edit/replace/
clear-goal workflow. Plan Mode rejects Start and Resume. An idle worker with no
retained queue/review inputs is required. **Queue next is disabled in Goal draft
mode and during goal work.**

One native goal-run handle owns all serial automatic turns, retries, compaction
and gaps between turns. **Stop cancels the entire run**, not just its current
provider turn. A correlated run completion reports that execution ended; it does
not assert that the objective is `complete`. Inspect the separate semantic goal
status and deferred state before deciding whether to resume. Unknown outcomes
are never automatically replayed.

The fixed `managed-explicit-goals` worker profile suppresses goal tool schemas
and rejects their dispatch during ordinary prompts. Within explicit goal work,
get/update apply only to the owning goal; the model cannot create or replace a
goal. This is still the existing agent loop, not a browser scheduler or autonomous
multi-agent workflow. Plugins, MCP and subagents remain disabled. Skills are disabled by default;
the **Enable installed skills** startup checkbox admits the normal worker catalog
and remembers its setting per project, independently of goal controls and tool
permissions. Changes apply on the next explicit start, not to a live run. Managed
process tools are enabled but retain normal hard-policy and permission checks.

The optional budget follows the accounting rules below and is **not a strict
provider billing cap**. See [Using Snow](using-snow.md#start-or-resume-a-thread-goal)
for the manager's controls and [RPC](https://github.com/elmissouri16/snow-core/blob/main/docs/rpc.md)
for the typed integration contract.

## Use goals with Plan Mode

Entering Plan Mode stops and waits for automatic goal work. Planning turns do
not consume the goal budget, and Snow does not launch new automatic goal turns
until the branch returns to Default mode.

A goal remains attached to its branch during compaction. Snow includes a
compact status reminder in provider context so the objective survives long
conversations without repeatedly copying its full text.

## Review usage and privacy

Goal usage includes provider input, cached input, output, reasoning, and tool
requests from automatic work. When provider pricing is available, Snow may
also show estimated cost. Provider usage remains authoritative.

When the model calls `create_goal` during a running turn, accounting begins
at creation. Subsequent provider requests and elapsed work count toward the
new goal; earlier requests are not charged to it.

When usage reaches the goal budget, Snow skips pending tool calls and records
why they were skipped. It allows at most one report-only provider request with
no tools and no retries. The in-flight response and final report can exceed
the token budget; the budget stops further substantive work rather than acting
as a strict provider billing cap. Queued input that the budget prevents from
running remains available for recovery.

Automatic compaction counts toward the owning goal, including usage reported
by failed summary attempts. Repeated usage events within one attempt are
cumulative snapshots. Crossing the budget during compaction enters the budget
completion path. Manual compaction counts toward session usage but does not
charge the goal. Auxiliary usage stays in branch metadata, separate
from conversation messages and context-occupancy estimates.

An SDK caller canceling its prompt or reaching its deadline pauses the attached
active goal. Resume it explicitly when the host is ready to continue.

Goal text is private session state and is not published in summary events.
However, Snow sends the active objective to the selected model provider while
working on it. Do not put credentials or unnecessary sensitive data in a goal.

Subagents do not receive the root objective automatically. Give each child a
focused task and assume every child incurs separate provider usage.

## Related documents

- [Plan Mode](plan-mode.md)
- [Sessions and branches](sessions.md)
- [Using Snow](using-snow.md)
- [Go SDK](sdk.md)
- [Security model](security.md)
