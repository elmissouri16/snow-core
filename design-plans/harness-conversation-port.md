# Port Harness conversation composition, not just landing geometry

Written against Snow db7b2185c86a6506006ff797dce3129e2de385f0 plus the uncommitted web implementation; reference c291e7961a515f6d7af9304e7fd1d257929aef26.

## Evidence chain
- Surface: Snow activated conversation (`templates/live.html`, `static/app.js`, `static/harness.css`). User supplied a screenshot rejecting its oversized form and conversation presentation.
- Design evidence: reference `packages/client/ui-conversation/src/client/skeleton/ConversationRoot.module.css`, `InputBar.module.css`; `ui-chat/src/client/chat/{ChatView,MessageItem,MessageIconActions}.module.css`; `ui-primitives/src/markdown/{MarkdownText,CodeBlock}.module.css`. Local research checkout is `dist/harness-reference/`.
- Runtime: reference `ConversationRoot → ChatView + InputBar`, inspected through the running authenticated `http://127.0.0.1:3080/?fixture` in-memory transport. Its non-persisting welcome overlay was hidden only in this synthetic browser view to inspect the actual underlying conversation. No real runtime or prompt was started.
- Problem: Snow uses a fixed 820px width for everything, separate large header/status bars, 30px flow gaps and card labels; the real conversation uses a shared adaptive width, distinct message flow and controls inside the composer.
- Uncertainty: Snow's current public activity DTO does not necessarily expose enough history attribution to reproduce every turn-local upstream process group. Never invent ordering or private reasoning.

## Decision and reuse
Port the resident conversation composition and typography, adapting React/CSS-module source into Snow templates/plain JS. Keep Snow's sanitized bounded Markdown and serial RPC controls. Include upstream MIT notice for adapted source; retain Snow branding.

- Transcript W = clamp(680px, .64 × conversation column width, 920px), composer W+32px; narrow clearances 32px transcript / 16px composer. Observe actual column width.
- Header uses compact current-session title and separate real actions. No fake Trajectory tab; upstream omits tabs when only one view exists.
- Docked input 36px minimum, hero52px; editor14/24, toolbar inside card, radius22, gap12. Send34px; mode/permission left, model/context right. Preserve current documented send shortcut and IME behavior unless explicitly changed with tests.
- Flow gap16, user bubble r22/padding10x16/max min(.702W,82%); assistant unboxed prose. Do not show uppercase role labels as visual headers. Keep accessible roles.
- Markdown body14/24; h1 21/30, h2 19/28, h3 18/26; lists18px inset/6px item gaps; inline code r6; code block r12 with real copy controls.
- Public tools use compact disclosures; never forge historical turn association or reveal private reasoning.

## Changes and validation
- `internal/web/templates/live.html`, new message partial if needed: new composition; preserve form routes/CSRF/instance identity, mounted draft and visible permission/error/reconnect states.
- `internal/web/static/app.js`, Markdown presentation assets: real message copy actions with HTMX cleanup; no second runtime loop. `conversation-width.js` owns column measurement and browser-local width interaction; `scroll.js` retains scroll and input-sizing authority.
- `internal/web/static/harness.css`: replace prior live composition rather than piling on unexplained overrides.
- Validate real exported templates with long Markdown, code/tables, active/idle, approvals/questions, long history, narrow composer, open menus, resize and inspector. Preserve all existing workflow/race tests; update only deliberately replaced presentation assertions.
- Run `go test ./internal/web`, browser harness-layout/conversation-workflow/inspection-race, JS syntax, full repository gates and install after passing.

## Scope and stop conditions
Exclude unsupported queues, attachments, model effort, trajectory, forks and private reasoning. Stop and inspect public DTO ownership if faithful ordering requires backend changes. Never create faux feature controls merely to fill reference seats.

## Documentation
Update `docs/using-snow.md` and the current-design section of `docs/web-manager-implementation-plan.md` after verification. Previous fixed-width/controls-above-composer claims are superseded by this port.

## Verified chat-geometry follow-through

Current source now implements the remaining measured reference contracts:
content-height-driven multiline editing (36px normal floor, viewport-aware cap
of at most 336px), 16px transcript vertical insets at every width, and latest-user
actions that remain visible when an assistant follows. Sizing belongs to the
existing scroll owner and preserves the same input node, draft, selection,
reader anchor and bounded editor scroll position. Short seats retain a smaller
floor rather than hiding Send.

Production-template composer tests passed 216 assertions across desktop/mobile,
normal/short heights and both themes. The full layout matrix passed 1,890 states
and 26,524 assertions with no failures. Streaming/cancellation (51), real
permission/recovery (74) and conversation workflow (1,288) checks also passed.
These results establish the supported port, not private trajectory, tool-turn
attribution or unsupported controls.

## Browser-local width interaction

The reference's width gesture is now implemented in `conversation-width.js`:
left/right pointer capture, symmetric 2× outward travel, a 640px custom floor,
and a column-minus-176px maximum. The existing adaptive clamp remains the unset
default; the composer continues sharing transcript width plus 32px. A narrower
window or inspector changes only the displayed clamp, not the saved preference.
Only horizontal travel commits a gesture. Escape, pointer cancellation, capture
loss, blur, and disposal abandon it. Home restores automatic width; keyboard
arrows move the selected edge, with larger Shift steps.

`SnowWidth` now owns the column measurement previously in `SnowMessages`.
`SnowScroll` remains the only reader/follow and composer-sizing owner. Width
updates use its before/after reconciliation hooks. Fixed handle descendants do
not extend the scroll range, but native Chrome testing established that fixed
ancestry does not route overflow scrolling: their explicit wheel bridge belongs
to `SnowScroll`, preserving upward intent and Ctrl+wheel zoom. Handles disappear
from hit testing and tab order when margins do not fit. No server preference,
project setting, permission action or runtime request is added.

The focused production-browser gate (`scripts/tests/browser/chat-width/run.mjs`)
passed 253 assertions across 23 reports, independently repeated after the wheel
fix. It uses native CDP pointer, touch-cancellation, keyboard and pixel-wheel
input; synthetic wheel-mode/cancellation tests are labeled separately. Coverage
includes both edges, pointer capture beyond the original strip, reader anchors,
clamped preferences without destructive persistence, inspector/window resizing,
storage failure, and disposal/remounting. Evidence is under
`dist/chat-width-evidence/` when `SNOW_CHAT_WIDTH_EVIDENCE=1` is set.

### Tool disclosures and session permissions

The next increment ports the reference's compact 24px tool-row treatment:
action titles and tool-kind icons, no repeated wire-name summary, quiet accessible
success status, and visible running/error/rejected/unknown outcomes. Expansion
shows bounded literal public output only. This increment initially retained a
quiet runtime provenance caption instead of inventing a saved-message owner;
BUG-139 below corrects the remaining live timeline placement. Saved history
retains its authoritative association and omission notices on re-enhance.

Following explicit user authorization, the composer now exposes Snow's real
Ask/Deny/Allow session policies, not upstream sandbox presets. Allow requires a
fresh unchecked acknowledgment; policy changes require a verified idle bound
session, an exact typed mutation and an authoritative response. Stale, busy,
pending-interaction, disconnected and uncertain states cannot authorize changes.
New sessions start Ask; explicit resume/switch restores saved policy. Real worker
coverage caught and removed the old launch override that prevented restoration,
without changing explicit CLI override behavior. See `docs/security.md` and
`docs/using-snow.md` for the current authority and persistence contract.

The composer alone uses `novalidate`: its existing application-owned empty and
UTF-8 size guards remain. Blank click/Ctrl+Enter produces neither a native invalid
event nor a prompt request. This verifies the behavior, not the unproven exact
origin of the originally reported screenshot popup.

Final independent verification passed the policy suite (1,104 assertions / eight
reports), tool rows (360 / 12), and full screenshot layout matrix (26,524 / 1,890).
The 320px repeat passed another 1,800 assertions / 135 reports after correcting
narrow-toolbar wrapping. Existing composer, workspace, conversation, width,
permission-execution and live-stream suites also passed, along with full Go/vet,
affected Go race, Python and benchmark gates. Screenshot evidence is under
`dist/tools-policy-final/` and optionally `dist/tool-row-evidence/`; sampled
composer and compact tool-row captures were visually inspected. This is not a
claim of pixel-exact upstream parity or manual screen-reader verification.

### Chronological live tool steps (BUG-139)

The runtime now inserts a stable local `tool_activity` marker when a new root
call first appears in the accepted event stream. Consecutive calls share a step;
intervening text or a new prompt starts another. Each public activity carries its
original marker ID. Synthetic results without a start event also establish a
boundary; duplicate starts/ends do not split or relocate subsequent text.
These markers describe live event positions, not invented persisted message IDs.
The existing saved-history owner projection is unchanged.

The browser mounts keyed, heading-free tool groups among the transcript messages
and routes activities only through exact marker matches. Fully associated calls
have no duplicate global footer. Legacy/missing/evicted owners retain a truthful
unassociated fallback, never the nearest or latest assistant as a guessed owner.
Disclosure, output-node, open-state and focus identities survive snapshot updates
and movement into/out of fallback. Count/byte truncation notices remain visible;
existing activity and message bounds remain enforced.

Independent Chrome coverage passes 288 assertions over eight live/saved,
320/1280px, dark/light reports, including two explicit user turns, interleaved
text/calls, cancellation/error/unknown states, reused synthetic provider IDs,
orphan eviction/restoration and native pointer/keyboard/accessibility-tree checks.
`TestWebToolTimelineRealWorkerMultiplePrompts` separately verifies actual emitted
worker options, the app/RPC stream and builtin read/write execution across three
prompts with a reused provider call ID; closing/reopening yields the same saved
chronology. Its private fixture initially failed its existing directory-mode
startup guard, then passed after setting the required `0700` mode—this was not a
production startup failure. Full Go/vet and affected race gates passed.

`SNOW_TOOL_TIMELINE_EVIDENCE=1 node scripts/tests/browser/tool-timeline/run.mjs`
exports 16 illustrative open/closed captures under `dist/tool-timeline-evidence/`.
The desktop two-turn capture was visually inspected; fixture prose uses escaped
paragraph HTML, matching the normal server Markdown shape instead of the shared
mock transport's code-block fallback. No running user manager or provider was
used, and no original user prompt was replayed.

Final integration gates passed: 1,890 layout reports / 26,524 assertions; existing
tool-row, conversation, composer, inspection, workspace, width, permission-policy,
real permission-execution and live-stream browser suites; full Go/vet, affected
race, Python and benchmark checks. The real-worker chronology test also passed
ten race-enabled repetitions. Full layout artifacts are in
`dist/tool-timeline-layout/`.

### Stop and initial copy-to-continue increment (BUG-140)

The initial implementation misread “same conversation” as copy-and-append. That
editing interpretation is superseded by BUG-142 below; its Stop behavior remains.
Each complete user row originally exposed **Edit & continue**. It loaded
literal public text into the existing composer; only explicit Send appends a new
prompt at the current tip. Original history stays unchanged. Cancel restores the
previous draft and selection, and a successful send restores that draft only if
the user has not typed something else meanwhile. Draft/reuse maps share a bounded
16-conversation tab-memory lifetime. Truncated/non-user/inactive source does not
become an executable replacement prompt.

Stop has its own action channel. An optional capability supplies a local opaque
turn token and cancellation-request latch; a verified running snapshot authorizes
one cancellation even while prompt admission is pending. The manager serializes
actual RPC abort with its existing operation gate, revalidates the same turn,
and joins the task at shutdown. No prompt is queued or replayed. An idle snapshot
alone is insufficient if the abort RPC still owns the gate: cancellation remains
latched until both completion and dispatcher retirement. Only then may the UI
announce Ready. A later token retires stale browser intent without automatically
stopping the new turn. Failed cancellation retains uncertainty but permits an
explicit confirmed Close for recovery, never automatic reopening.

The real app/RPC test holds prompt admission and separately buffers an abort
acknowledgment, letting `prompt_completed` pass first. It verifies one abort per
turn, no premature next-prompt admission, immediate admission after publicly
advertised readiness, and rejection of an old token on a later turn. Ten race
repetitions passed. Initial fixture setup/counting errors were corrected; the
full-suite busy-after-idle failure was a real readiness race and drove the final
latch-retirement fix rather than a test sleep or mutation retry.

Independent native Chrome tests pass 840 assertions across four dark/light,
320/1280px reports, including per-row selection, drafts, no branch/mutation on
open or dismiss, attention, admission, completion-before-abort-ack, stale/unknown
state and failed-worker Close recovery. Eight illustrative screenshots are in
`dist/stop-reuse-evidence/`; narrow editing and desktop Stopping captures were
visually inspected. Fixture assistant text uses the shared mock's code-block
projection; those captures demonstrate controls, not exact Markdown rendering.
The complete layout matrix, including the new inactive user surface, passes
1,932 reports / 27,298 assertions with zero failures under
`dist/stop-reuse-final-layout/`. Legacy Abort and the new token-capable workflow
have separate explicit Go exports. No user manager, provider or original prompt
was used during verification.

### Historical Edit & resend (BUG-142)

The clarified behavior replaces the selected user on the active path and drops
its following replies/tools from view before regenerating. It is not an edited
copy appended at the bottom and does not create a second chat. Preparation is
read-only and obtains complete text from an exact saved identity; live rows bind
to their own accepted root-turn marker, never a guessed message index or text.
Commit revalidates a short-lived single-use token, runs prompt/plugin validation,
and changes the same-session branch plus replacement-turn admission atomically.
The original append-only path remains internally; executed effects are not undone.

The durable public-history ACK precedes provider work. Web projection replacement
and bounded event replay preserve even fast completion while rotating instance
and SSE authority. Only a guaranteed precommit rejection permits the unchanged
old view to remain Ready; ambiguous append, rollback or ACK failure fails closed.
Exact durable entry probes cover stores that commit before returning an append
error. Notifications run after admission/plugin locks release. Edit history reads
have preflight traversal/byte limits, not an unbounded BranchEntries fallback.

Verification includes native first/middle/latest edits, full-text preparation,
local cancel/draft/selection restoration, no copy-and-append fallback, stale and
unknown outcomes, new-instance Stop, and old SSE retirement: 1,208 assertions in
four 320/1280px dark/light reports. Twelve screenshots are retained under
`dist/message-edit-evidence/`; the narrow middle-edit/stopped result was visually
inspected. The independent real app/RPC worker verifies exact provider context,
unchanged chat/title, active-path reopen, and ten race-enabled repetitions. A
separate real-tool test verifies deleted Write rows do not undo a file write or
change the permission policy. Core tests cover memory/SQLite history retention,
first-turn statistics, vetoes, concurrency, rollback and durable-write errors.
The full layout remains 1,932 reports / 27,298 assertions / zero failures under
`dist/message-edit-final-layout/`; existing Stop, timeline, tools, permissions,
live-stream, composer, conversation, workspace and width suites pass. No original
user prompt, user manager or real provider was used.

### Confirmed Regenerate reply

Eligible terminal assistant replies now expose **Regenerate**. A read-only,
identity-bound preparation precedes a native confirmation dialog. Confirmation
restarts the whole owning turn from its server-held original prompt—including
its earlier text and tool work—and replaces following conversation in the same
chat. There is one unchanged-text user prompt on the new active path, not a
copy appended at the bottom. The shared historical-edit transaction retains
old history, current permissions, atomic admission and bounded public events.
Regeneration tokens cannot be used as editing tokens or carry replacement text.

The warning explicitly states that tools may run again and earlier changes are
not undone. Draft text/selection and concurrent typing survive preparation,
cancel and commit. Successful replacement restores a surviving focus target only
if removal stranded focus on the body. Unknown outcomes never replay or fall
back to normal prompting; Stop applies to the newly admitted turn.

Verification found and corrected mixed plan/text eligibility (BUG-143), native
focus loss (BUG-144), and lost saved terminal metadata (BUG-145). Public history
retains safe lifecycle metadata without raw errors or private output; core and
saved Web projection share a conservative local-shape predicate while exact
ownership remains an authoritative preparation check. A separate full-gate
fixture race was corrected with deterministic goal setup (BUG-146).

Independent native Chrome verification passes **1,748 assertions / four reports /
zero failures**; 12 screenshots are retained in `dist/regenerate-evidence/`.
The layout matrix passes **1,932 reports / 27,298 assertions / zero failures**
under `dist/regenerate-final-layout/`. Existing edit, Stop, tools, permissions,
streaming, conversation, composer, inspection, workspace and width suites pass.
Actual app/RPC worker tests cover first/middle/latest/identical prompts, Stop,
positive reopened-source preparation/commit, exact provider context, mixed plans,
and respecting current Deny while retaining prior real tool effects. The worker
suite passes three race-enabled repetitions. All inputs/providers/projects are
fictional and isolated; the user's manager was not touched. The final full Go
suite, vet, affected-package race checks, 67 Python tests, benchmark budgets,
bundled-resource check, diff check and isolated fake-provider SDK example pass.

## Compact context controls (BUG-172)

The original attachment scope exclusion above is historical: BUG-170 added real
bounded file attachments and file/skill discovery through existing RPC owners.
Their first presentation added a separate toolbar and oversized suggestion cards;
functional passes did not establish fidelity to the requested reference.

The correction puts 28px plus/paperclip controls inside the existing single bottom
action row. Plus uses `SnowMenus` and performs no discovery until a row is chosen;
mentions retain the textarea-focused listbox, now aligned to composer width.
Files have line icons, compact names and folder-navigation chevrons. Skills retain
one-line summaries and full native titles instead of paragraph-height cards.
Only idle guidance is visually hidden, not connection/outcome/error state or
explicit Queue next. Native text editing, attachment privacy notices, authority
fencing, permission takeover and disposal remain with their existing owners.

Production-browser checks measure a 98px idle card and 42px action row at both
320×900 and 1280×900, shrinking the card to 78px at 320×240. Skill rows are 56px;
the popup is composer-width and capped at 320px or available viewport clearance.
The full context suite passes 1,600 functional plus 232 visual assertions; focused
composer sizing passes 252 assertions. Deterministic screenshots and measured
geometry are retained under `dist/compact-composer/`. These checks establish
composition, density and behavior, not pixel-identical cross-browser rendering.
