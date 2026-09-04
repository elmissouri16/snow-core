# Using Snow

This guide covers Snow's terminal surfaces, essential TUI controls, and common
commands. Start with [Getting started](getting-started.md) if Snow is not yet
installed or connected to a provider.

> **Note:** Snow is alpha software. Run `snow --help` for the command reference
> that matches your installed version.

## On this page

- [Choose a runtime mode](#choose-a-runtime-mode)
- [Use common flags](#use-common-flags)
- [Navigate the TUI](#navigate-the-tui)
- [Steer active work](#steer-active-work)
- [Use slash commands](#use-slash-commands)
- [Use composer completions](#use-composer-completions)
- [Choose Plan Mode or a Thread Goal](#choose-plan-mode-or-a-thread-goal)
- [Manage sessions](#manage-sessions)
- [Answer model questions](#answer-model-questions)
- [Use print and JSON output](#use-print-and-json-output)
- [Manage capabilities](#manage-capabilities)
- [Related documents](#related-documents)

## Choose a runtime mode

| Mode | Invocation | Use it for |
|---|---|---|
| TUI | `snow` | Interactive coding, approvals, sessions, and settings |
| Resume | `snow resume [path]` | Continue a saved conversation |
| Print | `snow -p "prompt"` | Human-readable one-shot output |
| JSON | `snow --mode json -p "prompt"` | One normalized event per JSONL line |
| RPC | `snow --mode rpc` | Long-lived control from another process |

Supplying `-p` selects print behavior unless `--mode json` or `--mode rpc` is
set. Print and JSON modes require a nonblank prompt. RPC keeps standard input
open for commands and ignores `-p`.

The complete RPC contract remains available in the repository's
[JSONL RPC reference](https://github.com/elmissouri16/snow-core/blob/main/docs/rpc.md).

## Use common flags

| Flag | Purpose |
|---|---|
| `-p, --prompt TEXT` | Run a prompt outside the TUI |
| `--provider ID` | Select a provider or named compatible profile |
| `--model ID` | Override the configured model |
| `--thinking LEVEL` | Select a supported reasoning effort |
| `--collaboration-mode MODE` | Start in `default` or `plan` |
| `--permission MODE` | Select `ask`, `allow`, or `deny` |
| `--tools LIST` | Restrict built-in tools to a comma-separated list |
| `--session PATH` | Open or create a chosen SQLite session |
| `--no-session` | Keep conversation history in memory |
| `--config PATH`, `--auth PATH` | Override global config or auth paths |
| `--api-key VALUE`, `--base-url URL` | Override provider connection values |
| `--plugin VALUE`, `--mcp VALUE` | Add an explicit extension; repeatable |
| `--skill-dir PATH` | Add a trusted skills directory; repeatable |
| `--no-plugins`, `--no-mcp`, `--no-skills` | Disable an extension family |
| `--subagents`, `--no-subagents` | Override child-agent enablement |
| `--usage` | Print normalized usage after a print-mode prompt |

Use [Providers](providers.md) for authentication commands and
[Configuration](configuration.md) for persistent equivalents.

## Navigate the TUI

The default TUI has:

- a header with provider, model, collaboration mode, and activity state;
- a scrollable conversation transcript;
- a composer for prompts, slash commands, and completions; and
- a footer with context, usage, key hints, and pending work.

Most keys can be changed in `keybindings.yaml` or `/keybindings`.

| Key | Action |
|---|---|
| `Enter` | Submit a prompt or accept the active picker item |
| `Alt+Enter` or `Ctrl+J` | Insert a newline |
| `Up` / `Down` | Browse prompt history or picker items |
| `Shift+Tab` | Toggle Default and Plan modes |
| `Ctrl+T` | Cycle supported reasoning efforts |
| `Alt+M` | Open the model picker |
| `Alt+A` | Open the subagent inspector |
| `Alt+P` | Open the managed-process inspector |
| `PageUp` / `PageDown` | Scroll the transcript or active detail view |
| `Home` / `End` | Jump to the beginning or end |
| `Ctrl+A` | Select only the current composer draft; typing, pasting, or deleting replaces it |
| `Ctrl+C` | Copy a Ctrl+A-selected draft; otherwise quit while idle or abort active work |
| `Esc` | Close a modal or abort active work |
| `Ctrl+D` | Quit when the composer is empty |
| `F6` | Toggle Snow-managed and terminal-native mouse behavior |

With `tui.mouse: true`, the wheel scrolls Snow's transcript. Primary-button
drag selects transcript text, and right-click opens a copy menu. Hold Fn while
dragging in Apple Terminal for terminal-native selection.

Use `Ctrl+A` for composer-only Select All on Linux and macOS. Snow clears any
app-owned transcript selection before highlighting the draft. A
terminal emulator can reserve `Command+A`/`Super+A` and select its entire screen
before Snow receives a key event; a terminal application cannot portably
override that global shortcut. To use `Command+A`, configure the terminal's
Snow profile to send the `Ctrl+A` control character instead. This mapping is
terminal-specific; Snow then handles it exactly like physical `Ctrl+A`.

## Steer active work

Submit text with Enter during an active turn to queue steering at the next safe
assistant/tool boundary. Use Alt+Enter to queue a follow-up that runs after
steering and ordinary work settle.

Snow shows queued input in the composer/footer. Ctrl+C or Esc aborts active
work, restores unsent text, clears queued input, and defers automatic goal
continuation.

Use steering for corrections that should affect the current task. Use a
follow-up for work that can wait until the current response completes.

## Use slash commands

Type `/` to open command completion. The essential commands are:

| Command | Purpose |
|---|---|
| `/help` | Open commands and active keybindings |
| `/init` | Create missing `AGENTS.md` and `.snow/config.json` files |
| `/model [id]` | Open the model picker or select a model |
| `/thinking [level]` | Open or set supported reasoning effort |
| `/settings` | Open common model, UI, permission, capability, and update settings |
| `/keybindings` | Inspect or edit global/project shortcuts |
| `/permissions [mode]` | Inspect or set `ask`, `allow`, or `deny` |
| `/login`, `/logout` | Manage provider credentials |
| `/default`, `/plan [prompt]` | Select collaboration mode |
| `/goal [--budget N] [objective]` | Inspect or create a Thread Goal |
| `/compact`, `/context` | Compact or inspect model context |
| `/sessions`, `/resume`, `/new` | Manage saved conversations |
| `/fork`, `/tree` | Fork or navigate conversation branches |
| `/agent [path]` | Inspect subagents |
| `/processes [id or name]` | Inspect managed processes |
| `/mcp`, `/skills [clear]` | Inspect configured capabilities |
| `/trust [allow or deny]` | Inspect or update project trust |
| `/debug [status or action]` | Inspect or control diagnostic capture |
| `/quit` | Exit Snow |

Use `/allow`, `/allow always`, or `/deny` only for the permission request that
is currently visible. Review the requested operation before approving it.

The settings card includes opt-in GitHub update controls. **Check for updates
now** performs a fresh explicit check, while **Update now** checks again before
installing. Startup checking is disabled by default and applies only to
interactive TUI launches. Startup checks fetch release metadata only. When one
finds a newer eligible release, Snow asks you to choose **Install update** or
**Skip for now** and does not download the archive automatically. An approved
install opens a foreground card with byte counts, percentage, a progress bar,
verification, and installation phases. Successful installation offers
**Restart now** after clean shutdown or **Later** to keep using the current old
in-memory process. Development builds can check but never replace themselves.

## Use composer completions

Type `@` to find project files. Enter or Tab inserts the selected path without
submitting the prompt. Whitespace-delimited `@path` tokens use the active theme's
accent color while you type, so referenced files and folder paths stand apart
from ordinary prompt text.

Type `$` after whitespace to find enabled Agent Skills. Enter or Tab inserts
the selected `$skill-name`. An exact whitespace-delimited skill token in a
submitted prompt activates that skill. Pasted text can activate a matching
skill as well, so wrap literal examples in backticks.

Project `AGENTS.md` files load nearest-first as bounded, untrusted instructions.
They are separate from project-extension trust.

## Choose Plan Mode or a Thread Goal

Use Plan Mode when you want investigation and a decision-complete plan without
project mutation:

```text
/plan design the change
/default
```

Snow blocks mutating tools in Plan Mode even if the permission mode would
otherwise allow them. See [Plan Mode](plan-mode.md).

Use a Thread Goal when one branch should continue toward a bounded objective:

```text
/goal --budget 20000 ship and verify the parser
/goal pause
/goal resume
/goal clear
```

See [Persistent Thread Goals](goals.md) for statuses, budgets, and stopping.

## Manage sessions

Snow saves sessions by default. Use:

```sh
snow resume
snow resume /absolute/path/to/session.db
snow --no-session -p "review this directory"
```

Inside the TUI, `/sessions` switches conversations, `/tree` manages branches,
`/compact` checkpoints older context, and `/fork` creates a branch, independent
session, or Git worktree fork. See [Sessions and branches](sessions.md).

## Answer model questions

The model can request structured input when it needs a decision. In the TUI,
Snow opens a distinct question card. Review the full prompt, select or enter an
answer, and submit it separately from tool permission approval.

Print and JSON modes have no interactive question broker and fail closed. SDK
and RPC hosts must explicitly install or enable a trusted input broker.

The complete cross-surface contract is in the repository's
[model-requested input
reference](https://github.com/elmissouri16/snow-core/blob/main/docs/user-input.md).

## Use print and JSON output

Print mode writes assistant text to standard output and lifecycle/tool status to
standard error:

```sh
snow -p "summarize this repository"
snow --usage -p "review this package"
```

JSON mode emits one `protocol.AgentEvent` object per line:

```sh
snow --mode json -p "run the focused tests"
```

Redirect or parse JSONL as a stream rather than waiting for one final object.
If an output consumer blocks longer than Snow's bounded event-subscriber
deadline, print and JSON modes return an explicit error instead of silently
reporting a truncated stream as successful. Both modes fail closed for `ask`;
use `deny` or deliberately grant `allow` in a trusted external environment.

## Manage capabilities

Use dedicated guides for setup. Common inspection commands are:

```sh
snow mcp list
snow mcp check NAME
snow skills list
snow skills get NAME
snow plugin list --all
snow plugin get ID
snow plugin check MANIFEST_OR_EXECUTABLE
```

Disable capabilities for one launch with:

```sh
snow --no-plugins --no-mcp --no-skills --no-subagents
```

## Related documents

- [Getting started](getting-started.md)
- [Providers](providers.md)
- [Configuration](configuration.md)
- [Sessions and branches](sessions.md)
- [Plan Mode](plan-mode.md)
- [Agent Skills](skills.md)
- [MCP](mcp.md)
- [Security model](security.md)
