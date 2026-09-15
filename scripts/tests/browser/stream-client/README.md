# Stream client deterministic unit regression

Run from the repository root:

```sh
node scripts/tests/browser/stream-client/run.mjs
```

Requires Node.js 22 or newer; verified with Node.js 24.16.0. No npm packages,
Chrome, Go build, server, provider credentials, or network access are needed.
The source path resolves relative to the runner, so invocation also works from
another working directory.

## What runs

The runner reads **the actual `internal/web/static/stream.js`** and executes it
unchanged in a fresh Node `vm` context for each test. It does not copy the stream
parser, retry logic, or lifecycle implementation into a fixture. Its browser
boundary is deliberately small:

- A stub document/EventTarget tracks visibility and listener removal.
- Controlled fetch and reader promises choose exactly when responses, chunks,
  EOF, and transport errors arrive. Fetch asserts that **every request is GET**
  with the expected instance-bound URL and read-only subscription options.
- Stub abort controllers expose cancellation without automatically settling a
  read. This is intentional: an already pending read can resolve *after* its
  connection is retired, reproducing the ownership race deterministically.
- Node's real `TextEncoder` and fatal streaming `TextDecoder` exercise UTF-8.
- A fake clock executes watchdog/backoff timers without sleeping. Pending
  requests, reads, timers, timer iterations, and microtask drains are bounded.
  A 20-second wall-clock deadline is only a failure backstop, not test timing.

The runner prints each pass/failure, a final passed/total count, and exits nonzero
if any test fails. There are no expected failures or automatic skips. Cleanup
closes each subscription and settles deliberately delayed operations even after
an assertion fails.

## Coverage

The 51 independent cases cover:

- Retired readers returning either `snapshot` or `closed` after explicit close,
  while hidden, and after a visible replacement has begun reading. Stale reads
  must dispatch zero callbacks and must not abort the replacement or clear its
  watchdog. Rejected retired reads and late fetch responses are covered too.
- Snapshot callbacks synchronously closing, aborting, hiding, or replacing the
  subscription during a multi-frame chunk: no subsequent snapshot or terminal
  frame may dispatch.
- Reconnecting callbacks synchronously closing, aborting, or hiding: no retry
  timer may be installed after that callback retires the connection.
- One-byte chunk boundaries through multibyte UTF-8, JSON, and frame delimiters;
  multiline data; ignored events; heartbeats refreshing the watchdog; invalid
  UTF-8/JSON/snapshot revisions; complete and partial oversized frames at the
  production 16 MiB bound.
- EOF exponential backoff and cap, reset after a valid snapshot, watchdog abort,
  transport rejection, and no mutation request or prompt replay.
- HTTP 401/403 auth termination, 404 closed, 409 replaced, corresponding SSE
  terminal events, **501-only legacy fallback**, 429 and server-error retries,
  wrong content type, and missing response body.
- Owner abort during fetch/read/retry, an already-aborted owner, initial hidden
  state, visibility retry cancellation, and disposal of timers/listeners.

## Scope: unit tests, not a real browser or RPC fixture

Despite residing under `scripts/tests/browser`, this is a **Node VM unit suite**.
It does not prove real browser fetch cancellation semantics, HTTP streaming,
provider-to-agent-to-RPC delivery, live DOM rendering, scroll/focus behavior, or
multiple prefixes becoming visible while a real turn is still running. Keep
those checks in the separate real RPC/browser streaming fixture. Passing this
suite is complementary evidence, never a substitute for that end-to-end check.

Maintainers can add the command above to affected-area verification alongside
that fixture. A failing production behavior should be fixed in production and
rerun; do not weaken the controlled late-read scenarios or turn them into skips.
