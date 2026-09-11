# Plan Mode

Plan Mode lets Snow investigate and prepare an implementation specification
without changing the project. It is a persisted collaboration mode, separate
from the implementation checklist used after work begins.

## Select Plan Mode

In the TUI, use:

```text
Shift+Tab
/plan
/plan design a branch-aware retry system
/default
```

`Shift+Tab` toggles Default and Plan while Snow is idle. During an active turn,
Snow queues the change until the turn finishes; press the shortcut again to
cancel the pending change.

Start a one-shot planning turn from the command line with:

```sh
snow --collaboration-mode plan -p "design the change"
```

Mode is stored per session branch and restored when that branch resumes.

## Understand the boundary

In Plan Mode, Snow should:

1. inspect the repository without changing it;
2. ask only the questions needed to settle intent; and
3. return a decision-complete implementation plan.

Snow blocks file writes, arbitrary Bash, process lifecycle changes, mutating or
unclassified extensions, and mutation-capable child work. Permission approval
does not override this boundary. Read and search tools remain available.

> **Warning:** Plan Mode is an application policy, not an operating-system
> sandbox. Snow and allowed tools still run with the user's privileges.

A Plan Mode root can delegate only to children whose resolved tool profile is
read-only and non-recursive. Snow also rejects entry into Plan Mode while
mutation-capable child work is active.

## Review a proposed plan

A completed plan appears as a distinct plan block in the TUI and event stream.
Interrupted plans remain visible but do not open the implementation handoff.

Check that a plan identifies:

- the files and public interfaces to change;
- required behavior and important edge cases;
- tests and validation commands; and
- unresolved assumptions that need your decision.

## Start implementation

After a complete plan, the TUI offers three choices:

1. switch to Default and implement in the current session;
2. start a fresh session with the complete plan; or
3. remain in Plan Mode.

Switching from Plan to Default clears session-active Agent Skills so a
planning-only instruction does not accidentally constrain implementation.
Historical activation records remain in the session. `/skills clear` is still
available for manual recovery.

## Configure reasoning effort

Set the default mode and optional Plan Mode reasoning effort in
`~/.snow/config.json`:

```json
{
  "collaboration_mode": "default",
  "plan_mode_reasoning_effort": "medium"
}
```

The effort may be `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`,
or `ultra`. The selected model must support the configured value.

## Related documents

- [Thread Goals](goals.md)
- [Sessions and branches](sessions.md)
- [Using Snow](using-snow.md)
- [Configuration](configuration.md)
- [Go SDK](sdk.md)
