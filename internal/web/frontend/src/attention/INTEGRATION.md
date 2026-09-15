# Attention integration contract

Import `{attention}` from `./attention/index.ts` and expose `window.SnowAttention = attention` before app init. Retire static attention.js from production assets; do not independently mount from main.

Go slot: keep the existing outer `#live-attention` element and add `data-react-attention`. It starts hidden; React owns every descendant.

API remains `init({root, project?, session?, instance?, nonce?, onLayout?, onTakeover?})`, `render(snapshot, {safe,busy,canStop,stopping})`, `dispose({clearDrafts?})`.
`render` returns `{takingOver:boolean}`. Supply `onTakeover(active)` at BOTH existing init sites (initial and switch): parent sets `#live-attention.hidden = !active`, `#live-composer-seat.dataset.attention = String(active)`, and `#live-composer-normal.hidden/inert = active`. Only these outer controller-owned nodes may be mutated; never modify composer textarea or descendants of a React root. Callback runs on init/render/dispose; normal draft preservation stays in app/SnowLiveView.

Attention retains only local question answers, paging, collapse and validation. All HTTP/CSRF, permission/input/Stop action admission, deadline, stale result and ACK handling remain in app. Forms/buttons retain native delegated hooks. React flushSync commits local answers and control safety before native parent reads/POST. No React submit handler sends a request.

Remove app's obsolete `other.checked = true` attention input mutation; React now owns that selection. Keep delegated submit answer extraction and validity clearing (the latter can be removed because React validates before delegate). No approval is automatic; truncated/incomplete summaries cannot Allow.

Parent owns main/templates/app changes, generated bundle, browser matrix and install-local. Critical checks: manager-execution, harness-layout attention keyboard/scroll, live-stream permission/input, queue attention draft, stop-reuse, and native focus/IME across SSE refresh.

Verified locally: `node --experimental-strip-types --test src/attention/model.test.ts`
passes 5 tests; `npm run typecheck` passes after the concurrent reader fixes.
Parent should add attention/model.test.ts to the configured npm test command.
The component preserves the exact recommendation suffix handling (not first-option
endorsement), native radio values, 8192-character textarea cap, bounded 16/32
question/options projection and 64-KiB UTF-8 answer aggregate validation.
Question/request/turn keys are canonical public fields, not snapshot revision or
object identity. Identical refreshes do not remount or refocus fields; only
explicit question navigation/choice/focus invokes local focus handling.
