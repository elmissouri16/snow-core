# Native Chrome Regenerate reply regression suite

```sh
node scripts/tests/browser/message-regenerate/run.mjs
```

Requires Node 22+ with native WebSocket, installed Chrome/Chromium, and this
checkout's Go toolchain. `SNOW_CHROME_BIN` selects an installed executable.
Default matrix: 320×740 and 1280×740, dark/light. Debug one report with
`SNOW_MESSAGE_REGENERATE_WIDTH=1280 SNOW_MESSAGE_REGENERATE_THEME=dark`.
`SNOW_MESSAGE_REGENERATE_EVIDENCE=1` retains bounded screenshots in a separately
reported temporary evidence directory.

## Boundary

The runner invokes `TestExportHarnessVisualFixtures`, serves the **actual
production `workflow-regenerate` HTML and JavaScript/CSS**, and requires exported
regeneration/edit/cancel/streaming capabilities. Existing `workflow-cancel` and
`saved-user` exports cover unsupported and inactive surfaces. No synthesized DOM,
fetch, timer, EventSource, provider or worker mocks are installed. Native CDP
pointer, Tab/Enter/Space/Escape, text insertion and selection operate controls.
Runtime evaluations inspect DOM, apart from the existing runner's scroll into
view for native pointer targeting and initialization of theme/error listeners.

Only the public HTTP/SSE transport is simulated on an ephemeral loopback port:

- `POST …/message-regenerate-prepare`: exact `csrf, instance_id, message_id`;
  response binds project/session/instance/message IDs and `edit_token`, **no
  original prompt text**.
- `POST …/message-regenerate-commit`: exact `csrf, instance_id, edit_token,
  confirm=regenerate`, **no replacement text**. Returns same-session/new-instance
  authoritative prefix through the exact original user, with no duplicate user.
- `POST …/cancel`: exact `csrf, instance_id, cancel_token` for the regenerated run.

Prompt, edit, branch/fork, switch and activation paths fail strictly. The fixture
retains its old archive; this is **not evidence of real append-only persistence**.
Core/SDK/HTTP Go tests own persistence and server-side action-token isolation.
The browser suite verifies that it never routes regeneration authority through
Edit & resend or normal prompt controls.

No real user config, port 7331, provider, worker, credentials or session files
are touched. Chrome and generated fixtures/profile are cleaned up. Optional
screenshots intentionally remain for inspection.

## Coverage

- First/middle/latest final-response selection; one action per multi-tool-step
  turn; none for prefaces, plan/tool-step, truncated, orphan or user rows.
- Read-only prepare; native accessible confirmation warns that later conversation
  is replaced, tools may run again, and previous file changes are not undone.
  Native keyboard cancel/Escape restores trigger focus and keeps draft/selection.
- Confirm once without prompt text; repeated Enter never double-commits; local
  draft/selection never submitted or replaced. Concurrent typing during delayed
  acknowledgment survives subsequent projection/answer updates.
- Same chat/session/title, new instance, original user retained exactly once;
  selected response's tool/plan steps and later suffix disappear while previous
  turns remain. New answer starts there, rather than after discarded suffix.
- Old SSE subscription retires; replacement SSE subscribes. Late old traffic
  cannot resurrect suffix. Fast completion before a delayed running acknowledgment
  renders the retained user and regenerated answer only once.
- Stop targets new instance/turn. Cancel preparing, orphan prepare after newer
  typing, source revocation, runtime replacement, malformed/stale/expired and
  unknown responses never auto-replay, send a prompt, or misuse edit authority.
- Visible unknown-outcome review notice and explicit dismissal; unsupported-host
  and saved inactive pages never synthesize regeneration or activate workers.

Each report includes assertion labels. Errors retain completed-assertion counts
and failure labels. Assertion failure, strict HTTP violation, page JavaScript
error, unexpected external request, missing capability/export or deadline exits
nonzero. Run the existing edit suite separately to check Edit & resend regression:

```sh
node scripts/tests/browser/message-edit/run.mjs
```
