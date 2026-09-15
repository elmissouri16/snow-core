# Conversation width integration

`static/conversation-width.js` currently creates visible separator divs, so this
port is necessary. Register `width` from `./width` as `window.SnowWidth` and retire
that classic script. The facade retains `init(region)`, `reset()`, and `dispose()`;
`init` also returns its controller. No new transport or storage format.

Parent template supplies exactly one EMPTY foreign mount inside `#live-stream`,
after existing transcript/activity/notice children:
`<div data-react-width-controls></div>`. Keep it outside the messages root,
inside the native stream: handle-wheel forwarding verifies stream ancestry.
No parent React root may reconcile its children.

External `conversation-width.css` must change the existing direct-child selector
`.conversation-stream > .chat-width-handle` to
`.conversation-stream > [data-react-width-controls] > .chat-width-handle` (or cover
both), retaining all fixed-position, pointer, and z-index declarations. Add
`[data-react-width-controls] { display: contents; }` externally, not inline in Go.
The wrapper introduces no flow box/scroll range. Other `.chat-width-handle`
selectors work unchanged, including generated grip pseudo-elements.

JSX owns both visible separators, labels, role/orientation/controls, title,
visibility/tab order, width ARIA values and dragging marker. The source controller
retains exact native pointer/capture/keyboard/nonpassive-wheel listeners and
viewport/observer/RAF/measurement algorithms; owned refs receive geometry styles.
`SnowScroll.beforeUpdate/afterUpdate` still wrap column-width style updates.
The reader facade must be installed before width initialization.

Dispose before scope/DOM retirement. Native chat-width scenarios remain the
verification authority: storage errors, bounds, preview/commit/cancel, focus,
wheel intent and reader anchoring. No additional harness is introduced.

## Verification

Focused strict TypeScript compilation of `src/width/index.ts` and its imports
passed. `npm test` passed 83 existing tests. The initial full `npm run typecheck`
was blocked only by concurrent parent `src/main.tsx` integration errors (unused
imports and the navigation callback return type); no width errors were reported.
Re-run the full typecheck after integration settles.

The native command `node scripts/tests/browser/chat-width/run.mjs` must run after
parent template/CSS/registration integration and regenerated production assets.
This folder-only port did not build generated files or install a binary.
