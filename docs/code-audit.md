# Code Audit and Remediation Record

This document is the repository-wide bug and fragility audit record from the
August 2026 remediation pass. It lists every identified finding, its severity,
its closure status, and the evidence that closes it. Source code and tests
remain authoritative over this summary.

> **Note:** All 46 findings recorded here are closed. Severity is a
> retrospective classification of remediation priority, not a statement of
> current exploitability.

## On this page

- [Status legend](#status-legend)
- [Status summary](#status-summary)
- [Performance and resource bounds](#performance-and-resource-bounds)
- [Security and filesystem safety](#security-and-filesystem-safety)
- [Extension and lifecycle correctness](#extension-and-lifecycle-correctness)
- [Sessions, branches, and compaction](#sessions-branches-and-compaction)
- [Tools and providers](#tools-and-providers)
- [Application and public surfaces](#application-and-public-surfaces)
- [Verification record](#verification-record)
- [Related documents](#related-documents)

## Status legend

| Label | Meaning |
|---|---|
| Critical | Credential exposure, path-confinement escape, or unrecoverable data loss. |
| High | State-corruption or lifecycle race that can lose, misroute, or duplicate data. |
| Medium | Resource-bound or robustness defect with bounded impact. |
| Low | Reporting, cleanup, or consistency defect. |
| Closed | Remediated, covered by focused tests where practical, and validated by the full suite. |

## Status summary

| Finding | Severity | Status | Closure |
|---|---|---|---|
| PERF-01 | Medium | Closed | Bounded event queue; TUI mailbox and row-cache caps |
| PERF-02 | Medium | Closed | Text/reasoning caps plus idle watchdog |
| PERF-03 | Medium | Closed | Newest-marker tail queries for compacted history |
| PERF-04 | Medium | Closed | Query-only inspection; reused FTS index |
| PERF-05 | Medium | Closed | Concurrent catalog load; cached disk; debounced refresh |
| PERF-06 | Medium | Closed | Serialized refresh; artifact sweep; worker join |
| SEC-01 | High | Closed | Allowlisted decisions; deny plus error default |
| SEC-02 | Critical | Closed | Single canonical `PathGuard`; closed dynamic guards |
| SEC-03 | Critical | Closed | Pinned `os.Root` handles; rooted file operations |
| SEC-04 | Critical | Closed | Exact-secret redaction before events are built |
| SEC-05 | High | Closed | Rooted opens for validation and search |
| EXT-01 | Medium | Closed | Atomic owner-scoped catalog commit |
| EXT-02 | Medium | Closed | Declaration preflight and manager ID claims |
| EXT-03 | Medium | Closed | Sorted names; refresh-local suffix map |
| EXT-04 | Medium | Closed | Non-reentrant, idempotent shutdown |
| EXT-05 | Medium | Closed | Stored failure state; single registration |
| EXT-06 | Medium | Closed | Capacity-one context token for writers |
| EXT-07 | Medium | Closed | Full deferred ranking before top-N selection |
| EXT-08 | Medium | Closed | Global candidate bound across all roots |
| EXT-09 | High | Closed | Serialized shutdown/refresh; locked reconciliation |
| SES-01 | High | Closed | Shared durable-state definition |
| SES-02 | High | Closed | Topology normalization on every path |
| SES-03 | Critical | Closed | Fixed `cwd-v2-<sha256>` directories; dedupe |
| SES-04 | Medium | Closed | Deep clone on ingress, egress, projection, fork |
| SES-05 | High | Closed | Admission lock across public reads |
| SES-06 | High | Closed | Expected-tip CAS with rollback |
| SES-07 | High | Closed | Turn boundaries include mailbox and autonomous turns |
| SES-08 | High | Closed | `finishTurnMailbox` finalization |
| SES-09 | Low | Closed | Legacy encoder parity |
| TOOL-01 | Critical | Closed | Rooted temporary staging plus atomic rename |
| TOOL-02 | Medium | Closed | 1 MiB line cap; drained without retention |
| PROV-01 | Medium | Closed | SSE, argument, and tool-call limits |
| PROV-02 | Medium | Closed | Five-second discovery child context |
| PROV-03 | Medium | Closed | 1-60 second clamp; capped growth |
| PROV-04 | Medium | Closed | Fail-fast SSE and argument limits |
| APP-01 | Medium | Closed | Reverse-order deferred cleanup |
| APP-02 | Low | Closed | Canonical `SetModel` setter |
| APP-03 | Medium | Closed | Common cancellation and join path |
| APP-04 | Low | Closed | Named returns with `errors.Join` |
| APP-05 | Medium | Closed | Idempotent mailbox close |
| APP-06 | Medium | Closed | Fail before resources are acquired |
| APP-07 | Low | Closed | Single `snow:` diagnostic |
| APP-08 | Low | Closed | Prompt validation before runtime construction |
| APP-09 | Low | Closed | Blank model ID rejection |
| APP-10 | Medium | Closed | Parse-before-convert wait validation |
| APP-11 | Medium | Closed | Shutdown gate and worker join |

## Performance and resource bounds

- **PERF-01 — Bound event delivery and TUI retention** (Medium, closed). The
  agent event bus keeps a bounded queue, preferentially sheds stream/snapshot
  events under a pathological slow observer, and rejects reentrant drains. The
  TUI mailbox has hard item/byte bounds, and full-screen transcript rows use a
  bounded recent cache with incremental wrapping and lazy selection splitting.
- **PERF-02 — Bound provider streams** (Medium, closed). Chat Completions and
  Responses cap accumulated text/reasoning, and a configurable reset-on-byte
  idle watchdog defaults to ten minutes (`-1` disables it).
- **PERF-03 — Stop decoding compacted SQLite history** (Medium, closed).
  Context projection locates the newest marker and queries only its retained
  tail, validates corrupt boundaries, and resolves repeated-compaction virtual
  IDs to real persisted boundaries.
- **PERF-04 — Make session discovery/search incremental** (Medium, closed).
  Session listing uses query-only SQLite inspection, branch stats avoid
  full-history decoding, usage/reference counts use focused JSON queries, and
  the derived FTS index is reused until an exact branch name/tip fingerprint
  changes (including WAL writes).
- **PERF-05 — Reduce startup and extension churn** (Medium, closed). Provider
  catalogs load concurrently after the active provider, OpenCode catalogs use a
  bounded private disk cache, and MCP list-change refresh is debounced,
  timeout-bounded, lock-short, identical-catalog aware, and collision-safe.
- **PERF-06 — Close lifecycle and disk-I/O leaks** (Medium, closed). OAuth
  network refresh is serialized by a provider-specific lock without holding the
  global auth-file lock, auth reads are stat/inode cached, crash-orphaned
  artifacts are repaired, stale atomic-write files are swept conservatively,
  browser helper processes are reaped, skill discovery reads frontmatter
  prefixes, and subagent eviction workers are coalesced and joined.
- **PERF-07 — Page exact TUI hydration and gate regressions** (Medium, closed).
  Schema-v11 stores an append-atomic, rebuildable ancestry projection; the TUI
  reads message blobs in 256-entry pages for only the retained suffix plus
  focused tool-call lookbehind. Parity tests preserve omission, plan, input,
  context, compaction, branch, and tool-card semantics. Linux CI gates reviewed
  median `B/op` and `allocs/op` ceilings while reporting noisy timing only.

## Security and filesystem safety

- **SEC-01 — Reject unknown permission decisions** (High, closed). `Authorize`
  now allowlists documented decisions, remembers session/always allows, and
  returns deny plus an error for every unknown value.
- **SEC-02 — Pin allowed roots for the lifetime of a tool host** (Critical,
  closed). App wiring creates one canonical `PathGuard`; built-ins no longer
  replace configured guards with mutable host strings. Dynamic-host guards are
  explicitly closed.
- **SEC-03 — Prevent resolve/open symlink races** (Critical, closed).
  `PathGuard` pins Go `os.Root` directory handles. Read, write, edit, and
  search opens/stat/renames are rooted, including atomic temporary-file
  replacement. Descendant-swap and host-retarget tests verify confinement.
- **SEC-04 — Redact provider-controlled errors** (Critical, closed). Shared
  exact-secret redaction now covers OpenCode and ChatGPT HTTP/usage-limit/SSE
  failures before errors or events are constructed.
- **SEC-05 — Bind type validation and search enumeration to rooted opens**
  (High, closed). Read/edit/write/grep validate the opened inode, using
  nonblocking opens where FIFOs exist. Search recursion and ignore-file reads
  operate through `os.Root` rather than ambient `WalkDir`/`Open`.

## Extension and lifecycle correctness

- **EXT-01 — Make MCP catalog refresh atomic** (Medium, closed).
  `SimpleRegistry.ReplaceOwner` validates a complete candidate catalog and runs
  router preparation before one owner-scoped commit. Failed initial/live
  refreshes retain the previous state.
- **EXT-02 — Reject duplicate MCP server IDs** (Medium, closed). Complete
  declaration preflight and manager-level ID claims prevent overwrite and
  leaked sessions.
- **EXT-03 — Stabilize MCP collision naming** (Medium, closed). Remote tools
  are sorted by original name, exact duplicates are rejected, and suffix
  allocation uses a refresh-local map published only after commit.
- **EXT-04 — Make plugin shutdown non-reentrant and non-blocking on event
  observers** (Medium, closed). Shutdown stops new emissions without waiting
  for synchronous best-effort observers; reentrant/concurrent closes are
  idempotent.
- **EXT-05 — Preserve plugin initialization failure state** (Medium, closed).
  Fatal static registration failures are stored and returned unchanged on
  later calls; registration and rollback run once.
- **EXT-06 — Remove polling-based external-plugin writer locking** (Medium,
  closed). A capacity-one context-aware token serializes request and
  notification frames.
- **EXT-07 — Make permission-aware tool routing exhaustive** (Medium, closed).
  Automatic and explicit searches request the full deferred ranking before
  selecting the top permitted schemas.
- **EXT-08 — Enforce the skill discovery candidate bound** (Medium, closed).
  Every candidate directory, including malformed and duplicate-name entries,
  consumes the global limit, which stops all remaining roots deterministically.
- **EXT-09 — Close extension lifecycle races completely** (High, closed). MCP
  shutdown is serialized with refresh and late connect publication; closed
  runtimes cannot republish tools. Router observer installation reconciles
  under the registry catalog lock. Plugin initialization joins rollback close
  failures.

## Sessions, branches, and compaction

- **SES-01 — Preserve goal-only and branch-only SQLite sessions** (High,
  closed). Close and listing share a durable-state definition covering
  messages, goals, branches, thread state/metadata, deferrals, and subagent
  topology.
- **SES-02 — Normalize message topology** (High, closed). Entry ID/parent
  columns now normalize embedded messages on every append path and when
  historical SQLite/JSONL records are read.
- **SES-03 — Eliminate session-directory collisions** (Critical, closed). New
  sessions use fixed-size `cwd-v2-<full-sha256>` directories. Listings search
  legacy slugs, filter by stored normalized CWD, and deduplicate paths.
- **SES-04 — Deep-clone in-memory messages** (Medium, closed).
  `protocol.Message.Clone` copies content, image data, raw arguments, usage,
  and nested cost; memory storage clones on ingress, egress, context
  projection, and fork.
- **SES-05 — Synchronize session reads with switching/closure** (High, closed).
  Public message/usage/context/branch/identity reads hold agent admission for
  the full store operation. Admitted and active-turn variants avoid reentrant
  locking; close and session switching participate in the same lifetime
  boundary.
- **SES-06 — Make SQLite `SetBranchTip` transactional and conflict-aware**
  (High, closed). Tip changes use expected-tip CAS, active-branch compatibility
  updates, full rollback, and local refresh on conflict.
- **SES-07 — Keep complete tool-call/result groups across compaction** (High,
  closed). Turn boundaries now include mailbox and private autonomous turns and
  never use a raw last-four-message cut through a tool continuation.
- **SES-08 — Persist mail arriving during compaction** (High, closed).
  Compaction finalizes through `finishTurnMailbox` on
  success/error/cancellation and joins persistence errors into its named
  return.
- **SES-09 — Preserve legacy session-directory edge cases** (Low, closed). The
  legacy encoder reproduces the original one-leading-hyphen trim and trailing
  hyphens.

## Tools and providers

- **TOOL-01 — Make `edit` atomic** (Critical, closed). Write and edit share
  rooted, same-directory temporary staging, mode preservation, sync, and atomic
  rename.
- **TOOL-02 — Bound grep memory** (Medium, closed). Logical lines are capped at
  1 MiB; oversized lines are drained without retention, reported, and later
  lines remain searchable with correct numbering.
- **PROV-01 — Bound OpenCode streaming** (Medium, closed). SSE lines,
  per-call/total tool arguments, and tool-call counts have explicit limits and
  fail with one stream error instead of unbounded accumulation.
- **PROV-02 — Bound OpenCode startup model discovery** (Medium, closed).
  `/models` and models.dev share a default five-second child context
  (configurable in tests) while chat streams remain caller-controlled.
- **PROV-03 — Clamp OAuth device polling** (Medium, closed). Intervals parse
  as `int64`, clamp to 1-60 seconds before duration conversion, and
  `slow_down` growth is capped.
- **PROV-04 — Bound ChatGPT Responses streaming** (Medium, closed). Aggregate
  SSE event size, fragment count, tool-call count, per-call/total arguments,
  identifier size, reasoning item count, and retained reasoning bytes now have
  explicit fail-fast limits; completed argument snapshots remain authoritative
  within those bounds.

## Application and public surfaces

- **APP-01 — Clean up every failed `app.New`** (Medium, closed). One
  ownership-aware deferred cleanup closes subagents, agent, interaction broker,
  MCP, plugins, router, session, and rooted filesystem handles in reverse
  order, joining errors.
- **APP-02 — Route TUI model changes through `App.SetModel`** (Low, closed).
  Root model, app mirrors, runtime child defaults, provider switches, and
  rollback now use the canonical setter.
- **APP-03 — Join RPC workers after scanner errors** (Medium, closed). Scanner
  failures flow through the common cancellation, interaction-close, prompt
  join, wait join, and combined error return path.
- **APP-04 — Propagate close errors** (Low, closed). Print/JSON, RPC, and
  `RunPrompt` use named returns with `errors.Join`; accumulated one-shot text
  is retained.
- **APP-05 — Release the TUI event waiter** (Medium, closed). The mailbox has
  idempotent close semantics, drops late events, drains queued work once, and
  wakes blocked Tea commands before app shutdown.
- **APP-06 — Reject invalid permission configuration** (Medium, closed).
  Explicit option and config-file typos fail before acquiring credentials,
  sessions, or extensions.
- **APP-07 — Print Cobra command errors once** (Low, closed). Cobra is silent
  and the executable owns the single `snow:` diagnostic; a helper-process test
  verifies it.
- **APP-08 — Validate print/JSON prompts first** (Low, closed). Blank or
  whitespace-only prompts fail before runtime construction or event output.
- **APP-09 — Reject empty model IDs** (Low, closed). Startup options/config
  plus app and agent setters reject blank IDs without changing root or
  child-default selection.
- **APP-10 — Validate wait milliseconds before conversion** (Medium, closed).
  Shared parsing rejects negatives and duration overflow before RPC/model wait
  workers start; zero retains configured-default semantics.
- **APP-11 — Join late TUI bootstrap cleanup** (Medium, closed). Startup
  commands are admitted behind a shutdown gate and joined before `Model.Close`
  returns; apps completing after shutdown close themselves and contribute
  cleanup errors.

## Verification record

Baseline before remediation:

- `go test ./...` passed
- `go vet ./...` passed
- `go test -race ./internal/...` passed
- Focused agent/session/plugin/router/RPC/subagent/SDK tests with
  `-count=20` passed

Remediated tree:

- `go test ./... -count=1 -timeout=600s` passed
- `go vet ./...` passed
- `go test -race ./internal/... -count=1 -timeout=900s` passed
- Affected
  provider/plugin/MCP/session/agent/app/RPC/tool/router/subagent/TUI/SDK/CLI
  packages with repeated runs up to `-count=20` passed
- `git diff --check` passed

`staticcheck` and `govulncheck` were not installed in the original audit
environment. Alpha release CI now runs pinned `govulncheck` separately; this
historical note records only the tooling available during that audit.

## Related documents

- [Agent working guide](../AGENTS.md)
- [Documentation index](README.md)
- [Security model](security.md)
- [Sessions and SQLite storage](sessions.md)
- [TUI responsiveness](tui-performance.md)
