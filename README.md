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

### Requirements

- macOS or Linux.
- Go 1.27 only when building from source; `go.mod` declares `1.27rc3` because
  that is the available toolchain required by the pinned Surf release.
  Prebuilt release archives do not require Go.

### Build or install

From the repository root:

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
`SNOW_INSTALL_DIR=/path/to/bin`. On an exact alpha tag,
`install-local.sh` embeds the tag version; `SNOW_VERSION` can provide an
explicit version for a reviewed local build. Tagged alpha releases also publish
macOS/Linux amd64/arm64 archives and `SHA256SUMS`; see the
[release policy](docs/releases.md). Snow treats the directory where it is
launched as the active project.

### Try it without credentials

Smoke-test the harness with the deterministic `fake` provider, which needs no
API key and no network access:

```sh
./snow --provider fake --no-session -p "hello"
```

### Authenticate

OpenCode Go — export a key or store it with Snow:

```sh
export OPENCODE_API_KEY=oc-...
snow -p "summarize this repository"

# or persist the key in Snow's credential store
snow login opencode-go
```

OpenCode Zen promotional free models — anonymous by default, with an optional
Zen API key:

```sh
snow --provider opencode-zen --model big-pickle \
  -p "summarize this repository"

# Optional: raises account-scoped limits when Zen permits it
export OPENCODE_API_KEY=sk-opencode-...
# or: snow login opencode-zen
```

Zen uses only Snow's maintained non-deprecated free-model catalog and never
silently switches to a paid model. Anonymous quotas and the promotional lineup
are not stable. Verified context/output limits drive the footer and automatic
compaction. At provider catalog refresh, Snow loads reasoning capability and
selectable efforts from OpenCode's current public models.dev record; the Zen
credential is never sent there, and Snow never guesses efforts from model names.
A provider completion with no answer or tool call is surfaced as an error
instead of a silent blank turn. The TUI model picker shows each model's
documented privacy or training notice; review it before sending private code.

OpenAI-compatible endpoint (Responses preferred; Chat Completions fallback; API
key optional):

```sh
snow --provider openai-compatible \
  --base-url https://gateway.example/v1 \
  --model model-id --api-key "$OPENAI_API_KEY" \
  -p "summarize this repository"
```

Inside the TUI, `/login openai-compatible` prompts for a profile name, endpoint,
and optional masked API key, then refreshes `/models`. Use names such as
`x-provider` to keep multiple endpoints and credentials distinct; the name
becomes the provider selector shown in `/login` and `/model` and works as
`--provider x-provider` or `/login x-provider`. A blank name preserves the
legacy `openai-compatible` profile. Create the same profile from the CLI with:

```sh
snow login openai-compatible --name x-provider \
  --base-url https://gateway.example/v1
```

Profile endpoints and type metadata are stored in `config.json`; each profile's
key is stored separately under the same name in `auth.json`. Leaving the TUI key
step blank preserves an existing stored key or stays keyless. `OPENAI_API_KEY`
only falls back for the legacy `openai-compatible` profile, so named profiles do
not silently share one environment credential.

ChatGPT/Codex:

```sh
snow login chatgpt                 # browser PKCE
snow login chatgpt --device-code   # headless/device flow
snow auth check chatgpt            # inspect without refreshing
```

Credentials resolve in this order: explicit `--api-key`/SDK option, Snow's auth
store, then a known environment fallback such as `OPENCODE_API_KEY` or
`OPENAI_API_KEY`. A provider-scoped auth service owns precedence, status, login,
persistence, refresh locking, and logout; the agent consumes a credential-free
provider runtime, so new built-in providers stay out of agent and UI auth logic.
Secrets are stored separately from configuration and are never printed by
inventory commands. See [ChatGPT authentication](docs/chatgpt-auth.md) for
OAuth, refresh, imports, and account-scoped catalogs.

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
| RPC | `snow --mode rpc` | Versioned, long-lived foreign-language/IDE control over JSONL stdio |
| Go SDK | `github.com/elmissouri16/snow-core/pkg/snowsdk` | In-process embedding without Cobra or Bubble Tea |
| Python SDK | [`sdk/python`](sdk/python) | Async typed local client around an external Snow binary |
| JavaScript/TypeScript SDK | [`sdk/javascript`](sdk/javascript) | Zero-dependency ESM client with TypeScript declarations |
| Python plugin SDK | [`sdk/plugin-python`](sdk/plugin-python) | Author private protocol-v2 plugins with `snow_plugin` |
| JavaScript/TypeScript plugin SDK | [`sdk/plugin-javascript`](sdk/plugin-javascript) | Author private protocol-v2 plugins with `@snow-core/plugin` |

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
only efforts advertised by the selected model and rejects unsupported explicit
levels.

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
  inside one long active turn are safe checkpoint boundaries, and oversized
  provider requests receive at most one bounded recovery retry
- Durable structured working-state checkpoints preserve objectives, decisions,
  files, deterministic verification/failure evidence, collaboration updates,
  retrieval references, and pending work while complete old turns—including
  provider continuity—leave only the model-facing projection
- Oversized plain-text tool results spill to private session-scoped artifacts;
  provider context keeps bounded previews, and compacted tool prefixes gain a
  bounded verified text/metadata transcript reference without rewriting
  append-only history
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
- **Plan Mode** is a branch-persisted collaboration instruction that asks the
  model not to mutate and emits structured proposed-plan events plus
  `request_user_input`. It is not a permission or sandbox boundary:
  write, shell, plugin, and MCP capabilities remain behind their normal gates.
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
  Dependency-free JavaScript and Python examples are included; the binary can
  vendor private SDK snapshots offline, and configuration has side-effect-free
  list/get plus add/enable/disable/remove management.
- **Agent Skills:** strict open `SKILL.md` validation with metadata-only startup
  context, TUI autocomplete for leading `$skill-name` or model-driven
  activation, pinned on-demand resource confinement, and trust-aware precedence.
  The binary
  embeds `$plugin-builder`, a supervised workflow and template set for staging,
  validating, reviewing, and explicitly enabling agent-authored plugins.
- **Tool routing:** opt-in, namespace-first Bleve BM25 discovery keeps deferred
  schemas out of ordinary provider requests, retains a global rescue ranking,
  and exposes `search_tools` as a recovery path.
- **Subagents:** optional bounded child agent tree with independent sessions,
  role-scoped tools, attributed mailboxes, concurrency/depth limits, and
  SDK/RPC/TUI observation.

Build or manage external runtimes without hot-loading them:

```sh
# In a Snow prompt, start with: $plugin-builder Build a reusable ...
snow plugin sdk vendor --runtime javascript .snow/generated-plugins/my-plugin --json
snow plugin check examples/plugins/javascript/manifest.json
snow plugin check examples/plugins/python/manifest.json --json
snow plugin add ./my-plugin/manifest.json --project # staged disabled
snow plugin enable my-plugin --project              # next launch; restart required
snow plugin list --all
```

`plugin check` starts the runtime, so it requires the same trust as executing
other generated code. SDK vendoring writes executable source but does not run
it; list/get/add/enable/disable/remove also never start a plugin.

See [MCP](docs/mcp.md), [plugins](docs/plugins.md), the complete
[plugin protocol](docs/plugin-protocol.md), [Agent Skills](docs/skills.md),
[tool routing](docs/tool-routing.md), and [subagents](docs/subagents.md).

Runnable integration projects live under [`examples/`](examples/): a standalone
[Go SDK module](examples/sdk), [Python](examples/rpc/python) and
[JavaScript](examples/rpc/javascript) language-SDK clients, and
JavaScript/Python plugin runtimes. All SDK/RPC examples default to the
credential-free fake provider and are exercised by CI on Linux and macOS. See
the [cross-language SDK guide](docs/language-sdks.md).

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
finishes. See the [RPC protocol reference](docs/rpc.md) and
[cross-language SDK guide](docs/language-sdks.md).

## Security first

Snow is a harness, **not a whole-process sandbox**:

- Snow, `bash`, plugins, stdio MCP servers, and subagents run with the user's
  OS privileges. Snow has no built-in process sandbox; use an external
  container, VM, or OS policy when containment is required.
- Headless SDK/RPC/print callers should normally use `deny`; `ask` has no
  interactive permission reply channel outside the TUI and therefore fails
  closed.
- Project trust permits loading project-local configuration and extensions. It
  does not constrain what an enabled process can do.
- Plan Mode is a collaboration contract, not an OS enforcement boundary.
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

Start at the [documentation index](docs/README.md).

| Task | Guide |
|---|---|
| Learn the TUI and CLI modes | [Using Snow](docs/using-snow.md) |
| Configure paths, providers, tools, themes, and search | [Configuration](docs/configuration.md) |
| Embed Snow in Go | [Go SDK](docs/sdk.md) · [standalone example](examples/sdk) |
| Embed from Python or JavaScript/TypeScript | [Language SDKs](docs/language-sdks.md) · [Python](sdk/python) · [JavaScript](sdk/javascript) |
| Build a raw JSONL client | [RPC](docs/rpc.md) · [schemas](pkg/protocol/schema/rpc/v1) |
| Author JavaScript/Python plugins | [Plugins](docs/plugins.md) · [Protocol v2](docs/plugin-protocol.md) |
| Review operational boundaries or report a vulnerability | [Security model](docs/security.md) · [Reporting policy](SECURITY.md) |
| Prepare or verify a release | [Release policy](docs/releases.md) · [Changelog](CHANGELOG.md) |
| Authenticate ChatGPT/Codex | [ChatGPT auth](docs/chatgpt-auth.md) |
| Resume and branch conversations | [Sessions](docs/sessions.md) |
| Use Plan Mode, goals, or subagents | [Plan Mode](docs/plan-mode.md) · [Goals](docs/goals.md) · [Subagents](docs/subagents.md) |
| Extend Snow | [MCP](docs/mcp.md) · [Plugins](docs/plugins.md) · [Skills](docs/skills.md) |
| Understand architecture and roadmap | [IMPLEMENTATION.md](IMPLEMENTATION.md) |

## Development

[GitHub Actions CI](.github/workflows/ci.yml) runs automatically for `main`
pushes and pull requests and remains manually dispatchable. Linux and macOS run
the network-free suite, binary and SDK/plugin checks, and examples; Linux also
runs the race detector, four release-target cross-builds, and `govulncheck`.
This local list is the common baseline;
the affected-area matrix in [`IMPLEMENTATION.md`](IMPLEMENTATION.md#testing-and-verification)
is the complete maintainer reference:

```sh
gofmt -w <changed-go-files>
go test ./...
go vet ./...
go test -race ./internal/... ./pkg/snowsdk
(cd examples/sdk && go test ./... && go run .)
go build -o ./snow ./cmd/snow
SNOW_TEST_BINARY="$PWD/snow" PYTHONPATH=sdk/python/src python3 -m unittest discover -s sdk/python/tests -v
(cd sdk/javascript && npm test && SNOW_TEST_BINARY="$PWD/../../snow" npm run test:integration && npm run pack:check)
PYTHONPATH=sdk/plugin-python/src python3 -m unittest discover -s sdk/plugin-python/tests -v
(cd sdk/plugin-javascript && npm test && npm run pack:check)
./snow plugin check examples/plugins/python-sdk/manifest.json
./snow plugin check examples/plugins/javascript-sdk/manifest.json
python3 examples/rpc/python/client.py --snow ./snow
node examples/rpc/javascript/client.mjs ./snow
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
- [Security model](docs/security.md) and [reporting policy](SECURITY.md) —
  operational boundaries and private vulnerability disclosure.
- [Release policy](docs/releases.md) and [changelog](CHANGELOG.md) — alpha
  versioning, artifacts, checksums, and release history.
- [Using Snow](docs/using-snow.md) — TUI/CLI modes, keys, commands, and
  workflows.
- [Agent working guide](AGENTS.md) — repository rules for contributors.
- [Architecture and roadmap](IMPLEMENTATION.md) — design decisions and open
  risks.
