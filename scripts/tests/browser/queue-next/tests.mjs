// Real CDP input only: DOM expressions inspect, fixture owns HTTP/SSE changes.
import {setTimeout as delay} from "node:timers/promises";
import {originals, queueItem} from "./fixture.mjs";

export async function checks({state, evaluate, wait, key, click, navigate, capture, insert, width, height, theme}) {
  const results = [], failures = [];
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  const assert = async (expression, label) => check(await evaluate(expression), label);
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const prompt = "#live-prompt", enqueue = "[data-queue-next]", panel = "#live-queue-next", stop = "#live-composer-normal [data-runtime-abort]";
  const item = id => `[data-queue-item-id="${id}"]`;
  const action = (id, name) => item(id) + ` [data-queue-${name}]`;
  const editor = id => item(id) + " textarea";
  const row = id => `[data-message-id="${id}"]`;
  const enabled = selector => `${q(selector)} && !${q(selector)}.disabled && !${q(selector)}.hidden`;
  const visible = selector => `${q(selector)} && !${q(selector)}.hidden && ${q(selector)}.getClientRects().length>0`;
  const requests = suffix => state.requests.filter(request => request.path.endsWith("/" + suffix));
  const count = async (suffix, expected) => {
    for (let i = 0; i < 150 && requests(suffix).length < expected; i++) await delay(20);
    check(requests(suffix).length === expected, `Exactly ${expected} ${suffix} request(s)`);
  };
  const replace = async (text, selector = prompt) => { await click(selector); await key("a", "KeyA", 65, process.platform === "darwin" ? 4 : 2); if (text) await insert(text); else await key("Backspace", "Backspace", 8); };
  const shortcut = () => key("Enter", "Enter", 13, process.platform === "darwin" ? 4 : 2);
  const focus = async selector => {
    for (let i = 0; i < 100; i++) { if (await evaluate(`document.activeElement===${q(selector)}`)) return; await key("Tab", "Tab", 9); }
    throw Error("Native keyboard could not reach " + selector);
  };
  const selection = (selector = prompt) => evaluate(`({start:${q(selector)}.selectionStart,end:${q(selector)}.selectionEnd,direction:${q(selector)}.selectionDirection})`);
  const seed = async (items, fields = {}) => { state.queue({items, ...fields}); if (items.length) await wait(`${q(item(items[0].id))}!==null`); };
  const unchanged = async label => {
    await assert(`JSON.stringify([...document.querySelectorAll('#live-transcript [data-message-id]')].map(n=>n.dataset.messageId))===${JSON.stringify(JSON.stringify(originals.map(message => message.id)))}`, `${label}: pending input is separate, never optimistically inserted into transcript`);
    for (const message of originals) await assert(`${q(row(message.id))}?.querySelector('.message-body')?.textContent===${JSON.stringify(message.text)}`, `${label}: original ${message.id} untouched`);
  };
  const clean = async label => {
    check(state.errors.length === 0, `${label}: exact public HTTP fields (${state.errors.join("; ")})`);
    check(state.requests.every(request => /\/(queue-enqueue|queue-update|queue-remove|cancel)$/.test(request.path)), `${label}: no prompt fallback, edit/regenerate token reuse, branch/switch/restart or activation`);
    await assert("queueNextErrors.length===0", `${label}: no page errors or unhandled rejections`);
  };
  const draft = "Queued follow-up **literal** & <source>\nKeep exact café 😀 text.";
  const enqueueDraft = async (text = draft, keyboard = false) => {
    await replace(text); await wait(enabled(enqueue));
    if (keyboard) { await focus(enqueue); await key("Enter", "Enter", 13); } else await click(enqueue);
    await count("queue-enqueue", 1);
  };
  const guarded = async label => {
    await shortcut(); await delay(70);
    check(requests("queue-enqueue").length === 0, `${label}: invalid authority cannot enqueue via shortcut`);
    await assert(`${q(enqueue)}.disabled || ${q(enqueue)}.hidden || !${q("[data-queue-error]")}.hidden`, `${label}: invalid submission is disabled or explicitly rejected locally`);
  };

  try {
    await navigate();
    await assert(`${q('#live-session')}.dataset.queueNextEnabled==='true'`, "Actual production Go queue capability enabled");
    await assert(`typeof window.SnowMessages?.render==='function'`, "Actual production transcript renderer loaded");
    await replace(draft); await wait(enabled(enqueue));
    await assert(`${q(enqueue)}.textContent.trim()==='Queue next'`, "Visible explicit Queue next label, never disguised as Send");
    await assert(`${q('#live-send')}.disabled && (${enabled(stop)})`, "Normal prompt is unavailable during running root; Stop remains available");
    await assert(`${q("#composer-hint")}.textContent.includes("queue next")`, "Running shortcut explicitly announces Queue next, never implies ordinary Send");
    await assert(`(() => {const a=${q(enqueue)}.getBoundingClientRect(),b=${q(stop)}.getBoundingClientRect();return a.width>0&&a.height>0&&a.left>=-1&&a.right<=innerWidth+1&&a.top>=-1&&a.bottom<=innerHeight+1&&(a.right<=b.left||a.left>=b.right||a.bottom<=b.top||a.top>=b.bottom);})()`, `Queue next is visible, inside ${width}×${height} ${theme}, and does not overlap Stop`);
    await assert("document.documentElement.scrollWidth<=innerWidth+1", "Queue controls create no horizontal document overflow");
    await shortcut(); await count("queue-enqueue", 1);
    await wait(`${q(item('queued-1'))}!==null && ${q(prompt)}.value===''`);
    check(requests("queue-enqueue")[0].fields.text === draft && requests("queue-enqueue")[0].fields.queue_token === "root-queue-token" && requests("queue-enqueue")[0].fields.queue_revision === "1", "One enqueue captures exact text and observed queue nonce/revision");
    await assert(`${q(item('queued-1'))}.textContent.includes(${JSON.stringify(draft)}) && !${q('#live-transcript')}.contains(${q(item('queued-1'))})`, "Accepted pending text lives outside conversation transcript");
    await unchanged("Accepted enqueue"); await clean("Accepted enqueue"); await capture("pending-outside-transcript");

    // Successful acknowledgment only clears the exact submitted draft revision.
    await navigate(); state.behavior = "hold"; await enqueueDraft();
    await assert(`${enabled(stop)}`, "Stop stays actionable during an in-flight queue request");
    await shortcut(); await delay(60); await count("queue-enqueue", 1);
    await replace("New typing while enqueue acknowledgement is delayed 📝."); const newerSelection = await selection();
    await key("Enter", "Enter", 13); // a literal newline, never queue retry
    const newerDraft = await evaluate(`${q(prompt)}.value`), selectedAfterTyping = await selection();
    state.release(); await wait(`${q(item('queued-1'))}!==null`); await delay(100);
    await assert(`${q(prompt)}.value===${JSON.stringify(newerDraft)}`, "Delayed successful enqueue preserves concurrent typing instead of clearing it");
    check(JSON.stringify(await selection()) === JSON.stringify(selectedAfterTyping) && newerSelection.start >= 0, "Delayed acknowledgment preserves current composer selection");
    await count("queue-enqueue", 1); await unchanged("Delayed enqueue"); await clean("Concurrent typing");

    // Stable list identities and a separate local editor, with explicit CAS save.
    await navigate(); await seed([queueItem("one"), queueItem("two")]); await replace("Do not overwrite this composer draft.");
    await click(action("one", "edit")); await wait(visible(editor("one")));
    await replace("Locally edited pending one — not saved yet.", editor("one")); const editSelection = await selection(editor("one"));
    state.queue({items: [queueItem("one"), queueItem("two"), queueItem("three")]}); await delay(100);
    await assert(`${q(editor('one'))}.value==='Locally edited pending one — not saved yet.'`, "Unrelated list refresh cannot overwrite pending item editor");
    check(JSON.stringify(await selection(editor("one"))) === JSON.stringify(editSelection), "List refresh preserves local item editor selection");
    await assert(`${q(prompt)}.value==='Do not overwrite this composer draft.'`, "Pending item editor never borrows composer draft");
    await click(action("one", "cancel"));
    check(requests("queue-update").length === 0, "Cancel item edit is local-only");
    await assert(`${q(item('one'))}.textContent.includes('Pending one text.')`, "Cancel editor leaves authoritative queued original unchanged");
    await click(action("one", "edit")); await replace("Saved replacement pending text.", editor("one"));
    await focus(action("one", "save")); await key("Enter", "Enter", 13); await count("queue-update", 1);
    await wait(`${q(item('one'))}.textContent.includes('Saved replacement pending text.')`);
    check(requests("queue-update")[0].fields.item_id === "one" && requests("queue-update")[0].fields.text === "Saved replacement pending text.", "Save CAS targets stable exact item ID and replacement text");
    await click(action("two", "remove")); await count("queue-remove", 1); await wait(`${q(item('two'))}===null`);
    check(requests("queue-remove")[0].fields.item_id === "two" && !Object.hasOwn(requests("queue-remove")[0].fields, "text"), "Remove CAS targets exact stable item ID without text");
    await unchanged("Pending update/remove"); await clean("Pending update/remove");

    await navigate(); await seed([queueItem("stale-editor")]);
    await click(action("stale-editor", "edit")); await replace("Retain replacement after stale editor CAS.", editor("stale-editor"));
    const editorBaseRevision = state.snapshot.queue.revision;
    state.queue({items: [queueItem("stale-editor"), queueItem("newer-item")]}); await delay(70);
    await click(action("stale-editor", "save")); await count("queue-update", 1); await delay(100);
    check(requests("queue-update")[0].fields.queue_revision === String(editorBaseRevision), "Editor save keeps its original CAS revision rather than silently retargeting newer queue");
    await assert(`${q(editor("stale-editor"))}.value==='Retain replacement after stale editor CAS.'`, "Stale editor CAS rejection preserves replacement text for explicit review");
    await count("queue-update", 1); await clean("Stale editor CAS");

    // Accepted enqueue races delivery: the old HTTP snapshot must not resurrect it.
    await navigate(); state.behavior = "hold"; await enqueueDraft();
    state.deliver("queued-1"); state.release();
    await wait(`${q(row('delivered-queued-1'))}!==null`); await delay(100);
    await assert(`${q(item('queued-1'))}===null`, "Late original enqueue acknowledgment cannot resurrect consumed item");
    await assert(`JSON.stringify([...document.querySelectorAll('#live-transcript [data-message-id]')].map(n=>n.dataset.messageId))===${JSON.stringify(JSON.stringify([...originals.map(message => message.id), "delivered-queued-1", "step-queued-1", "answer-queued-1"]))}`, "Delivered source user appears chronologically before tool and answer, exactly once");
    await assert(`${q(row('delivered-queued-1'))}?.querySelector('.message-body')?.textContent===${JSON.stringify(draft)}`, "Only actual delivery adds authoritative user source text");
    state.update({status: "idle", cancel_token: "", queue: {...state.snapshot.queue, revision: state.snapshot.queue.revision + 1, can_enqueue: false}});
    await wait(`${q(row('delivered-queued-1')+' [data-message-edit]')} && !${q(row('delivered-queued-1')+' [data-message-edit]')}.disabled`);
    await assert(`${q(row('answer-queued-1')+' [data-message-regenerate]')} && !${q(row('answer-queued-1')+' [data-message-regenerate]')}.disabled`, "Delivered queued turn final response preserves exact regeneration capability");
    await assert(`!${q(row('root-user')+' [data-message-edit]')} && !${q(row('step-queued-1')+' [data-message-regenerate]')}`, "Historical actions honor source flags, never infer closest user or tool preface");
    await count("queue-enqueue", 1); await clean("Delivery beats acknowledgment"); await capture("delivered-source-order");

    // A stale queue revision with an older outer snapshot cannot regress list.
    await navigate(); await seed([queueItem("one"), queueItem("two")]); const oldSnapshot = structuredClone(state.snapshot);
    state.deliver("one"); await wait(`${q(row('delivered-one'))}!==null`); state.emit(oldSnapshot); await delay(100);
    await assert(`${q(item('one'))}===null && ${q(item('two'))}!==null`, "Out-of-order SSE snapshot cannot restore consumed stable ID");
    await clean("Out-of-order SSE");

    for (const mutation of ["update", "remove"]) {
      await navigate(); await seed([queueItem("race")]);
      if (mutation === "update") { await click(action("race", "edit")); await replace("Keep my raced editor replacement.", editor("race")); }
      state.behavior = "hold-reject";
      await click(action("race", mutation === "update" ? "save" : "remove")); await count(`queue-${mutation}`, 1);
      state.deliver("race"); state.release(); await delay(160);
      await assert(`${q(row('delivered-race'))}!==null`, `${mutation} race: actual delivered source stays in transcript`);
      if (mutation === "update") await assert(`${q(panel)}.textContent.includes('Keep my raced editor replacement.') || [...${q(panel)}.querySelectorAll('textarea')].some(n=>n.value==='Keep my raced editor replacement.')`, "Update/delivery rejection preserves unsaved replacement text for review");
      await assert(`/delivered|unknown|review|changed/i.test(${q(panel)}.textContent)`, `${mutation} race: no silent success; explicit review outcome`);
      await count(`queue-${mutation}`, 1); await clean(`${mutation} versus delivery`);
    }

    // Cancellation preserves pending text as review-only; copy is never send.
    await navigate(); await seed([queueItem("held-one"), queueItem("held-two")]); await replace("Prior unsent composer draft.");
    await click(stop); await count("cancel", 1);
    state.update({status: "idle", cancel_token: "", cancel_requested: false, recovery: {state: "canceled"}});
    await wait(`${q(item('held-one'))}.textContent.includes('Pending held-one text.')`);
    await assert(`${q(action('held-one','edit'))}===null || ${q(action('held-one','edit'))}.disabled || ${q(action('held-one','edit'))}.hidden`, "Canceled held items cannot be edited back into scheduled work");
    await click(action("held-one", "copy-draft"));
    await assert(`${q(prompt)}.value==='Pending held-one text.'`, "Held item Copy to draft is explicit and local-only");
    check(requests("queue-enqueue").length === 0, "Copy held text never sends or automatically queues it");
    await click("[data-queue-restore-draft]");
    await assert(`${q(prompt)}.value==='Prior unsent composer draft.'`, "Copy-to-draft workflow restores earlier unsent draft");
    await click(action("held-two", "remove")); await count("queue-remove", 1); await wait(`${q(item("held-two"))}===null`);
    check(requests("queue-enqueue").length === 0, "Removing held review text never revokes prior effects or reschedules a turn");
    await clean("Stop and review"); await capture("held-review-copy");

    await navigate(); state.behavior = "hold"; await enqueueDraft();
    await click(stop); await count("cancel", 1); state.release(); await delay(120);
    await assert(`${q(item("queued-1"))}.dataset.queueState==='held'`, "Stop racing delayed enqueue acknowledgment preserves held review text, never pending replay");
    await count("queue-enqueue", 1); await clean("Stop during pending enqueue");

    // Starting is a valid locked pre-delivery state, not a chat source or an
    // uncertain review item. A Stop latch alone cannot rewrite its outcome.
    await navigate(); await seed([queueItem("starting", "Delivery reserved, not durably observed yet.", "starting")]);
    await assert(`${q(item("starting"))}.dataset.queueState==='starting' && !/Could not verify/i.test(${q(panel)}.textContent)`, "Starting snapshot is a valid queue projection, not malformed or uncertain");
    await assert(`/starting.*locked/i.test(${q(item("starting"))}.textContent)`, "Starting item visibly reports delivery progress and lock");
    for (const name of ["edit", "remove", "copy-draft"]) {
      await assert(`${q(action("starting", name))}.hidden || ${q(action("starting", name))}.disabled`, `Starting item cannot ${name} before delivery outcome is known`);
    }
    await unchanged("Starting reservation before durable source event");
    await click(prompt); await key("Tab", "Tab", 9); await key("Tab", "Tab", 9);
    await assert(`!document.activeElement.closest(${JSON.stringify(item("starting"))})`, "Native Tab cannot enter locked starting-item actions");
    await click(stop); await count("cancel", 1); await delay(100);
    await assert(`${q(item("starting"))}.dataset.queueState==='starting' && !/unknown|may already/i.test(${q(item("starting"))}.textContent)`, "Stop latch alone leaves starting outcome locked, never invents uncertainty or holding");
    for (const name of ["edit", "remove", "copy-draft"]) await assert(`${q(action("starting", name))}.hidden || ${q(action("starting", name))}.disabled`, `Stop-latched starting item still cannot ${name}`);
    await unchanged("Stop-latched starting reservation");
    check(requests("queue-update").length === 0 && requests("queue-remove").length === 0 && requests("queue-enqueue").length === 0, "Starting inspection and Stop never issue queue mutation or prompt fallback");
    state.deliver("starting"); await wait(`${q(row("delivered-starting"))}!==null`);
    await assert(`${q(item("starting"))}===null && document.querySelectorAll(${JSON.stringify(row("delivered-starting"))}).length===1`, "Only subsequent authoritative delivery event retires starting reservation and adds user exactly once");
    await assert(`JSON.stringify([...document.querySelectorAll('#live-transcript [data-message-id]')].map(n=>n.dataset.messageId))===${JSON.stringify(JSON.stringify([...originals.map(message => message.id), "delivered-starting", "step-starting", "answer-starting"]))}`, "Starting-to-delivered source appears before its tools and response");
    await clean("Starting and Stop-latched delivery"); await capture("starting-delivered-after-stop-latch");

    await navigate(); await seed([queueItem("failed-starting", "Keep reservation text after worker failure.", "starting")]);
    state.update({status: "failed", cancel_token: "", queue: {...state.snapshot.queue, revision: state.snapshot.queue.revision + 1, can_enqueue: false, items: [queueItem("failed-starting", "Keep reservation text after worker failure.", "uncertain")]}});
    await wait(`${q(item("failed-starting"))}.dataset.queueState==='uncertain'`);
    await assert(`/unknown.*already.*delivered/i.test(${q(item("failed-starting"))}.textContent)`, "Worker failure promotes starting to explicitly uncertain review, not a fabricated delivery");
    await unchanged("Worker failure before observed delivery");
    await click(action("failed-starting", "copy-draft"));
    await assert(`${q(prompt)}.value==='Keep reservation text after worker failure.'`, "Failed starting reservation preserves exact review text for explicit Copy to draft");
    check(state.requests.length === 0, "Worker-failure review copy never requeues or starts a normal prompt"); await clean("Starting worker failure");

    await navigate(); await seed([queueItem("uncertain", "Possibly already delivered input.", "uncertain")], {can_enqueue: false});
    state.update({status: "failed", cancel_token: ""}); await delay(100);
    await assert(`/unknown.*already.*delivered/i.test(${q(item("uncertain"))}.textContent)`, "Uncertain item visibly warns it may already have been delivered");
    await click(action("uncertain", "copy-draft"));
    await assert(`${q(prompt)}.value==='Possibly already delivered input.'`, "Failed-run uncertain input can be explicitly copied for review");
    check(state.requests.length === 0, "Review-only failed-run copy never restarts or reschedules work"); await clean("Failed uncertain review");

    await navigate(); await seed([queueItem("reconnect", "Keep across read-only reconnect.")]); await replace("Draft during reconnect.");
    const oldSubscriptions = state.subscriptions;
    for (const stream of state.streams) stream.response.end();
    state.queue({can_enqueue: false, items: [queueItem("reconnect", "Keep across read-only reconnect.", "held")]});
    for (let i = 0; i < 240 && state.subscriptions === oldSubscriptions; i++) await delay(25);
    check(state.subscriptions > oldSubscriptions, "Native EventSource reconnects through real loopback SSE transport");
    await wait(`${q(item("reconnect"))}.dataset.queueState==='held'`);
    await assert(`${q(prompt)}.value==='Draft during reconnect.'`, "Read-only reconnect preserves current composer draft");
    check(state.requests.length === 0, "Reconnect cannot replay held input or submit a normal prompt"); await clean("Read-only reconnect");

    // Takeovers own the composer seat, without losing queue or normal draft.
    for (const kind of ["permission", "input"]) {
      await navigate(); await seed([queueItem("attention")]); await replace("Draft before attention takeover.");
      const attention = kind === "permission" ? {permission: {id: "approval-one", tool: "read", risk: "low", reason: "Fixture approval", paths: ["README.md"]}} : {input: {id: "question-one", questions: [{id: "choice-one", header: "Fixture question", question: "Continue?", options: [{label: "Yes", description: "Continue"}]}]}};
      state.update({status: kind, ...attention}); await wait(`${q('#live-attention')}.textContent.length>0 && !${q('#live-attention')}.hidden`);
      await assert(`${q(prompt)}.value==='Draft before attention takeover.' && ${q(item('attention'))}!==null`, `${kind}: attention preserves normal composer draft and separate pending list`);
      state.update({status: "running", permission: null, input: null}); await wait(visible(prompt));
      await assert(`${q(prompt)}.value==='Draft before attention takeover.'`, `${kind}: releasing takeover restores draft`);
      await clean(`${kind} takeover`);
    }

    for (const behavior of ["reject", "closing", "unknown", "malformed", "missing-queue", "wrong-instance", "wrong-session", "wrong-token", "unsafe-revision"]) {
      await navigate(); state.behavior = behavior; await enqueueDraft(); await delay(140);
      await assert(`${q(prompt)}.value===${JSON.stringify(draft)}`, `${behavior}: rejected/unverified enqueue keeps exact draft for review`);
      await shortcut(); await delay(70); await count("queue-enqueue", 1);
      await assert(`/unknown|review|changed|closed|already/i.test(${q(panel)}.textContent)`, `${behavior}: visible queue error/review, never ordinary prompt fallback`);
      await clean(`${behavior} acknowledgment`);
    }

    await navigate(); state.behavior = "hold"; await enqueueDraft();
    await replace("New-root draft must not be retargeted.");
    state.update({cancel_token: "new-root-cancel", queue: {token: "new-root-queue", revision: 1, can_enqueue: true, items: []}}); state.release(); await delay(160);
    await assert(`${q(item("queued-1"))}===null`, "Late original-root acknowledgment cannot resurrect old pending item");
    await assert(`${q(prompt)}.value==='New-root draft must not be retargeted.'`, "Late original-root acknowledgment cannot clear newer draft");
    await count("queue-enqueue", 1); await clean("Root nonce replacement");

    await navigate(); state.behavior = "hold"; await enqueueDraft(); await replace("Keep draft across instance replacement.");
    const previousInstance = state.snapshot.instance_id;
    state.snapshot = {...state.snapshot, instance_id: "replacement-instance", revision: state.snapshot.revision + 1, queue: {token: "other-instance-queue", revision: 1, can_enqueue: false, items: []}};
    state.emit(state.snapshot, previousInstance); state.release(); await delay(160);
    await assert(`${q(prompt)}.value==='Keep draft across instance replacement.'`, "Replaced instance preserves local draft and never retargets original enqueue");
    await count("queue-enqueue", 1); await clean("Instance replacement");

    for (const invalid of ["empty-token", "unsafe-revision", "duplicate-id", "invalid-state", "full", "same-revision-full", "byte-full"]) {
      await navigate();
      let queue = {...state.snapshot.queue};
      if (invalid === "empty-token") queue.token = "";
      if (invalid === "unsafe-revision") queue.revision = Number.MAX_SAFE_INTEGER + 1;
      if (invalid === "duplicate-id") queue.items = [queueItem("duplicate"), queueItem("duplicate")];
      if (invalid === "invalid-state") queue.items = [queueItem("invalid", "Invalid state text", "resend-automatically")];
      if (invalid === "full") { queue.items = Array.from({length: 8}, (_, index) => queueItem(`full-${index}`)); queue.revision++; }
      if (invalid === "same-revision-full") queue.items = Array.from({length: 8}, (_, index) => queueItem(`same-version-full-${index}`));
      if (invalid === "byte-full") { queue.items = Array.from({length: 4}, (_, index) => queueItem(`bytes-${index}`, "x".repeat(65536))); queue.revision++; }
      state.update({queue}); await delay(100); await replace(draft);
      if (invalid === "byte-full") {
        await shortcut(); await delay(100);
        await assert(`${q(prompt)}.value===${JSON.stringify(draft)} && /review|full|limit|bound|KiB/i.test(${q(panel)}.textContent)`, "256KiB aggregate queue bound preserves rejected draft, never starts a prompt");
        check(requests("queue-enqueue").length <= 1, "Aggregate bound rejection never retries");
      } else await guarded(invalid);
      await clean(`${invalid} queue`);
    }
    await navigate(); await replace("  \n\t"); await guarded("Whitespace-only queued text");
    await replace("é".repeat(40000)); await guarded("UTF-8 text exceeds 64KiB without hitting native character maxlength"); await clean("Text bounds");
    await navigate(); state.update({status: "idle", cancel_token: "", queue: {...state.snapshot.queue, can_enqueue: false}}); await delay(80); await replace(draft);
    await assert(`${q(enqueue)}.hidden || ${q(enqueue)}.disabled`, "Idle queue is not a scheduler or implicit new root");
    check(state.requests.length === 0, "Idle queue observation cannot start work"); await clean("Idle queue");
    await navigate("workflow-cancel"); await assert(`!${q(enqueue)} && !${q(panel)}`, "Unsupported host never synthesizes Queue next"); await clean("Unsupported capability");
    await navigate("saved-user"); await assert(`!${q(enqueue)} && !${q(panel)}`, "Saved inactive conversation has no queue scheduler or worker activation"); await clean("Saved inactive page");
    return {results, failures};
  } catch (error) { error.results = results; error.failures = failures; throw error; }
}
