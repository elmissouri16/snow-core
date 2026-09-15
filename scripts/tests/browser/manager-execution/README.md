# Production Goal / Process browser verification

Run from repository root with Node 22+, the repository Go toolchain, and an
already installed Chrome/Chromium:

```sh
node scripts/tests/browser/manager-execution/run.mjs
# Optional private screenshots + bounded assertion/network reports:
SNOW_MANAGER_EXECUTION_ARTIFACTS=/tmp/snow-manager-execution-reports \
  node scripts/tests/browser/manager-execution/run.mjs
# One diagnostic matrix cell:
SNOW_MANAGER_EXECUTION_WIDTH=320 SNOW_MANAGER_EXECUTION_THEME=dark \
  node scripts/tests/browser/manager-execution/run.mjs
```

No install, npm dependency, external provider, shared port 7331, asset export
server, snapshot fabrication, response substitution, or production source edit.
One preflight race probe holds a real read response in the browser until a native
composer edit finishes; it does not change the response, authority or mutation path.
The default matrix uses **four independent fixtures**: 320/1280 pixels ×
dark/light, height 740. Each fixture has a private 0700 temporary root, HOME,
project, session database, Chrome profile and production HTTP listener on a
random loopback port. The runner compiles `./cmd/snow` once and launches only
`TestWebManagerExecutionBrowserFixture` in each cell. The real runtime manager,
RPC subprocess, App/Agent scheduler, approval broker and process supervisor run.
Provider responses alone are fictional and gated.

Every child is owned by the runner; terminal cleanup closes Chrome and fixture
stdin, awaits exits with bounded escalation, and removes fixture/build roots.
The complete cell has a 110-second deadline. Chrome/fixture output never prints
the cookie-bearing readiness frame. Reports retain public route names, statuses,
field names, fixture text and public goal/process identity only—not cookies,
CSRF values, request headers, private provider continuity, or full snapshots.
Reports cap HTTP observations at 2,000 per cell; screenshots are viewport-only
and optional. Artifact files are 0600; new artifact directories are 0700.

## What is real and what is setup

The runner reuses only the dependency-free CDP transport from `live-stream` and
the managed child lifecycle pattern from `manager-workflows`. All authored
files live in this separate directory.

**Activation setup exception:** the fixture's worker currently requires explicit
`--provider fake --model fake-1` arguments, while the real native activation form
intentionally uses host defaults and submits neither field. Native activation
therefore exits this fixture worker with code **95**. The runner bootstraps via
the real production HTTP activation endpoint with `confirm=activate` and the
explicit fake provider/model, using the actual page's CSRF field privately.
It then reloads the real page. This is an honest production API setup operation,
not a browser stub; native activation is **not** claimed as tested here. No
other mutation uses a helper HTTP request. Snapshot GETs are read-only evidence.

All Goal actions, composer edits, Stop controls, process approval, inventory and
logs, session Allow policy confirmation, and new-conversation switching use
native CDP keyboard/pointer input. Browser-native process Stop confirmation is
reviewed in the React-owned confirmation panel and explicitly accepted by
clicking `[data-process-stop-confirm]`; no `window.confirm` is used.

## Assertions

- Every stylesheet/script referenced by the actual activated production page
  has a real HTTP 200 response, including the generated React module and
  Processes/Queue assets. The actual head contains exactly one generated module;
  reasoning, compaction, goals, versions and history-controls classic scripts are
  absent from both the head and network. Readiness requires `SnowReactReady`.
- Browsing, activation and Goal inspection do not call the provider.
- Native Goal toggles local composer intent, without a dialog, inspection or
  execution. Toggling preserves the same editor node, draft, caret and selection.
  Secondary Details stays in the header menu and preserves its focus return.
- There is no second objective editor. Zero budget is rejected in Details;
  mobile exercises no budget, desktop a 100,000-token budget. Explicit Send
  submits the composer objective once and clears it only on a confirmed receipt.
- Editing during a held real inspection cancels preflight, preserving the draft
  and mode with zero Start/provider calls. Start uses the fresh exact inspected
  revision and unchanged captured authority. Resume retains its separate fresh
  exact-goal review and consent.
- Three distinct native `turn_done` events, plus a fourth held provider request,
  remain one correlated goal run without synthetic user messages or extra
  normal prompt POSTs. Queue next is unavailable during the run.
- Stop cancels the whole run and leaves a deferred non-complete goal. Reload
  never automatically resumes it. Explicit Resume names the exact goal ID;
  pre-released completion exercises a fast acknowledgement/completion race and
  checks the real HTTP `goal_run_ack` admission receipt.
- A normal prompt causes actual `process_start` Ask approval. Inventory is
  empty before approval; an opaque current process handle and real readiness
  output appear after approval. Active logs use a bounded plain-text node.
- Direct Stop under Ask fails without manufacturing approval and is not
  automatically retried. Explicit native session Allow needs its own consent.
  Reopening the workspace permits a new explicit Stop of the same handle and
  stopped logs reach EOF.
- New conversation changes actual scope; old process handles, output, log
  metadata and visibility must be cleared. Page remains within viewport width.

Plan/Deny stop rejection is covered by the real HTTP tests in
`cmd/snow/web_manager_execution_process_test.go`, not duplicated here. The
fixture emits a fixed plain readiness marker, so the browser checks actual
plain-text rendering and byte bounds but does **not** claim adversarial HTML or
32 KiB truncation payload coverage.

## Current composer-controls verification

The composer Goal relocation passes **324 assertions across four reports**, zero
failures, at **320/1280 × dark/light**. Native composer opening/Close, draft and
selection preservation, one explicit Start, serial turns, whole-run Stop, exact
Resume, retained menu entry points and process/permission/scope checks all pass.
The first rerun had 320 passes/four failures solely from a retired four-dialog
assertion (BUG-210); its corrected assertion requires the three actual dialogs
plus direct compaction's composer icon and inline status.

Tested generated module: **821,699 bytes**, SHA-256
`0f5e5bb84afadd7520c740ddaf42efb4af3aabe60da3aacba5b5b33aede195d6`.

## Previous runtime-panel execution evidence

After the Goal focus fix, the full **320/1280 × dark/light** production matrix
passes **388 assertions across four reports, zero failures** (**97 per cell**).
The native Goal modal-focus and Close-focus assertions both pass, alongside all
existing Goal/Stop/process/permission/scope assertions, actual module delivery,
and five-script retirement checks. The previously recorded stale process-log
presentation on New conversation is also no longer reproduced; its original
regression assertion remains intact and passes.

### Verified Goal focus regression (BUG-186)

Before the fix, native Session menu → Goal followed by the genuine
`(confirmed absent)` inspection left a modal dialog with focus on `BODY` outside
it: `{"modal":true,"inside":false,"activeTag":"BODY","activeId":""}`.
All four cells reproduced that failure; a focused 1280/light rerun reproduced it
again. Opening initially focused Refresh goal, which the immediate inspection
then disabled. Production now focuses the stable Close target before inspection.
The rebuilt production-bundle full matrix verifies focus stays inside the native
modal and returns to the surviving session-menu launcher on Close. The focused
assertions remain in place without relaxing readiness, statuses, or deadlines.

This is private fake-provider browser acceptance, not real-provider or
user-session coverage.

## Integrated React rerun

The prior full-React setup failed before readiness with `paired page lacks CSRF`.
That blocker is no longer current: `newManagerExecutionHTTP` now uses the shared
`fixturePageCSRF` helper, which accepts the bounded Shell JSON bootstrap and
rejects conflicting or malformed authority. It does not synthesize a CSRF token
or bypass the real production activation endpoint.

`node scripts/tests/browser/manager-execution/run.mjs` now passes **324 assertions
across four reports, zero failures** (81 per 320/1280 × dark/light cell) on the
integrated generated module. The asset enumeration is smaller because retired
classic scripts are no longer shipped in the head; the native Goal, whole-run
Stop, process approval/Stop, permission and scope-reset checks remain. The
activation setup exception above still applies. This newer run supersedes the
startup-blocked record, not the distinction from live-provider or user-runtime
acceptance.
