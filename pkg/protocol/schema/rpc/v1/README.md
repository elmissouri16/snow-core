# Snow RPC protocol v1 schemas

These Draft 2020-12 schemas are the machine-readable contract for one JSONL
request or stdout frame. Canonical IDs use
`https://snow-core.dev/schemas/rpc/v1/<file>`; test loaders resolve those IDs
from this directory and reject network fallback.

- `request.schema.json` covers eager-runtime commands.
- `catalog-request.schema.json` and `catalog-output.schema.json` cover the
  separate opt-in `--rpc-startup catalog` read-only surface; its capabilities
  are not advertised by normal eager RPC.
- `handshake.schema.json` defines the first `rpc_ready` frame.
- `response.schema.json` defines command acknowledgements/results, including the
  session-only `session_set_model` acknowledgement.
- `model-discovery.schema.json` defines the bounded `models_discover` result;
  legacy `models_list` remains active-provider-only.
- `prompt-completed.schema.json` defines definitive prompt termination.
- `agent-event.schema.json` defines normalized event frames.
- `output.schema.json` is the stdout-frame union.
- `plugins.schema.json` defines JavaScript extension commands, views, settings,
  themes, and audit transforms.
- `model.schema.json`, `message.schema.json`, `session-branch.schema.json`,
  `session-info.schema.json`, `session-fork.schema.json`,
  `worktree.schema.json`, and `common.schema.json` contain shared public DTO
  shapes.

The v1 schemas are strict (`additionalProperties: false`) so Go conformance tests
catch accidental wire drift. Clients should preserve or safely ignore unknown
event types and additive fields for forward compatibility.
Breaking or newly required fields need a new RPC protocol version.

Session messages optionally carry `public_tool_result`, using the same strict
`{text, truncated}` shape as `tool_end.tool_result`. The producer captures this
public-text provenance before persisting tool completion. Missing provenance
(including legacy history and private-marker results) is unavailable, not
permission to fall back to `content`, `tool_display`, or plugin metadata.
An explicit empty `text` remains a valid public result. This field is not
projected into provider requests.

Newly synthesized interruption-recovery messages may carry
`tool_outcome_unknown: true`. This is explicit bookkeeping provenance, not an
execution result. Public tool history keeps the call unresolved and suppresses
result identity/output even if the message has an error flag or conflicting
preview. Existing unmarked records are not inferred from private content or
backfilled. The field does not trigger recovery, authorization, or replay.
