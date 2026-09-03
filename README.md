# Snow

Snow (`snow-core`) is a small, streaming coding-agent harness written in Go. One
agent loop powers an interactive terminal UI, print/JSON/RPC command-line modes,
and an embeddable pure-Go SDK, with the same tools, sessions, permissions, and
events behind every surface.

[![Go 1.27rc3](https://img.shields.io/badge/Go-1.27rc3-00ADD8)](https://go.dev/doc/install)
[![CI](https://github.com/elmissouri16/snow-core/actions/workflows/ci.yml/badge.svg)](https://github.com/elmissouri16/snow-core/actions/workflows/ci.yml)

> **Note:** Snow is alpha software. The core runtime is functional and tested,
> but public APIs, protocols, configuration, and file formats may still change
> before v1.

- Interactive terminal UI, print mode, JSONL events, and JSONL RPC
- OpenCode Go, OpenCode Zen free models, user-configured OpenAI-compatible
  endpoints, and ChatGPT/Codex OAuth
- SQLite sessions with resume, branches, compaction, and persistent goals
- Built-in coding tools, MCP, plugins, Agent Skills, and optional subagents
- Opt-in bounded diagnostics with private, credential-redacted session dumps
- Opt-in interactive GitHub release checks and verified self-updates
- Pure-Go SDK under [`pkg/snowsdk`](pkg/snowsdk)

## On this page

- [Quick start](#quick-start)
- [Choose a surface](#choose-a-surface)
- [Providers](#providers)
- [Capabilities](#capabilities)
- [Embed with Go](#embed-with-go)
- [Automate over RPC](#automate-over-rpc)
- [Security first](#security-first)
- [Configuration and storage](#configuration-and-storage)
- [Documentation](#documentation)
- [Development](#development)
- [Remaining roadmap](#remaining-roadmap)
- [Further reading](#further-reading)

## Quick start

The canonical [getting-started guide](docs/getting-started.md) walks through
installation, a credential-free smoke test, provider authentication, the first
interactive prompt, permissions, and troubleshooting.

Snow release archives support macOS and Linux on amd64 and arm64. Install the
latest published release with a POSIX shell; Go is not required:

```sh
curl -fsSL https://raw.githubusercontent.com/elmissouri16/snow-core/main/scripts/install.sh | sh
```

The installer verifies the release checksum and binary version, installs to
`~/.local/bin/snow`, and adds that directory to the appropriate Bash, Zsh, or
POSIX startup file. Restart your shell after the first installation.

Set `SNOW_INSTALL_DIR` to another absolute directory, `SNOW_VERSION` to an exact
release such as `v0.1.0-alpha.1`, or `SNOW_NO_MODIFY_PATH=1` to leave startup
files unchanged. Piping a remote script into `sh` trusts the repository; review
[`scripts/install.sh`](scripts/install.sh) first when your environment requires
it. Checksums protect release integrity but are not an independent signature.

Interactive users can open `/settings` to check for newer GitHub releases,
install an update, or opt into startup checks and automatic installation. Both
startup options are disabled by default. Startup checks run only in the TUI;
print, JSON, RPC, and SDK startup never contacts GitHub or replaces the
executable implicitly. Self-update supports official macOS/Linux release builds
whose current regular executable can be safely replaced, including custom
writable install directories. Development builds are never replaced.

Verify the local agent loop without credentials:

```sh
snow --provider fake --no-session -p "hello"
```

Then launch Snow from the project you want to work on. A fresh configuration can
use anonymous OpenCode Zen, or you can authenticate another provider first:

```sh
cd /path/to/project
snow --provider opencode-zen

# Alternatives:
snow login chatgpt
snow --provider chatgpt
snow login opencode-go
snow --provider opencode-go
```

The first interactive launch asks whether project-local Snow configuration may
be loaded. This is an input-loading decision, not a sandbox boundary. Review the
[security model](docs/security.md) before granting broad tool authority.

To build the checkout instead, install Go 1.27rc3, then run:

```sh
go build -o snow ./cmd/snow
./snow --version
```

## Choose a surface

All surfaces observe the same normalized `protocol.AgentEvent` stream and use
the same provider → tool → session loop.

| Surface | Command or package | Best for |
|---|---|---|
| Interactive TUI | `snow` | Daily terminal coding with pickers, approvals, sessions, Plan Mode, goals, and subagent inspection |
| Print | `snow -p "..."` | Human-readable one-shot automation |
| JSON events | `snow --mode json -p "..."` | Shell pipelines and event recording |
| RPC | `snow --mode rpc` | Versioned, long-lived foreign-language/IDE control over JSONL stdio |
| Go SDK | `github.com/elmissouri16/snow-core/pkg/snowsdk` | In-process embedding without Cobra or Bubble Tea |

Common examples:

```sh
# Print assistant text and tool status
snow --permission deny -p "list the Go packages"

# Emit one AgentEvent JSON object per line
snow --mode json --permission deny -p "summarize recent changes"

# Pick a saved session, branch it, or create an independent fork
snow resume
snow resume ~/.snow/sessions/<encoded-cwd>/<session>.db
snow fork SESSION.db --from-entry ENTRY --name independent
snow fork-worktree SESSION.db --worktree ../project-experiment --git-branch snow/experiment

# Start a long-lived RPC process; keep stdin open while prompts run
snow --mode rpc --permission deny
```

The TUI uses Bubble Tea's supported full-window pattern: alternate screen,
sticky header/footer, and a Bubbles transcript viewport. `/processes` opens an
auto-refreshing process fleet inspector with a selectable managed-process list
and a live, scrollable combined stdout/stderr panel; `/processes ID_OR_NAME`
preselects one record. `Alt+P` opens the process fleet directly, while `Alt+A`
opens the subagent fleet; both remain available during active turns. Model,
interaction, tool, and subprocess text is stripped of terminal controls before
Snow adds its own display styling, so untrusted CSI/OSC sequences cannot become
terminal commands. Mouse mode defaults on
so wheel/trackpad gestures scroll Snow's transcript viewport instead of terminal
scrollback. Primary drag selects and copies transcript text; on Apple Terminal,
hold Fn while dragging for terminal-native selection. Right-click opens Snow's
compact context menu for the current selection; choosing **Copy selection**
writes the host clipboard (`pbcopy` on macOS, standard Linux clipboard tools,
with OSC 52 fallback) without changing mouse mode, so viewport scrolling stays
active. F6 toggles app/native mode explicitly, and PageUp/PageDown, Home/End,
and Ctrl+Up/Ctrl+Down scroll the viewport. In the composer, Ctrl+V inserts an
inline `[Image #N]` attachment for a PNG/JPEG/GIF/WebP clipboard image (up to
eight images, 20 MiB each and 40 MiB aggregate), sent only to vision-capable
models; Backspace or Esc removes the last image and token when no ordinary text
remains.

Read the [user guide](docs/using-snow.md) for TUI keys, slash commands, queue
semantics, sessions, and modes. Read the [RPC protocol](docs/rpc.md) before
building an RPC client; RPC is Snow JSONL, not JSON-RPC 2.0.

## Providers

Use the concise [provider setup guide](docs/providers.md) to connect any
supported provider.

| Provider | ID | Authentication | Runtime |
|---|---|---|---|
| OpenCode Go | `opencode-go` | API key | OpenAI-compatible chat completions/SSE with live model discovery enriched by models.dev metadata |
| OpenCode Zen | `opencode-zen` | Optional API key or anonymous | Maintained promotional free catalog; live availability plus models.dev reasoning discovery; per-model Chat Completions or Responses/SSE; bounded 429 retries |
| OpenAI-compatible | `openai-compatible` or a named profile | Optional Bearer API key per profile | One or more user-supplied API roots with sibling `/models`; prefers Responses/SSE and falls back to Chat Completions/SSE |
| ChatGPT/Codex | `chatgpt` | OAuth access/refresh token | Codex Responses/SSE with browser/device login, guarded refresh, account-scoped catalogs, session affinity, zstd, and bounded pre-output retries |
| Fake | `fake` | None | Deterministic local provider for tests and examples |

Model metadata controls tool, vision, context, reasoning, summary, verbosity,
and pricing behavior. Thinking levels are model-aware: Snow accepts `off`,
`minimal`, `low`, `medium`, `high`, `xhigh`, `max`, and `ultra`, but exposes
only explicit per-model efforts advertised by provider metadata and rejects
unsupported levels. Generic reasoning-support flags never synthesize picker
options; without an advertised effort list, only Snow's local `off` is exposed.

## Capabilities

### Built-in tools

| Tool | Purpose | Default risk |
|---|---|---|
| `read` | Read a bounded file window | read |
| `write` | Atomically create or replace a file; new files honor the process umask | write |
| `edit` | Apply exact, uniqueness-checked replacements to files up to 8 MiB | write |
| `bash` | Run a bounded foreground POSIX `sh` command | exec |
| `process_start` / `process_stop` | Start or stop an app-owned background process group | exec |
| `process_status` / `process_logs` / `process_list` | Inspect bounded session-scoped managed-process state and output | read |
| `grep` | RE2 text search with globs, ignore files, and output caps | read |
| `glob` | Pure-Go path matching, including recursive `**` | read |
| `ask_user` | Ask the host structured questions | read/interaction |
| `update_plan` | Emit a turn-local Default-mode checklist | read |
| `artifact_read` / `artifact_grep` | Retrieve bounded fragments from private spilled tool results (deferred) | read |
| `webfetch` | Fetch bounded public HTTP(S) content as text/Markdown | network |

File tools enforce configured roots through pinned Go `os.Root` handles, so
launch-path replacement and ancestor-swap races cannot redirect built-in file
operations outside the root. Search honors hierarchical `.gitignore` and
`.ignore`, global/trusted-project search policy, hidden/generated defaults, and
per-call exclusions. `webfetch` is deferred, public-address-only,
redirect-checked, and never executes JavaScript. Managed background processes
run across later turns and branches in the active session, continuously retain a
bounded output tail, and are stopped and reaped on normal Snow shutdown. Their
opaque handles are runtime-local and are never reattached after restart.

### Sessions and context

- Pure-Go SQLite session databases with append-only parent-linked entries
- Indexed branch tips, named same-database forks, branch selection, rename, and
  guarded delete
- Independent durable session snapshots with immutable parent provenance and
  exact stable-entry boundaries
- Clean Git-worktree forks with direct argument-based Git execution, rollback,
  and an independent project trust identity
- Current-directory session picker with automatic first-prompt titles, manual
  rename, explicit path resume, and a three-way `/fork` picker
- Turn-aware compaction for ordinary, goal, and child turns at configurable
  total-context and aggregate old-tool-history thresholds; completed tool cycles
  inside one long active turn are safe checkpoint boundaries, and context
  overflow receives at most one dedicated compaction recovery
- Durable structured working-state checkpoints preserve objectives, decisions,
  files, deterministic verification/failure evidence, collaboration updates,
  retrieval references, and pending work while complete old turns—including
  provider continuity—leave only the model-facing projection
- Oversized plain-text tool results spill to private session-scoped artifacts;
  provider context keeps bounded previews, and compacted tool prefixes gain a
  bounded verified text/metadata transcript reference without rewriting
  append-only history
- Centralized cancellation-aware provider recovery with structured temporary
  outage/throttle classification, exponential jittered backoff, `Retry-After`,
  a five-minute ordinary window, and a longer 30-minute `/goal` window
- Strict provider terminal-event validation, stop/content consistency checks,
  and synthetic errors instead of executing length-truncated tool calls
- Resume-time repair of interrupted final tool batches with risk-aware
  unknown-outcome results instead of automatic side-effect retries
- Run-scoped tool-call limits plus advisory detection of identical consecutive
  calls, with bounded reminders at escalating thresholds to break unproductive
  loops
- Embedded Markdown system preamble with optional global/trusted-project file
  override, plus `AGENTS.md` discovery with a hard byte cap
- Usage and optional catalog-derived cost persisted with assistant messages
- Deferred same-project `session_search` and `session_reference` tools with a
  disposable FTS5 index, tip-pinned bounded snapshots, and private-data
  exclusions

See [sessions](docs/sessions.md) and [configuration](docs/configuration.md).

### Collaboration

- **Default mode** allows the normal coding tool surface and turn-local
  `update_plan` checklists.
- **Plan Mode** is a branch-persisted collaboration mode that emits structured
  proposed-plan events plus `request_user_input` and enforces a non-mutation
  boundary before permission checks and tool dispatch. File writes, arbitrary
  shell execution, process lifecycle changes, mutating or unclassified
  extensions, and mutation-capable subagents require an explicit transition to
  Default mode. This application policy is defense in depth, not an OS sandbox.
- **Thread Goals** attach a persisted objective and optional token budget to a
  session branch, may continue through bounded private turns, and show durable
  cumulative token usage plus estimated cost when model pricing is available.
- **Steering and follow-ups** are accepted only during an active root run and
  delivered one at a time at safe assistant/tool boundaries. Provider failures
  continue with accepted input; internal failures and turn-limit rejection keep
  undelivered entries recoverable instead of silently dropping them.

Plan and Goal contracts use embedded Markdown sources under `internal/plan` and
`internal/goal`, separate from a configurable base preamble. See
[Plan Mode](docs/plan-mode.md), [Thread Goals](docs/goals.md), and
[model-requested user input](docs/user-input.md).

### Extensibility

- **MCP:** official Go SDK client for current stateless Streamable HTTP and
  stdio, with legacy negotiation, tools, resources, prompts, subscriptions,
  live tool-catalog refresh, and opt-in lazy or lazy-keep-alive connections
  backed by a bounded, secret-free catalog cache with explicit status, refresh,
  clear, and strict no-bootstrap startup.
- **Plugins:** statically linked Go extensions or persistent JSON-RPC v2 child
  runtimes with namespaced tools, declared risk, private result metadata,
  progress, cancellation, and explicitly subscribed observe-only events.
  Dependency-free external protocol examples are included, and configuration
  has side-effect-free list/get plus add/enable/disable/remove management.
- **Agent Skills:** strict open `SKILL.md` validation with metadata-only startup
  context, inline `$skill-name` autocomplete and explicit activation, pinned
  on-demand resource confinement, and trust-aware precedence.
- **Tool routing:** opt-in, namespace-first Bleve BM25 discovery keeps deferred
  schemas out of ordinary provider requests, retains a global rescue ranking,
  and exposes `search_tools` as a recovery path.
- **Subagents:** optional bounded child agent tree with independent sessions,
  role-scoped tools, attributed mailboxes, concurrency/depth limits, and
  SDK/RPC/TUI observation.

Validate or manage external runtimes without hot-loading configured plugins:

```sh
snow plugin check ./my-plugin/manifest.json --json
snow plugin add ./my-plugin/manifest.json --project # staged disabled
snow plugin enable my-plugin --project              # next launch; restart required
snow plugin list --all
```

`plugin check` starts the runtime, so it requires the same trust as executing
other code. List/get/add/enable/disable/remove never start a plugin.

See [MCP](docs/mcp.md), [plugins](docs/plugins.md), the complete
[plugin protocol](docs/plugin-protocol.md), [Agent Skills](docs/skills.md),
[tool routing](docs/tool-routing.md), and [subagents](docs/subagents.md).

The runnable SDK integration is the standalone [Go SDK module](examples/sdk).
It defaults to the credential-free fake provider and is exercised by CI on
Linux and macOS. Other files under `examples/` are raw external-protocol
fixtures, not JavaScript or Python SDKs.

## Embed with Go

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

RPC protocol v1 uses LF-delimited JSON objects over stdin/stdout. The first
frame is `rpc_ready`; responses, `prompt_completed`, and asynchronous agent
events then share stdout. Clients correlate responses by `id` and terminal
prompt results by `request_id`.

```json
{"id":"info-1","type":"session_info"}
{"id":"prompt-1","type":"prompt","message":"Summarize this repository"}
{"id":"steer-1","type":"steer","message":"Focus on public APIs"}
{"id":"abort-1","type":"abort"}
```

A prompt receives an immediate admission acknowledgement. `turn_done` ends the
agent turn; exactly one later `prompt_completed` frame reports definitive
`completed`, `failed`, or `canceled` status. Model discovery is available
through `models_list` and `subagent_models`. Keep stdin open until work
finishes. See the [RPC protocol reference](docs/rpc.md).

## Security first

Snow is a harness, **not a whole-process sandbox**:

- Snow, `bash`, plugins, stdio MCP servers, and subagents run with the user's
  OS privileges. Snow has no built-in process sandbox; use an external
  container, VM, or OS policy when containment is required.
- Headless SDK/RPC/print callers should normally use `deny`. Print mode has no
  interactive permission reply channel and denies in `ask`; trusted Go SDK and
  RPC hosts can deliberately install a permission handler or resolve correlated
  `permission_request` events with `permission_reply`/`permission_reject`.
- Project trust permits loading project-local configuration and extensions. It
  does not constrain what an enabled process can do.
- Plan Mode enforces Snow's application-level non-mutation policy before tool
  permission checks, but it is not an OS sandbox. Unknown and potentially
  mutating tools, including arbitrary Bash, are blocked until an explicit
  transition to Default mode.
- Repository text, tool output, extensions, skills, and child results may
  contain prompt injection.
- Subagents share the working tree and process side effects; parallel mutation
  can conflict and each provider request incurs separate usage.
- Auth tokens, API keys, MCP headers, and provider continuity data must never be
  logged.
- A configured `openai-compatible` endpoint is operator-trusted: Snow sends
  prompts, tool schemas/results, and any configured Bearer key to that origin.
  Cross-origin redirects are rejected, but Snow does not certify or sandbox the
  remote service.

Read the consolidated [security model](docs/security.md) before enabling shell,
network, extension, subagent mutation, or automatic approval in an embedding.
Report suspected vulnerabilities privately through the
[security policy](SECURITY.md), not in public issues.

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

`SNOW_HOME` relocates global configuration, auth, trust, cache, and auxiliary
files, and `SNOW_SESSIONS_DIR` relocates session databases. Interactive provider,
model, and thinking selections are remembered independently for each absolute
project directory in the operator-owned global config. Global or trusted-project
configuration may select a Markdown `system_prompt_file`; explicit SDK
`SystemPrompt` remains highest precedence. Trusted projects may also define a
restricted `.snow/config.json`, `.snow/keybindings.yaml`, `.snow/search.yaml`,
and `.snow/themes/*.yaml`. See the
[configuration reference](docs/configuration.md) for precedence, every global
field, project scope, environment variables, and YAML examples.

## Documentation

The [Snow documentation site](https://elmissouri16.github.io/snow-core/) is the
curated manual for installing and using Snow. Start with
[Install Snow and run your first prompt](docs/getting-started.md).

| User task | Guide |
|---|---|
| Learn the TUI and CLI modes | [Using Snow](docs/using-snow.md) |
| Configure paths, providers, tools, themes, and search | [Configuration](docs/configuration.md) |
| Resume work, plan, pursue goals, or delegate | [Sessions](docs/sessions.md) · [Plan Mode](docs/plan-mode.md) · [Goals](docs/goals.md) · [Subagents](docs/subagents.md) |
| Extend Snow | [Agent Skills](docs/skills.md) · [MCP](docs/mcp.md) · [Plugins](docs/plugins.md) |
| Embed or automate Snow | [Go SDK](docs/sdk.md) · [JSONL RPC](docs/rpc.md) · [Plugin protocol](docs/plugin-protocol.md) |
| Understand operational boundaries | [Security model](docs/security.md) |

Maintainer, architecture, release, audit, research, and implementation records
remain available in the repository but are intentionally excluded from Pages.
Use the repository-only [complete documentation index](docs/README.md) to find
them.

## Development

[GitHub Actions CI](.github/workflows/ci.yml) runs automatically for `main`
pushes and pull requests and remains manually dispatchable. Linux and macOS run
the network-free suite, support-script tests, binary, and Go SDK example; Linux
also runs the race detector, deterministic performance-regression guard, four
release-target cross-builds, and `govulncheck`. The pinned
[Documentation workflow](.github/workflows/pages.yml) stages, builds, validates,
and deploys the GitHub Pages site after relevant `main` changes.
This local list is the common baseline;
the affected-area matrix in [`IMPLEMENTATION.md`](IMPLEMENTATION.md#testing-and-verification)
is the complete maintainer reference:

```sh
gofmt -w <changed-go-files>
go test ./...
go vet ./...
python3 scripts/check_benchmarks.py
go test -race ./internal/... ./pkg/snowsdk
(cd examples/sdk && go test ./... && go run .)
go build -o ./snow ./cmd/snow
# Release check after installing the pinned command:
govulncheck ./...
```

Provider integration tests use local mocked HTTP/SSE servers. Real-provider
checks require credentials and should not be added to the default suite.

Repository package boundaries and contributor workflow are documented in
[`AGENTS.md`](AGENTS.md). The architecture, interfaces, decisions, and phased
roadmap live in [`IMPLEMENTATION.md`](IMPLEMENTATION.md).

## Remaining roadmap

- Namespace-first tool routing currently uses local BM25; optional
  semantic/vector routing is deferred pending a suitable downloadable
  cross-platform model.

## Further reading

- [Documentation index](docs/README.md) — every user, integration, extension,
  and maintainer guide in one place.
- [Documentation site](docs/pages.md) — GitHub Pages publishing, validation,
  and troubleshooting.
- [Security model](docs/security.md) and [reporting policy](SECURITY.md) —
  operational boundaries and private vulnerability disclosure.
- [Release policy](docs/releases.md) and [changelog](CHANGELOG.md) — alpha
  versioning, artifacts, checksums, and release history.
- [Using Snow](docs/using-snow.md) — TUI/CLI modes, keys, commands, and
  workflows.
- [Agent working guide](AGENTS.md) — repository rules for contributors.
- [Architecture and roadmap](IMPLEMENTATION.md) — design decisions and open
  risks.
