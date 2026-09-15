# Stop and reuse — production-browser regression checks

Run from the repository root:

```sh
node scripts/tests/browser/stop-reuse/run.mjs
```

Requirements: the repository's Go toolchain, Node 22+, and installed Chrome or
Chromium. `SNOW_CHROME_BIN` selects a nonstandard browser executable. The normal
matrix runs **320×740 and 1280×740, each dark and light**. For a focused rerun:

```sh
SNOW_STOP_REUSE_WIDTH=320 SNOW_STOP_REUSE_THEME=dark \
  node scripts/tests/browser/stop-reuse/run.mjs
```

The latest complete run passed **840 assertions, zero failures across four
reports** (210 assertions per report). That is this focused feature matrix, not
the repository's broader layout or release verification gate.

## What actually runs

The runner invokes the existing Go `TestExportHarnessVisualFixtures` exporter,
then serves its **unchanged production HTML, embedded app.js, other JavaScript,
and stylesheets** through a bounded, loopback-only HTTP fixture. The `workflow-cancel`
export must expose `data-turn-cancel="true"`; the runner refuses to manufacture
that capability or any product controls. It reuses `live-stream/cdp.mjs` and the
production-export/polling fixture approach from `permission-policy/`.

Chrome runs with a fresh temporary profile against an ephemeral loopback port.
No user manager, port 7331, Snow/RPC worker, credentials, or provider is used.
Every product activation is a native CDP pointer/key event. Composer text and
selection use native CDP insertion/editing commands, including `selectAll` for
cross-platform headless Chrome. DOM evaluation observes rendering, identity,
focus, and selection; it never calls production action functions or dispatches
synthetic DOM events. The only test-time browser timing adjustment accelerates
public polling/reconnect timers; it does not replace `fetch` or application
logic. All application network requests must stay on the fixture's own origin.

The mock server captures every mutation and rejects unexpected paths, methods,
CSRF/instance fields, duplicate/extra form fields, oversized prompts, and stale
turn tokens. Successful prompt/cancel responses use the actual production
`{"success":true}` acknowledgement shape; authoritative runtime state arrives
separately through GET snapshots. Held POST acknowledgements and malformed
responses are controlled exclusively by the HTTP fixture.

## Covered behavior

- Pointer or keyboard **Edit & continue** on each of three specific old user
  rows, including non-latest rows, loads that exact source into the composer.
- Opening, locally editing, and canceling do not mutate anything. Cancel restores
  the exact prior unsent draft and native selection. Original history stays
  unchanged; assistant, truncated, missing-source, UTF-8-byte-oversized, and
  character-oversized sources are not reusable. The production `saved-user` export proves non-live
  saved user text has no available Edit & continue action or active composer, and
  reading it or pressing the Send shortcut never activates or mutates anything.
- Explicit edited Send appends exactly one prompt to the same instance/session.
  Original user and assistant nodes retain identity and exact source text across
  the new turn. The earlier unsent draft is restored after successful Send.
- A native Send double-click cannot turn its second click into Stop or send a
  duplicate prompt.
- Stop becomes available from a verified running snapshot/token even while the
  prompt POST acknowledgement is held. It submits one token-bound cancellation;
  repeated clicks stay locked. Stopping persists after acknowledgement until
  authoritative turn completion and cancellation-dispatch retirement, without
  aborting the prompt fetch. An idle snapshot with cancellation still pending
  keeps Stopping visible and Send/edit disabled; it does not announce Ready.
- Cancellation never retargets a later turn, including when the old cancellation
  acknowledgement arrives after a newer turn has started. A fresh next-turn token
  also retires old Stopping when an intermediate idle snapshot is coalesced away;
  only a separate explicit Stop may target that independently observed turn.
- Permission and input attention-seat Stop work both normally and during held
  prompt acknowledgement, preserve drafts, and do not submit answers.
- Idle, missing-token, unknown-status, offline, and replaced-instance states
  remain conservative. Unknown outcomes do not automatically replay commands.
- Empty/missing-status/wrong-token/false-success/extra-field cancellation
  acknowledgements and server rejection/503 responses cannot fabricate successful
  cancellation or prematurely enable Send. Unknown edited-send outcomes retain
  text and still allow restoring the earlier draft without retrying.
- A worker failure during cancellation retires Stop, preserves the draft and
  unknown-outcome warning, and keeps Send/edit/review-to-retry disabled. Explicit
  Close confirmation remains available: opening or dismissing it does not mutate;
  confirming it sends one bound Close and returns to production saved history
  without another cancellation, prompt, or activation.
- Bounds checks cover editing, normal Stop, and permission/input Stop in all four
  viewport/theme combinations; the conversation does not overflow horizontally.
- All scenarios check captured mutations for forbidden branch, switch, restart,
  or activation calls and production JavaScript errors.

## Production fixtures and evidence

This suite deliberately does not test RPC/backend cancellation internals; those
belong to the Go tests. Production Go fixture changes are owned outside this
folder. The runner requires both parent-owned exports: `workflow-cancel` for the
token-bound live capability and `saved-user` for inactive saved history. It does
not change the existing legacy `workflow` fixture or its `/abort` tests. The
mocked successful Close recovery returns the unchanged exported saved-user page.

For optional PNG evidence of **Editing a copy** and **Stopping** in every report:

```sh
SNOW_STOP_REUSE_EVIDENCE=1 node scripts/tests/browser/stop-reuse/run.mjs
```

The runner writes bounded PNGs to a separate, retained temporary evidence
folder and prints their exact paths in each report's `evidence` array. Evidence
uses only public fixture text and contains no user conversation or credentials.
Both evidence-enabled width-specific runs also passed all 828 assertions in
total; eight PNGs were generated and their dimensions checked against the
requested 320×740 / 1280×740 viewports. Screenshots are supplemental artifacts,
not a substitute for assertions or a claim of manual visual review.

The runner removes its temporary exported assets and Chrome profile on exit.
Chrome is closed gracefully before bounded fallback termination, avoiding
inherited subprocess pipes keeping Node alive. The runner reports each completed scenario matrix with assertion labels/failures and
returns a nonzero exit status on assertion failures, fatal checks, or unexpected
external application requests.
