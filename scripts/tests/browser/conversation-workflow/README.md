# Conversation workflow browser regression

Optional, network-free browser coverage using the actual Go page template and
all its embedded public assets. The runner exports a fresh production page on
every run; no copied HTML or hidden legacy model form is kept in this suite.

```sh
node scripts/tests/browser/conversation-workflow/run.mjs
```

Requires the repository Go toolchain, Node 22+ (built-in WebSocket) and Chrome/Chromium. The runner checks
fixed common executable paths; set `SNOW_CHROME_BIN` to an explicit browser
executable if needed. It launches an isolated temporary browser profile,
exports and opens the local fixture, and removes the profile/export on exit. No provider,
credentials, Go server, browser package installation, or external network is
required. Failure to find a browser is an explicit environment failure, not a
passing skipped check.

The fixture uses production-shaped form requests, `RuntimeSnapshot`, and
`RuntimeChoices` DTOs, including server-sanitized public message HTML. All
requests are intercepted in memory. Only polling timers are shortened. The
HTMX test shim exercises the real before/after-swap events without a server;
its `process` hook is a no-op because link rebinding is outside this shim.
File-origin history updates omit the app URL because `file://` cannot adopt an
HTTP-origin route.

124 assertions run at each width 320, 360, 390, 768, 1024, 1280, and 1512 in
both light and dark (1,736 assertions total). Height is 740px at width 360
and 900px otherwise:

- Passive page load and HTMX replacement never discover models. Explicitly
  clicking the model trigger opens the searchable list directly and authorizes
  exactly one instance/CSRF-bound discovery POST when inventory is absent;
  closing/reopening during that request never duplicates it. There is no
  additional Load host models action or Model/back submenu.
- Search autofocuses before discovery completes, filters provider, ID, and
  display name case-insensitively without network requests, and preserves its
  query/focus through snapshot repaint, asynchronous inventory, and refresh
  failure/retry. No matches has explicit feedback; one match never auto-selects.
  Home/End/Space retain native input behavior; ArrowDown enters the first model
  row, ArrowUp returns to search, and Escape restores the trigger focus. During
  IME composition, Escape and ArrowDown leave search focus/query intact without
  closing, navigating, preventing the input event, or submitting a request.
- Reopening reuses cached provider-grouped choices. Refresh models is explicit;
  pending discovery/refresh and active turns prohibit switching or duplicate
  loading. Errors expose Retry loading models. Wrong-nonce inventory and late
  responses from disposed workspaces cannot replace current choices.
- Selecting the current exact provider/model pair closes without mutation.
  Selecting a different provider with the **same model ID** sends exactly one
  mutation with that exact pair directly, without an Apply step.
- Instance/CSRF binding, authoritative mode changes, public plan Markdown,
  telemetry availability, and preservation of the public tool timeline.
- Rename, New, idle switching, native stop confirmation, nonce rotation,
  independent session drafts, and memory preservation across HTMX navigation.
- Offline/login-required status, no action replay, unknown mutation outcomes
  requiring explicit review, stale canceled poll delivery, and passive
  replacement requiring review/reload rather than silent retargeting.
- One 76px live-owned header; healthy connection row hidden while reconnect,
  login-required and stale-instance rows remain visible. Composer-owned Stop
  replaces Send only during active turns, stays disabled when unsafe/pending,
  sends an explicit identity-bound abort, and preserves the unsent draft.
- Rename cancellation followed by Settings Close/native Escape restores the
  Settings trigger rather than the session trigger, including after HTMX replacement.
- External 404 becomes terminal Runtime closed with visible explicit review;
  restoring transport cannot auto-reopen, resume authority, or replay an action.
- A delayed prompt POST disables duplicate Send immediately. Even an already
  terminal response remains Synchronizing with inert actions while the next
  bound GET is withheld; a fresh terminal read restores idle Send, never a
  synthetic running state. Streaming-delivery races have separate transport tests.
- No disk-backed prompt/guard storage, responsive sticky composer,
  project-navigation drawers, and read-only Files / Changes interaction.

The existing independent inspector race regression remains available:

```sh
node scripts/tests/browser/inspection-race/run.mjs
```
