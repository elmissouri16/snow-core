# Real incremental-stream browser regression

From the repository root:

```sh
node scripts/tests/browser/live-stream/run.mjs
```

Requirements: the checkout's Go toolchain, Node with built-in `WebSocket`
(Node 22+), and an installed Chrome/Chromium. Set `SNOW_CHROME_BIN` to an
explicit browser executable if it is not at one of the runner's fixed paths.
No dependency installation, provider credential, production Snow executable,
or external network service is required. Network traffic is loopback HTTP/CDP.

Unlike the exported-HTML browser suites, this suite uses **unmodified production
HTML, JavaScript, HTTP handlers, and streaming transport**. It does not override
`fetch`, manufacture snapshots, replace browser timers, or accelerate polling.
The execution path is:

```text
context-aware gated fake provider EventStream
  -> real app.New / agent / append-only session
  -> real rpc.New / Server.Serve in an external test-binary worker
  -> real RuntimeManager
  -> production HTTP + SSE and Markdown rendering
  -> real Chrome running the production browser assets
```

`cmd/snow/web_stream_fixture_test.go` contains all implementation-dependent
fixture code. No `internal/*` implementation import is added under `internal/web`
or `pkg/agentclient`. The test-only worker intercepts the exact manager-owned RPC
arguments before Go's testing flag parser. Catalog requests use the real
`rpc.CatalogMain`. The runtime uses `app.New`, a fake provider substitution, and
`rpc.New(...).Serve`. Because the production CLI's event-forwarding function is
private to `internal/rpc`, the fixture supplies only a serialized, bounded
transport bridge from the real agent subscription to stdout; it does not
implement an agent loop or an RPC command dispatcher.

## Coverage

The runner currently checks 87 assertions:

- The home composer accepts a draft, selects a workspace locally, and continues
  through explicit activation without sending a prompt. Add workspace preserves
  the draft through real registration/redirects and HTMX navigation; interactions
  wait for HTMX settlement before submitting newly inserted forms or navigating
  again after runtime closure.
- Activation transfers a startup draft into an empty composer without reloading.
  An existing draft is preserved; Use draft works only after explicitly clearing
  it. Drafts and activation form fields never enter navigation URLs.
- Registration lands on explicit `new=1` with an empty workspace-bound cold
  draft. Each workspace has an independent disclosure; expanding both preserves
  the center and URL, reads only session metadata, and never discovers models or
  activates a worker. A history-free workspace opens the empty conversation center.
- Draft-only sessions are explicitly renamed so normal empty-session cleanup
  does not remove them. Workspace-row New creates a second live session;
  its sidebar inventory link
  switches back to the first through the guarded runtime owner without replacing
  the center or sidebar. A pending startup draft never leaks into the second
  session, and the first session retains its own draft on return.
- Cross-workspace selection uses real HTMX: browse the other cold workspace,
  then select a non-current saved session under the first, still-live workspace.
  The browser first mounts that workspace's existing owner and restores its draft,
  then performs exactly one deliberate switch to the requested session. Both
  session drafts and the pending startup conflict remain intact; no second worker,
  model discovery, provider call, prompt send, or document reload occurs.
- Merely browsing, activating and switching never invokes the provider.
- A Markdown heading and partial fenced Go block arrive while the provider is
  blocked on an explicit first gate, before any second chunk or terminal event.
- Releasing the next gate joins the split `fmt.Pr` / `intln` token without
  duplication; the full cumulative answer is still **running** until a separate
  terminal gate is released.
- Chrome's CDP streaming-resource observation sees actual SSE snapshot frames,
  independently of the browser DOM and the read-only HTTP snapshot assertions.
- Stop cancels an actual context-blocked `EventStream.Next` and preserves the
  partial answer, without another provider call or replayed prompt POST.
- Cancellation before any text displays an explicit outcome instead of silently
  returning to Ready. Native single-click Stop still cancels; explicit subsequent
  prompts stream and complete on the same worker. Cancellation feedback survives
  explicit resume, and the composer remains reachable at 320×240.
- Native repeated pointer events at the original Send position must not cancel
  the new stream. Since the working footer can move Stop, a separate direct
  click-count-2 event targets Stop to verify its safeguard independently of
  geometry. Ordinary single-click Stop remains covered. These tests establish
  the UI behavior, not a past user's exact input sequence.
- A real connection stalls when **only the fixture manager** receives `SIGSTOP`.
  The unchanged production watchdog detects the missing heartbeat and aborts
  its fetch. `SIGCONT` lets the production retry open a new SSE request and
  receive subsequent gated chunks. No prompt POST is replayed. This deliberately
  takes approximately 25 seconds: Chrome's offline emulation does not reliably
  sever an already-open localhost streaming response.
- The provider emits an actual `ask_user` call; the real tool and RPC broker hold
  the turn pending the browser's answer. The public timeline shows the running
  tool, then its actual completed public result. The next provider request
  verifies that the authoritative tool-result text contains the selected answer.
- After closing the worker, opening its workspace defaults to the most recent
  saved conversation in the center, rather than a catalog detour. The production
  runtime-free history matches exact completed, canceled-partial, and tool-continuation
  assistant text. Close preserves the exact saved-session review destination.
  Persisted public tool output appears once inside its empty-text assistant
  owner. Explicitly resuming that session rotates instance authority, preserves
  tool identity/output, and restores no pending approval or live activity.
  The cold draft has explicit workspace/session identity and no form field name;
  explicit Resume transfers it without sending, retains both expanded workspace
  groups, and keeps the same browser document and conversation center surface.
  Provider-call and prompt-POST counts prove no read/resume prompt replay.

This covers a real user-input interaction, not a fabricated permission DTO.
This runner does not exercise a permission approval turn or claim coverage of arbitrary
network/browser failure classes. The separate [permission workflow](../permission-workflow/README.md)
checks actual builtin-write approval, denial, failure boundaries, and no replay. The fixture allowlists only `read` and
`ask_user`; process/mutation tools, plugins, MCP, skills, debug, and subagents are
disabled. The runtime uses `ask` so the production RPC interaction brokers remain
authoritative; no mutation tool is granted to the fake provider.

## Isolation and lifetime

The runner compiles a test binary into a mode-0700 temporary directory. It then
runs the equivalent of:

```text
SNOW_WEB_LIVE_FIXTURE_DIR=<private-temp-dir> <temp>/snow-fixture.test \
  -test.run=^TestWebLiveStreamFixture$ -test.count=1 -test.timeout=160s
```

This invocation is a private IPC contract, **not an interactive fixture server
command**: stdout includes a cookie-bearing ready frame that the runner consumes
without logging. The Go fixture uses the real loopback login endpoint internally;
the pairing code is never published, put in a URL, or written to screenshots.
The runner installs the resulting cookie in its isolated browser via CDP and
immediately drops it from its configuration object.

`HOME`, `SNOW_HOME`, project, registry, sessions, gate files, and Chrome profile
are temporary. Provider/auth/config environment variables are not inherited by
the fixture. Cleanup first resumes a possibly paused manager, closes its stdin,
and lets production `web.Run` close RuntimeManager and the worker. The runner has
an 85-second browser deadline, the manager/worker each have a 150-second lifetime,
and final cleanup has bounded termination waits. Compilation has a separate
120-second timeout. Nothing is installed in `~/.local/bin`.

Screenshots are written to the ignored directory:

```text
.snow/browser/live-stream/
```

They cover prefix-running, second-chunk-running, stopped partial, pending
question, completed tool, saved history, and explicit same-session resume. A failure screenshot is also saved.
No browser cookie, pairing code, raw provider request, or private continuity data
is included in the artifacts.

Fast Go-only fixture checks (no browser):

```sh
go test ./cmd/snow -run '^TestWebStreamFixture' -count=1
```

The long-lived fixture skips unless its explicit environment is set, so ordinary
`go test ./...` remains finite and network-free.
