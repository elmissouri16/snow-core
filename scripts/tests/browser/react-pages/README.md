# React pages and settings — native Chromium regression checks

Run from the repository root after building the production frontend:

```sh
(cd internal/web/frontend && npm ci && npm run build)
node scripts/tests/browser/react-pages/run.mjs
```

Requires the repository's Go toolchain, Node 22+ (built-in `WebSocket`) and an
installed Chrome/Chromium. The runner reuses `../live-stream/cdp.mjs`; set
`SNOW_CHROME_BIN` to an executable path when Chromium is not in one of its
standard locations. No Playwright, Puppeteer, DOM shim, runtime React download,
new dependencies or browser test framework is used. Chromium runs with its normal
sandbox configuration; the runner does not add `--no-sandbox`.

For retained screenshots and measurements in a newly created temporary directory:

```sh
SNOW_REACT_PAGES_EVIDENCE=1 node scripts/tests/browser/react-pages/run.mjs
```

The runner prints the evidence directory. Otherwise its exported fixtures,
Chromium profile and temporary files are removed. Browser failures produce a
nonzero exit status. There is a 120-second exporter bound, a 300-second browser
suite bound, bounded request bodies/assets and explicit browser/server teardown.

## What is real, and what is simulated

- The existing `TestExportHarnessVisualFixtures` exports the **production Go
  template shell and embedded static files** without constructing a registry,
  manager, worker, provider or catalog. The fixture retains that shell's head,
  script/stylesheet order and native workspace navigation boundary.
- The executable frontend is the exact shipped `/static/generated/app.js` React
  bundle and the other production scripts. All production stylesheets remain in
  their exported order. Legacy `manager-activity.js`, `organization.js`,
  `host-settings.js`, `host-api-key.js` and `browser-access.js` must not appear in
  the head; nothing mocks or patches React, `SnowNavigation`, `fetch`, timers, DOM
  APIs or production event handlers.
- Because the existing Go exporter does not provide Activity/Organization page
  fixtures, this runner replaces only the exported workspace main content with
  an escaped `data-react-page`/`data-react-props` bootstrap root, and sets its
  workspace view. Fixture values follow the real public presentation contract:
  UUID project IDs, a deliberately fake 64-hex CSRF value, canonical pagination,
  bounded offsets, archived/active sessions and missing-folder restoration.
  Phase-two host settings and browser inventory use their actual exported Go
  roots in the production Settings dialog; the fixture overrides only public
  props to enable supported controls and use fake IDs/CSRF. Actual Go projection/
  escaping and authenticated route handling remain covered by the repository's
  Go tests, not by this synthetic props transport.
- The isolated loopback Node server serves those public assets and fake DTOs.
  It records native URL-encoded forms with **exact endpoint, field, CSRF,
  confirmation, session identity and offset checks**. Successful POSTs return 204
  and mutate nothing. Settings handlers likewise record and return fake public
  receipts without touching a registry or credential store. The shared production
  settings dialog receives an empty fake `/access/browsers` inventory unless a
  browser scenario supplies bounded public fixture rows. Every other nonasset endpoint is rejected;
  external browser requests and uncaught exceptions fail the suite.
- No real browser access tokens, user data, settings, project folders, sessions,
  runtimes or provider credentials are read. No real manager or mutation target
  is contacted. The profile, project paths and CSRF string are fixture-only.

## Verified scope

Current complete run: **202 assertions across 28 scenarios** (including fixture
transport invariants and asset checks).

### Activity

- Initial React commit and exact JSON GET, public counts/states, canonical
  conversation links, no mutation controls and no loaded legacy page scripts.
- One initial GET and one polling owner using the real two-second timer; repeated
  explicit refresh clicks cannot overlap a pending request.
- Keyed project cards and links preserve DOM identity and focused links across
  automatic summary updates. Malicious labels remain literal text.
- Missing fields, duplicate IDs, invalid navigation/counts, too many projects,
  malformed JSON, wrong media type, advertised/streamed body bounds and failed
  HTTP responses cannot replace last-good data. Errors explicitly mark stale
  state; a valid manual GET recovers.
- Pending headers **and** partially streamed bodies abort when their workspace
  ancestor is hidden. Late replies cannot apply; hidden pages stop polling and
  resume with exactly one fresh read.
- HTTP 401/403 clear displayed data and permanently retire that mounted owner;
  explicit refresh and visibility notifications cannot retry it.
- Unavailable registry and malformed bootstrap perform no Activity reads. A
  stalled response body hits the real five-second deadline and releases the
  polling slot for explicit recovery.

### Organization

- React title filtering and archived visibility retain input/row identity, focus
  and an unrelated unsaved workspace-label draft. Filtering sends no request.
- Malicious project/session labels render as text, canonical pagination remains
  local, missing-folder Restore is disabled and opening either native archive
  confirmation sends no request.
- Native Save label, workspace Pin/Unpin/Archive/Restore and session
  Pin/Unpin/Archive/Restore send nine strict, mock-only form POSTs. CSRF,
  confirmation values, session IDs, loaded-page offset and field sets are checked.
- Invalid JSON/CSRF/project IDs/pagination/offsets, duplicate session IDs and an
  oversized bootstrap fail closed into a safe error with no forms or POSTs.

### Host defaults, API keys and browser inventory

- Actual React host settings/API-key child mount without any host-default,
  provider-status or API-key reads. Reads require explicit actions. Browser
  inventory preserves its separate, existing one-initial-read-per-root behavior.
- Host global/project defaults use exact scope/identity GETs and conditional POST
  bodies: immutable loaded revision, supported fields only, omitted unchanged
  fields, atomic provider/model pairs and value-free resets. React edits retain
  control identity. Conflict, wrong-scope/authentication replies and unmount
  cannot grant stale write authority or replay mutations.
- API-key metadata binds the exact provider and inspected revision. OAuth-only
  and mismatched-provider metadata cannot enable entry; unknown wire fields are
  excluded. Server-disabled capabilities remain disabled. The fixture checks
  native same-origin POST headers and exact field sets; real HTTPS/backend gates
  are verified by the existing real-manager suite noted below.
- The masked uncontrolled password clears before validation/dispatch, including
  missing consent and replacement-consent failures. Each save consumes authority;
  success requires reinspection and uncertain/auth failure never retries. Provider
  changes, dialog close and actual native ancestor unmount clear retained password nodes.
  A partially streamed inspection aborted on close cannot reopen the form later.
- A bounded, read-only traversal checks that the fixture password is absent from
  React's reachable enumerable fiber/state/props graph while typed and during a
  pending write, and from serialized markup, visible text and local/session
  storage. This is a focused invariant check, **not a complete heap-erasure or
  closure-memory proof**. The input DOM value is deliberately excluded before
  submission. Fixture transport records redact the password immediately; neither
  it nor failed response content is included in assertion output.
- Browser inventory preserves literal malicious labels, safe Cancel/Refresh
  focus, exact single-browser CSRF/confirmation POSTs and synchronous duplicate
  admission fencing. Malformed inventories and mismatched receipts cannot remove
  unrelated rows or retain revoke authority. Rejected native navigations preserve
  roots and listeners but retire confirmation authority until explicit Refresh.
  Interrupted revocations never replay; HTTP 401 routes to the fixture pairing
  page without mutation.

### Ownership, navigation and presentation

- Real production native links replace the workspace ancestor Activity →
  Organization → Activity; browser Back/Forward restore the appropriate React
  page. Held navigation bodies abort, old roots unmount before
  `snow:navigation-before-swap`, and departed Activity stops polling.
- Rejecting an invalid fragment preserves the original mounted root and working
  listeners. A later successful navigation still cleans it up.
- Explicit cleanup and repeated `pageshow` notifications cannot add duplicate
  React/poll owners. `pagehide` aborts and unmounts a retained root, which
  remounts on `pageshow`.
  Page-transition events are deliberately dispatched, **not a claim of browser
  BFCache eligibility or a genuine OS suspend/resume test**.
- Activity and Organization are measured at 1440px and 390px in dark/light
  themes: eight representative states verify viewport fit, visible heading/
  control geometry and actual computed theme changes. Optional screenshots and
  measurements support inspection. These are not a full visual-diff, contrast,
  keyboard, screen-reader, accessibility-conformance or cross-browser audit.

## Complementary real-manager checks

The existing suites were run against the migrated production assets without
editing those suites. Desktop/dark verification passed:

```sh
SNOW_HOST_CONTROLS_WIDTH=1280 SNOW_HOST_CONTROLS_THEME=dark \
  node scripts/tests/browser/manager-host-controls/run.mjs
# 52 assertions, 1 report, zero failures

SNOW_MANAGER_ACCESS_WIDTH=1280 SNOW_MANAGER_ACCESS_THEME=dark \
  node scripts/tests/browser/manager-access/run.mjs
# 50 assertions, 1 report, zero failures
```

Those suites use isolated production Go managers with temporary HOME/data,
fixture credentials and native pairing; host controls deliberately activate a
fake worker and verify real direct-TLS/API-key backend guards. Browser access
covers independent browser contexts, self/other revocation and actual durable
manager restarts. These results are scoped to the selected desktop/dark runs,
not their complete default viewport/theme matrices. The mock-only React runner
itself still constructs no manager or worker.

## Files

- `run.mjs` — export/load production assets, launch native Chrome through CDP,
  bounded assertions, isolated network/error checks and cleanup.
- `fixture.mjs` — fake public props/DTOs and strictly allowlisted HTTP transport.
- `activity.mjs` — Activity data, polling, cancellation and validation checks.
- `organization.mjs` — React state/identity, safe bootstrap and native form checks.
- `lifecycle.mjs` — real native-navigation/history journeys, ownership cleanup and representative
  production-CSS measurements.
- `settings.mjs` — phase-two fixture contracts and native host defaults, write-only
  API-key and browser-inventory state/ownership checks.
