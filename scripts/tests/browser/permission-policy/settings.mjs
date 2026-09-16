// Native fetch + production SSE/DOM owners, against bounded HTTP snapshots.
import {setTimeout as delay} from "node:timers/promises";

export async function settingsChecks({state, evaluate, wait, click, navigate, width, height, theme}) {
  const results = [], failures = [];
  const check = (ok, label) => { results.push(label); if (!ok) failures.push(label); };
  const assert = async (expression, label) => check(await evaluate(expression), label);
  const messages = Array.from({length: 30}, (_, i) => ({id: `stable-${i}`, role: i % 2 ? "assistant" : "user", text: `Stable conversation message ${i}.\n\nKeep this content mounted during mode changes.`}));
  const modeRequests = () => state.requests.filter(request => request.path.endsWith("/mode"));
  const ready = () => wait('!document.querySelector("#live-send").disabled && document.querySelector("#live-connection").textContent === "Live"');
  const select = async mode => { await click("[data-mode-menu]"); await click(`[data-menu-key="${mode}"]`); };
  await navigate({streaming: true, messages});
  await ready();
  await evaluate(`(() => {
    const prompt = document.querySelector('#live-prompt'); prompt.value = 'Unsent draft stays here'; prompt.setSelectionRange(3, 9); prompt.dispatchEvent(new Event('input', {bubbles:true}));
    const stream = document.querySelector('#live-stream'); stream.scrollTop = 200; stream.dispatchEvent(new Event('scroll'));
  })()`);
  await delay(100);
  const streamCount = state.streamOpens, reads = state.reads;
  await evaluate(`(() => {
    const prompt=document.querySelector('#live-prompt'), transcript=document.querySelector('#live-transcript'), stream=document.querySelector('#live-stream'), composer=document.querySelector('#live-composer');
    window.smooth={prompt,transcript,row:transcript.firstElementChild,frames:[],run:true,baseline:composer.getBoundingClientRect().toJSON(),scroll:stream.scrollTop};
    const sample=()=>{if(!smooth.run)return; const r=composer.getBoundingClientRect(); smooth.frames.push({y:r.y,height:r.height,scroll:stream.scrollTop,connection:document.querySelector('#live-connection').textContent,policy:document.querySelector('[data-permission-policy-label]').textContent}); requestAnimationFrame(sample);};requestAnimationFrame(sample);
  })()`);
  for (const mode of ["plan", "default", "plan"]) {
    const count = modeRequests().length;
    state.next = "hold";
    await select(mode);
    for (let i=0;i<100&&!state.held;i++) await delay(10);
    check(modeRequests().length === count + 1 && modeRequests().at(-1)?.accept === "application/json", "Mode selection sends exactly one native JSON request with bounded fields");
    await assert('document.querySelector("#live-send").disabled && document.querySelector("[data-permission-policy-menu]").disabled', "Pending setting keeps competing mutations disabled");
    await assert(`document.querySelector('[data-mode-label]').textContent === ${JSON.stringify(mode === "plan" ? "Default" : "Plan Mode")}`, "Mode label waits for authoritative acknowledgement");
    await delay(120);
    if (!state.held) throw new Error("Mode request was not held");
    // A queued read cannot overwrite the newer acknowledgement.
    state.push();
    state.held(mode);
    await ready();
    await assert(`document.querySelector('[data-mode-label]').textContent === ${JSON.stringify(mode === "plan" ? "Plan Mode" : "Default")}`, "Accepted mode is not rolled back by an older queued SSE snapshot");
    await delay(80);
  }
  const measurement = await evaluate(`(() => {
    smooth.run=false;
    return {samples:smooth.frames.length,stable:smooth.frames.every(f=>Math.abs(f.y-smooth.baseline.y)<1&&Math.abs(f.height-smooth.baseline.height)<1&&Math.abs(f.scroll-smooth.scroll)<1),live:smooth.frames.every(f=>f.connection==='Live'&&f.policy==='Ask'),nodes:smooth.prompt===document.querySelector('#live-prompt')&&smooth.transcript===document.querySelector('#live-transcript')&&smooth.row===smooth.transcript.firstElementChild,draft:smooth.prompt.value,selection:[smooth.prompt.selectionStart,smooth.prompt.selectionEnd]};
  })()`);
  check(measurement.samples > 3 && measurement.stable, `No frame-level composer/scroll jump at ${width}×${height} ${theme}: ${JSON.stringify(measurement)}`);
  check(measurement.live, "Healthy setting changes never flash Synchronizing, disconnected or Unknown permission labels");
  check(measurement.nodes && measurement.draft === "Unsent draft stays here" && measurement.selection.join() === "3,9", "Transcript, message, composer draft and selection retain their identities/state");
  check(state.streamOpens === streamCount && state.reads === reads, "Three mode switches cause zero SSE reconnects or extra snapshot GETs");
  check(state.requests.every(request => request.path.endsWith("/mode")), "Settings never submit the composer or contact a provider");

  // A later SSE revision remains authoritative; no retained stale mode label.
  state.update({mode: "default"}); state.push();
  await wait('document.querySelector("[data-mode-label]").textContent === "Default"');
  check(true, "Later authoritative SSE updates still reach the mode label");
  state.next = "malformed";
  await select("plan");
  await wait('!document.querySelector("#live-unknown").hidden', `malformed mode acknowledgement; requests=${JSON.stringify(modeRequests())}; prior failures=${JSON.stringify(failures)}`);
  await assert('document.querySelector("#live-send").disabled', "Same-revision malformed setting acknowledgement fails closed");
  const count = modeRequests().length; await delay(150);
  check(modeRequests().length === count, "Uncertain setting is not automatically retried");

  await navigate({streaming: true, messages});
  state.next = "lost";
  await select("plan");
  await wait('!document.querySelector("#live-unknown").hidden', `lost mode response; requests=${JSON.stringify(modeRequests())}`);
  await assert('document.querySelector("#live-send").disabled', "Lost native response retains explicit unknown-outcome review even if SSE saw acceptance");
  await delay(150);
  check(modeRequests().length === 1, "Accepted setting with a lost response is never replayed");
  await assert('document.querySelector("#connection-error").hidden', "Lost settings response never shows the workspace banner suggesting a retry");

  await navigate({streaming: true, messages});
  state.next = "hold"; await select("plan");
  for (let i=0;i<100&&!state.held;i++) await delay(10);
  if (!state.held) throw new Error("Replacement test has no held mode request");
  state.terminate("replaced");
  await wait('!document.querySelector("[data-runtime-reload]").hidden');
  await assert('document.querySelector("#live-send").disabled && document.querySelector("[data-permission-policy-menu]").disabled', "Terminal SSE replacement immediately revokes controls during a pending native setting");
  state.held("plan"); await delay(120);
  await assert('document.querySelector("[data-mode-label]").textContent === "Default" && document.querySelector("#live-send").disabled', "Late old-lifetime settings response cannot change the replacement UI or restore authority");
  check(modeRequests().length === 1, "Replacement never replays the retired settings request");
  return {results, failures};
}
