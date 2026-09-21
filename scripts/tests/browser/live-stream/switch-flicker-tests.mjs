import {exercise as journey} from "./tests.mjs";
import {setTimeout as delay} from "node:timers/promises";

export async function exercise(options) {
  const {client, sessionId} = options;
  await client.send("Page.addScriptToEvaluateOnNewDocument", {source: `
    window.switchFrames = []; window.switchNewRows = []; let selection;
    document.addEventListener("snow:session-new", event => {
      const region = document.querySelector("#live-session");
      if (!region || region.dataset.project !== event.detail.project) return;
      const oldID = region.dataset.session;
      const group = Array.from(document.querySelectorAll('[data-sidebar-project]')).find(row=>row.dataset.sidebarProject === event.detail.project);
      const rows = Array.from(group.querySelectorAll('[data-shell-session]'), row=>({row,id:row.dataset.shellSession}));
      const observer = new MutationObserver(() => {
        if (region.dataset.session === oldID) return;
        observer.disconnect();
        window.switchNewRows.push(rows.every(({row,id})=>row.isConnected && row.dataset.shellSession === id && !row.hidden && !row.hasAttribute('data-shell-live-session')) && Array.from(group.querySelectorAll('[data-shell-live-session]')).filter(row=>row.dataset.shellSession === region.dataset.session).length === 1);
      });
      observer.observe(region,{attributes:true,attributeFilter:['data-session']});
      setTimeout(()=>observer.disconnect(),10000);
    });
    document.addEventListener("snow:session-select", event => {
      const region = document.querySelector("#live-session");
      selection = {project:event.detail.project, target:event.detail.session, source:region?.dataset.session, cross:region?.dataset.project !== event.detail.project};
    });
    function frame() {
      const region = document.querySelector("#live-session");
      if (selection && region) {
        window.switchFrames.push({...selection, session:region.dataset.session, projectNow:region.dataset.project,
          draft:document.querySelector("#live-prompt")?.value, connection:document.querySelector("#live-connection")?.textContent,
          opening:region.dataset.workspaceOpening, notice:!!document.querySelector("#workspace-opening"), visible:region.checkVisibility(), idle:document.querySelector("#composer-state")?.classList.contains("is-idle"),
          text:document.querySelector("#live-transcript")?.textContent, empty:!document.querySelector("#live-empty")?.hidden});
        if (region.dataset.session === selection.target && document.querySelector("#live-connection")?.dataset.connected === "true") selection = null;
      }
      requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
  `}, sessionId);
  let delayInventory = false, inventoryHeld = 0;
  await client.send("Fetch.enable", {patterns:[{urlPattern:"*/runtime/switch", requestStage:"Response"}, {urlPattern:"*/sidebar-sessions*", requestStage:"Response"}]}, sessionId);
  const unsubscribe = client.onEvent(event => {
    if (event.sessionId === sessionId && event.method === "Fetch.requestPaused") {
      const inventory = new URL(event.params.request.url).pathname.endsWith("/sidebar-sessions");
      const hold = inventory && delayInventory;
      if (hold) inventoryHeld++;
      void delay(hold ? 900 : inventory ? 0 : 120).then(() => client.send("Fetch.continueResponse", {requestId:event.params.requestId}, sessionId));
    }
  });
  const evaluate = async expression => {
    const result = await client.send("Runtime.evaluate", {expression, awaitPromise:true, returnByValue:true}, sessionId);
    if (result.exceptionDetails) throw Error("Switch frame evaluation failed");
    return result.result.value;
  };
  const wait = async expression => {
    for (let attempt=0; attempt<300; attempt++) { if (await evaluate(expression)) return; await delay(25); }
    throw Error("Switch frame verification timed out");
  };
  try {
    await journey(options);
    // Reopen the real saved history and switch away/back without another prompt.
    await wait('!!document.querySelector("[data-runtime-open]")');
    await evaluate(`document.querySelector('[data-runtime-open]').requestSubmit()`);
    await wait('document.querySelector("#live-connection")?.dataset.connected === "true"');
    await wait(`!document.querySelector('[data-sidebar-project="${options.ready.project}"] [data-workspace-sessions]')?.hasAttribute('aria-busy')`);
    await evaluate(`window.currentWorkspaceProbe = {workspace: document.querySelector('#workspace'), prompt: document.querySelector('#live-prompt'), starts: 0};
      window.currentWorkspaceStarted = () => { window.currentWorkspaceProbe.starts++; };
      document.addEventListener('snow:navigation-start', window.currentWorkspaceStarted);
      document.querySelector('[data-sidebar-project="${options.ready.project}"] .project-link').click()`);
    await delay(160);
    const currentWorkspace = await evaluate(`(() => {
      document.removeEventListener('snow:navigation-start', window.currentWorkspaceStarted);
      const result = {starts: window.currentWorkspaceProbe.starts, sameWorkspace: window.currentWorkspaceProbe.workspace === document.querySelector('#workspace'), samePrompt: window.currentWorkspaceProbe.prompt === document.querySelector('#live-prompt')};
      delete window.currentWorkspaceStarted; delete window.currentWorkspaceProbe;
      return result;
    })()`);
    if (currentWorkspace.starts || !currentWorkspace.sameWorkspace || !currentWorkspace.samePrompt) throw Error("Current workspace click remounted the live composer: " + JSON.stringify(currentWorkspace));
    const historyFrameStart = await evaluate("window.switchFrames.length");
    const historySession = await evaluate('document.querySelector("#live-session").dataset.session');
    const otherSession = await evaluate(`Array.from(document.querySelectorAll('[data-sidebar-project="${options.ready.project}"] [data-shell-session]')).find(row=>row.dataset.shellSession !== ${JSON.stringify(historySession)})?.dataset.shellSession`);
    if (!otherSession) throw Error("Missing second saved conversation for switch verification");
    for (const target of [otherSession, historySession]) {
      const heldBefore = inventoryHeld;
      delayInventory = true;
      await evaluate(`window.switchOldRow=document.querySelector('[data-sidebar-project="${options.ready.project}"] [data-shell-live-session]'); window.switchOldID=window.switchOldRow.dataset.shellSession; window.switchTargetRow=document.querySelector('[data-sidebar-project="${options.ready.project}"] [data-shell-session="${target}"]'); window.switchTargetLink=window.switchTargetRow.querySelector('[data-shell-session-open]'); window.switchTargetLink.focus()`);
      await evaluate(`document.querySelector('[data-sidebar-project="${options.ready.project}"] [data-shell-session="${target}"] [data-shell-session-open]').click()`);
      await wait(`document.querySelector('#live-session')?.dataset.session === ${JSON.stringify(target)} && document.querySelector('#live-connection')?.dataset.connected === "true"`);
      for(let attempt=0; attempt<100 && inventoryHeld === heldBefore; attempt++) await delay(5);
      if (inventoryHeld === heldBefore) throw Error("No delayed inventory read observed after switch");
      const preserved = await evaluate(`({target:window.switchTargetRow.isConnected && !window.switchTargetRow.hidden && window.switchTargetRow.hasAttribute('data-shell-live-session'), old:window.switchOldRow.isConnected && window.switchOldRow.dataset.shellSession === window.switchOldID && !window.switchOldRow.hidden && !window.switchOldRow.hasAttribute('data-shell-live-session'), focus:document.activeElement === window.switchTargetLink, busy:document.querySelector('[data-sidebar-project="${options.ready.project}"] [data-workspace-sessions]').hasAttribute('aria-busy')})`);
      if (!preserved.busy || !preserved.target || !preserved.old || !preserved.focus) throw Error("Switch must preserve clicked node, focus and previous session before inventory settles: " + JSON.stringify(preserved));
      delayInventory = false;
      await wait(`!document.querySelector('[data-sidebar-project="${options.ready.project}"] [data-workspace-sessions]')?.hasAttribute('aria-busy')`);
    }
    if (!await evaluate('document.querySelectorAll("#live-transcript [data-message-id]").length > 0 && document.querySelector("#live-empty").hidden')) throw Error("Saved target history must paint without an empty frame");
    await evaluate('window.switchAnchor=document.querySelector("#live-transcript").firstElementChild; undefined');
    await evaluate(`document.querySelector('[data-sidebar-project="${options.ready.project}"] [data-shell-session="${historySession}"] [data-shell-session-open]').click()`);
    await delay(160);
    if (!await evaluate('window.switchAnchor === document.querySelector("#live-transcript").firstElementChild')) throw Error("No-op selection replaced transcript DOM");
    const result = await client.send("Runtime.evaluate", {expression:"window.switchFrames", returnByValue:true}, sessionId);
    const frames = result.result.value;
    const intermediates = frames.filter(f => f.visible && f.cross && f.projectNow === f.project && f.session !== f.target);
    const layoutFlashes = frames.filter(f => !f.cross && f.connection === "Live" && !f.idle);
    const emptyHistory = frames.slice(historyFrameStart).filter(f => !f.cross && f.target === historySession && f.session === historySession && (!f.text.trim() || f.empty));
    const newRows = await evaluate("window.switchNewRows");
    if (!newRows.length || newRows.some(kept=>!kept)) throw Error("New session overwrote an existing sidebar row before inventory refresh");
    console.log(JSON.stringify({frames:frames.length, intermediateFrames:intermediates.length, layoutFlashes:layoutFlashes.length, emptyHistoryFrames:emptyHistory.length, preservedSwitchRows:2, preservedNewRows:newRows.length}));
    if (!frames.some(f=>f.cross) || !frames.some(f=>f.target===historySession)) throw Error("Switch frame observation did not cover the required transitions");
    if (layoutFlashes.length || emptyHistory.length) throw Error("Switch introduced an idle-layout or empty-history frame");
    if (intermediates.length) throw Error("Cross-workspace switch paints the unintended live owner before the selected session");
  } finally { unsubscribe(); }
}
