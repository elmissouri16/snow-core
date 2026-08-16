# Subagent implementation plan

This document is the historical, phase-by-phase plan that produced Snow's
Codex-V2-style subagent tree. It records the original scope, tasks, acceptance
criteria, and statuses for each implementation phase, preserved as they were
planned. Verified user-facing behavior now lives in
[Subagents](subagents.md); treat this file as task provenance and design
rationale rather than a live feature reference.

> **Note:** Implemented. Every phase below landed in Snow, and the shipped
> behavior is documented in [Subagents](subagents.md). The upstream research
> basis is pinned to OpenAI Codex commit
> `3aae5d885bac39c1262491aa3fd100dfd8b3919f`.

## On this page

- [Status summary](#status-summary)
- [Research basis](#research-basis)
- [Phase 0 — lock public semantics and baseline](#phase-0-lock-public-semantics-and-baseline)
- [Phase 1 — protocol identity and Agent mailbox](#phase-1-protocol-identity-and-agent-mailbox)
- [Phase 2 — ephemeral manager and child factory](#phase-2-ephemeral-manager-and-child-factory)
- [Phase 3 — V2 model tools and headless integration](#phase-3-v2-model-tools-and-headless-integration)
- [Phase 4 — SDK and RPC surfaces](#phase-4-sdk-and-rpc-surfaces)
- [Phase 5 — TUI observation and interaction multiplexing](#phase-5-tui-observation-and-interaction-multiplexing)
- [Phase 6 — durable topology and lazy reload](#phase-6-durable-topology-and-lazy-reload)
- [Phase 7 — recursive agents, mutation, dynamic tools, and budgets](#phase-7-recursive-agents-mutation-dynamic-tools-and-budgets)
- [Related documents](#related-documents)

## Status summary

| Phase | Scope | Status | Evidence |
|---|---|---|---|
| 0 | Lock public semantics and baseline | Implemented | [Enabling subagents](subagents.md#enabling-subagents), [Limits](subagents.md#limits) |
| 1 | Protocol identity and Agent mailbox | Implemented | [Model tools](subagents.md#model-tools), [Paths, names, and identity](subagents.md#paths-names-and-identity) |
| 2 | Ephemeral manager and child factory | Implemented | [Context and lifecycle](subagents.md#context-and-lifecycle), [Authority and security](subagents.md#authority-and-security) |
| 3 | V2 model tools and headless integration | Implemented | [Model tools](subagents.md#model-tools), [Authority and security](subagents.md#authority-and-security) |
| 4 | SDK and RPC surfaces | Implemented | [Surfaces](subagents.md#surfaces) |
| 5 | TUI observation and interaction multiplexing | Implemented | [Surfaces](subagents.md#surfaces), [Context and lifecycle](subagents.md#context-and-lifecycle) |
| 6 | Durable topology and lazy reload | Implemented | [Persistence](subagents.md#persistence) |
| 7 | Recursive agents, mutation, dynamic tools, and budgets | Implemented | [Authority and security](subagents.md#authority-and-security), [Limits](subagents.md#limits) |

## Research basis

The upstream analysis is pinned to OpenAI Codex commit
`3aae5d885bac39c1262491aa3fd100dfd8b3919f` and was read from checked-out source
and adjacent tests, not product descriptions. Key sources:

| Source | Studied for |
|---|---|
| `codex-rs/core/src/agent/control.rs`, `control/spawn.rs`, `registry.rs`, `role.rs` | Agent control, spawn transaction, registry, role overlays |
| `codex-rs/core/src/session/input_queue.rs` | Attributed mailbox and safe delivery points |
| `codex-rs/core/src/tools/handlers/multi_agents_spec.rs`, `multi_agents_v2/` | V1/V2 model tool surface and contracts |
| `codex-rs/core/src/tools/spec_plan.rs` | Plan-mode interaction with subagent tools |
| `codex-rs/agent-graph-store/` | Durable topology and child transcript separation |
| `codex-rs/tui/src/app/agent_navigation.rs` | TUI-as-observer navigation model |
| `codex-rs/core/src/tools/handlers/multi_agents_tests.rs` | Behavioral test scenarios |

### Executive recommendation

Implement the Codex V2 architecture directly; do not first reproduce Codex V1
and migrate. The design adopted for Snow:

1. Every subagent is an independent `agent.Agent` using the existing provider →
   tools → persistence loop.
2. One root-scoped `internal/subagent.Manager` owns identity, topology, limits,
   lifecycle, mailboxes, child construction, and shutdown.
3. The model controls the manager through seven V2-style tools:
   `spawn_agent`, `list_subagent_models`, `send_message`, `followup_task`,
   `wait_agent`, `interrupt_agent`, and `list_agents`.
4. Agents use canonical task paths such as `/root/api_review`; UUID-like
   thread IDs remain host correlation keys but are not the preferred
   model-facing identity.
5. Parent and child histories never share one mutable session-store cursor.
   Children use independent memory stores, then separate SQLite databases plus
   a graph/metadata table in the root session.
6. Child messages enter an attributed mailbox and are incorporated only at safe
   agent-loop boundaries; a child completion never appends concurrently to a
   parent branch mid-tool-chain.
7. Child authority is never broader than parent authority. The first release is
   read-only and noninteractive by default; mutation and recursive spawning are
   separately enabled only after multiplexed permissions and resource limits
   are proven.
8. The feature ships disabled by default until the race suite, shutdown tests,
   public contracts, and shared-workspace warnings are complete.

### Codex findings

| # | Finding | Snow implication |
|---|---|---|
| 1.1 | Codex has two generations: V1 (`multi_agent`, default) and V2 (`multi_agent_v2`) | Snow has no compatibility burden, so only V2 was implemented |
| 1.2 | A subagent is a normal agent thread with its own transcript, provider turns, tools, and events | `internal/agent.Agent` is already the right child runtime; the manager constructs more `Agent` instances |
| 1.3 | Spawn is a reservation/commit transaction | Child identity and limits are reserved before the child turn starts |
| 1.4 | Roles are config overlays, not agent subclasses | Snow roles select tools and policy; they do not fork the agent loop |
| 1.5 | Identity uses canonical agent paths | Snow uses `/root/<name>` paths as the model-facing identity |
| 1.6 | Context inheritance is independent from runtime policy inheritance | Child context projection is explicit; authority is always intersected with parent policy |
| 1.7 | V2 messaging is a mailbox protocol | Child messages are attributed and consumed only at safe loop boundaries |
| 1.8 | Wait is an activity barrier | `wait_agent` blocks on lifecycle activity, not polling for result text |
| 1.9 | Limits distinguish execution from residency | Concurrency bounds active runs; agent count bounds durable identities |
| 1.10 | Status is event-derived | Child status is recomputed from ordered lifecycle events, never a cached string |
| 1.11 | Persistence separates transcript and topology | Child databases hold transcripts; the root session holds topology metadata |
| 1.12 | Permissions and sandbox policy remain inherited | Children never broaden authority; mutation requires explicit opt-ins |
| 1.13 | The TUI is an observer, not the orchestrator | The TUI renders manager events; the manager owns lifecycle |
| 1.14 | Hooks and telemetry are event-driven | Snow forwards attributed child events through the existing event bus |

### Gap analysis

| Area | Existing Snow seam | Missing work |
|---|---|---|
| Agent runtime | `agent.New(Options)` constructs a reusable loop | Child factory and tree-scoped manager |
| Turn admission | One `Agent` safely allows one active turn | One independent `Agent` per child |
| Tool control | Thread-safe registry and model tools | Seven manager-bound collaboration tools |
| Session history | Memory/SQLite stores, context projection, forks | Independent child stores and topology metadata |
| Messaging | User prompts and tool results only | Attributed mailbox safe points |
| Events | Ordered cloned cross-surface bus | Child identity, status, activity, filtered forwarding |
| Permissions | Thread-safe policy service | Attributed FIFO ask/user-input multiplexing |
| Limits | Per-agent turns/calls and goal budgets | Tree concurrency/depth/count/time/output/usage governor |
| SDK | Thin wrapper over App/Agent | List/get/spawn/message/follow-up/wait/interrupt APIs |
| RPC | Async root prompt and locked JSONL writer | Multi-child commands and event correlation |
| TUI | Lossless mailbox and one root transcript | Agent map, picker, status feed, per-child buffer/view |
| Persistence | Session branch tree | Subagent metadata/CAS, child DB lifecycle, recovery |
| Shutdown | App closes one Agent then resources | Cancel/join manager and children without event loss |

### Hard constraints from the existing code

1. Never run two turns on one `Agent`; its running, pending tool calls, usage,
   cancel function, mode, and session fields are singular.
2. Never share one mutable store handle; appends default to that handle's
   current tip.
3. Never treat existing user branches as concurrent worker lanes; branch
   selection updates database-global active metadata.
4. Never merge anonymous child deltas into root events; every forwarded event
   needs child identity.
5. Never share the TUI asker across children without a queue; it rejects a
   second pending request.
6. Never build child `App`s recursively; that would reconnect plugins/MCP,
   reload trust/config/auth, create unrelated top-level sessions, and duplicate
   global resources.
7. Never let roles broaden authority; role configuration is subordinate to
   parent and operator policy.

## Phase 0 — lock public semantics and baseline

Lock the public names, tool schemas, status transitions, and cancellation
policy before any runtime code changes, so later phases never force a public
DTO or API rename.

### Tasks

| Task | Status |
|------|--------|
| Add this plan to the repository and update the explicit multi-agent scope statement only when implementation begins | Implemented |
| Capture baseline focused/race tests before changing runtime code | Implemented |
| Decide the feature name (`subagents`) once and use it consistently in config, package names, docs, events, RPC, and SDK | Implemented |
| Add architecture decision records in code comments for cancellation, mailbox ordering, independent stores, and permission intersection | Implemented |

### Acceptance criteria

- Tool schemas and status transitions are approved.
- Parent abort versus child survival is documented.
- First-release read-only policy is documented.
- No unresolved choice can force a public DTO rename later.

### Verification

Phase 0 introduces no runtime code. Its outcomes are recorded in the
acceptance criteria above and carried forward as the baseline tests captured
in later phases.

## Phase 1 — protocol identity and Agent mailbox

Introduce the protocol-level identity, status, and message types plus a
race-safe Agent mailbox, so children can communicate through attributed mail
without writing directly to a parent store.

### Tasks

| Task | Status |
|------|--------|
| Add agent path, ref, status, message, state DTOs and validation | Implemented |
| Add attributed event fields and deep-clone support | Implemented |
| Add `RoleAgent` or an explicit agent-message content type | Implemented |
| Add the ordered Agent mailbox | Implemented |
| Drain mailbox before provider calls and at turn finalization | Implemented |
| Add idle enqueue and running enqueue race-safe paths | Implemented |
| Render agent messages consistently in ChatGPT Responses and OpenCode Go Chat Completions | Implemented |
| Ensure compaction retains bounded final messages while excluding obsolete collaboration chatter as designed | Implemented |

### Acceptance criteria

- No external goroutine writes directly to an Agent's store.
- Race tests pass for Agent mailbox and existing lifecycle tests.
- Existing root-only provider payloads remain unchanged.

### Verification

- Path validation, resolution, and escape rejection.
- JSON compatibility and clone isolation.
- Mailbox FIFO under concurrent producers.
- Enqueue while idle, streaming, between serial tool calls, and finalizing.
- Wait-tool-result chain remains linear after child completion arrives.
- Provider payload snapshots for attributed messages.
- Compaction and resume do not duplicate mailbox messages.
- Close unblocks mailbox waiters.

## Phase 2 — ephemeral manager and child factory

Build the in-memory manager and child factory that construct independent child
Agents, filter their registries by role, and run them concurrently without
exposing model tools yet.

### Tasks

| Task | Status |
|------|--------|
| Implement manager maps, reservation/commit, state transitions, activity generation channel, limits, execution semaphore, and shutdown | Implemented |
| Implement `ChildFactory` in App using independent `MemoryStore`s | Implemented |
| Build role-filtered child registries with permission-gated shell access and explicit dual-opt-in file mutation | Implemented |
| Reuse provider/auth/model/context resources without recursively constructing App | Implemented |
| Implement none/all/N context fork projection | Implemented |
| Subscribe to child events and derive state/usage/activity | Implemented |
| Queue one bounded final result to the direct parent | Implemented |
| Add explicit App close/session-switch ordering | Implemented |
| Keep model tools unregistered initially; exercise manager through tests | Implemented |

### Acceptance criteria

- Manager lifecycle is correct without any model-generated control calls.
- No goroutine or event-bus leak remains after repeated create/close tests.
- Child provider concurrency is either verified or replaced by a provider
  factory.

### Verification

- Two children complete out of order with correct identity.
- Failed construction releases path and concurrency reservation.
- Duplicate path race yields exactly one winner.
- Execution slots never oversubscribe.
- Queued follow-up begins when a slot opens.
- Interrupt leaves the child reusable.
- Parent abort does not cancel committed child.
- App close cancels and joins all children.
- Provider error, tool panic, and child panic cannot crash parent/sibling.
- Root/child transcripts remain independent.
- Fork projection includes exactly the selected turns.
- Explorer child registries cannot access goal, write, exec, net, plugin, or
  MCP tools; shell-capable roles receive only the explicitly permitted `bash`
  capability and still use root authorization.
- `go test -race` reports no map/store/event races.

## Phase 3 — V2 model tools and headless integration

Register the seven manager-bound collaboration tools, wire delegation
permissions, and validate the full spawn-to-result loop with the fake
provider.

### Tasks

| Task | Status |
|------|--------|
| Register seven manager-bound tools on the root registry before router indexing | Implemented |
| Bind caller identity in the tool object, not model arguments | Implemented |
| Make tools direct/always loaded while the feature is enabled; defer only after tool routing has a tested way to keep the complete collaboration set discoverable together | Implemented |
| Add collaboration usage guidance to the root and child system prompts | Implemented |
| Ensure Plan mode policy is explicit. Recommended first behavior: allow read-only `explorer` spawns in Plan mode, but reject implementer/mutating roles | Implemented |
| Add max-depth checks even while recursion is disabled | Implemented |
| Add `RiskDelegate` and explicit descriptors: spawn/follow-up are delegation risk; message/wait/interrupt/list are read-risk controls. Do not let the unknown-tool fallback classify collaboration as OS execution | Implemented |
| Apply the ordinary permission gate to model-requested delegation while SDK host methods call the manager as explicit host authority. Child actions remain independently permissioned | Implemented |

### Acceptance criteria

- All seven schemas and edge errors match the documented contract.
- Spawn returns asynchronously.
- Wait never returns child content.
- Results reach parent context exactly once.
- Tool calling remains serial in the parent; only child turns are parallel.

### Verification

The end-to-end fake-provider scenario:

1. Root calls two `spawn_agent` tools in serial tool order.
2. Both child turns run concurrently.
3. Root does useful local/read work.
4. Root calls `wait_agent` once.
5. One or both final messages enter its mailbox.
6. Root receives attributed results on the next provider request.
7. Root calls `list_agents` and sees stable states.
8. Root follows up with one completed child.
9. Root interrupts another child.
10. The conversation remains valid with no dangling tool calls.

Provider integration tests are added as needed.

## Phase 4 — SDK and RPC surfaces

Expose subagent orchestration through the public SDK and RPC surfaces while
keeping events attributed and close semantics deadlock-free. SDK methods call
App/Manager; they do not implement orchestration.

### Tasks

| Task | Status |
|------|--------|
| Add SDK methods: `SpawnSubagent`, `SendSubagentMessage`, `FollowupSubagent`, `WaitSubagents`, `InterruptSubagent`, `Subagents`, `Subagent`, and `ReadySubagents` | Implemented |
| Add RPC commands: `subagent_spawn`, `subagent_send_message`, `subagent_followup`, `subagent_wait`, `subagent_interrupt`, `subagent_list`, `subagent_get`, and `subagent_ready` | Implemented |
| Add subagent capability/config summaries to `session_info` without exposing prompts, credentials, or full child transcripts | Implemented |

### Acceptance criteria

- Public API names and JSON shapes are stable.
- `ReadySubagents` follows the existing subscribe-before-ready goal pattern.
- No constructor starts restored work before subscriptions exist.

### Verification

- Concurrent RPC commands never interleave JSONL frames.
- Root abort and child interrupt affect only documented targets.
- RPC EOF cancels/joins according to server close semantics.
- Wait command is cancellable.
- SDK close during child stream/bash/wait does not deadlock.
- Callback reentrancy remains safe.
- Events include identity and clone isolation.
- Headless child user-input calls fail fast until a handler is configured.

## Phase 5 — TUI observation and interaction multiplexing

Add the TUI fleet inspector and an attributed FIFO queue so the terminal
observes and multiplexes child interaction without becoming the orchestrator.

### Tasks

| Task | Status |
|------|--------|
| Add `/agent` with status view when no argument and picker/detail behavior when agents exist | Implemented |
| Maintain `map[threadID]AgentViewState` plus stable first-seen order | Implemented |
| Show path, role, status, elapsed time, recent bounded activity, and usage | Implemented |
| Allow viewing a child's transcript without allowing direct composer input | Implemented |
| Add previous/next navigation only after picker behavior is stable | Implemented |
| Keep root transcript concise: spawn/status/final summary, not every child token | Implemented |
| Buffer/coalesce child deltas per thread and cap inactive-thread previews | Implemented |
| Add an attributed FIFO queue for permission and `ask_user` requests before enabling interactive child mutation | Implemented |
| Show agent identity on every permission/input modal | Implemented |
| When a child is inactive, retain pending interaction state and surface it in the root UI without switching transcripts unexpectedly | Implemented |

### Acceptance criteria

- TUI is purely a manager/event client.
- Root remains usable while children run.
- No child event can be misattributed to root.

### Verification

- Stable spawn order and navigation wraparound.
- Duplicate/out-of-order events do not revive terminal agents.
- Child `session_updated` does not rehydrate root transcript.
- Direct input disabled on parent-owned child views.
- Large child streams keep input responsive.
- Pending child approval remains visible while root is selected.
- FIFO permission/user-input queue routes each answer to the correct child.
- Session teardown clears all child UI state.

## Phase 6 — durable topology and lazy reload

Persist child topology and transcripts in separate databases and restore them
lazily, so durable children survive restart without duplication or root store
contention.

### Tasks

| Task | Status |
|------|--------|
| Bump `SessionVersion` and add `subagent_threads` migration | Implemented |
| Add memory/SQLite parity for `SubagentTaskStore` | Implemented |
| Use generation-checked atomic transitions | Implemented |
| Create private child DB directory and independent child stores | Implemented |
| Persist child locator, topology, status, bounded result, and usage | Implemented |
| Flush child store before terminal metadata commit | Implemented |
| Restore topology metadata without eagerly opening child DBs | Implemented |
| Lazily construct runtime on follow-up/message/inspection | Implemented |
| Add LRU unloading for idle terminal children with empty mailboxes | Implemented |
| Reconcile stale running records as interrupted on cold open | Implemented |
| Define cleanup for deleted root sessions and explicit child history removal | Implemented |
| Keep child sessions out of normal session index/listing | Implemented |

Branch semantics required by this phase:

| Task | Status |
|------|--------|
| Each task records the root branch ID where it originated | Implemented |
| Active tasks block branch switching in the first durable implementation | Implemented |
| Forking a root branch does not clone running children | Implemented |
| Terminal child references may be listed as historical metadata on the source branch only | Implemented |
| Clearing/compacting the root does not delete child transcripts | Implemented |
| Child compaction is independent | Implemented |

### Acceptance criteria

- Durable child identity survives restart.
- No active task is duplicated after restart.
- Root and child persistence are independently valid and recoverable.

### Verification

- Schema migration from current version.
- File and directory modes are private.
- Crash/reopen marks stale work interrupted.
- No task runs before `ReadySubagents`.
- Lazy reload preserves path/role/model/history.
- Concurrent handles cannot CAS the same generation twice.
- Child DB appends never mutate root branch tips.
- Branch switch/fork policy is deterministic.
- LRU never unloads running or mailbox-nonempty children.
- Cleanup never deletes an unrelated session path.

## Phase 7 — recursive agents, mutation, dynamic tools, and budgets

Enable recursion, file mutation, dynamic tools, and tree budgets under
explicit policy, keeping every child within the root's authority and limits.

### Tasks

| Task | Status |
|------|--------|
| Enable manager tools in child registries only when `recursive=true` and depth allows | Implemented |
| Share the same manager, tree ID, concurrency governor, total task count, and budget with all descendants | Implemented |
| Enable implementer mutation only through explicit config/role policy | Implemented |
| Add role-specific file ownership guidance and visible shared-workspace warning | Implemented |
| Add attributed permission queue and verify no authority broadening | Implemented |
| Opt plugin/MCP tools in only after concurrency and cancellation behavior is tested; otherwise serialize calls per extension owner | Implemented |
| Add optional tree token/cost/time budgets | Implemented |
| Consider optional external worktree isolation as a backend, not as a claim of sandboxing | Implemented |

### Acceptance criteria

- Recursive spawning cannot bypass any root-scoped limit.
- Mutating child authority is explicit and observable.
- Documentation clearly states shared-workspace residual risks.

### Verification

- Nested relative/canonical path resolution.
- Max-depth enforcement from every level.
- Global concurrency shared across siblings and descendants.
- Child cannot forge caller identity.
- Child cannot exceed root tool/permission policy.
- Overlapping writers receive explicit warnings and do not silently revert
  peers in integration scenarios.
- Extension calls are serialized or proven concurrent-safe.
- Budget exhaustion rejects new turns without corrupting running ones.

## Related documents

- [Subagents](subagents.md)
- [Sessions](sessions.md)
- [Plan Mode](plan-mode.md)
- [Configuration](configuration.md)
- [SDK](sdk.md)
