/// <reference path="../snow.d.ts" />
snow.registerTool({
  name: "word_count",
  description: "Count whitespace-separated words in supplied text",
  parameters: {
    type: "object",
    properties: { text: { type: "string" } },
    required: ["text"],
    additionalProperties: false
  },
  execute(args) {
    if (typeof args.text !== "string") throw "text must be a string";
    const text = args.text.trim();
    const count = text ? text.split(/\s+/).length : 0;
    return { content: [{ type: "text", text: String(count) }] };
  }
});
