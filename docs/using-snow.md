# Using Snow

This guide covers Snow's terminal surfaces, essential TUI controls, and common
commands. Start with [Getting started](getting-started.md) if Snow is not yet
installed or connected to a provider.

> **Note:** Snow is alpha software. Run `snow --help` for the command reference
> that matches your installed version.

## On this page

- [Navigate the TUI](#navigate-the-tui)
- [Steer active work](#steer-active-work)
- [Use slash commands](#use-slash-commands)
- [Use composer completions](#use-composer-completions)
- [Choose Plan Mode or a Thread Goal](#choose-plan-mode-or-a-thread-goal)
- [Manage sessions](#manage-sessions)
- [Answer model questions](#answer-model-questions)
- [Choose a runtime mode](#choose-a-runtime-mode)
- [Use common flags](#use-common-flags)
- [Use print and JSON output](#use-print-and-json-output)
- [Manage capabilities](#manage-capabilities)
- [Related documents](#related-documents)

## Navigate the TUI

The default TUI has:

- a header with provider, model, collaboration mode, and activity state;
- a scrollable conversation transcript;
- a composer for prompts, slash commands, and completions; and
- a footer with context, usage, key hints, and pending work.

Selection dialogs open in centered panels: models, settings, login, MCP/skills
inspection, sessions, branches, fork destinations, permission mode, and plan/goal
confirmations. The process (`/processes`) and subagent (`/agent`) inspectors also
use centered panels, capped at 120 columns by 28 rows. They retain side-by-side
lists and details on wide terminals, stack them on narrower ones, and keep
refresh/close controls inside the panel. Tool approvals use the same centered
placement with a dedicated safety-review layout. Panels adapt to the terminal, keep the selected item in
view, and use compact controls on small windows; large windows retain bounded
card widths instead of stretching the list across the screen. Resizing preserves
the selection and draft. Short windows may temporarily cover the composer/footer;
closing the panel restores them. Background scrolling and clicks are blocked
while a modal owns input.

Tool approval is disabled if the card cannot show the required review context
and safety warnings. Enlarge the window to review, or press Escape to deny.
Slash-command, `$skill`, and `@file` completions remain attached to the composer
so you can keep typing to filter them. In `/skills`, Enter or Space toggles the
selected skill's saved enablement without closing the panel. Changes apply after
restart; the card distinguishes saved policy from the running catalog. See
[skill enable/disable controls](skills.md#enable-or-disable-skills).

Most keys can be changed in `keybindings.yaml` or `/keybindings`.

| Key | Action |
|---|---|
| `Enter` | Submit a prompt or accept the active picker item |
| `Shift+Enter`, `Alt+Enter`, or `Ctrl+J` | Insert a newline (Shift+Enter needs enhanced terminal keys) |
| `Up` / `Down` | Browse prompt history or picker items |
| `Shift+Up` / `Shift+Down` | Extend editor selection without recalling prompt history |
| `Shift+Tab` | Toggle Default and Plan modes |
| `Ctrl+T` | Cycle supported reasoning efforts |
| `Alt+M` | Open the model picker |
| `Alt+A` | Open the subagent inspector |
| `Alt+P` | Open the managed-process inspector |
| `PageUp` / `PageDown` | Scroll the transcript or active detail view |
| `Home` / `End` | Jump to the beginning or end |
| `Ctrl+A` | Select only the current composer draft; typing, pasting, or deleting replaces it |
| `Ctrl+C` | Copy a Ctrl+A-selected draft; otherwise quit while idle or abort active work |
| `Ctrl+Shift+C` | Copy selected editor text when the terminal forwards the shortcut |
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

Ghostty supports Shift+Enter through its enhanced keyboard protocol. Theme
colors follow the terminal background on focus and supported appearance changes. Ctrl+J
and Alt+Enter remain available in terminals that cannot distinguish Shift+Enter;
saved newline bindings keep their configured keys. Ghostty normally lets you
hold Shift while dragging for terminal-native selection. F6 also switches modes.

Snow also updates the tab/window title with the project and running/waiting
status. In Ghostty, a progress bar above the split shows activity and pauses for
approval or input. When you are in another tab/window, completion and requests
for attention send a generic desktop notification and terminal bell. Manual
compaction also reports completion or failure after final cleanup; cancellation
reports Stopped without a completion alert. If automatic compaction fails and
blocks a goal, Snow sends one generic failure alert.
Change
**Terminal tab title**, **Terminal progress**, or **Terminal alerts** in
`/settings`; alerts can be `off`, `unfocused` (default), or `always`.
See [terminal integration settings](configuration.md#tui) for Ghostty controls
and notification permissions.

Ctrl+V first checks the local clipboard for an image in the composer, then reads
text. Over SSH, it requests text from the terminal using OSC 52. A failed local
text read uses the same fallback. Terminal permission/settings can prevent the
reply; Snow times out after three seconds and terminal-native paste remains
available. A late reply is discarded after changing editors or timing out.
Because OSC 52 has no request IDs, another terminal query waits until that late
reply is drained. Repeating Ctrl+V while a query is pending keeps the original
reply valid and preserves the selected text and attachments. Text clipboard reads
are limited to 1 MiB. Terminal-native paste also works in session rename and
branch rename/fork name fields.

Large composer pastes collapse into inline attachments while preserving their exact
text. Like a small paste, a large paste replaces the current selection. Up/Down
history navigation works through collapsed entries, and Down past the newest entry
restores the saved draft. Recalled multiline or wrapped prompts keep the insertion
point visible.

Copy uses a host clipboard utility locally and OSC 52 over SSH or as a fallback,
with tmux/screen passthrough. A “sent” status means the command was written; the
terminal does not acknowledge that it changed the clipboard.

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

In `/model`, type to search and press **Ctrl+R** to refresh provider catalogs.
Opening the picker also refreshes expired Zen catalogs automatically. Cached
models remain visible while discovery runs.

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
is currently visible. The shell permission picker names these choices **Allow once**,
**Allow this scope**, and **Deny** when static effects are rememberable. Its
prompt shows inferred operations and paths and warns that approved shell commands run as
an unrestricted host process. Both `bash` and `process_start` use this preflight.
Dynamic or unknown requests cannot be remembered; reusable scopes include the
exact command, working directory, environment digest, and current policy. Review the command and requested effects before approving them;
static analysis cannot see operations hidden inside an external executable.

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
Snow opens a centered question card, matching the model and settings panels.
Select an answer or type in its bordered input field. Enter accepts, Escape
declines, and Tab/Shift+Tab moves between questions while preserving drafts.
Use Ctrl+V to paste and Shift+Enter or Ctrl+J for a new line. Answers are submitted separately
from tool permission approval.

Print and JSON modes have no interactive question broker and fail closed. SDK
and RPC hosts must explicitly install or enable a trusted input broker.

The complete cross-surface contract is in the repository's
[model-requested input
reference](https://github.com/elmissouri16/snow-core/blob/main/docs/user-input.md).

## Choose a runtime mode

| Mode | Invocation | Use it for |
|---|---|---|
| TUI | `snow` | Interactive coding, approvals, sessions, and settings |
| Resume | `snow resume [path]` | Continue a saved conversation |
| Print | `snow -p "prompt"` | Human-readable one-shot output |
| JSON | `snow --mode json -p "prompt"` | One normalized event per JSONL line |
| RPC | `snow --mode rpc` | Long-lived control from another process |
| Web preview | `snow --mode web` | Local authenticated manager shell; no agent execution |

Supplying `-p` selects print behavior unless `--mode json` or `--mode rpc` is
set. Print and JSON modes require a nonblank prompt. RPC keeps standard input
open for commands and ignores `-p`.

The complete RPC contract remains available in the repository's
[JSONL RPC reference](https://github.com/elmissouri16/snow-core/blob/main/docs/rpc.md).

### Try the local web manager shell

```sh
snow --mode web
# Optional port override (numeric loopback addresses only):
snow --mode web --web-listen 127.0.0.1:7441
```

Open the printed URL and enter the pairing code from the terminal. The code is
reusable for up to **30 days**, survives manager restarts, and expires at the time
printed on startup. Browser access can rotate it immediately without signing out
existing browsers. Paired browsers also survive restarts, with 30-day absolute
and idle limits and at most eight browsers. Sign out revokes the current browser;
**Revoke all browsers** revokes every browser and rotates the code. Restart to
print that replacement code. Credentials are stored privately under
`$SNOW_HOME/manager/access.json`; keep this file and terminal captures private.

After installing an updated Snow build, stop and restart the foreground manager
and activate fresh workers. A browser reload or reconnect does not replace an
already-running manager/worker executable; use documentation matching that build.

#### Activity and workspace organization

**Activity** is a read-only overview of at most **100 registered projects**. It
shows host-running work, permissions/questions needing attention, failures,
recovery hints and queued/review counts. Counts can overlap; a host-running badge
is not evidence that a browser is connected. Cards are navigation links, not
permission, Stop or execution authority. Activity reads registry metadata and
in-memory public state, never activates a worker or scans saved conversations.
A saved-session link that no longer matches the live session is rejected rather
than silently controlling another conversation.

**Organize workspaces** changes manager metadata only. Rename a workspace label,
pin/unpin it, or explicitly archive/restore its registration. Neither archive nor
**Remove registration** deletes project files or saved sessions. Close a live
workspace before archiving it. Restore retains the original registration ID and
requires the same canonical folder path and device/inode identity, no active
alias/duplicate, and room within the **100-active-project** limit. It does not
activate work or recreate a missing directory.

Saved conversations support manager-only pin/archive/restore flags, not a
session-file rewrite. **Archive conversation** acts on one click; **Restore
conversation** reverses it. Unlike workspace archive or permanent deletion, this
reversible metadata change has no extra confirmation step.
Close the project's worker first: each action rechecks the
exact session's membership on a fresh read-only catalog page. Flags are bounded to
**1,000 sessions per project / 10,000 total**. Catalog and archived-registration
pages contain at most **25 entries**, with navigation bounded at offset 10,000.
Title search and archived filtering apply to the **loaded page only**, not every
session or transcript; they do not change catalog pagination. A pin is metadata,
not a guarantee that every pinned conversation has been loaded.

#### Optional local HTTPS and browser inventory

HTTP remains the default. To use **direct numeric-loopback HTTPS**, supply your
own certificate and private key together:

```sh
snow --mode web --web-listen 127.0.0.1:7441 \
  --web-tls-cert /absolute/path/loopback-cert.pem \
  --web-tls-key /absolute/path/loopback-key.pem
```

Both paths must be absolute and clean, with bounded regular PEM files and no
symlink components; each input is limited to 1 MiB. TLS requires version 1.2 or
newer. Snow does not generate certificates, install browser/OS trust, accept DNS
listener names, or enable LAN/proxy/tunnel access. Arrange a certificate valid
for the numeric loopback address and trusted by your browser yourself. HTTPS
cookies are Secure; both transports retain HttpOnly, SameSite=Strict, exact
Host/Origin checks and CSRF protection. Supplying only one TLS flag fails closed.

**Browser access** lists paired browsers using independent public IDs, coarse
browser labels, creation/last-seen/expiry times and a current-browser marker.
Last seen is approximate after restart. Revoke a selected browser, including
this one, without revoking the others; IDs are targets, not credentials. An open
SSE stream rechecks authority approximately every five seconds, not immediately.
Revoking access does **not** stop agent work. Use the worker's explicit Stop/Close
controls separately when that is your intent.

#### Host defaults, local provider status and API-key entry

In **Settings → General**, explicitly load **Host defaults** or inspect local
provider status. Opening Settings alone does neither. These operations use a
short-lived runtime-free control worker: no agent/session, provider connection,
credential refresh, extension loading or tool execution is started.

Global defaults include provider/model, thinking, reasoning summary and text
verbosity. Project defaults include provider/model and thinking for the selected
registered host project; they are entries in the operator's global
`project_selections`, not project-authored configuration. Values distinguish
explicit settings, inherited effective values and their source. Save/reset uses
a reviewed revision; a conflict requires explicit reload and review. These
settings apply to **future workers**, not the current worker or a new conversation
created within it. Local provider status is only configured/expired/unavailable
metadata; it is not a network or credential-validity test.

**Write-only API key** is available only over the manager's actual direct
numeric-loopback HTTPS connection. Select an existing supported provider/profile
and explicitly inspect its local status before entering a key. That inspection
is a browser-bound, single-use grant valid for five minutes. Saving requires
explicit host-save confirmation and, when applicable, replacement confirmation;
a stale auth-file revision rejects the write. Keys are limited to 4 KiB and are
never returned by the status/save response. A write consumes the inspection even
if its outcome is uncertain: inspect again rather than automatically resending.
Existing workers must be restarted to use changed credentials.

There is no key export/delete, provider network verification/refresh or browser
OAuth flow. ChatGPT continues to use interactive `snow login chatgpt` on the host.
HTTP users must also use host-side interactive login rather than entering keys
in the browser. See [configuration scopes](configuration.md#local-web-manager-settings-scopes).

#### Workspace layout

The web UI opens on a dark, conversation-first landing page, closely matching
DeepSeek Harness's workspace layout while retaining Snow's identity and controls.
A full-height **Workspaces** sidebar contains registered folders, saved sessions,
project-name search, and **New session**. **Settings** at the bottom opens a
sectioned dialog: **General** for Light/Dark appearance, **Workspaces** for real
workspace navigation and management, and **Browser access** for pairing,
revocation, and sign-out. A saved light preference is honored; otherwise the
default is dark.

You can write a message on the home screen before starting anything. Choose a
workspace from the centered picker, then **Continue** to its Start/Resume page.
Continue without a selection opens the picker. Selecting a workspace on home
keeps your draft in place; sidebar links still browse projects directly. The
picker's **Add workspace** path also keeps the draft through folder registration.

Drafts stay in this tab's memory, never in URLs or browser storage; a full reload
or closing the tab clears them. Your draft remains visible during activation
and moves into the conversation composer afterward, ready for review and an
explicit **Send**. It is never auto-sent or used to replace an existing draft.
If the conversation already has a draft or a message edit in progress, the
startup draft stays in a separate notice with Edit/Discard controls; finish the
edit and clear the conversation draft before choosing **Use draft**.

Each workspace has an independent disclosure arrow and an indented session list.
Several workspaces can remain expanded while you read or switch sessions; the
sidebar does not become a separate catalog page. Lists load lazily in 25-entry
pages with explicit **Load more** and retry/unavailable states. Live inventories
are limited to 100 sessions. Expanding a workspace performs a session-only read:
it never activates an agent, discovers models, or contacts a model provider.
Expansion and bounded metadata are remembered in this tab, not durable settings.

Registering a workspace opens an empty **New session** draft without creating a
session or starting a runtime. Opening a cold workspace normally restores the
last session you viewed in this tab if it is still available, otherwise its most
recent saved session. Saved messages appear in the central conversation surface,
with **Resume session** in the composer seat; a workspace with no sessions shows
**Start session** or **Trust & start** there instead. The cold textarea remains
editable before Start/Resume, but starting does not send its text. A draft owned
by another cold session stays retained instead of being overwritten. Automatic
transfer requires the exact acknowledged workspace, session and runtime instance.
Existing session cleanup still applies: switching away from an unused, unnamed
empty session can remove it. Name it or send a message if you want it retained in
saved history; a tab-only draft is not a saved message.

The workspace/session sidebar keeps its open search, filter and scroll position
when you browse. On desktop, a focused list row keeps focus after navigation
instead of jumping into the conversation; moving focus elsewhere while a read
is pending does not pull it back. Rapid sidebar selections supersede older
pending reads. Closing the current conversation's sidebar Rename dialog returns
focus to its row. Saved-history browsing still does not activate a worker.

Starting a worker always requires an explicit action. On first
activation, confirm **I trust this workspace. Remember my choice…**. That choice
survives manager restarts for this exact registered folder and applies to this
manager’s paired browsers. Later visits show compact **Start session** or
**Resume session** controls instead of repeating the trust checkbox and warning.
Nothing starts automatically, and trust does not grant tools Allow permissions.
New sessions still start in Ask; resumed sessions restore their saved policy.

Use **Settings → Workspaces → Remembered project trust → Forget trust** to require
confirmation again. This affects future starts, not already-running workers.
Removing or archiving a registration clears remembered trust; restoring or
re-registering requires fresh consent. Missing or replaced folders cannot use
remembered consent. Existing registrations are not automatically trusted by an
update: confirm once after installing this feature. This is separate from CLI
`/trust`, which governs project extensions.
Each workspace row also has a chat-plus **New session** action and an actions
ellipsis. In a cold workspace, New opens the empty draft without starting a
worker. In a live workspace it uses the existing guarded session-switch workflow.
For another live workspace, the explicit click mounts and verifies that owner
before requesting New or the selected session—once, never through a mutation on
GET. Disconnection, a replacement owner or superseding navigation cancels the
pending intent rather than retrying it. Active turns still require **Stop &
switch** confirmation; queued work and unknown outcomes keep their existing
blocks. A direct mismatched saved-session URL never switches a live owner.
While a cross-workspace selection is being checked, the conversation area shows
**Opening selected conversation…** rather than flashing the previous session's
history or draft. A verified idle switch keeps its composer layout stable while
connecting the replacement subscription; real errors and Stop confirmations
remain visible.

Each supported session row has one **⋯** actions menu. Choose **Delete session**
from that menu, review the session name and permanent-deletion warning, then check
the confirmation before deleting. The current session's menu also offers Rename;
delete icons are not displayed directly beside session names. This removes that conversation and its managed
private session data, not workspace files; there is no undo. The currently active
session cannot be deleted: switch to another session or explicitly close the
workspace first. Running work, retained queue/goal guards, ownership changes, and
another process holding the database can reject deletion. Cold deletion uses a
runtime-free control worker and does not activate an agent, read provider
configuration, discover models, or create a replacement saved session.

After deleting the saved session you are viewing, Snow opens the empty **New
session** view without starting or sending anything. A matching startup draft is
retained in the tab and retargeted to that new draft; other session drafts stay
separate. An unsuccessful or interrupted request is never automatically retried;
if deletion cannot be confirmed, refresh the list and review the actual state
before taking further action. Removal may already have happened even when a
cleanup or connection error is reported.

**Workspace settings…** opens the Project inspector tab, while
**Remove registration…** reveals its existing unchecked confirmation. Opening
these actions neither reads Files/Changes nor submits removal. Removing a live
project remains disallowed until its runtime is explicitly closed.
Text inputs, search fields, textareas and selects use a thin blue focus edge
inside the field boundary, without a detached halo or a size change. Compound
fields such as the composer and free-text answers highlight their outer field
once, rather than outlining the inner textarea too. Buttons, links, checkboxes
and radio controls retain their separate keyboard-focus treatment.

After activation, the compact composer stays in a non-scrolling seat below an
adaptively sized transcript with 16px vertical insets. Multiline drafts grow from
the normal 36px editor floor to a viewport-aware cap of at most 336px, then scroll
inside the editor; clearing or restoring a draft recalculates its height. Pending questions or approvals replace the normal
composer in that same seat; the prompt draft remains mounted and is restored
when attention clears. Its toolbar contains a separate Default/Plan control, a
model-only picker, a context/usage indicator, and Send (replaced by Stop during active work).
Supported **Goal**, **Thinking: current level**, and **Compact context** controls
are directly in the composer toolbar. Goal toggles local composition intent:
type the objective in the existing editor and submit **Start goal run** to start
work. Toggling alone does nothing to an existing goal or run. Thinking opens a
compact model-capability-driven level dropdown; selecting a different level
applies it immediately to this session, without an Apply step. Compact starts
compaction immediately. Send, pending admission and the square Stop icon share
a fixed footprint; accessible names and tooltips identify their exact action.
Conversation actions live in the header ellipsis; the current sidebar session
also offers Rename. In an activated conversation, that menu also opens supported
**Conversation versions**, **Thread goal**, **Managed processes**, **Thinking &
response**, **Compact context**, and **Steer current run…** controls. Availability
comes from each existing runtime controller; opening the menu does not inspect,
start, restore, compact, or steer anything. Runtime Close and Files/Changes stay
separate header controls. These are Snow capabilities presented through
Harness-inspired menus, not claims that Harness provides the same features.

Runtime panels use bounded dialogs with a persistent title/Close row and a
separately scrolling body. Refresh and Stop keep normal text-button sizing;
Versions becomes one column on narrow screens. Closing returns focus to the
visible opener. Thinking uses a native anchored popover instead of a modal;
Escape and successful selection return to its composer trigger. Its secondary
**Response settings…** action opens the summary/verbosity dialog. Managed process
inventory polling runs only while its dialog is open and visible; closing it
does not stop processes. Goal status and Details sit above the composer;
compaction and steering status remain in a bounded conversation region.

Assistant and public-plan messages render sanitized Markdown,
with message/code copy controls; user input remains literal text. Public tool
activity uses compact disclosures at its chronological position in the live
transcript: calls stay before the following answer and do not collect below later
requests. Live tool-step markers are explicit runtime event positions, not claims
of persisted message identity. Legacy or bounded-out activity without a retained
marker stays in a separately labeled unassociated fallback rather than being
assigned to the latest answer.
Files/Changes remains closed until explicitly opened. Desktop navigation can
collapse to an icon rail; below 768px it becomes a keyboard-accessible drawer.
Menus are viewport-clamped with independently scrolling choices and a fixed
action footer. Files/Changes uses a side pane above 1100px, stacked panes at
768–1100px, and a full-height sheet below 768px. Files and Changes expose bounded
read-only host views, not an editor. The Project tab shows host/project metadata
and the explicit metadata-only registration-removal form; it is not a runtime
settings editor.

On wide conversation columns, drag either margin beside the transcript to
resize the centered chat and composer together. The width is a browser-local
appearance preference, not a project setting or runtime command. Shrinking the
window or opening the inspector clamps the displayed width without overwriting
a wider saved preference; narrow columns hide the handles. Keyboard users can
Tab to a width handle, use Left/Right to move that edge (Shift uses larger steps),
and press Home to restore automatic width. Escape cancels a drag without saving.
If browser storage is unavailable, resizing still works in the mounted conversation.

The transcript is its own scroll area. Scrolling upward releases automatic
following; updates preserve an identified visible message/activity anchor instead
of pulling you back to the bottom. **Jump to latest**, an accepted send, or
deliberately scrolling down into the tail resumes following. The jump control
uses the measured composer/attention seat, including viewport changes, rather
than a fixed guessed composer height. Drafts and scroll positions are tab-memory
conveniences, not durable recovery.

#### Stop a turn or edit an earlier message

During active work, **Stop** replaces Send. When a prompt is still being sent,
a current running snapshot must first confirm which turn is active. The real
manager then accepts a turn-bound cancellation request even while RPC admission
is pending. **Stopping…** means cancellation was requested, not that the agent
has already stopped: the composer becomes ready only after authoritative turn
completion and the cancellation control has finished. Pending questions and approvals offer **Stop turn** in the same
composer seat. Duplicate clicks do not dispatch repeated cancellation, and an
old cancellation cannot target a later prompt. Disconnected or uncertain browser
state does not authorize a new cancellation or silently retry it.

Stopping does **not** roll back file writes, commands, network requests or other
side effects already performed. Partial saved history remains. Review an unknown
outcome before continuing; never resend a mutating request merely to refresh the
screen.

In an idle, connected live conversation, choose **Edit & resend** on an eligible
user message. Snow loads its complete saved text into the composer. Edit it—or
leave it unchanged to regenerate—and explicitly choose Send. The selected message
is **replaced on the active conversation path**, its following replies and tool
rows disappear from that path, and Snow generates a new continuation from there.
It does not append an edited copy beneath the old replies or create another chat.
The conversation identity and title stay the same.

Opening the editor only prepares the edit: it does not change history or start a
provider. Cancel restores the earlier draft and selection. Previous drafts and
text typed during pending requests are preserved in bounded tab memory, not
durable storage. If an edit expires, the conversation changes, or the result is
uncertain, Snow does not silently fall back to a normal prompt or retry the edit.

The original history remains append-only internally, on an inactive branch; it
is not erased from storage. Editing does **not** undo earlier file changes,
commands, or other tool effects. The initial edit operation supports complete
plain-text user input up to 64 KiB, without attachments or plugin transformations;
active goals, subagents and recovered queued input must be settled first.
Truncated or unsupported sources are not executable replacements. Inactive saved
history does not automatically start a worker or send a prompt. Older hosts
without historical editing may offer the separate **Use as new prompt** action,
which explicitly copies text for a new message instead of editing history.

#### Versions: review and restore conversation history

In an already activated conversation, **Versions** lists bounded saved branches
(up to 100 per page) and previews their public history (up to 64 messages per
page). Opening or paging a preview neither changes the active branch nor invokes
a provider. It is a conversation-history view, not a Git/worktree version browser.

Choose **Restore** and explicitly confirm the reviewed target. Preparation binds
the exact worker/session, current source branch/tip and target branch/tip to a
**single-use token valid for two minutes**. Commit rechecks those identities;
stale or uncertain outcomes are not retried. Restore requires an idle worker
with no pending attention, active/nonterminal goal conflict or retained queue.
It selects the saved conversation path while preserving append-only history,
current model and permission authority, and applying the target branch's
**authoritative saved Default/Plan mode**. It stays idle: no provider replay,
queued work, filesystem undo or command rollback occurs. Restore does not restore
old permissions or resurrect another session's control handles.

#### Fork/rename history and compact context

**Versions** additionally offers **Fork branch here**, **Create detached
conversation**, and **Rename selected branch**. Enter a name and click Rename or
Create once; these actions do not need another confirmation or checkbox.
Forking **and activating** a branch still asks you to confirm the active-history
change. These controls require an inspected idle worker with exact session,
source/target branch tips, names and revision. The
store rechecks the transaction's authority before writing. Forking an ordinary
branch activates that new conversation branch; a detached conversation fork
creates saved history but does **not** open it. Branch rename changes metadata
only. No operation replays tools/prompts, rolls back project files, creates a
Git worktree, or resumes a saved goal. Passive Versions/history reads do none of
these mutations; stale/unknown results require review rather than retry.

Click **Compact context** using the icon in the live composer's action row
(or the same action in the conversation menu) to start compaction immediately.
There is no popup, review step or consent checkbox: the explicit click authorizes
one request. The tooltip notes that this uses provider tokens. Progress, results
and errors appear in a bounded notification above the composer, without resizing
the chat or changing its scroll position. When nothing needs compacting, it reports
**No compaction was needed** without showing the ordinary chat Working indicator.
Terminal feedback can be dismissed; dismissal never sends a request or changes
runtime state. Your unsent draft stays intact. **Queue next** is not offered for
compaction. The composer's **Stop** still cancels the whole compaction run.

The click captures the current session, branch, tip and revision, which the
existing admission layer and server recheck before starting work. This is real
provider work, not a read-only counter refresh. Pending attention, retained queue
work, nonterminal goal conflicts and uncertain outcomes block admission; repeated
clicks during admission or execution do not start another run. Nothing is retried
automatically. The run keeps ownership until terminal completion: progress saying
a summary is done is not proof the run has completed. Manual compaction is
available in Plan Mode and preserves that mode, append-only exact history and
safe tool pairing while reducing provider-facing context. Ordinary prompt
completion refreshes the authoritative branch tip before advertising idle
readiness, so subsequent compaction captures the newly completed history without
an extra user inspection or retry.

#### Current-session reasoning and recorded cost

Open **Thinking: current level** in the composer (also available as **Thinking &
response** in the conversation menu). Opening performs a read-only inspection
of the current model's local capabilities; it does not discover models or contact
a provider. Select an advertised level to apply it directly and close the picker.
Choosing the already-current level only closes it; Escape or outside click also
dismisses it without a write. There is no universal hardcoded level list or
separate Apply workflow. Busy controls retain the last reported level without
reflowing the composer; their disabled/busy state and tooltip indicate availability.
An unverified update shows **Unverified**, retains the recovery fence and never
replays automatically.

Changes require idle, exact session/branch/tip/model/mode/permission/revision
authority and update **only the current runtime**. They do not write config or new
session-history metadata, and are not a restart-persistent preference. Default
and Plan retain independent thinking settings. Changing thinking does not switch
collaboration mode, grant tool permissions or start work.

Reasoning summary and text verbosity are separate from thinking levels. The
secondary **Response settings…** option opens their capability-derived controls;
choose a preference/value there and click **Apply session-only update** once.
There is no additional review or consent checkbox.

The composer's **Context & usage** popup shows compact context, token-usage and
cost rows. **Details** opens the accounting and approximation explanations;
Back or Escape returns to the summary. Unchanged metrics stay in place during
live updates, and unavailable values remain explicitly **Unknown**.

Usage may show a **recorded cost estimate** with its currency. Unknown cost is
not zero; invalid or mixed currencies stay unknown instead of being summed.
Known amounts can cover only priced usage while other requests are unpriced.
This is neither a complete bill nor a spending/billing cap.

#### Steer current run versus Queue next

**Steer current run** sends native steering to the exact admitted ordinary run;
it is not a queued follow-up or an immediate promise to interrupt a tool. The
receipt means **accepted**, not delivered. The status can later become delivered,
discarded or uncertain. A delayed/failed POST may already have delivered input;
review its outcome rather than submitting the same steering again. A receipt
arriving after run completion still belongs to that original run, not a newer
conversation or draft. Goal runs and manual compaction do not accept this control.

If the steering response is lost, **Stop** remains available only for the exact
captured run; it cannot cancel a replacement run. Once that run is idle, choose
**Keep draft and dismiss** to retain the steering text and uncertainty while
releasing the panel. Then review the shared uncertainty notice and choose
**Reviewed** separately to release Send and Close. Neither action proves delivery
or retries steering; a later submission is a new explicit request.

Steering and Queue next share the bounds of **eight pending/review inputs**,
**64 KiB per input**, and **256 KiB total**. They use the existing serial agent
loop, not a browser scheduler. Neither reconnect nor Stop automatically replays
uncertain/discarded steering. Queue next remains the distinct natural-follow-up
workflow described below.

#### Start or resume a Thread Goal

In a connected, idle conversation, inspect **Thread Goal** and explicitly choose
**Start goal** with an objective and optional positive token budget, or **Resume
goal** for the reviewed eligible saved goal. Saved goals remain deferred on
activation, switching, restore and reconnect; reading them never starts work.
The controls bind the exact worker, session, branch, tip and existing goal, plus
the reviewed public-state revision. If these change, refresh and review again;
Snow does not silently replace an unfinished goal. Plan Mode rejects Start/Resume.

One correlated goal-run handle owns many serial native turns, including retries
and compaction. **Stop ends the whole run**, including gaps between turns; one
turn's completion does not release that ownership. A run ending does not mean the
objective is complete: inspect its separate semantic goal status (`paused`,
`blocked`, `usage_limited`, `budget_limited`, `complete`, or deferred `active`).
The optional budget stops further substantive work, not in-flight usage or all
billing; see [Thread Goals](goals.md#use-goals-in-the-local-web-manager).

Ordinary prompts cannot create or update goals through model tool calls: this
worker's goal tools are neither exposed nor dispatched for ordinary turns.
Explicit goal runs use only their owning goal; there is no browser goal-replacement
or autonomous goal-creation workflow. Resolve retained queue/review work first.

#### Managed processes

The fixed worker profile enables `process_start`, `process_status`, `process_logs`,
`process_stop` and `process_list` alongside the ordinary builtins. Agent process
launches still pass normal hard-policy and permission gates and run with host OS
privileges. **Processes** itself is an inspector, not an arbitrary command form:
it lists at most 128 managed records and reads at most **32 KiB of logs per page**,
with cursors and omitted-byte indicators. Logs are untrusted plain text, not HTML.

Every list/log/Stop request names the exact live session; logs and Stop additionally
name an opaque managed-process handle, never an OS PID. **Stop process** requires
Default mode, the agent's actual hard invocation policy, and an Allow decision
from the current noninteractive permission state. Ask without an applicable
remembered allow fails closed: this control opens no second approval broker and
cannot override Deny or Plan. The inspector is not a remembered-grant editor.
Switching sessions and manager shutdown stop managed processes; arbitrary detached
descendants and effects already performed are not contained or rolled back.

#### Queue next while Snow works

During an ordinary admitted prompt run, **Queue next** explicitly submits a follow-up for after
the current reply naturally finishes. It does not interrupt the reply or bypass
its tools, approvals or questions. The pending-work panel is separate from your
unsent composer draft. A queued input enters chat history only when the worker
actually delivers it—not when you add it to the panel.

You can edit or remove an item before delivery starts. Updates are checked
against its exact identity and queue revision; if delivery wins the race, the
edit is rejected rather than applied to another request. The queue accepts up to
**eight pending/review inputs combined**, each at most **64 KiB**, with
**256 KiB** of text in total. Enqueueing clears only the draft that was actually submitted;
concurrent typing is kept.

**Stop ends the admitted run and its queued continuations.** Unsent accepted
items are retained for review rather than automatically executed after a cancel,
failure or limit. An uncertain item is labeled as possibly delivered, not as a
safe retry. Copying review text to the draft never sends it; any new submission
requires an explicit action. A turn ending before enqueue admission does not
silently convert Queue next into Send.

This queue belongs to the **live worker**, not a durable scheduler. Reconnecting
to that worker reads its current state without replay. Closing/restarting a
worker or the manager does not resume pending work; copy text you need before
closing. Items never migrate to another chat or project. Delivered queued inputs
have durable input boundaries so eligible messages still support historical
editing and regeneration after reopening.

Retained queue/review items must be resolved before Start goal or Resume goal.
**Queue next is disabled during a goal run**; it cannot splice another objective
into the goal's serial turns.

#### Regenerate a reply

Choose **Regenerate** under an eligible completed assistant reply, then confirm.
Snow restarts that reply's entire turn from its **original prompt**, including
any tool work, and replaces the following conversation. The active chat contains
one copy of the original user prompt—not an additional prompt appended at the
bottom. The chat identity and title stay the same, and **Stop** remains available
while the replacement runs.

Opening or canceling the confirmation does not change history. Your composer
draft stays untouched; regeneration never copies the old prompt into it. Tool
prefaces, plans, truncated or incomplete replies, and inactive saved views do not
silently become regeneration targets. The original prompt must meet the same
plain-text and safe-history requirements as editing.

**Tools may run again.** Earlier file changes and commands are not undone; the
current permission policy still applies. The original conversation remains
internally preserved, but is no longer the active path. Unknown outcomes are
never automatically retried. Settle active work before regenerating and review
any uncertain outcome explicitly.

#### Choose a folder on the Snow host

Choose **Add workspace** beside Workspaces, or **Settings → Workspaces → Manage workspaces**,
then the host-folder browser. It lists directories **on the
machine running Snow**, inside the manager UI—not directories on the accessing
device, a browser upload API, or a desktop dialog on the server. Navigate Home,
Up, and directory entries, then **Select this folder**. You can also enter an
absolute host path manually. Only directory names are listed; file contents are
not uploaded or read by the picker. Listings scan at most 256 entries per page
and 4,096 per directory; use a direct path if the listing is limited. Host OS
permissions apply, and inaccessible folders cannot be browsed.

Register the existing directory with an optional name, then select a project
and saved conversation. Registration alone never activates a runtime. Removing
a registration requires confirmation and retains all project files and session
databases; close a live session first. Up to 100 projects persist in
`$SNOW_HOME/manager/manager.db` (default `~/.snow/manager/manager.db`), owned by one
foreground manager at a time. Missing/replaced roots stay visible but cannot be
activated; remove and register a changed root again deliberately.

#### Create or clone a host project

**Create / clone projects & operations** selects a parent directory on the Snow
host and creates one new child folder, either empty or from an **anonymous HTTPS**
Git repository. This uses the host user's filesystem authority, **not** a
startup-root allowlist or filesystem sandbox. The browser cannot supply an
arbitrary command. The selected canonical parent and its directory identity are
bound to this browser/manager by a five-minute grant; existing destinations are
never adopted or overwritten. SSH, credential-bearing URLs, local/file clones
and authenticated clone profiles are not supported.

Admission is durable before execution, with one active operation, no job queue,
at most 128 retained records and 32 records per page. The worker first creates
and pins the child, then waits for the manager to durably acknowledge that exact
identity before clone network work can start. Repeated admission for the same
operation and payload returns the recorded operation rather than rerunning it.

Successful create/clone completion stops at **awaiting registration**. Review the
recorded destination and explicitly choose **Register**; that separate action
checks the operation revision and directory identity before adding the project.
Success, Get/List, reconciliation and manager restart never register it
automatically. Registration never activates a runtime or opens a conversation.
If registration fails or its outcome is uncertain, inspect and review again
rather than automatically retrying. **Cancel** requests worker cleanup; it is not proof cleanup
has finished. **Reconcile** observes recorded/current identity only, without
restarting execution or registering. Interrupted or uncertain outcomes remain
for explicit review. **Dismiss** removes settled operation metadata only, never
the folder or a partial clone. There is no automatic recovery retry or deletion.

Creating an empty directory does not require Git. Clone admission validates the
fixed absolute Git executable before allocating a clone handle; an unavailable
selection fails without searching PATH or falling back to another executable.
Clones use that executable and a trusted descriptor-based helper, a separate
process group and worker-liveness pipe. The operation timeout is ten minutes;
Git output is budgeted at 64 KiB and is not exposed as a terminal/log stream.
Cancellation gives the group two seconds after TERM before forced termination.
These are time/output limits, **not** disk-size or network-transfer quotas and
not a process/network sandbox. Browser disconnection leaves admitted operations
running; manager shutdown cancels and joins its operation workers. Review failed
or partial destinations on the host; Snow does not clean them up by deleting them.

#### Saved history and live conversations

The catalog shows **inactive supported SQLite sessions only**. Live/locked,
unleased, WAL/recovery-dependent, invalid, child, empty nondurable, or larger
than 64 MiB databases are omitted; an empty list does not prove no saved sessions
exist. Close an active terminal session and refresh. Text history follows the
saved current branch, including pre-compaction history, but excludes thinking,
tool calls/results, images and provider-private state. Pages contain up to 25
entries with explicit truncation notices. Session storage still comes from
`SNOW_SESSIONS_DIR` or `~/.snow/sessions`, independently of `SNOW_HOME`; relative
overrides resolve at manager startup, before workers change directories. Runtime,
catalog, CONTROL and project-operation workers all capture the same absolute
operator `SNOW_HOME` and independently resolved session root before changing CWD.
Manager storage is absolute too; host-control and project-operation backends use
the registry’s canonical directory. A storage-root resolution failure disables
worker startup rather than inheriting ambiguous relative paths.

Each catalog read uses a short-lived `snow --mode rpc --rpc-startup catalog`
worker with fixed arguments and the selected project CWD. At most two reads run
concurrently without a queue or idle pool. Catalog startup bypasses app,
configuration, credentials, instructions, extension and provider initialization.

To work, explicitly activate a new session or the selected saved session. Activation uses the host's configured provider/model; if those defaults cannot
start, configure them on the host first. After activation, use the discovered
provider-grouped model picker rather than typing identities. Activation starts a separate
Snow RPC worker, loads host configuration and applicable trusted project
instructions, may discover provider models, and **defers any saved goal**. It
does not send a prompt. New sessions start with permission mode `ask`; explicitly
resuming a saved session can restore its saved policy. The live profile disables
plugins, MCP, subagents and debug capture. Skills are disabled unless explicitly
enabled with **Enable installed skills**, whose checkbox remembers the project's
last saved choice. The opt-in adds only the three skill lifecycle tools and
retains separate CLI extension trust. Its fixed
`managed-explicit-goals` profile enables read/glob/grep/write/edit/bash/ask_user
and the managed-process bundle. Goal tools are available only within an explicitly
admitted native goal run, not ordinary prompts. This is still **OS-privileged agent
execution, not a sandbox**.

Send a text prompt, watch text update, use Stop, and answer permission/question
cards. The attention card pages through up to 16 questions, retaining selections
and custom/free-text answers while moving back/forward or collapsing the card.
It submits the complete validated batch with exact option labels and question
identities; a recommendation is not an automatic selection. **Stop turn** cancels
the whole turn without submitting answers, not just the visible question.
Permission choices are **Allow once** or **Reject** (deny), never remembered
grants; truncated summaries cannot be allowed. The host-authority warning remains
visible outside the scrollable summary.

The composer's shield control selects the current session's **Ask / Deny /
Allow** permission policy while connected and idle. This is separate from both
Default/Plan collaboration mode and the per-request approval card. Ask requests
approval when required (existing session decisions still apply); Deny rejects
non-read-risk operations without prompting; Allow skips permission prompts.
Read-risk operations remain allowed in Ask/Deny. **None of these policies is a
sandbox.** Allow requires an explicit, unchecked risk acknowledgment before the
change is sent. Changes persist in this session, not in host configuration or
new-session defaults. Missing/unverified policy is shown as unknown, not assumed
to be Ask. Opening or dismissing the menu sends no policy change, and a failed or
uncertain change is never automatically replayed. The normal composer yields to
pending approval/question cards; policy switching is not a way to approve an
already-pending tool request. The menu keeps one short boundary note; **Details**
opens the complete policy explanation, with Back/Escape returning to the choices.

Default/Plan, permission-policy, model-selection and conversation-name changes
use HTMX's request API without swapping the conversation or composer. A verified
idle change retains the healthy live subscription, draft, selection and scroll
position; known labels remain stable while controls are briefly locked. Explicit
model discovery also uses HTMX and keeps cached choices, search focus and list
scroll while refreshing. Menus update existing rows instead of rebuilding their
contents; the model popup keeps a stable size through loading and retry.

Process inventory refreshes retain unchanged rows and controls. Versions refresh
keeps the previous read-only preview visible but immediately revokes its selection
and restore authority; explicitly select a version to verify it again. Actual
identity changes still clear retired data, and Files/Changes inspection retains
its stricter stale-preview clearing. Real disconnection or an uncertain response
still requires reconciliation/review—no optimistic setting changes or automatic
application-level retries.

Tool calls use compact single-line disclosures with tool-kind icons and action
titles. Completed status stays accessible without a prominent success badge;
failures, running calls and unknown outcomes remain visible. Duplicate tool-name
summaries are omitted. Expansion shows only bounded, literal public output—not
private arguments or invented result cards. Saved calls retain their associated
message; runtime-only activity is not falsely assigned to a saved message.

Live updates use one read-only SSE connection to the existing RPC event
projection, sending coalesced full public snapshots about every 75 ms when
changed. This is native browser `fetch` streaming, not the HTMX SSE extension.
The stream is bound to the activation/session instance and does not start work.
Legacy backends without subscription support retain two-second snapshot polling
(the SSE endpoint returns 501); other stream failures never trigger polling
fallback. Updates do not rebuild the composer or clear a draft. An
activation-specific identity binds actions to the selected worker. At most two projects can be live, with one worker
per project. Busy ordinary prompts are rejected; only explicit **Queue next**
uses the worker's follow-up queue, with no automatic retries. Close the worker before
removing a live project; conversation switching now uses that same worker.

### Attach files and mention context in the web composer

Use the paperclip **Attach files** button, drop files onto the composer, or paste
an image. Supported attachments are UTF-8 text/source files and PNG, JPEG, GIF
or WebP images. Images require a vision-capable model. PDFs and other binary
formats are not supported. Each attachment appears as a compact removable chip;
images include a small local thumbnail, and long filenames are abbreviated with
the full name available on hover. The short disclosure below the chips reminds
you that Send forwards contents to the provider and saves them with the chat;
its hover text retains the full explanation. Failed or interrupted reads remain
visible and must be removed rather than silently omitted from Send.

The bottom action row keeps **Add context (+)** and the paperclip beside the
permission/mode controls. Opening **Add context** does not read files or discover
skills; choose a menu item to begin. Suggestions align with the composer width;
file rows are compact, and skill summaries stay on one line (hover for the full
description). Idle keyboard guidance remains available to assistive technology;
connection, request-outcome, and in-progress notices remain visible.

- **`@` project files:** type `@` or choose **Add context → Project files**, filter the current folder and
  select a directory to browse deeper. Selecting a file reads and attaches its
  complete bounded text, and inserts a quoted project-relative reference.
  Simply typing a path does not read it. The existing Files inspector's
  protected-path, symlink and identity checks apply; truncated previews cannot
  be attached. Listings scan 256 entries per page with a 4,096-entry cap.
- **`$` skills:** type `$` or choose **Add context → Installed skills** to search installed skill names.
  Select a suggestion to insert an exact `$name ` token; selection alone does
  not activate the skill or send a message. Disabled entries cannot be selected.
  A disabled runtime explains how to enable skills at its next explicit start.
- **Keyboard:** arrows move through suggestions, Enter picks a result, Escape
  dismisses them, and Ctrl/⌘+Enter retains explicit Send. Native text editing and
  IME composition are preserved.

Attachments stay in tab memory, independently for each project/conversation;
there are no disk uploads or automatic sends. Sending forwards their contents
to the chosen provider and saves them in conversation history. Review files for
secrets before sending. Draft attachments survive failed/unknown requests and
permission/question takeover; successful admission removes only the submitted
items. Reloading the browser loses unsent drafts. Limits are eight attachments,
2 MiB total image bytes, 64 KiB attachment text including labels, 64 KiB prompt
text, and a 4 MiB encoded request. Oversized or unsupported content is rejected
rather than truncated. **Queue next**, **Edit & resend**, and **Reuse** remain
text-only and are disabled while attachments are present.

Sent user messages display compact image previews, including when reopening a
saved conversation. Previews are fetched separately through the paired manager,
one at a time; image bytes are not included in general status or history
snapshots. The message remains visible while an image is pending or unavailable.
Unsupported, oversized, failed or timed-out previews show **Image unavailable**
and do not retry automatically. Image-bearing messages do not offer text-only
Edit/reuse actions. These previews require an updated manager and worker; a
browser reload alone cannot upgrade an already-running process.

Skills are off by default in web workers. To use them, close the runtime and
select **Enable installed skills** when explicitly starting it. The choice is
saved per project across manager restarts and preselects the checkbox on later
starts or resumes. Uncheck it to save a disabled preference, or use **Settings →
Workspaces → Installed skills**. Settings are shared by the manager's paired
browsers and affect future starts only, never a running worker. Archiving/removing
the project registration clears this preference.

Enabling skills admits the worker's normal installed-skill catalog, not only
the skill you mention: the model can activate applicable enabled skills too.
Project skills still require Snow's separate CLI extension trust; this option
does not grant it or alter tool permissions, plugins, MCP, or subagents.
The saved skill preference is independent of remembered project activation consent.
Conversation menus use compact, monochrome SVG icons with their existing text
labels; `$` and `@` remain visible as shortcuts in the Add context menu.

### Choose a model

Click the model selector to load the host's available provider/model pairs and
open the searchable list directly. There is no extra Load or Model submenu step.
Search locally by model name, model ID or provider; typing never sends a request
or changes the selected model. Choices are cached for the current runtime
instance. **Refresh models** explicitly reloads them; failures offer a retry,
and empty, partial or bounded inventories are labeled rather than fabricated.

Opening an uncached picker is explicit metadata work through the activated,
connected, idle worker and may contact provider discovery. Merely loading the
page does not discover models, start a second worker or select anything. The
existing discovery operation also loads saved conversations. The separate
conversation-switch menu still labels its initial discovery **Load conversations
& models** for the same reason.

Models are grouped by provider. Search keeps keyboard focus as choices arrive;
Arrow Down enters the results, Arrow Up from the first result returns to search,
and Escape closes the picker and returns focus to the model selector. Choosing a model applies that exact host pair
while idle—there is no separate Provider field or Apply form. Selecting the
current pair merely closes the menu. Selection affects the current conversation
without rewriting host configuration or the operator-owned project selection;
the effective thinking level appears in the separate context/usage panel. Menus
support keyboard navigation, Escape/back, click-away dismissal, and focus return.
Snow does not advertise unsupported Harness agent presets. Reasoning selection
is a separate explicit, capability-gated current-session control, not part of
model discovery or a promise of provider support.

The controls can create a new conversation, rename the current conversation,
or switch to a listed saved conversation without manually closing the runtime.
Switching away from active work requires explicit Stop-and-switch confirmation;
the manager waits for definitive completion before rebinding the session. Every
session change creates a new control identity, so another tab's stale prompts,
approvals or controls cannot target the new conversation. The worker retains its
selected provider/model when switching; restored goals remain deferred.

**Plan Mode** uses the existing RPC/core gates to restrict mutating tools, not a
browser-only toggle. Public proposed plans stream into the conversation and are
restored from saved history. Changing back to Default mode does not automatically
execute a plan or send a prompt. Mode/model changes and renaming require an idle
conversation. Token totals and context counts distinguish measured data from
estimates and unavailable telemetry.

Connection state is separate from agent work state. Reconnecting does not stop
admitted work, retry mutations, or silently adopt a replacement session. Review
an uncertain action's outcome before resubmitting it. Hidden tabs pause their
subscription; returning to the tab reconnects with a read-only GET and receives
a fresh full snapshot, with no POST replay. Transport errors retry with bounded
backoff; closed/replaced runtimes and expired/revoked browser access end that
subscription and require explicit review or sign-in. After a control POST,
mutation controls remain disabled until a fresh bound snapshot synchronizes the
state; an acknowledgment alone is not proof of current runtime state.
A canceled turn shows an explicit notice even if it returned no text; the notice
survives explicit resume and clears when another turn starts. Nothing retries
automatically. Send and Stop share a position, but the continuation of a pointer
multi-click on Send cannot cancel the newly started turn; an intentional single
Stop click or keyboard activation still cancels.
Drafts stay in browser
memory per conversation during workspace navigation/switching; they are not
written to browser storage and do not survive a full page reload or browser exit.

Live history retains up to 100 messages, 256 KiB total and 64 KiB per
message/prompt. Assistant text renders as sanitized Markdown, including lists,
tables and fenced code with copy controls. Raw HTML cannot execute; remote
images are not loaded. User text, file content, diffs and tool results remain
plain text. Omitted history is marked; close the live session to browse catalog
history.

The live tool timeline shows running, completed, failed, canceled, and unknown
outcomes for this runtime. Expand an operation to read its explicitly public
text result, when available. Saved tool calls and results also appear inside
their owning assistant messages when browsing or explicitly resuming a session,
including assistant messages with no text. Saved disclosures do not rebuild or
replay the live timeline. Both views exclude arguments, private display/plugin
metadata, thinking, and provider continuity. Each view keeps at most 64 tools
and 128 KiB of output, with an 8 KiB per-result cap and visible omission notices.
A recorded result without explicit public provenance says **Public output was
not recorded**; legacy content is never guessed or backfilled. An absent or
ambiguous result is **unresolved**, not running, failed, or canceled. Newly
recorded interruption repairs also remain unresolved: a bookkeeping error added
on resume does not establish whether the tool already changed a file. Bounded
reads can omit results; an unresolved display is not proof that execution never
happened. Tool output stays literal text and is excluded from Copy message.

Browser disconnects do not cancel admitted work. Stop requests cancellation;
Close and manager shutdown stop owned workers, but cannot prove that arbitrary
tool side effects were canceled or reversed. A failed worker must be explicitly
closed before replacement. Close takes you to the same saved session for review;
**Resume session** remains a separate, confirmed activation. If a crashed
session needs WAL recovery and the read-only catalog cannot display it, the
explicit resume form still retains that exact session ID instead of silently
creating a new conversation. Activation must independently verify the saved
session; inspect its reloaded history before sending new work. No prompt, tool
execution, approval, or answer is automatically replayed.

The private manager registry retains one small recovery hint per registered
project: the last verified saved-session ID, observed admission/completion state,
and timestamp. It contains no prompt, transcript, tool output, or usable old
instance authority. Prompt intent must be saved before dispatch; failure to save
it prevents sending the prompt. An acknowledgment establishes admission, **not**
durable user-message persistence. Missing acknowledgment means admission is
unknown; acknowledged work without observed completion remains interrupted or
uncertain. Later outcome writes are best effort, so a restart can conservatively
retain an older uncertainty warning. Definitive completion, failure, or
cancellation evidence is not downgraded by a later worker disconnect. Manager
restart retains saved sessions, browser access, and recovery hints, but starts
with **zero live workers**. Review the indicated saved conversation and activate
it deliberately. Recovery hints are navigation metadata, never execution
authority or an exactly-once guarantee.

HTMX and styles are embedded; no Node server or CDN is needed. The workspace uses
project/session navigation beside the conversation, with mobile drawers and a
mounted composer. The separate preview fixture remains explicitly static.

Use the inspector's **Files** tab to navigate the selected host project and
preview UTF-8 text; **Changes** lists staged, unstaged and untracked paths and
opens a selected patch or untracked text preview. These are read-only operations:
no runtime is activated, and no project file or original Git index is written.
Refresh the view to see new changes. On narrow screens, open the inspector drawer.

Directory pages scan at most 256 entries, up to 4,096 entries per directory.
Files must be regular, non-linked UTF-8 text without NUL bytes and at most
128 KiB; previews show up to 64 KiB. Symlinks, hard links, `.git`, all `.env*`
names (including examples), and common credential/key names are excluded.
This name policy is not secret detection; ordinary source files may contain
sensitive text.

Git runs with isolated temporary control metadata and a fixed configuration;
repository/global configuration, custom filters, text conversion and external
diff drivers are ignored. Its raw view can differ from a locally configured Git
client. It supports bounded conventional SHA-1 repositories; linked worktrees,
shallow repositories, alternates, unsupported/extended indexes and oversized
object layouts report **unavailable**, not a clean worktree. At most two Git
inspections run without queuing, with a five-second command/snapshot budget,
256 KiB status output and 64 KiB selected patch output. Git still reads the live
host worktree and object store with OS privileges; metadata checks are not a
filesystem sandbox against another process changing files concurrently.

Directory deletion, Git worktree forks, image history, automatic worker
recovery and remote HTTPS/mesh-VPN proxy support remain unimplemented.
Runtime/configuration CLI flags are rejected in web mode; choose
provider/model during explicit activation instead. The mode cannot be combined
with subcommands, and `--web-listen` is rejected outside web mode. Use Ctrl+C to
stop the foreground server.

Do not expose this preview through a proxy or tunnel. Only direct numeric-loopback
HTTP or explicitly configured local HTTPS is supported, with exact Host/Origin checks and no trusted forwarding headers.
Cookies are HttpOnly and SameSite=Strict, with Secure additionally set on HTTPS.
Local TLS does not enable remote deployment. Host-side folder selection does not
change this deployment restriction.

The web package uses public process/RPC clients, not runtime/session internals or
a second agent loop. Shutdown reaps direct workers, not arbitrary detached tool
descendants. See the [implementation plan](https://github.com/elmissouri16/snow-core/blob/main/docs/web-manager-implementation-plan.md)
for remaining phases. Worktree forks, remote access, browser OAuth, extension
enablement, general Git writes (commit/push/reset), a file editor, PTY, preview
fleet and plugin/MCP/skill/subagent controls remain out of scope. The local
additions above describe source behavior with focused verification; full new
end-to-end acceptance is still pending. They do not claim the installed/running
manager has been updated.

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
| `--mcp VALUE` | Add an explicit MCP server; repeatable |
| `--skill-dir PATH` | Add a trusted skills directory; repeatable |
| `--js-plugin <directory>` | Load a local JavaScript package for this launch; see [Plugins](plugins.md) |
| `--no-plugins`, `--no-mcp`, `--no-skills` | Disable an extension family |
| `--subagents`, `--no-subagents` | Override child-agent enablement |
| `--usage` | Print normalized usage after a print-mode prompt |

Use [Providers](providers.md) for authentication commands and
[Configuration](configuration.md) for persistent equivalents.

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

Use `/plugins` to enable or disable individual registered JavaScript plugins.
Type to filter, choose a plugin with ↑/↓, and press Enter for its details.
Use ↑/↓ and Enter for that plugin's actions; Escape returns to the list.
You can also type
`/plugins enable <id>` / `/plugins disable <id>`. Saved changes apply after
restarting Snow; disabled plugins remain in the list. See [Plugins](plugins.md)
for global/project scope and explicit launch-option behavior.

Use dedicated guides for setup. Common inspection commands are:

```sh
snow mcp list
snow mcp check NAME
snow skills list
snow skills get NAME
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
