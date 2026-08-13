# Go SDK

`pkg/snowsdk` embeds Snow's production agent runtime in a Go process. It uses the
same providers, tools, permissions, SQLite sessions, events, goals, MCP, skills,
plugins, and subagent manager as the CLI; it does not import Cobra or Bubble Tea.

Public integration data lives in `pkg/protocol`, a standard-library-only package
shared by SDK, JSON, RPC, plugins, sessions, and the TUI.

> **Stability:** snow-core is pre-alpha. `pkg/snowsdk` and `pkg/protocol` are the
> intended public surfaces, but compatibility is not guaranteed until v1.

## Install

```sh
go get github.com/snow-core/snow/pkg/snowsdk
```

A separate checked-in module under [`examples/sdk`](../examples/sdk) exercises
only the public packages and is run by Linux/macOS CI. From this checkout:

```sh
cd examples/sdk
go run .                         # credential-free fake-provider lifecycle
go run . -provider opencode-go  # real streaming provider
```

Import both packages:

```go
import (
    "github.com/snow-core/snow/pkg/protocol"
    "github.com/snow-core/snow/pkg/snowsdk"
)
```

## Minimal streaming session

This example requires an OpenCode Go credential from `OPENCODE_API_KEY` or
`snow login opencode-go`. Replace the provider with `fake` for a credential-free
compile/lifecycle smoke test (the built-in fake has no scripted text response).

```go
package main

import (
    "context"
    "fmt"

    "github.com/snow-core/snow/pkg/protocol"
    "github.com/snow-core/snow/pkg/snowsdk"
)

func main() {
    ctx := context.Background()
    session, err := snowsdk.Open(ctx, snowsdk.Options{
        Provider:       "opencode-go",
        NoSession:      true,
        PermissionMode: "deny",
    })
    if err != nil {
        panic(err)
    }
    defer session.Close()

    session.Subscribe(func(event protocol.AgentEvent) {
        if event.Agent == nil && event.Type == protocol.EvTextDelta {
            fmt.Print(event.Text)
        }
    })

    // Install subscriptions/interaction handlers before readiness. Calling
    // both is safe for new sessions and is the complete resumed-session pattern.
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

`Prompt` blocks until the root turn finishes. Text, thinking, tools, usage,
plans, goals, and child activity stream through subscriptions while it runs.

## Lifecycle

Recommended host sequence:

1. Build a context with the cancellation/deadline policy for the embedding.
2. Call `snowsdk.Open`.
3. Install `Subscribe` callbacks and `UserInputHandler` in `Options`.
4. Read `StateEvent()` if the host needs the initial collaboration-mode state.
5. Call `ReadyGoals()` and `ReadySubagents()` after observers exist.
6. Call `Prompt`/`PromptWithMode`, or use the control methods while a turn runs.
7. Call `Close` on every exit path.

`Open(nil, ...)` uses `context.Background()`. Construction deliberately does not
restart restored goal work or publish restored subagent topology before the host
can observe it. CLI surfaces perform readiness automatically; SDK hosts own it.

`Close` marks the session stopped before releasing runtime resources. Calls after
close, a nil session, and repeated `Close` return `snowsdk.ErrStopped` where a
method can return an error.

## Options reference

Empty fields usually inherit the selected global configuration. The table
separates inheritance from clean-install defaults.

| Field | Meaning and effective behavior |
|---|---|
| `CWD` | Active project root. Empty uses the process working directory. |
| `Provider` | `opencode-go`, `openai-compatible`, `chatgpt`, or `fake`. Empty inherits config; clean-install default is `opencode-go`. |
| `Model` | Model ID. Empty resolves configured/provider default. |
| `SessionPath` | SQLite `.db` path to open or create; an existing database is resumed. Empty creates a new indexed durable session unless `NoSession` is true. |
| `NoSession` | Use an in-memory conversation. Auth and model caches remain persistent. Goals require a persisted session. |
| `AuthPath` | Credential file override. Empty uses `$SNOW_HOME/auth.json`. |
| `ConfigPath` | Global config override. Empty uses `$SNOW_HOME/config.json`. |
| `PermissionMode` | `ask`, `allow`, or `deny`. **Omission in the SDK forces `deny`**, rather than inheriting interactive `ask`. |
| `AutoApprove` | Forces `allow` and takes precedence over `PermissionMode`. Dangerous outside externally isolated/trusted environments. |
| `Tools` | Built-in tool allowlist. Empty exposes all registered built-ins. |
| `SystemPrompt` | Overrides configured system-prompt files and Snow's embedded Markdown preamble. Project context and runtime steering remain separately assembled where applicable. |
| `Thinking` | `off`, `minimal`, `low`, `medium`, or `high`. Empty inherits config; model metadata may reject a level. |
| `ReasoningSummary` | `off`, `auto`, `concise`, or `detailed`. Empty inherits config. |
| `TextVerbosity` | `low`, `medium`, or `high`. Empty inherits config. |
| `CollaborationMode` | `default` or `plan`. Empty restores branch state or clean-install Default. |
| `PlanModeReasoningEffort` | Optional override for Plan Mode's reasoning preset. |
| `APIKey` | Explicit credential with precedence over auth store and environment. |
| `BaseURL` | Active provider endpoint override; required for `openai-compatible` unless configured globally. Accepts an API root or full `/responses` or `/chat/completions` URL. |
| `Plugins` | Explicit external plugin process declarations. Configured plugins may also load. |
| `GoPlugins` | Statically linked `pkg/plugin.Plugin` implementations supplied by the host. |
| `NoPlugins` | Disable configured, explicit, and Go plugins. |
| `MCPServers` | Additional public `pkg/mcp.ServerSpec` declarations. |
| `NoMCP` | Disable configured and explicit MCP servers. |
| `SkillDirs` | Additional trusted Agent Skills discovery roots. |
| `NoSkills` | Disable skill discovery and activation tools. |
| `EnableSubagents` | Force subagents on for this session. Does not enable mutation or recursion. |
| `DisableSubagents` | Force subagents off. Setting both enable and disable is an error. |
| `SubagentProvider` / `SubagentModel` | Override the automatic provider/model defaults for child agents. |
| `SubagentMaxConcurrency` | Zero inherits config; clean-install default 4, maximum 256. Root does not consume a slot. |
| `SubagentMaxAgents` | Zero inherits config; clean-install default 32, maximum 4096, not below concurrency. |
| `SubagentMaxDepth` | Zero inherits config; clean-install default 1, maximum 8. |
| `UserInputHandler` | Answers `ask_user`; nil fails the tool call fast instead of blocking. It is not a permission asker. |

For an OpenAI-compatible gateway (Responses is preferred; HTTP 404/405/501 selects a cached Chat Completions fallback):

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
is unavailable; arbitrary named compatible providers and Azure/custom headers
remain unsupported. The TUI can persist the endpoint
and optional key through `/login openai-compatible`; SDK callers continue to use
`BaseURL` and `APIKey` explicitly.

Full persistent configuration and project trust rules are in
[Configuration](configuration.md).

## API map

### Constructors and helpers

| API | Purpose |
|---|---|
| `Open(ctx, options)` | Open or create a session and assemble the runtime |
| `MustOpen(ctx, options)` | Panic-on-error helper for tests and tiny programs |
| `RunPrompt(ctx, options, prompt)` | Open, collect root text, prompt, wait for goal/subagent/event quiescence, close |

`RunPrompt` returns only root `text_delta` content. Attributed child text remains
available only through a normal subscribed session. The helper closes the
runtime before returning and joins any plugin/session cleanup failure into its
returned error without discarding accumulated text.

### Turns, modes, and active input

| Method | Purpose |
|---|---|
| `Prompt(ctx, text)` | Run one root user turn to completion |
| `PromptWithMode(ctx, text, mode)` | Atomically select `default`/`plan` and start the prompt |
| `Mode()` / `SetMode(mode)` | Inspect or change collaboration mode while idle |
| `Steer(ctx, text)` | Queue input for the next safe assistant + complete tool-batch boundary |
| `FollowUp(ctx, text)` | Queue input after natural completion and earlier steering |
| `PendingInputs()` | Return an independent queue snapshot |
| `Abort(ctx)` | Cancel admitted work, clear undelivered root queue entries, and defer active goal continuation |
| `IsRunning()` | Report whether a root turn is in flight |

`Steer` and `FollowUp` require an active queue-accepting root turn. Idle calls
return `ErrNotRunning`. The queue is bounded and ordered; steering has priority,
with FIFO order inside each input class.

### Events and readiness

| Method | Purpose |
|---|---|
| `Subscribe(callback)` | Observe isolated copies of normalized agent events; returns unsubscribe |
| `StateEvent()` | Return the current collaboration-mode snapshot for initial host state |
| `ReadyGoals()` | Publish restored goal state and permit eligible automatic continuation |
| `ReadySubagents()` | Publish restored child topology without restarting stale work |

Call readiness after subscriptions. Both methods drain initial events when safe;
they also avoid deadlock when invoked from event callback context.

### Models and response controls

| Method | Purpose |
|---|---|
| `Model()` / `Models()` | Current model and defensive copy of active-provider catalog |
| `SetModel(model)` | Select a model for later turns |
| `Thinking()` / `SetThinking(level)` | Inspect/change normalized effort |
| `ReasoningSummary()` / `SetReasoningSummary(value)` | Inspect/change provider reasoning-summary request |
| `TextVerbosity()` / `SetTextVerbosity(value)` | Inspect/change provider response detail |

Use `model.SupportedThinkingLevels()` and `model.SupportsThinkingLevel(level)`.
Do not infer support from model names or provider branding.

### Messages, usage, and identity

| Method | Purpose |
|---|---|
| `Messages()` | Linearized durable messages on the active branch |
| `Usage()` | Aggregate token/cache/request/cost data for the active branch |
| `SessionID()` | Stable session identifier |
| `SessionName()` | Optional automatic or manually assigned display title |
| `RenameSession(name)` | Change the display title without changing ID/path/history |
| `SessionPath()` | SQLite path, or empty for in-memory sessions |
| `CWD()` | Active project directory |

The first accepted prompt assigns an untitled built-in store a deterministic,
provider-free title. `RenameSession` trims surrounding whitespace, requires
1–72 runes, and rejects control characters. Titles need not be unique and do
not enter provider context.

`protocol.Message` contains parent-linked IDs, typed content blocks, provider and
model metadata, stop reason, usage, and tool-result correlation. In
`protocol.Usage`, `Input` is the total prompt count including cached tokens;
`CacheReadKnown` distinguishes an explicit `CacheRead == 0` miss from a provider
that omitted cache metrics. On aggregate usage, it remains true only when every
included request reported the cache-read metric. A `provider_data` block is
persistence-only and must not be rendered or logged.

### Branches and compaction

| Method | Purpose |
|---|---|
| `Branches()` | List named durable branches and topology |
| `SelectBranch(id)` | Switch active branch; affects later messages, usage, mode, goal, and prompts |
| `Fork(entryID)` | Fork the active branch at an existing entry and activate it |
| `ForkNamed(sourceBranchID, entryID, name)` | Fork an explicit source branch with an optional name |
| `RenameBranch(id, name)` | Change display name without changing stable ID |
| `DeleteBranch(id)` | Delete an eligible inactive leaf branch reference |
| `Compact(ctx)` | Manually summarize older projected context while retaining full history |

Forked branches share existing message rows; they do not copy history. Branch
management is rejected while conflicting root/subagent work is active. Automatic
Default-mode goal continuation may emit compaction events between goal turns at
the configured context threshold; this does not change the manual `Compact`
method contract.

### Persistent Thread Goals

| Method | Purpose |
|---|---|
| `Goal()` | Return active branch goal or nil |
| `CreateGoal(objective, budget, replace)` | Create a goal; `SetGoal` is an alias |
| `EditGoal(objective)` | Rotate objective/goal ID while preserving usage and budget |
| `PauseGoal()` / `ResumeGoal()` | Control eligible automatic continuation; resume also restarts an active abort-deferred goal |
| `ClearGoal()` | Remove the branch goal |
| `ContinueGoal()` | Clear continuation deferral and run eligible idle work |

Normal prompts never clear an abort/manual-compaction deferral; call
`ResumeGoal` or `ContinueGoal` explicitly. Goals require SQLite persistence.
Budgets must be positive. Goal statuses are
`active`, `paused`, `blocked`, `usage_limited`, `budget_limited`, and `complete`.
Returned `protocol.ThreadGoal` values include optional per-currency
`EstimatedCosts`; values come from provider/catalog pricing and are estimates,
not invoices. See [Persistent Thread Goals](goals.md).

### Subagents

| Method | Purpose |
|---|---|
| `SubagentModels()` | Return exact provider/model pairs available to children |
| `SpawnSubagent(ctx, request)` | Create a role-scoped child at a canonical path |
| `SendSubagentMessage(ctx, target, message)` | Queue attributed mail without starting a turn |
| `FollowupSubagent(ctx, target, message)` | Queue and run/reuse an idle child |
| `WaitSubagents(ctx, timeout)` | Wait for one activity/lifecycle change |
| `WaitSubagentsUntilAll(ctx, timeout)` | Wait until every root child is terminal or timeout |
| `InterruptSubagent(ctx, target)` | Cancel only the target's current turn |
| `Subagents()` | Return child snapshots |
| `Subagent(target)` | Inspect one child by canonical path or supported identifier |
| `SubagentUsage()` | Aggregate child usage |

`protocol.SpawnSubagentRequest` fields are `Name`, `Task`, `Role`, `ForkTurns`,
`Provider`, `Model`, and `ReasoningEffort`. `snowsdk.Options.SubagentProvider` and
`SubagentModel` set the automatic provider/model defaults for children. SDK names are strict lowercase segments under
`/root` with letters, digits, and underscores. Unlike the model-facing tool,
the SDK does not normalize hyphens.

Wait results contain aggregate state, not private child content. Results and
mail arrive through attributed `AgentEvent`/`AgentMessage` values. See
[Subagents](subagents.md).

### Discovery and diagnostics

| Method | Purpose |
|---|---|
| `MCPServers()` | Return secret-free negotiated MCP status |
| `Skills()` | Return enabled skill metadata exposed to provider context |
| `SkillInventory()` | Return enabled and policy-disabled discovered skills |
| `Diagnostics()` | Return non-fatal theme/keybinding/search configuration warnings |

Returned models, events, queues, skills, and MCP capability slices are defensive
copies at the public observation boundary.

## Event reference

`protocol.AgentEvent.Type` selects the relevant optional payload fields.

| Category | Event types |
|---|---|
| Streaming | `text_delta`, `thinking_delta`, `usage` |
| Tools | `tool_start`, `tool_progress`, `tool_end`, `tool_routing` |
| Interaction | `user_input_request`, `queue_updated`; `permission_request` exists in the cross-surface protocol but the public headless SDK has no permission asker that emits it |
| Lifecycle/state | `session_updated`, `turn_done`, `error`, `aborted`, `model_changed`, `mode_changed` |
| Plan | `plan_started`, `plan_delta`, `plan_completed`, `plan_update` |
| Compaction | `compaction_started`, `compaction_done`; `Compaction.Automatic` distinguishes goal-triggered runs |
| Goals | `thread_goal_updated` |
| Subagents | `subagent_started`, `subagent_status`, `subagent_message`, `subagent_activity` |

Common payload fields include:

- `Text`, `Message`, `IsError`
- `ToolCallID`, `ToolName`, bounded `ToolOutput`, `ToolDurationMS`,
  `ToolProgress`, `ToolRouting`
- `Usage`, `Model`, `Mode`
- `Plan`, `PlanUpdate`, `Compaction`
- `Permission`, `UserInput`, `Queue`, `ThreadGoal`
- `Agent`, `Subagent`, `AgentMessage`
- `TurnID`, `TurnOrigin`, `GoalContinuing`

Correlation rules:

- `event.Agent == nil` denotes root ordinary events.
- Attributed child stream/tool/usage events carry `Agent`.
- Child lifecycle snapshots carry `Subagent`.
- Mailbox events carry `AgentMessage`.
- `ToolOutput` is a bounded UI preview; complete tool results remain in
  `Messages()`.
- Every subscriber receives a deep clone. Mutating one callback's event cannot
  affect another observer.

Callbacks are dispatched in order. Keep them short: a blocking callback delays
later event delivery. Control calls are designed not to hold event-dispatch
locks, but starting long blocking work inside a callback still serializes
observation, and a dispatcher-reentrant prompt cannot wait on an RPC-style input
reply that the same dispatcher would need to publish.

## One-shot helper

```go
package main

import (
    "context"
    "fmt"

    "github.com/snow-core/snow/pkg/snowsdk"
)

func main() {
    text, err := snowsdk.RunPrompt(context.Background(), snowsdk.Options{
        Provider:       "opencode-go",
        NoSession:      true,
        PermissionMode: "deny",
    }, "Summarize this directory.")
    if err != nil {
        panic(err)
    }
    fmt.Println(text)
}
```

For credential-free harness tests, select `Provider: "fake"` and disable
unneeded plugins/MCP/skills.

## Resume safely

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

Readiness is especially important for restored automatic goals and durable child
topology. Restored stale child work is observed as interrupted metadata; it is
not silently restarted.

## Model-requested input

```go
session, err := snowsdk.Open(ctx, snowsdk.Options{
    Provider:       "opencode-go",
    PermissionMode: "deny",
    UserInputHandler: func(ctx context.Context, req protocol.UserInputRequest) (protocol.UserInputResponse, error) {
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

The handler must answer every question exactly once. It may omit `RequestID` and
Snow will fill it. The `user_input_request` event is published before the
callback runs. Handler errors fail the tool interaction rather than hanging.
See [Model-requested user input](user-input.md).

## Steering an active prompt

Run the blocking prompt in a goroutine and use events or host state to decide
when steering is useful:

```go
result := make(chan error, 1)
go func() {
    result <- session.Prompt(ctx, "Review the repository.")
}()

// A real host normally reacts to an event/UI action here.
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

`IsRunning` is only a snapshot; the turn may finish before `Steer`. Always handle
`ErrNotRunning` as a normal race.

## Plan Mode and branches

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

## Errors and cancellation

Exported sentinel errors:

```go
var (
    snowsdk.ErrNotRunning // active-input operation requires a running turn
    snowsdk.ErrStopped    // nil/closed session or repeated close
)
```

Use `errors.Is`. Configuration, provider, session, tool, goal, and subagent
failures are ordinary wrapped errors. Context cancellation propagates through
provider requests, tools, prompts, waits, and abort paths.

A method error and an `error` event serve different consumers; hosts should
check both returned errors and the event stream. `Abort` clears undelivered root
input. Closing joins owned child work and releases plugins/MCP/session resources.

## Concurrency guidance

- One root prompt is admitted at a time. Do not use concurrent `Prompt` calls as
  a queue; use `Steer`/`FollowUp` during a run or serialize prompts in the host.
- `Steer` and `FollowUp` are concurrency-safe bounded admissions while the root
  runs.
- Tool calls in one agent are serial. Different subagents may run concurrently.
- Subscription callbacks are ordered observation boundaries with cloned data.
- `WaitSubagentsUntilAll` is a join-style status wait; child results still arrive
  asynchronously as attributed events/mail.
- Mode/model/branch/compaction operations may reject while conflicting work is
  active.
- Give every long operation a context deadline appropriate to the embedding.

## Permissions and security

The SDK has no built-in interactive permission UI. Its omission default is
`deny`; `ask` uses a deny-by-default asker unless the embedding supplies a lower-
level interactive host, which `pkg/snowsdk` does not currently expose.

`AutoApprove` is equivalent to `allow`. It does not add a sandbox. Shell,
plugins, stdio MCP servers, and subagents execute with the user's OS privileges.
Subagents share filesystem/process side effects. Project trust controls input
loading, not containment.

For a narrow inspection embedding:

```go
snowsdk.Options{
    PermissionMode: "deny",
    Tools:          []string{"read", "grep", "glob"},
    NoPlugins:      true,
    NoMCP:          true,
    NoSkills:       true,
    DisableSubagents: true,
}
```

Read the complete [Security model](security.md) before granting mutation,
execution, network, delegation, or automatic continuation authority.
