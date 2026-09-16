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

The core is intentionally not a graphical application, a process sandbox, a memory
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

- No graphical dependencies or Electron contract in the agent core. The
  optional web surface stays outside it and reuses RPC rather than agent logic.
- No built-in process or per-extension sandbox. Bash, Go plugins, stdio
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
│   ├── diagnostics/         # opt-in bounded event recorder and secure JSON dumps
│   ├── goal/                # branch-scoped persistent Thread Goals
│   ├── mcp/                 # official-SDK MCP manager and tool/resource bridges
│   ├── permission/          # ask/allow/deny service and remembered rules
│   ├── plan/                # Plan collaboration-mode contract and parser
│   ├── plugin/              # lifecycle manager and Go/JavaScript adapters
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
├── examples/                # standalone Go SDK example
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
| `internal/diagnostics` | Opt-in asynchronous normalized-event recorder, sanitization, and atomic private dump writer |
| `internal/permission` | Ask/allow/deny service and remembered rules |
| `internal/plugin` | Lifecycle manager and Go/JavaScript adapters |
| `internal/plugindocs` | Deferred offline JavaScript plugin references and safe runtime-inventory tool |
| `internal/process` | Session-bound app-owned background processes, output rings, readiness, and cleanup |
| `internal/procgroup` | Shared Unix process-group configuration, signaling, and exit-state helpers |
| `internal/mcp` | Official-SDK MCP manager and tool/resource bridges |
| `internal/skills` | Agent Skills parser, catalog, and activation tools |
| `internal/provider` | `Provider` interface, registry, and adapters |
| `internal/rpc` | JSONL stdin/stdout control plane; eager, runtime-free catalog and allowlisted local control startup |
| `internal/hostcontrol` | Runtime-free allowlisted defaults/provider-status/API-key service over local config/auth helpers; no app/provider/session initialization |
| `internal/hostops` | Runtime-free descriptor-pinned CREATE and explicitly released anonymous HTTPS clone; fixed Git/helper, process-group/liveness supervision; no activation or manager DB |
| `internal/session` | SQLite/in-memory stores, topology, and session index |
| `internal/sessiondelete` | Shared inactive-session indexed deletion and managed artifact/goal cleanup; no app or provider construction |
| `internal/subagent` | Root manager, context projection, roles, V2 tools |
| `internal/tools` | `Tool`/`Registry`/`ToolHost` interfaces and BM25 router |
| `internal/trust` | `~/.snow/trust.json` project decisions |
| `internal/update` | Bounded GitHub release discovery, verification, and atomic self-update |
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
| `pkg/agentclient/rpc` | Bounded JSONL client over an owned deadline-capable connection; no process launcher |
| `internal/web` | HTTP-only trusted-LAN plus localhost manager, private project registry, RPC catalog/control/live-worker adapters, exact Host/Origin and pairing boundaries, durable host-operation admission, instance-bound public-snapshot SSE, sanitized Markdown, durable owner-bound public tool history, explicit recovery hints without replay, bounded read-only file/Git inspector; no TLS/proxy/profile stack, app/agent/session imports, or second agent loop |

### Web frontend package and build

`internal/web/frontend` is the private React 19.3/TypeScript 7/Vite 8 browser
package; `package.json` and `package-lock.json` pin its dependencies. It has no
Node production server, Next.js layer, or additional UI library. React owns only
explicitly mounted page/panel subtrees, including the conversation/composer,
transcript, sidebar, workspace pages and runtime panels. Go templates, the
first-party bounded workspace navigator and the serial browser network/admission
controller retain their separate responsibilities; source integration is not
whole-product native acceptance. Shared classic helpers retain snapshot transport and popup geometry,
not a second renderer inside React roots. Cost presentation belongs to the typed
conversation model; the obsolete script is no longer loaded or routed. No frontend dependency
enters core Go packages, the SDK, or the TUI.

Node >=22.12.0 is needed to develop these sources; frontend CI pins 24.16.0.
`npm ci --ignore-scripts`, `npm run build`, `npm test` and `npm run check` run
inside that package. Build typechecks and emits the checked-in
`internal/web/static/generated/app.js` and runtime third-party notices. Check
rebuilds into a temporary directory and compares the complete generated tree by
name and byte content without repairing it. Ordinary Go 1.27rc3 builds, including
release cross-builds, embed the checked-in assets without Node. The Go route
allowlist and same-origin security boundary remain authoritative. Source changes
require regeneration, a Go rebuild/install and an explicit manager/worker restart;
existing processes do not reload the new binary or embedded assets automatically.
See [Web frontend development](docs/web-frontend.md) for commands, ownership,
workbench limitations and the distinction between source ports and native evidence.

## Dependency direction and runtime data flow

```text
cmd/snow → app → {tui | print | rpc}
app → agent → {provider, tools, session, permission, context, compact}
provider adapters → auth + protocol
tui → app facades + protocol
snowsdk → app + protocol; never bubbletea
cmd/snow → web (local-only shell, no app construction)
cmd/snow → rpc control → {hostcontrol → config/auth, hostops} (no app)
web → agentclient process/RPC → external workers (no hostcontrol/hostops internals)
agentclient/rpc → protocol (transport client, no runtime internals)
```

`agent`, `provider`, `session`, `tools`, and `pkg/protocol` never import the
TUI or Cobra. `pkg/protocol` is standard-library-only. `internal/` is not a
stable external API; the public surface is `pkg/snowsdk`, `pkg/protocol`, and
other dependency-light `pkg/*` contracts. `go.mod` and
`README.md` both declare the Go 1.27 line (currently `1.27rc3`).

`internal/buildinfo.Version` is the single linked build-version default.
`cmd/snow` copies it into `app.Options.BuildVersion`; `internal/app.New`
normalizes and stores the value before passing it to RPC and
MCP handshakes. Go SDK sessions seed the same linked value. Release builds
replace the symbol through `-ldflags -X`, while untagged builds remain
`0.1.0-dev`.

`internal/update` is constructed by `internal/app.New` without doing I/O and is
invoked only through explicit app facades. The interactive TUI schedules an
opt-in metadata-only startup check after successful app attachment; every
available update requires an explicit Install/Skip decision before the archive
download begins, and print, JSON, RPC, SDK, version, and management startup
never invoke it. Approved downloads stream bounded byte/total snapshots into a
foreground TUI progress card. Release discovery includes published prereleases
and uses bounded fixed-origin HTTPS. Installation verifies
the checksum, exact archive shape, expanded sizes, and staged binary version,
then rechecks the pinned regular executable before same-directory atomic
replacement. TUI restart requests cross a typed lifecycle boundary to
`cmd/snow`, which re-executes only after Bubble Tea terminal restoration and app
shutdown.

### Runtime data flow

1. `cmd/snow` parses flags and builds `app.Options`.
2. `internal/app.New` loads config/auth/trust, builds the tool registry,
   constructs and fetches catalogs for only the active and explicitly configured
   subagent providers, and creates the session, permission service, provider,
   and agent. Picker-only and ad-hoc child adapters and catalogs materialize
   through shared, race-safe on-demand wrappers and caches.
3. `agent.Prompt` appends the user message, resolves credentials, starts a
   provider stream, publishes normalized events, persists the assistant
   message, then runs serial tool calls behind the permission service.
4. Tool results are appended to the session and the provider is called again
   until the model stops, errors, or the context is cancelled.
5. TUI, print, JSON, RPC, and SDK consumers observe the same
   `protocol.AgentEvent` stream; they do not duplicate loop logic. The opt-in
   diagnostics recorder subscribes to that stream through a nonblocking bounded
   queue, reports loss, and combines sanitized events with a stable idle-session
   snapshot only when a dump is explicitly requested. The TUI footer
   continuously shows current/model context usage and `/compact` has animated
   progress.

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
6. Caller cancellation before admission rejects input without persistence.
   After admission it aborts provider/tool work, preserves aborted history,
   returns the caller's context error, and pauses the attached active goal.
   Subagent manager admission waits are cancelable, so atomic controls can
   join canceled work without trapping a tool behind their admission lock.
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
12. The agent owns one structured provider-retry episode. Adapters classify
    temporary outage/throttle failures and expose `Retry-After` but do not
    schedule transient retries; ChatGPT retains only its guarded 401 refresh.
13. Pre-activity attempts repeat a side-effect-free request. Post-activity
    attempts continue from durable context; failed assistant boundaries are
    excluded, incomplete calls are never dispatched, and completed tool results
    remain visible. Restart with an unknown non-read outcome defers an active
    goal before automatic readiness.

### Provider recovery

`internal/provider` exposes `RetryAdvice` for temporary transport/overload and
temporary rate-limit failures plus shared bounded `Retry-After` parsing. HTTP
408/425/5xx, network failures, idle/truncated streams, and structured overload
codes are candidates; cancellation, auth, validation, hard quota/payment,
context, persistence, accounting, and tool failures are not. Joined failures
are retryable only when every member is retryable.

`internal/agent` applies exponential jittered backoff under both attempt and
elapsed limits. Defaults are 12 attempts/5 minutes with a 30-second delay cap
for ordinary and child turns, and 30 attempts/30 minutes with a two-minute cap
for automatic goals. Success resets consecutive failure state. Goal outage
exhaustion pauses, temporary throttle exhaustion becomes `usage_limited`, and
budget crossing retains precedence. `provider_retry` is a structured
nonterminal event; final exhaustion emits one `error`.

### Streaming events

The core publishes normalized `protocol.AgentEvent` values. Core types are
`session_updated`, `text_delta`, `thinking_delta`, `tool_start`,
`tool_progress`, `tool_end`, `tool_routing`, `permission_request`,
`user_input_request`, `usage`, `provider_retry`, `queue_updated`, `turn_done`, `error`,
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
| OpenCode Zen | `opencode-zen` | optional API key or anonymous | `https://opencode.ai/zen/v1`; live `/models` plus verified free models.dev metadata; model-specific `/chat/completions` or `/responses`; default `big-pickle` |
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

The primary API-key adapter. It attaches `Authorization: Bearer <key>` and the
stable opaque `X-Opencode-Session` conversation-affinity header, maps Chat
Completions SSE chunks and finish reasons into Snow events, surfaces
rate-limit and quota errors as structured errors, and maps normalized thinking
effort to OpenAI `reasoning_effort` while rejecting levels the selected model
does not advertise. Startup model discovery fetches `GET /models` and merges
matching IDs with OpenCode's public `models.dev` catalog for capability,
reasoning, and pricing metadata; the API key is never sent to the metadata
host and direct gateway fields win. Only explicit per-model effort arrays become
selectable thinking levels. Reasoning booleans and a generic
`reasoning_effort` parameter do not synthesize `low`/`medium`/`high`; without
advertised values the model exposes only Snow's local `off`. Discovery falls
back to the pinned static default without failing startup or logging keys.

### OpenCode Zen

`internal/provider/opencodezen` is a separate, optional-auth adapter for Zen's
promotional free routes. Credential resolution accepts an explicit key, the
`opencode-zen` Snow auth entry, `OPENCODE_API_KEY`, or an empty anonymous
credential; keyless requests omit `Authorization` completely. The provider
intersects live `GET /models` availability with verified models.dev free-model
metadata and is catalog-authoritative. New IDs require explicit zero input and
output prices (including cache charges and advertised context tiers), supported
text/tool capabilities, token limits, and a supported protocol. Paid, unknown,
and deprecated IDs cannot be selected accidentally. A seven-model bundled
catalog supplies offline fallback and local privacy/limit overrides;
`big-pickle` remains the default.

The models.dev provider package selects Responses/SSE (`@ai-sdk/openai`) or
Chat Completions/SSE (`@ai-sdk/openai-compatible`), inheriting the provider-wide
package when a model has no override. Snow does not execute these packages.
Bundled models retain local transport fallback. Both transports attach the same
stable opaque `X-Opencode-Session` conversation-affinity header and normalize
into the shared provider event contract. Temporary HTTP 429 responses carry
structured rate-limit advice and bounded `Retry-After`; the central agent policy
owns every wait and attempt so provider and goal budgets cannot multiply. HTTP
402 remains terminal usage limitation. Active keys are redacted from bounded
errors.

On a canonical-endpoint Zen catalog refresh, the provider concurrently fetches
live `/models` availability and the public models.dev `opencode` record under
the bounded discovery context. A custom base URL disables that merge unless the
internal provider config explicitly supplies a catalog URL. New free models
appear without a source update, while live pricing and deprecation override
bundled policy. `reasoning` and
`reasoning_options[type=effort].values` are normalized into model-level thinking
metadata; no model-specific effort set is compiled into Snow. Metadata requests
carry no Zen authorization. The v3 atomic 0600 catalog cache stores the verified
pricing, protocol, and capability evidence for newly discovered IDs and
rehydrates policy on load. Older schemas are invalidated so existing installs
discover the expanded catalog. Successful empty catalogs are persisted to avoid
reviving withdrawn promotions on an offline restart. A failed metadata refresh
uses verified cached metadata for IDs still advertised by `/models`; otherwise
only bundled policy applies. Advertised values serialize as
`reasoning_effort` for Chat Completions or `reasoning.effort` for Responses.
Snow's `off` setting omits the override rather than claiming the provider
disables inherent reasoning.

Zen owns a 15-minute catalog expiry plus a revision counter, forwarded through
the authenticated provider wrapper. App snapshots consult both, so opening the
model picker refreshes expired data and observes catalog changes made before an
inference request. Ctrl+R forces discovery through the same app catalog loader;
the TUI preserves the filter and any still-available selected row. Browsing
catalogs does not change the active model. Inference revalidates an expired
catalog and rejects a selected ID that is no longer approved. Discovery failures
retain the last verified snapshot and retry on a later lookup after one minute,
without extending the on-disk snapshot's age.

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
`internal/provider/responsesapi`. The request side expands normalized history
into pre-sized concrete wire items and appends the final JSON into one owned
output buffer; parity tests compare escaping, omission, ordering, raw-value
formatting, and non-finite-number errors with `encoding/json`. Valid
provider-private continuity is reused synchronously while the exported
compatibility projection retains a defensive clone. An HTTP 404/405/501 from
Responses selects and caches a Chat Completions/SSE fallback. OAuth, Codex
headers, refresh, and catalog behavior remain isolated. There is no default endpoint; discovery is
nonfatal when unavailable. Custom/Azure headers and query parameters are
excluded, and provider errors redact active keys.

### ChatGPT/Codex OAuth

`internal/provider/chatgpt` performs a side-effect-free credential check,
browser PKCE and device-code login, compatible Codex/Pi/OpenCode credential
imports, guarded automatic refresh, an origin-and-account-scoped ETag model
cache, and hardened Codex Responses SSE streaming with branch-scoped prompt
affinity, zstd compression, structured retry advice and error diagnostics, and
mandatory terminal events. The central agent retry coordinator schedules
transient recovery; the TUI/CLI report
configured, expired, or missing ChatGPT auth without refreshing during checks.

Login opens the system browser (or prints the URL with `--no-open`) and
receives the code on `localhost:1455/auth/callback`, or accepts the full
callback URL when the port is occupied or the browser is remote. The code is
exchanged for access/refresh tokens, `auth.json` is written atomically with
mode `0600`, JWT/account metadata is validated without persisting the ID
token, and the authenticated catalog is refreshed with only a same-account
backend cache as its outage fallback. The pre-login bundled model list carries
no guessed thinking efforts; selectable efforts and defaults appear only after
backend discovery. ChatGPT discovery excludes Codex's `ultra` host preset
because it enables host-side proactive multi-agent behavior and is not accepted
as a Responses `reasoning.effort`. Before `Chat`, credentials expiring within five minutes
are refreshed under the cross-process auth-store lock; a pre-stream 401
permits one guarded forced refresh and retry. WebSocket continuation remains
deferred.

Discovery uses the tested Codex `client_version=0.153.4` contract, independently
of Snow's release version. Older contracts can omit new models, including
GPT-6 Astra; caches from other contract versions are invalidated. ChatGPT
publishes its 15-minute catalog expiry and a revision counter through the
authenticated provider wrapper to the shared app loader. Reading a disk cache
preserves its original deadline; ETag revalidation renews freshness, and
failed refreshes retain only a same-account compatible cache with a one-minute
picker retry interval. `/model` reloads expired snapshots and Ctrl+R forces a
refresh without changing the active selection.

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
| `edit` | Exact string replace or patch | write | 8 MiB input/result and 10,000-match caps; bounded preview; fails on ambiguity unless `replace_all`; checks source identity and contents before replacement (see [concurrency limits](docs/security.md#protect-files-and-processes)) |
| `bash` | Run a foreground shell command in cwd | exec | POSIX `sh`; timeout, process-group cleanup, pipe-drain bounds, combined output cap |
| `process_start` | Start an app-owned background shell process | exec | Opaque handle; process group; bounded output tail; log-first readiness, with optional loopback TCP/HTTP only when network evidence is required |
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
| `snow_plugin_docs` | Create/update plugin references and inspect available JavaScript plugins | read | Deferred schema; offline API/guide/example resources; bounded pagination; safe saved/loaded metadata without config values or package execution |

`grep`, `glob`, `ask_user`, `update_plan`, `search_tools`, and session-history
tools are registered in the default builtin registry. `webfetch`, session
retrieval, artifact retrieval, plugin-development references, and the
managed-process lifecycle are deferred,
so the normal app loads the small direct `search_tools` recovery schema while
keeping their full schemas out of unrelated provider requests. `ask_user` has
no discovery metadata: its full
schema is sent with the other direct built-ins on every tool-capable request,
and the explicit SDK/CLI `Tools` allowlist remains authoritative. A choice
returns its exact label; Other and free-form responses return trimmed text.
The model-facing result is ordered JSON.

The five managed-process schemas form one deferred bundle: selecting any member
exposes all five, and retained process records keep the bundle visible across
turns so lifecycle control is never stranded. Their detailed system guidance is
also conditional on `process_start` exposure. One app-owned
`internal/process.Manager` outlives individual tool calls and continuously
drains child output while the serial agent loop proceeds. Processes are scoped
to the active session but shared by its branches; switching root sessions stops
and reaps every managed group before clearing the old inventory, while forks
never copy handles. `App.Close` closes agent admission, then terminates and reaps
managed groups before session/path resources. State is not reconstructed from
persisted PIDs after restart. The global `processes` configuration bounds
concurrent children, retained terminal records, and output per record; individual
child lifetime ends on natural exit, explicit stop, session switch, or app close.

### Performance evidence (2026-08-24)

Local Apple M3 Pro release-build proxies, recorded to make the context/startup
tradeoffs auditable rather than contractual:

- A clean fake-provider request exposes 15 schemas / 8,506 serialized schema
  bytes, down from the 14,031-byte pre-change recurring-context proxy (39.4%).
- A process-relevant request expands the complete five-tool lifecycle bundle on
  demand: 20 schemas / 11,989 bytes; an unrelated request carries none of those
  five schemas.
- Five sequential one-shot fake-provider launches measured 0.03–0.04 s real
  time and 47,480,832–48,381,952-byte maximum RSS (47,824,896-byte median),
  versus the earlier 48.1–48.7 MB startup proxy. A later isolated
  provider-initialization benchmark reduced the median from 3.975 µs / 7,136 B /
  60 allocations to 2.736 µs / 4,424 B / 59 allocations by retaining lazy
  constructors for inactive adapters: 31.2% less time/op and 38.0% fewer
  allocated bytes in that startup component. The per-start allocation saving is
  2,712 bytes with the default provider inventory, so whole-process RSS remained
  below the resolution of repeatable measurement.
- Shared Responses request construction now uses typed, exactly pre-sized wire
  items and a profiled single-output-allocation JSON appender. For 1,500
  messages and 20 tool schemas, plain history fell from a 1.158 ms median /
  2,408,961 B / 15,036 allocations to 0.396 ms / 522,560 B / 1,505 allocations;
  tool-heavy history fell from 0.815 ms / 1,546,497 B / 11,279 allocations to
  0.287 ms / 386,473 B / 2,254 allocations; and provider-continuity history
  fell from 3.627 ms / 4,963,204 B / 40,635 allocations to 1.207 ms / 973,374 B /
  7,517 allocations. Those medians are 64.8–66.7% less time, 75.0–80.4%
  fewer allocated bytes, and 80.0–90.0% fewer allocations. A separate 2 MiB
  image case fell from
  2.446 ms / 12,050,535 B / 30 allocations to 0.916 ms / 5,603,553 B / 5
  allocations; its remaining bytes are the unavoidable encoded data URI and
  owned request output. Stage profiles identified dynamic maps and temporary
  message slices in transformation, then reflective interface handling and the
  output clone in `encoding/json`; the final appender retains exact wire parity
  and allocates its output buffer once.
- A warm 1,500-message SQLite `ContextMessages` projection reduced from a
  201.824 µs median / 1,359,489 B / 1,518 allocations to 75.457 µs / 526,209 B /
  26 allocations by skipping the unused compaction index and packing defensive
  output storage in 64-message retention chunks: 62.6% less time, 61.3% fewer
  allocated bytes, and 98.3% fewer allocations. The cold recursive-query/decode
  path remained about 9.2–9.4 ms while bytes and allocations fell 10.6% and
  6.1%; recurring warm projection is the optimized path. A follow-on bounded
  MemoryStore lineage projection for 5,000 messages with a 100-message
  post-compaction tail reduced 768.5 µs / 3,918,900 B / 87 allocations to
  17.0 µs / 75,448 B / 20 allocations (97.8% less time and 98.1% fewer bytes)
  while exact history remained append-only.
- Startup recovery now uses indexed tail queries for interrupted tool calls and
  filtered branch-state queries for active skills instead of decoding complete
  history. Reopening a compacted 5,000-message, 12 MB SQLite session fell from
  about 113 ms / 97.2 MB allocated to 34.8 ms / 0.76 MB allocated; retained live
  heap stayed near 0.09 MB. The query fallbacks remain available for custom
  stores.
- OpenAI-compatible Chat request construction now pre-sizes transformations and
  uses a differential-tested JSON appender. At 1,500 messages and 20 tools,
  plain history fell from 908.3 µs / 3,343,277 B / 4,538 allocations to
  588.1 µs / 1,091,411 B / 1,507 allocations; tool-heavy history fell from
  885.0 µs / 2,189,334 B / 4,537 allocations to 498.9 µs / 809,260 B / 2,256
  allocations. Byte-oriented SSE ingestion reduced the 600-delta Chat proxy
  from 630.7 µs / 240,197 B / 3,618 allocations to 581.0 µs / 90,797 B / 1,211
  allocations. Reusable Responses SSE buffers reduced its corresponding proxy
  from 716.2 µs / 396,561 B / 8,448 allocations to 649.1 µs / 87,597 B / 4,843
  allocations. Wire and normalized-event parity remain covered by differential
  and stream corpus tests.
- Persistent event-subscriber workers preserve serial event order, concurrent
  subscriber callbacks, timeout eviction, reentrant-drain rejection, and
  payload isolation without a goroutine and timer per delivery. Monitored
  subscriptions expose timeout eviction to headless print/JSON consumers so a
  truncated stream cannot be reported as successful. A 256-event,
  one-subscriber batch fell from 828.5 µs / 286,050 B / 2,315 allocations to
  202.0 µs / 115,912 B / 523 allocations. Forking 1,500 inherited subagent
  messages in one batch fell from 878.0 µs / 2,387,694 B / 11,792 allocations
  to 645.3 µs / 2,328,852 B / 8,814 allocations; durable forks likewise use one
  SQLite transaction, and final-result lookup no longer clones full history.
- For the TUI, ingesting 10,000 adjacent deltas reduced from a 4.376 ms median /
  6,492,386 B / 20,025 allocations to 0.682 ms / 732,960 B / 27 allocations.
  The follow-on packed-fragment mailbox reduced that then-current 0.682 ms /
  732,961 B proxy to 0.596 ms / 399,136 B while bounding fragment metadata by
  payload size. Single-snapshot hydration, borrowed SQLite blobs, compacted
  context reuse, and retained-row rendering first reduced a 5,000-message /
  12 MB hydration from 256.0 ms / 253,929,896 B / 306,617 allocations to
  126.8 ms / 120,659,052 B / 105,086 allocations. Schema-v11 hydration
  projections now scan exact ancestry without old message blobs and fetch the
  visible suffix in 256-entry pages; embedding required input-history/plan text
  in the message-light projection also removes two whole-ancestry JSON queries.
  The assistant-heavy proxy is 115.9 ms / 106,075,832 B / 99,405 allocations.
  Relative to the original path that is 54.7% less time, 58.2% fewer bytes, and
  67.6% fewer allocations, with the same 2,000-row limit, omission count, input
  history, latest plan, context usage, and tool pairing. A mixed 5,000-entry
  user/assistant/tool proxy is 46.1 ms / 27,686,456 B / 111,622 allocations.
  Lightweight ancestry alone is 14.9 ms / 4,564,576 B / 35,043 allocations for
  5,000 entries; rendering retained Markdown rows is now the
  dominant cost. Reusing an unchanged 120-column viewport and exact fitted frame reduced a
  stable `View` from 170.592 µs / 112,947 B / 211 allocations to 51.152 µs /
  24,342 B / 144 allocations (70.0% less time, 78.4% fewer bytes). The cache is
  keyed by content generation, scroll, dimensions, and exact frame input; live
  changes still render normally.
- The stripped binary is 66,229,922 bytes (20,728,353 bytes at gzip `-9`),
  168,848 bytes / 0.26% above the 66,061,074-byte baseline. Surf and Bleve remain
  the dominant linked binary-size hotspots; this work keeps them for behavior
  compatibility and removes their eager recurring work instead.
- At 1,000 tools, steady-state router samples were 1.93–1.94 ms / about 42 KB /
  422 allocations for global search and 4.62–5.01 ms / about 279 KB / 3,555–3,557
  allocations for namespace-first search. Index construction is lazy,
  cancellation-aware, and shared by concurrent first searches.

Reproduction uses `go build -trimpath -ldflags='-s -w'`, `/usr/bin/time -l`,
the fake-provider JSON `tool_routing` event, and `go test` benchmarks
`BenchmarkBuildRequest`, `BenchmarkBuildRequestStages`,
`BenchmarkBuildRequestImages`, `BenchmarkBuildChatRequest1500`,
`BenchmarkChatSSE600`, `BenchmarkResponsesSSE600`,
`BenchmarkSQLiteContextMessages`, `BenchmarkMemoryContextMessagesAfterCompaction`,
`BenchmarkSQLiteAppendBatch1500`, `BenchmarkSQLiteBranchHydration5000`,
`BenchmarkEventBusDispatch256`,
`BenchmarkForkContext1500`,
`BenchmarkMailboxIngestion`, `BenchmarkSessionHydration5000`,
`BenchmarkSessionHydrationMixed5000`, `BenchmarkViewNormalAndNarrow`, and
`BenchmarkSearch/tools_1000`, all with
`-benchmem` and repeated counts. Numbers
vary by host and should be compared only with the same command and checkout
conditions.

On 2026-09-04, the built-in search tools gained a complete-line copy fast path
and per-search reuse of compiled ignore patterns and inherited rules. The
additional inherited-rule cache is bounded to 256 directories and 4,096
backing-array rule slots; overflow uses the original uncached assembly.
Same-host before/after benchmarks (Go 1.27rc3, Apple M3 Pro, `-cpu=1`, medians
of three samples at `-benchtime=10x`) measured:

| Benchmark | Before: time / B/op / allocs/op | After: time / B/op / allocs/op |
|---|---|---|
| `BenchmarkGrep10MB` | 21.28 ms / 22,477,364 / 200,303 | 18.85 ms / 11,276,890 / 100,299 |
| `BenchmarkGlob2000` | 196.95 ms / 19,825,568 / 499,670 | 196.15 ms / 8,107,912 / 177,739 |
| `BenchmarkReadSearchLines100000` | 5.43 ms / 22,432,800 / 200,002 | 3.26 ms / 11,232,800 / 100,002 |
| `BenchmarkSearchIgnore2000` | 88.07 ms / 16,163,592 / 444,190 | 63.80 ms / 4,735,616 / 128,719 |

Allocation volume fell about 50% for complete grep and 59% for complete glob.
The glob wall-clock samples varied from 178 to 213 ms after the change, so
this run does not establish a stable end-to-end glob speedup. Stage-only gains
must not be reported as full-operation gains. The fixtures and reproduction
command are documented in [the performance guide](docs/performance.md).

A second 2026-09-04 pass added one buffer reservation to process-output
sanitizing and reused the existing lowercase path for mention basename matching.
Go 1.27rc3 / Apple M3 Pro medians (`-cpu=1 -benchtime=100ms -count=3`):

| Benchmark | Before | After |
|---|---|---|
| `BenchmarkSanitizeProcessOutput/131072` | 0.906 ms / 514,840 B / 25 allocations | 0.609 ms / 131,104 B / 2 allocations |
| `BenchmarkProcessOutputLines` | 2.302 ms / 1,808,232 B | 2.126 ms / 1,260,656 B |
| `BenchmarkMentionMatching` | 210 µs / 133,440 B | 197 µs / 133,440 B |
| `BenchmarkMentionMatchingMixedCase` | 413 µs / 277,440 B / 4,012 allocations | 317 µs / 229,440 B / 2,012 allocations |

Process sanitizing plus wrapping allocates 30% fewer bytes; sanitizing alone
allocates 67–75% fewer bytes for 4–128 KiB inputs. Mention matching takes about
6–23% less time across these lowercase/mixed-case 2,000-file fixtures; this does
not measure file discovery. Escaping, invalid UTF-8 handling, result casing,
and match ordering remain unchanged. These benchmarks are outside the ceiling
guard:

```sh
go test ./internal/tui -run '^$' \
  -bench '^(BenchmarkSanitizeProcessOutput|BenchmarkProcessOutputLines|BenchmarkMentionMatching|BenchmarkMentionMatchingMixedCase)$' \
  -benchmem -benchtime=100ms -count=3 -cpu=1
```

A third 2026-09-04 pass added two three-line fast paths: leave single-text
tool results below the pruning threshold alone, and reuse single-text message
strings during checkpoint detection. Same-host Go 1.27rc3 / Apple M3 Pro
medians (`-cpu=1 -benchtime=100ms -count=3`):

| Benchmark | Before | After |
|---|---|---|
| `BenchmarkPruneMixedHistory/owned-50` | 63.29 µs / 275,910 B / 55 allocations | 16.31 µs / 71,100 B / 5 allocations |
| `BenchmarkPruneMixedHistory/defensive-50` | 70.66 µs / 293,495 B / 107 allocations | 21.97 µs / 88,684 B / 57 allocations |
| `BenchmarkPruneMixedHistory/owned-500` | 331.23 µs / 2,119,153 B / 505 allocations | 13.04 µs / 71,097 B / 5 allocations |
| `BenchmarkCheckpointContextUsage/tail-1500/checkpoint-32768` | 11.00 µs / 40,960 B / 1 allocation | 0.752 µs / 0 B / 0 allocations |

Pruning fixtures contain 50 or 500 unchanged 4 KiB tool results plus one 64 KiB
result that requires pruning. They measure the pruning helpers, excluding
artifact persistence. The checkpoint fixture measures persisted-token lookup
with a 32 KiB summary body and 1,500 retained messages before the checkpoint.
These are context-preparation measurements, not complete agent-turn timings.
Multi-block handling, threshold boundaries, artifact callbacks, and defensive
ownership retain their existing behavior. Reproduce outside the ceiling guard:

```sh
go test ./internal/compact ./internal/agent -run '^$' \
  -bench '^(BenchmarkPruneMixedHistory|BenchmarkCheckpointContextUsage)$' \
  -benchmem -benchtime=100ms -count=3 -cpu=1
```

The 2026-09-05 runtime fixes add four small performance changes (34 added and
12 removed production lines): incremental checkpoint section assembly, safe
terminal-text reuse, bounded rune-prefix decoding, and amortized subprocess
buffer compaction. Same-host three-sample medians show 6.19x faster normalization
for a 28 KB concentrated checkpoint, 2.30x faster ordinary tool previews, and
41% less time capturing 32 MiB of process output. A full default log buffer uses
about 256 KiB more live heap; checkpoint stress peak RSS fell about 14%.
See [the complete measurements](docs/runtime-fixes-performance.md) for raw
results, reproduction commands, smaller regressions, and scope limitations.

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

Tool descriptors separately carry a normalized effect (`read_only`,
`mutating`, or `conditional`). Plan Mode uses one shared effect policy for both
schema exposure and authoritative pre-dispatch enforcement, before the
permission broker; permission approval cannot override a Plan denial. Missing
effects derive conservatively from risk, arbitrary Bash is mutating, and
conditional delegation is revalidated by the owning subagent manager against
the resolved child capabilities.

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
    Remember(req Request, d Decision) // active-session rule
}
```

Every fresh interactive session starts in `ask`; `--permission` is an explicit
launch-only baseline override. TUI `/permissions` and Settings changes flow
through the permission service's session metadata handler, so they survive
resume but never become a default for a new session or project. Global and
project configurations ignore the removed `permission_mode` field for upgrade
compatibility; it cannot alter the launch baseline or active-session state.

The interactive TUI supplies an `Asker`; the headless SDK defaults to `deny`
for mutating tools unless the caller deliberately opts into `allow`/
`AutoApprove` or installs a trusted permission handler. Raw RPC `ask` enables
manual correlated replies and blocks until the host replies, rejects, cancels,
or closes the process; no headless surface silently approves. Project trust is
resolved on canonical paths before TUI runtime construction; every undecided
interactive project prompts. Runtime `/trust` changes apply on the next launch.
Trust controls input loading (project config, configured system-prompt files,
plugins, MCP declarations, skills) and is not a sandbox.

## Config and project context

Global configuration lives in `~/.snow/config.json`; secrets in
`~/.snow/auth.json`; trust decisions in `~/.snow/trust.json`; sessions under
`~/.snow/sessions/`; and TUI bindings/themes under `~/.snow/keybindings.yaml`
and `~/.snow/themes/*.yaml`. Project-scoped overrides use
`<project>/.snow/config.json` and are trust-gated. Typed settings and raw
MCP/skill section mutations share one process-wide and cross-process
read-modify-write lock, then publish with atomic replacement so concurrent Snow
processes preserve unrelated changes and unknown fields. See
`docs/configuration.md`.

Defaults include provider `opencode-go`, fresh interactive-session permission
`ask` (headless SDK defaults to deny), thinking `off`, 256 KiB tool output, a
120 s bash timeout,
a 10-minute stream-silence watchdog, and a 100 KiB project-context cap.

### Context assembly

1. Base preamble: explicit SDK `SystemPrompt`, trusted-project configured
   file, global configured file, or embedded `internal/context/system.md`.
2. `AGENTS.md` walk from cwd upward (bounded by depth and total bytes);
   nearest-first, always loaded, and documented as a residual prompt-injection
   risk.
3. Optional `CLAUDE.md` compatibility read (off by default in current app
   wiring).
4. Startup skill metadata plus root-only MCP instructions. Shell, mutation,
   process, and subagent guidance is appended per request only when matching
   tool schemas are actually exposed.
5. Per-request collaboration-mode instructions from embedded
   `internal/plan/system.md` and activated-skill instructions.
6. Goal-bearing turns receive separate trailing internal context rendered from
   embedded templates under `internal/goal/`; this is not system context. When a
   provider attempt produces an assistant boundary, Snow atomically stores each
   sent fragment immediately before that owning response as provider-only
   `internal_context` entries. `ContextMessages()` restores those private items
   in exact request order so later ChatGPT/Codex prompts extend the prior cache
   prefix; ordinary history, RPC, SDK, TUI, catalog, and web projections omit
   them. Within one high-level turn, unchanged recurring goal, mode, and plugin
   fragments are sent once and then supplied by durable history; a successful
   compaction clears that request-local suppression so active steering is
   reasserted. No-activity retries keep using the original ephemeral request
   and do not persist duplicate steering.

Configured prompt files are bounded by `context_cap_bytes`; project prompt
paths are trust-gated, confined to the canonical project root, and reject
symlink components. Each discovered `AGENTS.md` is opened through a pinned
parent-directory handle, must remain a regular non-symlink file, and is read
only through the remaining byte budget before a truncation notice is added.
The global `fixed_context_budget_percent` separately measures the final system
text plus exposed schemas against the model window (25% by default; unknown
windows use 32,768 estimated tokens). It is an admission guard for new skills,
model changes, and request-time routed schema growth, never a silent truncation
mechanism; restored over-budget
state remains intact and visible in the context report.

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

The current on-disk schema version is 12. Tables include `session_meta`
(header, title, provenance), `entries` (append-only messages, compaction
entries, and branch-local agent turn/step markers), `session_branches` (branch tips
and lineage), `thread_state`
(collaboration mode per branch), `thread_goals` and related cost/deferral
tables (persistent Thread Goals), and `subagent_threads` (child topology).
Forks copy branch state, goal estimates, managed objective resources, and
subagent topology where applicable. WAL transactions and indexed branch
queries keep open and reload bounded; opening never performs a full scan.

Goal mutations use one typed optimistic-conflict contract across controller,
memory, and SQLite layers. Conflicts expose only current goal/session/branch
identity plus a controller binding generation, allowing `update_goal` to direct
a deterministic refresh without leaking objective text or store paths. Tool
progress is recorded only after successful durable results. Three consecutive
automatic turns with the same unresolved terminal conflict first defer and then
pause the still-current goal, preventing unbounded reinjection even when the
assistant emits explanatory text.

Each durably admitted user, automatic-goal, or child-agent run appends one
`agent_turn_v1` metadata marker before provider execution. Each logical
provider-loop iteration appends one `agent_step_v1` marker immediately before
its first provider attempt. Tool-result continuations start new steps;
transport retries and overflow recovery retain the active step ID, while
compaction and auxiliary provider requests add no step. Counting both marker
classes through the active tip gives exact branch- and fork-local totals without
a mutable session-wide counter. The root TUI renders `turns:<n> · steps:<n>` for
only the active root branch; descendant child databases remain independent.
For historical prefixes written before a marker class existed, built-in stores
conservatively infer user turns from durable user messages and infer steps from
durable assistant messages; pre-marker automatic-goal boundaries remain
unknown rather than being guessed. Explicit markers become authoritative at
their first appearance, so new work remains exact without rewriting append-only
history. `AgentEvent.TurnSequence` remains
a separate process-local correlation order that restarts with the process.

### Provider-facing compaction

Compaction appends a logical boundary rather than rewriting entries. The
provider projection replaces an old complete-turn prefix with a structured
working-state checkpoint and retains at least the configured recent turns.
Planning fails closed if a boundary would split an assistant tool call from its
result. Opaque provider continuity leaves context only with its complete owning
turn. Completed assistant-call/tool-result cycles inside one long tool-driven
turn are also safe boundaries when prefix projection does not consume the
retained prior-turn floor, so old cycles may compact while current and recent
cycles remain exact. They remain eligible after a terminal assistant response.
Planning first attempts an ordinary complete-turn prefix. If an
assistant-originated automatic goal is itself the oversized recent turn and no
such prefix exists, the planner deliberately retains the configured number of
newest complete cycles (plus an unresolved active cycle) and checkpoints the old
prefix. The goal objective is injected separately on every request. This
progress fallback is structurally limited to assistant-originated goal turns,
works across prior checkpoints, and never weakens call/result pairing or opaque
provider-state ownership. Besides the global pressure trigger, aggregate tool
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
authority for replay. The deterministic local fallback partitions command and
non-command evidence so each full payload appears once, uses references for
failure sections, and renders one retrieval helper for the complete verified
artifact list.

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
- Physical exact-entry forks create an independent session with provenance and
  remove the complete randomly named staging database/sidecar set, including
  its lease, before returning the independently leased destination.
- Detached clean Git-worktree forks use a bounded direct-argument Git utility.
- Prior-session reference is deliberately narrower than a general memory
  product: `session_search` rebuilds a disposable SQLite FTS5 corpus, and
  `session_reference` imports at most three tip-pinned, bounded, untrusted
  snapshots per target branch. The active session is excluded from both FTS
  corpus selection and its file-identity key, so current WAL churn does not
  rebuild an index that cannot return current-session results; changing the
  active exclusion or any historical database still invalidates it. Tool
  content, reasoning, images, provider-private data, credentials,
  permission/trust state, goals, queues, and child databases are excluded;
  references transfer information only and no authority.

## Plugins

The extensibility core is implemented in `pkg/plugin`, `internal/tools`, and
`internal/plugin`. Static Go plugins use `Manifest`, `Register`, and `Close`;
no Go shared-object loading is used. The manager owns registration, namespaced
tool descriptors, event subscriptions, diagnostics, and reverse-order
lifecycle.

JavaScript API 2 adds optional contracts in `pkg/plugin/extensions.go` and UI data
in `pkg/protocol/extensions.go`. App facades own controls and state; TUI rendering
stays outside core. Promise settlement is published only after the VM CPU
watchdog is detached. Context cancellation joins accepted host operations.
Pure ordered hooks intercept new input, request context, and tool execution;
transforms are audited without rewriting history. UI views are bounded,
coalesced, and cached. Typed settings use existing configuration writes; scoped
SQLite state never advances the conversation cursor. Selected child tools run
in independent profiles and are persisted in schema 12. See
[JavaScript extensions](docs/plugin-extensions.md) for the supported surface and
limits, including Plan-mode and recursive-child restrictions.

Compiled Go plugins are supplied through `GoPlugins`; local bundled JavaScript
plugins use `JavaScriptPlugins`, `js_plugins`, or `--js-plugin`. The JavaScript
adapter lives in `internal/plugin/javascript`: one serialized Goja worker per
plugin per root session, bounded observation queues, context cancellation,
plain-error/result bridges, and no implicit host globals or module loading.
JavaScript tools declare a per-handler subset of manifest host built-ins. Risk
is derived conservatively, and nested operations share the agent admission
helper for Plan, preflight, invocation policy, and permission checks. Plugin
origin and package/config fingerprints scope approval reuse. Nested results
remain inside one outer transcript tool pair. Children exclude JavaScript by default. API 2 tools with explicit child opt-in
can be selected per spawn, constrained by the role and persisted fingerprints. `NoPlugins` disables Go and JavaScript loading.

Go handlers retain existing inline event delivery and privilege semantics.
JavaScript observers enqueue sanitized copies without blocking event delivery.
JavaScript globals are ephemeral; scoped KV and branch workflow state have
separate persistence. Failures are diagnostic, and runtime interruption
invalidates the plugin until explicit reload or the next launch. Goja does not provide heap quotas
or OS containment. The public protocol remains standard-library-only.

External executable transport stays removed; legacy `plugins` declarations and
`--plugin` remain inert/unsupported. `snow plugin` now manages local JavaScript
registrations and local authoring/check/test operations. See `docs/plugins.md`
for the contract and complete limits.

The TUI, SDK, and RPC share app-owned individual JavaScript registration
controls. Status separates saved enablement from the current loaded catalog,
includes disabled declarations without reading packages, and reports pending
restarts. Scoped atomic configuration updates preserve project precedence;
explicit launch overrides remain controlled by launch options. Enablement
changes take effect only on the next launch, preserving active plugin work.

Branch-aware API 2 workflow state uses optional `session.WorkflowStateStore`
implementations for memory/SQLite and versioned `plugin_workflow_v1` metadata,
without a schema bump. Complete ancestry projection preserves historical forks,
compaction, and isolated siblings. Immediate atomic root-command updates compare
branch/tip identity, bound live state and append-only history, and may replace
one plugin's tool restriction in the same commit. `ctx.storage` remains separate
and does not advance the conversation tip. App-owned immutable policy snapshots
intersect loaded plugin restrictions with existing schema/routing/admission
policies for root, nested host calls, and descendants.

Pure root `before_session_change` and `before_compaction` gates accept only a
block reason. Explicit `workflowKeys` preload fresh bounded owner values without
granting hooks I/O. `plugin_session_changed` is a normalized post-commit event
published after transition locks are released and new hosts are bound.

Single-loaded-JS reload uses detached candidate registration and atomic owner
replacement while preserving manager/registry identity. Root/goal/host/command
and child lifecycle admission exclude incompatible work; children retaining the
target's tools must be closed. Pre-commit failures preserve the old catalog;
post-commit cleanup/readiness failures produce an applied receipt with bounded
diagnostics. Generation invalidation prevents stale contexts from updating the
replacement. TUI, SDK, and RPC call the app-owned reload facade. Enablement still
requires restart, and Go/MCP/SDK-owned descriptors remain untouched.

TypeScript scaffolds use synchronous default factories and a generated entry
bundled to neutral IIFE JavaScript. `snow plugin test` executes actual Goja with
an explicit ordered fake host, copied memory fixtures, lifecycle callbacks, and
bounded assertions; it never installs dependencies or invokes real providers.
The `agent-profiles` and `workflow-guard` examples include built JS and fixtures.
See [Plugin workflows and reload](docs/plugin-workflows.md) for the canonical
contracts and limits. Node compatibility, reload-all, remote distribution,
provider registration, and new native modes remain outside this milestone.

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
descriptions from standard user, explicitly configured, and trust-gated project
paths under a 64 KiB catalog budget.

`activate_skill` loads escaped full instructions, the TUI autocompletes
enabled leading `$skill-name` directives, and a directive activates before
provider dispatch while recording branch-scoped state. `deactivate_skill`
removes one named active skill, or all active skills only via `name: "*"`, and
atomically persists a provider-hidden lifecycle marker with the tool result so
the next continuation and resumed sessions omit that guidance.
`read_skill_resource` verifies the discovery-time directory identity before
using a pinned per-operation `os.Root` for filesystem resources. Activated content is
reattached on every provider call and reconstructed from successful markers
and session history after resume so compaction does not drop it; current
trust/disable/tool policy filters stale activations. A core policy appended
after active skill content keeps each skill's methods, style and stopping
conditions subordinate to the enclosing user request: limited skill output does
not cancel explicitly requested surrounding work, while skill activation alone
never authorizes extra side effects. Collaboration policy, safety and tool
permissions remain authoritative. New activations are admitted atomically
against the final serialized fixed-context budget and are rejected before
result/marker persistence rather than truncated. Existing resumed state remains
grandfathered and observable through `/context`. See `docs/skills.md`.

Global and trust-gated project `skills.disabled`/`skills.overrides` policy can
hide entries from prompts and activation without deleting their files. CLI
`skills list|get|enable|disable`, SDK `SkillInventory`, and TUI `/skills`
expose that inventory. The Skills panel uses app-owned `SkillStatuses` and
`SetSkillEnabled` to display/save named policy in the effective global or
startup-trusted project scope. Enter/Space toggles saved enablement; generation-
guarded asynchronous writes keep errors and status inside the panel. Saved policy
and immutable runtime enablement are distinct: restart-required rows do not
change live catalogs, tool schemas, active skills, or child agents.

## Embedded plugin authoring references

`internal/plugindocs` embeds `resources/` for the deferred read-only native
`snow_plugin_docs` tool. Its `overview`, `list`, `search`, and `read` actions
progressively expose the complete creation/update workflow, handwritten
capability/example maps, and 66 synchronized canonical guide/type/example files.
`read` uses resource-relative paths, a 1-based line `offset`, and line-count
`limit`; `list` and `search` paginate results with 1-based `offset`/`limit`. `plugins` supplies safe runtime
registration metadata or `plugin_id` details without reading/executing disabled
packages. Registrations, loaded inventory, and offline examples stay distinct.
Actual existing-package inspection and edits use ordinary rooted file tools.
No action enables, modifies, executes, or reloads a plugin, and reference access
does not authorize those effects or provide an OS sandbox.

This replaces the built-in `snow-js-plugin` skill completely, without a legacy
alias or policy migration. An old `skills.overrides.snow-js-plugin` entry is
inert unless a user supplies an actual same-named filesystem skill. Tool
allowlists control the new tool independently of skills/plugins configuration;
`--no-skills` and `--no-plugins` do not disable read-only references. The bundle
requires no checkout, extraction, or network access. Maintainer-only
`internal/plugindocs/scripts/sync_resources.py` and
`scripts/tests/test_plugin_docs.py` maintain source/hash parity. See
`docs/plugins.md#plugin-authoring-references` and `docs/maintaining.md`.

## Tool routing

Existing tools and zero-value discovery metadata remain always loaded.
Native, Go-plugin, SDK, and MCP registrations may opt into
`deferred` discovery per tool. Snow retains a compact schema-free metadata
snapshot after startup registration and lazily builds the in-memory Bleve BM25
indexes on the first non-empty search. Candidate windows start at 20 and double
only when permission filtering has not found five usable results. Registry
projection filters immutable metadata before cloning accepted parameter JSON.
`search_tools` provides an explicit recovery pass; index/search failures use a
bounded metadata ranker with a five-tool/64-KiB fallback instead of exposing the
whole catalog. Routing emits structured metrics but does not make an extra LLM
call. Optional semantic/vector routing remains
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
write/edit require both mutation switches. In root Plan Mode, spawn, follow-up,
and resume validate the resolved tool profile and persisted role fingerprint;
role names do not bypass Bash/write/unknown-capability rejection. Child system
prompts are assembled
from the finalized child registry: MCP, process, shell, mutation, and recursive
delegation guidance is included only when the corresponding capability is
present. Root startup validates the active and explicitly configured child
providers; other catalogs resolve on demand through context-aware manager
callbacks.

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
untrusted. The TUI and trusted RPC hosts serialize root/child permission
requests through the same attributed FIFO broker. SDK/print ask without a handler denies; no
headless surface silently approves. Child `ask_user` stays excluded, preventing
ambiguous input routing. See
`docs/subagents.md`.

## TUI and surfaces

The Bubble Tea v2 TUI is the interactive default. Declarative `tea.View` state
owns alternate screen, focus reporting, and mouse capture; the v2 decoder and
renderer negotiate enhanced keyboard, synchronized output, and Unicode widths.
Bubbles v2 textareas use virtual cursors, and Lip Gloss v2 styles share the
existing bounded geometry. Background reports and supported mode 2031
notifications refresh built-in, custom, and plugin palettes while preserving
interactive state; notification mode ownership is restored after renderer
shutdown. The TUI owns OS signals through `signal.NotifyContext` and disables
Bubble Tea's internal signal handler to avoid competing shutdown paths. Core
and public SDK packages remain UI-independent.

Terminal status stays in `internal/tui/terminal_status.go`. Declarative title
and progress use the existing reducer's root work/approval/input state. Global
preferences apply live through an app facade without changing agent/RPC/SDK
contracts. Generic attention/desktop alerts are deduplicated and focus-gated;
child completion, snapshots, stale turns, and goal continuations do not announce
root completion. Manual compaction settles from its own completion event or
command result in `terminal_compaction.go`. Command identities captured during
core admission and an epoch/sequence watermark fence delayed events across
consecutive operations, including external successors. Pre-admission rejection
has no operation identity and stays governed by command generations.
Locally requested compaction defers alerts until its final command result so
mailbox-cleanup errors override provisional stream success. Pre-admission errors
report failure, and cancellation reports Stopped. Automatic goal-compaction
failures announce once at the idle-core terminal goal boundary, without treating
intermediate compaction success or goal snapshots as completed work.
A generation-scoped, cancelable one-second progress keep-alive
goes through a Bubble Tea program filter and `RawMsg` output, deriving state
immediately before emission. Only these terminal-only messages reuse the last
complete `tea.View`; all ordinary updates, including resize and focus, invalidate
that shortcut. Progress timers stop at idle, and Bubble Tea restores owned
title/progress state on quit, cancellation, and suspend.

Frame composition performs one final padding/clipping pass and reuses a bounded
composer view until input, cursor, selection, geometry, or styling changes.
Textarea is a reproducible Bubbles v2.2.1 snapshot under `internal/tui/textarea`:
its wrapping patch avoids string conversion/grapheme scans for printable ASCII,
and its view patch styles only visible rows while preserving viewport bounds.
Unicode and input lifecycle logic remain upstream behavior;
the original tests, license, source hashes, and sync script are retained.
Fresh before/after measurements improve all seven rendering/editing fixtures
against v1. The follow-up audit adds stable 8 KiB insertion/deletion fixtures for
ASCII, accented text, CJK, and emoji; fixed latency improves 11–34% against v1.
The standard benchmark gate covers both sets of fixtures. See
[TUI performance](docs/tui-performance.md#charm-v2-migration-measurements).

The TUI renders a transcript with
markdown, streaming updates, model/provider pickers, model-aware `/thinking`
effort selection, login/logout, permissions, sessions, slash completion, and
`@` file mentions. A leading `$` autocompletes enabled Agent Skills. Strict
bounded YAML custom themes and keybindings support global and trusted-project
precedence with warnings.

The active composer queues plain Enter as steering and Alt+Enter as a
follow-up; Shift+Enter and Ctrl+J remain multiline, and abort clears/restores queued TUI
text. Queue delivery is bounded, one-at-a-time, after complete serial tool
batches. Top-level Shift+Tab toggles Default/Plan mode (queued to `turn_done`
while busy).

The TUI uses Bubble Tea's alternate-screen, app-owned viewport so scrolling
cannot reveal stale frame chrome. `tui.mouse` defaults to `true` so wheel and
trackpad gestures stay inside Snow's viewport; primary drag uses Snow
selection/copy, F6 toggles app/native mouse mode, and right-click opens Snow's
bounded **Copy selection** menu without disabling viewport mouse reporting. In
app mouse mode the accented `provider/model ▾` header segment opens a centered
model card layered into that same frame. Typing filters immediately, cached
catalog refresh preserves the query and stable model selection, and a selected
model's thinking-effort step remains in the card. Standalone `/thinking` and
its header control use the same centered fixed-frame card rather than consuming
transcript/chrome layout. `/settings` uses the shared centered compositor; its
selected-row window, save status, and errors update inside fixed geometry, and
nested model selection or catalog failure returns to the settings card.
`selection_card.go` supplies reusable selection-following lists, wrapped detail
rows, pinned controls, compact layouts, and cell-safe name-field tails for
`/mcp`, `/skills`, `/sessions`, `/tree`, `/fork`, `/permissions`, and plan/goal
confirmations. `native_selection_view.go` maps each flow's existing state to that
component. These dialogs overlay the fitted frame in both transcript modes,
without entering composer-overlay row budgets. Empty inventories and loading,
rename, deletion-confirmation, and progress states stay inside the panel.
Permission requests use a separate safety-aware card with the same compositor;
complete wrapped safety messages, choices, and required concrete review rows
share the approval gate's row budget. Insufficient space disables approval while
Escape still denies. Shared `renderPickerCard` bounds content before applying
borders, including borderless tiny-size fallback. Reducer keyboard/mouse guards
prevent background navigation while a modal owns input. Command, skill, and file
completions remain composer-attached, not centered modals.
`fleet_panel.go` shares bounded list/detail geometry and rendering for `/agent`
and `/processes`: 120×28 maximum outer cells, side-by-side panes when wide,
stacked panes when narrow, and row-budgeted controls. Both inspectors use the
same centered overlay compositor over the existing transcript/composer frame,
not an early full-frame renderer. Host permissions, user questions, and plugin
screens retain precedence; closing a fleet restores its unchanged background.
`/plugins` uses a searchable registration picker with arrow-key selection and
per-plugin detail/action pages; Escape restores the filtered list and contributed
views return to their owning plugin page. `/help`
uses the compositor for a scrollable complete command and active-keybinding
reference instead of appending that reference to the transcript. The complete
TUI authentication flow also reuses that fixed-frame card:
provider/logout selection,
serialized logout progress, compatible profile and endpoint fields, masked key
capture, ChatGPT account/method selection and OAuth progress, and compatible
model discovery. ChatGPT OAuth workers are owned by the model lifetime: shutdown
cancels and joins them, while an in-session Escape still receives its terminal
cancellation event. Nested auth cards retain a bounded non-secret navigation stack:
Esc restores the prior field or selection card, while the root Esc cancels and
masked key drafts are discarded rather than retained. Required device codes and
validation errors win constrained card rows, and endpoint paths are not echoed
into post-submit progress or the completion transcript. Single-line auth fields
strip terminal/layout controls, and delayed clipboard results are scoped to
their originating field generation.
Slash-command/login transitions invalidate outstanding composer text and image
paste results before the shared editor is reused. Blocking host requests preempt
all centered cards and transcript context menus without discarding suspended
modal state. `Ctrl+V` attaches supported
clipboard images in the agent composer, then reads text with bounded host
utilities. SSH and host text failures use OSC 52 with target/generation guards,
a three-second timeout, and a retained canceled-request slot until its late reply
is drained. Occupied clipboard retries are rejected before editor selection or
attachment mutation. Collapsed paste uses ordinary textarea replacement semantics;
recalled collapsed entries remain navigable, and programmatic composer replacement
explicitly reconciles cursor scrolling without redundant per-frame setters.
Clipboard output uses Bubble Tea commands rather than frame content.

### Slash commands

`/allow [always]`, `/default`, `/deny`, `/fork`, `/help`, `/init`, `/login`,
`/logout [provider]`, `/model`, `/plan [message]`, `/thinking`, `/new`,
`/permissions`, `/resume`, `/agent [path]`, `/agent concurrency N`,
`/processes [id|name]`, `/sessions`, `/settings`, `/compact`, `/mcp`, `/skills`,
`/tree`, `/quit`, and `/trust [allow|deny]`.

`/init` is a model-driven Default-mode turn that asks the existing permissioned
tool loop to initialize two independent targets in the current working
directory. It inspects the checkout before creating a repository-specific
`AGENTS.md`, and creates a missing `.snow/config.json` as the minimal `{}`
project configuration without copying global settings or credentials. Its
embedded task prompt forbids overwriting either target; the command does not add
a second tool loop or bypass file roots, permission checks, or symlink
protections.

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
  controls, session and branch management, manual compaction, count-only context reports,
  active-branch messages and usage, MCP/skill discovery and branch-active skill
  clearing, pending-input
  inspection/clearing, configuration diagnostics, goals, subagents,
  model-requested input, and a trusted-host interactive permission broker
  (`permission_reply`/`permission_reject`, gated by `permission_interaction`).
- Events mirror SDK events; RPC-only control frames are not persisted events.
- Framing splits on `\n` only (not Unicode line separators).
- Schemas are network-free Draft 2020-12 contracts under
  `pkg/protocol/schema/rpc/v1`.
- Provider/model/thinking controls persist the canonical project-scoped
  selection, while reasoning summary, text verbosity, and debug controls use
  the locked atomic global configuration path shared with the TUI. RPC settings
  DTOs remain secret-free and cannot mutate credentials or transport policy.

RPC prompts run asynchronously so the command reader remains available while
the agent waits. Admission returns an immediate response; exactly one later
`prompt_completed` frame reports `completed`, `failed`, or `canceled` after
all prompt events, and legacy same-ID prompt failure responses are retained.
`user_input_reply.params` is a `UserInputResponse`;
`user_input_reject.params` contains `request_id`. EOF stops command admission,
cancels the serve context and active prompt/wait workers, closes the user-input
and permission brokers, and joins all RPC workers before returning. Dispatcher ownership is split by cohesive
command domain (for example `subagent_commands.go`), and an AST parity test
requires every `pkg/protocol` command-inventory entry to have a dispatcher case.

Consumers include non-Go hosts and IDE bridges. They invoke an
installed/explicit Snow binary. Go hosts should prefer `pkg/snowsdk`. See
`docs/rpc.md`.

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
Go plugin tool risk defaults to `exec`; less restrictive declarations
are trusted metadata and do not constrain the plugin code.

File tools resolve symlinks and enforce allowed roots; do not weaken this
guard. Auth writes are atomic and `0600`; never log secrets or include them in
errors. Tool output and command duration are bounded, and `context.Context`
passes through network, process, file, and tool operations.

SDK and headless code should use deny mode unless the caller deliberately opts
into `allow`/`AutoApprove` in a trusted environment. Repository text,
`AGENTS.md`, tool output, and plugin output are potentially prompt-injected
and must not override the user's request or this guide. See `docs/security.md`.

## Testing and verification

The normal suite is network-free. Provider integration tests use local
SSE/mocked servers; real provider checks require credentials and are manual.
Run these commands from the repository root:

```sh
gofmt -w <changed-go-files>
go test ./...
go vet ./...
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
python3 scripts/check_benchmarks.py
go test -race ./internal/...
go test -race ./internal/subagent ./internal/agent ./internal/app ./internal/session ./internal/rpc ./pkg/snowsdk
go test ./internal/agent ./cmd/snow -count=1
(cd examples/sdk && go test ./... && go run .)
go build -o ./snow ./cmd/snow
govulncheck ./...
```

Web transport and presentation checks use repository runners with no npm
dependencies or external services. The stream-client suite requires Node 22+
only; browser fixtures additionally require an installed Chrome/Chromium:

```sh
node --check internal/web/static/app.js
node --check internal/web/static/visibility.js
node --check internal/web/static/markdown.js
node --check internal/web/static/stream.js
node --check internal/web/static/attention.js
node --check internal/web/static/scroll.js
node scripts/tests/browser/stream-client/run.mjs
node scripts/tests/browser/live-stream/run.mjs
node scripts/tests/browser/permission-workflow/run.mjs
node scripts/tests/browser/inspection-race/run.mjs
node scripts/tests/browser/conversation-workflow/run.mjs
node scripts/tests/browser/queue-next/run.mjs
node scripts/tests/browser/harness-layout/run.mjs
```

Set `SNOW_CHROME_BIN` when Chrome is not at one of the runner's fixed system
locations. The fixture runs headlessly with an ephemeral profile and fails closed
on missing results or failed assertions; these are separate from the Go/Python
suite. `stream-client` executes the production parser/lifecycle in a deterministic
Node VM, including retired-reader races; it is not a browser or RPC test.
`live-stream` uses a gated fake provider through the real app/agent/session,
external RPC worker, RuntimeManager, HTTP/SSE and Chrome, without replacing fetch
or manufacturing snapshots. It checks incremental prefixes before completion,
Stop, stalled-stream recovery without POST replay, a real question/tool result,
and exact persisted history. It does not claim a real permission-approval turn.
`permission-workflow` separately drives actual builtin writes through the real
permission broker, RPC, and browser in two temporary projects. It checks Allow
once/Deny, colliding worker-local approval IDs, stale instance/duplicate replies,
foreground transport loss while approval is pending, independent-project
execution, and worker death before/after a committed write. Durable counters,
file bytes, saved catalog output and explicit resume establish no replay in these
scenarios. Newly synthesized interrupted-tool records remain publicly unresolved
rather than claiming an execution failure. This is local fake-provider evidence,
not remote-provider, arbitrary mutation-tool, or detached-side-effect coverage.
`harness-layout` exports actual Go templates/assets; the latest recorded local
run passed all 1,932 viewport/state reports across seven widths, two themes and
normal/short heights. Its strict mocked public DTOs cover attention paging/approval,
reader anchoring, menus and inspector geometry; screenshots are evidence, not
pixel-hash assertions. The fixture now admits the exact existing read-only startup
`GET /access/browsers`, without allowing other unexpected reads or mutations;
17 fixture tests passed. This corrects a missing mock, not production behavior.
A reduced smoke/width run or partial report is not a substitute for this matrix.

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
- Installer tests simulate every supported OS/architecture mapping and verify
  latest-release resolution, checksums, successful installation, unsupported
  platform rejection, and preservation of an existing binary on failure.
- Documentation-site tests verify allowlisted staging, generated Jekyll front
  matter, relative-link targets, immutable action pins, and Pages deployment
  structure without requiring provider or deployment credentials.
- Benchmarks cover TUI startup, stream lag, bounded branch hydration,
  provider request/stream processing, event delivery, and large-session reload.
  Reviewed allocation ceilings are enforced by `scripts/check_benchmarks.py`;
  generous timing ceilings catch catastrophic regressions without treating
  hosted-runner latency as a tight product contract.

### CI

`.github/workflows/ci.yml` runs for `main` pushes, pull requests, and manual
dispatches. Linux and macOS both run `go test ./...` and the standalone SDK
tests; macOS additionally retains a focused native installer suite. Linux owns
formatting, vet, the full support-script suite, the production build and
credential-free lifecycle smoke, SDK example execution, the deterministic
performance-regression guard, `go test -race ./internal/... ./pkg/snowsdk`,
cgo-disabled builds for all four release targets, and the pinned `govulncheck`
reachable-code scan. Real-provider checks remain manual.
`.github/workflows/release-alpha.yml` accepts only
`vMAJOR.MINOR.PATCH-alpha.N` tags, peels each annotated tag to its commit,
requires that commit to be on `main`, and fails closed unless GitHub records
exact completed successful `ci.yml` and `pages.yml` `push` runs for that SHA and
branch. It does not rerun CI or documentation validation after tagging. It
publishes macOS/Linux amd64/arm64 archives and
`SHA256SUMS`, smoke-tests the Linux amd64 release binary, and marks the GitHub
release as a prerelease. `scripts/install.sh` resolves the newest published release
(including prereleases), verifies the selected archive, validates its embedded
version, atomically installs the matching binary without requiring Go, and
idempotently persists its directory in the detected shell profile unless opted
out.
`.github/workflows/pages.yml` is the canonical rendered-documentation gate. It
stages a curated end-user guide set through the explicit bounded allowlist in
`scripts/build-pages.sh` and builds it with pinned official GitHub Pages/Jekyll
actions. Relevant pull requests build and validate without uploading or
deploying an artifact; relevant `main` pushes build, validate, and serially
deploy the static site to the `github-pages` environment.
`docs/getting-started.md` owns installation and the
first-run sequence, while `docs/providers.md` gives each supported provider an
equal setup path. Pages publishes concise task guides; exhaustive RPC, plugin,
ChatGPT authentication, SDK, and implementation references remain available in
the repository through `docs/README.md`.

## Roadmap

Phases 0 through 4 are implemented in-tree. The list below records what each
phase delivered; the current work queue is "Known gaps / next work" under the
next heading.

| Phase | Delivered |
|---|---|
| 0 — Spec and skeleton | `go.mod`, `cmd/snow` stub, `pkg/protocol` types, interface files compiling with the `fake` provider, in-memory session store, README |
| 1 — Vertical slice | Agent loop with serial tool dispatch, SQLite session persistence, `read`/`bash`, OpenCode Go streaming chat and startup model discovery, API-key auth, print mode, basic TUI, system prompt and `AGENTS.md` load, cancellation |
| 2 — OAuth, mutations, permissions | `write`/`edit` with path gates, ask/allow/deny permissions, login/logout, ChatGPT browser/device OAuth with guarded refresh, sessions/resume/new, durable branches and `/tree`, manual `/compact`, project trust, interactive asker |
| 3 — SDK, search, RPC | Public `pkg/snowsdk`, `grep`/`glob`, JSON mode, RPC protocol v1, extensibility core and JSON-RPC v2 stdio host, bounded steer/follow-up queue |
| 4 — Extensibility and UX | Agent Skills, MCP client, themes and keybindings, persistent ChatGPT catalog cache, fork/tree navigation, macOS/Linux platform guard, plugin permission gate, opt-in BM25 tool routing |

### Known gaps / next work

#### Optional web manager: bounded local management and explicit execution

For this accumulated source checkpoint, start with the canonical
[remaining-work checklist](docs/web-manager-implementation-plan.md#commit-checkpoint-and-remaining-work).
It separates completed local controls from private remote deployment, outstanding
frontend acceptance work, known defects, release gates and running-manager
adoption. Earlier verification totals below retain their milestone scope.

[Web manager research and implementation plan](docs/web-manager-implementation-plan.md)
records the single-user, single-host browser design, its historical transport milestones, and phased verification gates.
`snow --mode web` now includes an authenticated host-side directory browser,
persistent project registration, saved text history, and explicitly activated
RPC-backed live conversations. The responsive workspace uses project/session
navigation, a conversation center, and a mounted composer. Browser credentials
and reusable pairing codes persist privately for up to 30 days across restarts.
The private SQLite registry retains up to 100 canonical roots with identity checks
and an exclusive manager lifetime lock. Removal retains project/session files.

`pkg/agentclient/rpc` supplies bounded framing/handshake/correlation over an owned
connection; `pkg/agentclient/process` adds deadline-capable stdio and direct-child
cleanup. Catalog reads use at most two nonqueued short-lived workers. Dedicated
`--mode rpc --rpc-startup catalog` dispatches before runtime/configuration and
uses a read-only session facade; ordinary eager RPC behavior remains unchanged.
Catalog browsing does not activate a session or initialize providers/plugins. It
omits active/unleased/recovery-dependent/unsupported databases and projects
bounded current-branch text, including pre-compaction history.

Explicit activation owns at most two live project workers. Fixed CLI policy uses
an Ask new-session baseline and the fixed `managed-explicit-goals` profile.
Plugins/MCP/skills/subagents/debug remain disabled; managed-process tools are
enabled. Ordinary prompts neither expose nor dispatch goal tools; only explicitly
admitted native goal work can get/update the owning goal. New or resumed durable sessions support serial text
prompts, definitive completion, Stop, allow-once/deny decisions and typed question
replies. Worker-instance identities bind controls and read-only subscriptions.
Saved goals are deferred rather than auto-resumed. Browser disconnects do not
cancel work; manager shutdown closes direct workers. The web package imports no
app/agent/session internals and implements no second turn loop.

Explicit **Queue next** uses typed `queue_next` RPC controls and the existing
agent follow-up loop, not a manager scheduler. The pending-work panel supports
revision-bound editing/removal before delivery starts, distinct from the draft.
Eight pending/review items share a 256 KiB budget (64 KiB each). Atomic input-span
and user-entry persistence preserves exact historical Edit/Regenerate ownership
within one admitted root and one correlated completion. Durable delivery creates
chat rows; acknowledgments and source events have separate ordering fences.
Stop/failure retains unsent work for explicit review, and unknown outcomes never
trigger retries. Retained work blocks fresh prompts and session/branch transitions
before authority changes; closing remains explicit live-only discard. Healthy
starting delivery is locked, distinct from uncertain delivery. Current tool
permission gates apply independently to every queued continuation.

The current local-management source increment adds the following bounded
contracts. These increments have production HTTP/RPC/browser coverage and pass
the integrated Go, race, vet, Python and benchmark gates. This verifies the bounded
local scope below, not a release or completion of the entire target roadmap:

- **Activity:** read-only counts/navigation across at most 100 registered projects,
  sampled from registry/recovery metadata and live memory, without catalog reads,
  activation, transcript/goal content or command authority. Running and attention
  counts overlap; stale saved-session navigation cannot select another live chat.
- **Organization:** manager-only project labels/pins/archive/restore and session
  pin/archive flags. Restore retains registration identity and requires canonical
  path plus device/inode, no active duplicate, and the 100-project cap. Session
  flags require fresh catalog membership and are capped at 1,000/project and
  10,000 total; search/archived filtering is loaded-page-only. Live projects cannot
  be archived or have saved sessions organized. No project/session files are deleted.
- **Versions:** live-worker branch pages (100) and public history pages (64),
  read-only until explicit Restore. Two-minute single-use preparation binds exact
  session/source/target branch/tip; commit uses idle source/target CAS, rejects goal
  conflicts/retained queues, preserves model/permission authority and applies the
  target branch's authoritative saved mode. No provider replay or filesystem undo.
- **Goals:** explicit Start/Resume binds source worker/session/branch/tip/goal,
  plus a web-only expected snapshot revision CAS. One correlated native handle
  owns many serial turns/retries/compaction and whole-run Stop. Semantic goal
  status is distinct from run completion; an optional budget is not a billing cap.
  No unfinished-goal replacement, ordinary-prompt goal dispatch, Plan admission,
  queue splicing or automatic restored-goal continuation. Retained queue/review
  work blocks admission; Queue next is disabled during goal runs.
- **Processes:** the fixed bundle permits ordinary permissioned process tools;
  typed operator controls provide list (128), bounded logs (32 KiB/page) and Stop,
  never arbitrary launch. Exact session and opaque process handles are mandatory.
  Stop requires Default mode, actual configured hard InvocationPolicy and current
  noninteractive permission authorization. Undecided Ask fails closed without a
  second broker. Switching/shutdown stops managed processes, not guaranteed
  arbitrary detached effects.

The existing historical Edit & resend, Regenerate and explicit Queue next remain
on the shared agent admission path. See the canonical user/security guides for
limits. Verification includes 388 production Queue/Activity/organization/Versions
browser assertions and 308 production Goal/Process assertions, plus 1,932 shell
layout reports. Goal/Process fixtures bootstrap the fictional model through the
real HTTP API; their native activation UI is not claimed. Local installation uses
`./scripts/install-local.sh`; it does not hot-update a running manager or workers,
which must be restarted before exercising the new build.

The optional `RuntimeSubscriber`/`RuntimeSubscription` seam atomically registers
an observer and snapshots under the runtime lock. One-slot wakeups coalesce
revisions; no per-token queue, replay ring or manager event stream is introduced.
`GET /projects/{p}/runtime/events?instance_id=...` emits LF-delimited SSE with
full display-safe public snapshots, initially and on newer revisions at 75 ms.
The handler admits 16 streams globally/four per browser; runtimes admit 32
subscribers. Encoded snapshot JSON is capped at 4 MiB, heartbeat is 10 seconds,
authority is rechecked periodically every five seconds (not immediate revocation),
frame writes have five-second deadlines, and connection lifetime is ten minutes.
Rendering/writes stay outside the RPC drain and runtime lock. This is a private
web transport, not an RPC/provider protocol change.

The browser uses native fetch SSE. Legacy backends
advertised without subscription support (or returning 501) retain two-second
snapshot polling; other failures reconnect GET with backoff, never replay POST.
Hidden tabs pause, terminal close/auth states require review/sign-in, and controls
remain disabled after mutations until a fresh bound snapshot reconciles state.

The activated worker now supplies explicit bounded `models_discover` catalogs,
usage/context counts with recorded-cost estimates, and current-CWD session choices. The additive
`session_set_model` command selects a cached provider/model pair without rewriting
operator-owned host/project settings; legacy `set_model` remains unchanged.
Conversation creation/opening reuses the worker, requires explicit confirmation
before stopping active work, waits for definitive completion, rotates control
identities and fences retired-session events. Nonqueued controls revalidate the
identity after lock acquisition. Unknown transitions fail closed rather than
retargeting old authority. Rename and authoritative `set_mode` controls run while
idle. Browser drafts and uncertain-outcome guards survive workspace navigation
in tab memory, never browser disk; reconnects do not replay mutations or adopt
replacement instances without review.

Public assistant and proposed-plan text uses bounded, cached goldmark rendering with an independent
bluemonday allowlist; existing dependencies are reused, with no remote assets.
Live activity uses the additive `AgentEvent.ToolResult` public-text preview rather
than private/UI `ToolOutput`, bounds output and correlation storage, preserves
expansion during snapshot reconciliation, and settles canceled operations. Saved tool history and
images remain excluded.

Presentation modules keep the normal composer mounted while question/approval
attention takes over its non-scrolling seat. Questions page through the complete
validated batch with exact answers and tab-memory drafts; Stop cancels the whole
turn. Truncated approvals cannot be allowed and host authority stays explicit.
Reader-owned scrolling anchors identified message/activity rows across updates,
head trimming and attention resizing; explicit Jump/accepted send or deliberate
scrolling into the tail resumes following. Seat geometry is measured, including
visual-viewport changes. Viewport-clamped menus separate scrolling choices from
a fixed footer; responsive inspector panes remain public, bounded read-only text
views rather than editors or fabricated backend controls.

The Files / Changes inspector reads registered host projects without activating
workers. Pinned no-follow file reads and credential-name exclusions bound previews.
Git uses a fixed executable, isolated temporary control metadata/index and a clean
environment; no project/global configuration, filters or original-index writes.
Unsupported layouts explicitly report unavailable. This is not a process sandbox:
Git still reads the live worktree/object store, with pre/post metadata checks.

The expanded local source increment adds the following without changing that
single-loop ownership. Recorded local Go, browser and supporting gates verify
these paths; their exact scope is listed below. Local verification is not reusable
CI/release approval, remote/live-provider coverage or an installed/running-binary
update.

- Browser inventory uses independent public IDs and permits targeted durable
  revocation. Open SSE authority is still periodic at five seconds, not immediate;
  revocation stops browser authority, not agent execution.
- Ordinary `snow --mode web` selects the first active private IPv4 address
  (otherwise an IPv6 ULA), binds it and `127.0.0.1` on port 7331, and serves the
  shared manager directly on both exact same-port origins. Each listener retains
  its own Host/Origin boundary and separate local/LAN host-only cookie names with
  profile-bound persisted session authority; legacy unscoped sessions are
  revoked during schema migration. An offline host serves numeric loopback
  directly. Pairing, exact Host/Origin,
  CSRF, revocation, strict host-only cookies, the persisted 20/minute global
  limiter, and bounded per-peer 10/minute throttling remain enforced. Transport
  and cookies are intentionally unencrypted/non-Secure and therefore limited to
  a trusted LAN. Public, multicast, unspecified, wildcard, DNS-named,
  zone-qualified, malformed, and noncanonical listeners fail closed. The Web
  Manager has no TLS, certificate, generated-CA, saved-profile, DNS-origin,
  trusted-proxy, forwarding-header, or hidden network-override path. Snow does
  not change firewall/router policy, and forwarded headers never select origin,
  identity, permission, or rate-limit authority.
- `--rpc-startup control` dispatches before app construction. `WorkerControl`
  uses at most two short-lived workers and a nonqueued write gate for allowlisted
  global/project defaults and local-only provider status. Project selections live
  in operator global config; locked revision-CAS updates affect future workers,
  not active workers or conversations newly created inside them. The browser
  does not expose API-key entry or provider OAuth; those remain explicit
  host-terminal or control-RPC operations.
- All four worker families capture absolute operator `SNOW_HOME` and independent
  session roots through inert `freezeWorkerEnvironment` before project/job CWD
  changes. Resolution errors disable startup; no config/auth reads or inherited
  relative fallback occur. CLI manager storage is absolute, and CONTROL/project
  backends receive the registry’s canonical directory.
- Current-session reasoning uses typed local capability inspection and full
  authority CAS, not legacy persistent settings setters or discovery. Its
  overrides stay in runtime memory, with independent Default/Plan thinking.
  Ordinary branch fork activates the new branch; detached-session fork does not
  open its result. Fork/rename use source/target tip/name CAS in store transactions,
  preserve append-only history, and never replay tools or create worktrees.
- Manual compaction reserves a captured native run for provider work; progress
  does not release ownership before terminal completion. Whole-run Stop and Plan
  Mode are supported; exact history and current collaboration mode are preserved.
  Native Steer targets ordinary work, distinct from Queue next; accepted is not
  delivered and a late/failed POST can already have delivered. Shared pending/
  review limits are eight inputs, 64 KiB each and 256 KiB total. Uncertain steering
  retains exact-captured-root Stop, never replacement-root cancellation. Idle
  Keep draft and dismiss preserves uncertainty/text and releases the panel;
  separate shared Reviewed acknowledgement releases Send/Close without replay.
  Ordinary completion performs authoritative `goal_inspect` before releasing
  idle readiness, refreshing the durable tip under captured ownership/epoch
  fences so immediate manual compaction does not require a corrective inspection.
- Recorded cost is a currency-labeled estimate, not billing. Mixed/invalid
  currencies remain unknown; known priced subtotals do not establish completeness
  when other usage is unpriced.
- `ProjectOperations` durably admits one CREATE/anonymous-HTTPS-clone job, with
  128 retained rows and 32-row pages. The OS-user folder browser issues a
  five-minute browser/manager-bound parent-identity grant, not startup-root
  confinement. The worker returns pinned child identity after mkdir; the manager
  durably acknowledges it before clone starts. CREATE does not require Git;
  clone admission checks the fixed absolute executable before handle allocation,
  with no PATH search/fallback. Fixed Git/descriptor helper,
  process-group and liveness supervision use a ten-minute operation timeout,
  64 KiB output budget and two-second TERM grace, not disk/network-byte quotas.
  Reconcile observes only, Cancel awaits cleanup evidence, and Dismiss removes
  settled metadata, never host files. No automatic restart/retry or activation.
  Successful completion stops at `awaiting_registration`; only a separate
  explicitly reviewed Register request inserts the project, with operation
  revision/identity checks. Success, Get/List, reconciliation and restart never
  register automatically, and registration never activates or opens a session.

The native browser-access, runtime-controls and host-controls matrices passed
all 12 reports (616 assertions) across 320/1280 px and dark/light. Runtime coverage
includes lost steering receipts and immediate post-fork/prompt compaction. The
host desktop failure was corrected in the fixture’s native keyboard driver, not
product behavior. Fresh native workflow/execution baselines passed 388/380
assertions across four reports each; the full layout passed 1,932 reports, and
conversation workflow passed 1,288 assertions across 14 width/theme reports.
Latest full Go tests/vet and the full internal race suite passed after the
completion-scope fix. The complete CLI/public-package race suites also passed
in a separate final command; this is not one combined all-package race invocation.
Supporting checks passed 67 Python tests, the benchmark guard and 112 Node tests. The
[expanded-controls acceptance evidence](docs/web-manager-implementation-plan.md#expanded-controls-acceptance-evidence)
separates authored coverage from executed passes. These private, fictional-provider
fixtures do not exercise live providers or user data, establish reusable CI/release
approval, or update installed binaries. Using the verified checkout requires a
local build/install and manager/worker restart, separate from source verification.

Broader real-device private-network acceptance, automatic worker recovery and
saved media rendering remain planned. The Web Manager now has one bounded
network contract: automatic trusted-LAN HTTP plus a same-port direct localhost
origin, with loopback-only fallback when offline. TLS, certificates/generated CAs, saved
network profiles, DNS origins, trusted proxies/Tailscale forwarding, hidden
network overrides, and browser API-key entry were removed; this is not a
public-Internet deployment contract. Worktree forks, browser OAuth, extension
enablement, general Git writes, editors, PTYs and preview fleets remain outside
this manager increment. Durable public tool history is available, not a private-tool replay. See
[Using Snow](docs/using-snow.md#try-the-local-web-manager-shell) for exact current
behavior, limits and OS-privilege warnings.

#### Effect-aware Bash permission preflight

Snow analyzes both built-in `bash` and `process_start` before the ordinary
permission broker. The shared preflight uses `mvdan.cc/sh/v3/syntax` for POSIX
shell grammar and produces the same tool-independent effects for every surface:

```text
Shell source + actual launch directory/environment
    → bounded POSIX shell AST
    → command specification + abstract shell state + resource policy
    → effects, capabilities, concrete resources, unknowns
    → hard policy
    → invocation-scoped decision or ask/allow/deny
    → existing host sh -c execution
```

`internal/shellanalysis/commands.json` defines commands, subcommands, option
aliases, value arity, operand roles, and semantic handlers. It is embedded,
validated, and compiled to lookup maps once. One option parser handles short
clusters, attached values, long equals values, and `--`; unknown options never
inherit a generic safe classification. Specialized bounded handlers implement
copy destinations, shell state, nested execution, traversal, and network
resources. No external help command or project-provided specification is
executed or trusted to grant authority.

The state engine intersects known values across possible conditional outcomes,
isolates child/pipeline state, invalidates unmodeled loop/builtin state, and
preserves quote context. Runtime globs, field splitting, unsupported escapes,
and unresolved words remain unknown; unresolved prefixes are not presented as
concrete resources. Read/write redirection emits both capabilities. The node
budget is shared across nested parses, and wrapper recursion, values, effects,
and per-invocation filesystem observation caches have independent caps.

Protected defaults live in `protected_paths.json`, separate from shell grammar
and command handlers. Global `shell_protected_paths` adds protected paths/trees
without weakening defaults; project overlays cannot modify it. Path rules and
symlinks are prepared once per invocation, with bounded ancestor reuse and no
cross-invocation filesystem cache.

Approval scopes include exact command source, launch CWD, a digest of the actual
launch environment, analyzer and embedded-specification versions, effective
resource policy, and inferred effects. No environment values enter public
permission data. The v2 scope invalidates earlier analyzer approvals; legacy
broad allows do not authorize either analyzed shell tool. Current unknown or
non-rememberable analysis ignores cached allows. Git, network clients, nested
shells, recursive traversal, and other runtime-dependent programs remain
one-time approvals even when visible effects are recognized. Unsupported
structural syntax, unparsed embedded configuration, and exhausted analysis
budgets fail closed before all permission modes.

`process_start` analyzes the manager's actual immutable CWD and inherited
process environment, independently of the model-facing ToolHost; network
readiness adds uncertainty and cannot receive reusable approval. Its TUI card
uses the same host-execution warning, command display, and small-frame approval
requirements as Bash. The agent's existing serial permission/dispatch path is
shared without duplicating execution logic.

This is static launch control, not containment. Processes still run with the
operator's OS privileges; hidden executable behavior, runtime configuration,
and filesystem changes after preflight require external containment. Remaining
work includes richer bounded value sets, more command/vendor-specific option
coverage, and runtime effect enforcement as a separate containment project.

Regression coverage compares harmless temporary shell execution with inferred
resources; checks Git mutation scopes, option roles, quoted tilde, globs,
redirection creation, state joins, policy configuration and trusted-project
isolation, shared managed-process dispatch gates, cancellation, bounds, and
fuzz invariants. `BenchmarkAnalyzeShell` measures common preflight paths.

#### Semantic/vector tool routing

Namespace-first tool routing currently uses local BM25. Optional semantic or
vector routing remains deferred pending a locally downloadable open-source model
with acceptable licensing, cross-platform behavior, and startup cost.

## Research and decisions

This section condenses the recorded research and locked decisions. Rationale
that is fully covered elsewhere is referenced rather than repeated.

### Locked decisions

| Decision | Choice |
|---|---|
| Product role | Standalone harness, not an IDE backend |
| Binary name and module | `snow`, `github.com/elmissouri16/snow-core` |
| Modularity | In-process Go interfaces and Goja JavaScript; no subprocess plugins or Go `.so` loading |
| Auth | OpenCode Go API key, optional-key/anonymous OpenCode Zen, user-configured OpenAI-compatible endpoints, and ChatGPT/Codex OAuth |
| Sessions | Snow-owned pure-Go SQLite tree (schema version 12) |
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
  HTTP streams, and optional Go plugin registration.

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
| Arbitrary Bash effects are opaque at approval time | Overbroad approval or host damage | Current boundary is documented and requires external containment; the shared shell preflight above bounds visible effects but does not contain execution |
| Plugin/MCP/subagent OS authority | Runs as the user | Documented boundary; external container/VM required for hostile code |
| Pre-v1 API and file-format drift | Breaking changes | Stabilize `pkg/snowsdk`, `pkg/protocol`, and the session schema before v1 |
| Semantic/vector tool routing | Deferred | Await a locally downloadable open-source model with acceptable licensing and startup cost |

## Related documents

- [Using Snow](docs/using-snow.md) — TUI/CLI modes, flags, keys, commands, queues, and workflows
- [Security model](docs/security.md) — consolidated privilege and threat boundaries
- [Security reporting](SECURITY.md) — private vulnerability disclosure policy
- [Release policy](docs/releases.md) — alpha versioning, verification, artifacts, and rollback
- [SDK](docs/sdk.md) — concise public Go SDK quickstart
- [SDK reference](docs/sdk-reference.md) — complete lifecycle and API reference
- [Sessions](docs/sessions.md) — user workflows for resume, branches, and forks
- [Session storage internals](docs/session-storage-internals.md) — pure-Go
  SQLite storage, schema, migrations, and projections
- [Performance](docs/performance.md) — allocation ceilings and CI regression guard
- [RPC](docs/rpc.md) — versioned JSONL framing, schemas, commands, and events
