# JSONL RPC

Snow RPC is a long-lived, bidirectional JSON-lines control plane for IDEs,
editor plugins, foreign-language hosts, and subprocess integrations. This
reference defines the wire framing, handshake, command surface, event stream,
ordering guarantees, error model, and shutdown semantics. Companion material
for model-requested input and the Python and JavaScript SDKs lives in
[Model-requested user input](user-input.md) and
[Python and JavaScript/TypeScript SDKs](language-sdks.md).

> **Note:** This is not JSON-RPC 2.0. Snow's external plugin protocol uses
> JSON-RPC 2.0; the CLI control plane documented here is a separate
> Snow-specific protocol with one JSON object per line.

## On this page

- [Overview and framing](#overview-and-framing)
- [Run the server](#run-the-server)
- [Protocol handshake](#protocol-handshake)
- [Request and response envelopes](#request-and-response-envelopes)
- [Prompt commands](#prompt-commands)
- [Model and mode commands](#model-and-mode-commands)
- [Session commands](#session-commands)
- [User input commands](#user-input-commands)
- [Goal commands](#goal-commands)
- [Subagent commands](#subagent-commands)
- [Event stream](#event-stream)
- [Event payload reference](#event-payload-reference)
- [Prompt and response ordering](#prompt-and-response-ordering)
- [Example clients](#example-clients)
- [Errors and shutdown](#errors-and-shutdown)
- [Permission model](#permission-model)
- [Current RPC boundary](#current-rpc-boundary)
- [Related documents](#related-documents)

## Overview and framing

The server runs inside the `snow` process with the current user's OS
privileges. It reads command objects from stdin and writes a single
newline-delimited stream to stdout.

| Stream | Content |
|---|---|
| stdin | Client request objects |
| stdout | `rpc_ready`, response objects, `prompt_completed`, and `protocol.AgentEvent` objects |
| stderr | Startup/configuration diagnostics and process-level errors |

### Framing rules

- Frames are UTF-8 JSON, exactly one object per LF (`\n`) line.
- The maximum input line is 16 MiB.
- A zero-length line is ignored; a whitespace-only line is invalid JSON.
- Split only on the LF byte. Unicode line separators are not frame
  boundaries.
- Responses and events share one serialized writer, so bytes from different
  objects never interleave. Clients must continuously drain stdout. Embedded
  custom transports must provide an interruptible input (`Close` or a deadline
  actually ends `Read`) and deadline-capable or explicitly bounded writes;
  unbounded transports are rejected before serving. Object ordering is still
  asynchronous: a later command may respond before an earlier prompt or
  `subagent_wait` completes.

The first frame is always `rpc_ready`. Snow then writes the initial
collaboration-mode event and restored goal and subagent events before
accepting commands or while processing them. Clients must accept events
before their first response.

## Run the server

Start the server from the repository root or from an installed binary:

```sh
snow --mode rpc --permission deny --no-session
```

Select provider, model, tools, session, extensions, skills, or subagents with
the normal runtime flags:

```sh
snow --mode rpc \
  --provider opencode-go \
  --permission deny \
  --session /path/to/session.db \
  --subagents
```

Keep stdin open until asynchronous prompts and waits finish. Piping a single
prompt with `echo ... | snow --mode rpc` closes stdin immediately and begins
orderly shutdown, which cancels active RPC work. Use a persistent subprocess
client for interactive use.

## Protocol handshake

```json
{
  "type": "rpc_ready",
  "protocol_version": "1",
  "snow_version": "0.1.0-alpha.1",
  "capabilities": [
    "active_input",
    "branch_management",
    "compaction",
    "diagnostics",
    "goals",
    "mcp_servers",
    "messages_list",
    "models_list",
    "multimodal_prompts",
    "pending_inputs",
    "permission_interaction",
    "prompt_completion",
    "response_controls",
    "session_forks",
    "session_info",
    "skills",
    "subagent_models",
    "subagents",
    "usage",
    "user_input"
  ],
  "max_input_bytes": 16777216
}
```

Clients must validate `protocol_version` before sending commands and should
check capabilities before exposing optional high-level methods. Capabilities
state wire support, not runtime enablement; for example, `subagent_models` is
advertised even when subagents are disabled for the current process.

Version 1 is additive: clients must tolerate unknown capabilities, event
types, and optional output fields. Removing or changing existing fields or
enums requires a new protocol version.

### Multimodal prompts

`prompt` is additive: when a Snow binary announces `multimodal_prompts`, the
request may carry a `content` array in addition to the legacy `message`
string. Each content block is exactly `{"type":"text","text":...}` or
`{"type":"image","mime_type":...,"data":...}` (base64 in the request frame).
`message` alone remains valid; an empty `message` is accepted only when
`content` contains an image. The 16 MiB encoded request bound applies to the
full frame, so clients must keep aggregate base64 below that limit. Blocks
other than text/image such as `thinking` or `provider_data` are rejected
before admission, and the active model must advertise image support for image
blocks.

## Request and response envelopes

### Request envelope

```json
{
  "id": "client-generated-correlation-id",
  "type": "prompt",
  "message": "Summarize this repository",
  "model": "model-id",
  "thinking": "low",
  "mode": "default",
  "params": {}
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID; strongly recommended and copied only to responses |
| `type` | string | Yes | Command name |
| `message` | string | No | Top-level text for `prompt`, `steer`, and `follow_up` |
| `model` | string | No | Top-level model ID for `set_model` |
| `thinking` | string | No | Top-level effort for `set_thinking` |
| `reasoning_summary` | string | No | Top-level provider summary preference for `set_reasoning_summary` |
| `text_verbosity` | string | No | Top-level provider verbosity preference for `set_text_verbosity` |
| `mode` | string | No | Top-level collaboration mode for `set_mode` or a prompt-attached mode |
| `params` | object | No | Command-specific JSON object |

Use a unique ID for every request. Ordinary agent events do not carry request
IDs. The public `protocol.RPCRequest`, `protocol.RPCResponse`,
`protocol.RPCReady`, `protocol.RPCPromptCompleted`, `protocol.RPCSessionInfo`,
and model-list DTOs define the stable Go wire representation. Canonical JSON
schemas live under
[`pkg/protocol/schema/rpc/v1`](../pkg/protocol/schema/rpc/v1).

### Response envelope

Success:

```json
{
  "id": "info-1",
  "type": "response",
  "command": "session_info",
  "success": true,
  "data": {}
}
```

Failure:

```json
{
  "id": "model-1",
  "type": "response",
  "command": "set_model",
  "success": false,
  "error": "invalid model id",
  "error_code": "invalid"
}
```

`error_code` is optional for compatibility and, when present, is one of
`canceled`, `conflict`, `destination_exists`, `git_dirty`, `git_failure`,
`invalid`, `not_found`, `not_git_repository`, `session_busy`,
`subagents_active`, or `unsupported`. Branch on this stable code and display
the human-readable `error` only as diagnostics.

A client should route `type == "response"` by ID, route
`type == "prompt_completed"` by `request_id`, and send remaining known types
to its agent-event handler. `rpc_ready` is handled once during startup.

Malformed JSON receives a failure response with `command: "invalid"`. Unknown
commands and validation or runtime failures use the requested command name.

## Prompt commands

### `prompt`

```json
{
  "id": "prompt-1",
  "type": "prompt",
  "message": "Review the public API"
}
```

Attach a collaboration mode atomically:

```json
{
  "id": "prompt-2",
  "type": "prompt",
  "mode": "plan",
  "message": "Design the migration"
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID copied to the admission response and `prompt_completed.request_id` |
| `type` | string | Yes | Must be `prompt` |
| `message` | string | Yes | Non-empty top-level prompt text |
| `mode` | string | No | `default` or `plan`; attaches a collaboration mode to the prompt |

The server immediately returns a successful admission acknowledgement, then
runs the root prompt asynchronously while continuing to read stdin. The
admission response:

```json
{
  "id": "prompt-1",
  "type": "response",
  "command": "prompt",
  "success": true
}
```

`turn_done` marks the agent lifecycle boundary. The definitive RPC result
follows after the prompt fully unwinds:

```json
{
  "type": "prompt_completed",
  "request_id": "prompt-1",
  "status": "completed"
}
```

Failure and cancellation use `status: "failed"` (with `error`) or
`status: "canceled"`. For compatibility, a failed prompt also retains the
older same-ID `success: false` response immediately before
`prompt_completed`. New clients must resolve prompt futures from exactly one
`prompt_completed` frame, not from `turn_done` or the admission response.

Only one root prompt may run. A second `prompt` fails; it never implicitly
cancels accepted work. Use `steer`, `follow_up`, or `abort`.

### `steer` and `follow_up`

```json
{
  "id": "steer-1",
  "type": "steer",
  "message": "Focus on API compatibility"
}
```

```json
{
  "id": "follow-1",
  "type": "follow_up",
  "message": "Then propose tests"
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | `steer` or `follow_up` |
| `message` | string | Yes | Non-empty input text |

Both commands require an active root turn. `steer` becomes eligible at the
next safe boundary after the current assistant response and complete serial
tool batch. `follow_up` becomes eligible only after a natural provider stop
and after earlier steering. Queue updates arrive as `queue_updated` events.

Success response:

```json
{
  "id": "steer-1",
  "type": "response",
  "command": "steer",
  "success": true
}
```

### `abort`

```json
{
  "id": "abort-1",
  "type": "abort"
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `abort` |

Cancels admitted root work and clears undelivered queued input. If goal work
was active, it remains deferred across ordinary `prompt` commands until an
explicit `goal_resume` or `goal_continue`. The command is acknowledged even
when no prompt is active.

```json
{
  "id": "abort-1",
  "type": "response",
  "command": "abort",
  "success": true
}
```

## Model and mode commands

### `models_list`

```json
{
  "id": "models-1",
  "type": "models_list"
}
```

Returns the active provider, current model ID, and a defensive copy of the
active provider catalog:

```json
{
  "id": "models-1",
  "type": "response",
  "command": "models_list",
  "success": true,
  "data": {
    "provider": "fake",
    "current": "fake-1",
    "models": [
      {
        "provider": "fake",
        "id": "fake-1",
        "supports_tools": true,
        "supports_thinking": false,
        "supports_vision": false
      }
    ]
  }
}
```

An unavailable or empty discovered catalog is a successful empty list;
explicitly configured compatible model IDs may still work.

### `set_model`

```json
{
  "id": "model-1",
  "type": "set_model",
  "model": "kimi-k2.6"
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `set_model` |
| `model` | string | Yes | Model ID to activate |

Snow uses matching catalog metadata when available. A change may be rejected
while work is active or when current settings are incompatible.

### `set_thinking`

```json
{
  "id": "thinking-1",
  "type": "set_thinking",
  "thinking": "medium"
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `set_thinking` |
| `thinking` | string | Yes | Reasoning effort level |

Values are `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, and
`ultra`. The active model's advertised capabilities are authoritative.

### `set_mode`

```json
{
  "id": "mode-1",
  "type": "set_mode",
  "mode": "plan"
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `set_mode` |
| `mode` | string | Yes | `default` or `plan` |

Mode changes may be rejected while conflicting work is active. Prefer an
explicit value even though an omitted value normalizes to the Default mode
internally.

### `set_reasoning_summary` and `set_text_verbosity`

These commands change provider response preferences for subsequent turns:

```json
{"id":"summary-1","type":"set_reasoning_summary","reasoning_summary":"concise"}
```

```json
{"id":"verbosity-1","type":"set_text_verbosity","text_verbosity":"high"}
```

`reasoning_summary` must be `off`, `auto`, `concise`, or `detailed`.
`text_verbosity` must be `low`, `medium`, or `high`. Unsupported combinations
are rejected by the active model/provider configuration. Current normalized
values are exposed as optional additive fields in `session_info`.

## Session commands

### `session_rename`

```json
{
  "id": "rename-1",
  "type": "session_rename",
  "params": {
    "name": "API cleanup"
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `session_rename` |
| `params.name` | string | Yes | New display title, 1-72 runes and no control characters |

Changes the active session display title without changing its stable ID,
path, branches, or history. The response `data` contains `session_id` and the
normalized `name`. The command may be rejected while conflicting
root/subagent work is active.

### `branch_fork`

```json
{
  "id": "branch-1",
  "type": "branch_fork",
  "params": {
    "source_branch_id": "main",
    "from_entry_id": "entry-123",
    "name": "experiment"
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `branch_fork` |
| `params.source_branch_id` | string | No | Source branch; empty selects the active branch |
| `params.from_entry_id` | string | No | Entry to fork from; omitted uses the branch tip |
| `params.name` | string | No | New branch name |

Creates and activates a same-database branch. Success `data` is a
`SessionBranch`. The source database and shared entry rows retain the
existing branch semantics.

### Branch inspection and mutation

`branches_list` takes no parameters and returns
`{"branches":[SessionBranch,...]}`. The list contains stable IDs, names,
parent/fork provenance, tips, message counts, previews, timestamps, and the
active flag.

`branch_select`, `branch_rename`, and `branch_delete` use command-specific
`params`:

```json
{"id":"select-1","type":"branch_select","params":{"branch_id":"experiment"}}
```

```json
{"id":"rename-1","type":"branch_rename","params":{"branch_id":"experiment","name":"review"}}
```

```json
{"id":"delete-1","type":"branch_delete","params":{"branch_id":"experiment"}}
```

Selection and deletion return an acknowledgement; rename returns the updated
`SessionBranch`. Existing app admission checks remain authoritative: these
operations can fail with `session_busy` or `subagents_active`, and the active
branch cannot be deleted.

### `compact`

```json
{"id":"compact-1","type":"compact"}
```

Manually compacts provider-facing context for the active branch while retaining
the append-only exact session history. Success `data` contains
`summarized_messages`, `retained_messages`, and optional `summary`,
`used_fallback`, and `automatic` fields. The existing `compaction_started` and
`compaction_done` events describe lifecycle; `automatic` is false for this
manual command. Compaction is rejected while conflicting work is active.

### `session_fork`

```json
{
  "id": "fork-1",
  "type": "session_fork",
  "params": {
    "from_entry_id": "entry-123",
    "name": "independent"
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `session_fork` |
| `params.source_branch_id` | string | No | Source branch |
| `params.from_entry_id` | string | No | Entry to fork from |
| `params.name` | string | No | Child session display name |
| `params.destination_path` | string | No | Must end in `.db` and must not exist |

Creates a detached, independent SQLite child and leaves the RPC process on
the source. Success `data` is `SessionForkResult`, including source
session/branch/entry identity, child ID, path, CWD, its local `main` branch,
and optional worktree information. The response is sent only after the child
database is durable and reopenable.

### `session_worktree_fork`

```json
{
  "id": "worktree-1",
  "type": "session_worktree_fork",
  "params": {
    "from_entry_id": "entry-123",
    "worktree_path": "../snow-experiment",
    "git_branch": "snow/experiment",
    "name": "experiment"
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `session_worktree_fork` |
| `params.source_branch_id` | string | No | Source branch |
| `params.from_entry_id` | string | No | Entry to fork from |
| `params.name` | string | No | Child session display name |
| `params.destination_path` | string | No | Child session path inside the new worktree |
| `params.worktree_path` | string | No | Git worktree path |
| `params.git_branch` | string | No | Git branch name |

Requires a clean Git source and creates a new worktree and branch plus a
detached child session. Omitted worktree and branch values are generated
safely. Relative worktree paths resolve from the source Git root, so a
sibling is normally `../name`; a relative `destination_path` resolves inside
the new worktree and cannot traverse out. Failure never falls back to a
same-workspace branch. The running RPC process retains its source project
bindings.

### `session_info`

```json
{
  "id": "info-1",
  "type": "session_info"
}
```

Successful `data` contains:

```json
{
  "session_id": "...",
  "name": "API cleanup",
  "path": "/path/to/session.db",
  "cwd": "/path/to/project",
  "provider": "opencode-go",
  "model": "kimi-k2.6",
  "thinking": "off",
  "thinking_levels": ["off", "low", "medium", "high"],
  "reasoning_summary": "auto",
  "text_verbosity": "low",
  "collaboration_mode": "default",
  "goal": {
    "goal_id": "...",
    "status": "active",
    "tokens_used": 1200,
    "token_budget": 20000,
    "estimated_costs": [
      {
        "currency": "USD",
        "input": 0.004,
        "output": 0.002,
        "cache_read": 0.0001,
        "cache_write": 0,
        "total": 0.0061
      }
    ]
  },
  "subagents": {
    "enabled": false,
    "max_concurrent_agents": 4,
    "max_concurrent_threads": 4,
    "max_agents_per_session": 32,
    "max_depth": 1,
    "durable": true,
    "allow_mutation": false
  },
  "pending_inputs": {
    "steering": 0,
    "follow_up": 0,
    "total": 0
  }
}
```

| Field | Type | Notes |
|---|---|---|
| `session_id` | string | Stable session identity |
| `name` | string | Display title |
| `path` | string | Session database path; empty for `--no-session` |
| `cwd` | string | Working directory of the session |
| `provider` | string | Active provider ID |
| `model` | string | Active model ID |
| `thinking` | string | Active reasoning effort |
| `thinking_levels` | array | Levels advertised by the active model |
| `reasoning_summary` | string | Optional active reasoning-summary preference |
| `text_verbosity` | string | Optional active text-verbosity preference |
| `collaboration_mode` | string | `default` or `plan` |
| `goal` | object | Present only when a goal exists |
| `subagents` | object | Effective child-agent availability and bounds |
| `pending_inputs` | object | Admitted steering and follow-up waiting for delivery |

`name` is empty until assigned for legacy/untitled stores; built-in stores
receive a local title with their first accepted prompt. Inside a present
goal, `token_budget` is `null` when unlimited, `blocked_reason` is present
for newly blocked goals, and `estimated_costs` can be `null` when pricing is
unavailable. A blocked goal migrated from a pre-version-10 session can omit the
reason because the older schema did not retain one.
`max_concurrent_agents` is a
compatibility alias of `max_concurrent_threads`; both currently carry the
same limit. `reasoning_summary` and `text_verbosity` are optional additive
fields so v1 clients and older recorded frames remain compatible.

### History, usage, pending input, and diagnostics

The following inspection commands take no parameters:

| Command | Success `data` | Notes |
|---|---|---|
| `messages_list` | `{"messages":[...]}` | Linearized active-branch history; provider-private continuity blocks are omitted |
| `usage` | usage object | Aggregate token, cache, request, and optional cost totals for the active branch |
| `pending_inputs` | `{"items":[...]}` | Submission-ordered queued `steer` and `follow_up` input |
| `pending_inputs_clear` | `{"items":[...]}` | Atomically removes and returns undelivered queued input |
| `diagnostics` | `{"diagnostics":[...]}` | Non-fatal configuration warnings with `path` and `message` |
| `mcp_servers` | `{"servers":[...]}` | Secret-free negotiated MCP server status (no credentials, headers, or argv) |
| `skills` | `{"skills":[...],"diagnostics":[...]}` | Full skill catalog plus discovery diagnostics |

## Permission interaction

Ask-mode permission requests are published as `permission_request` events with
a stable, host-facing `id`:

```json
{"type":"permission_request","permission":{"request":{"id":"perm-3","tool":"bash","risk":"exec"}}}
```

A client resolves them with `permission_reply` (decision `allow`,
`allow_session`, `allow_always`, or `deny`) or `permission_reject`:

```json
{"id":"pr-1","type":"permission_reply","params":{"request_id":"perm-3","decision":"allow"}}
```

```json
{"id":"rj-1","type":"permission_reject","params":{"request_id":"perm-3"}}
```

Remaining security invariants are preserved: the service is deny-by-default,
reads never ask, allow/deny modes never consult a broker, and ask-mode requests
block only when the surface is opted in with a handler or manual replies.
`allow_session` and `allow_always` are remembered for the remainder of the
session. `permission_interaction` capability gates these commands.

`messages_list` can include user and assistant text, images, thinking summaries,
tool calls, tool results, and surface-safe tool-display metadata. It never
emits opaque `provider_data` continuity blocks. Tool output and queued input can
contain project/user data; clients must apply their own display and storage
policy. Arrays are encoded as `[]`, not `null`.

## User input commands

A blocked `ask_user` call emits:

```json
{
  "type": "user_input_request",
  "user_input": {
    "id": "call-1",
    "tool_call_id": "call-1",
    "questions": [
      {
        "id": "format",
        "header": "Format",
        "question": "Which format should I use?",
        "options": [
          {
            "label": "JSON",
            "description": "Machine-readable"
          },
          {
            "label": "Text",
            "description": "Human-readable"
          }
        ]
      }
    ]
  }
}
```

### `user_input_reply`

```json
{
  "id": "reply-1",
  "type": "user_input_reply",
  "params": {
    "request_id": "call-1",
    "answers": [
      {
        "id": "format",
        "answer": "JSON"
      }
    ]
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `user_input_reply` |
| `params.request_id` | string | Yes | Pending request ID from the `user_input_request` event |
| `params.answers` | array | Yes | One answer per question, keyed by stable question ID |
| `params.answers[].id` | string | Yes | Question ID |
| `params.answers[].answer` | string | Yes | Answer text or selected option label |

### `user_input_reject`

```json
{
  "id": "reject-1",
  "type": "user_input_reject",
  "params": {
    "request_id": "call-1"
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `user_input_reject` |
| `params.request_id` | string | Yes | Pending request ID to reject |

Answers are trimmed, non-empty, limited to 8 KiB, and normalized to request
order. Invalid, incomplete, duplicate, oversized, or stale replies fail
without clearing the pending request, so the client may correct and retry.
Only one input request is pending because Snow executes each agent's tool
calls serially. See [Model-requested user input](user-input.md).

## Goal commands

Goals require a branch-scoped session store. SQLite makes them durable across
processes; `--no-session` uses the same commands with process-lifetime
in-memory state. Full semantics are documented in
[Persistent Thread Goals](goals.md).

| Command | `params` | Success `data` |
|---|---|---|
| `goal_get` | none | `ThreadGoal` or `null` |
| `goal_create` | `objective`, optional `token_budget`, optional `replace` | Created goal |
| `goal_set` | same as `goal_create` | Alias of create |
| `goal_edit` | `objective` | Updated/rotated goal |
| `goal_pause` | none | Updated goal |
| `goal_resume` | none | Updated goal; also resumes an active abort-deferred goal |
| `goal_clear` | none | `{"cleared":true|false}` |
| `goal_continue` | none | Eligible continued goal state |

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | One of the goal command names above |
| `params.objective` | string | Yes | Goal objective, at most 32 Ki Unicode characters |
| `params.token_budget` | integer | No | Positive token budget |
| `params.replace` | boolean | No | Replace an existing goal |

Examples:

```json
{
  "id": "goal-1",
  "type": "goal_create",
  "params": {
    "objective": "Ship and verify the parser",
    "token_budget": 20000
  }
}
```

```json
{
  "id": "goal-2",
  "type": "goal_pause"
}
```

```json
{
  "id": "goal-3",
  "type": "goal_resume"
}
```

```json
{
  "id": "goal-4",
  "type": "goal_edit",
  "params": {
    "objective": "Ship, benchmark, and verify the parser"
  }
}
```

```json
{
  "id": "goal-5",
  "type": "goal_clear"
}
```

Token budgets must be positive. When pricing is available, goal DTOs include
optional `estimated_costs` grouped by currency; these are catalog/provider
estimates, not invoices. Goal state also streams through
`thread_goal_updated`.

## Subagent commands

Enable subagents with `--subagents` or configuration. See
[Subagents](subagents.md) for role, authority, persistence, and lifecycle
details.

### `subagent_models`

```json
{
  "id": "child-models-1",
  "type": "subagent_models"
}
```

Returns exact provider/model pairs available to children and an `enabled`
flag for the current runtime. The catalog is returned even when spawning is
disabled, so a host can configure a future session without guessing model
IDs.

### `subagent_spawn`

```json
{
  "id": "agent-1",
  "type": "subagent_spawn",
  "params": {
    "name": "api_review",
    "task": "Review the public API for compatibility risks.",
    "role": "explorer",
    "provider": "opencode-go",
    "model": "exact-model-id",
    "fork_turns": "all",
    "reasoning_effort": "low"
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `subagent_spawn` |
| `params.name` | string | Yes | Child name; strict lowercase canonical segment |
| `params.task` | string | Yes | Child assignment |
| `params.role` | string | No | Capability profile |
| `params.provider` | string | No | Child provider override |
| `params.model` | string | No | Child model override |
| `params.fork_turns` | string | No | `none`, `all`, or a positive integer |
| `params.reasoning_effort` | string | No | Child reasoning effort level |

RPC names are strict lowercase canonical segments; hyphens are not
normalized. Success returns `SubagentState`.

### Messaging and follow-up

```json
{
  "id": "mail-1",
  "type": "subagent_send_message",
  "params": {
    "target": "/root/api_review",
    "message": "Check events too."
  }
}
```

```json
{
  "id": "task-2",
  "type": "subagent_followup",
  "params": {
    "target": "/root/api_review",
    "message": "Now inspect tests."
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | `subagent_send_message` or `subagent_followup` |
| `params.target` | string | Yes | Canonical child path |
| `params.message` | string | Yes | Non-empty message text |

`subagent_send_message` queues attributed mail without starting a child turn.
`subagent_followup` queues and starts or reuses eligible child work.

### `subagent_wait`

```json
{
  "id": "wait-1",
  "type": "subagent_wait",
  "params": {
    "timeout_ms": 30000,
    "until": "activity"
  }
}
```

```json
{
  "id": "wait-2",
  "type": "subagent_wait",
  "params": {
    "timeout_ms": 60000,
    "until": "all"
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `subagent_wait` |
| `params.timeout_ms` | integer | Yes | Nonnegative wait timeout; zero uses the configured default |
| `params.until` | string | No | `activity` (default) or `all` |

The default/empty `until` is `activity`. `all` waits until every descendant
is terminal or the bounded timeout expires. Values that cannot be represented
safely are rejected before a wait worker starts. Wait handling is
asynchronous; several wait responses may arrive out of request order. A server
accepts at most 64 concurrent wait workers and rejects additional waits until a
slot is released. `data`
is a `WaitSubagentsResult` aggregate and never contains private child result
text.

### Inspect and interrupt

```json
{
  "id": "list-1",
  "type": "subagent_list",
  "params": {
    "path_prefix": "/root"
  }
}
```

```json
{
  "id": "get-1",
  "type": "subagent_get",
  "params": {
    "target": "/root/api_review"
  }
}
```

```json
{
  "id": "stop-1",
  "type": "subagent_interrupt",
  "params": {
    "target": "/root/api_review"
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | `subagent_list`, `subagent_get`, or `subagent_interrupt` |
| `params.path_prefix` | string | No | Path prefix filter for `subagent_list` |
| `params.target` | string | Yes | Canonical child path for `subagent_get` and `subagent_interrupt` |

`subagent_list` returns a `SubagentList` with snapshots and limits. Its `open`
field is the number of identities consuming the root session's `agent_limit`;
`closed` is the visible closed-child count. `subagent_get` returns one
`SubagentState`. `subagent_interrupt` returns `{"previous_status":"..."}` and
leaves the child reusable.

### Close and resume

```json
{
  "id": "close-1",
  "type": "subagent_close",
  "params": {"target": "/root/api_review"}
}
```

```json
{
  "id": "resume-1",
  "type": "subagent_resume",
  "params": {"target": "/root/api_review"}
}
```

`subagent_close` requires a terminal child, releases its open-agent slot, and
preserves its stable path, thread ID, topology, result, usage, and durable
transcript. Success returns its previous and current (`closed`) statuses.
`subagent_resume` consumes an available open-agent slot and returns the reopened
`SubagentState` without starting a turn. `subagent_followup` performs this
resume automatically when its target is closed. Closed paths remain reserved.

### `subagent_ready`

`subagent_ready` exposes the explicit readiness seam used by embedders, but
`snow --mode rpc` already readies restored topology before accepting
commands.

## Event stream

After the `rpc_ready` handshake, frames other than `response` and
`prompt_completed` are normalized `protocol.AgentEvent` values.

| Category | Event types |
|---|---|
| Streaming | `text_delta`, `thinking_delta`, `usage`, `provider_retry` |
| Tools | `tool_start`, `tool_progress`, `tool_end`, `tool_routing` |
| Interaction | `user_input_request`, `queue_updated` |
| Lifecycle/state | `session_updated`, `turn_done`, `error`, `aborted`, `model_changed`, `mode_changed` |
| Plan | `plan_started`, `plan_delta`, `plan_completed`, `plan_update` |
| Compaction | `compaction_started`, `compaction_done` |
| Goals | `thread_goal_updated` |
| Subagents | `subagent_started`, `subagent_status`, `subagent_message`, `subagent_activity` |

`compaction.automatic` marks any non-manual pressure or overflow-repair run.
`subagent_activity` is a reserved wire event type that is not currently
emitted.

### Correlation rules

- `agent` omitted: root-agent event, including ordinary prompts, goal
  continuation, and root state/lifecycle events.
- `agent` present: attributed child stream, tool, or usage event.
- `subagent` present: child lifecycle state.
- `snapshot: true`: restored state published to initialize observers after
  startup or a session switch, not a lifecycle transition that just occurred.
- `agent_message` present: attributed mailbox event.
- `turn_sequence`: process-local monotonic admission order for correlated
  turn events. Use `turn_id` as the stable identity; the sequence restarts
  with the process.
- `root_epoch`: process-local root session/branch reconciliation generation
  stamped on every root event, including events outside a turn.
- `tool_output`: bounded preview only; full results remain in session
  storage.

`permission_request` is part of the shared `AgentEvent` type, but RPC's
headless permission asker fails closed and does not emit it.
`user_input_request` is a separate model-question interaction and is emitted
as documented above.

## Event payload reference

Fields not listed for an event are omitted unless they are one of the
correlation fields above. Nested objects use the public `pkg/protocol` JSON
tags.

### Streaming events

| Event type | Payload fields | Ordering |
|---|---|---|
| `text_delta` | `text` | In provider stream order |
| `thinking_delta` | `text` | In provider stream order |
| `usage` | `usage` | Per provider usage record, including at turn completion |
| `provider_retry` | `provider_retry` | Before a cancellation-aware retry wait; nonterminal |

### Tool events

| Event type | Payload fields | Ordering |
|---|---|---|
| `tool_start` | `tool_call_id`, `tool_name` | Before tool execution |
| `tool_progress` | `tool_progress` | During long-running tool execution |
| `tool_end` | `tool_call_id`, `tool_name`, `tool_output?`, `tool_duration_ms?`, `is_error?` | After tool completion |
| `tool_routing` | `tool_routing` | When deferred-tool discovery selects tools |

### Interaction events

| Event type | Payload fields | Ordering |
|---|---|---|
| `user_input_request` | `user_input` | While an `ask_user` call blocks for host input |
| `queue_updated` | `queue` | When steer/follow-up input is admitted or delivered |

### Lifecycle and state events

| Event type | Payload fields | Ordering |
|---|---|---|
| `session_updated` | correlation/state fields; `message` may hold detail | On session metadata changes |
| `turn_done` | correlation/state fields; `usage` may be present | At the end of an agent turn |
| `error` | `message` | On recoverable and non-fatal errors |
| `aborted` | correlation/state fields; `message` may hold detail | On cancellation of active work |
| `model_changed` | `model` | When the active model changes |
| `mode_changed` | `mode` | When collaboration mode or reasoning effort changes |

`provider_retry` carries `provider`, retry `kind`, request `phase`, next
`attempt`, `max_attempts`, `delay_ms`, `elapsed_ms`, and `max_elapsed_ms`.
Expected retry waits do not emit `error`; final exhaustion emits one terminal
error diagnostic. `error` events carry `message` only; `is_error` is not
currently set on them. Error-path `compaction_done` events do set
`is_error: true`.

### Plan events

| Event type | Payload fields | Ordering |
|---|---|---|
| `plan_started` | `plan` | Plan presentation begins |
| `plan_delta` | `plan`, `text` | Incremental plan text |
| `plan_completed` | `plan` | Plan presentation ends |
| `plan_update` | `plan_update` | Checklist state changes |

### Compaction events

| Event type | Payload fields | Ordering |
|---|---|---|
| `compaction_started` | `compaction` | Compaction begins |
| `compaction_done` | `compaction`, `message?`, `is_error?` | Compaction ends; error-path events set `is_error: true` |

### Goal events

| Event type | Payload fields | Ordering |
|---|---|---|
| `thread_goal_updated` | `thread_goal` | On goal create, edit, pause, resume, clear, or continue |

### Subagent events

| Event type | Payload fields | Ordering |
|---|---|---|
| `subagent_started` | `subagent` | Child lifecycle snapshot on start |
| `subagent_status` | `subagent`, `snapshot?` | Child lifecycle/status change, or restored observer state when `snapshot` is true |
| `subagent_message` | `agent_message` | Attributed mailbox delivery |
| `subagent_activity` | reserved | Not currently emitted |

### Nested payload shapes

A `tool_progress` object has `tool_call_id`, `name`, optional `message`,
`done`, and optional `is_error`.

A `tool_routing` object has `trigger`, optional `tool_ids`,
`candidate_count`, `selected_count`, `exposed_count`, `schema_bytes`,
`latency_ms`, and optional `fallback`.

A `plan` object has `id` and optional `text`; delta text is in `text` where
present. A `plan_update` object has optional `explanation` and a `plan` array
of `{step,status}` entries where status is `pending`, `in_progress`, or
`completed`.

A `compaction` object has `summarized_messages`, `retained_messages`,
optional `summary`, optional `used_fallback`, and optional `automatic`.

A `queue` object has `items`, an array of `{id,kind,text,order}` entries in
submission order where `kind` is `steer` or `follow_up`.

A `thread_goal` object has optional `goal` and optional `cleared`. A cleared
goal event uses `thread_goal.cleared: true` with no goal.

A `usage` object has `input`, `output`, optional `reasoning`, `cache_read`,
optional `cache_read_known`, `cache_write`, `total_tokens`, optional
`requests`, and optional `cost`. Cost is
`{currency?,input,output,cache_read,cache_write,total}`.

A full goal contains `session_id`, `branch_id`, `goal_id`, `objective`,
`status`, optional `blocked_reason`, optional `token_budget`, `tokens_used`,
`seconds_used`, optional `estimated_costs`, `created_at`, and `updated_at`.
`blocked_reason` explains the durable blocker and is omitted in other states;
it can also be absent on a blocked goal migrated from a pre-version-10 session.
Status is `active`,
`paused`, `blocked`, `usage_limited`, `budget_limited`, or `complete`.

A subagent snapshot contains `agent`, `status`, optional provider, model, and
thinking metadata, timestamps, bounded `result` and `error`, optional
`usage`, and optional `generation`. The nested agent reference contains
`thread_id`, optional `parent_thread_id`, canonical `path` and
`parent_path`, optional role and nickname, and `depth`. Lifecycle status is
`pending_init`, `queued`, `running`, `interrupted`, `completed`, `errored`,
`shutdown`, `not_loaded`, or `not_found` where the command or event permits
it. `subagent_list` additionally returns `running`, `queued`, `terminal`,
`concurrent_limit`, `agent_limit`, and optional `truncated`.

Usage payloads keep `input` as the total prompt count, including cached
tokens. `cache_read_known: true` means the provider explicitly reported its
cached-token field, so `cache_read > 0` is a hit and `cache_read == 0` is a
confirmed miss. When `cache_read_known` is absent or false, zero is unknown
rather than a miss. For aggregate usage it is true only when every included
provider request reported the metric.

Clients should switch on `type` and tolerate new optional fields and event
types for forward compatibility.

## Prompt and response ordering

A typical sequence is:

```text
rpc_ready                              # first frame; validate version
mode_changed event                     # startup state
response(id=prompt-1, success=true)    # prompt admitted
text_delta / tool_* / usage events
queue_updated events                   # if steer/follow-up is admitted/delivered
turn_done event                        # agent lifecycle boundary
prompt_completed(request_id=prompt-1)  # definitive RPC result
```

Important ordering rules:

- Prompt acknowledgement is admission, not completion.
- `turn_done` ends the agent turn; `prompt_completed` is the terminal RPC
  result.
- A failed prompt retains a legacy same-ID failure response immediately
  before its single `prompt_completed(status=failed)` frame.
- `subagent_wait` responses are asynchronous.
- Different command responses can arrive out of request order.
- Writes are frame-atomic, so a single JSON line is never mixed with another.
- EOF, cancellation, and scanner failures cancel and join prompt and wait
  workers before `Serve` returns; cleanup failures are returned to the host.
- Keep a response table keyed by ID, a prompt-terminal table keyed by
  `request_id`, and process agent events independently.

## Example clients

Typed, zero-runtime-dependency SDKs and runnable examples are exercised by
Linux/macOS CI against the fake provider:

```sh
go build -o ./snow ./cmd/snow
python3 examples/rpc/python/client.py --snow ./snow
node examples/rpc/javascript/client.mjs ./snow
```

See [Python and JavaScript/TypeScript SDKs](language-sdks.md) for the
supported high-level clients. The following low-level example shows the
underlying framing, including the distinction between `turn_done` and
`prompt_completed`.

```python
#!/usr/bin/env python3
import json
import subprocess
import sys

proc = subprocess.Popen(
    [
        "snow",
        "--mode", "rpc",
        "--permission", "deny",
        "--no-session",
    ],
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    text=True,
    bufsize=1,
)


def send(message):
    proc.stdin.write(json.dumps(message, separators=(",", ":")) + "\n")
    proc.stdin.flush()


ready = json.loads(proc.stdout.readline())
if ready.get("type") != "rpc_ready" or ready.get("protocol_version") != "1":
    raise RuntimeError("unsupported Snow RPC protocol")

send({
    "id": "prompt-1",
    "type": "prompt",
    "message": "Summarize this repository.",
})

legacy_error = None
for line in proc.stdout:
    message = json.loads(line)
    kind = message.get("type")

    if kind == "response":
        if not message.get("success") and message.get("id") == "prompt-1":
            legacy_error = message.get("error", "RPC error")
        continue

    if kind == "text_delta" and "agent" not in message:
        print(message.get("text", ""), end="", flush=True)

    if kind == "user_input_request":
        request = message["user_input"]
        answers = []
        for question in request["questions"]:
            options = question.get("options", [])
            answer = options[0]["label"] if options else "No additional constraints"
            answers.append({"id": question["id"], "answer": answer})
        send({
            "id": "input-1",
            "type": "user_input_reply",
            "params": {
                "request_id": request["id"],
                "answers": answers,
            },
        })

    if kind == "turn_done" and "agent" not in message:
        print()

    if kind == "prompt_completed" and message.get("request_id") == "prompt-1":
        if message.get("status") != "completed":
            raise RuntimeError(
                message.get("error") or legacy_error or message.get("status")
            )
        break

proc.stdin.close()  # EOF: orderly RPC shutdown
raise SystemExit(proc.wait())
```

This compact snippet demonstrates raw framing. Applications should normally
use the checked-in Python or JavaScript SDK, which validates the handshake,
routes out-of-order responses, bounds frames and queues, and waits for
definitive prompt completion.

Production clients should also:

- use an asynchronous reader independent from request submission;
- maintain ID-indexed promises/futures;
- bound client-side frames and logs;
- apply process and request deadlines;
- handle stderr separately;
- redact secrets and provider-sensitive payloads;
- tolerate events before the first request;
- reject prompt futures if the process exits before `prompt_completed`.

## Errors and shutdown

- Empty lines produce no output.
- Invalid JSON returns a failure response when stdout remains writable.
- Unknown command and validation errors do not terminate the server.
- Scanner errors, broken or short stdout writes, startup failures, or parent
  context cancellation terminate serving.
- EOF stops command admission, cancels RPC work and waits, releases
  user-input waiters, joins the active prompt and goroutines, then exits.
- There is no `shutdown` command. Close stdin, signal the process, or cancel
  the embedding context.

## Permission model

RPC has no permission request/reply handshake. In `ask` mode its headless
asker denies without emitting an interactive permission event. Use an
explicit headless policy:

- `--permission deny` for read-oriented operation;
- `--permission allow` only in an externally trusted or isolated environment.

`ask` fails closed in RPC. `user_input_reply` answers model questions and
does not authorize OS or tool access.

RPC, shell, plugins, stdio MCP servers, and subagents run with the current
user's OS privileges. Read the [Security model](security.md).

## Current RPC boundary

The current command surface covers prompts, active root input, cancellation,
active-provider and subagent model discovery, model/thinking/mode and response
controls, session and branch management, manual compaction, active-branch
messages and usage, MCP/skill discovery, pending-input inspection/clearing,
configuration diagnostics, model-requested input, goals, and subagents.

Configuration mutation, login, and an interactive permission broker remain
outside the RPC command surface. MCP and skill inventory is read-only and
secret-free; management/mutation stays outside the boundary.

## Related documents

- [Model-requested user input](user-input.md)
- [Python and JavaScript/TypeScript SDKs](language-sdks.md)
- [Persistent Thread Goals](goals.md)
- [Subagents](subagents.md)
- [Security model](security.md)
