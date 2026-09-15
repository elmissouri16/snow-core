# One bounded attention/composer seat, with reachable question and approval actions

Written against Snow db7b2185c86a6506006ff797dce3129e2de385f0 plus uncommitted web implementation; Harness c291e7961a515f6d7af9304e7fd1d257929aef26.

## Evidence chain
- Surface: activated conversation while awaiting user input/permission. User screenshot shows the question card occupying most remaining chat space, answer/submit below its fold, and Jump to latest overlapping the answer area.
- Owner: internal/web/templates/live.html and static/app.js renderAttention; app.css/harness.css own current flex layout.
- Reference: dist/harness-reference/packages/client/ui-user-questions/src/client/QuestionComposer.{tsx,module.css}; ui-approval/src/client/ApprovalPanel; ui-conversation/src/client/skeleton/ConversationRoot; ui-slots fallback takeover chain.
- Direct rendered evidence: running localhost:3080/?fixture, synthetic alpha conversation only. Its non-persisting welcome overlay was hidden temporarily for inspection, not a host setting change. Normal editor has zero bounds during expanded AND collapsed questions. Card x496.76,y422,w788.48,h308 at1512x740; max-height444; header63, scroll-body187, footer36 outside body.
- Scope: all pending questions (1–16), choices-only/freeform/mixed, permission complete/unknown/truncated, busy/disconnected/unknown outcome, narrow/short/keyboard-reduced viewports.
- Uncertainty: no inference that Snow current whole-card overflow is literally inaccessible; verified difference is submit/header scroll away and both attention+normal composer consume height.

## Design decision
Use a single resident composer seat. Pending attention replaces normal composer visually, retaining its DOM/draft. Questions use one page at a time, fixed header/footer, and only a scrolling body. Never hide or replace authority warnings to gain space.

## Reuse
Existing pending request IDs, hooks/runtimeAction, complete answers map, exact option labels, ChoicesOnly gates, nonce/safety/unknown outcome state. New attention.js/attention.css are warranted because keyed attention state and pagination should have one owner instead of enlarging app.js. Do not introduce another RPC action controller.

## Changes
1. templates/live.html: seat wraps retained normal composer and live-attention, mutually exclusive. Preserve unique selectors and explicit reconnect/review/close paths. Question/permission takeover remains available in legacy workflow-disabled runtime.
2. attention.js: init/render/dispose scoped to project+session+instance+request; keyed drafts preserved across snapshots and appropriate remount, cleared on changed instance/request. Render header eyebrow11/16, question title16/22/500, numbered20px option seats with separated label14/24/500 and description14/24/400. Recommendation suffix may be displayed as a badge but exact submitted label remains untouched. Native inputs keep semantics; custom entry is option-shaped growing textarea, omitted for ChoicesOnly. One question visible; pager and Next/Submit outside scroll body. Auto-advance choices except last; page changes never submit incomplete batch. IME-safe Enter advances/submits, Shift+Enter newline. Collapse hides body/footer, not restoring composer. Explicit Stop turn invokes existing abort; no fake Skip/Dismiss or multiselect.
3. attention.css: frame6px32px10px; max-content-width card r20, max-height min(60vh,520px, available seat height), column flex/overflowhidden, padding-bottom10. Header nonshrinking padding20px16px0 24; body minheight0 flex1 overflowauto; footer nonshrinking margin-top12 padding0 10px0 18. Narrow <=720 r16/title15x21/header10px12px0 18. Textarea max144px, independent overflow after six lines. Never fixed tall card.
4. Approval same seat: warning strip13/18, body overflowauto capped336px, persistent right-aligned Reject/Allow once action footer. Keep full public bounded summaries, host authority warnings, incomplete-summary allow prohibition and backend enforcement.
5. app.js integrates owner and safe state; disable editing/navigation/actions during in-flight answer/stop and unsafe state. Preserve unknown-outcome review instead of blind retries.

## Validation
Actual Go templates and browser interaction, not hand-built ideal markup: request arrival/removal while typing normal draft, multistep drafts, custom/ChoicesOnly, exact recommendation label, IME, all answers required, collapsed lifecycle, >30 options and long text, permission unknown/truncated/deny, active/disconnected/busy, error/review, HTMX/session transitions. At320/360/390/768/1024/1280/1512, short landscape and reduced visualViewport, footer/action boxes must remain inside seat and scroll body must never cover footer. Test keyboard reachability and text selection, not just rectangle existence.

## Scope and stop conditions
No new multi-select/skip/plan-review capabilities: public Snow DTO lacks them. Stop and reconcile if request identity/lifecycle differs; never weaken no-replay, truncated approvals or stale-instance checks.

## Documentation
Update docs/using-snow.md with question takeover/pager and supported cancellation semantics; record verified defects and resolutions in bugs.md only after tests.
