# snow-core

**snow** is a minimal, modular coding-agent harness written in Go — a fast
terminal client and embeddable library in one binary, inspired by pi,
OpenCode, and Codex.

> **Status:** pre-alpha (Phase 1–4 of the [IMPLEMENTATION.md](./IMPLEMENTATION.md)
> roadmap). Core loop, sessions, tools, OpenCode Go adapter, TUI, print/JSON/RPC
> modes, and the SDK are functional and tested.

## Highlights

- **Small core** — agent loop, sessions, tools, providers, permissions. No Electron or external database server.
- **Streaming-first** — tokens, tool progress, bounded tool previews, and lifecycle events flow to the UI/SDK without buffering full turns.
- **Modular** — `Tool`, `Provider`, `permission.Service`, `session.Store`, and
  capability-oriented Go plugin interfaces; optional JSON-RPC v2 plugins run as
  explicit argv-based child processes.
- **MCP 2026-07-28** — official Go SDK v1.7.0 client with modern stateless
  Streamable HTTP, stdio, legacy negotiation, tools, resources, prompts, and
  live tool-catalog refresh.
- **Agent Skills** — open `SKILL.md` format, trust-gated project/user discovery,
  metadata-only startup catalogs, on-demand activation, and confined resources.
- **Three surfaces, one loop** — interactive TUI, print/JSON/RPC CLI modes, and a
  pure-Go SDK (`pkg/snowsdk`) with no TUI dependency.
- **Sessions** — pure-Go SQLite storage with indexed tree branching (`id`/`parentId`), fork/resume.
- **Built-in tools** — bounded `read`/`write`/`edit`/`bash`, pure-Go `grep` and `glob`, direct `ask_user` interaction, plus deferred public-web `webfetch` with Surf Chrome 150 impersonation and HTML-to-Markdown conversion; interactive file edits and overwrites show compact red/green diffs.
- **Progressive tool discovery** — existing schemas remain direct; MCP and
  plugin tools can opt into an in-process Bleve BM25 router that exposes only
  the five most relevant schemas and provides `search_tools` recovery.
- **Persistent Thread Goals** — saved branch objectives, budgets, private automatic continuation, usage accounting, and `/goal`/SDK/RPC controls. See [docs/goals.md](docs/goals.md).
- **Plan Mode** — branch-persisted Codex-style non-mutating planning,
  blocking `request_user_input`, chunk-safe structured proposed-plan streaming,
  and an atomic Plan-to-implementation TUI handoff.
- **Opt-in Codex-style subagents** — independent child agent loops, canonical
  task paths, safe attributed mailboxes, six V2 control tools, SDK/RPC/TUI
  observation, bounded concurrency, and optional independent SQLite history.

## Install & run

Requires Go 1.27. The module currently declares Go 1.27rc2 because that is the
available 1.27 toolchain required by the latest Surf release.

```bash
go build ./cmd/snow
# Install/update the development build at ~/.local/bin/snow
./scripts/install-local.sh
```

`install-local.sh` builds a stripped binary and atomically replaces the existing
installation. Override the destination with `SNOW_INSTALL_DIR=/another/bin`.
After installation, `snow` is available from any directory on a shell whose
`PATH` contains `~/.local/bin`. Snow treats the directory where it is launched
as the active project:

```bash
cd ~/Coding/my-project
snow
```

```bash
# Interactive TUI
snow

# Print mode
snow -p "summarize this repo"

# JSONL event stream
snow --mode json -p "list the files"
snow --subagents --subagent-max-concurrency 10  # run up to 10 children at once

# RPC mode (JSONL over stdin/stdout)
echo '{"id":"1","type":"prompt","message":"hello"}' | snow --mode rpc
```

## Providers & auth

| Provider | Auth | Notes |
|----------|------|-------|
| `opencode-go` | `OPENCODE_API_KEY` env, `~/.snow/auth.json` (`opencode-go`), or `--api-key` | OpenAI-compatible streaming adapter with live startup model discovery and capability metadata |
| `chatgpt` | ChatGPT/Codex OAuth in `~/.snow/auth.json` (`chatgpt`) | Browser PKCE/device login, automatic refresh, authenticated cached catalog, and Responses streaming |
| `fake` | none | Deterministic scripted provider for tests/demos |

```bash
export OPENCODE_API_KEY=oc-...
snow -p "hello"
```

Credentials resolution order: explicit flag/SDK option → `~/.snow/auth.json` →
environment. The auth file is created with `0600` permissions.

Run `snow login chatgpt` for browser PKCE login, or
`snow login chatgpt --device-code` on a headless machine. `--no-open` prints the
browser URL without launching it. In the TUI, `/login` → `chatgpt` offers browser
login, device-code login, and import from an existing Codex, Pi, or OpenCode
credential. Access tokens refresh automatically and rotated refresh tokens are
saved atomically. `/model` opens the cached model catalog for the active provider
(OpenCode Go or ChatGPT/Codex); ChatGPT catalogs are fetched per account from the
Codex backend, cached for 15 minutes with ETags, and fall back safely offline. OpenCode
Go availability from `GET /models` is enriched with the same public models.dev
capability, limit, pricing, and reasoning-effort metadata used by OpenCode.
Direct gateway fields remain authoritative. `/thinking`
opens only the reasoning levels advertised by the active model. The normalized
levels are `off`, `minimal`, `low`, `medium`, and `high`; unsupported explicit
selections are rejected rather than silently downgraded. The equivalent CLI
flag is `--thinking off|minimal|low|medium|high`. Tab completes slash commands,
while Enter runs them. `/settings` opens one persistent panel for model, theme, thinking effort,
reasoning summary, text verbosity, permission mode, subagent enablement, and
Agent Skills enablement. Built-in themes are `default` (adaptive), `dark`,
`light`, and `high-contrast`. Changes
save immediately to `~/.snow/config.json`; reasoning summary
(`off|auto|concise|detailed`) and text verbosity (`low|medium|high`) are enabled
for ChatGPT/Codex and shown as unavailable for other providers. `/permissions`
remains a focused shortcut to the same persisted permission setting. The
subagent and Agent Skills toggles are persisted and take effect on the next
Snow launch.
Reasoning summaries stream into a muted Markdown-styled `think:` block and stay
visible in the transcript; an animated placeholder remains visible when the
provider has not emitted a summary delta yet. The full-screen frame keeps only
the transcript scrollable, while the composer grows from three to six rows as
needed. By default Snow leaves mouse reporting disabled, so ordinary drag
selection and the terminal's copy shortcut work on generated content.
PageUp/PageDown, Home/End, and `Ctrl+Up`/`Ctrl+Down` scroll the transcript.
Pickers accept arrows or `j`/`k`, plus Tab/Shift+Tab and Home/End. Set
`"tui": {"mouse": true}` in `~/.snow/config.json` to opt into mouse/trackpad
scrolling (terminal selection may then require the terminal's mouse-override
modifier). Long streams freeze an off-tail snapshot until it reaches the bottom
again. `Ctrl+V` reads the clipboard into the active textarea, while platform
terminal paste shortcuts such as `Cmd+V` or `Ctrl+Shift+V` arrive as safe
bracketed paste. `Ctrl+J` reliably inserts a newline; `Option+Return` also works
when the macOS terminal reports Option as Meta/Alt. Plain Enter submits. `Ctrl+C`
continues to abort while busy and quit while idle, so use the terminal's copy
shortcut for selected text. While a prompt runs, a live row shows its elapsed
time and `Esc` cancels the run.
`Shift+Tab` toggles Default/Plan mode at the top-level composer; during an active
turn the toggle is queued until `turn_done`. `/plan [message]` enters Plan Mode
and `/default` returns to normal execution. Proposed
plans stream as separate Markdown items, followed by current-context, fresh-
context, or keep-planning choices. See [docs/plan-mode.md](docs/plan-mode.md).
`/goal [objective]` creates a saved branch goal; `/goal pause|resume|edit|clear`
controls its private automatic continuation. Pause/edit/clear stay available
while goal work runs. See [docs/goals.md](docs/goals.md).
Type `@` in
the composer to browse current project files and insert a path reference;
file discovery runs asynchronously so the first `@` never blocks typing.
Enter/Tab accepts a file without submitting the prompt. The composer footer
always shows the active permission mode and current/model context-token usage.
If startup fails before the agent is ready, the frame switches to an explicit
error state and keeps `Ctrl+C`/`Ctrl+D` available to restore the terminal and
quit.
`/agent` shows the current subagent tree with running/queued/finished totals,
capacity, role/model, timing, usage, results/errors, and durability;
`/agent <path-or-id>` shows a bounded tool-aware transcript without turning the
root composer into direct child input. `/agent concurrency N` persists the
maximum simultaneously running children (the root does not consume a slot),
and `/settings` exposes the same restart-applied value. Durable child histories
are enabled by default so `/agent` remains useful after session resume.
`/sessions` opens a compact picker for persisted sessions in the current
directory, `/resume` opens the same picker (or resumes an explicit path), `/new`
creates a persisted session, `/compact` manually summarizes older context with
an animated progress indicator, and `/tree` navigates branches inside the active
session. Check the configured credential without refreshing it or printing secrets:

```bash
snow auth check chatgpt

# Resume a persisted session from the CLI
snow --session ~/.snow/sessions/<cwd>/<session>.db
```

See [docs/chatgpt-auth.md](docs/chatgpt-auth.md) for OAuth commands, compatible
import locations, refresh behavior, cache paths, and the backend compatibility boundary.

## SDK

```go
package main

import (
    "context"
    "fmt"

    "github.com/snow-core/snow/pkg/snowsdk"
    "github.com/snow-core/snow/pkg/protocol"
)

func main() {
    ctx := context.Background()
    s, err := snowsdk.Open(ctx, snowsdk.Options{
        Provider:       "opencode-go",
        NoSession:      true,
        PermissionMode: "deny",
    })
    if err != nil { panic(err) }
    defer s.Close()

    s.Subscribe(func(ev protocol.AgentEvent) {
        if ev.Type == protocol.EvTextDelta {
            fmt.Print(ev.Text)
        }
    })

    if err := s.Prompt(ctx, "List the Go files in this repo."); err != nil {
        panic(err)
    }
}
```

For an SDK session resumed with persisted state, install event subscriptions
first and then call `Session.ReadyGoals()` and/or `Session.ReadySubagents()`.
These explicit surface-ready steps prevent constructor-time event loss;
subagent readiness restores topology but never silently restarts stale work.

Enable subagents with `Options.EnableSubagents`; set
`Options.SubagentMaxConcurrency` and `Options.SubagentMaxAgents` for execution
and identity limits. SDK orchestration methods are
`SpawnSubagent`, `SendSubagentMessage`, `FollowupSubagent`, `WaitSubagents`,
`WaitSubagentsUntilAll`, `InterruptSubagent`, `Subagents`, `Subagent`, and `SubagentUsage`. See
[docs/subagents.md](docs/subagents.md) for contracts and limits.

Usage is reported as normalized `protocol.Usage` events and persisted on
assistant messages. It includes input/output, cache-read/cache-write,
reasoning, totals, and optional catalog-derived cost. SDK callers can retrieve
branch totals with `Session.Usage()`, inspect the current provider catalog with
`Session.Models()`, and change effort at runtime with `Session.SetThinking`.
Response controls are also available through `Options.ReasoningSummary`,
`Options.TextVerbosity`, `Session.SetReasoningSummary`, and
`Session.SetTextVerbosity`.
JSON mode emits the same usage events, and print mode supports `--usage`.

### Model-requested user input

`ask_user` is an always-loaded direct built-in (unless an explicit `Tools`
allowlist excludes it). A call contains one to three questions. Each question
is either free-form or has two to three single-select choices; choice questions
also show an automatic **Other** free-form option. The TUI presents the request
inline and keeps the transcript scrollable. Use Enter to accept, `Ctrl+V` to
paste, `Ctrl+J` for a newline in free-form answers, Tab/Shift+Tab to move between
questions, Esc to decline the tool call, or Ctrl+C to abort the whole turn.

SDK embeddings supply `Options.UserInputHandler`. The callback receives a
`protocol.UserInputRequest` and returns a `protocol.UserInputResponse`; answers
are normalized to question order before the model receives them. Without a
handler, print/JSON and SDK calls fail fast with an unavailable-input tool
result instead of hanging.

RPC also exposes `subagent_ready`, `subagent_spawn`,
`subagent_send_message`, `subagent_followup`, `subagent_wait` (with optional
`until: "activity"|"all"`), `subagent_interrupt`, `subagent_list`, and `subagent_get`; `session_info`
reports bounded capability/limit metadata.

RPC clients resolve the emitted `user_input_request` event with one of these
commands:

```json
{"id":"reply-1","type":"user_input_reply","params":{"request_id":"call-1","answers":[{"id":"format","answer":"JSON"}]}}
{"id":"reject-1","type":"user_input_reject","params":{"request_id":"call-1"}}
```

Answers are trimmed, must be non-empty, and are limited to 8 KiB each. See
[docs/user-input.md](docs/user-input.md) for the complete schema and surface
behavior.

## Plugins

The JSONL RPC control plane also accepts `{"type":"set_thinking","thinking":"low"}`
and `{"type":"set_mode","mode":"plan"}`; prompts may attach a mode
atomically. `session_info` reports effort, supported levels, and mode.
The first extensibility slice supports statically linked Go plugins and explicit
JSON-RPC v2 stdio runtimes. Use `--plugin <manifest-or-executable>` repeatedly,
`--no-plugins`, or `snowsdk.Options.Plugins`/`GoPlugins`. Plugin tools are
namespaced as `plugin_<id>_<tool>` and remain behind the normal permission
service. Project-local plugin declarations are trust-gated; plugins run with
user OS privileges and are not a sandbox. Tool definitions may opt into
per-tool deferred discovery without changing their execution transport. See
[docs/plugins.md](docs/plugins.md) and [docs/tool-routing.md](docs/tool-routing.md).

## MCP servers

Snow uses the official MCP Go SDK v1.7.0 and negotiates the current
`2026-07-28` protocol, including its stateless Streamable HTTP lifecycle, with
legacy fallback for older servers. Configure `mcp_servers` globally in
`~/.snow/config.json`, in trust-gated project `.snow/config.json`, or use
`--mcp <manifest-or-url-or-executable>` repeatedly. `--no-mcp` disables all
servers. `snow mcp`/`snow mcp list` show configured servers without starting
them; `snow mcp check [name]` performs a live negotiation. The management
surface also provides `get`, `add`, `enable`, `disable`, and `remove`, with
`--project` on mutations for current-project configuration.

MCP tools are namespaced as `mcp_<server>_<tool>` and deferred through the
local router by default. Resources and prompts receive namespaced list/read/get
bridges; `tools/list_changed` refreshes the registry and BM25 index live. HTTP
calls remain network-risk permission requests, while stdio calls remain
execution-risk requests. See [docs/mcp.md](docs/mcp.md) for config, SDK usage,
capabilities, authentication, and security boundaries.

## Agent Skills

Snow discovers the open Agent Skills format from user `~/.agents/skills` and
`~/.snow/skills` locations plus trust-gated project `.agents/skills` and
`.snow/skills`. Only names and descriptions enter startup context;
`activate_skill` loads full instructions and `read_skill_resource` loads one
bundled file on demand. Activated instructions survive compaction and resume.

Use `--skill-dir` for an additional trusted directory and `--no-skills` for a
one-run disable. `snow skills list|get|enable|disable` inventories and controls
global or project policy without modifying skill files. SDK callers use
`Options.SkillDirs`/`MCPServers`, `Session.Skills()` for enabled entries, and
`Session.SkillInventory()` for enabled plus disabled entries. The TUI exposes
read-only `/mcp` and `/skills` status pickers. See [docs/skills.md](docs/skills.md).

## Permissions & security

- Runs **as the user**; no in-process sandbox (see
  [IMPLEMENTATION.md §9](./IMPLEMENTATION.md#9-security-model)).
- `--permission ask|allow|deny` gates write/edit/bash, deferred network tools,
  and the separate `delegate` risk used to start/follow up subagents.
  `--tools read,write,edit,bash,grep,glob` optionally restricts the built-in
  tool registry to an explicit allowlist, which is useful for reproducible
  headless runs and capability-matched benchmarks.
  `read`, `grep`, and `glob` are read-only and allowed in ask/deny modes;
  `webfetch` is classified as network access, prompts in ask mode, and is hidden
  in deny mode. Interactive
  permission mode selected through `/settings` or `/permissions` becomes the
  global default and applies to the active session; “allow always” rules remain
  scoped to the active session.
  Headless default is `deny`.
- Project MCP declarations and Agent Skills are trust-gated. Configured stdio
  MCP servers and skill scripts still run with user OS privileges when invoked;
  neither project trust nor the skill format is a sandbox.
- File tools enforce path roots (cwd + explicit allows) with symlink resolution.
- Plan Mode's non-mutation rule is model instruction, not a sandbox; shell,
  plugin, and MCP tools still run with the user's OS privileges.
- Auth secrets are never logged; the auth file is `0600`.
- Prompt injection from repo files, tool output, extensions, and child results is a documented residual risk.
- Subagents share the cwd, OS privileges, processes, and provider usage. They are
  not sandboxed; parallel mutation can conflict. The `default` (`general` alias) and `worker`
  roles can use permission-gated `bash`; `explorer` remains read-only. File
  mutation still requires both `subagents.allow_mutation=true` and the selected
  role's `allow_mutation=true`, and enabling subagents never implies mutation or
  recursion. See [docs/subagents.md](docs/subagents.md) for the explicit worker
  configuration example.

## Development

```bash
go test ./...
go vet ./...
go test -race ./internal/...
```

The default test suite is network-free and includes end-to-end coverage for the
agent's real built-in tools, ordered tool calls and progress events, permission
modes, SQLite resume/continuation, provider failure paths, search bounds/path
safety, optimized read/write behavior, BM25 deferred-tool routing/fallback,
Surf Chrome 150 web fetching with public-address/redirect guards, MCP
`2026-07-28` stdio/stateless-HTTP negotiation and capability bridging, Agent
Skills trust/disclosure/path confinement, and the CLI print/JSON modes against
local HTTP/SSE fixtures.

```bash
# Focused agent and CLI end-to-end suites
go test ./internal/agent ./cmd/snow -count=1
```

See [IMPLEMENTATION.md](./IMPLEMENTATION.md) for the full architecture,
interfaces, phased roadmap (0–4), and provider verification checklist. See
[docs/sessions.md](./docs/sessions.md) for SQLite storage and usage. See
[docs/tui-performance.md](./docs/tui-performance.md) for Bubble Tea integration,
upstream examples, and rendering practices.

## Non-goals (v1)

- No Electron/desktop shell, no notes/tasks/memory product surfaces.
- No full pi/OpenCode provider catalog (only OpenCode Go + fake today).
- No built-in sandbox/container backend.
- No autonomous multi-agent product/workflow engine beyond the bounded,
  root-scoped subagent tree documented in [docs/subagents.md](docs/subagents.md).
