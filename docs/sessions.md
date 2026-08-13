# SQLite session storage

Snow stores persisted conversations in one SQLite database per session:

```text
~/.snow/sessions/<encoded-working-directory>/<timestamp>_<suffix>.db
```

Set `SNOW_SESSIONS_DIR` to move the root. New directory names use
`cwd-v2-<full-sha256>` over the normalized absolute working directory, avoiding
the collisions and path-length growth of the legacy flattened format.
`FileIndex.List` also searches the legacy directory and filters every database
by its stored CWD, so old sessions remain discoverable without cross-project
mixing. `--no-session`/`NoSession` keeps the session in memory. Interactive
`snow resume` opens a current-working-directory session picker; `snow resume
/path/to/session.db` requires and opens that explicit database. In headless
print/JSON/RPC use, no-argument resume selects the newest indexed session because
a picker is unavailable. The lower-level `--session /path/to/session.db` flag
and SDK `SessionPath` option also select a session path. The previous JSONL
format is intentionally not migrated.

## Driver

The project uses [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite),
a pure-Go, CGo-free SQLite driver that implements Go's `database/sql` API. No
SQLite shared library or C toolchain is required.

The store opens databases with:

- WAL journaling for concurrent read/write behavior;
- `synchronous=NORMAL` to avoid an `fsync` for every individual entry while
  retaining SQLite transaction recovery;
- foreign keys enabled;
- a 5-second busy timeout;
- one database connection, avoiding connection-local pragma surprises.

## Schema

Each database contains:

- `session_meta`: one metadata row with schema version, session identity, CWD,
  display name, and the compatibility active branch tip;
- `session_branches`: durable branch references with stable ID, unique display
  name, parent branch/fork point, tip entry, timestamps, and active state;
- `entries`: append-ordered parent-linked entries with a unique ID, type,
  optional JSON-encoded protocol message, and optional compaction boundary;
- `thread_goals` and `thread_goal_costs`: branch-scoped objective, token/time
  accounting, and optional per-currency estimated pricing totals;
- indexes on `entries.parent_id`, `entries.entry_type`, and active branches.

User image attachments are stored as `image` content blocks in the message JSON;
Go's JSON encoding represents their bytes as base64. Clipboard images therefore
increase the session database size and remain available when a branch is resumed.

Version-1 databases are upgraded on open by creating a `main` branch pointing
at the existing `branch_tip`. Forking creates another reference in the same
SQLite tree; message rows are not copied. Selecting a branch changes the active
tip used by subsequent appends, `Messages`, `Usage`, and prompts. `Branches`
returns branch topology, previews, and message counts for the TUI/SDK tree picker.
Schema version 7 adds names and parent/fork metadata; legacy non-main branches
retain their IDs as names and attach to `main` when ancestry is unavailable.
Schema version 8 adds atomic per-currency goal cost totals. Migration backfills
priced historical goal usage only when its exact token sum matches the persisted
goal counter.

An append inserts the entry and updates the active branch tip in one transaction.
Entry ID/parent columns are authoritative and normalize embedded message
identity on write and read. Branch-tip moves also use transactional optimistic
compare-and-swap, so stale handles return a conflict instead of overwriting a
newer tip.
Root-only databases are removed on close and omitted from `FileIndex.List`.
Messages, goals, additional branches, non-default thread state, remembered
metadata, or subagent topology make a session durable and listable even when its
active branch currently has zero messages.
`Messages()` uses a recursive CTE to walk only the active branch, so opening a
large session does not deserialize every historical branch into memory.
`ContextMessages()` applies the latest compaction marker logically: providers
receive one summary plus the retained tail, while `Messages()` continues to
return the complete historical branch. Before semantic compaction, oversized
plain-text tool results in the older summarization prefix are projected as a
bounded head, omission marker, and tail. This model-free pruning reduces
summarizer input without changing exact durable messages or the ordinary
`Messages()`/`ContextMessages()` APIs. `Metadata`/`SetMetadata` store append-only
per-session state such as permission mode and remembered tool rules.

When an existing session is opened, the agent checks the final provider tool
batch for calls without results. A hard crash cannot prove whether an external
operation completed, so Snow appends error results that mark read-risk calls as
retryable and write/exec/network/delegation calls as having an unknown outcome.
It never automatically retries an interrupted side effect. Recovery is
idempotent and uses one atomic batch when the store supports batch appends.
`FileIndex.List` counts branch messages with SQL rather than loading the full
transcript.

## Prior-session search and references

Snow exposes two deferred, read-only model tools for reusing prior work:

- `session_search` builds a disposable SQLite FTS5 index from the current
  project’s durable root sessions and returns one bounded representative hit per
  matching branch. The durable session databases remain authoritative; the
  derived index is rebuilt from their current tips and is never a memory store.
- `session_reference` captures a selected search result as a bounded immutable
  snapshot. The snapshot is persisted as the ordinary tool-result message on the
  current branch, so later changes to the source session cannot alter replay.

Authorization is host-enforced using the same exact normalized CWD filtering as
`FileIndex.List`. The current session and private `.db.agents` child databases
are excluded. Search and reference projection includes direct user text,
finalized assistant text/plan blocks, session names, and compaction summaries.
It excludes tool messages and tool calls, thinking, images, provider-private
continuity data, agent mail, metadata, permission state, goals, queues, trust,
credentials, and child ownership.

Search results carry source session, branch, entry, and current tip IDs.
`session_reference` requires that tip ID and fails if the source branch changed
between search and capture. Captures default to 65,536 bytes, permit at most
262,144 bytes, and are limited to three successful references per target
branch. Historical text is explicitly framed as untrusted information and can
never grant permissions or override current instructions.

## Go usage

The normal app wiring uses `session.NewSQLiteStore` and `FileIndex`. Direct
embedding can open a store through the session interface:

```go
st, err := session.NewSQLiteStore(path, cwd, session.Options{})
if err != nil {
    return err
}
defer st.Close()

if err := st.Append(session.Entry{
    Type:    session.EntryMessage,
    Message: &protocol.Message{Role: protocol.RoleUser,
        Content: []protocol.ContentBlock{protocol.NewTextBlock("hello")}},
}); err != nil {
    return err
}
messages, err := st.Messages() // complete history
contextMessages, err := st.ContextMessages() // provider-facing projection
```

For lower-level integrations, the driver follows the standard `database/sql`
pattern:

```go
import (
    "database/sql"
    _ "modernc.org/sqlite"
)

db, err := sql.Open("sqlite", "file:/tmp/example.db?_pragma=journal_mode(WAL)")
```

Use transactions for related writes. Do not write directly to `entries` from
outside `SQLiteStore`; the store validates parent IDs and keeps branch tips
atomically consistent.

## Subagent topology and child databases

Schema version 5 introduced `subagent_threads`, a root-session topology table with
thread/parent identity, originating branch, canonical path, role, bounded
status/result/error/usage, child locator, and a generation used for atomic
compare-and-swap transitions. Version 6 adds an immutable role fingerprint so a
trusted config edit cannot silently change a durable child's authority. The table
does not store the child transcript. Pre-v6 rows have no fingerprint and are
reloaded with a conservative read-only role; they never regain mutation
authority from the current configuration.

When `subagents.durable` is enabled, each child uses an independent private
database under `<root-session>.agents/<thread-id>.db`. The directory is `0700`
and files are `0600`. `FileIndex.List` skips every `.db.agents` subtree, so a
child never appears as a resumable root session. Child appends cannot change the
root active branch or tip.

Cold open restores only topology. Stale pending/running/queued records are
reconciled to interrupted metadata and no work starts before the surface calls
`ReadySubagents`. Completed/error histories are represented as unloaded and are
opened lazily for follow-up, messaging, or transcript inspection. A root branch
records where each child originated; active children block branch changes and
terminal metadata is listed only on its source branch. Root compaction does not
rewrite or delete child history.

Deleting a root session manually should also delete its adjacent, same-basename
`.agents` directory. Snow never follows a child locator outside that root-owned
directory during normal construction, and the normal session picker does not
traverse it.

## References

- [modernc.org/sqlite package docs](https://pkg.go.dev/modernc.org/sqlite)
- [SQLite WAL documentation](https://sqlite.org/wal.html)
- [Go `database/sql` transactions](https://pkg.go.dev/database/sql#Tx)
