# Native production browser-access gate

```sh
node scripts/tests/browser/manager-access/run.mjs
```

Requires this checkout's Go toolchain, Node 22+ and installed Chrome/Chromium
(`SNOW_CHROME_BIN` may select its executable). No npm dependencies.

The default matrix is 320×740 and 1280×740, dark/light. Each report compiles/runs
the opt-in `cmd/snow` `TestWebAccessFixture` against real production `web.Run`,
HTTP routes, HTML, JavaScript and CSS. Two independent Chrome browser contexts
pair through native pointer/input form submission; no session-cookie injection,
mock fetch, synthetic DOM controls, asset server or intercepted response exists.
Theme changes use the real native Settings controls. On narrow screens, the
fixture then closes the still-open project drawer through its native Close
control before interacting with the workspace. It does not bypass the drawer’s
inert background or modify product state to force clicks.

The fixture uses private temporary HOME, SNOW_HOME, manager and Chrome roots;
only PATH and the private fixture paths are passed to its subprocess. Listeners
bind `127.0.0.1:0`, never the user's 7331 manager. No project is registered and
no provider, worker, extension or user configuration is initialized. The fixture
executable path intentionally cannot launch a worker. Startup pairing codes are
consumed through suppressed private IPC, never URLs, reports or artifacts.

Coverage includes real production asset allowlisting, two-browser inventory,
opaque public IDs/current indicators, bounded labels and timestamps, explicit
Cancel/Confirm, exact other-browser revocation, current-browser logout, stale
native confirmations, no replay/revoke-all/activation, narrow overflow and both
themes. Two complete fixture-process shutdown/restart cycles per report prove
unrevoked pairing and public identity persistence, durable individual/current
revocation, reusable pairing credentials and fresh IDs on explicit re-pair.

The UI's existing five-second SSE reauthorization disclosure is checked here;
actual stream termination remains covered by
`TestBrowserInventoryRevokeExistingSSEFiveSecondReauthorization`. This browser
suite deliberately does not start a runtime merely to manufacture an SSE stream.

Reduced debugging runs (not substitutes for the complete gate):

```sh
SNOW_MANAGER_ACCESS_WIDTH=320 SNOW_MANAGER_ACCESS_THEME=dark \
  node scripts/tests/browser/manager-access/run.mjs
```

Subprocess lifetimes/output are bounded and temporary fixtures are removed.
Reports contain assertion labels and sanitized route/field-name observations,
not authentication cookies, cookie hashes, pairing credentials or CSRF values.
