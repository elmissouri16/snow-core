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
- **stdout:** response objects and asynchronous `protocol.AgentEvent` objects
- **stderr:** startup/configuration diagnostics and process-level errors
- **framing:** UTF-8 JSON, exactly one object per LF (`\n`) line
- **maximum input line:** 16 MiB
- **blank lines:** ignored

Split only on the LF byte. Do not use Unicode line separators as frames.
Responses and events share one serialized writer, so bytes from different
objects never interleave. Object ordering is still asynchronous: a later command
may respond before an earlier prompt or `subagent_wait` completes.

RPC writes an initial collaboration-mode event and restored goal/subagent events
at startup. Clients must accept events before their first response.

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

Use a unique ID for every request. Events do not carry request IDs.

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

A client should route `type == "response"` by ID and route every other known
`type` through its event handler.

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

The server immediately returns a successful acknowledgement, then runs the root
prompt asynchronously while continuing to read stdin. Completion is signaled by
the event stream—normally `turn_done`—not by the acknowledgement. A later runtime
failure may produce a second `success:false` response with the same prompt ID.

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

Cancels admitted root work and clears undelivered queued input. The command is
acknowledged even when no prompt is active.

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

Values are `off`, `minimal`, `low`, `medium`, and `high`. The active model's
advertised capabilities are authoritative.

### `set_mode`

```json
{"id":"mode-1","type":"set_mode","mode":"plan"}
```

Use `default` or `plan`. Mode changes may be rejected while conflicting work is
active. Prefer an explicit value even though an omitted value normalizes to the
Default mode internally.

### `session_info`

```json
{"id":"info-1","type":"session_info"}
```

Successful `data` contains:

```json
{
  "session_id": "...",
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
    "token_budget": 20000
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

`path` is empty for `--no-session`; `goal` is omitted when none exists.

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
| `goal_resume` | none | Updated goal |
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
must be positive. Goal state also streams through `thread_goal_updated`.

## Subagent commands

Enable subagents with `--subagents` or configuration. See [Subagents](subagents.md)
for role, authority, persistence, and lifecycle details.

### `subagent_spawn`

```json
{
  "id": "agent-1",
  "type": "subagent_spawn",
  "params": {
    "task_name": "api_review",
    "message": "Review the public API for compatibility risks.",
    "agent_type": "explorer",
    "fork_turns": "all",
    "reasoning_effort": "low"
  }
}
```

Required: `task_name`, `message`. Optional: `agent_type`, `fork_turns`, `model`,
`reasoning_effort`. RPC task names are strict lowercase canonical segments;
hyphens are not normalized. Success returns `SubagentState`.

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

Every non-response stdout object is a normalized `protocol.AgentEvent`.

| Category | Event types |
|---|---|
| Streaming | `text_delta`, `thinking_delta`, `usage` |
| Tools | `tool_start`, `tool_progress`, `tool_end`, `tool_routing` |
| Interaction | `user_input_request`, `queue_updated` |
| Lifecycle/state | `session_updated`, `turn_done`, `error`, `aborted`, `model_changed`, `mode_changed` |
| Plan | `plan_started`, `plan_delta`, `plan_completed`, `plan_update` |
| Compaction | `compaction_started`, `compaction_done` |
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
turn_id turn_origin goal_continuing
```

Correlation rules:

- `agent` omitted: ordinary root event;
- `agent` present: attributed child stream/tool/usage event;
- `subagent` present: child lifecycle snapshot;
- `agent_message` present: attributed mailbox event;
- `tool_output`: bounded preview only; full results remain in session storage.

Clients should switch on `type` and tolerate new optional fields/event types for
forward compatibility.

## Prompt and response ordering

A typical sequence is:

```text
mode_changed event                     # startup, before requests
response(id=prompt-1, success=true)    # prompt accepted
text_delta / tool_* / usage events
queue_updated events                   # if steer/follow-up is admitted/delivered
turn_done event                        # root turn complete
```

Important ordering rules:

- Prompt acknowledgement is not completion.
- A prompt runtime failure can send a later failure response with the same ID.
- `subagent_wait` responses are asynchronous.
- Different command responses can arrive out of request order.
- Writes are frame-atomic, so a single JSON line is never mixed with another.
- EOF, cancellation, and scanner failures cancel and join prompt/wait workers
  before `Serve` returns; cleanup failures are returned to the host.
- Keep a response table keyed by ID and process events independently.

## Complete Python client

This example starts a persistent RPC process, sends one prompt, prints root text,
answers model questions, waits for `turn_done`, then closes stdin for orderly
shutdown.

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


send({
    "id": "prompt-1",
    "type": "prompt",
    "message": "Summarize this repository.",
})

for line in proc.stdout:
    message = json.loads(line)
    kind = message.get("type")

    if kind == "response":
        if not message.get("success"):
            print(message.get("error", "RPC error"), file=sys.stderr)
            if message.get("id") == "prompt-1":
                break
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
        break

proc.stdin.close()  # EOF: orderly RPC shutdown
raise SystemExit(proc.wait())
```

Production clients should also:

- use an asynchronous reader independent from request submission;
- maintain ID-indexed promises/futures;
- bound client-side frames and logs;
- apply process/request deadlines;
- handle stderr separately;
- redact secrets and provider-sensitive payloads;
- tolerate events before the first request;
- decide what to do if the process exits before `turn_done`.

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
model/thinking/mode controls, session inspection, model-requested input, goals,
and subagents. Branch management, compaction, configuration mutation, MCP/skill
management, and login are currently CLI/TUI/SDK concerns rather than RPC
commands.
