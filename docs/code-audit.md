# Code audit and remediation record

This document records the repository-wide bug and fragility audit requested in
August 2026. Every identified item below was inspected, remediated, covered by
focused tests where practical, and included in full-suite, race, vet, repeated,
and Windows compile-time validation. Source and tests remain authoritative.

## Security and filesystem safety

- [x] **SEC-01 — Reject unknown permission decisions.** `Authorize` now
  allowlists documented decisions, remembers session/always allows, and returns
  deny plus an error for every unknown value.
- [x] **SEC-02 — Pin allowed roots for the lifetime of a tool host.** App wiring
  creates one canonical `PathGuard`; built-ins no longer replace configured
  guards with mutable host strings. Dynamic-host guards are explicitly closed.
- [x] **SEC-03 — Prevent resolve/open symlink races.** `PathGuard` pins Go
  `os.Root` directory handles. Read, write, edit, and search opens/stat/renames
  are rooted, including atomic temporary-file replacement. Descendant-swap and
  host-retarget tests verify confinement.
- [x] **SEC-04 — Redact provider-controlled errors.** Shared exact-secret
  redaction now covers OpenCode and ChatGPT HTTP/usage-limit/SSE failures before
  errors or events are constructed.
- [x] **SEC-05 — Bind type validation and search enumeration to rooted opens.**
  Read/edit/write/grep validate the opened inode, using nonblocking opens where
  FIFOs exist. Search recursion and ignore-file reads operate through `os.Root`
  rather than ambient `WalkDir`/`Open`. Windows atomic replacement reopens the
  rooted temporary inode with `WRITE_DAC` and copies the destination DACL plus
  its protected/inheritable state before rename.

## Extension and lifecycle correctness

- [x] **EXT-01 — Make MCP catalog refresh atomic.** `SimpleRegistry.ReplaceOwner`
  validates a complete candidate catalog and runs router preparation before one
  owner-scoped commit. Failed initial/live refreshes retain the previous state.
- [x] **EXT-02 — Reject duplicate MCP server IDs.** Complete declaration
  preflight and manager-level ID claims prevent overwrite and leaked sessions.
- [x] **EXT-03 — Stabilize MCP collision naming.** Remote tools are sorted by
  original name, exact duplicates are rejected, and suffix allocation uses a
  refresh-local map published only after commit.
- [x] **EXT-04 — Make plugin shutdown non-reentrant and non-blocking on event
  observers.** Shutdown stops new emissions without waiting for synchronous
  best-effort observers; reentrant/concurrent closes are idempotent.
- [x] **EXT-05 — Preserve plugin initialization failure state.** Fatal static
  registration failures are stored and returned unchanged on later calls;
  registration and rollback run once.
- [x] **EXT-06 — Remove polling-based external-plugin writer locking.** A
  capacity-one context-aware token serializes request and notification frames.
- [x] **EXT-07 — Make permission-aware tool routing exhaustive.** Automatic and
  explicit searches request the full deferred ranking before selecting the top
  permitted schemas.
- [x] **EXT-08 — Enforce the skill discovery candidate bound.** Every candidate
  directory—including malformed and duplicate-name entries—consumes the global
  limit, which stops all remaining roots deterministically.
- [x] **EXT-09 — Close extension lifecycle races completely.** MCP shutdown is
  serialized with refresh and late connect publication; closed runtimes cannot
  republish tools. Router observer installation reconciles under the registry
  catalog lock. Plugin initialization joins rollback close failures.

## Sessions, branches, and compaction

- [x] **SES-01 — Preserve goal-only and branch-only SQLite sessions.** Close and
  listing share a durable-state definition covering messages, goals, branches,
  thread state/metadata, deferrals, and subagent topology.
- [x] **SES-02 — Normalize message topology.** Entry ID/parent columns now
  normalize embedded messages on every append path and when historical
  SQLite/JSONL records are read.
- [x] **SES-03 — Eliminate session-directory collisions.** New sessions use
  fixed-size `cwd-v2-<full-sha256>` directories. Listings search legacy slugs,
  filter by stored normalized CWD, and deduplicate paths.
- [x] **SES-04 — Deep-clone in-memory messages.** `protocol.Message.Clone`
  copies content, image data, raw arguments, usage, and nested cost; memory
  storage clones on ingress, egress, context projection, and fork.
- [x] **SES-05 — Synchronize session reads with switching/closure.** Public
  message/usage/context/branch/identity reads hold agent admission for the full
  store operation. Admitted and active-turn variants avoid reentrant locking;
  close and session switching participate in the same lifetime boundary.
- [x] **SES-06 — Make SQLite `SetBranchTip` transactional and conflict-aware.**
  Tip changes use expected-tip CAS, active-branch compatibility updates, full
  rollback, and local refresh on conflict.
- [x] **SES-07 — Keep complete tool-call/result groups across compaction.** Turn
  boundaries now include mailbox and private autonomous turns and never use a
  raw last-four-message cut through a tool continuation.
- [x] **SES-08 — Persist mail arriving during compaction.** Compaction finalizes
  through `finishTurnMailbox` on success/error/cancellation and joins persistence
  errors into its named return.
- [x] **SES-09 — Preserve every legacy session-directory edge case.** The legacy
  encoder now reproduces the original one-leading-hyphen trim, trailing hyphens,
  and Windows backslash behavior exactly. Windows v2 hashes use the same
  case-insensitive identity relation as listing.

## Tools and providers

- [x] **TOOL-01 — Make `edit` atomic.** Write and edit share rooted,
  same-directory temporary staging, mode preservation, sync, and atomic rename.
- [x] **TOOL-02 — Bound grep memory.** Logical lines are capped at 1 MiB;
  oversized lines are drained without retention, reported, and later lines
  remain searchable with correct numbering.
- [x] **PROV-01 — Bound OpenCode streaming.** SSE lines, per-call/total tool
  arguments, and tool-call counts have explicit limits and fail with one stream
  error instead of unbounded accumulation.
- [x] **PROV-02 — Bound OpenCode startup model discovery.** `/models` and
  models.dev share a default five-second child context (configurable in tests)
  while chat streams remain caller-controlled.
- [x] **PROV-03 — Clamp OAuth device polling.** Intervals parse as `int64`, clamp
  to 1–60 seconds before duration conversion, and `slow_down` growth is capped.
- [x] **PROV-04 — Bound ChatGPT Responses streaming.** Aggregate SSE event size,
  fragment count, tool-call count, per-call/total arguments, identifier size,
  reasoning item count, and retained reasoning bytes now have explicit fail-fast
  limits; completed argument snapshots remain authoritative within those bounds.

## Application and public surfaces

- [x] **APP-01 — Clean up every failed `app.New`.** One ownership-aware deferred
  cleanup closes subagents, agent, interaction broker, MCP, plugins, router,
  session, and rooted filesystem handles in reverse order, joining errors.
- [x] **APP-02 — Route TUI model changes through `App.SetModel`.** Root model,
  app mirrors, runtime child defaults, provider switches, and rollback now use
  the canonical setter.
- [x] **APP-03 — Join RPC workers after scanner errors.** Scanner failures flow
  through the common cancellation, interaction-close, prompt join, wait join,
  and combined error return path.
- [x] **APP-04 — Propagate close errors.** Print/JSON, RPC, and `RunPrompt` use
  named returns with `errors.Join`; accumulated one-shot text is retained.
- [x] **APP-05 — Release the TUI event waiter.** The mailbox has idempotent close
  semantics, drops late events, drains queued work once, and wakes blocked Tea
  commands before app shutdown.
- [x] **APP-06 — Reject invalid permission configuration.** Explicit option and
  config-file typos fail before acquiring credentials, sessions, or extensions.
- [x] **APP-07 — Print Cobra command errors once.** Cobra is silent and the
  executable owns the single `snow:` diagnostic; a helper-process test verifies
  it.
- [x] **APP-08 — Validate print/JSON prompts first.** Blank or whitespace-only
  prompts fail before runtime construction or event output.
- [x] **APP-09 — Reject empty model IDs.** Startup options/config plus app and
  agent setters reject blank IDs without changing root or child-default selection.
- [x] **APP-10 — Validate wait milliseconds before conversion.** Shared parsing
  rejects negatives and duration overflow before RPC/model wait workers start;
  zero retains configured-default semantics.
- [x] **APP-11 — Join late TUI bootstrap cleanup.** Startup commands are admitted
  behind a shutdown gate and joined before `Model.Close` returns; apps completing
  after shutdown close themselves and contribute cleanup errors.

## Verification record

Baseline before remediation:

- `go test ./...` — passed
- `go vet ./...` — passed
- `go test -race ./internal/...` — passed
- focused agent/session/plugin/router/RPC/subagent/SDK tests with `-count=20` — passed

Remediated tree:

- `go test ./... -count=1 -timeout=600s` — passed
- `go vet ./...` — passed
- `go test -race ./internal/... -count=1 -timeout=900s` — passed
- affected provider/plugin/MCP/session/agent/app/RPC/tool/router/subagent/TUI/SDK/CLI
  packages with repeated runs up to `-count=20` — passed
- `GOOS=windows GOARCH=amd64 go vet` for tools/providers/session/MCP/plugin/app/TUI/RPC/CLI — passed
- `git diff --check` — passed

`staticcheck` and `govulncheck` were not installed in the audit environment.
