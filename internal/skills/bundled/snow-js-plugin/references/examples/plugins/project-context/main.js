/// <reference path="../snow.d.ts" />
function projectPath(value) {
  if (value === undefined) return ".";
  if (typeof value !== "string" || !value.trim() || value.length > 512 ||
      value.startsWith("/") || value.includes("\\") || /[\x00-\x1f]/.test(value) ||
      value.split("/").includes("..")) {
    throw "path must be a relative project directory without .. components";
  }
  return value.replace(/\/+$/, "") || ".";
}

function preview(result) {
  const text = result.content.map(block => block.text).join("\n");
  return text.length > 3500 ? text.slice(0, 3500) + "\n[plugin preview truncated]" : text;
}

snow.registerTool({
  name: "brief",
  description: "Build a bounded project orientation brief: a sample of files, README, and Go/Node/Python/Rust manifests. Missing optional files are reported, not inferred.",
  parameters: {
    type: "object",
    properties: { path: { type: "string", description: "Project-relative directory; defaults to ." } },
    additionalProperties: false
  },
  uses: ["glob", "read"],
  execute(args, ctx) {
    if (!args || typeof args !== "object" || Array.isArray(args)) throw "arguments must be an object";
    const path = projectPath(args.path);
    ctx.progress("Collecting project context");
    const inventory = ctx.callTool("glob", { path, pattern: "**/*", max_results: 40 });
    if (inventory.isError) return inventory;
    const sections = ["Project: " + path, "File sample (up to 40; ignore rules apply):\n" + preview(inventory)];
    for (const name of ["README.md", "go.mod", "package.json", "pyproject.toml", "Cargo.toml"]) {
      const result = ctx.callTool("read", { path: path + "/" + name, limit: 60 });
      sections.push(name + (result.isError ? " (unavailable):\n" : ":\n") + preview(result));
    }
    sections.push("This is an orientation sample, not a complete file or dependency inventory.");
    return { content: [{ type: "text", text: sections.join("\n\n") }] };
  }
});
