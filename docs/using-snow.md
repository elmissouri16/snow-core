# Using Snow

This guide covers Snow's terminal surfaces: the interactive TUI, print output,
JSON events, and the command-line controls shared by all modes. For machine
control, see [JSONL RPC](rpc.md). For embedding, see the [Go SDK](sdk.md).

> **Note:** Snow is pre-alpha. The generated command reference from
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
- [Persistent smolvm Bash guest](#persistent-smolvm-bash-guest)
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
| `--require-sandbox` | Fail startup unless this canonical project has a smolvm Bash association |
| `--no-sandbox` | Explicitly keep Bash on the host even when an association exists |
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
snow login openai-compatible
snow login openai-compatible --name x-provider --base-url https://gateway.example/v1
snow login chatgpt
snow login chatgpt --device-code
snow login chatgpt --no-open
snow logout opencode-go
snow logout openai-compatible
snow logout chatgpt
snow auth check chatgpt
```

The no-argument TUI `/login` and `/logout` commands open provider pickers.
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
   reasoning effort, working directory, and status. The run-status row shows
   activity and queued-input count. Provider waits use a pulsing-points thinking
   animation distinct from the rotating working indicator in the run-status row.
   The footer shows permission mode, mode/goal
   state, context usage, and the latest request's prompt-cache hit rate as
   `CH<n>%`; inline mode may compact provider/model/effort into that footer.

Bash activity uses the sticky run-status row while executing, then adds one
compact `✓ <command> · <duration>` transcript summary followed by any command
output. Long or multiline commands are reduced to one truncated display row;
routine start and finished progress events do not consume separate rows.

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
| `Ctrl+V` | Paste through the active textarea | Paste through the active textarea |
| `Shift+Tab` | Toggle Default/Plan mode | Queue mode change until `turn_done` |
| `Ctrl+T` | Open the active model's thinking-effort picker | Open the picker; the selected effort applies to subsequent provider requests |
| `Ctrl+C` | Quit | Abort, clear queued work, restore queued composer text, and defer active goal continuation |
| `Esc` | Close modal/picker | Abort active work and defer active goal continuation, or reject the active input modal |
| `Ctrl+D` | Quit when the composer is empty | — |
| Wheel/trackpad (`tui.mouse: true`) | Scroll transcript viewport | Same |
| Primary-button drag (`tui.mouse: true`) | Select and copy transcript text | Same |
| Right-click (`tui.mouse: true`) | Open Snow context menu for the current selection; Copy selection preserves viewport mouse mode | Same |
| `F6` | Toggle app mouse handling/native terminal selection and context menu | Same |
| `r` in `/sessions` or `/resume` picker | Rename selected session | Same |
| `PageUp` / `PageDown` | Scroll transcript viewport | Same |
| `Home` / `End` | Jump transcript viewport | Same |
| `Ctrl+Up` / `Ctrl+Down` | Scroll viewport by line | Same |

Choice pickers accept arrows, `j`/`k`, Tab/Shift+Tab, Home/End, and Enter. The
model picker also accepts `/` to search provider IDs, model IDs, display names,
and descriptions. Selecting a reasoning-capable model opens a second picker
with that model's advertised effort levels, including `off`, before applying
and persisting both. Blocking permission and model-requested input overlays
take keyboard and visual precedence over ordinary pickers, including requests
from subagents.

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

Type `/` to open completion. Enter runs commands whose no-argument form is
meaningful. Interactive provider/model/thinking changes are remembered by
absolute working directory, so restarting Snow in project A restores project
A's tuple without changing project B. Global defaults apply only when a project
has no remembered tuple; explicit startup flags override it for that process.

| Command | Purpose |
|---|---|
| `/help` | Show commands and active keybindings |
| `/model [id]` | Open the model picker or select a model; provider/model/effort persist for the current project folder |
| `/thinking [level]` | Choose and persist a model-supported effort for the current project folder |
| `/settings` | Configure model, theme, response controls, permission mode, subagents, and skills |
| `/permissions [ask|allow|deny]` | Open or directly change permission mode |
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
| `/mcp` | Inspect configured/connected MCP server status |
| `/sandbox [status|init|start|stop|delete confirm]` | Inspect or control the persistent smolvm Bash guest; init accepts `--from`, `--read-only`, and `--network` |
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

Project `AGENTS.md` files are loaded nearest-first into bounded context. They
are always treated as instructions, independently from project-extension trust.

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
The provider aggregate remains authoritative; individual category attribution,
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
transcript persistence failure instead of advertising a missing artifact.

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

snow plugin list [--all] [--json]
snow plugin get ID
snow plugin add MANIFEST_OR_EXECUTABLE [--project] [--replace] [--enable]
snow plugin enable ID [--project]
snow plugin disable ID [--project]
snow plugin remove ID [--project]
snow plugin check MANIFEST_OR_EXECUTABLE [--json]

snow sandbox [status] [--json]
snow sandbox init [IMAGE_OR_PACK] [--profile ubuntu|go|node|python] [--from]
                  [--cpus N] [--memory MiB]
                  [--storage GIB] [--overlay GIB] [--guest-cwd PATH]
                  [--read-only] [--network]
snow sandbox start
snow sandbox stop
snow sandbox delete --force [--forget]
```

## Persistent smolvm Bash guest

For a consolidated profile, persistence, project-scoping, process, and recovery
guide, see [Sandboxed Bash with smolvm](sandbox.md).

`snow sandbox init` is the one-command default: it ensures pinned smolvm 1.8.1,
creates a deterministic digest-pinned Ubuntu 24.04 machine for the exact
canonical current project, starts it, then atomically publishes the
association. Supply another image, configure global `sandbox.default_image`, or
use `--from` with an existing local `.smolmachine` artifact to override Ubuntu.

If the default `smolvm` command is absent, init downloads the version-tagged
official installer script and platform release archive over HTTPS, checks
Snow-pinned SHA-256 values for both, and executes the installer against only
that already verified local archive with `--version 1.8.1 --no-modify-path`.
The upstream installer writes user-local `~/.smolvm`, `~/.local/bin/smolvm`, and
platform smolvm data files. It does not modify shell profiles. A custom
`sandbox.executable` is operator policy: if that path/command is absent, Snow
fails instead of installing or replacing it.

The create operation mounts only the canonical project directory, at
`/workspace` by default. `--read-only` changes that Bash guest mount. Guest
runtime networking is off in the supported smolvm line unless `--network` is
explicit. That flag persists full guest network authority for later Bash calls.
No-argument init downloads the digest-pinned registry image over host HTTPS
into a private temporary Docker-save archive, then gives smolvm that local
archive; no guest bootstrap network is enabled. Use a local pack when bootstrap
itself must be offline. `snow sandbox stop` explicitly persists host routing
while preserving the machine; `snow sandbox start` restores VM routing. Active
backend failures never silently fall back to the host. Bash commands use
foreground `machine exec --stream`, keep the existing Snow output/timeout
bounds, and forward only the global environment name allowlist. A timeout/cancel
first sends SIGINT so smolvm can cancel the guest command, then kills the
launcher process group after a short grace.

Once initialized, an active association enables sandboxed Bash for each newly
assembled CLI/TUI/RPC runtime in that project. While active, corrupt state, a
missing pinned executable, unavailable machinery, and exec errors are returned
as Bash errors, never retried on the host. `stop` is an explicit routing-policy
change: after the VM stops and state commits, it preserves VM storage but sends
future Bash calls to the host; no Bash call auto-starts it. `start` restores VM
routing. `delete --force` deletes the VM and removes the association. If the VM
was removed outside Snow or association cleanup must be repaired,
`delete --force --forget` removes only the operator record and leaves Bash on
the host.

In the TUI use `/sandbox` or `/sandbox status`; `/sandbox init` opens an
interactive setup form for the environment profile, CPUs, memory MiB, storage
GiB, overlay GiB, project mount mode, and guest networking. Built-in choices are
digest-pinned Minimal Ubuntu, Go 1.27rc2, Node.js 22, and Python 3.12 with
uv 0.12.5. The form starts on the configured/custom image with its existing
network choice; a deliberate environment-row change selects a profile. The Go
profile recommends 4 CPUs and 6144 MiB RAM by default so snow-core's heavier
dependencies compile; resource fields and CLI flags can still override those
values. Every built-in profile explicitly enables persistent guest networking
so apt, go, npm, and uv can reach their registries; custom/configured images
retain the separately selected network choice. Choosing `smolvm default` for
either disk explicitly clears a nonzero global disk default. An optional source
still accepts `--from`, `--read-only`, and `--network` as initial form choices.
Use arrows to select/change values, Space to toggle authority choices, Enter to
create, and Esc to cancel. Deletion requires the explicit
`/sandbox delete confirm` spelling. CLI `--profile` is mutually exclusive with
an explicit image/pack source. On the CLI, omitting `--storage`/`--overlay`
uses global defaults while an explicit zero requests smolvm's own default. A
wide header continuously labels the Bash boundary as green `shell:vm` or
warning-yellow `shell:host`.

This boundary applies only to the model-facing Bash tool. Snow itself,
`read`/`write`/`edit`, webfetch/provider traffic, plugins, MCP servers, and
host-side subagent orchestration retain their documented host authority.

MCP, skill, and plugin mutations are global by default; add `--project` to
target project configuration. Plugin add stages declarations disabled unless
`--enable` is explicit, every plugin configuration change requires restart, and
project declarations still require project trust before loading. Plugin
list/get/add/enable/disable/remove never start a process. `plugin check`
performs a bounded runtime handshake and therefore executes the plugin, but does
not mutate configuration. Full details are in [MCP](mcp.md),
[Agent Skills](skills.md), and [Plugins](plugins.md).

## Related documents

- [Configuration](configuration.md)
- [JSONL RPC](rpc.md)
- [Go SDK](sdk.md)
- [Sessions](sessions.md)
- [Sandboxed Bash with smolvm](sandbox.md)
