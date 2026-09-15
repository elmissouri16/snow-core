// Evaluations only inspect DOM/identity; every user activation and text selection
// uses native CDP input. Only the loopback HTTP server changes runtime state.
import {setTimeout as delay} from "node:timers/promises";
import {originals} from "./fixture.mjs";

export async function checks({state, evaluate, wait, key, click, navigate, send, capture, insert, width, height, theme}) {
  const results = [], failures = [];
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  const assert = async (expression, label) => check(await evaluate(expression), label);
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const prompt = "#live-prompt", sendButton = "#live-send", cancelEdit = "[data-message-reuse-cancel]";
  const stop = "#live-composer-normal [data-runtime-abort]";
  const row = id => `[data-message-id="${id}"]`;
  const edit = id => row(id) + " [data-message-reuse]";
  const requests = suffix => state.requests.filter(item => item.path.endsWith("/" + suffix));
  const editable = selector => `${q(selector)} && !${q(selector)}.disabled && !${q(selector)}.hidden`;
  const visible = selector => `${q(selector)} && ${q(selector)}.getClientRects().length > 0 && !${q(selector)}.hidden`;
  const stopped = selector => `${q(selector)} && (${q(selector)}.hidden || ${q(selector)}.disabled)`;
  const snapshot = async fields => {
    const previous = state.reads; state.update(fields);
    for (let i = 0; i < 180 && state.reads <= previous; i++) await delay(20);
    await delay(100);
  };
  const count = async (suffix, expected) => {
    for (let i = 0; i < 150 && requests(suffix).length < expected; i++) await delay(20);
    check(requests(suffix).length === expected, `Exactly ${expected} ${suffix} request(s)`);
  };
  const replace = async text => { await click(prompt); await key("a", "KeyA", 65, process.platform === "darwin" ? 4 : 2); await insert(text); };
  const bounds = async (selector, label) => assert(`(() => { const n=${q(selector)}, r=n.getBoundingClientRect(); return r.width>0&&r.height>0&&r.left>=-1&&r.top>=-1&&r.right<=innerWidth+1&&r.bottom<=innerHeight+1; })()`, `${label} remains inside ${width}×${height} ${theme}`);
  const keyboardFocus = async selector => {
    for (let i = 0; i < 90; i++) { if (await evaluate(`document.activeElement === ${q(selector)}`)) return; await key("Tab", "Tab", 9); }
    throw Error("Native Tab could not reach " + selector);
  };
  const idle = () => snapshot({status: "idle", cancel_token: "", cancel_requested: false, permission: null, input: null, recovery: {state: "canceled"}});
  const running = (fields = {}) => snapshot({status: "running", cancel_token: "turn-one-token", cancel_requested: false, ...fields});
  const clean = (label, allowClose = false) => {
    check(state.errors.length === 0, `${label}: HTTP contract has no errors (${state.errors.join("; ")})`);
    check(state.requests.every(item => (allowClose ? /\/(prompt|cancel|close)$/ : /\/(prompt|cancel)$/).test(item.path)), `${label}: no branch, switch, restart, activation, or other mutation`);
  };

  await navigate();
  await wait(editable(edit("older-user")));
  await assert(`${q('#live-session')}.dataset.turnCancel === "true"`, "Production Go template exposes optional token-bound cancellation capability");
  await assert(`typeof window.SnowMessages?.render === "function"`, "Actual production message renderer is loaded");
  await assert(stopped(stop), "Stop is unavailable while authoritative snapshot is idle");
  await assert(`${q(edit("truncated-user"))} === null || ${q(edit("truncated-user"))}.disabled || ${q(edit("truncated-user"))}.hidden`, "Truncated source cannot be edited and silently resent as complete");
  await assert(`!${q(row("older-answer"))}.querySelector('[data-message-reuse]')`, "Assistant rows have no user-message edit action");
  await assert('document.documentElement.scrollWidth <= innerWidth + 1', "Conversation creates no horizontal document overflow");
  const draft = "Keep my prior unsent draft 📝";
  await replace(draft);
  for (let i = 0; i < 3; i++) await key("ArrowLeft", "ArrowLeft", 37, 1);
  const selection = await evaluate(`[${q(prompt)}.selectionStart,${q(prompt)}.selectionEnd]`);
  await evaluate(`window.retainedRows = new Map([...document.querySelectorAll('#live-transcript [data-message-id]')].map(n=>[n.dataset.messageId,n]));`);
  for (const [index, message] of originals.filter(item => item.role === "user" && !item.truncated).entries()) {
    if (index === 1) { await keyboardFocus(edit(message.id)); await key(" ", "Space", 32); }
    else await click(edit(message.id));
    await wait(`${q(prompt)}.value === ${JSON.stringify(message.text)}`);
    await assert(`document.activeElement === ${q(prompt)} && ${q(prompt)}.selectionStart === ${q(prompt)}.value.length`, `Edit selected ${message.id}, not latest user row, and focuses composer`);
    await assert(visible("#live-reuse-notice"), "Edit notice explains copy/new-message semantics before Send");
    await bounds(cancelEdit, "Cancel edit action");
    if (index === 0) await capture("editing");
    check(state.requests.length === 0, `Opening ${message.id} performs no mutation`);
    await insert(" changed locally, not sent");
    await click(cancelEdit);
    await assert(`${q(prompt)}.value === ${JSON.stringify(draft)} && ${q(prompt)}.selectionStart === ${selection[0]} && ${q(prompt)}.selectionEnd === ${selection[1]}`, `Cancel ${message.id} restores exact prior unsent draft and native selection`);
    check(state.requests.length === 0, `Dismissing ${message.id} performs no mutation`);
    await assert(`${q(row(message.id))}.querySelector('.message-body').textContent === ${JSON.stringify(message.text)}`, `Original ${message.id} stays unchanged after opening/editing/dismissing`);
  }
  clean("Edit without Send");

  // One real Send, then the second click of that same pointer double-click.
  // Hold admission acknowledgement so only a fresh GET grants Stop authority.
  await click(edit("older-user"));
  const edited = "Edited first request — append exactly once 😀";
  await replace(edited); state.promptBehavior = "hold";
  const point = await evaluate(`(() => {const r=${q(sendButton)}.getBoundingClientRect();return {x:r.x+r.width/2,y:r.y+r.height/2};})()`);
  for (const type of ["mousePressed", "mouseReleased"]) await send("Input.dispatchMouseEvent", {type, ...point, button: "left", clickCount: 1});
  await count("prompt", 1);
  await wait(editable(stop), "verified running snapshot enables Stop before held prompt acknowledgement");
  for (const type of ["mousePressed", "mouseReleased"]) await send("Input.dispatchMouseEvent", {type, ...point, button: "left", clickCount: 2});
  await delay(120);
  check(requests("prompt").length === 1 && requests("cancel").length === 0, "Send double-click never becomes a fresh Stop decision or duplicate prompt");
  check(!!state.heldPrompt, "Stop is enabled while original prompt POST acknowledgement remains pending");
  await bounds(stop, "Running Stop action");
  await assert(`${q(edit("older-user"))}.disabled`, "Message editing is unavailable during running/admission-pending turn");
  state.cancelBehavior = "hold"; await click(stop); await count("cancel", 1);
  await assert(`${q(stop)}.disabled && /Stopping/i.test(${q(stop)}.textContent + ${q(stop)}.getAttribute('aria-label'))`, "Pending Stop locks duplicate cancellation and visibly says Stopping");
  await capture("stopping");
  await click(stop); await delay(120);
  check(requests("cancel").length === 1, "Repeated pointer Stop while cancel POST is pending sends no duplicate");
  check(requests("cancel")[0]?.fields.cancel_token === "turn-one-token", "Cancellation carries exactly the observed per-turn token");
  check(requests("prompt")[0]?.fields.text === edited, "Edited Send submits exactly the selected edited text");
  check(requests("prompt")[0]?.fields.instance_id === "instance-one" && state.snapshot.session_id === "session-one", "Edited Send remains in the same runtime and conversation");
  state.heldCancel(); await delay(150);
  await assert(`${q(stop)}.disabled && /Stopping/i.test(${q(stop)}.textContent + ${q(stop)}.getAttribute('aria-label'))`, "Cancel acknowledgement alone keeps Stopping until definitive idle");
  check(!!state.heldPrompt, "Stopping never aborts the original prompt fetch to pretend work stopped");
  state.heldPrompt(); await delay(150);
  await assert(`${q(prompt)}.value === ${JSON.stringify(draft)}`, "Successful edited Send restores the pre-existing unsent draft");
  await assert(`[...retainedRows].every(([id,n])=>n===document.querySelector('[data-message-id="'+id+'"]'))`, "All original user/assistant DOM identities survive a new turn and cancellation updates");
  for (const message of originals) await assert(`${q(row(message.id))}._snowCopyText === ${JSON.stringify(message.text)}`, `New turn retains original text of ${message.id}`);
  await snapshot({status: "idle", cancel_requested: true, recovery: {state: "canceled"}});
  await delay(150);
  await assert(`${q(stop)}.disabled && !${q(stop)}.hidden && /Stopping/i.test(${q(stop)}.getAttribute('aria-label'))`, "Completion before abort RPC acknowledgement retains visible Stopping");
  await assert(`${q(sendButton)}.disabled && ${q(edit('older-user'))}.disabled`, "Completed turn cannot advertise Send or Edit before cancellation control retires");
  await assert(`${q('#live-status')}.textContent === 'Stopping' && ${q('#live-turn-outcome')}.hidden`, "Pending abort acknowledgement does not announce Ready or invite another message");
  await idle(); await wait(editable(sendButton));
  await assert(stopped(stop), "Only authoritative idle with retired cancellation ends Stopping and restores Send");
  await running({cancel_token: "later-turn-token"}); await wait(editable(stop)); await delay(160);
  check(requests("cancel").length === 1 && requests("cancel")[0].fields.cancel_token !== state.snapshot.cancel_token, "Old cancellation never automatically targets a later turn");
  clean("Pending admission cancellation");

  for (const kind of ["permission", "input"]) for (const pendingAdmission of [false, true]) {
    await navigate(); await replace(draft);
    if (pendingAdmission) {
      state.promptBehavior = "hold";
      await key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2);
      await count("prompt", 1);
    }
    const fields = kind === "permission" ? {permission: {id: "approval-one", tool: "read", risk: "low", reason: "Fixture approval", paths: ["README.md"]}} : {input: {id: "question-one", questions: [{id: "choice-one", header: "Fixture question", question: "Continue?", options: [{label: "Yes", description: "Continue"}], choices_only: false}]}};
    await running({status: kind, ...fields});
    const attentionStop = "#live-attention [data-runtime-abort]";
    await wait(editable(attentionStop)); await bounds(attentionStop, `${kind} seat Stop`);
    await assert(`${q('#live-composer-normal')}.hidden && ${q(prompt)}.value === ${JSON.stringify(draft)}`, `${kind} seat preserves normal composer draft`);
    await click(attentionStop); await count("cancel", 1); await delay(120);
    await assert(`${q(attentionStop)}.disabled && /Stopping/i.test(${q(attentionStop)}.textContent)`, `${kind} Stop remains visibly pending until idle`);
    if (pendingAdmission) {
      check(!!state.heldPrompt, `${kind} Stop is available before delayed prompt acknowledgement`);
      // A new draft typed during running admission is not the sent prompt.
      await snapshot({status: "running", permission: null, input: null});
      await replace("New draft while acknowledgement is pending");
      state.heldPrompt(); await delay(100);
    }
    await idle(); await wait(editable(sendButton));
    await assert(`${q(prompt)}.value === ${JSON.stringify(pendingAdmission ? "New draft while acknowledgement is pending" : draft)}`, `${kind} cancellation restores normal draft without answering`);
    check(requests("prompt").length === Number(pendingAdmission) && requests("cancel").length === 1, `${kind} Stop makes only one cancel, no answer or additional prompt mutation`);
    clean(`${kind} attention`);
  }

  await navigate(); await running(); await wait(editable(stop));
  state.cancelBehavior = "hold"; await click(stop); await count("cancel", 1);
  await idle(); await wait(`${q('#live-status')}.textContent === 'Ready'`);
  await assert(`${q(sendButton)}.disabled`, "Idle observed before cancellation acknowledgement cannot release the pending request lock");
  await running({cancel_token: "later-turn-before-old-ack"});
  check(requests("cancel").length === 1 && requests("cancel")[0].fields.cancel_token === "turn-one-token", "Observing a later turn during old pending acknowledgement never retargets cancellation");
  state.heldCancel(); await wait(editable(stop)); await delay(160);
  check(state.snapshot.status === "running" && state.snapshot.cancel_token === "later-turn-before-old-ack" && !state.snapshot.cancel_requested, "Delayed old cancellation acknowledgement cannot overwrite current later-turn authority");
  check(requests("cancel").length === 1 && requests("cancel")[0].fields.cancel_token === "turn-one-token", "Delayed cancellation acknowledgement never issues cancellation against the later turn");
  clean("Late cancellation acknowledgement");

  await navigate(); await running(); await wait(editable(stop));
  state.cancelBehavior = "hold"; await click(stop); await count("cancel", 1);
  // No intermediate idle is ever delivered: production may coalesce that read.
  await running({cancel_token: "coalesced-next-turn"}); await wait(editable(stop));
  await assert(`!/Stopping/i.test(${q(stop)}.textContent + ${q(stop)}.getAttribute('aria-label'))`, "A fresh next-turn token retires old Stopping even when idle was coalesced away");
  check(requests("cancel").length === 1, "Coalesced next-turn observation does not automatically cancel that new turn");
  state.heldCancel(); await delay(120);
  await assert(editable(stop), "Old acknowledgement cannot lock or consume the new turn Stop authority");
  await click(stop); await count("cancel", 2);
  check(requests("cancel")[1]?.fields.cancel_token === "coalesced-next-turn", "Only a new explicit pointer Stop cancels the independently observed new turn");
  await idle(); await wait(editable(sendButton));
  clean("Coalesced next turn without idle");

  await navigate(); await replace(draft); await running(); await wait(editable(stop));
  state.cancelBehavior = "hold"; await click(stop); await count("cancel", 1);
  await snapshot({status: "failed", cancel_token: "", cancel_requested: false, error: "Fixture worker failed during cancellation"});
  await wait(`${q('#live-status')}.textContent === 'Worker failed' && !${q('[data-runtime-close]')}.disabled`);
  await assert(stopped(stop), "Failed cancellation RPC never fabricates running Stop authority");
  await assert(`${q(sendButton)}.disabled && ${q(edit('older-user'))}.disabled && ${q('[data-runtime-reviewed]')}.disabled`, "Failed worker permits no Send, message edit, or review-to-retry bypass");
  await assert(`${q(prompt)}.value === ${JSON.stringify(draft)} && !${q('#live-unknown')}.hidden`, "Failed cancellation keeps draft and unknown-outcome warning");
  state.heldCancel(); await delay(120);
  await assert(`${q('#live-status')}.textContent === 'Worker failed' && !${q('[data-runtime-close]')}.disabled`, "Late cancellation acknowledgement cannot undo failed-worker Close recovery");
  await click('[data-runtime-close]'); await wait(`${q('#runtime-close-dialog')}.open`);
  check(requests("close").length === 0, "Opening failed-worker Close confirmation does not close or reopen anything");
  await key("Escape", "Escape", 27);
  check(requests("close").length === 0, "Dismissing failed-worker Close confirmation performs no mutation");
  await click('[data-runtime-close]'); await wait(`${q('#runtime-close-dialog')}.open`);
  await click('[data-runtime-close-confirm]'); await count("close", 1);
  await wait(`${q('[data-message-id=fixture-saved-user-message]')} !== null && ${q('#live-session')} === null`, "Explicit Close returns to production saved history");
  check(state.closed && requests("cancel").length === 1 && requests("prompt").length === 0 && requests("close")[0]?.fields.instance_id === "instance-one", "Failed-worker recovery issues only one explicit bound Close, never another cancel, prompt, or activation");
  clean("Explicit failed-worker Close recovery", true);

  for (const unsafe of ["missing-token", "unknown-status", "offline", "stale-instance"]) {
    await navigate(); await replace(draft);
    if (unsafe === "missing-token") { await running({cancel_token: ""}); await wait(`${q('#live-status')}.textContent === 'Working'`); }
    if (unsafe === "unknown-status") await snapshot({status: "unknown", cancel_token: "not-authority"});
    if (unsafe === "offline") { await running(); state.offline = true; await wait(`${q('#live-connection')}.textContent !== 'Live'`); }
    if (unsafe === "stale-instance") { await running(); await snapshot({instance_id: "replaced-instance", cancel_token: "later-token"}); await wait(`${q('#live-connection')}.textContent === 'Session changed'`); }
    await assert(stopped(stop), `${unsafe}: Stop is unavailable without verified current authority`);
    await assert(`${q(sendButton)}.disabled && ${q(edit('older-user'))}.disabled`, `${unsafe}: Send and edit remain conservative`);
    await key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2); await delay(100);
    check(state.requests.length === 0, `${unsafe}: no automatic cancel, prompt, or retry`);
    await assert(`${q(prompt)}.value === ${JSON.stringify(draft)}`, `${unsafe}: draft stays intact`);
    clean(unsafe);
  }

  for (const behavior of ["malformed", "missing-status", "wrong-token", "false-success", "extra-ack-fields", "reject", "unknown"]) {
    await navigate(); await running(); await wait(editable(stop));
    state.cancelBehavior = behavior; await click(stop); await count("cancel", 1);
    await wait(`!${q('#live-unknown')}.hidden`);
    await assert(`${q(stop)}.disabled && ${q(sendButton)}.disabled`, `${behavior} cancel acknowledgement does not falsely enable Stop or Send`);
    await delay(180); check(requests("cancel").length === 1, `${behavior} cancel is never retried automatically`);
    await idle(); await wait(`${q('#live-status')}.textContent === 'Ready'`);
    await assert(`${q(sendButton)}.disabled`, `${behavior} outcome still requires explicit review after idle`);
    clean(`${behavior} cancel acknowledgement`);
  }

  for (const behavior of ["unknown", "malformed"]) {
    await navigate(); await replace(draft); await click(edit("middle-user")); await replace(edited);
    state.promptBehavior = behavior; await key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2);
    await count("prompt", 1); await wait(`!${q('#live-unknown')}.hidden`);
    await assert(`${q(prompt)}.value === ${JSON.stringify(edited)} && ${q(sendButton)}.disabled && ${q(edit('older-user'))}.disabled`, `${behavior} edited Send retains text and blocks retry/edit`);
    await key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2); await delay(160);
    check(requests("prompt").length === 1, `${behavior} edited Send is never retried by keypress or reconnect`);
    await click(cancelEdit);
    await assert(`${q(prompt)}.value === ${JSON.stringify(draft)}`, `${behavior} edited Send still permits restoring the original draft`);
    clean(`${behavior} prompt acknowledgement`);
  }
  await navigate();
  await snapshot({messages: [...originals,
    {id: "missing-source", role: "user", text: null},
    {id: "byte-oversized", role: "user", text: "😀".repeat(16385)},
    {id: "character-oversized", role: "user", text: "x".repeat(65537)}]});
  for (const id of ["missing-source", "byte-oversized", "character-oversized"]) {
    await assert(`${q(edit(id))} === null || ${q(edit(id))}.disabled || ${q(edit(id))}.hidden`, `${id}: unsafe user source is unavailable for edit/resend`);
  }
  check(state.requests.length === 0, "Rendering unsafe sources never sends or branches");
  await assert('stopReuseErrors.length === 0', "No production JavaScript errors or unhandled rejections");
  await navigate("saved-user");
  const saved = '[data-message-id="fixture-saved-user-message"]';
  await assert(`${q(saved)}.querySelector('.message-body').textContent === 'Saved user text must not activate or send itself.' && ${q(saved)}.dataset.messageRole === 'user'`, "Production Go export renders the exact non-live saved user source");
  await assert(`${q('#live-session')} === null && ${q(prompt)} === null`, "Saved user page has no activated runtime or live composer");
  await assert(`${q(saved)}.querySelector('[data-message-reuse]') === null || ${q(saved)}.querySelector('[data-message-reuse]').hidden || ${q(saved)}.querySelector('[data-message-reuse]').disabled`, "Non-live saved user Edit & continue is absent, hidden, or disabled");
  await click(saved + ' .message-body');
  await key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2); await delay(120);
  check(state.requests.length === 0, "Reading or using Send shortcut on saved user text never activates, sends, branches, or cancels");
  await assert('stopReuseErrors.length === 0', "Non-live saved-user production page has no JavaScript errors");
  clean("Non-live saved user");
  return {results, failures};
}
