# Native Chrome Edit & resend regression suite

```sh
node scripts/tests/browser/message-edit/run.mjs
```

Requires Node 22+ (native WebSocket), installed Chrome/Chromium, and the checkout's
Go toolchain. `SNOW_CHROME_BIN` selects an installed executable. The suite runs
320×740 and 1280×740 in dark/light themes. Narrow a debugging run with
`SNOW_MESSAGE_EDIT_WIDTH=1280 SNOW_MESSAGE_EDIT_THEME=dark`; optionally set
`SNOW_MESSAGE_EDIT_EVIDENCE=1` for bounded screenshots in a separate temporary
output directory reported in JSON.

The runner invokes `TestExportHarnessVisualFixtures` and serves the **actual Go
`workflow-edit` export and production JavaScript/CSS**, plus existing
`workflow-cancel` (optional capability absent) and `saved-user` fixtures. Missing
edit/SSE capability fails early; tests never inject replacement controls. Native
CDP pointer, Tab/Enter/Space, typing, and selection operate the page. Runtime
evaluations only inspect the DOM, except the existing shared runner's scroll into
view for native pointer targeting and error/theme initialization. No DOM event,
fetch, EventSource, timer, provider or worker mocks are installed.

Only the public loopback HTTP/SSE transport is simulated, on an ephemeral port.
It strictly captures CSRF and instance-bound field sets for
`message-edit-prepare`, `message-edit-commit` and token-bound `cancel`; normal
`prompt`, branch/fork, switch and activation requests fail the suite. Prepare
returns authoritative text intentionally different from the displayed copy.
Commit replaces the visible prefix at the selected user, changes the instance,
and retains the session/title. The fixture's archive is retained separately;
**this does not prove real append-only persistence**—core/SDK/HTTP Go tests own
that verification. No real user configuration, port 7331, provider, worker or
session files are touched.

Coverage:

- First/middle/latest selection; exact local row identity; full authoritative
  text, not the bounded display copy; prefix retained and selected/suffix rows,
  answers, plans and tool-bearing rows discarded rather than appended after.
- Read-only preparation, local typing, canceled edit with exact unsent draft,
  selection/direction and focus restoration; native keyboard activation.
- One commit despite repeated submit; no optimistic old-source mutation during
  delayed acknowledgment; new runtime in same chat; old SSE cannot resurrect
  discarded suffix; fast terminal acknowledgment renders user/answer once.
- Stop bound to replacement instance/turn; invalid or stale preparation,
  concurrent typing/cancel while prepare is delayed; rejected/unknown/malformed
  commit retains edit text, requires review and never auto-replays.
- Empty/oversized replacements and preparations; nontext and truncated source
  exclusion; explicit Use as new prompt fallback only when capability absent;
  no edit activation on saved/non-live text; no horizontal document overflow.

Reports contain individual assertion labels and totals. A failed assertion,
fixture contract violation, page JavaScript error, unexpected external request,
missing fixture, Chrome startup failure or bounded deadline exits nonzero.
Chrome, SSE connections and generated fixture/profile directories are cleaned
up; optional evidence screenshots intentionally remain for inspection.
