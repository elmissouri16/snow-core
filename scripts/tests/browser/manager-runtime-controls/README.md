# Native manager runtime-controls acceptance

This opt-in suite starts production `web.Run` on an ephemeral loopback port,
uses its real pairing flow privately, and opens its real HTTP-served assets in
headless Chrome. Every tested mutation uses native pointer/keyboard/form input.
There is no browser fetch mutation, intercepted asset, synthetic snapshot, mock
RPC response, real network provider, or user manager connection. Port 7331 is
explicitly rejected.

The fixture is
`cmd/snow/web_runtime_controls_browser_fixture_test.go`. It reuses the existing
private-HOME execution fixture's HTTP transport helpers, with independent
initialization so the manager captures only this fixture's worker environment. A
fake provider gates fictional responses. Seeded SQLite history uses ordinary
app/session APIs; provider metadata advertises real reasoning capabilities.
Compaction progress holds **after** a genuine native progress event; the late
steering receipt buffers genuine RPC acceptance bytes while real native delivery
and completion continue. The lost-receipt case uses a CDP response-stage network
failure only after the real steering HTTP operation runs; it does not fabricate a
response or replay the operation. No event or native operation result is manufactured.

The Thinking-dropdown assertions supersede the historical direct-Apply evidence
below. They require a fresh serial browser run after the parent rebuilds assets;
no browser run or asset rebuild was performed while editing this fixture.

## Matrix and scope

Four reports: **320 / 1280 px × dark / light**, height 740.

Narrow critical cases:

- Native activation using private fake host defaults, capability flags and actual
  production asset HTTP success, the actual React module handshake, exactly one
  generated module in the head, and absence of all five retired classic scripts
  from both the head and network requests.
- Compact **Thinking: current level** composer dropdown. Opening (including the
  header Thinking entry) only inspects authoritative capabilities; choosing a new
  level applies exactly one session-only Default/Plan update immediately. There
  is no primary Preference/New Value/Apply or consent workflow. Real request
  receipts verify the complete session/revision/branch/tip and unchanged response
  preference payload. Reselecting the current level is a no-op. Outside click and
  Escape dismiss the picker; successful selection restores composer focus.
  Summary/verbosity remain in explicitly secondary **Response settings…**, not
  among level choices. Exact unchanged host/project config bytes, composer
  selection/draft retention and passive Settings open/Escape remain covered.
- Direct saved-label rename and detached fork without a confirmation panel, with
  inventory-only explicit Open links. Active branch fork still requires review,
  fresh consent and confirmation, preserving target Plan mode and composer draft.
  A subsequent native prompt/approval/tool/reply verifies the replacement event
  epoch; every history action posts once without provider work.
- One-click **Compact context** in the normal composer, with no popup or consent
  checkbox. The real POST must carry the exact current instance/session/branch/
  tip/revision, including after an ordinary prompt completes. Native rapid repeated
  clicks on the disabled button during held provider work and held progress admit
  no second POST or provider invocation. Shared composer Stop cancels the entire
  operation; inline progress remains nonterminal until native cleanup. Actual
  fake-provider checkpoint work, refreshed visible context/usage, a subsequent
  explicit native no-op without provider work, no automatic repeats, and preserved
  unsent draft are asserted. Bounded animation-frame sampling across admission,
  Stop, progress, completion and menu-origin no-op checks editor/button identity,
  stable dock/action-row geometry and the absence of Queue next. It settles prior
  input autosizing and normalizes intentional ancestor scrolling by native click
  helpers; raw screen movement is not mistaken for product reflow. A separate
  no-op sampler checks transcript top/height, content height, scroll offset and
  retained message/editor nodes after dismissing prior feedback. New feedback
  must not resize the chat or show an ordinary Working indicator. Terminal
  dismissal preserves the draft and only changes presentation. Receipt body
  inspection waits for `Network.loadingFinished`, not merely response headers.
- Native steering acceptance distinct from delivery, no optimistic chat row,
  literal delivery, and completion before the held acceptance receipt. No Queue
  next tunnel or automatic browser retries. A genuinely lost HTTP receipt retains
  the draft and exact-root Stop; after native Stop, explicit local draft dismissal
  and global Reviewed release controls without inferring delivery or retrying.

This is not the full Goals/Processes, host-settings/API-key, layout, transport,
SDK, or provider matrix. See the adjacent suites for those contracts.

## Run in a serial test slot

Requires the repository Go toolchain, Node 22+, and installed Chrome/Chromium:

```sh
node scripts/tests/browser/manager-runtime-controls/run.mjs
```

To avoid rebuilding while another agent owns a compiler/cache slot, the parent
can supply its already compiled `go test -c ./cmd/snow` binary:

```sh
SNOW_RUNTIME_CONTROLS_BINARY=/absolute/private/path/snow-fixture.test \
  node scripts/tests/browser/manager-runtime-controls/run.mjs
```

Optional environment:

- `SNOW_CHROME_BIN`: Chrome executable (shared CDP helper convention).
- `SNOW_RUNTIME_CONTROLS_ARTIFACTS`: private output directory. At most **four
  viewport-only screenshots plus one sanitized JSON report per matrix case**.
  Cookie-bearing startup IPC, request bodies, CSRF values, native private frames,
  provider contexts, SQLite files, host config, and browser profiles are never
  copied into artifacts. Public DOM/report privacy scans precede retention.
- `SNOW_RUNTIME_CONTROLS_WIDTH=320|1280` and
  `SNOW_RUNTIME_CONTROLS_THEME=dark|light`: diagnostic subset, not the full gate.

Each case owns its temp HOME, config/session roots, project and browser profile.
Private startup IPC carries the pairing cookie; the runner never echoes it. The
runner retains only bounded HTTP method/path/field-name observations, closes the
manager through private stdin, and removes its temporary trees. A browser failure
reports sanitized UI status, not RPC/auth/private-provider payloads.

## Verification status

The direct-action baseline passes the full **320/1280 × dark/light** matrix:
**348 assertions, zero failures across four reports**.
It exercises both composer and conversation-menu compaction activation, including
native one-request/no-op outcomes. The initial run had 340 passing assertions
and four obsolete shared-harness failures requiring a compaction dialog. Updating
that assertion to the three remaining dialogs plus the composer/inline compaction
owner cleared the complete rerun; see BUG-210 for the retained failure evidence.

That baseline module was **820,981 bytes**, SHA-256
`c867d02e176896f4f0672b083f727be330faa7d960d286c55981e9d322ba8d0e`.

The subsequent composer-flicker regression (BUG-211) passes **352 assertions,
zero failures across all four reports**. Every report samples zero dock-height,
scroll-normalized top and action-row-height change, with stable editor/button
identity and no visible Queue next through compaction. Initial and partial-fix
narrow runs failed the new geometry check; they are not acceptance evidence.
The separate `manager-workflows/run.mjs` also passes 432 assertions, preserving
ordinary Queue next delivery and retained-item review.

The later composer-controls run also passes **352 assertions / four reports**.
Reasoning is opened through the real composer lightbulb, with Close/Escape focus
returned to that icon. Header-menu aliases are independently covered by the
runtime-layout matrix. The earlier 432-assertion workflow run remains historical.

Current generated module: **821,699 bytes**, SHA-256
`0f5e5bb84afadd7520c740ddaf42efb4af3aabe60da3aacba5b5b33aede195d6`.
The presentation projection also changes `static/app.js`, SHA-256
`af8ed3709537499d20bcf06921bfce57930d12ef420ba6b06e3d6ff5846bf65f`.

All owned runtime-control assertions passed in all four cells: direct reasoning
Apply, safe history actions without review, preserved activating-fork gates,
exact compaction POST scope, single provider invocation under repeated native
clicks, whole-operation Stop, held nonterminal progress, terminal counts and
visible telemetry, native no-op without provider work, preserved drafts, and
existing steering/lost-receipt recovery.

Commands run:

```sh
node --check scripts/tests/browser/manager-runtime-controls/controls.mjs
node scripts/tests/browser/manager-runtime-controls/run.mjs
```

Syntax checking passed. An earlier matrix attempt hit the execution tool's
120-second timeout without a retained report; the completed run above supersedes
that inconclusive attempt. Historical 244-assertion panel evidence predates these
direct-action changes and is not the current gate result.

All acceptance here uses private fake-provider fixtures, not real-provider or
user-session coverage. No user manager was restarted and no local installation
was performed.
