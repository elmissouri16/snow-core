# Stable transcript following and seat-aware return to latest

Written against Snow db7b2185c86a6506006ff797dce3129e2de385f0 plus uncommitted web source; Harness c291e7961a515f6d7af9304e7fd1d257929aef26.

## Evidence chain
- Surface: active and saved conversations, live stream growth, question/approval takeover, narrow/short viewports.
- Problem: user screenshot shows Jump to latest over the question input. internal/web/static/harness.css uses absolute bottom:176px and center alignment independent of the attention seat. app.js renderMessages samples <100px on every snapshot before attention renders, and scroll listener only hides jump; it does not reveal it until next fetch.
- Reference: dist/harness-reference/packages/client/ui-chat/src/client/ChatView.{tsx,module.css}, ui-conversation/src/client/skeleton/ConversationRoot.{tsx,module.css}. Return-to-bottom is34px circle aligned to content right, clears measured composer height+16px. ConversationRoot observes seat and scrollport. ChatView tracks explicit pinned/reader state with24px bottom threshold and refuses to snap reader merely because they are within100px.
- Direct evidence: reference pending question takes over measured sticky composer seat; normal composer rect zero. User screenshot verifies fixed Snow jump overlaps attention.

## Design decision
One scroll owner measures viewport and composer seat and distinguishes following from intentional reading. Position return-to-latest at transcript bottom, above the measured seat, never inside its form. A root geometry change cannot silently turn reading back into following.

## Reuse
Keep existing transcript DOM, SnowMessages keyed reconciliation, and runtime applySnapshot. Add a narrow SnowScroll presentation owner rather than burying more scroll state in app.js. Preserve draft and focus; do not move read position on unchanged snapshots.

## Changes
1. New static/scroll.js/css: init region, capture before-render, reconcile after messages AND attention, dispose on HTMX. Maintain explicit following state, per-session reading anchor and pending programmatic scroll writes. Immediate scroll event toggles return control. Own send and explicit jump restore following; ordinary chunks don't.
2. Observe content/seat/viewport geometry for follow only while pinned; manual reading retains stable row+offset across resize and bounded-history trimming where row survives. Prevent native shrink-clamp/programmatic events from masquerading as user gestures. Streaming must not continuously cancel upward reading.
3. Seat-aware 34px circular Return to latest with accessible label; current wide text pill removed. Stable content-axis placement; respect safe area and narrow widths. Reduced-motion preference honored.
4. Browser visualViewport changes should be experimentally verified, including keyboard pan. Use bounded viewport sizing where needed; do not claim Harness has a keyboard inset implementation (it does not).
5. Growing Markdown: reuse stable article/copy controls; prevent code-copy focus loss from replacing innerHTML. Stream coalescing belongs to existing event transport/render scheduling, not a second agent loop.

## Validation
Production DOM: pinned and away during cumulative streamed Markdown/code; immediate jump appearance/hide; tiny upward scroll gestures; exact new-user-message behavior; question arrival/collapse/answer; textarea wrapping; tool disclosure; resize; navigation/remount; old history trimming; focused Copy code surviving updates. Viewports320–1512 wide, landscape240–360 CSS height, zoom-equivalent layouts and mobile keyboard-reduced visualViewport. Assert relative geometry and actual scrollTop/content visibility, not only control existence. Retain existing safety/workflow tests.

## Scope and stop conditions
No invented tool-to-message historical association. No raw provider HTML/progress. Abort if IDs are positional/duplicated: fix projection identity before relying on stable anchors. Do not change turn authority or automatically send/replay drafts.

## Documentation
Record stable following/return behavior in docs/using-snow.md and corresponding verified defects in bugs.md.
