# Model-requested user input

Snow exposes `ask_user` as a direct, always-loaded built-in tool that pauses
the current tool call until an interactive surface answers. This guide
defines the request/response contract, the per-surface behavior, the Plan-mode
alias, and the privacy and timeout semantics. It is not indexed by deferred
discovery, and an explicit CLI/SDK tool allowlist can still exclude it.

## Request contract

A call contains one to three questions. IDs are unique, stable snake_case
strings. Headers are short labels. A question without `options` is free-form;
a question with options must contain two or three mutually exclusive choices.

```json
{
  "questions": [
    {
      "id": "format",
      "header": "Format",
      "question": "Which output format should I use?",
      "options": [
        {
          "label": "JSON",
          "description": "Machine-readable output."
        },
        {
          "label": "Text",
          "description": "Human-readable prose."
        }
      ]
    },
    {
      "id": "notes",
      "header": "Notes",
      "question": "Anything else I should preserve?"
    }
  ]
}
```

| Field | Type | Required | Purpose |
|---|---|---|---|
| `id` | string | Yes | Stable identifier, `^[a-z][a-z0-9_]{0,63}$` |
| `header` | string | Yes | Short label, 1-30 runes |
| `question` | string | Yes | Complete question, 1-1000 runes |
| `options` | array | No | Two or three mutually exclusive choices; omit for free-form input |
| `options[].label` | string | Yes | Choice label, 1-80 runes |
| `options[].description` | string | Yes | Choice description, 1-300 runes |

Choice questions automatically include an **Other** entry; models must not
provide an option named `Other`.

The host assigns the tool call ID to both `id` and `tool_call_id` in the
emitted `user_input_request` event. RPC clients echo that `id` as
`params.request_id` when replying or rejecting.

Answers are trimmed, non-empty, and limited to 8 KiB. Choice answers preserve
the selected label exactly. Responses are normalized to request order, and the
tool returns only this model-facing JSON:

```json
{
  "answers": [
    {
      "id": "format",
      "answer": "JSON"
    },
    {
      "id": "notes",
      "answer": "Keep comments"
    }
  ]
}
```

## Surface behavior

### TUI

The request appears inline above the sticky composer while the transcript
remains independently scrollable. Arrow keys select a choice. Enter accepts a
choice or free-form answer. `Ctrl+J` inserts a newline, Tab and Shift+Tab move
between questions, Esc rejects only the `ask_user` call, and Ctrl+C aborts the
whole agent turn.

### Go SDK

Set `snowsdk.Options.UserInputHandler`:

```go
UserInputHandler: func(ctx context.Context, req protocol.UserInputRequest) (protocol.UserInputResponse, error) {
    return protocol.UserInputResponse{
        RequestID: req.ID,
        Answers: []protocol.UserInputAnswer{
            {QuestionID: req.Questions[0].ID, Answer: "JSON"},
        },
    }, nil
},
```

The callback may omit `RequestID`; Snow fills it from the request. It must
return exactly one answer for every question. The request event is published
before the callback runs.

### RPC

RPC prompt execution is asynchronous, so stdin remains available while the
tool waits. Reply to a `user_input_request` event with:

```json
{
  "id": "reply-1",
  "type": "user_input_reply",
  "params": {
    "request_id": "call-1",
    "answers": [
      {
        "id": "format",
        "answer": "JSON"
      }
    ]
  }
}
```

Or reject it with:

```json
{
  "id": "reject-1",
  "type": "user_input_reject",
  "params": {
    "request_id": "call-1"
  }
}
```

Invalid, incomplete, duplicate, oversized, or stale replies fail without
clearing the pending request, allowing the client to correct and resend them.

### Print and JSON

These one-shot surfaces have no input channel. If the model invokes
`ask_user`, the call immediately becomes an unavailable-input tool error and
the agent may continue or finish normally. The process never blocks waiting
for input.

## Plan-mode alias

In Plan mode, `ask_user` is unavailable and `request_user_input` is the
compatible alias backed by the same broker. In Default mode the reverse
applies. See [Plan mode](plan-mode.md) for the mode contract.

## Privacy and timeouts

Only one request can be pending because Snow executes tool calls serially.
Context cancellation releases the wait. Closing the app rejects any pending
request. There is no fixed answer timeout; a request blocks until it is
answered, rejected, or cancelled.

Questions and answers are sent to the model and persisted in the session
transcript. Never ask for or echo secrets.

`ask_user` is interaction/read risk and does not trigger the mutation
permission prompt; it cannot bypass the normal allowlist.

## Related documents

- [JSONL RPC](rpc.md)
- [Go SDK](sdk.md)
- [Plan mode](plan-mode.md)
- [Security model](security.md)
