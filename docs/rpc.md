# JSONL RPC

Snow RPC is a long-lived, bidirectional control plane for IDEs, editor plugins,
foreign-language hosts, and subprocess integrations.

It is **not JSON-RPC 2.0**. Snow's external plugin protocol uses JSON-RPC 2.0;
the CLI control plane documented here is a separate Snow-specific protocol with
one JSON object per line.

## Start the server

```sh
snow --mode rpc --permission deny --no-session
```

Use the normal runtime flags to select provider, model, tools, session,
extensions, skills, or subagents:

```sh
snow --mode rpc \
  --provider opencode-go \
  --permission deny \
  --session /path/to/session.db \
  --subagents
```

Keep stdin open until asynchronous prompts and waits finish. Sending a prompt
through `echo ... | snow --mode rpc` immediately closes stdin and begins orderly
shutdown, which cancels active RPC work. Use a persistent subprocess client.

## Transport and framing

- **stdin:** client request objects
- **stdout:** `rpc_ready`, response objects, `prompt_completed`, and asynchronous `protocol.AgentEvent` objects
- **stderr:** startup/configuration diagnostics and process-level errors
- **framing:** UTF-8 JSON, exactly one object per LF (`\n`) line
- **maximum input line:** 16 MiB
- **empty frames:** a zero-length line is ignored; whitespace-only lines are invalid JSON

Split only on the LF byte. Do not use Unicode line separators as frames.
Responses and events share one serialized writer, so bytes from different
objects never interleave. Object ordering is still asynchronous: a later command
may respond before an earlier prompt or `subagent_wait` completes.

The first frame is always `rpc_ready`. Snow then writes the initial
collaboration-mode event and restored goal/subagent events before accepting or
while processing commands. Clients must accept events before their first
response.

## Protocol handshake

```json
{
  "type": "rpc_ready",
  "protocol_version": "1",
  "snow_version": "0.1.0-dev",
  "capabilities": [
    "active_input",
    "goals",
    "models_list",
    "prompt_completion",
    "session_info",
    "subagent_models",
    "subagents",
    "user_input"
  ],
  "max_input_bytes": 16777216
}
```

Clients must validate `protocol_version` before sending commands and should
check capabilities before exposing optional high-level methods. Capabilities
state wire support, not runtime enablement; for example, `subagent_models` is
advertised even when subagents are disabled for the current process.

Version 1 is additive: clients must tolerate unknown capabilities, event types,
and optional output fields. Removing or changing existing fields/enums requires
a new protocol version.

## Request envelope

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

| Field | Use |
|---|---|
| `id` | Optional to the server, strongly recommended; copied only to responses |
| `type` | Required command name |
| `message` | Top-level text for `prompt`, `steer`, and `follow_up` |
| `model` | Top-level model ID for `set_model` |
| `thinking` | Top-level effort for `set_thinking` |
| `mode` | Top-level collaboration mode for `set_mode` or prompt-attached mode |
| `params` | Command-specific JSON object |

Use a unique ID for every request. Ordinary agent events do not carry request
IDs. Public `protocol.RPCRequest`, `RPCResponse`, `RPCReady`,
`RPCPromptCompleted`, `RPCSessionInfo`, and model-list DTOs define the stable Go
wire representation. Canonical JSON schemas live under
[`pkg/protocol/schema/rpc/v1`](../pkg/protocol/schema/rpc/v1).

## Response envelope

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
  "error": "..."
}
```

A client should route `type == "response"` by ID, route
`type == "prompt_completed"` by `request_id`, and send remaining known types to
its agent-event handler. `rpc_ready` is handled once during startup.

Malformed JSON receives a failure response with `command: "invalid"`. Unknown
commands and validation/runtime failures use the requested command name.

## Core commands

### `prompt`

```json
{"id":"prompt-1","type":"prompt","message":"Review the public API"}
```

Attach a collaboration mode atomically:

```json
{"id":"prompt-2","type":"prompt","mode":"plan","message":"Design the migration"}
```

`message` is required. `mode`, when present, is `default` or `plan`.

The server immediately returns a successful admission acknowledgement, then runs
the root prompt asynchronously while continuing to read stdin. `turn_done`
marks the agent lifecycle boundary. The definitive RPC result follows after the
prompt fully unwinds:

```json
{"type":"prompt_completed","request_id":"prompt-1","status":"completed"}
```

Failure and cancellation use `status: "failed"` (with `error`) or
`status: "canceled"`. For compatibility, a failed prompt also retains the older
same-ID `success:false` response immediately before `prompt_completed`. New
clients must resolve prompt futures from exactly one `prompt_completed` frame,
not from `turn_done` or the admission response.

Only one root prompt may run. A second `prompt` fails; it never implicitly
cancels accepted work. Use `steer`, `follow_up`, or `abort`.

### `steer`

```json
{"id":"steer-1","type":"steer","message":"Focus on API compatibility"}
```

Requires a non-empty message and an active root turn. The input becomes eligible
at the next safe boundary after the current assistant response and complete
serial tool batch.

### `follow_up`

```json
{"id":"follow-1","type":"follow_up","message":"Then propose tests"}
```

Requires an active root turn. It becomes eligible only after a natural provider
stop and after earlier steering. Queue updates arrive as `queue_updated` events.

### `abort`

```json
{"id":"abort-1","type":"abort"}
```

Cancels admitted root work and clears undelivered queued input. If goal work was
active, it remains deferred across ordinary `prompt` commands until an explicit
`goal_resume` or `goal_continue`. The command is acknowledged even when no
prompt is active.

### `models_list`

```json
{"id":"models-1","type":"models_list"}
```

Returns the active provider, current model ID, and a defensive copy of the
active provider catalog:

```json
{
  "id":"models-1",
  "type":"response",
  "command":"models_list",
  "success":true,
  "data":{"provider":"fake","current":"fake-1","models":[{"provider":"fake","id":"fake-1","supports_tools":true,"supports_thinking":false,"supports_vision":false}]}
}
```

An unavailable/empty discovered catalog is a successful empty list; explicitly
configured compatible model IDs may still work.

### `set_model`

```json
{"id":"model-1","type":"set_model","model":"kimi-k2.6"}
```

`model` is required. Snow uses matching catalog metadata when available. A
change may be rejected while work is active or when current settings are
incompatible.

### `set_thinking`

```json
{"id":"thinking-1","type":"set_thinking","thinking":"medium"}
```

Values are `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, and
`ultra`. The active model's advertised capabilities are authoritative.

### `set_mode`

```json
{"id":"mode-1","type":"set_mode","mode":"plan"}
```

Use `default` or `plan`. Mode changes may be rejected while conflicting work is
active. Prefer an explicit value even though an omitted value normalizes to the
Default mode internally.

### `session_rename`

```json
{"id":"rename-1","type":"session_rename","params":{"name":"API cleanup"}}
```

Changes the active session display title without changing its stable ID, path,
branches, or history. The trimmed title must contain 1–72 runes and no control
characters. The response `data` contains `session_id` and the normalized `name`.
The command may be rejected while conflicting root/subagent work is active.

### `session_info`

```json
{"id":"info-1","type":"session_info"}
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
  "collaboration_mode": "default",
  "goal": {
    "goal_id": "...",
    "status": "active",
    "tokens_used": 1200,
    "token_budget": 20000,
    "estimated_costs": [
      {"currency":"USD","input":0.004,"output":0.002,"cache_read":0.0001,"cache_write":0,"total":0.0061}
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

`name` is empty until assigned for legacy/untitled stores; built-in stores receive
a local title with their first accepted prompt. `path` is empty for
`--no-session`; `goal` is omitted when none exists. Inside a present goal,
`token_budget` is `null` when unlimited and `estimated_costs` can be `null` when
pricing is unavailable. `max_concurrent_agents` is a compatibility alias of
`max_concurrent_threads`; both currently carry the same limit.

## Model-requested user input

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
          {"label": "JSON", "description": "Machine-readable"},
          {"label": "Text", "description": "Human-readable"}
        ]
      }
    ]
  }
}
```

Reply to every question exactly once by stable question ID:

```json
{
  "id": "reply-1",
  "type": "user_input_reply",
  "params": {
    "request_id": "call-1",
    "answers": [
      {"id": "format", "answer": "JSON"}
    ]
  }
}
```

Or reject only the pending tool interaction:

```json
{
  "id": "reject-1",
  "type": "user_input_reject",
  "params": {"request_id": "call-1"}
}
```

Answers are trimmed, non-empty, limited to 8 KiB, and normalized to request
order. Invalid, incomplete, duplicate, oversized, or stale replies fail without
clearing the pending request, so the client may correct and retry. Only one input
request is pending because Snow executes each agent's tool calls serially.

See [Model-requested user input](user-input.md).

## Goal commands

Goals require a persisted SQLite session. Full semantics are documented in
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

Examples:

```json
{"id":"goal-1","type":"goal_create","params":{"objective":"Ship and verify the parser","token_budget":20000}}
{"id":"goal-2","type":"goal_pause"}
{"id":"goal-3","type":"goal_resume"}
{"id":"goal-4","type":"goal_edit","params":{"objective":"Ship, benchmark, and verify the parser"}}
{"id":"goal-5","type":"goal_clear"}
```

Objectives are required and limited to 32 Ki Unicode characters. Token budgets
must be positive. When pricing is available, goal DTOs include optional
`estimated_costs` grouped by currency; these are catalog/provider estimates,
not invoices. Goal state also streams through `thread_goal_updated`.

## Subagent commands

Enable subagents with `--subagents` or configuration. See [Subagents](subagents.md)
for role, authority, persistence, and lifecycle details.

### `subagent_models`

```json
{"id":"child-models-1","type":"subagent_models"}
```

Returns exact provider/model pairs available to children and an `enabled` flag
for the current runtime. The catalog is returned even when spawning is disabled,
so a host can configure a future session without guessing model IDs.

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

Required: `name`, `task`. Optional: `role`, `fork_turns`, `provider`, `model`,
`reasoning_effort`. RPC names are strict lowercase canonical segments; hyphens
are not normalized. Success returns `SubagentState`.

### Messaging and follow-up

```json
{"id":"mail-1","type":"subagent_send_message","params":{"target":"/root/api_review","message":"Check events too."}}
{"id":"task-2","type":"subagent_followup","params":{"target":"/root/api_review","message":"Now inspect tests."}}
```

`send_message` queues attributed mail without starting a child turn.
`subagent_followup` queues and starts/reuses eligible child work.

### Wait

```json
{"id":"wait-1","type":"subagent_wait","params":{"timeout_ms":30000,"until":"activity"}}
{"id":"wait-2","type":"subagent_wait","params":{"timeout_ms":60000,"until":"all"}}
```

The default/empty `until` is `activity`. `all` waits until every descendant is
terminal or the bounded timeout expires. `timeout_ms` must be nonnegative; zero
uses the configured default, and values that cannot be represented safely are
rejected before a wait worker starts. Wait handling is asynchronous; several
wait responses may arrive out of request order. `data` is a
`WaitSubagentsResult` aggregate and never contains private child result text.

### Inspect and interrupt

```json
{"id":"list-1","type":"subagent_list","params":{"path_prefix":"/root"}}
{"id":"get-1","type":"subagent_get","params":{"target":"/root/api_review"}}
{"id":"stop-1","type":"subagent_interrupt","params":{"target":"/root/api_review"}}
```

- `subagent_list` returns a `SubagentList` with snapshots and limits.
- `subagent_get` returns one `SubagentState`.
- `subagent_interrupt` returns `{"previous_status":"..."}` and leaves the child
  reusable.

`subagent_ready` exposes the explicit readiness seam used by embedders, but
`snow --mode rpc` already readies restored topology before accepting commands.

## Event stream

After the `rpc_ready` handshake, frames other than `response` and
`prompt_completed` are normalized `protocol.AgentEvent` values.

| Category | Event types |
|---|---|
| Streaming | `text_delta`, `thinking_delta`, `usage` |
| Tools | `tool_start`, `tool_progress`, `tool_end`, `tool_routing` |
| Interaction | `user_input_request`, `queue_updated` |
| Lifecycle/state | `session_updated`, `turn_done`, `error`, `aborted`, `model_changed`, `mode_changed` |
| Plan | `plan_started`, `plan_delta`, `plan_completed`, `plan_update` |
| Compaction | `compaction_started`, `compaction_done` (`compaction.automatic` marks goal-triggered runs) |
| Goals | `thread_goal_updated` |
| Subagents | `subagent_started`, `subagent_status`, `subagent_message`, `subagent_activity` |

Possible payload fields:

```text
text message is_error

tool_call_id tool_name tool_output tool_duration_ms
tool_progress tool_routing

usage model mode plan plan_update compaction
user_input queue thread_goal

agent subagent agent_message
turn_id turn_origin turn_sequence root_epoch goal_continuing
```

Correlation rules:

- `agent` omitted: root-agent event, including ordinary prompts, goal continuation, and root state/lifecycle events;
- `agent` present: attributed child stream/tool/usage event;
- `subagent` present: child lifecycle snapshot;
- `agent_message` present: attributed mailbox event;
- `turn_sequence`: process-local monotonic admission order for correlated turn events (use `turn_id` as the stable identity; the sequence restarts with the process);
- `root_epoch`: process-local root session/branch reconciliation generation stamped on every root event, including events outside a turn;
- `tool_output`: bounded preview only; full results remain in session storage.

`permission_request` is part of the shared `AgentEvent` type, but RPC's
headless permission asker fails closed and does not emit it. `user_input_request`
is a separate model-question interaction and is emitted as documented above.

### Event payload quick reference

Fields not listed for an event are omitted unless they are one of the correlation
fields above. Nested objects use the public `pkg/protocol` JSON tags.

| Events | Primary payload |
|---|---|
| `text_delta`, `thinking_delta` | `text` |
| `tool_start` | `tool_call_id`, `tool_name` |
| `tool_progress` | `tool_progress: {tool_call_id,name,message?,done,is_error?}` |
| `tool_end` | `tool_call_id`, `tool_name`, `tool_output?`, `tool_duration_ms?`, `is_error?` |
| `tool_routing` | `tool_routing: {trigger,tool_ids?,candidate_count,selected_count,exposed_count,schema_bytes,latency_ms,fallback?}` |
| `usage` | `usage` object described below |
| `model_changed` | `model` (`id`, `provider`, capability and optional pricing metadata) |
| `mode_changed` | `mode: {mode,reasoning_effort}` |
| `plan_started`, `plan_delta`, `plan_completed` | `plan: {id,text?}`; delta text is in `text` where present |
| `plan_update` | `plan_update: {explanation?,plan:[{step,status}]}`; status is `pending`, `in_progress`, or `completed` |
| `compaction_started`, `compaction_done` | `compaction: {summarized_messages,retained_messages,summary?,used_fallback?,automatic?}` |
| `user_input_request` | `user_input` object from the interaction section above |
| `queue_updated` | `queue: {items:[{id,kind,text,order}]}` where `kind` is `steer` or `follow_up`; items are in submission order |
| `thread_goal_updated` | `thread_goal: {goal?,cleared?}`; `goal` uses the full shape below |
| `subagent_started`, `subagent_status` | `subagent` snapshot described below |
| `subagent_message` | `agent_message: {id,author,recipient,kind,content,trigger_turn?,created_at}` |
| `subagent_activity` | aggregate activity text in `message`, with related `agent`/`subagent` fields when available |
| `session_updated`, `turn_done`, `aborted` | correlation/state fields; `message` may provide a human-readable detail |
| `error` | `message`, normally `is_error:true` |

A `usage` object has `input`, `output`, optional `reasoning`, `cache_read`,
optional `cache_read_known`, `cache_write`, `total_tokens`, optional `requests`,
and optional `cost`. Cost is
`{currency?,input,output,cache_read,cache_write,total}`.

A full goal contains `session_id`, `branch_id`, `goal_id`, `objective`, `status`,
optional `token_budget`, `tokens_used`, `seconds_used`, optional
`estimated_costs`, `created_at`, and `updated_at`. Status is `active`, `paused`,
`blocked`, `usage_limited`, `budget_limited`, or `complete`. A cleared goal event
uses `thread_goal.cleared:true` with no goal.

A subagent snapshot contains `agent`, `status`, optional provider/model/thinking,
timestamps, bounded `result`/`error`, optional `usage`, and optional `generation`.
The nested agent reference contains `thread_id`, optional `parent_thread_id`,
canonical `path`/`parent_path`, optional role/nickname, and `depth`. Lifecycle
status is `pending_init`, `queued`, `running`, `interrupted`, `completed`,
`errored`, `shutdown`, `not_loaded`, or `not_found` where the command/event
permits it. `subagent_list` additionally returns `running`, `queued`, `terminal`,
`concurrent_limit`, `agent_limit`, and optional `truncated`.

Usage payloads keep `input` as the total prompt count, including cached tokens.
`cache_read_known: true` means the provider explicitly reported its cached-token
field, so `cache_read > 0` is a hit and `cache_read == 0` is a confirmed miss.
When `cache_read_known` is absent or false, zero is unknown rather than a miss.
For aggregate usage it is true only when every included provider request reported
the metric.

Clients should switch on `type` and tolerate new optional fields/event types for
forward compatibility.

## Prompt and response ordering

A typical sequence is:

```text
rpc_ready                              # first frame; validate version
mode_changed event                     # startup state
response(id=prompt-1, success=true)    # prompt admitted
text_delta / tool_* / usage events
queue_updated events                   # if steer/follow-up is admitted/delivered
turn_done event                        # agent lifecycle boundary
prompt_completed(request_id=prompt-1) # definitive RPC result
```

Important ordering rules:

- Prompt acknowledgement is admission, not completion.
- `turn_done` ends the agent turn; `prompt_completed` is the terminal RPC result.
- A failed prompt retains a legacy same-ID failure response immediately before
  its single `prompt_completed(status=failed)` frame.
- `subagent_wait` responses are asynchronous.
- Different command responses can arrive out of request order.
- Writes are frame-atomic, so a single JSON line is never mixed with another.
- EOF, cancellation, and scanner failures cancel and join prompt/wait workers
  before `Serve` returns; cleanup failures are returned to the host.
- Keep a response table keyed by ID, a prompt-terminal table keyed by
  `request_id`, and process agent events independently.

## Runnable Python and JavaScript clients

Typed, zero-runtime-dependency SDKs and runnable examples are exercised by
Linux/macOS CI against the fake provider:

```sh
go build -o ./snow ./cmd/snow
python3 examples/rpc/python/client.py --snow ./snow
node examples/rpc/javascript/client.mjs ./snow
```

See [Python and JavaScript/TypeScript SDKs](language-sdks.md) for the supported
high-level clients. The following low-level example shows the underlying framing,
including the distinction between `turn_done` and `prompt_completed`.

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
            raise RuntimeError(message.get("error") or legacy_error or message.get("status"))
        break

proc.stdin.close()  # EOF: orderly RPC shutdown
raise SystemExit(proc.wait())
```

This compact snippet demonstrates raw framing. Applications should normally use
the checked-in Python or JavaScript SDK, which validates the handshake, routes
out-of-order responses, bounds frames/queues, and waits for definitive prompt
completion.

Production clients should also:

- use an asynchronous reader independent from request submission;
- maintain ID-indexed promises/futures;
- bound client-side frames and logs;
- apply process/request deadlines;
- handle stderr separately;
- redact secrets and provider-sensitive payloads;
- tolerate events before the first request;
- reject prompt futures if the process exits before `prompt_completed`.

## Errors and shutdown

- Empty lines produce no output.
- Invalid JSON returns a failure response when stdout remains writable.
- Unknown command and validation errors do not terminate the server.
- Scanner errors, broken/short stdout writes, startup failures, or parent context
  cancellation terminate serving.
- EOF stops command admission, cancels RPC work/waits, releases user-input
  waiters, joins the active prompt and goroutines, then exits.
- There is no `shutdown` command. Close stdin, signal the process, or cancel the
  embedding context.

## Permission limitation

RPC has no permission request/reply handshake. In `ask` mode its headless asker
denies without emitting an interactive permission event. Use an explicit
headless policy:

- `--permission deny` for read-oriented operation;
- `--permission allow` only in an externally trusted/isolated environment.

`ask` fails closed in RPC. `user_input_reply` answers model questions and does
not authorize OS/tool access.

RPC, shell, plugins, stdio MCP servers, and subagents run with the current user's
OS privileges. Read the [Security model](security.md).

## Current RPC boundary

The current command surface covers prompts, active root input, cancellation,
active-provider and subagent model discovery, model/thinking/mode controls,
session inspection, model-requested input, goals, and subagents.

Branch management, compaction, reasoning-summary/text-verbosity controls,
configuration mutation, MCP/skill management, and login are currently
CLI/TUI/SDK concerns rather than RPC commands.
