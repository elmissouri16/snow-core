// Compact read-only telemetry exercised through production HTML, HTTP and SSE.
export async function telemetryChecks({state, evaluate, wait, click, key, navigate, height}) {
  const results = [], failures = [];
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  const assert = async (expression, label) => check(await evaluate(expression), label);
  const telemetry = {available: true, input_tokens: 0, output_tokens: 0, total_tokens: 0, context_available: true, context_tokens: 3619, context_window: 272000, estimated: true};
  await navigate({streaming: true});
  state.update({telemetry}); state.push();
  const reads = state.reads, streams = state.streamOpens;
  await click('[data-telemetry-menu]');
  await wait('document.querySelector("[data-workflow-context]").textContent.startsWith("3,619")');
  await assert('document.querySelector("[data-workflow-cost]").textContent === "Unknown" && document.querySelector("[data-workflow-usage]").textContent === "0 in · 0 out · 0 total"', 'Compact summary distinguishes unavailable cost from verified zero usage');
  await assert('!document.querySelector(".telemetry-menu .snow-menu-note,.telemetry-menu .session-cost-note") && !!document.querySelector("[data-menu-key=telemetry-details]")', 'Default telemetry shows metrics and Details, not explanatory paragraphs');
  const geometry = await evaluate(`(() => {
    const panel=document.querySelector('.telemetry-menu'), rows=[...panel.querySelectorAll('dl > div')];
    const compact=panel.getBoundingClientRect().height;
    const clean=rows.every(row=>{const css=getComputedStyle(row);return css.paddingTop==='0px'&&css.paddingBottom==='0px'&&css.borderBottomWidth==='0px'});
    // Recreate the old layout locally to measure against the same data/viewport.
    const legacy=panel.cloneNode(true);legacy.removeAttribute('id');
    legacy.querySelector('[data-menu-key=telemetry-details]').remove();
    for(const row of legacy.querySelectorAll('dl > div')){row.style.display='block';row.style.padding='14px 0';row.style.borderBottom='1px solid var(--line)'}
    const note=document.createElement('p');note.className='snow-menu-note';note.textContent='Context estimates are approximate. Unknown values are not zero.';legacy.append(note);
    const costNote=document.createElement('p');costNote.className='session-cost-note';costNote.textContent='May exclude unpriced requests; aggregate currency coverage is not verified. An estimate, not a billing charge. Unknown is not zero.';legacy.querySelector('dl > div:last-child').append(costNote);
    legacy.style.visibility='hidden';document.body.append(legacy);const before=legacy.getBoundingClientRect().height;legacy.remove();
    return {compact,before,clean,within:panel.scrollWidth<=panel.clientWidth+1};
  })()`);
  check(geometry.clean && geometry.within, 'Telemetry rows reset inspector spacing without horizontal overflow');
  check(height < 300 || geometry.compact < geometry.before, `Compact popup reduces default height: ${geometry.before}px → ${geometry.compact}px (viewport height ${height})`);
  await evaluate(`(() => {
    const panel=document.querySelector('.telemetry-menu');
    window.telemetryProbe={panel,values:[...panel.querySelectorAll('dd')],mutations:[],renders:0,revision:0};
    telemetryProbe.observer=new MutationObserver(records=>telemetryProbe.mutations.push(...records));
    telemetryProbe.observer.observe(panel,{subtree:true,childList:true,attributes:true,characterData:true});
    const menus=window.SnowMenus;window.SnowMenus={...menus,reconcile(...args){telemetryProbe.renders++;return menus.reconcile(...args)}};
    const conversation=window.SnowConversation;window.SnowConversation={...conversation,render(snapshot,...args){const result=conversation.render(snapshot,...args);telemetryProbe.revision=snapshot?.revision;return result}};
  })()`);
  for (const fields of [{}, {session_name: 'Unrelated title update'}, {mode: 'plan'}]) {
    state.update(fields); state.push();
    await wait(`telemetryProbe.revision === ${state.snapshot.revision}`);
  }
  await assert('telemetryProbe.renders===0 && telemetryProbe.mutations.length===0', 'Identical telemetry and unrelated settings updates cause zero menu reconciliations or DOM mutations');
  state.update({telemetry: {...telemetry, context_tokens: 3620}}); state.push();
  await wait('document.querySelector("[data-workflow-context]").textContent.startsWith("3,620")');
  await assert('telemetryProbe.renders===1 && telemetryProbe.values.every((n,i)=>n===document.querySelectorAll(".telemetry-menu dd")[i]) && !telemetryProbe.mutations.some(m=>m.type==="childList")', 'Changed metric patches text in retained value nodes without rebuilding rows');
  await evaluate('telemetryProbe.observer.disconnect()');
  await click('[data-menu-key=telemetry-details]');
  await assert('document.querySelector(".telemetry-menu").textContent.includes("not a billing charge") && document.querySelector(".telemetry-menu").textContent.includes("Unknown values are not zero") && !!document.querySelector("[data-menu-key=back]")', 'Details retains accounting and estimate qualifications with a Back action');
  state.update({telemetry: {...telemetry, cost: {known: true, currency: 'USD', total: 0}}}); state.push();
  await wait('document.querySelector("[data-workflow-cost]").textContent === "USD 0.00"');
  await assert('!!document.querySelector("[data-menu-key=back]")', 'Live cost update keeps the Details pane open');
  await key('Escape', 'Escape', 27);
  await assert('!!document.querySelector("[data-menu-key=telemetry-details]") && document.querySelector("[data-workflow-cost]").dataset.known === "true"', 'Escape returns from Details to the summary with verified zero preserved');
  for (const [cost, known] of [[{known:true,currency:'EUR',total:1e-8},true],[{known:true,currency:'EUR',total:1e12},true],[{known:false,currency:'USD',total:0},false],[{known:true,currency:'usd',total:1},false]]) {
    state.update({telemetry: {...telemetry, estimated: false, cost}}); state.push();
    await wait(`telemetryProbe.revision === ${state.snapshot.revision}`);
    await assert(`document.querySelector('[data-workflow-cost]').dataset.known === '${known}' && ${known ? "document.querySelector('[data-workflow-cost]').textContent !== 'EUR 0.00'" : "document.querySelector('[data-workflow-cost]').textContent === 'Unknown'"}`, 'Compact costs preserve validated currency, tiny amounts and unavailable states');
  }
  await assert('document.querySelector("[data-workflow-context-label]").textContent === "Last reported input"', 'Reported context remains distinct from an estimate');
  state.update({telemetry: {...telemetry, context_window: 0}}); state.push();
  await wait('document.querySelector("[data-workflow-context]").textContent.includes("unknown window")');
  await assert('document.querySelector("[data-telemetry-menu]").dataset.unknown === "true"', 'Missing context window never implies a known usage percentage');
  state.update({telemetry: null}); state.push();
  await wait('document.querySelector("[data-workflow-context]").textContent === "Unknown"');
  await assert('[...document.querySelectorAll(".telemetry-menu dd")].every(n=>n.textContent === "Unknown")', 'Absent telemetry invalidates all displayed metrics, never replacing them with zero');
  await key('Escape', 'Escape', 27);
  await assert('!document.querySelector(".telemetry-menu") && document.activeElement.matches("[data-telemetry-menu]")', 'Closing telemetry restores focus to its composer trigger');
  check(state.reads===reads && state.streamOpens===streams && state.requests.length===0, 'Reading telemetry and Details sends no actions, inventory requests or extra subscriptions');
  await assert('policyErrors.length===0', 'Compact telemetry has no production JavaScript errors');
  return {results, failures};
}
