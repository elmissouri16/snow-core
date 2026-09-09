/// <reference path="../snow-v2.d.ts" />
// Every contribution is declarative. Editing uses native Snow dialogs.
snow.registerView({name: "studio", title: "UI Studio", placement: "screen"});
snow.registerView({name: "banner", title: "Focus banner", placement: "header"});
snow.registerView({name: "focus", title: "Composer focus", placement: "above_input"});

const palettes = {
  ocean: {accent: ["#0369a1", "#38bdf8"], muted: ["#475569", "#94a3b8"], foreground: ["#0f172a", "#e2e8f0"], warning: ["#a16207", "#facc15"], error: ["#b91c1c", "#f87171"], success: ["#047857", "#34d399"], separator: ["#94a3b8", "#475569"]},
  amber: {accent: ["#92400e", "#fbbf24"], muted: ["#57534e", "#a8a29e"], foreground: ["#292524", "#fafaf9"], warning: ["#9a3412", "#fb923c"], error: ["#b91c1c", "#f87171"], success: ["#166534", "#86efac"], separator: ["#a8a29e", "#57534e"]}
};
for (const name of Object.keys(palettes)) {
  const colors = {};
  for (const key of Object.keys(palettes[name])) {
    colors[key] = {light: palettes[name][key][0], dark: palettes[name][key][1]};
  }
  snow.registerTheme({name, colors});
}

async function render(ctx) {
  const saved = await ctx.storage.get({scope: "project", key: "focus"});
  const focus = saved || {task: "Explore the plugin API", steps: 3, tests: true};
  await ctx.ui.update({name: "studio", content: {type: "column", children: [
    {type: "markdown", text: "## " + String(snow.config.title) + "\nNative components rendered by Snow."},
    {type: "row", children: [{type: "text", text: "Accent · ", tone: "accent"}, {type: "text", text: "Success · ", tone: "success"}, {type: "text", text: "Warning", tone: "warning"}]},
    {type: "table", columns: ["Surface", "Example"], rows: [["Sidebar / footer", "/dashboard"], ["Header / composer", "Show focus button below"], ["Modal screen", "This studio"]]},
    {type: "input", text: "Task", input: String(focus.task)},
    {type: "select", text: "Planned steps", input: String(focus.steps)},
    {type: "checkbox", text: "Include tests", value: focus.tests ? 1 : 0},
    {type: "progress", text: "Component gallery", value: 1},
    {type: "list", children: [{type: "text", text: "Tab selects buttons; arrows scroll; Enter runs them."}, {type: "text", text: "Escape returns to your conversation."}]},
    {type: "button", text: "Edit focus (typed form)", action: "form"},
    {type: "button", text: "Show focus above the composer", action: "show"},
    {type: "button", text: "Draft focus prompt", action: "draft"},
    {type: "button", text: "Choose theme", action: "theme"},
    {type: "button", text: "Hide focus contributions", action: "hide"}
  ]}});
  return focus;
}

snow.registerCommand({name: "open", alias: "ui-studio", shortcut: "alt+u", description: "Explore native components, forms, and themes", uses: ["ui", "storage"], async run(_, ctx) {
  await render(ctx);
  if (ctx.ui.available) await ctx.ui.open({name: "studio"});
  return "UI Studio ready. Use /ui-studio in the TUI.";
}});
snow.registerCommand({name: "form", description: "Edit a project focus with string, number, and boolean fields", uses: ["ui", "storage"], async run(_, ctx) {
  const values = await ctx.ui.form({title: "Project focus", fields: [
    {name: "task", title: "What are you working on?", type: "string"},
    {name: "steps", title: "How many steps (1–20)?", type: "number"},
    {name: "tests", title: "Include tests?", type: "boolean"}
  ]});
  if (!String(values.task).trim() || String(values.task).length > 500) throw new Error("Use a task between 1 and 500 characters.");
  if (!Number.isInteger(values.steps) || values.steps < 1 || values.steps > 20) throw new Error("Steps must be a whole number from 1 to 20.");
  await ctx.storage.set({scope: "project", key: "focus", value: values});
  await render(ctx);
  await ctx.ui.notify({text: "Project focus saved"});
}});
snow.registerCommand({name: "show", description: "Show the saved focus in the header and above the composer", uses: ["ui", "storage"], async run(_, ctx) {
  const focus = await render(ctx);
  await ctx.ui.update({name: "banner", content: {type: "text", text: "Focus · " + focus.task, tone: "accent"}});
  await ctx.ui.update({name: "focus", content: {type: "text", text: focus.steps + " steps · " + (focus.tests ? "include tests" : "tests optional") + " · /ui-studio:hide to hide", tone: "muted"}});
  if (ctx.ui.available) await ctx.ui.close();
  return "Focus shown.";
}});
snow.registerCommand({name: "hide", description: "Hide the focus header and composer hint", uses: ["ui"], async run(_, ctx) {
  for (const name of ["banner", "focus"]) await ctx.ui.update({name, content: {type: "text", text: ""}});
  return "Focus hidden.";
}});
snow.registerCommand({name: "draft", description: "Insert the saved focus into the composer without sending it", uses: ["ui", "storage"], async run(_, ctx) {
  const focus = await render(ctx);
  const text = "Help me with: " + focus.task + ". Work in up to " + focus.steps + " steps." + (focus.tests ? " Verify with relevant tests." : "");
  if (!ctx.ui.available) return text;
  // Preserve any existing composer text.
  const previous = await ctx.ui.editorGet();
  await ctx.ui.editorInsert({text: (previous ? "\n\n" : "") + text});
  await ctx.ui.close();
}});
snow.registerCommand({name: "theme", alias: "plugin-theme", description: "Apply Ocean or Amber for this Snow run", argumentHint: "[ocean|amber]", uses: ["ui"], async run(input, ctx) {
  const name = input.trim() || await ctx.ui.select({title: "Choose a theme for this run", options: ["ocean", "amber"]});
  if (!palettes[name]) throw new Error("Choose ocean or amber.");
  await ctx.ui.theme({name});
  return "Applied " + name + ". Use /settings to select a built-in theme.";
}});
