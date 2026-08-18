# Snow external plugin protocol v2 checklist

Prefer Snow's local Python or JavaScript authoring SDK. The SDK owns JSON-RPC
framing, lifecycle, cancellation, progress, bounded queues/results,
concurrency, shutdown, stdout discipline, and unexpected-error sanitization.
Generated SDK-based source should declare only the manifest, tools, schemas,
risks, handlers, events, and optional lifecycle hooks.

The SDK packages are private and unpublished:

- Python: vendored to `vendor/python`, imported as `snow_plugin`.
- JavaScript: vendored to `vendor/javascript`, imported from
  `./vendor/javascript/src/index.js`.

After explicit write approval, copy the selected SDK snapshot embedded in Snow:

```sh
snow plugin sdk vendor --runtime <python|javascript> \
  .snow/generated-plugins/<plugin-id> --json
```

This command is offline and does not execute the copied SDK. Review its file
hashes and `snow-sdk.json` before validation. Never download a similarly named
registry package or hand-roll a partial protocol runtime.

## Wire contract

Transport is one UTF-8 JSON-RPC 2.0 object per LF-terminated stdout line. Use
stderr only for diagnostics.

Host requests:

- `initialize`: return `manifest`, optional `capabilities`, optional
  `supported_events`, and runtime `limits`.
- `tools/list`: return `{ "tools": [...] }`.
- `tools/call`: execute the original unqualified tool name and return content
  blocks.
- `shutdown`: cancel or bounded-join work, return an empty result, and exit
  cleanly.

Minimum manifest:

```json
{
  "id": "PLUGIN_ID",
  "name": "Human name",
  "version": "0.1.0",
  "protocol_version": 2
}
```

Each tool requires a lowercase name, description, strict JSON Schema object in
`parameters`, and truthful optional risk (`read`, `write`, `exec`, or
`network`). Omitted risk defaults to `exec`.

Successful call result:

```json
{
  "content": [{"type":"text","text":"result"}],
  "details": {"private_host_metadata": true},
  "is_error": false
}
```

`content` is provider-facing. `details` remains private host metadata. Use the
SDK's expected tool-error type for deliberately provider-facing failures.
Unexpected exceptions must produce a bounded JSON-RPC error containing only a
safe class/type, never the exception message, configuration, credentials, or
provider-private data.

Correlate responses with the request's string `id`. A JSON-RPC error uses an
`error` object with integer `code` and bounded `message`. Never put protocol
frames, credentials, or unbounded output in logs.

Optional notifications:

- plugin to host: `notifications/progress`, `notifications/log`;
- host to plugin: `notifications/cancelled`, `notifications/event` for
  explicitly subscribed events.

Long-running handlers must honor the SDK cancellation signal/context and host
deadline. Event delivery and diagnostics remain bounded. Keep
`max_concurrent: 1` unless both runtime and handlers safely support overlap.

A configured command is an argv array, never a shell string. Snow supplies
exactly the configured child environment; omitted or empty environment does
not inherit host credentials. Validation starts the process and is therefore
executable-code approval, not passive schema inspection.
