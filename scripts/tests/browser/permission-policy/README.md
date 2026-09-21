# Permission-policy browser regression checks

Run from the repository root:

```sh
node scripts/tests/browser/permission-policy/run.mjs
```

Requires the repository's Go toolchain, Node 22+ (native WebSocket), and installed
Chrome. Set `SNOW_CHROME_BIN` if Chrome is outside the standard locations.
The harness uses a temporary Chrome profile and removes its temporary fixture
export and profile on exit. It never starts a Snow manager, RPC worker, provider,
or reads user Snow configuration.

The opt-in `TestExportHarnessVisualFixtures` Go test exports the **production
streaming `workflow-edit` template and embedded assets**. That fixture must enable
`PermissionPolicyEnabled`; the runner fails clearly rather than inserting fake
controls when that capability is absent. A bounded loopback HTTP server supplies
public snapshots and validates the actual browser POSTs (method, project,
instance/session identity, CSRF, policy, and Allow-only acknowledgement). The
additional mode checks use the real bounded native JSON transport and production
SSE parser against a local stream. `fetch`, `SnowNavigation`, DOM markup, and CSS
are not replaced. Telemetry checks observe the production panel and wrap only
the public conversation render entry point to identify snapshot settlement; they
assert retained node identity and exact DOM mutations rather than renderer calls.
The two-second polling delay is shortened to keep checks bounded.

The runner also exports the production `many-home` catalog for desktop sidebar
checks. Native workspace reads exercise preserved search/scroll/focus,
superseded navigation, delayed responses, Rename focus return, unchanged row
updates, and live-state survival after a rejected swap. The mock returns the
exported `#workspace` fragment to match production navigation; newly inserted
links are exercised only after native lifecycle settlement. No navigation markup is fabricated.

All eight combinations of widths **320 / 1280**, heights **740 / 240**, and
**dark / light** run the core checks using native Chrome mouse/key events.
The 1280×740 dark/light cases additionally run the long-sidebar scenarios:

- Shared SnowMenus geometry, ARIA state, Escape/focus restoration, Space/Enter,
  and Arrow/Home/End navigation; no action for opening or current-policy choices.
- Unchecked Allow, cancellation, Escape, fresh acknowledgement on every open,
  and exactly one explicitly confirmed Allow POST.
- Immediate Ask/Deny requests, pending-request lock, non-optimistic labels, and
  display updated from later authoritative public snapshots.
- Lock/revocation during running, permission, input, unknown state, unknown,
  missing or null policy, idle with pending attention, uncertain recovery,
  disconnection, and instance/session replacement. Policy changes also revoke a
  confirmation tied to the previous policy.
- Failed HTTP responses, `{}` acknowledgements, and otherwise plausible
  acknowledgements with absent/non-string status remain uncertain and never
  automatically retry, even while fresh snapshot reads continue.
- Composer `novalidate`, zero native invalid events or prompt requests for blank
  and whitespace click/Ctrl+Enter, retained UTF-8 size guard, and one real prompt
  request for explicit nonblank native text entry.

- Compact permission root with a short boundary note; Details retains the full
  warnings and Back/Escape returns to policy selection.
- Repeated held Default/Plan POSTs use real native fetch with exact allowlisted fields,
  no prompt-form serialization, no transcript/composer replacement, zero extra
  SSE opens/snapshot GETs, and stable composer/scroll geometry sampled each frame.
- Known permission labels, draft text/selection and node identities survive
  settings updates. Stale queued reads cannot roll back newer acknowledgements;
  later stream updates remain authoritative.
- Malformed or interrupted acknowledgements still fail closed without application
  replay. Terminal SSE replacement revokes authority during an in-flight native
  request; a late old-lifetime reply cannot re-enable the replaced conversation.

- Native JSON model discovery holds the response through initial load, identical
  refresh, failure and retry. It checks cached model/search/scroll-node identity,
  input focus, stable popup geometry, frame-sampled composer and transcript scroll,
  verified permission labels, no implicit prompt/model-selection work and no extra SSE connections.
  Model selection uses the retained row's current action and rejects success-only
  acknowledgements instead of unlocking stale data.

- Compact telemetry rows, Details/Back/Escape, unknown versus verified-zero cost,
  invalid currencies, tiny/large amounts and missing context windows. Native DOM
  measurements compare compact height against reconstructed inspector spacing
  and expanded notes at the same viewport. Instrumentation verifies zero DOM
  mutations for unchanged metrics and unrelated settings, exactly one text
  mutation in retained nodes for changed values, and no extra requests.

`SNOW_BROWSER_QUICK=1` runs only 1280×740 dark for local debugging; it is not a
substitute for the full eight-case matrix.

This is frontend/public-transport regression coverage, **not** proof of backend
policy persistence, provider behavior, RPC enforcement, or security containment.
The normal Go suite remains network-free; this opt-in harness uses loopback only.
