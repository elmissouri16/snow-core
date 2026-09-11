export default function register(snow: SnowAPI): void {
  snow.registerView({ name: "guard", title: "Workflow guard", placement: "footer" });
  async function show(ctx: SnowContext, enabled: boolean): Promise<void> {
    if (ctx.ui.available) {
      await ctx.ui.update({ name: "guard", content: { type: "text", text: "Workflow guard: " + (enabled ? "on" : "off"), tone: enabled ? "warning" : "muted" } });
    }
  }
  async function refresh(_: Params, ctx: SnowContext): Promise<void> {
    await show(ctx, (await ctx.workflow.get({ key: "unfinished" })) === true);
  }
  snow.registerCommand({
    name: "workflow-guard", alias: "workflow-guard", description: "Guard unfinished branch work before switching or compacting", argumentHint: "on | off", uses: ["workflow", "ui"],
    async run(input, ctx) {
      const name = input.trim() || "off";
      if (name !== "on" && name !== "off") throw new Error("Choose on or off");
      const enabled = name === "on";
      await ctx.workflow.set({ key: "unfinished", value: enabled });
      await show(ctx, enabled);
      return enabled ? "Workflow guard on. Run /workflow-guard off to allow session changes and compaction." : "Workflow guard off.";
    },
  });
  function guard(request: SnowHook): SnowHookResult {
    return request.workflow?.unfinished === true ? { block: "This branch has unfinished work. Run /workflow-guard off before switching sessions/branches or compacting." } : {};
  }
  snow.registerHook("before_session_change", guard, { workflowKeys: ["unfinished"] });
  snow.registerHook("before_compaction", guard, { workflowKeys: ["unfinished"] });
  snow.onReady(refresh);
  snow.on("plugin_session_changed", refresh);
}
