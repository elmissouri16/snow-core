# Replace the mixed conversation settings form with real task menus

Written against Snow db7b2185c86a6506006ff797dce3129e2de385f0 plus uncommitted web implementation; reference c291e7961a515f6d7af9304e7fd1d257929aef26 in dist/harness-reference/.

## Evidence chain
- Surface: activated Snow model trigger, `internal/web/templates/live.html:15–20` and `static/conversation.js`.
- User screenshot explicitly rejects the model popup containing explanation paragraphs, native Provider/Model/Conversation selects, Apply and telemetry.
- Reference owners: `packages/client/ui-model-selection/src/client/ModelSelect.{tsx,module.css}`, `ui-primitives/src/Menu.{tsx,module.css}`, `ui-conversation/src/client/skeleton/ContextMeter.{tsx,module.css}`, `ui-workspace/src/client/rows/Rows.tsx`.
- Runtime chain: InputBar contributes ModelSelect and ContextMeter; WorkspaceBrowser renders row-owned actions. Real reference menus were opened for browser measurement without changing a selection.
- Binding contract: user's explicit request to port those actual surfaces, not restyle the old form.
- Uncertainty: Snow has no exposed model effort or arbitrary inactive-session rename/fork/archive capability. Do not invent those rows.

## Design decision and reuse
Separate controls by task. Use a shared portal menu controller because native details cannot express body-level viewport-clamped positioning and shared keyboard/click-away cleanup; do not create a second runtime action controller.

- Shared menu material: dark #353638/light white, r20, padding4, no border; elevation 0 0 0 .5px white6%, 0 3px 8px black4%, 0 0 20px black5%. Rows r10,14/22,padding8x10,min-height40; dense34. Clamp12px from viewport.
- Model trigger inside composer:28px/13x20/500,r24. Root row Model → current display name, then provider-grouped selectable model rows; no separate provider form, no Apply button. Selecting exact host pair uses existing hooks.action('model', ...) directly; selecting current closes without mutation.
- Model menu above/end aligned with8px gap,min240/max420,width-content,max-height min360px or viewport−96. Show explicit Load host models button before any provider discovery, concise one-line explanation; opening alone must not silently contact providers. Loaded choices can be reused. Errors/retry in-menu; uncertain mutation outcomes remain globally visible and never auto-replayed.
- Context/usage moves to separate compact ring/details popover, using only existing telemetry. Unknown is not zero. Never invent breakdown fields.
- Session selection/New/Rename move to header/sidebar-owned menu with existing authoritative workflow and stop-switch confirmation. Ellipsis must not also activate its row. Only offer supported actions.
- Snow Default/Plan remain authoritative mode controls, separate from model. Upstream Standard/PTC/Minimal/Creator are agent presets, NOT equivalents of Default/Plan. No fake preset roster.

## Changes
1. `static/conversation.js`: preserve init/render/dispose, hooks, generation/instance checks, controls.safe, abort cleanup, confirmations and stale-response guards; replace native-select presentation.
2. `templates/live.html`: remove mixed workflow body, split menus into actual owners, preserve all active attention/draft/runtime routes and dialogs.
3. New self-hosted menu JS/CSS, allowlisted/ordered by `render.go` and head: portal positioning, outside pointer, Escape (back from drilled pane then close), arrows/Home/End, focus return, one-open-menu and HTMX teardown.
4. Browser tests: replace old selection-then-Apply assertion with explicit discovery and direct exact-pair selection; retain every safety assertion. Add open-menu snapshots at 360/768/1280/1512, unknown/disconnected/empty/long model names, no clipped panel or hidden heading.

## Reopened responsive audit — current uncommitted source

The initial port is not a whole-surface acceptance result. Reopened against the same commits after the user's further rejection:

- `conversation.js` puts mode/telemetry headings, rows and notes directly in the panel. `menus.js` clamps all panels to visualViewport.height−96, while `menus.css` overflow:hidden and only grouped lists scroll. Add a shared min-height:0 scrolling content region for non-grouped content, preserving header/footer ownership where present; verify actual text/actions reachable at240px CSS viewport height and with large type.
- `templates/pages.html` workspace picker puts Add workspace in its scrolling list. Reference `ui-workspace/WorkspacePicker.tsx` partitions items/footer and `ui-primitives/Menu.module.css` keeps footer flex:none. Partition Snow's portaled picker likewise; Add remains visible with100 projects and short viewports.
- `settings.css` last-loaded `.sidebar .sidebar-new-session` overrides the canonical38px expanded/36px collapsed control to34px. Remove this duplicate size override; retain canonical `harness.css` geometry.
- Verify all shared consumers: model unloaded/loading/error/empty/partial/truncated; mode; usage known/unknown; session root/list; workspace picker empty/long/unavailable; view menu; current-session actions. No clipped headings/notes or unreachable last row after viewport shrink, scrolling, zoom-equivalent layouts or HTMX navigation.

## Validation and scope
Use actual Go-exported templates and mocked public DTOs, not copied ideal markup. Run browser workflow, inspection and layout suites, HTTP asset tests and repository gates. Preserve direct-loopback deployment, real host folder picker, no automatic activation or recovery.
Stop if an action needs new runtime authority or private data; do not fake it. Add upstream MIT attribution for adapted structures/styles. Record available interactions in docs/using-snow.md after successful verification.

## Workspace-row follow-through

The current implementation now includes the reference's workspace-owned sibling
action slot: New conversation (+) and a shared portaled workspace menu. Current
live-project New forwards to the existing guarded session workflow; other
projects retain session-free browse/activation navigation. Project settings and
Remove registration use the existing Project inspector and unchecked retention
confirmation, not a new removal or runtime action owner. Unsupported workspace
rename/archive/fork and permission editing remain excluded.

The updated production-template matrix passed 1,890 states / 26,524 assertions
with no failures, including workspace portal geometry, exact supported rows,
mobile inspector dismissal and no implicit filesystem/mutation requests.
Focused shell routing checks cover current/other projects and stale callbacks.

## Follow-up: compact Context & usage without losing truthful telemetry

Written against: `db7b2185c86a6506006ff797dce3129e2de385f0` plus the current
uncommitted manager implementation. **Status: implemented and verified (BUG-175).**
This section supersedes the earlier telemetry presentation guidance only; it
is not permission to redo model, session, workspace or permission menus.

### Evidence chain

- Surface: the active conversation's composer ring (`data-telemetry-menu` in
  `internal/web/templates/live.html`) opens the body-level telemetry menu through
  `showMenu` → `menuContent` → `telemetryContent` in
  `internal/web/static/conversation.js`.
- User evidence: the supplied screenshot explicitly highlights the large unknown
  cost block and explanatory paragraphs as unwanted information/space. It shows
  zero recorded usage, estimated context of 3,619 / 272,000 tokens, unavailable
  cost, and a long bottom note extending beyond the visible popup content.
  Screenshot scale is unknown; do not treat image pixels as CSS measurements.
- `telemetryContent` always appends the cost renderer plus an approximation note.
  `internal/web/static/costs.js::render` always appends a separate multi-sentence
  cost disclaimer, even when no amount is available. These are the highlighted
  text owners, not provider output or a live billing result.
- `templates/pages.html` loads `app.css` before `menus.css` and `costs.css`.
  The global `dl>div {padding:14px 0;border-bottom:1px solid var(--line)}` and
  `dt {margin-bottom:5px}` still reach telemetry rows. The local telemetry rules
  add a 12px grid gap and their own value margin without resetting those global
  row rules. This explains the stacked spacing and dividers in the screenshot.
- Binding current requirement: the user's request for less information and less
  wasted space in this popup. Current semantic constraints are in
  `docs/using-snow.md`, “Current-session reasoning and recorded cost”: unknown
  cost is not zero, currency/coverage can be incomplete, and an estimate is not
  a bill or spending cap. Preserve those facts, not their current prominence.
- Scope: this live telemetry popup and its use of the shared costs helper. The
  static `templates/costs.html` and other cost consumers are compatibility checks,
  not permission to restyle them. No `DESIGN.md` was found.
- Uncertainty: no new browser measurement or performance profile was taken in
  this audit. The earlier refresh regressions are evidence for preserving the
  existing reconciler, not a measured performance gain from this proposed change.

### Design decision

Replace the stacked explanatory default view with compact metric rows and one
**Details** entry. Keep explicit values in the main view:

- Existing context label, retaining Estimated context versus Last reported input.
- Recorded input/output/total usage, with explicit unknown when unavailable.
- **Cost estimate** with the verified currency/amount, or simply **Unknown**.

Move the cost-coverage/billing disclaimer and approximation explanation into the
Details pane. Do not retain duplicate copies in the root pane, replace unknown
with zero, hide a valid zero, or imply an unavailable estimate is free usage.
The user's highlighted unknown-cost paragraph becomes one small status value,
not a prominent block. Preserve existing counts rather than inventing metrics.

### Reuse

- `SnowMenus.open`, `SnowMenus.reconcile`, `SnowMenus.reposition`, its viewport
  clamping, scroll container, one-open-menu lifetime and focus return.
- `conversation.js`'s `row`, `pane` and existing permission-help Details/Back
  composition as the exact disclosure exemplar. Use a telemetry-owned pane key.
- `SnowCosts.presentation` for validation and amount formatting; do not duplicate
  provider-cost arithmetic or alter the normal renderer for unrelated consumers.
- Existing `.telemetry-menu` 12/18 label and 13/20 value typography, shared
  `--menu-surface`, `--text`, `--muted`, radius and elevation. No new design system.

### Changes

1. `internal/web/static/conversation.js`
   - Render one compact definition-list row per metric, then the existing shared
     Details row. Keep `data-workflow-usage`, `data-workflow-context`,
     `data-workflow-context-label`, and `data-workflow-cost` on their actual values.
   - Use `SnowCosts.presentation` in this compact composition; keep its `known`
     result on `data-known`. The root unknown copy is `Unknown`, while Details
     explains what is unavailable and why it must not be treated as zero.
   - Add the telemetry Details pane and Back behavior using the current owner,
     not a second dialog/controller. Preserve the distinction between estimated
     context and measured last input, and all cost-coverage qualifications.
   - Opening, dismissing and reading Details must send no requests or actions.
2. `internal/web/static/menus.css`
   - Scope changes to `.telemetry-menu`: reset inherited metric-row padding and
     borders, and reset `dt`'s inherited bottom margin. Let the existing 12px gap
     be the one vertical row-spacing owner instead of accumulating multiple gaps.
   - Put labels and values alongside each other with wrap-safe columns. Use the
     existing typography and 10px horizontal inset, not smaller type to hide bulk.
     Retain content-sized height; do not copy the model picker's fixed height.
   - Retain the existing minimum/maximum menu widths and short-viewport scrolling.
     Do not remove global `dl` styles needed by inspectors or settings panels.
3. Refresh/performance preservation
   - Keep the keyed reconciler and resident nodes. Do not use HTML replacement,
     extra reads, polling or SSE reconnects to update these metrics.
   - Add a regression asserting identical or unrelated snapshots do not recreate
     metric nodes or cause a geometry/scroll change. If that test proves an
     unnecessary telemetry redraw, restrict only its signature to displayed
     telemetry and pane dependencies; do not alter other menus' authority keys.
   - Measure before/after DOM mutations and popup geometry with the same fixture.
     Do not claim CPU, frame-rate or latency gains without corresponding evidence.

### Scope and validation

- Inherit: only the live Context & usage popup. Verify shared menu keyboard,
  dismissal, viewport and lifetime behavior; verify existing cost consumers remain
  unchanged. Exclude provider accounting, pricing, runtime authority, billing caps,
  other page layouts and removal of action-specific safety confirmations.
- Product cases: the screenshot's empty-usage/known-context/unknown-cost case;
  available nonzero cost; verified zero; tiny positive and very large values;
  missing telemetry; missing context window; reported versus estimated context;
  invalid/mixed currencies. Never fabricate a cost or substitute zero for unknown.
- Interface: 320/1280 widths and 240/740 heights, both themes; long values and
  zoom-equivalent layouts; opening Details and Escape/Back; repeated live updates
  while each pane is open. Verify the root is shorter than before using identical
  data, with every metric and the Details action reachable, no clipped footer,
  stable surviving nodes, and no composer movement or stream reconnect.
- Extend `scripts/tests/browser/conversation-workflow/tests.js` and
  `scripts/tests/browser/harness-layout/controls.js` for metric semantics and
  Details. Extend the native checks under `scripts/tests/browser/permission-policy/`
  for refresh/geometry invariants rather than relying only on mocked DOM tests.
- Run `node --test internal/web/runtime_cost_frontend.test.mjs`,
  `node scripts/tests/browser/conversation-workflow/run.mjs`,
  `node scripts/tests/browser/permission-policy/run.mjs`, `go test ./internal/web`,
  `go test ./...` and `go vet ./...`. Tests must preserve cost safety while checking
  that explanatory text is in Details, not require old default-pane wording.
- Follow the repository's final verification and local installation workflow only
  after implementation is verified. Do not restart the user's running manager.

### Stop conditions and documentation

- Stop if telemetry ownership, the cost DTO or the menu lifetime has changed;
  retrace current source before editing. Do not silently expand to all UI panels.
- Stop if the intended layout requires deleting truthful unavailable states,
  inventing numbers or removing action-specific authority warnings.
- After implementation and acceptance, document the compact metrics/Details
  interaction in `docs/using-snow.md`. Preserve the existing recorded-cost semantic
  contract. Update the appropriate tracker entry with fresh reproduction and
  verification; this read-only audit changes neither product source nor tracker.
