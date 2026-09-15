// DOM evaluations inspect only; real pointer/keyboard input operates Go export.
import {setTimeout as delay} from "node:timers/promises";
import {originals, owners} from "./fixture.mjs";

export async function checks({state, evaluate, wait, key, click, navigate, capture, insert, width, height, theme}) {
  const results = [], failures = [];
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  const assert = async (expression, label) => check(await evaluate(expression), label);
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const prompt = "#live-prompt", dialog = "#message-regenerate-dialog", confirm = "[data-message-regenerate-confirm]", cancel = "[data-message-regenerate-cancel]";
  const stop = "#live-composer-normal [data-runtime-abort]";
  const row = id => `[data-message-id="${id}"]`, regenerate = id => row(id) + " [data-message-regenerate]";
  const enabled = selector => `${q(selector)} && !${q(selector)}.disabled && !${q(selector)}.hidden`;
  const requests = suffix => state.requests.filter(request => request.path.endsWith("/" + suffix));
  const count = async (suffix, expected) => {
    for (let i = 0; i < 150 && requests(suffix).length < expected; i++) await delay(20);
    check(requests(suffix).length === expected, `Exactly ${expected} ${suffix} request(s)`);
  };
  const replace = async text => { await click(prompt); await key("a", "KeyA", 65, process.platform === "darwin" ? 4 : 2); await insert(text); };
  const selection = () => evaluate(`({start:${q(prompt)}.selectionStart,end:${q(prompt)}.selectionEnd,direction:${q(prompt)}.selectionDirection})`);
  const focus = async selector => {
    for (let i = 0; i < 100; i++) { if (await evaluate(`document.activeElement===${q(selector)}`)) return; await key("Tab", "Tab", 9); }
    throw Error("Native keyboard could not reach " + selector);
  };
  const clean = async label => {
    check(state.errors.length === 0, `${label}: strict HTTP contract (${state.errors.join("; ")})`);
    check(state.requests.every(request => /\/(message-regenerate-prepare|message-regenerate-commit|cancel)$/.test(request.path)), `${label}: never prompt, edit, branch, fork, switch or activation; regeneration token is action-scoped`);
    await assert("messageRegenerateErrors.length===0", `${label}: no JavaScript errors or unhandled rejections`);
  };
  const originalDOM = async label => {
    for (const message of originals) await assert(`${q(row(message.id))}?.querySelector('.message-body')?.textContent === ${JSON.stringify(message.text)}`, `${label}: original ${message.id} untouched`);
  };
  const unchangedDraft = async (text, selected, label) => {
    await assert(`${q(prompt)}.value===${JSON.stringify(text)}`, `${label}: composer draft is never replaced, cleared or submitted`);
    check(JSON.stringify(await selection()) === JSON.stringify(selected), `${label}: composer selection and direction preserved`);
  };
  const prepare = async (id, keyboard = false) => {
    await wait(enabled(regenerate(id)));
    if (keyboard) { await focus(regenerate(id)); await key("Enter", "Enter", 13); } else await click(regenerate(id));
    await wait(`${q(dialog)}.open && (${enabled(confirm)})`, "authoritative preparation enables explicit confirmation");
  };
  const terminal = async () => {
    state.update({status: "idle", cancel_token: "", cancel_requested: false, recovery: {state: "canceled"}});
    await wait(`${q('#live-connection')}?.textContent==='Live' && (${q(stop)}.disabled || ${q(stop)}.hidden)`);
  };
  const projection = async selected => {
    const end = originals.findIndex(message => message.id === owners[selected]), prefix = originals.slice(0, end + 1);
    await wait(`${q('#live-session')}.dataset.instance==='regenerated-instance'`);
    for (const message of prefix) await assert(`${q(row(message.id))}?.querySelector('.message-body')?.textContent === ${JSON.stringify(message.text)}`, `${selected}: exact prefix ${message.id} retained`);
    for (const message of originals.slice(end + 1)) await assert(`${q(row(message.id))}===null`, `${selected}: selected response, owning tool/plan steps and suffix ${message.id} discarded`);
    await assert(`JSON.stringify([...document.querySelectorAll('#live-transcript [data-message-id]')].map(n=>n.dataset.messageId)) === ${JSON.stringify(JSON.stringify(prefix.map(message => message.id)))}`, `${selected}: projection ends at same original user, never a duplicate appended user`);
    check(JSON.stringify(state.archive) === JSON.stringify(originals), `${selected}: HTTP fixture retains archive (real persistence belongs to Go tests)`);
  };
  const draft = "Keep this unsent draft 📝 with selection.";
  const draftSelection = async () => {
    await replace(draft); await key("Home", "Home", 36);
    for (let i = 0; i < 4; i++) await key("ArrowRight", "ArrowRight", 39, 8);
    return selection();
  };

  try {
    await navigate(); await wait(enabled(regenerate("first-answer")));
    await assert(`${q('#live-session')}.dataset.messageRegenerateEnabled==='true'`, "Production Go optional regeneration capability enabled");
    await assert(`typeof window.SnowMessages?.render==='function'`, "Actual production renderer loaded");
    await assert(`document.querySelectorAll('#live-transcript [data-message-regenerate]').length===3`, "Exactly one final-response action per multi-step turn");
    for (const id of ["first-user", "first-preface", "first-step", "middle-plan", "middle-step-one", "middle-step-two", "truncated-answer", "orphan-answer"]) await assert(`!${q(regenerate(id))} || ${q(regenerate(id))}.disabled || ${q(regenerate(id))}.hidden`, `${id}: no regeneration authority for user/preface/plan/tool-step/truncated/orphan row`);
    await assert("document.documentElement.scrollWidth<=innerWidth+1", `No horizontal overflow at ${width}×${height} ${theme}`);
    const selected = await draftSelection(); check(selected.end > selected.start, "Native Shift+Arrow establishes actual selection");
    for (const [index, id] of Object.keys(owners).entries()) {
      await prepare(id, index === 1);
      check(state.requests.at(-1).fields.message_id === id && state.requests.at(-1).fields.instance_id === state.snapshot.instance_id, `${id}: read-only prepare binds exact selected final-response row and instance`);
      await assert(`(() => {const n=${q(dialog)}, h=document.getElementById(n.getAttribute('aria-labelledby')), d=document.getElementById(n.getAttribute('aria-describedby')); return n.open && h?.textContent.includes('Regenerate') && /replaces.*following conversation/i.test(d?.textContent) && /tools may run again/i.test(d?.textContent) && /not undone|no rollback/i.test(d?.textContent);})()`, `${id}: accessible confirmation warns of suffix replacement, tools rerunning and no rollback`);
      await assert(`(() => {const r=${q(dialog)}.getBoundingClientRect();return r.width>0&&r.left>=-1&&r.right<=innerWidth+1&&r.top>=-1&&r.bottom<=innerHeight+1;})()`, `${id}: confirmation fits viewport`);
      await unchangedDraft(draft, selected, `${id} confirmation`); await originalDOM(`${id} preparation`);
      await assert(`${q('#live-send')}.disabled && [...document.querySelectorAll('[data-message-edit]')].every(n=>n.disabled)`, `${id}: prepared authority cannot be sent through composer or Edit & resend`);
      check(requests("message-regenerate-commit").length === 0, `${id}: selection and preparation are not confirmation`);
      if (index === 1) await key("Escape", "Escape", 27); else { await focus(cancel); await key(" ", "Space", 32); }
      await wait(`!${q(dialog)}.open`);
      await unchangedDraft(draft, selected, `${id} canceled`);
      await assert(`document.activeElement===${q(regenerate(id))}`, `${id}: cancel restores invoking action focus`);
    }
    await count("message-regenerate-prepare", 3); await clean("Read-only confirmation/cancel"); await capture("cancel-preserves-draft");

    for (const [index, id] of Object.keys(owners).entries()) {
      await navigate(); let selected = await draftSelection(), currentDraft = draft;
      const identity = await evaluate(`({session:${q('#live-session')}.dataset.session,instance:${q('#live-session')}.dataset.instance,title:document.title,chatTitle:${q('[data-live-title]')}.textContent})`);
      await prepare(id, index === 2); state.commitBehavior = "hold";
      if (index === 2) { await focus(confirm); await key("Enter", "Enter", 13); } else await click(confirm);
      await count("message-regenerate-commit", 1);
      await key("Enter", "Enter", 13); await key("Escape", "Escape", 27);
      await assert(`!${q(dialog)}.open && ${q(confirm)}.disabled && !${q("#live-regenerate-notice")}.hidden && ${q("[data-message-regenerate-dismiss]")}.hidden`, `${id}: pending commit is single-flight with persistent inline status, never an obstructing modal`);
      await unchangedDraft(draft, selected, `${id} delayed acknowledgment`); await originalDOM(`${id} pending acknowledgment`);
      const commit = requests("message-regenerate-commit")[0];
      check(commit.fields.confirm === "regenerate" && commit.fields.instance_id === identity.instance && !!commit.fields.edit_token && !Object.hasOwn(commit.fields, "text"), `${id}: exact one-shot confirmation has no prompt text`);
      if (index === 1) {
        currentDraft = "New typing while regeneration acknowledgment is delayed 📝.";
        await replace(currentDraft); selected = await selection();
      }
      state.release(); await projection(id); await wait(`!${q(dialog)}.open && (${enabled(stop)})`);
      await unchangedDraft(currentDraft, selected, `${id} committed`);
      await assert(`${q('#live-session')}.dataset.session===${JSON.stringify(identity.session)} && document.title===${JSON.stringify(identity.title)} && ${q('[data-live-title]')}.textContent===${JSON.stringify(identity.chatTitle)}`, `${id}: same conversation identity and visible title`);
      await assert(`document.activeElement!==document.body && !${q(dialog)}.contains(document.activeElement)`, `${id}: successful regeneration restores focus to a surviving live control`);
      for (let i = 0; i < 100 && ![...state.streams].some(stream => stream.instance === "regenerated-instance"); i++) await delay(20);
      check([...state.streams].some(stream => stream.instance === "regenerated-instance") && ![...state.streams].some(stream => stream.instance === identity.instance), `${id}: old SSE retired; new-instance SSE subscribed`);
      state.emit({...state.previous, revision: 999, messages: [...originals, {id: "stale-answer", role: "assistant", text: "Stale suffix"}]}, identity.instance); await delay(80);
      await assert(`${q(row('stale-answer'))}===null && ${q('#live-connection')}.textContent==='Live'`, `${id}: old SSE never resurrects tail or poisons replacement connection`);
      state.update({messages: [...state.snapshot.messages, {id: "regenerated-answer", role: "assistant", text: "Fresh regenerated answer.", html: "<p>Fresh regenerated answer.</p>"}]});
      await wait(`${q(row('regenerated-answer'))}?.textContent.includes('Fresh regenerated answer.')`);
      await assert(`document.querySelectorAll(${JSON.stringify(row(owners[id]))}).length===1`, `${id}: original owning user remains exactly once after new answer starts`);
      await unchangedDraft(currentDraft, selected, `${id} response update`);
      if (index === 1) {
        await click(stop); await count("cancel", 1);
        check(requests("cancel")[0].fields.instance_id === "regenerated-instance" && requests("cancel")[0].fields.cancel_token === "regenerated-turn-token", "Stop targets regenerated runtime and turn token");
        await terminal(); await capture("middle-regenerated-stopped");
      } else await terminal();
      await count("message-regenerate-commit", 1); await clean(`${id} committed`);
    }

    await navigate(); const fastSelection = await draftSelection(); await prepare("middle-answer"); state.commitBehavior = "fast-terminal";
    await click(confirm); await count("message-regenerate-commit", 1); state.release();
    await wait(`${q(row('regenerated-answer'))}?.textContent.includes('Fresh regenerated answer exactly once.')`);
    await assert(`document.querySelectorAll(${JSON.stringify(row('middle-user'))}).length===1 && document.querySelectorAll(${JSON.stringify(row('regenerated-answer'))}).length===1 && ${q(row('middle-answer'))}===null && ${q(row('latest-answer'))}===null`, "Fast terminal before delayed running acknowledgement renders retained user and new answer once; no old tail");
    await unchangedDraft(draft, fastSelection, "Fast terminal"); await clean("Fast terminal"); await capture("fast-terminal");

    await navigate(); const pendingSelection = await draftSelection(); state.prepareBehavior = "hold";
    await click(regenerate("first-answer")); await count("message-regenerate-prepare", 1);
    await assert(`${q(confirm)}.disabled`, "Pending preparation cannot be confirmed");
    await key("Escape", "Escape", 27); await wait(`!${q(dialog)}.open`);
    await unchangedDraft(draft, pendingSelection, "Pending prepare canceled");
    await replace("New typing after cancel; stale preparation must not clobber it."); const newerSelection = await selection();
    state.release(); await delay(120);
    await unchangedDraft("New typing after cancel; stale preparation must not clobber it.", newerSelection, "Orphan prepare reply after newer typing");
    await assert(`!${q(dialog)}.open`, "Canceled prepare reply never reopens confirmation");
    check(requests("message-regenerate-commit").length === 0, "Cancel preparing and newer typing never mutate conversation"); await clean("Canceled prepare race");

    for (const behavior of ["reject", "missing-token", "wrong-source", "wrong-instance", "wrong-session", "malformed"]) {
      await navigate(); const selected = await draftSelection(); state.prepareBehavior = behavior;
      await click(regenerate("first-answer")); await count("message-regenerate-prepare", 1); await delay(120);
      await assert(`${q(confirm)}.disabled`, `${behavior}: unverified preparation cannot enable confirmation`);
      await unchangedDraft(draft, selected, `${behavior} prepare`);
      await key("Enter", "Enter", 13); await delay(60);
      check(requests("message-regenerate-commit").length === 0, `${behavior}: malformed/stale prepare has no fallback or replay`);
      await clean(`${behavior} prepare`);
    }

    await navigate(); await draftSelection(); state.prepareBehavior = "hold-reject";
    await click(regenerate("middle-answer")); await count("message-regenerate-prepare", 1);
    state.update({messages: [...originals, {id: "external-change", role: "assistant", text: "Source changed elsewhere."}]}); state.release(); await delay(120);
    await assert(`${q(confirm)}.disabled`, "Source changed during preparation retires confirmation authority");
    check(requests("message-regenerate-commit").length === 0, "Changed source never becomes automatic new prompt"); await clean("Stale preparation");

    await navigate(); const revokedSelection = await draftSelection(); await prepare("middle-answer");
    state.update({messages: originals.map(message => message.id === "middle-answer" ? {...message, can_regenerate: false} : message)});
    await wait(`${q(confirm)}.disabled`);
    await unchangedDraft(draft, revokedSelection, "Prepared source eligibility revoked");
    await key("Enter", "Enter", 13); await delay(60);
    check(requests("message-regenerate-commit").length === 0, "Source revoked after preparation cannot consume stale authority"); await clean("Revoked preparation");

    await navigate(); await draftSelection(); state.prepareBehavior = "hold";
    await click(regenerate("first-answer")); await count("message-regenerate-prepare", 1);
    const oldInstance = state.snapshot.instance_id;
    state.snapshot = {...state.snapshot, instance_id: "external-instance", revision: state.snapshot.revision + 1};
    state.emit(state.snapshot, oldInstance); state.release(); await delay(120);
    await assert(`${q(confirm)}.disabled`, "External runtime replacement retires an orphaned preparation reply");
    check(requests("message-regenerate-commit").length === 0, "Orphaned preparation never crosses runtime identity or auto-replays"); await clean("Orphaned preparation");

    for (const behavior of ["reject", "expired", "unknown", "malformed", "wrong-session"]) {
      await navigate(); const selected = await draftSelection(); await prepare("middle-answer"); state.commitBehavior = behavior;
      await click(confirm); await count("message-regenerate-commit", 1); await delay(150);
      await assert(`${q(confirm)}.disabled`, `${behavior}: rejected/unknown commit retires token authority`);
      await unchangedDraft(draft, selected, `${behavior} commit`);
      await assert(`!${q("#live-regenerate-notice")}.hidden && /unknown|review|changed|expired/i.test(${q("[data-message-regenerate-outcome]")}.textContent)`, `${behavior}: explicit visible review/error outcome shown`);
      await key("Enter", "Enter", 13); await delay(80); await count("message-regenerate-commit", 1);
      await click("[data-message-regenerate-dismiss]");
      await key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2); await delay(80);
      await count("message-regenerate-commit", 1); await originalDOM(`${behavior} commit`); await clean(`${behavior} commit`);
    }

    await navigate();
    state.update({status: "running", cancel_token: "unrelated-turn"});
    await wait(`${q(regenerate('first-answer'))}.disabled`);
    await assert(`[...document.querySelectorAll('[data-message-regenerate]')].every(n=>n.disabled)`, "Active unrelated turn disables all historical regeneration controls");
    await clean("Active turn guard");
    await navigate("workflow-cancel");
    await assert(`!${q('[data-message-regenerate]')} && !${q(dialog)}`, "Optional capability absent: no regeneration controls or synthetic confirmation");
    await clean("Unsupported capability");
    await navigate("saved-user");
    await assert(`!${q('#live-session')} && !${q('[data-message-regenerate]')}`, "Saved inactive conversation cannot regenerate by activating a worker");
    await clean("Saved inactive conversation");
    return {results, failures};
  } catch (error) { error.results = results; error.failures = failures; throw error; }
}
