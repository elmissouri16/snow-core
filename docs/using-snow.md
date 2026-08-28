# Using Snow

This guide covers Snow's terminal surfaces: the interactive TUI, print output,
JSON events, and the command-line controls shared by all modes. For machine
control, see [JSONL RPC](rpc.md). For embedding, see the [Go SDK](sdk.md).

> **Note:** Snow is alpha software. The generated command reference from
> `snow --help` is authoritative for your build, and behavior described here is
> verified against source.

## On this page

- [Runtime modes](#runtime-modes)
- [Common flags](#common-flags)
- [Authentication commands](#authentication-commands)
- [TUI layout](#tui-layout)
- [Composer and transcript keys](#composer-and-transcript-keys)
- [Steering and follow-ups](#steering-and-follow-ups)
- [Slash commands](#slash-commands)
- [Composer completions](#composer-completions)
- [Plan Mode and goals](#plan-mode-and-goals)
- [Sessions, branches, and compaction](#sessions-branches-and-compaction)
- [Model-requested input](#model-requested-input)
- [Print and JSON behavior](#print-and-json-behavior)
- [Agent Skills activation](#agent-skills-activation)
- [Management commands](#management-commands)
- [Related documents](#related-documents)

## Runtime modes

| Mode | Invocation | Behavior |
|---|---|---|
| TUI | `snow` | Full-screen interactive terminal with transcript, composer, pickers, permissions, sessions, and settings |
| Resume | `snow resume [session-path]` | Opens a current-project session picker, or resumes an explicit SQLite database |
| Session fork | `snow fork SESSION [flags]` | Materializes an independent SQLite child session |
| Worktree fork | `snow fork-worktree SESSION [flags]` | Creates a clean Git worktree plus an independent child session |
| Print | `snow -p "prompt"` | Streams root text and concise lifecycle/tool status to stdout/stderr |
| JSON | `snow --mode json -p "prompt"` | Emits one normalized `AgentEvent` JSON object per line |
| RPC | `snow --mode rpc` | Long-lived Snow-specific JSONL request/response/event protocol over stdio |

`--mode print` can be used explicitly. Explicit print and JSON modes require a
nonblank `-p`; Snow validates that before constructing sessions or extensions.
Supplying `-p` selects print behavior unless `--mode json` or `--mode rpc` is
set. RPC keeps stdin open for asynchronous commands; it ignores `-p` and is not
a one-shot `echo | snow` protocol for prompts. Unknown permission modes are
startup errors rather than silently falling back.

## Common flags

| Flag | Purpose |
|---|---|
| `-p, --prompt TEXT` | Run a prompt outside the TUI |
| `--mode print|json|rpc` | Select a non-interactive output/control mode |
| `--provider ID` | Select a built-in provider or a named OpenAI-compatible profile |
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

Run `snow --help` or `snow <command> --help` for the generated command
reference. Configuration precedence and persistent equivalents are documented
in [Configuration](configuration.md).

## Authentication commands

```sh
snow login opencode-go
snow login opencode-zen       # optional; no login is needed for anonymous use
snow login openai-compatible
snow login openai-compatible --name x-provider --base-url https://gateway.example/v1
snow login chatgpt
snow login chatgpt --device-code
snow login chatgpt --no-open
snow logout opencode-go
snow logout opencode-zen      # removes Snow's stored key
snow logout openai-compatible
snow logout chatgpt
snow auth check chatgpt
```

The no-argument TUI `/login` and `/logout` commands open centered provider
cards. Login keeps every subsequent step in that fixed-frame card, including
OpenAI-compatible profile/endpoint fields and masked key capture, ChatGPT
account/method choice and OAuth progress, and compatible model discovery.
Validation appears in-place. Within a nested login, Esc returns to the previous
field or selection card and restores non-secret field values; the provider card
is the root, where Esc cancels the flow. Backing out of masked key capture always
discards the typed key, and canceling OAuth returns to the ChatGPT method list.
Single-line auth fields flatten pasted layout controls and ignore delayed
clipboard results after a step transition.
Required device codes and errors stay visible on short cards, compatible
endpoint paths are not echoed into the transcript, and a pending logout blocks
another authentication action until its credential deletion finishes.
Required-auth providers such as `opencode-go` and `chatgpt` do not appear in
model inventories until their credential resolves; logout removes their models
again. `opencode-zen` works without login and omits the authorization header
when no key resolves. An optional key can come
from `OPENCODE_API_KEY`, `--api-key`, or
the masked login flow. Logout removes only Snow's stored key; an explicit flag
or environment fallback remains active until cleared. Zen exposes only the
maintained promotional free catalog and never switches to a paid model.
Anonymous quotas and model availability can change; HTTP 429 responses are
retried before output after 2, 5, and 15 seconds, then surfaced as a usage-limit
error. The model picker shows the documented privacy/training notice for the
highlighted Zen model. Zen model metadata includes context and output limits,
so the footer shows a concrete context budget. Reasoning capability and effort
choices are loaded dynamically by the Zen provider from explicit per-model
`reasoning_options[type=effort].values` in OpenCode's public models.dev catalog;
a reasoning boolean or toggle alone adds no picker values. Snow sends no Zen
credential to that host and does not pin model-specific effort lists. A
provider response that ends without text or a
tool call is shown as an error rather than a silent blank turn.

Selecting `openai-compatible` in the TUI first asks for a profile name, then an
endpoint and optional masked API key. A blank name updates the legacy
`openai-compatible` profile; a name such as `x-provider` creates a separate
provider selectable through `/model`, `/login x-provider`, and
`--provider x-provider`. Enter accepts an API root or full `/responses` or
`/chat/completions` URL; Snow prefers Responses and falls back to Chat
Completions when that endpoint is unavailable. An empty key step preserves an
existing stored key or remains keyless. The legacy profile may also use
`OPENAI_API_KEY`; named profiles deliberately do not share that fallback.

The top-level `snow login openai-compatible` remains key-only for the legacy
profile. Add `--name PROFILE --base-url URL` to create or update a named
profile; subsequent `snow login PROFILE`, `snow logout PROFILE`, and
`snow auth check PROFILE` address its separate credential. An optional default
model can still be configured through config, CLI flags, or the SDK. For a
one-shot keyless local gateway:

```sh
snow --provider openai-compatible --base-url http://127.0.0.1:8080/v1 \
  --model local-model -p "summarize this project"
```

See [ChatGPT authentication](chatgpt-auth.md) for browser callbacks, device
login, local-account discovery, refresh, and model-cache behavior.

## TUI layout

Snow follows Bubble Tea's supported full-window pager/chat pattern:

1. The program uses the alternate screen and composes a sticky provider/model
   header, a Bubbles transcript viewport, overlays/run status, composer, and
   footer in one renderer-owned frame.
2. Finalized Markdown, reasoning, tools, plans, goals, and subagent rows remain
   in the viewport. Scrolling never enters terminal scrollback, so it cannot
   expose stale headers, separators, or prior composer frames.
3. The sticky header shows the current provider/model, collaboration mode,
   reasoning effort, working directory, and status. In app mouse mode the
   accented `provider/model ▾` and `thinking:<effort>` segments open their
   pickers, while `mode:<mode>` toggles Default/Plan; F6/native mode restores
   non-clickable dim labels, with `Alt+M`, `/model`, and
   Settings as fallbacks. `/help`, `/settings`, `/login`, and `/logout` use the same
   centered, frame-preserving card treatment for their interactive flows. Help
   keeps the complete command and active-keybinding reference in a scrollable
   fixed frame. Settings keeps its geometry while the selected-row window, save
   status, and errors update in place; use Up/Down to choose a row, Left/Right
   to change its value, and Enter to open nested model selection. The run-status
   row shows activity and queued-input count. Provider waits use a
   pulsing-points thinking animation distinct from the rotating working
   indicator in the run-status row. The
   footer shows permission mode, mode/goal state, context usage, and the share
   of the latest request's input tokens served from the prompt cache as
   `cached:<n>%`; inline mode may compact provider/model/effort into that footer.
   This is token coverage for one request, not a request-level cache-hit
   frequency, turn aggregate, session average, or cost-savings percentage.

Bash activity uses the sticky run-status row while executing, then adds one
compact `✓ <command> · <duration>` transcript summary followed by any command
output. Long or multiline commands are reduced to one truncated display row;
routine start and finished progress events do not consume separate rows.

When a task needs them, Snow's capability-aware routing exposes the complete
managed-process lifecycle bundle and adds its detailed guidance. Ordinary
editing and search requests do not carry these five schemas. Development
servers, preview servers, file watchers, background workers, and similar
long-running commands use this family instead of Bash or shell backgrounding
(`&`, `nohup`, or `disown`). It uses stable names and checks the active process list to avoid
starting duplicates. A stable startup log marker is sufficient readiness
evidence, so Snow prefers RE2 log readiness and does not reconfirm that marker
with an HTTP request or TCP connection. Network probes are reserved for an
explicit service/network-health request or a process without a reliable log
marker. Otherwise Snow verifies startup through status and logs without
guessing a port, URL, or pattern. Snow does not claim that a server is ready
without evidence:

- `process_start` launches a non-interactive POSIX command in the project and
  returns an opaque process ID. It can optionally wait up to 120 seconds for an
  RE2 log pattern or, when network evidence is actually required, a loopback TCP
  port or loopback HTTP(S) response.
- `process_status` reads cached running or terminal state.
- `process_logs` reads combined stdout/stderr with a non-destructive byte cursor,
  bounded output, rollover accounting, and an optional wait up to 30 seconds.
- `process_stop` terminates and reaps the process group with bounded graceful
  escalation.
- `process_list` returns a secret-safe active-session inventory without command
  strings, environment values, or OS PIDs.

Selecting any member exposes all five lifecycle schemas. After the first
process record is created, the bundle remains available on later turns,
including for exited-process logs, until session rebinding clears the runtime
inventory.

These processes continue while later agent turns edit files or run checks. They
share all branches in the active session and stop on explicit request, session
switch, or normal Snow shutdown. A session switch stops and reaps every managed
process and clears its runtime inventory before binding the new session. If that
bounded cleanup fails, the switch fails rather than orphaning old-session work.
Handles do not survive a restart, independent fork, crash, or `SIGKILL`, and
intentionally daemonized children may escape cleanup. Standard input is `/dev/null`; interactive process
control and live pushed logs are not supported.

The full-screen viewport keeps a bounded recent render cache (up to 2,000
entries or 4 MiB of rendered rows) so long-running streams cannot grow terminal
memory without limit. An omission marker replaces older rendered rows; durable
session history remains append-only and is restored from the session database.

`CH` appears only when the provider explicitly reports cached-token usage; an
explicit zero is shown as `CH0.0%`, while an omitted cache metric remains
hidden. The percentage is `cache_read / input`, because Snow's normalized
`input` is the total prompt count including cached tokens. Context usage follows
the active theme: green below 50%, accent color from 50-69%, warning/yellow
from 70-89%, and red at 90% or above. With the default `tui.mouse: true`,
wheel/trackpad gestures scroll Snow's transcript instead of terminal history;
primary drag highlights and copies through OSC 52. On Apple Terminal, hold Fn
while dragging for instant terminal-native selection. Right-click opens Snow's
compact context menu for the current selection. Choose **Copy selection** with
a mouse click, Enter, or `c`; Esc, an outside click, or the wheel dismisses it.
Copy writes the host clipboard through `pbcopy` on macOS or an available Linux
clipboard utility, with OSC 52 as fallback. The menu never disables application
mouse reporting, so the wheel remains attached to the transcript viewport. F6 is the explicit
app/native mouse-mode toggle. In native mode, wheel gestures may move terminal
scrollback; PageUp/PageDown, Home/End, and Ctrl+Up/Ctrl+Down still scroll Snow.

## Composer and transcript keys

These are built-in defaults; most can be overridden in `keybindings.yaml` as
described in [Configuration](configuration.md).

| Key | Idle behavior | Busy behavior |
|---|---|---|
| `Enter` | Submit prompt/accept picker | Queue steering for the next safe boundary |
| `Alt+Enter` | Insert newline where reported as Meta/Alt | Queue a follow-up after steering and ordinary work settle |
| `Ctrl+J` | Insert a reliable newline | Insert a reliable newline |
| `Ctrl+A` | Select only the ordinary composer's current draft; typing, paste, or deletion replaces it, and Ctrl+C copies it | Same, except Ctrl+C retains its active-turn abort behavior |
| `Ctrl+V` | Paste through the active textarea; replaces a selected composer draft | Same |
| `Up` / `Down` | Browse prompts from the active session branch; Down past the newest restores the current draft | Same; recalled text can be submitted as steering or follow-up input |
| `Shift+Tab` or click `mode:<mode>` (`tui.mouse: true`) | Toggle Default/Plan mode | Queue mode change until `turn_done` |
| `Ctrl+T` | Cycle through the active model's supported thinking efforts | Cycle the effort; the header/footer briefly highlights the new value without adding a transcript entry |
| Click `thinking:<effort>` (`tui.mouse: true`) | Open the centered thinking-effort card | Open the card; the selected effort applies to subsequent provider requests |
| `Alt+M` or click `provider/model ▾` (`tui.mouse: true`) | Open the centered model picker | Report that model changes must wait for the current turn |
| `Alt+A` | Open the subagent fleet inspector | Open the inspector without interrupting the active turn |
| `Alt+P` | Open the managed-process fleet inspector | Open the inspector without interrupting the active turn |
| `Ctrl+C` | Copy a Ctrl+A-selected composer draft; otherwise quit | Abort, clear queued work, restore queued composer text, and defer active goal continuation |
| `Esc` | Return one nested login/model step, otherwise close the modal/picker | Abort active work and defer active goal continuation, or reject the active input modal |
| `Ctrl+D` | Quit when the composer is empty | — |
| Wheel/trackpad (`tui.mouse: true`) | Scroll transcript viewport | Same |
| Primary-button drag (`tui.mouse: true`) | Select and copy transcript text | Same |
| Click `Working` (`tui.mouse: true`) | — | Jump directly to the live transcript bottom |
| Right-click (`tui.mouse: true`) | Open Snow context menu for the current selection; Copy selection preserves viewport mouse mode | Same |
| `F6` | Toggle app mouse handling/native terminal selection and context menu | Same |
| `r` in `/sessions` or `/resume` picker | Rename selected session | Same |
| `PageUp` / `PageDown` | Scroll transcript viewport | Same |
| `Home` / `End` | Jump transcript viewport | Same |
| `Ctrl+Up` / `Ctrl+Down` | Scroll viewport by line | Same |

Prompt history is branch-scoped and is rebuilt from all durable user messages
when a session is opened or resumed. Up starts history browsing from an empty or
single-line draft; multiline drafts keep normal textarea arrow navigation. Once
history browsing starts, Up and Down traverse multiline entries too, and Down
past the newest entry restores the draft that was present before browsing.

Choice pickers accept arrows, `j`/`k`, Tab/Shift+Tab, Home/End, and Enter. The
centered model picker is the exception: ordinary typing immediately filters
provider IDs, model IDs, display names, and descriptions, so `j` and `k` are
search text there. Backspace edits and Ctrl+U clears the query. Arrows,
Tab/Shift+Tab, PageUp/PageDown, Home/End, and Enter navigate and apply; Esc
clears a non-empty query before a second Esc closes the card. Open it by
pressing `Alt+M` or clicking the accented header model in app mouse mode, with
`/model` and the Model row in `/settings` available as additional fallbacks.

The card opens immediately from the active/cached catalog, loads missing
inactive provider catalogs asynchronously, and keeps both the current query and
stable provider/model selection while that refresh completes. Snow constructs
only the active and explicitly configured subagent provider adapters at startup;
other adapters and their HTTP clients materialize on first catalog, login, or
selection use. Consequently, an invalid inactive-provider endpoint is reported
when that provider is first used rather than blocking an unrelated provider's
startup. Selecting a model with controllable reasoning keeps the model's
advertised effort levels, including `off`, in the same centered card before
applying and persisting both. Esc returns from that effort step to the filtered
model list. `off` means Snow omits the provider effort override; an inherently
reasoning model may still use its provider-default reasoning behavior. Blocking
permission and model-requested input overlays
take keyboard and visual precedence over ordinary pickers, including requests
from subagents.

### Large text paste

The ordinary composer collapses a pasted block of at least 4,096 characters or
40 lines into one inline `[Pasted text #N · …]` token. Snow keeps the exact text
behind that token and expands it only when the prompt is submitted, so large
pastes do not remain in the textarea's per-keystroke render path. Prompt
history, queued steering/follow-ups, and rejected or aborted submissions retain
the full text. When the draft contains only paste tokens, Backspace or Esc
removes the newest one. Smaller snippets remain directly editable.

### Image paste

In the ordinary agent composer, Ctrl+V probes the system clipboard for PNG,
JPEG, GIF, or WebP image data before falling back to text paste. Each
attachment appears at the paste cursor as an inline `[Image #N]` token; the
token is removed from model-visible text when Enter sends the corresponding
image block to a vision-capable model. Up to eight images are accepted, each at
most 20 MiB and 40 MiB in aggregate. When no ordinary text remains, Backspace
(or Esc) removes the last attachment and token. Images cannot be queued as
steering/follow-up input while another turn runs. Apple Terminal intercepts
Cmd+V as terminal text paste, so use Ctrl+V for image capture. Linux image
paste requires `wl-paste` or `xclip`; remote SSH sessions read the remote host
clipboard, not the local desktop clipboard.

## Steering and follow-ups

While the root agent is running, new composer submissions do not cancel or
replace accepted work:

- **Steering** becomes eligible after the current assistant response and its
  complete serial tool batch. Tool calls are never skipped halfway through a
  batch.
- **Follow-ups** become eligible after a natural provider stop and after all
  steering eligible at earlier boundaries.
- Delivery is bounded, one message at a time, with steering priority and FIFO
  ordering inside each class. If an ordinary provider request fails, one already
  accepted queue item is persisted and starts a fresh request instead of being
  silently discarded; repeated failures consume only the finite accepted queue.
  Internal persistence/accounting failures and turn-limit rejection leave the
  closed queue recoverable through `PendingInputs`/`ClearPendingInputs`; the TUI
  automatically restores that text to the composer at `turn_done`.
- Abort clears undelivered queue entries and restores their original compact TUI
  text, including unexpanded `@` mentions. If a goal was active, ordinary
  prompts leave it deferred; use `/goal resume` to restart automatic
  continuation.

SDK and RPC callers get the same behavior through `Steer`/`FollowUp` and
`steer`/`follow_up`.

## Slash commands

Type `/` to open completion. Use Up/Down to move through the complete command
list; the visible window follows the selection when the list is taller than the
available frame. Enter runs commands whose no-argument form is meaningful.
Interactive provider/model/thinking changes are remembered by
absolute working directory, so restarting Snow in project A restores project
A's tuple without changing project B. Global defaults apply only when a project
has no remembered tuple; explicit startup flags override it for that process.
Permission mode is different: each fresh session starts in `ask`, an explicit
`--permission` flag overrides the current launch, and interactive changes are
stored only with the active session for resume.

| Command | Purpose |
|---|---|
| `/help` | Open a centered, scrollable reference for commands and active keybindings |
| `/init` | Inspect the current project and create a tailored `AGENTS.md` contributor guide without overwriting an existing one |
| `/model [id]` | Open the model picker or select a model; provider/model/effort persist for the current project folder |
| `/thinking [level]` | Open the centered effort card or directly choose and persist a model-supported effort for the current project folder |
| `/settings` | Open the centered settings card for model, theme, response controls, session permission, subagents, skills, and keybindings |
| `/keybindings` | Open the interactive global/project shortcut editor; changes save and apply immediately |
| `/permissions [ask|allow|deny]` | Open or change the active session's persisted permission mode |
| `/allow [always]` | Resolve a pending tool request; optional session rule |
| `/deny` | Deny a pending tool request |
| `/login [provider] [profile-name]` | Open login flow/provider picker; compatible profiles can be named |
| `/logout [provider]` | Open credential picker or remove one provider credential |
| `/default` | Switch to Default collaboration mode |
| `/plan [message]` | Switch to Plan Mode and optionally submit a planning prompt |
| `/goal [--budget N] [objective]` | Show or create a persistent branch goal with an optional token budget |
| `/goal edit OBJECTIVE` / `/goal replace OBJECTIVE` | Replace the active objective while preserving usage/budget |
| `/goal pause` / `/goal resume` | Pause or explicitly resume eligible automatic goal work, including an active goal deferred by abort |
| `/goal clear` | Remove the branch goal |
| `/compact` | Summarize older complete turns behind a logical context boundary |
| `/context` | Report the estimated system, tool, message, and tool-result share of model context |
| `/sessions` | Pick a persisted session for the current directory |
| `/resume [path]` | Open the session picker or resume an explicit database |
| `/new` | Create a new persisted session |
| `/fork` | Choose a same-session branch, independent local session, or detached Git worktree fork |
| `/tree` | Inspect and switch named branches; `f`, `r`, `d` fork/rename/delete |
| `/agent` | Open the live subagent fleet inspector; select with ↑/↓ or j/k, scroll detail with wheel/trackpad or PageUp/PageDown, refresh with `r`, close with Esc |
| `/agent PATH` | Open the fleet inspector with one child preselected |
| `/agent concurrency N` | Persist child concurrency for the next launch |
| `/processes [id \| name]` | Open the session's auto-refreshing process fleet—even during an active agent turn; select with ↑/↓ or j/k, inspect combined stdout/stderr with wheel/trackpad or PageUp/PageDown, jump with Home/End, refresh with `r`, and close with Esc |
| `/mcp` | Inspect configured/connected MCP server status |
| `/skills [clear]` | Inspect discovered Agent Skills, or durably clear session-active skills |
| `/trust [allow|deny]` | Show or persist exact-project trust for the next launch |
| `/quit` | Exit Snow |

`/init` runs a normal model-driven agent turn in Default mode. The agent first
checks for `AGENTS.md` in Snow's current working directory, then inspects the
checkout and writes a concise, repository-specific guide only when that target
does not already exist. It never intentionally overwrites an existing guide.
Repository inspection and creation use the normal tools, permission policy,
configured path roots, and symlink protections; `/init` does not bypass a
permission denial. A newly created guide is loaded as project context the next
time Snow starts in its scope.

### Interactive keybindings

Open `/keybindings`, or choose **Keybindings** from `/settings`, to inspect and
edit every supported TUI action. The centered card shows the effective keys and
their source (`default`, `global`, or `project`). Global scope writes
`$SNOW_HOME/keybindings.yaml`; press `S` to edit a trusted project's
`.snow/keybindings.yaml` overrides instead.

Select an action with the arrows and Enter. In the action editor, select an
existing key to replace it, **Replace all** to start a new list, or **Add key**
to append another shortcut. The next key event is captured directly, including
Enter or Esc. Backspace/Delete removes a draft key, `R` restores the inherited
or built-in keys, `Ctrl+S` validates and saves, and Esc discards the draft.
Resetting from the main list removes that scope's override: global actions return
to built-in defaults, while project actions inherit the global/default value.

Snow applies successful changes immediately to the composer, transcript,
pickers, help, and shortcut hints. Invalid names, empty binding lists, and keys
that collide in the same interaction context remain unsaved and are reported in
the popup. Emergency Ctrl+C and modal Esc behavior cannot be removed. Project
scope is unavailable until the project is trusted. The popup does not open
while an agent turn is active.

## Composer completions

Typing `@` in the composer starts asynchronous project-file discovery. Enter or
Tab inserts the selected path without submitting the prompt. Discovery never
follows symlink entries and respects Snow's search policy.

Typing `$` after whitespace at the end of the composer opens enabled Agent
Skills completion. The picker shows each matching name and description; Enter or Tab
inserts `$skill-name ` at the current token without submitting. Exact,
whitespace-delimited references activate installed skills in prompts such as
`use $review for this change`; multiple references can appear in one prompt.
Pasted text with an exact enabled `$skill-name` token also activates it, so wrap
literal examples in backticks or attach punctuation when activation is not
intended.

Project `AGENTS.md` files are loaded nearest-first into bounded context. They
are always treated as instructions, independently from project-extension trust.

## Plan Mode and goals

Default and Plan are collaboration modes, not session types. Mode is persisted
per branch.

- Plan Mode instructs the model not to mutate and emits a structured proposed
  plan. It removes mode-specific incompatible aliases/checklists, but ordinary
  `write`, `edit`, `bash`, plugin, and MCP capabilities remain exposed behind
  their normal permission gates. It is not a sandbox.
- Leaving Plan for Default automatically and durably clears active Agent Skills,
  whether the transition uses Shift+Tab, `/default`, the implementation picker,
  SDK/RPC mode control, or an atomic Default-mode prompt. No manual clear is
  required.
- `update_plan` is a Default-mode implementation checklist and is deliberately
  unavailable in Plan Mode.
- Thread Goals are branch-scoped persisted objectives with optional token
  budgets and private automatic continuation.

Read [Plan Mode](plan-mode.md) and [Persistent Thread Goals](goals.md) for exact
state and lifecycle semantics.

## Sessions, branches, and compaction

Snow creates SQLite sessions by default. From an interactive shell,
`snow resume` opens a picker of resumable sessions for the current project and
starts the selected conversation. `snow resume PATH` opens an explicit existing
SQLite database immediately. Headless modes cannot show a picker, so
`snow resume -p "continue"` resumes the most recently updated indexed session.
The command rejects missing paths and `--no-session` instead of silently
creating an empty or ephemeral conversation.

Inside the TUI, `/new`, `/sessions`, and `/resume` operate on the current
project's session index. Sessions receive a local, provider-free title from the
first user prompt. In the `/sessions` or no-path `/resume` picker, press `r` to
edit the selected title; this works without switching to that session. Press
`d`, then Enter, to permanently delete an inactive selected session together
with its SQLite sidecars, subagent histories, private artifacts, and managed
goal files. Deletion bypasses the system Trash and cannot be undone; switch to
another session before deleting the active one. Snow also refuses deletion while
another Snow process has that database open. Titles are 1-72 runes after
trimming, do not need to be unique, and never
change the stable session ID or database path. `/tree` operates inside the
currently open database.

A named branch fork shares prior append-only entries and diverges from a
selected entry; it does not copy message rows. Branch selection changes
subsequent prompts, messages, usage, mode, and goal state. Delete is restricted
to inactive leaf branches and never deletes shared history.

`/fork` makes the distinction explicit:

- **current session** uses the same-database branch behavior above and
  activates the new branch;
- **independent session here** physically copies the exact
  root-to-selected-entry chain into a new SQLite database, records parent
  session/branch/entry provenance, and switches the TUI only after the child is
  reopenable;
- **Git worktree** requires a clean non-bare Git worktree, creates a new branch
  and non-existing destination with direct `git` arguments, then creates a
  detached child session rooted there. The current TUI stays in the source and
  prints a `snow resume` command for opening the child in a fresh runtime.

The non-interactive independent-fork equivalents are `snow fork` and
`snow fork-worktree`. Each accepts an optional session path (or selects the
newest current-project session), `--from-entry`, `--source-branch`, and
`--name`. Same-database branch management remains on the active-runtime TUI, Go
SDK, and RPC surfaces so a detached CLI process cannot race the database's
active-branch cursor. Session forks also accept `--destination`; worktree forks
accept `--worktree` and `--git-branch`. Explicit failures never silently fall
back to a less isolated fork.

A historical independent fork copies conversation entries and metadata only
through the selected entry. Current collaboration mode is inherited only when
forking the source tip; historical forks start in Default mode because mode and
goal tables do not retain entry-by-entry history. Subagent topology is never
copied. A fork ending inside an unresolved tool batch is rejected instead of
being repaired or silently advanced.

`/context` reports the normalized content used for the latest provider request:
system instructions, exposed tool schemas, internal steering, user and agent
messages, assistant text, tool calls, tool results, images, and replayable
provider continuity state. Raw stored thinking blocks are omitted because
providers do not replay them as input. Before the current runtime has sent a
request on the active branch, the command shows a stored-context preflight;
prompt-time routing, tool-result pruning, automatic compaction, and internal
steering may change the eventual request. Category shares begin with a
provider-neutral UTF-8 bytes/4 estimate for text plus a bounded, dimension-based
vision-patch estimate for images; compressed PNG/JPEG/GIF file size is never
used as an image token count. The shares are then rescaled to the same
provider-calibrated current-context total shown in the footer. When calibration
changes the total, `/context` also shows the raw local estimate as a diagnostic.
It separately reports estimated fixed-context tokens, the model-aware admission
budget, and an over-budget marker. Fixed context includes final system guidance,
active skills, and exposed schemas, but not conversation messages. The provider aggregate remains authoritative; individual category attribution,
especially opaque continuity and images, remains approximate. Latest generation
usage is shown separately because only its persisted content is added to a
following request.

`/compact` replaces an older complete-turn prefix with a structured working-state
checkpoint while retaining complete recent turns. The checkpoint preserves
objectives, constraints, decisions, files and symbols, verification outcomes,
failures, attributed agent updates, retrieval references, and unresolved work.
Oversized plain-text tool results in the prefix are reduced before summary, and
a bounded private transcript of compacted tool text, arguments, and result/image
metadata is linked for `artifact_read`/`artifact_grep`; image payloads and full
history remain in the append-only session. Up to 24 verified retrieval
references survive repeated compaction, and a lifecycle warning reports any
transcript persistence failure instead of advertising a missing artifact. The
bounded local fallback stores each command/tool evidence payload once, uses
cross-references for failures, and prints the artifact retrieval helper once per
checkpoint rather than once per reference.

When invoked during automatic goal work, manual compaction pauses that goal
after the checkpoint; use `/goal resume` to continue. Every admitted turn may
auto-compact at a safe top-of-cycle boundary when either total context reaches
80% by default or safely compactable old tool history reaches 20% of the model
window. Active and minimum-retained recent tool batches remain exact. Tool calls
and results are validated as pairs, and opaque provider state is removed only
with its complete old turn.

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

See [Model-requested user input](user-input.md) for the schema and SDK/RPC
reply contracts.

## Print and JSON behavior

Print mode renders root assistant text, plan text, selected tool/subagent
status, errors, and optional usage. Child token streams are not mixed into root
text.

JSON mode writes the same `protocol.AgentEvent` objects used by the SDK and
RPC, one per line. It is an observation stream only; it cannot answer
`ask_user`, permission prompts, steering, or follow-ups. Use RPC for
bidirectional control.

> **Warning:** Headless `ask` has no interactive permission asker and fails
> closed. Prefer an explicit `--permission deny` for inspection or
> `--permission allow` only in a trusted environment whose tool authority is
> intentional.

## Agent Skills activation

A matching skill may be activated by the model through `activate_skill`. To
activate one explicitly on any interactive, print, RPC, or SDK prompt path,
begin the text with its discovered name as `$skill-name`; Snow loads it before
the provider request. Multiple leading skill directives are allowed, and the
same syntax works in queued steering and follow-ups. Active skills persist
across turns independently of Default or Plan mode. When you explicitly leave a
skill workflow or request a conflicting handoff, the model can call
`deactivate_skill` for that skill before continuing; `name: "*"` is reserved for
an explicit request to clear all active skills. `/skills clear` provides the
same all-skills recovery directly in the TUI.

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

snow plugin list [--all] [--json]
snow plugin get ID
snow plugin add MANIFEST_OR_EXECUTABLE [--project] [--replace] [--enable]
snow plugin enable ID [--project]
snow plugin disable ID [--project]
snow plugin remove ID [--project]
snow plugin check MANIFEST_OR_EXECUTABLE [--json]
```

## Related documents

- [Configuration](configuration.md)
- [JSONL RPC](rpc.md)
- [Go SDK](sdk.md)
- [Sessions](sessions.md)
