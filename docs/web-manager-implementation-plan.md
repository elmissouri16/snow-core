# Web manager research and implementation plan

> **Current local implementation:** direct-loopback projects and persistent browser
> access; explicitly activated RPC conversations with Markdown, public tool history,
> approvals/questions, Stop, model/session/mode controls, Queue next, Edit & resend
> and Regenerate; snapshot SSE and read-only Files/Changes. The current source
> increment adds read-only Activity, manager-only organization, bounded Versions
> with explicit idle Restore, exact-source Start/Resume goals, and managed-process
> inspection/Stop with the process-tool bundle enabled. Extensions and subagents
> remain disabled. The integrated Go/race/vet/Python/benchmark and production
> browser gates verify this bounded local increment, not the entire target roadmap.
> A subsequent local-source increment added loopback TLS alongside host control,
> reasoning/history/compaction/steering, cost estimates and durable CREATE/clone.
> That TLS path was later superseded and removed by the HTTP-only LAN contract
> described below. Its recorded local gates include all three native matrices and fresh
> native baseline/layout/conversation checks. These are not reusable CI/release
> approval or live-provider coverage; earlier totals retain their milestone scope.
> The source now implements a deliberately small Phase 4 network boundary:
> automatic exact-origin trusted-LAN HTTP on one private interface plus localhost
> on the same port, with an offline loopback fallback. The obsolete TLS,
> certificate/CA, saved-profile, DNS-origin, trusted-proxy, forwarding-header,
> browser API-key, and hidden network-override paths were removed. Automatic
> recovery and broader real-device acceptance remain future work. Current behavior
> and limits are canonical in [Using Snow](using-snow.md#try-the-local-web-manager-shell)
> and the [security guide](security.md#local-web-manager-preview).


This document tracks the optional browser-based Snow manager for one operator
on one host. It records DeepSeek Harness research, Snow integration evidence,
product and visual design, remote-access safeguards, and implementation-sized
work packages. The status section describes delivered increments; later design
sections are a handoff for remaining implementation, not an availability promise.

> **Note:** The complete target design remains proposed. The bounded local source
> implementation and reusable RPC client below are distinct from future flags,
> routes, limits and runtime behaviors. Historical increment notes are not proof
> that every current verification gate has passed.
> Research date: 2026-09-11. Snow baseline:
> `db7b2185c86a6506006ff797dce3129e2de385f0`.

## Commit checkpoint and remaining work

This checkpoint includes the accumulated local web-manager, RPC/public protocol,
session/goal/process controls, React frontend, browser fixtures and documentation
work since `db7b218`. It is not a release or a claim that the complete target
manager is finished. In particular, the completed Goal composer toggle, direct
Thinking picker and no-op compaction feedback should not be implemented again.
The latest no-op regression checks the whole transcript and scroll position,
not just composer geometry; its native matrix passes 404 assertions and the
runtime-only responsive matrix passes 84 reports.

### Still to do

| Work | Next action / completion boundary |
|---|---|
| Private remote operation (Phase 4) | Ordinary unconfigured `snow --mode web` now selects the first active private IPv4 address (otherwise an IPv6 ULA), binds both it and `127.0.0.1` on port 7331, and serves the shared manager directly on both exact origins with pairing and no certificate/proxy/setup step. Local and LAN origins use separate exact Host/Origin boundaries and host-only cookie names; offline hosts serve loopback directly. This transport is explicitly unencrypted and unsupported on public networks. TLS, certificate/CA, saved-profile, DNS-origin, trusted-proxy, Tailscale forwarding, browser API-key entry, and hidden network overrides were removed. Wildcard/public/DNS listeners remain unavailable. Complete real same-LAN phone/desktop and Tailscale acceptance before release claims. |
| Resource limits and recovery | Focused acceptance now covers two independently active project workers and proves that one worker's crash leaves the other's original instance, pending approval and live control path usable through definitive completion. The production-browser workflow exercises the same behavior earlier in its sequence, but its complete gate is currently blocked by BUG-243 during a later recovery scenario. Finish the Phase 4 aggregate-admission, slow-client, process/stream bounds, manager-death and restart-reconciliation acceptance work. Preserve the current two-live-project cap and never replay prompts, approvals or uncertain jobs automatically. Existing local guards are not proof of the complete remote reliability gate. |
| Frontend migration/coverage reconciliation | The production frontend now uses React islands plus the first-party bounded `SnowNavigation` controller; the former third-party navigation runtime and retired classic scripts are removed. Continue auditing browser and source-extractor entrypoints for React parity, and map production-manager coverage for grouped cross-project navigation/lifecycle and committed workspace New/Stop/removal journeys. Component/exported-page fixtures alone do not certify those mutations. See [frontend boundaries](web-frontend.md). |
| Known reproducible defects | BUG-086, BUG-175, [BUG-180](../bugs.md#bug-180-sidebar-navigation-and-row-controls-lose-focus-and-list-state) and BUG-218 are resolved. Continue treating the complete eight-report permission/sidebar matrix as the regression boundary for React navigation focus and retained state. |
| Capabilities not delivered by this increment | Automatic worker recovery and saved media rendering remain planned. Browser OAuth, extension/subagent enablement, worktree forks, general Git writes, editors, PTYs, preview fleets and workspace destruction are not provided by the current manager; several require separate scope approval rather than filling in a missing button. |
| Release and real-environment acceptance | Run the reusable CI gate and the canonical [release runbook](releases.md#next-release-runbook), including changelog/artifacts and secret-free manual provider smoke. Real private-network phone/desktop journeys, other browser-engine/device acceptance and release approval are not established by loopback fixtures. Do not publish or move a tag as part of this checkpoint. |
| Running-manager adoption | The latest UI was built and installed locally, but the already-running manager was not restarted. Restart it explicitly and refresh the browser when it is safe to interrupt its work. A Git commit or binary replacement alone does not update a running process. |

### Verification boundary

Milestone counts elsewhere in this document remain historical evidence for
their specific artifacts. The latest focused UI checks pass: 128 frontend tests,
typecheck/build and asset/notices reproducibility, JS syntax, the 404-assertion
native runtime-control matrix, all 84 runtime-layout reports, the Go web package
and all 1,686 assertions across the eight-report permission/sidebar matrix.

Before committing this checkpoint, the following broader local checks also
passed with the available Go 1.27rc3 toolchain:

- `go test ./...` (including valid Go test-cache reuse).
- `go vet ./...`.
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v` — 70 tests.
- `python3 scripts/check_benchmarks.py`.
- Changed/new Go files remain within the 1,000-line limit.

The zero-setup LAN implementation makes ordinary startup exact-origin
trusted-LAN HTTP automatically: a managed installed-binary start selected
`192.168.100.156:7331`,
printed the unencrypted transport warning, returned `200 ok` for the exact Host,
rejected a foreign Host with `403`, and issued the distinct non-Secure LAN pairing
cookie. Pairing output was redacted, and the temporary process/state were removed.
The follow-up localhost correction starts the private and `127.0.0.1` listeners
on the same port and serves the shared manager directly on both. Real-listener
integration verifies `200 ok` independently on both health URLs, joined
cancellation, and fail-closed cleanup when the localhost port is unavailable.
Focused exact-origin coverage verifies separate local/LAN pairing cookies and
cross-origin rejection. The final cleanup removed the obsolete
TLS/certificate/profile/proxy and browser API-key paths; all 127 remaining
frontend tests, typecheck, and the reproducible production build pass. The
post-cleanup native host-control matrix passed 176 assertions across four
width/theme reports, and the React-pages matrix passed 192 assertions across 26
scenarios. The latest `go test ./...` rerun passed every package, including the cell tracked as the
intermittent [BUG-221](../bugs.md#bug-221-worker-loss-project-operation-fixture-leaves-fictional-git-observable); that single pass does not by itself close the recorded intermittent defect.

These checks do not retroactively rerun every browser suite or establish race,
live-provider, physical-device, reusable-CI or release acceptance for this exact
checkpoint. The full race suites, standalone SDK example, vulnerability scan and
complete browser suite were not rerun during commit preparation. BUG-218 remains
open despite subsequent passing Go runs; its failing response was not captured.

## Current implementation status

The first increment originally implemented `snow --mode web` on numeric loopback
with in-memory browser pairing, same-origin/CSRF checks, a third-party
server-rendered navigation runtime, Overview/Projects/Browser access, light/dark
theme, and a clearly labeled static conversation fixture. That runtime has since
been replaced by the bounded first-party React-compatible workspace navigator.
The current source automatically serves the shared manager directly on exact
trusted-LAN and same-port localhost HTTP origins when a private address is
available, with loopback-only HTTP fallback when offline. Public Internet,
HTTPS/TLS, certificates, generated CAs, saved network profiles, DNS origins,
trusted proxies, forwarding-header authority, wildcard listeners, hidden
network overrides, and browser API-key entry are unsupported and absent.

The second increment adds a private SQLite registry (100 existing canonical
project roots, persisted identity, single-manager lifetime lock, metadata-only
removal) and real read-only saved-session browsing through typed RPC backend
methods. `pkg/agentclient/process` supervises direct children over deadline-capable
stdio, reusing `pkg/agentclient/rpc` framing, handshake, correlation and overflow
handling. Each read starts and reaps a worker; at most two run, without queuing or
an idle pool. This deliberately does not implement the longer-lived worker design
below. Dedicated `--rpc-startup catalog` bypasses app/config/auth/plugin/provider
startup and rejects every execution/mutation command. Ordinary RPC remains eager.

Catalog reads list inactive supported SQLite databases only; active, unleased,
WAL/recovery-dependent, invalid, child, empty nondurable and oversized databases
are omitted. History exposes bounded current-branch user/assistant text, not
thinking, tools/results, images or private provider data. The manager never
imports session/runtime internals or opens a session DB itself. Session roots
respect `SNOW_SESSIONS_DIR` independently of `SNOW_HOME`.

The third increment replaces the initial memory-only pairing and static workspace
limitations with reusable 30-day pairing and durable browser credentials, host
folder browsing, mobile drawers, a mounted composer, and explicitly activated RPC
workers. At most two projects run, with one serial worker each. Text, Stop,
allow-once/deny approvals and typed questions use the existing normalized stream.
Activation identities bind mutations; restored goals remain deferred. Manager
restart does not reactivate workers or replay work.

The fourth increment adds sanitized assistant Markdown and bounded public tool
activity via additive `AgentEvent.ToolResult`, separate from private display
previews. The read-only Files / Changes inspector uses pinned file reads and
isolated Git control metadata without worker activation or original-index writes.
Git supports bounded conventional repositories; unsupported layouts visibly fail
closed. It still reads the live host worktree/object store and is not a sandbox
against concurrent host mutation. File-name exclusions are not secret detection.

The fifth increment adds explicit model/session discovery through the activated
worker, durable conversation-scoped model selection that leaves host settings
unchanged, create/rename/switch controls, authoritative Plan Mode, public proposed-plan
history, and counts-only usage/context indicators. Active switching requires
Stop-and-switch confirmation and definitive completion; each session transition
rotates control identities and fences old events. Nonqueued controls revalidate
identity after admission and unknown transitions fail closed. Drafts and
uncertain-outcome guards remain in tab memory across navigation. Reconnection
never replays mutations or silently retargets a replacement session. Initial
activation uses configured host defaults; host-backed selectors become available
afterward via explicit discovery. Clicking the model selector now performs
uncached discovery directly and focuses a local name/ID/provider search field
(BUG-169), without a second Load or Model-submenu click. Results stay grouped by
provider, cached per runtime identity and explicitly refreshable. Page load,
search typing and cached reopening do not discover or select anything. The
bounded/partial/empty/error states and idle admission remain authoritative.
A pinned search header and separately scrollable results retain keyboard editing,
IME composition, Escape/focus return and 12px visible-viewport clearance.
A saved conversation selection is restored when switching or explicitly
reopening it; a conversation without saved selection inherits the active worker
pair. The searchable-picker workflow suite passes **1,736
assertions** across seven widths and both themes; the full responsive matrix
passes **2,058 reports / 38,804 assertions**. Short 240px viewports retain a
visible search field above scrolling results. Evidence is in
`dist/model-picker-full-layout/` and `dist/model-picker-screenshots/` (320px dark).
These are local Chromium fixture checks, not live-provider or physical-keyboard
certification.

The composer-context increment (BUG-170) reuses existing typed RPC content and
skill inventory rather than introducing uploads or another agent loop. Browser
images and text files remain bounded tab-memory drafts until explicit Send.
The `prompt-content` action validates closed text/image variants, at most sixteen
blocks/eight images, 2 MiB raw images and 128 KiB total text within a 4 MiB form;
ordinary `prompt` and other 256 KiB forms remain unchanged. Image headers/MIME
and dimensions are checked without pixel decompression. File labels stay in
content blocks, never in explicit skill-mention input. Public snapshots omit
image bytes. Queue/edit/reuse remain text-only and cannot silently drop context.
`@` suggestions reuse the existing authenticated Files inspection POSTs, folder
pagination and pinned-root protections; a nonempty filter advances through
bounded pages until a match, exhaustion or the existing 4,096-entry scan cap.
Explicit selection captures complete bounded UTF-8 content, rejecting truncated
previews. Plain Tab and Enter accept enabled `/`, `@`, and `$` rows while modified
Tab and IME retain native behavior. The `/` catalog contains only Web-native
commands and delegates to their existing typed controls. Drafts beginning with
`/` stay command-only across keyboard submission and the Send button's focus
transition, so selected, incomplete, unsupported, and argument-bearing slash
text never becomes a provider prompt; TUI-only commands remain absent. `$` uses an instance-bound
100-entry metadata projection from RPC `skills` and inserts exact tokens without
activation. The workspace's saved next-start policy admits three lifecycle tools;
Settings → Workspaces changes future starts without a repeated activation checkbox.
New registrations default enabled, existing saved preferences survive upgrade, and
archive/removal clears an opt-out back to that default. Manager consent never grants
CLI extension trust or changes permission policy.
Draft ownership, asynchronous query/caret/instance fencing and no replay are
shared with the existing composer controller. PDFs/arbitrary binaries and native
attachment queue/edit support remain out of scope.

BUG-172 corrects that increment's visual composition: plus/context and paperclip
sit in the existing single action row, not an additional toolbar. Plus opens the
shared menu without discovery until explicit selection. The textarea-owned
suggestion listbox follows composer width, with compact file/folder icons and
one-line skill summaries retaining complete titles. Idle guidance is visually
hidden while actionable runtime/error state remains visible. Production-browser
measurements show 98px idle cards at desktop and 320px width, a 42px action row,
and 56px long-skill rows; short viewports keep their bounded sizing. No content,
consent, permission, draft, or execution authority changes accompany this correction.

The subsequent UI-only redesign replaces the overview dashboard and fabricated
conversation with the measured Harness-style landing and conversation workspace:
280px full-height sidebar, dark canvas, an approximately 820px centered composer,
compact controls, and bottom Settings. Snow branding, real host folder selection,
explicit activation and permission boundaries are retained. `harness.css` owns
this composition after shared `app.css`; the React shell and first-party
workspace navigator own local navigation. Production-template browser fixtures
cover responsive geometry and interaction,
complementing the deeper request-ordering/race suites. No runtime feature or
network exposure is added by this redesign.

### Current workspace/session navigation contract

`design-plans/workspace-session-flow.md` replaces the catalog-first web navigation
with one conversation surface and independent workspace/session groups. The
workspace-name link navigates; its disclosure only expands a lazy session list.
Several groups remain expanded across native workspace replacements, with
existing search, scroll, focus and no-op reconciliation preserved. Registration selects explicit `new=1`
without creating a session; an ordinary cold workspace prefers a valid tab-memory
`last_session` hint, then its most recently updated saved session. Explicit saved
links retain their existing live-owner mismatch rejection. A deliberate ordinary
same-tab saved-session row selection resumes the exact session directly when
workspace trust is remembered; its authenticated POST is the activation action.
Direct GETs, reloads and modified clicks never switch or activate a runtime.

- `GET /projects/{project}/sidebar-sessions?offset=N` exposes authenticated public
  metadata only: project/instance identity, session ID/name/update time, availability,
  truncation, pagination, `active_session_id` and optional `delete_supported`. Cold pages reuse the runtime-free catalog; live pages
  use optional `RuntimeSessionInventoryBackend` and only public `sessions_list`.
  Pages have 25 entries; live inventory and per-workspace browser cache cap at 100.
  Sidebar reads never invoke combined Choices, discover models or refresh telemetry.
- Background read admission is separate from mutation admission. Up to four sidebar
  reads can be in flight. Explicit Start/Switch cancels its workspace's reads and
  waits for canceled I/O teardown before admitting ownership; unrelated reads do not
  reject activation. New reads cannot enter that workspace during the owner change.
  Cancellation failures remain closed, without hidden action retries or catalog
  access overlapping newly admitted runtime ownership.
- `sidebar-sessions.js` owns bounded tab-memory branches and stale-response guards;
  `shell.js` dispatches deliberate row selections to `app.js`. The app verifies a
  mounted live owner before consuming a cross-workspace mutation intent once. For
  a cold workspace, it first binds the selected URL/session and requires the exact
  remembered-trust hidden confirmation before submitting one activation; untrusted,
  unavailable and read-only views stay passive. `conversation.js` retains
  switch/queue/Stop/unknown-outcome admission. A busy
  instance-bound displayed target opens existing Stop & switch confirmation rather
  than trying an idle-only inventory read. The runtime validates the actual target.
- Switching preserves a complete, identity-checked idle snapshot's connected
  presentation while subscribing to the replacement instance. Cross-workspace
  admission shows an opening placeholder instead of unrelated history, but reveals
  actual state for Stop confirmation and errors. Sidebar reconciliation promotes
  the existing target row rather than rewriting another row's immutable ID; focus
  and the previous row survive delayed inventory refresh.
- Each session row has one shared **⋯** menu: Rename for the current session and
  capability-gated Delete session. There are no standalone row trash buttons.
  `session-actions.js` provides named, unchecked permanent-deletion confirmation.
  `POST /projects/{project}/sessions/{session}/delete` accepts only a 4 KiB bounded,
  authenticated/CSRF form with `confirm=delete` and exact `instance_id` (empty cold).
  Current active sessions cannot be deleted. Live deletion revalidates inactive
  membership under the existing idle/control gate before one `session_delete`;
  cold deletion uses the bounded runtime-free control client with
  `inactive_session_delete_v1`. The shared `internal/sessiondelete` helper retains
  FileIndex identity/lease/quarantine and managed artifact/goal cleanup semantics.
  Deletion preempts and joins sidebar reads, and admission stays owned through
  worker teardown. Only an exact `{project_id,session_id,instance_id,deleted:true}`
  receipt removes a row; uncertain failures never trigger automatic retries.
  Deleting the viewed cold session opens `new=1`, preserving a matching tab-only
  startup draft, without activating or sending. No workspace files are deleted.
- Editable fields share the `app.css` focus primitive: one 1px solid `--focus`
  outline inset by 1px, with no detached gap or layout shift. It covers text-like
  inputs, searches, textareas and selects across themes and panels. Non-text
  controls retain keyboard-focus variants. Composer and free-text-answer owners
  apply the same edge to the compound field and suppress the inner textarea
  outline; model search and organization fields do not override this primitive.
- Cold and saved sessions share the conversation surface and Start/Resume composer
  seat. Direct/project navigation remains at that passive seat; a deliberate
  same-tab saved-session row selection uses the remembered-trust form as its one
  Resume admission and enters the live conversation. The cold draft is unnamed,
  disabled without JavaScript and never submitted as prompt text during activation. A single bounded pending startup draft cannot
  overwrite another cold or live draft; automatic transfer matches the explicit
  activation receipt's project/session/instance. Explicit Use draft is separate
  consent. Nothing is auto-sent, and a late activation receipt cannot pull a newer
  workspace view back to its retired form.
- Trust, the installed-skills next-start policy, CLI extension trust and session
  permission policy remain distinct. First-start host-privilege/no-sandbox disclosure remains
  visible; the restored saved-Allow reminder stays available under the collapsed
  Startup settings disclosure. Settings retains actual trust revocation.

Workflow verification status and reproduced integration defects are tracked in
`bugs.md` (BUG-181 through BUG-184); historical matrix counts below are not a claim
that they were rerun against every later working-tree edit.

### Current implementation contract: snapshot SSE and presentation polish

This subsection describes source behavior, not a claim that every current
verification gate or full visual-parity check has passed. In particular, the
historical epoch/delta/two-stream proposal below is **not** the delivered wire
protocol. The current implementation is intentionally smaller:

- `RuntimeBackend` remains compatible; optional `RuntimeSubscriber.Subscribe`
  atomically returns an owned initial `RuntimeSnapshot` and a registered
  `RuntimeSubscription`. Subscription binds one project/instance and never
  activates, controls or switches work. The existing RPC event drain remains
  the only ingress; `publishLocked` commits a monotonic projection revision and
  offers one buffered wakeup per observer. There is no second transcript,
  per-token subscriber queue or durable/replay log.
- The authenticated private endpoint is
  `GET /projects/{p}/runtime/events?instance_id=...`, with exactly one expected
  instance ID. It emits LF-delimited SSE: `snapshot` contains a full JSON public
  display snapshot, initially and when newer revisions are available. Changes
  coalesce at 75 ms. `closed` and `auth_required` terminate the subscription;
  instance rotation closes the old binding rather than adopting its replacement.
  There are no epoch/delta events, `Last-Event-ID` replay, HTML-fragment stream,
  separate manager-attention stream or new public RPC methods for streaming.
- Limits are **16** simultaneous streams across the HTTP handler, **four** per
  browser credential and **32** subscriptions per runtime. Encoded snapshot JSON
  is capped at **4 MiB**. Heartbeats are **10 seconds**, each frame write/flush
  has a **five-second** deadline, and each connection lasts at most **ten
  minutes**. Browser authority is rechecked **periodically every five seconds**
  with a bounded durable-store read (up to three seconds), not immediately on
  revocation. Rendering/network writes stay outside the runtime lock and RPC
  reader. A disconnected browser does not cancel admitted agent work.
- The optional web turn-cancellation capability publishes a local opaque
  `cancel_token` before prompt admission and a `cancel_requested` latch. The
  authenticated, CSRF-protected `runtime/cancel` action accepts only that current
  busy turn and runtime instance, including while the prompt admission POST is
  pending. Exactly one worker-lifetime task waits for the existing control gate,
  revalidates the same session/turn, then sends one bounded RPC abort. Completion,
  replacement or shutdown can retire intent without aborting a later prompt.
  This queues cancellation intent only, never another prompt; an acknowledgment
  does not imply idle or rollback. Legacy Abort remains available to older hosts.
- Eligible user messages in a verified idle live conversation expose **Edit &
  resend**, not copy-and-append. Read-only `message-edit-prepare` resolves an exact
  saved source or bound live root-turn identity and returns complete text with a
  short-lived token. Explicit `message-edit-commit` atomically changes the active
  same-session path and admits the replacement through the ordinary agent loop.
  The original append-only tree remains internally; no second chat is created
  and executed tool effects are not undone. A durable public-history ACK replaces
  the visible suffix before buffered new-turn events are replayed. Instance
  rotation retires old browser/SSE authority. Unknown outcomes never replay or
  restore a falsely ready old projection. Previous drafts and concurrent typing
  survive in bounded tab memory. Unsupported legacy hosts separately label the
  old copy action **Use as new prompt**. Truncated, mixed-content, transformed or
  inactive sources cannot silently become executable replacements.
- Eligible terminal assistant replies also expose **Regenerate**. Read-only
  preparation resolves the exact saved assistant or accepted live root turn and
  its original user input; a separate action-bound commit accepts no browser
  prompt text. Explicit confirmation warns that the whole reply and its tool work
  restart, following conversation is replaced, and prior effects are not undone.
  This shares editing's atomic admission/public projection rather than adding a
  second loop or duplicate prompt. The composer draft and current permission
  policy are preserved; Stop remains turn-bound. Prefaces, plans, partial or
  inactive sources and stale tokens cannot silently become executable retries.
- **Queue next** adds a separate pending-work panel and explicit follow-up
  admission during a confirmed run. Typed session/root-bound queue controls use
  stable item IDs and revision CAS; editable text is complete and bounded to
  eight pending/review items, 64 KiB each and 256 KiB combined. Full snapshot
  revisions prevent late HTTP acknowledgments from rolling back newer SSE state.
  Durable delivery—not enqueue—adds a user row. Atomic persisted input-span
  markers retain historical Edit/Regenerate within the same admitted root loop.
  Stop/failure retains unsent work for explicit review/removal; unknown delivery
  never becomes an automatic retry. New prompts and session transitions cannot
  strand retained work. The queue remains live-only, not a restart scheduler;
  the manager still has no second turn loop or activation queue.
- `stream.js` uses native browser `fetch`, a fatal streaming UTF-8 decoder,
  bounded LF frame parsing (16 MiB client buffer), a 25-second no-data watchdog,
  and reconnect backoff from 500 ms up to ten seconds. Only unsupported legacy
  backends (advertised without the capability or returning HTTP 501) use the
  two-second snapshot poll; errors/429 never silently select polling. Hidden
  tabs pause; returning reconnects GET for a full snapshot. Terminal close,
  replacement or authentication loss stops automatic subscription recovery.
  No POST is replayed. Navigation/disposal retires readers; stale callbacks
  cannot mutate the replacement subscription.
- The browser verifies project/session/instance and revision before reconciling
  public snapshots. After a control POST it disables mutations until a fresh
  bound read synchronizes authoritative state. Unknown outcomes require explicit
  review; replacement and login states require workspace review/sign-in rather
  than silently retargeting controls. These are not durable idempotency receipts
  or a claim of exactly-once execution.
- `attention.js` and `attention.css` replace the normal composer presentation
  with a question or approval card in the same non-scrolling seat. The normal
  draft remains mounted. Question batches page through up to 16 questions,
  preserve exact option/custom/free-text answers in bounded tab memory, support
  back/next/collapse and IME/multiline input, and submit only a complete validated
  batch through the existing action broker. Recommended labels are presentation,
  not preselection. **Stop turn** cancels the whole turn without answering.
  Approval details scroll separately from the host-authority warning/actions;
  truncated summaries cannot be allowed. No remembered permission grants appear.
- `scroll.js` owns the transcript scrollport and measured composer/attention
  seat. Upward reader intent releases follow; visible message/activity identity
  anchors survive reconciliation, bounded head trimming, reflow and attention
  changes. Jump to latest, an accepted send, or deliberate downward movement
  into the tail resumes follow. It does not treat an incidental layout clamp as
  reader intent. Position/draft memory is tab-local, not sessionStorage or a
  durable recovery promise. Visual-viewport handling is not a claim of verified
  physical iOS/Android keyboard behavior.
- Menus separate scrolling choices from a fixed action footer; responsive
  navigation, attention, Settings and Files/Changes retain accessible controls.
  The inspector and tool timeline show bounded public text, not an editor,
  private trajectory or fabricated backend features. Raw arguments, thinking
  content, private provider continuity, remembered approvals, queue/process/
  subagent controls and broader defaults editors remained outside that milestone;
  later bounded local controls are described below.

Sources: `internal/web/{conversation,runtime,runtime_stream,stream,markdown}.go`,
`internal/web/static/{app,stream,attention,scroll,menus,conversation,visibility}.js`,
and their templates/styles/tests. `cmd/snow/web_stream_fixture_test.go` supplies
the real agent/RPC fixture outside `internal/web`; no app/agent/session-internal
imports or second loop are added to web. Public RPC/private provider contracts
are unchanged by snapshot SSE. Direct loopback is still the only supported
network deployment.

### Durable public tool history and explicit recovery increment

The implemented increment persists explicit public tool-result provenance before
publishing completion, with no private/legacy fallback. Runtime-free catalog
reads opt into `catalog_public_tools`; active workers use the narrow
`messages_public_history` paging capability. Shared standard-library-only
projection attaches stable bounded tool disclosures to their owning assistant,
including tool-only owners, pre-compaction history, page-end results, and
unresolved trailing calls. Duplicate ownership/results and incomplete catalog
intervals fail conservatively. Browser output remains literal text and is not
part of message-copy source. The live activity timeline remains separate.

One private registry recovery hint per registered project records only verified
session identity, coarse observed state, and timestamp. Intent persistence is a
pre-dispatch gate; later outcome updates are bounded best effort. Observed RPC
admission is not a durable-message guarantee. Late acknowledgments preserve
terminal worker status and definitive completion evidence. Worker loss/close
leaves unconfirmed tools unknown, not falsely canceled. A cold manager has no
live workers; reads never reactivate or replay. Close retains the saved-session
review destination, followed by separate explicit resume with fresh authority.
Removal deletes the hint; no transcript, prompts, outputs, approvals, old
instance IDs, or queues are stored in manager metadata.

That recovery increment alone did not enable execution or branch controls.
Current explicit goal, Versions and process contracts are described below; none
adds automatic recovery, exactly-once execution, extensions/subagents or remote
access.
Canonical limits, legacy behavior, and uncertainty semantics are in
[Using Snow](using-snow.md#try-the-local-web-manager-shell) and [RPC](rpc.md).

### Current local-management and explicit-execution source increment

The bounded local increment passes the integrated Go, race, vet, Python and
benchmark gates. Production browser checks pass 388 Queue/Activity/organization/
Versions assertions and 308 Goal/Process assertions across four reports each;
the shell layout matrix passes 1,932 reports. Goal/Process fixture activation
uses an explicit fictional-model HTTP bootstrap, so native activation is not
claimed by that suite. This is not a whole-product or target-roadmap completion
claim. Existing running managers do not acquire new controls through browser
refresh: after local installation, restart the manager and workers.

- **Activity:** read-only counts/navigation for at most 100 registered projects;
  256 KiB maximum summary. It samples registry recovery hints and in-memory public
  state, not catalog/session content, and carries no mutation/activation authority.
  Attention/running/queue/review counts may overlap; host-running is not browser
  connectivity. Saved-session links cannot silently select a different live chat.
- **Organization:** labels, pins and archive/restore are manager-only metadata.
  Registration Restore retains its ID and requires canonical path/device/inode,
  no active duplicate and capacity under the 100-project cap. Saved-session flags
  require exact membership in a freshly read catalog page and are bounded to
  1,000/project and 10,000 total. Pages contain 25 items with offset capped at
  10,000; title search/archive filters are loaded-page-only. Close the project's
  worker before archiving it or organizing saved sessions. No filesystem or
  session deletion/recreation and no runtime activation occur.
- **Versions:** bounded live-worker branch/history preview (100 branches / 64
  messages per page), then explicit two-minute single-use Restore preparation.
  Session, source and target branch/tip CAS must still match on idle commit;
  pending attention, nonterminal goal conflicts and retained queues reject it.
  Current model and permission authority survive; the core supplies the target's
  authoritative saved Default/Plan mode. Append-only history remains intact.
  Restore stays idle: no filesystem undo, provider replay or automatic goals.
- **Goals:** explicit Start/Resume names worker/session/branch/tip/expected goal
  and adds web `expected_revision` CAS against reviewed public state. One native
  correlated handle owns many serial turns, retry/compaction gaps and whole-run
  Stop; semantic goal status is distinct from execution completion. Optional
  token budgets are not strict billing caps. No unfinished-goal replacement,
  Plan admission, ordinary-prompt goal-tool exposure/dispatch or automatic
  restored-goal continuation. Retained queue/review items block goals; Queue next
  is disabled during them. The manager adds no second agent loop.
- **Processes:** the fixed `managed-explicit-goals` worker profile enables the
  process_start/status/logs/stop/list bundle under normal tool authority. Typed
  inspector controls list 128 records, read 32 KiB cursor pages and Stop exact
  session/opaque handles, never PIDs or arbitrary command launches. Operator Stop
  requires Default mode, the agent's actual hard InvocationPolicy and current
  noninteractive permission authorization. Ask without an applicable remembered
  allow fails closed, without a second broker. Session switching and shutdown
  stop managed processes, not guaranteed arbitrary detached effects.

Existing Queue next, Edit & resend and Regenerate remain subject to their shared
admission and recovery rules. At that milestone, skills were disabled by default
and required an explicit per-start checkbox; the later Settings-owned enabled
default documented above supersedes that startup policy. CLI extension trust and
tool permissions stay independent. Plugins and debug remain disabled. Configured
MCP servers and bounded subagents are enabled in explicitly activated workers;
MCP and shell-capable child operations use the session permission broker. Trust
remembered before this authority expansion is revoked once so the new activation
disclosure must be reviewed explicitly. At that
milestone, remote access, host-settings/provider-setup editors,
clone-directory jobs, worktree forks and manual compaction were not supplied.
The later local increment below supersedes only its implemented subset.
The phased capability tables below remain targets, not the availability contract.

Source ownership: `internal/web/manager_activity.go`, `registry_organization*.go`,
`organization_http.go`, `runtime_version*.go`, `runtime_goal*.go`,
`runtime_process.go` and their HTTP/templates/assets; typed client/RPC commands
bridge to app/agent/session facades. No web imports of app/agent/session internals,
resource-sync shortcut, alternate permission service or browser scheduler are
introduced.

### Expanded local controls: implemented and locally verified

The additions below are implemented with recorded local verification, including
the three new native matrices and fresh baseline reruns listed below. Do not
extend earlier milestone counts to these features or infer an installed/running
manager update. Earlier increments remain historical evidence, not proof of
every broader roadmap, reusable CI or release gate. The canonical operator instructions are
in [Using Snow](using-snow.md#try-the-local-web-manager-shell); configuration
ownership is in [Configuration](configuration.md#local-web-manager-settings-scopes).

- **Browser inventory and HTTP boundary:** paired-browser rows contain independent
  public IDs, coarse labels, approximate activity/expiry metadata and a current
  marker. Targeted revocation is durable and leaves agent work running; SSE
  notices it on the periodic five-second recheck, not immediately. Automatic
  trusted-LAN HTTP plus localhost is the only network path; there is no TLS,
  certificate, profile, proxy, forwarding-header, or browser API-key layer.
- **Runtime-free host control:** control startup dispatches before app creation.
  `internal/hostcontrol` uses local config/auth helpers for global defaults,
  operator-owned project selections and local provider status. Explicit reads
  and revision-CAS writes never initialize a provider or refresh credentials.
  Global provider/model, thinking, reasoning summary and verbosity, and project
  provider/model/thinking, apply to future workers only—not new conversations
  within an existing worker. These are scoped allowlists, not a raw settings or
  project-extension editor.
  All four worker families freeze absolute operator `SNOW_HOME` and independent
  session roots before changing CWD, without config/auth reads. Resolution errors
  disable startup. Manager storage is absolute; CONTROL/project-job backends use
  the registry’s canonical directory.
- **Current reasoning/history:** effective current-session reasoning is inspected
  locally using affirmative model capabilities and exact session/branch/tip/
  provider/model/mode/permission/revision CAS. Updates are in-memory, not config
  or new session-history metadata, with independent Default/Plan thinking.
  Ordinary branch fork activates its new branch; detached-session fork does not
  auto-open. Rename and forks repeat source/target tip/name checks atomically
  in the store; no history reads replay tools or implicitly execute controls.
- **Manual compaction, steering and cost:** provider-backed compaction owns one
  captured run until terminal completion, distinct from progress; whole-run Stop
  works and Plan Mode is preserved. Native Steer is not Queue next, accepted is
  not delivered, and a late/failed POST can already have delivered input. Their
  shared capacity is eight pending/review items, 64 KiB each and 256 KiB total.
  Lost receipts retain Stop for the exact captured root, never a replacement.
  Idle Keep draft and dismiss preserves text/uncertainty and releases the panel;
  separate global Reviewed releases Send/Close without retry or inferred delivery.
  Ordinary completion reads authoritative `goal_inspect` before idle readiness,
  capturing the new durable tip under original ownership/epoch fences. Immediate
  post-prompt compaction needs no extra inspection. The composer icon and menu
  action now execute on one explicit click, without a compaction dialog or
  checkbox; progress/errors use bounded, dismissible feedback above the composer
  without resizing the transcript, and Stop stays in the composer. Native no-op
  coverage checks transcript geometry, scroll and retained node identity. Goal is
  now a local composer-mode toggle: the existing editor supplies the objective,
  and explicit Send performs a fresh exact-scope preflight and native Start.
  Only the inspection's own single revision advance is accepted; changed facts,
  additional revisions or edited drafts abort admission. Confirmed native receipts
  correlate objective/budget before unchanged-draft clearing. Saved goal status
  and Details remain separate, with explicit exact-target Resume consent.
  Thinking is a compact capability-derived level popover beside model/usage;
  selecting a level applies one session-only update without Apply. Summary and
  verbosity stay in secondary Response settings. Existing React owners and
  transport/admission paths remain authoritative. Send/progress/Stop use a stable
  icon footprint, and short-screen composer overflow never scrolls the header.
  Rename and detached history forks
  act directly after name entry. Activating/replacing history retains confirmation.
  Cost is a recorded estimate, with mixed/invalid currencies unknown and no
  promise unpriced usage is included; it is not a bill or spending limit.
- **CREATE/anonymous HTTPS clone:** a five-minute browser/manager-bound parent
  selection records canonical path/device/inode. Durable admission precedes
  worker launch, with one active job, no queue, 128 retained rows and bounded
  32-row pages. A two-phase mkdir/pinned-child-identity/durable-ACK exchange must
  finish before clone network execution. CREATE works without Git; clone admission
  validates the fixed absolute executable before allocating a handle, with no
  PATH lookup/fallback. `internal/hostops` owns fixed Git and
  the trusted descriptor helper, process group and worker-liveness cancellation.
  Ten minutes, 64 KiB output and two-second TERM grace are not disk or transfer
  quotas. SSH/authenticated clones are unsupported. Cancel, identity-only
  Reconcile, explicit Register and metadata-only Dismiss do not delete files,
  activate agents or retry an interrupted operation.

**Explicit registration boundary:** successful CREATE/clone settles at
`awaiting_registration`; only a separate explicitly reviewed Register request
may insert the project, with operation-revision CAS and directory-identity checks.
Success, Get/List, reconciliation and restart never register automatically.
Registration does not activate a runtime, open a conversation or reexecute the
operation. Failed registration and interrupted/uncertain outcomes remain for
explicit review. BUG-159 records the focused pre-fix reproduction and verified
regression coverage; the local acceptance matrix below records broader verification.

**Filesystem-authority decision:** the delivered picker browses directories
available to the Snow OS user. CREATE/clone pins the specifically selected
parent; it is **not** restricted to startup-approved roots. This supersedes the
historical allowed-root proposal later in this document. Pinned identity protects
against unintended target changes; it is not general filesystem/process/network
containment. The read-only project inspector remains separately pinned to each
registered root with its own exclusion and input/output rules.

Worktree forks, remote deployment, browser OAuth, plugin enablement,
browser-side MCP configuration, direct subagent management controls, general Git
mutation controls, editors, PTYs and preview fleets are deliberately out of scope
for this delivered local increment. Explicitly activated workers do load configured
MCP servers and expose bounded model-directed subagents. Skills retain a separate
per-project remembered startup preference; changing it does not alter plugins,
MCP or subagent policy. Keep these boundaries independent of what other Snow
surfaces support. No web imports of app/agent/session/hostcontrol/hostops internals
or duplicate turn loop are introduced; public typed RPC carries the controls.

### Expanded-controls acceptance evidence

The React migration's newer, separately executed page, layout, replacement,
runtime and streaming baselines are recorded in
[Web frontend development](web-frontend.md#verification-and-release-integration).
Those records distinguish the complete layout run from later focused composer
changes. The newer integrated module also passes the 324-assertion Goal/Process
matrix (the old CSRF fixture startup blocker is cleared), the 432-assertion
manager-workflow matrix after native document-readiness repair, and the ported
thumbnail regression with 130 standalone plus 52 ColdWorkspace parent-owned
React assertions. The parent checks cover rejected/committed native navigation,
read/Blob lifetime and scope rejection; page restoration uses synthetic lifecycle
events, not actual bfcache eligibility. Workspace-action routing, grouped inventory
and exported-page checks now execute the production module as well (BUG-208),
with mocked public transport and no claim of committed mutations or real-worker
navigation. Earlier workflow failures and their verified fix are tracked as
BUG-207; old `messages.js` test coverage was replaced under BUG-206.
The historical local-manager runs below do not automatically establish React
parity for every retired rendering module.

The recorded verification below covers the bounded local implementation,
not the broader roadmap, reusable CI or release approval.
Authored scenarios are not passing execution evidence; reduced runs do not
replace a complete matrix. These fixtures use private temporary homes/stores,
ephemeral loopback listeners and browser profiles—not a user's running manager
or data. Runtime/provider work uses a gated fictional provider; host clone work
uses a fixture Git executable without network access. None establishes live
provider, credential, public-network clone or remote-deployment compatibility.

| Gate | Authored coverage | Recorded execution status |
|---|---|---|
| Native browser access | 320/1280 px × dark/light; pairing, inventory, exact revocation and restart persistence | **Passed: four reports, 200 assertions.** No runtime/provider activation. |
| Native runtime controls | 320/1280 px × dark/light; reasoning, rename/forks, prompt/approval/tool/reply, compaction and steering | **Passed: four reports, 208 assertions.** Includes genuinely lost HTTP response after native steering acceptance, exact-root Stop and idle dismissal/Reviewed cleanup; immediate compaction after the post-fork prompt uses the refreshed tip without corrective inspection or mutation retry. |
| Native host controls | 320/1280 px × dark/light; private HTTP/TLS, settings, write-only keys, CREATE/clone and explicit registration | **Passed: four reports, 208 assertions, zero failures.** The earlier desktop failure was resolved by a fixture-only native keyboard-driver keycode correction. |
| Focused source regressions | Completion scope/ownership, steering recovery, CREATE without Git, clone executable admission, canonical manager directory and all-worker storage roots | Focused Go/frontend checks passed; completion/queue/compaction coverage also passed race checks. These are not substitutes for native browser or full repository gates. |
| Native workflow/execution baselines | Fresh production browser journeys, four width/theme reports per suite | **Passed:** workflows 388 assertions / four reports; execution 380 assertions / four reports. |
| Production-template layout | Seven widths × dark/light, normal/short heights, strict mocked public DTOs | **Passed: all 1,932 reports.** The corrected fixture admits only the exact existing startup `GET /access/browsers`; 17 fixture tests passed. The earlier missing-mock failure required no production change or blanket read allowance. |
| Conversation workflow | Fresh seven-width × dark/light matrix | **Passed: 14 reports, 92 assertions each (1,288 total).** |
| Repository Go verification | Full Go tests/vet and affected concurrency boundaries | Latest `go test ./...`, `go vet ./...` and full internal race suite passed after the completion-scope fix. The complete CLI/public-package race suites also passed in a separate final command, not as one combined all-package race invocation. |
| Supporting checks | Python tests, benchmark guard and Node tests | **Passed:** 67 Python tests, benchmark guard and 112 Node tests. |
| Reusable CI, release and installation | Separate release gates and adoption of the verified checkout | Local checks do **not** establish reusable CI/release approval. A local build/install and manager/worker restart are separate operator steps; no existing user process is updated by verification. |

Together, the three new native matrices passed **616 assertions across 12
reports**, covering 320/1280 px in dark/light. Fresh native baselines add **768
assertions across eight reports**; the **1,932 layout reports** are a distinct
fixture matrix, not additional native execution assertions. These local gates
verify the bounded source increment, not the full roadmap or installation state.

Commands for the three native browser suites are
`node scripts/tests/browser/manager-access/run.mjs`,
`node scripts/tests/browser/manager-runtime-controls/run.mjs`, and
`node scripts/tests/browser/manager-host-controls/run.mjs`. Their fixture READMEs
specify isolation, prerequisites, artifacts and diagnostic subset selectors.
This status does not claim that an installed binary or existing manager/worker
process has been updated.

### Permission-execution reliability increment

A separate isolated browser fixture now drives the real permission broker and
builtin `write` through the existing app/agent, durable session, external RPC
worker, manager and production browser. It verifies Allow once/Deny enforcement,
colliding worker-local request IDs across two projects, stale and duplicate
replies, pending-approval transport loss, independent project execution, explicit
close while awaiting approval, and worker death before/after an actual write.
File bytes and durable execution/provider counters establish no replay during
saved-history review and explicit resume. No permissions or production tools are
expanded. Newly synthesized interrupted-tool repair records persist explicit
unknown-outcome provenance, so they remain unresolved in public history rather
than claiming a definitive failure. Legacy private text is not reclassified.

Current verification commands (requirements and scope matter):

```sh
# Node 22+, no browser/network: actual stream.js in a deterministic VM
node scripts/tests/browser/stream-client/run.mjs
# Node 22+, Go and installed Chrome/Chromium; no external provider/network
node scripts/tests/browser/live-stream/run.mjs
node scripts/tests/browser/permission-workflow/run.mjs
node scripts/tests/browser/harness-layout/run.mjs
node scripts/tests/browser/conversation-workflow/run.mjs
node scripts/tests/browser/inspection-race/run.mjs
```

The live-stream fixture drives a gated fake provider through real app/agent,
append-only session, external RPC worker, RuntimeManager, HTTP/SSE and Chrome.
It neither replaces fetch nor manufactures snapshots: incremental prefixes are
observed before completion, followed by Stop, stalled-stream recovery without
POST replay, an actual question/tool result and exact saved-history checks. It
is not itself a real permission-approval-turn test; the separate
`permission-workflow` fixture provides that coverage using actual builtin writes
in temporary projects. Neither fixture uses a real remote provider or establishes
arbitrary-tool/detached-effect guarantees. The deterministic stream-client
suite complements them with parser, backoff, visibility and retired-reader races;
it is not end-to-end evidence on its own.

The latest production-template layout run passed **all 1,932 viewport/state
reports** across 320, 360, 390, 768, 1024, 1280 and 1512px, light/dark, with ordinary
740px and selected 360/240px heights. Its expanded states include all-question
attention, approvals, stream anchoring, contentful inspectors, Settings and open
menus. The strict fixture allows the exact existing read-only browser-inventory
startup request, not arbitrary reads or mutations. This is full execution evidence
for that fixture matrix, not whole-product visual parity. Smoke/selected-width
runs and partial screenshots do not replace the full gate. See the runner
READMEs for prerequisites, evidence output and bounded-run options, and
`IMPLEMENTATION.md#testing-and-verification` for the canonical matrix.

These are bounded increments, not completion of every phase gate below. Remote
HTTPS/proxies, automatic worker recovery, saved media history, worktree forks,
browser OAuth, extension enablement and broader runtime capabilities remain
future work; bounded local defaults/reasoning and TLS are implemented above. Web-mode runtime
CLI flags remain rejected.
The canonical available behavior is [Using Snow](using-snow.md#try-the-local-web-manager-shell).
The sections below retain the target architecture and implementation gates.

## On this page

- [Current implementation contract](#current-implementation-contract-snapshot-sse-and-presentation-polish)
- [Product decision](#product-decision)
- [Research findings](#research-findings)
- [Scope and feature coverage](#scope-and-feature-coverage)
- [Launch and configuration contract](#launch-and-configuration-contract)
- [Information architecture and journeys](#information-architecture-and-journeys)
- [Visual and interaction design](#visual-and-interaction-design)
- [Snow integration map](#snow-integration-map)
- [Package and dependency design](#package-and-dependency-design)
- [Persistence and identity](#persistence-and-identity)
- [Runtime ownership and lifecycle](#runtime-ownership-and-lifecycle)
- [HTMX and streaming design](#htmx-and-streaming-design)
- [Permissions and user questions](#permissions-and-user-questions)
- [Project and filesystem operations](#project-and-filesystem-operations)
- [Models and settings](#models-and-settings)
- [Remote access and browser authentication](#remote-access-and-browser-authentication)
- [Threat model and operational bounds](#threat-model-and-operational-bounds)
- [HTTP route contract](#http-route-contract)
- [Implementation sequence](#implementation-sequence)
- [Verification and release gates](#verification-and-release-gates)
- [Decisions and remaining product choices](#decisions-and-remaining-product-choices)
- [Research sources](#research-sources)
- [Related documents](#related-documents)

## Product decision

Build **Snow Manager**, an optional first-party HTTP surface launched with
`snow --mode web`. The foreground process manages projects, browser access,
and a bounded set of Snow RPC worker connections through replaceable backend
interfaces. Workers reuse the existing runtime and own agent execution; the
manager never embeds agent business logic. Browser connections do not own
worker lifetime. Bundling the launcher in one binary does not couple these
components internally.

Confirmed requirements:

- The reference is [DeepSeek Harness][dsh], not the DeepSeek chat website.
- One user manages one host; several browsers belonging to that user may
  connect from desktop, tablet, or phone.
- Projects can be registered, created, renamed, and removed through the UI.
- Removing a project keeps its directory and saved conversations by default.
- The delivered application uses server-rendered HTML plus the bounded
  first-party `SnowNavigation` controller and React-owned interaction islands;
  it must feel deliberately designed, not like a basic administration table or
  a terminal pasted into a webpage. The earlier HTMX target retained below is
  historical research, not an active dependency or contract.
- Access works locally, on a LAN, and through a mesh VPN such as Tailscale.
- Use existing Snow RPC workers behind small backend interfaces. Keep the
  manager decoupled from agent internals and support independent project agents.

Recommended technical choices:

- Go `net/http`, `html/template`, embedded static assets, semantic CSS tokens,
  React-owned interaction islands, and a small first-party navigation/controller
  layer. The original target used HTMX and its SSE extension; both are absent
  from the delivered browser stack, whose live updates use native fetch SSE.
- A separate manager SQLite database for project/UI metadata; existing Snow
  session stores remain authoritative for conversations and branch history.
- A thin `internal/manager` service over backend interfaces; its initial Snow
  adapter reuses existing JSONL RPC in separate workers. No manager/web imports
  of agent runtime internals, direct session DB reads, or second agent loop.
- Existing app facades remain the implementation behind RPC inside workers.
  Extend missing reusable RPC capabilities rather than embedding their logic
  in the manager; use a fake backend to develop/test the UI independently.
- Loopback by default; authenticated HTTPS for remote browser access. Tailscale
  Serve is the preferred first remote-access guide, not a required dependency.
- Explicit project roots, lazy runtime activation, no automatic execution just
  because someone opens a page, and fail-closed permission interactions.

The current architecture calls a graphical application a non-goal. Treat this
as a deliberate expansion of supported **surfaces**, not a rewrite of the core
mission. The first implementation change must clarify the wording in
`AGENTS.md` and `IMPLEMENTATION.md`: an optional web surface is permitted;
graphical dependencies remain outside core packages, while turn execution
stays inside the existing agent loop, reached through RPC workers. Do not
silently pretend the current roadmap already promises a web application.

## Research findings

### DeepSeek Harness: verified patterns

Research pinned the repository tree to
`c291e7961a515f6d7af9304e7fd1d257929aef26`. The observations below come from its
README, subsystem and package documentation, a workspace component, and
committed accessibility snapshots. No DeepSeek installation was executed and
no live rendered instance was visually or performance-tested. Exact visual
quality and undocumented behaviors therefore remain unverified.

| Verified evidence | Snow decision |
|---|---|
| `npx @deepseek-ai/dsh web` launches a separate web application; default URL is `127.0.0.1:3080` [D1] | Provide an explicit Snow launch mode; ordinary TUI invocation remains unchanged. |
| A fresh UI requires a selected workspace before composing a task [D2] | Make project selection explicit and show the host path near the composer. |
| Workspace identity is a generated ID over a canonical directory, not a basename [D3] | Distinguish `frontend` directories in different parents; use opaque project IDs in URLs. |
| Workspace UI groups sessions and provides search, rename, reorder, fork, archive, and registration deletion [D4] | Use project-grouped navigation and clear, separate project/session lifecycles. |
| Its workspace picker delegates to a host-directory flow; the browse variant lists and creates host folders [D5] | Implement a remote-capable host picker, not a browser-native local folder chooser. |
| Its layout has a left sidebar, conversation center, and optional right inspection panel [D6] | Adopt this hierarchy, with mobile-specific drawers instead of squeezing three columns. |
| Approval UI answers allow-once/reject and can temporarily occupy the composer [D7] | Keep approvals close to the task, but preserve the draft and show a global attention queue. |
| Compact chat folds completed tool/process rows while keeping answers visible; it has explicit scroll ownership [D8] | Build structured turn/tool cards and reader-preserving streaming, not a raw log dump. |
| Themes use semantic tokens and light/dark/system modes [D9] | Define a small Snow token system before feature templates proliferate. |
| Its current web boot uses Vite, React, Cordis, and dynamic client plugins [D10] | Borrow interaction patterns, not its plugin architecture or frontend stack. |
| The documented shipped `dsh web` launcher rejects an all-interface bind; the carrier alone provides no TLS/auth policy [D11] | Do not assume copying its launcher delivers secure LAN access. Design Snow's boundary explicitly. |

Useful improvements over the reference's documented limitations:

- Aggregate pending approval/question counts into collapsed project headers
  and the global navigation; attention must not disappear inside a folder.
- Keep benign layout preferences across reloads rather than resetting panel
  widths every time.
- Use a responsive single-column host-directory picker on phones instead of
  shrinking a desktop Miller-column dialog.
- Give project removal an explicit retention summary and recovery path.
- Separate connection state from runtime state: a disconnected browser does
  not imply that the agent stopped.

Do not copy branding, arbitrary source code, sandbox claims, dynamic browser
plugin execution, scheduling, or desktop/Electron infrastructure. DeepSeek is
an experimental developer preview; its safety notice explicitly warns against
relying on it as the sole isolation boundary [D12].

### HTMX suitability and constraints

This is historical library research, not the installed browser stack. The
third-party runtime described below was later removed. Current navigation uses
the bounded first-party `SnowNavigation` fetch controller, while live
subscriptions use native fetch SSE.

Official documentation confirms that HTMX is designed around server-rendered
HTML responses, targeted swaps, normal forms, navigation history, and optional
extensions [H1]. Its SSE extension offers `sse-connect`, named `sse-swap`, SSE
triggered HTTP requests, and reconnection behavior [H2].

This is a good fit: submit commands through authenticated POSTs and stream
server-rendered state back through SSE. Sending prompts does not require a
WebSocket. A full interactive PTY would be a different feature, not a reason
to make the entire manager use WebSockets.

The researched SSE installation example pairs HTMX **2.0.10** with SSE
extension **2.2.4**. Use that as the initial compatibility candidate, not an
unqualified claim about the latest release. Pin reviewed files, hashes, and
licenses in the checkout; do not load `latest` or a public CDN at runtime.
Recheck advisories and compatibility when implementation starts. Context7
indexed several HTMX generations, so do not mix v4 examples into a v2 build.

HTMX does not supply authentication, CSRF protection, durable event replay,
exactly-once prompt execution, sanitization, or application state ownership.
Those remain explicit server responsibilities.

### Tailscale suitability and constraints

Tailscale Serve proxies a local service to an HTTPS tailnet address. Funnel
exposes a service to the public internet and is not the intended mechanism
[T1, T2]. Serve can attach identity headers, but Snow should not trust arbitrary
incoming identity headers or silently turn tailnet membership into application
authorization. Keep Snow's own browser pairing in the initial version.

## Scope and feature coverage

This is the historical broader target table, not current availability. In
particular, its allowed-root browser proposal is superseded by the OS-user
filesystem-authority decision in the expanded local-controls section above.

Use the following matrix as the product acceptance checklist. “First usable”
is the private alpha; “v1” is the complete initial manager release. Advanced
rows are not allowed to hold up the core vertical slice indefinitely.

| Capability | First usable | v1 / later boundary |
|---|---|---|
| Pair/login, logout, connection status | Required | Revoke individual browser sessions and rotate pairing access in v1. |
| List/register/create/rename/remove projects | Required | Pin/archive/filter and restore registrations in v1. |
| Host-directory browsing | Required, allowed roots only | No unrestricted host file browser. |
| Clone a repository | Not initially | v1 explicit bounded job; never put credentials in URLs. |
| Session create/list/open/rename | Required | Search metadata and archive/restore in v1. |
| Stream answers, tool calls/results, safe reasoning summaries | Required | Render only public blocks; never opaque provider continuity. |
| Prompt, cancel, follow-up queue | Required | Steer and queue edit/remove in v1 using existing queue semantics. |
| Permission approvals and `ask_user` | Required | Root/child attribution, reconnect, timeout, stale reply coverage required before remote use. |
| Provider/model/thinking and permission controls | Required | Capability-aware choices; active vs default settings made explicit. |
| Plan Mode and proposed-plan review | Required | Existing authoritative gates, no UI-only mode switch. |
| Resume, branch selection, fork, manual compaction | Resume required | Full branch/fork/compaction controls in v1. |
| Goals, budget and usage | View in alpha | Create/edit/pause/resume through existing RPC goal commands in v1. |
| Subagents | Show activity if enabled | Tree, safe transcript, follow-up, interrupt, close/reopen in v1. |
| Files and Git changes | Read-only bounded preview in alpha | Untracked status and diff review in v1; staging/commit optional later. |
| Managed processes | List/logs in alpha | Explicit stop action in v1; no arbitrary browser shell. |
| Skills/MCP/plugins | Show inventory and failures | Safe existing controls in v1; TUI-only plugin views need explicit unsupported states. |
| Provider credentials | Existing host configuration only | Browser API-key and OAuth entry are excluded; use host-terminal or control-RPC workflows. |
| Mobile, keyboard, accessible dialogs, themes | Required foundations | Must pass the v1 acceptance journeys, not a post-release polish promise. |
| Permanent workspace deletion | Excluded | Separate future proposal, never bundled into Remove project. |
| Multi-user/host fleet, cron, autonomous workflows | Excluded | Separate product decisions. |
| Full code editor, live app-preview proxy, interactive PTY | Excluded | Separate threat models and transport work. |

## Launch and configuration contract

### Mode selection

The only Web Manager launch contract is:

```sh
snow --mode web
```

Snow selects the first active private IPv4 address, otherwise an IPv6 ULA, and
binds it on port 7331. It also binds `127.0.0.1:7331` and serves the shared
manager directly on both exact origins with separate local/LAN browser cookies.
With no private address, Snow serves loopback HTTP directly. Startup fails
rather than silently choosing a wildcard, public, DNS-named, malformed, or different port.

There is no `snow web` configuration command, saved network profile, generated
certificate/CA, TLS option, public-origin override, trusted-proxy mode, or hidden
`--web-*` networking flag. Ordinary runtime/provider/configuration flags remain
invalid in Web mode. Browser pairing, exact Host/Origin checks, CSRF, revocation,
and normal permission boundaries remain mandatory. Traffic is unencrypted and
this mode is suitable only for a trusted private LAN.

## Information architecture and journeys

### Screens and navigation

- **Overview:** projects, running sessions, waiting interactions, recent work,
  provider setup warnings, and host connection status. Avoid decorative charts.
- **Project:** canonical host path, trust state, sessions, files/changes, and
  settings. Primary action: New session. Secondary action: project menu.
- **Session:** transcript and composer; inspector tabs for Changes, Files,
  Plan/Goal, Agents, Processes, and Details, shown only when relevant.
- **Attention:** cross-project pending permissions and user questions, grouped
  by session with exact host/project/agent attribution.
- **Settings:** appearance, provider status, defaults, browser access, extension
  inventory, limits, and diagnostics. Clearly distinguish host and session scope.

Every significant selection has a navigable URL. Browser Back restores project,
session, scroll anchor, and inspector selection without resubmitting a prompt.
Passive saved-session reads must not start plugins, goals, or an agent. A deliberate
ordinary same-tab sidebar selection may resume the exact session only after
remembered workspace trust is verified; direct URLs, reloads and modified clicks
remain read-only.

### First launch

1. Open the printed URL and pair this browser.
2. Show a welcome page with the host name and allowed workspace roots.
3. Choose Add existing project or Create project. Do not auto-trust the invoking
   repository or start an unsolicited agent turn.
4. Review project trust before any runtime can load project-defined extensions.
5. If no usable provider exists, show specific setup instructions rather than
   letting the first prompt fail mysteriously.
6. Open a New session draft. Persist a durable conversation only when needed
   for the first accepted task or another explicit durable operation.

### Returning to work

Show running and waiting work first. Opening a session loads recent completed
turns plus the current live projection. Display “Running on HOST” separately
from “Connected/Reconnecting.” Closing the tab leaves work running. On return,
restore the draft and scroll position, reconcile the stream, and surface any
pending decision without issuing another prompt.

### Remote creation and removal

The directory picker explicitly says “Folders on HOST,” not “Choose a folder
on your device.” Creating a project shows the destination path before submit.
On success, select it and offer New session; do not open a native host dialog.

Remove project shows its name/path, whether work is active, and this contract:
“Remove from Snow Manager. Files and saved sessions remain on the host.”
Active work must be stopped and settled first. Keep an Undo/Removed projects
route to restore the registration. File deletion and session deletion are not
hidden checkboxes in this dialog.

### Errors and recovery

Provide designed states for empty inventory, missing directory, unreadable
root, untrusted project, malformed project config, missing credentials, unknown
model, busy runtime, permission timeout, disconnected browser, disk full,
interrupted turn, truncated output, and unsupported plugin UI. Every state
needs a next action and must preserve unrelated drafts and transcript history.

## Visual and interaction design

### Direction

Use a calm developer workspace: neutral surfaces, strong typography, restrained
Snow accent, precise separators, and semantic success/warning/error states.
No generic dashboard gradients, excessive nested cards, or animation that
competes with streaming text. Carry over Snow's semantic accent/muted/warning/
error/success roles without importing Lip Gloss or terminal styling code.

The current port follows upstream DeepSeek Harness components at revision
`c291e7961a515f6d7af9304e7fd1d257929aef26`, backed by read-only inspection of its
running Settings/model menus and built-in synthetic conversation fixture. The
first landing-only approximation was rejected: its large mixed workflow form
and fixed live-composer dimensions were not representative of Harness. Source
contracts are recorded in `design-plans/harness-{conversation,task-menus,settings}-port.md`.
Current composition:

- Canvas `#151517`, sidebar `#1b1b1c`, composer `#2c2c2e`, primary text
  `#f9fafb`, send accent `#679efe`; semantic warning/error states retain text.
- Full-height 280px sidebar; New session 252 × 38px with 12px corners.
  Hero composer retains its approximately 820 × 114px desktop geometry. Live
  transcript width is `clamp(680px, column × .64, 920px)`; composer width is
  transcript width plus 32px, bounded by 16px side clearance. Its docked editor
  has a 36px floor, a 22px card radius, and an inside-card toolbar. Send/Stop
  occupies one circular 34px seat. One session header replaces stacked
  project/session headers: it is 76px on desktop and content-sized on mobile
  with a 40px single-row floor, expanding only when its contents genuinely wrap.
  There is no desktop global top bar or unsupported tabs.
- Independent model, Default/Plan, conversation-actions and context panels
  replace the mixed settings form. Model choices are provider-grouped and apply
  exact identities directly; opening alone never starts provider discovery.
  Shared menus use 20px corners, 4px inset, 14px rows and viewport-clamped portals.
- The conversation-actions menu also forwards supported Versions, Goal,
  Processes, Reasoning, Compaction and Steer launches to their resident owners.
  It does not duplicate admission or requests; absent owners are omitted and
  disabled states refresh while the menu is open. Close and Files/Changes remain
  independent header controls. These Snow actions are not represented as
  upstream Harness feature parity.
- Runtime panels adapt `ui-primitives/src/Modal.module.css`: 24px card corners,
  16/24 title, 28px icon Close, 24px body inset, persistent header and a bounded
  scrolling body. Versions keeps its two-pane desktop/stacked mobile layout;
  Processes keeps native details visibility/polling inside its dialog. The
  `ui-jobs/src/client/JobListAction` reference informs compact process rows, not
  unsupported job scheduling. Text Refresh/Stop actions never inherit icon widths.
  Active goal/compaction/steering summaries remain in a bounded conversation
  status region. Focus returns to the visible menu trigger, not a hidden launcher.
- Sidebar read navigation uses the first-party navigator's shared supersession
  scope so newer choices cancel older reads without canceling runtime operations. Successful workspace
  swaps retain the sidebar search/filter and scroll position, and restore an
  actually focused desktop list row before the app's content-focus fallback.
  Mobile still moves focus into content. Rejected swaps do not retire live state.
  Unchanged runtime snapshots leave sidebar labels and row attributes untouched;
  sidebar Rename uses the existing conversation admission owner with the row as
  its dialog focus-return target, not an intermediate header menu.
- New session and Add workspace use distinct chat-plus/folder-plus SVGs in the
  shared sidebar, rather than indistinguishable bare plus signs when collapsed.
- The collapsed 56px rail keeps the home-linked Snowflake above its separate
  expand control; only the wordmark and WORKSPACE badge disappear. The brand
  row measures 65px with the existing mark, gap and button padding. On very
  short desktop windows the rail scrolls vertically, preserving access to every
  action without shrinking controls or widening the rail. Expanded desktop and
  mobile retain their existing brand layout and scroll owners.
- Activity and Organize have labeled 36px icon controls in the collapsed 56px
  rail, matching Settings rather than leaving bare wrapping links.
- Bottom Settings opens an 800px, viewport-bounded sectioned modal for General,
  Workspaces and Browser access. Actual CSRF-protected forms remain canonical;
  no unsupported plugin/preset/permission editors are presented.
- System UI typography: 14px navigation and transcript, 26px landing title,
  13px contextual controls, 16px live mobile input, monospace code/logs.
- Dark default; an explicitly saved light preference is honored. Theme is the
  only localStorage preference; drafts and guards remain in tab memory.
- Keep Snow's own name and snowflake. No copied provider branding, fake
  session transcript, decorative working controls, or implicit activation.

`internal/web/static/app.css` contains shared component tokens/styles;
`harness.css` is the later-loaded canonical workspace composition; `menus.css`,
`messages.css`, `settings.css`, `dialogs.css`, `attention.css`, `scroll.css` and
`inspection.css` own their corresponding components. Adapted
upstream portions retain the full MIT notice in `static/HARNESS-NOTICE.txt`. The
network-free `scripts/tests/browser/harness-layout/run.mjs` renders actual Go
templates and embedded assets for geometry, interaction and screenshot checks
across the seven-width, two-theme, normal/short-height schedule described in the
current contract. This is not a platform-dependent pixel-hash gate.

#### Runtime menu/modal responsive verification

The enabled-runtime coverage added for BUG-165/167 uses actual production
launchers/controllers and strict public-read DTO fixtures. The completed local
full layout run passed **2,016 reports / 37,442 assertions**, including
**84 runtime reports / 8,212 assertions** (enabled and unsupported capabilities,
seven widths, light/dark, 740/360/240px heights). Checks include text glyph-line
wrapping, internal scrolling, persistent reachable Close, menu-to-dialog focus
return, collapsed footer containment and header/drag-handle hit testing after
layout changes. Evidence: `dist/runtime-ui-full-layout/layout-report.json` and
`dist/runtime-ui-responsive/layout-report.json` (local generated artifacts).

A separate 320px dark run with `--runtime-only --screenshots` records each of the
six dialogs both open and scrolled at all three heights, before dismissal, in
`dist/runtime-ui-open-screenshots/`. The runner now supports this open-state
capture directly rather than capturing only the closed post-test conversation.
These screenshots and geometry tests do not certify physical mobile keyboards,
platform zoom, other browser engines, live providers or release readiness.

Native runtime-controls passed **208 assertions / four reports**; manager
workflows (including Versions) passed **388 assertions / four reports**, and the
Goals/Processes execution matrix passed. Conversation-workflow and chat-width
checks passed (width: **253 assertions / 23 reports**), as did the real live-stream
path (**54 assertions**) and stream-client units (**51 tests**). Frontend/controller
and fixture mocks passed **118 tests**; full Go test/vet, 67 Python checks, and the
benchmark guard passed. The steering capacity regression additionally passed
100 repeats in each of two scheduling configurations and ten race-enabled
Steer/Queue runs after a read-only event-projection barrier corrected its stale
revision assumption (BUG-166). Native Stop tests now target the visible composer,
not duplicate controls in a closed dialog.

An initial parallel full Go run hit a host-clone helper child-readiness timeout;
three isolated repeats and the final full suite passed without helper changes.
The local evidence above supersedes neither CI/release gates nor future
verification after additional source changes.

#### Remembered project activation consent

Project trust is now an explicit, revocable manager preference (BUG-168), stored
against exact registration ID/path/device/inode in private registry storage.
Registration/migration never grants it. The first Start/Resume form explicitly
asks to remember consent; later forms retain a required explicit activation POST
but omit the repeated warning and checkbox. Settings → Workspaces exposes Forget
trust, including unavailable folders. Revocation does not stop existing workers;
archive/removal clears consent atomically, and restore/re-registration requires
fresh consent. CLI extension trust and saved/current tool permission policies
remain separate. The selected consent persists even if worker startup later fails.

Verification: full layout **2,058 reports / 38,762 assertions**, including the
**56-report / 1,718-assertion** untrusted/trusted startup subset at seven widths
and both themes (trusted screens additionally use 360/240px heights). Native
manager workflows pass **412 assertions / four reports**, covering explicit
initial trust, passive reload, compact Resume of an explicitly selected saved
session, and Forget trust. A plain project URL after reload deliberately has no
saved-session selection; it must not silently resume the last conversation.
Registry reopen, migration, identity, corruption, revocation and HTTP admission
tests pass, including focused race checks, full Go/vet, JS/Python tests and the
benchmark guard. Local evidence is in `dist/trust-{responsive,full-layout}/`;
320px dark startup screenshots are in `dist/trust-startup-screenshots/`.

### Layout

```text
┌──────────────────────────────────────────────────────────────────────┐
│ Snow · HOST                   connection       Attention (2) Settings │
├───────────────────┬───────────────────────────────┬──────────────────┤
│ Projects / Search │ project / session       status │ Changes | Files  │
│ + New project     │ model · mode · permission      │ Plan | Agents    │
│                   ├───────────────────────────────┤                  │
│ ▾ project A   (1) │ user task                     │ contextual       │
│   running session │ assistant response            │ inspection       │
│   saved session   │ folded tool group             │                  │
│ ▸ project B   (1) │ approval or question card      │                  │
│                   ├───────────────────────────────┤                  │
│                   │ draft / attachments            │                  │
│                   │ Queue next       Stop / Send   │                  │
└───────────────────┴───────────────────────────────┴──────────────────┘
```

The diagram above retains broader future controls; the delivered layout has no
desktop top bar, attachment queue, or agent panel. Current responsive behavior:

- At 768px and above: 280px full-height navigation, optionally collapsed to a
  56px icon rail. Conversation and composer share the adaptive width axis above;
  the 820px hero maximum is not applied to populated conversations.
- Above 1100px: the optional 340px Files/Changes inspector is alongside chat.
  At 768–1100px it follows chat in the scrollable content pane rather than
  obscuring interactive controls in a nonmodal overlay.
- Below 768px: one primary pane, 48px mobile bar, modal navigation drawer and
  full-height Files/Changes sheet. The composer respects safe-area insets.
- Inspector starts closed. Desktop rail preference survives native workspace replacements in tab
  memory and is clamped to the current viewport; resizing closes open drawers.

### Component acceptance rules

These are historical broader-target acceptance rules, not a list of delivered
capabilities. In particular, current activity lacks historical owning-turn IDs,
Queue next was delivered before native steering; the latter is now a distinct
exact-run control as described above, and drafts use bounded tab
memory, **not** sessionStorage. The current contract above governs shipped attention and scroll
behavior; permissions expose public effect summaries, never raw arguments.

- Project rows show title, disambiguating parent path, activity and attention
  counts. Search is accessible without depending on hover.
- Session rows show title, last activity, and textual state; secondary actions
  live in a keyboard-accessible menu, not a row of tiny icons.
- Tool cards show name, risk, target/path, progress, duration, exit status, and
  bounded expandable output. Multi-step activity is grouped by owning turn.
- Distinguish a complete assistant answer, an in-progress answer, a cancelled
  turn, a retry, and an incomplete history window.
- Approvals use a clear warning treatment, exact command/path details, and
  equally discoverable Reject and Allow once actions. Never preselect Allow.
- The composer remains mounted during stream updates. Shift+Enter inserts a
  newline; desktop send shortcut is documented; mobile has a clear Send button.
  Respect IME composition. Enter-to-send is not allowed to corrupt CJK input.
- While running, label follow-up submission “Queue next”; steering is a
  separate action with different semantics, not an ambiguous Send button.
- If the reader scrolls away from the bottom, streaming must not pull them
  back. Show Jump to latest and preserve the history anchor on pagination.
- Keep drafts per project/session in tab-scoped `sessionStorage`, not server
  logs or persistent DOM-snapshot history. Label draft recovery; clear on explicit
  send/discard/logout. This storage is convenience, not a secrecy boundary.
- Copy controls are accessible and report success without moving focus.
- Confirmations return focus to their invoking control; Escape closes
  non-destructive dialogs; focus remains trapped while a modal is open.
- Announce lifecycle/attention changes with a restrained live region, not
  every streamed token. Aim for WCAG 2.2 AA.

Before real provider integration, implement a fixture-driven component gallery
and full-page states: empty, active, waiting, failed, disconnected, long answer,
large diff, duplicate project names, and mobile keyboard open. Approve the
screenshots and interaction flow before expanding features.

## Snow integration map

The following source paths are current evidence, not proposed new APIs. App,
agent and session symbols are **worker-side reuse points**, not dependencies
that the manager may call directly. RPC handlers remain their control boundary.

| Existing responsibility | Reuse point and implementation implication |
|---|---|
| CLI options and surface dispatch | `cmd/snow/cli.go`: `buildOptions`, `runInteractiveOptions`; currently accepts print/json/rpc and otherwise enters TUI. Add web dispatch before any app construction. |
| Runtime wiring | `internal/rpc/main.go` constructs `app.New`; workers receive a fixed project CWD and validated startup options. Introduce opt-in lazy startup without changing ordinary RPC behavior. |
| Project trust | `internal/app/options.go`: `InspectProjectTrust`; `trust_preflight_test.go` verifies preflight, headless denial and symlink retarget behavior. Do this before app startup. |
| Configuration layering | `internal/app/startup_config.go`; project model selection, explicit overrides, trust and extension loading remain app-owned. |
| Project-scoped session inventory | `internal/app/session_inventory.go`: `ListSessions`, `CreateSession`, `OpenSession`, `RenameSessionByID`, `DeleteSessionByID`; IDs and CWD membership are checked. |
| Session/branch/goal transitions | `internal/app/sessions_goals.go`: `SelectBranch`, `ForkBranchWithOptions`, `GoalState`, `CreateGoal`, `PauseGoal`, `ResumeGoal`, and related controls. Preserve admission and continuation rules. |
| Agent execution | `internal/agent`; `pkg/snowsdk/session.go` demonstrates calling the existing agent Prompt/PromptContent methods. Do not reproduce this loop in HTTP. |
| Browser permissions/questions | `internal/app/runtime_facades.go`: manual enable/reply/reject APIs; `app.Options.PermissionHandler` and `UserInputHandler` also exist. |
| Public history projection | `internal/rpc/server.go`: `publicMessages`; `messages_page.go` supplies bounded cursor pagination. Consume the public wire DTOs; factor worker-side snapshot logic for inactive history without importing RPC server internals into web. |
| Provider-private state | `pkg/protocol/message.go`: `BlockProviderData`; `model.go` makes stream provider data persistence-only, never an AgentEvent. Exclude it from every browser snapshot/export. |
| Runtime controls and catalogs | `internal/app/runtime_controls.go`, `runtime_model_facades.go`, `runtime_facades.go`, `settings_rpc.go`; reuse transition locks and capability validation. |
| Managed processes | App `ListManagedProcesses` and `ManagedProcessLogs` facades already exist; add a safe stop facade if needed, not a new process supervisor. |
| Subagents | App facades and `internal/subagent`; root/child attribution and shared-host effects must be visible. |
| Existing lifetime file locks | `internal/session/lifetime_lock_unix.go`; shared-open/exclusive-operation locking is not automatically a single active runtime lease. Audit before assuming ownership exclusion. |
| SDK alternative | `pkg/snowsdk` supports separate sessions with explicit CWDs but lacks some management methods exposed by RPC. It is not the selected backend; no second production adapter is required. |

Representative regression evidence to retain includes app session-inventory,
trust-preflight, runtime-controls, goals, process-manager and subagent tests;
RPC permission, user-input, event-forwarding, messages-page, session-command,
and prompt-lifecycle tests; and SDK cancellation tests.

Important gaps are new product infrastructure, not verified existing defects:
project registry, HTTP/auth, safe public snapshot API, browser stream recovery,
manager-level runtime admission, browser interaction projection, and web views.
Do not file these as bugs merely because Snow currently has no web manager.

## Package and dependency design

The operator selected **RPC** after comparing it with the SDK. Both support
multiple independent project agents; RPC is selected for its broader existing
management contract and separate worker lifetimes, not because the SDK cannot
handle concurrency. The manager must be decoupled and easily extensible,
reusing existing functionality rather than embedding agent logic. This
supersedes the initial direct-`internal/app` recommendation. Use a **thin
control plane with backend interfaces and a Snow RPC adapter**. Implement only
this production adapter initially, alongside a fake for independent UI tests.

Proposed dependency direction:

```text
Browser: server-rendered HTML + React islands + SnowNavigation + SSE
                   │ exact-origin HTTP
                   ▼
internal/web ──→ internal/manager ──→ backend interfaces / public DTOs
                       │                       ▲
                       ▼                       │ implements
                 manager metadata       Snow RPC adapter
                                              │ JSONL over stdio
                                              ▼
                                    snow --mode rpc worker
                                              │
                                              ▼
                                     internal/rpc → app
                                              │
                                              ▼
                                  existing agent/tools/session/etc.

cmd/snow --mode web: composition root, not an agent runtime
TUI / print / RPC / snowsdk: existing runtime ownership remains intact
```

Shipping HTTP assets and the launcher in one `snow` binary is a packaging
convenience, not permission to import runtime internals into the manager. The
launcher wires the port to the RPC adapter. The manager process does not
instantiate `app.App`, call `agent.Agent`, open Snow session databases, or
reimplement permissions, provider recovery, compaction, goals or child loops.
A future standalone manager executable should reuse the same components
without extracting business logic from the agent.

### Ownership and extension seams

| Component | Owns | Must not own |
|---|---|---|
| Web adapter | HTML/templates, browser auth/CSRF, HTTP actions, SSE delivery, focus/scroll/drafts | Agent execution, protocol parsing, SQLite session reads |
| Manager service | Project registrations, browser-visible view models, worker admission, UI preferences, attention index | Provider/tool loops, authoritative permission decisions, branch/goal persistence |
| Backend contracts | Typed queries, commands, events, capability descriptions and lifecycle handles | HTML, Cobra, process globals, raw internal app structs |
| Snow RPC adapter | Process pipes, handshake, request correlation, frame bounds, command/event mapping | Business decisions, transcript rewriting, guessed capabilities |
| Snow RPC worker | Existing app construction, agent execution, tools, sessions, permissions, goals, subagents and their cleanup | Browser cookies, HTML rendering, project-dashboard metadata |
| Project filesystem service | Explicit bounded register/create/browse/clone operations under approved roots | General agent tools, arbitrary browser shell or session DB access |

Define small interfaces by responsibility rather than one enormous backend:
capabilities/lifecycle, session catalog/history, turn commands/events,
interactions, and optional model/goal/subagent/process/settings controls.
Use existing `pkg/protocol` DTOs wherever suitable. Any new reusable client
contracts belong in a dependency-light `pkg/agentclient`, not under HTTP or
`internal/app`. This package is proposed, not currently available. Keep
transport framing separate from typed convenience methods so another host can
reuse the RPC client without depending on the web manager.

The initial implementations are Snow RPC and an in-memory fake for UI/tests.
Do not build an arbitrary plugin loader, provider framework or multi-engine
fleet now. Adding another backend later should require an adapter and contract
tests, not template edits throughout the application. Capability data controls
which actions are available, with explicit unavailable reasons; no fake parity
when a backend does not implement Snow-specific branches/goals/permissions.

Suggested cohesive files/directories:

```text
cmd/snow/web.go                  composition and host-only admin helpers
pkg/agentclient/                 backend contracts and reusable typed client
pkg/agentclient/rpc/             bounded JSONL codec and Snow command mapping
internal/web/server.go          listeners, timeouts, shutdown, route assembly
internal/web/auth.go            pairing, cookies, browser-session checks
internal/web/security.go        origin/Host/CSRF/proxy policy
internal/web/projects.go        project HTTP actions
internal/web/sessions.go        session HTTP actions
internal/web/interactions.go    permission/question HTTP actions
internal/web/events.go          SSE transport, not agent execution
internal/web/render.go          templates and safe presentation
internal/web/templates/         layout, pages, reusable fragments
internal/web/static/            generated React, first-party controllers, CSS, notices
internal/manager/manager.go     backend injection and manager lifecycle
internal/manager/projects.go    registration/create/remove/restore
internal/manager/workers.go     backend handles, slots and worker supervision
internal/manager/projection.go  normalized public UI state
internal/manager/interactions.go pending request presentation and expiry
internal/manager/store/         manager-only metadata migrations/transactions
internal/manager/snowrpc/       child launch/exit policy, implements backend
internal/rpc/                   reuse handlers; additive capabilities if needed
internal/app/                   worker-side shared facade changes only
```

Keep HTTP types and HTML out of manager/client/runtime; keep Cobra at the
composition root. Inject backend, clock, storage, ID source, filesystem service
and listener. Most UI/manager tests use a fake backend, not real app/provider
construction. Add import-boundary tests: web, manager and client packages may
not import `internal/app`, `internal/agent`, `internal/session`, provider,
permission, goal or subagent implementations. Sharing reviewed low-level
filesystem/configuration utilities is allowed, not a loophole to call runtime
methods. Keep every Go source and test file below 1,000 lines.

### Existing RPC reuse and additive gaps

Snow already supplies JSONL framing, a version/capability handshake, typed
request/response DTOs, normalized events and extensive control commands. It is
**not JSON-RPC 2.0**. Reuse it rather than creating a competing agent API.
The adapter validates `rpc_ready`, handles responses by request ID and
`prompt_completed` by request ID, accepts events before ordinary responses,
and tolerates unknown additive capabilities/fields/events.

Continuously drain stdout and stderr, bound both, serialize stdin writes and
correlate asynchronous responses. Command acknowledgement is not turn
completion. An RPC request ID is a correlation ID, not durable idempotency.
Browser code never sees raw pipes or sends arbitrary RPC command names.

Current `internal/rpc/main.go` eagerly calls `app.New`, subscribes to events,
and invokes goal/subagent readiness; `Server.Serve` starts plugin extensions.
Consequently ordinary RPC is **not** a side-effect-free catalog service. Do not
launch `--mode rpc --no-session` on every page and claim that fixes it.

Before the first usable manager, add an opt-in lazy/control-only RPC startup
contract, for example proposed `--rpc-startup lazy`, while preserving existing
RPC startup by default. In lazy mode:

- The worker is bound to one validated project CWD and can handshake and serve
  side-effect-free trust preflight, inventory and public history before an app
  exists. No throwaway session, provider request, project extension or goal
  continuation occurs during these reads.
- An explicit runtime-activation command constructs the existing app, wires
  the existing RPC brokers/event forwarding, and enables existing controls.
  Restore goals only after deliberate execution activation and subscriptions.
- Read-only snapshots of another saved session never call `session_open` on
  an active runtime or disturb its branch/processes. Implement safe worker-side
  catalog/snapshot reads through existing store/index logic.
- Use a bounded short-lived pool of lazy catalog workers for inactive projects,
  not a permanent worker per registered directory. Active project workers can
  also answer explicit read-only snapshot queries without switching sessions.
- Add capability-negotiated pending-interaction snapshots/settlement,
  versioned public state snapshots, runtime activation, prompt receipt lookup
  and process stop only where current commands do not provide them.

Document/schema-test these additive contracts in `docs/rpc.md` and
`pkg/protocol/schema/rpc/v1`. Reuse worker-side app facades beneath handlers;
do not add HTTP-specific concepts to the core. An older Snow binary can remain
usable by other clients, but the manager must reject missing safety-critical
capabilities rather than falling back to direct database/internal access.

### Backend contract and delivery map

The table distinguishes implemented wire commands from new work. Names for
new capabilities and DTOs are provisional until the phase 0 contract review.
Do not advertise a proposed capability as available in today's Snow binary.

| Backend responsibility | Existing RPC reuse | Required addition or constraint |
|---|---|---|
| Connect/capabilities | `rpc_ready`, version, capabilities, max input bound | Lazy startup/activation state and capability checks before manager admission. |
| Catalog/history | `sessions_list`, `session_info`, `messages_page` | Runtime-free project catalog and inactive-session public snapshots; no implicit session switch. |
| Session mutations | `session_create`, `session_open`, `session_rename`, `session_delete` | Keep existing busy/membership checks; deletion is not project removal. Active controls require deliberate activation. |
| Turn control | `prompt`, `abort`, `steer`, `follow_up`, `pending_inputs`, `pending_inputs_clear`, `prompt_completed` | Durable submission correlation/lookup; bind commands to expected runtime/session/epoch. No invented per-item queue editing if unsupported. |
| Browser recovery | Normalized agent events and public history | Worker public snapshot/event watermark and authoritative pending-interaction snapshot/settlement. |
| Permission/question replies | `permission_reply`, `permission_reject`, `user_input_reply`, `user_input_reject` | Existing broker validation plus worker-incarnation checks; no blind replay. |
| Branches/goals/children | Existing branch, session-fork, goal and subagent command families | Capability-gated typed wrappers; preserve existing admission rules. |
| Models/settings/auth | `models_list`, `set_model`, response controls, `settings_get`, `settings_update`, auth commands | Worker-owned secret-safe projections; explicit control-only availability before activation where needed. |
| Trust | `trust_get`, `trust_set` for constructed apps | Side-effect-free preflight and explicit trust authorization before lazy activation; trust changes retain restart semantics. |
| Processes | `processes_list`, `process_logs` | Owner-checked stop command and negotiated aggregate budgets if exposed; no raw PID control. |

Cross-worker settings/auth writes still target shared host stores. Serialize
manager-originated global mutations and use existing atomic store/update
semantics inside workers. Publish invalidation and explicitly refresh affected
workers; do not pretend a change in one process immediately changed all others.
Preserve restart-required flags and per-project effective overrides. Never route
host-wide writes to whichever project happens to be selected without declaring
the scope; a runtime-free control capability is preferable to creating an agent
only to configure credentials. Browsers receive redacted status, not raw auth
RPC payloads or provider secrets.

Introduce public snapshot sequencing at the worker projection boundary, not as
a modification of the provider stream. Subscribe before requesting a snapshot,
then reconcile buffered events using the worker's snapshot watermark. The
manager separately sequences its derived HTML stream. Worker restart changes
its incarnation and invalidates prior command targets, pending approvals and
snapshot cursors. Do not treat unrelated command responses as an atomic
snapshot or use `TurnSequence` as a transport-wide event cursor.

Contract tests exercise the same backend behavior against the fake and a
real Snow subprocess using local fake providers. Keep manager-only project
metadata and browser preferences outside the agent wire protocol. For missing
safety-critical capabilities, fail clearly; for optional capabilities, disable
only the affected action with an explanation. Do not turn the backend into an
unrestricted generic command tunnel.

This process boundary supports independent UI evolution and worker-failure
containment. It is not an OS sandbox, durable detached-agent service, or a
promise that workers survive manager failure.

## Persistence and identity

Use `${SNOW_HOME}/manager/manager.db`, following Snow's resolved global directory
rather than hard-coding `~/.snow`. Restrict the directory to `0700`, sensitive
files to `0600`, and protect creation/replacement and SQLite sidecars through
reviewed existing storage conventions. Use Snow's existing SQLite dependency,
not an additional database driver.

Proposed schema responsibilities:

| Record | Fields and authority |
|---|---|
| Project | UUID, display name, canonical path, allowed-root identity, created/updated timestamps, pin/order, removed timestamp, imported vs manager-created origin. |
| Session presentation | Project ID + durable session ID, archive timestamp, UI ordering; no copied conversation or mutable replacement transcript. |
| Browser session | Hash of random session credential, created/last-used/absolute expiry, revoked timestamp, operator-provided device label. |
| Pairing code | Reusable random code and expiry in the private mode-0600 access store so foreground startup can reprint it after restart; no credential in URLs or public logs. |
| Submission receipt | Browser request key, project/session/branch identity, payload digest, admitted/rejected/uncertain outcome, associated durable input ID where available. |
| Manager operation | Explicit create/clone operation ID, state and bounded error, reconciliation marker; not an agent workflow engine. |
| UI preferences | Theme, panel sizes, compact display; no provider credentials. |
| Audit metadata | Action, target IDs, browser ID, outcome, timestamp; no transcript, tool arguments, secret values, or opaque provider data. |

Session databases retain messages, parent links, branch tips, compaction
checkpoints, goals and child history exactly where the runtime stores them.
Project trust and provider credentials remain in their existing stores. Do not
create a second permission or credential authority in the manager database.

Respect the independent storage roots: `config.GlobalDir` uses `SNOW_HOME`,
but `session.DefaultSessionsRoot` currently uses `SNOW_SESSIONS_DIR` or
`~/.snow/sessions`, not `SNOW_HOME`. Show both resolved locations in host
settings/backup guidance. A custom config/auth path does not relocate every
resource. Tests must set both home and session roots; do not change those
existing semantics incidentally while adding manager storage.

Rules:

- Canonical paths are unique among active registrations. Display names need
  not be unique. Missing directories remain visible as missing, not empty.
- Browser inputs use IDs and root-relative paths; resolve session paths only
  inside the worker using the existing indexed membership checks.
- Reading a project list or historical session does not create a runtime.
  Use the lazy RPC catalog/read capability, implemented through worker-side
  shared read facades, rather than constructing an app for an instance method.
- Removal soft-deletes manager registration/presentation metadata. Restoring
  revalidates path/root identity and session CWD; it never silently repoints a
  project to a different directory that happens to share its name.
- Folder creation and manager DB commits are not one transaction. Record an
  operation and reconcile partial outcomes; never delete a directory containing
  unexpected content as an automatic rollback.
- Schema migrations are transactional, versioned, and tested for interrupted
  startup. Backups include manager DB and existing session/auth/trust/config
  stores with explicit treatment of secrets; no ad hoc copying of live WAL DBs.

## Runtime ownership and lifecycle

### Admission and concurrency

Keep one authoritative app runtime **inside a Snow RPC worker** for a live
session. The manager stores an opaque backend handle and public projection,
not an app pointer. Key ownership by session ID and validated project identity,
not by a browser tab. Manager admission limits worker slots; Snow alone admits
turns and tool work. Multiple tabs share the same worker connection.

Launch the same resolved Snow executable with a fixed argument vector,
project-specific process CWD and explicit inherited configuration paths. Do not
search an untrusted project PATH or invoke a shell to construct a worker
command. Use private stdin/stdout pipes, not another network listener. Keep
provider secrets out of arguments and protocol diagnostics; workers use the
existing host auth store. Fail incompatible handshakes with a clear upgrade
message, and never forward arbitrary browser fields as CLI arguments.

Bound startup time and concurrent lazy catalog workers (initial target: two).
Track process identity, exit, pipe failure and active session separately from
browser connectivity. One worker failing must leave the manager and other
workers usable. Show that worker's work as interrupted; do not automatically
replay it or silently move the session into another running worker.

Initial policy:

- At most one active root runtime per project; other sessions remain readable.
- At most four active root runtimes across non-overlapping project roots.
- Reject concurrent activity in nested/overlapping project directories with a
  useful explanation. This limits accidents; Bash can still affect any host
  path allowed by OS privileges and permission policy.
- Each root keeps the existing serial turn/tool execution and existing bounded
  steer/follow-up queue. Do not insert a second autonomous turn scheduler.
- Project workers own independent root agents, not children of a manager agent.
  Subagents remain within their owning worker/runtime. Establish aggregate
  resource ceilings using worker-reported usage and worker-enforced budgets;
  the manager must not reach into subagent/process managers to enforce them.
- Acquisition failure returns Busy with the owning session, not a hidden queue
  of surprise future projects. Release a root slot only after root, children,
  and owned process cleanup settle.

Cross-process ownership is a prerequisite to advertise safe TUI/web coexistence.
Audit the existing lifetime locks: shared session opens primarily protect
lifetime/deletion, not necessarily exclusive execution. Add an app/session-owned
exclusive **runtime lease**, distinct from read handles and deletion locks, for
all durable runtimes across TUI, RPC, print, SDK, web, resume and fork transitions.
Acquire before executing session-bound startup effects; hold through close;
release on OS process exit; expose read-only browsing while leased. Test against
both processes and multiple apps in one process. Do not silently take over a
running TUI session. Older binaries that do not participate cannot be made safe
by an advisory lease; document the version boundary.

### Contexts and state

Use separate contexts for HTTP requests, manager lifetime, worker connections,
worker-owned root turns, and stream subscriptions. A browser disconnect cancels only its subscription, not the root
turn or its children. Stop is an explicit authenticated action using existing
cancel controls. Never keep an HTTP POST open for an entire provider turn.

Runtime states: inactive, starting, ready, running, waiting-permission,
waiting-input, stopping, failed, closing. A projection may show an attention
reason alongside a root's running state; do not rewrite protocol lifecycle
semantics to force a single flat UI enum.

Opening history is read-only. Starting/reopening execution is explicit and
applies project trust, provider validation, leases and interaction brokers
before enabling tools. An active persisted goal must not resume merely because
a historical session page was loaded; use the app's existing goal readiness
and continuation rules inside the worker when execution is deliberately activated.

Evict idle runtimes only when they have no active turn, pending interaction,
queued work, child activity, continuing goal, or managed process. Begin with a
15-minute eligible-idle timeout and make close/resource release observable.
Do not create one app or permanent worker per registered project. Catalog workers
remain runtime-free, bounded and evictable independently of execution slots.

### Restart and shutdown

- Stop accepting commands; mark the manager draining and inform subscribers.
- Reject/unblock pending questions and permissions; request cancellation of
  roots; stop child/process work through existing owners; join workers.
- Keep draining worker output, close worker stdin to trigger existing RPC EOF
  cleanup, and wait/reap each process. Worker-owned `App.Close` closes agents,
  stores and extensions. After a bounded grace period, escalate termination of
  the owned process group; do not claim forced termination ran graceful cleanup.
- Terminate SSE subscriptions and HTTP serving within the overall deadline.
- Keep health routes free of project details; readiness fails while draining.
- Restart restores metadata/history, not a promise to resume interrupted tool
  execution. Mark interrupted work honestly and require an explicit new action.
- Never automatically replay an uncertain prompt, shell command, approval,
  clone, or goal continuation after a crash.

Foreground mode is the first delivery. Document service-manager examples later;
closing a browser keeps work running, but manager failure closes its RPC pipes
and starts worker shutdown. A wedged worker or escaped descendant may require
operator cleanup; test abrupt parent death on supported platforms and do not
claim subprocess separation contains arbitrary OS-privileged code. Independent
durable worker reconnection is outside v1. No automatic daemon installation,
worker adoption, or detached/background launch from the startup flag.

## HTMX and streaming design

> **Historical target / future proposal, not the current stream contract.**
> This section retains the researched HTMX-extension, epoch/delta, replay and
> two-stream design for possible future work. The delivered implementation uses
> native-fetch LF SSE with one instance-bound full-snapshot subscription and
> one-slot wakeups; see [the current contract](#current-implementation-contract-snapshot-sse-and-presentation-polish).
> No replay ring, manager stream, durable submission receipt or automatic POST
> retry is implemented. The proposed limits and heartbeat below are not current
> constants.

### HTML ownership

Use server-rendered full pages and reusable fragments from the same templates.
Ordinary links/forms remain meaningful without HTMX; live streaming and enhanced
navigation require JavaScript. Set `Vary: HX-Request` where response shapes
actually differ, and never cache authenticated content in shared caches.

HTMX handles navigation, search, forms, validation, dialogs, and targeted
updates. Small external JavaScript modules handle focus, clipboard, drafts,
scroll anchoring, stream connection state and reconciliation. No parallel SPA
store or client-side reconstruction of agent/business state.

A form POST returns an acknowledgement or validation fragment promptly. Disable
its submit button while pending, preserve its request key across retry, and
clear the draft only after durable admission is known. Explicitly configure and
test handling for 409/422 error fragments instead of assuming HTMX swaps every
error status by default. Full non-HTMX success paths use POST/redirect/GET.

### Projection and stream protocol

Consume each worker's existing normalized `protocol.AgentEvent` stream once
through the RPC adapter. Apply events to a server-owned public projection.
The HTTP stream is a derived presentation channel, not a replacement provider
protocol and not a second durable conversation log.

Use one persistent SSE connection for the current session plus one lightweight
manager attention/activity stream per tab, not one connection per tool card.
Inactive session pages need only manager notifications until a runtime starts.

An SSE delivery has a manager-epoch and monotonic sequence ID, event name, and
server-rendered HTML fragment. Use stable DOM IDs based on session/branch,
message, tool call and agent identity. Named events include transcript update,
tool update, session status, attention update, usage, and resync-required.
Version this private wire shape, without promising a public SDK API.

Correctness requirements:

1. Atomically establish a public snapshot and its watermark with subscription
   registration. Events after the watermark are buffered so initial loading
   cannot miss a tool completion between a snapshot GET and stream connection.
2. Bound the ring by bytes and count. Start with 4 MiB/2,000 events per active
   root and a separate small manager ring; measure and tune, not unbounded RAM.
3. Resume from `Last-Event-ID` where available. HTMX extension-created new
   EventSource connections may not preserve the browser's last ID; maintain
   an explicit in-memory reconnect cursor and test extension reconnect paths.
   Put only a non-secret cursor in the URL, never auth credentials.
4. If the cursor is stale, unknown, from another epoch/branch, or beyond the
   retained window, issue resync-required and obtain a fresh bounded snapshot.
5. Replace stable nodes idempotently rather than blindly appending duplicate
   token/message HTML. Ignore stale projection revisions in the enhancement
   layer. Navigation invalidates the previous session's stream immediately.
6. Distinguish live provisional message blocks from persisted history, then
   replace them on authoritative completion. Never display both copies.
7. Bound each subscriber queue. Disconnect a slow client and require resync
   rather than blocking the agent callback on a network write. Overflow in the
   projection ingestion path must mark a gap and rebuild, never silently lose
   lifecycle/permission events.
8. Revalidate auth/expiry for open streams and close them on revoke/logout.
   Do not rely only on the initial SSE GET authorization.

Keep monitored agent subscriptions and draining inside the existing RPC event
forwarder. The adapter continuously drains its worker pipe into bounded queues;
web rendering never executes on that reader. The initial collaboration-mode
event is not a comprehensive manager snapshot. Normalize child attribution
(`Agent` versus `Subagent`), turn origin, root epoch and restoration snapshots
explicitly. Render HTML outside the pipe reader and worker agent callback.
Treat `ToolOutput` as a bounded preview, not proof that the complete persisted
result has been loaded.

Managed processes currently expose cursor-based list/log snapshots but no
normalized lifecycle event family. Poll through `processes_list`/`process_logs`
at a bounded rate only while needed and publish derived UI updates; do not
invent agent events merely to animate the Processes tab.

Use a stable stream-owning DOM element outside frequently swapped content.
Render multiline SSE data correctly, flush event boundaries, send a heartbeat
roughly every 15 seconds, disable proxy buffering where supported, and avoid
compression middleware that holds streaming output. Configure per-write
stream deadlines rather than a whole-response write timeout that kills SSE.

Coalesce text updates around 50–100 ms while keeping controls responsive.
Render provisional text safely and only rerender the current growing block;
finalize sanitized Markdown at meaningful boundaries. Do not parse and replace
an entire 20,000-message transcript on every token.

History starts with about 30 recent complete turns and a byte cap. Add
cursor-based older/newer pages with branch/generation validation and anchored
scroll restoration. Preserve tool-call/result context at page boundaries,
label incomplete windows, and fetch oversized outputs separately. The existing
RPC pagination implementation is useful evidence, but loading all history and
slicing afterward is not the end-state performance design.

### Submission idempotency

Button disabling is not enough: HTTP retries and two tabs can duplicate work.
Bind an unpredictable request key to project/session/branch, action and payload
digest. Same key/same payload returns its previous outcome; same key/different
payload is a conflict. Enforce durable input correlation at agent admission,
not merely before sending the RPC `prompt` command from the adapter.

The receipt DB and a session DB cannot provide a cross-file transaction for
free. Add worker-side app/session admission correlation and capability-negotiated
RPC receipt lookup, with crash reconciliation tests. The manager stores only its
request receipt and consumes public reconciliation results; it never opens the
session database. A successful pipe write or ordinary RPC correlation ID is not
durable admission evidence. If admission outcome cannot be proved after a restart,
mark the receipt uncertain and ask the user to inspect history; never silently
resubmit or claim universal exactly-once execution. Tool side effects are not
transactionally reversible.

## Permissions and user questions

The web host is a trusted interaction broker only after authenticated startup
and explicit broker wiring. Default root permission mode is `ask` when the
broker is operational, otherwise `deny`. A disconnected browser must never
convert ask into allow or disable hard shell-policy denials.

Use existing RPC `permission_reply`/`permission_reject` and
`user_input_reply`/`user_input_reject` commands. Their worker-side app brokers
remain authoritative; never instantiate a second permission broker in web.
Permissions and user questions are separate brokers: permissions serialize
root/child requests FIFO, while the question broker allows one pending request
per broker. The existing question settlement signal is not a complete pending
inventory, and permissions need equivalent settlement observation for robust
browser recovery. Add narrow worker-side snapshot/settlement facades and
additive public RPC contracts with tests rather than
assuming every resolved/cancelled request already has a matching UI event.

Maintain a manager-side **presentation index**, not a replacement permission
authority. Each pending item carries its request ID, root session, branch/turn
where applicable, child agent path, tool identity, risk, bounded arguments,
expiry and terminal state. Scope replies to that exact ownership tuple.

Approval card:

- Show exact command/file target and the runtime's structured shell-analysis
  summary, effects, unknowns, CWD, and whether remembering is supported.
- Default actions: Reject and Allow once. Add Remember only when the existing
  permission request explicitly permits the exact supported scope.
- Preserve the runtime's hard-denial behavior even if global mode is allow.
- No broad “Approve all forever” inferred from a browser convenience action.
- Show child attribution and shared-host warning where relevant.

Question card preserves one-to-three questions, options/Other, free-form input,
validation, submit and decline. Preserve partially typed answers while updates
arrive; do not replace the active question form with a stream fragment.

Lifecycle:

- Pending requests survive browser reconnect while their broker/runtime lives.
- Start with a visible 15-minute deadline; expiry rejects/unblocks, it never
  approves. Enforce the smaller bound if the existing broker context expires
  earlier. An explicit bounded extension can be a later action.
- Multiple tabs can see a pending card; the first valid reply settles it.
  Other replies return a stale/conflict response and refresh the terminal state.
- Cancel, branch/session close, process shutdown and relevant provider failure
  invalidate pending items and wake blocked callers.
- No broker reentry from event callbacks. HTTP replies use the typed backend
  outside projection locks; its RPC command reaches the authoritative worker
  broker. Completion becomes a public event/state update. Scope requests to the
  worker incarnation as well as session/request ID; never resend an approval
  after worker restart.
- Model text cannot manufacture a working approval button or a pending request.

The global Attention page and collapsed project counts are release-critical:
remote management fails if a hidden child can wait indefinitely without a
visible signal.

## Project and filesystem operations

> **Historical proposal:** startup-approved-root browsing/registration and SSH
> clone support below are not the delivered contract. Current browsing has the
> Snow OS user's filesystem authority, CREATE/clone pins the selected parent,
> and clone supports only anonymous HTTPS. There is no startup-root sandbox,
> SSH profile, git-init option, general Git editor or file-deletion control.
> See the expanded local-controls section for actual admission/cleanup bounds
> and the mandatory separate explicit-registration boundary.

### Registration and creation

Use existing pinned-root/symlink-safe primitives where they fit. Canonicalizing
once and then calling unrestricted filesystem operations is not sufficient:
validate root identity and operate through pinned handles at use time. Never
weaken file-tool protections to share a convenience helper with the web UI.

- Browse only startup-approved roots; cap entries, path depth and response size.
- Initial picker hides sensitive/protected directories server-side; a hidden
  file toggle must not disclose protected paths.
- Register only an existing directory under an allowed root; reject path
  traversal, encoded separator tricks, symlink escape and aliases that collide.
- Create accepts a parent-root/directory ID and one validated directory name,
  not an arbitrary shell command or concatenated absolute path.
- Do not adopt a pre-existing nonempty destination as a newly created project.
- Optional `git init` is a distinct checkbox/action with a fixed argument vector
  and controlled environment; do not execute project hooks during discovery.
- Adding a project is not granting trust. Preview trust-relevant configuration
  safely and reuse exact-project trust decisions before runtime startup.

For clone in v1: allow reviewed HTTPS and SSH URL forms; reject local/file,
remote-helper and arbitrary protocol schemes; disable prompts and recursive
submodules; use bounded output, timeout, process-group cancellation and explicit
SSH host-key/credential setup. Avoid inherited unsafe Git configuration and
credential echo. Treat clone as an explicit operator exec/network operation,
not a permission-free model tool. Do not proxy URLs supplied by unauthenticated
requests or silently enable project extensions afterward.

### Files and changes

Start read-only: tree, text preview, bounded file search and Git status/diff.
Show the scope of a diff honestly: current worktree changes can include changes
that predate the session. Label “since session start” only if an explicit
baseline was captured; branch selection does not magically restore files.

Use argument-safe commands or bounded readers, disable Git external diff/text
conversion/pagers and unsafe protocol/config effects, and preserve byte limits.
Large/binary files get a clear unsupported/truncated state rather than loading
into memory. Agent-produced HTML/SVG must not execute under the manager origin.
Download unknown/generated content as attachments; do not provide an implicit
same-origin app-preview server.

Use RPC-managed process log queries for development command inspection. A Stop
action must validate root/process ownership and use a capability-negotiated
worker command backed by the existing process manager, not a manager-side kill
by browser-supplied PID. This differs from supervising the RPC worker itself.
Do not add `/exec?cmd=...`, general terminal access, browser-editable environment
variables, or process-port proxying to achieve basic project management.

## Models and settings

Show provider ID, configured/expired/unavailable status, active model, supported
reasoning levels, permission mode and collaboration mode. Use actual capability
snapshots; do not hard-code provider model lists or reasoning enums in templates.
Usage and cost show known/unknown values explicitly, with separate root and
child totals to avoid double counting.

Separate controls for:

- Current session selection, subject to runtime admission/transition locks.
- Project defaults applied to future sessions through existing configuration
  persistence, with clear override precedence.
- Global operator defaults, saved through typed RPC settings commands backed
  by existing worker-side persistence; invalidate other workers explicitly.
- Appearance, which can update immediately without affecting the agent.

Do not serialize `app.Cfg`, auth stores, MCP headers, environment values or
plugin configuration wholesale to the browser. Use explicit allowlisted DTOs.
Global changes do not silently hot-reconfigure other running projects; show
which changes apply next session or require explicit idle-runtime reload.

Provider API-key and OAuth entry are excluded from the browser. Keep credential
setup in explicit host-terminal workflows or the separately documented
control-RPC API-key contract, using the existing atomic mode-0600 auth service.
The Web UI may provide exact host CLI instructions and bounded local provider
status, but must not accept credentials, run connection tests, copy browser
cookies, or invent an OAuth callback tunnel. Startup continues to work with
credentials already configured on the host.

MCP/skill/plugin controls must retain current enablement, trust, reload and
restart semantics. Render inventories and failures safely. A plugin's terminal
widget or arbitrary HTML is not automatically a trusted web component; support
shared structured dialogs explicitly and report unsupported UI capabilities.

## Remote access and browser authentication

### Trusted-LAN HTTP

`snow --mode web` exposes one exact private-IP HTTP origin plus an exact
localhost origin on the same port, both serving the shared manager directly.
Each listener enforces its own Host/Origin boundary and separate host-only cookie
names backed by profile-bound persisted sessions. Legacy unscoped sessions are
revoked during the one-time schema migration. Snow never binds `0.0.0.0`, `::`, a public or multicast address, a DNS
name, or a zone-qualified IPv6 address. Forwarding headers carry no origin,
identity, permission, or rate-limit authority.

The transport is deliberately unencrypted. The pairing code, cookies, prompts,
responses, and tool output can be observed by other parties on the network. Use
it only on a trusted home/work LAN, never public Wi-Fi or the Internet. Snow does
not configure firewalls, routers, DNS, mDNS, VPNs, browser HTTPS policy, or client
network isolation.

HTTPS cannot be accepted without a certificate. Rather than retaining an unused
certificate/proxy framework, the Web Manager exposes no TLS, generated-CA,
saved-profile, named-origin, trusted-proxy, Tailscale forwarding, or browser
API-key path. Provider credentials remain host-terminal or control-RPC concerns.

### Pairing and session lifecycle

Use browser pairing rather than credentials in query strings:

1. Create a cryptographically random reusable pairing code with a 30-day expiry.
   Persist the code in the private mode-0600 access store so foreground startup
   can reprint it after restart; derive its comparison hash in memory. Print the
   code and exact expiry only at foreground startup. Never put it in a URL,
   access log, event, diagnostic, or export.
2. The operator enters it into the exact-origin pairing form. Rate-limit attempts
   globally and per admitted network source, validate Host and Origin, and
   compare hashed secrets safely.
3. Issue a separate random browser-session credential. Never reuse the pairing
   code as the cookie value.
4. Persist only the cookie credential's hash and metadata. Set a host-only,
   HttpOnly, SameSite=Strict, path=/, non-Secure cookie: HTTP is the only
   supported transport, and there is no HTTPS exception or deployment profile.
5. Enforce the implemented 30-day idle and absolute limits and the eight-browser
   cap. Sign out revokes the current browser; Browser access can revoke one
   selected browser or all browsers. Active streams periodically recheck access.
6. A paired browser may deliberately rotate the pairing code through a
   CSRF-protected action without signing out existing browsers. Revoke-all also
   rotates the code; restart the foreground manager to print the replacement.

There is no `snow web` recovery/configuration command. Pairing codes and session
cookies never appear in URLs, SSE data, browser history, or exports, and a
caller-provided device label is never treated as identity. Forwarded headers
have no authority. Private-IP and localhost listeners each use their own exact
origin and separate local/LAN cookie names while sharing manager/runtime state.
Pairing one browser origin does not authenticate the other. In the offline
loopback fallback, loopback HTTP is the only exact configured origin.

## Threat model and operational bounds

> Current HTTP-only trusted-LAN threat bounds. The exact private-IP and direct
> same-port localhost origins, offline loopback fallback, periodic stream
> reauthorization, and current capabilities are canonical above and in
> `docs/security.md`. The table below includes historical target estimates rather
> than shipped limits (for example, current workers are two,
> pairing/browser limits are 30 days, and SSE uses no replay ring).

This is a remote control plane capable of authorizing host-level commands.
Its network/UI controls do not sandbox Snow, Bash, plugins, MCP or children.
Recommend a dedicated low-privilege account or external container/VM for
untrusted workloads, and protect manager/auth storage from agent modification
with external containment when a real host boundary is required.

Required defenses:

- Authenticate every project/session/file/action/stream endpoint, not only the
  main page. Opaque IDs are identifiers, not authorization secrets.
- Validate project/session/branch/agent/process membership server-side on every
  action. Reject stale and cross-project IDs before invoking runtime code.
- CSRF token on every unsafe method, including pairing/logout/settings;
  validate same-origin Origin and Fetch Metadata where available. Do not rely
  on SameSite alone. No state changes on GET, permissive CORS or JSONP.
- Exact Host/public-origin validation to resist DNS rebinding. Forwarded headers
  have no authority; reject unsupported direct Host and Origin values.
- Render text with `html/template`; sanitize Markdown through a reviewed
  allowlist, with raw HTML disabled. Strip scripts, event attributes, dangerous
  URL schemes and HTMX attributes from all model/repository/plugin content.
- Wrap untrusted content in HTMX-disabled regions; disable HTMX evaluation and
  script processing, avoid `hx-on`/`js:` expressions, and disable sensitive
  history snapshots (`hx-history="false"`, history cache off). External JS
  supplies event handlers under a strict CSP.
- CSP defaults to self with nonce/hash-based trusted bootstrap if needed, no
  unsafe-eval or unsafe-inline scripts, no objects, no embedding by other
  origins, self-only forms/connects, and no arbitrary remote images. Test HTMX
  configuration such as injected indicator styles against the chosen CSP.
- `Cache-Control: no-store` for authenticated HTML, SSE and sensitive file
  responses; immutable caching only for fingerprinted public static assets.
  Set nosniff and a same-origin referrer policy (preserving browser form Origin
  without cross-origin referrers). No service-worker transcript cache.
- Do not automatically fetch remote images/links from generated Markdown.
  External links use safe schemes and no opener; remote preview needs an
  explicit separate policy.
- Treat filenames, ANSI/terminal escape output, titles, search snippets and
  errors as untrusted, including when constructing attributes or CSS classes.
- Audit state-changing actions without logging prompts, secrets, private
  provider state, sensitive headers or tool argument bodies. Rotate/bound logs.
- Protect auth/config/manager DB paths and pinned-root operations with the same
  symlink/inode discipline as existing file tools; no global host file server.

Initial bounds to implement as named configuration/constants with tests:

| Resource | Starting bound |
|---|---|
| Active root runtimes | 4 globally, 1 per non-overlapping project |
| Outstanding unsafe HTTP actions | Bounded per-browser and global admission |
| Prompt command POST body | Implemented: 256 KiB ordinary forms; 4 MiB for gated `prompt-content` text/image forms |
| Prompt bytes | At most existing runtime/protocol limit, further capped by route |
| Directory listing | 500 entries/page and explicit continuation/truncation |
| Text preview | 256 KiB initial read; binary/oversized states, no automatic full read |
| History | ~30 turns plus a byte cap; bounded separately fetched tool outputs |
| Stream replay | 4 MiB/2,000 events per active root, bounded per-client queue |
| SSE heartbeat | 15 seconds; bounded connection count and idle cleanup |
| Pending interaction | 15-minute maximum initial deadline, cancellation-aware |
| Execution workers | At most 4 active roots, one per non-overlapping project |
| Lazy catalog workers | At most 2 runtime-free workers; evict after 30 idle seconds |
| Idle runtime eviction | 15 eligible idle minutes; never evict owned active work |
| Pairing | 5-minute TTL, one use, bounded attempt rate |
| Browser session | 24-hour idle / seven-day absolute expiry |

Choose explicit per-browser/global HTTP, stream, child-agent, process and audit
retention ceilings during the runtime spike. Measure memory and event volume;
these numbers are starting targets, not performance claims. Propagate contexts
and bound bodies, headers, pagination cursors, response bytes, process duration,
shutdown time, directory scans and Markdown rendering complexity.

## HTTP route contract

> Historical proposed route inventory, not an implemented API reference. The
> delivered SSE route is `GET /projects/{p}/runtime/events?instance_id=...`;
> neither the session-scoped event route nor `/events` manager stream below is
> implemented. Stream revocation is periodic, not synchronous with logout.

Routes are private first-party HTML endpoints in v1, not a new public REST API.
Use GET for views and POST for mutations to support ordinary forms. Every
mutation includes CSRF and, where retry-sensitive, an idempotency key and
expected entity revision. Parameter IDs are immutable server-validated IDs.

| Method and route | Purpose |
|---|---|
| `GET /login`, `POST /login` | Pair/authenticate without credentials in URLs. |
| `POST /logout` | Revoke current browser and close its streams. |
| `GET /` | Overview; never start a runtime. |
| `GET /projects`, `GET /projects/new` | Inventory and creation/registration form. |
| `POST /projects` | Register or create using an explicit operation kind. |
| `GET /directories` | Bounded listing under a server-configured root ID. |
| `GET /projects/{p}` | Project overview and session inventory. |
| `POST /projects/{p}/rename` | Display title only, not directory rename. |
| `POST /projects/{p}/remove`, `.../restore` | Registration lifecycle; never remove files. |
| `POST /projects/{p}/trust` | Explicit trust choice after safe preflight. |
| `POST /projects/{p}/sessions` | Start new-session flow with durable admission rules. |
| `GET /projects/{p}/sessions/{s}` | Bounded read-only snapshot and live status. |
| `GET .../{s}/history` | Cursor-based public history fragment. |
| `POST .../{s}/prompts` | Admit a root prompt or explicit follow-up operation. |
| `POST .../{s}/cancel` | Cancel existing runtime work; no new execution. |
| `POST .../{s}/queue/...` | Only RPC-supported queue operations; no invented per-item edit/remove semantics. |
| `POST .../{s}/rename`, `.../archive`, `.../restore` | Session presentation operations. |
| `POST .../{s}/branches/...`, `.../fork`, `.../compact` | Typed RPC-backed durable history controls. |
| `GET .../{s}/events` | Authenticated bounded SSE presentation stream. |
| `GET /events` | Manager attention/activity SSE stream. |
| `GET /attention` | Global pending decision inventory. |
| `POST /interactions/{id}/reply` | Exact attributed permission/question reply. |
| `GET /projects/{p}/files`, `.../changes` | Bounded safe filesystem/Git previews. |
| `GET .../{s}/processes/{pid}/logs` | Cursor-based RPC-managed log retrieval. |
| `POST .../{s}/processes/{pid}/stop` | Explicit process-owner checked stop. |
| `GET /settings`, scoped `POST /settings/...` | Allowlisted settings and browser access. |
| `GET /healthz` | Minimal liveness only; no secrets, roots, models or sessions. |

Create route-specific equivalents for goal/subagent actions as those phases
land. Do not implement a generic `/command` dispatcher accepting arbitrary
method names, app fields, tool calls or unrestricted configuration patches.
Use semantic error states: 401 login needed, 403 forbidden, 404 unknown/out of
scope, 409 busy/stale/conflicting receipt, 422 validation, 429 admission limit,
and 503 draining/unavailable. Return bounded safe errors with recovery actions.

## Implementation sequence

Each phase must leave a usable, tested increment. Dependencies matter more than
calendar estimates; do not begin all phases as parallel mutators sharing core
lifecycle files. UI fixtures can progress independently from runtime adapters.

### Phase 0: contracts and risk spikes

Deliver:

- Approve optional-web scope wording and this feature matrix.
- Finalize CLI flags, pairing recovery syntax and default roots.
- Implement design fixtures/component gallery with the intended layout,
  typography, interaction states and mobile views before provider wiring.
- Freeze small backend interfaces and a capability/command map against current
  RPC. Build the fake backend and a bounded client spike: handshake, interleaved
  replies/events, prompt acknowledgement versus completion, EOF and pipe stalls.
- Specify opt-in lazy RPC startup, public catalog/snapshot and interaction
  contracts, including worker-incarnation/snapshot sequencing. Ordinary RPC
  startup remains backward compatible; manager browsing never creates an app.
- Spike worker-side public history snapshots, atomic snapshot/subscription
  handoff, runtime leases, durable prompt correlation, broker reconnect,
  two independent project workers and bounded subprocess shutdown.
- Review HTMX/SSE versions, sanitizer/CSP behavior, existing Markdown packages,
  SQLite conventions, proxy headers and dependency licenses.

Exit: written decisions and passing mocked prototypes for the risky boundaries;
no externally reachable server or implicit provider work. If a facade gap needs
core changes, land it with TUI/RPC/SDK parity tests first.

### Phase 1: authenticated shell and mode

Owned areas: `cmd/snow/web.go`, new web server/auth/security/static/template
files, operator configuration and targeted CLI tests.

Deliver embedded assets, local listener, mode validation, pairing/login/logout,
security headers, health/readiness, graceful shutdown, and fixture-driven
Overview/Project/Session pages. No real agent tools available yet.

Exit: auth/origin/CSRF tests pass; no CDN requests; normal CLI modes unchanged;
no app/session/provider is created by starting the manager or opening a page.

### Phase 2: project registry and read-only history

Owned areas: manager store/projects, project filesystem service, reusable RPC
client/adapter, lazy RPC startup and worker-side catalog/public-history
contracts, project/history templates.

Deliver migrations, add/create/rename/remove/restore, missing-root handling,
trust preview, session lists, public transcript pages, safe file preview and
project/session metadata search. Implement bounded lazy catalog worker pooling
and typed RPC queries; use existing Snow index/history logic inside workers,
without copying conversations or opening session databases from the manager.
Land additive public DTO/schema changes and worker-side read facades together.

Exit: imported project removal preserves files and sessions; aliases/duplicate
basenames/missing directories behave correctly; untrusted project extensions
cannot execute during inventory/history browsing; pagination is bounded.
Catalog workers handshake without constructing an app, creating a session or
resuming goals. Missing required capabilities fail explicitly. Import-boundary
tests prevent web/manager/client dependencies on agent runtime internals.

### Phase 3: complete agent vertical slice

Owned areas: worker-side runtime lease/admission and RPC capabilities,
manager worker supervisor/backend handles, projection/streams/interactions,
prompt/tool/composer components.

Deliver explicit worker runtime activation using the unchanged agent loop,
fake-provider streaming, prompt acknowledgement/durable correlation, cancel,
follow-up queue, public tool cards, authoritative permissions/questions,
worker snapshot reconciliation, browser reconnect/resync and shutdown cleanup.
Keep runtime cancellation distinct from canceling an HTTP request or abandoning
one RPC response wait. Accepted work continues until explicit worker control.

Exit: two tabs cannot duplicate a turn or approve the wrong request; browser
return reconstructs current state; pending root/child interactions settle on
timeout/cancel; full-history invariants pass. Test worker exit before/after
prompt acknowledgement and durable persistence without replaying uncertain
work. One failed worker leaves another project's agent and the manager usable.
Browser disconnect leaves worker pipes open; EOF/manager shutdown closes work
through existing RPC owners. This is the first usable local private alpha,
not yet the remote release.

### Phase 4: private remote operation and resource limits

Status: automatic trusted-LAN HTTP is implemented. Ordinary startup selects one
assigned private address, binds it and localhost on port 7331, serves the shared
manager directly on both exact origins, and falls back to loopback when offline.
Exact per-listener Host/Origin, origin-specific cookies, CSRF, pairing,
revocation, per-peer throttling, bounded HTTP servers, and fail-closed
dual-listener lifecycle remain covered.

The earlier TLS/certificate/generated-CA/saved-profile/DNS/trusted-proxy design
was removed rather than retained as unused complexity. Real-device acceptance
must use the explicit HTTP URL and a trusted LAN; browser HTTPS-only settings are
client policy, not a server feature.

Remaining Phase 4 work is limited to aggregate resource accounting, recovery,
and broader device/network acceptance—not alternate network transports.

### Phase 5: standard Snow control coverage

Deliver model/reasoning/permission/session-default controls, Plan Mode review,
branch/fork/compaction, goal controls and usage, subagent controls, managed
process stop/logs, read-only Git review, extension inventory and supported
controls, session archiving, and bounded clone jobs.

Exit: each supported action maps to a typed, capability-gated RPC operation
backed by the existing runtime and has a busy/unsupported/failed state. No UI
action bypasses the adapter to access app fields, stores or process internals. Browser OAuth, PTY, arbitrary app previews,
workspace destruction and Git write operations remain out of scope unless
separately approved.

### Phase 6: polish, performance and first release

Deliver complete accessibility/mobile/visual regression coverage, long-session
fixtures, stream stress tests, local/network operational guides, changelog and
packaging validation. Add command palette/shortcuts only after core navigation
is keyboard-complete; prioritize reliable task completion over novelty.

Exit: all verification gates below pass, documentation states exact limitations,
and manual provider smoke is completed without secrets in artifacts. Follow
`docs/releases.md#next-release-runbook` for any published version. After a
successfully verified implementation feature change, run
`./scripts/install-local.sh` as required by the repository workflow; this
research-only change does not require installing a new binary.

## Verification and release gates

The current runnable browser checks and their actual scope are listed in the
current implementation contract above. The following is the broader historical
release target; epoch/replay, remote proxy, child/process and future package
checks are not implemented test coverage or evidence of completed gates.

### Automated tests

Keep the normal suite network-free. Use fake providers/local HTTP mocks and
temporary Snow homes, roots, clocks and stores.

- Unit: registry identity, path policy, migrations, leases, public block
  projection, cursor invalidation, HTML escaping, sanitized Markdown and
  interaction ownership/expiry.
- Boundaries/contracts: prevent runtime-internal imports from web/manager/client;
  run shared backend contract tests against fake and real RPC workers. Check
  additive schema conformance and compatibility with existing RPC clients.
- RPC adapter: handshake/version/capabilities, stdout LF framing across partial
  reads, interleaved responses/events, unknown additive frames, malformed and
  oversized output, bounded stderr, stalled writes, EOF during startup/turn,
  cancellation and request acknowledgement versus `prompt_completed`. Use the
  advertised input bound and an explicit client-side output bound. A reader
  must never wait on HTML rendering or a slow browser.
- Lazy startup: inventory/history/trust preflight create no app/session, load
  no project extensions, and do not resume goals; inactive snapshots never
  switch an active worker's session. Activation/restart uses fresh identity and
  restores brokers/subscriptions before execution. Bound catalog worker count.
- Worker supervision: independent CWD/session/events for two projects, one
  worker crash leaving another usable, browser disconnect keeping pipes open,
  stdin EOF and graceful termination cleaning up, forced termination/abrupt
  manager death, no automatic prompt/approval replay, no PID-reuse takeover,
  no worker-command shell interpolation, and no secret leakage in diagnostics.
- HTTP: mode flags, auth, CSRF, Origin/Host, proxy spoofing, cookie attributes,
  no-store/Vary, body limits, pagination, scope checks and safe error fragments.
- Lifecycle: cancel during startup/provider/tool/permission/question, close
  with children/processes, browser reconnect, manager restart, SIGTERM,
  runtime eviction and disk-full/admission partial outcomes.
- Stream: snapshot/subscribe race, replay overflow, epoch change, duplicate and
  out-of-order projection revisions, multiple tabs, slow subscriber, auth
  expiry/revocation on open streams and SSE proxy buffering.
- Security regression: path traversal and symlink swaps, malicious filename,
  Markdown HTML/HTMX injection, unsafe links, secret field DTO exclusion,
  fake approval text, stale reply, disallowed project-local listener config.
- Invariants: unchanged append-only parent tree, branch tip, tool pairing,
  provider continuity retention, compaction boundary and existing Plan Mode
  mutation gates across TUI/RPC/SDK/web.
- Browser: use a pinned browser automation dev dependency in CI, not a runtime
  requirement. Chrome/Firefox plus mobile Safari checks where infrastructure
  supports them; no downloading test tooling in the normal Go test path.

Proposed implementation verification commands, once packages exist:

```sh
gofmt -w <changed-go-files>
go test ./cmd/snow ./pkg/agentclient/... ./internal/web/... ./internal/manager/...
go test ./internal/app ./internal/agent ./internal/session ./internal/rpc ./pkg/snowsdk
go test -race ./pkg/agentclient/... ./internal/web/... ./internal/manager/...
go test -race ./internal/app ./internal/agent ./internal/session ./internal/rpc ./pkg/snowsdk
go test ./...
go vet ./...
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
python3 scripts/check_benchmarks.py
(cd examples/sdk && go test ./... && go run .)
go build -o ./snow ./cmd/snow
```

Add browser and manager load-test commands to the canonical verification matrix
when the tooling exists; do not present hypothetical commands as checks run.
Preserve Go 1.27/1.27rc3 documentation synchronization and the existing release
CI, vulnerability and secret-free provider smoke requirements.

### End-to-end product acceptance

1. Start with the web flag and pair a desktop browser; normal `snow` still
   launches the TUI. No provider call occurs before an explicit task.
2. From a phone over Tailscale, create a project folder on the host, review
   trust, select a provider/model, run a task and approve a bounded file edit.
3. Move the phone offline during generation, return, and see exactly one task,
   current tool state and no duplicate answer. A waiting question reappears.
4. Read an older answer while output streams; neither token updates nor opening
   the inspector destroys scroll position, text selection, focus or the draft.
5. See a hidden child's pending approval in the global attention count; deny it
   and observe the correct owning session settle.
6. Run two different non-overlapping projects; reject a conflicting second root
   in the same project and reject a session already leased by a participating
   TUI/RPC/SDK process without stealing it.
7. Remove a project registration, verify files and session DBs remain, restore
   it, and resume the same history. A missing folder is not silently recreated.
8. Fork/select branches and compact a long thread; exact old history remains
   available and no provider-private data reaches HTML or downloads.
9. Stop a root with active processes/children, then shut down the manager;
   there are no orphan runtime workers or forgotten waiting broker requests.
10. Revoke a second browser while it is streaming; subsequent actions fail and
    its live stream closes. Hostile Origin/Host and forged proxy headers fail.
11. Complete the same core journeys keyboard-only and at a 360-pixel viewport,
    with visible focus, accessible labels, readable contrast and no unintended
    horizontal page scroll.
12. Terminate one project worker during a fake-provider turn. The manager and
    another project remain usable; the affected session shows interrupted or
    uncertain admission honestly. Restart requires explicit activation, issues
    fresh worker identity, and never replays an old prompt or approval.
13. Browse many inactive projects through the bounded lazy catalog pool. No
    provider request, plugin startup, new conversation or goal continuation
    occurs; opening history never switches a running worker's session.

### Performance targets

These are proposed measured budgets, not benchmark results:

- Warm local non-provider command acknowledgement: p95 below 200 ms on a
  documented reference machine; provider latency measured separately.
- Event-to-visible update: p95 below 250 ms locally under expected concurrency.
- Initial authenticated shell assets: aim below 250 KiB compressed, excluding
  optional large content; no runtime third-party assets or tracking requests.
- Inventory pagination at 100 projects/10,000 sessions without app startup per
  row or rescanning all SQLite histories for every keystroke.
- Long transcript fixture: 20,000 messages while initial render loads only a
  bounded window, and streaming does not grow DOM/memory without bounds.
- Multi-hour slow-client/reconnect soak with four roots: no monotonic goroutine,
  subscription, replay-buffer or file-descriptor growth after idle cleanup.

Record baseline hardware, workload, method, memory, event rates and failures.
Use browser traces and screen recordings to judge UX only after there is a
rendered implementation; screenshots alone cannot verify runtime correctness.

## Decisions and remaining product choices

The plan is implementation-ready in direction without requiring another large
requirements interview. Confirmed choices are single-user/single-host,
DeepSeek Harness as the reference, HTMX, private remote access, keeping
project files on removal, and the RPC-worker backend selected by the operator.

Recommended defaults to accept or change before phase 1:

| Choice | Recommendation |
|---|---|
| Entry point | `snow --mode web`, consistent with existing surface selection. |
| Scope wording | Optional first-party web surface; core remains UI-independent. |
| Backend (selected) | Existing Snow RPC behind typed backend interfaces; no direct app/agent/session access from manager. |
| Worker topology | One lazy-activated worker per active project/session, bounded runtime-free catalog pool; separate root agents, not manager subagents. |
| Appearance | Snow-neutral light/dark/system, three-pane desktop and phone-first task flow. |
| Parallel work | Four distinct-project roots; one root per project; no implicit fleet scheduling. |
| Remote access | Automatic trusted-LAN HTTP plus Snow pairing; direct exact-origin localhost service and offline loopback fallback. |
| Filesystem authority | Launch-configured roots; create/register/remove, no destructive workspace deletion. |
| Credential setup | Host-terminal or control-RPC only; browser API-key and OAuth entry excluded. |
| Terminal/editor | Safe files/diffs/process logs first; no PTY or full IDE in v1. |
| Git | Status/diff and bounded clone in v1; commit/push/worktree features later. |
| Session ownership | Read-only inspection across surfaces, exclusive participating-runtime execution lease. |

Highest engineering risks are shared runtime ownership, lazy RPC startup,
worker supervision/pipe backpressure, crash-safe prompt correlation, complete
public snapshots, broker reconnect,
HTML/HTMX injection, and slow-client backpressure. Resolve these before building
large settings pages or optional features. A polished interface is not a reason
to weaken permission, filesystem, or session invariants.

## Research sources

DeepSeek links below are pinned to the researched commit. Their documentation
and accessibility snapshots establish documented/source behavior, not a live
visual audit. HTMX and Tailscale links are official documentation fetched during
research; their supported versions and CLI behavior must be rechecked at the
implementation gate.

- D1: [DeepSeek README][dsh-readme].
- D2: [Web UI guide][dsh-guide].
- D3: [Workspace subsystem and identity][dsh-workspace].
- D4: [Workspace UI behaviors and limitations][dsh-ui-workspace].
- D5: [Host-directory browser UI][dsh-picker] and
  [workspace picker component][dsh-picker-source].
- D6: [Layout geometry and responsive behavior][dsh-layout].
- D7: [Approval UI][dsh-approval].
- D8: [Chat rendering and scroll ownership][dsh-chat].
- D9: [Theme tokens and preferences][dsh-theme].
- D10: [Web boot and React/Cordis module composition][dsh-web].
- D11: [HTTP server, bind and composition security][dsh-server].
- D12: [Safety notice][dsh-safety].
- D13: [Committed initial-screen accessibility snapshot][dsh-snapshot].
- H1: [HTMX documentation, security and configuration][htmx].
- H2: [HTMX SSE extension][htmx-sse].
- T1: [Tailscale Serve CLI reference][tailscale-serve].
- T2: [Tailscale CLI reference and Funnel distinction][tailscale-cli].
- T3: [Tailscale Serve identity and proxy behavior][tailscale-feature].

Context7 was used to resolve HTMX and Tailscale documentation libraries and
retrieve SSE/Serve references. No provider credentials, repository code, or
private configuration were sent in those documentation queries.

[dsh]: https://github.com/deepseek-ai/deepseek-harness
[dsh-readme]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/README.md
[dsh-guide]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/docs/user/guide/index.md
[dsh-workspace]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/docs/subsystems/workspace.md
[dsh-ui-workspace]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/client/ui-workspace/README.md
[dsh-picker]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/client/ui-directory-picker-browse/README.md
[dsh-picker-source]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/client/ui-workspace/src/client/WorkspacePicker.tsx
[dsh-layout]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/client/ui-layout/README.md
[dsh-approval]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/client/ui-approval/README.md
[dsh-chat]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/client/ui-chat/README.md
[dsh-theme]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/client/ui-theme/README.md
[dsh-web]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/packages/client/web/README.md
[dsh-server]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/docs/subsystems/web-server.md
[dsh-safety]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/SAFETY.md
[dsh-snapshot]: https://github.com/deepseek-ai/deepseek-harness/blob/c291e7961a515f6d7af9304e7fd1d257929aef26/snapshots/web/lifecycle-chrome/hero.expected.md
[htmx]: https://htmx.org/docs/
[htmx-sse]: https://htmx.org/extensions/sse/
[tailscale-serve]: https://tailscale.com/docs/reference/tailscale-cli/serve
[tailscale-cli]: https://tailscale.com/docs/reference/tailscale-cli
[tailscale-feature]: https://tailscale.com/docs/features/tailscale-serve

## Related documents

- [Architecture and roadmap](../IMPLEMENTATION.md)
- [Security model](security.md)
- [Session storage internals](session-storage-internals.md)
- [RPC reference](rpc.md)
- [Maintainer documentation ownership](maintaining.md)
