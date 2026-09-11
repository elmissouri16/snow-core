/// <reference path="../snow.d.ts" />
// Fixed commands only: user strings are never interpolated into the shell.
const git = "git --no-optional-locks --no-pager";
snow.registerTool({
  name: "status",
  description: "Show Git branch and working-tree status without refreshing the index. Requires command permission; does not stage, commit, or fetch.",
  parameters: { type: "object", properties: {}, additionalProperties: false },
  uses: ["bash"],
  execute(args, ctx) {
    if (!args || typeof args !== "object" || Array.isArray(args) || Object.keys(args).length) throw "status takes an empty argument object";
    ctx.progress("Inspecting Git status");
    return ctx.callTool("bash", {
      command: git + " status --short --branch --untracked-files=normal --ignore-submodules=all",
      timeout_ms: 10000
    });
  }
});

snow.registerTool({
  name: "diff",
  description: "Show staged or unstaged tracked-file changes under the current directory, as a patch or diffstat. Untracked contents are not included. External diff, textconv and submodule inspection are disabled. Git configuration still applies.",
  parameters: {
    type: "object",
    properties: {
      scope: { type: "string", enum: ["unstaged", "staged"], default: "unstaged" },
      stat: { type: "boolean", default: false, description: "Return a compact diffstat instead of a patch" }
    },
    additionalProperties: false
  },
  uses: ["bash"],
  execute(args, ctx) {
    if (!args || typeof args !== "object" || Array.isArray(args)) throw "arguments must be an object";
    const scope = args.scope === undefined ? "unstaged" : args.scope;
    if (scope !== "unstaged" && scope !== "staged") throw "scope must be staged or unstaged";
    if (args.stat !== undefined && typeof args.stat !== "boolean") throw "stat must be a boolean";
    ctx.progress("Inspecting " + scope + " changes");
    return ctx.callTool("bash", {
      command: git + " diff --no-ext-diff --no-textconv --no-color --ignore-submodules=all --unified=3" +
        (scope === "staged" ? " --cached" : "") + (args.stat ? " --stat" : "") + " -- .",
      timeout_ms: 10000
    });
  }
});
