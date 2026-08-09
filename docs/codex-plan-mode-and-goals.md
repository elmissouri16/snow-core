# How OpenAI Codex implements Plan Mode and Thread Goals

Analysis of the open-source `openai/codex` repository (Rust implementation,
`codex-rs/`), shallow-cloned at commit
`208f05b23387c47d7e52fd2153d59255e945d0b7` (2026-08-07, "Enforce automatic
review for managed models"). All file paths below are relative to the Codex
repo root.

This document explains the two features end to end: **Plan Mode** (a
collaboration mode with read-only planning behavior and a structured
`<proposed_plan>` output contract) and **Thread Goals** (persisted per-thread
objectives with budgets, usage accounting, and automatic idle continuation).

---

## Part 1 — Plan Mode

### 1.1 What Plan Mode is (and is not)

Plan Mode is a **collaboration mode**, not a tool. It is a mode the user
switches the whole session into, and it changes:

- the developer instructions injected into the model prompt,
- the default reasoning effort,
- which tools behave how (`update_plan` is rejected, `request_user_input` is
  allowed and blocking),
- how assistant output is streamed and displayed (a special
  `<proposed_plan>` block becomes a rendered plan item instead of chat text),
- whether automatic/idle turns are allowed,
- whether token usage is charged to a thread goal.

A common confusion is `update_plan`: that is a **TODO/checklist tool** used in
Default mode for step-by-step progress tracking. The plan-mode developer
instructions explicitly say *"Plan Mode is a collaboration mode... `update_plan`
is a checklist/progress/TODOs tool; it does not enter or exit Plan Mode...
If you try to use `update_plan` in Plan mode, it will return an error."* The
handler enforces this:

```rust
// codex-rs/core/src/tools/handlers/plan.rs
if turn.mode == ModeKind::Plan {
    return Err(FunctionCallError::RespondToModel(
        "update_plan is a TODO/checklist tool and is not allowed in Plan mode".to_string(),
    ));
}
```

### 1.2 Mode data model

`codex-rs/protocol/src/config_types.rs`:

```rust
pub enum ModeKind {
    Plan,
    #[default] Default,   // serde aliases: "code", "pair_programming", "execute", "custom"
}

pub const TUI_VISIBLE_COLLABORATION_MODES: [ModeKind; 2] = [ModeKind::Default, ModeKind::Plan];

pub struct CollaborationMode {
    pub mode: ModeKind,
    pub settings: Settings,   // model, reasoning_effort, developer_instructions, ...
}
```

`ModeKind` is the wire type; `CollaborationMode` bundles a mode with its
settings (which can carry a different model, reasoning effort, and custom
developer instructions per mode). In the TUI a mode is applied as a
`CollaborationModeMask` (partial application: mode + optional model +
optional effort + optional instructions), so switching modes can also switch
models.

### 1.3 Built-in mode presets

`codex-rs/models-manager/src/collaboration_mode_presets.rs` defines the two
built-in presets:

```rust
fn plan_preset() -> CollaborationModeMask {
    CollaborationModeMask {
        name: ModeKind::Plan.display_name().to_string(),
        mode: Some(ModeKind::Plan),
        model: None,
        reasoning_effort: Some(Some(ReasoningEffort::Medium)),   // plans get more thinking
        developer_instructions: Some(Some(COLLABORATION_MODE_PLAN.to_string())), // plan.md template
    }
}
```

`COLLABORATION_MODE_PLAN` is the embedded `plan.md` template
(`codex-rs/collaboration-mode-templates/templates/plan.md`), which is the core
behavioral spec. Its key rules:

- **3 phases**: (1) ground in the environment with non-mutating exploration,
  (2) intent chat, (3) implementation chat until the spec is
  "decision complete" (no decisions left for the implementer).
- **Execution vs mutation**: only *non-mutating* actions are allowed (reads,
  searches, dry-run commands, tests/builds that don't edit repo-tracked
  files). Mutating actions (editing/writing files, formatters/linters that
  rewrite, applying patches/codegen, side-effectful "doing the work"
  commands) are prohibited. This is enforced by instruction, not by tool
  filtering.
- **Questioning**: strongly prefer the `request_user_input` tool; only ask
  questions that materially change the spec or lock an assumption; explore
  before asking ("discoverable facts" vs "preferences/tradeoffs").
- **Finalization contract**: when the plan is decision-complete, wrap it in a
  `<proposed_plan>` block (opening tag on its own line, Markdown inside,
  closing tag on its own line, at most one block per turn, complete
  replacement on revisions). Do not ask "should I proceed?".

Models can override or supply their own mode instructions through the model
catalog: `ModelMessages.collaboration_modes: CollaborationModeMessages
{ default: Option<String>, plan: Option<String> }`
(`codex-rs/protocol/src/openai_models.rs`).

### 1.4 Prompt injection: collaboration mode as a world-state section

The mode instructions are injected into the model context as a
**developer-message fragment** with a persistent marker, managed by the
world-state section `codex-rs/core/src/context/world_state/collaboration_mode.rs`:

- `CollaborationModeState` snapshots `{ mode, model }` plus the resolved
  instructions (catalog message first, then
  `settings.developer_instructions`).
- It renders as `CollaborationModeInstructions` with markers
  `<collaboration_mode>` / `</collaboration_mode>` and role `developer`.
- It participates in the world-state **diff** machinery: when the mode or
  model changes, a new fragment is appended; when unchanged, nothing is
  re-emitted. It also matches *retained* fragments so compaction/restore
  doesn't duplicate the instructions.

The fragment is persisted in history, so the mode survives compaction and
resume (it is re-rendered as part of the retained context).

### 1.5 Turn-loop integration

`codex-rs/core/src/session/turn.rs`:

```rust
let plan_mode = turn_context.mode == ModeKind::Plan;
let mut assistant_message_stream_parsers = AssistantMessageStreamParsers::new(plan_mode);
let mut plan_mode_state = plan_mode.then(|| PlanModeStreamState::new(&turn_context.sub_id));
```

`PlanModeStreamState` is **ephemeral per-response state** (deliberately not
persisted; the final plan text is re-extracted from the completed assistant
message):

```rust
struct PlanModeStreamState {
    pending_agent_message_items: HashMap<String, TurnItem>,  // agent msg starts deferred
    started_agent_message_items: HashSet<String>,
    leading_whitespace_by_item: HashMap<String, String>,
    plan_item_state: ProposedPlanItemState,                  // one plan item per turn
}
```

Key streaming behaviors in plan mode:

1. **Agent message starts are deferred** until the parser emits non-plan
   text, so a plan-only response never shows up as an empty assistant message
   (`maybe_emit_pending_agent_message_start`).
2. **Leading whitespace is buffered** per item until non-whitespace text
   rules out a tag prefix.
3. **Plan content is routed to a `TurnItem::Plan`** (a `PlanItem` with its own
   id `<turn_id>-plan`) and streamed via `PlanDelta` events, while normal text
   goes to `AgentMessageContentDelta` events.
4. On item completion, `maybe_complete_plan_item_from_message` re-extracts the
   plan text from the finalized message and completes the plan item
   (`emit_turn_item_completed`), so the final plan is authoritative even if
   streaming was interrupted.
5. `flush_assistant_text_segments_for_item` / `..._all` flush buffered parser
   state at item end and response completion.
6. In plan mode, seeded text for a new agent message is stored empty
   (`text: String::new()` when `plan_mode`) and streamed only through the
   segment machinery.

### 1.6 The streaming `<proposed_plan>` parser pipeline

`codex-rs/utils/stream-parser/` is a small incremental-stream parsing
library:

- `tagged_line_parser.rs` — a generic incremental parser for `TagSpec { open,
  close, tag }` line-oriented markup, emitting
  `TaggedLineSegment::{Normal, TagStart, TagDelta, TagEnd}` as chunks arrive
  (handles tags split across chunk boundaries, unterminated tags on finish).
- `proposed_plan.rs` — `ProposedPlanParser` wraps it with
  `<proposed_plan>`/`</proposed_plan>` and maps segments to
  `ProposedPlanSegment::{Normal(String), ProposedPlanStart,
  ProposedPlanDelta(String), ProposedPlanEnd}`. It also provides
  `strip_proposed_plan_blocks(text)` (used for display text) and
  `extract_proposed_plan_text(text)` (used to finalize plan items).
- `assistant_text.rs` — `AssistantTextStreamParser` composes
  `CitationStreamParser` (strips `<oai-mem-citation>` tags and extracts
  citations) with the plan parser, in one pass. In non-plan mode the plan
  parser is skipped entirely.
- `stream_text.rs` — `StreamTextChunk { visible_text, extracted }` carries
  both the cleaned display text and the ordered extracted segments.

`handle_plan_segments` in `turn.rs` consumes the segments:

| Segment | Effect |
|---|---|
| `Normal(delta)` | Whitespace-only deltas buffered; real text emits pending agent-message start then `AgentMessageContentDelta`. |
| `ProposedPlanStart` | Starts the `TurnItem::Plan` (emits `item started`). |
| `ProposedPlanDelta(delta)` | Ensures plan started; emits `EventMsg::PlanDelta`. |
| `ProposedPlanEnd` | No-op (completion happens from the finalized message). |

`codex-rs/core/src/stream_events_utils.rs` also strips
`<proposed_plan>` blocks from realtime/text event payloads so downstream
display never shows raw tags.

### 1.7 Plan-mode behavioral gates

Beyond instructions, plan mode has hard behavioral gates:

1. **`update_plan` rejected** in Plan mode (Section 1.1).
2. **`request_user_input` is Plan-mode-only and blocking**
   (`codex-rs/core/src/tools/handlers/request_user_input.rs`):

   ```rust
   if let Some(message) = request_user_input_unavailable_message(mode, &self.available_modes) {
       return Err(FunctionCallError::RespondToModel(message));
   }
   let args = RequestUserInputArgs { questions: args.questions,
       is_blocking: mode == ModeKind::Plan, auto_resolution_ms: None };
   ```

   Also `ModeKind::allows_request_user_input()` returns `matches!(self,
   Self::Plan)`, and the tool description shown to the model is generated per
   `available_modes`. Only the root thread may use it.
3. **No autonomous idle turns in Plan mode**
   (`codex-rs/core/src/session/inject.rs`): `try_start_turn_if_idle` rejects
   input without user content when `collaboration_mode().mode ==
   ModeKind::Plan` (`TryStartTurnIfIdleRejectionReason::PlanMode`). This is
   the same gate goal continuation uses, so **goals never auto-continue in
   Plan mode**.
4. **Goal accounting excludes Plan mode**
   (`codex-rs/ext/goal/src/accounting.rs`): on turn start,
   `GoalTurnAccounting::new(..., !matches!(collaboration_mode, ModeKind::Plan))`
   sets `account_tokens = false`, and the goal extension calls
   `clear_current_turn_goal()` in Plan mode — planning time is not charged to
   a goal.

### 1.8 TUI surface

- **`/plan`** (or `/plan <message>`) —
  `codex-rs/tui/src/chatwidget/slash_dispatch.rs` →
  `apply_plan_slash_command()` → `collaboration_modes::plan_mask(catalog)` →
  `set_collaboration_mask_from_user_action(mask)`, which updates the mask,
  refreshes the mode indicator/nudge, and pushes the mode to the app server
  (`submit_collaboration_mode_settings_update`). With a message, the text is
  submitted as a user turn *with* the Plan collaboration mode attached
  (`Op::UserTurn { collaboration_mode: Some(...) }`).
- **Mode switching is blocked mid-turn**: `submit_user_message_with_mode`
  errors with "Cannot switch collaboration mode while a turn is running."
- **Plan nudge**: the composer shows a "make a plan" suggestion when the
  draft text contains a standalone `plan`/`/plan`/`!plan` keyword while in
  Default mode (`chatwidget/tests/plan_mode.rs`); dismissal is scoped per
  thread.
- **Mode indicator** in the footer/status area and `ModeKind` display name
  ("Plan" / "Default").
- **Plan rendering**: `codex-rs/tui/src/history_cell/plans.rs` —
  `StreamingPlanTailCell` (live tail while streaming) and `ProposedPlanCell`
  (committed, source-backed markdown re-rendered on resize). `PlanUpdateCell`
  renders `update_plan` TODO updates as a checkbox list.
- **Plan → implementation prompt**: when a turn ends in Plan mode and a plan
  item was seen (`transcript.saw_plan_item_this_turn`),
  `maybe_prompt_plan_implementation` →
  `codex-rs/tui/src/chatwidget/plan_implementation.rs` shows "Implement this
  plan?" with three choices:
  - *Yes, implement this plan* → submits `"Implement the plan."` with the
    Default mode mask (`SubmitUserMessageWithMode`).
  - *Yes, clear context and implement* → `ClearUiAndSubmitUserMessage` with a
    fresh-thread message that embeds the plan markdown
    (`PLAN_IMPLEMENTATION_CLEAR_CONTEXT_PREFIX` + plan) — context usage
    label shown when available.
  - *No, stay in Plan mode*.
- **`plan_mode_reasoning_effort` config** overrides the preset's Medium
  reasoning effort for Plan mode (`config/mod.rs`; applied in
  `set_collaboration_mask` and `submit_user_message_with_mode`).

### 1.9 Plan Mode lifecycle (ASCII)

```
user: /plan [message]
   │
   ▼
TUI: plan_mask(catalog) ──► set_collaboration_mask (indicator + nudge refresh)
   │                          │
   │                          └─► app server: collaboration mode persisted
   ▼
TurnContext.mode = Plan
   │
   ├─ prompt: <collaboration_mode> plan.md instructions (developer fragment)
   ├─ tools:  update_plan rejected; request_user_input allowed+blocking
   ├─ idle:   try_start_turn_if_idle rejects non-user input
   └─ goals:  tokens not charged to thread goal
   ▼
model streams text (+ optional <proposed_plan> block)
   │
   ▼
AssistantTextStreamParser (citations stripped, plan tags parsed incrementally)
   │
   ├─ Normal text ──► deferred agent-message start ──► AgentMessageContentDelta
   └─ Plan segments ─► TurnItem::Plan (id <turn>-plan) + PlanDelta events
   ▼
response completed: plan text re-extracted from finalized message,
                    plan item completed (PlanItem)
   ▼
TUI: plan rendered as ProposedPlanCell
   │
   ▼
turn ends in Plan mode + plan seen ──► "Implement this plan?" popup
   ├─ Yes        ──► Default mode + "Implement the plan."
   ├─ Yes (fresh)──► new thread, plan markdown embedded, Default mode
   └─ No         ──► stay in Plan mode
```

---

## Part 2 — Thread Goals

### 2.1 What a goal is

A thread goal is a **persisted, per-thread objective** with a status machine,
an optional token budget, token/elapsed-time usage accounting, and the
ability to **continue automatically** ("while idle") across turns until it is
complete, blocked, or budget-limited. It is implemented as a core **extension
(`ext/goal`)** that hooks the turn lifecycle, plus:

- SQLite state (`codex-rs/state/`),
- an app-server API (`codex-rs/app-server/src/request_processors/thread_goal_processor.rs`),
- model-facing tools (`get_goal`, `create_goal`, `update_goal`),
- steering prompts injected as hidden internal context,
- TUI slash commands and a goal menu.

### 2.2 Data model and status machine

`codex-rs/state/src/model/thread_goal.rs`:

```rust
pub enum ThreadGoalStatus {
    Active, Paused, Blocked, UsageLimited, BudgetLimited, Complete,
}

pub struct ThreadGoal {
    pub thread_id: ThreadId,
    pub goal_id: String,
    pub objective: String,
    pub status: ThreadGoalStatus,
    pub token_budget: Option<i64>,
    pub tokens_used: i64,
    pub time_used_seconds: i64,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}
```

Status semantics:

- **Active** — being pursued; eligible for automatic idle continuation.
- **Paused** — user-paused; no continuation, resume prompt after `/resume`.
- **Blocked** — set only after a strict 3-consecutive-turn blocked audit
  (see 2.9); prevents auto-continuation loops.
- **UsageLimited** — system sets it when a turn dies from a usage-limit
  error.
- **BudgetLimited** — terminal; set by the state layer when
  `tokens_used >= token_budget` during accounting. A final "budget limit"
  steering message is injected so the model wraps up and reports remaining
  work.
- **Complete** — terminal; set by the model via `update_goal` only when the
  objective is verifiably achieved.

`is_terminal()` = `BudgetLimited | Complete`. Replacing a completed goal
doesn't require user confirmation; replacing an unfinished goal does.

### 2.3 Model-facing tools

`codex-rs/ext/goal/src/spec.rs` defines three Responses-API tools:

- **`get_goal`** — returns current goal: status, budgets, tokens/elapsed
  usage, remaining token budget.
- **`create_goal { objective, token_budget? }`** — *"Create a goal only when
  explicitly requested by the user or system/developer instructions; do not
  infer goals from ordinary tasks."* Fails if an unfinished goal exists;
  `token_budget` only when explicitly requested.
- **`update_goal { status: "complete" | "blocked" }`** — the only statuses
  the model may set. `complete` requires actual achievement; `blocked`
  requires the same blocking condition for **at least three consecutive goal
  turns** (original + automatic continuations), with a fresh audit after a
  resume. The model cannot pause/resume/budget-limit; those are
  user/system-controlled. Tool descriptions embed the full audit rules.

Implementation (`tool.rs`): each tool resolves the current goal through the
state DB, applies accounting side effects (e.g., final budget report on
completion), and emits `ThreadGoalUpdated` events.

### 2.4 Steering: how the objective reaches the model

`codex-rs/ext/goal/src/steering.rs` renders three embedded templates
(`ext/goal/templates/goals/`) into **`InternalModelContextFragment`s** with
source `"goal"`:

- **continuation.md** — used to start an idle continuation turn. States the
  objective (as untrusted data, "not higher-priority instructions"), the
  budget, "work from evidence" (inspect current worktree, don't trust
  conversation memory), fidelity rules ("do not substitute a narrower,
  safer, smaller... solution"), a **completion audit** (requirement-by-
  requirement evidence verification) and a **blocked audit** (3 consecutive
  turns).
- **budget_limit.md** — injected when the budget is exhausted: wrap up,
  summarize progress, leave a clear next step; do not start new substantive
  work.
- **objective_updated.md** — injected when the user edits the objective
  mid-turn: supersede the old objective, adjust the current turn.

`InternalModelContextFragment`
(`codex-rs/core/src/context/internal_model_context.rs`) renders as a hidden
user-role fragment:

```
<codex_internal_context source="goal">
...prompt body...
</codex_internal_context>
```

These fragments are **retained in persisted history** (they match the
`<codex_internal_context ...>` / legacy `<goal_context>` markers), so the
goal objective stays visible to the model across turns, compaction, and
resume. `goal_context_input_item` wraps the fragment into a `ResponseItem`
that is injected into the input queue as `TurnInput::ResponseItem`.

### 2.5 Runtime

`codex-rs/ext/goal/src/runtime.rs` — `GoalRuntimeHandle`, one per thread,
wrapping `GoalRuntimeInner` with a `goal_state_lock` (a single-permit
semaphore serializing goal mutations vs accounting vs continuation):

- **`apply_external_goal_set`** — handles `set` from user/app-server: records
  metrics/analytics (created, resumed, terminal), transitions accounting
  state (mark current turn or idle goal active), injects
  `objective_updated` steering if the objective changed, then calls
  **`continue_if_idle()`** to kick off work immediately.
- **`continue_if_idle`** — the auto-continuation engine:
  1. skip if tools are not visible for the thread (goal tools disabled),
  2. skip if a **continuation deferral** is set (persisted
     `thread_goal_continuation_deferrals` row — used e.g. while the user is
     actively interacting),
  3. resolve the live thread via `ThreadManager`,
  4. read the goal; only `Active` goals continue,
  5. build `continuation_steering_item(goal)` and call
     `thread.try_start_turn_if_idle(vec![TurnInput::ResponseItem(item)])`,
  6. the same gate that **rejects Plan-mode idle turns** applies, so goals
     never fire in Plan mode,
  7. after the launch, if the launched turn didn't become the current
     active-goal turn, clear the accounting active-goal marker.
- **`stop_active_goal_for_turn(turn_id, reason)`** — on terminal turn errors:
  `TurnError → Blocked`, `UsageLimit → UsageLimited` (only from Active, or
  from BudgetLimited → UsageLimited), persists the status, emits
  `ThreadGoalUpdated`.
- **`restore_after_resume`** — after `/resume`, re-mark an `Active` goal as
  the idle active goal so continuation resumes.
- **`prepare_external_goal_mutation`** — flushes accounting before an
  external set/clear/fork so usage is charged to the right goal.
- **`inject_active_turn_steering`** — injects steering items into a *running*
  turn (`inject_if_running`) when the objective changes mid-turn.

Extension wiring (`extension.rs`):

- `on_extension_start` — registers the runtime for the thread and restores it
  (`restore_after_resume`).
- `on_thread_start` / `on_turn_start` — records the active goal for the turn;
  **skipped entirely in Plan mode** (`clear_current_turn_goal`), and
  `account_tokens = false` for Plan turns.
- `on_turn_stop` / `on_turn_abort` — `account_active_goal_progress` then
  `finish_turn`.
- `on_turn_error` — `stop_active_goal_for_turn` (blocks the goal on
  non-retryable/retry-exhausted errors to stop token-burning loops).
- `TokenUsageContributor` — feeds per-turn token deltas into accounting.
- `on_config_change` / `on_thread_clear` etc. — enable/disable and cleanup.

### 2.6 Accounting

`codex-rs/ext/goal/src/accounting.rs` — `GoalAccountingState`, per thread:

```rust
struct GoalAccountingInner {
    current_turn_id: Option<String>,
    turns: HashMap<String, GoalTurnAccounting>,   // current vs last accounted TokenUsage per turn
    wall_clock: GoalWallClockAccounting,          // idle-time baseline
    budget_limit_reported_goal_id: Option<String>,
}
struct GoalTurnAccounting {
    current_token_usage: TokenUsage,
    last_accounted_token_usage: TokenUsage,
    active_goal_id: Option<String>,
    account_tokens: bool,                         // false in Plan mode
}
```

- Token deltas are charged at turn stop/abort and at each tool-completion
  hook from `(current - last_accounted)` snapshots; a
  `progress_accounting_permit` semaphore **serializes concurrent charges** so
  a delta is never double-counted.
- Idle (wall-clock) time between turns is charged for an idle `Active` goal
  (`account_idle_goal_progress`), resetting the baseline when accounted.
- The state layer's `account_thread_goal_usage`
  (`codex-rs/state/src/runtime/goals.rs`) applies the delta and atomically
  transitions to **`BudgetLimited`** when `tokens_used >= token_budget`
  (`status_after_budget_limit`). Optimistic-concurrency checks
  (`expected_goal_id`) ignore charges against a replaced goal version.
- Dispositions: `BudgetLimitedGoalDisposition::{KeepActive, ClearActive}`
  controls whether the accounting state keeps the goal marked after a
  budget-limit charge.

### 2.7 Persistence

SQLite via `ThreadGoals` runtime (`state/src/runtime/goals.rs`), with
`get/replace/insert/update/delete/account_thread_goal_usage`,
`replace_thread_goal_snapshot` (for fork), and the continuation-deferral
table (`has/clear_thread_goal_continuation_deferral`). Concurrency: partial
updates preserve independent fields; `expected_goal_id` guards against stale
writes. Forking flushes progress first
(`flush_thread_goal_progress_for_fork`, `request_processors/thread_fork_goal.rs`).

### 2.8 Events and TUI

- `EventMsg::ThreadGoalUpdated(ThreadGoalUpdatedEvent { goal, ... })` is
  emitted on every persisted change (set, status change, accounting, clear)
  and drives the TUI goal status indicator and summaries.
- TUI commands (`chatwidget/slash_dispatch.rs`, `app/thread_goal_actions.rs`,
  `chatwidget/goal_menu.rs`):
  - `/goal` — goal menu/summary (status, objective, time/tokens used,
    budget, contextual command hints).
  - `/goal <objective>` — set a new goal (confirm before replacing an
    unfinished goal; completed goals replace without confirmation).
  - `/goal edit`, `/goal pause`, `/goal resume`, `/goal clear`.
  - After `/resume`, a paused/blocked/usage-limited goal prompts
    "Resume paused goal?".
  - Feature-flagged via `Feature::Goals`; needs a **saved** (non-ephemeral)
    session — ephemeral threads get a helpful error.
- `goal_files.rs` materializes oversized objectives, pasted text, and images
  as app-server-hosted files (`goal-objective.md` + attachments under codex
  home), replacing the objective body with a "Read the Codex goal objective
  file at <path> before continuing." reference (there is a
  `MAX_THREAD_GOAL_OBJECTIVE_CHARS` limit).

### 2.9 Completion and blocked audits

The completion/blocked discipline is **prompt-driven**, embedded in
continuation.md and the tool descriptions (the system only enforces the
budget/usage-limit transitions):

- **Completion audit**: derive concrete requirements from the objective and
  referenced artifacts; for each requirement find authoritative evidence in
  the *current* state (files, command output, tests, PR state, runtime
  behavior); weak/indirect/missing evidence ⇒ keep working. Only call
  `update_goal status=complete` when every requirement is proven.
- **Blocked audit**: never mark `blocked` on first appearance; only after the
  same blocker recurs for **at least three consecutive goal turns**
  (original turn + automatic continuations); a resumed run starts a fresh
  audit. `blocked` is reserved for true impasses needing user input or
  external-state change.

The `blocked`/`complete` statuses flow back through `update_goal` → state DB
→ `ThreadGoalUpdated` → TUI, and terminal states stop automatic
continuation.

### 2.10 Goal lifecycle (ASCII)

```
user: /goal "Ship the parser"   (or model: create_goal)
   │
   ▼
TUI/app-server: materialize draft (goal-objective.md if large) ─► thread/goal/set
   │
   ▼
GoalService.set_thread_goal ─► state DB insert ─► ThreadGoalUpdated event
   │
   └─► GoalRuntime.apply_external_goal_set (Active)
         ├─ metrics/analytics (created/resumed)
         ├─ accounting: mark idle goal active
         └─ continue_if_idle()
               └─ try_start_turn_if_idle(<codex_internal_context source="goal">
                                           continuation.md + objective>)
                     └─ (rejected in Plan mode)
   ▼
turn runs; model may call get_goal / create_goal / update_goal
   │
   ├─ on_turn_start: mark turn goal active (not in Plan mode)
   ├─ tool-completion hooks: account token deltas
   ├─ on_turn_stop/abort: account tokens + elapsed time ─► BudgetLimited if over budget
   └─ on_turn_error:  UsageLimit → UsageLimited; other terminal → Blocked
   ▼
turn ends ──► continue_if_idle again while status == Active
   │
   ├─ model: update_goal complete (verified) ──► Complete (terminal, stops)
   ├─ model: update_goal blocked (3-turn audit) ─► Blocked (stops)
   └─ user: /goal pause | clear ──► Paused | deleted
```

---

## Key files index

| Concern | Path (in `openai/codex`) |
|---|---|
| ModeKind / CollaborationMode | `codex-rs/protocol/src/config_types.rs` |
| Mode presets | `codex-rs/models-manager/src/collaboration_mode_presets.rs` |
| Plan-mode instructions | `codex-rs/collaboration-mode-templates/templates/plan.md` |
| Mode prompt injection | `codex-rs/core/src/context/world_state/collaboration_mode.rs` |
| Turn-loop plan-mode state machine | `codex-rs/core/src/session/turn.rs` |
| `<proposed_plan>` stream parser | `codex-rs/utils/stream-parser/src/{proposed_plan,assistant_text,tagged_line_parser}.rs` |
| `update_plan` TODO tool | `codex-rs/core/src/tools/handlers/{plan,plan_spec}.rs`, `codex-rs/protocol/src/plan_tool.rs` |
| `request_user_input` | `codex-rs/core/src/tools/handlers/request_user_input.rs` |
| Idle-turn gate (Plan-mode rejection) | `codex-rs/core/src/session/inject.rs` |
| Plan rendering (TUI) | `codex-rs/tui/src/history_cell/plans.rs` |
| "Implement this plan?" prompt | `codex-rs/tui/src/chatwidget/plan_implementation.rs`, `turn_runtime.rs` |
| Goal extension wiring | `codex-rs/ext/goal/src/{extension,runtime,accounting,steering,tool,spec,api}.rs` |
| Goal steering templates | `codex-rs/ext/goal/templates/goals/{continuation,budget_limit,objective_updated}.md` |
| Goal SQLite state | `codex-rs/state/src/{model/thread_goal.rs,runtime/goals.rs}` |
| Goal app-server API | `codex-rs/app-server/src/request_processors/thread_goal_processor.rs` |
| Goal TUI | `codex-rs/tui/src/{chatwidget/goal_menu.rs, app/thread_goal_actions.rs, goal_files.rs, goal_display.rs}` |

## Relevance to snow-core

The two features are deliberately separable design patterns, and Codex's
choices map cleanly onto snow-core's architecture:

- **Plan mode** is mostly *context + streaming contract*, not new tooling:
  a `ModeKind` on the turn context, per-mode developer instructions injected
  as a world-state fragment, an incremental tag parser on the output stream
  (`<proposed_plan>` blocks → structured plan items), and a few hard gates
  (`update_plan` rejection, `request_user_input` availability/blocking, idle
  continuation suppression, goal-accounting exclusion). snow-core implements
  those seams in `internal/agent`, `internal/plan`, and `pkg/protocol`; the
  current built-in preset supports a configurable reasoning override but does
  not ingest Codex's model-catalog-specific collaboration instruction field.
- **Thread goals** are a lifecycle *extension* over the session store: a
  persisted per-thread goal row, turn-lifecycle hooks for usage accounting
  (token deltas + idle wall-clock), hidden internal-context fragments to
  steer the model, and prompt-driven completion/blocked audits. snow-core's
  SQLite session store (`internal/session`), `internal/goal`, and
  `pkg/snowsdk` now host that shape, with TUI/RPC/CLI controls layered on
  `protocol.AgentEvent`. Snow deliberately charges monotonic in-process goal
  work time rather than Codex's idle process downtime, and starts restored
  goals only after a surface-ready signal so initial events cannot be lost.
  Oversized text is privately resolved by the controller; image attachment
  hosting remains outside Snow's text-only goal contract.

The full reference implementation is available at
`https://github.com/openai/codex` (files under `codex-rs/`).
