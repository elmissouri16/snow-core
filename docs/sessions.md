# Sessions and branches

Snow saves conversations so you can resume work, create alternate branches, or
fork an independent copy. This guide covers the workflows available from the
terminal and the public SDK options that select session behavior.

## On this page

- [Where sessions are stored](#where-sessions-are-stored)
- [Resume or delete a session](#resume-or-delete-a-session)
- [Name a session](#name-a-session)
- [Create and switch branches](#create-and-switch-branches)
- [Compact long conversations](#compact-long-conversations)
- [Fork an independent session](#fork-an-independent-session)
- [Fork into a Git worktree](#fork-into-a-git-worktree)
- [Reuse earlier work](#reuse-earlier-work)
- [Choose session behavior in the Go SDK](#choose-session-behavior-in-the-go-sdk)
- [Subagent histories](#subagent-histories)
- [Related documents](#related-documents)

## Where sessions are stored

By default, Snow stores one SQLite database per conversation below:

```text
~/.snow/sessions/<project>/<session>.db
```

Set `SNOW_SESSIONS_DIR` to move the session root. Snow keeps sessions separated
by normalized working directory, so the interactive picker shows conversations
for the current project rather than mixing unrelated directories.

Session databases contain conversation text, tool calls and results, titles,
branches, goals, and other state needed to resume the work. Image attachments
are stored in the database and can increase its size. Oversized tool results may
also use private spill files below `~/.snow/artifacts` or
`$SNOW_HOME/artifacts`.

Use `--no-session` when you do not want durable history:

```sh
snow --no-session -p "review this directory"
```

This keeps the conversation in memory only for that Snow process.

## Resume or delete a session

From a project directory, open the interactive resume picker:

```sh
snow resume
```

You can also open a known database directly:

```sh
snow resume /absolute/path/to/session.db
```

In print, JSON, or RPC mode, `snow resume` without a path selects the newest
indexed session because no interactive picker is available. The lower-level
`--session /absolute/path/to/session.db` flag also selects a specific database.

Inside the TUI, use `/sessions` or `/resume` to switch conversations. Switching
sessions stops Snow-managed background processes from the old session before
the new session becomes active. Process handles and operating-system PIDs are
not resumable, although their prior tool results remain in the transcript.

To permanently delete a session from the `/sessions` picker, press `d`, then
confirm with Enter. Deletion removes the database and Snow-owned companion data;
it does not use the system Trash and cannot be undone. Snow refuses to delete
the active session or a database currently open in another Snow process.

## Name a session

Snow creates a display title from the first accepted user prompt. In the
`/sessions` or `/resume` picker, press `r` to rename the selected session.
Names must contain 1–72 characters after surrounding whitespace is removed and
cannot contain control characters.

A title helps with discovery but does not rename the database or change the
session's stable identity.

## Create and switch branches

A branch is another line of conversation inside the same session. It shares
history up to the fork point, then records new messages independently.

Use `/tree` to inspect the branch tree. From that view you can:

- select a branch and press Enter to switch to it;
- press `f` to fork from the selected point;
- press `r` to rename a branch; or
- press `d` to delete a branch after confirmation.

Snow never rewrites the shared earlier history when you switch or fork. The
active branch determines which messages, goals, collaboration mode, and future
appends belong to the current conversation path.

Active subagent work can temporarily block a branch change. Wait for or stop the
child work before switching branches so results cannot be attached to the wrong
history.

## Compact long conversations

Use `/compact` when a conversation approaches the model's context limit. Snow
creates a durable working-state checkpoint for older complete turns while
keeping the exact session history available for resume and inspection.

Compaction preserves recent work and complete tool call/result pairs. It changes
the context sent to the provider, not the append-only conversation history.
Snow can also compact automatically when the configured model window requires
it.

## Fork an independent session

An independent fork copies the selected conversation path into a new session
database. The source and child then evolve independently.

Inside the TUI, use `/fork` and choose the independent-session option. From the
command line, run:

```sh
snow fork [session-path] \
  --source-branch BRANCH \
  --from-entry ENTRY_ID \
  --name experiment \
  --destination /absolute/path/to/experiment.db
```

All flags after the optional session path are optional. Snow validates the
selected point and refuses to fork an incomplete assistant response or an
unresolved tool call. A historical fork starts in Default collaboration mode;
a current-tip fork carries the source branch's current mode. Active goals and
subagent trees are not copied.

## Fork into a Git worktree

Use a worktree fork when you want both a separate session and a clean Git
working directory:

```sh
snow fork-worktree [session-path] \
  --worktree ../snow-experiment \
  --git-branch snow/experiment \
  --name experiment
```

The source repository must be non-bare and clean because uncommitted files do
not transfer to the new worktree. Snow creates a new branch and path; it does
not reuse an existing destination. Relative explicit destinations are rejected.
If setup fails, Snow rolls back the worktree and branch rather than silently
falling back to a less isolated fork.

The TUI `/fork` worktree option keeps the current TUI in the source and prints a
`snow resume` command for opening the child.

## Reuse earlier work

Snow exposes two read-only tools when prior-session retrieval is enabled:

- `session_search` finds relevant user and assistant text in durable sessions
  for the current project.
- `session_reference` captures a bounded immutable snapshot from one search
  result into the active conversation.

Search excludes credentials, permission state, provider-private continuity,
images, raw tool traffic, and private subagent databases. Referenced text is
explicitly treated as untrusted historical information; it cannot grant
permissions or override current instructions.

## Choose session behavior in the Go SDK

The public Go SDK uses the same session lifecycle as the terminal surfaces.
Configure `snowsdk.Options` with:

- `NoSession` for in-memory-only history;
- `SessionPath` to open a specific session database; or
- neither option to use the normal persistent session location.

Use the SDK's session, branch, and fork methods rather than importing packages
under `internal/`. See the [Go SDK guide](sdk.md) for constructors, lifecycle,
methods, events, permissions, and concurrency requirements.

## Subagent histories

When durable subagents are enabled, Snow stores each child transcript privately
beside the root session. Child histories do not appear as ordinary resumable
root sessions. Reopening a root session restores the child topology; completed
children load their detailed transcript only when needed.

Deleting a root session through Snow also removes its Snow-owned child history.
Manual database deletion can leave companion data behind, so prefer the session
picker for permanent removal.

## Related documents

- [Using Snow](using-snow.md) — session pickers, commands, keys, and workflows
- [Configuration](configuration.md) — session paths and retrieval settings
- [Thread Goals](goals.md) — branch-scoped persistent objectives
- [Subagents](subagents.md) — child-agent lifecycle and inspection
- [Go SDK](sdk.md) — public embedding API
- [Security model](security.md) — session data, filesystem, and process boundaries
