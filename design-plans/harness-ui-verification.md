# Harness-aligned web UI polish: implementation and verification

## Reference and scope

Reference checkout: DeepSeek Harness `c291e7961a515f6d7af9304e7fd1d257929aef26`
(0.1.5-rc.2). Snow checkout: `db7b2185c86a6506006ff797dce3129e2de385f0`
plus the uncommitted web implementation. This record concerns the complete
**shipped Snow web-manager surface family**, not backend capability equivalence
with every Harness plugin. MIT attribution remains in `HARNESS-NOTICE.txt`.

The original failure was visible in the user's question screenshot: competing
composer seats, scrolling-away actions, and an overlapping Jump control. The
actual reference question composer was inspected in its synthetic fixture at
1512×740: normal composer absent while expanded **and collapsed**; separate
header/body/footer; 788.48px content width and a 60vh/520px cap. Source owners and
precise geometry are recorded in the attention, scroll, conversation, menu and
Settings plans in this directory.

## Requirement-to-evidence audit

| Requirement / surface | Implemented and directly verified |
| --- | --- |
| Pending questions and approvals | One composer seat; retained normal draft; one question page; exact labels and complete answer batch; IME/Shift+Enter; collapse remains takeover; fixed actions and independent body scrolling; authority warnings and truncated-summary prohibition retained. Production-template tests exercise 16 questions and 32 choices. |
| Streaming, not delayed final-message rendering | Instance-bound SSE delivers coalesced complete public snapshots. Real gated fake provider → actual agent → RPC subprocess → manager HTTP → unchanged browser assets displays first and second prefixes **before separately released completion**. No browser fetch/timer replacement in this test. |
| Conversation reading and tiny details | Explicit follow/reader ownership; measured 34px Jump clearance; stable saved/live IDs; mixed saved text/plan/text preserved; focused code-copy controls survive growth and head eviction and copy the updated code; genuinely overflowing six-column tables retain nonzero horizontal scroll. Public Working signal spans running output and disappears at completion; reduced-motion and forced-color fallbacks are defined. |
| Composer and live workflow | Canonical 98px docked composer, including 320px Plan mode; responsive inline toolbar groups; drafts and unknown-outcome review; Send/Stop ownership; post-acknowledgement controls remain inert until authoritative synchronization; rename/new/switch/close and sidebar identity remain consistent. |
| Menus and navigation | Workspace/model/mode/session/usage panes; long names and 100 workspaces; pinned Add workspace footer; keyboard focus scrolling inside menus; empty/loading/error/disconnected states; canonical 38px/36px New session controls; current sidebar row follows actual rename and switch results. |
| Settings, registration, folders and access | All three Settings sections, host folder populated/empty/limited/error states, explicit activation/registration, pairing/login errors; fixed dialog actions, native focus restoration and short-screen clearance. Snow-specific trust text is not replaced with misleading reference controls. |
| Saved history and inspector | All 35 inactive rows precede activation without overlap; independent scroll region. Files/Changes/Project have bounded content scrollers, reachable controls, file breadcrumbs, truthful loading/stale/empty states, inert semantic diffs and public tool disclosures. Original stale-response tests remain. |
| Responsiveness | Production Go markup/assets at 320, 360, 390, 768, 1024, 1280 and 1512 CSS-pixel widths, dark/light themes, and scheduled 740/360/240px states. Native CDP keyboard/pointer checks, scroll reachability, clipping and uncaught-error gates. Scale-1 visualViewport reduction exercises the keyboard-height listener. |
| Recovery and bounded resources | Closed/replaced/auth-required states fail closed with explicit review; no automatic worker reopening or POST replay. Deterministic old-reader/callback races; real watchdog reconnect; Go/race coverage for atomic subscriptions, expiry, caps and deadlines. Real blocked `net.Pipe` writes release stream slots without canceling the worker. |

## Final executed gates

The final screenshot-enabled layout command completed with exit **0**:

```sh
node scripts/tests/browser/harness-layout/run.mjs \
  --output-dir "$PWD/dist/polish-final-all" --screenshots
```

- **1,806 reports; 25,628 assertions; zero failures; 2,170 PNGs.**
- Evidence: ignored `dist/polish-final-all/layout-report.json` and PNGs.
- Earlier red runs remain diagnostic evidence, not acceptance: short dialogs,
  inactive flex/scroll regression, attention observer feedback, disconnected
  composer clipping, and a too-narrow desktop table test fixture were corrected.
  No assertion or browser error was discarded to obtain the final pass.

Additional executed checks:

```sh
go test ./...
go vet ./...
go test -race ./internal/web ./internal/agent ./internal/rpc \
  ./internal/session ./pkg/protocol ./pkg/agentclient/... ./cmd/snow \
  ./internal/subagent ./internal/app ./pkg/snowsdk
node scripts/tests/browser/conversation-workflow/run.mjs
node scripts/tests/browser/inspection-race/run.mjs
node scripts/tests/browser/stream-client/run.mjs
node scripts/tests/browser/live-stream/run.mjs
python3 internal/plugindocs/scripts/sync_resources.py
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
python3 scripts/check_benchmarks.py
git diff --check
```

- Workflow: **1,050 assertions** (75 × seven widths × two themes).
- Inspector: **360 assertions** (60 × three viewports × two themes).
- Stream-client: **51 deterministic Node VM cases**; unit evidence, not RPC.
- Live stream: **28 real RPC/HTTP/browser assertions**; isolated fake provider,
  unchanged production fetch/timers/handlers; screenshots in ignored
  `.snow/browser/live-stream/`.
- Changed Go files: 96 checked, none above 1,000 lines, none unformatted.
- First-party changed JS/MJS: 24 syntax checks passed.

## Boundaries, rather than fabricated parity

Snow's public contracts do not supply Harness multiselect/skip/plan-review,
provider effort inventories, archived/forked UI workflows, editor mutations,
private trajectories or historical per-turn tool associations. Those controls
are not simulated. Tools still run with the user's host privileges; loopback-only
deployment, trust/activation, discovery consent, permission gates and immutable
instance guards remain authoritative. SSE transmits public projections only.

Physical iOS/Android keyboard pan, actual pinch zoom, cross-browser engines and
real remote-provider behavior were **not** established by desktop Chrome
emulation or the isolated provider fixture. The visualViewport test is explicitly
simulated, not hardware evidence. Screenshot/layout passes are concrete
regression evidence, not proof of subjective pixel perfection on every device.

The reference browser was returned to its normal `http://127.0.0.1:3080/` route.
No reference prompts, approvals or host settings were changed. User-owned web
managers were not restarted. Installation follows repository gates; running
managers must be restarted by their owner to load newly embedded assets.

## Installation

After all gates above passed, `./scripts/install-local.sh` completed successfully:
`/Users/el/.local/bin/snow`, version `0.1.0-alpha.9`. This is a local checkout
install, not a new release/tag. No commits were created and unrelated working
changes were preserved. All managed verification processes exited; child agents
were closed. The running user-owned manager was deliberately left untouched.
