# External plugin protocol v2

Snow's external plugin protocol is the language-neutral ABI for JavaScript,
Python, and other subprocess runtimes. It uses JSON-RPC 2.0 objects framed as
one UTF-8 JSON object per LF-terminated line on stdin/stdout.

This is separate from Snow's user-facing JSONL RPC mode and from MCP. Use MCP
for interoperable tools/resources/prompts; use this protocol for Snow-specific
tool registration, lifecycle, progress, and observation-only agent events.

## Process contract

Snow starts one persistent process per plugin declaration. The process remains
alive for the Snow session and may expose several tools.

- stdin receives host requests and notifications;
- stdout is reserved exclusively for JSON-RPC frames;
- stderr is drained into bounded diagnostics with best-effort redaction;
- every stdout frame must end with `\n`;
- request and response IDs are non-empty JSON strings;
- responses may complete out of order;
- notifications have no `id` and receive no response;
- malformed JSON, invalid JSON-RPC versions, oversized frames, EOF, or process
  exit fail the plugin and release pending calls.

Use stderr, `console.error`, or Python logging configured for stderr. A normal
`console.log()` or `print()` corrupts the stdout protocol stream.

## Lifecycle

```text
spawn process
  → initialize
  → tools/list
  → tools/call and notifications/event, zero or more times
  → shutdown
  → stdin closes and process exits
```

External startup failures are isolated and reported as plugin diagnostics. They
do not prevent Snow's core agent from starting.

## Configuration

```json
{
  "id": "my-tools",
  "command": ["/absolute/path/to/python", "-u", "/absolute/path/to/plugin.py"],
  "enabled": true,
  "cwd": "/optional/working/directory",
  "env": ["PATH=/usr/local/bin:/usr/bin:/bin"],
  "timeout_ms": 120000,
  "max_frame_bytes": 4194304,
  "max_input_bytes": 4194304,
  "max_output_bytes": 262144,
  "max_progress_bytes": 16384,
  "max_concurrent": 8,
  "capabilities": [],
  "config": {}
}
```

`command` is an argv array and is never passed through a shell. When the runtime
returns a manifest ID, it must match the configured plugin ID.

Snow intentionally supplies `env`; when omitted, the child receives an empty
environment. Entries must be unique literal `NAME=VALUE` assignments; invalid
or duplicate names are rejected. If `command[0]` has no path separator, Go
resolves it using Snow's launch environment before assigning the child
environment. The configured child `PATH` therefore affects plugin behavior and
child processes, not selection of the already resolved interpreter. Plugin
`env` entries do not expand `${VAR}`.

Prefer absolute interpreter/script paths. If a plugin needs certificate or
locale variables or child processes, provide a deliberately minimal environment
instead of inheriting every host credential. Never commit secrets in a manifest;
use a plugin-owned secure store or runtime-only `PluginSpec` injection.

Defaults:

| Limit | Default |
|---|---:|
| Frame | 4 MiB |
| Input | Frame limit |
| Tool output | 256 KiB |
| Progress message | 16 KiB |
| Concurrent calls | 8 |
| Event notification queue | 64 frames |

`timeout_ms` is optional. A zero value gives calls no plugin-specific deadline,
although the surrounding agent operation can still be cancelled. A positive
value also bounds the startup `initialize` and `tools/list` requests.

## `initialize`

Host request:

```json
{
  "jsonrpc": "2.0",
  "id": "1",
  "method": "initialize",
  "params": {
    "protocol_version": 2,
    "host_version": "snow-core",
    "cwd": "/effective/plugin/cwd",
    "session_id": "session-id",
    "host_capabilities": ["tools", "events"],
    "config": {}
  }
}
```

`host_version` is an opaque host value: normal app wiring currently sends
`snow-core`, while `plugin check` may send the binary build version.

Preferred result:

```json
{
  "jsonrpc": "2.0",
  "id": "1",
  "result": {
    "manifest": {
      "id": "my-tools",
      "name": "My tools",
      "version": "1.0.0",
      "protocol_version": 2,
      "capabilities": []
    },
    "capabilities": [],
    "supported_events": ["tool_end", "turn_done"],
    "limits": {}
  }
}
```

Small runtimes may return top-level `name`, `version`, and `protocol_version`
instead of `manifest`, but the explicit manifest form is preferred. For
protocol-v2 compatibility, Snow fills an omitted manifest ID from the configured
`PluginSpec.ID` and an omitted/zero protocol version with `2`. An explicitly
returned ID must match the configuration, and any nonzero protocol version must
be `2`. IDs use lowercase `[a-z0-9][a-z0-9_-]{0,63}`. Name and version remain
required.

`supported_events` is an explicit subscription. An omitted or empty list means
that the plugin receives no agent events. Event delivery is observation-only,
bounded, and best effort; it must never be used as a reliable transaction log.

The returned `limits` map is informational and appears in `snow plugin check`.
Host-enforced limits come from the plugin declaration.

## `tools/list`

Snow calls `tools/list` immediately after successful initialization:

```json
{"jsonrpc":"2.0","id":"2","method":"tools/list","params":{}}
```

Result:

```json
{
  "jsonrpc": "2.0",
  "id": "2",
  "result": {
    "tools": [
      {
        "name": "lookup",
        "description": "Look up a local record",
        "parameters": {
          "type": "object",
          "properties": {
            "id": {"type": "string"}
          },
          "required": ["id"],
          "additionalProperties": false
        },
        "risk": "read",
        "capabilities": ["records"],
        "discovery": {
          "mode": "deferred",
          "namespace": "records",
          "keywords": ["lookup", "record"]
        }
      }
    ]
  }
}
```

Tool names use lowercase `[a-z][a-z0-9_-]{0,127}` and must be unique inside the
plugin. Snow exposes them to the model as
`plugin_<plugin-id>_<tool-name>`.

`parameters` must be a non-null JSON object representing a JSON Schema.
`discovery` is optional; omitted tools are always exposed. Deferred tools remain
in the registry and are selected by Snow's local tool router.

### Risk metadata

`risk` is optional:

| Value | Intended operation |
|---|---|
| `read` | Read-only/local observation |
| `write` | Mutation |
| `exec` | Process or arbitrary execution |
| `network` | Network access |

Omitted risk defaults to `exec`. Invalid values reject plugin initialization.
Risk feeds Snow's central permission classification, but it is plugin-declared
metadata—not an OS-level restriction on the subprocess. Only trusted plugins
should receive a less restrictive classification.

Per-tool `capabilities` are retained descriptor/discovery metadata rather than
an independent authorization mechanism. They are combined with manifest,
initialization, and configuration capabilities.

For compatibility, `initialize` may also return `tools`. Snow uses that catalog
only when `tools/list.tools` is null or omitted; an explicitly empty array means
no tools.

## `tools/call`

```json
{
  "jsonrpc": "2.0",
  "id": "3",
  "method": "tools/call",
  "params": {
    "name": "lookup",
    "call_id": "provider-tool-call-id",
    "arguments": {"id": "42"},
    "timeout_ms": 119999,
    "cancellation": {"supported": true}
  }
}
```

- `name` is the original unqualified tool name.
- `call_id` identifies progress and agent events.
- `arguments` is the model-generated JSON value.
- `timeout_ms` is the remaining host deadline in milliseconds, or zero when no
  deadline exists.
- Calls can overlap up to the configured concurrency limit.

Successful tool result:

```json
{
  "jsonrpc": "2.0",
  "id": "3",
  "result": {
    "content": [
      {"type": "text", "text": "record 42"}
    ],
    "details": {
      "source": "local-cache"
    },
    "is_error": false
  }
}
```

`content` is sent to the model. Text content blocks are the portable default.
`details` is preserved as private host metadata and is not provider-facing.
`is_error: true` represents a structured tool error while still completing the
JSON-RPC request successfully.

If the encoded result exceeds the configured output limit, Snow replaces it
with a bounded error result.

## Progress

A plugin reports bounded progress with a notification:

```json
{
  "jsonrpc": "2.0",
  "method": "notifications/progress",
  "params": {
    "call_id": "provider-tool-call-id",
    "message": "Reading index",
    "done": false,
    "is_error": false
  }
}
```

Always include the originating `call_id`. Snow ignores progress with an empty
call ID, and language helpers should reject it before writing a frame.

## Cancellation

When Snow stops waiting for a call, it removes the pending host request and
sends a best-effort notification:

```json
{
  "jsonrpc": "2.0",
  "method": "notifications/cancelled",
  "params": {
    "call_id": "provider-tool-call-id",
    "request_id": "3",
    "reason": "context canceled"
  }
}
```

The runtime should cancel work by both request and call ID. Cancellation is
cooperative: synchronous CPU work or a blocking native call may not stop until
the process exits. A late result is ignored by Snow. Language helpers should
also enforce `tools/call.params.timeout_ms` locally because cancellation delivery
uses a bounded queue.

## Agent events

Snow sends only event types listed in `supported_events`:

```json
{
  "jsonrpc": "2.0",
  "method": "notifications/event",
  "params": {
    "version": 2,
    "type": "tool_end",
    "payload": {
      "type": "tool_end",
      "tool_call_id": "call-1",
      "tool_name": "read",
      "tool_output": "bounded preview"
    }
  }
}
```

Payloads are cloned, sanitized, and bounded. Event delivery has a bounded queue
and may be dropped when a plugin is slow. Event handlers cannot mutate, veto, or
reorder Snow's agent loop.

Common event names include:

```text
session_updated text_delta thinking_delta
tool_start tool_progress tool_end tool_routing
permission_request user_input_request usage turn_done
error aborted model_changed mode_changed
plan_started plan_delta plan_completed plan_update
compaction_started compaction_done thread_goal_updated queue_updated
subagent_started subagent_status subagent_message
```

Clients should ignore unknown future event types.

## Plugin diagnostics

A runtime may emit a bounded protocol log without writing directly to stderr:

```json
{
  "jsonrpc": "2.0",
  "method": "notifications/log",
  "params": {
    "severity": "info",
    "message": "cache initialized"
  }
}
```

Stderr and protocol logs are retained only up to the smaller of the configured
output limit and the default 256 KiB output limit; each individual protocol log
message is also bounded. Retained diagnostics pass through best-effort redaction
for common credential assignments, JSON fields, and authorization headers.
Redaction is defense in depth and cannot recognize every secret format. Plugins must never intentionally emit credentials.

## Errors

Normal JSON-RPC error response:

```json
{
  "jsonrpc": "2.0",
  "id": "3",
  "error": {
    "code": -32602,
    "message": "invalid arguments"
  }
}
```

Recommended codes:

| Code | Meaning |
|---:|---|
| `-32700` | Parse error |
| `-32600` | Invalid request |
| `-32601` | Unknown method |
| `-32602` | Invalid method or tool parameters |
| `-32603` | Internal JSON-RPC error |
| `-32000` | Bounded plugin/tool failure |

Do not include secrets, tracebacks, or unbounded third-party output in error
messages. Notifications never receive error responses.

## `shutdown`

```json
{"jsonrpc":"2.0","id":"4","method":"shutdown","params":{}}
```

The runtime should stop accepting calls, cancel or drain active work, send a
successful response, flush stdout, and exit. Snow then closes stdin and waits
within a bounded shutdown context before killing an unresponsive child.

Do not call Node's `process.exit()` before the response write completes. Python
should flush `sys.stdout.buffer` before returning.

## Validate a plugin

```sh
snow plugin check examples/plugins/javascript/manifest.json
snow plugin check /absolute/path/to/plugin-executable
snow plugin check examples/plugins/python/manifest.json --json
```

The command accepts a manifest or executable shorthand and has a 10-second
default overall timeout. It starts the runtime without creating an agent
session, validates the manifest and schemas, lists effective plugin/tool
capabilities, risks/discovery
modes and subscribed events, reports initialization time, prints bounded
diagnostics with best-effort redaction, then performs graceful shutdown.

See the dependency-free reference runtimes under `examples/plugins/` and the
[plugin overview](plugins.md).
