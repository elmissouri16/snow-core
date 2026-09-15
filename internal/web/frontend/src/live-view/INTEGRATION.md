# Live view integration

Export `LiveViewBridge` from `./live-view`; register `window.SnowLiveView` before
`SnowReactReady`. The existing app controller calls init/render/dispose; main
must NOT independently mount/unmount these roots. No new transport or workers.

Empty template mounts inside `#live-session` (display: contents):

- `#live-chrome-view`: replace connection-line, unknown, recovery, live-error,
  live-turn-outcome. Add `data-initial-error="{{.Live.Error}}"` on this mount.
- `#live-composer-notices-view`: replace regenerate/edit/reuse notices, at their
  current position before context items.
- `#live-composer-editor-view`: replace ONLY label + textarea, after independent
  context items/status/mentions mounts. Keep form and hidden inputs external.
- `#live-composer-actions-view`: replace ONLY Send + normal Stop, inside trailing
  composer controls, after conversation model/telemetry mount.
- `#live-composer-status-view`: replace ALL composer-footer children: state,
  Queue next button and hint. Queue projects their presentation; its existing
  root click listener owns Queue next. Preserve the outer composer-footer.
- `#live-turn-status-view`: replace ONLY `#live-turn-status` at its current stream
  position; preserve transcript, activity and empty-state owners.
- `#live-regenerate-dialog-view`: replace regeneration dialog; may always exist,
  but actual dialog renders only when data-message-regenerate-enabled=true.

The heading, all model/policy/mode controls, queue/context/attention children,
transcript, runtime-close dialog, outer identity metadata remain foreign.
No raw page-state JSON bootstrap; only existing capability/status attributes and
bounded initial error text are read. Runtime snapshots never enter this facade;
app projects bounded presentation strings and booleans, never edit tokens.

API: `init(region)`, `updateChrome(partial)`, `updateControls(partial)`,
`updateDraft(text): boolean`, `updateSuggestions(state)`, `dispose()`/`clear()`. Commits use flushSync.
Compaction preserves the normal composer footprint: app projects its operation
as nonqueueable `compacting`, and uses `statusIdle` only as a presentation flag to
keep redundant routine status screen-reader-only. Compaction's own inline status
includes stopping; connection/unknown-outcome warnings remain visible. This flag
must never become admission authority. Native runtime tests sample animation-frame
geometry and editor/button identity across compaction and reject Queue next.
Compaction also suppresses the ordinary turn Working indicator. Its existing
owner renders feedback out of flow using `--scroll-jump-bottom`, so wrapped
status text cannot resize the transcript on a native no-op. Feedback dismissal
is local presentation only; native operation and Stop authority remain intact.
No-op coverage samples transcript top/height, content height, scroll offset and
message/editor identity—not only composer geometry.
`goalMode` is a presentation-only controls flag. Only changes to that flag call
the editor's imperative mode setter, updating its accessible label and placeholder
without remounting it or pushing routine streaming updates through its state.
The external Goal owner retains local draft mode/budget; app submits the existing
editor text through fresh exact-scope inspection and native goal admission.
Attachments and edit/reuse intents never silently become a text-only goal.
Only a valid native admission receipt permits clearing an unchanged objective;
concurrent typing, unknown outcomes and stale scope preserve it. Goal's draft
mode is independent of saved goal status and whole-run Stop.

Send, admission progress and Stop have the same 34px icon footprint with full
ARIA/tooltips. The bounded composer form owns its overflow; the ordinary live
region clips rather than scrolling its header on native child focus. Explicit
error/attention scroll modes retain their existing overrides.

Textarea DOM node is stable; controlled local state owns values and IME. Only
programmatic writes route through updateDraft; the existing delegated input,
submit, action, keyboard and native dialog lifecycle remain single owners.

Conversation integration from its INTEGRATION.md is incorporated in app:
heading writes removed; closeDisabled + inspectorExpanded passed to render.

Parent owns template/CSS, main export, bundle regeneration, browser matrix and
install-local. Native edit/reuse/regenerate and stream suites are critical.

## Focused verification / extractor integration

`npm run typecheck` and `node --check internal/web/static/app.js` pass. The
existing source-function extractor suites need a mock `window.SnowLiveView`
presentation boundary, especially `updateDraft(text)` which synchronously
publishes the mock textarea value and returns true. Keep extracting and invoking
actual app functions; do not restore production DOM value fallbacks. Affected:
`workspace_session_flow.test.mjs`, `session_switch_flicker.test.mjs`, and the
replacement case in `live_panels_transport_contract.test.mjs` (its source
assertion at line 70 specifically expects the retired direct `.value` writer;
assert the same `!replacing` guard around `SnowLiveView.updateDraft` instead).

All live textarea value writes, live/error/recovery/status/action/notices/dialog
presentation writes have been removed from app. Legacy outer identity/hidden
form inputs, workspace opening/home-draft notice, transcript/history and
attention lifecycle remain with their existing owners, not this facade.
Redundant app attention-child clearing is removed: SnowAttention.render/dispose
remain the sole owners of its subtree.

Latest focused result: 55 transport/source tests, 51 pass, 4 boundary-harness
failures above. Full configured frontend unit suite: 50 pass. Typecheck passes.
