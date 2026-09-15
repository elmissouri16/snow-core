# Chronological runtime tool timeline browser regressions

Run from the repository root with the repository's Go toolchain, Node 22+ and an
installed Chrome/Chromium:

```sh
node scripts/tests/browser/tool-timeline/run.mjs
```

Set `SNOW_CHROME_BIN` for a nonstandard browser location. No npm install or
browser download is performed. The runner exports the **actual Go templates and
embedded production assets** through `TestExportHarnessVisualFixtures`, serves
them on an ephemeral loopback port, and opens an isolated temporary Chrome
profile. It reuses `../harness-layout/fixture.js` for mock HTTP snapshots and
`../live-stream/cdp.mjs` for real Chrome DevTools Protocol input. The existing
`tool-rows` suite remains unchanged and is not replaced by this suite.

No user manager, worker, agent, provider, real tool, saved-session store, or user
configuration is opened. Production scripts receive synthetic public snapshots
through the mocked `fetch` response, rather than calling their render functions
with alternate DOM implementations. Runtime mutation requests fail closed in
the mock; the suite also asserts that no mutation other than read-like model
choice discovery was requested. Saved-page checks assert zero API requests.

## Matrix and assertions

Eight reports cover live `chat` and server-rendered `saved-markdown` at **320px
and 1280px**, each in **dark and light**, at a 740px viewport height. Assertions
exercise:

- Two explicit user turns: user 1 → one tool-only marker holding two glob calls
  → final 1 → user 2 → write marker → final 2. A new prompt must retain the old
  group, row and final-answer DOM identities and their chronological placement.
- Assistant intro → tools → assistant follow-up → tools → final, with explicit
  `role: "tool_activity"`, empty-text `step-N` message markers and activity
  `message_id` bindings. Real tool groups must not acquire assistant message
  bodies or copy controls.
- Distinct public activity IDs sharing the same synthetic raw provider call ID;
  presentation must not merge calls across prompts.
- A running-to-terminal update followed by polling, retaining the actual group,
  `details`, `summary`, output node, disclosure-open state and keyboard focus.
- No duplicate global footer rows when all activities have marker owners; mixed
  associated and unassociated activity still has exactly one row per public ID.
- Missing, legacy, invalid assistant/user, canceled, failed and unknown owners
  remain in the explicitly unassociated runtime fallback, never under an
  invented assistant owner. Removing an owner moves its retained row to fallback
  without erasing its open state. Known canceled calls keep their actual group.
- Saved-history associations, output, source data and DOM identities are
  unchanged by runtime activity or repeated saved-page enhancement. Unresolved
  saved results remain outcome-unknown.
- Defensive browser limits: the newest 128 activity rows, bounded output and
  summaries with truncation notices, and the last 100 chronological message
  entries. Message-limit eviction preserves a focused/open orphan row in
  fallback; empty markers introduce no visible blank answer. These are browser
  bounds, not the stricter Go runtime activity projection budget.
- Non-public raw arguments, root/private metadata, legacy output fields and
  activity/marker HTML do not appear in rendered text or DOM. Explicit public
  output containing script/img syntax remains literal and does not execute.
- Every tool summary remains within the viewport without document horizontal
  overflow, including oversized grouped output at 320px.

The runner sends **real CDP pointer, Enter, Space, Tab and Shift+Tab input** to
native tool disclosures inside a chronological group and in saved history. It
checks the real Chrome accessibility tree for the wire tool name, completed
outcome and expanded state, and verifies hover/focus disclosure affordances.
No synthetic `click()` is used to claim keyboard or pointer behavior.

This isolates presentation regressions. It does **not** prove backend root-event
filtering, raw-ID hashing, step generation, provider execution, persistence, or
server output budgets: those require the Go/runtime and real app/RPC fixtures.
The initial live shell and saved history use the Go exporter; chronological live
snapshots are synthetic public HTTP responses. These eight focused reports also
do not replace the full harness layout or existing tool-row/browser suites.

## Optional PNG evidence

```sh
SNOW_TOOL_TIMELINE_EVIDENCE=1 node scripts/tests/browser/tool-timeline/run.mjs
```

This saves 16 open/closed PNGs under `dist/tool-timeline-evidence/`: one pair per
fixture, width and theme. Live screenshots capture the two-user-turn chronology
before the adversarial bounds phase. They are supporting material for manual
review, not an assertion that visual review was performed.

The runner prints the Chrome version, a SHA-256 digest of the exported pages and
assets, each report's actual assertion/failure labels, and the aggregate count.
Temporary exports, browser profile and the loopback server are cleaned up on
exit. A failed assertion or fixture/browser exception exits nonzero. Export and
browser phases are each bounded to 120 seconds.

## Verified run

On the implementation checkout, Chrome **152.0.7977.84** passed **288 assertions,
0 failures, across all 8 reports**, including an evidence-enabled run. The
exported production digest was
`d11d0322cad9cbf425aca349933f6ef5095034eb61077fda43d5c566ae38bc4e`.
Both new JavaScript files also passed `node --check`. Rerun after final source
changes; this evidence is tied to that exported checkout, not a future build.
