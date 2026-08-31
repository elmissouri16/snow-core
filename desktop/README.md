# Snow Desktop

Snow Desktop is a basic macOS-first [GPUI](https://gpui.rs/) client for the
local Snow coding-agent runtime. It is an independent Rust application under
`desktop/`; it is not part of Snow's Go module and does not embed or duplicate
the agent loop.

The current milestone intentionally stays small:

- starts one long-lived `snow --mode rpc` child process and safely restarts it
  when the user picks another provider;
- keeps a normal Snow SQLite session, pins its canonical path across provider
  restarts, and restores surface-safe text history through RPC;
- loads the active provider's model catalog, offers searchable provider/model
  controls and bounded manual model IDs, and switches models without restarting
  Snow;
- validates the Snow JSONL RPC v1 handshake and requires both trusted
  interaction capabilities;
- submits one root prompt at a time;
- resolves ask-mode tool permissions and model-requested questions through
  separate trusted, request-correlated cards;
- streams root assistant text and basic tool status;
- distinguishes prompt admission, `turn_done`, and definitive
  `prompt_completed` status;
- aborts the active turn through RPC;
- reports bounded startup, protocol, stderr, and process errors;
- closes stdin, waits, and force-stops Snow only when orderly shutdown times
  out.

Snow Desktop currently targets macOS arm64. The process and protocol layers use
portable Rust APIs so Linux support can be added later.

## Architecture

```text
GPUI window and Workspace entity
        |
        | bounded RuntimeEvent channel
        v
Rust SnowClient
        |
        | JSONL over private stdin/stdout pipes
        v
external snow --mode rpc process
```

The GPUI thread never reads or writes child-process pipes. Dedicated workers own
Snow stdin, stdout, stderr, and exit supervision. The stdout reader enforces a
16 MiB frame limit and requires `rpc_ready` to be the first non-empty stdout
frame. Unknown future event types are retained as bounded diagnostics instead
of crashing the UI.

Snow RPC is Snow-specific JSONL, not JSON-RPC 2.0. The canonical protocol is in
[`../docs/rpc.md`](../docs/rpc.md), with machine-readable v1 schemas under
[`../pkg/protocol/schema/rpc/v1`](../pkg/protocol/schema/rpc/v1).

## Requirements

- macOS on Apple Silicon for the initial milestone;
- a current Rust toolchain;
- an existing compatible Snow executable;
- provider credentials only when selecting a real hosted provider.

The GPUI dependency enables its `runtime_shaders` feature so local builds do not
require Xcode's optional command-line Metal Toolchain component. macOS still
requires the normal platform frameworks used by GPUI.

## Run safely with the fake provider

From the repository root, an existing `./snow` binary can be used without
rebuilding Snow:

```sh
cd desktop
SNOW_BINARY=../snow SNOW_PROJECT=.. cargo run
```

The desktop client defaults to:

```text
--provider fake
--permission ask
--thinking off
--no-plugins
--no-mcp
--no-skills
--no-subagents
```

Snow creates a normal private SQLite session. After the initial `rpc_ready`, the
desktop requests `session_info`, remembers Snow's canonical session path, and
passes that exact path to replacement provider processes. Set
`SNOW_NO_SESSION=1` for an explicitly ephemeral run, or set `SNOW_SESSION` to a
specific database when reopening the same conversation in a later desktop
process.

The fake provider verifies the complete process and prompt lifecycle without
credentials or network traffic. It normally emits `turn_done` and
`prompt_completed` without assistant text, so the user prompt remains visible
and the composer returns to Ready.

## Run with a configured provider

Use Snow's existing authentication store or environment-based provider
credential. Do not put API keys in desktop command-line arguments.

For example, after authenticating Snow normally:

```sh
cd desktop
SNOW_BINARY=../snow \
SNOW_PROJECT=.. \
SNOW_PROVIDER=opencode-go \
cargo run
```

The prompt composer’s searchable provider picker can switch between Fake,
OpenCode Zen, OpenCode Go, ChatGPT, and the legacy OpenAI-compatible profile.
Search matches labels and provider IDs. Authentication and endpoint
configuration still belong to Snow; the desktop never reads or writes provider
credentials. OpenCode Zen is the easiest streaming proof because its maintained
free catalog supports anonymous use when available.

## Environment variables

| Variable | Default | Purpose |
|---|---|---|
| `SNOW_BINARY` | `../snow` when present, otherwise `snow` on `PATH` | Existing Snow executable to launch |
| `SNOW_PROJECT` | Desktop process working directory | Project directory assigned to the Snow subprocess |
| `SNOW_PROVIDER` | `fake` | Initially selected provider; the composer picker can restart Snow with another built-in provider |
| `SNOW_MODEL` | provider default | Initial model ID; the model controls can change it through RPC |
| `SNOW_SESSION` | new Snow-managed session | Explicit SQLite session path to create or resume; use this to reopen the same conversation after relaunch |
| `SNOW_NO_SESSION` | unset | Set to any value except `0` for an ephemeral in-memory session; this takes precedence over `SNOW_SESSION` |
| `SNOW_TEST_BINARY` | none | Existing binary used by the ignored real-process integration tests |

When running `cargo run` from `desktop`, set `SNOW_PROJECT=..` to make the
repository root the agent project rather than the desktop subdirectory.

## Workspace and controls

The desktop uses one conversation-first canvas. A compact top bar identifies
Snow, the current project, and connection state; the centered transcript owns
the window; and one anchored prompt surface contains composition plus the
provider, model, and Send/Stop controls. Tool activity appears contextually
above the prompt only after a tool has run.

- The composer **Provider** picker searches Fake, Zen Free, OpenCode Go,
  ChatGPT, and Compatible by display label or provider ID. In both searchable
  pickers, Up/Down moves the highlighted row, Enter selects it, and Escape
  closes the popover. Switching is
  available only while idle, shuts down and reaps the current Snow process
  before starting the new one, resumes the exact active session, and replaces
  the visible transcript with restored surface-safe history. If startup fails,
  the old transcript is retained and the selected provider can be retried.
- The searchable composer **Model** picker shows the active provider's live
  Snow catalog and matches display name, model ID, or provider. When discovery
  is empty or has no exact ID match, a trimmed manual ID of at most 256
  characters can still be submitted. A discovered selection uses `set_model`,
  does not restart Snow, and includes a compatible thinking level atomically,
  falling back to the model's advertised default or Off when the previous level
  is unsupported. Manual IDs are selected atomically with thinking Off because
  no discovery metadata is available. Model changes remain disabled while a
  prompt or another model change is active.
- The composer **Thinking** picker sits immediately to the right of the model
  picker and shows only the active model's advertised effort levels. Off is
  always available. An off-only model keeps the current setting visible but
  disables the picker; accepted changes apply to subsequent prompts.
- The top-bar **session/branch** control shows the current session title and
  active branch without adding a permanent sidebar. Its transient menu can
  rename the active session, switch among branches in that session, or fork the
  current branch at its tip. All mutations are idle-only and correlated to
  their exact RPC responses; branch changes restore session metadata, history,
  models, and the complete branch catalog before prompt actions are re-enabled.
  Snow RPC does not yet expose independent-session enumeration or switching, so
  this control is deliberately not presented as a multi-session picker.
- Tool rows show bounded recent execution status only when activity exists.
  They do not render historical tool output or provider-private continuity
  data.
- **Send** or **Enter** submits the composer text when connected, restored, and
  idle. The prompt input grows from one to six rows; secondary Enter remains
  available for a line break.
- A tool authorization request appears in a trusted card above the composer,
  never as transcript content. The card shows bounded tool, risk, reason, path,
  and argument details and offers **Allow once**, **Allow for session**,
  **Always allow**, and **Deny**. It stays visible in a submitting state until
  Snow acknowledges the exact correlated command.
- Model-requested input uses a separate trusted card with one question at a
  time, option descriptions, an explicit Other/free-form path, preserved
  per-question drafts, previous/next navigation, and ordered answer validation.
  Declining the form rejects only the model-input request and never authorizes a
  tool. One additional interaction may queue; malformed or further overlapping
  requests fail closed by stopping the turn.
- While a turn is active or blocked on either trusted interaction, **Stop**
  replaces Send and issues one Snow RPC `abort` command. Repeated abort requests
  remain gated until terminal completion; ordinary prompt and configuration
  actions remain disabled while an interaction is pending.
- Closing the final window synchronously performs bounded Snow shutdown during
  application teardown; timeout fallback force-stops and reaps the child before
  the desktop process exits.

The current proof uses a plain-text auto-growing composer and restores only
user and assistant text/plan blocks. Assistant text is rendered as selectable
GitHub-flavored Markdown with syntax-highlighted fenced code and per-block Copy
actions; streamed deltas update the same Markdown surface progressively. User
messages remain literal text. Rich historical tool cards, images, private
thinking blocks and an independent-session picker remain deferred.
Provider-private continuity data is never rendered. The compact header derives
its project label from the authoritative working directory returned by
`session_info`.

## Development checks

Run entirely from this directory:

```sh
cargo fmt --check
cargo check
cargo test
cargo clippy --all-targets --all-features -- -D warnings
```

Run the network-free real-process conformance smoke check against an existing
Snow binary:

```sh
SNOW_TEST_BINARY=../snow cargo test --test rpc_integration -- --ignored
```

Manual GPUI smoke check:

```sh
SNOW_BINARY=../snow SNOW_PROJECT=.. SNOW_PROVIDER=fake cargo run
```

Expected behavior:

1. the window opens as one conversation canvas with a compact Starting status bar;
2. the status bar changes to Connected without duplicating provider/model metadata;
3. provider and model pickers remain attached to the prompt surface, open above
   it, and filter as text is entered;
4. a missing discovered model can be entered as a bounded manual model ID;
5. entering a prompt adds a right-aligned user message in the conversation;
6. fake-provider completion replaces Stop with Send and returns the composer to Ready;
7. permission and model-input fixtures render separate trusted cards above the
   composer, retain the card while submitting, and keep Stop available;
8. tool rows remain absent until activity occurs and then appear above the prompt;
9. the top-bar session control opens a transient current-session branch menu;
10. session, provider, model, and thinking changes remain disabled during
    prompts and trusted interactions;
11. closing the window leaves no Snow child process.

## Security

Snow Desktop's subprocess boundary improves lifecycle management; it is **not a
sandbox**. Snow, Bash tools, plugins, MCP servers, and subagents run with the
user's operating-system privileges when enabled.

This milestone uses `--permission ask` and disables plugins, MCP, skills, and
subagents. Startup requires both `permission_interaction` and `user_input`.
Permission and model-input requests are decoded into separate typed host state,
never transcript messages; replies are correlated by host command and Snow
request IDs, and controls stay disabled until the exact acknowledgement. A
malformed, stale, dismissed, or excess request is rejected or terminates the
transport fail closed rather than authorizing or silently dropping it. Model
questions can never resolve tool authorization.

The stderr reader drains diagnostics into a bounded event channel. The client
does not log the inherited environment or put provider credentials in process
arguments. Durable mode stores prompts and responses in Snow's private SQLite
session storage; use `SNOW_NO_SESSION=1` when that persistence is not wanted.
History restoration intentionally renders only surface-safe text/plan content
and excludes provider-private continuity blocks. Project content, model output,
tool output, restored history, and stderr should still be treated as
potentially sensitive.

Read Snow's full [`security model`](../docs/security.md) before expanding tool
authority.

## Current limitations

The basic client does not yet provide:

- login, named-profile discovery/enumeration, arbitrary provider-ID entry, or
  provider credential/configuration UI (the searchable built-in picker uses
  Snow's existing configuration);
- an independent-session picker, automatic last-session reopening across app
  launches, or rich historical tool/image rendering (`SNOW_SESSION` provides
  explicit relaunch continuity; current-session branch controls are available);
- image/attachment rendering or a richer multiline composition editor;
- steering, follow-ups, goals, Plan Mode, or subagents;
- plugins, MCP, or Agent Skills;
- binary bundling, signing, notarization, or updates;
- Linux packaging.

Snow and its RPC protocol remain alpha surfaces. Bundle or select a tested Snow
binary and continue validating `protocol_version` and capabilities at startup.
