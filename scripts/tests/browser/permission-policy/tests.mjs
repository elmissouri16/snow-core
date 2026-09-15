// All user activations below use CDP native mouse/key events, not .click() or
// dispatched DOM events. Evaluations inspect DOM or stage input/server fixtures.
import {setTimeout as delay} from "node:timers/promises";

export async function checks({state, evaluate, wait, key, click, navigate, insert, width, height, theme}) {
  const results = [], failures = [];
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  const assert = async (expression, label) => check(await evaluate(expression), label);
  const trigger = "[data-permission-policy-menu]", label = "[data-permission-policy-label]";
  const dialog = "#workflow-permission-dialog", ack = "[data-policy-ack]", confirm = "[data-policy-confirm]";
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const policies = () => state.requests.filter(item => item.path.endsWith("/permission-mode"));
  const prompts = () => state.requests.filter(item => item.path.endsWith("/prompt"));
  const connected = () => wait(`${q(trigger)} && !${q(trigger)}.disabled && ${q("#live-connection")}.textContent === "Live"`);
  const waitRequests = async count => {
    for (let i = 0; i < 100 && policies().length < count; i++) await delay(20);
    check(policies().length === count, `Exactly ${count} explicit policy request(s)`);
  };
  const bounds = async (selector, name) => assert(`(() => { const n=${q(selector)}, r=n.getBoundingClientRect(); return r.width>0 && r.height>0 && r.left>=-1 && r.top>=-1 && r.right<=innerWidth+1 && r.bottom<=innerHeight+1; })()`, `${name} stays inside ${width}×${height} ${theme} viewport`);
  const open = async () => {
    await click(trigger);
    try { await wait(`${q('.snow-menu[aria-label="Session permissions"]')} !== null`); }
    catch (error) { throw new Error(`${error.message}; after=${results.at(-1)}; requests=${JSON.stringify(policies())}; errors=${await evaluate("JSON.stringify(policyErrors)")}; ui=${await evaluate('JSON.stringify({events:policyEvents,menu:document.querySelector(".snow-menu")?.textContent,disabled:document.querySelector("[data-permission-policy-menu]").disabled,unknown:!document.querySelector("#live-unknown").hidden,dialog:document.querySelector("dialog[open]")?.id,hit:document.elementFromPoint(...(()=>{const r=document.querySelector("[data-permission-policy-menu]").getBoundingClientRect();return [r.x+r.width/2,r.y+r.height/2]})())?.outerHTML})')}`); }
  };
  const choose = async mode => { await open(); await click(`[data-menu-key="${mode}"]`); };
  const allowDialog = async () => {
    await choose("allow"); await wait(`${q(dialog)}.open`);
    await assert(`!${q(ack)}.checked && ${q(confirm)}.disabled`, "Each Allow dialog starts unchecked with confirmation disabled");
  };
  const snapshot = async fields => { const previous = state.reads; state.update(fields); for (let i = 0; i < 100 && state.reads <= previous; i++) await delay(20); await delay(60); };

  await navigate();
  await assert(`${q(label)}.textContent === "Ask"`, "Initial label comes from known public snapshot");
  await assert(`${q(trigger)}.getAttribute("aria-label") === "Session permissions: Ask"`, "Compact icon keeps accessible current-policy label");
  await assert('typeof window.SnowMenus?.open === "function"', "Production shared SnowMenus is loaded");
  await bounds(trigger, "Policy trigger");
  await assert('document.documentElement.scrollWidth <= innerWidth + 1', "Composer creates no horizontal document overflow");
  await open();
  await bounds(".snow-menu", "Shared policy menu");
  await assert(`${q(trigger)}.getAttribute("aria-controls") === ${q(".snow-menu")}.id && ${q(trigger)}.getAttribute("aria-expanded") === "true"`, "Menu uses shared ARIA linkage");
  await assert('document.querySelectorAll(".snow-menu [role=menuitemradio]").length === 3 && document.querySelector("[data-menu-key=ask]").getAttribute("aria-checked") === "true"', "Ask / Deny / Allow radio rows reflect current snapshot");
  await assert('/Session only · not a sandbox/.test(document.querySelector(".snow-menu").textContent) && !/host account/.test(document.querySelector(".snow-menu").textContent)', "Root menu has one short boundary note, not two expanded paragraphs");
  await click('[data-menu-key="permission-help"]');
  await assert('/not sandbox modes/.test(document.querySelector(".snow-menu").textContent) && /host account/.test(document.querySelector(".snow-menu").textContent)', "Details retains the complete approval and host-authority explanation");
  await key("Escape", "Escape", 27);
  await assert('document.querySelector("[data-menu-key=ask]") !== null', "Escape from Details returns to policy choices without mutation");
  check(state.requests.length === 0, "Opening menu performs no POST, discovery, prompt, or policy action");
  await key("Escape", "Escape", 27);
  await assert(`${q('.snow-menu')} === null && document.activeElement === ${q(trigger)}`, "Escape dismisses menu and restores trigger focus");
  await key(" ", "Space", 32);
  await wait(`${q('.snow-menu')} !== null`);
  await key("ArrowDown", "ArrowDown", 40);
  await assert('document.activeElement.dataset.menuKey === "deny"', "ArrowDown moves through shared menu rows");
  await key("End", "End", 35);
  await assert('document.activeElement.dataset.menuKey === "permission-help"', "End reaches Details even in short scrollable menu");
  await key("ArrowUp", "ArrowUp", 38);
  await assert('document.activeElement.dataset.menuKey === "allow"', "Allow remains keyboard-reachable before Details");
  await key("Home", "Home", 36);
  await key("Enter", "Enter", 13);
  await assert(`${q('.snow-menu')} === null`, "Keyboard selecting current Ask closes menu");
  check(state.requests.length === 0, "Current-policy selection and keyboard navigation perform no action");

  await allowDialog();
  await bounds(dialog, "Allow confirmation dialog");
  await click(confirm); // Native disabled button must not dispatch a mutation.
  check(policies().length === 0, "Unchecked Allow cannot submit through native click");
  await evaluate(`${q(ack)}.focus()`);
  await key("Enter", "Enter", 13);
  check(policies().length === 0, "Enter on unchecked acknowledgement cannot authorize Allow");
  await click("[data-policy-cancel]");
  await assert(`!${q(dialog)}.open && !${q(ack)}.checked`, "Cancel closes confirmation and clears acknowledgement");
  check(policies().length === 0, "Cancel performs no policy action");
  await allowDialog();
  await click(ack);
  await assert(`${q(ack)}.checked && !${q(confirm)}.disabled`, "Explicit checkbox enables Allow confirmation");
  await key("Escape", "Escape", 27);
  await assert(`!${q(dialog)}.open && !${q(ack)}.checked`, "Native dialog Escape clears previous acknowledgement");
  check(policies().length === 0, "Acknowledgement plus Escape performs no policy action");
  await allowDialog();
  await evaluate(`${q(ack)}.focus()`);
  await key(" ", "Space", 32);
  await assert(`${q(ack)}.checked && !${q(confirm)}.disabled`, "Native Space explicitly checks Allow acknowledgement");
  await key("Tab", "Tab", 9);
  // Confirm placement is verified independently; focus it before native Enter
  // so this remains robust to the preceding cancel button in the dialog.
  await evaluate(`${q(confirm)}.focus()`);
  await key("Enter", "Enter", 13);
  await key("Enter", "Enter", 13);
  await waitRequests(1); await connected();
  // A fast in-place acknowledgement can re-enable the restored trigger before
  // the second Enter. Opening its menu is valid; it must not repeat Allow.
  if (await evaluate('!!document.querySelector(".snow-menu")')) await key("Escape", "Escape", 27);
  await assert(`${q(label)}.textContent === "Allow"`, "Confirmed Allow displays authoritative accepted snapshot");
  check(policies()[0]?.fields.confirm_allow === "allow" && policies()[0]?.fields.mode === "allow", "Exactly one checked Allow request carries explicit confirm_allow=allow");

  await choose("ask"); await waitRequests(2); await connected();
  await assert(`${q(label)}.textContent === "Ask"`, "Ask applies through one immediate validated request");
  check(!Object.hasOwn(policies()[1]?.fields || {}, "confirm_allow"), "Ask does not send an Allow acknowledgement");
  state.next = "hold";
  await choose("deny"); await waitRequests(3);
  await assert(`${q(trigger)}.disabled && ${q(label)}.textContent !== "Deny"`, "Pending Deny locks selector without optimistic requested-policy display");
  check(typeof state.held === "function", "Actual HTTP mutation remains pending until fixture responds");
  state.held("deny"); await connected();
  await assert(`${q(label)}.textContent === "Deny"`, "Deny displays only after authoritative response/fresh snapshot");
  check(!Object.hasOwn(policies()[2]?.fields || {}, "confirm_allow"), "Deny is one validated request without Allow acknowledgement");
  await choose("deny");
  check(policies().length === 3, "Clicking current Deny does not send a redundant action");
  await snapshot({permission_mode: "ask"}); await connected();
  await assert(`${q(label)}.textContent === "Ask"`, "Later public snapshot, not last requested mode, determines display");
  check(policies().length === 3, "Authoritative snapshot update never replays last command");

  // Even an acknowledged open Allow dialog must be revoked as soon as runtime
  // state ceases to authorize a fresh explicit policy action.
  for (const [name, fields] of [
    ["running", {status: "running"}], ["permission", {status: "permission", permission: {id: "fixture-approval", tool: "write", risk: "write", reason: "Fixture approval"}}],
    ["input", {status: "input", input: {id: "fixture-input", questions: [{id: "question", header: "Fixture question", question: "Choose an answer", options: [{label: "Yes", description: "Fixture option"}]}]}}],
    ["idle with pending permission", {status: "idle", permission: {id: "pending-approval", tool: "write"}}],
    ["idle with pending input", {status: "idle", input: {id: "pending-input", questions: []}}],
    ["admitted recovery", {status: "idle", recovery: {state: "admitted"}}],
    ["unknown-admission recovery", {status: "idle", recovery: {state: "admission_unknown"}}],
    ["unknown runtime state", {status: "future-status"}], ["missing policy", {permission_mode: undefined}], ["null policy", {permission_mode: null}], ["unknown policy", {permission_mode: "future-policy"}]
  ]) {
    await allowDialog(); await click(ack);
    await snapshot(fields);
    await assert(`${q(trigger)}.disabled`, `${name}: selector locks`);
    await assert(`!${q(dialog)}.open && !${q(ack)}.checked`, `${name}: confirmation closes and acknowledgement resets`);
    if (name.includes("policy")) await assert(`${q(label)}.textContent === "Unknown"`, `${name}: unsupported policy is not displayed as Ask`);
    await key("Enter", "Enter", 13);
    check(policies().length === 3, `${name}: stale keyboard confirmation cannot mutate policy`);
    await snapshot({status: "idle", permission: null, input: null, recovery: null, permission_mode: "ask"}); await connected();
  }
  await allowDialog(); await click(ack); await snapshot({permission_mode: "deny"});
  await assert(`!${q(dialog)}.open && !${q(ack)}.checked && ${q(label)}.textContent === "Deny"`, "Authoritative policy change revokes a confirmation bound to previous policy");
  check(policies().length === 3, "Policy change during confirmation does not authorize stale Allow");
  await snapshot({permission_mode: "ask"}); await connected();
  await open(); await snapshot({status: "running"});
  await assert(`${q('.snow-menu')} === null && ${q(trigger)}.disabled`, "Transition to running closes an open menu");
  await snapshot({status: "idle"}); await connected();
  await allowDialog(); await click(ack); state.offline = true;
  await wait(`${q(trigger)}.disabled`);
  await assert(`!${q(dialog)}.open && !${q(ack)}.checked && ${q(label)}.textContent === "Unknown"`, "Offline disconnect closes confirmation, resets acknowledgement, and clears trusted label");
  await delay(160); check(policies().length === 3, "Offline polling performs no automatic mutation retry");
  state.offline = false; await connected();
  await allowDialog(); await click(ack);
  await snapshot({instance_id: "replacement-instance"});
  await assert(`${q(trigger)}.disabled && !${q(dialog)}.open && !${q(ack)}.checked`, "Instance replacement locks stale controls and revokes acknowledged Allow");
  await assert(`${q(label)}.textContent === "Unknown"`, "Stale instance does not present a trusted policy");
  await delay(160); check(policies().length === 3, "Replacement instance receives no automatic policy action");
  check(state.errors.length === 0, `HTTP contract validation has no errors: ${state.errors.join("; ")}`);
  await assert('policyErrors.length === 0', "No production JavaScript errors during control lifecycle");

  await navigate(); await allowDialog(); await click(ack);
  await snapshot({session_id: "replacement-session"});
  await assert(`${q(trigger)}.disabled && !${q(dialog)}.open && !${q(ack)}.checked`, "Session replacement locks stale controls and revokes acknowledged Allow");
  check(policies().length === 0, "Replacement session receives no stale policy action");

  for (const fault of ["fail", "empty", "missing-status", "invalid-status"]) {
    await navigate(); state.next = fault;
    await choose("deny"); await waitRequests(1);
    await wait(`!${q("#live-unknown")}.hidden`, `unknown policy outcome for ${fault}; requests=${JSON.stringify(policies())}`);
    await assert(`${q(trigger)}.disabled && ${q(label)}.textContent !== "Deny"`, `${fault}: failed or malformed acknowledgement does not optimistically enable requested policy`);
    const reads = state.reads; await delay(240);
    check(state.reads > reads, `${fault}: fresh read reconciliation still occurs`);
    check(policies().length === 1, `${fault}: failure never automatically retries mutation`);
    await assert(`${q(trigger)}.disabled`, `${fault}: a fresh read alone cannot clear unknown-outcome guard`);
    check(state.errors.length === 0, `${fault}: rejected response was intentional, request contract still valid`);
  }

  await navigate();
  await assert(`${q("#live-composer")}.noValidate`, "Live composer has novalidate; native validity bubbles are not workflow UI");
  await click("#live-send");
  await evaluate(`${q("#live-prompt")}.focus()`);
  await key("Enter", "Enter", 13, 2);
  check(prompts().length === 0, "Blank click and Ctrl+Enter send no prompt request");
  await insert("   ");
  await click("#live-send");
  await evaluate(`${q("#live-prompt")}.focus()`); await key("Enter", "Enter", 13, 2);
  check(prompts().length === 0, "Whitespace-only text remains guarded without native validation");
  await assert('policyInvalidEvents === 0', "Blank click / Ctrl+Enter cause zero native invalid events");
  await evaluate(`${q("#live-prompt")}.value = "界".repeat(22000); window.policySizeErrorSeen=false; window.policySizeObserver=new MutationObserver(() => { if (${q("#live-error")}.textContent.includes("64 KiB")) window.policySizeErrorSeen=true; }); policySizeObserver.observe(${q("#live-error")},{childList:true,subtree:true,characterData:true});`);
  await click("#live-send");
  check(prompts().length === 0, "Actual UTF-8 64KiB text bound remains enforced with novalidate");
  await assert("policySizeErrorSeen", "Oversized actual text shows application-owned validation error");
  await evaluate("policySizeObserver.disconnect()");
  await evaluate(`${q("#live-prompt")}.value = ""; ${q("#live-prompt")}.focus()`);
  await insert("Explicit fixture prompt"); await key("Enter", "Enter", 13, 2);
  for (let i = 0; i < 100 && prompts().length === 0; i++) await delay(20);
  check(prompts().length === 1 && prompts()[0].fields.text === "Explicit fixture prompt", "Nonblank native text + Ctrl+Enter sends exactly one validated actual prompt");
  check(policies().length === 0, "Composer controls never modify permission policy");
  check(state.errors.length === 0, `Final HTTP contract validation has no errors: ${state.errors.join("; ")}`);
  await assert('policyErrors.length === 0 && policyInvalidEvents === 0', "No production JavaScript errors or native invalid events");
  return {results, failures};
}
