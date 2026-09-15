# Real production-manager browser workflows

```sh
node scripts/tests/browser/manager-workflows/run.mjs
```

This is a production integration gate, **not an exported-HTML fixture**. It
compiles and launches `cmd/snow`'s opt-in `TestWebLiveStreamFixture`, which runs:

```text
native Chrome -> production HTTP/assets/SSE -> RuntimeManager -> real RPC worker
              -> production app/agent/session -> gated local fake provider
```

No JavaScript DOM/fetch/EventSource/timer mock, intercepted response, auxiliary
asset server, or manually projected runtime snapshot is used. Native CDP pointer
clicks, Tab/Enter, text insertion, and selection operate the real UI. Evaluations
inspect DOM or perform read-only production snapshot GETs; the pointer helper
scrolls actual controls into view. Only provider release gates are simulated,
using the existing Go fixture's private filesystem handshake.

## Requirements and isolation

- Go toolchain required by this checkout, Node 22+ with native WebSocket,
  Python 3 (standard-library SQLite only), installed Chrome/Chromium.
- `SNOW_CHROME_BIN` selects the browser executable.
- Default matrix: 320×740 and 1280×740, dark/light. Each report launches a fresh
  isolated real manager, worker, session store, home, and browser profile.
- Narrow run:

  ```sh
  SNOW_MANAGER_WORKFLOWS_WIDTH=1280 SNOW_MANAGER_WORKFLOWS_THEME=dark \
    node scripts/tests/browser/manager-workflows/run.mjs
  ```

The manager binds its own `127.0.0.1:0` listener. Pairing uses real HTTP login;
the private cookie-bearing startup IPC frame is consumed without logging. Only
the isolated fake provider and safe `read`/`ask_user` tools exist in the worker.
No user environment credentials/config are forwarded. The existing Go fixture
sets private `HOME`, `SNOW_HOME`, and sessions roots before starting the manager.
Port 7331, real providers, user sessions/configuration and installations are never
touched. Worker, manager, Chrome and temporary state are shut down/removed.

## Queue production gate

- Actual `generated/app.js` and `queue.css` HTTP responses must be 200. This catches
  BUG-149, which exported-file servers missed by bypassing the production asset
  allowlist. `SnowQueue` must initialize and real CSS must constrain panel scroll.
- Native activation creates a real RPC worker without starting a provider turn.
- Native root Send starts one gated provider call. Native Queue next submits one
  exact queue POST, not another prompt. Pending text is separate from transcript
  and absent from durable user history until actual agent delivery.
- Releasing root completion delivers the exact queued text through the existing
  root loop. Instance/session remain unchanged; no additional `/prompt` is sent.
  User source appears once in runtime and durable SQLite history with ancestry.
- Read-only reload cannot replay consumed queue or provider work.
- A second real root is queued then stopped. Actual backend retains undelivered
  text as `held`, not persisted user history. Reload and explicit Copy to draft
  neither enqueue nor start a normal prompt/provider call.
- Production asset behavior, capacity styling, and native controls are checked
  at both widths/themes. HTTP/SSE observation and durable reads are bounded.

`history.py` opens only this runner's private fixture session databases with
SQLite `mode=ro` and `query_only`. It returns bounded text/ID/ancestry summaries,
not provider-private blocks or credentials. This inspects actual storage rather
than treating a transport fixture's archive copy as persistence evidence.

## Real Activity and organization navigation

`navigation.mjs` uses actual manager links, native mobile navigation controls,
HTTP summary reads, registration forms, and backend persistence:

- Label save, pin/unpin, explicit archive/restore of this runner's own inactive
  registration. Exactly five manager mutations, zero provider requests; original
  registration ID and directory inode/device survive archive/restore. Each native
  POST/redirect must replace the prior document and reach complete/React-ready
  painted state before the next pointer action. Selector presence alone is not
  readiness: pending load/scroll restoration previously redirected an intended
  Archive disclosure click onto the workspace heading link (BUG-207).
- Real Activity while the root is running with pending input, and after Stop with
  held review input. Correct project/session links and counts, host-versus-browser
  status, no transcript/control-authority leakage, read-only refresh/navigation.
- Native return from Activity rejoins the same live session without prompting,
  activating, switching workers, or replaying queued text.
- Live Organization shows its close-before-organizing guard, not saved-session
  archive/pin controls; returning retains the exact session.
- Both surfaces remain within the viewport at both matrix widths/themes.

## Real Versions and historical edit

`versions.mjs` extends the same live session after queue/Activity/organization:

- Explicitly removes the held review item, then lists and previews the current
  durable branch. Actual `generated/app.js`/`versions.css` must return HTTP 200.
- Native historical Edit & resend creates a real second branch and a replacement
  worker. The fixture's globally unique `call-4` proves exactly one execution with
  the edited source; only that fresh call's release gates can finish it.
- Sets the current permission policy to Deny through the native menu, then keeps
  an unsent composer draft and keyboard-created selection for restore checks.
- Lists/previews the original branch without replacing the edited transcript or
  executing work. Current branch/tip identities match actual durable SQLite state.
- Exercises explicit restore prepare, Cancel, a fresh prepare and one confirmation
  commit. The commit carries only its exact typed authority fields, never text.
- Restores the original branch into a fresh idle worker in the same session,
  preserving provider/model, collaboration mode, the changed permission policy,
  thinking settings, and the exact unsent draft/selection.
- SQLite returns to the original branch tip with both branches' entire append-only
  entry history unchanged. An external file written only inside the private
  fixture project remains unchanged; no tool execution or filesystem undo is
  claimed. Both preview and confirmation explain the history-only boundary.
- Retires old dialog/confirmation/Stop/approval/input/queue state. Reopening reads
  a clean Versions view and identifies the restored branch as Current.
- Globally unique provider records prove that restore and reload never start a
  provider turn, replay a prompt, or duplicate the restore commit.

## Expansion boundary

Processes and Goals are **not covered by this suite**.
They require agreed production contracts and parent-owned Go fixture scenarios.
The existing fixture offers gated text plus `ask_user`, no arbitrary Bash or
real external process spawning, and a 150-second lifetime per instance. Missing
capabilities are blockers, never synthesized controls or weakened assertions.

Failures report actual passed assertion counts and sanitized labels. Missing
assets, compile/startup failure, deadline, assertion failure or browser error
exits nonzero. No success is inferred from the exported-asset suites.
