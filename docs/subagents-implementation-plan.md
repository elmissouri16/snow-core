# Codex-style subagents: research and implementation plan

## Status and research basis

**Implemented.** The architecture and public contracts in this plan landed in
Snow; verified user-facing behavior is documented in
[`docs/subagents.md`](subagents.md). This file remains the pinned research,
design rationale, phased checklist, and test matrix used for the implementation.

The upstream analysis is pinned to OpenAI Codex commit
[`3aae5d885bac39c1262491aa3fd100dfd8b3919f`](https://github.com/openai/codex/tree/3aae5d885bac39c1262491aa3fd100dfd8b3919f).
Pinning the commit matters because Codex currently contains two multi-agent
implementations and is actively changing the newer one.

The conclusions below come from the checked-out source and adjacent tests, not
from product descriptions:

- [`codex-rs/core/src/agent/control.rs`](https://github.com/openai/codex/blob/3aae5d885bac39c1262491aa3fd100dfd8b3919f/codex-rs/core/src/agent/control.rs)
- [`codex-rs/core/src/agent/control/spawn.rs`](https://github.com/openai/codex/blob/3aae5d885bac39c1262491aa3fd100dfd8b3919f/codex-rs/core/src/agent/control/spawn.rs)
- [`codex-rs/core/src/agent/registry.rs`](https://github.com/openai/codex/blob/3aae5d885bac39c1262491aa3fd100dfd8b3919f/codex-rs/core/src/agent/registry.rs)
- [`codex-rs/core/src/agent/role.rs`](https://github.com/openai/codex/blob/3aae5d885bac39c1262491aa3fd100dfd8b3919f/codex-rs/core/src/agent/role.rs)
- [`codex-rs/core/src/session/input_queue.rs`](https://github.com/openai/codex/blob/3aae5d885bac39c1262491aa3fd100dfd8b3919f/codex-rs/core/src/session/input_queue.rs)
- [`codex-rs/core/src/tools/handlers/multi_agents_spec.rs`](https://github.com/openai/codex/blob/3aae5d885bac39c1262491aa3fd100dfd8b3919f/codex-rs/core/src/tools/handlers/multi_agents_spec.rs)
- [`codex-rs/core/src/tools/handlers/multi_agents_v2/`](https://github.com/openai/codex/tree/3aae5d885bac39c1262491aa3fd100dfd8b3919f/codex-rs/core/src/tools/handlers/multi_agents_v2)
- [`codex-rs/core/src/tools/spec_plan.rs`](https://github.com/openai/codex/blob/3aae5d885bac39c1262491aa3fd100dfd8b3919f/codex-rs/core/src/tools/spec_plan.rs)
- [`codex-rs/agent-graph-store/`](https://github.com/openai/codex/tree/3aae5d885bac39c1262491aa3fd100dfd8b3919f/codex-rs/agent-graph-store)
- [`codex-rs/tui/src/app/agent_navigation.rs`](https://github.com/openai/codex/blob/3aae5d885bac39c1262491aa3fd100dfd8b3919f/codex-rs/tui/src/app/agent_navigation.rs)
- [`codex-rs/core/src/tools/handlers/multi_agents_tests.rs`](https://github.com/openai/codex/blob/3aae5d885bac39c1262491aa3fd100dfd8b3919f/codex-rs/core/src/tools/handlers/multi_agents_tests.rs)

## Executive recommendation

Implement the Codex V2 architecture directly. Do not first reproduce Codex V1
and then migrate it.

The Snow design should have these properties:

1. Every subagent is an independent `agent.Agent` using the existing provider →
   tools → persistence loop.
2. One root-scoped `internal/subagent.Manager` owns identity, topology, limits,
   lifecycle, mailboxes, child construction, and shutdown.
3. The model controls the manager through six V2-style tools:
   `spawn_agent`, `send_message`, `followup_task`, `wait_agent`,
   `interrupt_agent`, and `list_agents`.
4. Agents use canonical task paths such as `/root/api_review`; UUID-like thread
   IDs remain host correlation keys but are not the preferred model-facing
   identity.
5. Parent and child histories never share one mutable `session.Store` cursor.
   Ephemeral children initially use independent memory stores. Durable children
   later use separate SQLite databases plus a graph/metadata table in the root
   session.
6. Child messages enter an attributed mailbox and are incorporated only at safe
   agent-loop boundaries. A child completion must never append concurrently to
   a parent branch while that parent is chaining tool results.
7. Child authority is never broader than parent authority. The first release is
   read-only/noninteractive by default; mutating tools and recursive spawning are
   separately enabled only after multiplexed permissions and resource limits are
   proven.
8. The feature is disabled by default until the race suite, shutdown tests,
   public contracts, and shared-workspace warnings are complete.

This preserves Snow's main architectural promise: one understandable agent loop
behind every surface. The new manager orchestrates existing loops; it does not
create a second provider/tool implementation.

---

# 1. How Codex implements subagents

## 1.1 Two generations exist

At the pinned commit, both feature flags are marked stable:

| Generation | Feature | Default | Model tools |
|---|---|---:|---|
| V1 | `multi_agent` | on | `spawn_agent`, `send_input`, `resume_agent`, `wait_agent`, `close_agent` |
| V2 | `multi_agent_v2` | off | `spawn_agent`, `send_message`, `followup_task`, `wait_agent`, `interrupt_agent`, `list_agents` |

V1 remains the compatibility/default surface. V2 is the destination
architecture: canonical paths, mailboxes, selective context inheritance,
execution limits, lazy runtime residency, and persistent topology replace V1's
ID-only, explicitly close/resume-oriented API.

Snow has no compatibility burden, so implementing both would add migration code
without user value.

## 1.2 A subagent is a normal agent thread

Codex does not implement child reasoning inside a special tool handler. A child
is a normal `CodexThread` and `Session` created by the same `ThreadManager` as
the root. It receives its own:

- thread ID;
- transcript and context window;
- provider turns;
- tool execution;
- status watch;
- event stream;
- persisted rollout.

All descendants of one root share an `AgentControl`. That shared control has:

- a weak handle to the process-wide thread manager;
- one session/tree ID;
- an `AgentRegistry`;
- V2 execution and residency limiters;
- a shared rollout/token budget;
- spawn, message, interrupt, status, reload, and shutdown operations.

The weak manager reference avoids retaining the entire thread graph through a
cycle.

### Snow implication

`internal/agent.Agent` is already the right child runtime. It accepts provider,
registry, store, permission, host, model, context, and auth dependencies through
`agent.Options`. The manager should construct more `Agent` instances; it should
not make `Agent` itself multi-threaded.

## 1.3 Spawn is a reservation/commit transaction

Codex's `AgentRegistry` reserves capacity before starting asynchronous work.
`SpawnReservation` also reserves a nickname and canonical path. Dropping an
uncommitted reservation releases capacity and path ownership. Only after the
thread exists does spawn commit metadata.

The spawn sequence is effectively:

1. Resolve multi-agent generation and limits.
2. Reserve execution/residency capacity.
3. Reserve a unique task path and nickname.
4. Snapshot the parent turn's effective runtime policy.
5. Apply optional model/reasoning/service-tier overrides.
6. Apply the selected role as a config layer.
7. Reapply live approval, permission, sandbox, cwd, and environment policy so a
   role cannot accidentally weaken them.
8. Create a fresh or forked child thread.
9. Commit registry metadata and durable parent edge.
10. Send the initial task.
11. Emit thread/activity events.

### Snow implication

`Manager.Spawn` needs the same prepare/commit split. No child should become
visible through `list_agents`, consume a durable path, or leak a concurrency slot
if store creation, role validation, model validation, or `agent.New` fails.

## 1.4 Roles are config overlays, not subclasses

Codex uses `default`, `explorer`, and `worker`; Snow exposes the clearer
`general`, `explorer`, and `implementer` roles. A role can change
model, reasoning, service tier, developer instructions, and nickname candidates.
The role loader uses normal config layering and requirements enforcement.

Important rules:

- Parent model/provider/reasoning remain the default.
- A role changes only settings it explicitly owns.
- Live runtime permission/sandbox/cwd values are reapplied after role loading.
- Spawn tool descriptions include available role guidance.
- `explorer` is optimized for narrow codebase questions.
- `implementer` guidance requires explicit file/responsibility ownership and warns
  that peers may be editing the same workspace.

### Snow implication

Start with a smaller, explicit role model. A Snow role should contain only:

```go
type Role struct {
    Name          string
    Description   string
    System        string
    Provider      string
    Model         string
    Thinking      *protocol.ThinkingLevel
    Tools         []string
    AllowMutation bool
}
```

Role tool permissions are an intersection with parent/operator permissions,
never a union. Provider, auth, roots, project trust, and permission mode are not
role-overridable.

## 1.5 Snow identity uses canonical agent paths

Snow's public spawn request separates `name`, `task`, and `role`. If
`/root/task1` spawns a child named `task_3`, the child is
`/root/task1/task_3`. The parent can use `task_3` or the canonical path; agents in
other branches use the canonical path.

Codex keeps both path and thread ID:

- path is stable and model-friendly;
- thread ID is durable host identity;
- nickname is optional presentation metadata;
- role is separate metadata.

The registry rejects duplicate/reserved paths atomically.

### Snow implication

Add a dependency-free `protocol.AgentPath` validation helper with these rules:

- root is exactly `/root`;
- each segment matches `^[a-z][a-z0-9_]{0,63}$`;
- no `.`, `..`, empty, slash-containing, or `root` child segment;
- canonical length is bounded, for example 512 bytes;
- relative resolution never escapes `/root`;
- one live/durable identity owns one canonical path.

Do not use the existing branch ID as an agent identity.

## 1.6 Context inheritance is independent from runtime policy inheritance

Codex always inherits live runtime configuration such as cwd, approval policy,
permission profile, model defaults, and environment. Conversation history is a
separate choice:

- `fork_turns="none"`: fresh child context;
- `fork_turns="all"`: full history, the V2 default;
- positive integer string: only the latest N turns.

Forking is not a raw transcript copy. Codex flushes the parent first, then
filters the rollout:

- keeps system/developer/user content and assistant final answers;
- removes reasoning, tool execution artifacts, old collaboration messages, and
  stale usage hints;
- handles compacted replacement history;
- replaces parent-only developer guidance with child guidance;
- retains reference-context state only when safe.

### Snow implication

Create `internal/subagent/context.go` with a tested `ForkContext` function. It
must build a new independent store and must not reuse a parent's active store
handle.

Proposed Snow behavior:

- `none`: new empty child store, then append the initial task.
- `all`: start from `ContextMessages()` rather than complete pre-compaction
  history, sanitize it, then append the task.
- `N`: group messages by user turn, keep the last N groups, sanitize, repair
  parent IDs, then append the task.

Sanitization rules:

1. Remove thinking blocks.
2. Remove incomplete plan blocks.
3. Remove prior subagent activity/status events and mailbox envelopes.
4. Keep tool calls only when their complete ordered result chain is also kept;
   otherwise remove both call and result.
5. Preserve assistant final text and completed plans.
6. Give copied messages new IDs or maintain a verified old→new ID map; never
   retain dangling parent IDs.
7. Do not copy branch goals into children.
8. Rebuild the child's system prompt from current trusted context, role, skills,
   and collaboration guidance rather than copying a stale system message.

Default `fork_turns="all"` matches Codex V2. Tool guidance should still advise
`none` for narrow explorer work when the task is self-contained.

## 1.7 V2 messaging is a mailbox protocol

Codex separates two operations:

- `send_message`: queue-only, no new turn;
- `followup_task`: queues input and triggers a turn when the target is idle.

Messages have structured attribution:

```text
Message Type: NEW_TASK | MESSAGE | FINAL_ANSWER
Task name: /root/...
Sender: /root/...
Payload:
...
```

A completed child sends a non-triggering `FINAL_ANSWER` to its direct parent.
The parent receives the result through normal model context, not as an
out-of-band untrusted string returned by `wait_agent`.

The mailbox distinguishes ordinary mail from steered user input and preserves
ordering. Queue-only mail can wait for the next safe model boundary rather than
forcing another provider request.

### Snow implication

The existing `Agent` needs a small general mailbox primitive before subagents
are added. Direct concurrent `Store.Append` from a child completion is unsafe:
`executeToolCalls` captures a parent tip and then serially chains tool results.
An external append in the middle can fork or hide one chain.

Add to `internal/agent`:

```go
type MailboxMessage struct {
    ID        string
    Author    protocol.AgentPath
    Recipient protocol.AgentPath
    Kind      protocol.AgentMessageKind
    Content   string
    CreatedAt int64
}

func (a *Agent) EnqueueMailbox(MailboxMessage) error
func (a *Agent) MailboxActivity() <-chan struct{}
```

Safe delivery rules:

- Producers enqueue under a mailbox mutex and never touch the store.
- `run` drains mail immediately before every provider request.
- After a blocking `wait_agent` returns, the next provider iteration drains it.
- Turn finalization flushes any queued messages into the session so they survive
  until the next user/follow-up turn.
- When idle, enqueue acquires admission and persists at the current tip.
- Mailbox persistence is one atomic ordered batch.
- Mail is represented by a new `RoleAgent` or explicit agent-message block, not
  an untyped root user prompt.
- Both provider adapters must render the role consistently. If the ChatGPT
  backend accepts Codex `agent_message` input, use it; otherwise use a sealed,
  attributed compatibility message and test both adapters.

## 1.8 Wait is an activity barrier in V2

V1 waits on explicit target IDs and returns terminal result content. V2
`wait_agent` waits for any mailbox activity or steered user input. It returns a
small summary and timeout flag; the actual content arrives through the mailbox.

Codex defaults:

- minimum wait: 10 seconds;
- default wait: 30 seconds;
- maximum wait: 1 hour.

This avoids result duplication and discourages tight polling.

### Snow implication

Use the same V2 contract. The wait tool should subscribe to a manager generation
channel, check already-pending activity before blocking, and return on:

- any newly queued agent message;
- any child terminal notification;
- parent context cancellation/abort;
- timeout.

It must not consume messages and must not return private child content. Snow preserves this as the default `until=activity` mode and adds a bounded `until=all` orchestration join. The latter waits for every descendant (excluding a recursive caller's own turn) to become terminal and returns aggregate counts only; attributed mailbox messages remain the sole result-content path.

## 1.9 Limits distinguish execution from residency

Codex V1 limits total open agents and nesting depth. A completed agent consumes a
slot until closed.

Codex V2 defaults to four concurrent threads including root. It separately:

- limits active child turns with an execution guard;
- limits loaded child runtimes;
- evicts the least-recently-used child only when it is terminal, idle, and has
  no pending mailbox;
- reloads evicted children from persisted history when messaged again.

This is why V2 has no model-facing close/resume tools.

### Snow implication

Implement limits in layers:

1. `MaxConcurrentThreads` retains its compatibility name but defaults to 4
   concurrent child turns; the root does not consume a slot. TUI `/settings`,
   `/agent concurrency N`, config, CLI, and SDK expose the limit.
2. `MaxAgentsPerSession` default 32, bounding persistent identity growth.
3. `MaxDepth` default 1 for the first release, configurable only after recursive
   spawning is enabled.
4. `TaskTimeout` default 30 minutes, with a hard configured maximum.
5. `MaxResultBytes` default 64 KiB for parent-visible completion text.
6. `MaxBufferedEventsPerAgent` and bounded preview storage.
7. Optional total token/cost budget across the tree.

Concurrency capacity is acquired when a child turn begins, not merely when an
identity exists. Completed children remain reusable. Add LRU unloading only in
the durability phase; do not add a public `resume_agent` tool.

## 1.10 Status is event-derived

Codex uses:

- `pending_init`;
- `running`;
- `interrupted`;
- `completed(final message)`;
- `errored(error)`;
- `shutdown`;
- `not_found`.

V2 can transition a completed/interrupted child back to running through a
follow-up task. Interrupt is not close.

### Snow implication

Use a validated transition function rather than scattered assignments:

```text
pending_init -> queued | running | errored | shutdown
queued       -> running | interrupted | shutdown
running      -> completed | interrupted | errored | shutdown
completed    -> queued | running | shutdown | not_loaded
interrupted  -> queued | running | shutdown | not_loaded
errored      -> queued | running | shutdown | not_loaded
not_loaded   -> queued | running | shutdown
shutdown     -> (terminal)
```

`not_found` is a lookup result, not persisted state.

## 1.11 Persistence separates transcript and topology

Codex persists each child as a normal thread history. A separate
`AgentGraphStore` records directional parent→child edges with Open/Closed state.
V2 restores topology metadata without eagerly loading every child runtime.

### Snow implication

Do not overload `session_branches` as child threads. Current Snow branches have
one active marker and every `SQLiteStore` handle owns a mutable branch/tip
cursor. Concurrent branch handles would fight over active state and
`session_meta.branch_tip`.

Use:

- one independent child session DB per durable child;
- one root-session `subagent_threads` table for topology/status/locator metadata;
- optional `SubagentStore` interfaces implemented by memory and SQLite stores.

Suggested root metadata table:

```sql
CREATE TABLE subagent_threads (
    thread_id TEXT PRIMARY KEY,
    parent_thread_id TEXT NOT NULL,
    parent_branch_id TEXT NOT NULL,
    agent_path TEXT NOT NULL UNIQUE,
    role TEXT NOT NULL,
    status TEXT NOT NULL,
    child_session_path TEXT NOT NULL,
    model_provider TEXT NOT NULL,
    model_id TEXT NOT NULL,
    thinking TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    started_at INTEGER,
    finished_at INTEGER,
    last_activity_at INTEGER NOT NULL,
    result TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    usage_json BLOB,
    generation INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX subagent_threads_parent_idx
    ON subagent_threads(parent_thread_id, created_at);
CREATE INDEX subagent_threads_branch_idx
    ON subagent_threads(parent_branch_id, created_at);
```

Child database location:

```text
<root-session>.agents/<thread-id>.db
```

All directories/files use the same private storage policy as sessions. Child DBs
must not appear in the normal session picker.

On cold resume:

- restore identity and terminal metadata only;
- convert stale `running`/`queued` records to `interrupted` unless an explicit
  durable resume policy says otherwise;
- load a child runtime only for follow-up, inspection, or explicit resume;
- require `ReadySubagents()` after surfaces subscribe before starting anything;
- never silently duplicate a running task after process restart.

## 1.12 Permissions and sandbox policy remain inherited

Codex gives children the normal tool surface and normal approval/sandbox flow.
It adds no subagent sandbox. All agents share cwd and filesystem.

### Snow implication

Snow must state the same limitation more prominently:

- subagents run with the user's OS privileges;
- root confinement is not a sandbox;
- project trust is not a sandbox;
- parallel edits can conflict;
- bash/process side effects are shared;
- API/model calls incur independent usage;
- child output and repository content remain prompt-injection inputs.

Current authority policy:

- the feature remains disabled by default;
- `general` children receive `read`, `grep`, `glob`, permitted
  read-only skill/resource tools, and role-selected `bash`;
- `explorer` children remain read-only and do not receive `bash`;
- `implementer` children are shell-capable, while `write`/`edit` require both
  `subagents.allow_mutation=true` and role `allow_mutation=true`;
- network, plugin, MCP, goal, and user-input tools remain excluded from child
  registries;
- child permissions are an intersection of root policy, role allowlist, and
  operator config, with the parent tool allowlist as an upper bound;
- every child tool call still invokes `permission.Authorize`, and shell,
  mutation, and recursive delegation calls use the root permission service;
- the TUI's attributed FIFO broker serializes concurrent root/child approval
  requests; headless ask mode remains deny-by-default.

Bash remains an OS-privileged, shared-workspace operation rather than a
sandbox. Durable children carry a role-policy fingerprint; a policy revision or
trusted role change fails safe during lazy restore instead of granting new
capabilities.

Add `permission.RiskDelegate` rather than misclassifying spawn as filesystem
read or OS execution:

- `spawn_agent` and `followup_task` use `RiskDelegate` because they start paid
  model work;
- `send_message`, `wait_agent`, `interrupt_agent`, and `list_agents` use
  `RiskRead` because they only control already-authorized tree state;
- ask mode prompts for delegation and can remember a session-scoped decision;
- deny mode rejects model-requested spawn/follow-up even when schemas are
  present;
- allow mode permits delegation, while every child tool call remains separately
  authorized;
- explicit SDK host methods are host-authorized APIs and call the manager
  directly rather than pretending to be model tool calls.

Teach exposure policy about `RiskDelegate` so a deny-mode model is not encouraged
to call a capability guaranteed to fail. This must be narrowly implemented;
do not silently change current direct write/exec schema exposure in the same
feature.

## 1.13 TUI is an observer, not the orchestrator

Codex's TUI consumes app-server thread events. It provides:

- `/agent` picker/status;
- stable first-seen spawn order;
- next/previous keyboard navigation;
- canonical path/role/nickname labels;
- bounded recent-activity previews;
- per-child transcript viewing;
- inspection of completed/closed children;
- direct input disabled for parent-owned subagent threads;
- child permission/input requests surfaced with attribution.

### Snow implication

Keep the manager in core/app. TUI state should be a map keyed by thread ID and
updated only in Bubble Tea's `Update`. Do not let child goroutines mutate UI
state or reuse the root's scalar `assistantBuf`, `thinkingBuf`, `busy`, or
permission fields.

## 1.14 Hooks and telemetry

Codex emits subagent start/stop hooks, annotates ordinary hook payloads with
subagent identity, emits structured collaboration activities, records spawn
telemetry by version/role, and can enforce a shared rollout budget.

### Snow implication

Snow has plugins but not Codex's hook subsystem. Do not add a hook framework as
part of this feature. Instead:

- forward structured subagent lifecycle events to existing plugin observers;
- include child usage in normal events;
- add `Session.SubagentUsage()` and optional tree aggregate usage;
- never log task/message contents by default;
- keep bounded previews separate from full child transcripts.

---

# 2. Snow gap analysis

| Area | Existing Snow seam | Missing work |
|---|---|---|
| Agent runtime | `agent.New(Options)` constructs a reusable loop | Child factory and tree-scoped manager |
| Turn admission | One `Agent` safely allows one active turn | One independent `Agent` per child |
| Tool control | Thread-safe registry and model tools | Six manager-bound collaboration tools |
| Session history | Memory/SQLite stores, context projection, forks | Independent child stores and topology metadata |
| Messaging | User prompts and tool results only | Attributed mailbox safe points |
| Events | Ordered cloned cross-surface bus | Child identity, status, activity, filtered forwarding |
| Permissions | Thread-safe policy service | Attributed FIFO ask/user-input multiplexing |
| Limits | Per-agent turns/calls and goal budgets | Tree concurrency/depth/count/time/output/usage governor |
| SDK | Thin wrapper over App/Agent | List/get/spawn/message/follow-up/wait/interrupt APIs |
| RPC | Async root prompt and locked JSONL writer | Multi-child commands and event correlation |
| TUI | Lossless mailbox and one root transcript | Agent map, picker, status feed, per-child buffer/view |
| Persistence | Schema v4 branch tree | Subagent metadata/CAS, child DB lifecycle, recovery |
| Shutdown | App closes one Agent then resources | Cancel/join manager and children without event loss |

## Hard constraints from current code

1. **Never run two turns on one `Agent`.** Its `running`, pending tool calls,
   usage, cancel function, mode, and session fields are singular.
2. **Never share one mutable Store handle.** Appends default to that handle's
   current tip.
3. **Never treat existing user branches as concurrent worker lanes.** Branch
   selection updates database-global active metadata.
4. **Never merge anonymous child deltas into root events.** Every forwarded event
   needs child identity.
5. **Never share today's TUI asker across children without a queue.** It rejects a
   second pending request.
6. **Never build child Apps recursively.** That would reconnect plugins/MCP,
   reload trust/config/auth, create unrelated top-level sessions, and duplicate
   global resources.
7. **Never let roles broaden authority.** Role configuration is subordinate to
   parent/operator policy.

---

# 3. Proposed Snow architecture

## 3.1 Package boundary

Add `internal/subagent`:

```text
internal/subagent/
├── manager.go          # registry, topology, state transitions, limits
├── runtime.go          # one child Agent/store and its worker lifecycle
├── factory.go          # narrow child construction interface
├── roles.go            # built-in/configured role resolution
├── context.go          # none/all/N history projection
├── mailbox.go          # message envelopes and activity notifications
├── tools.go            # six model-facing tools
├── persistence.go      # optional session store adapter
├── manager_test.go
├── lifecycle_test.go
├── context_test.go
└── tools_test.go
```

Dependency direction:

```text
cmd/snow -> app -> {agent, subagent, tui, rpc}
subagent -> {agent, provider interfaces, tools, session, permission, protocol}
agent -> {provider, tools, session, permission, protocol}
```

`agent` must not import `subagent`. Collaboration enters `agent` through ordinary
registered tools and the generic mailbox API.

## 3.2 Core types

Add `pkg/protocol/subagent.go` with standard-library-only DTOs:

```go
type AgentStatus string

const (
    AgentPendingInit AgentStatus = "pending_init"
    AgentQueued      AgentStatus = "queued"
    AgentRunning     AgentStatus = "running"
    AgentInterrupted AgentStatus = "interrupted"
    AgentCompleted   AgentStatus = "completed"
    AgentErrored     AgentStatus = "errored"
    AgentShutdown    AgentStatus = "shutdown"
    AgentNotLoaded   AgentStatus = "not_loaded"
    AgentNotFound    AgentStatus = "not_found"
)

type AgentRef struct {
    ThreadID       string
    ParentThreadID string
    Path           AgentPath
    ParentPath     AgentPath
    Role           string
    Nickname       string
    Depth          int
}

type SubagentState struct {
    Agent      AgentRef
    Status     AgentStatus
    Model      string
    Provider   string
    Thinking   ThinkingLevel
    CreatedAt  int64
    StartedAt  int64
    FinishedAt int64
    Result     string
    Error      string
    Usage      *Usage
}

type AgentMessageKind string
const (
    AgentMessageNewTask AgentMessageKind = "new_task"
    AgentMessageNormal  AgentMessageKind = "message"
    AgentMessageFinal   AgentMessageKind = "final_answer"
)

type AgentMessage struct {
    ID         string
    Author     AgentPath
    Recipient  AgentPath
    Kind       AgentMessageKind
    Content    string
    TriggerTurn bool
    CreatedAt  int64
}
```

Use explicit JSON tags and `Validate`/`Clone` methods. Bound all strings during
construction, not only during rendering.

## 3.3 Manager contract

Proposed internal API:

```go
type Manager interface {
    Spawn(context.Context, Caller, SpawnRequest) (protocol.SubagentState, error)
    SendMessage(context.Context, Caller, target string, message string) error
    Followup(context.Context, Caller, target string, message string) error
    Wait(context.Context, Caller, time.Duration) (WaitResult, error)
    Interrupt(context.Context, Caller, target string) (protocol.AgentStatus, error)
    List(context.Context, Caller, string) ([]protocol.SubagentState, error)
    Get(context.Context, string) (protocol.SubagentState, error)
    Ready(context.Context) error
    Close(context.Context) error
}
```

`Caller` is bound when tools are registered for one agent. Do not trust a
model-supplied sender path.

Internal `manager` fields:

```go
type manager struct {
    ctx        context.Context
    cancel     context.CancelFunc
    mu         sync.RWMutex
    byID       map[string]*runtime
    byPath     map[protocol.AgentPath]*runtime
    factory    ChildFactory
    store      TaskStore
    limits     Limits
    slots      chan struct{}
    activity   chan struct{} // edge-triggered generation wake-up
    generation uint64
    wg         sync.WaitGroup
    ready      bool
    closed     bool
    root       *agent.Agent
    publish    func(protocol.AgentEvent)
}
```

Use immutable copies for public/list results. Never return internal runtime
pointers.

The manager treats `/root` as a registered endpoint even though it is not a
child `runtime`. Target resolution, mailbox delivery, `list_agents`, and
child-to-parent messages special-case that endpoint through the bound root
`Agent`; execution slots and child-count limits do not count it twice.

Wait subscriptions capture the current `generation` under the manager lock and
block until the generation changes. A model-tool wait additionally checks the
caller's pending Agent mailbox before blocking. Programmatic SDK/RPC waits keep
a per-call cursor so an old unread activity signal cannot cause an unbounded
immediate-return loop.

## 3.4 Child factory

`internal/subagent` should not know how App assembled auth, providers, dynamic
tools, skills, or MCP. Define:

```go
type ChildFactory interface {
    NewChild(context.Context, ChildSpec) (ChildRuntime, error)
}

type ChildRuntime interface {
    Prompt(context.Context, string) error
    EnqueueMailbox(protocol.AgentMessage) error
    AbortContext(context.Context) error
    IsRunning() bool
    Messages() ([]protocol.Message, error)
    Usage() (protocol.Usage, error)
    Subscribe(func(protocol.AgentEvent)) func()
    Close()
}
```

`internal/app` supplies the factory using already-resolved runtime resources.
The factory creates:

- a fresh independent child store;
- a filtered child registry;
- manager tools bound to the child's caller identity when recursion is allowed;
- a child-specific tool host using the same cwd/roots;
- inherited/narrowed permission policy;
- inherited provider/model/auth defaults;
- child system prompt and role instructions;
- no branch goal controller;
- no second plugin/MCP manager.

## 3.5 Registry policy

Add a safe registry snapshot helper rather than sharing and mutating the root
registry:

```go
func CloneRegistry(src Registry, allow func(ToolDescriptor) bool) (*SimpleRegistry, error)
```

Current allow policy:

- include built-in read/search tools;
- include skill/resource reads only when already trusted and read-only;
- include `bash` for the shell-capable `general` and `implementer` roles;
- keep `explorer` read-only without `bash`;
- exclude root goal tools;
- exclude `ask_user`/`request_user_input`;
- include `write`/`edit` only when both global and role mutation switches are
  enabled;
- exclude `webfetch`, plugin, and MCP tools;
- register collaboration tools only when `Depth < MaxDepth` and recursion is
  enabled;
- preserve deferred metadata and re-create a per-child router when needed.

Every shell or mutation call still goes through the root permission service;
role filtering is capability selection, not a sandbox.

Later policy can opt dynamic tools in after their concurrency contract is clear.
A shared external plugin process may need per-owner serialization even when the
registry map itself is thread-safe.

## 3.6 Event model

Extend `protocol.AgentEvent` with optional correlation:

```go
Agent       *protocol.AgentRef      `json:"agent,omitempty"`
Subagent    *protocol.SubagentState `json:"subagent,omitempty"`
AgentMessage *protocol.AgentMessage `json:"agent_message,omitempty"`
```

Add event types:

```text
subagent_started
subagent_status
subagent_message
subagent_activity
```

Rules:

- Root events keep `Agent=nil` for backward compatibility.
- Child lifecycle events always include `Agent`.
- Child text/thinking deltas are not copied into the root transcript by default.
- Child tool start/progress/end may be forwarded as attributed events for SDK/RPC
  and buffered per child by TUI; root TUI renders only compact activity.
- `session_updated` from a child must not make the root TUI hydrate the root
  transcript.
- Terminal status is emitted exactly once per child turn.
- Completion message delivery and status publication have deterministic order:
  persist child result → queue parent mailbox → publish message activity →
  publish terminal status.
- `AgentEvent.Clone` deep-copies every new nested field.
- Plugins observe the same normalized, attributed events.

## 3.7 Cancellation and shutdown semantics

Choose explicit semantics rather than inheriting a tool call's context by
accident:

- Spawn preparation uses the calling tool context.
- After spawn commit, the child runs under the manager/App root context.
- Parent turn completion does not cancel children.
- Parent turn abort does not automatically cancel already committed children.
- `interrupt_agent` cancels only the target's current turn.
- App/session close cancels the full tree and joins all child goroutines.
- Switching the root session rejects while children are active. Once every
  child is terminal, Snow joins/detaches the old in-memory runtimes before
  binding the new session; durable topology remains attached to its original
  root database.
- Root branch switching is rejected while nonterminal children belong to the
  current branch. Terminal child history remains attached to its originating
  branch.

Close ordering:

1. Mark manager closed; reject new work.
2. Cancel all child contexts.
3. Cancel root agent if it is blocked in `wait_agent`.
4. Join child workers and flush terminal/interrupted state.
5. Stop event forwarding.
6. Close child agents/stores.
7. Close root agent event bus.
8. Close MCP/plugins/router/root session.

The implementation may need a two-phase `Agent.StopAccepting`/`Agent.Close`
seam so the parent bus remains alive until child terminal events are drained.

## 3.8 App assembly order

The manager, root tools, root Agent, and child factory refer to one another, so
App must wire them in an explicit order rather than hiding a partially usable
cycle:

1. Resolve config, trust, auth, providers, models, root session, permissions,
   built-ins, skills, plugins, and MCP exactly as today.
2. Construct an unbound manager shell that rejects operations until `Bind`.
3. Register root collaboration tool instances bound to `/root` and the manager.
4. Finish deferred-router indexing after collaboration tools are registered.
5. Construct the root `Agent`.
6. Construct the child factory closure from immutable/common App resources; the
   closure creates a child registry and collaboration tools bound to the new
   child path.
7. Atomically `Bind(rootAgent, factory, publisher, taskStore)` on the manager.
8. Subscribe plugin/surface observers and emit initial root state.
9. Call `ReadySubagents` only from the surface-readiness path.

A prompt cannot reach collaboration tools before step 7. `Bind` is single-use,
and every manager method returns a stable `ErrNotReady` before it. Failed App
construction closes the unbound manager without launching work.

## 3.9 Usage and budgets

Each child already receives provider usage through its own Agent. The manager
should:

- store per-turn and cumulative child usage;
- publish attributed `usage` events;
- maintain an optional tree total under one mutex/CAS store;
- reject new child turns before they start when the configured tree budget is
  exhausted;
- never stop already-running work merely because a future-launch budget was
  crossed unless an explicit hard-cancel policy is configured;
- expose root-only, child-only, and tree totals distinctly.

Public SDK methods:

```go
Session.Subagents() []protocol.SubagentState
Session.Subagent(idOrPath string) (protocol.SubagentState, error)
Session.SubagentUsage() (protocol.Usage, error)
```

Do not silently change existing `Session.Usage()` from root-branch usage to tree
usage.

---

# 4. Model-visible tool contracts

Snow currently exposes flat function names, so use the V2 names directly rather
than adding provider-specific namespaces.

## 4.1 `spawn_agent`

Input:

```json
{
  "type": "object",
  "properties": {
    "name": {
      "type": "string",
      "description": "Lowercase identity name for the new child."
    },
    "task": {
      "type": "string",
      "description": "Concrete, bounded initial task."
    },
    "role": {
      "type": "string",
      "description": "Optional configured capability role."
    },
    "fork_turns": {
      "type": "string",
      "description": "none, all, or a positive integer string; defaults to all."
    },
    "model": {
      "type": "string",
      "description": "Optional model override permitted by policy."
    },
    "reasoning_effort": {
      "type": "string",
      "enum": ["off", "minimal", "low", "medium", "high"]
    }
  },
  "required": ["name", "task"],
  "additionalProperties": false
}
```

Output:

```json
{"name":"/root/api_review","status":"running"}
```

Rules:

- validate nonempty/bounded task before reserving;
- reject unknown fields;
- resolve path relative to caller;
- reject duplicate paths;
- validate role/model/effort before commit;
- default to parent model/effort;
- enforce count/depth/concurrency policy;
- return after the child is registered and its initial task is accepted, not
  after task completion;
- never return secrets, raw child context, or thread internals.

## 4.2 `send_message`

Input:

```json
{"target":"api_review","message":"Check the error path too."}
```

Rules:

- resolve relative/canonical path or thread ID;
- allow child→parent and sibling messaging within the same root tree;
- queue without starting a turn;
- reject blank/oversized content;
- ensure an unloaded durable target can accept mail after lazy load, or persist
  the mailbox until load;
- return an empty success object or a submission ID, never message content.

## 4.3 `followup_task`

Input:

```json
{"target":"api_review","message":"Now verify the focused tests."}
```

Rules:

- root cannot be targeted by `followup_task`;
- queue a `NEW_TASK` and start/reuse the target when idle;
- when running, preserve FIFO order and deliver at the next safe boundary;
- acquire an execution slot before starting a new provider turn;
- completed/interrupted/errored/not-loaded children are reusable;
- one runtime processes turns serially.

## 4.4 `wait_agent`

Input:

```json
{"timeout_ms":30000}
```

Output:

```json
{"message":"Wait completed.","timed_out":false}
```

Rules:

- optional timeout only;
- clamp below configured minimum and mention the clamp;
- reject above configured maximum;
- wake on already queued activity;
- wake on any new child message/terminal event;
- wake/cancel on parent abort;
- never return completion content.

## 4.5 `interrupt_agent`

Input:

```json
{"target":"api_review"}
```

Output:

```json
{"previous_status":"running"}
```

Rules:

- reject root and self;
- interrupt current turn if any;
- keep identity/history/mailbox reusable;
- return success for a race where the target just became terminal;
- preserve deterministic previous-status reporting.

## 4.6 `list_agents`

Input:

```json
{"path_prefix":"/root/reviews"}
```

Output:

```json
{
  "agents": [
    {"agent_name":"/root","agent_status":"running"},
    {"agent_name":"/root/reviews/api","agent_status":"completed"}
  ]
}
```

Rules:

- optional relative/canonical prefix;
- stable first-seen/spawn order;
- include root;
- show live and durable not-loaded identities;
- no prompts, full results, credentials, or private paths;
- cap returned entries and indicate truncation.

---

# 5. Configuration

Add a single config object instead of separate V1/V2 flags:

```go
type SubagentConfig struct {
    Enabled                 bool                  `json:"enabled,omitempty"`
    Recursive               bool                  `json:"recursive,omitempty"`
    MaxConcurrentThreads    int                   `json:"max_concurrent_threads,omitempty"`
    MaxAgentsPerSession     int                   `json:"max_agents_per_session,omitempty"`
    MaxDepth                int                   `json:"max_depth,omitempty"`
    MinWaitTimeoutMS        int                   `json:"min_wait_timeout_ms,omitempty"`
    DefaultWaitTimeoutMS    int                   `json:"default_wait_timeout_ms,omitempty"`
    MaxWaitTimeoutMS        int                   `json:"max_wait_timeout_ms,omitempty"`
    TaskTimeoutMS           int                   `json:"task_timeout_ms,omitempty"`
    MaxResultBytes          int                   `json:"max_result_bytes,omitempty"`
    Durable                 bool                  `json:"durable,omitempty"`
    AllowMutation           bool                  `json:"allow_mutation,omitempty"`
    ExposeChildToolEvents   bool                  `json:"expose_child_tool_events,omitempty"`
    DefaultProvider         string                `json:"default_provider,omitempty"`
    DefaultModel            string                `json:"default_model,omitempty"`
    DefaultRole             string                `json:"default_role,omitempty"`
    Roles                   map[string]AgentRole  `json:"roles,omitempty"`
}
```

Defaults:

```text
enabled=false
recursive=false
max_concurrent_threads=4  # concurrent child agents; root does not consume a slot
max_agents_per_session=32
max_depth=1
min_wait_timeout_ms=10000
default_wait_timeout_ms=30000
max_wait_timeout_ms=3600000
task_timeout_ms=1800000
max_result_bytes=65536
durable=true
allow_mutation=false
expose_child_tool_events=true
default_model=
default_role=general
```

Validation must reject:

- max concurrency below 1;
- max agent count below available child concurrency;
- depth below 1 or above a hard cap;
- negative or inverted timeout ranges;
- task timeout above a hard cap;
- result cap outside safe bounds;
- invalid/duplicate role names;
- role tool/model/effort values that cannot be resolved;
- a role that attempts to broaden global authority.

CLI/SDK additions can follow after core stabilization:

```text
--subagents
--no-subagents
--subagent-max-concurrency N
--subagent-max-depth N
```

Do not make `--subagents` imply mutation or recursive spawning.

---

# 6. Implementation phases

## Phase 0 — lock public semantics and baseline

### Work

- Add this plan to the repository and update the explicit multi-agent non-goal
  only when implementation begins.
- Capture baseline focused/race tests before changing runtime code.
- Decide the feature name (`subagents`) once and use it consistently in config,
  package names, docs, events, RPC, and SDK.
- Add architecture decision records in code comments for cancellation, mailbox
  ordering, independent stores, and permission intersection.

### Exit criteria

- Tool schemas and status transitions are approved.
- Parent abort versus child survival is documented.
- First-release read-only policy is documented.
- No unresolved choice can force a public DTO rename later.

## Phase 1 — protocol identity and Agent mailbox

### Files

- `pkg/protocol/subagent.go` (new)
- `pkg/protocol/subagent_test.go` (new)
- `pkg/protocol/events.go`
- `pkg/protocol/message.go`
- `internal/agent/agent.go`
- `internal/agent/mailbox_test.go` (new)
- both provider adapters and focused tests

### Work

- Add agent path, ref, status, message, state DTOs and validation.
- Add attributed event fields and deep-clone support.
- Add `RoleAgent` or an explicit agent-message content type.
- Add the ordered Agent mailbox.
- Drain mailbox before provider calls and at turn finalization.
- Add idle enqueue and running enqueue race-safe paths.
- Render agent messages consistently in ChatGPT Responses and OpenCode Go Chat
  Completions.
- Ensure compaction retains bounded final messages while excluding obsolete
  collaboration chatter as designed.

### Tests

- path validation/resolution/escape rejection;
- JSON compatibility and clone isolation;
- mailbox FIFO under concurrent producers;
- enqueue while idle, streaming, between serial tool calls, and finalizing;
- wait-tool-result chain remains linear after child completion arrives;
- provider payload snapshots for attributed messages;
- compaction and resume do not duplicate mailbox messages;
- close unblocks mailbox waiters.

### Exit criteria

- No external goroutine writes directly to an Agent's store.
- Race tests pass for Agent mailbox and existing lifecycle tests.
- Existing root-only provider payloads remain unchanged.

## Phase 2 — ephemeral manager and child factory

### Files

- `internal/subagent/*` (new)
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/tools/tools.go` for registry clone/filter helper
- `internal/app/app.go`
- `internal/app/subagent_test.go` (new)

### Work

- Implement manager maps, reservation/commit, state transitions, activity
  generation channel, limits, execution semaphore, and shutdown.
- Implement `ChildFactory` in App using independent `MemoryStore`s.
- Build role-filtered child registries with permission-gated shell access and
  explicit dual-opt-in file mutation.
- Reuse provider/auth/model/context resources without recursively constructing
  App.
- Implement none/all/N context fork projection.
- Subscribe to child events and derive state/usage/activity.
- Queue one bounded final result to the direct parent.
- Add explicit App close/session-switch ordering.
- Keep model tools unregistered initially; exercise manager through tests.

### Tests

- two children complete out of order with correct identity;
- failed construction releases path and concurrency reservation;
- duplicate path race yields exactly one winner;
- execution slots never oversubscribe;
- queued follow-up begins when a slot opens;
- interrupt leaves the child reusable;
- parent abort does not cancel committed child;
- App close cancels and joins all children;
- provider error, tool panic, and child panic cannot crash parent/sibling;
- root/child transcripts remain independent;
- fork projection includes exactly the selected turns;
- explorer child registries cannot access goal, write, exec, net, plugin, or
  MCP tools; shell-capable roles receive only the explicitly permitted `bash`
  capability and still use root authorization;
- `go test -race` reports no map/store/event races.

### Exit criteria

- Manager lifecycle is correct without any model-generated control calls.
- No goroutine or event-bus leak remains after repeated create/close tests.
- Child provider concurrency is either verified or replaced by a provider factory.

## Phase 3 — V2 model tools and headless integration

### Files

- `internal/subagent/tools.go`
- `internal/subagent/tools_test.go`
- `internal/app/app.go`
- `internal/agent/agent_e2e_test.go`
- provider integration tests as needed

### Work

- Register six manager-bound tools on the root registry before router indexing.
- Bind caller identity in the tool object, not model arguments.
- Make tools direct/always loaded while the feature is enabled; defer only after
  tool routing has a tested way to keep the complete collaboration set
  discoverable together.
- Add collaboration usage guidance to the root and child system prompts.
- Ensure Plan mode policy is explicit. Recommended first behavior: allow
  read-only `explorer` spawns in Plan mode, but reject implementer/mutating roles.
- Add max-depth checks even while recursion is disabled.
- Add `RiskDelegate` and explicit descriptors: spawn/follow-up are delegation
  risk; message/wait/interrupt/list are read-risk controls. Do not let the
  unknown-tool fallback classify collaboration as OS execution.
- Apply the ordinary permission gate to model-requested delegation while SDK
  host methods call the manager as explicit host authority. Child actions remain
  independently permissioned.

### End-to-end fake-provider scenario

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

### Exit criteria

- All six schemas and edge errors match the documented contract.
- Spawn returns asynchronously.
- Wait never returns child content.
- Results reach parent context exactly once.
- Tool calling remains serial in the parent; only child turns are parallel.

## Phase 4 — SDK and RPC surfaces

### Files

- `pkg/snowsdk/snowsdk.go`
- `pkg/snowsdk/subagent_test.go` (new)
- `internal/rpc/rpc.go`
- `internal/rpc/subagent_test.go` (new)
- `pkg/protocol` public DTO tests

### SDK methods

```go
SpawnSubagent(ctx, request)
SendSubagentMessage(ctx, target, message)
FollowupSubagent(ctx, target, message)
WaitSubagents(ctx, timeout)
InterruptSubagent(ctx, target)
Subagents()
Subagent(target)
ReadySubagents()
```

SDK methods call App/Manager; they do not implement orchestration.

### RPC commands

```text
subagent_spawn
subagent_send_message
subagent_followup
subagent_wait
subagent_interrupt
subagent_list
subagent_get
subagent_ready
```

Add subagent capability/config summaries to `session_info` without exposing
prompts, credentials, or full child transcripts.

### Tests

- concurrent RPC commands never interleave JSONL frames;
- root abort and child interrupt affect only documented targets;
- RPC EOF cancels/joins according to server close semantics;
- wait command is cancellable;
- SDK close during child stream/bash/wait does not deadlock;
- callback reentrancy remains safe;
- events include identity and clone isolation;
- headless child user-input calls fail fast until a handler is configured.

### Exit criteria

- Public API names and JSON shapes are stable.
- `ReadySubagents` follows the existing subscribe-before-ready goal pattern.
- No constructor starts restored work before subscriptions exist.

## Phase 5 — TUI observation and interaction multiplexing

### Files

- `internal/tui/subagents.go` (new)
- `internal/tui/tui.go`
- `internal/tui/complete.go`
- `internal/tui/event_mailbox.go` only if bounded child-specific coalescing needs
  an extension
- focused TUI tests

### Work

- Add `/agent` with status view when no argument and picker/detail behavior when
  agents exist.
- Maintain `map[threadID]AgentViewState` plus stable first-seen order.
- Show path, role, status, elapsed time, recent bounded activity, and usage.
- Allow viewing a child's transcript without allowing direct composer input.
- Add previous/next navigation only after picker behavior is stable.
- Keep root transcript concise: spawn/status/final summary, not every child token.
- Buffer/coalesce child deltas per thread and cap inactive-thread previews.
- Add an attributed FIFO queue for permission and `ask_user` requests before
  enabling interactive child mutation.
- Show agent identity on every permission/input modal.
- When a child is inactive, retain pending interaction state and surface it in
  the root UI without switching transcripts unexpectedly.

### Tests

- stable spawn order and navigation wraparound;
- duplicate/out-of-order events do not revive terminal agents;
- child `session_updated` does not rehydrate root transcript;
- direct input disabled on parent-owned child views;
- large child streams keep input responsive;
- pending child approval remains visible while root is selected;
- FIFO permission/user-input queue routes each answer to the correct child;
- session teardown clears all child UI state.

### Exit criteria

- TUI is purely a manager/event client.
- Root remains usable while children run.
- No child event can be misattributed to root.

## Phase 6 — durable topology and lazy reload

### Files

- `internal/session/session.go`
- `internal/session/sqlite.go`
- new subagent store tests
- `internal/subagent/persistence.go`
- `docs/sessions.md`

### Work

- Bump `SessionVersion` and add `subagent_threads` migration.
- Add memory/SQLite parity for `SubagentTaskStore`.
- Use generation-checked atomic transitions.
- Create private child DB directory and independent child stores.
- Persist child locator, topology, status, bounded result, and usage.
- Flush child store before terminal metadata commit.
- Restore topology metadata without eagerly opening child DBs.
- Lazily construct runtime on follow-up/message/inspection.
- Add LRU unloading for idle terminal children with empty mailboxes.
- Reconcile stale running records as interrupted on cold open.
- Define cleanup for deleted root sessions and explicit child history removal.
- Keep child sessions out of normal session index/listing.

### Branch semantics

- Each task records the root branch ID where it originated.
- Active tasks block branch switching in the first durable implementation.
- Forking a root branch does not clone running children.
- Terminal child references may be listed as historical metadata on the source
  branch only.
- Clearing/compacting the root does not delete child transcripts.
- Child compaction is independent.

### Tests

- schema migration from current version;
- file and directory modes are private;
- crash/reopen marks stale work interrupted;
- no task runs before `ReadySubagents`;
- lazy reload preserves path/role/model/history;
- concurrent handles cannot CAS the same generation twice;
- child DB appends never mutate root branch tips;
- branch switch/fork policy is deterministic;
- LRU never unloads running or mailbox-nonempty children;
- cleanup never deletes an unrelated session path.

### Exit criteria

- Durable child identity survives restart.
- No active task is duplicated after restart.
- Root and child persistence are independently valid and recoverable.

## Phase 7 — recursive agents, mutation, dynamic tools, and budgets

### Work

- Enable manager tools in child registries only when `recursive=true` and depth
  allows.
- Share the same manager, tree ID, concurrency governor, total task count, and
  budget with all descendants.
- Enable implementer mutation only through explicit config/role policy.
- Add role-specific file ownership guidance and visible shared-workspace warning.
- Add attributed permission queue and verify no authority broadening.
- Opt plugin/MCP tools in only after concurrency and cancellation behavior is
  tested; otherwise serialize calls per extension owner.
- Add optional tree token/cost/time budgets.
- Consider optional external worktree isolation as a backend, not as a claim of
  sandboxing.

### Tests

- nested relative/canonical path resolution;
- max-depth enforcement from every level;
- global concurrency shared across siblings and descendants;
- child cannot forge caller identity;
- child cannot exceed root tool/permission policy;
- overlapping writers receive explicit warnings and do not silently revert
  peers in integration scenarios;
- extension calls are serialized or proven concurrent-safe;
- budget exhaustion rejects new turns without corrupting running ones.

### Exit criteria

- Recursive spawning cannot bypass any root-scoped limit.
- Mutating child authority is explicit and observable.
- Documentation clearly states shared-workspace residual risks.

---

# 7. Test and verification plan

## 7.1 Focused commands during development

```sh
gofmt -w <changed-go-files>
go test ./internal/subagent ./internal/agent ./internal/app ./pkg/protocol -count=1
go test ./internal/session ./internal/rpc ./pkg/snowsdk ./internal/tui -count=1
go test -race ./internal/subagent ./internal/agent ./internal/app ./internal/session ./internal/rpc ./pkg/snowsdk
go vet ./...
go test ./...
```

After a verified implementation milestone:

```sh
./scripts/install-local.sh
```

## 7.2 Required behavior matrix

### Spawn and identity

- fresh, full, and last-N context;
- duplicate task names;
- relative and canonical paths;
- invalid path segments and escape attempts;
- unknown roles/models/efforts;
- reservation rollback on every factory failure point;
- stable list order.

### Parallelism and limits

- two children complete in either order;
- three-child capacity with fourth queued;
- root remains responsive while slots are full;
- per-child turns remain serial;
- session count, depth, timeout, and result caps;
- no busy-loop polling.

### Messaging

- root→child, child→root, sibling→sibling;
- queue-only versus trigger-turn behavior;
- final answer delivered exactly once;
- message during provider sampling/tool execution/finalization;
- pending message survives lazy unload/reload;
- wait wakes without consuming content.

### Lifecycle

- interrupt pending/running/just-completed/not-loaded target;
- follow-up after completed/interrupted/errored;
- parent abort while children continue;
- App close while children stream, execute bash, await permission, or wait;
- session switch and branch switch policy;
- no goroutine/event/subscription leaks.

### Permissions and security

- role tool intersection cannot broaden root policy;
- deny mode hides/denies write, exec, and network tools;
- remembered rules are correctly scoped;
- attributed FIFO prompts route to the correct child;
- child cannot forge path/thread identity;
- no credential appears in events, errors, RPC, logs, or persisted metadata;
- output and task messages are bounded before publication/persistence.

### Persistence

- schema upgrade and rollback behavior;
- root/child DB independence;
- `0600` files;
- cold resume and stale-running reconciliation;
- no constructor-time execution;
- CAS conflicts;
- branch/fork/compaction/cleanup policy;
- malformed/missing child DB produces a bounded errored state, not root failure.

### Surfaces

- SDK state/events/close;
- RPC framing and EOF;
- JSON event correlation;
- print mode concise lifecycle output;
- TUI stable picker, responsive streams, attributed approvals, disabled direct
  child input;
- plugin observers receive cloned attributed events.

## 7.3 Manual smoke tests

1. Spawn two read-only explorers against different code areas and verify real
   parallel provider requests.
2. Abort the root turn while both run; verify they continue and later report.
3. Interrupt one child and follow it up with a new task.
4. Run one child that errors and ensure sibling/root continue.
5. In ask mode, trigger child permission requests and verify FIFO attribution.
6. With mutation explicitly enabled, give implementers disjoint files and verify each
   sees peer edits in the shared cwd.
7. Deliberately give two implementers the same file and confirm the product warns that
   there is no isolation/conflict prevention.
8. Close Snow during a child bash command and confirm process-group cleanup.
9. Resume a durable root and inspect child topology before calling
   `ReadySubagents`.
10. Inspect root and child SQLite DBs to verify independent tips and private file
    modes.

---

# 8. Documentation changes required with implementation

Update these only as milestones land:

- `AGENTS.md`: remove multi-agent orchestration from non-goals; add implemented
  status, package map, commands, storage, security, and tests.
- `README.md`: add feature flag, tools, `/agent`, SDK/RPC examples, limits,
  persistence status, usage implications, and shared-workspace warning.
- `IMPLEMENTATION.md`: add manager architecture, dependency direction, state
  machine, phases, and public contracts.
- `docs/sessions.md`: document topology table, child DB directory, migrations,
  resume, branch, compaction, retention, and cleanup.
- New final `docs/subagents.md`: replace this planning document's future tense
  with verified user-facing behavior once shipped.

Do not describe project trust, permission mode, path roots, or optional
worktrees as a sandbox.

---

# 9. Intentional differences from Codex

Snow should copy the architecture, not every compatibility detail.

| Codex behavior | Snow plan | Reason |
|---|---|---|
| Maintains V1 and V2 | Implement V2 only | No legacy histories exist |
| Namespace tools when backend supports it | Flat stable names initially | Snow protocol has flat `ToolSchema` |
| V2 default max is four including root | Four child slots; root excluded | User-facing concurrency equals simultaneously running subagents |
| V2 effectively allows nested spawning | Default depth one, recursion opt-in | Safer rollout and simpler first release |
| Children normally receive broad tools | Role-filtered registry: shell-capable general/implementer, read-only explorer | Shared OS privileges require root permission gating and explicit mutation opt-in |
| Shared filesystem | Same, explicitly documented | Snow has no sandbox/worktree core |
| Separate thread rollouts and graph store | Separate child DBs plus root graph metadata | Avoid current active-branch cursor conflicts |
| V2 LRU runtime residency | Add only with durability | Unnecessary for ephemeral milestone |
| Subagent lifecycle hooks | Structured plugin events only | Snow has no hook framework and should not add one here |
| Full-history default | Same | Behavioral parity; guidance can prefer fresh explorers |

---

# 10. Definition of done

The feature is complete only when all of the following are true:

- The model can spawn, message, follow up, wait, interrupt, and list independent
  child Agents through the documented V2 tools.
- Children execute concurrently while each child's own turns and tools remain
  serial and valid.
- Canonical paths and parent/child topology are stable and unforgeable.
- none/all/N context inheritance is deterministic and free of dangling tool
  protocol entries.
- Mailbox delivery cannot race root/child session appends and every completion is
  delivered exactly once.
- Parent abort, child interrupt, App close, session switch, and cold resume obey
  documented lifecycle semantics without leaks or deadlocks.
- Child authority never exceeds root/operator authority; interactive requests
  are attributed and serialized.
- Root and child transcripts use independent mutable stores.
- SDK, RPC, JSON/print, TUI, and plugins observe the same attributed lifecycle.
- Race tests, focused tests, full tests, vet, and required manual smoke checks
  pass.
- User-facing docs accurately state shared filesystem, OS privileges, usage
  cost, limits, persistence, and residual prompt-injection/conflict risks.
- The locally installed Snow binary is refreshed after the final verified
  implementation.

Until those conditions hold, keep `subagents.enabled` false by default and avoid
claiming Codex-style subagents as an implemented Snow capability.
