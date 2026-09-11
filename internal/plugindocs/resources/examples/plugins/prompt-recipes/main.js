/// <reference path="../snow-v2.d.ts" />
const recipes = {
  review: "Review the following request. Inspect relevant code and tests, report actionable findings with file references, and distinguish evidence from assumptions. Do not modify files.\n\n",
  explain: "Explain the following using the actual code. Start with the main behavior, then trace the important paths with file references.\n\n",
  test: "Investigate test coverage for the following. Identify meaningful missing cases and propose focused tests before making changes.\n\n"
};
const limit = snow.config.read_limit;
if (!Number.isInteger(limit) || limit < 1 || limit > 2000) throw new Error("read_limit must be an integer from 1 to 2000.");
// Pure hooks have no host I/O. Concise behavior is off until explicitly enabled.
let concise = false;
snow.registerHook("before_prompt", request => {
  const match = /^#(review|explain|test)\s+([\s\S]+)$/.exec(request.text || "");
  return match ? {text: recipes[match[1]] + match[2]} : {};
});
snow.registerHook("before_request", () => concise ? {context: [{text: "Prompt Recipes concise mode: answer briefly with concrete evidence. File reads are capped at " + limit + " lines each; use offsets to inspect further lines when needed."}]} : {});
snow.registerHook("before_tool", request => {
  if (!concise || request.tool !== "read" || !request.arguments) return {};
  const args = request.arguments;
  if (args.limit === undefined || (typeof args.limit === "number" && (args.limit < 0 || args.limit > limit))) return {arguments: {...args, limit}};
  return {};
});
snow.registerHook("after_tool", request => {
  if (!concise || request.tool !== "read" || request.isError) return {};
  return {content: [...(request.content || []), {type: "text", text: "[Prompt Recipes: bounded read; request a later offset when more context is needed.]"}]};
});
snow.registerCommand({name: "draft", alias: "recipe", description: "Draft a review, explain, or test prompt", argumentHint: "[review|explain|test] [task]", uses: ["ui"], async run(input, ctx) {
  const parts = input.trim().split(/\s+/);
  const name = parts[0] || await ctx.ui.select({title: "Choose a prompt recipe", options: Object.keys(recipes)});
  if (!recipes[name]) throw new Error("Choose review, explain, or test.");
  const task = parts.slice(1).join(" ") || await ctx.ui.input({title: "What should the " + name + " focus on?"});
  const draft = "#" + name + " " + task;
  if (!ctx.ui.available) return draft;
  const previous = await ctx.ui.editorGet();
  if (previous && !await ctx.ui.confirm({title: "Replace the current draft with this recipe?"})) return "Current draft kept.";
  await ctx.ui.editorSet({text: draft});
  return "Recipe drafted. Send it when ready.";
}});
snow.registerCommand({name: "mode", alias: "recipe-mode", description: "Toggle concise request context and bounded read hooks for this run", argumentHint: "<concise|off|status>", uses: [], run(input) {
  const value = input.trim() || "status";
  if (value === "concise") concise = true;
  else if (value === "off") concise = false;
  else if (value !== "status") throw new Error("Use concise, off, or status.");
  return "Concise hooks " + (concise ? "on: read cap " + limit + " lines" : "off") + ". #review, #explain, and #test prefixes remain available.";
}});
