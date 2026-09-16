# Shared session-menu deletion regressions

Run from the repository root:

```sh
node --test scripts/tests/session_delete_frontend.test.mjs
node scripts/tests/browser/session-delete/run.mjs
```

The first command needs Node only. The native suite needs Node 22+ and installed
Chrome/Chromium; `SNOW_CHROME_BIN` selects an explicit browser executable. Nothing
is installed. Neither command starts a Snow manager, runtime worker, provider,
registration, or real saved session. Chrome uses a fresh temporary profile and a
bounded loopback fixture server; both are cleaned up on success and failure.

## What runs

`run.mjs` reads the current production `menus.js`, generated React bundle,
`app.js` navigation prefix, and the CSS files in production `pages.html` load
order once at startup. `fixture.mjs` supplies a small real-DOM sidebar
projection, fictional inventory, and deferred deletion responses. The fixture
uses the real shared-menu implementation and shell routing—not a menu adapter.
Its current live row includes the existing Rename ellipsis, and a recording-only
`SnowConversation.rename` boundary verifies that Rename keeps its real launcher.
It mirrors the first-party latest-navigation-wins controller, the fixed
`#workspace` replacement boundary, and each New anchor's real `href` plus
`data-snow-navigation` marker. Native geometry and focus checks use actual browser
elements and production CSS, not a VM's geometry model.

This is a **component/transport contract suite**, not a Go-template export or a
real-manager end-to-end deletion test. Existing manager, backend, and exported-page
suites remain responsible for authentication, filesystem deletion, full app
lifecycle, and complete shell/template integration. Existing live-stream tests
are not modified or executed by this runner.

All `fetch` traffic is strictly mocked: only exact fictional sidebar inventory
GETs and deletion POSTs are accepted. Other transport or session-selection/new
intents fail the scenario. Most navigation is recorded without changing the page.
Two integration scenarios use the **real first-party `SnowNavigation` fetch
controller** against exact, held loopback fixture routes, never manager endpoints:

1. A confirmed cold viewed-session deletion swaps in an empty conversation and
   pushes `/?view=projects&project=a&new=1` into browser history.
2. A later navigation to workspace B supersedes a held deletion navigation to A.
   The old request is canceled; releasing its stale response cannot replace B or
   overwrite the newer URL.

No time-based sleep hides a race. The suite releases each deletion response and
native navigation response explicitly, waits for DOM predicates with
`MutationObserver`, and uses CDP page-load events. Deletion's 15-second callback
is delivered manually to the real `AbortController` instead of waiting 15 seconds.
DOM, navigation, page-load, and whole-suite deadlines fail closed. A real keyboard
gesture opens the pending-Escape scenario: Chromium can intentionally make its
native dialog cancel event noncancelable for dialogs opened only by untrusted
script clicks, which is not equivalent to user interaction.

## Coverage

The native suite currently has **257 assertions across 58 scenarios**:

- `delete_supported: false`, absent or non-boolean capability: no Delete menu item.
  A missing/non-string `active_session_id` also fails closed. Current Rename
  remains available without deletion capability.
- Explicit supported inventory: exactly one accessible **⋯** launcher per row,
  reusing the existing current Rename launcher. No visible trash icon, inline
  Delete control, or extra row button is allowed. Delete exists only as a menu
  item in the shared body popup; the current popup contains both Rename and
  Delete, with only Delete disabled for the active session and an explanation.
- Opening and closing the popup sends no inventory, deletion, activation or
  navigation request. Escape restores focus to the ellipsis. Rename still
  delegates to its existing owner with the actual row launcher.
- Unavailable inventory disables Delete rather than the whole shared menu;
  pending inventory refresh revokes a previously enabled popup action while
  leaving Rename usable. Capability withdrawal hides the unusable non-current
  launcher, closes only its own open popup, and returns focus to its surviving
  session link. Current Rename and another workspace’s popup retain their
  controls and focus; a confirmation from the revoked launcher also falls back
  to the surviving row link.
- Confirmation names the immutable target, renders hostile titles as text, warns
  that deletion is permanent, and starts unchecked. Synthetic submit events
  cannot bypass acknowledgement. Cancel and native Escape send no request and
  restore focus to the shared ellipsis. Reopening resets acknowledgement.
- Exactly one POST to the captured project/session, even if trigger metadata
  changes or submit is dispatched twice. Form fields are exactly `csrf`,
  `confirm=delete`, and `instance_id`; credentials/media types are checked.
- All pending controls are disabled; native Escape cannot conceal an admitted
  request. A matching receipt removes only the exact project/session row,
  preserves other row identities and draft text, emits exact
  `{project, session, instance}` metadata once, and restores focus to the
  workspace disclosure when its trigger no longer exists.
- Only a deleted, currently viewed **cold** session navigates to `new=1`; no
  runtime activation, session switching, prompt send, or automatic deletion retry.
- Wrong project/session/instance, missing instance, non-true `deleted`, HTTP
  400/401/403/404/409/500/503, timeout, disconnect, invalid JSON/UTF-8, and oversized
  responses cannot emit false success. Uncertain outcomes retain the row and
  allow dismissal, not automatic replay. Proxy HTML is never rendered; bounded
  plain-text application errors remain actionable.
- Captured popup callbacks cannot open confirmation after project/session/instance
  changes, root replacement, target activation, capability withdrawal, unavailable
  inventory, trigger detachment, or popup dismissal. No callback may authorize a
  different row merely because it retains a former Delete menu item.
- Confirmation from a replaced workspace, changed project/session/instance,
  now-active target, revoked capability, or missing CSRF cannot mutate. Native
  pre-replacement cleanup cancels unsubmitted confirmation. A late receipt cannot
  navigate a superseding root.
- Native ellipsis → popup → confirmation keyboard/focus, menu/row hit testing,
  long-name wrapping, horizontal bounds and scrollable acknowledgement/submit
  controls at **1280×740, 1280×240, 320×740,
  and 320×240**. Both popup and confirmation remain bounded; Escape returns a
  visible ellipsis at each size.

The **13 Node tests** execute the bounded production `app.js` prefix using the
existing `workspace_session_flow.test.mjs` VM-fixture pattern. They verify asset
ordering and the actual `snow:session-deleted` consumer: only matching visited
ownership is cleared, matching startup text is retained and retargeted to New
with its old acknowledgement revoked, and ordinary/edit/reuse/uncertain drafts
(including unrelated workspace/session drafts) are untouched. They also cover
malformed metadata and idempotent repeated notification. They do not claim to
simulate the complete application DOM; that is the native suite's role.

Every scenario uses only fictional public fixture data and the deliberately
non-secret `fixture-csrf-not-a-secret` marker. The runner continues independent
scenarios after failures, prints bounded diagnostics, and exits nonzero if any
scenario fails. No evidence screenshots or other persistent artifacts are
created by default.
