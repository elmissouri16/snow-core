import {writeFile, readdir, mkdir} from "node:fs/promises";
import {join} from "node:path";
import {setTimeout as delay} from "node:timers/promises";

export async function exercise({client, sessionId, ready, directory, artifacts, pauseManager, resumeManager}) {
  let passed = 0;
  const requests = [], snapshots = [], responses = [];
  const streamBuffers = new Map();
  function streamData(id, encoded) {
    let buffer = (streamBuffers.get(id) || "") + Buffer.from(encoded || "", "base64").toString("utf8");
    let end;
    while ((end = buffer.indexOf("\n\n")) >= 0) {
      const frame = buffer.slice(0, end); buffer = buffer.slice(end + 2);
      if (frame.startsWith("event: snapshot\n")) {
        const data = frame.split("\n").filter(line=>line.startsWith("data:")).map(line=>line.slice(5).trimStart()).join("\n");
        try { snapshots.push(JSON.parse(data)); } catch { /* assertions fail if missing */ }
      }
    }
    if (buffer.length > 1<<20) throw Error("Fixture observation buffer exceeded");
    streamBuffers.set(id, buffer);
  }
  const dispose = client.onEvent(event => {
    if (event.sessionId !== sessionId) return;
    if (event.method === "Network.requestWillBeSent") {
      const {method, url} = event.params.request;
      if (url.startsWith(ready.origin)) requests.push({method, path: new URL(url).pathname, query: new URL(url).search});
    }
    if (event.method === "Network.responseReceived" && event.params.response.url.startsWith(ready.origin)) {
      const path = new URL(event.params.response.url).pathname;
      if (path.startsWith("/projects/")) responses.push({path, status: event.params.response.status});
    }
    if (event.method === "Network.responseReceived" && new URL(event.params.response.url).pathname.endsWith("/events")) {
      const id = event.params.requestId;
      streamBuffers.set(id, "");
      client.send("Network.streamResourceContent", {requestId: id}, sessionId).then(result=>streamData(id, result.bufferedData)).catch(()=>{});
    }
    if (event.method === "Network.dataReceived" && streamBuffers.has(event.params.requestId) && event.params.data) streamData(event.params.requestId, event.params.data);
  });
  const evaluate = async expression => {
    const result = await client.send("Runtime.evaluate", {expression, awaitPromise: true, returnByValue: true}, sessionId);
    if (result.exceptionDetails) throw new Error(`Browser evaluation failed: ${result.exceptionDetails.text}`);
    return result.result?.value;
  };
  const wait = async (expression, label, timeout = 12000) => {
    const end = Date.now() + timeout;
    while (Date.now() < end) { if (await evaluate(expression)) return; await delay(35); }
    throw new Error(`Timed out: ${label}; state=${await evaluate('JSON.stringify({status:document.querySelector("#live-status")?.textContent,connection:document.querySelector("#live-connection")?.textContent,error:document.querySelector("#live-error")?.textContent,activation:document.querySelector("[data-action-error]")?.textContent,coldSession:document.querySelector("[data-runtime-open] input[name=session_id]")?.value,sources:document.querySelectorAll(".message-source").length,coldMounted:document.querySelector("[data-react-page=workspace-cold]")?.dataset.reactMounted,alert:document.querySelector("[data-react-page=workspace-cold] [role=alert]")?.textContent})')}`);
  };
  const assert = (condition, label) => { if (!condition) throw new Error(label); passed++; };
  const click = selector => evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`);
  const snapshot = () => evaluate(`fetch(${JSON.stringify(`/projects/${ready.project}/runtime`)}).then(r => {if (!r.ok) throw Error('snapshot unavailable'); return r.json()})`);
  const gate = (call, phase) => writeFile(join(directory, `release-${call}-${phase}`), "release", {mode: 0o600});
  const callCount = async () => (await readdir(directory)).filter(name => /^call-\d+$/.test(name)).length;
  const postCount = () => requests.filter(r => r.method === "POST" && r.path.endsWith("/prompt")).length;
  const projectRow = `[data-sidebar-project="${ready.project}"]`;
  const sessionLink = id => `${projectRow} [data-shell-session="${id}"] [data-shell-session-open]`;
  const settledNavigation = async (action, expression, label) => {
    await evaluate("window.workspaceFlowSettled=false; document.addEventListener('snow:navigation-end',()=>window.workspaceFlowSettled=true,{once:true})");
    await action();
    await wait(`(${expression}) && window.workspaceFlowSettled`, label);
  };
  const shot = async name => { const {data} = await client.send("Page.captureScreenshot", {format: "png"}, sessionId); await writeFile(join(artifacts, `${name}.png`), Buffer.from(data, "base64")); };
  const renameSession = async name => {
    await click('[data-session-menu]');
    await click('[data-menu-key="rename"]');
    await wait('document.querySelector("#workflow-rename-dialog")?.open', 'rename draft-only session');
    await evaluate("document.querySelector('#workflow-name').focus(); document.querySelector('#workflow-name').select()");
    await client.send('Input.insertText', {text: name}, sessionId);
    await click('[data-workflow-rename-form] button[type="submit"]');
    await wait(`!document.querySelector('#workflow-rename-dialog')?.open && document.querySelector('[data-live-title]')?.textContent === ${JSON.stringify(name)}`, 'session has a durable explicit name');
  };
  const prompt = async text => {
    await wait('document.querySelector("#live-status")?.textContent === "Ready" && !document.querySelector("#live-send")?.disabled', "composer ready");
    await evaluate(`document.querySelector('#live-prompt').value=${JSON.stringify(text)}; document.querySelector('#live-prompt').dispatchEvent(new Event('input',{bubbles:true})); document.querySelector('#live-composer').requestSubmit()`);
  };
  try {
    await client.send("Page.navigate", {url: `${ready.origin}/`}, sessionId);
    await wait('document.querySelector("#home-prompt")?.disabled === false', "working home composer");
    const startupDraft = "A private startup draft <not markup>";
    await evaluate(`window.homeLifetimeMarker = 1; document.querySelector('#home-prompt').value=${JSON.stringify(startupDraft)}; document.querySelector('#home-prompt').dispatchEvent(new Event('input',{bubbles:true})); document.querySelector('#home-composer').requestSubmit()`);
    await wait('!!document.querySelector(".shell-workspace-menu")', "Continue without workspace opens picker");
    const addedPath = join(directory, 'home-added-project'); await mkdir(addedPath);
    await evaluate("window.homeNavSettled=false; document.addEventListener('snow:navigation-end',()=>window.homeNavSettled=true,{once:true})");
    await click('.shell-workspace-menu .picker-add');
    await wait('!!document.querySelector("#add-project-form") && window.homeNavSettled', 'Add workspace navigation settled');
    assert(await evaluate("window.homeLifetimeMarker === 1"), 'Add workspace link preserves document');
    await settledNavigation(() => evaluate(`document.querySelector('#project-path').value=${JSON.stringify(addedPath)}; document.querySelector('#project-name').value='Home draft workspace'; document.querySelector('#add-project-form').requestSubmit()`), '!!document.querySelector("[data-runtime-open]")', 'registered project activation review settled');
    assert(await evaluate("new URL(location.href).searchParams.get('new') === '1' && document.querySelector('#workspace-prompt')?.dataset.draftProject === document.querySelector('#workspace').dataset.project && document.querySelector('#workspace-prompt').dataset.draftSession === '' && !document.querySelector('#workspace-prompt').hasAttribute('name')"), 'registration opens an explicit empty session with an unnamed, workspace-bound draft');
    assert(await evaluate(`window.homeLifetimeMarker === 1 && document.querySelector('#workspace-prompt')?.value === ${JSON.stringify(startupDraft)} && !document.querySelector('#workspace-prompt').disabled`), 'registration retains the editable cold draft without a full reload');
    await settledNavigation(() => click('#project-navigation a.brand'), 'document.querySelector("#home-prompt")?.disabled === false', 'return to editable home draft');
    await click('.workspace-picker > summary');
    await wait('!!document.querySelector(".shell-workspace-menu")', 'workspace picker after registration');
    await click(`.shell-workspace-menu [data-home-project="${ready.project}"]`);
    assert(await evaluate(`document.querySelector('#home-prompt')?.value === ${JSON.stringify(startupDraft)} && !document.querySelector('.shell-workspace-menu')`), "workspace selection retains editable draft on home");
    assert(requests.filter(r => r.method === "POST" && r.path.endsWith('/runtime/open')).length === 0 && await callCount() === 0, "drafting and selecting never activate a worker");
    await evaluate("document.querySelector('#home-composer').requestSubmit()");
    await wait('!!document.querySelector("[data-runtime-open]")', "production activation page");
    assert(await evaluate(`document.querySelector('#workspace-prompt')?.value === ${JSON.stringify(startupDraft)} && !document.querySelector('#workspace-prompt').disabled`), "activation review keeps draft editable as plain text in the conversation center");
    assert(await callCount() === 0, "browsing must not call a provider");
    assert(await evaluate(`document.querySelector('#workspace-prompt')?.dataset.draftProject === ${JSON.stringify(ready.project)} && document.querySelector('#workspace-prompt').dataset.draftSession === '' && !document.querySelector('[data-runtime-open] input[name=session_id]')`), "cold workspace without history opens the empty center, not a session catalog");
    await evaluate("window.workspaceFlowCenter=document.querySelector('#workspace-content'); window.workspaceFlowURL=location.href");
    const disclosures = await evaluate("Array.from(document.querySelectorAll('[data-sidebar-project] [data-workspace-toggle]'), node=>({project:node.closest('[data-sidebar-project]').dataset.sidebarProject,expanded:node.getAttribute('aria-expanded')}))");
    assert(disclosures.length === 2, "both registered workspaces own independent disclosure controls");
    for (const {project, expanded} of disclosures) {
      const selector = `[data-sidebar-project="${project}"] [data-workspace-toggle]`;
      if (expanded === "true") await click(selector);
      await click(selector);
    }
    await wait("Array.from(document.querySelectorAll('[data-sidebar-project]')).every(row=>row.querySelector('[data-workspace-toggle]')?.getAttribute('aria-expanded') === 'true' && !row.querySelector('.session-tree[data-workspace-sessions]')?.hidden)", "independently expanded workspace groups");
    assert(await evaluate("window.workspaceFlowCenter === document.querySelector('#workspace-content') && window.workspaceFlowURL === location.href"), "disclosure preserves the center and navigation URL");
    assert(requests.some(r => r.path === `/projects/${ready.project}/sidebar-sessions`) && !requests.some(r => /\/(?:models|choices)$/.test(r.path)) && requests.filter(r => r.method === "POST" && r.path.endsWith('/runtime/open')).length === 0, "workspace disclosure reads session metadata without model discovery or runtime activation");
    await evaluate('document.querySelector("[data-runtime-open] input[name=confirm]").checked=true; document.querySelector("[data-runtime-open]").requestSubmit()');
    await wait('document.querySelector("#live-status")?.textContent === "Ready" && document.querySelector("#live-connection")?.textContent === "Live"', "real worker activation", 20000);
    assert(await evaluate(`document.querySelector('#live-prompt')?.value === ${JSON.stringify(startupDraft)} && window.homeLifetimeMarker === 1 && !document.querySelector('#home-draft-notice')`), "activation transfers startup draft without reloading or duplicating it");
    assert(postCount() === 0 && await callCount() === 0, "startup draft is never auto-sent");
    await evaluate("window.SnowNavigation.visit('/',{history:'none'})");
    await wait('document.querySelector("#home-prompt")?.disabled === false', 'home from live conversation');
    await evaluate("document.querySelector('#home-prompt').value='Second startup draft'; document.querySelector('#home-prompt').dispatchEvent(new Event('input',{bubbles:true})); document.querySelector('#home-composer').requestSubmit()");
    await click(`.shell-workspace-menu [data-home-project="${ready.project}"]`);
    await evaluate("document.querySelector('#home-composer').requestSubmit()");
    await wait('!!document.querySelector("[data-home-draft-use]")', 'existing draft conflict notice');
    assert(await evaluate(`document.querySelector('#live-prompt').value === ${JSON.stringify(startupDraft)} && document.querySelector('[data-home-draft-use]').disabled`), 'startup flow does not overwrite an existing conversation draft');
    // A pending home draft belongs to the first session, not whichever empty
    // session happens to become active next. Sidebar switching shares admission.
    const firstSession = (await snapshot()).session_id;
    // Empty unnamed sessions are deliberately removed on close. Give this
    // draft-only session a durable name without sending a provider prompt.
    await renameSession('First workspace session');
    await evaluate("window.workspaceFlowCenter=document.querySelector('#workspace-content'); window.workspaceFlowSidebar=document.querySelector('#project-navigation'); void 0");
    assert(await evaluate(`new URL(document.querySelector(${JSON.stringify(projectRow + ' [data-shell-project-new]')}).href).searchParams.get('new') === '1'`), "workspace New retains its explicit-new fallback URL");
    await click(`${projectRow} [data-shell-project-new]`);
    await wait(`document.querySelector('#live-session')?.dataset.session && document.querySelector('#live-session').dataset.session !== ${JSON.stringify(firstSession)} && document.querySelector('#live-status')?.textContent === 'Ready'`, "New opens a second session in the active workspace");
    const secondSession = (await snapshot()).session_id;
    assert(await evaluate("document.querySelector('#live-prompt').value === '' && window.workspaceFlowCenter === document.querySelector('#workspace-content') && window.workspaceFlowSidebar === document.querySelector('#project-navigation')"), "New retains the center/sidebar without transferring the first session's pending startup draft");
    assert(postCount() === 0 && await callCount() === 0, "creating a second session never auto-sends a pending draft");
    await renameSession('Second workspace session');
    const secondDraft = "A private second-session draft";
    await evaluate(`document.querySelector('#live-prompt').value=${JSON.stringify(secondDraft)}; document.querySelector('#live-prompt').dispatchEvent(new Event('input',{bubbles:true}))`);
    const inventory = await evaluate(`fetch('/projects/${ready.project}/sidebar-sessions').then(r=>{if (!r.ok) throw Error('sidebar inventory unavailable'); return r.json()})`);
    assert(inventory.project_id === ready.project && inventory.available && inventory.instance_id === (await snapshot()).instance_id && !('models' in inventory) && inventory.sessions.length <= 25 && inventory.sessions.every(item=>Object.keys(item).every(key=>['session_id','name','updated_at'].includes(key))), "browser-authenticated live inventory is bounded session-only metadata bound to the current runtime");
    assert(inventory.sessions.some(item=>item.session_id === firstSession) && inventory.sessions.some(item=>item.session_id === secondSession), "authoritative live inventory contains both created sessions");
    await wait(`!!document.querySelector(${JSON.stringify(sessionLink(firstSession))}) && !!document.querySelector(${JSON.stringify(sessionLink(secondSession))})`, "live workspace inventory contains both sessions");
    const switchesBefore = requests.filter(r => r.method === "POST" && r.path.endsWith('/switch')).length;
    await click(sessionLink(firstSession));
    await wait(`document.querySelector('#live-session')?.dataset.session === ${JSON.stringify(firstSession)} && document.querySelector('#live-status')?.textContent === 'Ready'`, "sidebar reopens the first session through the live owner");
    assert(requests.filter(r => r.method === "POST" && r.path.endsWith('/switch')).length === switchesBefore + 1 && await evaluate("window.workspaceFlowCenter === document.querySelector('#workspace-content') && window.workspaceFlowSidebar === document.querySelector('#project-navigation')"), "old session link performs one guarded runtime switch without replacing the conversation shell");
    assert(await evaluate(`document.querySelector('#live-prompt').value === ${JSON.stringify(startupDraft)}`), "returning to the first session restores its own draft");
    assert(!requests.some(r => /\/(?:models|choices)$/.test(r.path)), "sidebar New and saved-session selection do not load model choices");
    // Exercise a real cross-workspace intent: the destination is already live
    // on session two while its requested saved session is session one. Mount
    // that current owner first, then deliberately switch; never cold-resume it.
    await click(sessionLink(secondSession));
    await wait(`document.querySelector('#live-session')?.dataset.session === ${JSON.stringify(secondSession)} && document.querySelector('#live-status')?.textContent === 'Ready'`, 'return to second session before cross-workspace navigation');
    assert(await evaluate(`document.querySelector('#live-prompt').value === ${JSON.stringify(secondDraft)}`), 'each live session restores its own draft before crossing workspaces');
    const otherProject = disclosures.find(item=>item.project !== ready.project).project;
    const opensBeforeCross = requests.filter(r=>r.method === 'POST' && r.path.endsWith('/runtime/open')).length;
    await settledNavigation(() => click(`[data-sidebar-project="${otherProject}"] .project-link`), `document.querySelector('#workspace')?.dataset.project === ${JSON.stringify(otherProject)} && !!document.querySelector('[data-runtime-open]')`, 'browse the other cold workspace while the first worker remains live');
    assert(await evaluate(`!document.querySelector('#live-session[data-runtime=true]') && document.querySelector('#workspace-prompt')?.dataset.draftProject === ${JSON.stringify(otherProject)} && document.querySelector('#workspace-prompt').value === '' && document.querySelector('#workspace-prompt').disabled`) && (await snapshot()).session_id === secondSession, 'cold workspace does not steal the pending draft or change the remote live owner');
    await wait(`!!document.querySelector(${JSON.stringify(sessionLink(firstSession))}) && document.querySelector(${JSON.stringify(projectRow + ' [data-workspace-toggle]')})?.getAttribute('aria-expanded') === 'true'`, 'non-current saved session remains selectable under the other live workspace');
    const crossSwitchesBefore = requests.filter(r=>r.method === 'POST' && r.path.endsWith('/switch')).length;
    await evaluate("window.crossWorkspaceMount=null; document.addEventListener('snow:navigation-after-swap',()=>{const live=document.querySelector('#live-session[data-runtime=true]'); window.crossWorkspaceMount=live && {project:live.dataset.project,session:live.dataset.session,draft:document.querySelector('#live-prompt')?.value};},{once:true})");
    await settledNavigation(() => click(sessionLink(firstSession)), `document.querySelector('#live-session')?.dataset.session === ${JSON.stringify(firstSession)} && document.querySelector('#live-status')?.textContent === 'Ready'`, 'cross-workspace saved link mounts its owner then switches to the requested session');
    assert(await evaluate(`window.crossWorkspaceMount?.project === ${JSON.stringify(ready.project)} && window.crossWorkspaceMount.session === ${JSON.stringify(secondSession)} && window.crossWorkspaceMount.draft === ${JSON.stringify(secondDraft)} && window.homeLifetimeMarker === 1`), 'native navigation first mounts the existing second-session owner and restores its draft without a document reload');
    assert(requests.filter(r=>r.method === 'POST' && r.path.endsWith('/switch')).length === crossSwitchesBefore + 1 && requests.filter(r=>r.method === 'POST' && r.path.endsWith('/runtime/open')).length === opensBeforeCross, 'cross-workspace selection performs exactly one deliberate switch and no new activation');
    assert(await evaluate(`document.querySelector('#live-prompt').value === ${JSON.stringify(startupDraft)} && document.querySelector('#home-draft-notice')?.textContent.includes('Second startup draft') && document.querySelector('[data-home-draft-use]').disabled`), 'cross-workspace return preserves the first session draft and pending startup conflict');
    assert(postCount() === 0 && await callCount() === 0 && !requests.some(r=>/\/(?:models|choices)$/.test(r.path)), 'cross-workspace reads and owner switching never send drafts, call the provider, or discover models');
    await evaluate("document.querySelector('#live-prompt').value=''; document.querySelector('#live-prompt').dispatchEvent(new Event('input',{bubbles:true}))");
    await click('[data-home-draft-use]');
    assert(await evaluate("document.querySelector('#live-prompt').value === 'Second startup draft' && !document.querySelector('#home-draft-notice')"), 'explicit Use draft transfers only after the existing draft is cleared');
    assert(postCount() === 0 && await callCount() === 0, 'draft conflict resolution never sends a prompt');
    assert(requests.every(r => !/[?&](?:csrf|text|confirm|enable_skills)=/.test(r.query)), "navigation never puts draft or activation form fields in URLs");
    const initial = await snapshot();
    assert(initial.provider === "fake" && initial.model === "fake-1", "fixture must select only the fake provider");
    assert(await callCount() === 0, "activation must not send a provider request");
    await prompt("stream an incremental answer");
    await wait('document.querySelector("#live-transcript")?.textContent.includes("First chunk is visible.")', "first chunk before release");
    let state = await snapshot();
    assert(state.status === "running" && await evaluate('!document.querySelector("#live-turn-status").hidden && document.querySelector("#live-turn-status").textContent === "Working…"'), "first chunk and public turn-level signal must arrive while still running");
    assert(state.messages.at(-1).text === ready.prefix, "first cumulative snapshot must exactly match provider prefix");
    assert(!await evaluate('document.querySelector("#live-transcript").textContent.includes("second chunk")'), "second chunk must remain gated");
    assert(await evaluate('!!document.querySelector("#live-transcript h1") && !!document.querySelector("#live-transcript pre code")'), "split Markdown must render a heading and open code fence");
    assert(await evaluate('document.querySelector("#live-session").dataset.turnCancel === "true"'), "real manager advertises token-bound cancellation");
    assert(await evaluate('!document.querySelector("#live-composer [data-runtime-abort]").disabled'), "Stop must be enabled during the gated turn");
    await shot("01-prefix-running");
    await gate(1, "second");
    await wait('document.querySelector("#live-transcript")?.textContent.includes("second chunk")', "second chunk before done");
    state = await snapshot();
    assert(state.status === "running" && state.messages.at(-1).text === ready.complete, "second snapshot must be cumulative and still running");
    assert(await evaluate('document.querySelector("#live-transcript pre code")?.textContent.includes(\'fmt.Println("second chunk")\')'), "split code token must join without duplication");
    await shot("02-second-running");
    await gate(1, "done");
    await wait('document.querySelector("#live-status")?.textContent === "Ready"', "explicit provider completion");
    assert((await snapshot()).messages.at(-1).text === ready.complete && await evaluate('document.querySelector("#live-turn-status").hidden'), "completion retains the exact answer and removes its working signal");
    assert(snapshots.some(s => s.status === "running" && s.messages?.at(-1)?.text === ready.prefix), "real SSE must deliver prefix snapshot before completion");
    assert(snapshots.some(s => s.status === "running" && s.messages?.at(-1)?.text === ready.complete), "real SSE must deliver second snapshot before completion");

    await prompt("stop this partial answer");
    await wait(`document.querySelectorAll('#live-transcript [data-message-role="assistant"]').length === 2 && document.querySelector('#live-status')?.textContent === 'Working'`, "second gated prompt");
    await wait(`fetch('/projects/${ready.project}/runtime').then(r=>r.json()).then(s=>s.messages.at(-1).text===${JSON.stringify(ready.prefix)})`, "second prefix");
    await click("#live-composer [data-runtime-abort]");
    await wait('document.querySelector("#live-status")?.textContent === "Ready"', "Stop cancels a context-blocked provider");
    state = await snapshot();
    assert(state.messages.at(-1).text === ready.prefix, "Stop must retain partial text");
    assert(await callCount() === 2 && postCount() === 2, "Stop must not replay POST or restart provider");
    await shot("03-stopped-partial");

    await prompt("reconnect without replay");
    await wait(`fetch('/projects/${ready.project}/runtime').then(r=>r.json()).then(s=>s.status==='running'&&s.messages.filter(m=>m.role==='assistant').length===3&&s.messages.at(-1).text===${JSON.stringify(ready.prefix)})`, "third gated prefix");
    // A direct snapshot GET can beat the post-prompt SSE replacement. Wait for
    // that subscription's rendered state before measuring a watchdog reconnect.
    await wait('document.querySelector("#live-connection")?.textContent === "Live" && document.querySelectorAll("#live-transcript [data-message-role=assistant]").length === 3', "third prompt reconciled through live subscription");
    const eventConnections = requests.filter(r => r.path.endsWith("/events")).length;
    // Suspend only our isolated HTTP manager. This stalls real bytes (including
    // heartbeat frames), letting the unchanged production watchdog abort the
    // connection. Chrome's offline emulation does not reliably cut localhost
    // streaming responses that were already open.
    pauseManager();
    await wait('document.querySelector("#live-connection")?.textContent !== "Live"', "actual browser transport disconnect", 35000);
    assert(await callCount() === 3, "disconnect must not start another provider request");
    resumeManager();
    await wait('document.querySelector("#live-connection")?.textContent === "Live"', "production SSE reconnect", 18000);
    assert(requests.filter(r => r.path.endsWith("/events")).length > eventConnections, "recovery must open another real SSE request");
    assert(await callCount() === 3 && postCount() === 3, "reconnect must not replay a prompt POST");
    await gate(3, "second");
    await wait('Array.from(document.querySelectorAll("#live-transcript [data-message-role=assistant]")).at(-1)?.textContent.includes("second chunk")', "post-reconnect incremental delivery");
    assert((await snapshot()).status === "running", "reconnected stream must remain gated until done");
    await gate(3, "done");
    await wait('document.querySelector("#live-status")?.textContent === "Ready"', "reconnected completion");

    await prompt("ask a question");
    await wait('document.querySelector("#live-status")?.textContent === "Input needed" && !!document.querySelector("[data-runtime-input]")', "real ask_user RPC question");
    state = await snapshot();
    assert(state.input.questions[0].id === "color", "pending input must come from the actual ask_user broker");
    assert(state.activities.some(a => a.tool === "ask_user" && a.status === "running"), "public tool timeline must show an actually gated tool");
    await shot("04-authoritative-question");
    await click('[data-runtime-input] input[value="blue"]');
    await evaluate('document.querySelector("[data-runtime-input]").requestSubmit()');
    await wait('document.querySelector("#live-status")?.textContent === "Ready" && document.querySelector("#live-transcript")?.textContent.includes("Received authoritative answer: blue.")', "answer traverses HTTP and real RPC broker");
    state = await snapshot();
    assert(state.activities.some(a => a.tool === "ask_user" && a.status === "completed" && a.output.includes("blue")), "public tool result must carry the actual answer");
    const publicToolOutput = state.activities.find(a => a.tool === "ask_user" && a.status === "completed").output;
    assert(state.recovery?.state === "completed", "definitive completion must be reflected in recovery evidence");
    assert(state.input === null, "answered input must clear the pending request");
    assert(await callCount() === 5 && postCount() === 4, "tool continuation is a provider step, not a replayed browser POST");
    await shot("05-completed-tool");

    // Reproduce the observed terminal state by canceling a provider blocked
    // before its first delta. This does not establish the live user's cancel source.
    const cancelWithoutText = async count => {
      const cancelPosts = requests.filter(r => r.method === "POST" && r.path.endsWith("/cancel")).length;
      await prompt("cancel before text");
      await wait('document.querySelector("#live-status")?.textContent === "Working" && !document.querySelector("#live-composer [data-runtime-abort]").disabled', "provider blocked before first text");
      // Runtime dialogs have their own exact-run Stop buttons. Native input
      // must target the visible composer owner, never a closed modal's zero rect.
      const stopPoint = await evaluate('(() => { const el=document.querySelector("#live-composer [data-runtime-abort]"), r=el.getBoundingClientRect(), x=r.x+r.width/2,y=r.y+r.height/2; return r.width>0&&r.height>0&&el.contains(document.elementFromPoint(x,y)) ? {x,y} : null; })()');
      assert(!!stopPoint, "visible composer Stop must have a reachable native hit target");
      for (const type of ["mousePressed", "mouseReleased"]) await client.send("Input.dispatchMouseEvent", {type, ...stopPoint, button: "left", clickCount: 1}, sessionId);
      await wait(`fetch('/projects/${ready.project}/runtime').then(r=>r.json()).then(s=>s.status==='idle'&&s.recovery?.state==='canceled')`, "empty canceled turn completion");
      await wait('document.querySelector("#live-connection")?.textContent === "Live" && !document.querySelector("#live-send").disabled', "cancellation reconciled");
      assert((await snapshot()).messages.at(-1).role === "user", "empty canceled turn must have no manufactured assistant reply");
      assert(await callCount() === count, "empty canceled turn must not retry its provider request");
      assert(requests.filter(r => r.method === "POST" && r.path.endsWith("/cancel")).length === cancelPosts + 1, "each controlled cancellation must send exactly one Stop request");
      assert(await evaluate('!document.querySelector("#live-turn-outcome").hidden && document.querySelector("#live-turn-outcome").textContent.includes("canceled")'), "empty canceled turn must display an explicit outcome");
    };
    await cancelWithoutText(6);
    await shot("08-empty-canceled");
    await wait('!document.querySelector("#live-send").disabled', "next prompt is enabled");
    await evaluate('document.querySelector("#live-prompt").value="continue after cancellation"');
    const sendPoint = await evaluate('(() => { const r=document.querySelector("#live-send").getBoundingClientRect(); return {x:r.x+r.width/2,y:r.y+r.height/2}; })()');
    const pointerClick = async count => {
      for (const type of ["mousePressed", "mouseReleased"]) await client.send("Input.dispatchMouseEvent", {type, ...sendPoint, button: "left", clickCount: count}, sessionId);
    };
    await pointerClick(1);
    await wait(`fetch('/projects/${ready.project}/runtime').then(r=>r.json()).then(s=>s.status==='running'&&s.messages.at(-1).text===${JSON.stringify(ready.prefix)})`, "explicit next turn streams normally");
    await wait('!document.querySelector("#live-composer [data-runtime-abort]").disabled', "Stop replaces Send");
    // The working footer may move Stop above the idle Send coordinates. Keep
    // the real repeated-pointer check, then exercise Stop's click-count guard
    // directly so that protection remains covered regardless of layout.
    const replacementStop = await evaluate('(() => { const el=document.querySelector("#live-composer [data-runtime-abort]"),r=el.getBoundingClientRect(),x=r.x+r.width/2,y=r.y+r.height/2; return el.contains(document.elementFromPoint(x,y)) ? {x,y} : null; })()');
    assert(!!replacementStop, "replacement Stop remains reachable");
    const cancelsBeforeDoubleClick = requests.filter(r => r.method === "POST" && r.path.endsWith("/cancel")).length;
    await pointerClick(2);
    await new Promise(resolve => setTimeout(resolve, 250));
    assert((await snapshot()).status === "running" && requests.filter(r => r.method === "POST" && r.path.endsWith("/cancel")).length === cancelsBeforeDoubleClick, "double-clicking the Send location must not cancel the turn");
    for (const type of ["mousePressed", "mouseReleased"]) await client.send("Input.dispatchMouseEvent", {type, ...replacementStop, button: "left", clickCount: 2}, sessionId);
    await delay(100);
    assert((await snapshot()).status === "running" && requests.filter(r => r.method === "POST" && r.path.endsWith("/cancel")).length === cancelsBeforeDoubleClick, "replacement Stop ignores the second click even when targeted directly");
    assert(await evaluate('document.querySelector("#live-turn-outcome").hidden && !document.querySelector("#live-turn-status").hidden'), "new running turn must clear prior cancellation feedback");
    await gate(7, "second"); await gate(7, "done");
    await wait('document.querySelector("#live-status")?.textContent === "Ready" && !document.querySelector("#live-send").disabled', "next turn completes normally");
    assert((await snapshot()).messages.at(-1).text === ready.complete, "same worker must deliver the full answer after a canceled turn");
    await cancelWithoutText(8);
    // Resize only after the native multi-click scenario: changing viewport
    // geometry between clicks would no longer test the same pointer location.
    await client.send("Emulation.setDeviceMetricsOverride", {width: 320, height: 240, deviceScaleFactor: 1, mobile: false}, sessionId);
    await wait('document.querySelector("#live-send").getBoundingClientRect().bottom <= innerHeight + 1', "tiny viewport composer remains reachable after cancellation");
    assert(await evaluate('document.documentElement.scrollWidth <= innerWidth + 1 && !document.querySelector("#live-turn-outcome").hidden'), "cancellation notice must not introduce horizontal overflow at 320x240");
    await shot("09-empty-canceled-tiny");
    await client.send("Emulation.setDeviceMetricsOverride", {width: 1280, height: 900, deviceScaleFactor: 1, mobile: false}, sessionId);
    assert(postCount() === 7, "each additional turn requires exactly one explicit prompt POST");

    // Drop the worker, then review durable history in the same workspace center.
    await click("[data-runtime-close]");
    await settledNavigation(() => click("[data-runtime-close-confirm]"), '!document.querySelector("#live-session[data-runtime=true]")', "close runtime navigation settled");
    await wait(`document.querySelector('[data-runtime-open] input[name="session_id"]')?.value === ${JSON.stringify(initial.session_id)}`, "close preserves exact saved-session recovery target");
    assert(await evaluate(`document.querySelector('[data-runtime-open] input[name="session_id"]')?.value === ${JSON.stringify(initial.session_id)}`), "close must review the same session, not offer accidental new-session activation");
    await settledNavigation(() => click(`${projectRow} .project-link`), 'document.querySelectorAll(".message-source").length >= 8', "cold workspace defaults to its most recent saved conversation");
    assert(await evaluate(`document.querySelector('#workspace-prompt')?.dataset.draftSession === ${JSON.stringify(initial.session_id)} && document.querySelector('#workspace-prompt').dataset.draftProject === ${JSON.stringify(ready.project)} && !document.querySelector('#workspace-prompt').hasAttribute('name')`), "cold saved conversation owns its unnamed session-bound draft in the center");
    await evaluate("window.workspaceFlowResumeMarker=1; document.querySelector('#workspace-prompt').value='Cold resume draft'; document.querySelector('#workspace-prompt').dispatchEvent(new Event('input',{bubbles:true}))");
    const saved = await evaluate('Array.from(document.querySelectorAll("[data-message-role=assistant] .message-source"), node=>node.textContent)');
    assert(saved.includes(ready.complete), "saved final text must exactly match the cumulative live answer");
    assert(saved.includes(ready.prefix), "saved history must retain the canceled partial answer");
    assert(saved.includes("Received authoritative answer: blue."), "saved history must retain actual tool continuation");
    assert(await callCount() === 8 && postCount() === 7, "saved catalog and closure must not run or replay work");
    const savedTool = await evaluate(`(() => { const tool = document.querySelector("[data-history-tool-id]"); return tool && {id: tool.dataset.historyToolId, output: tool.querySelector(".activity-output").textContent, role: tool.closest("article").dataset.messageRole, source: tool.closest("article").querySelector(".message-source").textContent, count: document.querySelectorAll("[data-history-tool-id]").length}; })()`);
    assert(savedTool?.count === 1 && savedTool.role === "assistant" && savedTool.source === "", "saved actual tool call belongs to its empty-text assistant owner exactly once");
    assert(savedTool.output === publicToolOutput, "only the persisted explicit-public output must survive closure");
    await shot("06-saved-history");
    // A remembered trust field is hidden, not a checkbox: activation still
    // requires an explicit submit, never a synthetic change to trust state.
    await evaluate('const confirm = document.querySelector("[data-runtime-open] input[name=confirm]"); if (confirm.type === "checkbox") confirm.checked = true; document.querySelector("[data-runtime-open]").requestSubmit()');
    await wait('document.querySelector("#live-session[data-runtime=true]") && document.querySelector("#live-connection")?.textContent === "Live"', "explicitly resume original session");
    state = await snapshot();
    assert(await evaluate("window.workspaceFlowResumeMarker === 1 && !!document.querySelector('#workspace-content #live-session') && document.querySelectorAll('#project-navigation [data-sidebar-project]').length === 2 && Array.from(document.querySelectorAll('[data-workspace-toggle]')).every(button=>button.getAttribute('aria-expanded') === 'true') && document.querySelector('#live-prompt').value === 'Cold resume draft'"), "explicit cold resume retains sidebar and center and transfers only that session's reviewed draft");
    assert(state.session_id === initial.session_id && state.instance_id !== initial.instance_id, "reactivation must bind the same saved session with fresh authority");
    const resumedTools = state.messages.flatMap(message => message.tools || []);
    assert(resumedTools.length === 1 && resumedTools[0].id === savedTool.id && resumedTools[0].output === publicToolOutput && resumedTools[0].output_available, "saved tool identity and public output must survive RPC history hydration");
    assert((state.activities || []).length === 0 && state.input === null && state.permission === null, "reactivation must not restore old live activity or usable approvals");
    assert(await callCount() === 8 && postCount() === 7, "resume and public history reads must not replay prompts or execute tools");
    assert(await evaluate('!document.querySelector("#live-turn-outcome").hidden && document.querySelector("#live-turn-outcome").textContent.includes("canceled")'), "explicit resume must retain cancellation feedback from durable recovery evidence");
    await shot("07-explicit-resume");
    await click("[data-runtime-close]");
    await click("[data-runtime-close-confirm]");
    await wait('!document.querySelector("#live-session[data-runtime=true]")', "close verification worker");
    console.log(`live-stream: ${passed} assertions passed; screenshots .snow/browser/live-stream/`);
  } catch (error) {
    // Paths/statuses only: never print cookies, form bodies, or activation URLs.
    console.error("live-stream recent HTTP responses: " + JSON.stringify(responses.slice(-20)));
    console.error("live-stream sidebar state: " + JSON.stringify(await evaluate("Array.from(document.querySelectorAll('[data-sidebar-project]'), row=>({project:row.dataset.sidebarProject,rows:Array.from(row.querySelectorAll('[data-shell-session]'),node=>({session:node.dataset.shellSession,link:!!node.querySelector('[data-shell-session-open]')})),status:row.querySelector('[data-workspace-sessions]')?.textContent}))").catch(()=>null)));
    await shot("failure").catch(() => {});
    throw error;
  } finally { resumeManager(); dispose(); }
}
