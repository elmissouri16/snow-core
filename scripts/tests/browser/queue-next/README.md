# Queue next: native Chrome regression suite

```sh
node scripts/tests/browser/queue-next/run.mjs
```

Requires Node 22+ with native WebSocket, installed Chrome/Chromium, and this
checkout's Go toolchain. `SNOW_CHROME_BIN` selects an installed executable.
Default matrix: **320×740 and 1280×740, dark/light**. Narrow one report:

```sh
SNOW_QUEUE_NEXT_WIDTH=1280 SNOW_QUEUE_NEXT_THEME=dark \
  node scripts/tests/browser/queue-next/run.mjs
```

`SNOW_QUEUE_NEXT_EVIDENCE=1` retains screenshots in a separately reported temporary
evidence directory. The runner otherwise cleans up its generated fixtures,
profile, browser and ephemeral loopback listener.

## Isolation and real product surface

The runner invokes production `TestExportHarnessVisualFixtures`, serves actual
`workflow-queue` HTML and production JS/CSS verbatim, and requires exported
queue/edit/regenerate/cancel/streaming capabilities. Production `workflow-cancel`
and `saved-user` exports cover unsupported and inactive surfaces.

No synthesized controls, DOM replacements, fetch stubs, timer mocks or fake
EventSource are installed. Chrome CDP performs pointer clicks, native keyboard
shortcuts/Tab/Enter, text insertion and selection. DOM evaluations inspect state;
only the shared pointer helper scrolls real targets into view. Initialization
sets theme and passive error listeners. Reconnect really closes the HTTP SSE
response and waits for Chrome's EventSource to establish another subscription.

Only the public HTTP/SSE transport is simulated on an ephemeral loopback port.
No production mutation, user config, port 7331, credentials, provider, worker,
real session, installation or commit is involved.

## Public queue contract

Snapshot `queue` is `{token, revision, can_enqueue, items:[{id,text,state}]}` with
an independent outer snapshot `revision`. States are exactly `pending`, `starting`, `held`, and `uncertain`. A starting
reservation is locked; it is neither observed durable delivery nor uncertainty.

Every queue mutation binds exact fields:

| Route suffix | Form fields |
| --- | --- |
| `queue-enqueue` | `csrf, instance_id, session_id, queue_token, queue_revision, text` |
| `queue-update` | same authority fields plus `item_id,text` |
| `queue-remove` | same authority fields plus `item_id`, **no text** |
| `cancel` | `csrf, instance_id, cancel_token`, **not queue authority** |

Requests with unexpected/duplicate fields or wrong instance/session/nonce fail
strictly. Stale canonical revisions return CAS rejection, preserving local text.
Ordinary prompt, edit/regenerate, branch/fork, switch, restart and activation
routes are forbidden. Limits: eight items, 256 KiB aggregate UTF-8 text, 64 KiB
per message. Aggregate limits remain server-authoritative: rejecting an explicit
bounded mutation is valid, but retrying or falling back to a prompt is not.

## Covered behavior

- Visible explicit Queue next and accurately labeled native shortcut during a
  confirmed running root; normal Send unavailable and Stop still usable.
- No horizontal overflow, off-viewport Queue next or overlap with Stop in the
  matrix. Pending inputs are separate from transcript, never optimistic user rows.
- Enqueue once with exact source text and observed queue authority. Successful
  acknowledgment clears only the submitted draft; delayed acknowledgment retains
  concurrent composer typing and selection. Pending requests never debounce-retry.
- Separate stable-ID item editor; unrelated list updates preserve text/selection;
  Cancel stays local; Save and Remove bind explicit CAS revisions. Stale editor
  revision and update/remove racing delivery preserve review text, without retry.
- Actual delivery inserts user source chronologically before tools/answer.
  Source capability flags, not nearest-user guesses, determine Edit & resend /
  Regenerate availability on a delivered queued turn. No action is invoked by this
  fixture; core/backend tests own persisted span authorization and replay semantics.
- Late HTTP acknowledgments and out-of-order SSE cannot resurrect consumed IDs or
  an old root queue; commands never retarget a replacement instance/root nonce.
- A valid `starting` reservation shows locked progress, has no Edit/Remove/Copy
  action, and adds no chat row before authoritative delivery. A Stop latch alone
  keeps it starting; observed worker failure changes it to uncertain review.
- Stop during an in-flight enqueue retains held text. Held/uncertain items are
  review-only, visibly distinguish possibly-delivered input, and require explicit
  Copy to draft. Earlier drafts can be restored. Removing review text never starts
  work or reverses effects.
- Permission/question takeovers retain the composer draft and separate queue;
  native SSE reconnect reads retained review state without replaying input.
- Closed/rejected/unknown/malformed responses, foreign session/instance/token,
  unsafe revisions, malformed item schema, full queue, blank and oversized UTF-8
  input all preserve text and never fall back to ordinary Send.
- Unsupported host and saved inactive conversation have no synthetic queue or
  worker activation.

The fixture models one same-instance root run: it never starts a worker for a
queued item. This **does not verify actual agent delivery boundaries, durable
append-only history, provider execution, or cross-surface RPC arbitration**;
those are Go core/RPC/web test responsibilities.

Each report contains assertion labels; fatal reports retain completed counts,
failure labels and request paths. A failed assertion, strict HTTP violation,
page error, unexpected external request, missing capability/export, or deadline
exits nonzero. This suite owns only this new directory and does not modify the
existing message-edit or message-regenerate suites.
