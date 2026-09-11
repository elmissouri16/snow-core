/// <reference path="../snow-v2.d.ts" />
snow.registerView({name: "pilot", title: "Session Pilot", placement: "screen"});
const usage = "Use /pilot to open controls; /pilot-run, /pilot-followup, /pilot-steer, /pilot-stop, /pilot-model, /pilot-branch, /pilot-rename, /pilot-goal.";

async function overview(_, ctx) {
  const state = await ctx.agent.state();
  const goal = await ctx.goals.get();
  const branches = await ctx.session.branches();
  const rows = [
    ["Agent", state.running ? "running" : "idle"],
    ["Model", String(state.model.id || "unknown")],
    ["Thinking", state.thinking], ["Mode", state.mode],
    ["Session", state.sessionId],
    ["Branch", String((branches.find(b => b.active) || {}).name || "main")],
    ["Goal", goal ? String(goal.status) + ": " + String(goal.objective).slice(0,200) : "none"]
  ];
  await ctx.ui.update({name: "pilot", content: {type: "column", children: [
    {type: "text", text: "Session Pilot", tone: "accent"},
    {type: "table", columns: ["Control", "Current state"], rows},
    {type: "button", text: "Refresh", action: "status"},
    {type: "button", text: "Choose a model", action: "model"},
    {type: "button", text: "Rename session", action: "rename"},
    {type: "button", text: "Fork conversation branch", action: "branch", input: "fork"},
    {type: "button", text: "Switch conversation branch", action: "branch", input: "switch"},
    {type: "button", text: "Show goal", action: "goal", input: "status"},
    {type: "button", text: "Pause goal", action: "goal", input: "pause"},
    {type: "button", text: "Stop agent", action: "stop"}
  ]}});
  return rows.map(row => row.join(": ")).join("\n");
}
snow.registerCommand({name: "open", alias: "pilot", description: "Open model, session, branch, and goal controls", uses: ["ui"], async run(_, ctx) {
  const status = await overview(_, ctx);
  if (ctx.ui.available) await ctx.ui.open({name: "pilot"});
  return status;
}});
snow.registerCommand({name: "status", description: "Refresh the control panel", uses: ["ui"], run: overview});
snow.registerCommand({name: "run", alias: "pilot-run", description: "Run one prompt through the normal agent loop", argumentHint: "<task>", uses: ["agent", "ui"], async run(input, ctx) {
  const text = input.trim() || await ctx.ui.input({title: "Task to send to the agent (uses your model)"});
  if (!text.trim()) throw new Error("A task is required.");
  if (ctx.ui.available) await ctx.ui.close();
  await ctx.agent.prompt({text});
}});
for (const [name, method, description] of [
  ["followup", "followUp", "Queue a task after the current turn"],
  ["steer", "steer", "Send guidance to the current agent turn"]
]) {
  snow.registerCommand({name, alias: "pilot-" + name, description, argumentHint: "<text>", uses: ["agent"], async run(input, ctx) {
    if (!input.trim()) throw new Error("Provide text after /pilot-" + name + ".");
    await ctx.agent[method]({text: input.trim()});
    return "Guidance accepted.";
  }});
}
snow.registerCommand({name: "stop", alias: "pilot-stop", description: "Abort root work through the normal lifecycle", uses: ["agent"], async run(_, ctx) {
  await ctx.agent.abort();
  return "Agent stopped.";
}});
snow.registerCommand({name: "model", alias: "pilot-model", description: "Choose from configured models", argumentHint: "[model-id]", uses: ["agent", "ui"], async run(input, ctx) {
  const catalog = await ctx.models.list();
  let id = input.trim();
  if (!id) {
    const options = [...new Set(catalog.models.map(m => String(m.id)))].slice(0, 16);
    if (!options.length) return "No models in the configured catalog.";
    id = await ctx.ui.select({title: "Choose a model (first 16; pass an ID for others)", options});
  }
  if (!catalog.models.some(m => m.id === id)) throw new Error("Model is not in the configured catalog.");
  await ctx.models.set({model: id});
  return "Model selected: " + id;
}});
snow.registerCommand({name: "rename", alias: "pilot-rename", description: "Rename the current saved session", argumentHint: "[name]", uses: ["session", "ui"], async run(input, ctx) {
  const name = input.trim() || await ctx.ui.input({title: "New session name"});
  if (!name.trim()) throw new Error("A session name is required.");
  await ctx.session.rename({name});
  return "Session renamed: " + name;
}});
snow.registerCommand({name: "branch", alias: "pilot-branch", description: "List, fork, or switch conversation branches (not Git branches)", argumentHint: "[list|fork name|switch id]", uses: ["session", "ui"], async run(input, ctx) {
  const [action, ...rest] = input.trim().split(/\s+/);
  const value = rest.join(" ");
  if (action === "fork") {
    const name = value || await ctx.ui.input({title: "Name for a new conversation branch"});
    if (!name.trim()) throw new Error("A branch name is required.");
    await ctx.session.fork({name});
    return "Conversation fork scheduled: " + name;
  }
  const branches = await ctx.session.branches();
  if (action === "switch") {
    let branch = branches.find(b => b.id === value);
    if (!value) {
      const choices = branches.filter(b => !b.active).slice(0, 16);
      if (!choices.length) return "No other conversation branches. Use /pilot-branch fork experiment.";
      const labels = choices.map(b => String(b.name || b.id) + " [" + b.id + "]");
      const selected = await ctx.ui.select({title: "Switch conversation branch", options: labels});
      branch = choices[labels.indexOf(selected)];
    }
    if (!branch) throw new Error("Unknown branch ID. Use /pilot-branch list.");
    await ctx.session.selectBranch({id: branch.id});
    return "Conversation branch switch scheduled.";
  }
  if (action && action !== "list") throw new Error("Use list, fork <name>, or switch <id>.");
  return branches.map(b => (b.active ? "* " : "  ") + b.id + " · " + (b.name || "unnamed")).join("\n");
}});
snow.registerCommand({name: "goal", alias: "pilot-goal", description: "Inspect or explicitly control the current goal", argumentHint: "[status|create objective|pause|resume|clear]", uses: ["goals", "ui"], async run(input, ctx) {
  const [action, ...rest] = input.trim().split(/\s+/);
  if (!action || action === "status") {
    const goal = await ctx.goals.get();
    return goal ? JSON.stringify(goal, null, 2) : "No active goal. " + usage;
  }
  if (action === "pause") { await ctx.goals.pause(); return "Goal paused."; }
  if (action === "resume") {
    if (!await ctx.ui.confirm({title: "Resume automatic goal work using your model?"})) return "Goal unchanged.";
    await ctx.goals.resume(); return "Goal resumed.";
  }
  if (action === "clear") {
    if (!await ctx.ui.confirm({title: "Clear this session's goal?"})) return "Goal kept.";
    await ctx.goals.clear(); return "Goal cleared.";
  }
  if (action === "create") {
    const objective = rest.join(" ");
    if (!objective) throw new Error("Use /pilot-goal create <objective>.");
    if (await ctx.goals.get()) throw new Error("A goal already exists. Inspect it with /pilot-goal status.");
    if (!await ctx.ui.confirm({title: "Start automatic goal work using your model: " + objective.slice(0, 400) + "?"})) return "Goal not started.";
    await ctx.goals.create({objective}); return "Goal started.";
  }
  throw new Error("Use status, create <objective>, pause, resume, or clear.");
}});
