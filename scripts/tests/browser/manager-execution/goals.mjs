import {writeFile, readFile} from 'node:fs/promises';
import {join} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';

export async function goalChecks(h) {
  const {q,evaluate,check,assert,wait,key,click,replace,snapshot,count,waitCount,release,posts,capture,idle,reload,directory,send,receipts,width} = h;
  const draft = 'Fictional unsent composer draft, not the objective.';
  const objective = 'Fictional objective: inventory three imaginary moons.';
  await replace('#live-prompt', draft);
  await key('Home','Home',36); await key('ArrowRight','ArrowRight',39,1); await key('ArrowRight','ArrowRight',39,1);
  const selection = await evaluate(`({start:${q('#live-prompt')}.selectionStart,end:${q('#live-prompt')}.selectionEnd})`);
  await evaluate('window.goalEditorIdentity=document.querySelector("#live-prompt"); true');
  await click('#live-composer [data-goal-toggle]');
  await assert(`${q('[data-goal-toggle]')}.getAttribute('aria-pressed')==='true' && ${q('#live-prompt')}.placeholder==='Describe the goal…' && document.activeElement===${q('#live-prompt')} && !document.querySelector('dialog[open]')`, 'Goal is a local composer mode, not a dialog');
  check(await count() === 0 && posts('goal-inspect').length === 0 && posts('goal-start').length === 0, 'Goal toggle performs no inspection or execution');
  await click('[data-goal-toggle]');
  await assert(`${q('[data-goal-toggle]')}.getAttribute('aria-pressed')==='false' && ${q('#live-prompt')}.value===${JSON.stringify(draft)} && ${q('#live-prompt')}.selectionStart===${selection.start} && ${q('#live-prompt')}.selectionEnd===${selection.end} && ${q('#live-prompt')}===window.goalEditorIdentity`, 'Goal toggle round trip preserves editor identity, draft, caret and selection');
  await click('[data-session-menu]'); await click('[data-menu-key="goals"]');
  await wait(`${q('[data-goal-scope')}?.textContent.includes('(confirmed absent)')`, 'secondary goal absence inspection');
  await assert(`!document.querySelector('[data-goal-objective-draft]') && ${q('[data-goal-authority]')}.textContent.includes('fake-1')`, 'Goal details contain authority and optional budget, not a second objective editor');
  await replace('[data-goal-token-budget]','0');
  await assert(`${q('[data-goal-write]')}.disabled`, 'Zero budget rejects new goal entry');
  await replace('[data-goal-token-budget]',width === 320 ? '' : '100000');
  await click('[data-goals-close]');
  await assert(`!${q('#goals-dialog')}.open && document.activeElement===${q('[data-session-menu]')}`, 'Secondary Goal Close restores the conversation menu');
  await click('[data-goal-toggle]'); await replace('#live-prompt',objective);
  // Hold one real read response, not its payload, to exercise typing during the
  // asynchronous preflight. The actual submission and edit both use native UI.
  await evaluate(`(() => { const original=window.fetch; const gate=new Promise(resolve=>window.releaseGoalInspection=resolve); window.goalInspectionHeld=false; window.fetch=async (...args)=>{ const response=await original(...args); if(String(args[0]).endsWith('/goal-inspect')) {window.fetch=original; window.goalInspectionHeld=true; await gate;} return response; }; })()`);
  await click('#live-send');
  await wait('window.goalInspectionHeld', 'goal preflight response held');
  await replace('#live-prompt',draft);
  await evaluate('window.releaseGoalInspection()');
  await wait('!window.SnowGoals.composerState().busy', 'changed-draft preflight rejected');
  check(await count() === 0 && posts('goal-start').length === 0, 'Typing during goal preflight cancels admission instead of sending a stale objective');
  await assert(`${q('#live-prompt')}.value===${JSON.stringify(draft)} && ${q('[data-goal-toggle]')}.getAttribute('aria-pressed')==='true'`, 'Rejected preflight preserves the edited draft and Goal mode');
  await replace('#live-prompt',objective);
  await assert(`${q('#live-send')}.getAttribute('aria-label')==='Start goal run' && !${q('#live-send')}.disabled`, 'Goal mode clearly changes the existing submit action');
  await capture('goal-composer');
  await click('#live-send');
  await wait('!window.SnowGoals.composerState().busy', 'goal admission settles');
  const admissionError = await evaluate(`${q('#live-error')}.textContent`);
  if (admissionError) throw Error('Goal admission: ' + admissionError);
  await waitCount(1);
  await wait(`${q('[data-goal-toggle]')}.getAttribute('aria-pressed')==='false' && ${q('#live-prompt')}.value===''`, 'confirmed goal consumes only its unchanged objective draft');
  await replace('#live-prompt',draft);
  let s = await snapshot(); const goal = s.goal.goal_id, run = s.goal.goal_run_id;
  check(!!goal && !!run && s.goal.running, 'Explicit Start owns real goal/run identities');
  check(s.goal.objective === objective && (width === 320 ? s.goal.token_budget == null : s.goal.token_budget === 100000), 'New goal preserves exact objective and optional budget');
  check(posts('goal-start').length === 1 && posts('goal-start')[0].objective === objective && !posts('goal-start')[0].keys.includes('text'), 'Exactly one Start POST; no prompt-text surrogate');
  await assert(`${q('#live-prompt')}.value===${JSON.stringify(draft)} && (${q('[data-queue-next]')}.hidden || ${q('[data-queue-next]')}.disabled)`, 'Goal run retains unsent draft and disallows Queue next');
  for (let n=1;n<=3;n++) {
    await release(n,`imaginary moon ${n}`); await waitCount(n+1); s = await snapshot();
    check(s.goal.goal_id === goal && s.goal.goal_run_id === run && s.goal.running, `Serial turn ${n+1} remains same native goal run`);
  }
  const events = (await readFile(join(directory,'native-events.jsonl'),'utf8')).trim().split('\n').map(JSON.parse).filter(e=>e.type==='turn_done' && e.turn_origin==='goal');
  check(events.length===3 && events.every(e=>e.goal_run_id===run) && new Set(events.map(e=>e.turn_id)).size===3, 'Three distinct serial native turn_done events correlate to one goal run');
  check(!(await snapshot()).messages.some(m=>m.role==='user'), 'Goal admission never invents a normal user prompt');
  await capture('goal-running');
  await click('#live-composer-normal [data-runtime-abort]'); await idle();
  await wait(`${q('[data-goal-status-badge]')}.textContent==='Run stopped'`, 'inline run-stopped status');
  await assert(`!!${q('[data-goal-details]')} && !${q('#goals-dialog')}.open`, 'Inline Goal status distinguishes a stopped run from semantic completion without opening a dialog');
  await click('[data-session-menu]'); await click('[data-menu-key="goals"]');
  await wait(`${q('[data-goal-deferred]')}?.textContent.includes('Yes')`, 'Stop defers goal');
  s = await snapshot();
  check(s.goal.goal_id===goal && !s.goal.running && s.goal.status!=='complete' && s.goal.deferred, 'Stop cancels whole run, leaving semantic goal deferred and not complete');
  await capture('goal-deferred');
  await click('[data-goals-close]'); await reload(); await delay(250);
  check(await count()===4 && posts('goal-resume').length===0, 'Reload never automatically resumes deferred goal');
  await click('[data-session-menu]'); await click('[data-menu-key="goals"]'); await wait(`!${q('[data-goal-resume]')}.disabled`, 'exact saved goal inspected for Resume');
  await assert(`${q('[data-goal-scope]')}.textContent.includes(${JSON.stringify(goal)}) && ${q('[data-goal-create-fields]')}.hidden`, 'Resume reviews exact goal ID and cannot edit existing budget');
  await click('[data-goal-resume]');
  await assert(`${q('[data-goal-confirm]')}.disabled && !${q('[data-goal-consent]')}.checked`, 'Resume requires separate fresh consent');
  // Release files are prepared before admission to exercise an immediate tool
  // completion / ACK race. The ID is observed from actual backend scope.
  await writeFile(join(directory,'selected-goal'),goal,{mode:0o600});
  await release(5,'complete'); await release(6,'selected goal completed');
  await click('[data-goal-consent]'); await click('[data-goal-confirm]');
  const deadline=Date.now()+15000;
  while(Date.now()<deadline) {
    s=await snapshot(); if(s.status==='permission') { await click('[data-permission="allow"]'); }
    if(s.status==='idle' && s.goal.status==='complete') break;
    await delay(50);
  }
  await idle(); s=await snapshot();
  check(s.goal.goal_id===goal && s.goal.status==='complete' && !s.goal.running, 'Fast explicit Resume completes exact selected goal');
  check(await count()===6 && posts('goal-resume').length===1 && posts('goal-resume')[0].goal===goal, 'One Resume of exact goal; no frontend automatic loop');
  const receipt = receipts.find(r=>r.action==='goal-resume');
  const body = receipt ? JSON.parse((await send('Network.getResponseBody',{requestId:receipt.requestId})).body) : null;
  check(body?.goal_run_ack?.goal_id===goal && !!body.goal_run_ack.goal_run_id && body.goal_run_ack.goal_run_id!==run, 'Production Resume HTTP ACK carries correlated admission receipt even with fast completion');
  await assert(`${q('#live-unknown')}.hidden`, 'Fast completion ACK does not mark admitted run unknown');
  await capture('goal-complete'); await reload(); await delay(250);
  check(await count()===6, 'Completed goal reload remains passive');
}
