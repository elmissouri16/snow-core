/// <reference path="../snow-v2.d.ts" />
snow.registerView({name: "notes", title: "Workspace Notes", placement: "screen"});
const location = () => ({scope: String(snow.config.scope), key: "notes"});
async function read(ctx) { return await ctx.storage.get(location()) || []; }
async function show(ctx) {
  const notes = await read(ctx);
  await ctx.ui.update({name: "notes", content: {type: "column", children: [
    {type: "text", text: snow.config.scope.charAt(0).toUpperCase() + snow.config.scope.slice(1) + " · " + notes.length + (notes.length === 1 ? " saved note" : " saved notes"), tone: "accent"},
    {type: "text", text: ""},
    ...notes.map((note, index) => ({type: "column", children: [
      {type: "text", text: "Note " + (index + 1), tone: "muted"},
      {type: "text", text: note.text + "\n"}
    ]})),
    ...(!notes.length ? [{type: "text", text: "No notes yet", tone: "accent"}, {type: "text", text: "Add a reminder, decision, or detail to keep for later.", tone: "muted"}] : []),
    {type: "button", text: "Add a note", action: "add"},
    {type: "button", text: "Insert into composer", action: "insert"},
    {type: "button", text: "Clear notes", action: "clear", tone: "error"}
  ]}});
  return notes;
}
snow.registerCommand({name: "open", alias: "notes", shortcut: "alt+n", description: "Open persistent workspace notes", uses: ["ui", "storage"], async run(_, ctx) {
  const notes = await show(ctx);
  if (ctx.ui.available) await ctx.ui.open({name: "notes"});
  return notes.map((n, i) => (i + 1) + ". " + n.text).join("\n") || "No notes yet.";
}});
snow.registerCommand({name: "add", alias: "note", description: "Save a note without sending it to the model", argumentHint: "[text]", uses: ["ui", "storage"], async run(input, ctx) {
  const text = (input || await ctx.ui.input({title: "Save a workspace note"})).trim();
  if (!text || text.length > 800) throw new Error("Notes must contain 1–800 characters.");
  const notes = await read(ctx);
  if (notes.length >= 20) throw new Error("This scope has 20 notes. Remove a note or clear it first.");
  notes.push({text, created: new Date().toISOString()});
  await ctx.storage.set({...location(), value: notes});
  await show(ctx);
  return "Saved note " + notes.length + " (" + snow.config.scope + ").";
}});
snow.registerCommand({name: "remove", description: "Delete one note by its displayed number", argumentHint: "<number>", uses: ["storage", "ui"], async run(input, ctx) {
  const index = Number(input.trim()) - 1;
  const notes = await read(ctx);
  if (!Number.isInteger(index) || index < 0 || index >= notes.length) throw new Error("Use a note number shown by /notes.");
  notes.splice(index, 1);
  await ctx.storage.set({...location(), value: notes});
  await show(ctx);
  return "Note removed.";
}});
snow.registerCommand({name: "insert", description: "Append saved notes to the composer for review before sending", uses: ["ui", "storage"], async run(_, ctx) {
  const notes = await read(ctx);
  if (!notes.length) return "No notes to insert.";
  const text = "Workspace notes (reference material):\n" + notes.map(n => "- " + n.text).join("\n");
  if (!ctx.ui.available) return text;
  const previous = await ctx.ui.editorGet();
  await ctx.ui.editorInsert({text: (previous ? "\n\n" : "") + text});
  await ctx.ui.close();
}});
snow.registerCommand({name: "clear", description: "Confirm and clear notes in the configured scope", uses: ["ui", "storage"], async run(_, ctx) {
  if (!await ctx.ui.confirm({title: "Clear all " + snow.config.scope + " notes?"})) return "Notes kept.";
  await ctx.storage.delete(location());
  await show(ctx);
  return "Notes cleared.";
}});
