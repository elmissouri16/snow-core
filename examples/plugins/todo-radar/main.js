/// <reference path="../snow.d.ts" />
function matchLimit(value) {
  if (!Number.isInteger(value) || value < 1 || value > 200) throw "limit must be an integer from 1 to 200";
  return value;
}
const defaultLimit = matchLimit(snow.config.default_limit === undefined ? 60 : snow.config.default_limit);
const extraExcludes = snow.config.exclude === undefined ? [] : snow.config.exclude;
if (!Array.isArray(extraExcludes) || extraExcludes.length > 20 ||
    extraExcludes.some(value => typeof value !== "string" || !value || value.length > 200)) {
  throw "config.exclude must contain at most 20 nonempty glob strings of at most 200 characters";
}
const excludes = ["**/node_modules/**", "**/vendor/**", "**/dist/**", "**/build/**",
  "**/*.min.js", "**/go.sum", "**/package-lock.json", "**/yarn.lock", "**/pnpm-lock.yaml"].concat(extraExcludes);

snow.registerTool({
  name: "scan",
  description: "Find TODO, FIXME, HACK, and XXX markers with file/line references. Respects ignore rules and excludes common generated/dependency files. Matches are candidates, not a parsed comment audit.",
  parameters: {
    type: "object",
    properties: {
      path: { type: "string", description: "File or directory within allowed project roots; defaults to ." },
      glob: { type: "string", description: "Optional file filter, e.g. **/*.go" },
      kind: { type: "string", enum: ["all", "todo", "fixme", "hack", "xxx"], default: "all" },
      limit: { type: "integer", minimum: 1, maximum: 200 }
    },
    additionalProperties: false
  },
  uses: ["grep"],
  execute(args, ctx) {
    if (!args || typeof args !== "object" || Array.isArray(args)) throw "arguments must be an object";
    const kind = args.kind === undefined ? "all" : args.kind;
    if (!["all", "todo", "fixme", "hack", "xxx"].includes(kind)) throw "invalid marker kind";
    const path = args.path === undefined ? "." : args.path;
    const glob = args.glob === undefined ? "" : args.glob;
    if (typeof path !== "string" || !path.trim() || path.length > 512) throw "path must be a nonempty string of at most 512 characters";
    if (typeof glob !== "string" || glob.length > 200) throw "glob must be a string of at most 200 characters";
    const limit = args.limit === undefined ? defaultLimit : matchLimit(args.limit);
    ctx.progress("Searching " + kind + " markers");
    const result = ctx.callTool("grep", {
      path, glob, pattern: "\\b(" + (kind === "all" ? "TODO|FIXME|HACK|XXX" : kind.toUpperCase()) + ")\\b",
      ignore_case: true, max_matches: limit, exclude: excludes
    });
    if (result.isError) return result;
    return { content: [{ type: "text", text: "Marker candidates (limit " + limit + "; inspect context before treating these as tasks):\n" +
      result.content.map(block => block.text).join("\n") }] };
  }
});
