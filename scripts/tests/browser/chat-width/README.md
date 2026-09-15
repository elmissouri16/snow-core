# Chat width production-browser regression

```sh
node scripts/tests/browser/chat-width/run.mjs
# Optional screenshots + JSON report (fixed, git-ignored output only):
SNOW_CHAT_WIDTH_EVIDENCE=1 node scripts/tests/browser/chat-width/run.mjs
```

Requires Node 22+, the checkout's Go toolchain and installed Chrome/Chromium.
Set `SNOW_CHROME_BIN` for a nonstandard browser executable. No npm dependencies,
manager, live workers, provider credentials or external application requests.
The runner exports the actual `stream` Go template and embedded assets using
`TestExportHarnessVisualFixtures`, then reuses the public snapshot fixture and
CDP connection helpers from the existing browser harnesses.

Coverage includes native left/right pointer symmetry at 1280 and 1512px,
keyboard screen-space steps and Home, commit versus preview, Escape, real lost
pointer capture, native touch pointer cancellation, stationary/vertical-only
noncommits, min/max clamps, wider stored preference retention, reload and reset,
390px/inspector and short-height visibility, hidden-focus exclusion, native wheel
ownership/scroll-range isolation and immediate upward follow release, lifecycle
remounts, reader anchors, invalid storage,
quota-write failure, and a throwing localStorage getter. Width-only interactions are checked for
unexpected API requests and browser errors. Distinct synthetic WheelEvent cases
check line/page delta conversion, unconsumed Ctrl+wheel, and already-prevented
upward-event exclusion; these do not claim
native wheel-unit or browser zoom coverage. Public inspector requests remain
mocked and are isolated from that width-only audit.

Screenshots are opt-in and written only to `dist/chat-width-evidence`; temporary
fixture assets and the isolated browser profile are removed after each run.
This is a focused feature regression, not a replacement for the full layout
matrix or transport tests.
