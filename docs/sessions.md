# SQLite session storage

Snow persists each conversation as a single SQLite database. This document
covers the on-disk layout, schema and pragmas, the append-only entry and branch
model, resume and fork workflows, the session index, and the embedding API in
`internal/session`. Provider, TUI, and SDK surfaces are described in their own
guides.

## On this page

- [Storage layout and discovery](#storage-layout-and-discovery)
- [Titles and identity](#titles-and-identity)
- [Driver and pragmas](#driver-and-pragmas)
- [Schema](#schema)
- [Branches and compaction projection](#branches-and-compaction-projection)
- [Independent session forks](#independent-session-forks)
- [Git worktree forks](#git-worktree-forks)
- [Prior-session search and references](#prior-session-search-and-references)
- [Go usage and embedding](#go-usage-and-embedding)
- [Subagent topology and child databases](#subagent-topology-and-child-databases)
- [Related documents](#related-documents)

## Storage layout and discovery

Sessions live under one database per conversation:

```text
~/.snow/sessions/<encoded-working-directory>/<timestamp>_<suffix>.db
```

Set `SNOW_SESSIONS_DIR` to move the sessions root. New directory names use
`cwd-v2-<full-sha256>` over the normalized absolute working directory, which
avoids the collisions and path-length growth of the legacy flattened format.
`FileIndex.List` also searches the legacy directory and filters every database
by its stored CWD, so old sessions stay discoverable without cross-project
mixing.

`--no-session` (or the SDK `NoSession` option) keeps the session in memory.
Interactive `snow resume` opens a current-working-directory picker;
`snow resume /path/to/session.db` opens that explicit database. Headless
print/JSON/RPC use selects the newest indexed session when no argument is
given, because a picker is unavailable. The lower-level
`--session /path/to/session.db` flag and the SDK `SessionPath` option also
select a path. The previous JSONL format is intentionally not migrated.

## Titles and identity

Built-in sessions receive a provider-free display title from the first accepted
user prompt. Whitespace is collapsed and long titles are truncated at 72 runes;
image-only prompts use `Image prompt`. Manual rename trims surrounding
whitespace, requires 1-72 runes, and rejects control characters.

Titles are session-wide metadata: they need not be unique, do not enter
ordinary conversation context (although `session_search` and
`session_reference` results can surface them), and do not rename the database
or change its stable ID. A manually titled empty session is durable and remains
visible in the picker.

## Driver and pragmas

Snow uses [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite), a
pure-Go, CGo-free SQLite driver that implements Go's `database/sql` API. No
SQLite shared library or C toolchain is required.

New session databases are created with mode `0600` inside `0755` directories.
Opening an existing database does not re-chmod it, so a file loosened or
created outside Snow must be secured separately.

The store opens each database with:

- write-ahead logging (WAL) journaling for concurrent read/write behavior;
- `synchronous=NORMAL`, which avoids an `fsync` for every individual entry
  while retaining SQLite transaction recovery;
- foreign keys enabled;
- a 5-second busy timeout (see the
  [SQLite WAL documentation](https://sqlite.org/wal.html));
- one database connection, avoiding connection-local pragma surprises while
  WAL still permits readers to proceed during a writer commit.

## Schema

Each database contains the following tables:

| Table | Purpose | Columns |
|---|---|---|
| `session_meta` | One metadata row per session | `singleton`, `version`, `session_id`, `created_at`, `cwd`, `name`, `branch_tip`, `parent_session_id`, `parent_branch_id`, `fork_entry_id` |
| `entries` | Append-ordered, parent-linked entries | `seq`, `id`, `parent_id`, `entry_type`, `message`, `summary`, `compacted_through`, `meta_key`, `meta_value` |
| `session_branches` | Durable branch references | `branch_id`, `branch_name`, `parent_branch_id`, `forked_from_id`, `tip_id`, `created_at`, `updated_at`, `active` |
| `thread_state` | Branch-scoped collaboration mode | `branch_id`, `collaboration_mode` |
| `thread_goals` | Branch-scoped goal state | `branch_id`, `goal_id`, `objective`, `status`, `token_budget`, `tokens_used`, `seconds_used`, `created_at`, `updated_at` |
| `thread_goal_costs` | Per-currency estimated goal cost totals | `branch_id`, `currency`, `input_cost`, `output_cost`, `cache_read_cost`, `cache_write_cost`, `total_cost` |
| `thread_goal_deferrals` | Goal continuation deferral | `branch_id`, `deferred` |
| `subagent_threads` | Root-scoped subagent topology | `thread_id`, `parent_thread_id`, `parent_branch_id`, `agent_path`, `parent_path`, `role`, `role_fingerprint`, `nickname`, `depth`, `status`, `child_session_path`, `model_provider`, `model_id`, `thinking`, `created_at`, `started_at`, `finished_at`, `result`, `error`, `usage_json`, `generation` |

Snow creates these indexes:

- `entries_parent_idx` on `entries(parent_id)`;
- `entries_type_idx` on `entries(entry_type)`;
- `session_branches_active_idx` on `session_branches(active)`;
- `subagent_threads_parent_idx` on
  `subagent_threads(parent_thread_id, created_at)`;
- `subagent_threads_branch_idx` on
  `subagent_threads(parent_branch_id, created_at)`;
- a unique `session_branches_name_idx` on
  `session_branches(branch_name COLLATE NOCASE)`.

User image attachments are stored as `image` content blocks in the message
JSON; Go's JSON encoding represents their bytes as base64. Clipboard images
therefore increase the session database size and remain available when a
branch is resumed.

### Schema versions

Version-1 databases are upgraded on open by creating a `main` branch pointing
at the existing `branch_tip`. Forking creates another reference in the same
SQLite tree; message rows are not copied. Selecting a branch changes the active
tip used by subsequent appends, `Messages`, `Usage`, and prompts. `Branches`
returns branch topology, previews, and message counts for the TUI/SDK tree
picker.

Later versions add columns and backfill conservatively:

- version 2 adds the `compacted_through` compaction boundary column;
- version 5 introduces `subagent_threads`;
- version 6 adds the immutable `role_fingerprint` column;
- version 7 adds branch names and parent/fork metadata; legacy non-main
  branches retain their IDs as names and attach to `main` when ancestry is
  unavailable;
- version 8 adds atomic per-currency goal cost totals; migration backfills
  priced historical goal usage only when its exact token sum matches the
  persisted goal counter;
- version 9 adds empty-by-default session-fork provenance columns.

## Branches and compaction projection

`id` and `parent_id` are authoritative; Snow normalizes embedded message
identity from those columns on write and read. The active `BranchTip`
linearizes one branch, and an append inserts the entry and updates the active
branch tip in a single transaction. Branch-tip moves use transactional
optimistic compare-and-swap, so a stale handle returns a conflict instead of
overwriting a newer tip.

`Messages()` uses a recursive common table expression (CTE) to walk only
the active branch, so opening a large session does not deserialize every
historical branch into memory.
`ContextMessages()` applies the latest compaction marker logically: providers
receive one structured working-state checkpoint plus the retained tail, while
`Messages()` continues to return the complete historical branch. Checkpoints
carry objectives, decisions, files, verification, failures, collaboration
updates, retrieval references, and pending work. Compaction boundaries preserve
complete tool call/result pairs; provider-private continuity leaves projected
context only with its complete old turn.

Besides total context pressure, safely compactable old tool history has an
independent model-window budget. Minimum-retained recent work does not count
toward that trigger. During one long active turn, completed assistant-call/tool-
result cycles become safe boundaries when prefix-only projection can do so
without consuming the exact recent-turn floor; Snow may checkpoint older
complete cycles while retaining the current and recent cycles exactly. It never cuts
between a tool call and its result or detaches provider-private continuity from
its owning assistant message. Compacted tool prefixes gain one bounded private
text/metadata transcript reference. Up to 24 references verified against the
current session are carried across repeated compaction and physical forks;
raw markers are intersected with a single structural enumeration of the source
session's private artifact namespace, so forged or stale IDs do not cause
per-marker filesystem opens and cannot block a fork. To avoid a partially
retrievable exact fork, physical forking fails and rolls back only when more
than 1,024 verified owned artifacts are referenced. Image payloads remain
available in append-only session history rather than the text artifact.

Before every ordinary provider request and semantic compaction, oversized
plain-text tool results are projected as a bounded head, a byte-counted
omission marker, and a tail. Existing exact durable messages remain unchanged.
New oversized results are spilled immediately to immutable private files under
`~/.snow/artifacts` (or `SNOW_HOME/artifacts`); the durable tool message keeps
only the preview plus an opaque artifact ID. The deferred read-risk
`artifact_read` and `artifact_grep` tools authorize IDs against the current
session and return bounded fragments; artifacts are not added to ordinary
filesystem roots. Snow does not currently garbage-collect spilled artifacts,
and deleting a session database does not remove its artifact namespace.
This model-free pruning reduces every subsequent provider request, not only
the summarizer. `Metadata` and `SetMetadata` store append-only per-session
state such as permission mode and remembered tool rules.

On open, the agent checks the final provider tool batch for calls without
results. A hard crash cannot prove whether an external operation completed, so
Snow appends error results that mark read-risk calls as retryable and
write/exec/network/delegation calls as having an unknown outcome. It never
automatically retries an interrupted side effect. Recovery is idempotent and
uses one atomic batch when the store supports batch appends. `FileIndex.List`
counts branch messages with SQL rather than loading the full transcript.

Root-only databases are removed on close and omitted from `FileIndex.List`.
Messages, goals, additional branches, non-default thread state, remembered
metadata, or subagent topology make a session durable and listable even when
its active branch currently has zero messages.

## Independent session forks

Independent forks are physical snapshots, not additional `session_branches`
rows. Snow validates that the selected entry belongs to the requested branch
and that its root-to-entry chain does not end with an incomplete assistant
response or unresolved tool calls. It then:

1. creates a temporary SQLite database in the destination directory;
2. copies the exact entry chain, including metadata and compaction markers
   while preserving entry and parent IDs;
3. creates one local `main` branch and writes a new session ID plus parent
   provenance;
4. closes the database, publishes it without replacement, and reopens it
   before reporting success.

Private spill artifacts referenced by Snow's retained-result markers are
copied into the child session namespace with the same opaque IDs. Existing
destinations are never overwritten.

The source remains unchanged and parent/child databases diverge independently.
An independent child retains its fork provenance even if its parent session is
later moved or deleted. A current-tip fork copies collaboration mode. A
historical fork uses Default mode because mutable thread state is not
versioned per entry; active goals, goal accounting/deferrals, subagent
topology, and private child databases are not copied. Same-database branch
forks retain their branch-scoped mode and goal cloning semantics.

The CLI equivalent is `snow fork [session-path]` with `--from-entry`,
`--source-branch`, `--name`, and `--destination`.

## Git worktree forks

`snow fork-worktree` creates a detached child session inside a clean Git
worktree:

```sh
snow fork-worktree [session-path] --worktree ../snow-experiment \
  --git-branch snow/experiment --name experiment
```

It requires a clean, non-bare Git repository: uncommitted state is rejected
because Git does not transfer it to a new worktree. Snow invokes Git directly
with a 30-second timeout and bounded output, creates a new branch (default
`snow/<slug>-<suffix>`), and never reuses an existing path or branch. A
relative `--destination` resolves inside the new worktree; absolute paths are
explicit operator choices, and traversal outside the worktree is rejected.

On failure Snow rolls back the exact worktree and unchanged branch with
compare-and-swap and never silently falls back to a less isolated fork. The
TUI `/fork` Git worktree option keeps the current TUI in the source and prints
a `snow resume` command for opening the child; the CLI `snow fork-worktree`
command returns a JSON `SessionForkResult` with `Worktree` information.

## Prior-session search and references

Snow exposes two deferred, read-only model tools for reusing prior work:

- `session_search` builds a disposable SQLite full-text search 5 (FTS5)
  index from the current project's durable root sessions and returns one
  bounded representative hit per matching branch. The durable session
  databases remain authoritative and are never modified; the derived index
  is a disposable in-memory FTS5 cache rebuilt when the project's session
  set changes.
- `session_reference` captures a selected search result as a bounded immutable
  snapshot. The snapshot is persisted as the ordinary tool-result message on
  the current branch, so later changes to the source session cannot alter
  replay.

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

## Go usage and embedding

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
messages, err := st.Messages()          // complete history
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

Use [Go `database/sql` transactions](https://pkg.go.dev/database/sql#Tx) for
related writes. Do not write directly to `entries` from outside
`SQLiteStore`; the store validates parent IDs and keeps branch tips
atomically consistent.

## Subagent topology and child databases

The `subagent_threads` table stores root-session topology: thread/parent
identity, originating branch, canonical path, role, bounded
status/result/error/usage, child locator, and a generation used for atomic
compare-and-swap transitions. An immutable role fingerprint prevents a trusted
config edit from silently changing a durable child's authority. The table does
not store the child transcript. Pre-v6 rows have no fingerprint and are
reloaded with a conservative read-only role; they never regain mutation
authority from the current configuration.

When `subagents.durable` is enabled, each child uses an independent private
database under `<root-session>.agents/<thread-id>.db`. The directory is `0700`
and files are `0600`. `FileIndex.List` skips every `.db.agents` subtree, so a
child never appears as a resumable root session. Child appends cannot change
the root active branch or tip.

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

## Related documents

- [Configuration](configuration.md)
- [Thread Goals](goals.md)
- [Subagents](subagents.md)
- [SDK](sdk.md)
- [Using Snow](using-snow.md)
