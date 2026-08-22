# Architecture and roadmap

This document is the architecture reference for snow-core: a small, modular Go
coding-agent harness that provides one streaming agent loop behind an
interactive terminal UI, print/JSONL/RPC command-line modes, and an embeddable
pure-Go SDK. It records design goals, the package and dependency map, the core
loop, providers, tools, sessions, extensions, the security model, the testing
strategy, the phased roadmap, and condensed research decisions. User-facing
behavior is documented under `docs/`; day-to-day operating guidance lives in
`AGENTS.md`.

> **Note:** snow-core is alpha software. Source code and tests are the
> behavioral authority; this document is the architecture and roadmap reference
> and does not substitute for checking current source and tests.

## On this page

- [Overview and design goals](#overview-and-design-goals)
- [Repository and package map](#repository-and-package-map)
- [Dependency direction and runtime data flow](#dependency-direction-and-runtime-data-flow)
- [Core agent loop](#core-agent-loop)
- [Providers and auth](#providers-and-auth)
- [Tools and permissions](#tools-and-permissions)
- [Config and project context](#config-and-project-context)
- [Sessions and storage](#sessions-and-storage)
- [Plugins](#plugins)
- [MCP](#mcp)
- [Agent Skills](#agent-skills)
- [Tool routing](#tool-routing)
- [Subagents](#subagents)
- [TUI and surfaces](#tui-and-surfaces)
- [Public API surface](#public-api-surface)
- [Security model](#security-model)
- [Testing and verification](#testing-and-verification)
- [Roadmap](#roadmap)
- [Research and decisions](#research-and-decisions)
- [Open risks and known gaps](#open-risks-and-known-gaps)
- [Related documents](#related-documents)

## Overview and design goals

Snow is a minimal terminal coding harness and library. One agent loop powers
every surface, so TUI, CLI, RPC, and SDK consumers observe the same streamed
events rather than reimplementing turn logic.

### Mission

The core is intentionally not a desktop product, a process sandbox, a memory
database, or an autonomous multi-agent workflow product. The optional
root-scoped subagent manager only orchestrates ordinary agent loops. The design keeps the
agent loop understandable, keeps providers and tools behind interfaces, and
keeps UI dependencies out of core packages.

### Goals

- A single Go `snow` binary for macOS and Linux.
- Streaming text, thinking, tool, usage, error, and lifecycle events.
- OpenCode Go API-key access, optional-auth OpenCode Zen promotional free
  models, user-configured OpenAI-compatible Responses or
  Chat Completions endpoints, and ChatGPT/Codex-compatible OAuth credentials.
- Built-in `read`, `write`, `edit`, `bash`, `grep`, `glob`, direct interactive
  `ask_user`, plus deferred public-web `webfetch`.
- SQLite-backed sessions with automatic/manual display titles, indexed branch
  IDs, resume, and fork primitives.
- A stable public surface under `pkg/snowsdk`, `pkg/protocol`, and other
  dependency-light `pkg/*` packages.
- Safe, explicit behavior: deny mutating tools by default in headless use and
  never log credentials.

### Non-goals

- No desktop product surface or Electron/IPC contract; Snow is a standalone
  harness, not a backend for another IDE.
- No built-in process or per-extension sandbox. Bash, external plugins, stdio
  MCP servers, and subagents execute with the user's OS privileges; operators
  provide external containment when needed.
- No general memory database. Prior-session reference is deliberately narrower
  than a general memory product.
- No autonomous multi-agent workflow engine. Subagent orchestration is optional,
  root-scoped, and built from the ordinary agent loop.

## Repository and package map

> **Note:** Checked-out source is authoritative for repository structure. This
> document owns the maintained package map; `AGENTS.md` intentionally keeps only
> the architecture constraints that must be loaded on every agent turn.

```text
.
├── cmd/snow/                # Cobra entry point and CLI mode selection
├── internal/
│   ├── agent/               # provider → stream → permission → tools turn loop
│   ├── app/                 # runtime wiring and provider/model/session catalogs
│   ├── artifact/            # immutable session-scoped tool-result spill artifacts
│   ├── auth/                # credential model and memory/file stores
│   ├── compact/             # context compaction planner and apply implementation
│   ├── config/              # global config defaults, load/save, path helpers
│   ├── context/             # preamble + AGENTS.md system-prompt assembly
│   ├── goal/                # branch-scoped persistent Thread Goals
│   ├── mcp/                 # official-SDK MCP manager and tool/resource bridges
│   ├── permission/          # ask/allow/deny service and remembered rules
│   ├── plan/                # Plan collaboration-mode contract and parser
│   ├── plugin/              # lifecycle manager and Go/external adapters
│   ├── process/             # app-owned managed background process runtime
│   ├── procgroup/           # shared Unix process-group signals and exit state
│   ├── provider/            # Provider interface and adapters
│   │   ├── fake/            # deterministic scripted provider for tests/demos
│   │   ├── opencodego/      # OpenCode Go API-key adapter
│   │   ├── opencodezen/     # Zen optional-auth free-model adapter
│   │   ├── openaicompat/    # user-configured Responses/Chat Completions adapter
│   │   ├── responsesapi/    # shared bounded Responses request/SSE codec
│   │   └── chatgpt/         # Codex OAuth checks/import and Responses adapter
│   ├── rpc/                 # JSONL stdin/stdout control plane
│   ├── session/             # SQLite/in-memory stores, topology, session index
│   ├── skills/              # Agent Skills parser, catalog, and activation tools
│   │   └── builtin/         # immutable rank-zero built-in skills
│   ├── subagent/            # root manager, context projection, roles, V2 tools
│   ├── tempfile/            # crash-orphaned atomic-write cleanup
│   ├── tools/               # Tool/Registry/ToolHost interfaces + BM25 router
│   │   ├── builtin/         # file, shell, search, and deferred web tools
│   │   └── router/          # deferred-tool BM25 routing index
│   ├── trust/               # ~/.snow/trust.json project decisions
│   ├── userinput/           # model-requested host-question coordination
│   ├── worktree/            # detached clean Git-worktree fork utility
│   └── tui/                 # Bubble Tea UI, markdown, mentions, askers
├── pkg/
│   ├── mcp/                 # dependency-light public MCP server config/status
│   ├── plugin/              # dependency-light public extension contract
│   ├── protocol/            # dependency-light public messages/events/models
│   │   └── schema/          # network-free Draft 2020-12 wire schemas
│   └── snowsdk/             # public embeddable API; no TUI dependency
├── examples/                # standalone SDK, RPC, and plugin examples
├── sdk/                     # Language clients and private plugin-authoring SDKs
└── docs/                    # user guides and per-topic references
```

| Package | Responsibility |
|---|---|
| `cmd/snow` | Cobra entry point and CLI mode selection |
| `internal/app` | Runtime wiring and provider/model/session catalogs |
| `internal/agent` | Provider → stream → permission → tools turn loop |
| `internal/auth` | Credential model and memory/file stores |
| `internal/compact` | Context compaction planner and apply implementation |
| `internal/config` | Global config defaults, load/save, path helpers |
| `internal/context` | Preamble and `AGENTS.md` system-prompt assembly |
| `internal/permission` | Ask/allow/deny service and remembered rules |
| `internal/plugin` | Lifecycle manager and Go/external adapters |
| `internal/process` | Session-bound app-owned background processes, output rings, readiness, and cleanup |
| `internal/procgroup` | Shared Unix process-group configuration, signaling, and exit-state helpers |
| `internal/pluginsdk` | Embedded private SDK snapshots and confined vendoring |
| `internal/mcp` | Official-SDK MCP manager and tool/resource bridges |
| `internal/skills` | Agent Skills parser, catalog, and activation tools |
| `internal/provider` | `Provider` interface, registry, and adapters |
| `internal/rpc` | JSONL stdin/stdout control plane |
| `internal/session` | SQLite/in-memory stores, topology, and session index |
| `internal/subagent` | Root manager, context projection, roles, V2 tools |
| `internal/tools` | `Tool`/`Registry`/`ToolHost` interfaces and BM25 router |
| `internal/trust` | `~/.snow/trust.json` project decisions |
| `internal/tui` | Bubble Tea UI, markdown, mentions, askers |
| `internal/artifact` | Immutable session-scoped tool-result spill artifacts |
| `internal/goal` | Branch-scoped persistent Thread Goals lifecycle |
| `internal/plan` | Plan collaboration-mode contract and parser |
| `internal/tempfile` | Crash-orphaned atomic-write cleanup |
| `internal/userinput` | Model-requested host-question coordination |
| `internal/worktree` | Detached clean Git-worktree fork utility |
| `pkg/plugin` | Dependency-light public extension contract |
| `pkg/mcp` | Dependency-light public MCP server config/status |
| `pkg/protocol` | Dependency-light public messages/events/models |
| `pkg/protocol/schema` | Network-free Draft 2020-12 wire schemas |
| `pkg/snowsdk` | Public embeddable API; no TUI dependency |

## Dependency direction and runtime data flow

```text
cmd/snow → app → {tui | print | rpc}
app → agent → {provider, tools, session, permission, context, compact}
provider adapters → auth + protocol
tui → app facades + protocol
snowsdk → app + protocol; never bubbletea
```

`agent`, `provider`, `session`, `tools`, and `pkg/protocol` never import the
TUI or Cobra. `pkg/protocol` is standard-library-only. `internal/` is not a
stable external API; the public surface is `pkg/snowsdk`, `pkg/protocol`, and
other dependency-light `pkg/*` contracts. `go.mod` and
`README.md` both declare the Go 1.27 line (currently `1.27rc3`).

`internal/buildinfo.Version` is the single linked build-version default.
`cmd/snow` copies it into `app.Options.BuildVersion`; `internal/app.New`
normalizes and stores the value before passing it to RPC, external-plugin, and
MCP handshakes. Go SDK sessions seed the same linked value. Release builds
replace the symbol through `-ldflags -X`, while untagged builds remain
`0.1.0-dev`.

### Runtime data flow

1. `cmd/snow` parses flags and builds `app.Options`.
2. `internal/app.New` loads config/auth/trust, builds the tool registry,
   fetches startup model catalogs, creates a session, permission service,
   provider, and agent.
3. `agent.Prompt` appends the user message, resolves credentials, starts a
   provider stream, publishes normalized events, persists the assistant
   message, then runs serial tool calls behind the permission service.
4. Tool results are appended to the session and the provider is called again
   until the model stops, errors, or the context is cancelled.
5. TUI, print, JSON, RPC, and SDK consumers observe the same
   `protocol.AgentEvent` stream; they do not duplicate loop logic. The TUI
   footer continuously shows current/model context usage and `/compact` has
   animated progress.

## Core agent loop

`internal/agent` owns the turn loop and is the only component that appends
messages, resolves credentials, and dispatches tools. Consumers subscribe to
events and never drive the loop themselves.

### Loop invariants

1. Every accepted user prompt gets a session entry before the first provider
   call (crash-safe intent).
2. Successful provider and compaction streams emit an explicit terminal
   `done`; EOF before it is persisted as a failed attempt, never normalized
   into success.
3. Assistant messages are finalized with `stop_reason` before a tool batch
   commits (or use an explicit `pending` state only in memory, never as
   durable terminal state).
4. Tool results always reference `tool_call_id`; length-truncated calls receive
   synthetic errors without execution. Opening a session atomically repairs an
   interrupted final tool batch with explicit retryable/unknown-outcome
   results without retrying side effects automatically.
5. Failed provider attempts remain durable for diagnostics but are excluded
   from subsequent provider and overflow-compaction context.
6. `context.Context` cancellation aborts the provider stream and in-flight
   tools, and persists one aborted assistant boundary regardless of which
   provider boundary observes it.
7. Events are the only cross-surface observation channel (TUI, SDK, print,
   RPC, and plugins all subscribe).
8. Tool-call limits span the complete admitted run, including multiple
   provider/tool batches.
9. Accepted queue entries are consumed after ordinary provider failures;
   internal failures and turn-limit rejection leave a closed, recoverable
   queue for host restoration rather than silently dropping input.
10. Synthetic-only tool batches (truncation, call-limit, validation, or
    permission results without a dispatched tool) receive one corrective
    provider round; a repeated synthetic-only batch terminates the admitted
    run instead of looping without a turn limit.
11. Identical consecutive tool calls are detected per admitted run using
    canonical JSON arguments; bounded advisory reminders at counts 3, 5, and 8
    do not veto execution.

### Streaming events

The core publishes normalized `protocol.AgentEvent` values. Core types are
`session_updated`, `text_delta`, `thinking_delta`, `tool_start`,
`tool_progress`, `tool_end`, `tool_routing`, `permission_request`,
`user_input_request`, `usage`, `queue_updated`, `turn_done`, `error`,
`aborted`, and `model_changed`. Plan, compaction, goal, and subagent
lifecycle events (`plan_started`, `compaction_started`, `thread_goal_updated`,
`subagent_started`, and friends) extend the same stream.

### Agent interface sketch

```go
type Agent interface {
    Prompt(ctx context.Context, text string, opts ...PromptOption) error
    Steer(text string) error     // active-run queue; next safe assistant+tool boundary
    FollowUp(text string) error  // active-run queue; after natural stop and all steering
    PendingInputs() protocol.InputQueue
    Abort(ctx context.Context) error
    Subscribe(func(AgentEvent)) (unsubscribe func)

    SetModel(model Model) error
    SetThinking(level ThinkingLevel)
    Model() Model
    Messages() []Message
    IsRunning() bool
}
```

Tool calls are serial within a turn: a complete tool batch is authorized and
executed, results are appended, and the provider is called again. This keeps
permission and filesystem behavior predictable and avoids parallel-tool write
races.

## Providers and auth

Providers are implemented behind the `internal/provider` interface and are
selected through a deterministic built-in module registry. Agents consume
credential-free provider runtimes; the auth service owns credential resolution
and lifecycle.

| Provider | ID | Credential | Endpoint and behavior |
|---|---|---|---|
| OpenCode Go | `opencode-go` | API key | `https://opencode.ai/zen/go/v1`, OpenAI-compatible `/models` and `/chat/completions`, default `kimi-k2.6` |
| OpenCode Zen | `opencode-zen` | optional API key or anonymous | `https://opencode.ai/zen/v1`; maintained free allowlist intersected with `/models`; model-specific `/chat/completions` or `/responses`; default `big-pickle` |
| OpenAI-compatible | `openai-compatible` or named profile | optional API key per profile | one or more user-supplied API roots plus sibling `/models`; Responses preferred with Chat Completions fallback; no built-in endpoint |
| ChatGPT/Codex | `chatgpt` | OAuth access/refresh token | ChatGPT Codex Responses backend; browser/device login, refresh, authenticated cached catalog |
| Fake | `fake` | none | deterministic scripted provider for tests and demos |

### Credential precedence

For a provider, the first match wins:

1. Explicit API key or SDK option (`--api-key`, `Options.Credential`).
2. The `auth.json` entry for that provider.
3. A known environment fallback (`OPENCODE_API_KEY`).
4. Otherwise, interactive `/login` (TUI) or a typed login-required error
   (headless).

The agent does not resolve credentials. `auth.Service` resolves the
credential, then the authenticated provider runtime supplies it to the
registered transport for both model discovery and inference. A remote OAuth
rejection can request one guarded provider-scoped refresh; API keys are not
refreshable. Never print `Key`, `Access`, or `Refresh` values.

### Auth service and store

`auth.Service` is the single owner of explicit/store/environment precedence,
provider isolation, status, login/logout, persistence, refresh locking and
compare-and-swap token rotation, and reusable API-key or provider-local OAuth
drivers. Auth stores are in-memory or atomic `~/.snow/auth.json` with `0600`
permissions and redacting JSON.

```go
type CredentialType string // api_key | oauth

type Credential struct {
    Provider  string         `json:"-"`
    Type      CredentialType `json:"type"`
    Key       string         `json:"key,omitempty"`
    Access    string         `json:"access,omitempty"`
    Refresh   string         `json:"refresh,omitempty"`
    Expires   int64          `json:"expires,omitempty"` // unix seconds
    AccountID string         `json:"accountId,omitempty"`
    Extra     map[string]any `json:"extra,omitempty"`
}

type Store interface {
    Get(provider string) (Credential, bool)
    Put(provider string, cred Credential) error
    Delete(provider string) error
    Update(provider string, fn UpdateFunc) (Credential, bool, error)
    Path() string // for diagnostics; never print secrets
}

type Driver interface {
    Descriptor() Descriptor
    Inspect(Credential) (Status, error) // local and side-effect-free
    Login(context.Context, LoginRequest, Interaction) (Credential, error)
    Validate(Credential) error
    NeedsRefresh(Credential, time.Time) bool
    Refresh(context.Context, Credential, RefreshReason) (Credential, error)
}
```

`provider.Registry` binds each built-in transport to one driver and builds the
credential-free runtimes used by root and child agents.

### Provider stream contract

```go
type Model struct {
    Provider         string
    ID               string
    DisplayName      string
    ContextWindow    int
    MaxOutputTokens  int
    SupportsTools    bool
    SupportsThinking bool
    ThinkingLevels   []ThinkingLevel // normalized non-off levels; off is implicit
    SupportsVision   bool
}

type ChatRequest struct {
    Model       Model
    Messages    []Message
    Tools       []ToolSchema
    System      string
    MaxTokens   int
    Temperature *float64
    Thinking    ThinkingLevel // off|minimal|low|medium|high|xhigh|max|ultra
}

type StreamEvent struct {
    Type       StreamEventType
    Text       string
    ToolCallID string
    ToolName   string
    Arguments  json.RawMessage // cumulative or final per adapter contract
    Usage      *Usage
    StopReason string
    Err        error
}

type EventStream interface {
    Next(ctx context.Context) (StreamEvent, error)
    Close() error
}

type Transport interface {
    ID() string
    ListModels(ctx context.Context) ([]Model, error)
    Chat(ctx context.Context, creds auth.Credential, req ChatRequest) (EventStream, error)
}

type Provider interface {
    ID() string
    ListModels(ctx context.Context) ([]Model, error)
    Chat(ctx context.Context, req ChatRequest) (EventStream, error)
}
```

Successful streams emit a terminal `done` before EOF; EOF without `done` is
truncation. Each adapter normalizes vendor SSE/JSON into `StreamEvent`.
Tool-call argument streaming may be vendor-specific; adapters must emit a final
`tool_call_done` with complete JSON arguments before the agent dispatches
tools.

### OpenCode Go

The primary API-key adapter. It attaches `Authorization: Bearer <key>`, maps
Chat Completions SSE chunks and finish reasons into Snow events, surfaces
rate-limit and quota errors as structured errors, and maps normalized thinking
effort to OpenAI `reasoning_effort` while rejecting levels the selected model
does not advertise. Startup model discovery fetches `GET /models` and merges
matching IDs with OpenCode's public `models.dev` catalog for capability,
reasoning, and pricing metadata; the API key is never sent to the metadata
host and direct gateway fields win. Discovery falls back to the pinned static
default without failing startup or logging keys.

### OpenCode Zen

`internal/provider/opencodezen` is a separate, optional-auth adapter for Zen's
promotional free routes. Credential resolution accepts an explicit key, the
`opencode-zen` Snow auth entry, `OPENCODE_API_KEY`, or an empty anonymous
credential; keyless requests omit `Authorization` completely. The provider
intersects live `GET /models` availability with a maintained seven-model free
allowlist and is catalog-authoritative, so paid, unknown, and deprecated IDs
cannot be selected accidentally. `big-pickle` is the bundled default.

The local transport map sends Muse Spark Contributor Free to Responses/SSE and
the remaining maintained models to Chat Completions/SSE. Both normalize into
the shared provider event contract. A pre-output HTTP 429 is retried after 2,
5, and 15 seconds with context-aware waits, then becomes a structured
`LimitError`; no retry occurs after output. Active keys are redacted from
bounded errors.

On a canonical-endpoint Zen catalog refresh, the provider concurrently fetches
live `/models` availability and the public models.dev `opencode` record under
the bounded discovery context. A custom base URL disables that merge unless the
internal provider config explicitly supplies a catalog URL. Only IDs in both
the maintained free policy and live Zen availability are returned. `reasoning` and
`reasoning_options[type=effort].values` are normalized into model-level thinking
metadata; no model-specific effort set is compiled into Snow. Metadata requests
carry no Zen authorization. The v2 atomic 0600 catalog cache rehydrates current
local transport/privacy/limit policy plus the last fetched reasoning metadata,
and invalidates the older cache schema that contained pinned capability values.
A failed metadata refresh uses verified cached reasoning when available and
otherwise exposes no guessed effort controls. Advertised values serialize as
`reasoning_effort` for Chat Completions or `reasoning.effort` for Responses.
Snow's `off` setting omits the override rather than claiming the provider
disables inherent reasoning.

Big Pickle uses its stricter 160k input limit as the effective context and
records 200k as its maximum. Successful terminal streams with no text or
completed tool call are converted to actionable stream errors instead of
durable blank assistant turns. Model descriptions carry the documented
retention/training notice shown by the TUI and exposed through existing SDK/RPC
model metadata. Snow does not import OpenCode credentials, rotate accounts,
fall back to paid Zen models, or promise continued promotional availability.

### OpenAI-compatible

`internal/provider/openaicompat` implements the legacy `openai-compatible`
provider plus any number of user-named profiles. Each profile requires an
absolute HTTP(S) API root or full `/responses`/`/chat/completions` URL,
derives sibling `GET /models`, and has an isolated auth/config key. Optional
Bearer auth comes from explicit options or `auth.json`; `OPENAI_API_KEY` is a
fallback for the legacy profile only. Responses is preferred and uses the
bounded request/SSE codec shared with ChatGPT through
`internal/provider/responsesapi`. An HTTP 404/405/501 from Responses selects
and caches a Chat Completions/SSE fallback. OAuth, Codex headers, refresh, and
catalog behavior remain isolated. There is no default endpoint; discovery is
nonfatal when unavailable. Custom/Azure headers and query parameters are
excluded, and provider errors redact active keys.

### ChatGPT/Codex OAuth

`internal/provider/chatgpt` performs a side-effect-free credential check,
browser PKCE and device-code login, compatible Codex/Pi/OpenCode credential
imports, guarded automatic refresh, an origin-and-account-scoped ETag model
cache, and hardened Codex Responses SSE streaming with branch-scoped prompt
affinity, zstd compression, bounded pre-output transient retries, structured
error diagnostics, and mandatory terminal events. The TUI/CLI report
configured, expired, or missing ChatGPT auth without refreshing during checks.

Login opens the system browser (or prints the URL with `--no-open`) and
receives the code on `localhost:1455/auth/callback`, or accepts the full
callback URL when the port is occupied or the browser is remote. The code is
exchanged for access/refresh tokens, `auth.json` is written atomically with
mode `0600`, JWT/account metadata is validated without persisting the ID
token, and the authenticated catalog is refreshed with a bundled offline
fallback on outage. Before `Chat`, credentials expiring within five minutes
are refreshed under the cross-process auth-store lock; a pre-stream 401
permits one guarded forced refresh and retry. WebSocket continuation remains
deferred.

### Fake

`internal/provider/fake` is a deterministic scripted provider for tests,
examples, and demos. It requires no credentials and drives multi-turn
tool-call round trips through the real agent loop.

## Tools and permissions

### Built-in tools

| Tool | Purpose | Risk | Notes |
|---|---|---|---|
| `read` | Read file contents (optional offset/limit) | read | Pinned `os.Root`; binary files produce a short error; streams bounded ranges |
| `write` | Create or overwrite a file | write | Rooted atomic same-directory replace; new files honor umask; replacements preserve mode |
| `edit` | Exact string replace or patch | write | 8 MiB input/result and 10,000-match caps; bounded preview; fails on ambiguity unless `replace_all` |
| `bash` | Run a foreground shell command in cwd | exec | POSIX `sh`; timeout, process-group cleanup, pipe-drain bounds, combined output cap |
| `process_start` | Start an app-owned background shell process | exec | Opaque handle; process group; bounded output tail; optional loopback TCP/HTTP or RE2-log readiness |
| `process_status` | Read one managed process state | read | Runtime/session-scoped metadata; no command, environment, or PID exposure |
| `process_logs` | Read combined managed-process output | read | Absolute retry-safe cursor, rollover count, UTF-8 sanitation, bounded optional wait |
| `process_stop` | Terminate and reap a managed process group | exec | Graceful group signal then bounded kill escalation; idempotent terminal stop |
| `process_list` | List safe active-session process metadata | read | Running-first bounded inventory; excludes commands, environment, PIDs, and logs |
| `grep` | Search text files with RE2 and line numbers | read | Pure Go; glob filter, case option, match/output caps |
| `glob` | Match regular file paths | read | Pure Go; recursive `**` segments and result/output caps |
| `ask_user` | Request one to three user decisions or free-form answers | read/interaction | TUI prompt, SDK callback, or RPC reply/reject; automatic Other choice |
| `update_plan` | Emit a turn-local implementation checklist | read | Direct Default-mode schema; unavailable/rejected in Plan mode; not persisted |
| `search_tools` | Find deferred tools by capability | read | Always-loaded recovery schema; returns top matching schemas |
| `session_search` | Search prior same-project sessions | read | Disposable SQLite FTS5 corpus over names, user/assistant text, and summaries |
| `session_reference` | Import a bounded snapshot of a prior branch | read | At most three tip-pinned, untrusted snapshots per branch |
| `webfetch` | Fetch a public HTTP(S) resource | network | Deferred schema; Surf Chrome 150; secure TLS; HTML to Markdown; SSRF, timeout, redirect, media-type, and output bounds |

`grep`, `glob`, `ask_user`, `update_plan`, `search_tools`, and session-history
tools are registered in the default builtin registry. `webfetch` is the first
built-in deferred tool, so the normal app also loads the small direct
`search_tools` recovery schema while keeping the full `webfetch` schema out of
unrelated provider requests. `ask_user` has no discovery metadata: its full
schema is sent with the other direct built-ins on every tool-capable request,
and the explicit SDK/CLI `Tools` allowlist remains authoritative. A choice
returns its exact label; Other and free-form responses return trimmed text.
The model-facing result is ordered JSON.

The five managed-process schemas are also direct built-ins. One app-owned
`internal/process.Manager` outlives individual tool calls and continuously
drains child output while the serial agent loop proceeds. Processes are scoped
to the active session but shared by its branches; active children block session
switching, while forks never copy handles. `App.Close` closes agent admission,
then terminates and reaps managed groups before session/path resources. State is
not reconstructed from persisted PIDs after restart. The global `processes`
configuration bounds concurrent children, retained terminal records, and output
per record; individual child lifetime is deliberately the owning app lifetime.

### Tool interfaces

```go
type ToolSchema struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"` // JSON Schema object
}

type ToolResult struct {
    Content []ContentBlock
    IsError bool
    // Details is tool-private metadata for the TUI (for example diff stats);
    // it is not sent to the model unless mirrored into Content.
    Details any
}

type ToolHost interface {
    CWD() string
    Roots() []string // path roots the tool may touch (cwd + explicit allows)
    Permission() permission.Service
    EmitProgress(event ToolProgressEvent)
    Environ() []string
}

type Tool interface {
    Schema() ToolSchema
    Run(ctx context.Context, args json.RawMessage, host ToolHost) (ToolResult, error)
}

type Registry interface {
    Register(Tool) error
    Get(name string) (Tool, bool)
    Schemas() []ToolSchema
    List() []Tool
}
```

### Path confinement and bounds

File and search tools use pinned `os.Root` confinement: they resolve symlinks
and require the final path under `Roots()` (cwd plus configured allows).
Escapes via `..` or symlinks return `IsError` without panicking. Tool output
defaults to a 256 KiB cap; read/search stream bounded data with explicit
truncation markers; bash has a 120 s default timeout and a 10-minute
stream-silence watchdog (`stream_idle_timeout_ms: -1` disables it). Writes
stage content beside the destination, sync, and rename into place, preserving
replacement modes. `webfetch` allows only public HTTP(S), disables environment
proxies, validates every redirect, pins public addresses at dial time,
verifies TLS certificates, rejects binary bodies, and labels returned content
as untrusted external data.

Search tools skip hidden/generated directories and symlink entries, honor
hierarchical `.gitignore`/`.ignore`, bounded global/trusted-project YAML
policy, hidden/generated defaults, and per-call soft-ignore overrides. Grep
supports RE2, line numbers, case-insensitive search, path globs, and
match/output caps. Glob supports ordinary path patterns plus recursive `**`.

### Permissions and trust

Permission modes are `ask`, `allow`, and `deny`. Write/edit/bash and network
tools are permission-gated; `read` remains allowed in deny/ask modes, while
deferred `webfetch` is filtered in deny mode. Unknown tool names return a
`tool_result` error string, panics are recovered to error results, and exact
consecutive repeats are advisory-loop-guarded without changing permission or
execution policy.

```go
type Mode string // ask | allow | deny

type Request struct {
    Tool   string
    Args   json.RawMessage
    Paths  []string // affected paths if known
    Risk   string   // read|write|exec|network
    Reason string
}

type Decision string // allow | deny | allow_session | allow_always

type Service interface {
    Mode() Mode
    SetMode(Mode)
    Authorize(ctx context.Context, req Request) (Decision, error)
    Remember(req Request, d Decision) // session or persistent scope
}
```

The interactive TUI supplies an `Asker`; headless SDK defaults to `deny` for
mutating tools unless the caller deliberately opts into `allow`/`AutoApprove`
in a trusted environment. Project trust is resolved on canonical paths before
TUI runtime construction; every undecided interactive project prompts, while
headless `ask` remains fail-closed. Runtime `/trust` changes apply on the next
launch. Trust controls input loading (project config, configured system-prompt
files, plugins, MCP declarations, skills) and is not a sandbox.

## Config and project context

Global configuration lives in `~/.snow/config.json`; secrets in
`~/.snow/auth.json`; trust decisions in `~/.snow/trust.json`; sessions under
`~/.snow/sessions/`; and TUI bindings/themes under `~/.snow/keybindings.yaml`
and `~/.snow/themes/*.yaml`. Project-scoped overrides use
`<project>/.snow/config.json` and are trust-gated. See
`docs/configuration.md`.

Defaults include provider `opencode-go`, permission `ask` (headless SDK
defaults to deny), thinking `off`, 256 KiB tool output, a 120 s bash timeout,
a 10-minute stream-silence watchdog, and a 100 KiB project-context cap.

### Context assembly

1. Base preamble: explicit SDK `SystemPrompt`, trusted-project configured
   file, global configured file, or embedded `internal/context/system.md`.
2. `AGENTS.md` walk from cwd upward (bounded by depth and total bytes);
   nearest-first, always loaded, and documented as a residual prompt-injection
   risk.
3. Optional `CLAUDE.md` compatibility read (off by default in current app
   wiring).
4. Startup skill metadata, MCP instructions, and subagent guidance when
   enabled.
5. Per-request collaboration-mode instructions from embedded
   `internal/plan/system.md` and activated-skill instructions.
6. Goal-bearing turns receive separate trailing internal context rendered from
   embedded templates under `internal/goal/`; this is not system context.

Configured prompt files are bounded by `context_cap_bytes`; project prompt
paths are trust-gated, confined to the canonical project root, and reject
symlink components. Each discovered `AGENTS.md` is opened through a pinned
parent-directory handle, must remain a regular non-symlink file, and is read
only through the remaining byte budget before a truncation notice is added.

## Sessions and storage

Sessions are SQLite databases with metadata and indexed entries. Messages
carry `id` and `parent_id`; `BranchTip` determines the linearized
conversation. The model is append-only and tree-shaped: preserve it when adding
resume or fork features.

```go
type SessionStore interface {
    ID() string
    Path() string // empty if in-memory
    Header() SessionHeader

    Append(entry Entry) error
    BranchTip() string
    SetBranchTip(id string) error
    Messages() ([]Message, error) // linearized root → tip
    Fork(fromID string) (SessionStore, error)
    Close() error
}

type SessionIndex interface {
    List(cwd string) ([]SessionInfo, error)
    Open(path string) (SessionStore, error)
    Create(cwd string) (SessionStore, error)
}
```

The current on-disk schema version is 9. Tables include `session_meta`
(header, title, provenance), `entries` (append-only messages and compaction
entries), `session_branches` (branch tips and lineage), `thread_state`
(collaboration mode per branch), `thread_goals` and related cost/deferral
tables (persistent Thread Goals), and `subagent_threads` (child topology).
Forks copy branch state, goal estimates, managed objective resources, and
subagent topology where applicable. WAL transactions and indexed branch
queries keep open and reload bounded; opening never performs a full scan.

### Provider-facing compaction

Compaction appends a logical boundary rather than rewriting entries. The
provider projection replaces an old complete-turn prefix with a structured
working-state checkpoint and retains at least the configured recent turns.
Planning fails closed if a boundary would split an assistant tool call from its
result. Opaque provider continuity leaves context only with its complete owning
turn. Completed assistant-call/tool-result cycles inside one long active turn
are also safe boundaries when prefix projection does not consume the retained
prior-turn floor, so old cycles may compact while current and recent cycles
remain exact. Besides the global pressure trigger, aggregate tool
calls/results in the safely compactable old prefix have an independent model-
window budget. Large individual results are measured through their bounded
provider projection. The OpenCode Go chat-completions adapter renders the
harness-owned checkpoint as authoritative user input, matching the Responses
adapter rather than presenting it as stale assistant output.

When compacted history contains tools, the agent saves one bounded private
transcript of tool calls and model-facing text/metadata, omitting image payloads,
private reasoning, and provider continuity. Verified artifact references are
carried forward with a fixed cap of 24 across repeated compaction and physical
forks; stale or forged markers are ignored. Transcript persistence failures emit
a lifecycle warning, while full append-only session history remains the
authority for replay.

### On-disk layout

```text
~/.snow/sessions/<cwd-encoded>/<timestamp>_<suffix>.db
<session>.db.agents/   # optional private child databases; excluded from picker
```

Current directories use `cwd-v2-<sha256(normalized-absolute-cwd)>`; the legacy
flattened encoder remains discoverable with stored-CWD verification. The
schema is Snow-owned; old JSONL sessions are intentionally not migrated.

### Forks

- Same-database branches share one file and diverge at a `BranchTip`.
- Physical exact-entry forks create an independent session with provenance.
- Detached clean Git-worktree forks use a bounded direct-argument Git utility.
- Prior-session reference is deliberately narrower than a general memory
  product: `session_search` rebuilds a disposable SQLite FTS5 corpus, and
  `session_reference` imports at most three tip-pinned, bounded, untrusted
  snapshots per target branch. Tool content, reasoning, images,
  provider-private data, credentials, permission/trust state, goals, queues,
  and child databases are excluded; references transfer information only and
  no authority.

## Plugins

The extensibility core is implemented in `pkg/plugin`, `internal/tools`, and
`internal/plugin`. Static Go plugins use `Manifest`, `Register`, and `Close`;
no Go shared-object loading is used. The manager owns registration, namespaced
tool descriptors, event subscriptions, diagnostics, and reverse-order
lifecycle.

External runtimes use JSON-RPC 2.0 JSONL on stdin/stdout, with stderr reserved
for bounded diagnostics. Request IDs are strings and one reader multiplexer
supports concurrent calls. The host sends `initialize`, `tools/list`,
`tools/call`, and `shutdown`; progress, explicitly subscribed sanitized
observation events, cancellation, and bounded logs are notifications. Empty
`supported_events` means no event fanout; delivery is best effort and cannot
block the agent loop.

External tool risk is optional (`read|write|exec|network`) and fails closed to
`exec`; per-tool capabilities and private raw-JSON result details survive
registry adaptation. Frames, input/output, progress, stderr, timeouts,
cancellation, and concurrent calls are bounded. Commands are argv arrays and
never shell strings.

Project-local plugin declarations are trust-gated. Trust controls input
loading, not plugin permissions or OS access; untrusted plugins need a
container/VM/OS sandbox. Persistent JavaScript and Python examples implement
protocol v2 under `examples/plugins`. `snow plugin check` performs a
provider-free live handshake with schema/event/risk and bounded-diagnostics
reporting, while side-effect-free `list|get` and restart-scoped
`add|enable|disable|remove` manage global or canonical-project declarations.
Adds stage disabled by default, targeted raw-JSON updates preserve unknown
fields, and global/project/explicit declarations merge by ID in increasing
precedence. The canonical wire contract is `docs/plugin-protocol.md`; runtime
selection benchmarks and deferrals are in
`docs/plugin-js-python-research.md`.

## MCP

`internal/mcp` uses the official `modelcontextprotocol/go-sdk` v1.7.0. It
negotiates the current stateless `2026-07-28` protocol and the SDK's supported
legacy revisions across stdio and Streamable HTTP. Server tools become
permissioned `mcp_<server>_<tool>` descriptors. Resources, templates,
subscriptions, and prompts use generic namespaced bridges; tool-list changes
atomically refresh the registry and BM25 index. Static HTTP headers and stdio
environment values support environment expansion without entering
diagnostics. Project server config is trust-gated. Eager lifecycle remains the
default; opt-in lazy servers use shared reconnect attempts, active-call and
resource-subscription leases, idle disconnect, and a seven-day versioned
catalog cache under the private Snow cache directory. Cached tool schemas and
resource/prompt capability flags reconstruct permissioned descriptors without
transport work; reconnect refresh validates stale metadata. Cache keys partition
declaration scope and project/root identity without persisting credential
values. Automatic catalogs with no tool or resource/prompt activation
descriptor use eager fallback so list-change notifications remain observable;
strict `cache_bootstrap: explicit` instead performs no startup transport and
requires deliberate refresh. `lazy-keep-alive` starts from cache and retains the
session after first activation. See `docs/mcp.md`.

The CLI separates side-effect-free configuration/cache inspection (`mcp
list|get|cache status`) from live connection and cache-refresh operations (`mcp
check`, `mcp cache refresh`), supports scoped cache clearing, and atomically
manages global or project declarations through `add|enable|disable|remove`.
Targeted JSON updates preserve unrelated and unknown config fields; all
inspection output redacts credential-bearing values.

## Agent Skills

`internal/skills` implements the open Agent Skills `SKILL.md` format. Startup
discovery strictly validates standard metadata and loads only names and
descriptions from immutable rank-zero embedded skills plus standard user and
trust-gated project paths under a 64 KiB catalog budget. The bundled
`plugin-builder` skill provides supervised, restart-required protocol-v2
authoring instructions and SDK-first Python/JavaScript templates. The binary
embeds reviewed private SDK snapshots; `plugin sdk vendor` copies one into a
plugin directory through staged, root-confined replacement without executing
it and reports per-file hashes. Generated templates require that reviewed copy
and fail closed rather than hand-rolling protocol framing.

`activate_skill` loads escaped full instructions, the TUI autocompletes
enabled leading `$skill-name` directives, and a directive activates before
provider dispatch while recording branch-scoped state. `deactivate_skill`
removes one named active skill, or all active skills only via `name: "*"`, and
atomically persists a provider-hidden lifecycle marker with the tool result so
the next continuation and resumed sessions omit that guidance.
`read_skill_resource` uses immutable bounded `embed.FS` reads for built-ins or
verifies the discovery-time directory identity before using a pinned
per-operation `os.Root` for filesystem resources. Activated content is
reattached on every provider call and reconstructed from successful markers
and session history after resume so compaction does not drop it; current
trust/disable/tool policy filters stale activations. See `docs/skills.md`.

Global and trust-gated project `skills.disabled`/`skills.overrides` policy can
hide entries from prompts and activation without deleting their files. CLI
`skills list|get|enable|disable`, SDK `SkillInventory`, and read-only TUI
`/skills` expose that inventory.

## Tool routing

Existing tools and zero-value discovery metadata remain always loaded.
Native, Go-plugin, external-plugin, SDK, and MCP registrations may opt into
`deferred` discovery per tool. Snow builds an in-memory Bleve BM25 index after
startup registration, indexes only name/namespace/description/keywords, and
loads the top five permitted full schemas from the authoritative registry.
`search_tools` provides an explicit recovery pass, while index/search failures
fall back to direct exposure for that turn. Routing emits structured metrics
but does not make an extra LLM call. Optional semantic/vector routing remains
deferred pending a locally downloadable open-source model with acceptable
licensing, platform support, binary size, memory use, and startup time. See
`docs/tool-routing.md`.

## Subagents

Snow implements a Codex-V2-style subagent tree directly. `internal/subagent`
owns canonical path identity, parent edges, reservation/commit, validated
state transitions, execution slots, limits, mail routing, child construction,
persistence, and shutdown. Every child is an ordinary `internal/agent.Agent`;
`agent` does not import `subagent`, and collaboration enters through
registered tools plus a generic attributed mailbox.

The nine direct model tools are `spawn_agent`, `list_subagent_models`,
`send_message`, `followup_task`, `wait_agent`, `interrupt_agent`,
`close_agent`, `resume_agent`, and `list_agents`. Tool instances bind caller
identity. Spawn, follow-up, and resume use `permission.RiskDelegate`; remaining
controls use read risk. `wait_agent`
supports the original next-activity barrier and an `until=all` descendant join
with aggregate running/queued/terminal counts; SDK and RPC expose the same
bounded join.

The feature defaults off. Child concurrency is configurable and defaults to
four simultaneously running children (bounded up to 256); the root does not
consume a slot. Depth defaults to one and is bounded up to eight. Child
authority is role-scoped: the `general` and `implementer` roles may use
permission-gated `bash`, while `explorer` remains read/search-only. Recursion
and file mutation are independent intersections of global and role policy;
write/edit require both mutation switches.

Parent and child transcripts never share a mutable cursor. Context forks use
`ContextMessages`, strip unsafe or incomplete protocol artifacts, and repair
IDs. Mailbox producers only enqueue; the admitted receiving loop drains before
provider requests and atomically marks final mail unread at turn finalization,
so external delivery cannot fork a serial tool-result chain.

`protocol.AgentPath`, `AgentRef`, `SubagentState`, and `AgentMessage` are
public DTOs. Ordinary child events carry `agent`; lifecycle and mail add
`subagent` and `agent_message`; root events omit correlation for
compatibility. SDK, RPC, print/JSON, TUI, and plugin observers consume one
cloned event bus.

Durable child histories default on, use private
`<root>.db.agents/<thread>.db` databases, stay out of the session index, and
load lazily. Cold open never restarts work; surfaces subscribe and call
`ReadySubagents` before restored topology is published. Shutdown joins the
manager before closing the root event bus and shared resources. Active
children block root-session switching; after all children reach a terminal
state, switching sessions detaches the old in-memory runtimes and restores the
target session's topology. The per-session agent limit counts open identities,
not historical closed ones. Closing a terminal durable child unloads its
runtime but preserves its stable path, thread ID, transcript, result, usage,
and topology; resume re-admits it, while follow-up resumes closed targets
automatically when capacity permits.

The shared cwd and OS authority are not a sandbox. Parallel edits can
conflict, provider usage is independent, and child/repository output is
untrusted. The TUI serializes root/child permission requests through an
attributed FIFO broker; headless ask mode remains fail-closed. Child
`ask_user` stays excluded, preventing ambiguous input routing. See
`docs/subagents.md`.

## TUI and surfaces

The Bubble Tea TUI is the interactive default. It renders a transcript with
markdown, streaming updates, model/provider pickers, model-aware `/thinking`
effort selection, login/logout, permissions, sessions, slash completion, and
`@` file mentions. A leading `$` autocompletes enabled Agent Skills. Strict
bounded YAML custom themes and keybindings support global and trusted-project
precedence with warnings.

The active composer queues plain Enter as steering and Alt+Enter as a
follow-up; Ctrl+J remains multiline, and abort clears/restores queued TUI
text. Queue delivery is bounded, one-at-a-time, after complete serial tool
batches. Top-level Shift+Tab toggles Default/Plan mode (queued to `turn_done`
while busy).

The TUI uses Bubble Tea's alternate-screen, app-owned viewport so scrolling
cannot reveal stale frame chrome. `tui.mouse` defaults to `true` so wheel and
trackpad gestures stay inside Snow's viewport; primary drag uses Snow
selection/copy, F6 toggles app/native mouse mode, and right-click opens Snow's
bounded **Copy selection** menu without disabling viewport mouse reporting. `Ctrl+V` attaches supported
clipboard images in the agent composer or falls back to textarea paste.

### Slash commands

`/allow [always]`, `/default`, `/deny`, `/fork`, `/help`, `/login`,
`/logout [provider]`, `/model`, `/plan [message]`, `/thinking`, `/new`,
`/permissions`, `/resume`, `/agent [path]`, `/agent concurrency N`,
`/processes [id|name]`, `/sessions`, `/settings`, `/compact`, `/mcp`, `/skills`,
`/tree`, `/quit`, and `/trust [allow|deny]`.

`/processes` is a first-party TUI projection over the app-owned process manager,
not a second lifecycle implementation or a public RPC/SDK process API. It polls
bounded state/log snapshots while open, preserves opaque-handle selection,
follows the output tail by default, supports explicit detail scrolling, and
neutralizes subprocess terminal control bytes before rendering.

### Event to UI mapping

| AgentEvent | UI behavior |
|---|---|
| `text_delta` | Append to the live assistant buffer |
| `thinking_delta` | Append to persistent muted Markdown thinking region; animated wait before first delta |
| `tool_start` | Open a native tool card |
| `tool_progress` | Append a bounded progress line |
| `tool_end` | Finalize card with duration, status, and bounded output preview |
| `permission_request` | Modal; block the tool until decided |
| `user_input_request` | Inline choice/free-form interaction; Esc rejects, Ctrl+C aborts the turn |
| `usage` | Always-visible current/model context counter in the footer |
| `turn_done` | Unlock the editor; finalize bubbles |
| `error` | Error banner |

Bubble Tea renders after every `Update`, so the TUI coalesces queued stream
events (bounded batch), caches stable transcript rendering, and composes one
alternate-screen frame from a sticky header, Bubbles viewport, composer, and
footer. See `docs/tui-performance.md`.

### Other surfaces

- Print mode: `snow -p "prompt"` streams text to stdout.
- JSON mode: `snow --mode json -p "..."` emits JSONL `protocol.AgentEvent`
  lines for piping.
- RPC mode: versioned JSONL over stdin/stdout (see the next section).
- SDK: in-process `pkg/snowsdk`.

## Public API surface

The stable public surface is `pkg/snowsdk`, `pkg/protocol`, and other
dependency-light `pkg/*` contracts.

### Go SDK

```go
type Options struct {
    CWD             string
    Provider        string
    Model           string
    SessionPath     string // existing SQLite .db to resume; empty creates a new session
    NoSession       bool   // use an ephemeral in-memory session
    AuthPath        string // default ~/.snow/auth.json
    ConfigPath      string
    PermissionMode  string // ask|allow|deny; default deny for mutating in non-TTY
    AutoApprove     bool   // dangerous; for trusted CI only
    Tools           []string // subset allowlist; empty = defaults
    SystemPrompt    string
    Thinking         string
    UserInputHandler func(context.Context, protocol.UserInputRequest) (protocol.UserInputResponse, error)
    PermissionHandler func(context.Context, protocol.PermissionRequest) (protocol.PermissionResponse, error)
}

type Session struct { /* opaque */ }

func Open(ctx context.Context, opts Options) (*Session, error)

func (s *Session) Prompt(ctx context.Context, text string) error
func (s *Session) PromptWithMode(ctx context.Context, text string, mode protocol.CollaborationMode) error
func (s *Session) Steer(ctx context.Context, text string) error
func (s *Session) FollowUp(ctx context.Context, text string) error
func (s *Session) PendingInputs() (protocol.InputQueue, error)
func (s *Session) Abort(ctx context.Context) error
func (s *Session) Subscribe(func(protocol.AgentEvent)) (cancel func)
func (s *Session) Model() protocol.Model
func (s *Session) SetModel(protocol.Model) error
func (s *Session) Mode() protocol.CollaborationMode
func (s *Session) SetMode(protocol.CollaborationMode) error
func (s *Session) Messages() ([]protocol.Message, error)
func (s *Session) Close() error
```

`pkg/snowsdk` embeds the same agent loop as the CLI and never imports
bubbletea. See `docs/sdk.md`.

### RPC protocol

Versioned JSONL over stdin/stdout:

- First frame: `rpc_ready` with string protocol version, Snow build version,
  sorted protocol capabilities, and maximum input size.
- Commands cover prompts (including additive text/image content when
  `multimodal_prompts` is announced) and active input, model/mode/response
  controls, session and branch management, manual compaction, active-branch
  messages and usage, MCP/skill discovery, pending-input
  inspection/clearing, configuration diagnostics, goals, subagents,
  model-requested input, and a trusted-host interactive permission broker
  (`permission_reply`/`permission_reject`, gated by `permission_interaction`).
- Events mirror SDK events; RPC-only control frames are not persisted events.
- Framing splits on `\n` only (not Unicode line separators).
- Schemas are network-free Draft 2020-12 contracts under
  `pkg/protocol/schema/rpc/v1`.

RPC prompts run asynchronously so the command reader remains available while
the agent waits. Admission returns an immediate response; exactly one later
`prompt_completed` frame reports `completed`, `failed`, or `canceled` after
all prompt events, and legacy same-ID prompt failure responses are retained.
`user_input_reply.params` is a `UserInputResponse`;
`user_input_reject.params` contains `request_id`. EOF closes the interactive
input broker so pending/future questions fail fast while an ordinary one-shot
prompt is still allowed to finish.

Primary consumers are the checked-in dependency-light Python 3.9+ async and
Node.js 22+ ESM/TypeScript SDKs, other non-Go hosts, and IDE bridges. They
invoke an installed/explicit Snow binary and do not download one. Go hosts
should prefer `pkg/snowsdk`. See `docs/rpc.md` and `docs/language-sdks.md`.

### Language SDKs

Zero-runtime-dependency Python 3.9+ async and Node.js 22+ ESM/TypeScript
packages use an explicitly installed external Snow binary, safe defaults,
bounded JSONL routing, multimodal prompts, MCP/skill discovery,
user-input and trusted-host permission handlers, and real-binary CI conformance
tests. They never download a binary.

Separate private plugin-authoring packages live in `sdk/plugin-python`
(`snow-plugin`, imported as `snow_plugin`) and `sdk/plugin-javascript`
(`@snow-core/plugin`). They implement the persistent external protocol-v2
lifecycle, bounded tool calls/results/events, cancellation, progress/logging,
shutdown, and stderr-safe diagnostics without runtime dependencies. Go
`ExternalHost` tests execute both packaged examples. External npm/PyPI
publication remains deliberately deferred.

## Security model

Snow, Bash, plugins, stdio MCP servers, and every subagent run with the user's
OS privileges. Snow has no built-in process sandbox; operators requiring
containment must run the harness inside an external container, VM, or suitable
OS policy.

Subagents share cwd, filesystem, and process side effects and incur separate
model usage. Parallel mutation can conflict; `general` and `implementer` roles
may use permission-gated bash, while `explorer` remains read-only. File
mutation requires both global and role mutation opt-ins.

Permission gates cover write/edit/bash and network tools: `read` remains
allowed in deny/ask modes, while deferred `webfetch` is filtered in deny mode.
External plugin tool risk defaults to `exec`; less restrictive declarations
are trusted metadata and do not constrain the child process.

File tools resolve symlinks and enforce allowed roots; do not weaken this
guard. Auth writes are atomic and `0600`; never log secrets or include them in
errors. Tool output and command duration are bounded, and `context.Context`
passes through network, process, file, and tool operations.

SDK and headless code should use deny mode unless the caller deliberately opts
into `allow`/`AutoApprove` in a trusted environment. Repository text,
`AGENTS.md`, tool output, and external plugins are potentially prompt-injected
and must not override the user's request or this guide. See `docs/security.md`.

## Testing and verification

The normal suite is network-free. Provider integration tests use local
SSE/mocked servers; real provider checks require credentials and are manual.
Run these commands from the repository root:

```sh
gofmt -w <changed-go-files>
go test ./...
go vet ./...
go test -race ./internal/...
go test -race ./internal/subagent ./internal/agent ./internal/app ./internal/session ./internal/rpc ./pkg/snowsdk
go test ./internal/agent ./cmd/snow -count=1
(cd examples/sdk && go test ./... && go run .)
PYTHONPATH=sdk/plugin-python/src python3 -m unittest discover -s sdk/plugin-python/tests -v
python3 -m compileall -q sdk/plugin-python/src sdk/plugin-python/tests
(cd sdk/plugin-javascript && npm test && npm run pack:check)
./snow plugin check examples/plugins/python-sdk/manifest.json
./snow plugin check examples/plugins/javascript-sdk/manifest.json
go build -o ./snow ./cmd/snow
SNOW_TEST_BINARY="$PWD/snow" PYTHONPATH=sdk/python/src python3 -m unittest discover -s sdk/python/tests -v
(cd sdk/javascript && npm test && SNOW_TEST_BINARY="$PWD/../../snow" npm run test:integration && npm run pack:check)
python3 examples/rpc/python/client.py --snow ./snow
node examples/rpc/javascript/client.mjs ./snow
govulncheck ./...
```

After a verified feature change, refresh the user-local binary with
`./scripts/install-local.sh`.

### Coverage

- Unit tests cover the session tree, path safety, the edit tool, auth store
  permissions, OAuth refresh with a mock clock, event ordering, the permission
  mode matrix, and model/reasoning metadata and effort filtering.
- Agent end-to-end tests run the real read/write/edit/bash/grep/glob registry
  through streamed multi-tool turns, exercise the deny/allow/ask permission
  matrix, verify ordered tool results, cover provider resolve/chat/stream/EOF
  failures, and reopen SQLite sessions for continuation.
- CLI end-to-end tests drive Cobra print and JSON modes against a local
  OpenAI-compatible SSE server with no credentials or network.
- Language SDK tests run network-free unit tests and real-binary fake-provider
  integration tests on Linux and macOS.
- Benchmarks cover TUI startup, stream lag, and large-session reload.

### CI

`.github/workflows/ci.yml` runs for `main` pushes, pull requests, manual
dispatches, and calls from the release workflow. Linux and macOS run formatting
(Linux), vet, `go test ./...`, production builds, credential-free standalone
SDK/RPC examples, language-SDK integration checks, JavaScript dry-run package
checks, and plugin-SDK conformance checks. Linux also runs
`go test -race ./internal/... ./pkg/snowsdk`, cgo-disabled builds for all four
release targets, and a pinned `govulncheck` reachable-code scan. Real-provider
checks remain manual. `.github/workflows/release-alpha.yml` accepts only
`vMAJOR.MINOR.PATCH-alpha.N` tags, reuses the complete CI gate, publishes
macOS/Linux amd64/arm64 archives and `SHA256SUMS`, and marks the GitHub release
as a prerelease.

## Roadmap

Phases 0 through 4 are implemented in-tree. The list below records what each
phase delivered; the current work queue is "Known gaps / next work" under the
next heading.

| Phase | Delivered |
|---|---|
| 0 — Spec and skeleton | `go.mod`, `cmd/snow` stub, `pkg/protocol` types, interface files compiling with the `fake` provider, in-memory session store, README |
| 1 — Vertical slice | Agent loop with serial tool dispatch, SQLite session persistence, `read`/`bash`, OpenCode Go streaming chat and startup model discovery, API-key auth, print mode, basic TUI, system prompt and `AGENTS.md` load, cancellation |
| 2 — OAuth, mutations, permissions | `write`/`edit` with path gates, ask/allow/deny permissions, login/logout, ChatGPT browser/device OAuth with guarded refresh, sessions/resume/new, durable branches and `/tree`, manual `/compact`, project trust, interactive asker |
| 3 — SDK, search, RPC | Public `pkg/snowsdk`, `grep`/`glob`, JSON mode, RPC protocol v1, Python/JavaScript SDKs, extensibility core and JSON-RPC v2 stdio host, bounded steer/follow-up queue |
| 4 — Extensibility and UX | Agent Skills, MCP client, themes and keybindings, persistent ChatGPT catalog cache, fork/tree navigation, macOS/Linux platform guard, plugin permission gate, opt-in BM25 tool routing |

## Research and decisions

This section condenses the recorded research and locked decisions. Rationale
that is fully covered elsewhere is referenced rather than repeated.

### Locked decisions

| Decision | Choice |
|---|---|
| Product role | Standalone harness, not an IDE backend |
| Binary name and module | `snow`, `github.com/elmissouri16/snow-core` |
| Modularity | In-process interfaces plus JSON-RPC stdio subprocess plugins; no Go `.so` loading |
| Auth | OpenCode Go API key, optional-key/anonymous OpenCode Zen, user-configured OpenAI-compatible endpoints, and ChatGPT/Codex OAuth |
| Sessions | Snow-owned pure-Go SQLite tree (schema version 9) |
| TUI | Charmbracelet Bubble Tea |
| SDK | `pkg/snowsdk` running the same core as the CLI |
| Process isolation | No built-in process or per-extension sandbox; use external containment when required |
| Subagents | Optional, root-scoped, Codex-V2-style, built from the ordinary agent loop |

### Research notes

- pi patterns worth copying: minimal core tools plus bounded search, SQLite
  tree sessions, SDK equals CLI core, project trust is not a sandbox, event
  subscribe model, `auth.json` with `0600` plus environment fallback.
- OpenCode/snow-agent lessons: event coalescing matters for UI performance,
  permission snapshots are user-visible safety, and memory databases, goal
  mode, and research mode explode scope; Snow stays a harness, not a full IDE.
- Codex/ChatGPT subscription: track current official Codex-for-OSS endpoints
  and client requirements; community reverse-engineering is unstable. The
  mitigation is a single adapter package, a verify checklist, and the ability
  to disable ChatGPT builds.
- Go TUI ecosystem: Charmbracelet (Bubble Tea) is the de-facto standard and
  fits streaming agent UIs via `Program.Send` from agent goroutines.
- Efficiency: Go single binary, stream processing, SQLite queries that only
  materialize the active branch, coalesced UI updates, serial tools in MVP,
  pure-Go search matchers, append-only logs, bounded tool output, cancelable
  HTTP streams, and subprocess plugin cost paid only when plugins are enabled.

## Open risks and known gaps

| Risk or gap | Impact | Mitigation or status |
|---|---|---|
| OpenCode Go API shape diverges from OpenAI-compatible assumptions | Adapter breaks | Isolate adapter; golden stream fixtures; verified live endpoints |
| ChatGPT OAuth or Codex endpoint churn | Login breaks | Adapter isolation; paste fallback; manual CI smoke with credentials |
| ToS or account policy changes | Legal/product | README compliance; official OSS paths only; easy provider disable |
| Terminal keybinding variance | Editor UX | Document per-terminal newlines; config overrides |
| Symlink path escapes | File safety bug | `EvalSymlinks` plus prefix check; thorough tests |
| Model ignores tool schema | Poor loops | Tight descriptions; malformed-argument errors; run-scoped call limits and repeated-call reminders |
| Parallel tool filesystem races | Data loss | Serial tools; no parallel mutation |
| Bash/plugin/MCP/subagent OS authority | Runs as the user | Documented boundary; external container/VM required for hostile code |
| Pre-v1 API and file-format drift | Breaking changes | Stabilize `pkg/snowsdk`, `pkg/protocol`, and the session schema before v1 |
| Semantic/vector tool routing | Deferred | Await a locally downloadable open-source model with acceptable licensing and startup cost |

## Related documents

- [Using Snow](docs/using-snow.md) — TUI/CLI modes, flags, keys, commands, queues, and workflows
- [Security model](docs/security.md) — consolidated privilege and threat boundaries
- [Security reporting](SECURITY.md) — private vulnerability disclosure policy
- [Release policy](docs/releases.md) — alpha versioning, verification, artifacts, and rollback
- [SDK](docs/sdk.md) — public Go SDK lifecycle and API reference
- [Sessions](docs/sessions.md) — pure-Go SQLite session storage and schema
- [RPC](docs/rpc.md) — versioned JSONL framing, schemas, commands, and events
