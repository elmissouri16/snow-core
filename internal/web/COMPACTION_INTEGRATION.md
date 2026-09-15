# Manual compaction integration hooks (web child owns new files)

Shared runtime integration completed by compaction owner:
- `runtime_events.go`: owned snapshot/receipt clones for Compaction and Steer; compaction buffering before goal/message-edit buffering; compaction consumption before goal/generic projection; exact-root queue events apply steering before queue projection.
- `runtime_cancel.go`: cancellation task retirement invokes `finishCompactionLocked`.
- `runtime_control.go`: fail marks active manual operation uncertain before publish.
- `runtime.go`: stop marks active manual operation uncertain before publish (only stop touched).
- `runtime_stream.go`: owned separately by steer agent; no compaction-agent edits.

Full integration contract (remaining surface/capability wiring owned by parent):
- RuntimeSnapshot: `Compaction *RuntimeCompaction json:"compaction"`, `CompactionACK *RuntimeCompactionACK json:"compaction_ack,omitempty"`, `CompactionEnabled bool json:"compaction_enabled"`.
- liveRuntime: `compaction runtimeCompactionState`.
- snapshot clone: `s.Compaction = s.Compaction.clone()` and copy ACK with `new(*s.CompactionACK)`.
- Activation: CompactionEnabled requires `compaction_run`, `messages_public_history`, `goal_run` (exact branch/tip via existing Goal inspection). Reset compaction and snapshot projection on session switch.
- drain: before goal/message-edit buffering invoke `bufferCompactionEventLocked(event)` under eventMu+mu. If not buffered invoke next existing buffer.
- consumeEvent: invoke `consumeCompactionEvent(event)` BEFORE goal/generic handling. It consumes every event during manual ownership (allowlists only correlated native progress); generic turn_done cannot settle.
- fail/close: under mu call `uncertainCompactionLocked()` before public publish; must not overwrite already terminal completion state.
- cancellation retirement: under mu invoke `finishCompactionLocked()` after task retirement. It releases only after completion + authoritative refresh + no cancel task.
- Cancellation captured authority: compaction pending has local Stop token before dispatch, active ACK captures compaction.accepted TurnID/RootEpoch/TurnSequence (origin `compact`). Native root Stop should support this authority. `busy=true`, `transitioning=false` keeps Stop admitted; root controls must reject `compaction.pending || compaction.active` (especially queue, which ordinarily allows busy prompt).
- HTTP: route `compaction-start` to `runtimeCompactionAction` after existing auth/CSRF/form parse/project lookup.
- render: CompactionEnabled template field + static compaction.js/css; include `{{template "compaction" .}}` in Versions/context entry area.
- JS: `SnowCompaction.init({root, identity, openDialog, closeDialog, reserve, commit})`; reserve binds shared panel owner `compaction`; commit(fields) calls existing runtimeAction('compaction-start', fields). `render(snapshot, {supported, readable, safe})` with safe idle mutationSafe('compaction'). dispose on replacement. Compaction admissions count as streamingAdmission; do not disable captured Stop during HTTP action/panel reservation. `SnowCompaction.validACK(result, identity, fields)` validates immutable response ACK, independent of current completion state. Unknown HTTP ACK triggers existing read-only snapshot reconciliation only; never replay.

StartCompaction CAS uses Goal's current session/branch/tip, positive exact snapshot revision, no nonterminal goal, queues, attention, or active runs. Plan Mode is allowed. Core remains final authority over reviews/subagents. Completion synchronously reads bounded public history, goal inspection and telemetry while keeping busy, before releasing. Native summaries/provider error text never copied.
