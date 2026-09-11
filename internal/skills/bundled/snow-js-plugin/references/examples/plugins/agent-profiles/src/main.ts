export default function register(snow: SnowAPI): void {
  const profiles: Record<string, { guidance: string; allow: string[] }> = {
    reviewer: {
      guidance: "Review the requested code for correctness, security, and regressions. Inspect evidence before reporting prioritized, actionable findings. Do not modify files.",
      allow: ["read", "grep", "glob"],
    },
    architect: {
      guidance: "Analyze requirements and existing boundaries. Compare concrete alternatives, explain tradeoffs, and produce an implementation plan grounded in inspected source. Do not modify files.",
      allow: ["read", "grep", "glob"],
    },
    debugger: {
      guidance: "Investigate the reported failure using read-only source inspection. Distinguish verified evidence from hypotheses, trace the failing path, and propose a focused regression test and fix. Do not run commands or modify files.",
      allow: ["read", "grep", "glob"],
    },
  };
  snow.registerView({ name: "profile", title: "Agent profile", placement: "footer" });
  async function show(ctx: SnowContext, name: string): Promise<void> {
    if (ctx.ui.available) {
      await ctx.ui.update({ name: "profile", content: { type: "text", text: "Profile: " + name, tone: name === "off" ? "muted" : "accent" } });
    }
  }
  async function refresh(_: Params, ctx: SnowContext): Promise<void> {
    const selected = await ctx.workflow.get({ key: "profile" });
    await show(ctx, typeof selected === "string" ? selected : "off");
  }
  snow.registerCommand({
    name: "profile", alias: "profile", description: "Select a branch-local agent profile", argumentHint: "reviewer | architect | debugger | off",
    uses: ["workflow", "tool_policy", "ui"],
    async run(input, ctx) {
      const name = input.trim() || "off";
      if (name !== "off" && !Object.prototype.hasOwnProperty.call(profiles, name)) {
        throw new Error("Choose reviewer, architect, debugger, or off");
      }
      // One durable update: instructions and restriction cannot be half-applied.
      await ctx.workflow.update({ set: { profile: name }, toolRestriction: name === "off" ? null : { allow: profiles[name].allow } });
      await show(ctx, name);
      return name === "off" ? "Profile off; only this plugin's restriction was cleared." : "Profile " + name + " selected for this branch (read-only tools).";
    },
  });
  snow.registerHook("before_request", request => {
    const selected = request.workflow?.profile;
    if (typeof selected !== "string" || !Object.prototype.hasOwnProperty.call(profiles, selected)) return {};
    return { context: [{ text: profiles[selected].guidance }] };
  }, { workflowKeys: ["profile"] });
  snow.onReady(refresh);
  snow.on("plugin_session_changed", refresh);
}
