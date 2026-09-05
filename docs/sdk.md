# Go SDK

Use `pkg/snowsdk` to run Snow's agent loop inside a Go process. The SDK uses
the same providers, tools, permissions, sessions, and normalized events as the
CLI without importing Cobra or Bubble Tea.

> **Note:** Snow is alpha software. `pkg/snowsdk` and the standard-library-only
> `pkg/protocol` package are the intended public surfaces, but compatibility is
> not guaranteed until v1.

## On this page

- [Install](#install)
- [Run a streaming session](#run-a-streaming-session)
- [Choose essential options](#choose-essential-options)
- [Follow the lifecycle](#follow-the-lifecycle)
- [Handle permissions and input](#handle-permissions-and-input)
- [Use a compatible endpoint](#use-a-compatible-endpoint)
- [Resume a session](#resume-a-session)
- [Handle events and errors](#handle-events-and-errors)
- [Understand current limits](#understand-current-limits)
- [Related documents](#related-documents)

## Install

```sh
go get github.com/elmissouri16/snow-core/pkg/snowsdk
```

Import the SDK and normalized protocol types:

```go
import (
    "github.com/elmissouri16/snow-core/pkg/protocol"
    "github.com/elmissouri16/snow-core/pkg/snowsdk"
)
```

The maintained standalone example uses only public packages:

```sh
cd examples/sdk
go run .
go run . -provider opencode-go
```

The first command uses Snow's credential-free fake provider to verify the SDK
lifecycle. The second requires `OPENCODE_API_KEY` or `snow login opencode-go`.

## Run a streaming session

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

    if err := session.ReadyGoals(); err != nil {
        panic(err)
    }
    if err := session.ReadySubagents(); err != nil {
        panic(err)
    }

    if err := session.Prompt(
        ctx,
        "List the Go packages in this repository.",
    ); err != nil {
        panic(err)
    }
}
```

`Prompt` blocks until the root turn finishes. Subscriptions receive text,
thinking, tool, usage, permission, plan, goal, and child-agent events while the
turn runs.

Use `Provider: "fake"` for a credential-free lifecycle test. The fake provider
emits completion events but no scripted response text.

## Choose essential options

Empty fields generally inherit global configuration.

| Option | Purpose |
|---|---|
| `CWD` | Select the active project root |
| `Provider`, `Model` | Select a built-in or configured provider and model |
| `SessionPath` | Open or create a specific SQLite session |
| `NoSession` | Keep conversation history in memory only |
| `PermissionMode` | Choose `deny`, `ask`, or `allow` |
| `Tools` | Restrict built-in tools to an allowlist |
| `CollaborationMode` | Start in `default` or `plan` |
| `APIKey`, `BaseURL` | Configure one explicit compatible endpoint |
| `NoPlugins`, `NoMCP`, `NoSkills` | Disable extension families |
| `EnableSubagents`, `DisableSubagents` | Override subagent enablement |
| `PermissionHandler` | Broker correlated permission decisions |
| `UserInputHandler` | Broker model-requested user input |

SDK permission handling fails closed: an omitted `PermissionMode` uses `deny`
rather than inheriting the interactive CLI default. `AutoApprove` forces
`allow` and should be used only inside a deliberately trusted or externally
isolated environment.

See the repository-only
[SDK reference](https://github.com/elmissouri16/snow-core/blob/main/docs/sdk-reference.md)
for the complete option, method, event, goal, branch, diagnostic, and subagent
inventory.

## Follow the lifecycle

Use this order for long-lived SDK sessions:

1. Create a context with the host's timeout and cancellation policy.
2. Call `snowsdk.Open`.
3. Subscribe to events and install trusted input or permission handlers.
4. Read `StateEvent()` if the host needs the initial mode immediately.
5. Call `ReadyGoals()` and `ReadySubagents()` after observers are installed.
6. Call `Prompt`, `PromptWithMode`, or other control methods.
7. Call `Close` on every exit path.

Construction does not restart restored goal work or publish restored child
state before the host is ready to observe it. The CLI handles readiness
automatically; an SDK host owns it.

For a short one-shot integration, use `RunPrompt`:

```go
text, err := snowsdk.RunPrompt(
    context.Background(),
    snowsdk.Options{
        CWD:              ".",
        Provider:         "opencode-go",
        NoSession:        true,
        PermissionMode:   "deny",
        NoPlugins:        true,
        NoMCP:            true,
        NoSkills:         true,
        DisableSubagents: true,
    },
    "Summarize this directory.",
)
if err != nil {
    return err
}
fmt.Println(text)
```

`RunPrompt` returns root-agent text only and closes the runtime before
returning. Omitted options can inherit the caller's working directory and
configured plugins, MCP servers, skills, or subagents, so disable ambient
capabilities explicitly when the host does not intend to load them.

## Handle permissions and input

Use `ask` only when the host supplies a trusted broker:

```go
session, err := snowsdk.Open(ctx, snowsdk.Options{
    PermissionMode: "ask",
    PermissionHandler: func(
        ctx context.Context,
        request protocol.PermissionRequest,
    ) (protocol.PermissionResponse, error) {
        return protocol.PermissionResponse{
            RequestID: request.ID,
            Decision:  protocol.PermissionAllowSession,
        }, nil
    },
})
```

Handler errors, invalid decisions, and mismatched request IDs deny the action.
Without a handler or explicitly enabled event-loop replies, `ask` denies
immediately rather than blocking. For Bash, inspect `Effects`, `Capabilities`,
`Paths`, `Unknown`, and `Rememberable` before deciding. `allow_session` and
`allow_always` remember only the analyzed workspace/capability/resource scope;
when `Rememberable` is false they apply once and are not stored. Approved Bash
still runs with the Snow process's host privileges.

A trusted host can similarly set `UserInputHandler` for model-requested
questions, or enable manual replies and correlate each response with its exact
request ID. Reject pending requests when the host UI closes.

> **Warning:** A permission handler is an authority boundary. Do not approve a
> request based only on model-generated explanation or presentation.

## Use a compatible endpoint

Pass a compatible endpoint and optional key directly:

```go
session, err := snowsdk.Open(ctx, snowsdk.Options{
    Provider:       "openai-compatible",
    BaseURL:       "https://gateway.example/v1",
    APIKey:        os.Getenv("GATEWAY_API_KEY"),
    Model:         "model-id",
    NoSession:     true,
    PermissionMode: "deny",
})
```

`BaseURL` accepts an API root or a complete `/responses` or
`/chat/completions` URL. The provider uses Responses streaming when available
and can fall back to Chat Completions streaming. To use a named profile from
Snow configuration, set `Provider: "profile-name"` instead of passing the
endpoint fields directly. Arbitrary per-call custom headers and Azure-specific
parameters remain unsupported.

## Resume a session

Set `SessionPath` to open or create a specific database:

```go
session, err := snowsdk.Open(ctx, snowsdk.Options{
    SessionPath:    databasePath,
    PermissionMode: "deny",
})
if err != nil {
    return err
}
defer session.Close()

unsubscribe := session.Subscribe(handleEvent)
defer unsubscribe()

handleEvent(session.StateEvent())
if err := session.ReadyGoals(); err != nil {
    return err
}
if err := session.ReadySubagents(); err != nil {
    return err
}
return session.Prompt(ctx, "Continue from the saved state.")
```

`SessionPath` is open-or-create. Validate the path before `Open` when the host
must require an existing session. The SDK does not currently provide the CLI's
saved-session picker.

Use SDK branch and fork methods rather than importing packages under
`internal/`.

## Handle events and errors

`protocol.AgentEvent.Type` identifies the active payload. Common categories are:

| Category | Examples |
|---|---|
| Streaming | `text_delta`, `thinking_delta`, `usage`, `provider_retry` |
| Tools | `tool_start`, `tool_progress`, `tool_end` |
| Interaction | `permission_request`, `user_input_request`, `queue_updated` |
| Lifecycle | `turn_done`, `error`, `aborted`, `mode_changed` |
| Planning and goals | `plan_delta`, `plan_completed`, `thread_goal_updated` |
| Subagents | `subagent_started`, `subagent_status`, `subagent_message` |

`event.Agent == nil` identifies root-agent events. Attributed child events carry
an agent identity. Callbacks run in order and should return quickly; offload
blocking work to the host's own queue.

Use `errors.Is` with exported SDK errors. In particular:

- `snowsdk.ErrBusy` means an idle-only operation was attempted during work;
- `snowsdk.ErrNotRunning` means steering or follow-up had no active turn;
- `snowsdk.ErrStopped` means the session is closed;
- `snowsdk.ErrUnsupported` means the requested capability is unavailable.

Use context cancellation and deadlines for prompts and host shutdown. Always
call `Close`, even after an error, so Snow can release sessions, extensions,
processes, and event delivery cleanly.

## Understand current limits

- The SDK is Go-only and alpha.
- `ask` has no built-in UI; the host must provide a trusted broker.
- `SessionPath` is open-or-create and there is no saved-session catalog.
- Persisted child transcripts are not exposed as ordinary root messages.
- `RunPrompt` returns root text only.
- Provider-private continuity remains internal.
- Complete RPC and plugin protocol contracts are repository references, not
  alternate language SDKs.

## Related documents

- [Configuration](configuration.md)
- [Security model](security.md)
- [Sessions and branches](sessions.md)
- [Subagents](subagents.md)
- [Maintained SDK example](../examples/sdk)
- [Complete SDK
  reference](https://github.com/elmissouri16/snow-core/blob/main/docs/sdk-reference.md)
