// All drags/keys use CDP native input; cancellation includes native touchCancel
// and an actual releasePointerCapture (not a fabricated lostcapture event).
export async function runScenarios({send, evaluate, check, wait, viewport, navigate, scenario, delay}) {
  const mouse = (type, x, y, buttons = 0) => send("Input.dispatchMouseEvent", {
    type, x, y, button: type === "mouseMoved" && !buttons ? "none" : "left", buttons, clickCount: type === "mouseMoved" ? 0 : 1
  });
  async function key(key, shift = false) {
    const codes = {ArrowLeft: 37, ArrowRight: 39, Home: 36, Escape: 27, Tab: 9};
    const event = {key, code: key, windowsVirtualKeyCode: codes[key], modifiers: shift ? 8 : 0};
    await send("Input.dispatchKeyEvent", {type: "keyDown", ...event});
    await send("Input.dispatchKeyEvent", {type: "keyUp", ...event});
    await delay(100);
  }
  async function point(side) {
    const p = await evaluate(`(() => { const node = handle(${JSON.stringify(side)}), r = box(node); return {x:r.x+r.width/2,y:r.y+r.height/2, hit: document.elementFromPoint(r.x+r.width/2,r.y+r.height/2)?.closest('[data-chat-width-handle]') === node}; })()`);
    if (!p.hit) throw new Error(`${side} handle is not natively hit-testable: ${JSON.stringify(await evaluate("widthState()"))}`);
    return p;
  }
  async function down(side) {
    await send("Page.bringToFront");
    const p = await point(side);
    await evaluate("window.widthPointer = null; window.widthEvents = []");
    await mouse("mouseMoved", p.x, p.y);
    await mouse("mousePressed", p.x, p.y, 1);
    await delay(30);
    await check("widthPointer?.trusted === true", "CDP produces a trusted native pointerdown");
    return p;
  }
  async function drag(side, dx, {commit = true, dy = 0} = {}) {
    const p = await down(side);
    await mouse("mouseMoved", p.x + dx, p.y + dy, 1); await delay(100);
    if (commit) { await mouse("mouseReleased", p.x + dx, p.y + dy); await delay(100); }
    return {x: p.x + dx, y: p.y + dy};
  }
  async function focus(side) {
    await evaluate(`handle(${JSON.stringify(side)}).focus({preventScroll:true})`);
    await check(`document.activeElement === handle(${JSON.stringify(side)})`, `${side} separator accepts keyboard focus`);
  }
  const noAPI = () => check("noWidthAPI() && harnessFixture.errors.length === 0", "Width interaction makes no API mutation and no browser error");
  async function desktopGeometry() {
    await check(`(() => { const s = widthState(); return s.handles.every(h => h && h.visible && h.role === 'separator' && h.orientation === 'vertical' && h.tabIndex === 0 && Number(h.min) === 640 && near(Number(h.max), s.max) && near(Number(h.now), s.width)); })()`, "Both visible vertical separators expose current/min/max ARIA widths");
    await check(`(() => { const s=widthState(), [l,r]=s.handles.map(h=>h.rect); return l.width > 0 && l.width <= 40 && r.width > 0 && r.width <= 40 && near(l.width,r.width) && l.left >= s.column.left+23 && r.right <= s.column.right-23 && s.transcript.left-l.right >= 23 && r.left-s.transcript.right >= 23 && near(s.transcript.left-l.right,r.left-s.transcript.right) && l.height > 0 && r.height > 0; })()`, "Symmetric strips retain content gap, outer clearance and bounded 40px hit width");
    await check("document.documentElement.scrollWidth <= innerWidth + 1", "No horizontal page overflow");
    await check(`(() => {const view=box($('#live-stream')), seat=box($('#live-composer-seat')), heading=box($('.live-header')); return widthState().handles.every(h => h.rect.top >= Math.max(0,view.top)-1 && h.rect.bottom <= Math.min(innerHeight,view.bottom)+1 && h.rect.top >= heading.bottom-1 && h.rect.bottom <= seat.top+1);})()`, "Hit strips stay within the visible transcript below header and above composer");
  }

  for (const width of [1280, 1512]) await scenario(`pointer-${width}`, async () => {
    await navigate({width});
    await check("near(widthState().width,widthState().auto) && widthState().stored === null", "Unconfigured width keeps the adaptive clamp(680px,64%,920px)");
    await desktopGeometry();
    for (const side of ["left", "right"]) {
      await evaluate("SnowWidth.reset()"); await delay(100);
      await evaluate("window.beforeWidth = widthState().width");
      const dx = side === "left" ? -18 : 18;
      const p = await drag(side, dx, {commit: false});
      await check("near(widthState().width,beforeWidth+36) && widthState().stored === null", `${side} outward 18px previews +36px without persistence`);
      await mouse("mouseReleased", p.x, p.y); await delay(100);
      await check("near(Number(widthState().stored),beforeWidth+36)", `${side} pointerup commits the actual changed width`);
      await drag(side, -dx);
      await check("near(widthState().width,beforeWidth) && near(Number(widthState().stored),beforeWidth)", `${side} inward movement is the symmetric inverse`);
    }
    await desktopGeometry(); await noAPI();
  });

  await scenario("keyboard-screen-space", async () => {
    await navigate();
    for (const side of ["left", "right"]) {
      await evaluate("SnowWidth.reset(); window.beforeWidth = widthState().width"); await delay(100);
      await focus(side);
      const outward = side === "left" ? "ArrowLeft" : "ArrowRight", inward = side === "left" ? "ArrowRight" : "ArrowLeft";
      await key(outward);
      await check("near(widthState().width,beforeWidth+10) && near(Number(widthState().stored),beforeWidth+10)", `${side} outward arrow commits a 10px screen-space width step`);
      await key(outward, true);
      await check("near(widthState().width,beforeWidth+50)", `${side} Shift+outward arrow adds 40px`);
      await key(inward, true); await key(inward);
      await check("near(widthState().width,beforeWidth)", `${side} inward arrows exactly undo both steps`);
      await key("Home");
      await check("widthState().stored === null && near(widthState().width,widthState().auto)", `${side} Home clears preference and restores adaptive width`);
    }
    await noAPI();
  });

  await scenario("cancel-and-stationary", async () => {
    await navigate({stored: 760});
    for (const side of ["left", "right"]) {
      const sign = side === "left" ? -1 : 1;
      let p = await drag(side, sign * 22, {commit: false});
      await check("near(widthState().width,804) && widthState().stored === '760'", `${side} live drag is a preview`);
      await key("Escape");
      await mouse("mouseReleased", p.x, p.y); await delay(100);
      await check("near(widthState().width,760) && widthState().stored === '760'", `${side} Escape restores preference and geometry`);
      p = await drag(side, sign * 22, {commit: false});
      await evaluate("handle(widthPointer.side).releasePointerCapture(widthPointer.id)");
      await mouse("mouseMoved", p.x + sign, p.y, 1); await delay(100);
      await mouse("mouseReleased", p.x + sign, p.y); await delay(100);
      await check("near(widthState().width,760) && widthState().stored === '760'", `${side} real lost capture cancels without commit`);
      await drag(side, 0, {dy: 16});
      await check("near(widthState().width,760) && widthState().stored === '760'", `${side} vertical-only pointer travel is not a width edit`);
    }
    await evaluate("SnowWidth.reset()"); await delay(100);
    await drag("right", 0);
    await check("widthState().stored === null && near(widthState().width,widthState().auto)", "Stationary pointerup never converts adaptive sizing to a preference");
    await noAPI();
  });

  await scenario("native-pointercancel", async () => {
    await navigate({stored: 760});
    await send("Emulation.setTouchEmulationEnabled", {enabled: true, maxTouchPoints: 1});
    try {
      for (const side of ["left", "right"]) {
        const p = await point(side), dx = side === "left" ? -20 : 20;
        const touch = (type, x = p.x) => send("Input.dispatchTouchEvent", {type, touchPoints: type === "touchCancel" ? [] : [{x, y: p.y, id: 1}]});
        await touch("touchStart"); await touch("touchMove", p.x + dx); await delay(100);
        await check("widthPointer?.trusted === true && near(widthState().width,800)", `${side} native touch drag previews the centered width`);
        await touch("touchCancel"); await delay(100);
        await check("near(widthState().width,760) && widthState().stored === '760'", `${side} native pointercancel restores width and storage`);
      }
    } finally { await send("Emulation.setTouchEmulationEnabled", {enabled: false}); }
    await noAPI();
  });

  await scenario("clamps-and-wide-preference-retention", async () => {
    await navigate({stored: 1400});
    await check("near(widthState().width,widthState().max) && widthState().stored === '1400'", "A wider stored preference is displayed clamped without rewriting it");
    await desktopGeometry();
    for (const side of ["left", "right"]) {
      await drag(side, 0); await drag(side, 0, {dy: 12});
      await check("widthState().stored === '1400'", `${side} stationary and vertical-only clamped drags retain wider preference`);
      const p = await drag(side, side === "left" ? 20 : -20, {commit: false});
      await key("Escape"); await mouse("mouseReleased", p.x, p.y); await delay(100);
      await check("widthState().stored === '1400' && near(widthState().width,widthState().max)", `${side} cancelling a clamped preview retains wider preference`);
    }
    await viewport(1280);
    await check("near(widthState().width,widthState().max) && widthState().stored === '1400'", "Viewport shrink clamps display only");
    await viewport(1512);
    await check("near(widthState().width,widthState().max) && widthState().stored === '1400'", "Viewport expansion restores available preferred width");
    await drag("right", 12);
    await check("near(Number(widthState().stored),widthState().max)", "Actual horizontal travel at the maximum deliberately commits the displayed clamp");
    await navigate({stored: 650});
    await drag("left", 100);
    await check("near(widthState().width,640) && near(Number(widthState().stored),640)", "Inward pointer drag clamps at 640px");
    await focus("right"); await key("ArrowLeft", true);
    await check("near(widthState().width,640)", "Keyboard cannot underflow the minimum");
    await noAPI();
  });

  await scenario("persistence-reload-clear-reset", async () => {
    await navigate();
    await drag("right", 28);
    const persisted = await evaluate("widthState().stored");
    await check("Number(widthState().stored) > 640", "Native drag produces a browser-local width preference");
    await navigate({preserve: true});
    await check(`widthState().stored === ${JSON.stringify(persisted)} && near(widthState().width,Number(widthState().stored))`, "Fresh production page restores committed preference");
    await evaluate('localStorage.removeItem("snow-manager-chat-width")');
    await navigate({preserve: true});
    await check("widthState().stored === null && near(widthState().width,widthState().auto)", "Clearing browser preference restores adaptive sizing on reload");
    await drag("left", -18); await evaluate("SnowWidth.reset()"); await delay(100);
    await check("widthState().stored === null && near(widthState().width,widthState().auto)", "Public reset clears persistence and restores adaptive sizing immediately");
    await navigate({preserve: true});
    await check("widthState().stored === null && near(widthState().width,widthState().auto)", "Reset remains adaptive after reload");
    await noAPI();
  });

  await scenario("narrow-and-inspector", async () => {
    await navigate({stored: 1400});
    // Opening the real inspector is itself allowed to fetch read-only files;
    // isolate those requests from the subsequent width-only action audit.
    await evaluate("$('.workspace-heading [data-inspector-toggle]').click()");
    await wait("!$('#project-inspector').hidden", "production inspector open"); await delay(200);
    await evaluate("startWidthRequests()");
    await check("widthState().stored === '1400' && near(widthState().width,Math.min(widthState().max, widthState().column.width-64))", "Inspector shrink retains wider browser preference while clamping content");
    await check(`(() => {const s=widthState(); return s.handles.every(h => h && (s.column.width-s.width >= 176 ? h.visible : !h.visible && h.tabIndex < 0));})()`, "Inspector handles disappear and leave tab order when gutters cannot fit");
    await viewport(1280);
    await check("widthState().stored === '1400' && widthState().handles.every(h => !h.visible && h.tabIndex < 0)", "1280px inspector suppresses unavailable handles without losing preference");
    await evaluate("$('#project-inspector [data-inspector-toggle]').click()"); await delay(150);
    await check("widthState().stored === '1400' && near(widthState().width,widthState().max)", "Closing inspector restores available preferred width");
    await focus("right");
    await viewport(390);
    await check("!document.activeElement?.matches('[data-chat-width-handle]')", "An active desktop separator relinquishes focus when hidden");
    await check("widthState().handles.every(h => !h.visible && h.tabIndex < 0) && widthState().stored === '1400'", "390px hides both separators without discarding desktop preference");
    await check("document.documentElement.scrollWidth <= innerWidth+1 && box($('#live-composer-seat')).bottom <= innerHeight+1", "Narrow transcript/composer remain bounded and reachable");
    await evaluate("$('#live-prompt').focus()");
    for (let i = 0; i < 14; i++) {
      await key("Tab");
      await check("!document.activeElement?.matches('[data-chat-width-handle]')", "Native Tab never reaches a hidden narrow separator");
    }
    await viewport(1512);
    await desktopGeometry();
    await check("widthState().stored === '1400'", "Returning from narrow viewport restores the retained preference");
    await noAPI();
  });

  await scenario("native-wheel", async () => {
    await navigate({stored: 760});
    await evaluate("$('#live-stream').scrollTop = ($('#live-stream').scrollHeight-$('#live-stream').clientHeight)*.5"); await delay(100);
    const control = await evaluate("(() => {const r=box($('#live-stream')); return {x:r.x+r.width/2,y:r.y+r.height/2,top:$('#live-stream').scrollTop};})()");
    await mouse("mouseMoved",control.x,control.y);
    await send("Input.dispatchMouseEvent", {type:"mouseWheel",x:control.x,y:control.y,deltaX:0,deltaY:120}); await delay(150);
    await check(`$('#live-stream').scrollTop > ${control.top}+20`, "Control native wheel over transcript scrolls normally");
    await evaluate("window.wheelBefore = {top:$('#live-stream').scrollTop,height:$('#live-stream').scrollHeight,heading:box($('.live-header')).top,seat:box($('#live-composer-seat')).top,pageY:scrollY}");
    for (const side of ["left", "right"]) {
      const p = await point(side);
      const before = await evaluate("$('#live-stream').scrollTop");
      await mouse("mouseMoved",p.x,p.y);
      await send("Input.dispatchMouseEvent", {type:"mouseWheel",x:p.x,y:p.y,deltaX:0,deltaY:120});
      await delay(150);
      await check(`$('#live-stream').scrollTop > ${before} + 20 && $('#live-session').dataset.scrollFollowing === 'false'`, `${side} native wheel scrolls the transcript and retains reader ownership`);
      await check("$('#live-stream').scrollHeight === wheelBefore.height && near(box($('.live-header')).top,wheelBefore.heading) && near(box($('#live-composer-seat')).top,wheelBefore.seat) && scrollY === wheelBefore.pageY", `${side} strip does not extend scroll range or pan background/header/composer`);
    }
    await noAPI();
  });

  await scenario("native-upward-wheel-releases-follow", async () => {
    await navigate({stored:760});
    for (const side of ["left", "right"]) {
      await evaluate("SnowScroll.follow()"); await delay(100);
      await check("$('#live-session').dataset.scrollFollowing === 'true' && near($('#live-stream').scrollTop,$('#live-stream').scrollHeight-$('#live-stream').clientHeight)", `${side} precondition: following is pinned at the real transcript floor`);
      await evaluate(`window.upwardWheelObserved=null; $('#live-stream').addEventListener('wheel',event => {window.upwardWheelObserved={trusted:event.isTrusted,following:$('#live-session').dataset.scrollFollowing,prevented:event.defaultPrevented};}, {once:true}); window.upwardFloor=$('#live-stream').scrollTop`);
      const p = await point(side);
      await mouse("mouseMoved",p.x,p.y);
      await send("Input.dispatchMouseEvent", {type:"mouseWheel",x:p.x,y:p.y,deltaX:0,deltaY:-120});
      await check("upwardWheelObserved?.trusted === true && upwardWheelObserved.following === 'false' && upwardWheelObserved.prevented === true", `${side} native upward wheel releases following during the event before a racing follow frame`);
      await delay(150);
      await check("$('#live-session').dataset.scrollFollowing === 'false' && near($('#live-stream').scrollTop,upwardFloor-120)", `${side} upward handle wheel stays at the reader offset rather than snapping back to tail`);
    }
    await noAPI();
  });

  await scenario("synthetic-wheel-modes-and-zoom-exclusion", async () => {
    await navigate({stored:760});
    // CDP native mouse wheel exposes pixel deltas only. Synthetic cancelable
    // WheelEvents isolate line/page conversion and zoom exclusion without
    // claiming that these assertions exercise native scrolling or browser zoom.
    for (const side of ["left", "right"]) {
      await evaluate("$('#live-stream').scrollTop = ($('#live-stream').scrollHeight-$('#live-stream').clientHeight)*.4"); await delay(100);
      for (const [mode,label] of [[1,"line"],[2,"page"]]) {
        await evaluate(`(() => {const stream=$('#live-stream'), before=stream.scrollTop, unit=${mode}===1 ? parseFloat(getComputedStyle(stream).lineHeight)||16 : stream.clientHeight; const event=new WheelEvent('wheel',{bubbles:true,cancelable:true,deltaY:2,deltaMode:${mode}}); handle(${JSON.stringify(side)}).dispatchEvent(event); window.wheelModeResult={prevented:event.defaultPrevented,before,after:stream.scrollTop,expected:before+unit*2,trusted:event.isTrusted};})()`);
        await check("wheelModeResult.trusted === false && wheelModeResult.prevented && near(wheelModeResult.after,wheelModeResult.expected)", `${side} synthetic ${label}-mode event converts exactly to transcript pixel distance`);
      }
      await evaluate("SnowScroll.follow()"); await delay(100);
      await evaluate(`(() => {const stream=$('#live-stream'), before=stream.scrollTop; const event=new WheelEvent('wheel',{bubbles:true,cancelable:true,ctrlKey:true,deltaY:-120}); handle(${JSON.stringify(side)}).dispatchEvent(event); window.ctrlWheelResult={prevented:event.defaultPrevented,before,after:stream.scrollTop,following:$('#live-session').dataset.scrollFollowing,trusted:event.isTrusted};})()`);
      await check("ctrlWheelResult.trusted === false && !ctrlWheelResult.prevented && ctrlWheelResult.following === 'true' && near(ctrlWheelResult.before,ctrlWheelResult.after)", `${side} synthetic Ctrl+wheel remains unconsumed and never acquires transcript reader ownership`);
      await delay(100);
      await check("$('#live-session').dataset.scrollFollowing === 'true' && near($('#live-stream').scrollTop,ctrlWheelResult.before)", `${side} excluded Ctrl+wheel schedules no delayed transcript movement`);
      await evaluate(`(() => {const stream=$('#live-stream'), node=handle(${JSON.stringify(side)}), before=stream.scrollTop; node.addEventListener('wheel',event=>event.preventDefault(),{capture:true,once:true}); const event=new WheelEvent('wheel',{bubbles:true,cancelable:true,deltaY:-120}); node.dispatchEvent(event); window.preventedWheelResult={prevented:event.defaultPrevented,before,after:stream.scrollTop,following:$('#live-session').dataset.scrollFollowing};})()`);
      await check("preventedWheelResult.prevented && preventedWheelResult.following === 'true' && near(preventedWheelResult.before,preventedWheelResult.after)", `${side} synthetic already-prevented upward wheel neither scrolls nor releases following`);
      await delay(100);
      await check("$('#live-session').dataset.scrollFollowing === 'true' && near($('#live-stream').scrollTop,preventedWheelResult.before)", `${side} canceled wheel bubbling cannot schedule a delayed follow release`);
    }
    await noAPI();
  });

  await scenario("lifecycle", async () => {
    await navigate({stored:760});
    await evaluate("SnowWidth.dispose()"); await delay(100);
    await check("document.querySelectorAll('[data-chat-width-handle]').length === 0 && widthState().stored === '760'", "Dispose removes handles while retaining the preference");
    await evaluate("SnowWidth.init($('#live-session')); SnowWidth.init($('#live-session'))"); await delay(100);
    await check("document.querySelectorAll('[data-chat-width-handle]').length === 2 && near(widthState().width,760)", "Repeated mount restores preference with exactly one pair of handles");
    await drag("right",20);
    await check("near(widthState().width,800) && near(Number(widthState().stored),800)", "Remount attaches exactly one native resize listener");
    await noAPI();
  });

  await scenario("reader-anchor", async () => {
    await navigate({stored: 760});
    // Public snapshot updates still pass through the production renderer/scroll
    // controller. Enough wrapping prose ensures width changes reflow history.
    await evaluate(`harnessFixture.update({status:'idle', messages:Array.from({length:30},(_,i)=>({id:'width-reader-'+i,role:i%2?'assistant':'user',text:('Public reader anchor paragraph '+i+'. ').repeat(35)}))})`);
    await wait("harnessFixture.deliveredRevision === harnessFixture.snapshot.revision", "long public transcript delivered"); await delay(150);
    await evaluate("$('#live-stream').scrollTop = ($('#live-stream').scrollHeight-$('#live-stream').clientHeight)*.45; $('#live-stream').dispatchEvent(new Event('scroll'))"); await delay(100);
    await evaluate("window.savedAnchor = readerAnchor()");
    await check("savedAnchor?.following === 'false'", "Reader owns an earlier transcript position");
    await drag("right", 35);
    await check("readerPreserved(savedAnchor)", "Native width drag preserves the reader's visible message offset");
    await evaluate("window.savedAnchor = readerAnchor()");
    await viewport(1280);
    await check("readerPreserved(savedAnchor)", "Viewport reconciliation preserves the reader anchor");
    await evaluate("window.savedAnchor = readerAnchor(); SnowWidth.reset()"); await delay(150);
    await check("readerPreserved(savedAnchor)", "Reset preserves reader ownership rather than snapping to tail");
    await noAPI();
  });

  for (const stored of ["not-a-width", "NaN", "Infinity", "-20", "0", "", "760px"]) await scenario(`invalid-storage-${stored || "empty"}`, async () => {
    await navigate({stored});
    await check("near(widthState().width,widthState().auto)", `Invalid stored value ${JSON.stringify(stored)} safely falls back to adaptive width`);
    await desktopGeometry(); await noAPI();
  });

  await scenario("short-transcript", async () => {
    await navigate({stored:760,height:180});
    await check(`(() => {const view=box($('#live-stream')); const usable=Math.max(0,Math.min(innerHeight,view.bottom)-Math.max(0,view.top)); return widthState().handles.every(h => usable<1 ? !h.visible && h.tabIndex<0 : h.visible && h.rect.height<=usable+1);})()`, "Short viewport strips match usable transcript height or leave tab order");
    await viewport(1512,120);
    await check(`(() => {const view=box($('#live-stream')); const usable=Math.max(0,Math.min(innerHeight,view.bottom)-Math.max(0,view.top)); return widthState().handles.every(h => usable<1 ? !h.visible && h.tabIndex<0 : h.visible && h.rect.height<=usable+1);})()`, "Collapsed transcript has no out-of-bounds hit surface");
    await noAPI();
  });

  await scenario("storage-write-failure", async () => {
    await navigate({stored:760});
    await evaluate(`window.savedSetItem=Storage.prototype.setItem; Storage.prototype.setItem=function(key,value) {if(key==='snow-manager-chat-width') throw new DOMException('Synthetic full storage','QuotaExceededError'); return savedSetItem.call(this,key,value);}`);
    await drag("right",20);
    await check("near(widthState().width,800) && widthState().stored === '760'", "Quota failure preserves a working in-tab width without corrupting existing storage");
    await focus("right"); await key("ArrowRight");
    await check("near(widthState().width,810) && widthState().stored === '760'", "Keyboard adjusts the in-memory preference after a write failure");
    await noAPI();
  });

  await scenario("blocked-local-storage", async () => {
    await navigate({blocked: true, theme: "light"});
    await check("widthState().stored === 'blocked' && near(widthState().width,widthState().auto)", "A throwing localStorage getter falls back to adaptive sizing");
    await evaluate("window.beforeWidth=widthState().width");
    await drag("right", 20);
    await check("near(widthState().width,beforeWidth+40)", "Native resize remains usable when persistence is blocked");
    await focus("left"); await key("Home");
    await check("near(widthState().width,widthState().auto)", "Home reset tolerates blocked localStorage removal");
    await noAPI();
  });
}
