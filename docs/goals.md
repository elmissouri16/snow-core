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
quota, a budget limit, or repeated non-progress stops further continuation
instead of looping indefinitely.

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
