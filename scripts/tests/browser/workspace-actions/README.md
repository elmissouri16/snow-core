# Workspace row actions and grouped inventory

Run all three existing acceptance entry points:

```sh
node scripts/tests/browser/workspace-actions/run.mjs
node scripts/tests/browser/workspace-actions/grouped.mjs
node scripts/tests/browser/workspace-actions/browser.mjs
```

All now execute **`internal/web/static/generated/app.js` in native Chromium**.
Neither `shell.js` nor `sidebar-sessions.js` is an acceptance authority. Requires
Node 22+ and installed Chrome/Chromium (`SNOW_CHROME_BIN` selects an executable).
The exported-page suite additionally requires Go. No npm dependencies are
installed. Each bounded one-shot runner owns and cleans up its temporary profile,
loopback server and Chrome process; none starts or restarts a user manager,
provider, runtime worker, authentication store or registration.

## Native component contracts

`run.mjs` and `grouped.mjs` share the small local `component.mjs` fixture. It serves
the current generated module and production `menus.js`, with empty React shell
hosts and a validated public shell bootstrap. It records owner intents/navigation,
serves deferred read-only inventory responses, and observes real native DOM,
focus, events and React commits. Ordinary actions use CDP pointer/keyboard input
with strict hit testing. Only modifier/default-prevention checks and deliberately
retired detached-node probes dispatch synthetic clicks; those are **not** claims
of native navigation or stale callback authority. Detached anchors retain browser
GET defaults, which the stale-intent probe suppresses solely to keep its fixture
page loaded. React-owned descendants are never cloned or imperatively rewritten.

Both runners print the generated module's size and SHA-256. They fail on uncaught
exceptions, browser/React console errors or warnings, unexpected HTTP requests,
external browser requests, or non-GET component transport. The hidden Settings
surface's read-only browser inventory is explicitly mocked and counted separately
from sidebar inventories.

### Routing (`run.mjs`)

**25 assertions** cover:

- Cold, live and other-project New emit exactly one `snow:session-new` owner
  intent, with project and actual launcher. They do not forward hidden workflow
  controls or issue incidental GETs. Disabled current/global New fail closed.
- Global New uses the current workspace; no selection retains the home callback.
  Modified New links preserve browser default semantics without owner intents.
- Live-inventory saved links emit exact project/session/instance selection;
  validated cold inventory instead uses the navigation callback.
- Production body portals offer only Settings/Remove, retain managed launcher
  ARIA and restore focus on Escape. Current project URLs retain the saved session;
  other project URLs are canonical and session-free.
- Mounted-project Settings/Remove dispatch `snow:inspect-project` presentation
  requests only. Other-project Remove dispatches exactly one canonical navigation,
  with the actual anchor's fragment/history metadata. No removal is submitted.
- Actual ancestor cleanup/remount retires old React launchers and popup actions.
  Final totals are exactly **5 session intents, 2 inspection requests and 3
  navigation callbacks**, with no automatic replay or mutation.

Current navigation anchors have `hx-push-url` and `hx-sync` **metadata only**.
There is no `hx-get`, `hx-post` or competing HTMX descendant processing. The app
owns activation/stop confirmation; these component tests do not replace it.

### Grouped inventory (`grouped.mjs`)

**26 assertions** cover selected-only startup, independent disclosures, selected
cold rows, validated live instance stamps, zero-DOM-mutation/focus-preserving
no-op visibility, keyed row reuse, explicit refresh, race cancellation, stale
response rejection, owner retirement, bounded rows, malformed scope/size
rejection, text-only untrusted names, explicit Retry, and activation preemption.
The complete schedule makes exactly **16 sidebar GETs and 3 browser inventory
GETs**, with zero session, navigation or inspection intents and no mutation.

The lifecycle expectations deliberately reflect the current React contract:

- Collapse/reopen of a loaded branch reuses its cache without another GET.
- Explicit invalidation refreshes; collapse/reopen during a pending read does
  not create concurrent reads. Invalidation is the explicit superseding operation
  used to exercise out-of-order replies, including replies after abort.
- Real ancestor replacement dispatches `htmx:beforeCleanupElement`, removes the
  external host, creates **empty** replacement hosts, and dispatches
  `htmx:afterSwap`. Cached rows/disclosures persist, but every expanded inventory
  is revalidated once (two, then three reads in this schedule). The retired
  script's zero-read root-cloning expectation is not current React behavior.
- The 100-row paging control remains a hidden keyed element, not an absent node.

This minimal component fixture does not load full production page CSS or the app
transport owner, and does not claim manager-backed navigation or layout parity.

## Exported production-page integration (`browser.mjs`)

Exports the actual Go page and embedded assets, including real HTMX. It asserts
that the page loads the generated React module and no retired shell scripts. A
bounded loopback server serves only exported assets and an exact-project
canonical URL alias; the existing harness-layout fixture mocks public runtime
transport before scripts run. No fabricated other-project selected view is served.

**108 assertions across four reports** cover 1280×740, 1280×240, 320×740 and
320×240 in dark mode:

- Actual sibling controls, keyboard-opened body portal, viewport clearance,
  Home/End/ArrowDown, Escape cleanup and focus return.
- Current-project Remove opens Project and the existing unchecked confirmation
  without filesystem requests, navigation or POST.
- Current-project New during an authoritative running snapshot opens the existing
  Stop/New confirmation. Opening and Cancel send no navigation or POST.
- Disconnected New fails closed without navigation or POST.
- All 99 other-project New URLs and another project's actual portal menu URLs
  are session-free. This is URL verification, not end-to-end cross-project
  navigation coverage.
- A fresh load at
  `/?view=projects&project=ACTUAL_FIXTURE_ID&inspect=project#remove-project`
  selects Project, opens removal, leaves confirmation unchecked and makes no
  filesystem request or POST. Inspector Close stays reachable and returns focus
  at both short viewport widths.

Readiness includes a committed React shell. Mobile navigation is opened using
native keyboard input; the harness waits for the committed, non-inert drawer and
its app-owned animation-frame initial-focus handoff before focusing a row. This
repairs the old imperative-shell assumption that opening and immediately focusing
in the same JS evaluation is synchronous. It does not retry actions or weaken
native hit testing, focus, exact zero-mutation, or no-replay assertions.

Independent scenarios continue after failures; any failure exits nonzero. Final
checks additionally reject uncaught exceptions, React/browser console diagnostics,
external browser requests and any server-side POST.

Screenshots of the **open** workspace menu, row-opened removal confirmation,
Stop/New confirmation and URL-opened removal confirmation go to
`dist/workspace-action-evidence/`, alongside `report.json`; failures also receive
screenshots. `SNOW_WORKSPACE_EVIDENCE=0` disables persistent evidence. All evidence
contains public mocked fixture content, not real runtime/project data.

## Verification scope

The repaired suites passed **159 assertions total** (25 + 26 + 108). The exported
four-cell matrix passed twice consecutively, the latter with explicit console and
external-network rejection. These results concern the current generated bundle,
not the retired scripts, real-provider compatibility, reusable CI or release
readiness. The suites do not send or confirm a real New/Stop/removal mutation and
do not replace manager-backed lifecycle/security acceptance.
