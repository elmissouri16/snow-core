# Go SDK reference

This repository-only reference documents the complete public `pkg/snowsdk`
surface. Start with the concise [Go SDK guide](sdk.md) for installation and a
minimal streaming session.

`pkg/snowsdk` embeds Snow's production agent runtime in a Go process. It uses
the same providers, tools, permissions, SQLite sessions, events, goals, MCP,
skills, plugins, and subagent manager as the CLI, and it does not import Cobra
or Bubble Tea.

Public integration data lives in `pkg/protocol`, a standard-library-only
package shared by the SDK, JSON, RPC, plugins, sessions, and the TUI.

> **Note:** Snow is alpha software. `pkg/snowsdk` and `pkg/protocol` are the
> intended public surfaces, but compatibility is not guaranteed until v1.

## On this page

- [Install](#install)
- [Minimal streaming session](#minimal-streaming-session)
- [Options and lifecycle](#options-and-lifecycle)
- [Events](#events)
- [Sessions](#sessions)
- [Permissions and security](#permissions-and-security)
- [Readiness and capabilities](#readiness-and-capabilities)
- [Concurrency and errors](#concurrency-and-errors)
- [Limitations](#limitations)
- [Related documents](#related-documents)

## Install

```sh
go get github.com/elmissouri16/snow-core/pkg/snowsdk
```

A separate checked-in module under [`examples/sdk`](../examples/sdk) exercises
only the public packages and is run by Linux and macOS CI. SDK-created runtimes
advertise the linked Snow build version to external plugins and MCP servers.
From this checkout:

```sh
cd examples/sdk
go run .                          # credential-free fake-provider lifecycle
go run . -provider opencode-go    # real streaming provider
```

Import both packages:

```go
import (
    "github.com/elmissouri16/snow-core/pkg/protocol"
    "github.com/elmissouri16/snow-core/pkg/snowsdk"
)
```

## Minimal streaming session

The example below uses `opencode-go`, which requires a credential from
`OPENCODE_API_KEY` or `snow login opencode-go`. Change `Provider` to `fake`
for a credential-free lifecycle smoke test; the built-in fake provider emits
`turn_done` but has no scripted text response.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/elmissouri16/snow-core/pkg/protocol"
    "github.com/elmissouri16/snow-core/pkg/snowsdk"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    session, err := snowsdk.Open(ctx, snowsdk.Options{
        Provider:         "opencode-go",
        NoSession:        true,
        PermissionMode:   "deny",
        NoPlugins:        true,
        NoMCP:            true,
        NoSkills:         true,
        DisableSubagents: true,
    })
    if err != nil {
        panic(err)
    }
    defer func() {
        if err := session.Close(); err != nil {
            panic(err)
        }
    }()

    unsubscribe := session.Subscribe(func(event protocol.AgentEvent) {
        if event.Agent == nil && event.Type == protocol.EvTextDelta {
            fmt.Print(event.Text)
        }
    })
    defer unsubscribe()

    // Install subscriptions and interaction handlers before readiness. Calling
    // both methods is safe for new sessions and is the complete resumed-session
    // pattern.
    if err := session.ReadyGoals(); err != nil {
        panic(err)
    }
    if err := session.ReadySubagents(); err != nil {
        panic(err)
    }

    if err := session.Prompt(ctx, "List the Go packages in this repository."); err != nil {
        panic(err)
    }
}
```

The credential-free variant differs only in the provider selection:

```go
session, err := snowsdk.Open(ctx, snowsdk.Options{
    Provider:         "fake",
    NoSession:        true,
    PermissionMode:   "deny",
    NoPlugins:        true,
    NoMCP:            true,
    NoSkills:         true,
    DisableSubagents: true,
})
```

`Prompt` blocks until the root turn finishes. Text, thinking, tools, usage,
plans, goals, and child activity stream through subscriptions while it runs.

## Options and lifecycle

### Options reference

Empty fields usually inherit the selected global configuration. The table
separates inheritance from clean-install defaults.

| Field | Meaning and effective behavior |
|---|---|
| `CWD` | Active project root. Empty uses the process working directory. |
| `Provider` | A built-in ID or configured named OpenAI-compatible profile. Empty inherits config; clean-install default is `opencode-zen`. |
| `Model` | Model ID. Empty resolves the configured or provider default. |
| `SessionPath` | SQLite `.db` path to open or create; an existing database is resumed. Empty creates a new indexed durable session unless `NoSession` is true. |
| `NoSession` | Use an in-memory conversation. Branches work for the process lifetime, but Thread Goals are unavailable; auth and model caches remain persistent. |
| `AuthPath` | Credential file override. Empty uses `$SNOW_HOME/auth.json`. |
| `ConfigPath` | Global config override. Empty uses `$SNOW_HOME/config.json`. |
| `PermissionMode` | `ask`, `allow`, or `deny`. Omission in the SDK forces `deny` rather than inheriting interactive `ask`. |
| `AutoApprove` | Forces `allow` and takes precedence over `PermissionMode`. Dangerous outside externally isolated or trusted environments. |
| `Tools` | Built-in tool allowlist. Empty exposes all registered built-ins. |
| `SystemPrompt` | Overrides configured system-prompt files and Snow's embedded Markdown preamble. Project context and runtime steering remain separately assembled where applicable. |
| `Thinking` | `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, or `ultra`. Empty inherits config; model metadata may reject a level. |
| `ReasoningSummary` | `off`, `auto`, `concise`, or `detailed`. Empty inherits config. |
| `TextVerbosity` | `low`, `medium`, or `high`. Empty inherits config. |
| `CollaborationMode` | `default` or `plan`. Empty restores branch state or clean-install Default. |
| `PlanModeReasoningEffort` | Optional Plan Mode override using the same eight thinking values; model support remains authoritative. |
| `Retry` | Optional `*snowsdk.RetryOptions` runtime override. Nil inherits global configuration; `Normal` and `Goal` profiles specify attempt, elapsed, initial/max-delay millisecond bounds, and jitter percent. Child agents inherit the effective policy. |
| `APIKey` | Explicit credential with precedence over the auth store and environment. |
| `BaseURL` | Active provider endpoint override; required for an OpenAI-compatible profile unless configured globally. Accepts an API root or a full `/responses` or `/chat/completions` URL. |
| `Plugins` | Explicit external plugin process declarations. Configured plugins may also load. |
| `GoPlugins` | Statically linked `pkg/plugin.Plugin` implementations supplied by the host. |
| `NoPlugins` | Disable configured, explicit, and Go plugins. |
| `MCPServers` | Additional public `pkg/mcp.ServerSpec` declarations. |
| `NoMCP` | Disable configured and explicit MCP servers. |
| `SkillDirs` | Additional trusted Agent Skills discovery roots. |
| `NoSkills` | Disable skill discovery and activation tools. |
| `EnableDebug` | Force shared diagnostic capture on for this SDK runtime. |
| `DisableDebug` | Force shared diagnostic capture off even when persisted config enables it. Setting both debug overrides is an error. |
| `DebugDumpPath` | Enable capture and write a final diagnostic dump during normal `Close`; an empty value disables automatic dumping. Relative paths resolve against `CWD`. |
| `EnableSubagents` | Force subagents on for this session. Does not enable mutation or recursion. |
| `DisableSubagents` | Force subagents off. Setting both enable and disable is an error. |
| `SubagentProvider` / `SubagentModel` | Override the automatic provider and model defaults for child agents. |
| `SubagentMaxConcurrency` | Zero inherits config; clean-install default 4, maximum 256. The root does not consume a slot. |
| `SubagentMaxAgents` | Zero inherits config; clean-install default 32, maximum 4096, not below concurrency. |
| `SubagentMaxDepth` | Zero inherits config; clean-install default 1, maximum 8. |
| `UserInputHandler` | Answers `ask_user`; nil fails the tool call fast instead of blocking. It is not a permission asker. |
| `PermissionHandler` | Resolves trusted-host `ask` permission requests after their event is published. It returns a correlated `protocol.PermissionResponse`; nil preserves fail-closed behavior unless manual replies are explicitly enabled. Bash requests include bounded static effects, capabilities, paths, uncertainty, and whether a scoped decision may be remembered; approved Bash remains an unrestricted host process. |

### OpenAI-compatible gateways

Responses is preferred; HTTP 404, 405, or 501 selects a cached Chat
Completions fallback.

```go
session, err := snowsdk.Open(ctx, snowsdk.Options{
    Provider:       "openai-compatible",
    BaseURL:        "https://gateway.example/v1",
    Model:          "model-id", // optional when GET /models succeeds
    APIKey:         os.Getenv("OPENAI_API_KEY"), // optional for keyless gateways
    PermissionMode: "deny",
})
```

The endpoint is operator-trusted and receives prompts and tool data. Snow
prefers Responses streaming and uses Chat Completions streaming when Responses
is unavailable. SDK callers can select a configured profile with
`Provider: "profile-name"`, or pass `BaseURL`, `APIKey`, and `Model` directly.
Arbitrary per-call custom headers and Azure-specific parameters remain
unsupported. The TUI can persist the endpoint and optional key through
`/login openai-compatible`.

Full persistent configuration and project trust rules are in
[Configuration](configuration.md).

### Lifecycle sequence

Recommended host sequence:

1. Build a context with the cancellation and deadline policy for the embedding.
2. Call `snowsdk.Open`.
3. Install `Subscribe` callbacks and any trusted `UserInputHandler` or
   `PermissionHandler` in `Options`.
4. Read `StateEvent()` if the host needs the initial collaboration-mode state.
5. Call `ReadyGoals()` and `ReadySubagents()` after observers exist.
6. Call `Prompt` or `PromptWithMode`, or use the control methods while a turn
   runs.
7. Call `Close` on every exit path.

`Open(nil, ...)` uses `context.Background()`. Construction deliberately does
not restart restored goal work or publish restored subagent topology before the
host can observe it. CLI surfaces perform readiness automatically; SDK hosts
own it.

`Close` marks the session stopped before releasing runtime resources. Calls
after close, a nil session, and repeated `Close` return `snowsdk.ErrStopped`
where a method can return an error.

For trusted interactive hosts, permission mode `ask` can use the correlated
broker without weakening the headless default:

```go
session, err := snowsdk.Open(ctx, snowsdk.Options{
    PermissionMode: "ask",
    PermissionHandler: func(
        ctx context.Context,
        req protocol.PermissionRequest,
    ) (protocol.PermissionResponse, error) {
        return protocol.PermissionResponse{
            RequestID: req.ID,
            Decision:  protocol.PermissionAllowSession,
        }, nil
    },
})
```

Snow publishes `permission_request` before calling the handler. Handler errors,
invalid decisions, and mismatched response IDs deny the request. Event-loop
hosts can instead call `EnablePermissionReplies`, then `ReplyPermission` or
`RejectPermission` with the exact request ID. With neither path enabled, `ask`
denies immediately rather than blocking.

### Constructors and helpers

| API | Purpose |
|---|---|
| `Open(ctx, options)` | Open or create a session and assemble the runtime |
| `MustOpen(ctx, options)` | Panic-on-error helper for tests and tiny programs |
| `RunPrompt(ctx, options, prompt)` | Open, collect root text, prompt, wait for goal, subagent, and event quiescence, then close |

`RunPrompt` returns only root `text_delta` content. Attributed child text
remains available only through a normal subscribed session. The helper closes
the runtime before returning and joins any plugin or session cleanup failure
into its returned error without discarding accumulated text.

```go
package main

import (
    "context"
    "fmt"

    "github.com/elmissouri16/snow-core/pkg/snowsdk"
)

func main() {
    text, err := snowsdk.RunPrompt(context.Background(), snowsdk.Options{
        CWD:              ".",
        Provider:         "opencode-go",
        NoSession:        true,
        PermissionMode:   "deny",
        NoPlugins:        true,
        NoMCP:            true,
        NoSkills:         true,
        DisableSubagents: true,
    }, "Summarize this directory.")
    if err != nil {
        panic(err)
    }
    fmt.Println(text)
}
```

For credential-free harness tests, select `Provider: "fake"` and disable
unneeded plugins, MCP, skills, and subagents. `RunPrompt` collects root
`text_delta` events until root-agent and event quiescence. With a persisted
session, an eligible automatically continued Thread Goal can contribute more
than one assistant turn to the returned string.


### Turns, modes, and active input

| Method | Purpose |
|---|---|
| `Prompt(ctx, text)` | Run one text root user turn to completion |
| `PromptContent(ctx, text, attachments)` | Run a root turn with text plus normalized image attachments |
| `PromptWithMode(ctx, text, mode)` | Atomically select `default` or `plan` and start a text prompt |
| `PromptContentWithMode(ctx, text, attachments, mode)` | Atomically select mode and start a prompt with image attachments |
| `Mode()` / `SetMode(mode)` | Inspect or change collaboration mode while idle |
| `Steer(ctx, text)` | Queue input for the next safe assistant plus complete tool-batch boundary |
| `FollowUp(ctx, text)` | Queue input after natural completion and earlier steering |
| `PendingInputs()` | Return an independent queue snapshot, including entries retained after an operational failure |
| `ClearPendingInputs()` | Close queue admission and return or remove undelivered entries so a host can restore or resubmit them |
| `Abort(ctx)` | Cancel admitted work, clear undelivered root queue entries, and defer active goal continuation |
| `IsRunning()` | Report whether a root turn is in flight |

`Steer` and `FollowUp` require an active queue-accepting root turn. Idle calls
return `ErrNotRunning`. The queue is bounded and ordered; steering has priority,
with FIFO order inside each input class. Ordinary provider failures consume
accepted input through a fresh request. Internal failures and turn-limit
rejection retain undelivered entries; inspect `PendingInputs`, then call
`ClearPendingInputs` to restore or resubmit them before starting another
prompt.

Run the blocking prompt in a goroutine and use events or host state to decide
when steering is useful:

```go
result := make(chan error, 1)
go func() {
    result <- session.Prompt(ctx, "Review the repository.")
}()

// A real host normally reacts to an event or UI action here.
if session.IsRunning() {
    if err := session.Steer(ctx, "Focus on the public API and RPC contracts."); err != nil &&
        !errors.Is(err, snowsdk.ErrNotRunning) {
        return err
    }
}

if err := <-result; err != nil {
    return err
}
```

`IsRunning` is only a snapshot; the turn may finish before `Steer`. Always
handle `ErrNotRunning` as a normal race.

### Model-requested input

```go
session, err := snowsdk.Open(ctx, snowsdk.Options{
    Provider:       "opencode-go",
    PermissionMode: "deny",
    UserInputHandler: func(
        ctx context.Context,
        req protocol.UserInputRequest,
    ) (protocol.UserInputResponse, error) {
        answers := make([]protocol.UserInputAnswer, 0, len(req.Questions))
        for _, question := range req.Questions {
            answers = append(answers, protocol.UserInputAnswer{
                QuestionID: question.ID,
                Answer:     chooseAnswer(question),
            })
        }
        return protocol.UserInputResponse{
            RequestID: req.ID,
            Answers:   answers,
        }, nil
    },
})
```

The handler must answer every question exactly once. It may omit `RequestID`
and Snow will fill it. The `user_input_request` event is published before the
callback runs. Handler errors fail the tool interaction rather than hanging.
See [Model-requested user input](user-input.md).


### Models and response controls

| Method | Purpose |
|---|---|
| `Model()` / `Models()` | Current model and defensive copy of the active-provider catalog |
| `SetModel(model)` | Select a model for later turns |
| `Thinking()` / `SetThinking(level)` | Inspect or change normalized effort |
| `ReasoningSummary()` / `SetReasoningSummary(value)` | Inspect or change the provider reasoning-summary request |
| `TextVerbosity()` / `SetTextVerbosity(value)` | Inspect or change provider response detail |

Use `model.SupportedThinkingLevels()` and
`model.SupportsThinkingLevel(level)`. Do not infer support from model names or
provider branding.

### Subagents

| Method | Purpose |
|---|---|
| `SubagentModels()` | Return exact provider and model pairs available to children |
| `SpawnSubagent(ctx, request)` | Create a role-scoped child at a canonical path |
| `SendSubagentMessage(ctx, target, message)` | Queue attributed mail without starting a turn |
| `FollowupSubagent(ctx, target, message)` | Queue and run or reuse an idle child |
| `WaitSubagents(ctx, timeout)` | Wait for one activity or lifecycle change |
| `WaitSubagentsUntilAll(ctx, timeout)` | Wait until every root child is terminal or the timeout expires |
| `InterruptSubagent(ctx, target)` | Cancel only the target's current turn |
| `CloseSubagent(ctx, target)` | Release a terminal child's open-agent slot while preserving identity and history |
| `ResumeSubagent(ctx, target)` | Reopen a closed identity without starting a turn |
| `Subagents()` | Return snapshots for the root and its visible descendants |
| `Subagent(target)` | Inspect one child by canonical path or supported identifier |
| `SubagentUsage()` | Aggregate child usage |

`protocol.SpawnSubagentRequest` fields are `Name`, `Task`, `Role`, `ForkTurns`,
`Provider`, `Model`, and `ReasoningEffort`. `snowsdk.Options.SubagentProvider`
and `SubagentModel` set the automatic provider and model defaults for children.
SDK names are strict lowercase segments under `/root` with letters, digits, and
underscores. Unlike the model-facing tool, the SDK does not normalize hyphens.

`Subagents()` includes the root snapshot as well as visible descendants; do not
interpret its length as the open-child count. Closed children remain visible
with status `closed`, retain their stable paths, and do not consume the
open-agent limit. `FollowupSubagent` automatically resumes a closed target when
capacity permits. Terminal child snapshots can expose
bounded `Result`, `Error`, `Usage`, and `Generation` metadata. Wait results
contain aggregate state, not full private child content. Results and mail
arrive live through attributed `AgentEvent` and `AgentMessage` values. The SDK
does not currently expose persisted child message history, so hosts that need
complete transcripts must retain attributed events while observing the run.
See [Subagents](subagents.md).

## Events

`Subscribe(callback)` observes isolated copies of normalized agent events and
returns an unsubscribe function. `StateEvent()` returns the current
collaboration-mode snapshot for initial host state.

### Event reference

`protocol.AgentEvent.Type` selects the relevant optional payload fields.

| Category | Event types |
|---|---|
| Streaming | `text_delta`, `thinking_delta`, `usage`, `provider_retry` |
| Tools | `tool_start`, `tool_progress`, `tool_end`, `tool_routing` |
| Interaction | `user_input_request`, `permission_request`, `queue_updated`; permission events are emitted only when `ask` uses a configured handler or manual replies |
| Lifecycle and state | `session_updated`, `run_stats_updated`, `turn_done`, `error`, `aborted`, `model_changed`, `mode_changed` |
| Plan | `plan_started`, `plan_delta`, `plan_completed`, `plan_update` |
| Compaction | `compaction_started`, `compaction_done`; `Compaction.Automatic` distinguishes non-manual pressure and overflow-repair runs |
| Goals | `thread_goal_updated` |
| Subagents | `subagent_started`, `subagent_status`, `subagent_message`; `subagent_activity` is reserved but not currently emitted |

Common payload fields include:

- `Text`, `Message`, `IsError`
- `ToolCallID`, `ToolName`, bounded `ToolOutput`, `ToolDurationMS`,
  `ToolProgress`, `ToolRouting`
- `Usage`, `ProviderRetry`, `Model`, `Mode`
- `Plan`, `PlanUpdate`, `Compaction`
- `Permission`, `UserInput`, `Queue`, `ThreadGoal`
- `Agent`, `Subagent`, `AgentMessage`
- `TurnID`, `TurnOrigin`, `TurnSequence`, `RootEpoch`, `Snapshot`,
  `GoalContinuing`

### Correlation and payload fields

- `event.Agent == nil` denotes a root-agent event, including ordinary prompts,
  goal continuation, and root state and lifecycle events.
- Attributed child stream, tool, and usage events carry `Agent`.
- Child lifecycle state carries `Subagent`; `Snapshot` is true when restored
  state initializes observers rather than reporting a transition that just
  occurred.
- Mailbox events carry `AgentMessage`.
- `TurnID` is the stable turn identity; `TurnSequence` is a process-local
  monotonic admission order that restarts with the process.
- `RootEpoch` is a process-local session and branch reconciliation generation
  on every root event, including events outside a turn.
- `TurnOrigin` and `GoalContinuing` distinguish ordinary user prompts from
  internal goal-continuation turns.
- `provider_retry` is nonterminal progress, not an `error`. Its payload reports
  provider, `transient`/`rate_limit` kind, `pre_activity`/`recovery` phase, next
  attempt, selected delay, and elapsed limits. Only final exhaustion emits an
  `error` event.
- `ToolOutput` is a bounded UI preview; complete root tool results remain in
  `Messages()`.
- Every subscriber receives a deep clone. Mutating one callback's event cannot
  affect another observer.

Callbacks are dispatched in order. Keep them short: a callback that does not
return within one second is evicted so it cannot strand later event delivery or
shutdown. Go cannot forcibly stop the callback's own code, so hosts must still
offload blocking work and arrange its cancellation. Control calls are designed
not to hold event-dispatch locks, and callback-reentrant drains fail fast.

## Sessions

### Open or resume a session

```go
session, err := snowsdk.Open(ctx, snowsdk.Options{
    SessionPath:    databasePath,
    PermissionMode: "deny",
})
if err != nil {
    return err
}
defer session.Close()

cancel := session.Subscribe(handleEvent)
defer cancel()

handleEvent(session.StateEvent())
if err := session.ReadyGoals(); err != nil {
    return err
}
if err := session.ReadySubagents(); err != nil {
    return err
}
return session.Prompt(ctx, "Continue from the saved state.")
```

`SessionPath` is open-or-create: a missing or mistyped path creates a new
SQLite database. The SDK currently has neither a strict "must already exist"
resume option nor a saved-session catalog, so hosts that require strict resume
must validate and select the path before `Open`. Readiness is especially
important for restored automatic goals and durable child topology; restored
stale child work is observed as interrupted metadata, not silently restarted.

### Session identity and state

| Method | Purpose |
|---|---|
| `Messages()` | Linearized durable messages on the active branch |
| `Usage()` | Aggregate token, cache, request, and cost data for the active branch |
| `SessionID()` | Stable session identifier |
| `SessionName()` | Optional automatic or manually assigned display title |
| `RenameSession(name)` | Change the display title without changing ID, path, or history |
| `SessionPath()` | SQLite path, or empty for in-memory sessions |
| `CWD()` | Active project directory |

The first accepted prompt assigns an untitled built-in store a deterministic,
provider-free title. `RenameSession` trims surrounding whitespace, requires
1–72 runes, and rejects control characters. Titles need not be unique and do
not enter provider context.

`protocol.Message` contains parent-linked IDs, typed content blocks, provider
and model metadata, stop reason, usage, and tool-result correlation. In
`protocol.Usage`, `Input` is the total prompt count including cached tokens;
`CacheReadKnown` distinguishes an explicit `CacheRead == 0` miss from a
provider that omitted cache metrics. On aggregate usage, it remains true only
when every included request reported the cache-read metric. A `provider_data`
block is persistence-only and must not be rendered or logged.

### Branches, forks, and compaction

| Method | Purpose |
|---|---|
| `Branches()` | List named session branches and topology |
| `SelectBranch(id)` | Switch active branch; affects later messages, usage, mode, goal, and prompts |
| `Branch(opts)` | Explicitly create and activate a same-database branch |
| `Fork(entryID)` | Compatibility alias that branches the active session |
| `ForkNamed(sourceBranchID, entryID, name)` | Compatibility alias for an explicitly sourced branch |
| `ForkSession(ctx, opts)` | Create a detached independent SQLite session in the same workspace |
| `ForkWorktree(ctx, opts)` | Create a detached clean Git worktree plus an independent session |
| `RenameBranch(id, name)` | Change display name without changing the stable ID |
| `DeleteBranch(id)` | Delete an eligible inactive leaf branch reference |
| `Compact(ctx)` | Manually summarize older projected context while retaining full history |

Branch forks share existing message rows; they do not copy history. Branches
persist across process restarts only in SQLite-backed sessions; under
`NoSession` their topology and messages are in-memory and ephemeral.
`ForkSession` and `ForkWorktree` instead return a `SessionForkResult` for a
new, reopenable database and deliberately leave the SDK receiver bound to its
source. Open `result.SessionPath` explicitly to continue in the child. This
detached contract prevents an in-place worktree operation from reusing stale
project trust, search, or file-root bindings.

Independent forks preserve the exact stable root-to-entry chain and immutable
parent provenance. They reject unresolved tool-call boundaries, never overwrite
an explicit destination, do not copy subagent topology, and inherit
collaboration mode only at the current branch tip. Callers can classify common
failures with `ErrForkDestinationExists`, `ErrWorktreeDestinationExists`,
`ErrInvalidForkBoundary`, `ErrNotGitRepository`, `ErrGitDirty`, and
`ErrUnsafeWorktreeDestination` through `errors.Is`. Branch and fork management
is rejected while conflicting root or subagent work is active. Automatic
Default-mode goal continuation may emit compaction events between goal turns at
the configured context threshold; this does not change the manual `Compact`
method contract.

Plan Mode and branches:

```go
if err := session.PromptWithMode(ctx, "Design the migration.", protocol.ModePlan); err != nil {
    return err
}

messages, err := session.Messages()
if err != nil || len(messages) == 0 {
    return err
}

fork, err := session.Fork(messages[len(messages)-1].ID)
if err != nil {
    return err
}
fmt.Println("active fork:", fork.ID)
```

Consume `plan_started`, `plan_delta`, and `plan_completed` rather than scraping
transport tags. `plan_update` is the separate Default-mode checklist event.

### Persistent Thread Goals

| Method | Purpose |
|---|---|
| `Goal()` | Return the active branch goal or nil |
| `CreateGoal(objective, budget, replace)` | Create a goal; `SetGoal` is an alias |
| `EditGoal(objective)` | Rotate objective and goal ID while preserving usage and budget |
| `PauseGoal()` / `ResumeGoal()` | Control eligible automatic continuation; resume also restarts an active abort-deferred goal |
| `ClearGoal()` | Remove the branch goal |
| `ContinueGoal()` | Clear continuation deferral and run eligible idle work |

Normal prompts never clear an abort or manual-compaction deferral; call
`ResumeGoal` or `ContinueGoal` explicitly. SQLite persists goals across
processes; Thread Goals require a persisted session and are unavailable with
`NoSession`. Budgets must be positive. Goal statuses are `active`, `paused`, `blocked`, `usage_limited`,
`budget_limited`, and `complete`. Returned `protocol.ThreadGoal` values include
a durable `BlockedReason` while blocked (empty only for pre-version-10
sessions migrated without one) and optional per-currency `EstimatedCosts`;
blocker reasons clear on resume or objective revision, while
cost values come from provider or catalog pricing and are estimates, not invoices. See
[Persistent Thread Goals](goals.md).

## Permissions and security

The SDK has no built-in interactive permission UI. Its omission default is
`deny`; trusted embeddings can deliberately install `PermissionHandler` for
`ask` mode. The handler receives the already-published correlated request and
returns an explicit decision; absent or invalid handling never silently grants
authority.

`AutoApprove` is equivalent to `allow`; it does not add containment. Plugins,
stdio MCP servers, file tools, Bash, managed-process starts/stops, and subagents
execute with their documented host privileges. An empty `Tools` list exposes
all five managed-process tools by default; `process_start` can keep a host
command alive across later prompts until `process_stop`, process exit, session
switch, or normal app shutdown. Session switching stops and clears every managed
process before binding the new session. Handles are runtime-local and crashes
cannot guarantee cleanup.
Snow has no built-in process sandbox; use external isolation when containment is
required. Subagents share filesystem and process side effects but do not receive
the managed-process tools in v1. Project trust controls input loading, not
containment.

For a narrow inspection embedding:

```go
snowsdk.Options{
    PermissionMode:    "deny",
    Tools:             []string{"read", "grep", "glob"},
    NoPlugins:         true,
    NoMCP:             true,
    NoSkills:          true,
    DisableSubagents:  true,
}
```

Read the complete [Security model](security.md) before granting mutation,
execution, network, delegation, or automatic continuation authority.

## Readiness and capabilities

The Go SDK has no JSONL wire handshake; RPC version and capability negotiation
belongs to the [JSONL RPC protocol](rpc.md). Embeddings instead observe session
readiness through the methods below.

### Readiness methods

| Method | Purpose |
|---|---|
| `StateEvent()` | Return the current collaboration-mode snapshot for initial host state |
| `ReadyGoals()` | Publish restored goal state and permit eligible automatic continuation |
| `ReadySubagents()` | Publish restored child topology without restarting stale work |

Call readiness after subscriptions. Both methods drain initial events when
safe; they also avoid deadlock when invoked from event callback context.

### Discovery and diagnostics

| Method | Purpose |
|---|---|
| `MCPServers()` | Return secret-free negotiated MCP status |
| `Skills()` | Return enabled skill metadata exposed to provider context |
| `SkillInventory()` | Return enabled and policy-disabled discovered skills |
| `Diagnostics()` | Return non-fatal theme, keybinding, and search configuration warnings |
| `DebugStatus()` | Return runtime capture state, retained count/bytes, limits, and dropped-event count |
| `SetDebugEnabled(enabled)` | Enable or disable capture for this runtime without rewriting persistent configuration |
| `ClearDebugEvents(ctx)` | Flush pending capture and clear retained events and drop counters |
| `CreateDebugDump(ctx, path)` | Write an idle-boundary `snow-diagnostic-v1` dump; blank path generates one under `$SNOW_HOME/diagnostics` |

Diagnostic capture is disabled by default unless config or an SDK override
enables it. Recorder callbacks are nonblocking and bounded. Dumps contain full
session, prompt, thinking, tool, path, and error content; they omit
`provider_data` and redact known credentials, but must still be reviewed as
sensitive before sharing. `CreateDebugDump` fails while the root agent is
running. See [Security model](security.md#diagnostic-dumps).

`Diagnostics()` does not currently include plugin startup failures or detailed
Agent Skill parse diagnostics; inspect extension inventory and status through
the relevant extension surface and retain startup errors from `Open`. Returned
models, events, queues, skills, and MCP capability slices are defensive copies
at the public observation boundary.

## Concurrency and errors

Exported sentinel errors:

```go
var (
    snowsdk.ErrNotRunning // active-input operation requires a running turn
    snowsdk.ErrStopped    // nil or closed session, or repeated close
)
```

Use `errors.Is`. Configuration, provider, session, tool, goal, and subagent
failures are ordinary wrapped errors. Context cancellation propagates through
provider requests, tools, prompts, waits, and abort paths.

A method error and an `error` event serve different consumers; hosts should
check both returned errors and the event stream. `Abort` clears undelivered
root input. Closing joins owned child work and releases plugin, MCP, and
session resources.

Concurrency guidance:

- One root prompt is admitted at a time. Do not use concurrent `Prompt` calls
  as a queue; use `Steer` and `FollowUp` during a run or serialize prompts in
  the host.
- `Steer` and `FollowUp` are concurrency-safe bounded admissions while the root
  runs.
- Tool calls in one agent are serial. Different subagents may run concurrently.
- Subscription callbacks are ordered observation boundaries with cloned data.
- `WaitSubagentsUntilAll` is a join-style status wait; child results still
  arrive asynchronously as attributed events and mail.
- Mode, model, branch, and compaction operations may reject while conflicting
  work is active.
- Give every long operation a context deadline appropriate to the embedding.

## Limitations

- The SDK does not provide a permission UI; trusted hosts must install and own
  the lifecycle of a `PermissionHandler`, while omission remains deny-by-default.
- Content prompts accept normalized text and image blocks; provider-specific
  opaque continuity blocks remain internal.
- `SessionPath` is open-or-create. There is no strict "must already exist"
  resume option and no saved-session catalog; hosts that require strict resume
  must validate and select the path before `Open`.
- Persisted child message history is not exposed through the SDK; hosts must
  retain attributed events to keep complete child transcripts.
- `RunPrompt` returns root text only; attributed child text requires a normal
  subscribed session.
- `Diagnostics()` omits plugin startup failures and detailed skill parse
  diagnostics.
- The package is alpha; public compatibility is not guaranteed until v1.

## Related documents

- [Configuration](configuration.md)
- [JSONL RPC](rpc.md)
- [Security model](security.md)
- [Sessions](sessions.md)
- [Subagents](subagents.md)
