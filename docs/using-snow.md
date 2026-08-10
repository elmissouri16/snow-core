# Using Snow

This guide covers Snow's terminal surfaces: interactive TUI, print output, JSON
events, and the command-line controls shared by all modes. For machine control,
see [JSONL RPC](rpc.md). For embedding, see the [Go SDK](sdk.md).

## Runtime modes

| Mode | Invocation | Behavior |
|---|---|---|
| TUI | `snow` | Full-screen interactive terminal with transcript, composer, pickers, permissions, sessions, and settings |
| Print | `snow -p "prompt"` | Streams root text and concise lifecycle/tool status to stdout/stderr |
| JSON | `snow --mode json -p "prompt"` | Emits one normalized `AgentEvent` JSON object per line |
| RPC | `snow --mode rpc` | Long-lived Snow-specific JSONL request/response/event protocol over stdio |

`--mode print` can be used explicitly. Explicit print and JSON modes require a
nonblank `-p`; Snow validates that before constructing sessions or extensions.
Supplying `-p` selects print behavior unless `--mode json` is set. RPC keeps
stdin open for asynchronous commands; it is not a one-shot `echo | snow`
protocol for prompts. Unknown permission modes are startup errors rather than
silently falling back.

## Common flags

| Flag | Purpose |
|---|---|
| `-p, --prompt TEXT` | Run a prompt outside the TUI |
| `--mode print|json|rpc` | Select a non-interactive output/control mode |
| `--provider ID` | Select `opencode-go`, `chatgpt`, or `fake` |
| `--model ID` | Override the provider's configured/default model |
| `--thinking LEVEL` | Set `off`, `minimal`, `low`, `medium`, or `high` |
| `--collaboration-mode MODE` | Start in `default` or `plan` |
| `--permission MODE` | Set `ask`, `allow`, or `deny` |
| `--tools LIST` | Restrict built-ins to a comma-separated allowlist |
| `--session PATH` | Resume an existing SQLite session |
| `--no-session` | Use an ephemeral in-memory conversation |
| `--config PATH` / `--auth PATH` | Override global configuration or credential files |
| `--api-key VALUE` | Supply an explicit provider credential |
| `--base-url URL` | Override the active provider endpoint |
| `--plugin VALUE` | Load an explicit plugin manifest/executable; repeatable |
| `--mcp VALUE` | Add an MCP manifest, URL, or executable; repeatable |
| `--skill-dir PATH` | Add a trusted Agent Skills directory; repeatable |
| `--no-plugins`, `--no-mcp`, `--no-skills` | Disable an extension family for this launch |
| `--subagents` / `--no-subagents` | Override configured subagent enablement |
| `--subagent-max-concurrency N` | Override simultaneously running child agents |
| `--subagent-max-agents N` | Override child identity limit |
| `--subagent-max-depth N` | Override child nesting depth |
| `--usage` | Print normalized usage after a print-mode prompt |

Run `snow --help` or `snow <command> --help` for the generated command reference.
Configuration precedence and persistent equivalents are documented in
[Configuration](configuration.md).

## Authentication commands

```sh
snow login opencode-go
snow login chatgpt
snow login chatgpt --device-code
snow login chatgpt --no-open
snow logout opencode-go
snow logout chatgpt
snow auth check chatgpt
```

The no-argument TUI `/login` and `/logout` commands open provider pickers. See
[ChatGPT authentication](chatgpt-auth.md) for browser callbacks, device login,
local-account discovery, refresh, and model-cache behavior.

## TUI layout

The TUI has three primary regions:

1. A scrollable transcript containing Markdown, reasoning summaries, tools,
   plans, goals, and subagent lifecycle rows.
2. A sticky composer for prompts, commands, mentions, and queued input.
3. A footer showing permission mode, model, context usage, activity, and pending
   queue state.

Only the transcript scrolls. Long streams preserve an off-tail view until the
user returns to the bottom. Mouse reporting is disabled by default so native
terminal drag selection and copy continue to work.

## Composer and transcript keys

These are built-in defaults; most can be overridden in
`keybindings.yaml` as described in [Configuration](configuration.md).

| Key | Idle behavior | Busy behavior |
|---|---|---|
| `Enter` | Submit prompt/accept picker | Queue steering for the next safe boundary |
| `Alt+Enter` | Insert newline where reported as Meta/Alt | Queue a follow-up after steering and ordinary work settle |
| `Ctrl+J` | Insert a reliable newline | Insert a reliable newline |
| `Ctrl+V` | Paste through the active textarea | Paste through the active textarea |
| `Shift+Tab` | Toggle Default/Plan mode | Queue mode change until `turn_done` |
| `Ctrl+C` | Quit | Abort, clear queued work, and restore queued composer text |
| `Esc` | Close modal/picker | Abort active work or reject the active input modal |
| `Ctrl+D` | Quit when the composer is empty | — |
| `PageUp` / `PageDown` | Scroll transcript | Scroll transcript |
| `Home` / `End` | Jump transcript | Jump transcript |
| `Ctrl+Up` / `Ctrl+Down` | Scroll by line | Scroll by line |

Choice pickers accept arrows, `j`/`k`, Tab/Shift+Tab, Home/End, and Enter. The
model picker also accepts `/` to search provider IDs, model IDs, display names,
and descriptions.

### Steering and follow-ups

While the root agent is running, new composer submissions do not cancel or
replace accepted work:

- **Steering** becomes eligible after the current assistant response and its
  complete serial tool batch. Tool calls are never skipped halfway through a
  batch.
- **Follow-ups** become eligible after a natural provider stop and after all
  steering eligible at earlier boundaries.
- Delivery is bounded, one message at a time, with steering priority and FIFO
  ordering inside each class.
- Abort clears undelivered queue entries and restores their original compact TUI
  text, including unexpanded `@` mentions.

SDK and RPC callers get the same behavior through `Steer`/`FollowUp` and
`steer`/`follow_up`.

## Slash commands

Type `/` to open completion. Enter runs commands whose no-argument form is
meaningful.

| Command | Purpose |
|---|---|
| `/help` | Show commands and active keybindings |
| `/model [id]` | Open the model picker or select a model; selection persists |
| `/thinking [level]` | Choose model-supported reasoning effort |
| `/settings` | Configure model, theme, response controls, permission mode, subagents, and skills |
| `/permissions [ask|allow|deny]` | Open or directly change permission mode |
| `/allow [always]` | Resolve a pending tool request; optional session rule |
| `/deny` | Deny a pending tool request |
| `/login [provider]` | Open login flow/provider picker |
| `/logout [provider]` | Open credential picker or remove one provider credential |
| `/default` | Switch to Default collaboration mode |
| `/plan [message]` | Switch to Plan Mode and optionally submit a planning prompt |
| `/goal [--budget N] [objective]` | Show or create a persistent branch goal with an optional token budget |
| `/goal edit OBJECTIVE` | Replace the active objective while preserving usage/budget |
| `/goal pause` / `/goal resume` | Pause or resume eligible automatic goal work |
| `/goal clear` | Remove the branch goal |
| `/compact` | Summarize older complete turns behind a logical context boundary |
| `/sessions` | Pick a persisted session for the current directory |
| `/resume [path]` | Open the session picker or resume an explicit database |
| `/new` | Create a new persisted session |
| `/tree` | Inspect and switch named branches; `f`, `r`, `d` fork/rename/delete |
| `/agent` | Show the subagent tree and aggregate state |
| `/agent PATH` | Inspect one child's bounded tool-aware transcript |
| `/agent concurrency N` | Persist child concurrency for the next launch |
| `/mcp` | Inspect configured/connected MCP server status |
| `/skills` | Inspect discovered Agent Skills |
| `/trust [allow|deny]` | Show or persist exact-project trust for the next launch |
| `/quit` | Exit Snow |

## Files and mentions

Typing `@` in the composer starts asynchronous project-file discovery. Enter or
Tab inserts the selected path without submitting the prompt. Discovery never
follows symlink entries and respects Snow's search policy.

Project `AGENTS.md` files are loaded nearest-first into bounded context. They are
always treated as instructions, independently from project-extension trust.

## Plan Mode and goals

Default and Plan are collaboration modes, not session types. Mode is persisted
per branch.

- Plan Mode instructs the model not to mutate and emits a structured proposed
  plan. It removes mode-specific incompatible aliases/checklists, but ordinary
  `write`, `edit`, `bash`, plugin, and MCP capabilities remain exposed behind
  their normal permission gates. It is not a sandbox.
- `update_plan` is a Default-mode implementation checklist and is deliberately
  unavailable in Plan Mode.
- Thread Goals are branch-scoped persisted objectives with optional token
  budgets and private automatic continuation.

Read [Plan Mode](plan-mode.md) and [Persistent Thread Goals](goals.md) for exact
state and lifecycle semantics.

## Sessions, branches, and compaction

Snow creates SQLite sessions by default. `/new`, `/sessions`, and `/resume`
operate on the current project's session index. `/tree` operates inside the
currently open database.

A named fork shares prior append-only entries and diverges from a selected
entry; it does not copy message rows. Branch selection changes subsequent
prompts, messages, usage, mode, and goal state. Delete is restricted to inactive
leaf branches and never deletes shared history.

`/compact` summarizes the projected context while retaining complete recent
turns. The full append-only history remains available. See [Sessions](sessions.md).

## Model-requested input

When `ask_user` or Plan Mode's compatible `request_user_input` asks a question,
the TUI displays it inline:

- arrows choose options;
- Enter accepts;
- Tab/Shift+Tab changes question;
- `Ctrl+J` inserts free-form newlines;
- Esc rejects the tool call;
- Ctrl+C aborts the complete turn.

See [Model-requested user input](user-input.md) for the schema and SDK/RPC reply
contracts.

## Print and JSON behavior

Print mode renders root assistant text, plan text, selected tool/subagent status,
errors, and optional usage. Child token streams are not mixed into root text.

JSON mode writes the same `protocol.AgentEvent` objects used by the SDK and RPC,
one per line. It is an observation stream only; it cannot answer `ask_user`,
permission prompts, steering, or follow-ups. Use RPC for bidirectional control.

Headless `ask` has no interactive permission asker and fails closed. Prefer an
explicit `--permission deny` for inspection or `--permission allow` only in a
trusted environment whose tool authority is intentional.

## Management commands

Snow includes side-effect-free inventories and explicit mutation commands:

```sh
snow mcp list [--json]
snow mcp get NAME
snow mcp check [NAME]
snow mcp add NAME -- COMMAND [ARG...]
snow mcp enable NAME
snow mcp disable NAME
snow mcp remove NAME

snow skills list [--json]
snow skills get NAME
snow skills enable NAME
snow skills disable NAME

snow plugin check MANIFEST_OR_EXECUTABLE [--json]
```

MCP and skill mutations are global by default; add `--project` to target trusted
project configuration. `plugin check` performs a bounded runtime handshake and
does not mutate configuration. Full details are in [MCP](mcp.md),
[Agent Skills](skills.md), and [Plugins](plugins.md).
