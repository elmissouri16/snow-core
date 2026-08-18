// Snow JavaScript SDK template. Replace PLUGIN_ID and echo.
import { existsSync } from "node:fs";

const sdkEntrypoint = new URL("./vendor/javascript/src/index.js", import.meta.url);
if (!existsSync(sdkEntrypoint)) {
  throw new Error(
    "vendored @snow-core/plugin SDK is unavailable; run `snow plugin sdk vendor --runtime javascript <plugin-directory>` before validation",
  );
}
const { definePlugin, defineTool, serve, textResult } = await import(sdkEntrypoint);

const plugin = definePlugin({
  manifest: {
    id: "PLUGIN_ID",
    name: "PLUGIN_ID generated plugin",
    version: "0.1.0",
  },
  tools: [
    defineTool({
      name: "echo",
      description: "Replace this example with the reusable capability.",
      parameters: {
        type: "object",
        properties: { text: { type: "string" } },
        required: ["text"],
        additionalProperties: false,
      },
      risk: "read",
      async execute(args, context) {
        await context.progress("Preparing result");
        context.signal.throwIfAborted();
        const text = String(args.text);
        return textResult(text, { details: { length: text.length } });
      },
    }),
  ],
});

await serve(plugin);
