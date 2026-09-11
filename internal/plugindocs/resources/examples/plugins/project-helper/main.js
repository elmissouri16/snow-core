/// <reference path="../snow.d.ts" />
snow.registerTool({
  name: "read_file",
  description: "Read one file under the current project's permitted roots",
  parameters: {
    type: "object",
    properties: { path: { type: "string" } },
    required: ["path"],
    additionalProperties: false
  },
  uses: ["read"],
  execute(args, ctx) {
    if (typeof args.path !== "string") throw "path must be a string";
    ctx.progress("Reading project file");
    return ctx.callTool("read", { path: args.path });
  }
});
snow.on("turn_done", () => snow.log("info", "Turn completed"));
