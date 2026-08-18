# @snow-core/plugin (private, unpublished)

Zero-dependency JavaScript/TypeScript SDK for authoring Snow protocol-v2
plugins. This package is **not published**; it lives in the Snow repository for
local development and packaging checks.

The wire contract is owned by `docs/plugin-protocol.md`. stdout belongs
exclusively to JSON-RPC frames; use `console.error`/stderr for diagnostics.

## Usage

```js
import { definePlugin, defineTool, serve, textResult } from "@snow-core/plugin";

const plugin = definePlugin({
  manifest: { id: "example-js", name: "Example JS", version: "0.1.0" },
  tools: [
    defineTool({
      name: "echo",
      description: "Echo text",
      risk: "read",
      parameters: {
        type: "object",
        properties: { text: { type: "string" } },
        required: ["text"],
        additionalProperties: false,
      },
      async execute(args, context) {
        await context.progress("echoing");
        return textResult(args.text, { details: { length: args.text.length } });
      },
    }),
  ],
});

await serve(plugin);
```

`definePlugin` inserts protocol version 2 automatically. Tool handlers receive
an `AbortSignal` and a deadline; use them for cooperative cancellation. Event
handlers, setup/shutdown hooks, progress, logging, and bounded
concurrency/queue limits are supported.

## Development

```sh
npm test
npm run pack:check
```
