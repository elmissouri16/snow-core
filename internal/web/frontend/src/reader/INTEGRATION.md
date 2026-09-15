# Reader integration contract

Install `reader` from `./reader` as `window.SnowScroll` before app initialization;
retire the legacy `static/scroll.js` script. The facade retains `init(region,key)`,
`beforeUpdate`, `afterUpdate`, `follow`, `wheelFromHandle`, and `dispose`;
`init` returns the mounted controller. `onLayout` schedules existing geometry
reconciliation; `userIntent` releases following without scrolling.

Replace the old `#jump-latest` markup with an EMPTY foreign React root:
`<div data-react-reader-controls></div>` in the same position inside
`#live-session`, outside `#live-stream`, `#live-transcript`, and composer roots.
Use an external stylesheet rule `[data-react-reader-controls] { display: contents; }`
(no inline template style under CSP). Reader JSX owns the existing native
`#jump-latest.jump-latest` button, title/aria-label and arrow; no extra indicator.
IMPORTANT: update the direct-child selector in `static/scroll.css` from
`#live-session > #jump-latest` to
`#live-session > [data-react-reader-controls] > #jump-latest` (or cover both).
`display: contents` does not flatten CSS selectors. `#live-session` remains the
absolute-position containing block; retain every existing size/offset rule.

Keep the parent-owned synchronous transaction:
`SnowScroll.beforeUpdate(); SnowMessages.render(...);` then activity, attention,
composer/chrome reconciliation, then `SnowScroll.afterUpdate()` after React
message roots have flushSync-committed. No effect-delayed commit boundary.
Do not move message/article or activity nodes into reader roots. Anchors continue
using exact unique `data-message-id` / `data-activity-id` attributes anywhere under
`#live-stream`; transcript maintains its current DOM host position. Existing
ResizeObserver / disclosure MutationObserver and visual-viewport callbacks stay
in the controller. Async image layout may call `onLayout()` (or `afterUpdate`).

Dispose BEFORE old scope/DOM retirement, then initialize once new keyed DOM is
committed. This cancels pending RAF/listeners/observers, discards old references,
and retains only the legacy bounded tab-local numeric/key anchor memory. Scope
identity is the parent-provided project/session key; do not reuse it across
unrelated sessions. No transport, snapshots, draft editing or selection ownership
is added. Parent retains message selection/edit/reuse retirement and accepted-send
`follow()` calls. Reader does not replace or focus the composer draft.

## Verification / retained native gates

`npm run typecheck` and `npm test` passed (83 tests) after the isolated reader
port. No geometry model/harness was introduced: the legacy controller algorithm
is preserved, including 40-session memory, eight anchor candidates, 24px tail
threshold, 0.5px write epsilon, clamp ledger, nested-scroll intent handling,
visual viewport, prompt sizing and bounded observer set.

Run the existing production-native harness-layout stream scenarios,
composer-layout, chat-width and live-stream suites after parent integration and
generated-asset rebuild. They cover real-floor following, 1px upward ownership,
head trimming, question-seat resize, retained draft/focus/selection, disclosure
layout and explicit Jump. The unit suite does not substitute for those gates.
No install/build touching generated assets was run by this folder-only child.

`userIntent()` is an explicit release hook, not a scroll event classifier. Native
wheel/touch/key handlers retain the legacy nested-scroller rules, and scroll
callbacks retain the observed-top/clamping ledger. `onLayout()` is safe from a
React layout effect: it schedules RAF instead of synchronously rendering a root.
Use the returned instance for async callbacks so disposed scope callbacks cannot
act on the facade's newer owner. `afterUpdate()` remains the synchronous parent
commit boundary, not an image component effect.
