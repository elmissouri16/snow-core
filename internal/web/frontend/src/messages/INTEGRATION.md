# Transcript integration

Exports from `messages/index.ts`: `messages.{render,enhance,init,dispose,updateActions}` and
`visibility.{renderActivities,renderToolRow}`. Calls commit synchronously.

Mark `#live-transcript` and the saved history's message-only wrapper with
`data-react-messages`. Existing bounded server article/message-source/tool/image
DOM is projected once before mounting; no transcript JSON bootstrap is needed.
Keep `#live-activities` outside this wrapper, and leave the stream container,
notices, empty state and SnowScroll-owned jump indicator outside these roots.
The existing `render(transcript, messages)` then `renderActivities(region,
snapshot, transcript)` sequence remains supported. `enhance` is idempotent.

Install these globals before controller initialization; retire old messages.js,
markdown.js DOM enhancement and visibility.js's activity renderer (not Inspector).
No parent root may own transcript children or activities children. The transcript
root owns public activity rows through stable portals: portal slots move when an
explicit event-step marker is evicted, retaining the same details/summary nodes.

Parent controller still owns admission/HTTP, delegated edit/reuse/regenerate,
session identity and SnowScroll. React exclusively owns message button
`disabled`/`hidden`/`title` attributes and article `data-editing`; do not mutate
these nodes from app controller loops. Supply the existing admission decisions
through the synchronous, partial-merge presentation facade:

```ts
messages.updateActions(transcript, {
  reuse: {hidden: false, disabled: false, title: 'Optional reusable-row title'},
  edit: {hidden: false, disabled: false, messageID: 'editing-user-message-id'},
  regenerate: {hidden: false, disabled: false},
  historicalDisabled: false,
});
```

Every group and field is optional; omitted values retain their current projection.
Clear the editing marker with `edit: {messageID: ''}`. Explicitly clear
`historicalDisabled` when regeneration ends. `historicalDisabled: true` disables
both Edit and Reuse independently of their projected disabled flags; it does not
set Regenerate's own flag. All controls begin hidden and disabled, and action
state is never carried into a new scope or restored after disposal. The facade
ignores detached hosts. Identical projections skip rendering.

Each rendered row also applies its model's reusable/editable/regeneratable
eligibility and live-runtime scope; a parent `disabled:false` cannot override
row ineligibility. A non-reusable row receives the existing truncation title
rather than a supplied reusable-row title. Capability-specific button presence
is still scoped to the root's edit/regenerate support. Parent computations such
as `ready`, draft presence, operation phase and HTTP guards remain in app.js.
Do not read button state as admission authority.

React publishes `_snowCopyText`, `_snowReusable`, `_snowEditable`,
`_snowRegeneratable`, `_snowHasImages` on keyed article nodes. It does not store
private continuation or transport state. Call dispose before workspace cleanup.

For initial live chronological association, SSR must include a `section
[data-runtime-activity-group][data-message-id]` with `.activity-list` for each
public `role:"tool_activity"` marker. Initial activity details must also carry
`data-message-id` with the exact public `message_id`; no nearest-assistant
inference. Add `data-message-truncated` to saved articles (or use the shared
`.message-truncated` notice) if changing the existing saved truncation copy.
The existing saved truncation text is understood for cached templates.

Image reads are credentialed same-origin GETs to the exact existing bound image
routes only, one globally admitted request at a time, 800 retained jobs maximum,
8s including image decoding, 2 MiB streaming body cap, exact raster MIME check,
redirect refusal, identity checks and abort/Blob-URL revocation on retirement.
No image URL is navigated directly and no bytes enter browser storage.

`dispose` retains only an ephemeral public projection keyed weakly to the same
DOM host so bfcache/reinitialization does not lose saved history. It unmounts
all React roots, cancels copies/images and revokes Blob URLs; a scope identity
change rejects the retired projection. This is not persisted storage.

Verification: `src/messages/model.test.ts` is included in the frontend npm test list.
It also runs network-free with `node --experimental-strip-types --test
src/messages/model.test.ts`. Eight focused cases cover action projection defaults/merging, DTO limits, exact tool
association/outcomes, UTF-8 output budgets, authenticated image route identity
and the passive Markdown/link allowlist. An isolated production Vite bundle and
20-assertion native Chrome smoke passed (stable articles/code buttons, prefix
selection, parent admission presentation, saved/runtime disclosures, orphan moves,
credentialed Blob preview, markup rejection and saved SSR/lifecycle import).
This is not the full existing native parity/viewport/transport gate; run those
against the final integrated/generated asset and install only after verification.

The action-ownership follow-up also passes a 17-assertion native Chrome check:
synchronous action projection, partial-group preservation, per-row eligibility,
React-owned editing markers/titles, retained button node/focus, historical
blocking and clearing, plus fail-closed disposal/session-identity resets. This
replaces parent direct DOM writes; it does not move admission into React.

## Integrated thumbnail regression

`node --test internal/web/message_image_browser.test.mjs` now serves the checked-in
production React module rather than retired `static/messages.js`. Both tests
pass: 130 native assertions plus desktop/mobile sizing and server-side request
and cancellation checks. The fixture requires a same-origin cookie and the exact
raster Accept header, exercises one global read queue, rejects mismatched image
scope, and verifies timeout/failure stickiness, row/root cancellation, Blob URL
revocation and fresh same-host remount ownership. It also fails on uncaught
browser errors or React error diagnostics.

Pending-to-acknowledged identity changes may replace the image node while keeping
the message row; unchanged streaming text must retain the current image node
and Blob source. Saved SSR is captured once and replaced by React, not enhanced
in place. The integrated suite now additionally runs 52 parent-composed
ColdWorkspace assertions at 320/1280 through the production module: native navigation
canceled/committed requests, standalone-facade isolation, parent cancellation and
Blob revocation, exact scope rejection, and same-host remount idempotence. Page
lifecycle events are synthetic calls to the production listeners, not a claim
that Chrome actually admitted the page to bfcache. The bounded public SSR and
image responses are fixture data, not a running manager/provider. This supplements
the earlier parent-component evidence below; it does not replace the full browser
matrix.

## Saved history composed into ColdWorkspace

When the cold-workspace parent root replaces the SSR subtree, capture before its
first render and pass the actual component in the same parent tree:

```tsx
const projection = captureSavedHistory(
  element.querySelector<HTMLElement>('.catalog-history'),
  {project: props.project.id, session: props.sessionID},
);
const content = <ColdConversation {...props}
  history={<SavedHistory projection={projection}/>}/>;
```

Exports: `captureSavedHistory`, `SavedHistory`, and the opaque
`SavedHistoryProjection` type. Capture requires the exact expected project and
session to match the original saved host, rejects live/composite sources, and
returns `null` on a mismatch. It reads only the already bounded public SSR
message projection; no new API, JSON bootstrap, increased bootstrap cap or
caller-supplied HTML is introduced. Retain the handle for subsequent parent
renders rather than recapturing a React-owned tree. Handles are ephemeral weak
keys with private public-DTO projections, not serialized or persisted data.

`SavedHistory` renders the existing keyed Message components immediately in its
parent tree, including copy controls, Markdown, tool disclosures and thumbnails.
It has no inner root or effect-time flush. Its owned callback ref supplies image
scope after commit; the complete text/tool content exists in the first commit.
The `.catalog-history` host carries `data-react-saved-history`, NOT
`data-react-messages`. Global enhancement and standalone render/action facades
skip this composite boundary, preventing nested competing roots.

The parent React tree owns this component's lifetime. Its unmount cancels its
copy/image work and revokes Blob URLs. `messages.dispose()` now unmounts only its
standalone roots, whose existing component cleanup releases their image jobs;
it cannot cancel a still-mounted parent-owned saved-history thumbnail. Actions
remain hidden/disabled and no saved history control gains admission authority.

Parent-composed verification: 21 native Chrome assertions pass, including exact
scope rejection, first-commit JSX/escaped Markdown, source copy metadata,
disclosure/node retention, enhancement/render boundary refusal, authenticated
saved-image reads, independent facade disposal, parent-unmount request abort and
Blob revocation, and rejection of a forged projection handle. Two image reads
were observed; the held read was aborted by the parent unmount, not global
facade disposal. Keep the opaque handle with the parent's validated page state
for same-host remount/bfcache recovery: once the parent unmounts, its original
SSR has been consumed and cannot be recaptured. Never reuse that handle for a
different project/session identity.
