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
mixing. `--no-session`/`NoSession` keeps the session in memory. `--session
/path/to/session.db` resumes a specific database. The previous JSONL format is
intentionally not migrated.

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
- indexes on `entries.parent_id`, `entries.entry_type`, and active branches.

Version-1 databases are upgraded on open by creating a `main` branch pointing
at the existing `branch_tip`. Forking creates another reference in the same
SQLite tree; message rows are not copied. Selecting a branch changes the active
tip used by subsequent appends, `Messages`, `Usage`, and prompts. `Branches`
returns branch topology, previews, and message counts for the TUI/SDK tree picker.
Schema version 7 adds names and parent/fork metadata; legacy non-main branches
retain their IDs as names and attach to `main` when ancestry is unavailable.

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
return the complete historical branch. `Metadata`/`SetMetadata` store
append-only per-session state such as permission mode and remembered tool rules.
`FileIndex.List` counts branch messages with SQL rather than loading the full
transcript.

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
