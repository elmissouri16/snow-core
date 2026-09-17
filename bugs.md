# Known bugs

## BUG-232: Plugin reload fixture races its own delivery notification

- **Status:** Resolved — ordered drain, busy-boundary, package, and full-suite checks pass.
- **Severity:** Low
- **Surface:** JavaScript plugin reload API-version regression fixture
- **Evidence:** Exact-release CI run 35234481852 failed `TestReloadJavaScriptAPIVersions/1` on macOS when its immediate second reload returned `plugin reload: event delivery active`. A successful first reload asynchronously publishes `EvSessionUpdated`; the plugin manager's event subscriber correctly holds its delivery read lock while forwarding that event. The test could request its second reload before delivery completed, so the nonblocking generation-boundary lock correctly rejected it. The unapplied receipt confirmed that no partial reload occurred.
- **Fix:** Drain the ordered agent event stream after checking the first reload and immediately before asserting that an unchanged-byte second reload succeeds. Active or snapshotted delivery still rejects reload; the production nonblocking lock, generation boundary, notification, and busy-admission behavior are unchanged.
- **Verification:** The API-version fixture passes 100 normal and 100 race-enabled repetitions; the snapshotted-delivery refusal passes 100 repetitions; event-bus drain tests pass 20 repetitions; `go test ./internal/app ./internal/plugin -count=1`, `go test ./... -count=1`, and `go vet ./...` pass. Replacement exact-SHA CI and Documentation workflows remain required before tagging.

## BUG-231: Provider retry fixture fails the repository gofmt gate

- **Status:** Resolved — the repository format gate and focused package checks pass.
- **Severity:** Low
- **Surface:** Linux CI formatting gate for the provider retry regression
- **Evidence:** Exact-release CI run 35232873690 stopped its Ubuntu test job before tests because `gofmt -l $(git ls-files '*.go')` reported `internal/agent/provider_retry_test.go`. Local Go 1.27rc3 reproduced the same output. The file's `Model` field was not aligned with the adjacent `InternalContext` field in a composite literal; behavior and the macOS test job were otherwise unaffected.
- **Fix:** Apply Go 1.27rc3 `gofmt` to the fixture and retain the existing retry assertions unchanged.
- **Verification:** `gofmt -l $(git ls-files '*.go')` returns no paths; `go test ./internal/agent -count=1` and `go vet ./internal/agent` pass. Replacement exact-SHA CI and Documentation workflows remain required before tagging.

## BUG-230: Tool timeline treats Shell inventory reads as runtime activation

- **Status:** Resolved — the complete tool-timeline matrix passes.
- **Severity:** Low
- **Surface:** Saved-history tool-timeline browser regression fixture
- **Evidence:** `tool-timeline/run.mjs` passed all live-timeline behavior but failed four saved-history reports solely because `tests.js` asserted that the complete shared request log was empty. The current shared layout fixture intentionally records exact read-only browser and sidebar inventory GETs from the React Shell separately from runtime/mutation traffic; counting those reads as activation contradicted the fixture's own `nonInventoryRequests()` boundary.
- **Fix:** Use the shared filtered request projection for the saved-history no-activation assertion. Exact inventory validation, unexpected-request recording, runtime/mutation rejection, saved ownership, chronology, output bounds, and page-error checks remain unchanged.
- **Verification:** `node scripts/tests/browser/tool-timeline/run.mjs` passes 288 assertions with zero failures across eight live/saved, 320/1280 × dark/light reports. `node --check scripts/tests/browser/tool-timeline/tests.js` also passes.

## BUG-229: Queue next fixture counts Shell inventory reads as mutations

- **Status:** Resolved — the complete native Queue next matrix passes.
- **Severity:** Low
- **Surface:** Native Queue next browser regression fixture
- **Evidence:** The full `queue-next/run.mjs` matrix reported 804 passing assertions and 320 failures across four reports. Every functional queue assertion passed, but the fixture routed the React Shell's read-only `GET /access/browsers` and `GET /projects/{id}/sidebar-sessions?offset=0` startup requests into its CSRF-protected mutation parser. That recorded two expected-POST errors and two forbidden-route failures in each later transport-boundary check even though queue POST counts, exact queue behavior, page errors, and no-replay assertions passed.
- **Fix:** Handle only those exact bounded Shell inventory GETs before mutation accounting, matching the already-correct Edit & resend, Regenerate, and Stop/Reuse fixtures. Unknown reads, mutation paths, CSRF/instance/session/queue authority, field allowlists, counts, and no-fallback assertions remain strict.
- **Verification:** `node scripts/tests/browser/queue-next/run.mjs` passes 1,124 assertions with zero failures across all four 320/1280 × dark/light reports. The exact queue POST counts, field allowlists, CSRF/instance/session/queue binding, no-fallback checks, read-only Shell startup, page-error checks, and no-replay behaviors remain covered.

## BUG-228: Commit-message skills stop explicit commit and push requests

- **Status:** Resolved — active-skill boundaries no longer replace the enclosing user request.
- **Severity:** Medium
- **Surface:** Agent-skill activation during explicit Git workflow requests
- **Evidence:** Given `commit changes and push`, Snow automatically activated `caveman-commit`, generated a valid commit message, but then refused to stage, commit, or push because the skill says it only generates commit messages. Comparable coding agents use the generated message as one step and continue the explicitly requested Git operation.
- **Expected behavior:** A commit-message generator should control how the commit message is produced without canceling an explicit parent request to stage, commit, and push. Snow should stop after message generation only when the user asked for a message rather than repository operations, or when a permission/safety check blocks those operations.
- **Fix:** The agent now appends one generic core policy after every active skill set. A skill's methods, output scope, and stopping conditions govern only that skill's contribution; explicitly requested surrounding work continues through other capabilities. The same policy prevents skill activation from authorizing unrequested side effects and keeps collaboration-mode, safety, and permission controls authoritative. No skill name or Git workflow is special-cased.
- **Verification:** A model-triggered generic limited-output skill regression covers both a multi-step enclosing request and an artifact-only request, verifies the policy follows activated content on the provider continuation, and verifies the original user request remains present. `go test ./...`, `go vet ./...`, `go test -race ./internal/agent -count=1`, `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`, and `python3 scripts/check_benchmarks.py` pass.

## BUG-227: Local Web Manager URL redirects away from localhost in LAN mode

- **Status:** Resolved — localhost and LAN now serve the same manager directly on independent exact origins.
- **Severity:** Medium
- **Surface:** Ordinary `snow --mode web` on a host with a private LAN address
- **Evidence:** Snow printed `http://127.0.0.1:7331` as the Local URL, but every GET/HEAD immediately redirected to the private LAN origin. Same-machine use therefore changed address and unnecessarily depended on the LAN origin instead of serving the manager on the listener the user selected.
- **Fix:** Serve one shared shell directly from both listeners, with fixed per-listener Host/Origin boundaries, origin-specific cookie names and persisted session-profile binding. Legacy unscoped browser sessions are revoked during the one-time schema migration instead of being transferred ambiguously between origins. Runtime and agent ownership remain singular.
- **Verification:** `TestTrustedLANRunActivatesLANAndLocalhost` pairs both real origins independently, rejects crossed Host/Origin requests at the actual listeners, reads the same durable browser inventory from each origin and overlaps authenticated requests. Focused unit coverage rejects renamed local/LAN session-token replay while accepting each token only on its issuing profile. `go test -race ./internal/web ./cmd/snow -count=1` passes.

## BUG-226: Desktop conversation-header height overrides compact mobile layout

- **Status:** Resolved — compact and wrapped mobile header checks pass across the targeted mobile matrix.
- **Severity:** Low
- **Surface:** Web Manager activated conversations at widths up to 767px
- **Evidence:** The rendered mobile header contains one compact row of title/status/actions but retains a large empty lower area. `harness.css` fixes `.workspace-heading.live-header` at 76px; the mobile `.workspace-heading` and `.live-header` declarations have lower specificity and cannot replace both that height and minimum height. Browser layout/workflow checks also encode 76px at every width.
- **Remediation:** Give the exact live-header owner content-sized mobile geometry with the existing 40px mobile floor, preserving 76px on desktop and allowing genuine content wrapping to expand. Update responsive browser assertions and canonical composition documentation.
- **Verification:** The complete conversation workflow passes 1,750 assertions across seven widths and both themes, including 40px one-row mobile headers, bounded contents and unchanged 76px desktop headers. The targeted runtime-layout subset passes all 3,714 assertions across 36 reports at 320px, 360px and 390px, both themes and 740/360/240px heights, covering enabled and unsupported runtime panels. `go test ./internal/web` passes. The broader layout smoke's new header assertions pass, but its run remains nonzero because unrelated model/session fixture and reader-anchor regressions reopened BUG-176 and BUG-196.

## BUG-225: Automatic trusted-LAN mode initially disabled localhost access

- **Status:** Resolved — dual-listener integration and race checks pass.
- **Severity:** Medium
- **Surface:** Ordinary `snow --mode web` on a host with a private LAN address
- **Evidence:** The first zero-setup LAN implementation bound only the selected private address, so `http://127.0.0.1:7331` stopped working even though same-machine and same-LAN access are both required.
- **Fix:** Trusted-LAN startup binds the selected private address and `127.0.0.1` on the same port and serves one shared manager directly on both exact origins. Each listener retains independent Host/Origin/CSRF and local/LAN cookie boundaries. Offline startup still serves loopback directly.
- **Verification:** `TestTrustedLANRunActivatesLANAndLocalhost` starts both real listeners on one ephemeral port, verifies direct `200 ok` health responses, independent pairing, exact cross-listener rejection, shared durable inventory and concurrent authenticated reads, then shuts both down. Focused boundary coverage rejects foreign/cross-origin requests and renamed cross-profile session-token replay.

## BUG-224: Trusted-LAN HTTP initially reused HTTPS browser cookie names

- **Status:** Resolved — transport-specific pairing/login coverage passes.
- **Severity:** Medium
- **Surface:** Web Manager browser authentication after migration from the subsequently removed HTTPS/profile implementation to private-IP HTTP
- **Evidence:** The initial automatic trusted-LAN HTTP increment reused `snow_manager_local_session` and `snow_manager_local_pair_csrf`. A previously stored Secure cookie can conflict with a non-Secure replacement of the same name.
- **Fix:** Trusted-LAN HTTP consistently uses distinct session and pairing-CSRF cookie names across login, browser inventory/revocation, rendering, logout, and streaming. Loopback names remain separate, and the unused HTTPS/profile stack was subsequently removed.
- **Verification:** A focused integration test pairs and logs in over the trusted-LAN profile while an old Secure local cookie is present, then verifies that only non-Secure LAN-specific cookies are read, issued, and cleared.

## BUG-223: Native browser owners assume private-IP HTTP exposes `crypto.randomUUID`

- **Status:** Resolved — frontend tests, typecheck, production build, and native browser fallback pass.
- **Severity:** Medium
- **Surface:** Web Manager native steering and first-party navigation on automatic trusted-LAN HTTP
- **Evidence:** Private-IP HTTP is not a browser secure context, and `crypto.randomUUID()` may be unavailable there. Steering originally created its request ID before its guarded request path, so submitting a draft could throw without sending it. The initial first-party navigation implementation later repeated that assumption while assigning the current history entry, which could fail the entire React/navigation module during page initialization.
- **Fix:** Both owners prefer `randomUUID()` and otherwise construct an RFC 4122 version-4-shaped UUID from `crypto.getRandomValues()`, the Web Crypto primitive browsers expose to insecure contexts.
- **Verification:** The focused steering unit test removes `randomUUID`, supplies deterministic `getRandomValues`, and verifies the version/variant UUID shape. The production React browser gate now also removes `Crypto.prototype.randomUUID`, mounts the page, and completes first-party navigation through the fallback; its complete run passes 202 assertions across 28 scenarios.

## BUG-222: Web real-worker fixtures parse the pairing code by line number

- **Status:** Resolved — the focused parser regression and previously failing real-worker compaction fixture pass.
- **Severity:** Low
- **Surface:** CLI Web Manager real-worker/browser test harnesses after startup diagnostics change
- **Evidence:** After startup began printing a bounded private-IP notice, an otherwise unrelated `go test ./cmd/snow -skip '^TestWebProjectOperationsRealWorkerDeathRestartNoReplay$' -count=1` run failed real-worker fixtures with `fixture pairing did not issue a browser cookie`. Four fixture bootstrap paths assumed the pairing credential was always startup line 3, so they submitted the new address notice as the code. The isolated `TestWebCompactionRealWorkerProgressAndNextPrompt` reproduced the failure.
- **Fix:** Parse the private startup frame for the exact `Pairing code (` line instead of a positional line. Keep the origin framing and credential suppression unchanged.
- **Verification:** `TestFixturePairingCodeIgnoresAddressNotices` and the previously failing isolated real-worker compaction test pass. Broader package verification is recorded with the secure automatic-LAN increment.

## BUG-221: Worker-loss fixture obscures Git process-group cleanup

- **Status:** Resolved — repeated, race, package, process-group, and full-suite checks pass.
- **Severity:** Medium
- **Surface:** Web Manager real-worker project-operation cancellation fixture
- **Evidence:** Full-suite runs moved the same `fictional Git remained active after cancellation/worker loss` assertion between `manager_death_false` and `manager_death_true`, while isolated reruns often passed. Production starts fictional Git with `PID == PGID` and terminates/reaps that complete group, but the fixture checked `kill(pid, 0)`. After the original Git process is reaped, that probe can observe an unrelated process that reused the numeric PID. The stronger negative-PGID check then reproduced one intermittent failure; process observation showed Darwin can hold the large killed `snow.test` fixture image in kernel exit state with a stable tick, where no further signal can accelerate teardown. Focused process-tree observation confirmed the fictional Git leader owns its own process group.
- **Fix:** Keep the stable-tick requirement and check `kill(-pid, 0)` for the complete original Git process group, matching production `procgroup.Shutdown`. After all ACK, durable-row, identity, partial-work, output-canary, and start evidence checks pass, `syscall.Exec` replaces the heavyweight test image with a fixed minimal `/bin/sh` terminal loop while preserving its PID and PGID. The root is a quoted positional argument, not shell source, and the hardened helper environment remains in force. The loop's `sleep` descendant keeps complete-group cleanup coverage. No timeout, cancellation, cleanup, or no-replay boundary is weakened, and failure diagnostics now report any process still in the group.
- **Verification:** The complete worker-loss regression passes 50 consecutive normal repetitions and 10 consecutive race-enabled repetitions; `TestShutdownStopsCompleteProcessGroup` passes 50 repetitions; uncached `go test ./cmd/snow -count=1`, `go test ./...`, and `go vet ./...` pass. The broader internal/SDK race gate also passes.

## BUG-220: Long goal prompts stop extending the ChatGPT cache prefix

- **Status:** Resolved — exact-prefix, retry, resume/fork, compaction, provider-mapping, and public-history regressions pass.
- **Severity:** Medium
- **Surface:** ChatGPT/Codex prompt caching during Thread Goals and other recurring private steering
- **Evidence:** The TUI correctly renders `cached_tokens / input_tokens`, but consecutive long-goal requests were shaped as `history, ephemeral steering` and then `history, new assistant/tool work, next steering`. OpenAI prompt caching requires an exact prefix, so reuse stopped at the history that preceded the first private suffix and the cached percentage declined as goal work accumulated. A focused Responses request reproduction found only the original history item shared where the next request should have extended the prior input.
- **Fix:** Store every sent private steering fragment as a provider-only `internal_context` entry immediately before its owning assistant response. Provider context restores those entries as hidden `RoleInternal` inputs, so each successful request becomes an exact extension of the previous input/output sequence. Unchanged recurring fragments are sent once per high-level turn rather than once per provider/tool step, and are reasserted after compaction. Ordinary history and public RPC/web/TUI/SDK projections omit the entries; no-activity retries do not persist duplicates, internal text is excluded from compaction summaries and fork artifact trust scans, and compaction retains internal steering with the following assistant/tool cycle.
- **Verification:** Focused session/provider/agent/compaction/RPC tests pass; `go test ./...`, `go vet ./...`, the affected race suite, all 70 Python maintenance tests, and `python3 scripts/check_benchmarks.py` pass. One full-suite run hit an unrelated `worker_unavailable` timing failure in `TestWebProjectOperationsRealCreateActualRoot`; the isolated uncached test and two subsequent full-suite runs pass.

## BUG-219: Native runtime-control fixture reads HTTP bodies before they finish

- **Status:** Resolved — the final four-cell native runtime-control matrix passes all 404 assertions.
- **Evidence:** The extended no-op matrix reaches 90 passing assertions in 1280px/dark, then CDP rejects a steering receipt body read with `No data found for resource with given identifier`. `Network.responseReceived` announces headers, not a completed response body; the other three cells finish normally (393 assertions overall, one fixture failure).
- **Fix:** Retain a bounded set of `Network.loadingFinished` IDs and wait for the matching event before inspecting a receipt body. This only waits for browser evidence; it never repeats an application request or changes any production admission path.

## BUG-218: Activity privacy fixture intermittently matches a numeric sentinel

- **Status:** Open — exact response collision not captured; unrelated to the composer implementation.
- **Evidence:** One full `go test ./internal/web` run fails all four statuses in `TestManagerActivityGoalRunHTTPCountsExcludeGoalContentAndAuthority` on the literal `8765`; the immediate uncached full package rerun passes in 27.030s. The test searches the entire serialized Activity response for this four-digit sentinel, including public IDs and timestamps. Those fields can contain the same digits without exposing goal usage, but the failing response was not retained, so its exact source remains unverified.
- **Follow-up:** Reproduce with deterministic public IDs/timestamps and inspect the typed public projection. Preserve field/content exclusion checks; do not silence a possible disclosure or merely retry away the failure. No Go source or test was changed for this UI task.

## BUG-217: Wrapped composer controls scroll the header out of reach on short screens

- **Status:** Resolved — the final full runtime-layout matrix passes all 84 reports.
- **Evidence:** At 360×240, focusing the new two-row composer moves the conversation trigger from y=54 to y=4, behind the mobile topbar. Four header hit tests fail; header-origin Thinking also fails its bounded-picker check. The form is constrained but has no overflow owner; native focus scrolls the containing live region. Picker placement additionally trusts an offscreen composer anchor.
- **Fix:** Give the existing composer form bounded vertical overflow and contained overscroll, clip the ordinary live region against native ancestor focus scrolling, and allow the already-scrollable manager-controls region to shrink before displacing the composer. Form overflow alone was insufficient; clipping alone kept the header but left controls out of reach. Clamp Thinking's vertical anchor and available height to viewport gutters. Keep the same form, editor, header and native focus owner; no controls or geometry assertions are removed.
- **Verification:** Initial six-cell 360px/dark matrix has one failing cell with five assertions; intermediate partial fixes retain failures. Final six-cell diagnostic and full 84-report runtime-layout matrix exit 0. Native goal (332), runtime-control (396), and ordinary-workflow (432) assertions also pass on the final assets.

## BUG-216: Goal admission accepts a snapshot in place of its native receipt

- **Status:** Resolved — focused contracts and the full native goal matrix pass.
- **Evidence:** The existing ACK predicate falls back to `result.goal`, accepts an unchanged revision, and does not correlate a new objective/budget. The extended existing contract test reproduces missing-receipt acceptance (5 pass, 1 fail).
- **Fix:** Require the explicit native receipt, an advanced revision, a new exact goal identity and the submitted objective/budget. Objective comparison matches native Unicode whitespace normalization. Failed correlation retains the existing uncertain-outcome fence and draft; no replay is added.
- **Verification:** `node --test scripts/tests/goal_frontend_contract.test.mjs` passes 6/6 after the fix, including fast completion's separate admission receipt. Native 320px/dark goal execution subsequently passes 82 assertions; final full execution passes 332 across four reports.

## BUG-215: Goal composer preflight rejects its own inspection revision

- **Status:** Resolved — full native goal matrix passes.
- **Evidence:** After the toggle/menu checks pass, native Start never reaches the provider: the browser reports changed scope. `goal_inspect` publishes a new manager revision even when goal facts are unchanged, so comparing its result to the pre-read revision rejects every submission.
- **Fix:** Permit exactly the inspection's single revision advance while retaining every captured goal/authority fact and the unchanged-draft guard. Admission uses the exact inspected revision; any additional revision or fact change still rejects the submission. No backend compare-and-swap rule is relaxed.
- **Verification:** Native 320px/dark execution passes 82 assertions after the fix, including changed-draft rejection, one Start, serial turns, whole-run Stop, exact Resume and fast native ACK. Final full native execution passes 332 assertions across four reports.

## BUG-214: Goal composer mode hides its retained details menu action

- **Status:** Resolved — full native goal matrix passes.
- **Evidence:** After the new local Goal toggle passes, all four native manager-execution cells stop at 43 passing assertions because the `goals` conversation-menu entry is absent. Menu discovery filters hidden launcher proxies, including the intentionally hidden Goal-details proxy.
- **Fix:** The goal owner advertises whether its details proxy is supported. Menu discovery admits that specific supported secondary proxy without exposing a duplicate toolbar launcher or admitting unsupported controls. Busy/invalid state still disables the proxy.
- **Verification:** Reproduction exits 1 with 172 passing assertions and four missing-target failures. Invocation-time availability was corrected as well as discovery; final native execution passes 332 assertions across four reports.

## BUG-213: Goal composer browser fixture tries to serialize a React-owned DOM node

- **Status:** Resolved — full native goal matrix passes.
- **Evidence:** The new Goal editor-identity probe assigns the textarea to a browser global but returns that DOM node through CDP `returnByValue`. All four manager-execution cells stop at 40 passing assertions with `Object reference chain is too long`; no application JS error or goal execution occurs.
- **Fix:** Keep the identity reference in the browser and return only a scalar from the setup probe. Identity comparisons remain in-browser; no assertions or runtime guards are removed.
- **Verification:** Initial matrix exits 1: 160 passing assertions, four fixture failures. The later inline stopped-status probe was also moved before its own explicit Details-open action. Final native execution passes 332 assertions across four reports, with all identity, stopped-status and focus assertions retained.

## BUG-212: Composer launcher fixture counts earlier inspection as compaction mutation

- **Additional fixture correction:** New feedback-dismiss checks similarly counted unrelated background inventory/snapshot completions as dismissal requests. Keep exact hidden/focus assertions, and permit only the existing background-read allowlist across dismissal; panel/action requests still fail. The final runtime-layout matrix passes all 84 reports.

- **Status:** Resolved — all 84 corrected runtime-layout reports pass.
- **Evidence:** The new direct goal/reasoning launcher checks issue read-only inspection POSTs before the existing busy-compaction probe. That probe incorrectly checks all traffic since startup and fails `Busy compaction cannot submit or change the draft` in all six supported 320px cells; the other assertions and six unsupported cells pass.
- **Fix:** Capture the request boundary immediately before the busy-compaction probe and still reject every POST within that probe. Do not exempt mutation endpoints or weaken draft/disabled-control checks.
- **Verification:** Initial narrow matrix exits 1 (six of twelve cells fail). Corrected `harness-layout/run.mjs --runtime-only` exits 0 with all 84 reports passing, including direct composer launch, icon hit testing, native modal ownership, focus return and unchanged menu delegation at seven widths, three heights and both themes.

## BUG-211: Direct compaction makes the composer jump between status layouts

- **Status:** Resolved — whole-chat no-op checks pass, not only composer geometry.
- **Whole-chat reproduction:** Extending the native 320px/dark no-op probe beyond composer bounds exposes a 38.09375px transcript top/height delta (99 assertions pass, one fails). Nodes remain mounted, with no recovery banner or sampled Working indicator. Changing compaction feedback text in the in-flow manager-controls region changes its wrapped height. The earlier composer-only assertion misses this.
- **Current fix:** Render bounded, dismissible compaction feedback out of flow using the existing scroll owner's composer-clearance measurement; keep admission/Stop unchanged. Compaction does not show the ordinary chat Working indicator.
- **Whole-chat verification:** Final native runtime-control matrix passes 404 assertions across four reports. The native no-op sampler records zero transcript top/height, content-height and scroll-offset deltas, without retiring message/editor nodes. Runtime-only layout passes all 84 reports, including bounded feedback and dismissal at 240px height. Frontend tests (132), typecheck/build/reproducibility, syntax and `go test ./internal/web` pass.
- **Additional evidence:** The first composer-mode runtime matrix has 394 passing assertions and two desktop geometry failures: dock 98→138px, row delta 40px, footer unchanged at zero, stable editor identity, no Queue next. Keeping the last reported Thinking level visible alone does not fix the wrap: the wider labelled toolbar also crosses its threshold when Send expands into the Stop text pill. Send, pending admission and Stop now share a 34px icon footprint with exact ARIA/tooltips. Both subsequent and final four-cell native matrices pass all 396 assertions; the final ordinary-workflow regression also passes 432 assertions.
- **Evidence:** The native 320px/dark runtime fixture samples animation frames from compact admission through Stop, completion and no-op. Its first trace shows 58.39px dock-height variation with stable action-row height and editor/button identities. Normalizing intentional ancestor scrolling and settling prior typing removes the misleading 210px raw top movement. Suppressing the redundant generic status alone still reproduces a 36px height/top jump: dock 154→190px, form 154px and editor 52px unchanged, footer 0→36px. `Queue next` was being presented for compaction's native `running` status, even though it is not an ordinary follow-up turn.
- **Fix:** Project reservation/admission/native compaction as the queue's existing nonqueueable `compacting` state. Keep routine composer status screen-reader-only during compaction, which already owns inline status; show stopping there as well. Preserve visible connection/unknown-outcome warnings, existing native Stop and admission guards, and all retained queue-item controls. No remount, permanent spacer or backend change.
- **Verification:** Initial and partial-fix narrow runs exit 1 with 87 passing assertions and the new geometry assertion failing. The bounded animation-frame regression now settles typing, normalizes deliberate scrolling, checks editor/button identity and dock/row geometry, and rejects any visible Queue next. The final `manager-runtime-controls/run.mjs` exits 0: 352 assertions across 320/1280 × dark/light, with zero height/top/row variation and stable identities in every report. `manager-workflows/run.mjs` passes 432 assertions, retaining ordinary queue delivery/review. `harness-layout/run.mjs --runtime-only` passes all 84 reports. All 132 frontend tests, TypeScript, reproducible assets, JS syntax, diff checks and `go test ./internal/web` pass.

## BUG-210: Direct-action browser fixtures retain retired interaction assumptions

- **Status:** Resolved — corrected runtime-controls, manager-execution and layout matrices pass.
- **Evidence:** During the direct-compaction acceptance update, removing the former dialog-opening sequence also removed its `compact.focus()` reveal. The modified runtime-panel fixture repeatedly reports `Composer compaction icon is reachable at the current viewport` failures at 240px heights, while normal heights pass. The short-height composer intentionally has a bounded scroll owner; the assertion tested the offscreen row without scrolling or keyboard focus. The native runtime suite also reports four failures (one per report) from its obsolete requirement for a compaction dialog; its 340 remaining assertions pass.
- **Additional evidence:** The subsequent composer goal/reasoning relocation rerun exposes the same obsolete four-dialog assertion in `manager-execution/tests.mjs`: 320 assertions pass and four fail. Goal admission, serial turns, Stop/Resume, focus/draft preservation and process controls pass; the retired compaction dialog alone is missing as intended. That shared assertion now requires the three real dialogs plus direct compaction icon/status owner. The corrected full `manager-execution/run.mjs` exits 0 with 324 assertions; all four reports pass, including the new composer-origin Goal opening and focus return.
- **Fix:** Restore native focus before hit testing the direct-action icon; keep the real viewport/occlusion check. Require the three remaining React dialogs, composer compaction button and inline status instead of the removed compaction dialog. No product layout change or bypass of action/geometry checks.
- **Verification:** The initial layout matrix was stopped after repeated failures. The corrected `harness-layout/run.mjs --runtime-only` exits 0 with all 84 reports passing. The corrected `manager-runtime-controls/run.mjs` first passes 344 assertions and then, with explicit conversation-menu compaction coverage, exits 0 with 348 assertions across four real-manager reports; exact scope, direct Apply/history actions, single compaction POST/provider call under repeated clicks, Stop, progress, native no-op and no retries are covered. React pages also pass 217 assertions across 30 scenarios; affected Go tests/vet and all 132 frontend package tests pass.

## BUG-209: Composer compaction relocation loses menu-origin focus return

- **Status:** Resolved — the complete runtime-panel matrix passes.
- **Evidence:** After moving the compaction launcher into the composer, the existing runtime-panel matrix repeatedly times out at `compaction close/focus return`. Direct composer opening returns correctly, but opening through the conversation menu returns to the new composer icon rather than the menu launcher. The shared dialog helper's old manager-control ancestry heuristic no longer applies.
- **Fix:** Preserve the already-focused conversation-menu launcher when that menu forwards its action; direct composer activation continues returning to its own icon. Keep the existing single dialog/controller, capability gating and explicit consent.
- **Verification:** The initial matrix was stopped after repeated reproductions. After the fix, `node scripts/tests/browser/harness-layout/run.mjs --runtime-only` exits 0: all 84 reports pass across seven widths, dark/light, three heights and supported/unsupported fixtures. The extended existing fixture checks composer placement, accessible icon, no mutation on open/cancel, direct focus return, and retains the prior menu-origin focus assertions. All 132 frontend package tests, generated-asset reproducibility and `go test ./internal/web` also pass.

## BUG-208: Workspace-action acceptance uses retired owners and premature mobile focus

- **Status:** Resolved — all three production-bundle entry points pass.
- **Evidence:** `workspace-actions/run.mjs` executes retired `shell.js` in a hand-built DOM; `grouped.mjs` executes retired `sidebar-sessions.js` and clones owned descendants. The exported-page browser gate initially reports 70 assertions with four failed scenarios: it focuses a row before the React drawer commit and app-owned initial-focus handoff.
- **Fix:** Exercise the production generated module and menu host in native Chromium, retaining owner-intent counts, strict hit testing, modifier semantics, scope/race checks and bounded read-only inventory. Build empty external hosts for lifecycle remounts rather than cloning React descendants. Wait for mobile non-inert drawer readiness and the initial-focus handoff before aiming at its row. Current React cache/revalidation behavior is documented rather than asserting retired-script behavior.
- **Verification:** Final parent runs pass `node scripts/tests/browser/workspace-actions/run.mjs` (25 assertions), `grouped.mjs` (26) and `SNOW_WORKSPACE_EVIDENCE=0 node scripts/tests/browser/workspace-actions/browser.mjs` (108 across four width/height reports, zero failed scenarios). Component request/diagnostic recording is bounded. Child verification also passed two exported matrices. These are mocked public transport checks, not real-worker/provider or committed mutation acceptance.

## BUG-207: Manager workflow intermittently fails Archive pointer readiness

- **Status:** Resolved — two consecutive full matrices pass after native document-readiness repair.
- **Evidence:** Repeated full `manager-workflows` matrices stopped after rename/pin/unpin: the Archive confirmation hit test returned `hit:false` and `hitTag:INPUT`. An explicit disclosure-open wait exposed a missed summary click and navigation to the project heading's Organization link instead, in 1280/light. A focused cell and one full rerun passed, but the failure recurred. Organization submits native POST/redirect documents; selector presence alone can precede document load and scroll restoration, invalidating the next pointer coordinates.
- **Fix:** Wait for the prior document's marker to disappear, the new document to reach `complete`, React readiness, and a painted-frame boundary after each native organization submission. Also require the Archive disclosure to be open before confirmation. Preserve one click per action, strict native hit testing, exactly five organization mutations and no provider work. Failure diagnostics now identify the hit element and document/disclosure readiness without logging input values or credentials.
- **Verification:** The earlier disclosure-only wait did not fix the failure and is superseded. After the native-document readiness fix, two consecutive complete `node scripts/tests/browser/manager-workflows/run.mjs` runs pass 432 assertions each across 320/1280 × dark/light, with zero failures. Original native hit tests, mutation/provider counts, durable ancestry, trust, queue and history checks remain unchanged.

## BUG-206: Standalone thumbnail regression still exercises the retired renderer

- **Status:** Resolved — the existing native regression now passes against the generated React module.
- **Evidence:** `internal/web/message_image_browser.test.mjs` previously read and executed `static/messages.js`, which the production manager no longer loads or routes. Its direct-image-URL and retained SSR-node assertions did not exercise the current authenticated fetch/Blob transport or React root lifetime.
- **Fix:** Serve the production module, require the fixture cookie and exact raster Accept header on image reads, assert transport identity separately from Blob presentation, and retire/remount through the current React facade without mutating its owned descendants. Preserve scoped rejection, eight-image/global-queue bounds, timeout, failure and no-retry checks; add Blob revocation and uncaught-error/React-diagnostic checks.
- **Verification:** `node --test internal/web/message_image_browser.test.mjs` passes both tests, including 130 native React assertions plus desktop/mobile sizing and server-side request/cancellation checks. All 132 frontend package tests and generated-asset/notices reproducibility also pass. The initial port covered standalone saved-history enhancement. Its follow-up now also passes 52 ColdWorkspace parent-owned assertions at 320/1280: real production parent composition, retained public history/disclosures, standalone-facade isolation, real HTMX canceled/committed requests, abort/Blob revocation on parent retirement, same-host restore and exact scope rejection. Pagehide/pageshow events are synthetic calls to the production lifecycle listeners, not proof of real bfcache eligibility.

## BUG-205: Composer context range replacement overwrites the entire draft

- **Status:** Resolved — the complete native composer-context matrix passes.
- **Evidence:** The React context controller supplies a replacement substring plus start/end offsets, but `app.js` passes the substring directly to `updateDraft` and selects the old range. Selecting a file drops surrounding prose; selecting a folder leaves a range selection so token discovery cannot reopen.
- **Remediation:** Assemble the replacement inside the existing draft, collapse the caret after the inserted substring, and keep the existing owner/admission guard and single draft revision update.
- **Verification:** `node scripts/tests/browser/composer-context/run.mjs` passes all 1,044 assertions across 16 schedules, including exact surrounding-text preservation, nested directory discovery, caret/IME behavior, explicit selection and no automatic Send. Both legacy-text and edit/queue capability schedules pass at 320/1280 in dark/light.

## BUG-204: Mention discovery can erase freshly typed React composer text

- **Status:** Resolved — native input and complete composer-context schedules pass on the rebuilt bundle.
- **Evidence:** Inserting `@no` into `Read  afterwards` starts a file read but immediately restores `Read  afterwards`; its completed listing remains stuck at “Loading files…”. The target-level context listener synchronously publishes suggestion ARIA before the React bubble input handler captures the new text, so the controlled textarea re-renders the previous value and invalidates the query.
- **Remediation:** Capture the input value synchronously in React's capture phase, before the existing target-level discovery listener runs. Preserve the single discovery/admission owner, caret, IME and no-automatic-selection contracts.
- **Verification:** The existing file-query regression now uses native `Input.insertText` and reproduces the lost token before the fix. After rebuilding, all 1,044 composer-context assertions across 16 schedules pass, including native input, caret, IME, file/skill discovery and retained drafts; all 132 package tests and reproducible assets/notices also pass.

## BUG-202: Background sidebar catalogs can starve explicit cold-workspace navigation

- **Status:** Resolved — focused admission/race checks and the complete real-worker live-stream scenario pass.
- **Evidence:** Both expanded sidebar groups occupy the bounded catalog worker pool while the selected workspace requests saved history. The foreground page renders “Sessions are unavailable or the catalog is busy” with no saved messages. Current navigation does not preempt inventories; server preemption covers activation/deletion but not authenticated foreground catalog reads.
- **Remediation:** Retire browser inventory reads before explicit navigation and use the existing same-project canceled-I/O admission boundary before foreground catalog reads. Preserve the worker-pool bound, cross-project independence and no automatic retry/activation.
- **Verification:** The new `TestColdWorkspaceHistoryPreemptsItsSidebarRead` fails before the fix and passes afterward. Affected sidebar/recovery tests pass with `-race`, all 21 workspace-flow checks pass, `go test ./internal/web ./cmd/snow` and affected vet pass, and `live-stream/run.mjs` completes all 87 assertions with no automatic retries or activation.

## BUG-203: Composer-context browser fixture does not model React image and file transports

- **Status:** Resolved — authenticated thumbnail and full composer-context schedules pass.
- **Evidence:** The shared recorder replaces all `fetch` calls, including the new bounded authenticated image fetch (previously a direct image-element read), so the real loopback image fixture receives no request. File-choice checks also stall at “Loading files…” after the transport port. The 16 reported cells stop before the full functional assertions; no production validation should be weakened to make them pass.
- **Fix:** Delegate only the exact fixture image GET to native fetch and validate the resulting owner-created Blob preview, cookie-protected PNG decode and URL revocation. Caller-supplied Blob metadata remains rejected. The file-choice stall proved to be separate production input/range bugs (BUG-204 and BUG-205), not a reason to weaken the fixture's transport assertions.
- **Verification:** The complete `composer-context/run.mjs` matrix passes all 1,044 assertions across 16 schedules. All four thumbnail cells execute 32 assertions each with authenticated real HTTP image requests, no extra mutation, no leaked object URLs and unchanged caller-metadata rejection.

## BUG-201: Message replacement fixtures count Shell inventory reads as mutations

- **Status:** Resolved — corrected Edit & resend and Regenerate native matrices pass.
- **Evidence:** `message-edit/fixture.mjs` passes the Shell's `/access/browsers` and exact project sidebar inventory GETs into its CSRF/instance-bound POST checks. This records read-only startup traffic as forbidden mutations even while all edit replacement, draft and no-replay behavior checks pass. The regenerate fixture has the same missing read handlers.
- **Remediation:** Model only the exact production inventory GETs separately from mutation accounting, as the existing Stop/Reuse fixture does. Keep unknown reads, malformed requests, POST field validation and mutation-count assertions strict.
- **Verification:** `message-edit/run.mjs` passes 1,208 assertions and `message-regenerate/run.mjs` passes 1,748 assertions, each across all four 320/1280 × dark/light cells with zero failures. POST/identity/field and no-replay assertions remain unchanged.

## BUG-200: Live-stream browser fixture bypasses the React rename input owner

- **Status:** Resolved — native rename and the complete real-worker live-stream scenario pass.
- **Evidence:** `scripts/tests/browser/live-stream/tests.mjs` assigns `#workflow-name.value` and submits without a native input event. The controlled React form retains its previous rename draft, so this does not exercise a user's rename. The real manager remains Ready/Live with no action error.
- **Remediation:** Enter the name through native browser input before submitting; preserve the exact durable-name assertion and existing execution/no-replay checks.
- **Verification:** `node scripts/tests/browser/live-stream/run.mjs` passes all 87 assertions. The fixture also avoids serializing a retained React DOM node through CDP and waits for the post-prompt SSE snapshot before suspending its isolated manager for the watchdog check; durable names, reconnect and no-replay assertions remain enforced.

## BUG-199: Frontend test command omits Shell menu accessibility regressions

- **Status:** Resolved — all 132 package tests pass, including the three existing menu ARIA regressions.
- **Evidence:** `src/shell/menu-aria.test.ts` covers exact popup identity, revoked launcher authority and stable ARIA host references, but the explicit test list in `internal/web/frontend/package.json` omitted it. The previous 129-test command therefore never executed these migration regressions.
- **Fix:** Include the existing test file in the package test command without adding a duplicate harness.
- **Verification:** `(cd internal/web/frontend && npm test)` executes 132 tests with zero failures.

## BUG-198: React chrome wrapper bypasses short-height warning layout

- **Status:** Resolved — the complete seven-width × dark/light layout matrix passes all 2,058 reports.
- **Evidence:** At 320×240, the disconnected composer ended at y≈291 outside the 240px viewport. The existing warning/uncertainty rules required direct children of `#live-session`; React's display-contents chrome root breaks that selector relationship despite preserving visual flow.
- **Fix:** Match the existing unique warning IDs across the rendering boundary, restoring the bounded warning scroller and sticky composer without hiding warning text or weakening actions.
- **Verification:** `node scripts/tests/browser/harness-layout/run.mjs` passes all 2,058 viewport/state reports, including disconnected-state controls at all three heights, seven widths and both themes. This verifies layout, not whole-product migration parity.

## BUG-197: Question stress fixture exceeds the production input projection bound

- **Status:** Resolved — the corrected fixture passes all 97 question assertions across 320×740/360/240.
- **Evidence:** Sixteen questions with 32 options on each choices page and five repeated description sentences exceeded `projectInput`'s 64 KiB aggregate limit. The React validator correctly refused that impossible public projection; the old fixture bypassed Go's projection function.
- **Fix:** Preserve all 16 questions and 32 choices per choices page, while retaining one wrapping description sentence per option within the aggregate bound. The existing Go exporter now rejects initial input fixtures that fail `projectInput`.
- **Verification:** Native exporter and question checks ran via the current harness layout smoke. No production validation bound was loosened.

## BUG-196: Intermediate React layout clamps steal transcript reader ownership

- **Status:** Open — two reader-anchor assertions regressed in fresh BUG-226 layout verification; unrelated to the header geometry correction.
- **Regression evidence:** A serial 390px/dark layout smoke passes all `stream` cells at 360px and 240px heights but fails `Question arrival and seat resize preserve manual reader anchor` and `Collapsing attention preserves manual reader ownership and visible anchor` at 740px. The same run's base `chat` compact-header assertions pass.
- **Evidence:** Initial mounting briefly clamps the transcript before all composer roots finish, so a 4px geometry difference is mistaken for manual upward intent. Head eviction similarly clamps through intermediate commit layouts, recapturing the wrong anchor and moving surviving content by about 426px.
- **Fix:** Initialize the reader after synchronous presentation setup, keep attention layout notifications separate from the parent update transaction, and retain the pre-update reader anchor instead of sampling intermediate commit clamps as user input.
- **Verification:** Frontend build, all 132 package tests and reproducibility pass. The complete 2,058-report layout gate includes reader checks preserving initial following, incremental pinning, manual anchors, head eviction, attention resizing, copy focus and table scroll. The separate disconnected-state failure is also resolved (BUG-198).

## BUG-195: Shell icons lose production sizing hooks during the React port

- **Status:** Resolved — native Settings focus and short-height checks pass in the complete layout matrix.
- **Evidence:** Shell `Icon` emits no CSS class, so the existing `.icon` and `.appearance-icon` sizing rules do not match. The native Tab target is a 140px-tall theme button extending to y=261 in a 240px viewport, despite its modal remaining within y=24…216. Appearance glyphs previously used the explicit 16px class.
- **Remediation:** Preserve the default Shell icon hook and the appearance-specific hook in JSX; keep the existing production stylesheet dimensions rather than relaxing the viewport/focus assertion.
- **Verification:** `node scripts/tests/browser/harness-layout/run.mjs` passes all 2,058 reports across seven widths, dark/light and normal/short heights; the previously failing native popup, inspector and Settings checks now pass.

## BUG-194: Current-workspace menu navigates instead of opening its local inspector

- **Status:** Resolved — local inspector actions pass across every width/theme/height in the complete layout matrix.
- **Evidence:** Workspace settings / Remove registration from the current project's React menu starts document navigation rather than dispatching the existing presentation-only inspector action. The local Project tab and explicit removal confirmation do not open in the fixture; unexpected navigation also disrupts the subsequent composer checks. The retired menu used the guarded `snow:inspect-project` event when the current inspector matched the project.
- **Remediation:** Preserve the existing local inspector event for an exact current-project match; retain ordinary navigation for other projects and stale-owner checks for all menu callbacks. Never submit removal automatically.
- **Verification:** `node scripts/tests/browser/harness-layout/run.mjs` passes all 2,058 reports across seven widths, dark/light and normal/short heights; the previously failing native popup, inspector and Settings checks now pass.

## BUG-193: React workspace picker omits its production typography class

- **Status:** Resolved — the production workspace picker passes the complete seven-width × dark/light layout matrix.
- **Evidence:** The production workspace picker opens through `ShellPopup`, but its portal lacks `shell-workspace-menu`. Existing `settings.css` rules for block name/path rows, truncation, muted path typography and heading spacing therefore do not match. The native home assertion cannot find the production-styled popup.
- **Remediation:** Retain the workspace-specific class on the existing external portal host without restoring a legacy renderer or changing JSX child ownership.
- **Verification:** `node scripts/tests/browser/harness-layout/run.mjs` passes all 2,058 reports across seven widths, dark/light and normal/short heights; the previously failing native popup, inspector and Settings checks now pass.

## BUG-192: Cold workspace trust-management launcher has no React action owner

- **Status:** Resolved — current production bundle passes the complete real-manager workflow matrix.
- **Evidence:** After explicitly resuming and closing the exact saved conversation, opening Startup settings exposes Settings → Workspaces, but its native click does not open the dialog. The Cold React button retains only the old `data-settings-open` marker; the retired Shell renderer's document delegate no longer handles it.
- **Fix:** Route the Cold button through the existing typed Shell settings controller, preserving the clicked element as the return-focus owner. No competing legacy renderer or automatic trust mutation was added.
- **Verification:** Frontend build, all 129 tests, and reproducible assets/notices pass. `node scripts/tests/browser/manager-workflows/run.mjs` passes 432 assertions across all four 320/1280 × dark/light cells, including exact-session Resume, native disclosure/settings opening, explicit trust revocation and unchanged provider/mutation counts.

## BUG-191: Cold workspace rejects Go's canonical history/recovery URLs

- **Status:** Resolved — model regressions and complete real-manager workflow matrix verified.
- **Evidence:** Explicit Close succeeds, but the Cold page fails validation instead of offering activation. Its validator requires `/?view=projects&…`; Go's `url.Values.Encode` emits pagination and recovery queries in sorted-key order, such as `/?offset=30&project=…&session=…&view=projects`. The same valid manager destination is rejected solely because of query ordering.
- **Fix:** Validate the exact local route, allowed unique parameters, bounded offset and project identity independent of query order; retain external/ambiguous-navigation rejection.
- **Verification:** All 129 frontend tests and reproducible build pass, including sorted pagination/recovery URL regressions. `node scripts/tests/browser/manager-workflows/run.mjs` passes 432 assertions across all four 320/1280 × dark/light cells. Its saved-session selection now uses the authoritative sidebar and waits for the requested workspace replacement before Resume rather than accepting a matching input in the previous workspace; Startup settings is opened before clicking its disclosure-contained trust launcher.

## BUG-190: Background menu positioning scrolls a pending pointer target away

- **Status:** Resolved — full native runtime-controls and menu/layout matrices pass.
- **Evidence:** After native hit testing locates Thinking & response, a background Conversation repaint repositions the open menu and reveals its older focused row. The menu scroll changes before pointer dispatch, so Managed processes opens instead. No script error occurs; 320/light and 1280/dark time out awaiting preference inspection while `processes-dialog` is open.
- **Remediation:** Separate geometry updates from keyboard-focus revelation. Background repaints must preserve the user's menu scroll; explicit focus/navigation and viewport resize may reveal the focused item.
- **Verification:** `manager-runtime-controls/run.mjs` passes all 244 assertions across 320/1280 × dark/light. The complete 2,058-report layout matrix also passes the model, conversation and Settings menu/focus checks without relaxing pointer or viewport assertions.

## BUG-189: Cold workspace rejects normal saved-session identifiers

- **Status:** Resolved — public identifier regression and full native runtime-controls matrix verified.
- **Evidence:** The React Cold validator required a UUID for `sessionID`, but Snow's session store generates timestamp-plus-random-suffix IDs. A saved conversation therefore rendered the safe-error fallback instead of its native Resume form. The real manager completed pairing and loaded the production module without script errors; passive browsing could not reach Resume.
- **Fix:** Match the existing `runtimeIdentifier` contract: 1–128 ASCII letters, digits, hyphens or underscores, with empty allowed for New. Project UUID validation remains separate. Extend the existing model test with generated, fixture, underscore, UUID and invalid-path identifiers.
- **Verification:** All 129 frontend tests and reproducible build check pass. `node scripts/tests/browser/manager-runtime-controls/run.mjs` passes 244 assertions across all four 320/1280 × dark/light cells, including passive saved-history browsing, explicit native Resume, exact session identity and no provider call before admission.

## BUG-188: Script-enabled fallback disrupts the React workspace grid

- **Status:** Resolved — full native Stop/Reuse matrix verified.
- **Evidence:** The Shell fallback `<noscript>` used `.react-live-panel`, whose `display: contents` rule exposed its raw text with scripting enabled. It created an extra grid item and intercepted hit testing. At 1280×740/light the live session shrank into a 280×20px bottom region while the composer overflowed the viewport; all 26 stylesheets loaded and React readiness succeeded.
- **Fix:** Remove React mount styling from the passive no-JavaScript fallback, retaining its navigation markup.
- **Verification:** `node scripts/tests/browser/stop-reuse/run.mjs` passes 840 assertions, zero failures across all four 320/1280 × dark/light cells, including native composer/control geometry, hit testing, preserved drafts and exact mutation counts. Its fixture now handles the full Shell's exact read-only sidebar/browser inventory GETs separately from mutation accounting; mutation guards remain unchanged.

## BUG-187: Frontend workbench rejects its fictional bootstrap

- **Status:** Resolved — native development workbench reload verified.
- **Evidence:** Organization mounted only its safe-error fallback because the fictional CSRF placeholder was not the required 64-character hexadecimal projection. Styles loaded and no backend requests occurred, but the component workbench was unusable.
- **Fix:** Supply a clearly fictional all-zero hexadecimal placeholder without weakening production validation or connecting a backend.
- **Verification:** Native Chrome at the isolated Vite workbench renders Organize workspaces, Active registrations, and Archived registrations with `data-react-mounted=true`, all 26 stylesheets loaded, no alert, and no fetch/XHR requests.

## BUG-186: React Goal inspection drops modal keyboard focus

- **Status:** Resolved — native focus regression and complete execution matrix verified.
- **Evidence:** The native manager-execution suite opens Goal from the Session menu and waits for a confirmed-absent inspection. The dialog remains modal, but focus moves to `BODY` in all four 320/1280 × dark/light matrix cells. The failing run reports 384 passing assertions and four identical focus failures; a desktop/light repeat reproduces it.
- **Fix:** Focus the stable, enabled Close control through its React-owned ref immediately after opening, before inspection disables Refresh. Explicit reads, native modal cancellation, parent-owned return focus, and exactly-once goal admission remain unchanged.
- **Verification:** The rebuilt production bundle passes `node scripts/tests/browser/manager-execution/run.mjs`: 388 assertions, zero failures across 320/1280 × dark/light. All four cells retain modal focus and restore Close focus to the surviving session menu; existing Start/Resume/Stop and request-count assertions remain intact. Log: `/tmp/react-manager-execution-focus-fixed-full.log`.

## BUG-185: Revoked deletion capability leaves an empty session menu launcher

- **Status:** Resolved — focused native capability and focus regressions verified.
- **Evidence:** After a saved row gained its ⋯ launcher, an inventory refresh with `delete_supported:false` left the non-current launcher visible, enabled and focused despite having no remaining actions. Opening it did nothing; no deletion was sent.
- **Fix:** Reconcile capability loss immediately, hide/disable empty launchers, close only their own open popup, and restore focus to the surviving session link. Preserve current-session Rename where supported.
- **Verification:** The native capability-revocation scenario reproduced the stale launcher. Final menu/deletion suite passes 257 assertions across 58 scenarios, including a repeated run: empty popup retirement, link focus restoration, detached callback rejection, current Rename preservation, cross-workspace popup isolation and revoked-confirmation focus. The integrated frontend suite passes 157 tests.

## BUG-184: Session switching flashes unrelated history and a disconnected composer

- **Status:** Resolved — real-manager painted-frame and delayed-inventory node/focus regressions verified.
- **Evidence:** The real-manager browser probe recorded nine painted frames of the destination workspace's previous session and draft before switching to the clicked session. Same-workspace switch acknowledgement also applied a complete snapshot and immediately changed the UI to disconnected/Synchronizing before reconnecting its subscription. Sidebar reconciliation additionally rewrites the current row ID even when the requested target row already exists.
- **Remediation:** Show a bounded opening state during cross-workspace validation instead of unrelated history; reveal real state for confirmations and failures. Retain the connected presentation only for a complete, identity-checked idle switch acknowledgement while connecting its new subscription. Keep admission controls disabled, reveal real subscription failures, preserve drafts, and retain immutable sidebar row identity/focus.
- **Verification:** The existing 87-assertion real-manager journey passes. The final focused probe records 47 frames with zero intermediate-owner frames, layout flashes or empty-history frames; 15 focused unit regressions pass. Before the sidebar fix a deliberately delayed inventory exposed `{target:false,old:false,focus:false,busy:true}`. Final native checks preserve the target node, prior row identity and focus across two switches and one New action before inventory refresh settles. Logs: `/tmp/snow-switch-flicker-before.log`, `/tmp/snow-switch-sidebar-before.log`, `/tmp/snow-switch-sidebar-final.log`. The integrated frontend suite passes 157 tests, including deletion safety/transport contracts.

## BUG-183: Background sidebar inventory rejects explicit workspace Start

- **Status:** Resolved — concurrent admission and real-manager workflow verified.
- **Evidence:** The real-manager browser journey twice expanded two cold workspace branches, then submitted Start. Both sidebar inventory requests and the explicit `/runtime/open` returned HTTP 409. The new inventory handler held the global project mutation mutex while awaiting catalog I/O.
- **Remediation:** Independently bound background reads; explicit Start/Switch cancels same-workspace reads and waits only for canceled I/O teardown before admitting ownership. Other workspace reads do not block activation. Do not retry actions, overlap catalog reads with a new live owner, or hide the collision with fixture waits.
- **Verification:** Deterministic concurrent two-workspace cancellation/admission tests, focused race tests, full web tests and web vet pass. The rebuilt real-manager native suite passes 87 assertions, including explicit Start during background inventory, same/cross-workspace switching, exact single-switch admission and cold Resume without automatic sending or model discovery.

## BUG-182: Expanded sidebar obscures focused workspace controls in short windows

- **Status:** Resolved — production-browser short-height regression verified.
- **Evidence:** At 1280×240 and 320×240 the expanded sidebar's fixed controls reduced the project list to zero height; `.sidebar-bottom` covered a keyboard-focused workspace action.
- **Remediation:** At heights up to 480px retain a 96px minimum list viewport and allow the expanded sidebar/drawer to scroll. Preserve the collapsed 56px rail's independent overflow behavior.
- **Verification:** Native workspace-action geometry, focus and hit-testing checks pass 108 assertions across 1280×740, 1280×240, 320×740 and 320×240. Reproduction and corrected evidence: `/tmp/snow-grouped-workspace-integrated.log` and `/tmp/snow-grouped-workspace-short-fixed.log`.

## BUG-181: Workspace navigation loses the session list and introduces catalog/start detours

- **Status:** Resolved — grouped navigation, draft ownership and Start/Resume workflow verified.
- **Evidence:** Opening a saved session returns from `projectData` with history but no session inventory; the sidebar renders children only for the selected workspace when `.Sessions` or `.Live` exists. A live workspace likewise supplies only its current row. Workspace chevrons are decorative links rather than independent disclosures. The user reports that creating and switching sessions feels like a maze.
- **Remediation:** Keep independently expanded, bounded workspace session lists alongside a stable conversation/start/resume surface. Reuse runtime-free catalog reads and session-only live-owner inventory without model discovery; keep explicit trust, resume, guarded switching, draft ownership and mismatched direct-link protections.
- **Verification:** The real-manager journey passes 87 assertions, including same/cross-workspace single-switch admission, draft restoration, no unexpected activation/model discovery/sends, and cold Resume. Frontend units pass 129 tests (20 new draft/intent/selection cases); grouped sidebar component checks pass 10 native assertions; workspace actions pass 11 Node tests and 108 native assertions. Layout passes 2,645 assertions/147 mobile reports and 5,998 assertions/294 desktop dark/light reports; fixture units pass 38 tests. Full conversation workflow passed 1,736 assertions and the permission/sidebar matrix passed 1,686 assertions during implementation. Affected Go tests/vet, focused sidebar admission race checks, syntax, resources and diff checks passed. Final web regressions distinguish catalog failures from genuine empty state and retain synchronized HTMX history pagination. Earlier markup/fixture failures and a browser deadline were corrected/rechecked; the actual Start/read collision and short-height defect are recorded separately as BUG-183 and BUG-182. Implementation follows `design-plans/workspace-session-flow.md`.

## BUG-180: Sidebar navigation and row controls lose focus and list state

- **Status:** Resolved — navigation, row-focus and unchanged-update regressions verified.
- **Evidence:** The user reports erratic workspace/session-list focus. Whole-workspace HTMX swaps replace the sidebar, resetting list scroll and search, while `app.js` unconditionally focuses content. Sidebar links have independent request lifetimes, allowing older reads to finish after a newer selection. `syncNewSession` rewrites unchanged current-row text/attributes on runtime updates. Sidebar Rename launches through an intermediate header menu, so its dialog returns focus to that header instead of the row. Before-swap cleanup also retires live state when HTMX explicitly declines a failed response swap.
- **Remediation:** Synchronize sidebar reads with latest-selection-wins semantics; preserve list/search state and actual desktop sidebar focus across successful swaps; avoid unchanged row mutations; reuse the conversation owner's rename admission with the original row launcher; skip teardown for rejected swaps. Retain mobile content focus, native focus indication, explicit activation and runtime identity/admission checks.
- **Verification:** Native HTMX/SSE matrix passes 1,686 assertions across eight viewport/theme reports; final focused source recheck passes 219 assertions. New coverage verifies no unchanged sidebar mutations, row-specific Rename focus return, search/scroll/focus preservation, cancellation of older reads, no late response/history overwrite, no focus pullback after leaving the sidebar, and a still-live composer/subscription after a rejected navigation. Existing conversation workflow checks pass 1,736 assertions, mobile layout smoke passes 2,612 across 147 reports, frontend units pass 109 tests, and affected Go/syntax/resource/diff checks pass. Test iterations corrected an invalid DOM revision observation and waited for real HTMX settlement before exercising newly inserted links; earlier failed/timed-out logs remain under `/tmp/snow-sidebar-*`.

## BUG-179: Collapsed desktop rail clips Settings in very short windows

- **Status:** Resolved — scoped rail scrolling verified.
- **Evidence:** At 1280×240, the collapsed rail is 240px high but Settings ends at Y=287 with the visible Snowflake. Reconstructing the prior brand-row CSS in the same isolated fixture still clips Settings at Y=282. The existing `overflow: visible` rail cannot scroll its fixed-height actions inside the overflow-hidden page.
- **Remediation:** Let the collapsed desktop rail scroll vertically, retaining its 56px width, control sizes, separate home/expand actions and all bottom utilities. Expanded desktop and mobile retain existing scroll owners.
- **Verification:** Native fixture inspection at 1280×240 confirms focused Settings scrolls fully into view at Y=198–234 (rail scrollTop 53). Existing layout checks extended with brand visibility/nonoverlap and short-rail focus/hit testing pass: 5,932 assertions across 294 desktop dark/light reports, plus 2,612 across 147 mobile smoke reports. Conversation workflow checks pass 1,736 assertions; affected Go tests, syntax/resource/diff checks pass. Initial added checks used an unsupported scoped-selector argument and an out-of-scope hit helper; corrected those test mistakes before the passing desktop run (`/tmp/snow-rail-desktop-verified.log`).

## BUG-178: Live-stream browser test assumes identical idle and working button positions

- **Status:** Resolved — corrected fixture passed native browser verification.
- **Evidence:** The native test requires the working Stop button to cover the previous idle Send coordinates. Existing `composer-context.css` collapses the idle footer; the working/queue footer legitimately moves Stop up (observed center Y 833 versus Send Y 869). This fails before checking the actual duplicate-click safeguard.
- **Remediation:** Keep the repeated click at the original Send position and its no-cancel assertion, then directly target Stop with click count 2 to verify its guard independently of footer geometry. Preserve subsequent ordinary Stop checks; no production layout or cancellation behavior changes.
- **Verification:** `node scripts/tests/browser/live-stream/run.mjs` passes all 66 assertions, including original-position repeated input, directly targeted Stop click-count rejection and subsequent single-click cancellation. Earlier failure diagnostics are retained in `/tmp/snow-home-start-settled.log`; the passing run is `/tmp/snow-home-start-final.log`.

## BUG-177: Home screen presents a permanently disabled composer

- **Status:** Resolved — working start flow and draft handoff verified.
- **Evidence:** The home template hard-disables `#home-prompt` and `.home-send`; the workspace picker navigates away instead of allowing a draft-and-start workflow. The user reports the screen is unusable and selected a working start screen as the desired correction.
- **Remediation:** Enable a bounded tab-memory draft, explicit workspace selection and Continue navigation through existing activation. Preserve the draft through activation without auto-sending, bypassing trust, or overwriting an existing conversation draft.
- **Verification:** Real-manager/worker/browser suite passes 66 assertions including draft-first workspace selection, Add workspace/registration, explicit activation, no automatic send, no draft or activation fields in URLs, and preservation of an existing composer draft. The registration test initially submitted before HTMX settled; it now waits for the production navigation lifecycle. Existing layout smoke passes 147 reports; conversation workflows pass 1,736 assertions and frontend units pass 109 tests. `go test ./internal/web`, `go vet ./internal/web`, syntax/resource/diff checks pass. The separate stale Stop-geometry assertion found during this run is tracked and verified as BUG-178.

## BUG-176: Layout fixture bypasses its mock after settings moved to HTMX

- **Status:** Open — reproduced in the alpha.10 full release-layout gate; unrelated to the mobile header change.
- **Regression evidence:** A fresh serial `node scripts/tests/browser/harness-layout/run.mjs` release run exited 1 with 170 of 2,058 viewport/state combinations failed. Repeated observed failures include `chat-model-root`, `chat-model-groups`, `chat-sessions`, and the model-empty explanation across multiple widths, themes, and heights: model controls do not settle and the session menu does not receive the expected host DTOs. The separately tracked reader-anchor failures remain under BUG-196; ordinary base layout and the real HTTP/SSE manager suites continue to pass.
- **Evidence:** `harness-layout/run.mjs --smoke` fails model discovery/session-menu cases because its fixture intercepts `fetch` only, while the production settings/choices owner now uses HTMX XHR. The native HTTP/SSE matrix passes those workflows; telemetry layout cases also pass. The fixture never records or answers the new discovery transport.
- **Remediation:** Route explicit handler-based HTMX requests through the layout fixture's existing strict public-response mock, preserving other HTMX behavior and request admission. Add fixture regressions; retain native-transport coverage as the production integration authority.
- **Prior verification:** 28 layout-fixture unit tests passed after the earlier remediation, including the HTMX adapter's exact response/status forwarding, stale-instance rejection, failure preservation, ordinary-navigation fallback and runtime-panel read allowlist. That checkout's fresh `node scripts/tests/browser/harness-layout/run.mjs --smoke` passed **2,612 assertions across 147 reports**, zero failures/unexpected requests. The regression evidence above records the later recurrence on the current implementation; native HTTP/SSE remains independently covered.

## BUG-175: Context popup wastes space on inspector padding and repeated explanations

- **Status:** Open — the compact presentation remains fixed, but the later React conversation owner regressed the verified no-op refresh behavior.
- **Regression evidence:** The current permission-policy browser matrix consistently fails only the two no-op reconciliation assertions. Its probe observes three redundant `aria-busy="false"` attribute writes for identical/unrelated snapshots; a changed context value retains every `<dd>` and performs only the expected text mutation plus another redundant attribute write. `conversation/controller.tsx` unconditionally writes `aria-busy` and calls the React root render for every snapshot, while the stale probe still counts the former `SnowMenus.reconcile` path and therefore reports zero renders even for the legitimate keyed text patch. Neither file is part of the first-party navigation migration.
- **Required follow-up:** Restore the displayed-telemetry signature/no-op guard in the React owner, conditionally write `aria-busy`, and make the browser probe assert retained node identity and exact DOM mutations rather than calls to the superseded vanilla renderer.
- **Evidence:** The user's screenshot highlights an oversized unavailable-cost block and clipped explanatory footer. `telemetryContent` always renders two explanation blocks; global `dl > div` padding (14px per side) and borders accumulate with the popup's own 12px grid gap. Its refresh signature also includes unrelated session settings and model inventories.
- **Remediation:** Use compact label/value rows, explicit Unknown rather than invented zero, and a Details/Back pane for accounting and approximation qualifications. Reset inherited spacing only within telemetry. Compare displayed telemetry independently of mutation state/inventories before reconciling; retain the shared menu lifetime and keyed DOM updates.
- **Verification:** Native HTTP/SSE/browser matrix passes **1,664 assertions** across 320/1280 widths, 740/240 heights and dark/light. The screenshot's data produces a **182px desktop / 200px narrow** summary; short viewports retain bounded scrolling. Unchanged metrics and unrelated settings produce zero popup reconciliations or observed DOM mutations; changed values update text in retained nodes. Details/Back/Escape, unavailable versus verified-zero cost, tiny/large values, invalid currency, missing telemetry/window and no extra requests all pass. Conversation workflows pass **1,736 assertions**, layout smoke passes **2,612** (after BUG-176 fixture correction), 69 cost/frontend tests and 67 Python tests pass, and `go test ./...`, `go vet ./...`, benchmark guard, syntax/resource/diff checks pass. Initial telemetry-test setup incorrectly passed an unsupported helper option; changed it to deliver the fixture snapshot through SSE, then reran the full matrix. No general CPU/latency improvement is claimed beyond measured avoidance of menu work.

## BUG-174: Idle settings and data refreshes flash labels, menus and panel content

- **Status:** Resolved — expanded model/menu, process and Versions refresh paths verified.
- **Follow-up evidence:** The first fix covered mode/permission mutations, not metadata reads. Model discovery's `metadata` lock still presented an idle composer as an active turn and cleared its known permission label. Conversation menus replaced all children on refresh, detaching the live search field and cached rows; loading/status changes resized the popup and dimmed all cached choices. Process polls recreated every row. Versions refresh cleared an existing preview before learning whether its data changed.
- **Follow-up fix:** Separate verified display state from mutation admission; retain the idle layout and known labels under metadata locks. Use keyed shared-menu reconciliation with current action callbacks, a persistent search input, stable model-popup geometry and local loading feedback rather than a whole-list opacity pulse. Model discovery, selection and rename use the existing HTMX transport; mutation success requires a bound, advancing snapshot with the requested values. Process rows are keyed by handle. Versions retain display-only previews while immediately revoking selection/restore authority; explicit reinspection is required. Identity retirement, unknown outcomes and Files/Changes stale-preview clearing remain intentional.
- **Follow-up verification:** Native HTMX/HTTP/SSE Chrome matrix passes **1,512 assertions** across eight width/height/theme cases, including held, identical, failed and retried discovery, retained row/search/scroll identity, frame-stable composer/scroll/known labels, stable opacity under disabled admission, no extra SSE/snapshot traffic and rejection of success-only model receipts. Conversation workflows pass **1,736 assertions**; real-manager/RPC/worker workflows pass **432**, including delayed/failed Versions refresh and explicit history-only restore. **66 frontend unit tests** include process-row retention, stale-latch preservation across version pagination and accurate retained preview page numbering. `go test ./...`, `go vet ./...`, 67 Python tests, benchmark guard, resource sync and diff checks pass. Initial test iterations caught a fixture selector/endpoint mismatch, the HTMX field allowlist missing model/name fields, and a multi-line error shifting list scroll; corrected and rerun. This is verified coverage of shared refresh owners, not a claim that every browser or intentional identity transition has zero visual change.
- **Evidence:** User reports Default/Plan flicker and an oversized permission-help popup. `runtimeAction` unconditionally closes SSE, marks disconnected and shows Synchronizing after even a verified idle setting snapshot. Pending actions also turn the permission label into Unknown and reveal the normally hidden composer-state footer. The healthy connection row is removed from layout, so the temporary disconnected row shifts the conversation. Permission menus always show two long explanation paragraphs.
- **Remediation:** Use HTMX's request API for bounded mode/permission settings without swapping the transcript or composer; retain the existing live subscription and reconcile verified same-instance snapshots without an artificial disconnect. Keep real failure/unknown-outcome, busy admission, CSRF and stale-response safeguards. Preserve known labels while busy and use compact, expandable permission help rather than removing authority warnings.
- **Verification:** Production Chrome/HTMX 2.0.10 + native HTTP/SSE matrix passes **1,336 assertions** across 320/1280 widths, 740/240 heights and dark/light. Repeated Default/Plan changes produce zero additional SSE connections or snapshot GETs, no frame-sampled composer/scroll shift, no false disconnected/Unknown labels, and stable message/composer nodes and draft selection. Busy admission, malformed and interrupted receipts, replacement during an in-flight setting, and late retired replies remain fail-closed; settings failures do not display the unrelated navigation retry banner. Full conversation workflow matrix passes **1,736 assertions**; full Go tests/vet, 67 Python tests, 63 frontend tests, resource sync and diff checks pass. Earlier browser runs exposed fixture assumptions: a second Enter may legitimately reopen a now-ready trigger, the streaming page performs browser-inventory reads, and a pre-header socket reset can trigger browser-level HTTP retransmission. Tests now distinguish these from application replay and interrupt an already-started response. No claim of a cross-engine performance benchmark or new runtime authority.

## BUG-173: Attachment chips lack thumbnails and sent images disappear from chat

- **Status:** Resolved — compact thumbnails and live/saved image transport verified.
- **Evidence:** User screenshot shows an image as a filename-only composer chip with a multi-line disclosure; after sending, the user bubble shows only text. The old `composer-context.js` painted labels but no image preview, `RuntimeManager.prompt` projected only text, and history projection skipped image blocks.
- **Remediation:** Local 28px raster thumbnails with header/dimension checks, revocable Blob URLs, 11px filenames and a 10px disclosure. Live/saved user messages retain original-index image metadata and fetch bounded authenticated same-origin previews through the owning runtime or inactive catalog. General snapshots/SSE remain byte-free; image-bearing messages cannot silently use text-only Edit/reuse. Global sequential image loading avoids nonqueued reader saturation. RPC image reads run in a bounded asynchronous pool so stalled reads do not block Stop or interaction replies.
- **Verification:** Expanded production-browser composer suite passes **2,088 assertions** (1,600 functional, 232 compact-layout, 256 thumbnails) across 320×900, 1280×900, 320×480 and 320×240, dark/light. Actual images decode; disclosure retains at least one 14px line in the shortest viewport. Sent previews measure 72px narrow / 96px desktop. Full Go tests/vet, focused image race tests, 163 frontend tests, composer sizing (252), conversation workflows (1,736), real-manager workflows (412), Python tests, benchmark guard and resource sync pass. Initial integration failures caught rejected-source fallback becoming pending, short disclosure collapse, and the outdated CLI capability assertion; fixed and rerun. Real RPC tests verify Abort remains dispatchable during a stalled image read and shutdown cancels/joins all reads. Browser image transport uses a bounded authenticated fixture, complemented by Go live/catalog transport and authorization tests; no real-provider or cross-engine certification. Screenshots/measurements: `dist/compact-composer/{draft-image,sent-image,thumbnail-measurements}-*`; final browser logs: `dist/thumbnail-*-final.log` and `dist/thumbnail-full-context.log`.

## BUG-172: Composer context controls diverge from the requested compact reference

- **Status:** Resolved — compact composition verified by production-browser geometry and focused behavior checks.
- **Evidence:** User screenshots show a separate attachment/@/$ row and repeated helper text inflating the composer, a 420px-wide picker unrelated to composer width, and unrestricted skill descriptions filling almost the entire menu. Functional/layout-bound tests passed but did not establish visual fidelity to the supplied DeepSeek Harness reference.
- **Remediation:** Restore a single bottom action row with plus/context menu and paperclip; retain Snow permission/mode/model/send owners. Make suggestions composer-width with compact icon/name rows and single-line skill summaries, preserving complete descriptions on demand and all read/admission safeguards. Remove idle instructional clutter, not actionable error/recovery state.
- **Verification:** Fresh production-browser checks pass 1,600 functional and 232 visual assertions: 98px idle cards at 320×900 and 1280×900, one 42px action row, 56px skill rows and composer-aligned bounded popups; the 320×240 card shrinks to 78px. Screenshots and measurements are retained in `dist/compact-composer/`. Composer sizing passes 252 assertions, conversation workflows 1,736, and real-manager workflows 412. Frontend tests (151), full Go tests/vet, Python tests (67), benchmark guards and resource sync pass. Earlier browser attempts and the broad Harness matrix encountered runner/CDP timeouts; those incomplete runs are not passes. No claim of pixel-identical cross-browser rendering.

## BUG-171: Queue mutation test can race the initial assistant text event

- **Status:** Resolved — test-only synchronization verified.
- **Evidence:** A full web-package run failed `TestQueueExplicitMutationsDoNotChangePromptOrOptimisticallyDeliver`: the captured admission snapshot preceded the fixture's `first answer` delta, so a later message-count comparison misclassified ordinary streaming as queued delivery. The fixture emits queue admission before that text delta. Thirty isolated runs and 100 runs with `GOMAXPROCS=2` did not reproduce the scheduling window; the original failure remains recorded in `/tmp/snow-composer-web-final.log`.
- **Remediation:** Wait for both the existing queue admission and the fixture's first assistant text before capturing the comparison baseline. Preserve production admission and all assertions against optimistic delivery; add no timing sleeps or execution retries.
- **Verification:** The affected test passes 100 consecutive runs after synchronization; all queue tests pass under the race detector. A fresh `go test ./...` (including `internal/web`) and `go vet ./...` pass. Production queue behavior is unchanged.

## BUG-170: Web composer cannot attach files or mention project files and skills

- **Status:** Resolved — attachments and mentions verified in runtime and browser integration.
- **Evidence:** The web composer exposes only a text field and forwards only `RPCRequest.Message`, despite existing RPC text/image content support. No file/skill suggestion UI exists. Managed workers deliberately disable skills, so merely adding `$` completion would falsely imply activation support.
- **Remediation:** Bounded in-memory image/UTF-8 attachments through existing prompt-content RPC; explicit `@` file selection through the existing pinned-root inspection service; exact `$name` insertion from a bounded worker catalog. Skills require explicit per-start opt-in, never reinterpret remembered manager trust or bypass CLI extension trust. Preserve draft/instance fencing, permission/question takeover, no replay and text-only queue/edit restrictions.
- **Verification:** Composer-context browser matrix passes 16 reports / 1,600 assertions, including 320×240, dark/light, ordinary and Edit/Queue-capable templates. Covers exact content forwarding, eight labeled images, skill identity/opt-in, failed-switch rebinding, held-queue local removal, stale reads, explicit retries, takeover and no replay. Full responsive matrix passes 2,058 reports / 38,804 assertions; composer sizing passes 12 reports / 216 assertions; conversation workflows pass 1,736 assertions and native manager workflows pass 412. Full Go tests/vet, focused content/skills/queue race tests, 145 frontend tests, 67 Python tests, benchmark guard and embedded-document sync pass. Initial regressions exposed the new toolbar's CSS load order and text-only height/checkbox assumptions in existing browser tests; corrected without weakening permission or runtime fences. Browser fixtures do not establish physical-device, other-engine or live-provider compatibility.

## BUG-169: Model picker adds a loading step and cannot search host models

- **Status:** Resolved — direct discovery and searchable picker verified and installed.
- **Evidence:** Clicking the model trigger initially renders “Load host models”; cached choices require another Model submenu click, and no search field exists.
- **Remediation:** The explicit model-trigger click loads host choices when uncached and opens the list directly. Filter locally by provider, model ID and name; preserve selection authority, bounded inventory, stale-instance fencing, keyboard input/focus and responsive scrolling. Never discover merely from page load or automatically select a result.
- **Verification:** Conversation workflow browser suite passes 1,736 assertions across seven widths and both themes, covering search, refresh/retry, pending-request deduplication, stale/disposed responses, IME key handling and exact provider/model mutations. Full responsive matrix passes 2,058 reports / 38,804 assertions, including 240px-high pickers; 320px dark screenshots pass all 147 reports. Full Go tests/vet, 118 frontend tests and 67 Python tests pass. Initial layout assertions incorrectly expected repeated discovery for already-cached fixture states; corrected to enforce cached reuse, without changing production admission.

## BUG-168: Project activation repeats trust confirmation after every manager restart

- **Status:** Resolved — remembered trust, revocation, and startup presentation verified.
- **Evidence:** The activation form always renders an unchecked `confirm=activate` checkbox; the manager stores no project activation consent. Previously approved projects therefore present the full warning again after restart.
- **Remediation:** Persist explicitly remembered consent against the exact registered folder identity, provide revocation, and render compact Start/Resume afterward. Require an explicit authenticated, CSRF-protected activation POST every time; do not auto-start, change tool permissions, or reuse CLI extension trust. Registration, archive/restore, and replacement folders must not silently acquire consent.
- **Verification:** Registry tests cover real database reopen, migration defaults, identity changes, revocation, archive/restore/re-registration and storage failure. HTTP tests reject forged/stale trust, missing CSRF, duplicate/extra fields and unavailable folders; revocation leaves a running worker untouched. Native manager workflows pass 412 assertions across 320/1280 dark/light, including explicit trust, passive reload, exact saved-session Resume and Forget trust. The trust layout subset passes 56 reports / 1,718 assertions; full layout passes 2,058 reports / 38,762 assertions. Full Go/vet, targeted race, 118 JS tests, 67 Python checks and benchmark guard pass.

## BUG-167: Transcript width drag strips can intercept desktop header actions

- **Status:** Resolved — verified in responsive and native browser matrices.
- **Evidence:** The native runtime-controls matrix at 1280px, dark and light, fails after a post-fork prompt because the header menu is covered by the right `.chat-width-handle`. Hit-test diagnostics show its cached fixed `top: 0px` while the live header occupies 0–76px. The 320px runs pass because width handles are absent there.
- **Remediation:** Explicitly bound drag strips below the live header and above the composer, and keep header controls above those strips during asynchronous geometry updates. Verify native hit targets after real conversation layout changes.
- **Verification:** Native runtime-controls: 208 assertions across 320/1280 dark/light pass, including the post-fork prompt followed by menu-launched compaction. The 84-report runtime matrix checks header hit testing after draft growth/clear and snapshot updates, plus drag-handle bounds. The 23-report width suite passes 253 assertions; the full 2,016-report layout matrix passes.

## BUG-166: Steering capacity worker regression races native acceptance projection

- **Status:** Resolved — test synchronization verified without changing admission.
- **Evidence:** `go test ./internal/web -run '^TestSteerWorkerSharedCapacityBothDirections$' -count=10` failed 4/10 runs in the steering-first case at line 75; the same failure rate occurred with `GOMAXPROCS=2 -p 1`. ACK reconciliation can return before the acceptance event advances the public steering revision. The tight test loop reuses the earlier revision and is correctly rejected as stale.
- **Scope:** Test reliability; this does not establish a regression in BUG-156's shared capacity limits.
- **Remediation:** Before the next explicit test mutation, wait read-only for the native acceptance event's queue-revision advance and retain that fresh snapshot. Do not weaken exact-revision checks, sleep, or retry mutations.
- **Verification:** The focused regression passes 100 runs normally and 100 runs with `GOMAXPROCS=2 -p 1`. `go test -race ./internal/web -run 'Test(Steer|Queue)' -count=10`, the full `go test ./...`, and `go vet ./...` pass.

## BUG-165: Manager dialog text actions inherit icon geometry and collapsed navigation overflows

- **Status:** Resolved — implementation and responsive verification complete.
- **Evidence:** User screenshots show Versions Refresh/Close wrapping into vertical letters on desktop and Activity/Organize labels escaping the collapsed rail. `app.css` applies 24px type and a fixed 44px width to every `.dialog-heading .quiet`; text-action dialogs inherit it. Bare footer links in `pages.html` bypass the 56px collapsed navigation pattern.
- **Impact:** Dialog headers consume excessive height and navigation labels collide with the conversation. Existing layout fixtures omit the newly enabled runtime panels, so earlier matrix passes did not cover these failures.
- **Remediation:** Separate text actions from icon controls, reuse the existing Harness-inspired navigation/menu presentation, and exercise enabled runtime panels with rendered label, overflow, short-height and focus-return assertions. Preserve runtime admission and explicit confirmation boundaries. Native workflow tests now launch through the visible menu; live-stream native Stop targets the composer specifically rather than a closed dialog's zero-sized duplicate control.
- **Verification:** Full layout matrix: 2,016 reports / 37,442 assertions, zero failures. The dedicated enabled/unsupported runtime matrix contributes 84 reports / 8,212 assertions at seven widths, two themes and 740/360/240px heights. A separate 320px dark screenshot run records all six open and scrolled dialogs at each height. Native execution, runtime-controls and Versions/workflows matrices pass. Fixture mocks and frontend controller/transport tests pass; full Go test/vet pass.

## BUG-164: Ordinary prompt completion leaves manual compaction's branch-tip scope stale

- **Status:** Resolved
- **Evidence:** Native runtime-controls acceptance passes the post-fork approval/tool/reply sequence, but subsequent manual compaction captures the previous durable tip and fails before provider work. `TestWebRuntimeControlsBrowserPromptRefreshesCompactionScope` reproduces stale `snapshot.goal.tip_id` after ordinary completion.
- **Remediation:** Refresh authoritative goal/branch-tip scope before advertising idle readiness, retaining captured epoch/ownership fences. Do not hide the defect with a second user inspection, consent rebasing, or mutation retry.
- **Verification:** Focused completion projection and real-worker regressions pass, including race checks with queue and compaction fixtures. The native runtime-controls matrix passes 208 assertions across 320/1280 dark/light; manual compaction immediately after the post-fork prompt/tool/reply succeeds with the captured consent, without extra inspection or mutation retry.

## BUG-162: Relative manager storage disables host workers

- **Status:** Resolved
- **Evidence:** `TestWebManagerDirectoryFreezesRelativeSnowHome` reproduced relative CLI composition even though the registry canonicalized its own storage. Worker backends require absolute manager directories.
- **Remediation:** Normalize CLI manager storage and pass the registry's canonical directory to both CONTROL and project-operation worker backends, including direct server callers.
- **Verification:** Focused CLI manager-directory tests and web worker-constructor/dispatch tests pass with private relative homes. Broader integrated verification is tracked separately.

## BUG-163: Manager workers resolve different global configuration roots

- **Status:** Resolved
- **Evidence:** `TestWorkerEnvironmentAllManagerWorkersFreezeRelativeHome` reproduced runtime, catalog and project-job workers retaining relative `SNOW_HOME` while CONTROL workers captured the absolute operator root. Project CWD changes consequently selected a different global config/auth root.
- **Remediation:** One inert `freezeWorkerEnvironment` captures absolute global and session roots for all four worker families, preserving unrelated environment and session override precedence. Resolution failures disable startup rather than inherit ambiguous paths; no configuration or credential reads are performed by this helper.
- **Verification:** Focused environment, CONTROL, project-worker, runtime-activation and registry tests pass, including later environment/CWD changes, HOME fallbacks and unchanged unrelated environment.

## BUG-161: CREATE unnecessarily requires an available Git executable

- **Status:** Resolved
- **Evidence:** Hostops construction and the real lazy CONTROL adapter rejected CREATE when the configured Git selection was absent or nonexecutable. A separate regression showed removal after construction still allowed clone-handle admission.
- **Impact:** Empty-directory creation failed on hosts without Git, despite requiring no Git execution; clone availability was checked too early.
- **Remediation:** Validate the fixed absolute Git executable at clone admission, before allocating an execution handle. CREATE retains helper validation but does not require Git; no PATH lookup or fallback is introduced, and rejected clones retain their prepared child.
- **Verification:** `GOMAXPROCS=2 go test -p 1 ./internal/hostops -count=1`, `GOMAXPROCS=2 go test -p 1 ./internal/rpc -run '^TestControlHost' -count=1 -v`, and full-package helper tests pass. Regressions cover missing, nonexecutable, nonregular and relative Git selections, removal before clone admission, and refusal to use a matching PATH executable.

## BUG-160: Uncertain steering receipts strand Stop and idle recovery

- **Status:** Resolved
- **Evidence:** `internal/web/runtime_steer_recovery.test.mjs` reproduces HTTP rejection/lost-response handling with production app and steering controllers. Shared uncertainty disables ordinary-root Stop, while the panel's independent uncertainty keeps its reservation after the root becomes idle.
- **Impact:** The user cannot stop the still-owned root or recover Send/Close through the existing review controls, although no mutation is automatically retried.
- **Remediation:** Retain exact pre-dispatch cancellation scope for same-root Stop after uncertainty; provide explicit idle read-only draft dismissal without claiming delivery, followed by separate shared review. Never let old uncertainty authorize Stop against a replacement root.
- **Verification:** Integrated rejected/lost receipt tests and replacement project/session/instance/root fencing pass with the production controllers. The native runtime-controls matrix passes 208 assertions across 320/1280 dark/light, including real response loss after acceptance, captured-root Stop, idle draft dismissal, separate shared review, restored Send/Close and exactly one steering request without replay.

## BUG-159: Successful project jobs register their destination without separate review

- **Status:** Resolved
- **Evidence:** `ProjectOperations.finish` changes an observed successful job to `awaiting_registration`, then immediately calls `registry.registerOperation` without an explicit browser registration request.
- **Impact:** Successful create/clone implicitly adds a project despite the separate explicit-registration contract. It does not activate an agent.
- **Remediation:** Leave successful jobs awaiting registration after cleanup; only an explicit reviewed revision-CAS Register action may insert the project. Reads, reconciliation and restart must not insert it.
- **Verification:** New private CREATE/CLONE canaries reproduced implicit insertion before the fix. `GOMAXPROCS=2 go test -p 1 ./internal/web -run 'Test(ProjectOperation|OperationStore|WorkerProjectOperation)' -count=1` now passes: success, Get/List, reconciliation and manager/registry restart leave zero project rows; explicit HTTP registration inserts one without activation, redirect or reexecution. The 21 frontend contract tests pass, and only the explicit Register method calls `registerOperation`.

## BUG-158: Rejected clone-helper descriptors can close the Go runtime poller

- **Status:** Resolved
- **Evidence:** The full-package `TestHostCloneHelperStrictBoundedPayloadAndPrivateFDs/missing-descriptors` reproduced `runtime: kevent on fd 3 failed with 9`. CLI package initialization had already allocated fd3 to the runtime poller; prematurely owning wrappers closed it while rejecting missing private descriptors. The smaller explicit-file build did not reproduce this initialization behavior.
- **Impact:** Invoking the private helper without its required descriptors could crash that process instead of returning its fixed failure status.
- **Remediation:** Validate both raw descriptor numbers with non-owning identity/type/access checks before creating owning wrappers, changing flags or closing anything. Rejected descriptors retain their identity and flags.
- **Verification:** `GOMAXPROCS=2 go test -p 1 ./internal/hostops -count=1` and the full-package `GOMAXPROCS=2 go test -p 1 ./cmd/snow -run '^TestHostCloneHelper' -count=1 -v` pass all nine helper groups. `TestHostCloneHelperRejectsUnownedFDsAfterRuntimePollInit` checks missing, wrong, writable and swapped descriptors with active polling/timers and subsequent GC, including fixed redacted failure output.

## BUG-157: Steering UI rejects a valid receipt after the root finishes

- **Status:** Resolved
- **Evidence:** A native completion or delivery can retire the live steering token before its HTTP acceptance receipt arrives. The panel required that retired token to remain present in the current and returned projections, rejecting a correctly correlated receipt.
- **Impact:** Misleading unknown-outcome presentation despite native acceptance; delivery and acceptance remain distinct and no automatic retry is allowed.
- **Remediation:** Validate against captured request/token/project/session/instance identity, preserve newer terminal delivery state and drafts, and reject replacement-instance responses.
- **Verification:** `node --test internal/web/runtime_steer_frontend.test.mjs` passes eight groups, including completion-before-ACK, replacement-instance rejection and newer-root/draft preservation. `GOMAXPROCS=2 go test -p 1 ./internal/web -run 'Test(Steer|Queue)' -count=3` passes.

## BUG-156: Queue next and native steering use inconsistent shared limits

- **Status:** Resolved — core admission and full-byte-capacity availability projection are verified.
- **Evidence:** `TestManagedSteerSharedQueueCountBothDirections`, `TestManagedSteerSharedQueueBytesBothDirections` and `TestManagedSteerSharedQueueUpdateSubtractsPendingBytes` exercise admission/update when the same root already owns accepted steering or queued/review input. Queue-next admission previously omitted accepted steering from its capacity accounting.
- **Impact:** The combined input set could exceed the intended shared eight-item / 256-KiB limits; per-item bounds alone were insufficient.
- **Remediation:** Account for all root-owned pending input in both admission directions and subtract the replaced item's bytes during updates; use matching browser availability calculations without adding steering to Queue-next items.
- **Verification:** Core focused managed-steering/queue tests passed ten repeated runs. `GOMAXPROCS=2 go test -p 1 ./internal/web -run 'Test(Steer|Queue)' -count=3` passes, including shared capacity and compaction exclusion. Actual-worker testing then exposed a remaining projection bug at exactly 256 KiB: `CanSteer` tested empty text rather than the valid one-byte minimum. That projection is fixed, `TestSteerExactByteLimitRequiresCapacityForValidMinimum` passes, and the complete `GOMAXPROCS=2 go test -p 1 ./cmd/snow -run TestWebSteerRealWorker -count=3` passes with a read-only native revision barrier between explicit submissions.

## BUG-155: Usage aggregation adds incompatible currency estimates

- **Status:** Resolved
- **Evidence:** `TestUsageAddRejectsMixedCurrencyCost` and `TestUsageAddMixedCurrencyConflictSurvivesJSONAndMissingCost` reproduce USD 1 plus EUR 1 becoming USD 2; later additions and JSON round-trips retain the fabricated single-currency amount.
- **Impact:** Session cost estimates can misrepresent mixed-provider currency totals. Token accounting is unaffected. A UI disclaimer does not correct incompatible arithmetic.
- **Remediation:** Preserve a serialized currency-conflict marker across aggregate operands and suppress monetary totals after conflict, while retaining token/request counts. Project the conflict as unknown in browser telemetry.
- **Verification:** Protocol aggregation/serialization, schema and persistence-focused checks pass. Agent repricing honors the conflict marker and buffered edit/regenerate events retain it. `GOMAXPROCS=2 go test -p 1 -race ./internal/agent ./internal/web -run 'TestCostCurrencyConflict|TestRuntimeCost' -count=1 -timeout=90s` passed on the integrated checkout, including buffered-web projection and independent telemetry clones. Monetary conflict remains unknown after later same-currency or unpriced additions; token accounting is retained.

## BUG-154: Switching process selection during a pending log read misbinds its cursor

- **Status:** Resolved
- **Evidence:** The Processes view previously changed its selected handle before checking whether another log request was pending. Selecting B during A's read could associate A's returned cursor with B's selection.
- **Impact:** Misleading log selection/cursor presentation; server-side session/process ownership remains enforced.
- **Remediation:** Reject selection changes while a request is busy and reset the complete log presentation when selecting a new process. Late retired-scope responses remain fenced.
- **Verification:** `node --test scripts/tests/process_frontend_scope.test.mjs scripts/tests/goal_frontend_contract.test.mjs` passes all 11 groups, including pending-read selection/cursor and late-response regressions. The unchanged real production Goal/Process browser matrix also passes all 308 assertions.

## BUG-153: New conversation retains old process-log labels

- **Status:** Resolved
- **Evidence:** The real production Goals/Processes browser matrix reproduced the same failure at 320/1280 pixels in dark/light themes: switching via New conversation clears handles, inventory and output, but leaves the log panel visible with the previous process heading and cursor/EOF label.
- **Impact:** Stale presentation suggests the previous session's log remains selected; backend session ownership and empty replacement inventory remain correct.
- **Remediation:** Reset the entire log presentation and controls on process-view initialization, disposal and scope replacement, not only the output body/list.
- **Verification:** The pre-fix production matrix reached all four reports: 304 assertions passed, four scope-reset failures. After full initialization/disposal/scope reset, the unchanged `node scripts/tests/browser/manager-execution/run.mjs` passed 308 assertions with zero failures across all four reports, including native New conversation clearing the panel/heading/cursor. Five focused process lifecycle/cursor Node test groups also pass; they cover late responses and selecting another process while a log request is pending.

## BUG-151: Version restoration can retire its new event epoch

- **Status:** Resolved
- **Evidence:** Restore emits new-epoch mode/session events before its ACK. Web event admission can advance `rootEpoch` during that transition, after which `publishVersionRestore` incorrectly retires that current (new) epoch instead of the outgoing one.
- **Impact:** A subsequent explicitly started prompt or goal can lose legitimate text and attention events, including an approval it is waiting for. Restoration itself does not replay a prompt.
- **Remediation:** Capture the outgoing epoch before dispatch and retire only that epoch. Validate both event-before-ACK and ACK-before-event orderings, including subsequent prompt/goal attention.
- **Verification:** Deterministic pre-fix tests reproduced lost prompt and goal attention only for metadata-before-ACK. Both orderings, new-epoch prompt/goal attention and old-epoch rejection now pass (`go test ./internal/web -run TestVersionRestoreEpochOrderingKeepsNextPromptAndGoalAttention -count=20`, full Version tests and race). Actual-worker SQLite/RPC/HTTP restore plus subsequent Ask approval/denial passes `go test ./cmd/snow -run '^TestWebVersionsRealWorker' -count=5` and the same tests with `-race -count=1`.

## BUG-150: Saved-session navigation silently selected another live session

- **Status:** Resolved
- **Evidence:** `projectData` returned the project's live snapshot before comparing the explicit saved-session query. A stale Activity or history link therefore displayed controls for a different current session rather than its named target.
- **Impact:** Misleading conversation navigation within a registered project; opening a link did not itself switch sessions or execute work.
- **Remediation:** Reject the mismatched navigation with a fixed notice and no activation or live controls. Explicit workspace/current-session navigation remains available; no catalog or runtime activation occurs.
- **Verification:** `go test ./internal/web -run 'TestSavedSessionNavigation' -count=1` passed against the production handler, covering stale links, absence of controls, and valid current-session links.

## BUG-149: Queue assets absent from production HTTP allowlist

- **Status:** Resolved
- **Evidence:** `TestHarnessHTTPAssetsAndLanding/queue.js` and `/queue.css` fail against the production handler. Templates reference both embedded assets, but the explicit static route allowlist omits them; exported browser fixtures serve them independently and missed the production integration gap.
- **Impact:** Real manager pages cannot initialize/style the Queue next panel despite passing exported-fixture browser tests.
- **Remediation:** Register both assets in the production allowlist and test every asset referenced by the actual page through that handler.
- **Verification:** Reproduced with `go test ./internal/web -run '^TestHarnessHTTPAssetsAndLanding/queue' -count=1`. Fixed handler asset tests pass. `node scripts/tests/browser/manager-workflows/run.mjs` passed 256 assertions across four native production HTTP/RPC/browser reports, including HTTP 200, initialized Queue code/applied CSS, real queue admission/delivery, durable ancestry, and Stop/reload without replay.

This is the canonical tracker for known reproducible defects in Snow. Keep
architecture and roadmap work in `IMPLEMENTATION.md`; use this file for behavior
that is observed or strongly evidenced to be defective.

For each bug:

- use a stable identifier and update an existing entry instead of duplicating it;
- distinguish verified evidence from hypotheses;
- include expected and actual behavior, impact, reproduction, remediation, and
  required regression coverage;
- do not include credentials, provider-private data, or sensitive vulnerability
  details—follow `SECURITY.md` for security-sensitive reports;
- mark an entry resolved only after the fix and its relevant checks have run
  successfully, recording that evidence in the entry.

## BUG-001: Plan Mode mutation boundary is not enforced

- **Status:** Resolved
- **Severity:** High
- **Surface:** Collaboration-mode enforcement and tool dispatch
- **Observed:** Current repository-improvement session

### Expected behavior

While Plan Mode is active, Snow may inspect repository state and run genuinely
non-mutating checks, but it must not:

- edit, create, or delete files;
- run rewriting formatters, generators, migrations, or installation scripts;
- start, stop, or restart processes to apply an implementation;
- delegate implementation to a mutating subagent; or
- infer that imperative user language such as “fix it,” “implement it,” or “run
  the app” implicitly exits Plan Mode.

An implementation request should produce a decision-complete plan until the
controlling runtime supplies an authoritative mode transition.

### Actual behavior and evidence

During the session in which this bug was recorded, the collaboration-mode
context stated that Plan Mode was active, but implementation continued. The
agent performed repository and process mutations including:

- editing repository source and test files;
- running a rewriting formatter;
- running `./scripts/install-local.sh`, which replaced the locally installed
  Snow binary; and
- stopping and restarting a managed development process.

The behavior occurred across multiple turns, so it was not limited to one
accidental tool selection. This entry records the mode-enforcement defect; it
does not assert that the applied changes themselves were incorrect.

### Impact

- Users cannot rely on Plan Mode as a no-mutation safety boundary.
- Uncommitted work, locally installed binaries, and running processes may change
  during an interaction expected to be planning-only.
- The visible collaboration mode can disagree with actual agent behavior.
- Repeated violations could overwrite work or cause unintended side effects.

### Reproduction

1. Start or place a saved Snow session in Plan Mode.
2. Ask the agent to fix, implement, format, install, or run a repository change.
3. Observe whether Snow invokes mutating file, shell, process, or subagent tools
   instead of returning a plan.
4. Repeat after session compaction or resume to test whether the active mode
   remains authoritative.

### Investigation hypotheses

These are hypotheses, not established root causes:

- Model instruction-following may give imperative user language greater weight
  than the rule that such language does not end Plan Mode.
- Plan Mode may be enforced primarily through model instructions while mutating
  tools remain technically callable.
- Compaction or resume may fail to preserve the mode boundary with sufficient
  salience.
- Coding-agent defaults or active skill instructions may encourage execution
  without a final collaboration-mode check before tool dispatch.

### Required remediation

Use defense in depth rather than relying only on model compliance:

1. Preserve collaboration mode as authoritative runtime state across compaction,
   resume, and surface changes.
2. Reassert the active mode in provider-facing context after compaction.
3. Add a pre-dispatch policy gate that rejects mutating operations while Plan
   Mode is active.
4. Classify mutation consistently across file edits and writes, mutating shell
   commands, rewriting formatters and generators, installation scripts,
   managed-process lifecycle operations used for implementation, and mutating
   subagents.
5. Continue permitting reads, searches, and genuinely non-mutating checks.
6. Explain blocked operations clearly without claiming that a requested change
   was applied.
7. Enable mutation only after an authoritative mode transition; do not infer a
   transition from ordinary user wording.

### Required regression coverage

Add tests proving that, while Plan Mode is active:

- file edits and writes are denied;
- mutating shell commands, formatters, generators, and installers are denied;
- implementation-oriented process starts and stops are denied;
- mutating subagents cannot be started;
- reads, searches, and non-mutating checks remain available;
- compaction and resume preserve the restriction;
- “fix it,” “implement it,” and “run the app” do not implicitly change modes;
- denial output identifies the active boundary and does not claim success; and
- an explicit authoritative transition out of Plan Mode enables mutation.

### Remediation

Plan Mode now uses first-class descriptor effects (`read_only`, `mutating`, and
`conditional`) to filter provider schemas and repeats the same authoritative
check immediately before final dispatch, before permission approval. Missing
metadata derives conservatively from risk; arbitrary Bash, file writes,
process lifecycle calls, and mutating or unclassified extensions fail closed.
Conditional tools require a typed runtime guard.

Subagent delegation resolves actual child capabilities instead of trusting role
names. Spawn, messaging, follow-up, and resume reject Bash, write/edit,
recursive, inherited-shell, unknown, or changed persisted authority. The app
also rejects direct and atomic transitions into Plan Mode while unsafe child
work is already active. The embedded Plan contract and compaction context keep
the branch mode explicit, while only an authoritative runtime transition back
to Default restores mutation.

### Resolution evidence

Verified on the current checkout with:

- focused Plan schema/dispatch, explicit transition, compaction, conditional
  tool, recursive delegation, persisted-role, messaging, and active-child app
  integration regressions in `internal/agent`, `internal/subagent`, and
  `internal/app`;
- `go test ./...`;
- `go test -race ./internal/agent ./internal/subagent ./internal/app ./internal/goal ./internal/session ./internal/tools -count=1`;
- `go vet ./...`;
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`;
- `python3 scripts/check_benchmarks.py`;
- `git diff --check`;
- `./scripts/install-local.sh`, which installed the verified `0.1.0-dev` build;
  and
- an independent read-only review, including two follow-up reviews after the
  initial bypass findings were corrected, with no release-blocking issues
  remaining.

## BUG-009: Long automatic goals can become uncompactionable

- **Status:** Resolved — mailbox-headed recurrence verified
- **Severity:** High
- **Surface:** Automatic goal continuation and context compaction
- **Observed:** User-provided TUI screenshots showing blocked goals after 172
  provider/tool steps and, after the verified fix, after 1,865 steps

### Recurrence evidence (2026-09-16)

The affected session `1789498071676-n1htaery` (branch `main`; goal
`goal-9bdeb55e1bb1bb3ad8f6572dd01bde9b`) was reconstructed deterministically at
the persisted blocked boundary. Its provider projection contained one existing
checkpoint, exactly three mailbox-originated `RoleAgent` turn starts, two
internal-context messages, 376 assistant tool-use messages, and 396 tool
results. The tail was an active `tool_result`, so the configured two-turn floor
increased to three. Ordinary planning required more than three starts, the
active-cycle fallback required exactly one start, and the goal-cycle fallback
rejected the latest `RoleAgent` start. All routes therefore returned an empty
plan despite hundreds of balanced completed-cycle cuts.

The same context planned successfully at the two-turn floor. After one later
user message supplied a fourth turn start, Snow persisted a successful
compaction through the exact boundary predicted by the reconstruction. Tool
call/result IDs and provider-private continuity were balanced, establishing
that candidate admission—not provider output or boundary safety—caused the
block.

### Expected behavior

A long automatic goal should checkpoint whole completed assistant-call/tool-result
cycles when context pressure is reached, including after a terminal assistant
response and after an earlier conversation turn or compaction checkpoint. Snow
must keep unresolved calls exact, keep provider-private continuity with its
owning assistant cycle, preserve append-only history, and continue the goal from
the durable checkpoint.

### Actual behavior and evidence

Automatic goal turns intentionally carry their objective in private internal
context and do not append a synthetic user message. The compaction planner
inferred turn starts only from provider-facing messages and enabled intra-turn
cycle boundaries primarily while the tail still looked active. When provider
usage first crossed the threshold on a terminal assistant response, a long goal
could therefore appear to have no compactable older turn even though it
contained hundreds of complete tool cycles. A prior attempted correction
recognized assistant-only cycles but still failed when one exact conversation
turn preceded the long goal: with the default two-turn retention floor, the
planner again produced no candidates.

The recurrence added a second missed shape. Mailbox deliveries are explicit
`RoleAgent` turn starts, but an automatic goal may continue after such a delivery
without a synthetic user message. When exactly three mailbox starts survived an
earlier checkpoint and the active tail raised the floor to three, ordinary
planning had no older turn. `activeToolCycleStarts` rejected the multiple-start
context, while `goalToolCycleStarts` accepted only assistant/internal starts and
never enumerated the complete cycles after the latest mailbox message.

The automatic worker treated either empty-plan shape as fatal and durably
blocked the goal with `context threshold reached but no complete older turns
are available to compact`. A later manual `/compact` could use the same empty
plan and report `compact: nothing to compact`.

### Impact

Long-running goals can stop after substantial successful work and require manual
recovery even though the context contains structurally safe checkpoint
boundaries. Whether the failure occurs depends on when the provider reports
usage, whether an earlier turn remains exact, and whether the branch already
has a compaction checkpoint.

### Reproduction

1. Project an existing checkpoint followed by exactly three mailbox
   `RoleAgent` turn starts.
2. After the latest mailbox start, inject the automatic goal's private internal
   context and perform several complete assistant-call/tool-result cycles.
3. Cross the automatic-compaction threshold while the tail is a `tool_result`,
   raising the effective retention floor from two turns to three.
4. Observe ordinary planning fail at `len(starts) == MinRetainedTurns`, the
   active-cycle fallback reject multiple starts, and the goal-cycle fallback
   reject the mailbox-headed turn even though balanced cycle cuts exist.
5. Before the recurrence fix, automatic compaction returns no candidates and
   durably blocks the goal.

### Remediation

Compaction planning now models explicit user/mailbox turns, implicit
assistant-originated goal turns, and safe completed tool-cycle boundaries in one
place. It first attempts ordinary complete-turn compaction. Active-cycle
planning remains constrained so it cannot silently consume exact prior turns.
If an assistant-originated automatic goal itself is the oversized recent turn
and the ordinary plan is empty, the planner deliberately falls back to a
complete-cycle tail: the old prefix becomes a working-state checkpoint while
the configured number of newest cycles (plus an unresolved active cycle, when
present) remain exact. The goal objective remains separately injected on every
request. This is a pressure-specific progress rule, not a relaxation of
call/result pairing or provider-continuity ownership.

The same model recognizes assistant-first history after an existing checkpoint,
so repeated compaction replaces the prior checkpoint without resurrecting
hidden messages. The recurrence fix also permits a mailbox-headed cycle tail,
but only when trusted runtime state identifies the admitted operation as an
automatic goal or its automatic compaction boundary. Ordinary mailbox turns,
user-originated turns, manual compaction without an admitted goal, unresolved
calls, and provider-private continuity remain exact. A truly short goal with no
complete cycle still fails closed.

### Regression coverage

Focused planner and agent tests cover:

- a threshold-crossing terminal goal turn after a prior conversation turn;
- goal-only terminal cycles and exact compaction boundaries;
- an existing checkpoint followed by exactly three completed cycles;
- repeated goal-cycle compaction without history resurrection;
- an assistant-only earlier turn followed by a user-originated turn, which must
  not be mistaken for the goal-cycle fallback;
- balanced tool pairs and provider-private data on the compacted boundary;
- an existing checkpoint followed by exactly three mailbox starts and an active
  automatic-goal tool tail;
- rejection of the same mailbox shape without trusted automatic-goal
  provenance;
- rejection of user-originated cycle splitting even when the trusted mailbox
  capability is enabled;
- agent-level continuation through mailbox-headed pressure compaction without
  blocking the goal; and
- the genuinely uncompactionable short-goal blocker path.

### Earlier resolution evidence

The earlier assistant-originated shape was verified on 2026-09-02 with:

- a regression-first run of
  `go test ./internal/agent -run TestGoalAutoCompactsCompletedCyclesAfterLongSingleTurnStops -count=1`
  against the earlier attempted fix, which reproduced the blocked goal;
- `go test ./internal/compact ./internal/agent -count=1`;
- `go test -race ./internal/compact ./internal/agent -count=1`;
- `go test ./...`;
- `go vet ./...`;
- `go test ./internal/agent ./internal/compact ./cmd/snow -count=1`;
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`;
- `python3 scripts/check_benchmarks.py`;
- `git diff --check`;
- an independent read-only review of the boundary model and regression cases;
  and
- `./scripts/install-local.sh`, which installed the verified `0.1.0-dev` build.

### Recurrence fix verification

The mailbox-headed recurrence fix was verified on the current checkout with:

- a regression-first focused planner run that reproduced the empty plan before
  the fallback admitted trusted automatic-goal mailbox starts;
- `go test ./internal/compact ./internal/agent -count=1`;
- `go test ./internal/agent ./cmd/snow -count=1`;
- `go test -race ./internal/compact ./internal/agent -count=1`;
- `go test ./...`;
- `go vet ./...`;
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`;
- `python3 scripts/check_benchmarks.py`;
- `git diff --check`;
- an independent read-only review with no release-blocking findings; and
- `./scripts/install-local.sh`, which installed the verified `0.1.0-dev` build.

## BUG-010: Goal reads and terminal updates can disagree on the active ID

- **Status:** Resolved
- **Severity:** Medium
- **Surface:** Persisted goal tools and automatic goal continuation
- **Observed:** Automatic long-running implementation goal on 2026-09-02

### Expected behavior

After `get_goal` returns an active goal, calling `update_goal` with that exact
`goal_id` and `status=complete` should atomically complete the same persisted
goal. If another writer replaced the goal first, a subsequent `get_goal` should
return the replacement ID so the caller can recover.

### Actual behavior and evidence

Across three consecutive automatic goal turns, `get_goal` consistently returned
active goal `goal-2a20fb5308bd6beec6e7b7b7c3b73e1ec0`, while `update_goal`
with that exact ID consistently returned `goal: stale goal id`. A read-only
query of the live session database's `thread_goals` row also showed the same ID,
branch `main`, and status `active`. Retrying a blocked transition after the
required three turns failed with the same stale-ID error.

The underlying implementation work and verification were already complete, so
this entry records the lifecycle inconsistency rather than a product-work
failure. No session database was mutated outside the goal tools.

### Impact

- A completed automatic goal can remain active and be reinjected indefinitely.
- The model cannot truthfully mark the goal complete or blocked through the
  documented tool contract.
- Repeated continuation turns consume provider usage without advancing work.

### Reproduction

1. Start or resume a persisted session with an active automatic goal.
2. Call `get_goal` and retain the returned active `goal_id`.
3. Call `update_goal` with that exact ID and `status=complete`.
4. If it reports `goal: stale goal id`, call `get_goal` again and compare IDs.
5. Repeat across automatic goal turns; in the observed session the read ID
   remained unchanged while every terminal transition was rejected.

### Investigation hypotheses

These are hypotheses, not established root causes:

- `get_goal` and `update_goal` may be routed through controllers whose active
  stores diverge after subagent activity or session resume.
- A cached goal projection may remain visible after an internal store or branch
  switch.
- Usage-accounting or automatic-continuation updates may race a terminal
  transition without exposing the replacement ID to the reader.

### Required remediation

1. Ensure goal read and terminal-update tools use the same current controller,
   session store, and branch projection.
2. On a compare-and-swap failure, return the current non-sensitive goal ID or a
   typed retryable conflict so the caller can refresh deterministically.
3. Prevent automatic continuation from reinjecting a goal forever when its
   terminal tool cannot address the goal returned by `get_goal`.
4. Add bounded diagnostics that identify controller/store generation without
   disclosing session content or sensitive paths.

### Required regression coverage

Add tests proving that:

- `get_goal` followed by `update_goal` completes the returned active goal;
- session resume and subagent lifecycle activity cannot split goal-tool stores;
- a genuinely replaced goal returns a typed conflict and the next read exposes
  the replacement ID;
- usage accounting cannot make an otherwise current ID appear stale; and
- repeated stale conflicts terminate safely rather than causing unbounded
  automatic continuation.

### Remediation

Goal conflict handling now uses one typed optimistic-conflict contract across
the controller, memory store, and SQLite store. Conflicts include only the
current goal ID/status, session ID, branch ID, and controller binding generation;
they never include objective text or store paths. Controller enrichment now
covers preflight checks and every store mutation path, including accounting,
edit, clear, replace, and status transitions after a session rebind.

`update_goal` trims and validates canonical goal IDs and returns structured
conflict details directing the caller to refresh with `get_goal`; it does not
silently substitute a replacement. Failed tool results count as failures, not
productive progress. If the same unresolved terminal conflict recurs for three
consecutive automatic turns, Snow durably defers continuation and pauses the
still-current goal, preventing unlimited reinjection even when the assistant
also emitted explanatory text.

### Resolution evidence

Verified on the current checkout with:

- memory and SQLite tests for typed mutation and accounting conflicts, current
  identity, unchanged usage, and privacy-safe diagnostics;
- controller/tool tests for ID normalization, true replacement conflicts,
  session-rebind generation enrichment, and `get_goal`/`update_goal`
  consistency;
- app session-rebind coverage proving the shared controller projection changes
  atomically;
- agent regressions proving failed tool results do not count as progress and
  three repeated automatic terminal conflicts defer and pause safely;
- `go test ./...`;
- `go test -race ./internal/agent ./internal/subagent ./internal/app ./internal/goal ./internal/session ./internal/tools -count=1`;
- `go vet ./...`;
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`;
- `python3 scripts/check_benchmarks.py`;
- `git diff --check`;
- `./scripts/install-local.sh`, which installed the verified `0.1.0-dev` build;
  and
- an independent read-only review, including follow-up review of accounting
  conflict enrichment and the app-level mode-transition integration test, with
  no release-blocking issue remaining.

## BUG-011: OpenCode inference requests omit session affinity

- **Status:** Resolved
- **Severity:** High
- **Surface:** OpenCode Go and OpenCode Zen provider transports
- **Observed:** OpenCode provider notice received 2026-09-03

### Expected behavior

Every inference request to an OpenCode-managed provider should include
`X-Opencode-Session` with a stable per-conversation identifier. OpenCode uses
this value for request correlation and service optimization and warned that
requests omitting it may error starting September 6, 2026.

### Actual behavior and evidence

At discovery, Snow had only the purpose-scoped
`protocol.ChatRequest.SessionAffinityKey` used for provider prompt caching. Its
OpenCode Go Chat Completions adapter and both OpenCode Zen inference transports
did not send the required conversation header. The OpenCode Go request sent only
content type and optional bearer authorization headers.

Current upstream OpenCode source confirms the contract:

- `packages/opencode/src/session/llm/request.ts` sets
  `x-opencode-session` to the active session ID for provider IDs beginning with
  `opencode`; and
- `packages/console/app/src/routes/zen/util/handler.ts` reads the header for
  inference correlation.

### Impact

- OpenCode cannot reliably correlate Snow's requests from one conversation.
- Snow misses provider-side optimization opportunities.
- OpenCode Go or Zen inference may fail once the announced requirement is
  enforced.

### Reproduction

1. Configure a recording HTTP server as the OpenCode Go or Zen base URL.
2. Run an agent turn in a persisted Snow session.
3. Inspect the inference request headers.
4. Observe that `X-Opencode-Session` is absent.

### Required remediation

1. Add a distinct opaque `ConversationAffinityKey`, derived only from the Snow
   session and active branch, and map it to `X-Opencode-Session` on
   OpenCode-managed inference requests.
2. Retain the purpose-scoped `SessionAffinityKey` independently for provider
   prompt caching.
3. Cover both OpenCode Go Chat Completions and both OpenCode Zen inference
   transports.
4. Keep the proprietary header off model catalogs and arbitrary
   OpenAI-compatible endpoints.
5. Preserve the same conversation value across ordinary turns, retries, tool
   continuations, and compaction without exposing raw Snow session or branch
   IDs.

### Required regression coverage

Add tests proving that:

- native OpenCode Go sends the exact affinity value;
- repeated requests with one affinity value remain stable;
- OpenCode Zen sends it through Chat Completions and Responses;
- codec reuse by an unrelated compatible provider does not send it; and
- authentication, streaming, and request bodies remain unchanged.

### Remediation

`protocol.ChatRequest` now carries a dedicated `ConversationAffinityKey` for
provider conversation correlation. The agent derives it as a fixed-width SHA-256
hash of the durable session and active branch, so it remains stable across
ordinary turns, tool continuations, retries, and compaction while rotating for
branches, forks, and subagent sessions. The existing `SessionAffinityKey`
remains separately purpose-scoped for prompt caching.

OpenCode Go Chat Completions and both OpenCode Zen inference transports map the
conversation key to `X-Opencode-Session`. The reusable Chat Completions codec
requires an explicit opt-in outside native OpenCode Go, preventing arbitrary
OpenAI-compatible endpoints from receiving the proprietary identifier. Model
catalog requests also omit it.

### Resolution evidence

Verified on the current checkout with:

- upstream OpenCode source documentation showing
  `packages/opencode/src/session/llm/request.ts` sends the session header for
  OpenCode-managed providers and the Zen handler reads it for correlation;
- agent tests proving one conversation key survives a real turn followed by
  compaction while purpose-scoped request-cache keys remain distinct;
- branch tests proving the value is opaque, stable within a branch, and rotates
  across a fork;
- OpenCode Go and Zen wire tests covering repeated values, both Zen transports,
  compatible-provider isolation, and catalog omission;
- `go test ./...`;
- `go test -race ./internal/provider/opencodego ./internal/provider/opencodezen ./internal/agent ./internal/app ./pkg/protocol ./pkg/snowsdk -count=1`;
- `go vet ./...`;
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`;
- `python3 scripts/check_benchmarks.py`;
- `git diff --check`; and
- an independent read-only review, including follow-up confirmation that
  conversation correlation is no longer split by request purpose.

## BUG-012: Jekyll output can truncate guides and break heading links

- **Status:** Resolved
- **Severity:** Medium
- **Surface:** GitHub Pages documentation rendering
- **Observed:** Documentation-site implementation and rendered-link validation

### Expected behavior

Every canonical Markdown guide staged for GitHub Pages should render completely,
and its table-of-contents and cross-document fragment links should resolve to
headings in the generated HTML.

### Actual behavior and evidence

The official GitHub Pages Jekyll image rendered the latter portion of
`docs/security.md` as literal Markdown after a multiline inline-code span placed
`<name>` at the beginning of a physical line. Kramdown treated it as raw HTML,
so every later heading disappeared from the generated document. Rendered-link
validation also found stale fragment names in the RPC and performance guides and
single-hyphen phase anchors that disagreed with Kramdown's handling of em dashes.

### Impact

- The published security guide would lose navigation and formatting after the
  MCP cache section.
- Several table-of-contents and cross-guide links would land at the top of a
  page instead of the intended section.
- Source-only relative-link checks would pass despite defects in rendered HTML.

### Reproduction

1. Stage the documentation with `scripts/build-pages.sh`.
2. Build it with the same `actions/jekyll-build-pages` image used by Pages.
3. Inspect the generated `docs/security.html` after the credential section.
4. Validate generated links and fragments.
5. Observe literal Markdown and missing targets for the affected anchors.

### Required remediation

1. Keep the MCP cache command in one inline-code span that does not expose an
   apparent HTML tag at the beginning of a Markdown line.
2. Correct stale table-of-contents and cross-guide fragments.
3. Use heading punctuation that produces the same stable anchor under GitHub
   Markdown and Kramdown.
4. Validate bounded generated HTML links, assets, schemes, and fragments before
   uploading a Pages artifact.

### Required regression coverage

Add tests proving that:

- staged source links resolve within the explicit Pages allowlist;
- site navigation targets the Jekyll output routes;
- rendered internal links and fragments resolve;
- root-relative links cannot escape the `/snow-core` Pages base path;
- unsafe URL schemes are rejected; and
- validation input count and HTML byte size are bounded.

### Remediation

The affected Markdown now avoids a physical-line `<name>` token, stale fragment
links use their rendered Kramdown anchors, and heading punctuation produces
stable GitHub and Pages IDs. `scripts/check-pages-output.py` validates generated
links, assets, fragments, base-path confinement, duplicate URL attributes,
allowed schemes, and bounded file, byte, reference, and fragment counts before
the Pages artifact is uploaded.

The Pages staging allowlist publishes only canonical documents, the standalone
Go SDK example, RPC schemas, and required references. Raw JavaScript/Python
plugin fixtures and removed language-SDK locations remain outside the artifact.
CI and the deployment workflow both use the same pinned official Jekyll action
and rendered-output validator.

### Resolution evidence

Verified on the current checkout with:

- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`;
- a successful `actions/jekyll-build-pages` v1.0.13 container render;
- `python3 scripts/check-pages-output.py ./_site --base-path /snow-core`;
- assertions that the rendered security guide retains its later headings;
- assertions that edit links map back to the real `site/` sources;
- assertions that JavaScript/Python SDK and plugin-fixture paths are absent;
- `go test ./...`;
- `go vet ./...`;
- `python3 scripts/check_benchmarks.py`;
- `git diff --check`; and
- two independent read-only Pages reviews whose actionable findings were fixed.

## BUG-013: Release installer does not persist its PATH entry

- **Status:** Resolved
- **Severity:** Low
- **Surface:** macOS/Linux release installation
- **Observed:** One-line installer usability review

### Expected behavior

After the one-line installer places `snow` in its default or configured
installation directory, a newly opened supported shell should find the binary
without requiring the user to copy a separate `export PATH=...` command.
Repeated installation must not keep appending the same configuration.

### Actual behavior and impact

The installer previously printed a PATH instruction only when the destination
was absent from the current process environment. It could not change its parent
shell, and it did not persist the directory in a startup file. Users therefore
had to run and remember a second command after installation, weakening the
intended one-line experience.

### Remediation

`scripts/install.sh` now writes one safely quoted, idempotent PATH entry to the
configured login shell's `.zshrc`, `.bashrc`, `.bash_profile`, or `.profile`.
It requires an absolute install path, rejects control characters and
PATH-delimiter colons, preserves macOS Bash profile precedence, honors absolute
Zsh `ZDOTDIR`, and warns rather than corrupting a non-regular profile target.
It supports `SNOW_NO_MODIFY_PATH=1` for users who manage PATH themselves. The
public command is a compact `curl -fsSL … | sh` bootstrap; the persisted entry
takes effect in a new shell because a child installer cannot mutate its parent
process.

### Resolution evidence

Installer regression tests cover Bash execution, Linux Bash and macOS Zsh
profiles, idempotence, single-quote escaping, invalid configuration, opt-out,
and non-regular profile targets. Shell syntax checks run under both `sh` and
`bash`, and the README, release guide, and security model describe the same
behavior.

## BUG-014: Pages publishes the repository documentation index

- **Status:** Resolved
- **Severity:** Medium
- **Surface:** Public GitHub Pages documentation
- **Observed:** Public-site review after enabling the repository Pages URL
- **Resolved:** 2026-09-03

### Expected behavior

The repository Pages URL should open an organized end-user manual that starts
with installation and a first agent prompt, groups supported agent workflows by
user task, and publishes only documentation required to use, extend, integrate,
or operate Snow safely.

### Actual behavior and impact

GitHub Pages was configured to deploy `main /docs`, so the live homepage rendered
`docs/README.md` instead of the generated landing page under `site/`. That index
exposes maintainer design history, release operations, audits, research, and
canonical repository ownership before giving users a coherent first-run path.
The custom builder also copied every tracked document plus root architecture,
contributor, changelog, workflow, and benchmark files, so switching the Pages
source alone would still publish repository internals.

Users arriving from the README cannot quickly distinguish installation and
daily agent guidance from contributor records. Internal implementation material
also becomes part of the supported-looking public navigation and artifact even
though it is not intended as a user contract.

### Reproduction

1. Open `https://elmissouri16.github.io/snow-core/` while Pages uses the
   `main /docs` branch source.
2. Observe that the first paragraph matches `docs/README.md` and describes the
   documentation directory rather than a first-use task.
3. Run `scripts/build-pages.sh` against a fresh directory.
4. Observe that the staged artifact contains all `docs/*.md` files together with
   `IMPLEMENTATION.md`, `AGENTS.md`, `CHANGELOG.md`, workflow YAML, and benchmark
   configuration.

### Remediation requirements

- Configure Pages to deploy through the existing `Documentation` GitHub Actions
  workflow rather than directly from `main /docs`.
- Add a canonical getting-started guide and task-oriented homepage/navigation.
- Replace broad staging with an explicit allowlist of public user and integration
  guides while keeping maintainer documents available only in the repository.
- Add tests that fail if internal indexes, implementation records, audits,
  research, release procedures, workflows, or benchmarks re-enter the public
  artifact.

### Verification status

Resolved by `d581369` (`docs(pages): publish curated user manual`). The focused
Pages suite, the complete 52-test support-script suite, `go test ./...`,
`go vet ./...`, the benchmark guard, the official GitHub Pages Jekyll image,
and the rendered-output validator all passed. Documentation workflow run
[`33797490365`](https://github.com/elmissouri16/snow-core/actions/runs/33797490365)
built and deployed the site successfully. Live verification confirmed the
curated homepage and getting-started guide return HTTP 200, while bug,
implementation, research, session-internals, Pages, release, benchmark, and
design-plan routes return HTTP 404.

## BUG-015: Printed Pages guides hide their headings

- **Status:** Resolved in the working tree
- **Severity:** Low
- **Surface:** GitHub Pages print and PDF output
- **Observed:** Documentation presentation audit

### Expected behavior

Printed and PDF versions of every public guide should retain a visible heading
hierarchy on the white print background.

### Actual behavior and impact

The screen stylesheet assigns `#f6faff` to `.prose h1` through `.prose h4`.
The print rules change the page background to white and paragraphs, lists, and
table cells to dark text, but do not override the explicit heading color.
Headings therefore render almost white on white in print and PDF output, making
the document structure difficult to read.

### Reproduction

1. Open the homepage or any public guide.
2. Open the browser print preview or save the page as PDF.
3. Observe that prose headings retain their near-white screen color on the
   white print background.

### Remediation requirements

- Set an explicit dark print color for `.prose h1` through `.prose h4`.
- Keep screen styling unchanged.
- Add regression coverage that checks the print block owns all four heading
  levels.

### Verification status

Resolved by the Pages documentation overhaul in this working tree. The print
stylesheet now assigns dark colors to all prose heading levels and other
screen-specific text, with light backgrounds for quotes, tables, and code.
The focused Pages tests assert the heading color and other print selectors; the
complete support-script suite, official GitHub Pages Jekyll image, and rendered
site validator pass. A headless Chrome print of the provider guide produced a
PDF whose content stream contains the expected `#111` text drawing color.

## BUG-016: Native updater rejects valid release archives

- **Status:** Resolved in the working tree
- **Severity:** High
- **Surface:** Interactive native self-update installation
- **Observed:** Installing published `v0.1.0-alpha.3` from `v0.1.0-alpha.2`

### Expected behavior

After the release archive passes checksum and strict member validation, the
native updater should validate the staged binary and atomically install it.

### Actual behavior and impact

The updater reports `release archive has trailing or invalid data` for the
valid published archive even though the release workflow and external checksum
verification accept it. The tar reader reaches the end-of-archive marker before
the gzip reader consumes and validates the remaining gzip trailer, so the
underlying-reader length check mistakes unread valid framing for appended data.
Users cannot install the release through the new native update path.

### Reproduction

1. Run the published `v0.1.0-alpha.2` binary on supported macOS/Linux hardware.
2. Check for `v0.1.0-alpha.3` and select installation, or enable automatic
   installation.
3. Observe `update: release archive has trailing or invalid data`.
4. Independently verify the same archive against `SHA256SUMS` and extract it
   successfully.

### Remediation requirements

- Drain the validated gzip member through EOF after tar reaches its end marker
  so its checksum/trailer is consumed before testing for appended bytes.
- Continue rejecting a second gzip member and arbitrary trailing bytes.
- Add regression coverage using a sufficiently large valid release archive so
  gzip buffering cannot hide unread valid trailer bytes.
- Re-run native updater, focused TUI, race, full-suite, and vet checks.

### Verification status

Resolved in the working tree by draining the single gzip member through EOF
before closing it and checking the byte reader for appended data. Regression
coverage accepts a large valid archive while continuing to reject arbitrary
trailing bytes and a second gzip member. A live test copied the installed
`v0.1.0-alpha.2` executable to a temporary directory, discovered the published
`v0.1.0-alpha.3` release, installed it through the native updater, and verified
the resulting binary reports `0.1.0-alpha.3`; the user-local executable remained
unchanged. Focused updater/config/app/RPC/protocol/TUI/CLI tests, updater/app and
TUI/RPC race tests, `go test ./...`, `go vet ./...`, the 53-test support-script
suite, the benchmark guard, the production build, and `git diff --check` pass.

## BUG-017: Updater install test flakes under host load

- **Status:** Resolved in the working tree
- **Severity:** Low
- **Surface:** Local and CI updater verification
- **Observed:** Full-suite verification on macOS after other resource-intensive
  checks

### Expected behavior

`TestInstallVerifiedReleaseAtomically` should reliably execute its tiny staged
version script during a full `go test ./...` run.

### Actual behavior and impact

The test configures a two-second service command timeout and a separate
one-second final version-check timeout. Under host load, the subprocess can miss
those test-only deadlines and report `binary version check failed: signal:
killed`. The same test passes immediately in isolation, so verification can
produce a misleading failure and require an unnecessary rerun even though the
production updater defaults to a ten-second command timeout.

### Reproduction

1. Run resource-intensive benchmark or Go verification on a loaded macOS host.
2. Run `go test ./...`.
3. Observe `TestInstallVerifiedReleaseAtomically` occasionally fail during the
   staged binary version check with `signal: killed`.
4. Run `go test ./internal/update -run
   '^TestInstallVerifiedReleaseAtomically$' -count=1` and observe it pass.

### Remediation requirements

- Give this success-path test the same ten-second command allowance as the
  production updater default.
- Keep production timeout behavior unchanged.
- Re-run the focused updater test and the full Go suite.

### Verification status

Resolved in the working tree by using the production-equivalent ten-second
allowance for both success-path version checks. Production behavior is
unchanged. The focused test passed ten consecutive runs, and `go test ./...`
passed afterward.

## BUG-018: Headless output can be silently truncated after subscriber eviction

- **Status:** Resolved in the working tree, including the RPC variant
- **Severity:** High
- **Surface:** Print, JSON, and RPC output
- **Observed:** Repository-wide reliability audit

### Expected behavior

Print and JSON commands must either deliver their complete normalized event
stream or return an explicit output failure.

### Actual behavior and impact

Both modes write directly from a bounded event-subscriber callback. If stdout
blocks longer than the subscriber deadline, the event bus evicts the callback
but does not report that eviction to the command. Later events are discarded,
and the process can return success after emitting incomplete text or JSONL.

### Reproduction

1. Run print or JSON mode with stdout connected to a consumer that stops reading.
2. Keep the output write blocked for longer than the event-subscriber timeout.
3. Resume the consumer and observe that later events are missing even though the
   command reports success.

### Remediation and regression coverage

Expose monitored subscription failure state without changing ordinary
subscriber behavior. Print and JSON modes must use it and return the eviction
error after draining events. Tests gate both output modes beyond the deadline
and require `ErrEventSubscriberEvicted`; ordinary end-to-end output and event
ordering remain covered.

### Verification status

Focused agent/CLI tests, targeted race tests, the full Go suite, vet,
support-script tests, the benchmark guard, installation, and diff checks pass.

### 2026-09-04 audit: RPC variant

The audit found `internal/rpc/main.go:33` forwarding events through an unmonitored
`Agent.Subscribe`. The one-second RPC write timeout does not cover waiting for
`Server.writeMu` (`internal/rpc/server.go:855`), whereas the one-second event
subscriber deadline covers the complete callback. Waiting behind another RPC
response and then writing an event can therefore evict the subscriber even
when every individual write succeeds within its own timeout.

A temporary `TestAuditRPCSubscriberLoss` reproduced this with two serialized
650 ms writes: one response followed by one agent event. Later text and
`turn_done` were absent, while `Server.Serve` returned nil and no write error
was recorded. This can also strand an interactive client when subsequent
permission or user-input requests are no longer forwarded. This reproduction
used local bounded output only and required no provider credentials.

RPC must monitor subscription failure, terminate active work/transport with an
explicit error on eviction, and account for writer contention when enforcing
its delivery deadline. Add coverage for overlapping responses and events whose
individual writes remain below the output timeout. The reproduction failed
against the audited implementation.

### RPC resolution evidence

RPC now uses monitored forwarding with a lifetime-scoped watcher. Eviction
wakes the server's existing write-failure shutdown path even while stdin stays
open, and final cleanup drains events and checks failure synchronously before
unsubscribing. Regression coverage exercises two contending 650 ms writes
through the real process output wrapper, requires the eviction error, and
verifies the active provider prompt is canceled and durably aborted. Normal
cleanup still delivers final text and `turn_done` and is safe to call twice.

`go test ./internal/rpc ./internal/subagent`, `go test ./...`,
`go test -race ./internal/subagent ./internal/agent ./internal/app
./internal/session ./internal/rpc ./pkg/snowsdk`, `go vet ./...`, all 56
support-script tests, and `python3 scripts/check_benchmarks.py` passed. Tests
requiring session/artifact directory access were rerun outside the filesystem
sandbox after their initial access failures.

## BUG-019: Section-specific configuration updates can lose concurrent writes

- **Status:** Resolved in the working tree
- **Severity:** High
- **Surface:** Plugin, MCP, and Agent Skill configuration management
- **Observed:** Repository-wide concurrency audit

### Expected behavior

Every configuration read-modify-write operation must serialize against other
Snow processes and apply its mutation to the latest committed file.

### Actual behavior and impact

`updateSection` reads and atomically replaces configuration without acquiring
the lock used by `config.Update`. Concurrent valid plugin, MCP, or skill changes
can therefore use stale snapshots, with the last rename silently discarding an
earlier update.

### Reproduction

Run two Snow configuration-management operations concurrently against the same
file and inspect the resulting JSON. Without serialization, only one unrelated
mutation may survive.

### Remediation and regression coverage

Use one shared process-wide and cross-process lock helper around both typed and
raw-section updates, while retaining raw unknown fields and atomic replacement.
Tests require mutation callbacks to serialize and mix section writers with
ordinary settings writers across helper processes.

### Verification status

Focused configuration tests, targeted race tests, the full Go suite, vet,
support-script tests, the benchmark guard, installation, and diff checks pass.

## BUG-020: TUI shutdown can strand a ChatGPT OAuth worker

- **Status:** Resolved in the working tree
- **Severity:** Medium
- **Surface:** Interactive ChatGPT OAuth lifecycle
- **Observed:** Repository-wide TUI lifecycle audit

### Expected behavior

Closing the TUI must cancel and join its OAuth worker without breaking the
in-session Escape flow that waits for a cancellation completion event.

### Actual behavior and impact

OAuth completion performs an unconditional send to a small progress channel.
If the channel is full after Bubble Tea exits, no consumer remains and the
worker can block forever. The model does not own or join that goroutine before
closing app resources.

### Reproduction

1. Start ChatGPT OAuth and allow progress to fill its event channel.
2. Exit the TUI before login completes.
3. Let login return and observe the worker block while sending its completion.

### Remediation and regression coverage

Give the model a lifetime cancellation function and OAuth wait group. Completion
must select between event delivery and TUI-lifetime cancellation; `Model.Close`
must cancel and join the worker before closing the app. Tests cover a full
channel on shutdown, operation cancellation in a live TUI, and close-time join.

### Verification status

Focused TUI tests, affected-area race tests, the full Go suite, vet,
support-script tests, the benchmark guard, installation, and diff checks pass.

## BUG-021: Active-session writes repeatedly rebuild prior-session search

- **Status:** Resolved in the working tree
- **Severity:** Medium
- **Surface:** Prior-session FTS search performance
- **Observed:** Repository-wide performance audit

### Expected behavior

Writes to the active session, which is excluded from `session_search` results,
must not invalidate or consume capacity in the derived prior-session corpus.
Historical-session changes and active-session switches must still invalidate it.

### Actual behavior and impact

The cache identity includes every session database, WAL, and journal. Each
active-session append can therefore force the next search to reopen and decode
the bounded historical corpus and rebuild its in-memory FTS index, even though
the active session is discarded by the final SQL predicate.

### Reproduction

1. Search prior sessions while the current SQLite session remains open.
2. Append another current-session message, changing its WAL metadata.
3. Search again and observe the derived index rebuild count increase.

### Remediation and regression coverage

Pass the active session ID and path into search, omit that database and its
sidecars from corpus selection and cache identity, and bind the cache to the
exclusion. Tests require no rebuild after an active WAL append, a rebuild after
a historical append, correct exclusion, and a rebuild after switching sessions.

### Verification status

Focused session/built-in-tool tests, affected-area race tests, the full Go
suite, vet, support-script tests, the benchmark guard, installation, and diff
checks pass.

## BUG-022: Physical session forks leak staging lock files

- **Status:** Resolved in the working tree
- **Severity:** Medium
- **Surface:** Independent session forks
- **Observed:** Repository-wide session resource audit

### Expected behavior

A successful or failed physical fork must remove every randomly named staging
file while preserving the published destination's normal lifetime lease.

### Actual behavior and impact

Fork cleanup removes the staging database and SQLite sidecars but omits the
staging `.lock` created by `NewSQLiteStore`. Every populated fork can leave an
orphan hidden file, causing unbounded directory and inode clutter over time.

### Reproduction

Create a non-empty independent session fork and list hidden files beside the
new database. A `.<destination>.tmp-*.lock` file remains.

### Remediation and regression coverage

Include `.lock` in staging cleanup and explicitly remove the successful staging
lease after publishing the database. The fork test now rejects any remaining
staging pathname while continuing to reopen and use the destination.

### Verification status

Focused session tests, affected-area race tests, the full Go suite, vet,
support-script tests, the benchmark guard, installation, and diff checks pass.

## BUG-023: Subagent provider timeouts are reported as successful completion

- **Status:** Resolved in the working tree
- **Severity:** High
- **Surface:** Subagent task lifecycle, final-result delivery, SDK/RPC/TUI status
- **Observed:** 2026-09-04 codebase audit; reproduced with a real agent loop and
  a local provider fixture

### Expected behavior

A child whose task deadline expires during provider streaming must finish as
interrupted, preserve the timeout reason, and identify any returned text as a
partial result rather than completed work.

### Actual behavior and impact

`internal/agent/streaming_tools.go` persists provider-stream cancellation as
`StopAborted`, and `internal/agent/lifecycle_run.go:594` returns nil for that
stop reason. In `internal/subagent/operations.go:628-654`, the worker only
checks an explicit interrupt flag or a non-nil returned error. It never checks
whether the task context expired before its own cleanup cancellation.

Consequently, a child that reaches `task_timeout_ms` during a provider request
is marked `completed`, with an empty error, and its last partial assistant text
is delivered to the parent as the final result. Parents and integrations can
accept unfinished implementation or verification work as successfully done.
The default task deadline is 1,800,000 ms (30 minutes).

### Reproduction

A temporary `TestAuditSubagentTimeoutStatus` used the real `agent.Agent` inside
the subagent manager with a 40 ms task deadline. Its local provider emitted
`Starting the requested work...`, then blocked until the request context
expired. `WaitAll` returned nil and the child state was:

```text
status=completed error="" result="Starting the requested work..."
```

The assertion requiring `interrupted` failed. No network or external side
effects were involved.

### Remediation and required regression coverage

Capture the task context error before calling the worker's cleanup `cancel()`
and use it when classifying completion, including when the agent returns nil.
Preserve the interruption reason in status and parent-facing final delivery.
Test real-agent deadlines both before any provider output and after partial
output, for initial prompts and mailbox follow-ups. Keep successful completion
and explicit interruption covered separately.

### Verification status

The worker now captures the task context error before cleanup cancellation,
marks expired work interrupted even when the agent returns nil, and preserves
the interruption reason. Parent-facing final delivery explicitly labels any
partial text as incomplete work.

Permanent real-agent regression tests cover deadlines in `Chat`, before the
first stream output, and after partial output, for both initial prompts and
mailbox follow-ups. They assert child status/error and parent mailbox content;
successful initial work remains completed. Existing explicit-interruption
coverage also passes. The focused package tests, full Go suite, affected-area
race checks, vet, 56 support-script tests, and benchmark guard all passed.

## BUG-024: Repository searches repeat line copies and ignore-rule preparation

- **Status:** Resolved in the working tree
- **Severity:** Performance
- **Surface:** Built-in grep and glob tools
- **Observed:** 2026-09-04 performance audit

Complete buffered grep lines were copied into a temporary byte slice and then
copied again into their returned string. Ignore checks also recompiled patterns
and rebuilt the same inherited directory-rule lists for every file. A local
10 MB / 100,000-line grep allocated about 22.47 MB; a 2,000-file glob with 60
ignore rules allocated about 19.90 MB.

The fix returns an owned string directly for complete in-limit lines and keeps
the original fragmented-line, oversized-line drain, error, and cancellation
paths. Ignore patterns are prepared once, and each search caches inherited
rules for at most 256 directories and 4,096 backing-array rule slots. Exhausting
either cache budget falls back to uncached rule assembly without dropping rules
or changing precedence. No path-guard or ignore-file opening checks change.

Regression coverage checks line boundaries, ownership after reader-buffer reuse,
oversized-line recovery, errors, cancellation, pattern semantics, both cache
limits, and fresh rules between searches. Permanent benchmarks cover the line
reader, full 10 MB grep, ignore evaluation, and full 2,000-file glob.

Verification passed: `go test ./internal/tools/builtin -count=1`,
`go test ./...`, `go test -race ./internal/tools/builtin -count=1`,
`go vet ./...`, all 56 support-script tests, and
`python3 scripts/check_benchmarks.py`. Three-sample before/after benchmarks
confirmed about 50% less allocation volume for full grep and 59% less for
full glob. The line-reading stage took 40% less time and ignore evaluation
28% less; whole-glob timing was variable. Exact commands and measurements
are recorded in `docs/performance.md` and `IMPLEMENTATION.md`. Nothing was
installed.

## BUG-025: Process-log rendering and mention matching repeat avoidable work

- **Status:** Resolved in the working tree
- **Severity:** Performance
- **Surface:** TUI process logs and file mention completion

Process-output sanitizing repeatedly grew its buffer despite knowing an output
size lower bound after normalization. Mention basename matching lowercased text
already contained in the lowercase full path. Two production-line changes
reserve that buffer and reuse the lowercase path. They preserve escaping,
UTF-8 repair, original result casing, sorting, and existing output bounds.

Benchmarks and before/after measurements are recorded in `IMPLEMENTATION.md`.
Regression tests check control escaping, CRLF, invalid UTF-8, mixed-case and
Unicode mentions, and path-prefix priority. Existing process-fleet and mention
tests cover display and interaction behavior.

Verification passed: focused TUI tests, `go test ./...`,
`go test -race ./internal/tui -count=1`, `go vet ./...`, all 56 support-script
tests, and the performance regression guard. Three-sample benchmarks confirmed
30% fewer allocated bytes for process sanitizing plus wrapping and 6–23% less
mention-matching time. No installation was performed.

## BUG-026: Context preparation copies unchanged tool results and checkpoint text

- **Status:** Resolved in the working tree
- **Severity:** Performance
- **Surface:** Historical tool-result pruning and persisted context-token checks

When one oversized tool result triggers pruning, the pruning helper also copies
single-text results that are below the threshold. Checkpoint detection similarly
copies a whole single-text message just to inspect its prefix. Two three-line
fast paths skip these copies; multi-block handling, pruning thresholds,
artifact callbacks, and defensive projection ownership retain their existing
behavior. Focused regression tests and repeatable benchmarks cover both paths.

Verification passed: `go test ./internal/compact ./internal/agent -count=1`,
`go test ./...`, `go test -race ./internal/compact ./internal/agent -count=1`,
`go vet ./...`, all 56 support-script tests, and
`python3 scripts/check_benchmarks.py`. Repeated benchmarks measured about 74%
less time and allocation volume for pruning 50 small results plus one oversized
result in an owned projection. Persisted-token lookup with a 32 KiB checkpoint
and 1,500 retained messages dropped from 11.00 to 0.752 µs and from 40,960 to
zero allocated bytes. Scope and reproduction commands are in
`IMPLEMENTATION.md`. No installation was performed.

## BUG-027: Composer Select All can retain or hide selection

- **Status:** Resolved in the working tree
- **Severity:** Low
- **Surface:** TUI composer and application-owned transcript selection

### Expected behavior

Pressing `Ctrl+A` in the ordinary composer visibly selects only its current
draft. Any earlier Snow-managed transcript drag selection and copy menu should
be cleared so selection belongs to one visible surface at a time.

### Actual behavior and impact

The composer correctly tracked the entire draft as selected, but it did not
clear existing transcript selection state. A prior app-mouse drag selection
could therefore remain highlighted alongside the composer and make Select All
appear to include transcript content.

The attempted visual treatment also changed only Bubbles' public `Text` styles.
Bubbles renders the active row with `CursorLine`, and its copied textarea kept a
private active-style pointer aimed at the original unmodified style. A typical
one-line draft therefore had working replacement semantics but no visible
selection highlight.

This does not cover terminal-native `Command+A`/`Super+A`: major terminal
emulators commonly reserve that shortcut and select their own screen before a
TUI process receives an input event. Snow's portable composer shortcut is
`Ctrl+A` on every supported OS.

### Remediation and regression coverage

Composer Select All clears app-owned transcript selection and its context menu
before selecting the draft. Rendering applies reverse video to `Text` and
`CursorLine` for focused and blurred states, then rebinds the copied textarea's
active style before rendering. Focused regressions require the transcript state
to clear, the composer value to remain intact, and a one-line active draft to
contain a reverse-video selection sequence. In-app help and the canonical TUI
guide identify `Ctrl+A` as the cross-platform shortcut and explain how a
terminal-specific `Command+A` mapping can send the same control character.

### Verification status

Verified with the focused visual-selection regression,
`go test ./internal/tui -count=1`, `go test -race ./internal/tui -count=1`,
`go test ./...`, `go vet ./...`, all 56 support-script tests,
`python3 scripts/check_benchmarks.py`, and `git diff --check`. The verified local
binary was then installed with `./scripts/install-local.sh`.

## BUG-028: Multi-command Bash permission cards are noisy and misleading

- **Status:** Resolved
- **Severity:** Medium
- **Surface:** TUI permission picker and static Bash effect presentation
- **Observed:** User-reported screenshot during the Bash permission alpha

### Expected behavior

A permission card for a compound Bash command should present a concise,
distinguishable summary of each meaningful effect. Repeated operations should
identify their command or be grouped, dynamic effects should explain their
source without occupying several identical rows, and shell-test operators must
not be displayed as filesystem paths.

### Actual behavior and impact

A create-then-delete temporary-file command displayed several indistinguishable
`execute` rows, four indistinguishable `unknown (dynamic)` rows, and a bogus
filesystem read ending in `/!`. The command source also consumed multiple wide
rows without a compact label. The analyzer treats the shell `test` builtin as
an external file-reading command and the picker omits effect command names and
reasons, making a correct approval difficult to review when multiple commands
are present.

This is presentation and conservative-analysis behavior rather than a sandbox
escape, but the noisy card obscures the operation a user is being asked to
authorize and undermines the permission system's review value.

### Reproduction

Run in `ask` mode a Bash tool invocation equivalent to:

```sh
tmp_file="${TMPDIR:-/tmp}/snow-test-$$.txt"; printf x > "$tmp_file"; \
  test -f "$tmp_file"; rm -- "$tmp_file"; test ! -e "$tmp_file"
```

### Required remediation and regression coverage

- Treat `test` and `[` as shell builtins and do not infer their predicates as
  filesystem reads.
- Give process effects command-specific labels and dynamic effects concise,
  reason-bearing labels.
- Group identical rendered effect labels with counts and enforce a compact row
  budget independent of the raw protocol effect limit.
- Bound or summarize the displayed Bash source to keep the decision controls
  prominent.
- Add analyzer and TUI regressions reproducing the reported compound command.

### Resolution and verification

Resolved by recognizing only unqualified `test` and `[` command tokens as
shell builtins, preserving external execution for qualified names and wrappers,
and attaching command names to process effects. The permission picker now
groups equivalent effects while retaining every distinct dynamic reason,
escapes layout/control bytes without collapsing path identity, and budgets
command/effect details against the actual overlay height while reserving the
decision rows and footer. Blocking permission requests own small frames; when
the terminal cannot show both the safety context and at least one Bash
command/effect detail, approval is disabled until the terminal is resized.

Verified with focused analyzer and permission-picker regressions, including the
reported compound command, qualified and wrapped `test` executables, narrow and
short default-frame layouts, truncated unknown effects at seven rows, and
control-byte paths. Also verified with `go test ./...`, `go vet ./...`,
`go test -race ./internal/tui ./internal/shellanalysis ./internal/permission
./internal/agent -count=1`, all 56 support-script tests,
`python3 scripts/check_benchmarks.py`, `go build -o ./snow ./cmd/snow`, and
`git diff --check`.

## BUG-029: Bash grep summaries include the search pattern as a file

- **Status:** Resolved; verified in the working tree
- **Severity:** Low
- **Surface:** Static Bash effect summary and permission-card resources
- **Observed:** 2026-09-05 review of Bash command classification

For `grep needle README`, the analyzer reports both `needle` and `README` as
high-confidence filesystem reads. The first ordinary operand is the search
pattern in this invocation, so the extra `needle` path is misleading. This
is separate from the resolved shell-test builtin presentation issue in BUG-028.

The shared command specification now declares pattern, pattern-file, option-value,
and file-operand roles. The generic option parser consumes short clusters,
attached values, long equals values, and `--`; the grep handler distinguishes
an ordinary pattern from file operands. Permanent regressions cover ordinary
and explicit patterns, pattern files, context-count options, multiple files,
and filenames beginning with a dash.

Verified with the focused shell regression suite, the full Go suite, affected
race tests, vet, all 56 support-script tests, and the performance guard. No
security-sensitive reproduction details are included in this public tracker.

## BUG-030: SDK example omits the shell parser module dependency

- **Status:** Resolved; verified in the working tree
- **Severity:** Low
- **Surface:** Standalone `examples/sdk` module
- **Observed:** 2026-09-05 shell-preflight verification

The root module already required `mvdan.cc/sh/v3`, but the standalone SDK
example lacked its indirect requirement and checksums. Running `go test ./...`
in `examples/sdk` failed with a missing go.sum entry for the shell syntax
package. `go mod tidy` synchronized the example's module graph with the current
checkout. Both its test/build step and `go run .` now complete successfully
with the offline fake provider and an isolated temporary Snow home.

## BUG-031: Caller deadlines can return success and restart an active goal

- **Status:** Resolved; verified 2026-09-05
- **Severity:** High
- **Surface:** Core prompt lifecycle, direct SDK prompts, active goal continuation
- **Observed:** 2026-09-05 audit of revision `4d35f52`

When the caller's context expires during provider startup or streaming,
`streamTurnWithErrors` persists an aborted assistant boundary and returns
`StopAborted` with no error. `run` then returns nil at
`internal/agent/lifecycle_run.go:593-594`. The prompt finalizer passes that nil
to `finalizeGoalTurn` without checking the caller's context. An eligible active
goal therefore calls `ContinueGoal`, whose next provider request uses a fresh
background context.

Direct SDK `Prompt` callers can interpret incomplete work as successful, and
an active goal can incur additional provider usage and execute subsequently
approved tools after the host's deadline. RPC separately checks its prompt
context when reporting completion; that status check does not fix the core
continuation decision. This is distinct from BUG-023's resolved child-worker
timeout classification.

### Reproduction and verification

A temporary Go overlay test used a real agent, a temporary SQLite session,
and a local provider that either waited for cancellation in `Chat`, or emitted
`Partial work` and then waited in `Next`. With a 40 ms caller deadline, both
ordinary prompts returned nil with `ctx.Err() == context.DeadlineExceeded`.
With a persisted active goal, both cases also started provider request number
two after the deadline. All four cases reproduced in three consecutive runs.
The second request was held until agent cleanup; no network or tools ran.

### Required remediation and regression coverage

Preserve caller cancellation/deadline outcomes before finalizing a prompt and
deciding goal continuation. Prevent canceled work from launching an automatic
turn; distinguish cancellation from a provider failure when updating durable
goal status. Cover startup and partial-stream deadlines, direct SDK errors,
active goals, queued input, explicit Abort, and successful completion. Existing
tests that intentionally accept nil for cancellation need an explicit contract
decision; changing only the subagent or RPC wrapper is insufficient.

### Verified resolution

Caller context errors are joined into the core prompt and mailbox outcomes
after pending mail is persisted. A matching active goal pauses on caller
cancellation and cannot launch automatic continuation. Pre-canceled or
canceled admission waits return without persisting input; explicit Snow Abort
retains its distinct contract. Startup and partial-stream deadlines, attached
goals, all four SDK prompt methods, and existing queue/Abort behavior pass
focused tests and the agent/SDK race suites. See `docs/sdk.md` and
`docs/goals.md` for the public cancellation contract.

## BUG-032: Checkpoint normalization repeatedly copies growing section bodies

- **Status:** Resolved; verified 2026-09-05
- **Severity:** Performance
- **Surface:** Local checkpoint normalization during compaction
- **Observed:** 2026-09-05 audit of revision `4d35f52`

`canonicalizeWorkingStateCheckpoint` in `internal/compact/planner.go:370-373`
appends each line and separator to an immutable string. A section with many
lines repeatedly copies its entire accumulated body, producing quadratic
allocation volume. The public normalization path canonicalizes provider and
local summaries, so this cost is part of real compaction processing.

A candidate with six added and three removed production lines accumulates the
current section in a `strings.Builder`, assigns its string on flush, then resets
the builder. Existing compaction tests and 1,005 differential cases passed with
the candidate, including duplicate/unknown headings, blank lines, and Unicode.

Three-sample medians on Go 1.27rc3 / macOS arm64 / Apple M3 Pro, `-cpu=1`,
`-benchtime=500ms`, measuring the full `NormalizeWorkingStateCheckpoint` call:

| Fixture | Current time | Candidate time | Current B/op | Candidate B/op |
| --- | ---: | ---: | ---: | ---: |
| 7.5 KB, 100 lines across twelve sections | 109.31 us | 92.60 us | 185,528 | 127,408 |
| 28.8 KB, 400 lines across twelve sections | 547.46 us | 284.02 us | 1,446,408 | 509,936 |
| 28.5 KB, 400 lines in one section | 2,128.51 us | 346.92 us | 12,510,000 | 669,016 |
| 114 KB, 1,600 lines in one section | 22,277.87 us | 1,242.85 us | 193,995,808 | 2,389,576 |

The large fixtures are stress cases; the default provider summary target is
2,000 tokens. These measurements isolate local normalization with no historical
messages and exclude provider latency, summary generation, and persistence.
They do not imply an equivalent speedup for an entire agent turn.

Before adopting the candidate, retain focused equivalence coverage and add a
permanent benchmark for both concentrated and distributed section bodies.

### Verified resolution

Adopted the section-local builder in `internal/compact/planner.go` with the
1,005-case equivalence test and permanent concentrated/distributed benchmarks.
The final three-sample 28.5 KB concentrated case improved from 2.121 ms to
0.343 ms and 12,510,000 to 669,016 B/op. The stress case improved 17.22x;
whole-process peak RSS fell 14%. Full measurements and limitations are in
`docs/runtime-fixes-performance.md`. The compact tests and race suite pass.

## BUG-033: Goal controls can deadlock with subagent manager tools

- **Status:** Resolved; verified 2026-09-05
- **Severity:** High
- **Surface:** Plan-mode transitions and manual compaction during automatic goals
- **Observed:** 2026-09-05 follow-up audit of revision `4d35f52`

`Agent.SetMode` acquires root admission at `internal/agent/configuration.go:212`
and holds it while `StopGoal` cancels and joins automatic work at line 254.
`Manager.List` acquires the same lock at `internal/subagent/operations.go:326`.
If a running automatic goal reaches `list_agents` after the control acquires
admission, the control waits for the turn, and the turn waits for admission.
Canceling the turn cannot interrupt this mutex wait. No child needs to exist.

Entering Plan mode has no deadline on this join, so both calls remain blocked.
Manual `Compact` uses the same lock-and-join pattern at
`internal/agent/session_context.go:771-789`; a caller deadline releases that
control with an error, but a background context permits an indefinite hang.
Branch selection and forking also call `stopAutomaticForControl` while holding
admission; these share the source-level risk but were not separately reproduced.

### Reproduction and verification

A temporary Go overlay used a real agent, SQLite goal, subagent manager, and
the actual `list_agents` tool with a local fake provider. A scheduling gate
paused immediately before delegating to the real manager tool, then resumed
when the control canceled the turn. This forces the relevant legal interleaving
without adding a lock to the tool or creating a child.

The Plan transition hit a three-second test timeout. Its goroutine dump shows
`SetMode -> StopGoal -> stopWork` waiting for completion and the goal's
`managerTool.Run -> Manager.List -> LockAdmission` waiting for the mutex.
Manual compaction hit its 150 ms caller deadline in all three race-enabled
runs and unwound after releasing admission. No data race was reported; this
is a lock dependency cycle.

### Required remediation and regression coverage

Do not join running work while holding an admission lock that its tools need.
Separate cancellation/join from the admitted state transition, then reacquire
admission and revalidate the target state. Preserve transition guards and
prevent another turn from entering the gap. A context check before mutex
acquisition alone does not eliminate the race.

Cover Plan transitions and manual compaction racing with real manager tools,
plus branch/fork controls and concurrent prompt admission. Include no-child
fixtures and bounded completion assertions.

### Verified resolution

The initial proposal above was replaced by a smaller fix that retains atomic
control transactions: admission now supports cancellation while waiting, and
all context-bearing manager operations use it. Canceled tools leave the queue
so the control can finish joining the turn without releasing its transition
guards. Prompt and manual compaction admission also honor caller cancellation.
Permanent race-enabled tests force the original real-manager interleaving for
Plan mode, compaction, branch selection, fork, and replacement prompt, with
bounded completion. A separate test cancels an already-waiting admission.
The full internal race suite and final affected-area race checks pass.

## BUG-034: Compaction usage is missing from session and goal accounting

- **Status:** Resolved; verified 2026-09-05
- **Severity:** Medium
- **Surface:** Provider-backed compaction, usage/cost totals, automatic goal budgets
- **Observed:** 2026-09-05 follow-up audit of revision `4d35f52`

`readCompactionSummary` at `internal/agent/compaction.go:466-468` handles
`EvStreamUsage` only by setting an activity flag. `summarizeForCompaction`
returns summary text without accounting for the provider's reported tokens or
cost. Applying the checkpoint does not persist that usage either. Session
totals and automatic goal accounting therefore omit these provider requests.

This undercounts work as well as displayed cost: an automatic goal can continue
without recognizing that compaction crossed its token budget. The defect is
the discarded usage, not the ordinary possibility that one in-flight request
overshoots a budget. Repeated compactions can repeatedly escape accounting.

### Reproduction and verification

A temporary overlay extended the existing automatic-compaction fixture with
a 150-token goal budget and explicit local provider usage events. The ordinary
request reported 95 tokens and USD 0.10; the summary request reported another
100 tokens and USD 0.20. The fixture verified that request two was the actual
checkpoint request, then observed further normal goal work and completion.

In all three race-enabled runs, both the persisted goal and SQLite session
aggregate reported only 95 tokens and USD 0.10, despite the provider reporting
195 tokens and USD 0.30. The goal completed without recognizing the budget
crossing. Assertions for full usage and cost failed consistently; no data race
was reported. No network requests or real provider charges were involved.

### Required remediation and regression coverage

Collect provider summary usage, persist it in branch accounting, and attribute
automatic compaction to its owning goal before deciding further continuation.
Keep this separate from conversational context-occupancy measurements. Preserve
goal identity across between-turn compaction; simply calling a turn-accounting
helper may not have the correct active-turn attribution there.

Cover manual and automatic compaction, within-turn and between-turn boundaries,
budget crossings, reopen/branch aggregation, and reported usage on failed or
retried summaries. Avoid double counting cumulative usage events or adding
synthetic conversational turns solely for accounting.

### Verified resolution

Compaction captures the last cumulative usage snapshot once per provider
attempt, including reported usage on failed attempts, and appends branch-local
`provider_usage_v1` metadata. Memory and SQLite aggregates include it without
creating conversation turns or context-occupancy events. Automatic compaction
charges the admitted goal, including between-turn work; crossing a budget stops
normal continuation and invokes the existing budget-completion path. Manual
compaction charges only the session. Accounting failures remain fatal instead
of being hidden by local fallback and emit a terminal compaction error event.
Permanent tests verify 195 tokens / USD 0.30, cumulative snapshots, both
automatic boundaries, manual retries, budget status, forks, reopen, and failure
handling. Full tests and agent/session race checks pass. See `docs/goals.md`
and `docs/session-storage-internals.md`.

## BUG-035: Full process log buffers shift retained output on every small write

- **Status:** Resolved; verified 2026-09-05
- **Severity:** Performance
- **Surface:** Managed subprocess stdout/stderr capture
- **Observed:** 2026-09-05 performance audit of revision `4d35f52`

Once the retained log buffer is full, `outputRing.Write` at
`internal/process/output.go:37-38` shifts its surviving contents for every
write smaller than the retention cap. With the default 1 MiB cap, a 4 KiB
write copies approximately 1 MiB while holding the output mutex.

A one-file candidate with 12 added and two removed lines advances the data
slice and occasionally compacts it into reusable storage with 25% spare
capacity. Reads, cursors, notification, and the retained-byte cap remain
unchanged. This trades 256 KiB of reserved capacity per full default buffer
for far fewer copies; it is not a free memory optimization.

Three-sample median write times on Go 1.27rc3 / macOS arm64 / Apple M3 Pro,
with a full 1 MiB buffer, `-cpu=1 -benchtime=300ms`:

| Incoming chunk | Current | Candidate | Speedup |
| --- | ---: | ---: | ---: |
| 64 bytes | 23.647 us | 0.070 us | 338x |
| 4 KiB | 25.049 us | 0.599 us | 41.8x |
| 32 KiB | 24.943 us | 3.742 us | 6.7x |

These are steady-state capture-helper measurements, not subprocess or agent
turn speedups. At a smaller 64 KiB cap with 32 KiB writes the candidate showed
no gain, so retention/chunk size matters. Per-write notification still costs
one allocation; the candidate does not address it.

A separate local benchmark captured 32 MiB from `head -c 33554432 /dev/zero`
through `os/exec` stdout/stderr wired to the output ring, as in the runtime.
Including subprocess startup, pipe transfer, buffer growth, and cleanup,
three-sample median time fell from 47.679 ms to 26.744 ms (44% less time).
Total allocated bytes increased from 5,577,096 to 6,901,426 for that capture,
including the one-time reusable-storage allocation. This isolates output
capture; it does not predict the speedup of a build or agent turn.

The candidate passed the complete process package race suite and a randomized
5,500-write oracle across five capacities, including zero-length writes,
oversized writes, byte contents, cursors, full reads, and notifications.
Permanent regressions and a benchmark should accompany adoption.

### Verified resolution

Adopted reusable sliding storage with permanent write, cursor, notification,
and repeated-compaction regressions. Final three-sample full-buffer 4 KiB
writes improved 44.34x. Complete 32 MiB local subprocess capture took 41% less
time (45.27 to 26.56 ms), with 24% more total allocated bytes. The live heap
probe confirms about 256 KiB additional memory per full default buffer;
whole-process capture peak RSS rose 8%. This is an explicit speed/memory
tradeoff. The full process suite and race checks pass. Raw evidence and
reproduction are in `docs/runtime-fixes-performance.md`.

## BUG-036: Terminal sanitization allocates copies for already-safe text

- **Status:** Resolved; verified 2026-09-05
- **Severity:** Performance
- **Surface:** TUI text/thinking/plan deltas, labels, and bounded previews
- **Observed:** 2026-09-05 performance audit of revision `4d35f52`

`sanitizeTerminalTextLimit` at `internal/tui/tools_info.go:226` always builds a
new string for nonempty output. Ordinary text needs no transformation. A
five-line fast path returns the original string only when it fits the byte
limit and contains neither a disallowed control nor a replacement rune.
All other input uses the existing sanitizer, including malformed UTF-8.

Three-sample medians, `-cpu=1 -benchtime=200ms`, on the same machine as BUG-035:

| Input | Current | Candidate | Current B/op | Candidate B/op |
| --- | ---: | ---: | ---: | ---: |
| 20-byte ordinary delta | 148.8 ns | 35.1 ns | 24 | 0 |
| 4,400-byte ordinary text | 26.667 us | 8.153 us | 4,864 | 0 |
| 3,900-byte Unicode text | 21.982 us | 6.845 us | 4,096 | 0 |

Control-heavy input remained effectively unchanged. These measurements cover
sanitization, not provider latency or complete TUI rendering. This is separate
from the already-adopted process-output builder reservation in BUG-025.

The candidate and BUG-037 together passed the full TUI race suite and byte-for-
byte differential checks over 10,010 inputs at eight limits, plus rendered
preview/diff cases. Keep control removal, Unicode behavior, and byte-limit
regressions when adopting the fast path.

### Verified resolution

Adopted the safe-text return with permanent differential and benchmark
coverage. Final three-sample safe 4,400-byte text improved 3.04x and eliminated
4,864 B/op; 20-byte deltas improved 3.60x with zero allocation. Control-heavy
input measured 7% slower with unchanged allocation volume, which is retained
in the report rather than treated as a gain. Full TUI tests and race checks
pass. See `docs/runtime-fixes-performance.md` for all results and scope.

## BUG-037: Short display previews decode entire long strings into runes

- **Status:** Resolved; verified 2026-09-05
- **Severity:** Performance
- **Surface:** TUI rune-limited labels, tool previews, and subagent summaries
- **Observed:** 2026-09-05 performance audit of revision `4d35f52`

`truncateRunes` at `internal/tui/view.go:928` converts the complete input to a
rune slice before retaining a short prefix. A candidate with 11 added and
seven removed lines stops scanning when truncation is established and converts
only the needed prefix. It preserves the existing ellipsis, one-rune-limit,
short-string, and malformed-UTF-8 behavior.

Three-sample medians for a 120-rune limit, using the BUG-036 benchmark settings:

| Input | Current | Candidate | Current B/op | Candidate B/op |
| --- | ---: | ---: | ---: | ---: |
| 4 KiB ASCII | 9.986 us | 0.990 us | 16,640 | 736 |
| 128 KiB ASCII | 290.594 us | 0.984 us | 524,544 | 736 |
| Approximately 128 KiB Unicode | 300.705 us | 2.239 us | 180,992 | 1,248 |

The large ratios isolate truncation of long inputs; short strings improved
only from 43.1 ns to 26.5 ns. Combining this candidate with BUG-036 reduced
the actual `renderToolOutputPreview` benchmark for 45 ordinary result lines
at width 120 from 31.046 us to 12.417 us, and from 7,856 to 3,920 B/op.
The complete TUI race suite and the differential checks described in BUG-036
passed with both candidates. Add permanent bounded-prefix coverage on adoption.

### Verified resolution

Adopted bounded-prefix scanning with permanent original-behavior parity
coverage. Final three-sample 4 KiB truncation improved 9.58x and reduced
16,640 to 736 B/op. With BUG-036, an ordinary 45-line rendered tool preview
improved 2.30x and reduced 7,856 to 3,920 B/op. Full TUI tests and race checks
pass. Larger helper-only ratios, raw samples, and reproduction commands are
recorded in `docs/runtime-fixes-performance.md`.

## BUG-038: Concurrent keybinding lock creation intermittently fails on macOS

- **Status:** Resolved; verified 2026-09-05
- **Severity:** Medium
- **Surface:** Cross-process keybinding updates on macOS
- **Observed:** 2026-09-05, exact-commit CI run 33970958608

The macOS release gate failed in `TestUpdateKeybindingsSerializesAcrossProcesses`.
Locally, the existing test failed in four of 100 repetitions. A diagnostic test
overlay that retained helper output failed eight of 100 repetitions and exposed
`openat keybindings.yaml.lock: no such file or directory` during simultaneous
nonexclusive `O_CREATE` opens. Early fatal cleanup also caused secondary errors
in helpers that were still running, obscuring the original failure.

A temporary candidate uses exclusive lock creation, reopening the winner's
existing lock on `ErrExist`. All pinned-root, non-symlink, regular-file, inode,
permission, and flock checks remain. It passed 1,000 consecutive repetitions
of the original cross-process test on the same host. The permanent regression
now tries 16 fresh locks, captures helper failures, and joins every helper
before temporary-directory cleanup.

### Verified resolution

Adopted exclusive creation with an existing-file reopen. The strengthened
keybinding tests passed 20 repetitions, followed by the complete config race
suite, full Go suite, vet, all 56 support-script tests, and the unchanged
performance guard. The release must use a new exact-commit CI run; the failed
original run is not accepted as release evidence.

## BUG-039: Retired plugin support remains executable and documented

- **Status:** Resolved
- **Surface:** Plugin runtime, CLI, configuration, SDK, and documentation
- **Evidence:** SDK removal in `e2256fb` retained language-specific examples and
  a generic external-process host. Enabled `plugins` declarations, `--plugin`,
  and SDK `Plugins` options could still launch an interpreter or executable.
- **Expected:** Snow plugins use only the in-process Go interface.
- **Reproduction:** Before this fix, configure an enabled plugin with a command
  that writes a marker file. Starting Snow executes it before the protocol
  handshake, including from trusted project configuration.
- **Fix:** Removed the subprocess host, external registration path, CLI plugin
  commands/flag, config management APIs, public external types, and examples.
  Legacy `plugins` keys are ignored. Go plugin registration, tool execution,
  events, and lifecycle remain supported. Guides now document Go plugins;
  obsolete protocol and language research pages point to historical revisions.
- **Verification:** Regression tests cover ignored global and trusted-project
  declarations, invalid legacy declarations, rejected CLI entry points, and
  disabling supplied Go plugins. `go test ./...`, `go vet ./...`, affected-area
  race tests, the Go SDK example, all 56 support-script tests, and
  `python3 scripts/check_benchmarks.py` pass. Jekyll builds successfully and
  `scripts/check-pages-output.py` validates the rendered site.

## BUG-040: Documentation links target renamed sections

- **Status:** Resolved
- **Surface:** RPC/SDK diagnostic references and subagent design notes
- **Evidence:** Sixteen links targeted headings no longer present in the
  security and subagent guides, including `security.md#diagnostic-dumps`.
- **Fix:** Updated the links to the current sections.
- **Verified:** Checked relative file links and heading anchors across all 57
  repository Markdown files; no broken relative links remain.

## BUG-041: Exhausted goal budgets do not stop substantive tool work

- **Status:** Resolved; verified 2026-09-06
- **Severity:** High
- **Surface:** Agent tool-result chaining and budget completion
- **Expected:** After accounting exhausts a goal budget, stop substantive work
  at the next safe boundary and permit only a bounded completion report.
- **Actual:** `accountGoalUsage` sets `budgetWrap`, but `run` still dispatches
  pending tools and exposes the ordinary tool schemas on subsequent requests.
  Budget steering is only model instruction; the same tool-result chain can
  continue after the persisted goal becomes `budget_limited`.
- **Reproduction:** Create a saved goal with budget 1. Have a mock provider
  report 1 token and request a work tool, then report 10 tokens and request
  another work tool, then return a final response using 10 tokens. Both tools
  execute and the goal records 21 tokens. This includes a fresh tool request
  made after exhaustion, beyond any unavoidable in-flight response overshoot.
- **Impact:** A model that continues requesting tools can keep performing work
  and consuming provider usage beyond the user-selected goal budget.
- **Required remediation:** Enforce budget completion in the runtime before
  dispatch and request admission; preserve tool-call/result pairing with
  explicit non-execution results and bound the final reporting path.
- **Required regression coverage:** Budget crossing on tool-use responses,
  new tool requests after crossing, tool requests during the final wrap turn,
  and crossing during automatic compaction.
- **Verification:** Temporary Go overlay test
  `TestAuditGoalBudgetStopsSubstantiveTools` reproduced two post-exhaustion
  work-tool executions and 21 charged tokens against a budget of 1.

- **Resolution:** Budget completion now removes provider tool schemas, rejects
  pending and unsolicited report-time calls with paired results, and permits
  at most one report attempt without retries or tool-result chaining. Budget
  stops preserve queued input. Permanent regressions cover crossing batches,
  report tools, report failure, direct user work afterward, and automatic
  compaction both within and between goal turns.

## BUG-042: Goals created during a prompt miss subsequent turn usage

- **Status:** Resolved; verified 2026-09-06
- **Severity:** High
- **Surface:** Model-facing create_goal and admitted-turn usage ownership
- **Expected:** Once `create_goal` succeeds, subsequent provider requests
  working on that goal charge its usage and enforce its budget.
- **Actual:** `prompt` captures `goalAtTurn` only at admission. A successful
  `create_goal` does not bind the newly created goal to the current turn.
  `accountGoalUsage` and final duration accounting therefore skip that turn,
  even though subsequent requests receive the active goal objective.
- **Reproduction:** Start a prompt without an existing goal. Have the provider
  call `create_goal` with budget 1, then issue two responses consuming 10
  tokens each. Inspect the goal at the end of that admitted turn, before
  subsequent autonomous work: it remains active with zero tokens used.
- **Impact:** An entire potentially long tool chain can work on a new goal
  without charging tokens, elapsed time, or cost; its budget cannot stop it.
- **Required remediation:** Bind usage ownership at successful goal creation
  with a precise accounting baseline, retaining identity checks so replacing
  goals never charges old work to a new goal.
- **Required regression coverage:** Creation during a user turn, later usage
  and duration, budget crossing, completion in the same turn, and replacement
  after completion without stale accounting.
- **Verification:** Temporary Go overlay test
  `TestAuditGoalCreatedDuringPromptAccountsFollowingWork` reproduced zero
  charged tokens after 20 post-creation provider tokens. The probe suppresses
  subsequent automatic admission to inspect only the affected user turn.

- **Resolution:** The agent identifies its controller's actual built-in
  creation tool, flushes the previous owner's elapsed usage before creation,
  and binds the successful new goal before subsequent tool/provider work.
  Failed creation keeps the existing owner. Permanent regressions cover
  same-turn completion, cost and elapsed accounting, budget crossing, and
  replacing a completed goal without charging its usage to the new goal.

## BUG-043: Repeated blocker text bypasses the goal non-progress guard

- **Status:** Resolved; verified 2026-09-06
- **Severity:** Medium
- **Surface:** Automatic goal progress detection
- **Expected:** Repeated unchanged blocker-only responses should eventually
  pause automatic continuation instead of consuming usage indefinitely.
- **Actual:** Any non-whitespace text delta sets `turnProgress=true`, and any
  successful tool result does likewise. The three-turn guard detects empty
  output, not repeated non-progress. Every new goal turn resets this flag.
- **Reproduction:** Use a mock provider that repeatedly returns only
  `I am still waiting for credentials.` and a normal stop event. Six automatic
  internal turns leave the goal active; each response resets the empty count.
- **Impact:** A goal without a token budget can continue requesting responses
  to the same blocker until user intervention or another runtime/provider
  limit. The guide's repeated non-progress guarantee is too broad.
- **Required remediation:** Track repeated terminal output or another bounded,
  conservative non-progress signal across goal turns; pause with an honest
  diagnostic rather than claiming a model-audited blocked condition.
- **Required regression coverage:** Repeated identical blocker text, repeated
  goal-status reads, distinct productive turns, and explicit resume resetting
  the audit.
- **Verification:** Temporary Go overlay test
  `TestAuditRepeatedNonProgressTextPausesGoal` reproduced an active goal after
  six identical blocker-only responses.
- **Resolution:** A bounded SHA-256 fingerprint compares normalized response
  text across automatic goal turns. Three identical responses with no other
  successful tool work (including status-read-only turns) durably pause the
  goal. Distinct responses, successful non-status tool work, and explicit
  resume reset the repetition streak. Permanent tests cover these cases and
  differing stream chunks and whitespace.

### Goal-fix verification (BUG-041 through BUG-043)

All checks passed on 2026-09-06 with isolated temporary `SNOW_HOME` and
`GOCACHE` directories:

- `go test ./internal/agent ./internal/goal ./internal/session ./internal/app ./internal/rpc ./pkg/snowsdk -count=1`;
- `go test ./...` and `go vet ./...`;
- `go test -race ./internal/subagent ./internal/agent ./internal/app ./internal/goal ./internal/session ./internal/rpc ./pkg/snowsdk`;
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v` (56 tests);
- `python3 scripts/check_benchmarks.py`;
- standalone `examples/sdk`: `go test ./...` and `go run .` with the fake provider;
- `git diff --check`;
- `./scripts/install-local.sh` installed `~/.local/bin/snow` as `0.1.0-dev`;
- installed CLI: `snow --provider fake --no-session -p "Goal fixes lifecycle smoke test"`.

## BUG-044: Edit silently overwrites a concurrent file save

- **Status:** Resolved; verified 2026-09-06 (external-writer limitation below)
- **Severity:** High
- **Surface:** Built-in Edit and rooted atomic replacement
- **Expected:** An edit must preserve unrelated changes made after it opens the
  file, or report a conflict without replacing the newer contents.
- **Actual:** Edit reads its pinned file descriptor and computes a replacement,
  then `atomicReplaceRooted` renames the staged result over the destination
  without checking that the destination still represents the original content.
  An intervening editor save that atomically replaces the file leaves the old
  descriptor readable, so Snow silently replaces the newer file with stale data.
- **Reproduction:** Start with `target before\nbaseline\n`. In the tool host's
  first progress callback (after Edit opens the file), atomically save
  `target before\nnew user work\n` at the same path. Let Edit replace
  `target before` with `target after`. The tool reports success, but the final
  file is `target after\nbaseline\n`; the newer user work is lost.
- **Impact:** Concurrent editor saves or other writers can lose unrelated work
  even when the requested edit matches exactly once. Atomic replacement prevents
  partially written files but does not prevent this lost update.
- **Required remediation:** Serialize Snow's edits to a shared target and
  validate the source identity and contents before replacement; reject detected
  concurrent changes. Account explicitly for external writers and the remaining
  check-to-rename race instead of treating a process-local lock as sufficient.
- **Required regression coverage:** Concurrent atomic saves, in-place writes,
  overlapping Snow edits, and conflict handling that preserves the newer file.
- **Verification:** A temporary Go overlay test,
  `TestAuditEditPreservesConcurrentAtomicSave`, fails deterministically with
  `tool IsError=false` and the stale final contents above. The fixture uses only
  temporary files and a synchronous host callback to control the interleaving.
  Reproduce in this audit workspace with
  `go test -overlay=/private/tmp/snow-critical-audit-probes/overlay.json ./internal/tools/builtin -run TestAuditEditPreservesConcurrentAtomicSave -count=1`.
  The existing full internal-package and SDK race suite passes, demonstrating
  that those tests do not currently cover this filesystem lost-update case.
- **Resolution:** Built-in Edit and Write now share a cancelable process-wide
  mutation gate, including across tool instances and path aliases. Edit retains
  its original file metadata and bounded contents, then validates identity,
  metadata, and exact bytes through the pinned root immediately before rename.
  Detected conflicts preserve the newer file and remove the staged replacement.
  Write retains its intentional full-content overwrite behavior.
- **Remaining limitation:** External editors, shell commands, plugins, and
  other processes do not use the gate. Changes after final validation but before
  rename remain possible because portable replacement has no filesystem
  compare-and-swap operation. This limit is documented in `docs/security.md`.
- **Fix verification:** Permanent tests in `edit_conflict_test.go` pass for
  atomic saves (including identical contents on a new inode), in-place changes,
  deletion, changed contents with restored size/mtime, overlapping Edit/Write
  cancellation, and twelve concurrent edits preserving every change.
  `go test ./internal/tools/builtin -count=1`, `go test ./...`, `go vet ./...`,
  and `go test -race ./internal/tools/builtin ./internal/subagent ./internal/agent ./internal/app ./internal/session ./internal/rpc ./pkg/snowsdk`
  passed. All 56 support-script tests, `python3 scripts/check_benchmarks.py`,
  and `git diff --check` passed as well.
  `./scripts/install-local.sh` installed `~/.local/bin/snow` as `0.1.0-dev`;
  an isolated fake-provider CLI smoke check exited successfully with permissions
  denied and sessions, plugins, MCP, skills, subagents, and debug disabled.

## BUG-045: Subagent waits report completion before accepted follow-ups run

- **Status:** Resolved; verified 2026-09-06
- **Severity:** High
- **Surface:** Subagent WaitAll, WaitUntilAll, and wait-result activity counts;
  CLI and SDK one-shot completion
- **Expected:** A child with accepted, unprocessed follow-up work must keep an
  all-terminal wait pending until that work completes or is interrupted.
- **Actual:** The worker commits and publishes a terminal status for its first
  task before consuming an already queued follow-up. WaitAll and waitResult
  inspect lifecycle status and finalization but omit queued tasks and
  followupQueued. During that interval they report completion even though
  HasActive correctly reports outstanding work.
- **Reproduction:** Start a blocked child, submit a follow-up while it runs,
  then allow its first task to finish. Pause delivery of its first completed
  status to hold the worker before the next task. WaitAll returns nil and
  WaitUntilAll returns AllTerminal=true while the child's follow-up remains
  unread and has not executed.
- **Impact:** Callers can proceed using incomplete results. CLI and SDK
  one-shot helpers use WaitAll through WaitSubagentsIdle; premature return can
  let their normal shutdown cancel accepted follow-up work.
- **Required remediation:** Make wait predicates and activity counts use a
  consistent, atomic view of pending, executing, and finalizing work. Cover
  tasks already dequeued but waiting for a slot as well as queued follow-ups;
  signal activity whenever the final outstanding work becomes idle.
- **Required regression coverage:** Follow-up arrival during a running task,
  the terminal-to-next-task interval, slot waits, already-consumed mail,
  recursive wait scope, and CLI/SDK completion waiting for accepted work.
- **Verification:** Temporary Go overlay test
  `TestAuditWaitIncludesAcceptedFollowup` reproduced
  `WaitAll=<nil> AllTerminal=true HasActive=true pending=true followups=0`.
  The test uses a mock child and a synchronous event callback to control the
  worker interleaving without modifying production source.
- **Resolution:** Accepted tasks are counted until completion or skipping,
  including dequeue, slot waits, and finalization. WaitAll, wait-result counts,
  idle-tree checks, closure, and eviction now share the same active-work
  predicate. Wait results read the task state under one runtime lock. Permanent
  regressions cover pending follow-ups between turns, dequeued follow-ups,
  and already-consumed mail whose skipped task must wake waiters.

## BUG-046: Interrupting a queued child leaves stale active-work state

- **Status:** Resolved; verified 2026-09-06
- **Severity:** Medium
- **Surface:** Subagent interruption while waiting for an execution slot
- **Expected:** Once a queued task is interrupted and skipped, its runtime
  should become idle and permit closing the child or switching sessions.
- **Actual:** After acquiring a slot, the worker assigns r.cancel and checks
  skipQueued. The interrupted branch cancels its context and releases the slot
  but fails to clear r.cancel. The worker returns to waiting for another task,
  while active-work checks continue treating that stale callback as live work.
- **Reproduction:** Fill the execution capacity, spawn a child, wait until its
  worker is blocked acquiring a slot, interrupt it, then release capacity.
  After the skip completes, CloseAgent rejects the child as still active and
  HasActive remains true despite no child task running.
- **Impact:** The completed interruption blocks child closure and App session
  or branch transitions that require an idle subagent tree. The stale state
  persists while the child stays idle.
- **Required remediation:** Clear all per-task active state on every worker
  skip/cancellation path and notify activity observers when the task settles.
- **Required regression coverage:** Interruption before dequeue, while waiting
  for a slot, and after slot acquisition; subsequent close, session switching,
  and child reuse must observe a consistent idle state.
- **Verification:** Temporary Go overlay test
  `TestAuditInterruptedSlotWaitReleasesActiveState` observed the actual worker
  blocked at its slot select, then reproduced
  `close=subagents: agent /root/queued still has active work active=true`.
  Slot saturation is simulated locally; no provider or external process runs.
- **Resolution:** Every worker task exit uses common settlement to release its
  accepted-task count, clear its cancellation and interruption state, and wake
  activity observers. Permanent synchronized regressions interrupt before
  dequeue, before slot acquisition, and after acquisition; each verifies idle
  state, child closure, follow-up reuse, and a subsequent session switch.

Both second-scan probes are available in this audit workspace via
`go test -overlay=/private/tmp/snow-second-audit/overlay.json ./internal/subagent -run TestAudit -count=1`.
These historical overlays capture pre-fix source layout; permanent regressions
now live in `task_activity_test.go` and `worker_cancellation_test.go`.

### Subagent-fix verification (BUG-045 and BUG-046)

Verified 2026-09-06 with isolated temporary `SNOW_HOME` and `GOCACHE`:

- `go test ./internal/subagent -count=1` and `go test -race ./internal/subagent -count=1`;
- `go test ./...` and `go vet ./...`;
- `go test -race ./internal/agent ./internal/app ./internal/session ./internal/rpc ./pkg/snowsdk`;
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v` (56 tests);
- `python3 scripts/check_benchmarks.py`;
- standalone `examples/sdk`: `go test ./...` and `go run .` with the fake provider;
- `git diff --check`.
- `./scripts/install-local.sh` installed `~/.local/bin/snow` as `0.1.0-dev`;
  the installed CLI passed an isolated fake-provider smoke check with sessions
  and extensions disabled and permissions denied.

## BUG-047: Promise settlement cancels an active CPU watchdog

- **Status:** Resolved; verified 2026-09-07
- **Surface:** JavaScript API 2 commands with immediately resolving host promises
- **Actual:** Publishing a result from a Goja promise callback let the caller
  cancel its invocation before the worker detached the CPU watchdog. A successful
  command could disable the runtime with `context canceled` on its next call.
- **Resolution:** Queue settlements on the owner and publish them only after
  `execute` completes and detaches the watchdog.
- **Verification:** `TestPromiseSettlementDoesNotDisableRuntime` performs 200
  immediate host-promise commands; it and the app command tests passed with
  `go test -race ./internal/plugin/javascript ./internal/app -run
  'TestPromiseSettlement|TestPluginCommand' -count=10`.

## BUG-048: Workflow cancellation closes children before task settlement

- **Status:** Resolved; verified 2026-09-07
- **Surface:** JavaScript API 2 command-owned reviewers
- **Actual:** Interrupt requests returned before manager task accounting settled.
  An immediate close failed with active work and left a command-owned child open.
- **Resolution:** Cancel only the command's recorded children, then retry their
  close within a bounded cleanup context until accepted work settles. Cleanup
  errors are returned, and unrelated children remain untouched.
- **Verification:** `TestPluginCommandCancellationClosesOnlyOwnedChildren` passed
  ten race-enabled repetitions with an unrelated completed child in the session.

## BUG-049: Typed plugin forms reject valid numeric answers

- **Status:** Resolved; verified 2026-09-07
- **Surface:** JavaScript API 2 numeric/boolean form values
- **Actual:** Decoding into an interface already containing the answer string
  caused encoding/json/v2 to retain that concrete type and reject a numeric
  answer such as `7`.
- **Resolution:** Decode into a fresh interface, then validate the declared field
  type before returning values to JavaScript.
- **Verification:** `TestPluginFormUsesExistingBrokerAndValidatesTypes` exercises
  the real input broker and receives `number:7`; five race-enabled repetitions
  passed alongside branch-transition and persisted-tool-card tests.

## BUG-050: Provider adapters reject plugin request context attribution

- **Status:** Resolved; verified 2026-09-07
- **Surface:** API 2 `before_request` hooks with OpenAI-compatible and other
  providers that validate `InternalContextFragment`
- **Actual:** The manager emitted `plugin:<id>` sources, but the protocol permits
  only lowercase letters, digits, underscores, and hyphens. Any nonempty plugin
  request context failed before the provider request; fake-provider tests missed it.
- **Reproduction:** The extension pack smoke test runs `#review hook-read-fixture`
  through `session-pilot:run` against a local OpenAI-compatible mock and receives
  `protocol: invalid internal context source "plugin:project-helper-v2"`.
- **Remediation:** Use `plugin-<id>` attribution and validate contributed fragments
  at the hook boundary; preserve the original plugin ID in transform audit records.
- **Regression:** `TestHookContextSatisfiesProviderContract` and
  `python3 examples/plugins/extensions_smoke.py`.

## BUG-051: Configuration rejects explicit child plugin tool allowlists

- **Status:** Resolved; verified 2026-09-07
- **Surface:** API 2 selected child tools with a restricted role
- **Actual:** Spawn admission correctly requires the selected plugin tool in
  the role allowlist, but configuration validation only accepted built-in tool
  names, making such a role impossible to configure.
- **Remediation:** Accept syntactically valid canonical plugin tool names in
  role configuration. Loaded-catalog selection still enforces exact role names,
  child opt-in, fingerprints, and each required host tool; no wildcard grants.
- **Regression:** `TestChildPluginToolRoleConfiguration` and the source scout
  case in `python3 examples/plugins/extensions_smoke.py`.

## BUG-052: Plugin dialogs leave the TUI showing an active agent turn

- **Status:** Resolved; verified 2026-09-07
- **Surface:** Native input/select/form dialogs opened by an idle plugin command
- **Actual:** `startUserInput` unconditionally set the root `busy` flag. Plugin
  completion emits no root `turn_done`, leaving an idle agent displayed as
  thinking and subsequent input treated as guidance until aborted.
- **Reproduction:** Open UI Studio, select Ocean in its theme dialog, then close
  the screen. The fake-provider TUI remains “working” with zero actual turns.
- **Remediation:** Keep modal activity in `userInputPending`; preserve the root
  busy state owned by correlated turn lifecycle events.
- **Regression:** `TestPluginDialogDoesNotChangeAgentBusyState` plus an interactive
  theme-selection smoke check.

### Extension pack verification (BUG-050 through BUG-052)

- `go test ./...` and `go vet ./...` passed.
- `go test -race ./internal/config ./internal/plugin/... ./internal/tui` passed.
- `examples/plugins/extensions_smoke.py` passed with fake and local mocked
  providers, including actual selected child tool execution.
- The rebuilt TUI returned to idle after selecting Ocean through the native
  plugin dialog; no root prompt was started.

## BUG-053: Plugin UI changes clip terminal chrome and offset mouse targets

- **Status:** Resolved; verified 2026-09-07
- **Surface:** TUI plugin headers, footers, above-input views, and sidebars
- **Actual:** Updating an existing footer from one to two rows renders a
  25-row composition in a 24-row terminal until the deferred refresh. In short
  terminals, plugin contributions exceed the remaining row budget and clip the
  composer or core footer. Header contributions leave transcript selection and
  the Working mouse target two rows above their rendered positions.
- **Reproduction:** `go test ./internal/tui -run
  'TestPluginUpdatesResize|TestPluginChromeFits|TestPluginHeaderOffsets' -count=1`
  fails for immediate updates, short frames, overlays, and header mouse offsets.
- **Remediation:** Apply geometry changes before rendering, budget plugin rows
  after core controls and overlays, and share the rendered header offset with
  transcript and run-status hit testing.
- **Resolution:** Plugin updates synchronously resize/reflow changed geometry;
  one shared allocation shrinks optional chrome after reserving core controls
  and wrapped overlay rows. Plugin chrome is inset and sidebars have a gutter.
  Transcript selection and Working hit testing include rendered plugin headers.
- **Verification:** Regression tests cover immediate growth/shrink updates,
  both transcript modes, 20–160 columns, 8–48 rows, repeated resize/overlay
  transitions, growing input, active runs, and mouse offsets. `go test ./...`,
  `go vet ./...`, `go test -race ./internal/tui -count=1`, all 56 script tests,
  and `python3 scripts/check_benchmarks.py` passed. Tests requiring localhost
  listeners ran outside the sandbox; the full suite and benchmarks used
  isolated temporary `SNOW_HOME` directories. `./scripts/install-local.sh`
  installed the updated `0.1.0-dev` binary.

## BUG-054: Plugin screens lack native panels and hide focused actions

- **Status:** Resolved; verified 2026-09-07
- **Severity:** Medium
- **Surface:** Workspace Notes and other plugin screens in the TUI
- **Actual:** Screens replace the full frame with unbordered text, unlike the
  centered model picker. Actions render as bracketed labels with focus only in
  a footer; long content can hide the selected action. Scrolling beyond the end
  accumulates an invisible offset, so Up does not immediately move back.
- **Reproduction:** `TestPluginScreenUsesCenteredCard` and
  `TestPluginScreenKeepsFocusedActionsVisible` fail against the original renderer
  at both wide and narrow sizes, in inline and alternate-screen modes.
- **Remediation:** Use the shared centered picker card, a bounded content pane,
  and a separate action list with a visible selection. Clamp scrolling and keep
  blocking dialogs authoritative. Simplify the Workspace Notes presentation.
- **Required verification:** Panel bounds and centering, long content and action
  lists, forward/backward focus, resizing, dialog priority, draft restoration,
  and a rendered TUI check.
- **Resolution:** Plugin screens share the centered picker frame, use compact
  sizing for short content, and keep a highlighted action list outside the
  scrolling body. Scroll offsets are bounded; background clicks are consumed.
  Workspace Notes uses a scope/count line, separated notes, and shorter labels.
- **Verification:** The new panel regressions pass for 20–140 columns and 8–40
  rows, inline/alternate-screen modes, empty/passive views, Unicode, nested
  buttons, focus wrapping, dialog priority, and unchanged composer content.
  Rendered previews use the real Workspace Notes plugin with isolated storage.
  `go test ./...`, `go test -race ./internal/tui -count=1`, `go vet ./...`,
  all 56 support-script tests, and `python3 scripts/check_benchmarks.py` passed.
  After the final compact-copy adjustment, the focused panel tests and TUI vet
  passed again. `./scripts/install-local.sh` installed `0.1.0-dev`; the installed
  binary passed `python3 examples/plugins/extensions_smoke.py` with fake and
  localhost providers. A separate installed-binary PTY session opened `/notes`,
  added a note through the input dialog, changed the visible action with Tab,
  and inserted it into the composer with Enter while the agent remained idle.

## BUG-055: User-input dialogs retain the old composer-area layout

- **Status:** Resolved; verified 2026-09-07
- **Severity:** Medium
- **Surface:** TUI questions and plugin input/select/confirm/form dialogs
- **Actual:** The updated plugin screen opens a centered card, but its input
  dialog still spans the area above the composer with an oversized controls
  line. It changes transcript geometry and breaks the native panel pattern.
  Long content can hide choices, and resizing a multiline draft can leave its
  insertion point outside the visible editor until another update.
- **Reproduction:** The provided Workspace Notes screenshot and
  `TestUserInputCardIsCentered`, `TestUserInputCardKeepsSelectionAndErrorsVisible`,
  and `TestUserInputCardKeepsMultilineDraftAcrossResize` reproduce these failures.
- **Remediation:** Share the centered picker frame, bound prompt and answer
  regions, resize the editor to the card, and keep focus, validation, and
  controls visible. Preserve drafts and blocking-request priority.
- **Required verification:** Text and choice dialogs, forms, Unicode, long
  prompts and drafts, resizing, inline/alternate-screen geometry, permission
  priority, and an installed-binary input/confirm flow.
- **Resolution:** All user-input requests now use the shared centered card.
  Textareas fit an inset bordered field; choice windows follow the selection.
  Controls are compact, form progress appears only for multiple questions,
  and plugin display names replace generic dialog headings. Resize and draft
  restoration refresh the editor viewport before displaying the frame.
- **Verification:** Focused tests pass for 20–140 columns and 8–40 rows,
  inline/alternate-screen modes, choice windows, validation, Unicode,
  multiline draft restoration, permission priority, and returning to the
  parent panel. `go test ./...`, `go test -race ./internal/tui -count=1`,
  `go vet ./...`, all 56 support-script tests, and the benchmark guard passed.
  Wide/narrow text and confirmation frames were rendered and visually checked.
  `./scripts/install-local.sh` installed `0.1.0-dev`; an isolated installed
  PTY session opened the centered input, saved a multiline note, returned to
  Notes, opened the centered clear confirmation, and selected No. The agent
  returned to idle and the note remained intact.

## BUG-056: Failed plugin results apply transitions and retain owned children

- **Status:** Resolved; verified 2026-09-07
- **Severity:** High
- **Surface:** Plugin command lifecycle
- **Actual:** A command returning `isError: true` is treated as successful by
  deferred branch handling. Its queued fork is applied and its spawned children
  remain open. Failure to apply a scheduled transition also skips child cleanup.
- **Reproduction:** `TestPluginErrorResultDoesNotApplyBranchTransition` failed
  with a changed generation; `TestPluginErrorResultClosesOwnedChildren` failed
  with the owned child still running.
- **Remediation:** Apply transitions only after successful, uncancelled command
  results; clean up command-owned children on result, execution, or transition
  failure. Keep explicit error results intact for RPC/SDK callers and make them
  visible in the TUI even when they have no text content.
- **Required verification:** Error results, transition failure, cancellation,
  successful fork, and isolation of unrelated children.

## BUG-057: Plugin dialog lifetime is coupled to unrelated root events

- **Status:** Resolved; verified 2026-09-07
- **Severity:** High
- **Surface:** Plugin user-input broker and TUI
- **Actual:** Root `turn_done`, `aborted`, or an unrelated `ask_user` completion
  dismisses a live plugin dialog. Conversely, cancelling a plugin command leaves
  its settled dialog visible. Standalone plugin input can exit the TUI on Ctrl+C
  instead of resolving the request.
- **Reproduction:** All three cases of
  `TestPluginDialogSurvivesUnrelatedTurnEvents` failed against a real pending
  plugin command. The input handler's Ctrl+C path also left its broker blocked.
- **Remediation:** Signal broker settlement per request, dismiss only the
  matching dialog, preserve plugin requests across unrelated turn boundaries,
  and release standalone input on Ctrl+C. Ignore delayed settlement for newer
  requests, including reused request IDs.
- **Required verification:** Reply, rejection, cancellation, broker closure,
  late subscription, stale settlement, concurrent root events, and live TUI input.

## BUG-058: Plugin choice dialogs offer answers they cannot accept

- **Status:** Resolved; verified 2026-09-07
- **Severity:** Medium
- **Surface:** Plugin selects, confirmations, forms, and manual input replies
- **Actual:** Every choice dialog adds Other, including closed selects and typed
  enum/boolean fields. The custom answer closes the dialog and then fails host
  validation; confirmations silently interpret arbitrary text as false. Empty
  or duplicate choices can produce an unanswerable or ambiguous picker.
- **Remediation:** Add optional host-owned `choices_only` request metadata,
  enforce it in the broker before settlement, and omit Other from closed
  pickers. Validate plugin choices before showing them; default confirmation
  focus to No. Model-authored choice questions keep Other.
- **Required verification:** TUI choice navigation, corrected manual replies,
  typed forms, malformed definitions, and RPC schema compatibility.

## BUG-059: Late plugin completions retain UI from a previous branch

- **Status:** Resolved; verified 2026-09-07
- **Severity:** Medium
- **Surface:** TUI plugin command completion and branch changes
- **Actual:** The TUI accepts command output after the app has switched branches
  and can retain the previous branch's panel. Generation synchronization only
  runs on child events, which need not accompany a root branch change.
- **Reproduction:** `TestPluginCompletionCannotRestorePreviousBranchUI` fails
  after completing input, forking, then reducing the old command completion.
- **Remediation:** Correlate completions with their originating generation;
  refresh contributed views on root event batches and diagnostics as well as
  command completion. Release the running marker without rendering stale output.
- **Required verification:** Delayed completion after fork, successful commands,
  native panel layout, and extension smoke flows.

## BUG-060: Plugin prefix completions override an exactly typed command

- **Status:** Resolved; verified 2026-09-07
- **Severity:** Medium
- **Surface:** TUI slash-command completion
- **Actual:** With Workspace Notes loaded, typing `/note` and pressing Enter
  opens `/notes` instead of the add-note dialog. Exact and prefix matches share
  registration order; an old palette index can also override the exact match.
- **Reproduction:** Observed in the installed TUI, and reproduced by
  `TestPluginExactAliasPrecedesLongerPrefix`.
- **Remediation:** Rank exact names before longer prefixes and reset the focus
  to an exact match as the editor changes. Preserve navigation among prefixes
  and fuzzy matches.
- **Required verification:** Plugin aliases, retained palette selection,
  existing completion tests, and installed-binary `/note` input.


### Plugin audit verification (BUG-056 through BUG-060)

- New regressions cover explicit error results, failed transitions, owned-child
  cleanup, dialog cancellation/settlement, late notifications, typed selections,
  malformed choices, stale branch UI, and exact plugin aliases. Existing
  success, normal Other-answer, panel sizing, and permission tests still pass.
- `go test ./...` and `go vet ./...` passed after the final code changes. Race
  checks passed for `internal/plugin/...`, `internal/app`, `internal/tui`,
  `internal/userinput`, `internal/rpc`, and `pkg/snowsdk`; app/TUI race checks
  were repeated after the final alias and cancellation-feedback polish.
- All 56 Python support-script tests and `scripts/check_benchmarks.py` passed.
  Go checks used an isolated build cache; full/race checks required permission
  for localhost mock servers and used temporary Snow homes.
- The installed extension smoke pack passed registration, commands, typed
  forms, view/state persistence, branches, model controls, workflow cleanup,
  all four hooks, bounded file reads, persisted cards, and selected child tools.
- `./scripts/install-local.sh` installed the final checkout as `0.1.0-dev`.
  Wide/narrow question frames were rendered and visually reviewed. An isolated
  installed-binary PTY confirmed Ctrl+C dismisses input, another note can be
  saved immediately, confirmation offers only No/Yes with No selected, and
  accepting No preserves the note. A fresh launch verified `/note` opens the
  add dialog directly and cancellation prints one concise message.

## BUG-061: Successful plugin RPC responses match two schema branches

- **Status:** Resolved; verified 2026-09-09
- **Surface:** RPC version 1 response JSON Schema
- **Actual:** The generic success branch does not exclude the five existing
  plugin commands. Their successful responses also match their dedicated branch,
  so strict `oneOf` validation rejects valid plugin responses.
- **Reproduction:** Validate `plugins_list` with a valid `PluginInfo` array
  against `response.schema.json`; validation reports two matching branches.
- **Remediation:** Exclude plugin commands from the generic success branch and
  retain dedicated response validation for both existing commands and the new
  registration status and enable/disable controls.
- **Regression:** `TestPluginManagementSchemas` validates all eight responses.
- **Verification:** `TestPluginManagementSchemas`, `go test ./...`,
  `go vet ./...`, affected app/TUI/RPC/SDK race checks, all 56 support-script
  tests, and `scripts/check_benchmarks.py` passed. The installed extension smoke
  pack passed per-plugin persistence and restart checks plus existing command,
  hook, dialog, storage, and selected-child execution checks.

## BUG-062: Plugin management separates plugin navigation from its actions

- **Status:** Resolved; verified 2026-09-09
- **Surface:** TUI `/plugins` inspector
- **Actual:** A long scrolling document repeats registrations and loaded plugin
  details above an independently selected action list. Arrow keys scroll text,
  while Tab selects unrelated offscreen plugins' actions. The title counts text
  lines rather than plugins, and wrapped absolute paths dominate the list.
- **Reproduction:** Open `/plugins` with several registered API 2 extensions;
  scroll the document and observe that the selected toggle does not follow it.
- **Remediation:** Use a searchable plugin picker and a per-plugin details/action
  page. Keep arrow-key selection, return navigation, saved/running status,
  restart feedback, and toggle failures visible without changing the composer.
- **Required verification:** Filtering, selection, scoped actions, back navigation,
  toggle persistence/errors, disabled and launch-controlled registrations,
  diagnostics, narrow/short geometry, and an installed-binary keyboard flow.
- **Resolution:** A searchable picker displays each plugin once. Enter opens
  that plugin's details and actions; arrows choose plugins/actions and Escape
  restores the prior list or owning plugin page. Saving blocks repeated
  activation, errors identify the affected plugin, and restart feedback stays
  in the panel. Long details retain page-key scrolling.
- **Verification:** Focused regressions passed for filtering, no matches,
  Unicode, scoped actions, nested views, dialogs, draft preservation, pending
  saves, launch controls, diagnostics, and 20–140 column / 8–40 row frames in
  both transcript modes. `go test ./...`, `go vet ./...`,
  `go test -race ./internal/tui -count=1`, all 56 support-script tests, and
  `python3 scripts/check_benchmarks.py` passed. Localhost-dependent Go suites
  required execution outside the sandbox. Rendered wide/narrow panels were
  inspected; final copy changes passed focused TUI tests, vet, and page tests.
  `./scripts/install-local.sh` installed the checkout. An isolated installed
  PTY verified filtering, enable/disable persistence, restart feedback, opening
  Workspace Notes, returning to the same action/filter, resizing, and composer
  input without starting an agent turn.

## BUG-063: Zen catalog discovery omits newly published free models

- **Status:** Resolved; verified 2026-09-10
- **Surface:** OpenCode Zen model discovery and TUI `/model`
- **Expected:** Newly published, supported free Zen models become discoverable
  without manually adding each model ID to a Snow release; paid models remain
  excluded and transport/capability metadata must be verified.
- **Original behavior:** `internal/provider/opencodezen/catalog.go` intersected live
  `/models` IDs with the compiled `freeModels()` allowlist. Remote models.dev
  records enrich reasoning only; they cannot introduce model IDs. Both
  `muse-spark-1.3-contributor-free` and `ling-3.0-flash-fin-free` appear in the
  [live Zen catalog](https://opencode.ai/zen/v1/models) and
  [official guide](https://opencode.ai/docs/zen/) but are absent from the
  allowlist. The resulting five maintained live models match the user's Snow
  screenshot, while OpenCode shows seven.
- **Reproduction:** Compare those two live IDs against `freeModels()` and the
  `ListModelsWithCredential` loop over that list. A fresh cache or process still
  cannot return either ID. The existing
  `TestListModelsFiltersToMaintainedFreeCatalogAndOptionalAuth` also explicitly
  expects IDs outside the local list to be discarded.
- **Original refresh limitation:** The provider's 15-minute cache freshness was checked
  on discovery calls, not by a timer. `liveRuntimeSelection.ensureCatalog`
  reuses loaded catalogs without an expiry unless forced; Zen has no dedicated
  forced-refresh method to bypass its own fresh cache.
- **Impact:** Users cannot select newly offered free models even when upstream
  discovery succeeds. Restarting or waiting for the provider cache to expire
  does not address the missing-ID restriction.
- **Remediation:** Discover supported free models from current upstream
  availability and verified pricing/transport metadata, retain safe offline
  policy overrides, and provide an expiry-aware refresh path through the app
  and picker. Do not infer free access solely from an ID suffix.
- **Required regression coverage:** Newly published free IDs without source
  edits, paid/unknown-pricing exclusion, correct transport and limits,
  unavailable promotions, offline cache behavior, expiry, forced refresh, and
  picker propagation.
- **Resolution:** Live availability now admits new IDs using explicit free
  pricing, supported protocol/capability metadata, and deprecation filtering.
  The v3 disk cache retains and revalidates that evidence, including successful
  empty catalogs. Provider expiry and revisions propagate to app snapshots;
  opening `/model` refreshes expired Zen data and Ctrl+R forces discovery while
  retaining the filter and a still-available selected row. Inference checks
  expired catalogs before using a retained selection.
- **Verification:** `go test ./...`, `go vet ./...`, race checks for
  `./internal/provider/...`, `./internal/subagent`, `./internal/agent`,
  `./internal/app`, `./internal/session`, `./internal/rpc`, `./internal/tui`, and
  `./pkg/snowsdk`, all 56 Python support-script tests, and
  `python3 scripts/check_benchmarks.py` passed. Go tests used localhost mock
  servers outside the sandbox and an isolated build cache; the benchmark guard
  passed after using that writable cache. Focused regressions cover new IDs,
  both protocols, unknown/paid/tiered pricing, cache persistence and expiry,
  paid transitions, withdrawals, metadata outages, cancellation, app snapshot
  propagation, and picker filter/selection/closure behavior.
  `./scripts/install-local.sh` installed the checkout. An isolated installed
  RPC smoke check discovered all seven current active free Zen models and
  selected both missing IDs across process restarts using the v3 disk cache.
  An isolated installed-binary PTY verified seven filtered Zen matches, both
  missing models visible, a newer catalog timestamp after Ctrl+R, and responsive
  keyboard/resize behavior at 110 and 64 columns.

## BUG-064: ChatGPT catalog compatibility hides GPT-6 Astra

- **Status:** Resolved; verified 2026-09-10.
- **Surface:** ChatGPT subscription model discovery and the shared model picker.
- **Evidence:** Two read-only requests using the same Snow OAuth credential and
  `originator=snow` returned different catalogs: `client_version=0.147.0`
  omitted `gpt-6-astra`, while `client_version=0.153.4` returned it with
  `visibility=list`. The latter matches the installed Codex client's catalog
  compatibility version. Snow pins the older version in
  `internal/provider/chatgpt/client.go`; refreshing its cache cannot reveal a
  model omitted by that compatibility contract.
- **Related defect:** ChatGPT had a 15-minute disk cache but did not expose its
  expiry to the app's retained catalog snapshot, so an open process can keep
  an old inventory until forced refresh or restart.
- **Resolution:** Advanced the tested catalog contract to `0.153.4`; existing
  version validation rejects caches from the old contract. The ChatGPT adapter
  now publishes expiry and content revisions through the authenticated wrapper
  to the app loader. Cache reads retain their original deadline, 304 responses
  renew freshness, and outages keep only compatible same-account cached models
  with a one-minute picker retry interval. No model allowlist or inferred
  account entitlement was added.
- **Verification:** `go test ./...`, `go vet ./...`, and
  `go test -race ./internal/provider/chatgpt ./internal/provider ./internal/app
  ./internal/tui ./internal/rpc ./pkg/snowsdk` passed. All 56 Python tests and
  `python3 scripts/check_benchmarks.py` passed; the benchmark run required an
  isolated `SNOW_HOME` after the sandbox blocked the default artifact directory.
  Focused tests cover legacy-cache replacement, Astra capability mapping,
  deadline propagation through the real auth wrapper/app loader, ETags, outage
  retry, and account isolation. `./scripts/install-local.sh` installed the fix.
  Installed-binary live checks selected Astra from discovery and a cached
  restart, then completed a read-tool round trip with the correct random fixture
  contents. An installed PTY check confirmed Astra in the filtered picker,
  Ctrl+R refresh with preserved query, and model/reasoning selection.

## BUG-065: Modified Enter accepts composer completions

- **Status:** Resolved; verified 2026-09-10.
- **Surface:** Composer slash, skill, and file completion overlays.
- **Evidence:** With `/help` in the composer and completion open, Shift+Enter
  reaches the overlay's Enter handler before the configured newline binding.
  Alt+Enter follows the same path. The enhanced-key regression test
  `TestNewlineDoesNotAcceptComposerCompletion` reproduces the issue.
- **Resolution:** Composer newline/follow-up bindings bypass completion
  acceptance while ordinary Enter/Tab retain completion behavior.
- **Verification:** The regression failed before the fix; it and the focused
  composer, completion, mention, and skill tests pass after the change.

## BUG-066: Charm v2 wrapping exceeds transcript allocation limits

- **Status:** Resolved; verified 2026-09-10 on the migration branch.
- **Surface:** Transcript hydration and width-dependent reflow.
- **Evidence:** `BenchmarkSessionHydration5000` rose to 4,587,751 allocations
  and 154.9 MB per operation; mixed hydration reached 979,337 allocations and
  37.4 MB. Both exceed the unchanged benchmark guard. Profiling identifies
  Lip Gloss v2.0.6 `WrapWriter.Write`, whose interface write allocates a byte
  slice for each output byte.
- **Resolution:** Transcript wrapping batches string writes and caches active
  style/link sequences while retaining isolation per row, grapheme widths,
  and padding. Open styles and links are closed at the end of the transcript.
- **Verification:** Focused layout/terminal-state equivalence tests and
  `go test ./...` pass. The unchanged benchmark guard passes: three-sample
  medians are 121,626 allocations / 87.8 MB for hydration and 127,582
  allocations / 25.2 MB for mixed hydration.

## BUG-067: Charm v2 frame rendering is slower than v1

- **Status:** Resolved; verified 2026-09-10, including sustained long-draft edits.
- **Severity:** Medium (P2).
- **Surface:** Frame composition, composer edits, and transcript selection.
- **Evidence:** The migration benchmarks regress from 46/72 microseconds to
  121/230 microseconds for 40/120-column frames, 0.278 to 0.759 ms for short
  composer edits, and 0.448 to 1.117 ms for selection frames. The repository
  guard covers hydration but does not detect these rendering regressions.
- **Investigation:** CPU/allocation profiles identify redundant full-frame
  wrapping/alignment and unconditional textarea dimension updates. Preserve
  terminal/Unicode behavior and compare against v1, not only broad ceilings.
- **Initial resolution:** One final frame fit replaces repeated wrapping/alignment;
  unchanged composer output is cached with bounded retention and explicit
  cursor/theme invalidation. Layout avoids unchanged dimension setters. A
  reproducible Bubbles v2.2.1 textarea snapshot changes only printable-ASCII
  wrapping width measurement, preserving the original Unicode and lifecycle
  paths. Its complete upstream tests, license, and source hash checks remain.
- **Initial verification:** Fresh three-sample comparisons against pre-migration
  `8fad893` improve all seven measured latencies and allocated-byte totals:
  40/120-column frames are 55%/63% faster, selection 46% faster, and short,
  8 KiB, and 64 KiB edits 10%/29%/35% faster. Raw results are checked in under
  `benchmarks/results/2026-09-10-charm-v2-rendering`. `go test ./...`,
  `go vet ./...`, `go test -race ./internal/tui/... -count=1`, all 58 Python
  tests, snapshot verification, SDK example, and PTY/plugin smokes pass.
  Existing benchmark limits are unchanged; new rendering limits pass and
  reject the recorded pre-fix migration samples.
- **Follow-up evidence:** The shrinking-buffer backspace fixture misses repeated
  edits at a stable draft size. In an approximately 8 KiB composer at 120x40,
  insert one character, lay out/render, delete it, and lay out/render again.
  Three alternating baseline/current runs on one CPU, with matched true-color
  styling and dark backgrounds, show median latency per edit pair of
  2.495 -> 4.952 ms for ASCII (+98.5%), 2.707 -> 4.848 ms for
  accented text (+79.1%), 2.478 -> 4.119 ms for CJK (+66.3%), and
  2.516 -> 3.117 ms for emoji (+23.9%). Baseline is `8fad893`; current is
  `344fbe4`. These are local model/render measurements, not terminal GPU timings.
  Profiles attribute 53% of current sampled CPU to `textarea.(*Model).view`,
  called from both component Update and View. The ASCII wrap optimization does
  not eliminate that repeated rendering work.
- **Follow-up resolution:** The textarea styles visible rows only, preserving
  row counts, selection coordinates, and the viewport's bottom clamp. Update
  and View ordering remain intact. Stable-size ASCII/accent/CJK/emoji editing
  now has its own benchmark gate; old limits are unchanged.
- **Follow-up verification:** Differential tests against the unmodified pinned
  textarea pass for Unicode, cursor modes, selection, scrolling, narrow widths,
  resize, dynamic height, styles, and placeholders. Three alternating matched-
  color runs against v1 improve latency by 11–34% and allocated bytes by 20–23%.
  Allocation counts remain higher than v1. All four pre-fix v2 samples fail the
  new ceilings. Raw samples and details are in
  `benchmarks/results/2026-09-10-recent-changes-audit/fixes/`.

## BUG-068: Ghostty cannot show Snow activity or background attention

- **Status:** Resolved; verified 2026-09-10 on `feat/ghostty-v2`.
- **Surface:** Terminal progress, tab titles, and background notifications.
- **Evidence:** The v2 root View sets screen, focus, and mouse state but leaves
  title/progress empty. No completion/request path emits terminal attention or
  desktop notifications. A running Snow turn therefore has no external status
  in another Ghostty tab. Ghostty supports the required title, OSC 9;4 progress,
  OSC 9 desktop notification, and bell protocols.
- **Remediation:** Add configurable global title/progress/alert preferences,
  map accepted root state and serialized input requests, suppress stale/duplicate
  alerts, and maintain progress while blocked without rebuilding the frame.
  Verify progress clear/title cleanup on ordinary quit and cancellation.
- **Verification:** Focused state, policy, identity, sanitization, responsive
  settings, and real renderer tests pass. Consecutive paused-state heartbeats
  write only the escape sequence. `go test ./...`, `go vet ./...`, full affected
  race checks plus final focused race checks, all 58 Python tests, the benchmark
  guard, and all PTY lifecycle scenarios pass. The seven controlled rendering
  fixture medians do not regress; terminal metadata has identical frame
  allocations enabled/disabled. Results are recorded in
  `benchmarks/results/2026-09-10-terminal-status`.

## BUG-069: A late prompt error reopens a completed TUI turn

- **Status:** Resolved; verified 2026-09-10.
- **Surface:** TUI command results and root turn lifecycle projection.
- **Reproduction:** Deliver a root `error`, its `turn_done`, then the matching
  admitted `promptDoneMsg` error. The command-result reducer publishes another
  local error event; `adoptTurn` sets the completed turn busy again. The
  `TestLatePromptErrorKeepsTerminalCompletionSettled` regression fails before
  the fix, with busy state and terminal heartbeat incorrectly restarted.
- **Remediation:** Ignore the late result once an admitted turn has settled.
  Its authoritative event stream already delivered the error. Preserve the
  pre-admission failure path and handling while an admitted turn is still busy.
- **Verification:** The delayed-error terminal regression failed before the fix.
  It and the independent `TestLatePromptErrorDoesNotRestartSettledTurn` pass with
  the fix, as do `go test ./...`, `go vet ./...`, and focused race checks.

## BUG-070: Modified copy shortcut quits or aborts Snow

- **Status:** Resolved; verified 2026-09-10 on `feat/ghostty-v2`.
- **Severity:** Medium (P2).
- **Surface:** Enhanced keyboard input and composer selection.
- **Reproduction:** Type a draft, use Shift+Left to select text, then send
  Ctrl+Shift+C from a terminal that forwards this chord to Snow. The v2 textarea
  binds this chord to CopySelection, but the top-level emergency handler matches
  any C key containing the Ctrl modifier. It returns Quit while idle and requests
  abort while busy. The user-input dialog has the same broad modifier check.
- **Impact:** Attempting to copy can close Snow and lose an unsent draft, or
  interrupt a running turn. Terminal-native shortcuts can mask the defect when
  the emulator consumes the chord before Snow receives it.
- **Remediation:** Distinguish the emergency Ctrl+C chord from Ctrl+Shift+C and
  route selection-copy commands through the clipboard handling path. Preserve
  the emergency quit/abort behavior for actual Ctrl+C.
- **Original reproduction:** `TestAuditCopySelectedComposerDoesNotQuit` decodes real
  Shift+Left and Kitty Ctrl+Shift+C byte sequences; selection succeeds, then the
  returned command is incorrectly `tea.QuitMsg`. Add idle, busy, and dialog
  coverage when fixing. Audit reproduction source is retained in
  `benchmarks/results/2026-09-10-recent-changes-audit/repro_tests.go.txt`.
- **Verified fix:** Decoded Ctrl+Shift+C tests pass for idle, busy, dialog, whole-draft,
  and SSH copy. Plain Ctrl+C emergency and existing selection tests also pass. Editor
  commands, including cursor blink, are preserved.


## BUG-071: Bracketed paste is discarded in session and branch name fields

- **Status:** Resolved; verified 2026-09-10 on `feat/ghostty-v2`.
- **Severity:** Medium (P2).
- **Surface:** Session rename and branch rename/fork dialogs.
- **Reproduction:** Open an editable name field and paste a short name through
  the terminal. The new `handlePaste` routes several editors but falls through
  to `composerCoveredByModal` for these fields, discarding the paste. Their
  ordinary key handlers accept text correctly.
- **Impact:** Names must be typed manually; terminal paste that worked before
  the v2 migration no longer works in these dialogs.
- **Remediation:** Route literal, single-line paste to `sessionRenameInput` and
  `branchInput`, preserving the respective 72/64-rune bounds and modal ownership.
- **Original reproduction:** `TestAuditBracketedPasteReachesNameFields` fails for both
  name fields with fragmented bracketed-paste input on current HEAD. Equivalent
  v1 paste messages pass on `8fad893`. Include rename and fork coverage in the fix.
- **Verified fix:** Fragmented bracketed-paste tests pass for session rename and branch
  rename/fork, Unicode limits and sanitization, plus non-editable delete/loading guards.


## BUG-072: Repeated OSC 52 paste can invalidate a request or delete selected input

- **Status:** Resolved in the working tree; original and selection-loss regressions verified 2026-09-10.
- **Severity:** Medium (P2).
- **Surface:** Ctrl+V over SSH, or terminal clipboard fallback.
- **Reproduction:** Press Ctrl+V twice before the first terminal clipboard
  response arrives. The second call advances `clipboardGeneration` (and the
  composer's image-paste generation) before rejecting the overlapping query.
  The only outstanding response now fails `clipboardRequestCurrent`, so neither
  paste inserts text.
- **Impact:** Repeating paste while waiting for a slow terminal response silently
  loses the valid first paste. No replacement query is issued.
- **Remediation:** Reject or coalesce the overlapping action before advancing
  either generation, preserving the original owner and valid response. Keep
  canceled-request tombstones and modal/generation fencing intact.
- **Original reproduction:** `TestAuditRepeatedClipboardRequestKeepsFirstValid` confirms
  that the second query is rejected and the first reply leaves the editor empty.
  Cover composer and dialog reads, including local-to-terminal fallback.
- **Verified fix:** Remote and local-fallback tests pass for composer and dialog owners.
  Overlap is rejected before either generation changes; timeout, stale-owner, and
  canceled-request tests remain passing.
- **Follow-up evidence:** `TestFollowupBlockedClipboardPreservesSelection` starts
  an SSH clipboard request, injects its timeout, selects a draft, and retries
  Ctrl+V. The composer loses the entire selected `draft`; the dialog loses its
  selected final character (`draft` becomes `draf`), although no read is started.
  `handleComposerSelectionKey` clears text/attachments before the pending-read
  guard; dialog textarea Update also deletes selection before rejecting the read.
- **Verified follow-up fix:** Occupied clipboard retries are rejected before
  composer, dialog, and login textarea selection mutation. Whole/partial selection,
  text/image attachments, masked login input, owner and generation are preserved
  across pending, timed-out, canceled, and different-owner reads, including remapped
  paste bindings. `TestOccupiedClipboardRetryPreservesEditableSelection` and the
  original retained follow-up probe pass. Tombstone and original generation fences
  remain intact.


## BUG-073: Shift+Up enters composer history instead of selecting text

- **Status:** Resolved; verified 2026-09-10 on `feat/ghostty-v2`.
- **Severity:** Medium (P2).
- **Surface:** Composer history and enhanced selection keys.
- **Reproduction:** With at least one previous prompt, enter a single-line draft
  and press Shift+Up. `navigateInputHistory` checks only the new key Code, so
  Shift+Up is treated as plain Up and replaces the visible draft with history.
  Shift+Down can similarly traverse an already active history selection.
- **Impact:** Selection gestures unexpectedly recall old prompts; typing after
  the gesture edits the recalled prompt instead of the current draft. The saved
  history draft is still recoverable by navigating back before editing it.
- **Remediation:** Limit history navigation to its intended unmodified arrow
  keys and let modified selection/navigation chords reach the textarea.
- **Original reproduction:** `TestAuditShiftUpDoesNotBrowseHistory` passes with v1's
  distinct Shift+Up key on `8fad893` and fails after the migration. Preserve
  ordinary history navigation and multiline selection in regression coverage.
- **Verified fix:** Modified Up/Down tests preserve the current draft/history position
  and multiline selection; ordinary history navigation tests continue passing.


## BUG-074: Manual compaction settlement can report stale or incorrect status

- **Status:** Resolved in the working tree; original, delayed-operation, and real final-mailbox-error regressions verified 2026-09-10.
- **Severity:** Medium (P2).
- **Surface:** Terminal titles, progress/error state, and background alerts.
- **Reproduction:** Run `/compact` while unfocused with a failing summarizer and
  `compaction.fallback=error`. Core reports `EvCompactionDone{IsError:true}` and
  the command returns an error; it does not emit `EvError` or `EvTurnDone` for
  this manual operation. Terminal status observes only the latter event kinds,
  so it returns to Ready, clears progress, and sends no failure alert even though
  the transcript reports the compaction failure. Successful manual compaction
  also lacks the completed title/alert.
- **Impact:** Background users cannot distinguish failed compaction from an idle
  session using the new terminal integration.
- **Remediation:** Settle manual compaction from its own authoritative lifecycle
  and pre-admission error path. Preserve automatic compaction as an intermediate
  phase and suppress success notifications for cancellation.
- **Original reproduction:** `TestAuditManualCompactionFailureReportsFailed` runs the real
  agent with a deterministic failing fake summarizer, reduces its actual events
  and command result, and observes Ready with no alert. The corresponding
  successful-compaction probe reproduces the missing completion state.
- **Verified fix:** Real-agent successful/failing compaction tests pass, plus
  result-before-event, duplicate/acknowledgement, pre-admission failure, cancellation,
  automatic-phase, and stale-result coverage. Terminal settlements retain operation
  identity.
- **Follow-up evidence (real core events):** Complete two manual compactions and
  reduce both command results before their subscribed lifecycle events. Acknowledge
  completion with focus, then drain the events. The single retained settlement
  belongs to the second operation, allowing the first operation's events to replay
  and replace it; the second then replays too. The acknowledged Ready state becomes
  Done again. `TestFollowupTwoCompactionResultsBeforeEvents` reproduces this on
  `472097f` using real `Agent.Compact` calls, not fabricated operation identities.
- **Follow-up evidence (injected final error):** A successful `EvCompactionDone`
  followed by a matching `compactDoneMsg` containing an error stays Done because
  `settleTerminalCompaction` returns on matching identity before comparing failure.
  `TestFollowupCompactionFinalErrorOverridesSuccess` reproduces the reducer defect.
  Core `Agent.Compact` joins deferred `finishTurnMailbox` failures after emitting
  the lifecycle event, providing a source-traced path for conflicting outcomes;
  an actual mailbox storage failure has not been injected end-to-end in this audit.
- **Verified follow-up fix:** Compaction command results carry the identity captured
  at core admission, and an epoch/sequence watermark fences all older settled
  operations. Final errors upgrade provisional success; local notifications wait
  for final cleanup. Deferral stays scoped to the local operation, including a
  directly following external compaction. Old results cannot overwrite newer work
  or undo focus acknowledgement. Zero-identity pre-admission errors still settle
  after a session/branch fence.
- **Verified follow-up regression:** Both retained probes now pass. Permanent tests
  in `terminal_compaction_order_test.go` cover consecutive result-first operations,
  external successors, cancellation, focus, and real mailbox-append failure during
  `Agent.Compact` finalization in both delivery orders. The failure is injected at
  the session Store seam while actual compaction/checkpoint persistence succeeds;
  this is not a physical disk-failure test.


## BUG-075: Signal cancellation can deadlock terminal shutdown

- **Status:** Resolved; verified 2026-09-10 on `feat/ghostty-v2`.
- **Severity:** Medium (P2).
- **Surface:** Bubble Tea v2 TUI shutdown after SIGTERM/SIGINT.
- **Reproduction:** Start the fake-provider TUI in a PTY, wait for the composer,
  and send SIGTERM. The isolated repeated probe hung on its fourth process;
  the earlier full PTY matrix also intermittently timed out after cancellation.
- **Evidence:** The captured Go stack places the main goroutine in
  `Program.shutdown` / `channelHandlers.shutdown`, waiting for a signal handler
  blocked on `p.msgs <- QuitMsg` in Bubble Tea v2.0.9 `tea.go:681`. Snow's CLI
  signal context can cancel the event loop before that unguarded send completes.
- **Remediation:** Disable Bubble Tea's internal signal handler and own signals
  through a cancelable context for the TUI lifetime. Preserve standalone TUI
  signal handling, ordinary key quit, external cancellation, and mode restoration.
- **Verified fix:** The full fixed-binary PTY matrix passes, including 30 consecutive
  signal cancellations with terminal restoration. The original failure was captured
  before the fix; the TUI now uses context-owned signals with Bubble Tea signal handling
  disabled.

Verification for BUG-067 and BUG-070–075: `go test ./...`, `go vet ./...`,
`go test -race ./internal/tui/... ./internal/app ./internal/config -count=1`,
all 58 Python tests, textarea snapshot verification, and the complete benchmark
guard pass. The fixed PTY matrix includes 30 consecutive signal cancellations.
Logs and measurements are retained under
`benchmarks/results/2026-09-10-recent-changes-audit/fixes/`.

## BUG-076: Collapsed prompt history prevents returning to the saved draft

- **Status:** Resolved in the working tree; regression fixes verified 2026-09-10.
- **Severity:** Medium (P2).
- **Surface:** Composer history containing large prompts.
- **Expected:** Up/Down traverse history and Down past the newest entry restores
  the saved draft, including when recalled entries are displayed as attachments.
- **Reproduction:** Remember a 4,096-character prompt, type `new draft`, press Up,
  then Down. Up collapses the recalled prompt into a pasted-text attachment; Down
  leaves that token as the current history item rather than restoring `new draft`.
- **Cause/impact:** `navigateInputHistory` rejects arrows whenever `pastedTexts`
  is nonempty, including attachments created by history recall itself. Users
  cannot browse past large entries or recover the saved draft through navigation.
- **Remediation:** Distinguish entering history with independent attachments from
  continuing navigation through recall-generated attachments; retain the saved
  draft until browsing ends.
- **Original regression evidence:** `TestFollowupLargeHistoryRestoresDraft` failed
  on `472097f`. Cover both rune/line collapse thresholds, older/newer
  traversal, original-draft restoration, and independently edited attachments.
- **Verified fix:** Active history browsing accepts recall-generated attachments,
  while independent attachment drafts still block entry. Permanent regressions
  cover both collapse thresholds, older/newer traversal, empty/nonempty saved
  drafts, independent edits/images, and attachment deletion ending navigation.

## BUG-077: Large bracketed paste does not replace a partial selection

- **Status:** Resolved in the working tree; regression fixes verified 2026-09-10.
- **Severity:** Medium (P2).
- **Surface:** Composer paste collapsing and textarea selection.
- **Expected:** Pasting replaces selected text and clears the selection regardless
  of whether the body is large enough to become an attachment.
- **Reproduction:** Type `abc`, select `c` using Shift+Left, and bracketed-paste
  4,096 `x` characters. Expanded text has 4,099 characters and still ends in `c`,
  instead of the expected 4,098 characters; the selection remains active. A
  ten-character paste in the same fixture correctly replaces `c`.
- **Cause/impact:** `collapseComposerPaste` inserts the attachment token through
  `InsertString` without deleting or clearing the textarea selection. Submitted
  text includes content the user intended to replace.
- **Remediation:** Apply ordinary paste selection semantics before inserting the
  collapsed token, then prune displaced attachments.
- **Regression evidence:** The large subcase of
  `TestFollowupLargePasteReplacesPartialSelection` failed on `472097f`; its small
  control passed. Add multiline/partial selections, both collapse thresholds, attachment
  replacement, and exact expanded submission assertions.
- **Verified fix:** Collapsed tokens use the ordinary textarea paste path, replacing
  and clearing partial/multiline selections before existing attachment pruning.
  Permanent tests pass below/at rune and line thresholds, with Unicode and forward/
  backward selections, exact expanded submission, and displaced attachment removal.

## BUG-078: Recalled multiline prompts can leave the insertion point offscreen

- **Status:** Resolved in the working tree; regression fixes verified 2026-09-10.
- **Severity:** Medium (P2).
- **Surface:** Composer history and optimized layout dimension updates.
- **Expected:** Programmatic draft replacement keeps the insertion point visible.
- **Reproduction:** Remember two distinct twenty-line prompts below the collapse
  thresholds, then recall each with Up and render. After the second recall, the
  cursor is on logical line 19 while the six-row viewport remains at offset 0.
  The audit fixture also observes offset 0 after the first recall.
- **Cause/impact:** `SetValue` resets the viewport to its top; `CursorEnd` does not
  reconcile scrolling. `layout` now skips unchanged `SetHeight`, which formerly
  supplied a scrolling side effect. Users see the beginning of the recalled
  prompt instead of the location where typing will insert text.
- **Remediation:** Explicitly reconcile scrolling after programmatic composer
  replacement, without restoring redundant per-frame dimension setters. Check
  first render as well as successive same-height replacements.
- **Original regression evidence:** `TestFollowupHistoryKeepsCursorVisible` failed on `472097f`. Cover
  wrapped/multiline history, narrow terminals, initial recall, restored drafts,
  and consecutive values with unchanged dimensions.
- **Verified fix:** Programmatic composer replacement explicitly reconciles layout,
  wrapped content, and viewport scrolling once per replacement. No redundant
  per-frame dimension setter was restored. Permanent tests cover initial and
  consecutive same-height multiline/wrapped recall, narrow terminals, restored
  wrapped drafts, and visibility before the first rendered frame.

## BUG-079: Failed automatic goal compaction stops without a failure alert

- **Status:** Resolved in the working tree; reducer and actual automatic-summarizer failure regressions verified 2026-09-10.
- **Severity:** Medium (P2).
- **Surface:** Background terminal notifications for goal continuation.
- **Expected:** A terminal automatic-compaction failure sends exactly one generic
  failure alert when notifications are enabled for an unfocused terminal.
- **Reproduction:** While unfocused/busy, reduce automatic compaction start and
  failed completion, followed by `EvError` and a blocked-goal update with idle
  core state. The TUI settles to Failed, but emits no notification or bell.
- **Cause/impact:** Automatic compaction completion is correctly not announced
  as standalone success. However, `EvError` suppresses the alert while busy, and
  the blocked-goal boundary clears busy without settling that pending failure.
  No subsequent `EvTurnDone` exists for this boundary; background users miss
  the stopped goal. This sequence is source-traced to `prompt_goal.go`'s
  automatic-compaction error path; the audit injects its events at the reducer.
- **Remediation:** Announce pending failure when a terminal goal update releases
  busy state, preserving automatic intermediate success and cancellation rules.
- **Original regression evidence:** `TestFollowupGoalCompactionFailureAlerts` failed on `472097f`. Add
  exactly-once checks for repeated goal snapshots, focus/settings changes,
  cancellation, and a real failing automatic summarizer.
- **Verified fix:** The terminal goal boundary consumes pending error attention once
  after core admission is released. Non-continuing goal metadata no longer resets
  that pending error. Tests include actual automatic summarizer failure, repeated
  snapshots, focus/settings changes, queued-alert invalidation, cancellation,
  intermediate success, and a held core turn that must not settle prematurely.

## BUG-080: Manual compaction admits work after agent shutdown

- **Status:** Resolved in the working tree; verified 2026-09-10.
- **Severity:** Low (P3).
- **Surface:** Internal manual-compaction admission after `Agent.Close`.
- **Reproduction:** Close an agent, then call `Compact` again. The old path changes
  the admitted turn ID/sequence and briefly marks the closed agent running before
  context loading returns `agent: closed`. Found while verifying zero-identity
  pre-admission errors for BUG-074.
- **Cause:** The manual admission block checks running/queued work but not `closed`.
- **Verified fix:** Check `closed` under the existing admission-state mutex before
  changing runtime state. `TestManualCompactionRejectsClosedAgentBeforeAdmission`
  covers both ordinary and identity-returning calls, unchanged admission state,
  and a zero returned identity on rejection. No second compaction loop was added.

Follow-up reproduction sources, a portable Go-overlay runner, and failure output
for BUG-072, BUG-074, and BUG-076–079 are retained under
`benchmarks/results/2026-09-10-followup-validation/`. The original sources/output preserve the failures
on `472097f`; all seven probes now pass. Broader permanent regressions are included
in the ordinary suite. Fix verification is recorded in that directory's `fixes/`.

Verification for BUG-072, BUG-074, and BUG-076–080: full Go suite and vet,
TUI/textarea/agent/app/config race suite, final focused compaction race tests,
all 58 Python tests, source-qualified textarea snapshot check, all seven original
probes, and the isolated benchmark guard pass. The temporary-binary PTY matrix
passes with ten repeated signal cancellations and restored restart handoff.
The initial concurrent benchmark timing miss and incorrectly invoked snapshot
check are documented with their successful reruns in the fix report.

## BUG-081: Pure plugin session gates reenter the session lock

- **Status:** Resolved in the working tree; verified during workflow integration.
- **Severity:** Medium (P2).
- **Surface:** New API 2 `before_session_change` hooks.
- **Reproduction/evidence:** Register a session gate and select or fork a branch.
  The initial integration timed out: `invokeAsync` obtained the effectful host
  environment through `sessionMu.RLock` while the transition held its exclusive
  lock. Expected a bounded veto or successful transition, not a deadlock.
- **Fix:** Pure hooks use the optional immutable `HookEnvironment` snapshot;
  effectful callbacks retain ordinary host admission.
- **Verification:** Session gate rejection/timeout/invalid-result and successful
  branch-notification tests pass in `internal/app/plugin_session_lifecycle_test.go`;
  full app and agent tests and race suites pass.

## BUG-082: Workflow updates accept a stale inactive SQLite branch

- **Status:** Resolved in the working tree; verified during workflow integration.
- **Severity:** Medium (P2).
- **Surface:** New branch workflow metadata writes through separate store handles.
- **Reproduction/evidence:** Open two handles, fork/select a branch through one,
  then update through the other handle's cached old branch and tip. The initial
  implementation returned success and appended to the now-inactive branch;
  expected `ErrConflict` without an append.
- **Fix:** Verify durable branch activity and tip in the transaction, require
  `active=1` in the branch CAS, and verify the session-meta update affected a row.
- **Verification:** `TestWorkflowSQLiteStaleHandleAfterActiveBranchSwitch` failed
  before the fix (`stale active branch: <nil>`) and passes after it. Full session
  tests, session race tests, and vet pass.

## BUG-083: Individually valid workflow hooks exceed their phase snapshot budget

- **Status:** Resolved in the working tree; verified during workflow integration.
- **Severity:** Medium (P2).
- **Surface:** New API 2 `workflowKeys` registration and child hook hydration.
- **Reproduction/evidence:** Register two same-phase hooks with 33 disjoint keys.
  Initial registration succeeded, but root invocation failed the loader's 64-key
  union bound. A separate child-enabled hook also unnecessarily loaded root-only
  workflow keys. Expected early registration rejection and no child hydration.
- **Fix:** Validate the distinct per-plugin/phase key union at registration;
  skip workflow loading for child requests and retain per-handler input subsets.
- **Verification:** New workflow-hook tests cover disjoint/overlapping unions,
  phase independence, copied isolated values, explicit missing nulls, and mixed
  root/child hooks with a poison loader. Plugin/JavaScript tests, race tests, and
  vet pass.

## BUG-084: Legacy TUI dialogs use composer space and leak background navigation

- **Status:** Resolved in the working tree; verified with the checks below.
- **Severity:** Medium (P2).
- **Surface:** MCP/skills inspection, session/branch/fork pickers, permission
  dialogs, and plan/goal confirmations in both transcript modes.
- **Evidence/reproduction:** `/mcp` still renders at the lower-left above the
  composer (user screenshot). Legacy dialogs participate in `renderOverlays`
  and transcript height; terminal-based row budgets and top-clipping can hide
  controls on short windows. With a fork picker or plan/goal confirmation open,
  PageUp/Home and mouse events can reach the background transcript because the
  reducer's modal guard omits these flows.
- **Expected:** Native modal selections use centered, cell-bounded panels;
  selection and controls follow resize without changing the underlying frame
  geometry. Blocking permission requests keep input precedence and fail closed
  when the card cannot expose the required review context. Composer completion
  lists remain attached to the input rather than becoming modals.
- **Remediation:** Shared selection-card layout and centered placement, card-based
  list budgets, visible edit-field tails, permission-specific safety budgets,
  and consistent modal keyboard/mouse ownership.
- **Required regression coverage:** Inline/alternate-screen centering, narrow and
  large windows, long Unicode lists, first/last selection, loading/empty/edit/
  delete states, background input isolation, host-request precedence, and
  permission approval rejection after shrinking below the review budget.
- **Verification:** New selection-card and permission-popup tests pass, covering
  20×8 through 240×70 full frames in both transcript modes and 1–35 column /
  1–12 row component bounds. Regression tests include wide Unicode rename-field
  tails and the branch-delete confirmation key in compact layouts. `go test ./...`,
  `go vet ./...`, `go test -race ./internal/tui -count=1 -timeout=5m`, Python
  script tests, and `python3 scripts/check_benchmarks.py` passed. The first
  race-check invocation exceeded the shell's two-minute limit; the managed
  rerun completed successfully. `./scripts/install-local.sh` installed the
  verified checkout. Required review context and complete safety warnings are charged to
  the same permission-card budget; undersized approval remains disabled.

## BUG-085: Skills panel has no enable/disable action

- **Status:** Resolved in the working tree; verified with the checks below.
- **Severity:** Medium (P2).
- **Surface:** Centered TUI `/skills` inventory.
- **Evidence:** User screenshot shows only inspect/page/close controls.
  `startSkillsInfo` builds label/detail-only rows and `handleInfoPick` closes
  on Enter, despite existing named enable/disable policy writers used by the CLI.
- **Remediation:** App-owned effective-policy status and named mutation facade,
  Enter/Space toggle actions, persistent global/trusted-project policy writes,
  visible save/errors/restart-required state, and unchanged immutable running
  catalogs. Pending saves outlive panel dismissal so reopening cannot leave the
  displayed state stale; unrelated panels never receive skill results.
- **Required coverage:** Disable/re-enable persistence, policy precedence and
  trust, unchanged live catalog, failed/canceled writes, responsive action hints,
  pending/reopened panels, and preserved selection/composer draft.
- **Verification:** `go test ./...`, `go vet ./...`, focused race tests for
  `Test(SkillPolicy|SkillsPanel)` in app/TUI, all 62 Python tests, and the
  benchmark guard pass. Panel tests cover 20×8 through 240×70 in both transcript
  modes. Refreshed the bundled skill documentation snapshot after its drift
  check correctly detected the canonical `docs/skills.md` change.

## BUG-086: Compact session and branch panels hide management action hints

- **Status:** Open; identified by source/handler audit, not yet fixed.
- **Severity:** Medium (P2).
- **Surface:** TUI `/sessions` and `/tree` browsing at narrow/short sizes.
- **Reproduction:** Open either populated panel in a 40×12 terminal. The
  selection card has an inner width below 40, so `selectionCardLayout` replaces
  its footer with `↑↓ · Enter · Esc`. Session rename/delete and branch
  fork/rename/delete hints disappear; their keyboard handlers still work.
- **Evidence:** `internal/tui/native_selection_view.go` supplies management
  hints only in the normal session/tree browsing footers;
  `internal/tui/selection_card.go` replaces them with generic compact controls.
  `handleSessionPick` and `handleTreePick` retain the management actions.
- **Expected/remediation:** Provide action-preserving compact browsing footers
  using the existing selection-card layout and configured branch keys. Preserve
  frame bounds and selection; keep deletion confirmation and approval safety
  behavior unchanged.
- **Verification needed:** Responsive full-frame tests asserting management
  hints remain visible in both transcript modes, plus selection/draft and
  existing action tests. The audit's `go test ./internal/tui -count=1` passed,
  but existing responsive tests do not assert these management hints.

## BUG-087: Process and subagent fleet inspectors bypass centered panels

- **Status:** Resolved in the working tree; verified with the checks below.
- **Severity:** Medium (P2).
- **Surface:** `/processes` and `/agent` inspectors in both transcript modes.
- **Evidence/reproduction:** User screenshot shows the process inspector
  stretched almost edge-to-edge and aligned at the frame's upper-left.
  Both fleet layouts size themselves from the full terminal with no card cap;
  `viewContent` returns their fitted output before reaching the centered modal
  compositor. This also replaces the normal transcript/composer background.
- **Remediation:** Reuse bounded card geometry and centered overlay placement;
  share fleet-specific list/detail layout with a 120×28 outer-cell cap, wide
  side-by-side and narrow stacked panes, bounded controls, and selected identity
  fallback when only one list row fits. Preserve host-request precedence,
  scrolling, refresh, selection, and the underlying draft/layout.
- **Coverage:** New fleet panel regressions cover populated/empty/loading/error
  states, 20×8 through 240×70 resizing, Unicode labels, both transcript modes,
  centered border coordinates, host permission/question preemption, background
  input isolation, and 1–35 column / 1–12 row component bounds.
- **Verification:** `go test ./...`, `go vet ./...`,
  `go test -race ./internal/tui -count=1 -timeout=5m` (109.727s), all 62 Python
  tests, the benchmark guard, and `git diff --check` pass. Initial inline test
  setup was corrected to mark settled history as committed native scrollback
  before checking input isolation; no production behavior was relaxed.

## BUG-088: Plugin-reference child capability omitted from Plan admission

- **Status:** Resolved in the working tree.
- **Severity:** Medium (P2).
- **Surface:** Default explorer delegation and active-child Plan transitions.
- **Evidence/reproduction:** During deferred plugin-reference integration, the
  default explorer gained read-only `snow_plugin_docs`, but the separate
  `planReadOnlyChildTools` admission list did not. Running
  `go test ./internal/subagent -run TestDefaultExplorerPluginDocsPlanSafety -count=1`
  failed both Plan-mode spawning and transitions with an active default explorer.
- **Remediation:** Admit the read-only reference tool in the child Plan policy
  without admitting Bash, mutation, unknown tools, or recursive delegation.
- **Coverage:** Test the actual default explorer role for both Plan spawn and
  active-child transitions, alongside existing capability-rejection tests.
- **Verification:** Focused `go test ./internal/subagent -count=1`, full
  `go test ./...`, `go vet ./...`, and race checks for plugin references,
  skills, subagents, agent, app, session, RPC, and SDK pass. The existing
  mutation, unknown-capability, and recursive-delegation rejection tests remain
  unchanged and passing.

## BUG-089: Branch selection cards rebuild the parent index for every row

- **Status:** Resolved; verified with the checks below.
- **Severity:** Medium (P2).
- **Surface:** Branch picker rendering and page-size calculation.
- **Evidence/reproduction:** `treeCard()` formats every branch and calls
  `branchDepth` for each row; that helper rebuilds a full parent map per call.
  Opening or paging a session with B branches therefore performs O(B²) parent
  map insertions, even though ancestry traversal is capped at nine levels.
- **Expected/impact:** Large branch lists should remain responsive; card assembly
  should index parents once rather than repeatedly allocate the same map.
- **Remediation:** Build one parent index per card and reuse it for bounded depth
  traversal. Preserve missing-parent, cycle, and depth-cap behavior.
- **Regression coverage and verification:** Depth edge cases and a large-branch
  card benchmark cover 100, 1,000, and 10,000 branches. `go test ./internal/tui
  -count=1`, `go test ./...`, and `go vet ./...` pass. Running
  `go test ./internal/tui -run '^$' -bench '^BenchmarkBranchSelectionCard$'
  -benchmem -count=1` measured approximately 23 µs, 227 µs, and 2.1 ms per card,
  respectively, consistent with linear rather than quadratic scaling.

## BUG-090: Web preview pairing rejects ordinary browser form submissions

- **Status:** Resolved (verified in the initial web-shell implementation)
- **Surface:** Local web manager pairing and authenticated forms
- **Evidence:** Chrome 152 submits `Origin: null` for a same-origin HTML form
  under `Referrer-Policy: no-referrer`; the strict Origin guard correctly rejects
  it with HTTP 403. Synthetic HTTP tests supplied Origin explicitly and missed
  this browser interaction. Reproduced using the real local preview, without
  provider or agent execution.
- **Expected:** Valid local pairing and CSRF-protected forms work in browsers;
  foreign and null origins remain rejected.
- **Remediation:** Use `Referrer-Policy: same-origin` to preserve same-origin
  form Origin while suppressing cross-origin referrers. Keep exact Origin and
  CSRF validation; do not accept null Origin as a workaround.
- **Verification required:** Browser pairing, logout, additional-browser code
  generation, header regression test, and the existing hostile-Origin tests.
- **Verified:** Verified `TestPairingAndPages` and `TestOriginHostAndFormGuards`; Chrome 152 pairing, additional-code form submission, and logout passed with strict Origin validation.
  `go test ./internal/web -count=1` and the affected race suite passed.

## BUG-091: Web preview rejects the browser's default-port authority

- **Status:** Resolved (verified in the initial web-shell implementation)
- **Evidence:** Starting on port 80 retained `:80` in the expected Host/Origin,
  while browsers omit HTTP's default port. Strict comparison then rejected
  legitimate requests. Expected behavior is canonical same-origin matching.
- **Remediation/verification:** Normalize the expected default-port origin,
  retaining exact Host/Origin validation; test IPv4 and IPv6 port-80 authorities.
- **Verified:** Verified `TestDefaultPortBrowserAuthority` for canonical IPv4 and IPv6 port-80 authorities. No privileged port-80 live listener was required.
  `go test ./internal/web -count=1` and the affected race suite passed.

## BUG-092: Pairing-code results use a POST-only browser history URL

- **Status:** Resolved (verified in the initial web-shell implementation)
- **Evidence:** Code generation rendered a full page at `/access/pair`, but that
  route accepts only POST. Browser/HTMX history restoration performs GET and
  receives 405 instead of the access page.
- **Remediation/verification:** POST/redirect/GET to the canonical access page,
  using a one-time in-memory result. Verify redirect, one-time display, and
  browser back navigation; never place pairing credentials in URLs.
- **Verified:** Verified `TestPairAnotherBrowserLogoutAndRestart`; Chrome 152 code generation redirects to the GET access URL, Back restores that page, and the one-time code is not redisplayed.
  `go test ./internal/web -count=1` and the affected race suite passed.


## BUG-093: Catalog scan limit did not bound directory enumeration

- **Status:** Resolved
- **Severity:** Medium
- **Surface:** Runtime-free saved-session catalog (pre-release increment)
- **Observed:** Implementation review and bounded-enumeration regression tests

### Expected behavior

The 4,096-entry inventory limit must bound directory reads and allocations, not
only the number of callbacks processed after reading a directory.

### Actual behavior and evidence

The initial catalog used `fs.WalkDir`; it reads and sorts all entries in each
directory before invoking child callbacks. Thus a very large session directory
could exceed the intended read/allocation budget before the callback's limit or
cancellation check ran. The normal small-directory catalog tests did not cover
this pre-enumeration behavior.

### Remediation and verification

Catalog enumeration now uses batches of at most 64 entries, shares one inventory
budget across directories, checks cancellation between batches and entries, and
reads at most one additional entry to detect overflow. Instrumented tests cover
batch bounds before visitation, shared budgets, interbatch cancellation, and
child-directory exclusion. `go test ./internal/session ./internal/rpc -count=1`
passed after the fix; agent verification additionally passed race and vet for
these packages. No release containing the initial scan implementation was made.


## BUG-094: Failed project registration retained a POST-only history URL

- **Status:** Resolved
- **Severity:** Low
- **Surface:** Web manager project registration (pre-release increment)
- **Evidence:** Initial error handling rendered a complete document directly at
  `/projects/add`. Reload/history restoration could revisit a POST-only route or
  ask to resubmit instead of restoring the Projects page. This repeats the
  history failure pattern tracked for pairing-code generation in BUG-092.
- **Fix:** Redirect failed registration to a canonical Projects GET with a fixed
  public error token. Never place the submitted host path or arbitrary error text
  in URLs; preserve the registry list and show a generic actionable error.
- **Verification:** `TestFailedRegistrationUsesCanonicalGet` checks the redirect,
  safe URL/body, restored error page, and absence of worker activation. Verified
  with `go test ./internal/web -count=1` after the fix.

## BUG-095: Live web projection discarded attributed root-agent events

- **Status:** Resolved; production-shaped RPC fixture regressions and live CLI
  interaction checks verified.
- **Severity:** High (P1).
- **Surface:** Web live-runtime event projection.
- **Reproduction:** Send valid root-attributed (`AgentRef.Path == "/root"`) text,
  permission and question events through the RPC subprocess fixture. The old
  filter discarded every event with `Agent` metadata, including root events;
  completion could return idle with no assistant text or interaction card.
  Earlier fixtures omitted attribution and therefore missed this case.
- **Fix:** Accept legacy untagged and validated root-attributed events, while
  excluding child and malformed/inconsistent attribution. Fixtures now exercise
  text, permission and input with root metadata and inject excluded child events.
- **Verification:** `go test -race ./internal/web -run '^TestRuntime' -count=3`
  and focused web/client tests pass. A rebuilt CLI with a corrected local
  OpenAI-compatible fixture verifies streaming, escaped text, approved host writes,
  question replies and Stop in Chrome. Initial empty CLI output was separately
  traced to the smoke fixture serving Chat Completions frames on the Responses
  endpoint; that observation alone was not evidence of this attribution defect.

## BUG-096: Malformed UTF-8 could expand a bounded web text projection

- **Status:** Resolved; focused regression verified.
- **Severity:** Low (P3).
- **Surface:** Web runtime text projection helper.
- **Reproduction:** Pass invalid UTF-8 whose byte length is already within the
  requested limit to `runtimeText`. The previous control-stripping map replaced
  each invalid byte with a multi-byte replacement rune, exceeding the byte cap.
- **Fix:** Remove malformed UTF-8 before applying byte limits and control
  filtering; preserve valid Unicode and the declared output bound.
- **Verification:** `TestRuntimeActivityMalformedTextStaysBounded` passes under
  the race detector and covers the previously expanding input.

## BUG-097: Changes refresh could display a stale selected diff

- **Status:** Resolved; deterministic browser regression verified.
- **Severity:** Medium (P2).
- **Surface:** Web Files / Changes inspector.
- **Reproduction:** Begin refreshing Changes, select a still-visible old row,
  finish the list refresh, then deliver the old selection's delayed diff. The
  per-request guard alone allowed that diff to replace the current preview.
- **Fix:** Disable old rows during refresh and bind diff publication to both
  the owning Changes-list generation and the current selection generation.
  Refresh/tab leave clear selection and hide stale previews.
- **Verification:** The isolated Chrome regression reproduced eight failing
  assertions before the fix and passes all 14 afterward, including overlapping
  refreshes and delayed responses before/after list replacement. Retained at
  `scripts/tests/browser/inspection-race/run.mjs`.

## BUG-098: Individually bounded model discovery could exceed RPC frame limits

- **Status:** Resolved; encoded-size regression and affected package tests verified.
- **Severity:** Medium (P2).
- **Surface:** Explicit `models_discover` RPC used by the web conversation controls.
- **Reproduction:** Return 512 models whose individually valid descriptions and
  upgrade messages reach the field limits. The original result encoded to
  5,132,421 bytes for plain metadata and 26,103,941 bytes for escape-heavy metadata,
  exceeding the web client's 4,194,304-byte frame limit. Count/field bounds alone
  could make discovery terminate an otherwise usable connection.
- **Fix:** Budget fully escaped records against a 2 MiB result limit with a
  conservative envelope reserve. Keep complete identities, stop before overflow,
  and set `truncated:true`; never pre-encode the unbounded catalog.
- **Verification:** `TestModelsDiscoverEncodedBudgetKeepsRPCUsable` failed before
  the fix and passed afterward: 2,095,147 bytes/209 models for plain metadata and
  2,090,475 bytes/41 models for escape-heavy metadata. The actual RPC writer emits
  a decodable subsequent response. `go test ./internal/rpc ./pkg/protocol` passed;
  the writer test does not claim a separate spawned web-worker smoke check.

## BUG-099: Session-switch completion could overwrite a terminal worker status

- **Status:** Resolved; deterministic lifecycle-boundary regressions verified.
- **Severity:** Medium (P2).
- **Surface:** Web runtime session switching.
- **Reproduction:** Final switch telemetry completes, then worker EOF or manager
  cancellation publishes a terminal state before the switch publishes its idle
  snapshot. An unconditional publication could report success for a dead/closing
  worker. This is a false-success status defect, not a shutdown escape.
- **Fix:** Check context and terminal status under the same lock before rotating
  the switch snapshot or publishing the final idle state.
- **Verification:** `TestRuntimeWorkflowPublishSwitchPreservesTerminalState`
  exercises actual worker EOF, cancellation and closing at the extracted
  publication boundary without sleeps or production test hooks.
  `go test ./internal/web` passed.

## BUG-100: Reference-layout sidebar controls became unavailable at responsive widths

- **Status:** Resolved; production-template browser regressions verified.
- **Severity:** Low (P3).
- **Surface:** Web workspace sidebar after the reference-layout redesign.
- **Reproduction:** At 768–1279px, an older, more-specific CSS selector hides
  Add workspace. Collapse the desktop sidebar and choose Search: the input stays
  CSS-hidden. New session and Settings also lose their accessible names when the
  rail hides their text and their SVGs remain decorative.
- **Fix:** Override the exact legacy selector, expand the rail before opening
  search, and give both icon-rail actions permanent accessible labels.
- **Verification:** Production-CSS Chromium investigation reproduced all three
  conditions. The production-template `harness-layout` browser suite passes at
  360/768/1280/1512px, including Add workspace visibility, rail search focus and
  permanent accessible action names.

## BUG-101: Inactive saved-session lists could overlap runtime activation

- **Status:** Resolved; long-list production-template browser regression verified.
- **Severity:** Medium (P2).
- **Surface:** Selected inactive project with many saved conversations.
- **Reproduction:** Render 35 saved-session rows. The activation panel begins at
  y437 while rows continue to y1728 because the flex container shrinks around its
  overflow-visible contents. Scrolling can move activation out of view while
  overlapping rows remain.
- **Fix:** Prevent the inactive live-session container from shrinking; let the
  inactive conversation pane own scrolling so activation follows saved history.
  Also constrain the sidebar's grid minimum height so a long session tree cannot
  expand the desktop row beyond the viewport.
- **Verification:** Chromium first reproduced the overlap. The actual-template
  35-session fixture then exposed a 1437.56px sidebar/grid row at a 740px viewport;
  six assertions failed across desktop sizes before the grid-minimum fix.
  All 28 state/viewport cases now pass (1,043 assertions), including nonoverlap,
  full-height sidebar bounds and reachable activation after scrolling.


- **Polish regression reverified:** New `#live-session` scroll ownership initially
  overrode the inactive list flex fix. The later scoped inactive-list restoration
  keeps saved rows nonshrinking and overflow visible. Final production matrix
  again verifies all 35 rows, nonoverlapping activation and the canonical outer
  scroll region at every width/theme.
- **Superseded presentation:** BUG-181 intentionally replaces the central catalog
  with grouped sidebar pagination and independently scrolling saved transcripts.
  Start/Resume now occupies the composer seat, with bounded outer scrolling at
  short heights. The obsolete inspection-layer catalog override was removed;
  current nonoverlap/reachability checks live in the Harness workspace fixtures.

## BUG-102: Saved message copy included code-block UI text

- **Status:** Resolved; actual saved-template browser regression verified.
- **Severity:** Medium (P2).
- **Surface:** Inactive saved conversation containing assistant/plan Markdown.
- **Reproduction:** Open a saved assistant message with a fenced code block and
  use Copy message. The saved template had no escaped source node, so enhancement
  captured decorated body text, including the Copy code button, instead of the
  public Markdown source.
- **Fix:** Supply the exact escaped public message text in `.message-source`, as
  the live template does. Copy remains bounded and independent of rendered HTML.
- **Verification:** Production-template `saved-markdown` browser cases at
  360/768/1280/1512px confirm exact source including fences, `<public>` and `&`,
  no copy-banner contamination, separate code-only copy, and no requests.

## BUG-103: Settings close could restore another dialog's focus

- **Status:** Resolved; native-dialog browser regression verified.
- **Severity:** Low (P3).
- **Surface:** Opening Settings after canceling Rename conversation.
- **Reproduction:** Cancel Rename, open Settings, and close it. The generic
  dialog listener retained one global return target; its asynchronous close event
  could override Settings' own restoration and focus the session-menu trigger.
- **Fix:** Store/clear return targets per dialog, bind generic listeners once,
  and leave Settings' close/focus lifecycle to its shell owner.
- **Verification:** Browser workflow tests close Rename before opening Settings,
  then check X and native CDP Escape restoration before and after HTMX replacement
  at all four widths. All 70 workflow assertions pass at each width.


## BUG-104: Pending web questions compete with the normal composer

- **Status:** Resolved; implementation and regression verification passed.
- **Severity:** Medium (P2).
- **Evidence:** User screenshot and current `live.html`/`app.js`: attention and normal
  composer are separate visible seats. The entire question form scrolls, including
  its submit button; all questions are expanded together. Harness's actual pending
  question replaces the composer and keeps header/footer outside its scroll body.
- **Expected:** One bounded attention/composer seat, reachable persistent actions,
  question pagination retaining complete answer drafts, and safe permission warnings.
- **Regression required:** Long/multiple questions, narrow/short viewports, collapse,
  choices-only/custom input, fixed footer, busy/disconnected and unknown outcome.

- **Verified fix:** One resident attention/composer seat now pages questions, retains exact answers/drafts, and keeps header/actions outside the scrolling body. Final production layout gate passed 1,806 reports / 25,628 assertions, including all sixteen answers, IME, ChoicesOnly, collapse, approvals and 240px/reduced viewports.

## BUG-105: Return-to-latest overlaps attention and reader scrolling is overridden

- **Status:** Resolved; implementation and regression verification passed.
- **Severity:** Medium (P2).
- **Evidence:** User screenshot plus `harness.css` fixed bottom176px; jump position
  ignores attention height. `app.js` samples within100px each snapshot and scrolls
  before attention changes; scroll listener only hides, never immediately reveals.
- **Expected:** Measured seat clearance, immediate jump visibility, explicit
  following versus manual reading, and stable anchors across streamed reflow.
- **Regression required:** Tiny upward gestures, unchanged updates, resize,
  attention arrival/collapse, own send, bounded trimming and session navigation.

- **Verified fix:** SnowScroll owns explicit reader/follow state, stable row anchors and measured seat clearance. Final full browser matrix passed tiny/upward reader behavior, head trimming, attention arrival/collapse and explicit Jump without reader snap-back or overlay collision.

## BUG-106: Browser conversation updates batch incremental RPC text

- **Status:** Resolved; implementation and regression verification passed.
- **Severity:** Medium (P2).
- **Evidence:** `app.js` polls complete runtime snapshots two seconds after each
  completed fetch; no push endpoint exists in the audited source. Runtime drainage
  already accepts text deltas immediately. Short responses can first appear complete.
- **Expected:** Bounded instance-bound public snapshot push without another agent
  loop, automatic mutation replay, or exposure of private progress/reasoning.
- **Regression required:** Real gated provider -> agent -> RPC -> runtime -> HTTP ->
  production DOM; multiple prefixes visible while still running; stop, reconnect,
  lifetime/auth/identity and slow-reader resource bounds.

- **Verified fix:** Instance-bound, bounded full-snapshot SSE now coalesces updates at 75ms. Real gated provider → agent → RPC → HTTP → production DOM passed 28 assertions, including two visible prefixes before completion, Stop and genuine watchdog/reconnect without replay. Go/race tests additionally cover expiry, identity, limits and blocked net.Pipe writes releasing subscriptions without canceling the worker.

## BUG-107: Mixed saved text and plan blocks share a presentation ID

- **Status:** Resolved; implementation and regression verification passed.
- **Severity:** Medium (P2).
- **Evidence:** `runtime_events.go` loadHistory assigns the persisted message ID to
  each emitted text/plan segment. `messages.js` explicitly skips duplicate IDs.
  An assistant text/plan/text message loses the plan and trailing text on reconcile.
- **Expected:** Stable distinct presentation block IDs without changing saved IDs.
- **Regression required:** Interleaved text/plan/text through load and repeated DOM
  reconciliation, with every public block visible in order.

- **Verified fix:** Historical text/plan blocks now have distinct stable presentation IDs and original source correlation. Go identity tests and final browser matrix preserve all three saved text → plan → text blocks, order and node identities across reconciliation.

## BUG-108: Live message positional keys shift when bounded history trims

- **Status:** Resolved; implementation and regression verification passed.
- **Severity:** Medium (P2).
- **Evidence:** Live user/assistant projections omit IDs. Browser falls back to
  array indices; removing the oldest row reuses focused controls for another message.
- **Expected:** Stable projection IDs survive token growth and history trimming.
- **Regression required:** Count/byte trimming preserves surviving node identities,
  exact copy targets and reading anchors.

- **Verified fix:** Live projection IDs remain stable across count/byte trimming; obsolete head rows are removed before ordering survivors. Go and full production browser checks preserve surviving rows, active Copy code element, exact updated clipboard target, horizontal table position and reader anchor.

## BUG-109: Streaming Markdown replacement destroys code-copy focus

- **Status:** Resolved; implementation and regression verification passed.
- **Severity:** Medium (P2).
- **Evidence:** Growing assistant text calls SnowMarkdown.render, replacing innerHTML
  and focused code-copy descendants even while outer message identity is stable.
- **Expected:** Unchanged code controls retain DOM identity/focus as later text grows.
- **Regression required:** Focus and click code copy across incremental Markdown,
  exact updated code text, stable table scroll, and safe final rendering.

- **Verified fix:** Sanitized Markdown is reconciled into the live tree rather than replacing focused descendants. Final full browser matrix verifies code-copy identity/focus/exact updated content and real nonzero table scrollLeft across growth and head eviction, including desktop-width overflow.

## BUG-110: Externally closed web runtime reconnects indefinitely

- **Status:** Resolved; implementation and regression verification passed.
- **Severity:** Medium (P2).
- **Evidence:** Snapshot404 becomes generic Reconnecting in app.js; reload is offered
  only for replaced-instance/login states, so an externally closed runtime retries.
- **Expected:** Authoritative closed state with retained draft and explicit review;
  never reopen a worker or replay a prompt from reconnection.
- **Regression required:** External close, own close response/stream race, replaced
  session, permission/auth failure and explicit review flow.

- **Verified fix:** HTTP/SSE terminal closure now disables authority, retains the draft and offers explicit review instead of indefinite reconnect. Final 1,050-assertion workflow suite covers external 404/replacement, unknown outcomes and draft restoration; the real RPC/browser suite verifies own close and subsequent saved-history access.

## BUG-111: Short viewport menus clip non-grouped content and picker footer

- **Status:** Resolved; implementation and regression verification passed.
- **Severity:** Medium (P2).
- **Evidence:** menus.js clamps height to visualViewport minus96; menus.css hides
  overflow except grouped lists. Mode/telemetry headings, rows and notes lack a
  scroll region. Workspace Add action shares the long project-list scroll region.
- **Expected:** All menu information/actions reachable; workspace Add fixed outside
  list scrolling, following actual Harness Menu viewport/footer composition.
- **Regression required:** Every menu pane at240px height, viewport shrink, keyboard
  navigation, long100-project picker and discovery empty/error/truncated states.

- **Verified fix:** Menus now own a shared content scroller and separate pinned workspace footer, with keyboard scroll containment. Final layout matrix passed every menu pane across all seven widths, both themes and short heights, including 100 workspaces and empty/error/disconnected host choices.

## BUG-112: Settings cascade shrinks canonical sidebar New session controls

- **Status:** Resolved; implementation and regression verification passed.
- **Severity:** Low (P3).
- **Evidence:** Last-loaded settings.css overrides expanded38px/collapsed36px
  New session with34px despite canonical harness.css geometry.
- **Expected:** Canonical expanded/collapsed dimensions, not a second size owner.
- **Regression required:** Computed dimensions in both sidebar states and themes.

- **Verified fix:** Removed the trailing Settings size override. Canonical New session controls remain 38px expanded / 36px collapsed; production computed geometry and final dark/light sidebar matrix passed.

## BUG-113: Folder-picker actions fall below short viewports

- **Status:** Resolved; production-template browser verification passed.
- **Severity:** Medium (P2).
- **Evidence:** At 360×240, the Select/Cancel footer occupied y418–471, outside
  the modal. The generic dialog had no independent body/footer composition.
- **Fix:** A bounded 680×500 maximum folder dialog, scrolling body, and fixed
  explicit-action footer. Nothing selects/registers a folder from browsing alone.
- **Verification:** Production folder populated/empty/limited/error cases in the
  1,806-report layout gate passed at all seven widths, both themes, and 240/360/740
  heights; full gate exit 0 before subsequent test-coverage additions.

## BUG-114: Active conversation is missing or stale in the sidebar

- **Status:** Resolved; production workflow verification passed.
- **Severity:** Medium (P2).
- **Evidence:** The sidebar catalog deliberately excludes the live session, but
  the shell supplied no separate current row. Rename/switch could leave stale
  navigation identity when relying only on an inactive catalog refresh.
- **Fix:** One live-owned current row follows authoritative session identity and
  title; duplicate catalog rows are hidden without altering the saved catalog.
- **Verification:** Strengthened workflow assertions verify current ID, title,
  title attribute, href/hx-get and uniqueness through rename, switch and switch
  back: 75 assertions × seven widths × two themes passed.

## BUG-115: Command acknowledgement briefly enables stale idle controls

- **Status:** Resolved; delayed-response regression passed.
- **Severity:** Medium (P2).
- **Evidence:** While a POST ran, snapshots were withheld. Its acknowledgement
  cleared busy state against the previous idle snapshot before a fresh read;
  Send and model/switch controls could become enabled during a running turn.
- **Fix:** Keep mutation controls inert in Synchronizing until a fresh,
  instance-bound snapshot arrives. An acknowledgement does not invent running
  or idle state; unknown outcomes retain their separate explicit-review gate.
- **Verification:** Production workflow delays both POST response and next GET,
  checks disabled controls in between, then verifies fast completion restores
  idle correctly. Full 1,050-assertion workflow suite passed.

## BUG-116: Retired event readers dispatch or retain retry timers

- **Status:** Resolved; deterministic transport regressions passed.
- **Severity:** Medium (P2).
- **Evidence:** Reader ownership was checked before, not after, an awaited read.
  A retiring reconnect-state callback could also schedule a retry after close,
  abort or hiding the document.
- **Fix:** Recheck read ownership after await and callback dispatch; recheck
  disposal/visibility/ownership after reconnect callback before scheduling.
- **Verification:** Production stream.js Node VM suite: 51/51 passed, including
  late snapshot/closed frames, hidden/visible replacement, callback interruption,
  watchdog/backoff, fragmented UTF-8/JSON, bounded frames and complete cleanup.

## BUG-117: Attention resize callbacks cause observer feedback errors

- **Status:** Resolved; assembled browser regressions passed.
- **Severity:** Medium (P2).
- **Evidence:** Real question/approval pages emitted ResizeObserver loop errors
  while synchronous attention geometry writes called the scroll owner's layout.
- **Fix:** Coalesce observer work in one identity-guarded animation frame; cancel
  on disposal, make writes idempotent and notify only on meaningful changes.
- **Verification:** Questions and both approval variants passed the uncaught-error
  gate at all layout widths/themes/heights; no errors were filtered or ignored.

## BUG-118: Short workflow dialogs lose canonical edge clearance

- **Status:** Resolved; native dialog checks passed.
- **Severity:** Low (P3).
- **Evidence:** Mobile CSS allowed height viewport−20px: Rename occupied y10–230
  in a 240px viewport instead of preserving 12px clearances.
- **Fix:** Shared native workflow dialogs cap height to the lesser dynamic/visual
  viewport minus 24px, retaining native vertical scrolling and focus behavior.
- **Verification:** Rename/Switch/Close showModal checks at 320/390/768×240 all
  measured y12–228; final actions remained reachable through scrolling/focus.
  Full production Rename matrix passed without relaxing clearance assertions.

## BUG-119: Missing toolbar container expands narrow Plan composer

- **Status:** Resolved; unchanged geometry assertions passed.
- **Severity:** Low (P3).
- **Evidence:** At 320px the Plan composer grew to 141px. Missing toolbar inline
  containment prevented compact model-trigger queries; inline groups added
  baseline/wrapping height unlike the reference's explicit flex groups.
- **Fix:** Restore toolbar inline-size container and leading/trailing flex groups.
- **Verification:** Plan composer measures 98px at 320/390/768; all seven-width,
  two-theme production geometry assertions passed. Controls/hints remain present.

## BUG-120: Connection warnings push composer outside short viewport

- **Status:** Resolved; production short-viewport checks passed.
- **Severity:** Medium (P2).
- **Evidence:** Nonshrinking warning chrome plus composer exceeded the live
  region at 240px height while hidden overflow clipped the composer.
- **Fix:** Error/uncertainty-bearing live regions alone gain bounded outer
  scrolling and a sticky single composer seat; all warning text remains reachable.
- **Verification:** Disconnected model cases passed across widths/themes/heights.
  Additional actual-browser measurements at 320/390/768×240 confirmed the warning
  end scrolls above the seat, composer bottom near y210, seat bottom y240.

## BUG-121: Leaving Files permits an obsolete preview to publish

- **Status:** Resolved; file-preview race regressions passed.
- **Severity:** Medium (P2).
- **Evidence:** File preview requests lacked the complete tab/list/selection
  guards already used by diffs; a response could arrive after leaving Files.
- **Fix:** Abort on tab departure and enforce project/instance/list/selection
  ownership before rendering. Refresh disables obsolete navigation and rows.
- **Verification:** Inspection suite retains the original 14 race assertions and
  now passes 60 assertions at three viewport sizes in both themes, including
  delayed file responses, stale refresh, tab departure and project replacement.

## BUG-122: Tool-history ambiguity can select the wrong saved result

- **Status:** Resolved; protocol and catalog regressions verified.
- **Severity:** Medium (P2).
- **Evidence:** During durable-history integration, duplicate result candidates
  selected the first result, duplicate assistant IDs reused a map projection,
  and omitted result rows could hide ambiguity. A mismatched result tool name
  could also resolve the wrong call. Absolute block indexes changed presentation
  identity when RPC removed a preceding provider-only block.
- **Fix:** Reject ambiguous ownership/results before filtering; require result
  name agreement (with explicit empty-name legacy compatibility), retain
  tool-call ordinals for identity, and mark incompletely read owner intervals
  unresolved with omission notices.
- **Verification:** `go test ./pkg/protocol ./internal/session ./internal/rpc`
  passed, including `TestProjectHistoryToolsRejectsAmbiguousResultsBeforeFiltering`,
  `TestProjectHistoryToolsDuplicateOwnerIDsAreOmitted`,
  `TestProjectHistoryToolsIdentityUsesToolCallOrdinal`, and catalog interval,
  result-name, duplicate-result, and actual matching-row omission regressions.

## BUG-123: Generic message paging loses unresolved public tool history

- **Status:** Resolved; public-paging and real RPC/browser regressions verified.
- **Severity:** Medium (P2).
- **Evidence:** Legacy paired-snapshot paging deliberately excludes incomplete
  trailing calls. Using that page directly for web history hid an unresolved
  saved call after activation, or showed an existing result as absent when a
  page ended immediately before it.
- **Fix:** Add capability-gated `public_history` paging without changing the
  legacy contract. Include incomplete trailing owners and project each page's
  tools through its complete final ownership interval within the cursor
  snapshot. Clients trust the authoritative tool map even when empty.
- **Verification:** Public RPC paging tests cover trailing calls, page-end
  results, ambiguity, boundaries, mode-bound cursors, and complete frame limits.
  The real local fake-provider/RPC/browser suite passed 36 assertions, including
  persisted public output and stable owning-tool identity across explicit close,
  saved catalog read, and same-session reactivation without prompt replay.

## BUG-124: EOF races discard acknowledged admission or revive failed workers

- **Status:** Resolved; deterministic lifecycle regressions verified.
- **Severity:** Medium (P2).
- **Evidence:** A received successful RPC response processed after EOF retained
  `admission_unknown`. Late negative responses could overwrite terminal failed
  status with idle. The response/EOF gate reproduces the successful-response
  ordering without timing sleeps.
- **Fix:** Record correlated admission independently from runtime liveness;
  preserve definitive completion evidence and terminal worker status. Persist
  intent before dispatch and keep conservative recovery hints across restart.
  Lost workers and explicit close no longer label unconfirmed tools canceled.
- **Verification:** Full `go test ./internal/web` passed. Focused race tests
  exercised before/after-ack EOF, late responses, completion-before-ack,
  definitive cancellation, failed-worker isolation, and ten repeated gated EOF
  runs. Registry/restart tests retain pairing and hints, start zero workers on
  reads, and require fresh explicit activation without replay.

## BUG-125: Interrupted-tool repair appears as a definitive execution failure

- **Status:** Resolved; real SQLite and permission-workflow regressions verified.
- **Severity:** Medium (P2).
- **Evidence:** After a committed assistant tool call loses its result, session
  resume appends an error-shaped bookkeeping message to balance provider-facing
  history. Public projection classified that synthetic message as `failed`, even
  when a write might already have committed. The real SQLite regression failed
  before the fix both before/after a modeled side effect, in direct and inactive
  catalog history.
- **Fix:** Newly synthesized interruption records persist explicit
  `tool_outcome_unknown` provenance. Public projection retains `unresolved`, with
  no definitive result ID or output; bounded catalog decode and active public
  RPC paging preserve this provenance. Provider-facing error semantics remain
  unchanged. No legacy text inference, backfill, or automatic replay is added.
- **Verification:** `TestSQLiteResumeInterruptedToolHistoryStaysUnresolved`
  passes with append-only parent/branch-tip and close/reopen/idempotence checks.
  Protocol, agent, session, and RPC tests/vet/race passed. The real permission
  browser workflow also kills isolated workers before and after an actual builtin
  write, then checks truthful public history and unchanged execution counts on
  explicit resume.

## BUG-126: Permission browser runner can leave misleading success artifacts

- **Status:** Resolved; expected-failure artifact regression verified.
- **Severity:** Low (P3, verification harness).
- **Evidence:** The new runner initially published its successful report before
  fixture shutdown/temporary cleanup, and a later failed run could leave that
  older report intact. Process exit status failed correctly, but the artifact
  could be mistaken for evidence of a successful current run.
- **Fix:** Invalidate prior success before compilation, record failed runs, and
  publish success only after successful manager teardown and cleanup. Separate
  output directories keep the fault-injection regression's seeded artifacts
  isolated from normal verification evidence.
- **Verification:** `node scripts/tests/browser/permission-workflow/failure-report.mjs`
  forces a missing-browser prerequisite, checks nonzero exit/no success output,
  and verifies replacement of a seeded stale report with failed status. The
  normal workflow passes with a final `passed` report and completed cleanup.

## BUG-127: Canceled web turns silently return to Ready without an outcome

- **Status:** Resolved; real-worker browser regression and final gates verified.
- **Severity:** Medium (P2).
- **Evidence:** A live session and its manager recovery hint both recorded a
  canceled turn without assistant text. Production rendering hid recovery copy
  for idle/canceled state and showed only Ready. The isolated real-worker browser
  regression failed with `empty canceled turn must display an explicit outcome`.
- **Fix:** Render explicit cancellation feedback from the observed recovery
  state, preserve it on explicit resume, and clear it when another turn starts.
  Keep draft/send authority unchanged and never retry automatically.
- **Verification:** The 51-assertion real-worker browser workflow verifies
  empty/partial cancellations, intentional Stop, subsequent streaming, resume,
  no replay, and short-viewport feedback. Full Go/vet, affected race, existing
  conversation/permission/stream-client suites, and Python/benchmark gates pass.
  Stored history does not establish which browser input caused the live cancel.

## BUG-128: Provider-originated abort can produce completed RPC status

- **Status:** Resolved — invocation-local terminal evidence and real-worker recovery verified.
- **Severity:** Medium (P2).
- **Evidence:** A local fake provider emitting `EvStreamDone/StopAborted` with a
  live caller context persists an aborted assistant and emits `EvAborted`, but
  the RPC completion was `completed`. `server.go` classified cancellation only
  from the prompt context/returned error; the agent's provider-abort path returns
  nil. A real-worker browser fixture waiting for canceled recovery instead timed
  out with Ready/Live and no error. Before the fix, the new protocol regression
  reproduced `completed` instead of `canceled` for ordinary prompts, Edit & resend
  and Regenerate (the regeneration setup first needed a text-bearing reply).
- **Remediation:** Capture explicit provider-abort evidence synchronously in the
  owning invocation's context, independent of event delivery or mutable history.
  Ordinary/content/mode prompts and admitted edits/regeneration pass this outcome
  to shared RPC completion. A nil-returning terminal provider abort now reports
  `canceled`; actual persistence/accounting errors retain failure precedence.
  Go prompt return semantics, automatic-goal behavior and existing caller-context
  cancellation classification are unchanged. Fresh captures isolate later turns.
- **Verification:** Focused agent/RPC/CLI regressions pass: legacy Go errors,
  rejected/unrelated-event isolation, all four ordinary content/mode variants,
  edit/regeneration, schema-valid completion, completion-error precedence and
  subsequent normal completion. `TestWebProviderAbortRealWorker` uses the actual
  subprocess App/Agent/RPC/client/manager path and reaches canceled recovery with
  no Stop request, then completes one explicit next prompt without automatic
  retry. Full Go tests/vet, affected-package race tests (agent/app/RPC/web,
  process/RPC clients, SDK and CLI), Python tests, benchmark guard, resource sync
  and SDK example pass on Go 1.27rc3. The stream-client Node VM suite passes
  51/51 tests. This is local
  fake-provider and manager-projection evidence, not a fresh browser-engine,
  live-provider or private-network certification.
- **Scope:** Not established as the cause of the reported live case, whose
  manager already recorded canceled, not completed. No real-provider replay.

## BUG-129: Double-clicking Send can cancel its newly admitted turn

- **Status:** Resolved; real-worker browser regression and final gates verified.
- **Severity:** Medium (P2).
- **Evidence:** Send and Stop occupy the same position. Native browser pointer
  events targeting Send followed by click-count 2 at that position target the
  replacement Stop and issue a real abort. The isolated real-worker regression
  failed with `double-clicking Send must not cancel the turn via its replacement
  Stop button`. This mechanism is reproduced; historical browser input was not
  retained, so it is not proof of the reported user's exact trigger.
- **Fix:** Ignore multi-click continuation on Stop (`detail > 1`), while retaining
  intentional single-click and keyboard/programmatic Stop activation.
- **Verification:** Real CDP pointer events with click-count 2 hit-test the enabled
  replacement Stop at unchanged coordinates without sending an abort; native
  single-click Stop still cancels, and subsequent turns stream and complete.
  The 51-assertion real-worker workflow and final regression gates pass. The
  scripted count proves handler behavior, not historical physical input timing.


## BUG-130: Multiline web drafts stay in a fixed one-line editor

- **Status:** Resolved — production-template composer tests passed 216 assertions; full layout matrix passed 1,890 reports / 26,524 assertions with zero failures.
- **Surface:** Activated web conversation composer
- **Evidence:** The previous textarea height was fixed at 36px with no content-height updater. Two explicit short lines require 52px under the reference 24px line height and 4px top inset, but remained in a 36px viewport.
- **Impact:** Multiline drafts were unnecessarily hidden behind editor scrolling, diverging from the reference composer layout.
- **Remediation:** The existing scroll owner measures the same textarea on input, restored drafts, programmatic clear, attention restoration and viewport changes. Preserve selection, focus, reader anchors and the measured seat/336px cap; do not change submission.
- **Regression:** Production-template composer-layout checks exercise growth, shrink, 240px heights, restored drafts, selection, scroll position and no mutation. The full harness matrix also checks 16px transcript vertical insets and long-draft Send reachability.

## BUG-131: Latest user message actions disappear after an assistant reply

- **Status:** Resolved — production-template composer tests passed 216 assertions; full layout matrix passed 1,890 reports / 26,524 assertions with zero failures.
- **Surface:** Hover-capable web conversation message actions
- **Evidence:** The old recency selector hid user actions whenever any later message existed. Reference recency depends on a later user message, not an assistant reply.
- **Remediation:** Scope user-action recency to later user siblings; retain assistant recency, focus/hover disclosure and non-hover visibility.
- **Regression:** Production-template composer-layout checks verify latest-user and latest-assistant opacity, older-user/assistant hiding and keyboard reveal.

## BUG-132: Restart recovery test assumes insertion order for tied registrations

- **Status:** Resolved
- **Surface:** `TestRuntimeRestartRecoveryWithRegistryAndExplicitReopen`
- **Evidence:** The full Go gate failed when two adjacent registrations shared a timestamp and UUID order differed from insertion order. Registry listing deliberately orders by `(created_at, id)`; the test compared that list against insertion order and incorrectly reported changed registrations.
- **Remediation:** Compare the persisted List projection immediately before shutdown with List after reopening; retain exact ordered project equality and all restart/no-replay checks. Production ordering is unchanged.
- **Verification:** Focused test passed 30 consecutive runs, then `go test ./...`, `go vet ./...`, and `go test -race ./internal/web ./cmd/snow` passed.


## BUG-133: New-conversation links navigate before the guarded workflow

- **Status:** Resolved — native-navigation follow-up verified.
- **Surface:** Web sidebar New conversation links and the generic workspace-link delegate
- **Evidence:** Direct production-browser checks originally reproduced a navigation GET at 1280/320px widths and 740/240px heights before the document-bubble guard ran. During work this dismissed the Stop/New confirmation; while disconnected it navigated instead of failing closed. No mutation POST was observed. Mocked shell routing alone did not reproduce target-level third-party dispatch. After that runtime was removed, retaining the capture-phase workaround caused the inverse conflict: the generic first-party delegate intercepted every React link before guarded New, session-selection, and current-project inspector callbacks. The exported-page gate reproduced 12 failures.
- **Remediation:** The historical third-party runtime required a capture-phase interception. The first-party navigator does not. Run the generic `data-snow-navigation` owner in document bubble phase, allowing React callbacks to prevent/stop ordinary clicks first; plain server-rendered links still reach the generic owner. Preserve modifier-key and native fallback behavior, close mobile navigation through its presentation owner, and do not duplicate switch or inspector authority.
- **Native-history follow-up:** A pending Back navigation changes the address before its workspace response commits. If a marked link from the still-visible old DOM superseded that read and then failed, both requests could terminate while the destination URL remained over the old DOM. Native traversal now keys bounded scroll state to committed DOM/history-entry IDs and a terminal owner reloads whenever the current entry ID does not match the committed DOM ID. This does not reload an ordinary failed request whose URL and DOM still agree, a superseded request with a newer owner, or the intentional pairing redirect.
- **Verification:** `node scripts/tests/browser/workspace-actions/browser.mjs` passes 108 assertions across all four layouts after reproducing the 12 native-migration failures. Active New retains the real confirmation, Cancel sends nothing, disconnected New does not navigate, current-project removal remains explicitly unchecked and presentation-only, and ordinary links still navigate. The production React gate passes 202 assertions across 28 scenarios, including rapid successful Back→Forward, stored/hash scroll restoration, and a held Back superseded by a failing stale-DOM link that must reconcile through an ordinary document load. The focused shell routing tests also pass.

## BUG-134: Width-handle wheel input does not scroll the transcript

- **Status:** Resolved
- **Surface:** Browser-local conversation width handles
- **Evidence:** Native Chrome wheel input scrolls 120px over the transcript center, but the same input over the hit-tested left width handle dispatches a real wheel event without changing transcript scrollTop. Fixed-position descendants do not join the native overflow scroll chain merely through DOM ancestry. The initial focused run passed 226 assertions with one failing scenario.
- **Remediation:** Keep fixed handles out of scroll-range calculation, but route their wheel input through the existing `SnowScroll` owner. Preserve upward reader intent, pixel/line/page deltas and Ctrl+wheel zoom; do not add a competing scroll controller.
- **Verification:** `SNOW_CHAT_WIDTH_EVIDENCE=1 node scripts/tests/browser/chat-width/run.mjs` passed 253 assertions across 23 reports, independently repeated after the fix. Native wheel works over both handles; native upward wheel releases following immediately and retains the reader anchor. Separately labeled synthetic tests verify line/page conversion, Ctrl+wheel exclusion and already-prevented input preserving both scroll position and following after bubbling. The original wheel-intent listener now also respects event cancellation.

## BUG-135: Layout test samples attention geometry before its baseline settles

- **Status:** Resolved (test synchronization; no production change)
- **Surface:** Browser layout matrix approval/question initialization
- **Evidence:** A screenshot-free full matrix intermittently failed the fixed-footer assertion for truncated approvals at 320×360/light and 768×240/light. The test awaited transport readiness and fonts, but neither guarantees the attention owner's asynchronous layout has settled before capturing its baseline. A focused diagnostic rerun passed; no production defect was established. The failed full run remains recorded rather than being treated as a pass.
- **Remediation:** Require a bounded, stable attention-seat geometry baseline before scrolling, while retaining the strict post-scroll fixed-footer and positive-scroll assertions. Record before/after footer, body and seat measurements for approval diagnostics; fail if baseline never settles.
- **Verification:** The final matrix passed 1,890 reports / 26,524 assertions. Additional 320px/light and 768px/light runs passed 270 reports / 3,756 assertions, including both previously failing short-height cases, with the strict footer checks intact. The real permission workflow separately passed 74 assertions. Approval reports now retain concrete before/after measurements for future diagnosis.

## BUG-136: Re-enhancing saved tool history loses its omission notice

- **Status:** Resolved
- **Surface:** Saved tool history presentation
- **Evidence:** After the browser retained the bounded 64-call projection, repeated `SnowMessages.enhance()` recomputed omission from that already-trimmed DOM and hid the count-truncation notice.
- **Fix:** Preserve the existing omission indication while enhancing retained public history; do not pretend the bounded DOM is complete history.
- **Verification:** The focused tool-row browser suite includes repeated enhancement of over-limit saved history. Independent final-source runs passed 360 assertions across 12 reports, including a repeat capturing open/closed screenshot evidence under `dist/tool-row-evidence/`.

## BUG-137: Web startup override prevents restoration of saved permission policy

- **Status:** Resolved
- **Surface:** Editable web session permission policy
- **Evidence:** The original worker launch passes `--permission ask`, setting the app's explicit permission override. Session binding then skips persisted policy and decisions. Synthetic workflow fixtures restored a policy map and did not exercise this real startup behavior. Selecting Deny/Allow, switching away and back, or explicitly reopening therefore cannot meet the new persistence contract with those launch options.
- **Remediation:** Separate the new-session Ask default from an explicit CLI override. Preserve ordinary explicit CLI override semantics, and verify real worker/app restoration with the exact web launch options rather than a synthetic policy map.
- **Verification:** `TestWebPolicyRealWorkerRestoresPersistedModes` first reproduced both Deny and Allow restoring as Ask, then passed after the launch override was removed. It exercises emitted worker arguments, production option parsing, the real app/RPC/session path and actual read/write authorization in temporary projects with a fake provider. It covers new-session Ask, switch-back/restart restoration, read-risk access, Deny rejecting writes, Allow executing writes, and Ask requiring an explicit reply. The independent aggregate `go test ./...`, `go vet ./...`, and affected `go test -race ./internal/web ./cmd/snow` gates also passed.

## BUG-138: New permission chip wraps the 320px composer toolbar

- **Status:** Resolved
- **Surface:** Narrow live composer with permission, collaboration, model and context controls
- **Evidence:** The first full layout matrix found 14 failures at 320px: the toolbar wrapped and increased the idle composer from approximately 98px to 138px. No controls were removed to hide the regression.
- **Fix:** Reduce narrow inter-control gaps and use the shorter visible Plan label at narrow container widths, retaining the full collaboration mode in the accessible name and tooltip. Permission labels already collapse to the shield at narrow widths.
- **Verification:** The 320px/light repeat passed 135 reports / 1,800 assertions. The full final screenshot matrix passed 1,890 reports / 26,524 assertions with no failures (`dist/tools-policy-final/`). Existing composer, workspace, conversation, width, permission-execution and live-stream suites also passed.

## BUG-139: Live tools from multiple prompts accumulate after the final answer

- **Status:** Resolved
- **Surface:** Web live conversation timeline
- **Evidence:** User screenshots show two file searches from the first request and a write from the second request together below the second final answer. `projectActivity` retains a runtime-wide activity list without a chronological message/step anchor; the browser always renders it after the transcript. Styling the rows did not fix their ordering or ownership.
- **Remediation:** Emit bounded, stable live tool-step markers at the actual event position and bind activities to those markers. Render matched groups inline, preserving ordering across text, consecutive calls and later prompts. Preserve authoritative saved history association and show unassociated legacy/orphan activity truthfully rather than guessing an owner.
- **Verification:** `TestWebToolTimelineRealWorkerMultiplePrompts` passes actual manager/app/RPC read/write/read turns with a reused provider call ID and unchanged saved chronology after close/reopen; the race-enabled test also passed ten repetitions. Independent browser coverage passes 288 assertions / eight reports, including two turns, interleaved text/tools, stable disclosures/focus, bounds, fallback and saved ownership. Final layout: 1,890 reports / 26,524 assertions / zero failures. Existing tool-row, conversation, composer, inspection, workspace, width, permission-policy, real permission-execution and live-stream browser checks passed, as did full Go/vet, affected race, Python and benchmark gates. Sampled two-turn screenshot evidence is under `dist/tool-timeline-evidence/`; layout evidence is under `dist/tool-timeline-layout/`.

## BUG-140: Stop cannot request cancellation during prompt admission

- **Status:** Resolved
- **Surface:** Web composer/attention Stop controls and prompt admission
- **Evidence:** The prompt POST marks the browser action busy; Stop remains hidden until a running snapshot, then disabled while the POST is pending and until refresh. The backend also holds its nonqueued operation gate through the RPC admission acknowledgment, so merely enabling the legacy Abort button would fail with busy. Ordinary post-admission Stop already exists.
- **Remediation:** Add explicit per-turn cancellation identity and a narrowly scoped cancellation-intent path, separate from prompt mutation state. A verified running turn can receive one cancellation request during admission; dispatch remains serialized and revalidates the same turn before aborting. Duplicate/stale intent must never stop a later prompt, imply rollback, or report idle before definitive completion.
- **Integration finding:** A full-suite real-worker run exposed a second ordering race: `prompt_completed` can arrive before the abort RPC acknowledgment releases the control gate. Clearing the cancellation latch at completion advertised Send as ready too early, and the next explicit prompt was rejected as busy. Cancellation must stay pending until both completion and cancellation dispatch retire; tests must not hide this with sleeps or mutation retries.
- **Verification:** `TestWebCancelRealWorkerDuringAdmission` passes ten race-enabled repetitions with delayed admission and a separately buffered abort acknowledgment, letting completion pass before that acknowledgment. It verifies pending readiness, one abort per turn and stale-token rejection without sleeps or retries. Deterministic backend tests cover cancellation-dispatch retirement, replacement and shutdown. Independent browser checks pass 840 assertions / four reports; the full layout passes 1,932 reports / 27,298 assertions / zero failures. Existing conversation/composer/inspection/workspace, tool, permission, actual live-stream and width suites all pass on the final implementation. Full Go/vet, affected race, Python and benchmark gates passed. Evidence: `dist/stop-reuse-evidence/` and `dist/stop-reuse-final-layout/`.

## BUG-141: First-run layout evidence reporting masks fixture errors

- **Status:** Resolved
- **Surface:** Browser layout test runner
- **Evidence:** Adding the explicit cancellation and saved-user exports triggered the runner's strict manifest check. With a new output directory, its final report write threw `ENOENT`, hiding the original manifest failure.
- **Remediation:** Create the evidence directory before entering the export/manifest operation. Keep explicit legacy/cancel workflow fixtures separate, and include the saved-user surface in the layout manifest.
- **Verification:** `python3 -m unittest discover -s scripts/tests -p 'test_harness_layout_errors.py' -v` passes a deterministic empty-manifest fixture with a nonexistent evidence directory. It preserves the original manifest error and writes an empty failure report without launching Chrome or using the network.

## BUG-142: Message editing appends a copy instead of replacing the continuation

- **Status:** Resolved
- **Surface:** Web user-message editing
- **Evidence:** The existing Edit & continue action copies text into a normal prompt and appends it at the end. The user clarified that editing should replace the selected user message on the visible active path, remove its following replies from view, and regenerate in the same chat.
- **Remediation:** Resolve authoritative editable source rather than trusting displayed text or local IDs. Add a typed, bound prepare/commit operation with atomic history-transition and replacement-turn admission. Preserve the original append-only history internally and the session identity/title, replace the public active-path projection, and retire stale browser/event authority. Never automatically retry, create another chat, or claim that earlier tool effects were undone.
- **Verification:** Core memory/SQLite tests verify exact identity, preserved original branches, first/middle/latest replacement, turn counts, stale/replayed tokens, atomic admission, plugin veto/lock-safe notification, cancellation, rollback and durable-write-then-error uncertainty. History traversal is preflight-bounded before whole-path cloning/decoding. The independent real app/RPC worker verifies provider context, same chat/title and reopen, with ten race repetitions; a real-tool test confirms removed Write rows do not undo the file or change permission authority. Native Chrome passes 1,208 assertions in four reports; full layout passes 1,932 reports / 27,298 assertions / zero failures. Existing Stop, chronology, tools, permission, live-stream, workspace and layout workflows pass. Final Go/vet, affected race, Python (67 tests), benchmarks, resource sync and isolated fake-provider SDK example pass. A temporary bundled-document drift failure was corrected before the final gate. Evidence: `dist/message-edit-evidence/` and `dist/message-edit-final-layout/`.

## BUG-143: Mixed plan replies advertise unsupported live regeneration

- **Status:** Resolved
- **Surface:** Live assistant regeneration eligibility
- **Evidence:** A single assistant response with text, a structured plan, then trailing text promotes the trailing live row to `CanRegenerate`. Core preparation rejects the same plan-bearing assistant message, and reopening removes the action. Splitting presentation rows does not establish an independently regeneratable assistant response.
- **Remediation:** Retain plan-bearing eligibility across the owning assistant response, including buffered replacement events, and reset it only at a valid subsequent response boundary. Match live eligibility to saved/core validation without guessing persisted ownership from text or display positions.
- **Verification:** Direct and buffered text/plan/text regressions pass, including a subsequent plain reply after a new tool boundary and duplicate old tool events. The actual app/RPC worker verifies mixed Plan Mode regeneration and reopened ineligibility; plain replies remain eligible. Core and saved Web projection now share the local-shape predicate, with additional live/saved whitespace regressions. The actual worker suite passes three race repetitions; independent native regeneration passes 1,748 assertions across four reports.

## BUG-144: Successful regeneration drops keyboard focus onto the page body

- **Status:** Resolved
- **Surface:** Regeneration confirmation and replacement acknowledgment
- **Evidence:** Native Chrome first/latest regeneration removes the selected action after success and leaves `document.activeElement` on the body. The failure repeats at 320/1280 pixels in dark/light themes; typing in the composer during the pending operation avoids it.
- **Remediation:** After verified projection replacement, supply a surviving focus target only when prior focus was lost. Preserve active typing, composer contents and native selection; do not steal focus from a control deliberately chosen during the request.
- **Verification:** The initial native suite recorded eight focus failures. A surviving-composer fallback now applies only after verified replacement strands focus on the body; active typing and native selection remain intact. Independent final native verification passes 1,748 assertions / four viewport-theme reports / zero failures. Evidence is retained under `dist/regenerate-evidence/`; the full layout matrix also passes 27,298 assertions.

## BUG-145: Reopened plain replies lose regeneration eligibility metadata

- **Status:** Resolved
- **Surface:** Public saved-history projection and regeneration
- **Evidence:** The public messages-page projection omits assistant stop reason. Web eligibility correctly refuses to guess it, so reopening a chat removes Regenerate from otherwise eligible completed plain replies. Initial reopen tests checked timeline persistence but not positive regeneration eligibility.
- **Remediation:** Preserve bounded safe terminal/eligibility metadata without exposing raw errors or private content. Verify an actual reopened reply can prepare and commit a regeneration, and that error, partial, tool and plan-bearing replies do not gain false eligibility.
- **Verification:** The parent first reproduced lost eligibility in an actual reopened worker. Public-projection tests now verify known stop enums and failure bits, private/raw-error redaction, unsupported-content suppression, source immutability and byte budgets. Actual first/middle/latest/identical worker cases reopen, select an exact saved assistant, prepare and commit again, and verify exact original provider context with no duplicated prompt. The worker suite passes three race repetitions; source-only independent review found no remaining projection issue.

## BUG-146: Goal rejection test races automatic goal execution

- **Status:** Resolved
- **Surface:** Historical-revision regression fixture
- **Evidence:** The full affected race gate failed `TestMessageEditRejectsNonterminalGoalWithoutChangingDeferral` with “read-only preparation changed goal/history.” The fixture calls `App.CreateGoal`, which starts automatic goal execution, then compares branch tips across preparation while that independent runner can append history.
- **Remediation:** Establish deterministic nonterminal/deferred goal state without starting unrelated background execution. Keep strong assertions that rejected preparation preserves both history and goal deferral; do not hide the race with sleeps or retries.
- **Verification:** The original fixture reproduced twice in 100 race repetitions. The fixed fixture creates the goal without continuation and exercises both deferral states, specifically requires nonterminal-goal rejection, compares the complete goal/messages/branch tip/deferral, and requires an idle agent. All 100 post-fix race repetitions, the app package suite and vet pass. This is a test-fixture defect, not evidence that rejected preparation mutates history.

## BUG-147: Switching chats strands retained queue work

- **Status:** Resolved (verified)
- **Surface:** Queue review and session transitions
- **Evidence:** Source review identifies enqueue → cancel → retained review → session switch: core review state remains and blocks new prompts, while Web clears the old queue projection/token. A focused Web test also reproduced rejected ordinary Prompt resetting root state before the core rejection, stranding the same discard authority. The user can no longer remove retained items through their original authority.
- **Remediation:** Reject session/branch transitions before mutation while controlled pending or review work remains, preserving accessible explicit review/removal. Do not silently discard the projection or retarget retained requests into another session.
- **Verification:** Focused core/Web transition and rejected-Prompt authority tests pass. Actual-worker Stop/failure retention, rejected session creation, explicit removal and fresh-prompt checks pass. Final full Go/vet and broad internal/CLI/public-package race checks pass.

## BUG-148: Queue UI retained stale root rows and skipped fresh validation

- **Status:** Resolved (verified)
- **Surface:** Browser queue projection and local admission controls
- **Evidence:** The initial native matrix reported 820 passing assertions and 16 failures across four reports. Root-token rotation with a reset public queue revision retained an old pending row; same-token/revision snapshots skipped fresh validation for duplicate IDs, unsupported item states and updated capacity. These were projection/admission-attempt defects; no backend root or CAS boundary bypass was demonstrated.
- **Remediation:** Treat public queue revisions as token-scoped, retire old-root acknowledgments, and validate each newly received queue object and its bounds before enabling actions. Keep whole-snapshot revision ordering independent from per-queue control revisions.
- **Verification:** Final independent native Chrome matrix passes 1,124 assertions across four reports (320/1280, dark/light), including all four original cases and final starting/uncertain states. Existing Edit, Regenerate, Stop, tools, permissions and streaming suites pass; the layout matrix passes 27,298 assertions across 1,932 reports. Evidence: `dist/queue-next-evidence/` and `dist/queue-next-final-layout/`.

## BUG-152: Branch-restore mode test races an empty automatic worker

- **Status:** Resolved (verified; fixture synchronization only)
- **Evidence:** `go test ./internal/app -run '^TestBranchRestoreTargetModeIsBoundAndAppliedWithoutExecution$' -count=30` reproduced intermittent failures at `branch_restore_test.go:356`: `agent: branch restore requires an idle agent`. Both stores can fail; SQLite reproduced frequently.
- **Cause:** The fixture switches to Default mode immediately before restore preparation. Legacy `SetMode(Default)` schedules `ContinueGoal`, which briefly owns `autoRunning` even when no goal exists. The new restore admission correctly rejects that temporary worker instead of preempting it.
- **Impact:** Nondeterministic verification failure; the rejected restore does not mutate the active version. A single full-suite rerun can pass and does not establish stability.
- **Remediation:** The version-restore fixture explicitly calls `Agent.WaitGoal(t.Context())` after changing to Default mode, joining the legacy automatic worker before preparation. Strict restore admission and explicit goal-run ownership remain unchanged. Avoiding empty legacy workers would be a separate behavior change.
- **Verification:** After the fixture-only synchronization fix, `go test ./internal/app -run '^TestBranchRestoreTargetModeIsBoundAndAppliedWithoutExecution$' -count=50` passed (1.592s), and `go test -race ./internal/app -run '^TestBranchRestoreTargetModeIsBoundAndAppliedWithoutExecution$' -count=10` passed (3.838s). Both commands exercise Memory and SQLite cases.
