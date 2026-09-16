# Harness layout regression

This network-free browser test renders **the actual Go `page` template and
embedded public assets**. There is no copied HTML fixture or test stylesheet.
It exercises the DeepSeek Harness-inspired production shell in both light and
dark at widths 320, 360, 390, 768, 1024, 1280, and 1512 × 740. Expanded
contentful surfaces and open controls also run at 360px and 240px heights:

- Home with a registered workspace, and empty home
- Live conversation, authoritative Plan Mode, and pending permission
- Closed controls and direct searchable model/provider lists with long names and
  duplicate model IDs under distinct providers; loading, empty, failed and
  disconnected discovery. Opening an uncached picker loads choices once; typing
  and cached reopening never request discovery. Search/focus remain visible at
  short heights, with local no-match states and direct Escape dismissal.
- Open Default/Plan mode, session selection, rename and unknown usage menus
- General, Workspaces and Browser access Settings sections
- Long Markdown code/table, message/code copy, and public tool output
- Files inspector opened through its real control
- Cold workspace with 35 saved conversations in its grouped sidebar, paged as
  25 + 10 metadata rows; editable tab-local draft and explicit Start in the center
- Remembered project trust: compact Start/Resume without a checkbox, no automatic
  activation, and discoverable Settings revocation at normal and short heights
- Actual saved-history Markdown with exact-source message copy and code-only copy;
  independently scrolling history beside a visible Resume seat at normal heights,
  with draft/skills/Resume controls reachable through bounded short-height scrolling
- Forty registered workspaces, registration and host folder picking (populated,
  empty, limited and error responses), pairing, login, and validation errors
- All sixteen questions, recommended labels, Other/custom text, multiline free
  text, next/back/collapse, validation, IME and Shift+Enter, and exact batch POST
- Unknown-effect and truncated approvals, permanent authority warning, safe
  Reject/Stop, disabled truncated Allow, and disconnection gating
- Sixty-row streaming transcript with actual Go-rendered growing code, focused
  code-copy identity/text, table horizontal position, manual row anchoring, head
  eviction, attention changes, tail completion and explicit Jump to latest
- Contentful Files/Changes/Settings inspector tabs and known usage telemetry
- Workspace-row actions, real unchecked removal confirmation, project identity
  validation, no implicit Files/Changes reads, and mobile-sheet dismissal
- Compact docked composer: 90–106px idle card and exactly one action row, with
  context tools nested inside that row rather than a separate 28px toolbar.
- Multiline composer growth, long-draft caps, Send reachability, clear/shrink,
  and reference 16px transcript insets at every width and control height
- Enabled and unsupported runtime-panel fixtures: conversation-menu forwarding
  to Versions, Goal, Processes, Reasoning and Steer; supported-but-idle
  Steer stays disabled while unsupported actions are omitted
- Direct compaction's accessible icon in the composer action row: provider-work
  tooltip, no modal/checkbox, keyboard reveal and hit testing at short heights,
  busy-state gating and unchanged draft. This read-only fixture does not execute
  compaction; `../manager-runtime-controls/run.mjs` verifies direct native POSTs,
  repeated-click fencing, inline progress, composer Stop and no-op outcomes
- All five remaining runtime dialogs with persistent reachable Close, internal body scroll,
  glyph-line checks against character-per-line button wrapping, and return focus
  to the visible header trigger; Versions preview and bounded process inventory
- Expanded/collapsed footer navigation labels, last-menu-item keyboard scrolling,
  header hit testing after draft/snapshot changes, and width handles bounded
  between the header and composer

No authentication, project registration, real catalog scan, provider request, worker,
or agent tool is started. A pre-document CDP script mocks the public HTTP DTOs;
it rejects unexpected requests and requires explicit CSRF/instance-bound
workflow requests. The production polling, Markdown, navigation, workflow,
attention, inspection, settings and native navigation JavaScript still runs.
The same strict fixture handles bounded native JSON reads and writes. This layout
suite does not certify real HTTP serialization—`../permission-policy/run.mjs`
covers that integration with actual HTTP and SSE. Fixture unit tests retain failed-discovery responses
and reject stale instances and runtime-panel discovery outside the allowlist.

The embedded Browser access inventory intentionally performs one public metadata
read at startup per mounted inventory root, even while Settings is closed. The
mock admits only exact same-origin `GET /access/browsers`, without query, fragment,
body or extra headers, using the production no-store/credential/redirect options.
It returns one fictional public browser row. Every request remains recorded;
only accepted inventory reads are classified separately from interaction counts.
Missing or repeated Browser access inventory reads fail. The grouped sidebar
has a separate exact `GET /projects/{registered-project}/sidebar-sessions`
allowlist: optional bounded `offset`, no body or extra headers. Cold metadata
comes from the actual exported saved rows and pages at 25; live fixtures return
only bounded session metadata with their current instance ID. These reads remain
recorded and are distinguished from runtime-panel inspection, provider discovery
and mutations. Unknown projects, invalid paging, extra fields and mutations fail.
Other unmocked reads and all unexpected mutations still fail. Host defaults, API-key inspection, operations, provider
catalogs and worker activation receive no blanket read allowance.

## Run

Requirements: the repository's Go toolchain, Node 22+ (native `WebSocket`), and
an already installed Chrome/Chromium. No npm packages or browser downloads.
`SNOW_CHROME_BIN` optionally selects an explicit browser executable.

```sh
node --test scripts/tests/browser/harness-layout/fixture.test.mjs
node scripts/tests/browser/harness-layout/run.mjs
```

The runner generates pages with the opt-in Go exporter, serves only the exported
HTML/assets on an ephemeral loopback port, uses an isolated temporary Chrome
profile and bounded CDP requests, then removes all temporary files. Ordinary
`go test ./internal/web` skips the exporter and leaves no browser artifacts.

For screenshots and measured geometry, explicitly choose an output directory:

```sh
node scripts/tests/browser/harness-layout/run.mjs \
  --output-dir /tmp/snow-harness-evidence --screenshots
```

This writes `layout-report.json` and `STATE-WIDTHxHEIGHT-THEME.png` files only
inside the chosen directory. The full schedule currently produces 2,058
viewport/state reports (147 per width/theme, including six runtime-panel reports
and three remembered-trust reports);
screenshot count is larger because
initial question/approval/stream states and the reduced-viewport state are also
captured before interaction tests mutate them. Open controls are captured before dismissal, not
substituted with home/closed-composer screenshots. Screenshots document actual renderings; geometry and
interaction assertions, not platform-dependent pixel hashes, are the test gate.
Screenshots are captured even for assertion failures. Without `--output-dir`,
results are console-only. `--screenshots` requires `--output-dir`.

For a bounded investigation, use the 390px dark smoke schedule or select one
width/theme. These deliberately reduced schedules are not a full-matrix pass:

```sh
node scripts/tests/browser/harness-layout/run.mjs --smoke \
  --output-dir "$PWD/dist/polish-evidence" --screenshots
node scripts/tests/browser/harness-layout/run.mjs --width 320 --theme light
```

To isolate the **84-report** runtime-panel matrix (two fixtures × seven widths ×
three heights × two themes) while retaining production templates and owners:

```sh
node scripts/tests/browser/harness-layout/run.mjs --runtime-only \
  --output-dir /tmp/snow-runtime-layout
```

The remembered-trust startup subset has **56 reports**: one untrusted inactive
state plus three trusted heights, at seven widths and both themes. It tests
production markup and control reachability, not durable storage; registry/HTTP
and native manager-workflow tests cover consent persistence and revocation.

```sh
node scripts/tests/browser/harness-layout/run.mjs --trust-only \
  --output-dir /tmp/snow-trust-layout
```

The runtime-panel mock has a separate exact, scope-bound public-read allowlist. It rejects
extra/duplicate fields, wrong identities and transport, unsupported inspection,
and every mutation. This matrix supplements rather than replaces native worker
workflow/execution/runtime-control tests or the full layout schedule.

The full runner has a 30-minute internal deadline; use an execution facility
whose lifetime accommodates it. A caller timeout is an incomplete check, not a
pass. Screenshots/partial reports can still aid diagnosis.

To inspect/reuse an export independently (rerun after template/asset changes):

```sh
SNOW_WEB_FIXTURE_DIR=/tmp/snow-harness-pages \
  go test ./internal/web -run '^TestExportHarnessVisualFixtures$' -count=1 -v
node scripts/tests/browser/harness-layout/run.mjs \
  --fixtures-dir /tmp/snow-harness-pages
```

All directory arguments must be absolute. The Go exporter writes only within
its explicit `os.Root` destination; paths and session data are fictional public
fixtures. Do not point the exporter or evidence output at a source directory.

## Contract covered

- Actual workspace/navigation/control IDs and unique IDs; no dashboard cards
- Dark canvas `#151517`, full-height `#1b1b1c` 280px desktop sidebar, no desktop
  global top bar, correct content origin
- Centered landing mark/title, workspace/mode row above the ≤820px composer,
  approximately 114px hero height; docked composer ≈138px including its 28px context toolbar, with a 36px input and
  22px corners. Transcript width is clamp(680px, .64 × column, 920px), composer
  transcript+32px, with narrow 32px/16px clearances
- Sidebar collapse/restore, visible desktop Add workspace, collapsed-rail Search
  focus, persistent icon-only labels, and workspace filtering; real workspace picker,
  disabled unactivated composer, no automatic API work
- Real task menus with 20px corners, one body-level portal, viewport clamping,
  min(360px, viewport−96px) height, keyboard navigation/back, outside dismissal
  and focus restoration. No hidden native Provider/Model/Apply compatibility form
- Explicit discovery only, exact provider/model identity, authoritative mode,
  unknown telemetry, and truthful empty/error/disconnected menus
- Sectioned 800px Settings modal, real Light/Dark preferences, native Escape,
  backdrop/X dismissal, and focus return; canonical CSRF-protected pairing,
  revocation and sign-out forms without submitting those forms
- Exactly one live-owned 76px workspace header with Files / Changes and Close,
  no header Stop, and no healthy connection row. Idle Send becomes composer Stop
  during an ordinary active turn; pending input/permission replaces the normal
  composer with one visible attention-owned Stop in the same seat
- Permission attention disables unsafe controls; polling retains live transcript
- Saved-history Copy message preserves exact public Markdown, including fences
  and escaped angle brackets, without Copy code or banner text; Copy code stays
  code-only and neither action starts a worker
- All 35 cold saved rows remain in the grouped sidebar, not a central catalog;
  bounded paging preserves the cold draft, final-row focus/hit testing and
  separately reachable Start/Trust controls without activating a worker
- Mobile drawer focus and Escape, inspector modal boundaries/focus restoration,
  preserved drafts, and no horizontal page overflow

Native CDP Tab exercises Settings focus containment and actual question controls;
native Escape exercises modal dismissal. The reduced-visual-viewport check
simulates a scale-1 `visualViewport.height` change and dispatches its resize
event while retaining the CSS layout viewport. This verifies the production
viewport listener without claiming to have opened an actual mobile keyboard.
The real CDP 320px-width/240px-height layouts provide narrow/zoom-equivalent
reflow coverage; this suite does not claim physical-device, screen-reader,
platform browser zoom, or pixel-identical checks. Uncaught errors (including
ResizeObserver loops), unexpected requests, clipped controls and unsafe action
authority remain failures; no error suppression or screenshot-only gate.

The existing conversation-workflow and inspection-race suites remain the deeper
request ordering/race gates; this suite complements them with production-template
and reference-layout evidence rather than replacing them.


## Current fixture verification

After the strict public-inventory mock update, all 17 browser-free fixture tests
and the complete **320px dark slice (138 viewport/control reports)** pass. That
slice includes 740px, 360px and 240px heights where defined by the existing
matrix. The subsequent complete seven-width × dark/light run passed all **1,932
viewport/control reports**, including normal and reduced heights. This remains a
production-template/public-DTO fixture gate, separate from real-worker native
acceptance and physical-device or live-provider verification.
