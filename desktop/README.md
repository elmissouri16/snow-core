# Snow Desktop

Snow Desktop is the native Rust/[GPUI](https://gpui.rs/) presentation client
for Snow. It runs as an independent application under `desktop/` and starts one
external `snow --mode rpc` process. It does not embed, link, or reimplement the
Go agent loop: the TUI, desktop, print/JSON/RPC modes, and SDKs all observe the
same serial Snow runtime and normalized event stream.

Development is macOS-first, with Linux amd64/arm64 build and relocatable archive
support on Linux hosts. Snow Desktop and Snow RPC are alpha surfaces; package a
reviewed Snow binary from the same checkout and retain the protocol capability
gate.

## What is implemented

The desktop surface currently provides:

- a long-lived, supervised Snow RPC child with bounded JSONL framing, startup,
  stderr, shutdown, and force-stop fallbacks;
- a persistent two-pane workspace with a collapsible current-project/thread
  sidebar, compact project/session/branch toolbar, state-aware composer
  placement, and variable-height virtualized conversation rendering so large
  restored sessions do not eagerly lay out every message; bounded history pages
  are applied incrementally on the UI thread, and changing visible rows are
  explicitly remeasured and clipped to the transcript viewport;
- streaming Markdown assistant responses, syntax-highlighted code, compact
  clipboard-icon copy controls backed by the embedded GPUI Component SVG asset
  bundle registered at application startup, root/child activity, tool progress, abort, and definitive
  `prompt_completed` handling;
- normal prompts, active-turn steering, queued follow-ups, Default/Plan mode,
  plan nudges, and the plan-to-implementation boundary; the composer remains
  editable while a new or opened thread restores, while submission stays gated
  until that runtime state is complete;
- request-correlated trusted cards for ask-mode tool permission and
  model-requested user input; permission scope supports once, session, always,
  and deny;
- provider/model/thinking selection through bounded controlled popovers, bounded
  manual model IDs, and the model's available reasoning-summary and
  text-verbosity controls;
- multimodal prompts from bounded image files or clipboard images, with pending
  attachment review/removal before send;
- fixed-height uniform virtualized project session inventory in both the
  persistent sidebar and detailed management panel, with one bounded render
  pass per visible range for smooth local scrolling, create/open/confirmed delete, durable relaunch
  continuity, session and branch rename, branch switch/fork/delete, and the remaining
  focused branch-lifecycle coverage tracked in [`PARITY.md`](PARITY.md);
- provider authentication and logout using Snow's canonical auth backends,
  including named profiles, masked secret input, browser/device-code progress,
  and independent copy actions for URLs and user codes;
- a native settings surface for provider/model, thinking, reasoning summary,
  text verbosity, permission mode, diagnostics, subagent concurrency, Snow theme
  selection, native appearance, and global/trusted-project semantic keybindings;
- trust and project initialization, goals, usage, compaction, context reports,
  diagnostics, managed processes/logs, subagents, MCP status, and Agent Skills
  through desktop-native panels or typed desktop slash commands;
- searchable slash-command, file-mention, and Agent Skill completion through one
  bounded controlled composer popover, plus `/help` for the current desktop
  command/shortcut catalog.

Provider-private continuity data and private thinking are never placed in the
desktop transcript. Restored history is surface-filtered and retains typed
Markdown, plans, images, and paired tool call/results. Consecutive tool-only
segments collapse into one quiet activity disclosure by default; expanding it
shows lightweight bounded-summary rows with icon-only copy controls, while each
row's fenced input/output details remain in a fixed-height internal scroller so
large payloads cannot change neighboring transcript-row geometry. The remaining presentation
refinements are tracked explicitly in
[`PARITY.md`](PARITY.md), rather than being implied by the RPC transport alone.

## Architecture

```text
GPUI Window + Workspace entity
        |
        | bounded RuntimeEvent channel
        v
Rust SnowClient
        |
        | private stdin/stdout JSONL pipes
        v
external snow --mode rpc process
```

Dedicated workers own child stdin, stdout, stderr, and exit supervision. The
GPUI thread never blocks on child pipes. The reader requires `rpc_ready` as the
first non-empty stdout frame, validates protocol v1 and every capability needed
by the desktop, and enforces a 16 MiB frame limit. Unknown future events become
bounded diagnostics instead of crashing the process.

Snow RPC is Snow-specific JSONL, not JSON-RPC 2.0. See
[`../docs/rpc.md`](../docs/rpc.md) and the v1 schemas under
[`../pkg/protocol/schema/rpc/v1`](../pkg/protocol/schema/rpc/v1).

## Requirements

- macOS 12+ or a supported Wayland/X11 Linux desktop;
- a current Rust toolchain and GPUI platform build dependencies;
- an existing compatible `snow` executable for development or bundling;
- Python 3.9+ and `file` for packaging; and
- provider credentials only when using a hosted provider.

GPUI's `runtime_shaders` feature avoids a dependency on Xcode's optional
standalone Metal Toolchain. The normal macOS frameworks/Xcode command-line
components are still required. Linux dependencies are listed in
[`packaging/README.md`](packaging/README.md).

## Run locally

Build Snow once at the repository root, then start the desktop client:

```sh
go build -o ./snow ./cmd/snow
cd desktop
SNOW_BINARY=../snow SNOW_PROJECT=.. cargo run
```

The initial process still defaults to Snow's internal `fake` provider, which
exercises the complete process, session, prompt, and completion lifecycle
without credentials or network traffic. `fake` remains a supported
credential-free test runtime; only its desktop presentation is hidden. While it
is active, the footer uses neutral **Choose provider** and **Choose model**
labels, and no `fake` provider or model row appears in the desktop pickers.

The desktop provider picker is built from Snow's authoritative, bounded
`auth_providers` inventory. An active unknown non-test/custom provider remains
visible by its provider ID even when it is absent from that inventory; the
presentation exception applies only to the exact canonical ID `fake`.

To use an authenticated provider:

```sh
cd desktop
SNOW_BINARY=../snow \
SNOW_PROJECT=.. \
SNOW_PROVIDER=opencode-go \
cargo run
```

Use Snow's auth store, environment-based provider configuration, or the native
`/login` flow. Never put API keys in command-line arguments.

### Environment

| Variable | Meaning |
| --- | --- |
| `SNOW_BINARY` | Snow executable; defaults to `../snow` when present, then `snow` on `PATH`. |
| `SNOW_PROJECT` | Authoritative project working directory; defaults to the desktop process CWD. |
| `SNOW_PROVIDER` | Initial provider ID; defaults to `fake`. |
| `SNOW_MODEL` | Optional initial model override. |
| `SNOW_SESSION` | Exact SQLite session path to reopen. |
| `SNOW_NO_SESSION` | Any non-empty value except `0` requests an ephemeral run. |
| `SNOW_PERMISSION` | Explicit process override: `ask`, `allow`, or `deny`. |
| `SNOW_THINKING` | Explicit process override: `off`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, or `ultra`. |

The desktop **does not** force a permission mode, thinking level, or feature disable flags. In the absence of `SNOW_PERMISSION` or `SNOW_THINKING`, Snow's
canonical configuration remains authoritative. The explicit overrides are
validated before child launch.

Snow normally creates or resumes a private SQLite session. After `rpc_ready`,
the desktop requests canonical session metadata and pins the exact session path
across provider-process replacements. `SNOW_NO_SESSION=1` is intentionally
non-durable.

## Workspace layout

The primary window is a Snow workspace, not a single-column chat shell. An
expanded 296 px sidebar remains beside the main canvas across empty,
conversation, and ordinary management-panel states; it can collapse to a 58 px
rail without changing runtime or session ownership. Settings are the deliberate
exception: while open, their dedicated navigation rail and content page replace
the conversation workspace completely. The sidebar represents only the current
`SNOW_PROJECT`, obtains its bounded thread list from Snow's canonical
`sessions_list` projection, highlights the active session, and shows real
message-count/update metadata. It is deliberately not a multi-project manager.

“New thread” invokes `session_create`, and selecting an inactive thread invokes
`session_open`. The sidebar's **All** action opens the existing detailed session
panel, where create, open, and confirmed delete remain available. The compact
main toolbar shows a `project / session / active branch` breadcrumb plus real
Default/Plan, initialize, new-thread, sidebar, and session/branch actions. The
session/branch popover continues to own rename, switch, fork, and leaf-delete
workflows; session changes remain gated while Snow is busy or a trusted
interaction is blocking.

A new, unblocked thread centers the project-specific empty-state question and
the one shared composer in the main canvas. As soon as conversation content, a
blocking card, or an ordinary workspace panel exists, the transcript takes the
flexible middle and the same composer moves to the bottom. Settings instead
replace the entire chat canvas and do not render the transcript or composer.
User messages are compact,
right-aligned bubbles; assistant Markdown is left-aligned and unboxed. Copy
actions render as compact clipboard icons with hover tooltips instead of text
labels. Streaming, code, tool/history, plan, attachment, error, permission, and
user-input affordances retain their existing behavior and remain visually
secondary to the conversation.

This is a presentation recompose, not a new Snow product or agent path. The
sidebar, toolbar, and composer call the existing typed RPC/session handlers;
Snow's one-project runtime, serial prompt lifecycle, permission gates,
append-only session behavior, semantic theme palette, and product identity are
unchanged. No reference-product branding, fake projects, decorative dead
actions, second runtime, or second session database was added.

## Native workflows and slash commands

The header and composer expose provider, model, thinking, mode, session/branch,
settings, stop, and send controls. Enter sends a normal prompt or steers an
active turn. Option+Enter inserts a newline while idle and queues a follow-up
while a turn is active. Escape closes transient pickers. The header’s current
session control opens a bounded anchored card for rename and branch actions;
the card overlays the canvas instead of resizing the transcript or composer.

Provider, model, and thinking choices are bounded controlled popovers anchored
to their existing footer triggers. Input-driven slash-command, file-mention,
and Agent Skill choices share one bounded controlled popover anchored to the
existing composer input, with only the applicable suggestion surface shown.
These transient popovers overlay the canvas: opening or closing them does not
add a composer row, resize the conversation or composer, or move the persistent
error and activity rows that remain in normal layout flow.

Typing `/` opens searchable completion. Command names and descriptions render
as aligned columns with an explicit selected row. Successful RPC commands are
decoded into their bounded native panel, desktop state, or compact status;
generic response JSON is never appended to the conversation transcript.
Important command families include:

- `/plan`, `/default`, `/goal`, `/compact`, `/context`, and `/usage`;
- `/sessions`, `/new`, `/resume`, `/tree`, and `/fork [worktree]`;
- `/model`, `/thinking`, `/settings`, and `/permissions`;
- `/login`, `/logout`, `/trust`, and `/init`;
- `/agent`, `/processes`, `/mcp`, and `/skills [clear]`;
- `/attach`, `/paste-image`, `/attachments`, and `/detach`;
- `/debug`, `/allow [once|session|always]`, `/deny`, `/help`, and `/quit`.

`/help` is generated from the desktop command catalog and is the authority for
exact argument forms in the current checkout.

### Authentication

`/login [provider]` obtains Snow's provider/method catalog. Secret methods use
masked input; browser/device methods show bounded progress. URLs and device
codes have separate copy actions so a user code cannot be hidden behind a URL.
Named profiles are forwarded to Snow and stored by Snow's existing atomic,
mode-`0600` auth machinery. `/logout [provider]` removes the selected stored
credential only after Snow confirms the correlated request.

### Settings and runtime resources

`/settings` opens a dedicated responsive settings workspace rather than a panel
inside the conversation. Its General, Capabilities, Appearance, and Keybindings
navigation replaces the project/thread sidebar, transcript, and composer until
settings are closed. Model selection has a settings-local controlled popover so
it remains available without mounting a second composer.

Snow still reads canonical runtime settings rather than maintaining a desktop
copy. Model, thinking, response controls, permission mode, diagnostics, and
subagent concurrency use the existing typed RPC updates; Snow reports whether a
restart is required. The same workspace loads Snow's bounded theme catalog,
persists theme selection through Snow, and applies its adaptive semantic
palette. A separate native **System / Light / Dark** preference either follows
operating-system appearance changes or pins the chosen mode.

The settings workspace also loads Snow's effective semantic keybinding inventory.
Bindings can be replaced or reset globally and, for a trusted project, at the
project layer; validated changes replace the GPUI keymap at runtime. The
`/keybindings` command opens these controls, while `/help` renders the effective
shortcut catalog. This does not imply exact shell-alias or subcommand-completion
parity, which remains tracked separately. `/agent`, `/processes`, `/mcp`, and
`/skills` show bounded, surface-safe state. Process logs and child-agent details
remain treated as untrusted model/tool output.

### Images

`/attach <path>` and `/paste-image` add a PNG, JPEG, GIF, or WebP image to the
next prompt. Files, decoded dimensions, per-image bytes, aggregate bytes, and
count are bounded before RPC serialization. Attachments remain visible and can
be removed before send; they clear only after successful prompt admission.
Clipboard helper details for Linux are in the packaging guide.

## Security and privacy

Snow Desktop is **not** a sandbox. Snow, Bash, plugins, stdio MCP servers, and
subagents run with the user's OS privileges. External containment remains the
operator's responsibility.

The desktop does not put credentials in process arguments or logs and does not
log the inherited environment. Ask-mode is fail closed: permission and user
input requests are typed trusted host state, never transcript text; replies are
correlated by both host command ID and Snow request ID. Malformed, stale, or
excess interactions are rejected and may stop the active turn.

History restoration renders only public content. Provider-private continuity
blocks and private thinking are excluded. Image/tool payloads, diagnostics,
stderr, model output, project text, MCP/plugin output, and child-agent output
remain potentially sensitive and prompt-injected; all displayed copies are
bounded.

The process supervisor uses private pipes, bounded channels, bounded stderr,
strict `rpc_ready` validation, timeouts, and orderly reaping. Package
compatibility checks run a fake-provider Snow process in a private temporary
home with a minimal environment and require all desktop RPC capabilities.

Read [`../docs/security.md`](../docs/security.md) before granting broader tool
authority or distributing a package.

## Development verification

From `desktop/`:

```sh
cargo fmt --check
cargo check --all-targets
cargo test
cargo clippy --all-targets -- -D warnings
```

Build the repository Snow binary and run real-process network-free conformance:

```sh
go build -o ./snow ./cmd/snow
cd desktop
SNOW_TEST_BINARY="$PWD/../snow" \
  cargo test --test rpc_integration -- --ignored --test-threads=1
```

Focused controlled-state and layout-projection tests cover the composer
popovers. They are executable implementation evidence, not a claim that native
rendered screenshot comparison was performed.

Packaging tests and a relocatable archive check:

```sh
python3 -m unittest discover \
  -s desktop/scripts/tests -p 'test_*.py' -v
python3 desktop/scripts/package_desktop.py \
  --platform linux --arch amd64 \
  --snow-binary ./snow \
  --desktop-binary ./desktop/target/release/snow-desktop \
  --output ./desktop/dist
python3 desktop/scripts/verify_desktop_archive.py \
  ./desktop/dist/snow-desktop_0.1.0_linux_amd64.tar.gz
```

The non-publishing desktop workflow at
[`.github/workflows/desktop-ci.yml`](../.github/workflows/desktop-ci.yml) runs
Linux Rust checks, packaging tests, an actual cargo build, and archive
verification. It never tags, signs, notarizes, uploads, or publishes.

## Current limitations

The remaining audited presentation work is deliberately explicit:

- historical public image and tool-display blocks are bounded and decoded, but
  their rich transcript cards are still being integrated;
- branch rename/delete RPC and native controls exist, while focused native
  lifecycle coverage remains in progress;
- managed-process, subagent, and diagnostic panels do not yet match every TUI
  live-polling, filter, and detail view;
- safe model privacy/limit/vision/pricing metadata is decoded, but the full
  native metadata card is not complete;
- exact auto-growing editor behavior and one-click whole-response copy parity
  remain; and
- there is no official publishing, notarization, distro repository, package
  manager, or automatic-update channel.

These items are evidence-tracked in [`PARITY.md`](PARITY.md). Generic RPC
transport support is not treated as completed native parity.

## Packaging and release status

See [`packaging/README.md`](packaging/README.md) for reproducible macOS `.app`
ZIP and Linux tar archive commands, input validation, manifests, checksums,
signing boundaries, and local verification.

There is no official desktop release/update channel yet. The package script is
a local release-candidate path only. Repository-managed notarization,
distro-native Linux packages, package-manager feeds, automatic updates, and
arbitrary third-party plugin management remain out of scope. The root guide
currently records the existing main CI/release workflows as manually disabled;
do not publish any desktop artifact while required repository gates are
disabled or red.

The maintained TUI-to-desktop evidence and remaining integration work are in
[`PARITY.md`](PARITY.md).
