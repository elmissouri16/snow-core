// Browser expressions inspect only. Activations, typing and selections use
// native CDP pointer/keyboard input; server mutations are fixture-owned HTTP/SSE.
import {setTimeout as delay} from "node:timers/promises";
import {originals, sourceText} from "./fixture.mjs";

export async function checks({state, evaluate, wait, key, click, navigate, send, capture, insert, width, height, theme}) {
  const results = [], failures = [];
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  const assert = async (expression, label) => check(await evaluate(expression), label);
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const prompt = "#live-prompt", sendButton = "#live-send", cancel = "[data-message-edit-cancel]", notice = "#live-edit-notice";
  const stop = "#live-composer-normal [data-runtime-abort]";
  const row = id => `[data-message-id="${id}"]`, edit = id => row(id) + " [data-message-edit]";
  const enabled = selector => `${q(selector)} && !${q(selector)}.disabled && !${q(selector)}.hidden`;
  const visible = selector => `${q(selector)} && ${q(selector)}.getClientRects().length>0 && !${q(selector)}.hidden`;
  const requests = suffix => state.requests.filter(request => request.path.endsWith("/" + suffix));
  const count = async (suffix, expected) => {
    for (let i = 0; i < 150 && requests(suffix).length < expected; i++) await delay(20);
    check(requests(suffix).length === expected, `Exactly ${expected} ${suffix} request(s)`);
  };
  const replace = async text => { await click(prompt); await key("a", "KeyA", 65, process.platform === "darwin" ? 4 : 2); if (text) await insert(text); else await key("Backspace", "Backspace", 8); };
  const focus = async selector => {
    for (let i = 0; i < 100; i++) { if (await evaluate(`document.activeElement===${q(selector)}`)) return; await key("Tab", "Tab", 9); }
    throw Error("Native keyboard could not reach " + selector);
  };
  const clean = label => {
    check(state.errors.length === 0, `${label}: strict HTTP contract (${state.errors.join("; ")})`);
    check(state.requests.every(request => /\/(message-edit-prepare|message-edit-commit|cancel)$/.test(request.path)), `${label}: never normal prompt, branch, fork, switch, activate or other mutation`);
  };
  const originalDOM = async label => {
    for (const message of originals) await assert(`${q(row(message.id))}?.querySelector('.message-body')?.textContent === ${JSON.stringify(message.text)}`, `${label}: original ${message.id} unchanged`);
  };
  const prepare = async (id, keyboard = false) => {
    await wait(enabled(edit(id)));
    if (keyboard) { await focus(edit(id)); await key("Enter", "Enter", 13); } else await click(edit(id));
    await wait(`${q(prompt)}.value === ${JSON.stringify(sourceText[id])}`, "full authoritative source loaded");
  };
  const terminal = async () => {
    state.update({status: "idle", cancel_token: "", cancel_requested: false, recovery: {state: "canceled"}});
    await wait(`${q('#live-connection')}?.textContent==='Live' && (${q(stop)}.disabled || ${q(stop)}.hidden)`);
  };
  const projection = async (selected, replacement) => {
    const index = originals.findIndex(message => message.id === selected), prefix = originals.slice(0, index);
    await wait(`${q(row('replacement-user'))}?.querySelector('.message-body')?.textContent === ${JSON.stringify(replacement)}`);
    for (const message of prefix) await assert(`${q(row(message.id))}?.querySelector('.message-body')?.textContent === ${JSON.stringify(message.text)}`, `${selected}: exact prefix ${message.id} retained`);
    for (const message of originals.slice(index)) await assert(`${q(row(message.id))}===null`, `${selected}: selected/suffix ${message.id} removed from active visible path`);
    await assert(`JSON.stringify([...document.querySelectorAll('#live-transcript [data-message-id]')].map(n=>n.dataset.messageId)) === ${JSON.stringify(JSON.stringify([...prefix.map(message => message.id), "replacement-user"]))}`, `${selected}: replacement occupies selected position, never appended after obsolete suffix`);
    await assert(`${q('[data-history-tool-id="obsolete-tool"]')} ${index > originals.findIndex(message => message.id === 'middle-answer') ? '!==null' : '===null'}`, `${selected}: earlier tool retained only when its owner belongs to prefix`);
    check(JSON.stringify(state.archive) === JSON.stringify(originals), `${selected}: fixture's original append-only archive retained (real persistence verified by Go tests)`);
  };

  await navigate();
  await wait(enabled(edit("first-user")));
  await assert(`${q('#live-session')}.dataset.messageEditEnabled==='true'`, "Actual Go template enables optional edit capability");
  await assert(`typeof window.SnowMessages?.render==='function'`, "Actual production renderer loaded");
  await assert(`${q('[data-history-tool-id="obsolete-tool"]')}?.textContent.includes('Read') && ${q(row('middle-plan'))}?.textContent.includes('Obsolete middle plan.')`, "Old suffix really includes a rendered tool and plan before replacement");
  for (const id of ["first-answer", "truncated-user", "nontext-user"]) await assert(`!${q(edit(id))} || ${q(edit(id))}.disabled || ${q(edit(id))}.hidden`, `${id}: unsupported source cannot be silently edited`);
  await assert(`!${q('#live-transcript [data-message-reuse]')}`, "Edit-capable host does not disguise Use as new prompt as editing");
  await assert("document.documentElement.scrollWidth <= innerWidth+1", `No horizontal overflow at ${width}×${height} ${theme}`);
  const draft = "Keep my unsent draft 📝 and selection";
  await replace(draft);
  await key("Home", "Home", 36); for (let i = 0; i < 4; i++) await key("ArrowRight", "ArrowRight", 39, 8);
  const selection = await evaluate(`({start:${q(prompt)}.selectionStart,end:${q(prompt)}.selectionEnd,direction:${q(prompt)}.selectionDirection})`);
  check(selection.end > selection.start, "Native Shift+Arrow creates a real draft selection");
  for (const [index, id] of ["first-user", "middle-user", "latest-user"].entries()) {
    await prepare(id, index === 1);
    check(state.requests.at(-1).fields.message_id === id, `${id}: prepare uses exact local row identity, not position or display copy`);
    check(state.requests.at(-1).fields.instance_id === state.snapshot.instance_id, `${id}: prepare is instance-bound`);
    await originalDOM(`${id} preparing`);
    await replace("Local replacement not committed yet.");
    await originalDOM(`${id} local typing`);
    check(requests("message-edit-commit").length === 0, `${id}: prepare and local typing do not commit`);
    if (index === 1) { await focus(cancel); await key(" ", "Space", 32); } else await click(cancel);
    await assert(`${q(prompt)}.value===${JSON.stringify(draft)}`, `${id}: cancel restores prior unsent draft`);
    await assert(`${q(prompt)}.selectionStart===${selection.start} && ${q(prompt)}.selectionEnd===${selection.end} && ${q(prompt)}.selectionDirection===${JSON.stringify(selection.direction)}`, `${id}: cancel restores exact selection and direction`);
    await assert(`document.activeElement===${q(prompt)}`, `${id}: cancel returns focus to composer`);
  }
  await count("message-edit-prepare", 3); clean("Prepare/cancel");
  await capture("cancel-restores-draft");

  for (const [index, id] of ["first-user", "middle-user", "latest-user"].entries()) {
    await navigate();
    const identity = await evaluate(`({session:${q('#live-session')}.dataset.session,title:document.title,chatTitle:document.querySelector("[data-live-title]").textContent,instance:${q('#live-session')}.dataset.instance})`);
    await prepare(id, index === 2);
    const replacement = `Edited ${id}: retain literal **source** & <tag>\nNew answer starts here 😀.`;
    await replace(replacement);
    state.commitBehavior = "hold";
    await click(sendButton); await count("message-edit-commit", 1);
    await key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2);
    await originalDOM(`${id} delayed acknowledgement`);
    await assert(`${q(sendButton)}.disabled`, `${id}: commit is single-flight while acknowledgment pending`);
    const committed = requests("message-edit-commit")[0];
    check(committed.fields.text === replacement && committed.fields.instance_id === identity.instance && !!committed.fields.edit_token, `${id}: one exact replacement text/token/source-instance commit`);
    state.release();
    await projection(id, replacement);
    await wait(enabled(stop));
    await assert(`${q('#live-session')}.dataset.session===${JSON.stringify(identity.session)} && document.title===${JSON.stringify(identity.title)} && ${q("[data-live-title]")}.textContent===${JSON.stringify(identity.chatTitle)}`, `${id}: same chat session and title`);
    await assert(`${q('#live-session')}.dataset.instance==='replacement-instance'`, `${id}: new runtime instance installed`);
    await wait(`${q('#live-prompt')}.value === ''`);
    for (let attempt = 0; attempt < 100 && ![...state.streams].some(stream => stream.instance === "replacement-instance"); attempt++) await delay(20);
    check([...state.streams].some(stream => stream.instance === "replacement-instance") && ![...state.streams].some(stream => stream.instance === identity.instance), `${id}: old SSE subscription retired and replacement subscribed`);
    // A late old-instance frame must not resurrect its discarded projection.
    state.emit({...state.previous, revision: 999, messages: [...originals, {id: "stale-answer", role: "assistant", text: "Stale old reply"}]}, identity.instance);
    await delay(100);
    await assert(`${q(row('stale-answer'))}===null && ${q('#live-connection')}.textContent==='Live'`, `${id}: old SSE cannot restore suffix or poison replacement connection`);
    if (index === 1) {
      await click(stop); await count("cancel", 1);
      check(requests("cancel")[0].fields.instance_id === "replacement-instance" && requests("cancel")[0].fields.cancel_token === "replacement-turn-token", "Stop targets replacement instance and active turn token");
      await terminal(); await capture("middle-replaced-stopped");
    } else await terminal();
    await count("message-edit-commit", 1); clean(`${id} committed`);
  }

  await navigate(); await prepare("middle-user"); await replace("Fast replacement.");
  state.commitBehavior = "fast-terminal";
  await click(sendButton); await count("message-edit-commit", 1);
  state.release();
  await wait(`${q(row('replacement-answer'))}?.textContent.includes('Replacement finished once.')`);
  await assert(`document.querySelectorAll(${JSON.stringify(row('replacement-user'))}).length===1 && document.querySelectorAll(${JSON.stringify(row('replacement-answer'))}).length===1`, "Fast terminal before delayed commit acknowledgement renders user and answer exactly once");
  await assert(`${q(row('middle-plan'))}===null && ${q(row('middle-answer'))}===null && ${q(row('latest-answer'))}===null`, "Fast terminal never restores discarded plans/tools/replies");
  await wait(`${q(stop)}.disabled || ${q(stop)}.hidden`);
  await assert(`${q(prompt)}.value===''`, "Fast terminal clears only the committed edit, not a duplicate pending replacement");
  clean("Fast terminal"); await capture("fast-terminal");

  // A read-only preparation may race draft input or be canceled while in flight.
  await navigate(); await replace(draft); state.prepareBehavior = "hold";
  await click(edit("first-user")); await count("message-edit-prepare", 1);
  await replace("Newer typing during prepare."); state.release(); await delay(120);
  await assert(`${q(prompt)}.value==='Newer typing during prepare.'`, "Delayed prepare never overwrites newer local typing");
  await originalDOM("Prepare typing race");
  check(requests("message-edit-commit").length === 0, "Prepare typing race never commits"); clean("Prepare typing race");

  await navigate(); await replace(draft); state.prepareBehavior = "hold";
  await click(edit("first-user")); await count("message-edit-prepare", 1);
  await click(cancel); state.release(); await delay(120);
  await assert(`${q(prompt)}.value===${JSON.stringify(draft)} && !(${visible(notice)})`, "Cancel pending prepare ignores eventual full-source reply");
  check(requests("message-edit-commit").length === 0, "Cancel pending prepare has no mutation"); clean("Cancel pending prepare");

  await navigate(); await replace(draft); state.prepareBehavior = "hold-reject";
  await click(edit("middle-user")); await count("message-edit-prepare", 1);
  state.update({messages: [...originals, {id: "external-change", role: "assistant", text: "Another viewer changed the source."}]});
  state.release(); await delay(120);
  await assert(`${q(prompt)}.value===${JSON.stringify(draft)}`, "Source changed during read: rejected preparation leaves draft untouched");
  check(requests("message-edit-commit").length === 0, "Stale preparation never falls back to a prompt or mutation");
  clean("Stale preparation");

  for (const behavior of ["reject", "empty", "nontext", "oversize", "wrong-source"]) {
    await navigate(); await replace(draft); state.prepareBehavior = behavior;
    await click(edit("first-user")); await count("message-edit-prepare", 1); await delay(120);
    await assert(`${q(prompt)}.value===${JSON.stringify(draft)}`, `${behavior}: invalid preparation cannot overwrite unsent draft`);
    await assert(`!(${visible(notice)}) || ${q(sendButton)}.disabled`, `${behavior}: rejected/unverified source cannot enter sendable edit mode`);
    check(requests("message-edit-commit").length === 0, `${behavior}: preparation failure has no fallback mutation`);
    clean(`${behavior} preparation`);
  }

  for (const behavior of ["reject", "unknown", "malformed"]) {
    await navigate(); await prepare("middle-user"); const text = `Retain failed ${behavior} edit.`; await replace(text);
    state.commitBehavior = behavior; await click(sendButton); await count("message-edit-commit", 1); await delay(180);
    await assert(`${q(prompt)}.value===${JSON.stringify(text)}`, `${behavior}: rejected/unknown commit retains edited text for review`);
    await assert(`${q(sendButton)}.disabled`, `${behavior}: acknowledgment failure blocks automatic or accidental replay`);
    await key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2); await delay(100);
    await count("message-edit-commit", 1); await originalDOM(`${behavior} commit`);
    await assert(`/unknown|review|changed|unchanged/i.test(${q('#live-action-error')}?.textContent || ${q(notice)}?.textContent || '')`, `${behavior}: actionable review/error notice`);
    clean(`${behavior} commit`);
  }

  await navigate(); await prepare("first-user"); await replace("   \n\t");
  await key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2); await delay(100);
  check(requests("message-edit-commit").length === 0, "Whitespace-only replacement never commits");
  // Stay below native textarea maxlength while exceeding the UTF-8 byte cap;
  // ASCII beyond maxlength is clipped by Chrome before it reaches the app.
  await replace("é".repeat(40000));
  await key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2); await delay(100);
  check(requests("message-edit-commit").length === 0, "Oversized replacement never commits or truncates silently");
  await click(cancel);
  await assert("messageEditErrors.length===0", "Production page has no JavaScript errors or unhandled rejections");
  clean("Invalid replacement");
  await navigate("workflow-cancel");
  const reuse = row("first-user") + " [data-message-reuse]";
  await wait(enabled(reuse));
  await assert(`${q(reuse)}.textContent==='Use as new prompt'`, "Unsupported host labels separate copy workflow Use as new prompt, never Edit & resend");
  await assert(`!${q('#live-transcript [data-message-edit]')}`, "Optional edit capability absent: no edit action");
  await click(reuse);
  await assert(`${q(prompt)}.value===${JSON.stringify(originals[0].text)}`, "Unsupported host copy uses displayed source only as an explicitly new prompt draft");
  check(state.requests.length === 0, "Unsupported host copy does not prepare, commit, prompt or branch");
  await click("[data-message-reuse-cancel]"); clean("Unsupported capability");

  await navigate("saved-user");
  await assert(`!${q('#live-session')} && !${q('[data-message-edit]')}`, "Saved non-live user text never offers edit that activates a worker");
  await click('[data-message-id="fixture-saved-user-message"] .message-body');
  await key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2); await delay(100);
  check(state.requests.length === 0, "Reading saved user text never activates or sends itself");
  clean("Saved non-live user");
  return {results, failures};
}
