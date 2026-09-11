// Snow API 2: plain JavaScript, no build step or background timers.
// Cosmetic state belongs to this plugin runtime; reload/restart resets it.
const help = "Use /cat [show|pet|feed|nap|wake|hide|help].";
const poses = {
  idle: {face: "o.o", message: "keeping you company", tone: "accent"},
  happy: {face: "^.^", message: "purrr...", tone: "success"},
  fed: {face: "^w^", message: "nom nom!", tone: "success"},
  asleep: {face: "-.-", message: "zzz...", tone: "muted"},
  wink: {face: "o.-", message: "nice work!", tone: "accent"}
};
let state = {visible: true, mood: "idle"};

snow.registerView({
  name: "pet",
  title: "Mochi the cat",
  placement: "above_input"
});

function picture(next) {
  const pose = poses[next.mood];
  return " /\\_/\\\n(" + pose.face + ")  Mochi: " + pose.message +
    "\n > ^ <  /cat pet | feed | nap | hide";
}

async function display(ctx, next) {
  // update also maintains view snapshots headlessly; no screen/dialog needed.
  await ctx.ui.update({
    name: "pet",
    content: next.visible
      ? {type: "text", text: picture(next), tone: poses[next.mood].tone}
      : {type: "text", text: ""}
  });
  // A failed host update must not commit a command's cosmetic state.
  state = next;
}

snow.registerCommand({
  name: "cat",
  alias: "cat",
  description: "Show Mochi the cat, pet, feed, nap, wake, or hide it",
  argumentHint: "[show|pet|feed|nap|wake|hide|help]",
  uses: ["ui"],
  async run(input, ctx) {
    const action = input.trim().toLowerCase() || "show";
    const next = {visible: state.visible, mood: state.mood};
    switch (action) {
      case "show": next.visible = true; break;
      case "pet": next.mood = "happy"; break;
      case "feed": next.mood = "fed"; break;
      case "nap": next.mood = "asleep"; break;
      case "wake": next.mood = "idle"; break;
      case "hide": next.visible = false; break;
      case "help": return help;
      default: throw new Error(help);
    }
    await display(ctx, next);
    return next.visible ? picture(next) : "Mochi is hidden. Use /cat show to bring the cat back.";
  }
});

snow.onReady(async function (_, ctx) {
  await display(ctx, state);
});

snow.on("turn_done", async function (_, ctx) {
  // Only one update per completed turn. Never wake a napping or hidden cat.
  if (!state.visible || state.mood === "asleep") return;
  await display(ctx, {
    visible: true,
    mood: state.mood === "wink" ? "idle" : "wink"
  });
});
