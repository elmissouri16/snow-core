// Native historical edit and history-only restore through the production worker.
import {readFile, writeFile} from "node:fs/promises";
import {join} from "node:path";
import {setTimeout as delay} from "node:timers/promises";

export async function versionsChecks({ready,directory,click,key,replace,evaluate,wait,check,assert,requests,responses,callCount,waitCall,gate,snapshot,history,send,width,client,sessionId}) {
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const posts = action => requests.filter(request => request.method === "POST" && request.path.endsWith("/" + action));
  const identity = value => [value.project_id,value.session_id,value.instance_id];
  const settings = value => [value.provider,value.model,value.mode,value.permission_mode,value.thinking];
  const source = "Production manager root input.", edited = "Historical edit creates a real saved branch — café 😀.";
  const draft = "Unsent draft must survive history-only restore — not a new prompt.";
  const durable = async () => (await history()).find(session => session.session_id === readySession);
  const readySession = (await snapshot()).session_id;
  await click("[data-queue-remove]");
  await wait(`!${q('[data-queue-item-id]')}`, "explicitly remove held review item before history mutation");
  check(await callCount() === 3, "Explicit held-item removal permits history review without provider execution");
  await wait(`${q('[data-versions-open]')} && !${q('[data-versions-open]')}.disabled`, "real worker publishes Versions capability");
  for (const asset of ["generated/app.js","versions.css"]) check(responses.some(response => response.path.endsWith("/"+asset) && response.status === 200), `${asset}: actual production asset HTTP 200`);
  const baseline = await snapshot(), originalDurable = await durable();
  await click('[data-session-menu]'); await click('[data-menu-key="versions"]');
  await wait(`!!${q('[data-version-preview]')} && !${q('[data-version-preview]')}.disabled`, "real Versions list loads from RPC");
  const original = await evaluate(`(() => {const button=[...document.querySelectorAll('[data-version-preview]')].find(el=>el.textContent.includes(' · Current'));return button?{branch:button.dataset.versionId,tip:button.dataset.tipId}:null;})()`);
  check(!!original?.branch && original.tip === originalDurable.branch_tip, "Current Versions identity matches durable SQLite branch tip");
  if (!original) throw Error("Current durable version missing");
  const originalButton = `[data-version-preview][data-version-id="${original.branch}"]`;
  await click(originalButton);
  await wait(`${q('[data-version-preview-messages]')}.textContent.includes(${JSON.stringify(source)})`, "current branch preview reads real saved source");
  await assert(`${q('[data-version-restore]')}.hidden && ${q('#versions-boundary')}.textContent.includes('does not undo file changes or tool effects')`, "Current version cannot be restored and UI explicitly denies filesystem/tool undo");
  check(JSON.stringify(identity(await snapshot())) === JSON.stringify(identity(baseline)) && await callCount() === 3 && (await durable()).branch_tip === originalDurable.branch_tip, "Native Versions list and current preview do not execute, replace worker or mutate durable history");
  // Delay reads at the browser boundary, retaining production requests and
  // backend replies. Cached presentation must never retain restore authority.
  await evaluate(`window.versionRefresh={row:${q('[data-version-preview-messages]')}.firstElementChild,button:${q(originalButton)}};void 0`);
  const paused = [];
  const stopIntercept = client.onEvent(event => { if (event.sessionId === sessionId && event.method === 'Fetch.requestPaused') paused.push(event.params.requestId); });
  try {
    await send('Fetch.enable', {patterns:[{urlPattern:'*/runtime/versions-list', requestStage:'Request'}]});
    for (const failed of [true, false]) {
      await click('[data-versions-refresh]');
      for (let i=0;i<100&&!paused.length;i++) await delay(20);
      if (!paused.length) throw Error('Version refresh was not intercepted');
      await assert(`versionRefresh.row===${q('[data-version-preview-messages]')}.firstElementChild && !window.SnowVersions.selection() && ${q('[data-version-restore]')}.disabled`, 'Pending versions refresh keeps its preview but revokes selection/restore authority');
      await delay(100);
      const requestId = paused.shift();
      await send(failed ? 'Fetch.failRequest' : 'Fetch.continueRequest', failed ? {requestId,errorReason:'Failed'} : {requestId});
      await wait(`!${q('[data-versions-refresh]')}.disabled`, 'version refresh settles');
      await assert(`versionRefresh.row===${q('[data-version-preview-messages]')}.firstElementChild && versionRefresh.button===${q(originalButton)} && !window.SnowVersions.selection() && ${q('[data-version-restore]')}.disabled`, 'Failed or identical version refresh retains DOM but cannot authorize cached history');
    }
  } finally { await send('Fetch.disable'); stopIntercept(); }
  await click(originalButton);
  await wait('!!window.SnowVersions.selection()', 'explicit reinspection verifies retained version again');
  await assert(`versionRefresh.row===${q('[data-version-preview-messages]')}.firstElementChild`, 'Identical verified preview does not replace its message nodes');
  await click("[data-versions-close]");

  const sourceID = baseline.messages.find(message => message.role === "user" && message.text === source)?.id;
  if (!sourceID) throw Error("Historical source row unavailable");
  const editButton = `[data-message-id="${sourceID}"] [data-message-edit]`;
  await wait(`!!${q(editButton)} && !${q(editButton)}.disabled`, "historical source exposes real Edit and resend");
  await click(editButton);
  await wait(`${q('#live-prompt')}.value===${JSON.stringify(source)} && !${q('#live-send')}.disabled`, "read-only edit prepare returns authoritative original source");
  check(await callCount() === 3, "Historical edit prepare does not execute a provider");
  await replace(edited); await click("#live-send"); await waitCall(4);
  check(await readFile(join(directory,"call-4"),"utf8") === edited, "Unique cross-worker provider record contains exact edited source");
  await wait(`${q('#live-session')}.dataset.instance!==${JSON.stringify(baseline.instance_id)}`, "historical edit replaces worker in same durable session");
  const editing = await snapshot();
  check(editing.session_id === baseline.session_id && editing.instance_id !== baseline.instance_id, "Explicit historical edit forks history within same session with fresh runtime identity");
  await gate(4,"second"); await gate(4,"done");
  await wait(`${q('#live-status')}.textContent==='Ready' && ${q('#live-connection')}.textContent==='Live' && !${q('#live-send')}.disabled`, "real edited turn completes before Versions restore");
  if (await evaluate(`${q('#live-edit-notice')} && !${q('#live-edit-notice')}.hidden`)) await click("[data-message-edit-cancel]");
  await click("[data-permission-policy-menu]");
  await click('[data-menu-key="deny"]');
  await wait(`${q('[data-permission-policy-label]')}.textContent==='Deny'`, "explicit non-default permission policy before restore");
  check((await snapshot()).permission_mode === "deny" && await callCount() === 4, "Native Deny selection changes current session policy without executing work");
  await replace(draft);
  // Select a suffix using real keyboard events; restoration must preserve it.
  await key("End","End",35); await key("ArrowLeft","ArrowLeft",37,1);
  const draftState = await evaluate(`({value:${q('#live-prompt')}.value,start:${q('#live-prompt')}.selectionStart,end:${q('#live-prompt')}.selectionEnd,direction:${q('#live-prompt')}.selectionDirection})`);
  const editedSnapshot = await snapshot(), beforeRestore = await durable();
  check(editedSnapshot.messages.some(message=>message.text===edited) && !editedSnapshot.messages.some(message=>message.text===source), "Active edited projection excludes old source, while durable original history remains available");
  check(beforeRestore.entries.some(entry=>entry.text===source) && beforeRestore.entries.some(entry=>entry.text===edited), "Real SQLite retains both original and edited user source branches append-only");
  const marker = join(directory,"project","history-restore-sentinel.txt");
  await writeFile(marker,"External fixture file is not conversation history.\n",{mode:0o600});
  await click('[data-session-menu]'); await click('[data-menu-key="versions"]');
  await wait(`document.querySelectorAll('[data-version-preview]').length>=2`, "real Versions exposes original and edited branches");
  await assert(`${q(originalButton)}.dataset.tipId===${JSON.stringify(original.tip)} && !${q(originalButton)}.textContent.includes(' · Current')`, "Original version remains exact saved branch/tip, not active edited history");
  await click(originalButton);
  await wait(`${q('[data-version-preview-messages]')}.textContent.includes(${JSON.stringify(source)}) && !${q('[data-version-restore]')}.disabled`, "read-only original branch preview enables explicit history restore");
  await assert(`${q('#live-transcript')}.textContent.includes(${JSON.stringify(edited)}) && !${q('[data-version-preview-messages]')}.textContent.includes(${JSON.stringify(edited)})`, "Preview stays separate: original history never replaces active edited transcript while browsing");
  check(await callCount() === 4 && posts("prompt").length === 2 && posts("message-edit-commit").length === 1, "Historical version browsing generates no prompt or extra edit/provider execution");
  await click("[data-version-restore]");
  await wait(`${q('[data-version-restore-confirmation]')}.hidden===false && !${q('[data-version-restore-confirm]')}.disabled`, "real restore prepare produces explicit confirmation");
  await assert(`${q('[data-version-restore-confirmation]')}.textContent.includes('No prompt is replayed') && ${q('[data-version-restore-confirmation]')}.textContent.includes('Your unsent draft is kept')`, "Confirmation explicitly promises history-only restore, no generation and preserved draft");
  await click("[data-version-restore-cancel]");
  check((await snapshot()).instance_id === editedSnapshot.instance_id && (await durable()).branch_tip === beforeRestore.branch_tip && await callCount() === 4 && posts("version-restore-commit").length === 0, "Cancel restore retires preparation without changing worker/history or running a provider");
  await click("[data-version-restore]");
  await wait(`!${q('[data-version-restore-confirm]')}.disabled`, "fresh explicit preparation after cancel");
  await click("[data-version-restore-confirm]");
  await wait(`${q('#live-session')}.dataset.instance!==${JSON.stringify(editedSnapshot.instance_id)} && ${q('#live-status')}.textContent==='Ready' && ${q('#live-connection')}.textContent==='Live'`, "real version restore rejoins fresh idle worker");
  const restored = await snapshot(), restoredDurable = await durable();
  check(restored.session_id === baseline.session_id && restored.instance_id !== editedSnapshot.instance_id && restored.instance_id !== baseline.instance_id && restored.status === "idle", "Restore keeps exact durable session but rotates runtime identity and remains idle");
  check(JSON.stringify(settings(restored)) === JSON.stringify(settings(editedSnapshot)), "History restore preserves provider/model, collaboration mode, permission policy and thinking settings");
  check(restored.messages.some(message=>message.text===source) && !restored.messages.some(message=>message.text===edited), "Authoritative restored conversation shows original branch, not edited branch or preview overlay");
  check(restoredDurable.branch_tip === original.tip && JSON.stringify(restoredDurable.entries) === JSON.stringify(beforeRestore.entries), "SQLite selects original branch tip without deleting, rewriting or appending conversation entries");
  check(await readFile(marker,"utf8") === "External fixture file is not conversation history.\n", "History restore leaves external fixture file unchanged; no filesystem undo");
  const restoredDraft = await evaluate(`({value:${q('#live-prompt')}.value,start:${q('#live-prompt')}.selectionStart,end:${q('#live-prompt')}.selectionEnd,direction:${q('#live-prompt')}.selectionDirection})`);
  check(JSON.stringify(restoredDraft) === JSON.stringify(draftState), "Native restore preserves unsent composer draft and selection exactly");
  await assert(`!${q('#versions-dialog')}.open && ${q('[data-version-restore-confirmation]')}.hidden && ${q('[data-version-restore-confirm]')}.disabled`, "Successful restore closes old dialog and retires stale confirmation authority");
  check(!restored.cancel_token && !restored.permission && !restored.input && !restored.queue?.items?.length, "Fresh idle restored worker exposes no stale Stop/approval/input/queue controls");
  const preparePosts=posts("version-restore-prepare"), commitPosts=posts("version-restore-commit");
  check(preparePosts.length === 2 && commitPosts.length === 1 && [...commitPosts[0].keys].sort().join(',') === ['csrf','instance_id','session_id','restore_token'].sort().join(','), "Exactly two prepares and one explicit restore commit; commit sends typed authority only, never prompt text");
  await delay(180);
  check(await callCount() === 4 && posts("prompt").length === 2, "Globally unique cross-worker audit proves restore never starts a provider turn or replays a prompt");
  await click('[data-session-menu]'); await click('[data-menu-key="versions"]');
  await wait(`!!${q(originalButton)} && ${q(originalButton)}.textContent.includes(' · Current')`, "fresh Versions list labels restored branch Current");
  await assert(`${q('[data-version-preview-messages]')}.textContent==='' && ${q('[data-version-restore-confirm]')}.disabled`, "Reopening Versions starts clean with no stale preview or reusable confirmation");
  await assert('document.documentElement.scrollWidth<=innerWidth+1', `Real Versions modal remains within viewport at ${width}px`);
  await click("[data-versions-close]");
  await assert("managerWorkflowErrors.length===0", "Real historical edit and Versions workflow produce no JavaScript errors before reload");
  const actions = new Set(['queue-remove','versions-list','version-preview','message-edit-prepare','message-edit-commit','permission-mode','version-restore-prepare','version-restore-commit']);
  check(requests.filter(request=>request.method==='POST').every(request=>actions.has(request.path.split('/').at(-1)) || /\/(open|prompt|queue-enqueue|cancel)$/.test(request.path) || /^\/projects\/[^/]+\/organization\/(rename|pin|unpin|archive|restore)$/.test(request.path)), "Full production workflow uses only its explicit typed actions; no hidden regenerate/switch/restart fallback");
  await send("Page.reload",{ignoreCache:true});
  await wait(`${q('#live-connection')}?.textContent==='Live' && ${q('#live-status')}?.textContent==='Ready'`, "reload of restored history stays live and idle");
  check((await snapshot()).instance_id === restored.instance_id && (await durable()).branch_tip === original.tip && await callCount() === 4 && posts("version-restore-commit").length === 1, "Reload only reads restored branch: no worker replacement, duplicate commit or provider autoplay");
}
