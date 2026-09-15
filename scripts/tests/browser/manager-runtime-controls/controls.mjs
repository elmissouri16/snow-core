import {readFile} from 'node:fs/promises';
import {join} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';

export async function controlsChecks(h) {
  const {q,evaluate,check,assert,wait,waitSnapshot,key,click,replace,select,snapshot,count,waitCount,waitFile,file,release,posts,capture,idle,mode,directory,width,receipts,send,responseBody,armSteerLoss} = h;
  const draft = 'Fictional composer draft — keep selection and do not send.';
  const hostPath = join(directory,'home','.snow','config.json'), projectPath = join(directory,'project-a','.snow','config.json');
  const originalHost = await readFile(hostPath), originalProject = await readFile(projectPath);
  await replace('#live-prompt',draft); await key('Home','Home',36); await key('ArrowRight','ArrowRight',39,1);
  const selection = await evaluate(`({start:${q('#live-prompt')}.selectionStart,end:${q('#live-prompt')}.selectionEnd})`);
  const reasonOpen = async (fromMenu = false) => {
    if (fromMenu) { await click('[data-session-menu]'); await click('[data-menu-key="reasoning"]'); }
    else await click('#live-composer [data-reasoning-open]');
    await wait(`${q('#reasoning-picker')}.matches(':popover-open') && !!document.querySelector('[data-reasoning-level]:not(:disabled)')`, 'authoritative Thinking dropdown inspection');
  };
  const reasonSet = async (value, picture = false) => {
    const previous = posts('reasoning-set').length;
    await reasonOpen(); const before = await snapshot();
    await assert(`${q('[data-reasoning-open]')}.textContent.includes('Thinking:') && ${q('[data-reasoning-current]')}.textContent===${JSON.stringify(before.thinking)} && ${q('[data-reasoning-open]')}.getAttribute('aria-expanded')==='true'`, 'Labelled Thinking trigger shows the authoritative current level and expanded state');
    await assert(`!${q('#reasoning-dialog')}.open && !${q('#reasoning-picker')}.querySelector('select, [data-reasoning-confirm], [data-reasoning-review], [data-reasoning-consent]') && ${q('#reasoning-picker')}.contains(document.activeElement)`, 'Thinking opens an anchored dropdown, not a modal or Preference/New Value/Apply workflow');
    check(posts('reasoning-set').length===previous, 'Opening Thinking only inspects capabilities and never updates a preference');
    if (picture) await capture('thinking-dropdown');
    await click(`[data-reasoning-level="${value}"]`);
    await wait(`!${q('#reasoning-picker')}.matches(':popover-open') && ${q('[data-reasoning-current]')}.textContent===${JSON.stringify(value)}`, 'direct selection confirmed and picker dismissed');
    await idle();
    const receipt = receipts.filter(receipt=>receipt.action==='reasoning-set')[previous];
    if (!receipt) throw Error('Thinking HTTP receipt not observed');
    // Inspect privately; never put CSRF, request bodies or scope IDs in reports.
    const {postData} = await send('Network.getRequestPostData',{requestId:receipt.requestId});
    const payload = new URLSearchParams(postData);
    const response = await responseBody(receipt.requestId);
    const confirmed = JSON.parse(response.base64Encoded ? Buffer.from(response.body,'base64').toString('utf8') : response.body);
    check(payload.get('scope')==='session' && payload.get('confirm')==='session' && payload.get('field')==='thinking' && payload.get('value')===value && payload.get('instance_id')===before.instance_id && payload.get('session_id')===before.session_id && payload.get('branch_id')===before.goal.branch_id && payload.get('tip_id')===before.goal.tip_id && payload.get('expected_revision')===String(before.revision) && payload.get('thinking')===before.thinking && ['provider','model','mode','permission_mode'].every(key=>payload.get(key)===before[key]) && ['reasoning_summary','text_verbosity'].every(key=>payload.get(key)===confirmed[key]), 'Level selection sends exact session-only identity, branch/tip/revision and unchanged response/authority facts');
    check(JSON.stringify([...payload.keys()].filter(key=>key!=='csrf').sort())===JSON.stringify(['scope','field','value','confirm','expected_revision','instance_id','session_id','branch_id','tip_id','provider','model','mode','permission_mode','thinking','reasoning_summary','text_verbosity'].sort()), 'Thinking request contains only the exact session-update allowlist, never defaults or prompt fields');
    check(posts('reasoning-set').length===previous+1 && await count()===0, 'One direct level selection posts exactly once without provider work or saved defaults');
  };
  await reasonSet('high',true);
  await assert(`document.activeElement===${q('[data-reasoning-open]')}`, 'Confirmed Thinking selection restores the labelled composer trigger');
  await mode('plan'); await reasonSet('low');
  await mode('default'); await reasonOpen(true);
  await assert(`${q('[data-reasoning-current]')}.textContent==='high' && ${q('[data-reasoning-level="high"]')}.getAttribute('aria-checked')==='true' && !document.querySelector('.conversation-task-menu')`, 'Header Thinking opens the same picker; Default keeps its independent confirmed level after Plan changes');
  await click('[data-reasoning-level="high"]');
  await assert(`!${q('#reasoning-picker')}.matches(':popover-open')`, 'Choosing the current Thinking level dismisses without a write');
  check(posts('reasoning-set').length===2, 'Reselecting the current level does not mutate');
  await reasonOpen(); await click('[data-reasoning-open]');
  await assert(`!${q('#reasoning-picker')}.matches(':popover-open') && ${q('[data-reasoning-open]')}.getAttribute('aria-expanded')==='false'`, 'Clicking the expanded Thinking trigger toggles the picker closed');
  await reasonOpen(); await key('Escape','Escape',27);
  await assert(`!${q('#reasoning-picker')}.matches(':popover-open') && document.activeElement===${q('[data-reasoning-open]')}`, 'Escape dismisses Thinking and restores composer focus');
  await reasonOpen(); await click('#live-status');
  await assert(`!${q('#reasoning-picker')}.matches(':popover-open')`, 'Outside click light-dismisses Thinking without a modal');
  await reasonOpen(); await click('[data-reasoning-advanced]');
  await assert(`${q('#reasoning-dialog')}.matches(':modal') && !${q('#reasoning-picker')}.matches(':popover-open') && ![...${q('[data-reasoning-field]')}.options].some(option=>option.value==='thinking')`, 'Explicit secondary Response settings stays separate from level choices');
  await select('[data-reasoning-field]','reasoning_summary');
  await assert(`[...${q('[data-reasoning-value]')}.options].some(option=>option.value==='detailed')`, 'Secondary reasoning-summary choices come from advertised model capabilities');
  await select('[data-reasoning-field]','text_verbosity');
  await assert(`[...${q('[data-reasoning-value]')}.options].some(option=>option.value==='high')`, 'Secondary verbosity choices come from advertised model capabilities');
  await key('Escape','Escape',27);
  await assert(`!${q('#reasoning-dialog')}.open && document.activeElement===${q('[data-reasoning-open]')}`, 'Secondary response-settings Escape restores the Thinking trigger');
  check(await count()===0 && posts('reasoning-set').length===2 && posts('reasoning-set').every(post=>post.scope==='session'), 'Exactly two explicit session-only updates; no provider work or persisted-default fallback');
  check((await readFile(hostPath)).equals(originalHost) && (await readFile(projectPath)).equals(originalProject), 'Real host and project config bytes remain unchanged by runtime settings');
  await assert(`${q('#live-prompt')}.value===${JSON.stringify(draft)} && ${q('#live-prompt')}.selectionStart===${selection.start} && ${q('#live-prompt')}.selectionEnd===${selection.end}`, 'Thinking controls preserve composer draft and native selection');

  if (width < 768) await click('[data-nav-toggle]');
  await click('[data-settings-open]'); await wait(`${q('#settings-dialog')}.open`, 'native Settings dialog');
  await assert(`${q('#live-prompt')}.value===${JSON.stringify(draft)} && !${q('#reasoning-dialog')}.open`, 'Settings remains a separate modal without consuming the runtime composer draft');
  await key('Escape','Escape',27);
  if (await evaluate(`${q('#project-navigation')}?.classList.contains('nav-open')`)) await key('Escape','Escape',27);
  check(await count()===0 && !posts('host-settings-set').length, 'Opening and dismissing Settings does not mutate defaults or invoke provider');

  const versionsOpen = async () => { await click('[data-session-menu]'); await click('[data-menu-key="versions"]'); await wait(`!${q('[data-version-preview][data-version-id="main"]')}?.disabled`, 'bounded real version inventory'); };
  const selectMain = async () => { await click('[data-version-preview][data-version-id="main"]'); await wait(`!${q('[data-history-action="history-branch-rename"]')}.disabled`, 'exact selected branch preview'); };
  const historyReview = async (action,name) => {
    await replace('[data-history-name]',name); await click(`[data-history-review="${action}"]`);
    await assert(`${q('[data-history-confirm]')}.disabled && !${q('[data-history-consent]')}.checked`, 'History review requires fresh exact-target consent');
  };
  const historyAction = async (action,name) => {
    const previous = posts(action).length;
    await replace('[data-history-name]',name);
    await assert(`${q('[data-history-confirmation]')}.hidden`, 'Safe history action needs no review or consent panel');
    check(posts(action).length===previous, 'Typing a history name does not mutate before the explicit action');
    await click(`[data-history-action="${action}"]`);
    await assert(`${q('[data-history-confirmation]')}.hidden`, 'Safe history action executes directly without opening confirmation');
  };
  const historyConfirm = async () => { await click('[data-history-consent]'); await click('[data-history-confirm]'); };
  let s = await snapshot(); const source = {instance:s.instance_id,session:s.session_id,branch:s.goal.branch_id,tip:s.goal.tip_id};
  await versionsOpen(); await selectMain(); await historyAction('history-branch-rename','Fictional reviewed main');
  await wait(`${q('[data-version-preview][data-version-id="main"]')}?.textContent.includes('Fictional reviewed main')`, 'renamed inventory label');
  s=await snapshot();
  check(s.instance_id===source.instance && s.session_id===source.session && s.goal.branch_id===source.branch && s.goal.tip_id===source.tip, 'Rename changes the selected saved label, not instance, active history or branch');
  await selectMain(); await historyAction('history-session-fork','Fictional detached conversation');
  await wait(`${q('[data-history-inventory]')}?.textContent.includes('Fictional detached conversation')`, 'detached saved-conversation inventory');
  s=await snapshot();
  check(s.instance_id===source.instance && s.session_id===source.session && s.goal.branch_id===source.branch && s.goal.tip_id===source.tip, 'Detached fork only refreshes inventory and never opens or switches the child');
  await assert(`${q('[data-history-inventory] a')}?.href.includes('session=')`, 'Detached inventory exposes separate explicit Open links');
  await click('[data-versions-close]'); await idle();
  await assert(`!${q('#versions-dialog')}.open && document.activeElement===${q('[data-session-menu]')}`, 'React Versions Close restores menu focus after history changes');
  await versionsOpen(); await selectMain();
  await historyReview('history-branch-fork','Fictional active fork'); await capture('history-review'); await historyConfirm(); await idle();
  s=await snapshot();
  check(s.instance_id!==source.instance && s.session_id===source.session && s.goal.branch_id!==source.branch && s.mode==='plan', 'Branch fork rotates instance and activates the selected Plan history without replay');
  check(await count()===0 && posts('history-branch-fork').length===1 && posts('history-session-fork').length===1 && posts('history-branch-rename').length===1, 'Each explicit history action posts once and never invokes provider work');
  await assert(`${q('#live-prompt')}.value===${JSON.stringify(draft)}`, 'Branch replacement preserves the original composer draft');
  await mode('default');
  let n=await count()+1;
  await replace('#live-prompt','Fictional explicit post-fork request'); await key('Enter','Enter',13,process.platform==='darwin'?4:2); await waitCount(n);
  await release(n,'permission'); await wait(`${q('#live-status')}.textContent==='Approval needed'`, 'next-epoch native permission request');
  await click('[data-permission="allow"]'); await waitCount(++n); await release(n,'permission finished'); await idle();
  check((await readFile(join(directory,'project-a','fictional-browser-approved.txt'),'utf8')).includes('fictional native permission'), 'Post-fork native approval and tool completion remain visible in the new epoch');
  await assert(`${q('#live-transcript')}.textContent.includes('permission finished')`, 'Post-fork next-epoch assistant text is not retired with old history');

  await replace('#live-prompt',draft);
  const compactButton = '#live-composer-normal [data-compaction-open]';
  const compactStop = '#live-composer-normal [data-runtime-abort]';
  const noCompactionPopup = `!document.querySelector('dialog[open], [role="dialog"][aria-modal="true"], #compaction-dialog, [data-compaction-consent], [data-compaction-confirm]') && !document.querySelector('[data-react-live-panel="compaction"] input[type="checkbox"]')`;
  await assert(`${q(compactButton)}?.getAttribute('aria-label')==='Compact context' && document.querySelectorAll('[data-compaction-open]').length===1 && ${noCompactionPopup}`, 'Exactly one accessible composer Compact context action, without a popup or consent checkbox');
  const compact = async (fromMenu = false) => {
    await wait(`!${q(compactButton)}.disabled`, 'current direct compaction admission');
    const before = await snapshot(), postCount = posts('compaction-start').length;
    if (fromMenu) {
      await click('[data-session-menu]'); await click('[data-menu-key="compaction"]');
      await assert(`!document.querySelector('.conversation-task-menu') && ${noCompactionPopup}`, 'Conversation menu forwards compaction once and dismisses without a confirmation popup');
    } else await click(compactButton);
    return {before, postCount};
  };
  const compactReceipt = async ({before, postCount}) => {
    const end = Date.now()+15000;
    while (receipts.filter(receipt=>receipt.action==='compaction-start').length<=postCount && Date.now()<end) await delay(35);
    const receipt = receipts.filter(receipt=>receipt.action==='compaction-start')[postCount];
    if (!receipt) throw Error('Direct compaction HTTP receipt not observed');
    // Inspect locally; never retain request bodies, CSRF or exact scope IDs in reports.
    const {postData} = await send('Network.getRequestPostData',{requestId:receipt.requestId});
    const fields = new URLSearchParams(postData);
    check(fields.get('instance_id')===before.instance_id && fields.get('session_id')===before.session_id && fields.get('branch_id')===before.goal.branch_id && fields.get('expected_tip_id')===before.goal.tip_id && fields.get('expected_revision')===String(before.revision), 'Direct click posts the exact current instance/session/branch/tip/revision, including after ordinary completion');
    check(posts('compaction-start').length===postCount+1 && !fields.has('text') && !fields.has('confirm') && !fields.has('consent'), 'One direct click posts exactly one compaction request, without a prompt surrogate or consent fields');
  };
  const repeatWhileHeld = async () => {
    await assert(`${q(compactButton)}.disabled && ${q('#live-send')}.disabled && !${q(compactStop)}.disabled && ${noCompactionPopup}`, 'Serial compaction admission disables Compact and Send, keeps composer Stop, and opens no popup');
    // Native pointer attempts must reach the disabled control, not silently skip
    // it through click()'s enabled-target guard. No DOM click/event synthesis.
    const point = await evaluate(`(() => {const el=${q(compactButton)};el.scrollIntoView({block:'center',inline:'nearest'});const r=el.getBoundingClientRect(),x=(Math.min(innerWidth,r.right)+Math.max(0,r.left))/2,y=(Math.min(innerHeight,r.bottom)+Math.max(0,r.top))/2;return r.width>0&&r.height>0&&el.contains(document.elementFromPoint(x,y))?{x,y}:null;})()`);
    if (!point) throw Error('Disabled compact button missing or obscured during repeated native clicks');
    for (let i=0;i<3;i++) {
      await send('Input.dispatchMouseEvent',{type:'mousePressed',...point,button:'left',clickCount:1});
      await send('Input.dispatchMouseEvent',{type:'mouseReleased',...point,button:'left',clickCount:1});
    }
    await delay(150);
  };
  // Sample animation-frame boundaries, not just settled endpoints. Keep only
  // bounded public geometry/identity flags; never retain draft or scope data.
  // Settle prior typing and normalize ancestor scrolling performed by native
  // click helpers; neither input autosizing nor deliberate scrolling is flicker.
  await evaluate('new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))');
  await evaluate(`(() => {
    const dock=document.querySelector('#live-composer-normal'), editor=document.querySelector('#live-prompt'), button=document.querySelector('[data-compaction-open]');
    const layoutTop=()=>{ let top=dock.getBoundingClientRect().top; for(let p=dock.parentElement;p;p=p.parentElement) top+=p.scrollTop; return top; };
    const initial=dock.getBoundingClientRect(), top=layoutTop(), row=dock.querySelector('.composer-actions').getBoundingClientRect();
    const detail=()=>({dock:dock.getBoundingClientRect().height,editor:editor.getBoundingClientRect().height,form:document.querySelector('#live-composer').getBoundingClientRect().height,footer:dock.querySelector('.composer-footer').getBoundingClientRect().height,width:dock.getBoundingClientRect().width,status:document.querySelector('#composer-state').className,compaction:document.querySelector('[data-compaction-status]').textContent.slice(0,90)});
    const state=window.compactLayout={frames:0,heightDelta:0,topDelta:0,rowDelta:0,retired:false,queueVisible:false,done:false,initial:detail(),peak:null};
    const sample=()=>{
      const r=dock.getBoundingClientRect(), a=dock.querySelector('.composer-actions').getBoundingClientRect();
      if(Math.abs(r.height-initial.height)>state.heightDelta) state.peak=detail();
      state.heightDelta=Math.max(state.heightDelta,Math.abs(r.height-initial.height));
      state.topDelta=Math.max(state.topDelta,Math.abs(layoutTop()-top));
      state.rowDelta=Math.max(state.rowDelta,Math.abs(a.height-row.height));
      state.retired ||= !dock.isConnected || editor!==document.querySelector('#live-prompt') || button!==document.querySelector('[data-compaction-open]');
      state.queueVisible ||= !!dock.querySelector('[data-queue-next]:not([hidden])');
      if (!state.done && ++state.frames<1200) requestAnimationFrame(sample);
    }; requestAnimationFrame(sample);
  })()`);
  const compactPrompts = posts('prompt').length;
  const compactUsers = (await snapshot()).messages.filter(message=>message.role==='user').length;
  n=await count()+1; let admission=await compact(); await waitCount(n); await compactReceipt(admission);
  s=await waitSnapshot(s=>['pending','running'].includes(s.compaction?.state),'captured compaction operation');
  check(s.compaction.turn_origin==='compact' || s.compaction.state==='pending', 'Manual compaction uses captured native operation authority, not a prompt');
  check(s.compaction.session_id===admission.before.session_id && s.compaction.branch_id===admission.before.goal.branch_id && s.compaction.expected_tip_id===admission.before.goal.tip_id, 'Native operation captures the exact direct-click session, branch and tip');
  await repeatWhileHeld();
  check(await count()===n && posts('compaction-start').length===1 && posts('prompt').length===compactPrompts, 'Rapid repeated native clicks while provider work is held admit exactly one POST and one provider invocation');
  const aborts = posts('cancel').length;
  await wait(`!${q(compactStop)}.disabled`,'current compaction Stop authority'); await click(compactStop); await idle();
  s=await waitSnapshot(s=>s.compaction?.state==='canceled','native canceled compaction');
  check(s.status==='idle' && !s.cancel_requested, 'Stop waits for terminal native compaction cleanup before restoring readiness');
  check(posts('cancel').length===aborts+1 && await count()===n && posts('compaction-start').length===1, 'Composer Stop posts once and cancellation never retries compaction or provider work');
  await assert(`${q('#live-prompt')}.value===${JSON.stringify(draft)} && ${noCompactionPopup}`, 'Stopped direct compaction preserves the unsent draft without a dialog to close');
  await file('hold-compaction-progress'); n=await count()+1; admission=await compact(); await waitCount(n); await compactReceipt(admission); await release(n,'summary');
  s=await waitSnapshot(s=>s.compaction?.progress_done===true,'native compaction progress');
  check(s.compaction.state==='running', 'Native compaction_done progress does not falsely display terminal completion');
  await wait(`${q('[data-compaction-status]')}.textContent.includes('waiting for native cleanup')`,'native progress rendered through SSE');
  await assert(`!${q(compactStop)}.disabled && !${q('[data-compaction-status]')}.hidden && !${q('[data-compaction-status]')}.closest('dialog') && ${q('[data-compaction-status]')}.textContent.includes('waiting for native cleanup')`, 'Inline progress retains shared composer Stop and explicitly waits for native cleanup');
  await repeatWhileHeld();
  check(await count()===n && posts('compaction-start').length===2, 'Repeated clicks during held native progress cannot start another POST or provider invocation');
  await capture('compaction-progress'); await file('release-compaction-progress'); await idle();
  s=await waitSnapshot(s=>['completed','noop','fallback'].includes(s.compaction?.state),'terminal compact result');
  check(s.compaction.state==='completed' && s.compaction.summarized_messages>0, 'Actual fake-provider checkpoint finishes with refreshed compaction counts');
  const summary=JSON.parse(await readFile(join(directory,`request-${n}.json`),'utf8'));
  check(summary.System.startsWith('Create a factual working-state checkpoint') && !(summary.Tools||[]).length, 'Manual compact performs provider summary work without tools or a user prompt surrogate');
  await wait(`${q('[data-compaction-status]')}.textContent.includes('Manual compaction completed')`, 'terminal inline completion');
  await assert(`${q('#live-prompt')}.value===${JSON.stringify(draft)} && ${noCompactionPopup}`, 'Compaction and Stop preserve the unsent composer draft without a popup');
  const telemetry = s.telemetry;
  check(telemetry?.context_available===true && Number.isFinite(telemetry.context_tokens) && telemetry.context_tokens>=0, 'Terminal compaction refreshes authoritative context telemetry');
  await click('[data-telemetry-menu]');
  const number = value => Number(value||0).toLocaleString();
  const contextText = telemetry?.context_available ? `${number(telemetry.context_tokens)} tokens / ${telemetry.context_window>0?number(telemetry.context_window):'unknown window'}` : 'Unknown';
  const usageText = telemetry?.available ? `${number(telemetry.input_tokens)} in · ${number(telemetry.output_tokens)} out · ${number(telemetry.total_tokens)} total` : 'Unknown';
  await assert(`${q('[data-workflow-context]')}.textContent===${JSON.stringify(contextText)} && ${q('[data-workflow-usage]')}.textContent===${JSON.stringify(usageText)}`, 'Visible context and usage match the refreshed native telemetry; unknown usage is not fabricated');
  await key('Escape','Escape',27);
  await click('[data-compaction-dismiss]');
  await assert(`${q('[data-compaction-inline]')}.hidden && ${q('#live-prompt')}.value===${JSON.stringify(draft)}`, 'Dismissing compaction feedback preserves the draft without changing runtime state');
  // With no new turn after this checkpoint, an explicit second completed action
  // is a native no-op, not a hidden retry or another provider summary request.
  await evaluate(`(() => {
    const chat=document.querySelector('#live-stream'), transcript=document.querySelector('#live-transcript'), editor=document.querySelector('#live-prompt');
    const initial=chat.getBoundingClientRect(), scrollTop=chat.scrollTop, contentHeight=transcript.getBoundingClientRect().height, rows=[...transcript.querySelectorAll('.conversation-message')];
    const state=window.noopChatLayout={frames:0,topDelta:0,heightDelta:0,scrollDelta:0,contentDelta:0,retired:false,working:false,recovery:false,done:false,peak:null};
    const sample=()=>{ const r=chat.getBoundingClientRect(); state.frames++; state.retired ||= !chat.isConnected || !transcript.isConnected || !editor.isConnected || rows.some(row=>!row.isConnected);
      state.scrollDelta=Math.max(state.scrollDelta,Math.abs(chat.scrollTop-scrollTop)); state.contentDelta=Math.max(state.contentDelta,Math.abs(transcript.getBoundingClientRect().height-contentHeight));
      state.topDelta=Math.max(state.topDelta,Math.abs(r.top-initial.top)); state.heightDelta=Math.max(state.heightDelta,Math.abs(r.height-initial.height));
      state.working ||= !document.querySelector('#live-turn-status').hidden;
      state.recovery ||= !document.querySelector('#live-recovery').hidden;
      if(Math.abs(r.height-initial.height)>1) state.peak={before:initial.height,after:r.height,status:document.querySelector('[data-compaction-status]').textContent};
      if(!state.done && state.frames<600) requestAnimationFrame(sample);
    }; requestAnimationFrame(sample);
  })()`);
  admission=await compact(true); await compactReceipt(admission); await idle();
  s=await waitSnapshot(s=>s.compaction?.state==='noop','native no-op after completed checkpoint');
  check(s.compaction.summarized_messages===0 && await count()===n && posts('compaction-start').length===3, 'Explicit no-op posts once, summarizes no messages and invokes no provider');
  await wait(`${q('[data-compaction-status]')}.textContent.includes('No compaction was needed')`, 'inline native no-op result');
  await assert(`${q('#live-prompt')}.value===${JSON.stringify(draft)} && ${noCompactionPopup}`, 'Native no-op leaves draft intact and needs no dismissal or consent');
  await delay(150);
  const noopGeometry=await evaluate('(() => { noopChatLayout.done=true; return noopChatLayout; })()');
  check(noopGeometry.frames>0 && !noopGeometry.retired && !noopGeometry.working && !noopGeometry.recovery && noopGeometry.topDelta<=1 && noopGeometry.heightDelta<=1 && noopGeometry.scrollDelta<=1 && noopGeometry.contentDelta<=1, `No-op compaction leaves the whole chat stable: ${JSON.stringify(noopGeometry)}`);
  check(await count()===n && posts('compaction-start').length===3 && posts('prompt').length===compactPrompts && s.messages.filter(message=>message.role==='user').length===compactUsers, 'Completion, telemetry inspection and no-op do not repeat compaction, invoke provider work or append a user prompt');

  await evaluate('new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))');
  const geometry=await evaluate('(() => { compactLayout.done=true; return compactLayout; })()');
  check(geometry.frames>0 && geometry.frames<1200 && !geometry.retired && !geometry.queueVisible && geometry.heightDelta<=1 && geometry.topDelta<=1 && geometry.rowDelta<=1, `Compaction updates in place without frame-level composer jumps or Queue next: ${JSON.stringify(geometry)}`);

  const startRun=async text=>{const next=await count()+1;await replace('#live-prompt',text);await key('Enter','Enter',13,process.platform==='darwin'?4:2);await waitCount(next);await wait(`!${q('[data-steer-open]')}.disabled`,'native current-run steering');return next;};
  n=await startRun('Fictional ordinary steerable task');
  const users=(await snapshot()).messages.filter(m=>m.role==='user').length;
  await click('[data-session-menu]'); await click('[data-menu-key="steer"]'); await replace('[data-steer-text]','Fictional literal correction, not Queue next');
  await key('Enter','Enter',13,process.platform==='darwin'?4:2);
  s=await waitSnapshot(s=>s.steer?.items.some(item=>item.status==='accepted'),'native steering acceptance');
  check(s.messages.filter(m=>m.role==='user').length===users && await count()===n, 'Acceptance adds neither optimistic user rows nor a provider follow-up');
  await wait(`${q('[data-steer-error]')}.textContent.includes('Acceptance is not delivery')`,'acceptance presentation'); await capture('steer-accepted');
  await click('[data-steer-close]'); await release(n,'first steer answer'); await waitCount(++n);
  s=await waitSnapshot(s=>s.steer.items.some(item=>item.status==='delivered'),'native steering delivery');
  check(s.steer.items.at(-1).text==='Fictional literal correction, not Queue next', 'Only native delivery confirms the exact accepted steering text');
  await release(n,'steered task complete'); await idle();

  // Genuine native acceptance bytes are delayed, not forged: final SSE state
  // must win even when the HTTP receipt arrives after the live token retires.
  n=await startRun('Fictional late-ACK steering race'); await file('hold-steer-ack');
  await click('[data-session-menu]'); await click('[data-menu-key="steer"]'); await replace('[data-steer-text]','Fictional late accepted correction'); await key('Enter','Enter',13,process.platform==='darwin'?4:2);
  await waitFile('steer-ack-held');
  await release(n,'late root initial answer'); await waitCount(++n); await release(n,'late root final answer');
  s=await waitSnapshot(s=>s.status==='idle' && !s.steer.can_steer,'completion before HTTP steering ACK');
  check(s.messages.some(message=>message.role==='user' && message.text==='Fictional late accepted correction'), 'Native durable steering delivery precedes the held HTTP acceptance receipt');
  check(!s.steer.can_steer, 'Finished native root retires steering admission before its held HTTP receipt');
  await file('release-steer-ack');
  await wait(`${q('[data-steer-error]')}.textContent.includes('Acceptance is not delivery') && !${q('[data-steer-text]')}.readOnly`,'late correlated receipt accepted');
  await waitSnapshot(s=>s.steer.items.some(item=>item.text==='Fictional late accepted correction' && item.status==='delivered'),'receipt reconciles genuine buffered delivery');
  await assert(`${q('#live-unknown')}.hidden && ${q('#live-steer-history')}.textContent.includes('Delivered')`, 'Late HTTP acceptance does not become unknown or downgrade native delivery');
  await click('[data-steer-close]'); await idle();
  check(posts('steer').length===2 && posts('compaction-start').length===3 && posts('prompt').length===3, 'Two explicit steering requests and three compactions (including native no-op); no automatic retries');
  await delay(100);
  for (const receipt of receipts.filter(receipt=>receipt.action==='steer')) {
    const data=JSON.parse((await responseBody(receipt.requestId)).body);
    check(data.steer_ack?.status==='accepted' && !!data.steer_ack.item_id && !!data.steer_ack.request_id, 'Real steering HTTP receipt retains native request/item acceptance correlation');
  }
  // Actual HTTP-response loss after native acceptance, not a fabricated 409.
  // This must preserve a draft and only the exact captured running-root Stop.
  n=await startRun('Fictional transport-loss steering task');
  const capturedStop=(await snapshot()).cancel_token;
  await armSteerLoss(); await click('[data-session-menu]'); await click('[data-menu-key="steer"]');
  await replace('[data-steer-text]','Fictional lost-receipt draft');
  await key('Enter','Enter',13,process.platform==='darwin'?4:2);
  await wait(`${q('[data-steer-error]')}.textContent.includes('outcome is unknown')`,'actual lost HTTP receipt');
  await assert(`${q('[data-steer-text]')}.value==='Fictional lost-receipt draft' && !${q('#live-unknown')}.hidden`, 'Lost HTTP response preserves steering draft and global uncertainty');
  s=await snapshot(); check(s.cancel_token===capturedStop && s.status==='running','Unknown steering stays bound to the captured running root');
  await click('[data-steer-close]');
  await wait(`!${q('#live-composer-normal [data-runtime-abort]')}.disabled`,'unknown captured-root Stop remains available');
  await click('#live-composer-normal [data-runtime-abort]');
  await waitSnapshot(s=>s.status==='idle' && !s.cancel_requested,'unknown steering native Stop drains');
  await wait(`!${q('[data-steer-open]')}.disabled`,'idle explicit steering draft review');
  await click('[data-session-menu]'); await click('[data-menu-key="steer"]');
  await assert(`${q('[data-steer-review]')}.textContent==='Keep draft and dismiss' && ${q('[data-steer-submit]')}.disabled`, 'Idle uncertainty offers local draft dismissal, never implicit resubmission');
  await click('[data-steer-review]');
  await wait(`!${q('#live-steer-dialog')}.open`,'idle steering reservation dismissed');
  await assert(`${q('[data-steer-text]')}.value==='Fictional lost-receipt draft' && !${q('#live-unknown')}.hidden`, 'Local dismissal retains draft and does not claim the HTTP outcome known');
  await click('[data-runtime-reviewed]'); await idle();
  check(await count()===n && posts('steer').length===3, 'Lost response/Stop/local dismissal/global review never retry steering or provider work');
  await assert(`!${q('#live-send')}.disabled && !${q('[data-runtime-close]')}.disabled`, 'Explicit idle recovery releases both Send and Close without a reload');
  check((await readFile(hostPath)).equals(originalHost) && (await readFile(projectPath)).equals(originalProject), 'All runtime controls leave actual host/project config bytes unchanged');
}
