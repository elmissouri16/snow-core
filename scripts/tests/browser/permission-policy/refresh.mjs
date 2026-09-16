import {setTimeout as delay} from "node:timers/promises";

export async function refreshChecks({state, evaluate, wait, click, navigate, width, height, theme}) {
  const results = [], failures = [];
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  const assert = async (expression, label) => check(await evaluate(expression), label);
  await navigate({streaming: true, messages: Array.from({length: 25}, (_, i) => ({id: `refresh-${i}`, role: "assistant", text: `Stable message ${i}`}))});
  const choices = {project_id: state.snapshot.project_id, instance_id: state.snapshot.instance_id, sessions: [], models: Array.from({length: 40}, (_, i) => ({provider: "fixture-provider", id: `model-${i}`, name: `Model ${String(i).padStart(2, "0")}`}))};
  const held = async () => { for (let i=0;i<100&&!state.held;i++) await delay(10); if (!state.held) throw new Error("Missing held choices request"); };
  await evaluate(`(() => {
    const composer=document.querySelector('#live-composer'), stream=document.querySelector('#live-stream');
    stream.scrollTop=100; stream.dispatchEvent(new Event('scroll'));
    window.refreshProbe={base:composer.getBoundingClientRect().toJSON(),scroll:stream.scrollTop,frames:[],run:true};
    const sample=()=>{if(!refreshProbe.run)return; const r=composer.getBoundingClientRect(); refreshProbe.frames.push({y:r.y,h:r.height,scroll:stream.scrollTop,policy:document.querySelector('[data-permission-policy-label]').textContent,connection:document.querySelector('#live-connection').textContent});requestAnimationFrame(sample)};requestAnimationFrame(sample);
  })()`);
  const reads = state.reads, streams = state.streamOpens;
  await click('[data-model-menu]'); await held();
  await assert('document.querySelector("#live-send").disabled && document.querySelector("[data-permission-policy-menu]").disabled', "Discovery locks mutation authority without impersonating a running turn");
  await evaluate(`(() => { const p=document.querySelector('.model-picker-menu'); refreshProbe.menu=p;refreshProbe.bounds=p.getBoundingClientRect().toJSON();refreshProbe.search=p.querySelector('[data-model-search]');refreshProbe.search.focus(); })()`);
  await delay(100); state.held(choices);
  await wait('document.querySelectorAll("[data-model-id]").length === 40');
  await evaluate(`(() => { const p=refreshProbe.menu;refreshProbe.rows=[...p.querySelectorAll('[data-model-id]')];refreshProbe.content=p.querySelector('.snow-menu-content');refreshProbe.content.scrollTop=180;refreshProbe.menuScroll=refreshProbe.content.scrollTop;refreshProbe.search.focus(); })()`);
  const stable = 'refreshProbe.menu===document.querySelector(".model-picker-menu") && refreshProbe.search===document.querySelector("[data-model-search]") && refreshProbe.rows.every((r,i)=>r===document.querySelectorAll("[data-model-id]")[i]) && refreshProbe.content===document.querySelector(".model-picker-menu > .snow-menu-content") && Math.abs(refreshProbe.content.scrollTop-refreshProbe.menuScroll)<1';
  for (const fail of [false, true, false]) {
    await click('[data-menu-key="load"]'); await held();
    await evaluate('refreshProbe.search.focus()');
    await assert(stable, "Pending model refresh retains cached rows, scroll container and input nodes");
    await assert('refreshProbe.menu.getAttribute("aria-busy")==="true" && refreshProbe.rows[0].disabled && getComputedStyle(refreshProbe.rows[0]).opacity==="1" && getComputedStyle(document.querySelector("[data-permission-policy-menu]")).opacity==="1"', "Loading stays explicit without pulsing cached rows or unrelated known controls; mutations remain disabled");
    state.push(); await delay(100);
    await assert('document.activeElement === refreshProbe.search', "Unrelated live update does not detach focused model search");
    state.held(fail ? {error: "Fixture discovery unavailable"} : choices, fail ? 503 : 200);
    await wait('!document.querySelector("[data-menu-key=load]").disabled');
    await assert(stable + ' && document.activeElement===refreshProbe.search', "Identical or failed discovery retains row identity, focus and model-list scroll");
    const bounds = await evaluate('refreshProbe.menu.getBoundingClientRect().toJSON()');
    const base = await evaluate('refreshProbe.bounds');
    check(Math.abs(bounds.x-base.x)<1 && Math.abs(bounds.y-base.y)<1 && Math.abs(bounds.height-base.height)<1, `Model popup geometry stays stable during first load/retry at ${width}×${height} ${theme}`);
  }
  await evaluate('refreshProbe.run=false');
  const m = await evaluate('({frames:refreshProbe.frames.length,stable:refreshProbe.frames.every(f=>Math.abs(f.y-refreshProbe.base.y)<1&&Math.abs(f.h-refreshProbe.base.height)<1&&Math.abs(f.scroll-refreshProbe.scroll)<1&&f.policy==="Ask"&&f.connection==="Live")})');
  check(m.frames>3 && m.stable, `All discovery frames preserve composer geometry, scroll and known labels: ${JSON.stringify(m)}`);
  check(state.reads===reads && state.streamOpens===streams && state.requests.every(r=>r.path.endsWith('/choices')&&r.accept==='application/json'), "Discovery uses bounded native JSON with no prompt, mutation, stream reconnect or extra snapshot GET");
  await evaluate('refreshProbe.content.scrollTop=0');
  await click('[data-model-id="model-0"]');
  await delay(100);
  await wait('document.querySelector("#live-model").textContent === "Model 00"', `model selection; requests=${JSON.stringify(state.requests)}; errors=${JSON.stringify(state.errors)}; ui=${await evaluate('JSON.stringify({label:document.querySelector("#live-model").textContent,error:document.querySelector("#live-error").textContent,unknown:!document.querySelector("#live-unknown").hidden})')}`);
  check(state.requests.filter(r=>r.path.endsWith('/model')).length===1 && state.streamOpens===streams && state.reads===reads, "A retained model row invokes the current validated native selection without reconnecting");
  state.next = 'model-success-only';
  await click('[data-model-menu]'); await click('[data-model-id="model-1"]');
  await wait('!document.querySelector("#live-unknown").hidden');
  await assert('document.querySelector("#live-send").disabled && document.querySelector("#live-model").textContent === "Model 00"', "A success-only model ACK cannot unlock a stale snapshot or optimistically select a model");
  await assert('policyErrors.length===0', "Refresh reconciliation has no production JavaScript errors");
  check(state.errors.length===0, `Refresh transport contracts remain valid: ${state.errors.join('; ')}`);
  return {results, failures};
}
