# Sessions and branches

Snow saves conversations so you can resume work, create alternate branches, or
fork an independent copy. This guide covers the session workflows available
from the terminal.

## On this page

- [Store or skip a session](#store-or-skip-a-session)
- [Resume or delete a session](#resume-or-delete-a-session)
- [Name a session](#name-a-session)
- [Create and switch branches](#create-and-switch-branches)
- [Compact a long conversation](#compact-a-long-conversation)
- [Fork an independent session](#fork-an-independent-session)
- [Fork into a Git worktree](#fork-into-a-git-worktree)
- [Reuse earlier work](#reuse-earlier-work)
- [Related documents](#related-documents)

## Store or skip a session

Snow normally stores each conversation in a SQLite database below
`~/.snow/sessions/`. Set `SNOW_SESSIONS_DIR` to use another session root.
Session pickers show conversations for the current project.

Use `--no-session` when you do not want durable history:

```sh
snow --no-session -p "review this directory"
```

The conversation then exists only for the lifetime of that Snow process.

## Resume or delete a session

From the project directory, open the session picker:

```sh
snow resume
```

Open a known database directly when needed:

```sh
snow resume /absolute/path/to/session.db
```

Inside the TUI, use `/sessions` or `/resume` to switch conversations. In a
headless mode, `snow resume` without a path selects the newest indexed session
because Snow cannot display a picker.

To permanently delete a session, select it in the picker, press `d`, and
confirm with Enter. Snow refuses to delete the active session or a database
open in another Snow process.

> **Caution:** Session deletion bypasses the system Trash and removes the
> database plus Snow-owned companion data. It cannot be undone.

## Name a session

Snow creates a display title from the first accepted prompt. In the
`/sessions` or `/resume` picker, press `r` to rename the selected session.
Titles must contain 1–72 characters after trimming and cannot contain control
characters.

Renaming a session does not change its database path or stable identity.

## Create and switch branches

A branch shares conversation history up to a selected point, then records new
work independently inside the same session.

Use `/tree` to inspect the current session. From the tree you can:

- press Enter to switch to a selected branch;
- press `f` to fork from the selected point;
- press `r` to rename a branch; or
- press `d` to delete a branch after confirmation.

Active subagent work can temporarily block a branch change. Wait for or stop
that child work before switching so results cannot attach to the wrong branch.

## Compact a long conversation

Use `/compact` when a conversation approaches the model's context limit. Snow
creates a working-state checkpoint for older complete turns while retaining
the exact session history. Automatic compaction can also run when the selected
model's context window requires it.

Compaction changes what Snow sends to the provider; it does not delete the
original conversation.

## Fork an independent session

An independent fork copies the selected conversation path into a new database.
The original and child sessions then evolve independently.

Use `/fork` in the TUI, or run:

```sh
snow fork [session-path] \
  --source-branch BRANCH \
  --from-entry ENTRY_ID \
  --name experiment \
  --destination /absolute/path/to/experiment.db
```

All flags after the optional session path are optional. Active goals and
subagent trees are not copied.

## Fork into a Git worktree

Use a worktree fork when you want a separate session and a clean Git working
directory:

```sh
snow fork-worktree [session-path] \
  --worktree ../snow-experiment \
  --git-branch snow/experiment \
  --name experiment
```

The source repository must be non-bare and clean because uncommitted files do
not transfer. Snow creates a new branch and path rather than reusing an
existing destination. If setup fails, Snow rolls back the worktree and branch.

The TUI `/fork` worktree option leaves the current TUI in the source and prints
a `snow resume` command for the child.

## Reuse earlier work

When session retrieval is enabled, Snow can search prior user and assistant
messages for the current project and attach a bounded, read-only reference to
the current conversation.

Treat retrieved text as untrusted historical information. It cannot grant
permissions or override current instructions.

## Related documents

- [Using Snow](using-snow.md) — session pickers, commands, and TUI controls
- [Configuration](configuration.md) — session and retrieval settings
- [Thread Goals](goals.md) — branch-scoped objectives
- [Subagents](subagents.md) — child-agent lifecycle and inspection
- [Go SDK](sdk.md) — embedding Snow with a chosen session path
- [Security model](security.md) — session data and trust boundaries
