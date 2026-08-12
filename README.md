# snow-core

**snow** is a small, streaming coding-agent harness written in Go. It ships as
one terminal application and as an embeddable Go SDK, with the same agent loop,
tools, sessions, permissions, and events behind every surface.

> **Project status:** pre-alpha. The core runtime is functional and tested, but
> public APIs and file formats may still change before v1.

- Interactive terminal UI, print mode, JSONL events, and JSONL RPC
- OpenCode Go, user-configured OpenAI-compatible Responses or Chat Completions endpoints, and ChatGPT/Codex OAuth
- SQLite sessions with resume, branches, compaction, and persistent goals
- Built-in coding tools, MCP, plugins, Agent Skills, and optional subagents
- Pure-Go SDK under [`pkg/snowsdk`](pkg/snowsdk)

[Quick start](#quick-start) · [Surfaces](#choose-a-surface) ·
[Capabilities](#capabilities) · [Security](#security-first) ·
[Documentation](#documentation) · [Roadmap](IMPLEMENTATION.md)

## Quick start

### Requirements

- Go 1.27; `go.mod` currently declares `1.27rc2` because that is the available
  toolchain required by the pinned Surf release.
- macOS or Linux for the primary supported path. Windows behavior is covered by
  native path, PowerShell, process-job, and atomic-replacement tests.

### Build or install

```sh
git clone https://github.com/elmissouri16/snow-core.git
cd snow-core

# Build a repository-local binary

go build -o snow ./cmd/snow
./snow --version

# Or install/update ~/.local/bin/snow
./scripts/install-local.sh
export PATH="$HOME/.local/bin:$PATH"
```

Choose either build path. Override the install directory with
`SNOW_INSTALL_DIR=/path/to/bin`. Snow uses the directory where it is launched as
the active project.

### Authenticate

OpenCode Go:

```sh
export OPENCODE_API_KEY=oc-...
snow -p "summarize this repository"
```

Or save the key in Snow's credential store:

```sh
snow login opencode-go
```

OpenAI-compatible endpoint (Responses preferred; Chat Completions fallback; API key optional):

```sh
snow --provider openai-compatible \
  --base-url https://gateway.example/v1 \
  --model model-id --api-key "$OPENAI_API_KEY" \
  -p "summarize this repository"
```

Inside the TUI, `/login openai-compatible` prompts first for the endpoint and
then for an optional masked API key, persists the endpoint in `config.json`, and
refreshes `/models`. The top-level `snow login openai-compatible` command stores
only a key. Leaving the TUI key step blank preserves any existing explicit,
stored, or `OPENAI_API_KEY` fallback; it is keyless only when none of those
sources exists. The endpoint itself never belongs in `auth.json`.

ChatGPT/Codex:

```sh
snow login chatgpt                 # browser PKCE
snow login chatgpt --device-code   # headless/device flow
snow auth check chatgpt            # inspect without refreshing
```

Credentials resolve in this order: explicit `--api-key`/SDK option, Snow's auth
store, then a known environment fallback such as `OPENCODE_API_KEY` or
`OPENAI_API_KEY`. Secrets are
stored separately from configuration and are never printed by inventory commands.
See [ChatGPT authentication](docs/chatgpt-auth.md) for OAuth, refresh, imports,
and account-scoped model catalogs.

### Start the TUI

```sh
cd /path/to/project
snow
```

The first interactive launch in an undecided project asks whether project-local
Snow configuration may be loaded. This is an input-loading decision, not a
sandbox boundary.

## Choose a surface

All surfaces observe the same normalized `protocol.AgentEvent` stream and use
the same provider → tool → session loop.

| Surface | Command or package | Best for |
|---|---|---|
| Interactive TUI | `snow` | Daily terminal coding with pickers, approvals, sessions, Plan Mode, goals, and subagent inspection |
| Print | `snow -p "..."` | Human-readable one-shot automation |
| JSON events | `snow --mode json -p "..."` | Shell pipelines and event recording |
| RPC | `snow --mode rpc` | Long-lived foreign-language/IDE control over JSONL stdio |
| Go SDK | `github.com/snow-core/snow/pkg/snowsdk` | In-process embedding without Cobra or Bubble Tea |

Common examples:

```sh
# Print assistant text and tool status
snow --permission deny -p "list the Go packages"

# Emit one AgentEvent JSON object per line
snow --mode json --permission deny -p "summarize recent changes"

# Pick a saved session for this project, or resume a specific SQLite database
snow resume
snow resume ~/.snow/sessions/<encoded-cwd>/<session>.db

# Start a long-lived RPC process; keep stdin open while prompts run
snow --mode rpc --permission deny
```

The TUI uses Bubble Tea's supported full-window pattern: alternate screen,
sticky header/footer, and a Bubbles transcript viewport. Mouse mode defaults on so wheel/trackpad gestures scroll Snow's transcript viewport instead of terminal scrollback. Primary drag selects and copies transcript text; on Apple Terminal, hold Fn while dragging for instant terminal-native selection. F6 disables mouse reporting when native selection is preferred, with PageUp/PageDown, Home/End, and Ctrl+Up/Ctrl+Down available for viewport scrolling. In the composer, Ctrl+V attaches a PNG/JPEG/GIF/WebP clipboard image for vision-capable models (up to eight images, 20 MiB each); Backspace (or Esc) removes the last image when the text draft is empty.
Read the [user guide](docs/using-snow.md) for TUI keys, slash commands, queue
semantics, sessions, and modes. Read the [RPC protocol](docs/rpc.md) before
building an RPC client; RPC is Snow JSONL, not JSON-RPC 2.0.

## Providers

| Provider | ID | Authentication | Runtime |
|---|---|---|---|
| OpenCode Go | `opencode-go` | API key | OpenAI-compatible chat completions/SSE with live model discovery enriched by models.dev metadata |
| OpenAI-compatible | `openai-compatible` | Optional Bearer API key | User-supplied API root with sibling `/models`; prefers Responses/SSE and falls back to Chat Completions/SSE when Responses is unavailable |
| ChatGPT/Codex | `chatgpt` | OAuth access/refresh token | Codex Responses/SSE with browser/device login, guarded refresh, account-scoped catalogs, session affinity, zstd, and bounded pre-output retries |
| Fake | `fake` | None | Deterministic local provider for tests and examples |

Model metadata controls tool, vision, context, reasoning, summary, verbosity,
and pricing behavior. Thinking levels are model-aware: Snow accepts `off`, `minimal`, `low`,
`medium`, `high`, `xhigh`, `max`, and `ultra`, but exposes only efforts advertised
by the selected model and rejects unsupported explicit levels.

## Capabilities

### Built-in tools

| Tool | Purpose | Default risk |
|---|---|---|
| `read` | Read a bounded file window | read |
| `write` | Atomically create or replace a file | write |
| `edit` | Apply exact, uniqueness-checked replacements | write |
| `bash` | Run a bounded shell command (`sh` on Unix, PowerShell on Windows) | exec |
| `grep` | RE2 text search with globs, ignore files, and output caps | read |
| `glob` | Pure-Go path matching, including recursive `**` | read |
| `ask_user` | Ask the host structured questions | read/interaction |
| `update_plan` | Emit a turn-local Default-mode checklist | read |
| `webfetch` | Fetch bounded public HTTP(S) content as text/Markdown | network |

File tools enforce configured roots through pinned Go `os.Root` handles, so
launch-path replacement and ancestor-swap races cannot redirect built-in file
operations outside the root. Search honors
hierarchical `.gitignore` and `.ignore`, global/trusted-project search policy,
hidden/generated defaults, and per-call exclusions. `webfetch` is deferred,
public-address-only, redirect-checked, and never executes JavaScript.

### Sessions and context

- Pure-Go SQLite session databases with append-only parent-linked entries
- Indexed branch tips, named forks, branch selection, rename, and guarded delete
- Current-directory session picker and explicit path resume
- Turn-aware compaction that preserves complete history, manual for ordinary work and automatic for goals at a configurable context threshold
- Embedded Markdown system preamble with optional global/trusted-project file
  override, plus `AGENTS.md` discovery with a hard byte cap
- Usage and optional catalog-derived cost persisted with assistant messages

See [sessions](docs/sessions.md) and [configuration](docs/configuration.md).

### Collaboration

- **Default mode** allows the normal coding tool surface and turn-local
  `update_plan` checklists.
- **Plan Mode** is a branch-persisted collaboration instruction that asks the
  model not to mutate and emits structured proposed-plan events plus
  `request_user_input`. It is not a permission or sandbox boundary: ordinary
  write, shell, plugin, and MCP capabilities remain behind their normal gates.
- **Thread Goals** attach a persisted objective and optional token budget to a
  session branch, may continue through bounded private turns, and show durable
  cumulative token usage plus estimated cost when model pricing is available.
- **Steering and follow-ups** are accepted only during an active root run and
  delivered one at a time at safe assistant/tool boundaries.

Plan and Goal contracts also use embedded Markdown sources under `internal/plan`
and `internal/goal`; they remain separate from a configurable base preamble.
See [Plan Mode](docs/plan-mode.md), [Thread Goals](docs/goals.md), and
[model-requested user input](docs/user-input.md).

### Extensibility

- **MCP:** official Go SDK client for current stateless Streamable HTTP and stdio,
  with legacy negotiation, tools, resources, prompts, subscriptions, and live
  tool-catalog refresh.
- **Plugins:** statically linked Go extensions or persistent JSON-RPC v2 child
  runtimes with namespaced tools, declared risk, private result metadata,
  progress, cancellation, and explicitly subscribed observe-only events.
  Dependency-free JavaScript and Python examples are included.
- **Agent Skills:** strict open `SKILL.md` validation with metadata-only startup
  context, TUI autocomplete for leading `$skill-name` or model-driven activation,
  pinned on-demand resource confinement, and trust-aware precedence.
- **Tool routing:** opt-in, namespace-first Bleve BM25 discovery keeps deferred
  schemas out of ordinary provider requests, retains a global rescue ranking,
  and exposes `search_tools` as a recovery path.
- **Subagents:** optional bounded child agent tree with independent sessions,
  role-scoped tools, attributed mailboxes, concurrency/depth limits, and
  SDK/RPC/TUI observation.

Validate an external runtime without starting an agent:

```sh
snow plugin check examples/plugins/javascript/manifest.json
snow plugin check examples/plugins/python/manifest.json --json
```

See [MCP](docs/mcp.md), [plugins](docs/plugins.md), the complete
[plugin protocol](docs/plugin-protocol.md), [Agent Skills](docs/skills.md),
[tool routing](docs/tool-routing.md), and [subagents](docs/subagents.md).

Runnable integration projects live under [`examples/`](examples/): a standalone
[Go SDK module](examples/sdk), a dependency-free [Python RPC client](examples/rpc/python),
and JavaScript/Python plugin runtimes. The SDK and RPC examples default to the
credential-free fake provider and are exercised by CI on Linux and macOS.

## Embed with Go

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

    // Safe to call for both new and resumed sessions after subscriptions exist.
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

The SDK defaults headless permission handling to `deny`. `AutoApprove` forces
`allow` and is suitable only for deliberately trusted environments. See the
complete [Go SDK guide and API map](docs/sdk.md).

## Automate over RPC

RPC uses LF-delimited JSON objects over stdin/stdout. Responses and asynchronous
agent events share stdout, so clients must continuously read output and correlate
only objects whose `type` is `response` by `id`.

```json
{"id":"info-1","type":"session_info"}
{"id":"prompt-1","type":"prompt","message":"Summarize this repository"}
{"id":"steer-1","type":"steer","message":"Focus on public APIs"}
{"id":"abort-1","type":"abort"}
```

A prompt receives an immediate acknowledgement; completion is signaled by the
normal event stream, especially `turn_done`. Keep stdin open until work finishes.
See the [RPC protocol reference](docs/rpc.md) for every command, response and
event shape, concurrency rules, user-input replies, goals, and subagents.

## Security first

Snow is a harness, **not a sandbox**:

- Snow, `bash`, plugins, stdio MCP servers, and subagents run with the user's OS
  privileges.
- Headless SDK/RPC/print callers should normally use `deny`; `ask` has no
  interactive permission reply channel outside the TUI and therefore fails closed.
- Project trust permits loading project-local configuration and extensions. It
  does not constrain what an enabled process can do.
- Plan Mode is a collaboration contract, not an OS enforcement boundary.
- Repository text, tool output, extensions, skills, and child results may contain
  prompt injection.
- Subagents share the working tree and process side effects; parallel mutation
  can conflict and each provider request incurs separate usage.
- Auth tokens, API keys, MCP headers, and provider continuity data must never be
  logged.
- A configured `openai-compatible` endpoint is operator-trusted: Snow sends prompts,
  tool schemas/results, and any configured Bearer key to that origin. Cross-origin
  redirects are rejected, but Snow does not certify or sandbox the remote service.

Read the consolidated [security model](docs/security.md) before enabling shell,
network, extension, subagent mutation, or automatic approval in an embedding.

## Configuration and storage

Default global paths:

```text
~/.snow/config.json       runtime defaults
~/.snow/auth.json         provider credentials (0600)
~/.snow/trust.json        project trust decisions (0600)
~/.snow/sessions/         SQLite session databases
~/.snow/keybindings.yaml  TUI key overrides
~/.snow/themes/*.yaml     custom themes
~/.snow/search.yaml       grep/glob policy
```

`SNOW_HOME` relocates global configuration/auth/trust/auxiliary files and
`SNOW_SESSIONS_DIR` relocates session databases. Global or trusted-project
configuration may select a Markdown `system_prompt_file`; explicit SDK
`SystemPrompt` remains highest precedence. Trusted projects may also define a
restricted `.snow/config.json`, `.snow/keybindings.yaml`, `.snow/search.yaml`,
and `.snow/themes/*.yaml`. See the [configuration reference](docs/configuration.md)
for precedence, every global field, project scope, environment variables, and
YAML examples.

## Documentation

Start at the [documentation index](docs/README.md).

| Task | Guide |
|---|---|
| Learn the TUI and CLI modes | [Using Snow](docs/using-snow.md) |
| Configure paths, providers, tools, themes, and search | [Configuration](docs/configuration.md) |
| Embed Snow in Go | [SDK](docs/sdk.md) · [standalone example](examples/sdk) |
| Build a JSONL client | [RPC](docs/rpc.md) · [Python example](examples/rpc/python) |
| Author JavaScript/Python plugins | [Plugins](docs/plugins.md) · [Protocol v2](docs/plugin-protocol.md) |
| Review operational boundaries | [Security](docs/security.md) |
| Authenticate ChatGPT/Codex | [ChatGPT auth](docs/chatgpt-auth.md) |
| Resume and branch conversations | [Sessions](docs/sessions.md) |
| Use Plan Mode, goals, or subagents | [Plan Mode](docs/plan-mode.md) · [Goals](docs/goals.md) · [Subagents](docs/subagents.md) |
| Extend Snow | [MCP](docs/mcp.md) · [Plugins](docs/plugins.md) · [Skills](docs/skills.md) |
| Understand architecture and roadmap | [IMPLEMENTATION.md](IMPLEMENTATION.md) |

## Development

[GitHub Actions CI](.github/workflows/ci.yml) runs the network-free suite on
Linux and macOS, builds the binary, executes the standalone SDK/RPC examples,
and runs the race detector on Linux. The same core checks can be run locally:

```sh
gofmt -w <changed-go-files>
go test ./...
go vet ./...
go test -race ./internal/... ./pkg/snowsdk
(cd examples/sdk && go test ./... && go run .)
go build -o ./snow ./cmd/snow
python3 examples/rpc/python/client.py --snow ./snow

# Optional native Windows validation (not a hosted CI gate)
powershell -ExecutionPolicy Bypass -File scripts/test-windows.ps1
```

Provider integration tests use local mocked HTTP/SSE servers. Real-provider
checks require credentials and should not be added to the default suite.

Repository package boundaries and contributor workflow are documented in
[`AGENTS.md`](AGENTS.md). The architecture, interfaces, decisions, and phased
roadmap live in [`IMPLEMENTATION.md`](IMPLEMENTATION.md).

## Current boundaries and non-goals

- No Electron or desktop shell
- No broad pi/OpenCode-style provider catalog beyond OpenCode Go,
  ChatGPT/Codex, and fake
- No built-in sandbox/container runtime
- No autonomous workflow product beyond the bounded root-scoped subagent tree
- No notes, vector-memory, or marketplace product surface
- Optional MCP Apps, Tasks, Enterprise Managed Authorization, and interactive
  MCP OAuth are not yet exposed
- Namespace-first tool routing remains local BM25; optional semantic/vector
  routing is deferred pending a suitable downloadable cross-platform model
