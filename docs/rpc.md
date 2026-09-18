# JSONL RPC

Snow RPC is a long-lived, bidirectional JSON-lines control plane for IDEs,
editor plugins, foreign-language hosts, and subprocess integrations. This
reference defines the wire framing, handshake, command surface, event stream,
ordering guarantees, error model, and shutdown semantics. Companion material
for model-requested input lives in [Model-requested user input](user-input.md).

> **Note:** This is a Snow-specific protocol with one JSON object per line,
> not JSON-RPC 2.0.

## On this page

- [Overview and framing](#overview-and-framing)
- [Run the server](#run-the-server)
- [Protocol handshake](#protocol-handshake)
- [Request and response envelopes](#request-and-response-envelopes)
- [Authentication commands](#authentication-commands)
- [Prompt commands](#prompt-commands)
- [Model and mode commands](#model-and-mode-commands)
- [Session commands](#session-commands)
- [Diagnostic capture commands](#diagnostic-capture-commands)
- [Permission interaction](#permission-interaction)
- [User input commands](#user-input-commands)
- [Goal commands](#goal-commands)
- [Subagent commands](#subagent-commands)
- [Event stream](#event-stream)
- [Event payload reference](#event-payload-reference)
- [Prompt and response ordering](#prompt-and-response-ordering)
- [Example client](#example-client)
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
- Inbound frames use Go's JSON v2 semantics: object member names are matched
  case-sensitively, and duplicate member names or invalid UTF-8 are rejected.
  Unknown members remain command-specific; only closed-schema handlers reject
  them.
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

### Go transport client

`pkg/agentclient/rpc` supplies a dependency-light client for an **already
connected**, owned `net.Conn`. It is separate from the embedded agent SDK and
imports only standard-library packages and `pkg/protocol`. `New(ctx, conn,
Options)` validates the handshake and starts a bounded reader. `Call` generates
request IDs and returns the first correlated `RPCResponse`; remote rejection
is `Success == false`, not a transport error. Consume `Events()` continuously
for normalized agent events and `RPCPromptCompleted` notifications. A prompt
acknowledgement is not completion.

Defaults are 16 MiB per encoded frame, 64 queued notifications, 64 pending
calls, and ten-second handshake/write limits. Callers must bound response
waits with contexts. Overflow terminates the client explicitly with
`ErrEventOverflow`; `Done`, `Err`, and `Close` expose termination. Canceling a
response wait does not abort remote work; canceling an in-progress write closes
the transport because a partial frame cannot be safely resumed. Never blindly
retry a possibly executed command.

The supplied connection must honor concurrent operations, deadlines and Close.
The client does not launch `snow`, supervise/reap processes, supply a stdio
adapter, implement browser authentication or enable a TCP RPC listener.
`pkg/agentclient/process` supplies the optional owned stdio adapter: `Start(ctx,
Options{Executable, Args, Dir, Env, RPC, ShutdownTimeout})` returns a worker with
`Client` and concurrent/idempotent `Close`. It executes a direct argument vector,
never a shell, and discards stderr. Nil Env inherits; a nonnil slice replaces it.
Cancellation, child exit, RPC termination or Close sends EOF, allows a bounded
shutdown grace (default one second), kills if necessary and reaps the direct
child. Final stdout draining is also bounded by that grace. This is not a
sandbox, process-group supervisor or detached-worker guarantee. Normal RPC
startup remains eager; callers explicitly select any catalog startup below.

### Runtime-free read-only catalog

```sh
cd /absolute/project
snow --mode rpc --rpc-startup catalog
```

This dedicated opt-in mode dispatches before runtime construction and loads no
configuration, credentials, instructions, MCP, plugins or providers. It accepts
only the root command and these two flags; execution/runtime flags are rejected.
It creates no session files or directories. `--rpc-startup eager` is the default
ordinary RPC behavior, not lazy activation.

The first frame is `rpc_ready` with `runtime_free_catalog`,
`catalog_sessions`, `catalog_messages`, `catalog_public_tools`, `catalog_image`,
and `history_images` capabilities. All other commands,
including prompts and mutations, return `unsupported`. Requests are:

```json
{"id":"list","type":"catalog_sessions","params":{"offset":0,"limit":25}}
{"id":"history","type":"catalog_messages","params":{"session_id":"saved-id","offset":0,"limit":25}}
```

Lists return `{sessions, offset, next_offset, has_more}` with path-free existing
session summaries. History returns `{messages, offset, next_offset, has_more}`;
messages contain only `id`, `role`, `text`, `timestamp`, `truncated`. The saved
current branch is projected chronologically, including exact pre-compaction
history. Only user/assistant text is returned; no thinking, tool calls/results,
image bytes, private metadata or provider continuity by default. User rows may
include public `images` metadata as described below. Text is untrusted display data.

When `catalog_public_tools` is advertised, `catalog_messages` accepts additive
`"include_tools": true`. Assistant rows can then contain `tools`, and the page
can contain `tools_truncated`. User/assistant offsets and empty-text assistant
rows keep their existing pagination meaning. Each public tool has `id`,
`owner_id`, optional `result_id`, `tool`, `status`, `output`, `output_available`,
and `truncated`. Stable presentation IDs derive from assistant identity and
tool-call ordinal, not reusable provider call IDs. Unique calls/results match
only within their owning assistant interval; duplicate identities, mismatched
names, missing results, and incompletely read intervals remain unresolved.
Statuses are `completed`, `failed`, or `unresolved`—never inferred running or
canceled. Output comes exclusively from persisted `public_tool_result`; legacy
or private output is unavailable, without fallback to raw tool content or
metadata. Output is inert untrusted text, not Markdown or HTML.
New synthetic interruption-recovery messages carry `tool_outcome_unknown: true`.
This explicit provenance overrides error/success flags and any preview: public
history keeps the call `unresolved`, with no `result_id` or available output.
These records balance provider-facing history without establishing whether an
external effect occurred. Existing unmarked records are not backfilled or
classified by inspecting private tool-message text.

Tool history keeps the most recent 64 calls per page, 128 bytes per name,
8 KiB output per tool and 128 KiB aggregate output; oversized identities are
rejected rather than shortened. Catalog tool decoding additionally allows 8 MiB
aggregate raw messages and 256 result rows. Following results at a page boundary
are read from the same verified branch; omitted interval data invalidates that
owner's definitive result projection. `tools_truncated` marks omissions, so an
unresolved display does not prove that no result exists in exact history. The
complete one-MiB encoded response bound still applies, including escaping.

When `history_images` is advertised, saved user rows additionally include optional
`images: [{"index": 1, "mime_type": "image/png"}]`. Indices refer to the original
content-block positions, not image ordinals; at most eight image descriptors
are returned per user, with indices bounded to 0–10,000. MIME labels are limited
to PNG, JPEG, GIF and WebP; unsupported declarations have an empty MIME label and
cannot be retrieved. No filename is inferred from text, tool labels or metadata.
Assistant/tool/provider-private images are never projected. Metadata selection
is independent of the text decode cap, so a large multipart message may have
truncated text while retaining its bounded image descriptors.

`catalog_image` is a separate explicit read, never a history/snapshot field:

```json
{"id":"image","type":"catalog_image","params":{"session_id":"saved-id","message_id":"user-id","index":1}}
```

Its result is `{session_id, message_id, index, mime_type, data}`, where `data` is
base64-encoded raster bytes. All three selectors are required; `index` must be
an explicit nonnegative integer. IDs are bounded to 4,096 UTF-8 bytes. Only an
exact visible current-branch user message in the same project may be read, using
the existing catalog root, lease and immutable-database guards. Turn aliases,
paths, URLs, SVG and private blocks are rejected. The selected image must have a
supported matching signature and declared MIME type, at most 2 MiB of raw data,
positive dimensions no larger than 16,384 per axis, and at most 40 million pixels.
Image dimensions are checked without decompressing a pixel buffer. The complete
encoded response is bounded to 4 MiB **only for this dedicated image command**;
ordinary catalog pages retain their 1 MiB limit. Errors return no partial bytes.

Only inactive supported SQLite sessions under this CWD's existing session layout
are visible. Active, unleased, recovery/WAL-dependent, corrupt/foreign/child,
empty nondurable and >64 MiB databases are omitted. Missing session roots return
an empty catalog without initialization. Reads require an existing safe lifetime
lease and immutable read-only SQLite; they never create or change files. Session
root selection remains `SNOW_SESSIONS_DIR` or `~/.snow/sessions`, independent of
`SNOW_HOME`.

Limits: 32 entries by default, 50 maximum, offset/traversal 10,000, inventory
4,096 filesystem entries, five-second operation context, four-MiB raw message
decode and 256 KiB aggregate display text. Encoded pages are capped at one MiB,
including JSON escaping; pages shorten and individual text truncation is explicit.
Pagination is best effort under concurrent inventory changes. The dedicated
`catalog-request.schema.json` and `catalog-output.schema.json` schemas describe
this mode; normal eager command/capability inventories intentionally exclude it.

## Runtime-free host control

`snow --mode rpc --rpc-startup control` starts a separate bounded, serial host
control loop, not an eager agent runtime. It does not construct an App, Agent,
new session, provider, tools, or extensions. Its baseline `rpc_ready` advertises
only `runtime_free_control`, `defaults_control`, and `provider_status`, with
`max_input_bytes:65536`. Responses are also bounded to 64 KiB. Optional
`host_api_key_control` appears only when its trusted service is composed.
A composed host-operation service adds `host_operations`,
`host_project_create_v1`, and `host_project_clone_v1`. Always inspect this handshake;
normal eager commands and capabilities are not a fallback in control mode.
A composed inactive-session deletion service adds `inactive_session_delete_v1`;
the CLI composes this service, while embedders can omit it.

Cold requests use only `id`, `type`, and command-specific `params`. Unknown
members, duplicate members, explicit null input values, and oversized frames
are rejected. Request IDs are at most 128 bytes; strings and params have further
command-specific bounds. The launch working directory is pinned on the host;
request parameters cannot select arbitrary filesystem authority. Dedicated
`control-request.schema.json` and `control-output.schema.json` roots describe
this mode. Control-only commands are intentionally excluded from the eager
`KnownRPCCommands()` inventory. The capability-gated `session_delete` command
shares the existing eager command's immutable-ID request/result shape, but not
its runtime or admission ownership.

### Optional inactive-session deletion

`inactive_session_delete_v1` permits exactly this explicit mutation:

```json
{"id":"delete-cold-1","type":"session_delete","params":{"session_id":"immutable-saved-session-id"}}
```

Success data is `{"session_id":"immutable-saved-session-id","deleted":true}`.
Only inactive sessions indexed under the pinned launch working directory can be
deleted; browser/client input never selects an on-disk path. Unknown parameters,
foreign IDs, changed folder identities, or a database held open by another
process are rejected. Existing database identity, exclusive ownership, child
session, and quarantine checks remain authoritative. Managed artifact and goal
data are cleaned through the same deletion helper used by the eager app.

This operation does not load configuration/auth, construct a runtime, contact
providers, create a replacement session, or infer deletion from an inventory
read. A failure after admission may reflect partial cleanup or an uncertain
outcome, not unchanged storage. Re-read and review before any new explicit
request; do not automatically retry it. Runtime-free **catalog** mode remains
read-only and rejects `session_delete`.

### Operator defaults and local provider status

| Command | `params` | Success `data` |
|---|---|---|
| `defaults_get` | `scope:"global"`, or `scope:"project"` and the exact launch `cwd` | Scoped defaults projection and opaque `revision` |
| `defaults_update` | Same scope/binding, required reviewed `revision`, and `global` or `project` patch | Updated scoped defaults projection |
| `provider_status_list` | None or `{}` | `providers` and `checked_locally:true` |

```json
{"id":"defaults-1","type":"defaults_get","params":{"scope":"global"}}
{"id":"defaults-2","type":"defaults_update","params":{"scope":"global","revision":"opaque-reviewed-revision","global":{"thinking":{"op":"set","value":"high"},"text_verbosity":{"op":"reset"}}}}
{"id":"status-1","type":"provider_status_list"}
```

Global defaults support `provider_model`, `thinking`, `reasoning_summary`, and
`text_verbosity`; project defaults support only `provider_model` and `thinking`.
`provider_model` is a pair with `provider` and `model`, not a provider-discovery
request. Each patch member is `{"op":"set","value":...}` or `{"op":"reset"}`;
reset must omit `value`, and omitted operations leave values unchanged. Each
returned default has nullable `explicit`, an `effective` value, and `source`
(`builtin`, `global`, or `project`). Opaque revisions fence concurrent changes,
including same-value updates. A conflict returns `revision_conflict`; inspect
again before making a new explicit change.

Responses carry `applies_to:"future_runtime"`. Updating host defaults does not
reconfigure an existing worker, even when that worker opens a new conversation.
It does not write project configuration files, activate a project, or grant
permissions.

Provider status records contain only `provider_id`, `state` (`configured`,
`expired`, or `unavailable`), a fixed `reason`, and `checked_locally:true`.
Reasons are `credential_missing`, `auth_store_unavailable`, `anonymous_access`,
`credential_invalid`, `credential_expired`, or `credential_present`. Local
inspection is not remote authentication: it performs no OAuth refresh,
provider request, model discovery, or credential export, and returns no account,
endpoint, environment, expiration, or header data.

### Optional write-only API-key control

`host_api_key_control` adds `api_key_inspect` and `api_key_set` for explicitly
trusted local transports. These are not legacy login or OAuth commands.

| Command | Required `params` |
|---|---|
| `api_key_inspect` | `provider_id` |
| `api_key_set` | `provider_id`, `expected_revision`, `secret`, explicit boolean `confirm_replace` |

Inspection and successful writes both return `provider_id`,
`api_key_supported`, `replace_required`, `revision`, `status`,
`checked_locally:true`, and `applies_to:"future_runtime"`. Revisions are
`missing` or 64 lowercase hexadecimal characters derived from auth-file
metadata, not credential contents. Writes require that exact reviewed revision;
replacement requires explicit confirmation when indicated. Only locally
configured API-key providers are eligible; ChatGPT OAuth is not an API-key
write target. The submitted credential is input-only: never log, echo, retain
in a public event, or put it into a URL or diagnostics. These commands are for
trusted same-user local stdio/control-RPC clients only and must never be exposed
through the Web Manager HTTP surface. The response never contains either the
old or new key. Existing workers are
not silently reloaded, and a lost acknowledgement must not trigger automatic
resubmission.

### Optional two-phase host project operations

`host_operations` adds explicit filesystem operations without activating an
agent or exposing an arbitrary command runner. The accompanying
`host_project_create_v1` and `host_project_clone_v1` capabilities identify the
explicit create/clone handlers; they do not certify Git executable or network
availability:

| Command | Required `params` | Success `data` |
|---|---|---|
| `project_prepare` | `operation_id`, reviewed `parent` identity, `leaf` | `operation_id`, `parent`, created `child` identity, `leaf` |
| `project_clone_start` | `operation_id`, exact prepared `child`, reviewed anonymous HTTPS `url` | Same prepared metadata, acknowledging gated execution |
| `project_cancel` | `operation_id` | `operation_id` acknowledging cancellation request |

These commands require nonempty correlated request IDs. Directory identities
contain canonical absolute `path` plus decimal-string `device` and `inode`
values; strings avoid JavaScript integer precision loss. `leaf` is one bounded,
non-special path component, not a path. Prepare creates the child without a
network request or process launch. The caller must durably record the returned
child identity before asking to clone. Clone execution is released only after
its acceptance frame has been successfully written.

Only anonymous HTTPS clone URLs are supported: no SSH, user information,
query, fragment, credential, shell option, or generic command. This restriction
is not a network sandbox. Completion is the separate `project_completed` frame
with `request_id`, `operation_id`, `child`, and `status`: `succeeded`, `failed`,
`canceled`, `timed_out`, `output_limit`, or `cleanup_failed`. It carries no Git
output, raw provider/filesystem errors, credentials, or PIDs. Cancel acceptance
is not proof of completed cleanup. A terminal result does not prove that the
pathname still names the original child: reconcile identity before registering
a project. Do not automatically replay prepare/clone after uncertain output.

## Protocol handshake

```json
{
  "type": "rpc_ready",
  "protocol_version": "1",
  "snow_version": "0.1.0-alpha.1",
  "capabilities": [
    "active_input",
    "authentication",
    "branch_management",
    "branch_versions",
    "compaction",
    "compaction_run",
    "context_report",
    "debug_diagnostics",
    "diagnostics",
    "goals",
    "history_control",
    "goal_run",
    "managed_processes",
    "managed_steer",
    "mcp_servers",
    "messages_list",
    "messages_page",
    "model_discovery",
    "models_list",
    "multimodal_prompts",
    "pending_inputs",
    "permission_interaction",
    "permission_mode",
    "process_control",
    "project_init",
    "project_trust",
    "prompt_completion",
    "response_controls",
    "session_forks",
    "session_info",
    "session_management",
    "session_model_selection",
    "session_reasoning",
    "settings",
    "skills",
    "subagent_messages",
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
| `provider` | string | No | Provider ID for authentication commands or a cross-provider `set_model` |
| `model` | string | No | Top-level model ID for `set_model` |
| `thinking` | string | No | Top-level effort for `set_thinking` or atomic `set_model` |
| `reasoning_summary` | string | No | Top-level provider summary preference for `set_reasoning_summary` |
| `text_verbosity` | string | No | Top-level provider verbosity preference for `set_text_verbosity` |
| `mode` | string | No | Top-level collaboration mode for `set_mode` or a prompt-attached mode |
| `params` | object | No | Command-specific JSON object |

Use a unique ID for every request. Ordinary agent events do not carry request
IDs. The public `protocol.RPCRequest`, `protocol.RPCResponse`,
`protocol.RPCReady`, `protocol.RPCPromptCompleted`, `protocol.RPCSessionInfo`,
the `protocol.RPCSession*` inventory DTOs, and model-list DTOs define the
stable Go wire representation. Canonical JSON
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

## Authentication commands

The `authentication` capability exposes the same provider-owned auth drivers
used by the CLI and TUI. Authentication inventory and polling responses are
secret-free. RPC never returns credential values, authorization headers,
refresh tokens, or API keys.

### Inventory

`auth_providers` takes no provider or params and returns deterministic provider
metadata plus local-only status inspection:

```json
{"id":"auth-list","type":"auth_providers"}
```

```json
{
  "id":"auth-list",
  "type":"response",
  "command":"auth_providers",
  "success":true,
  "data":{"providers":[{
    "provider_id":"chatgpt",
    "display_name":"ChatGPT/Codex",
    "required":true,
    "kinds":["oauth"],
    "environment":[],
    "methods":[{"id":"browser","display_name":"Browser OAuth","kind":"oauth"}],
    "status":{"provider_id":"chatgpt","state":"missing","summary":"not configured"}
  }]}
}
```

Status is `missing`, `configured`, `expired`, or `invalid`. `account_id`,
`expires_at` (Unix seconds), and `refreshable` are included only when known.
Environment entries are variable *names*, never values. Inventory is local and
does not refresh or contact a provider.

### Start and poll login

All logins are asynchronous so a device or browser flow never blocks the RPC
reader. Exactly one login job may run at a time; at most eight recent jobs are
retained. Start with `auth_login_start`, then poll `auth_login_status` using the
returned `job_id`. A terminal state is `completed`, `failed`, or `canceled`.
Each job retains at most 16 bounded progress items.

API keys use the dedicated top-level `secret` field:

```json
{"id":"login-1","type":"auth_login_start","provider":"opencode-go","method":"api_key","secret":"..."}
```

`secret` is write-only authentication input. Snow passes it directly to the
provider auth driver and existing atomic mode-`0600` credential store. It is
never copied to a response, event, diagnostic, or error. Hosts must apply the
same protections to their request buffers and logs.

OAuth login never accepts `secret`:

```json
{"id":"login-2","type":"auth_login_start","provider":"chatgpt","method":"device","params":{"allowed_workspace_ids":["workspace-id"]}}
{"id":"status-2","type":"auth_login_status","params":{"job_id":"auth-2"}}
```

Browser jobs return an `open_url` progress item. The host—not the Snow RPC
process—opens that URL. Device jobs return the provider verification URL and
`user_code`. OAuth authorization URLs are ephemeral trusted-interaction data;
hosts must not log or persist them. Browser login uses Snow's loopback callback
and does not support pasted callback URLs over RPC. If loopback browser login
is unavailable, start an explicit `device` job. EOF cancels and joins all auth
jobs before process exit.

Cancel a running job with:

```json
{"id":"cancel-2","type":"auth_login_cancel","params":{"job_id":"auth-2"}}
```

### OpenAI-compatible profiles

`auth_profile_set` persists one secret-free endpoint in `config.json`, creates
or replaces its runtime profile, and optionally stores its key separately in
`auth.json`:

```json
{"id":"profile-1","type":"auth_profile_set","provider":"x-provider","method":"api_key","secret":"...","params":{"profile_id":"x-provider","base_url":"https://gateway.example/v1"}}
```

`provider` and `params.profile_id` must match when both are present. The key is
optional, preserving an existing key or keyless access when omitted. Endpoint
URLs must be absolute HTTP(S) URLs and cannot contain URL userinfo, query
parameters, or fragments, preventing credentials from being persisted as
profile metadata. Profile setup uses the same asynchronous job/status protocol.

### Logout

```json
{"id":"logout-1","type":"auth_logout","provider":"chatgpt"}
```

Logout delegates to the canonical auth service, updates authoritative model
catalog visibility, and returns only the resulting safe status. It does not
accept `secret`, `method`, or params.

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
`status: "canceled"`. An explicit terminal provider abort also reports `canceled`,
even when the caller context is still live and the Go prompt call returns nil.
This uses synchronous evidence from that invocation, not a requested Abort,
delayed event, or historical assistant status. Persistence/accounting errors
still report `failed` unless the existing context-cancellation rules apply.
The same classification applies to Edit & resend and Regenerate completions;
it does not change the Go prompt error contract or automatic-goal behavior.
For compatibility, a failed prompt also retains the
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

### `queue_list`, `queue_enqueue`, `queue_update`, `queue_remove`

Workers advertising `queue_next` offer revision-checked follow-up controls in
addition to the existing `steer` / `follow_up` commands. They reuse the same
admitted root operation and agent loop. Follow-ups start only after a natural
reply stop; there is still one correlated `prompt_completed` for the whole run.

```json
{"id":"q-view","type":"queue_list","params":{"session_id":"session","turn_id":"admitted-root-id"}}
{"id":"q-add","type":"queue_enqueue","params":{"session_id":"session","turn_id":"admitted-root-id","revision":7,"text":"Then check the tests"}}
{"id":"q-edit","type":"queue_update","params":{"session_id":"session","turn_id":"admitted-root-id","revision":8,"item_id":"queued-item-id","text":"Then check only the affected tests"}}
{"id":"q-remove","type":"queue_remove","params":{"session_id":"session","turn_id":"admitted-root-id","revision":9,"item_id":"queued-item-id"}}
```

All commands bind the exact session and admitted root marker. Mutations require
the current revision; revisions advance across roots. `queue_list` is read-only.
Enqueue requires a durably admitted user-origin run, open queue admission and no
nonterminal goal. Original text must be nonempty NUL-free UTF-8, at most 64 KiB.
Pending and review items share an eight-item / 256 KiB aggregate limit.

Responses contain `QueueControl`: `session_id`, `turn_id`, `revision`,
`accepting`, `items`, `review_items` and an attributed `change`. Item states are
`pending`, `delivering`, `held` or `delivery_unknown`. Updates cannot alter an
item whose delivery has started. Remove can explicitly discard a retained
review item; it never reverses persisted input or tool effects.

Normalized queue updates distinguish durable delivery from removal, closure and
uncertainty. Successful delivery includes exact user-entry and input-span IDs;
reply identities come from successful assistant persistence, never nearby text
or a guessed branch tip. Queued inputs are durably paired with versioned span
metadata so exact-entry editing/regeneration remains available inside a multi-
input run without inventing new root turns or changing turn accounting.

`queue_rejected` means no mutation; `queue_stale` requires a fresh state read
before another explicit action. `queue_unknown`, lost acknowledgments and
ambiguous persistence outcomes are not safe to retry. A disappearing item alone
is never evidence that it was delivered. Accepted unsent controlled items become
bounded review-only state after cancellation/failure/limits and are not resumed
automatically. Reconcile retained work before changing sessions or starting a
new root operation. This live queue is not a durable restart scheduler.

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

Cancels admitted root work and clears legacy undelivered queued input. Controlled
`queue_next` items are instead retained for explicit review; they never restart
automatically. If goal work
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

### `models_discover`

```json
{"id":"discover-1","type":"models_discover"}
```

The `model_discovery` capability advertises explicit discovery across currently
available host providers. This calls the existing lazy provider-catalog facade
with a five-second child context, without changing the selected provider/model.
Unlike `models_list`, it is not limited to the active provider and can contact
provider services. It does not run automatically during catalog startup.

The successful `data` payload is `protocol.RPCModelDiscovery`:

```json
{"models":[],"partial":false,"truncated":false}
```

Results retain available catalogs if another provider fails or the discovery
budget expires (`partial:true`). At most 512 bounded model records are returned, within a 2 MiB encoded-result
budget that accounts for JSON escaping and reserves envelope overhead. Invalid
identities, oversized metadata and records exceeding that budget are
omitted/clipped with `truncated:true`. Provider error text, credentials and account inventory are not
returned. Runtime-free catalog mode rejects this command and omits its capability.

### `session_set_model`

```json
{"id":"session-model-1","type":"session_set_model","provider":"opencode-go","model":"kimi-k2.6"}
```

The `session_model_selection` capability advertises model selection for the
current conversation without rewriting host configuration or the operator-owned
project selection. Supply an exact provider/model pair from an available cached
catalog; call `models_discover` first to populate inactive-provider choices.
Selection uses the existing admitted provider/model/effort transaction and is
rejected while work is active or the pair is unavailable. Read `session_info`
afterward for the effective model and compatible thinking level. Catalog startup
rejects this mutation. The legacy `set_model` command below retains its durable
project-selection behavior.

### `set_model`

```json
{
  "id": "model-1",
  "type": "set_model",
  "provider": "opencode-go",
  "model": "kimi-k2.6",
  "thinking": "high"
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `set_model` |
| `provider` | string | No | Provider to activate; omission keeps the active provider |
| `model` | string | Yes | Model ID to activate |
| `thinking` | string | No | Compatible effort to activate atomically with the model |

Snow uses matching catalog metadata when available. Provider, model, and effort
are applied as one idle transaction and durably remembered for the active
working directory through the same operator-owned project selection used by the
TUI. When `thinking` is omitted and the selected model cannot use the current
effort, Snow safely resets it to `off`, matching the TUI. A change may be
rejected while work is active, when the provider is unavailable, or when the
requested settings are incompatible. Persistence failure rolls the live
selection back instead of leaving memory and disk inconsistent.

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
`ultra`. The active model's advertised capabilities are authoritative. A
successful change durably updates the provider/model/effort tuple for the active
working directory.

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
are rejected by the active model/provider configuration. Successful changes
atomically update the global operator configuration and are restored by later
Snow processes. Current normalized values are exposed as optional additive
fields in `session_info`.

## Runtime policy, process, and project commands

### Settings snapshot and persisted updates

The `settings` capability exposes the secret-free settings represented by the
TUI settings panel:

```json
{"id":"settings-1","type":"settings_get"}
{"id":"settings-2","type":"settings_update","params":{"provider":"opencode-go","model":"kimi-k2.6","thinking":"high","reasoning_summary":"concise","text_verbosity":"high","debug_enabled":true,"subagents_enabled":true,"subagents_max_concurrent":6,"skills_enabled":false,"update_check_on_startup":true}}
```

`settings_get` returns the current provider, model, thinking, reasoning-summary,
text-verbosity, permission, and debug values for display. It also returns
persisted `subagents_enabled`, `subagents_max_concurrent`,
`subagents_max_agents`, `skills_enabled`, and `update_check_on_startup` values.

The update field configures the global startup-check preference only. RPC
startup never performs a release check or binary replacement, and this settings
capability does not expose a remote install command. Every installation remains
an explicit interactive-TUI decision.

`settings_update` is partial and requires at least one supported field. Live
fields are `provider`, `model`, `thinking`, `reasoning_summary`,
`text_verbosity`, and `debug_enabled`; changing provider requires a model in the
same update. Provider/model/thinking are remembered for the active working
directory, while reasoning summary, text verbosity, and debug are global. The
dedicated `set_model`, `set_thinking`, `set_reasoning_summary`,
`set_text_verbosity`, `debug_enable`, and `debug_disable` commands use the same
durable path.

All mutations use the same locked, atomic, concurrent-writer-safe global
configuration update as the TUI and preserve unrelated configuration. A failed
write rolls back already-applied live values. Subagent and Skills settings still
cannot rewire the running managers, and `subagents_max_agents` is raised when
needed to keep it at least as large as concurrency. Responses therefore include
`subagents_restart_required`, `skills_restart_required`, and aggregate
`restart_required` booleans relative to the process startup configuration.
Provider credentials, auth configuration, headers, environment variables, and
other secret-bearing configuration are neither accepted here nor returned.

The presentation settings are part of the same snapshot and update path. The
`theme` field reports the normalized selected theme; `settings_update` accepts
it as a partial field, validates it against the current built-in and trusted
custom-theme catalog, persists it atomically, and applies it to subsequent
clients without requiring a Snow restart.

### Themes and keybindings

The `presentation_settings` capability exposes the same theme catalog and
keybinding layers used by the TUI:

```json
{"id":"themes-1","type":"themes_list"}
{"id":"keys-1","type":"keybindings_get"}
{"id":"keys-2","type":"keybindings_update","params":{"scope":"global","bindings":{"submit":["enter"]},"reset":["newline"]}}
```

`themes_list` returns `selected` plus bounded descriptors for Snow, Frost,
Ember, Aurora, and valid custom themes. Each descriptor includes `name`,
`display_name`, `scope` (`builtin`, `global`, or `project`), `extends`, and the
fully resolved adaptive semantic colors. Project themes are included only when
the launch-time project policy allowed trusted configuration. Invalid auxiliary
files remain warn-and-fallback diagnostics and are not exposed with filesystem
paths.

`keybindings_get` returns all 31 canonical actions in deterministic order. Each
action contains its global and project override lists, effective list, and
source (`default`, `global`, or `project`). `keybindings_update` requires a
`global` or `project` scope and at least one `bindings` replacement or `reset`.
A project update is rejected unless project configuration was trusted at
launch. Updates use the shared strict parser, collision checks, emergency-key
retention, locked atomic writes, and concurrent-writer-safe merge path. The
returned snapshot is the newly effective layered state. Responses never expose
configuration paths or file contents beyond these bounded presentation values.

### Permission mode

The `permission_mode` capability exposes the active session permission policy.
The getter takes no parameters, and the setter accepts exactly one normalized
mode:

```json
{"id":"permissions-1","type":"permission_mode_get"}
{"id":"permissions-2","type":"permission_mode_set","params":{"mode":"ask"}}
```

Both return `{"mode":"ask|allow|deny"}`. `permission_mode_set` uses the same
app facade as the TUI, updates the permission service immediately, and persists
the override with the active session. The launch baseline for a newly created
session is unchanged. `session_info.permission_mode` carries the same current
value as an additive field.

### Project trust

The `project_trust` capability provides the same canonical preflight decision
used at startup:

```json
{"id":"trust-1","type":"trust_get"}
{"id":"trust-2","type":"trust_set","params":{"level":"allow"}}
```

`level` is `ask`, `allow`, or `deny`. Both responses contain:

```json
{
  "path":"/canonical/project",
  "level":"allow",
  "prompt":false,
  "loaded":false,
  "restart_required":true
}
```

`trust_set` writes the canonical project decision atomically for the next
launch. It deliberately does **not** load or unload project configuration,
MCP declarations or other trust-gated input in the running process;
clients must restart when `restart_required` is true. Trust controls input
loading only. It is not a sandbox and does not reduce Snow's OS privileges.

### Managed-process inventory and logs

The `managed_processes` capability exposes the app-owned process fleet without
adding a second process manager or a process-control bypass:

```json
{"id":"processes-1","type":"processes_list"}
{"id":"logs-1","type":"process_logs","params":{"process_id":"proc-...","cursor":0,"max_bytes":32768}}
```

`processes_list` returns `{"processes":[...]}` with the same secret-free state
used by the TUI: `process_id`, `name`, `status`, timestamps, optional exit/signal
metadata, and readiness. `process_logs` returns one UTF-8-safe bounded page with
`process_id`, `status`, optional `output`, `next_cursor`, optional
`omitted_bytes`, and `eof`. `cursor` is optional and `max_bytes` is clamped by
the app's configured tool-output bound. IDs are process-local, opaque, and
invalidated when the session is rebound or Snow restarts. These RPC commands do
not start or stop processes; model-facing process tools retain their normal
permission checks.

### Session-bound managed-process controls

The additive `process_control` capability exposes `process_control_list`,
`process_control_logs`, and `process_control_stop`. These do not change the
legacy `managed_processes` commands above. Every request names the current
`session_id`; stale bindings and unknown handles are rejected. Process handles
are runtime-only opaque `proc_` identifiers followed by 32 lowercase hexadecimal
characters, never operating-system PIDs.

```json
{"id":"fleet-1","type":"process_control_list","params":{"session_id":"session-1"}}
{"id":"page-1","type":"process_control_logs","params":{"session_id":"session-1","process_id":"proc_0123456789abcdef0123456789abcdef","cursor":0,"max_bytes":32768}}
{"id":"stop-1","type":"process_control_stop","params":{"session_id":"session-1","process_id":"proc_0123456789abcdef0123456789abcdef","grace_ms":1000}}
```

| Command | Success `data` |
|---|---|
| `process_control_list` | `session_id`, `processes` (at most 128 records), `truncated` |
| `process_control_logs` | `session_id`, `process_id`, `status`, `output`, `next_cursor`, `omitted_bytes`, `eof` |
| `process_control_stop` | `session_id`, `process` (the updated public process state) |

A process record includes `process_id`, `name`, `status`, `started_at`, and
optional `finished_at`, `exit_code`, `signal`, `reason`, and `ready`. Status is
`running`, `stopped`, or `exited`. Inventory omits command lines, environment,
raw PIDs, and worker stderr. Log output is task output, not guaranteed free of
secrets: display it only to authorized readers.

Log `cursor` is an optional nonnegative byte offset; omission starts at the
beginning of retained output. `max_bytes` may be omitted or zero for the default,
or an integer from 4 through 32768. Pages preserve UTF-8 and report bytes lost
from retention through `omitted_bytes`. The JSON Schema character bound does
not replace the producer's UTF-8 byte bound. Stop `grace_ms` may be omitted or
an integer from 0 through 5000. Unknown parameter fields and explicit null
parameter values are rejected.

Stop is available only in Default collaboration mode and must pass current
`process_stop` execution permission and policy checks. This noninteractive
control does not create a permission question: unresolved `ask` fails closed,
and neither operator intent nor tool visibility overrides `deny` or Plan Mode.
No process-launch command exists in this namespace. A successful stop is a
normal correlated `response`, not a new agent turn. After a lost acknowledgement,
refresh inventory instead of automatically replaying a stop. Session switching
or restart invalidates old handles.

### Project initialization

`project_init` takes no parameters and starts the core-owned project
initialization prompt through the normal serial agent lifecycle:

```json
{"id":"init-1","type":"project_init"}
```

Admission produces a successful `response` correlated by `id` and with
`command:"project_init"`; terminal success, failure, or cancellation is
reported by the ordinary `prompt_completed` frame with the same `request_id`.
Streaming and permission events are unchanged. The command is rejected while a
turn is active and in Plan mode, and it never bypasses permission checks for
writes. The TUI `/init` command uses this same core prompt and lifecycle rather
than maintaining a surface-private copy.

## Session commands

### Independent session inventory and switching

When `session_management` is advertised, clients can manage durable sessions
for the RPC process's current working directory without receiving or supplying
database paths. Session IDs are immutable selectors; Snow resolves each ID
through its project-scoped session index and rechecks the opened database's
identity before switching or mutating it.

`sessions_list` and `session_create` take no parameters:

```json
{"id":"sessions-1","type":"sessions_list"}
```

```json
{"id":"create-1","type":"session_create"}
```

`sessions_list` returns `{"sessions":[...]}`. Each path-free summary contains
`session_id`, `name`, `created_at`, `updated_at`, `messages`, optional
`messages_capped`, and `active`. At most one listed durable session is
active; an ephemeral in-memory session is not listed. `session_create` creates a new
durable session, switches the running app through the normal session rebinding
path, and returns its active summary directly.

`session_open` and `session_delete` require an immutable ID:

```json
{"id":"open-1","type":"session_open","params":{"session_id":"..."}}
```

```json
{"id":"delete-1","type":"session_delete","params":{"session_id":"..."}}
```

`session_open` switches the process and returns the selected active summary.
It preserves all normal goal, permission, process, subagent, and provider-agent
session bindings. `session_delete` returns
`{"session_id":"...","deleted":true}` and rejects the active session; open or
create another session first. Unknown, foreign-project, corrupt, replaced, or
otherwise unsafe identities fail with `not_found`. Switching and active-session
mutation can fail with `session_busy` or `subagents_active` when admission
checks reject the operation.

### `session_rename`

```json
{
  "id": "rename-1",
  "type": "session_rename",
  "params": {
    "session_id": "optional-inactive-session-id",
    "name": "API cleanup"
  }
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | No | Correlation ID |
| `type` | string | Yes | Must be `session_rename` |
| `params.session_id` | string | No | Session to rename; omitted selects the active session |
| `params.name` | string | Yes | New display title, 1-72 runes and no control characters |

Changes the selected project session display title without changing its stable
ID, path, branches, or history. Inactive sessions are resolved by ID and remain
inactive. The response `data` contains `session_id` and the
normalized `name`. The command may be rejected while conflicting
root/subagent work is active.

### `message_edit_prepare` / `message_edit_commit`

Workers advertising `message_edit` support historical plain-text user editing
within the same session. Preparation is read-only:

```json
{"id":"edit-source","type":"message_edit_prepare","params":{"session_id":"current-session","entry_id":"persisted-user-id"}}
```

Supply exactly one of `entry_id` or `turn_id`. A turn selector must identify the
persisted user-origin turn marker and its unique directly owned user message;
local display IDs, text matching and caller-supplied parents are not selectors.
The response includes `edit_token`, `session_id`, `source_branch_id`,
`source_tip_id`, `entry_id`, `turn_id`, complete `text`, and Unix-millisecond
`expires_at`. Preparations expire after two minutes; the pool is bounded to 64.
Sources must be on the active path with a verified safe retained prefix.
Edit-history reads are bounded to 100,000 entries and 32 MiB, with cancellation
and preflight checks before whole-path decoding or cloning. Unsupported stores
have no unbounded fallback. Attachments, transformed/internal inputs, active goals/subagents and recovered
queued input are not supported by this initial operation.

Explicitly commit once:

```json
{"id":"edit-run","type":"message_edit_commit","params":{"session_id":"current-session","edit_token":"prepared-token","text":"Revised request"}}
```

Both original and replacement input must be nonempty valid UTF-8, at most 64 KiB.
The single-use token is revalidated against the same session, branch, tip and
source. Unknown fields are rejected. Input validation and plugin hooks precede
branch activation, and the replacement turn is claimed under the same admission
lock. The original append-only history remains on the former branch; the new
active path retains the prefix, replaces the selected user and omits its suffix.
No new session/chat is created, the session-wide title is retained, and prior tool
side effects are not undone.

Unlike ordinary prompt admission, successful edit ACK data follows durable
replacement input and precedes replacement provider execution. It contains the
source identity fields, new `branch_id`, `turn_id`, `user_entry_id`, and a bounded
public `history` page ending with the replacement. `history.start > 0` means an
older prefix was omitted by the response bound. Replace the public projection
rather than appending this page to the old conversation. Buffer bounded incoming
events across the acknowledgment and retire previous turn/instance authority.
Completion remains the normal correlated `prompt_completed` lifecycle.

Only `error_code: "message_edit_rejected"` proves that no replacement was committed
and any attempted branch transition was restored. `message_edit_unknown`, a
transport/write failure, or an unrecognized failure code must not be interpreted
as unchanged history. Once replacement input may be durable, keep its branch and
reconcile explicitly; never automatically replay the request. A provider failure
following a successful ACK leaves the new continuation active.

### `message_regenerate_prepare` / `message_regenerate_commit`

Workers advertising `message_regenerate` can restart a completed reply from its
original user prompt. The read-only preparation selects an assistant, not a user:

```json
{"id":"regen-source","type":"message_regenerate_prepare","params":{"session_id":"current-session","entry_id":"persisted-assistant-id"}}
```

Supply exactly one of `entry_id` or `turn_id`. An entry must be the final terminal,
text-bearing assistant reply of its exact persisted user-origin turn. A turn
selector identifies that same final reply. Tool prefaces, plans, private-only
output, ambiguous multi-user turns and unsafe or unsupported original prompts are
rejected. Selection uses the bounded active-path history reader, not display
positions, adjacent user text or a client-supplied prompt.

The response extends `RPCMessageEditPrepared` with `reply_entry_id`; its inherited
`entry_id` identifies the owning user and `text` is the unchanged original prompt.
The token is action-bound, single-use and shares editing's two-minute expiry and
64-token pool. Editing and regeneration tokens cannot be exchanged.

Commit without a `text` field:

```json
{"id":"regen-run","type":"message_regenerate_commit","params":{"session_id":"current-session","edit_token":"prepared-regeneration-token"}}
```

The server revalidates the reply and uses its original server-held user input.
This reuses editing's atomic history transition/admission, public-history ACK,
correlated `prompt_completed`, and `message_edit_rejected` / `message_edit_unknown`
outcome distinction. It restarts the **whole owning turn**, including earlier text
and tool work, rather than only rewriting its final text fragment. The new active
path contains one unchanged-text user prompt and the new continuation; the former
path remains internally retained in the same session. Tools may run again under
the current permission policy, and prior side effects are not undone. Do not
convert a failed regeneration into a normal prompt or automatically replay it.

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

### Branch versions and restore

The optional `branch_versions` capability exposes bounded, public saved-history
inspection and an explicit prepare/commit restore flow. `branches_page` binds to
an exact `session_id`, accepts an opaque cursor, and returns at most 100 branch
versions per page. `branch_messages_page` binds to an exact `session_id`,
`branch_id`, and saved `tip_id`, accepts an opaque cursor, and returns at most 64
messages per page. Its public projection may include bounded `history_tools` and
`history_tools_truncated`; it never exposes raw session storage or
provider-private continuity data.

Prepare a restore with the exact source and target branch-tip pairs:

```json
{"id":"restore-prepare-1","type":"branch_restore_prepare","params":{"session_id":"current-session","source_branch_id":"main","source_tip_id":"source-tip","target_branch_id":"experiment","target_tip_id":"target-tip"}}
```

Preparation performs no restore or execution. It returns an opaque, single-use
`restore_token` that expires within two minutes. Commit it without resending or
altering the reviewed branch identities:

```json
{"id":"restore-commit-1","type":"branch_restore_commit","params":{"session_id":"current-session","restore_token":"prepared-restore-token"}}
```

Commit rechecks both source and target branch-tip pairs with compare-and-swap
semantics. The runtime must remain idle. Pending permission or user input,
retained queue/review work, and a nonterminal goal reject restoration. Success
selects the target branch's append-only saved history and applies that branch's
saved collaboration mode while preserving current provider, model, permission,
and related runtime authority. It performs no provider request, goal run,
ordinary prompt, tool execution, provider replay, or filesystem rollback.

`branch_restore_rejected` is a definitive admission or compare-and-swap
rejection. `branch_restore_unknown` means the mutation outcome cannot safely be
established. Clients must not automatically retry an unknown commit or reuse a
consumed token.

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

### `context`

```json
{"id":"context-1","type":"context"}
```

Returns a secret-safe estimate of provider-facing input for the active branch.
`latest_request` is true when the report describes the most recently sent
provider request and false when it projects the next request. `categories`
contains only `name`, `bytes`, `estimated_tokens`, and `items` counts. The
remaining fields report aggregate estimated input, fixed-context usage and
budget, message/tool counts, the model context window, and optional aggregate
`usage`. Prompt text, tool arguments/results, provider-private continuity data,
credentials, and instruction contents are never returned.

The `context_report` capability advertises this command. The initial projected
scan is serialized with prompt/session/branch admission; cached latest-request
reports remain count-only. The command accepts no parameters.

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

The following inspection commands are available. All rows except
`messages_page` take no parameters:

| Command | Success `data` | Notes |
|---|---|---|
| `messages_list` | `{"messages":[...]}` | Compatibility snapshot of linearized active-branch history; provider-private continuity blocks are omitted, but a large session can exceed a bounded client frame |
| `messages_page` | bounded page object described below | Cursor-based stable snapshot for hydrating history without a frame larger than 16 MiB |
| `usage` | usage object | Aggregate token, cache, request, and optional cost totals for the active branch |
| `pending_inputs` | `{"items":[...]}` | Submission-ordered queued `steer` and `follow_up` input |
| `pending_inputs_clear` | `{"items":[...]}` | Atomically removes and returns undelivered queued input |
| `diagnostics` | `{"diagnostics":[...]}` | Non-fatal configuration warnings with `path` and `message` |
| `mcp_servers` | `{"servers":[...]}` | Secret-free negotiated MCP server status (no credentials, headers, or argv) |
| `skills` | `{"skills":[...],"diagnostics":[...]}` | Full skill catalog plus discovery diagnostics |
| `skills_clear` | `{"cleared":N,"catalog":{"skills":[...],"diagnostics":[...]}}` | Durably deactivates all branch-active skills at an idle admission boundary; does not delete files or change global configuration |


`messages_page` accepts optional `params`:

```json
{"id":"history-1","type":"messages_page","params":{"limit":32,"max_bytes":2097152}}
```

`limit` defaults to 32 and is restricted to 1 through 128 messages.
`max_bytes` defaults to 2 MiB and is restricted to 64 KiB through 15 MiB plus
960 KiB (`16711680` bytes). It is a preferred whole-response frame budget. If
one durable message is larger than that preference, Snow returns that message
alone to guarantee progress, but never emits a `messages_page` frame at or above
the fixed 16 MiB transport limit. An individual history entry that cannot fit
the hard page bound is rejected instead of producing a truncated or unreadable
frame.

A successful page is ordered and shaped as follows:

```json
{
  "messages": [],
  "next_cursor": "opaque-server-value",
  "start": 0,
  "total": 250,
  "has_more": true
}
```

`start` is the zero-based position in the snapshot and `total` is fixed by the
first request. When `has_more` is true, clients send `next_cursor` unchanged as
`params.cursor`; the next page must start at the previous `start +
messages.length`. A terminal page has `has_more:false`, omits `next_cursor`, and
ends exactly at `total`. Cursors are opaque, bounded, and tied to the active
session-branch snapshot. Appending messages while paging does not move the
snapshot end. Switching sessions or branches invalidates the cursor rather than
mixing histories.

Pages preserve the exact append-only message order, IDs, `parent_id` ancestry,
and content blocks. The initial snapshot ends at the latest position with no
unmatched tool call, so an in-flight call is deferred until a later hydration
rather than exposed without its result. A complete tool call and its result can
still fall on adjacent pages, so a client that presents a hydrated transcript
should concatenate and validate all pages before publishing the replacement
history. The complete concatenation retains tool call/result pairing. Every
page applies the same public projection
as `messages_list`: provider-private continuity data is removed before cursoring
and never appears in cursor data or responses. `messages_list` remains available
for compatible clients and small snapshots; bounded clients should prefer the
`messages_page` capability.

Workers advertising `messages_public_history` also accept
`"public_history": true` in `messages_page` params. This opt-in allowlists
identities, explicit user/assistant text and plans, tool-call identity/name, and
explicit public-result metadata. It excludes arguments, images, thinking,
provider continuity, plugin/display metadata, and raw tool-result content.
Assistant lifecycle metadata retains recognized `stop_reason` values and a safe
`is_error` bit, never raw error text. A terminal claim is withheld if dropping
unsupported original content or tool metadata would falsely promote the reply
to regeneration eligibility. This metadata is included in the page byte budget;
source ownership still requires authoritative regeneration preparation.
Unlike legacy paired-snapshot mode, it includes trailing assistant calls without
results. `history_tools` maps owning assistant IDs to the bounded public tool
DTO described above; `history_tools_truncated` marks omissions. An empty map may
be omitted: clients must treat it as authoritative rather than infer results
from the partial message page. Projection looks ahead through the final owner's
complete result interval within the cursor snapshot, without crossing a new
user/assistant boundary. This prevents a page boundary from inventing an
unresolved result that exists immediately afterward. Cursor mode is bound;
switching between public and legacy mode requires starting a new snapshot.
Both modes retain the existing complete-frame/escaping/LF limits and default
legacy behavior remains unchanged.

### Read a live worker's durable user image

Workers advertising `history_images` add `history_images` to
`messages_page` with `public_history:true`: a map keyed by durable user message
ID, containing the same bounded `{index,mime_type}` descriptors as the catalog.
These descriptors are derived from original user blocks before public-history
projection removes image content. The map carries no bytes, labels or filenames
and is included in the normal page wire budget. Missing metadata must not
trigger a raw-message fallback. Legacy non-public history behavior is unchanged.
Clients must capability-check these additive features before using strict older
workers; no new parameters are required on ordinary history requests.

Workers advertising `message_image` accept either exact durable message identity
or the persisted user-origin turn identity returned by prompt acceptance:

```json
{"id":"image","type":"message_image","params":{"session_id":"active-id","message_id":"user-id","index":1}}
{"id":"image","type":"message_image","params":{"session_id":"active-id","turn_id":"accepted-turn-id","index":1}}
```

Exactly one of `message_id` and `turn_id` is required. Turn resolution requires
one exact adjacent user in that persisted root input span; follow-up spans,
ambiguous users, other branches and client-invented IDs are not aliases. Both
forms return `{session_id,message_id,index,mime_type,data}` with the resolved
durable message ID. The same raster/ID/index/2 MiB data/4 MiB frame limits apply.
The live getter reads only its worker-owned session under admission, never
through the inactive catalog, and starts no runtime/provider work or mutation.
Reads are cancellation-aware, time-bounded and use bounded stored history.
At most four `message_image` reads may be outstanding per worker. They dispatch
asynchronously so admission/history waits do not block `abort` or interaction
replies; responses may arrive out of request order and must be matched by ID.
Excess concurrent reads fail immediately without queueing. EOF, cancellation
and transport failure cancel outstanding reads, and worker shutdown joins them.
General public snapshots/pages still contain no image bytes. Image metadata does
not grant edit/reuse eligibility: historical text editing retains its existing
original-message, text-only authoritative checks.

## Diagnostic capture commands

The `debug_diagnostics` capability controls the shared bounded recorder.
`debug_enable` and `debug_disable` atomically persist `debug.enabled` before
changing live capture; status, clear, and dump do not rewrite configuration:

| Command | Params | Success `data` |
|---|---|---|
| `debug_status` | none | `enabled`, `started_at` when enabled, retained `event_count`/`retained_bytes`, `dropped_events`, and recorder limits |
| `debug_enable` | none | Updated status after enabling capture |
| `debug_disable` | none | Updated status after disabling new capture; retained records remain |
| `debug_clear` | none | Updated status after flushing and clearing retained records/drop counters |
| `debug_dump` | optional `{"path":"..."}` | Resolved absolute `path` plus a sharing `warning` |

A blank or omitted dump path creates a unique file under
`$SNOW_HOME/diagnostics`; a relative path resolves against the runtime working
directory. Dump creation fails while the root agent is running so the file uses
a stable turn boundary. Capture callbacks never block the ordered event
dispatcher; bounded losses appear in `dropped_events`.

Dumps are private, atomic, encoded `snow-diagnostic-v1` JSON files capped at
256 MiB. They intentionally preserve full prompt, response, thinking, tool,
path, error, and active-session content. Snow completely omits
`provider_data` and redacts known credentials and configured secret-bearing
transport fields, but unknown sensitive data may remain. Review every dump
before sharing. See [Security model](security.md#protect-credentials-and-diagnostics).

## Permission interaction

Ask-mode permission requests are published as `permission_request` events with
a stable, host-facing `id`. Bash requests additionally include bounded static
effects, capabilities, resolved paths, uncertainty, and whether the request may
be remembered:

```json
{"type":"permission_request","permission":{"request":{"id":"perm-3","tool":"bash","risk":"exec","paths":["/tmp/input.json"],"effects":[{"type":"filesystem","capability":"filesystem.read.external","operation":"read","resource":"/tmp/input.json","confidence":"high"}],"capabilities":["filesystem.read.external","process.exec"],"rememberable":true,"scope_label":"matching effects and resources in this workspace"}}}
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
reads never ask, and allow/deny modes never consult a broker. Raw RPC `ask`
deliberately enables manual replies and blocks until the trusted host resolves
the request, cancels the prompt, or closes the transport. `allow_session` and
`allow_always` are remembered for the remainder of the session using the
request's scoped identity. For analyzed Bash, legacy broad `bash|exec` allows
are ignored and requests with `rememberable:false` reduce a session-like reply
to one-time approval. The opaque internal scope hash is never published. Hosts
must treat `effects_truncated`, `capabilities_truncated`, or `paths_truncated`
as a signal to review the raw command because the bounded public projection is
incomplete. `permission_interaction` capability gates these commands.

`messages_list` and `messages_page` can include user and assistant text, images,
thinking summaries, tool calls, tool results, and surface-safe tool-display
metadata. Neither emits opaque `provider_data` continuity blocks. Tool output
and queued input can contain project/user data; clients must apply their own
display and storage policy. Arrays are encoded as `[]`, not `null`.

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

### Explicit goal inspection and owned runs

The additive `goal_run` capability exposes `goal_inspect` and `goal_run` without
changing the legacy `goals` commands above. Both require a nonempty request `id`
and strict `params`; unrelated envelope fields are rejected.

`goal_inspect` reads the active branch's authoritative projection. Supply
`session_id` and optionally `branch_id`; omitting the branch selects the current
active branch, not an arbitrary saved branch. A supplied binding must match.
Inspection never starts work or clears a deferred goal.

```json
{"id":"inspect-1","type":"goal_inspect","params":{"session_id":"session-1","branch_id":"main"}}
{"id":"inspect-1","type":"response","command":"goal_inspect","success":true,"data":{"session_id":"session-1","branch_id":"main","tip_id":"tip-1","goal":null,"deferred":false}}
```

Inspection always returns `session_id`, `branch_id`, `tip_id`, `goal` (a
`ThreadGoal` or explicit `null`), and `deferred`. Optional `budget_remaining`
is a nonnegative integer, present only for a budgeted goal; omission means no
token-budget limit, not zero remaining tokens. Optional `goal_run_id` identifies
an owned run currently in progress.

`goal_run` explicitly creates or resumes one serial owned run against the exact
reviewed session, branch, tip, and goal identity:

```json
{"id":"run-1","type":"goal_run","params":{"action":"create","session_id":"session-1","branch_id":"main","expected_tip_id":"tip-1","expected_goal_id":"","objective":"Ship and verify the parser","token_budget":20000}}
{"id":"run-1","type":"response","command":"goal_run","success":true,"data":{"goal_run_id":"run-opaque","goal_id":"goal-1","session_id":"session-1","branch_id":"main"}}
{"type":"text_delta","text":"Inspecting the parser","goal_run_id":"run-opaque"}
{"type":"goal_run_completed","request_id":"run-1","goal_run_id":"run-opaque","goal_id":"goal-1","status":"finished","goal_status":"paused"}
```

| Parameter | Contract |
|---|---|
| `action` | Required: `create` or `resume` |
| `session_id`, `branch_id` | Required nonempty current binding |
| `expected_tip_id` | Required exact current tip; empty explicitly asserts an empty tip |
| `expected_goal_id` | Required exact current goal ID; empty explicitly asserts no goal, never “whichever goal exists” |
| `objective` | Required for create, nonblank and at most 32 Ki Unicode characters; forbidden for resume |
| `token_budget` | Optional positive integer for create; omit for unlimited (nil in the Go DTO); forbidden for resume |

Create cannot replace an unfinished goal. Resume requires a nonterminal goal
with remaining budget when budgeted; an active goal must have been deferred for
review. Neither action bypasses admission, collaboration-mode, permission,
active-turn, or active-subagent checks. Refresh and review changed bindings
rather than silently substituting the latest tip or goal identity.

```json
{"id":"run-2","type":"goal_run","params":{"action":"resume","session_id":"session-1","branch_id":"main","expected_tip_id":"tip-2","expected_goal_id":"goal-1"}}
```

The success response acknowledges acceptance, **not completion**. Native agent
events from the owned run carry optional `goal_run_id`; use it to distinguish
this execution from other activity. `goal_run_completed` carries required
`type`, `request_id`, `goal_run_id`, `goal_id`, and `status`, with optional
`goal_status` and `error`. Execution status is `finished`, `failed`, or
`canceled`; `finished` does not mean the objective was achieved. The separate
`goal_status` remains one of `active`, `paused`, `blocked`, `usage_limited`,
`budget_limited`, or `complete`. This terminal envelope is distinct from
`prompt_completed`; `abort` cancels the active owned run through the shared
lifecycle.

A rejected admission reports `error_code:"goal_run_rejected"` before mutation.
An ambiguous goal write or acceptance result reports
`error_code:"goal_run_unknown"` when a response can still be delivered.
Do not automatically retry create/resume after a lost acknowledgement or an
uncertain mutation result. Reconnect, inspect authoritative goal and branch
state, and obtain a fresh explicit action. Merely inspecting or reconnecting
must not be treated as permission to resume a deferred goal.

The machine-readable contracts are in `goal-run.schema.json` and
`process-control.schema.json`, referenced by the normal request/response/output
schema roots under `pkg/protocol/schema/rpc/v1/`.

## Explicit managed runtime controls

These additive commands use a strict envelope containing exactly `id`, `type`,
and `params`. A nonempty correlated ID is required. Compare-and-swap fields
must be present even when their asserted value is empty; omitted tips are not
wildcards. Unknown params and unrelated legacy root fields are rejected from
the original JSON frame, including explicitly empty extras. These controls do
not weaken serial admission, collaboration mode, or permission boundaries.

### History control

`history_control` adds `history_branch_fork`, `history_session_fork`, and
`history_branch_rename`. All require `session_id`, `source_branch_id`,
`source_tip_id`, `target_branch_id`, `target_tip_id`, and `name`. Rename also
requires the reviewed `old_name`. Source identifies the active cursor; target
identifies an exact saved branch tip, never an arbitrary historical message.
The source and target may be the same branch. Tip assertions may explicitly be
empty. Names are bounded to 256 UTF-8 bytes by the producer.

```json
{"id":"fork-1","type":"history_branch_fork","params":{"session_id":"session-1","source_branch_id":"main","source_tip_id":"tip-1","target_branch_id":"main","target_tip_id":"tip-1","name":"Alternative"}}
```

Branch fork returns metadata only: `session_id`, `branch_id`, `tip_id`,
`branch`, `mode`, `reasoning_effort`, and `root_epoch`. Rename returns
`session_id`, `branch_id`, `tip_id`, `branch`, and `root_epoch`. Reload messages
through `branch_messages_page` and its public projection, not raw serialized
history. Session fork returns the new detached `session_id`, `name`, `branch`,
`source_session_id`, `source_branch_id`, `source_tip_id`, `mode`, and the
unchanged parent `root_epoch`. It allocates and closes a detached child; it has
no destination-path, worktree, or activate option. It does not switch the live
parent to the child.

Read-only admission failures report `history_control_rejected`; uncertain
mutations or acknowledgements report `history_control_unknown`. Refresh
history and identities after uncertainty instead of automatically retrying.

### Owned compaction

`compaction_run` adds `compaction_start` with required `session_id`, `branch_id`,
and `expected_tip_id`. It compares the exact reviewed idle branch, including an
explicit empty-tip assertion, before reserving a serial operation.

```json
{"id":"compact-1","type":"compaction_start","params":{"session_id":"session-1","branch_id":"main","expected_tip_id":"tip-1"}}
```

The accepted response contains `compaction_id`, `session_id`, `branch_id`,
`turn_id`, `turn_origin:"compact"`, `root_epoch`, and `turn_sequence`. Acceptance
precedes provider execution. The terminal `compaction_completed` frame carries
these same identities plus `type`, `request_id`, `status`,
`summarized_messages`, `retained_messages`, and `used_fallback`. Status is
`completed`, `noop`, `fallback`, `canceled`, or `failed`. Native
`compaction_started`/`compaction_done` events are progress, not the terminal
execution boundary. The terminal frame contains no summary or private provider
error. Cancellation or failure can still incur usage or save a marker; refresh
authoritative history before a new explicit action. Failures distinguish
`compaction_rejected` from `compaction_unknown`; neither lost acknowledgement
nor reconnect authorizes automatic replay.

### Managed native steering

`managed_steer` adds a command of the same name with required `session_id`,
`turn_id`, `root_epoch`, `request_id`, and literal `text`. It admits input only
for that exact running ordinary-user root, not an owned goal or compaction run.
The text is nonblank NUL-free UTF-8 of at most 64 KiB. Steering shares the
manager queue budget (eight items and 256 KiB total), including retained review
work; it does not use a second scheduler or `queue_enqueue` fallback.

```json
{"id":"steer-rpc-1","type":"managed_steer","params":{"session_id":"session-1","turn_id":"turn-1","root_epoch":4,"request_id":"steer-1","text":"Keep the public API unchanged"}}
```

Success contains `session_id`, `turn_id`, `root_epoch`, `request_id`, native
`item_id`, and `status:"accepted"`. Admission is **not delivery**. The
`queue_control.change` projection in `queue_updated` reports `steer_accepted`,
`delivered`, or `discarded` with that native `item_id`. Disappearance alone
proves neither delivery nor discard; native steering may still be delivered
after provider failure. `managed_steer_stale` reports a changed root;
`managed_steer_rejected` reports other admission failures. Request IDs correlate
observations, not durable replay keys. Never automatically retry ambiguous
steering responses.

### Session-local reasoning preferences

`session_reasoning` adds `session_reasoning_get` and `session_reasoning_set`.
Get requires only the exact active `session_id`; it returns effective values
and supported choices from already-loaded model metadata without discovery,
provider requests, goal continuation, defaults persistence, or session opening.
These controls require idle admission.

Get and set return the full current state: `session_id`, `branch_id`, `tip_id`,
`provider`, `model`, `mode`, `permission_mode`, `thinking`, `reasoning_summary`,
and `text_verbosity`, plus arrays `thinking_levels`, `reasoning_summaries`, and
`text_verbosities`. Set requires an `expected` object containing all ten current
state fields (including explicit empty `tip_id` when applicable), a `field`
(`thinking`, `reasoning_summary`, or `text_verbosity`), and a `value` advertised
by the current model. It changes exactly one effective preference. Thinking
changes the current collaboration mode's override; no host/project defaults,
conversation history, or new turn is written or started.

A stale or invalid operation returns `session_reasoning_rejected`; uncertain
outcomes return `session_reasoning_outcome_unknown`. Inspect again before a
fresh explicit action. Legacy settings commands are not a fallback for this
nonpersisting contract.

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

### Bounded child transcript

`subagent_messages` is the trusted-host detail surface for one selected child.
It returns only the child's public append-only conversation entries; it does not
return the `SubagentState.result` or `SubagentState.error` fields, and it removes
all provider-private `provider_data` blocks before framing the response.

```json
{
  "id": "child-history-1",
  "type": "subagent_messages",
  "params": {
    "target": "/root/api_review",
    "limit": 32,
    "max_bytes": 524288
  }
}
```

The required `params.target` is a canonical child path. `limit` defaults to 32
and must be between 1 and 128. `max_bytes` defaults to 512 KiB and must be
between 16 KiB and 8 MiB. The hard encoded-frame limit is 8 MiB; a single entry
may exceed the requested soft `max_bytes` to guarantee progress, but never the
hard limit. A page contains at most 16 image blocks.

Successful `data` contains:

| Field | Type | Purpose |
|---|---|---|
| `agent` | `AgentRef` | Stable selected path and thread identity |
| `generation` | integer | Child lifecycle generation captured when the page snapshot started |
| `messages` | array | Public `Message` entries in append order |
| `start` / `total` | integer | Page offset and stable snapshot size |
| `has_more` | boolean | Whether another page remains in this snapshot |
| `next_cursor` | string | Opaque continuation token, present exactly when `has_more` is true |

The first page fixes a safe snapshot boundary that does not split an open tool
call from its result. Appends after that request do not move `total`. Continuation
cursors bind the snapshot to the child thread/path and to first, last, and
preceding message anchors; a cursor cannot be reused for another child or after
history replacement. Send `next_cursor` back as `params.cursor` with the same
`target`. Clients must treat cursors as opaque and restart from an empty cursor
when the server reports that a snapshot is no longer available.

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
| Lifecycle/state | `plugin_session_changed`, `session_updated`, `run_stats_updated`, `turn_done`, `error`, `aborted`, `model_changed`, `mode_changed` |
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
- `tool_output`: legacy bounded UI preview; it can include private display
  details such as an edit diff. Do not treat it as an explicitly public result.
- `tool_result`: optional `{text, truncated}` on `tool_end`, containing at most
  8 KiB of valid UTF-8 from the tool message's explicit public text blocks only.
  Thinking, provider continuity, images, arguments and plugin/display metadata
  are excluded; private-detail results suppress this field entirely. Controls
  other than newline/tab are stripped. Full results remain in session storage.
  Older workers can omit this additive field; consumers must tolerate absence.

`permission_request` is emitted while an ask-mode tool authorization blocks for
a trusted host decision. `user_input_request` is a separate model-question
interaction and is emitted as documented above; replying to it never authorizes
a tool.

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
| `tool_end` | `tool_call_id`, `tool_name`, `tool_output?`, `tool_result?`, `tool_duration_ms?`, `is_error?` | After tool completion |
| `tool_routing` | `tool_routing` | When deferred-tool discovery selects tools |

### Interaction events

| Event type | Payload fields | Ordering |
|---|---|---|
| `user_input_request` | `user_input` | While an `ask_user` call blocks for host input |
| `permission_request` | `permission.request` | While an ask-mode authorization blocks for a trusted host decision |
| `queue_updated` | `queue` | When steer/follow-up input is admitted or delivered |

### Lifecycle and state events

| Event type | Payload fields | Ordering |
|---|---|---|
| `session_updated` | correlation/state fields; `message` may hold detail | On session metadata changes |
| `plugin_session_changed` | `plugin_session_changed` | After successful active session/branch transition and release of transition locks |
| `run_stats_updated` | correlation fields | After a durable turn or provider-step marker is appended; consumers may refresh branch-local statistics |
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

`plugin_session_changed` contains `old_session_id`, `new_session_id`,
`old_branch_id`, `new_branch_id`, `reason`, and `generation`. Hosts are rebound
to the new generation before notification; observer failures cannot roll back
the transition. The event does not contain plugin workflow values.

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

## Example client

The following low-level example shows the underlying framing, including the
distinction between `turn_done` and `prompt_completed`.

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

This compact snippet demonstrates raw framing. Production clients must validate
the handshake, route out-of-order responses, bound frames and queues, and wait
for definitive prompt completion. They should also:

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
- The CLI also monitors event delivery. If the event subscriber exceeds its
  deadline, including time waiting behind another stdout write, RPC cancels
  active work and exits with an explicit output error. Clients must continuously
  drain stdout; incomplete event delivery is never treated as success.
- EOF stops command admission, cancels RPC work and waits, releases
  user-input waiters, joins the active prompt and goroutines, then exits.
- There is no `shutdown` command. Close stdin, signal the process, or cancel
  the embedding context.

## Permission model

RPC supports an explicit trusted-host permission broker:

- `--permission deny` denies mutating operations without broker events;
- `--permission allow` authorizes them without broker events and should be used
  only in an externally trusted or isolated environment;
- `--permission ask` emits one correlated `permission_request` and blocks until
  `permission_reply`, `permission_reject`, prompt cancellation, or EOF.

Ask-mode hosts must continuously drain events and resolve every request. Use
`deny` unless the host deliberately implements this authority boundary.
`user_input_reply` answers model questions and never authorizes OS or tool
access.

RPC, shell, plugins, stdio MCP servers, and subagents run with the current
user's OS privileges. Read the [Security model](security.md).

## Current RPC boundary

The current command surface covers prompts, active root input, cancellation,
active-provider and subagent model discovery, model/thinking/mode and response
controls, session and branch management, manual compaction, count-only context
reporting, compatibility and bounded/paged active-branch messages, usage, MCP/skill discovery and
active-skill clearing, pending-input inspection/clearing, documented settings,
configuration diagnostics and diagnostic-capture controls, model-requested
input, goals, and subagents.

Trusted authentication inventory, asynchronous API-key/OAuth login, named
OpenAI-compatible profile setup, logout, permission-mode control, and project
trust control are available when their capabilities are advertised. Only the
explicit `settings_update` fields are mutable; arbitrary configuration mutation
remains outside this protocol boundary. MCP inventory remains read-only and
secret-free. Skill discovery is secret-free; `skills_clear` may deactivate only
the active branch state and never deletes skill files or mutates global
configuration. The `context` command exposes counts and estimates only, never
provider-facing contents.

## Related documents

- [Model-requested user input](user-input.md)
- [Persistent Thread Goals](goals.md)
- [Subagents](subagents.md)
- [Security model](security.md)

## JavaScript plugin integration

RPC sessions load the same global/trusted-project `js_plugins` declarations and
`--js-plugin` options as the CLI. `--no-plugins` disables Go and JavaScript.
Tools, progress, and outer results use the existing event stream; nested host
operations do not fabricate extra provider-facing tool pairs. Permission requests
may include `plugin: {plugin_id, tool_name, parent_tool_call_id, host_tool}`.
The optional `host_tool` identifies a nested built-in operation. These fields are
host-owned attribution, not authority supplied by the script. Runtime warnings
and bounded plugin logs are available through `diagnostics`. Registration status
and individual enable/disable controls are also available over RPC; adding or
removing registrations remains in the CLI. See [Plugins](plugins.md).

## JavaScript extension commands

`plugins_list`, `plugin_commands`, and `plugin_views` return loaded extension
metadata, command declarations, and current view snapshots. Run a command with
`plugin_command_run` and `params: {"command":"id:name","input":"text"}`; cancel
with `plugin_command_cancel` and `params: {"command":"id:name"}`. Execution is
asynchronous so the reader can accept cancellation and interaction replies.
See [JavaScript extensions](plugin-extensions.md) for capability and lifecycle
rules. These commands are additive to RPC version 1.

`plugin_statuses` lists effective registrations, including disabled entries,
without reading their packages. `plugin_enable` and `plugin_disable` take
`params: {"id":"plugin-id"}` and save to that registration's effective global
or trusted-project scope. They return a status object:

```json
{"id":"off","type":"plugin_disable","params":{"id":"workspace-notes"}}
{"id":"off","type":"response","command":"plugin_disable","success":true,"data":{"id":"workspace-notes","path":"/plugins/workspace-notes","scope":"global","enabled":false,"loaded":true,"can_toggle":true,"restart_required":true}}
```

Restart the Snow process to apply the saved state. Active tools, commands,
hooks, dialogs, and children continue unchanged until then. `enabled` is the
saved state; `loaded` describes this process. `restart_required` is true when
they differ. `--no-plugins` still suppresses all runtime loading. Explicit
`--js-plugin` inputs have `can_toggle: false`; change launch options or register
the package and remove the override. Enabling validates package files without
executing JavaScript. Missing IDs, invalid packages, and explicit overrides
return a failed response without changing the registration.


### Reload one loaded plugin

| Command | Parameters | Success data |
|---|---|---|
| `plugin_reload` | `{"id":"plugin-id"}` | `PluginReloadResult` |

```json
{"id":"reload","type":"plugin_reload","params":{"id":"agent-profiles"}}
{"id":"reload","type":"response","command":"plugin_reload","success":true,"data":{"plugin_id":"agent-profiles","applied":true,"generation":2,"fingerprint":"package-config-fingerprint"}}
```

The fingerprint above is illustrative. Only one already-loaded enabled API 1/2
JavaScript plugin can be reloaded. Registration toggles still require restart.
Reload is asynchronous so the reader remains available for broker replies that
replacement readiness may await; normal outstanding-operation limits apply.

Preparation/validation/busy errors return `success: false`, with the old plugin
unchanged. Once the catalog commits, `success: true` and `applied: true` may
include `diagnostics: [{"phase":"ready","message":"..."}]` or a cleanup
phase. These are post-commit diagnostics, not rollback. Pause active goals,
finish root/host/child work, and close children retaining the target plugin's
tools before retrying a busy rejection. See
[the canonical reload contract](plugin-workflows.md#reload-one-plugin) for trust,
state preservation, stale-context invalidation, and all busy conditions.
