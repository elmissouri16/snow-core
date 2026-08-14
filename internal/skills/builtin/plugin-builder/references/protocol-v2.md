# Snow external plugin protocol v2 checklist

Transport: one UTF-8 JSON-RPC 2.0 object per LF-terminated stdout line. Use stderr only for diagnostics.

Host requests:

- `initialize`: return `manifest`, optional `capabilities`, and optional `supported_events`.
- `tools/list`: return `{ "tools": [...] }`.
- `tools/call`: execute the original unqualified tool name and return content blocks.
- `shutdown`: stop work, return an empty result, and exit cleanly.

Minimum manifest:

```json
{
  "id": "PLUGIN_ID",
  "name": "Human name",
  "version": "0.1.0",
  "protocol_version": 2
}
```

Each tool requires a lowercase name, description, JSON Schema object in `parameters`, and truthful optional risk (`read`, `write`, `exec`, or `network`). Omitted risk defaults to `exec`.

Successful call result:

```json
{
  "content": [{"type":"text","text":"result"}],
  "details": {"private_host_metadata": true},
  "is_error": false
}
```

Correlate responses with the request's string `id`. A JSON-RPC error uses an `error` object with integer `code` and bounded `message`. Never put protocol frames, credentials, or unbounded output in logs.

Optional notifications:

- plugin to host: `notifications/progress`, `notifications/log`;
- host to plugin: `notifications/cancelled`, `notifications/event` for explicitly subscribed events.

A configured command is an argv array, never a shell string. Snow supplies exactly the configured child environment; omitted or empty environment does not inherit host credentials. Validation starts the process and is therefore executable-code approval, not passive schema inspection.
