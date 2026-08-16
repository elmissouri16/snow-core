# Snow RPC protocol v1 schemas

These Draft 2020-12 schemas are the machine-readable contract for one JSONL
request or stdout frame. Canonical IDs use
`https://snow-core.dev/schemas/rpc/v1/<file>`; test loaders resolve those IDs
from this directory and reject network fallback.

- `request.schema.json` covers every accepted command.
- `handshake.schema.json` defines the first `rpc_ready` frame.
- `response.schema.json` defines command acknowledgements/results.
- `prompt-completed.schema.json` defines definitive prompt termination.
- `agent-event.schema.json` defines normalized event frames.
- `output.schema.json` is the stdout-frame union.
- `model.schema.json`, `message.schema.json`, `session-info.schema.json`, and
  `common.schema.json` contain shared public DTO shapes. The message schema is
  the public hydration projection; runtime responses omit provider-private
  blocks and binary attachment payloads before validation.

The v1 schemas are strict (`additionalProperties: false`) so Go conformance tests
catch accidental wire drift. Python and JavaScript SDK decoders intentionally
preserve unknown event types and additive fields for forward compatibility.
Breaking or newly required fields need a new RPC protocol version.
