// Dependency-free @snow-core/plugin example (protocol v2).
// Run with: snow --plugin examples/plugins/javascript-sdk/manifest.json
import { definePlugin, defineTool, serve, textResult } from "../../../sdk/plugin-javascript/src/index.js";

const plugin = definePlugin({
  manifest: {
    id: "example-js-sdk",
    name: "Snow JavaScript SDK example",
    version: "1.0.0",
  },
  events: {
    tool_end() {},
  },
  tools: [
    defineTool({
      name: "echo",
      description: "Echo text after an optional delay",
      risk: "read",
      parameters: {
        type: "object",
        properties: {
          text: { type: "string" },
          delay_ms: { type: "integer", minimum: 0, maximum: 5000 },
        },
        required: ["text"],
        additionalProperties: false,
      },
      async execute(args, context) {
        await context.progress("Preparing echo");
        if (Number(args.delay_ms) > 0) {
          await new Promise((resolve) => setTimeout(resolve, Number(args.delay_ms)));
        }
        context.signal.throwIfAborted();
        return textResult(String(args.text), {
          details: { runtime: "node-sdk", length: String(args.text).length },
        });
      },
    }),
  ],
});

await serve(plugin);
