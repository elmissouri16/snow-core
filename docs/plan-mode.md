# Plan Mode

Snow implements Codex-style Plan Mode as a persisted collaboration mode. It is
separate from the `update_plan` TODO/checklist tool. This document covers mode
selection, the three-phase contract, the proposed-plan event stream, mode
persistence, and the handoff into implementation.

## On this page

- [Selecting Plan Mode](#selecting-plan-mode)
- [Behavior contract](#behavior-contract)
- [Proposed-plan events](#proposed-plan-events)
- [Implementation handoff](#implementation-handoff)
- [Configuration](#configuration)
- [Related documents](#related-documents)

## Selecting Plan Mode

```text
Shift+Tab                          toggle Default ↔ Plan in the TUI
/plan
/plan design a branch-aware retry system
/default
```

At the top-level composer, `Shift+Tab` changes mode immediately while idle. If
a prompt or automatic goal turn is active, Snow shows the pending transition as
`mode:<current>→<target>` and applies it after `turn_done`; pressing the
shortcut again cancels that pending transition. Modal pickers, completion
lists, model-requested questions, and the implementation prompt keep their
existing `Shift+Tab` navigation behavior.

The CLI, SDK, and RPC equivalents are:

```sh
snow --collaboration-mode plan -p "design the change"
```

```go
session.SetMode(protocol.ModePlan)
session.PromptWithMode(ctx, "design the change", protocol.ModePlan)
```

```json
{"id":"1","type":"set_mode","mode":"plan"}
{"id":"2","type":"prompt","mode":"plan","message":"design the change"}
```

Mode is stored per session branch, copied on fork, restored on resume, and
shown in the TUI header/footer. JSON/RPC and plugins receive an explicit mode
snapshot at surface startup; SDK hosts can read the same snapshot with
`Session.StateEvent()` after subscribing.

## Behavior contract

Every Plan-mode provider request receives the three-phase planning contract:
non-mutating repository exploration first, intent clarification second, and a
decision-complete implementation specification last. Its editable source is
`internal/plan/system.md`, embedded into the binary at build time and appended
separately from the configurable base system preamble. The rule is an
instruction boundary, not a sandbox: Snow still runs with user OS privileges
and does not attempt to classify arbitrary shell commands as read-only.

The model may ask blocking questions through `request_user_input`, backed by
the same TUI/SDK/RPC broker as `ask_user`. Default mode keeps the existing
`ask_user` name for compatibility. `update_plan` is hidden and rejected in
Plan mode; in Default mode it emits structured checklist updates. A Plan-mode
root may spawn only read-only `explorer` subagents; other roles or a mutating
explorer policy are rejected.

## Proposed-plan events

A final plan is wrapped by the model in exact line-delimited `<proposed_plan>`
tags. Snow parses the stream incrementally, suppresses the raw tags, and
emits:

- `plan_started`
- `plan_delta`
- `plan_completed`

The durable assistant message stores plan Markdown as a `plan` content block.
`plan_completed` is emitted only after that assistant message append succeeds,
so surfaces never receive an authoritative completion for a plan that was not
stored. Provider adapters reconstruct the tagged block when sending history
back to the model, while TUI/print/JSON/RPC/SDK consumers receive clean
structured output.

Split tags, CRLF, plan-only responses, unterminated blocks, and a second block
are handled deterministically. Only a bounded possible tag prefix at line start
is withheld, so ordinary text and long lines stream immediately. Interrupted
plans remain visible and durable but do not emit `plan_completed` or open the
implementation prompt.

Committed plans survive terminal resize and are reflowed with the transcript.
The current TUI retains committed rows as rendered strings rather than a typed
Markdown source tree, so resize preserves content and wrapping but does not
rerun Glamour from the original plan source.

## Implementation handoff

After a completed plan, the TUI offers to:

1. switch to Default and submit `Implement the plan.`;
2. create a fresh session, include the complete plan, and implement there;
3. remain in Plan mode.

The mode switch and implementation prompt are submitted atomically. Automatic
internal turns are rejected while Plan mode is active, which is the safety seam
used by persistent goals.

## Configuration

```json
{
  "collaboration_mode": "default",
  "plan_mode_reasoning_effort": "medium"
}
```

Plan mode uses medium reasoning when the model advertises it;
`plan_mode_reasoning_effort` can override that preset. The value accepts
`off|minimal|low|medium|high|xhigh|max|ultra`. When it is omitted, Snow uses
medium only if supported by the selected model; otherwise it preserves a
supported configured effort or falls back to off.

## Related documents

- [Thread Goals](goals.md)
- [Sessions](sessions.md)
- [Using Snow](using-snow.md)
- [Configuration](configuration.md)
- [SDK](sdk.md)
