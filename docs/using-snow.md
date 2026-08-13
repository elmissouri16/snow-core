# Using Snow

This guide covers Snow's terminal surfaces: interactive TUI, print output, JSON
events, and the command-line controls shared by all modes. For machine control,
see [JSONL RPC](rpc.md). For embedding, see the [Go SDK](sdk.md).

## Runtime modes

| Mode | Invocation | Behavior |
|---|---|---|
| TUI | `snow` | Full-screen interactive terminal with transcript, composer, pickers, permissions, sessions, and settings |
| Resume | `snow resume [session-path]` | Opens a current-project session picker, or resumes an explicit SQLite database |
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
| `--provider ID` | Select `opencode-go`, `openai-compatible`, `chatgpt`, or `fake` |
| `--model ID` | Override the provider's configured/default model |
| `--thinking LEVEL` | Set `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, or `ultra` |
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
| `--subagent-provider ID` / `--subagent-model MODEL` | Set automatic provider/model defaults for child agents |
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
snow login openai-compatible
snow login chatgpt
snow login chatgpt --device-code
snow login chatgpt --no-open
snow logout opencode-go
snow logout openai-compatible
snow logout chatgpt
snow auth check chatgpt
```

The no-argument TUI `/login` and `/logout` commands open provider pickers.
Selecting `openai-compatible` in the TUI prompts for its endpoint followed by an
optional masked API key. Enter accepts an API root or full `/responses` or
`/chat/completions` URL; Snow prefers Responses and falls back to Chat
Completions when that endpoint is unavailable. An empty key step preserves an
existing stored/environment fallback (or
remains keyless when no key source exists), and Snow persists the endpoint
before refreshing model discovery.
The top-level `snow login openai-compatible` command remains key-only. An
optional default model can still be configured through config, CLI flags, or the
SDK. For a one-shot keyless local gateway:

```sh
snow --provider openai-compatible --base-url http://127.0.0.1:8080/v1 \
  --model local-model -p "summarize this project"
```

See
[ChatGPT authentication](chatgpt-auth.md) for browser callbacks, device login,
local-account discovery, refresh, and model-cache behavior.

## TUI layout

Snow follows Bubble Tea's supported full-window pager/chat pattern:

1. The program uses the alternate screen and composes a sticky provider/model
   header, a Bubbles transcript viewport, overlays/run status, composer, and
   footer in one renderer-owned frame.
2. Finalized Markdown, reasoning, tools, plans, goals, and subagent rows remain in
   the viewport. Scrolling never enters terminal scrollback, so it cannot expose
   stale headers, separators, or prior composer frames.
3. The footer shows permission mode, context usage, activity, pending queue
   state, and—when width permits—the current provider/model, collaboration mode,
   reasoning effort, and the latest request's prompt-cache hit rate as `CH<n>%`.

`CH` appears only when the provider explicitly reports cached-token usage; an
explicit zero is shown as `CH0.0%`, while an omitted cache metric remains hidden.
The percentage is `cache_read / input`, because Snow's normalized `input` is the
total prompt count including cached tokens. Context usage follows the active
theme: green below 50%, accent color from
50–69%, warning/yellow from 70–89%, and red at 90% or above. With the default
`tui.mouse: true`, wheel/trackpad gestures scroll Snow's transcript instead of
terminal history; primary drag highlights and copies through OSC 52. On Apple
Terminal, hold Fn while dragging for instant terminal-native selection. A
right-click received by Snow disables mouse reporting so the terminal owns
native selection and its context menu; because terminal protocols cannot replay
the consumed press, repeat the right-click when the menu does not open on
release. F6 toggles app/native mouse mode. In native mode, wheel gestures may
move terminal scrollback; PageUp/PageDown, Home/End, and Ctrl+Up/Ctrl+Down still
scroll Snow.

In the ordinary agent composer, **Ctrl+V** probes the system clipboard for PNG, JPEG, GIF, or WebP image data before falling back to text paste. Attached images appear above the draft and are sent as image blocks when Enter submits to a vision-capable model. Up to eight images are accepted, each at most 20 MiB (40 MiB aggregate). With an empty text draft, Backspace (or Esc) removes the last attachment. Images cannot be queued as steering/follow-up input while another turn runs. Apple Terminal intercepts Cmd+V as terminal text paste, so use Ctrl+V for image capture. Linux image paste requires `wl-paste` or `xclip`; remote SSH sessions read the remote host clipboard, not the local desktop clipboard.

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
| `Ctrl+C` | Quit | Abort, clear queued work, restore queued composer text, and defer active goal continuation |
| `Esc` | Close modal/picker | Abort active work and defer active goal continuation, or reject the active input modal |
| `Ctrl+D` | Quit when the composer is empty | — |
| Wheel/trackpad (`tui.mouse: true`) | Scroll transcript viewport | Same |
| Primary-button drag (`tui.mouse: true`) | Select and copy transcript text | Same |
| Right-click (`tui.mouse: true`) | Switch to native mouse mode; repeat click if needed for terminal menu | Same |
| `F6` | Toggle app mouse handling/native terminal selection and context menu | Same |
| `r` in `/sessions` or `/resume` picker | Rename selected session | Same |
| `PageUp` / `PageDown` | Scroll transcript viewport | Same |
| `Home` / `End` | Jump transcript viewport | Same |
| `Ctrl+Up` / `Ctrl+Down` | Scroll viewport by line | Same |

Choice pickers accept arrows, `j`/`k`, Tab/Shift+Tab, Home/End, and Enter. The
model picker also accepts `/` to search provider IDs, model IDs, display names,
and descriptions. Selecting a reasoning-capable model opens a second picker
with that model's advertised effort levels, including `off`, before applying
and persisting both. Blocking permission and model-requested input overlays take
keyboard and visual precedence over ordinary pickers, including requests from
subagents.

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
  text, including unexpanded `@` mentions. If a goal was active, ordinary prompts
  leave it deferred; use `/goal resume` to restart automatic continuation.

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
| `/goal pause` / `/goal resume` | Pause or explicitly resume eligible automatic goal work, including an active goal deferred by abort |
| `/goal clear` | Remove the branch goal |
| `/compact` | Summarize older complete turns behind a logical context boundary |
| `/sessions` | Pick a persisted session for the current directory |
| `/resume [path]` | Open the session picker or resume an explicit database |
| `/new` | Create a new persisted session |
| `/tree` | Inspect and switch named branches; `f`, `r`, `d` fork/rename/delete |
| `/agent` | Open the live subagent fleet inspector; select with ↑/↓ or j/k, scroll detail with PageUp/PageDown, refresh with `r`, close with Esc |
| `/agent PATH` | Open the fleet inspector with one child preselected |
| `/agent concurrency N` | Persist child concurrency for the next launch |
| `/mcp` | Inspect configured/connected MCP server status |
| `/skills` | Inspect discovered Agent Skills |
| `/trust [allow|deny]` | Show or persist exact-project trust for the next launch |
| `/quit` | Exit Snow |

## Composer completions

Typing `@` in the composer starts asynchronous project-file discovery. Enter or
Tab inserts the selected path without submitting the prompt. Discovery never
follows symlink entries and respects Snow's search policy.

Typing `$` at the beginning of the composer opens enabled Agent Skills
completion. The picker shows each matching name and description; Enter or Tab
inserts `$skill-name ` without submitting. It also completes another leading
skill after an existing directive, such as `$review $docs `. Non-leading tokens
in ordinary or pasted prose do not open the picker or activate a skill.

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

Snow creates SQLite sessions by default. From an interactive shell, `snow
resume` opens a picker of resumable sessions for the current project and starts
the selected conversation. `snow resume PATH` opens an explicit existing SQLite
database immediately. Headless modes cannot show a picker, so `snow resume -p
"continue"` resumes the most recently updated indexed session. The command
rejects missing paths and `--no-session` instead of silently creating an empty
or ephemeral conversation.

Inside the TUI, `/new`, `/sessions`, and `/resume` operate on the current
project's session index. Sessions receive a local, provider-free title from the
first user prompt. In the `/sessions` or no-path `/resume` picker, press `r` to
edit the selected title; this works without switching to that session. Titles
are 1–72 runes after trimming, do not need to be unique, and never change the
stable session ID or database path. `/tree` operates inside the currently open
database.

A named fork shares prior append-only entries and diverges from a selected
entry; it does not copy message rows. Branch selection changes subsequent
prompts, messages, usage, mode, and goal state. Delete is restricted to inactive
leaf branches and never deletes shared history.

`/compact` summarizes the projected context while retaining complete recent
turns. Oversized plain-text tool results in the older summarization prefix are
first reduced to a bounded head and tail; exact session history remains
unchanged. When invoked during automatic goal work, manual compaction pauses that
goal after the summary; use `/goal resume` to continue. Active goals also compact automatically between complete continuation
turns at the configured context threshold (90% by default; `0` disables it).
Ordinary prompts do not auto-compact. The full append-only history remains
available.

Snow also detects identical consecutive tool calls during one admitted run and
adds advisory reminders after the third, fifth, and eighth repetition. It does
not block legitimate repeated calls. On session resume, unmatched calls in the
final interrupted tool batch receive synthetic error results; potentially
side-effecting calls are reported as having an unknown outcome and are never
automatically retried. See [Sessions](sessions.md).

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

## Agent Skills activation

A matching skill may be activated by the model through `activate_skill`. To
activate one explicitly on any interactive, print, RPC, or SDK prompt path,
begin the text with its discovered name as `$skill-name`; Snow loads it before
the provider request. Multiple leading skill directives are allowed, and the
same syntax works in queued steering and follow-ups.

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
