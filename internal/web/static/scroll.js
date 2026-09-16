/* Transcript following adapted from Harness ChatView/ConversationRoot (MIT).
 * See HARNESS-NOTICE.txt. Presentation only: no transport or runtime authority.
 *
 * Integration contract (one mounted conversation per document):
 *   SnowScroll.init(liveSessionRegion, projectID + ':' + sessionID)
 *   SnowScroll.beforeUpdate() // before changing messages, activity OR attention
 *   ...reconcile all DOM, including controls and #live-composer-seat...
 *   SnowScroll.afterUpdate()
 *   SnowScroll.follow()       // explicit Jump, or the operator's accepted send
 *   SnowScroll.dispose()      // before native navigation detaches the old workspace
 *
 * #live-stream is the sole transcript scrollport; #live-transcript is content.
 * #live-composer-seat is a non-scrolling sibling wrapping BOTH attention and
 * the normal composer card (inside the region or its .conversation-pane).
 * Attention owns its bounded form overflow. Remove
 * app.js's near-bottom sampling, initial scroll writes and old scroll listener.
 * This module binds the existing #jump-latest button, without synthetic clicks.
 * Session positions are bounded tab memory only; no prompt text is retained.
 */
(() => {
  "use strict";
  const positions = new Map(), memoryLimit = 40, threshold = 24, epsilon = 0.5;
  let owner;
  const $ = (selector, scope) => scope.querySelector(selector);
  const clamp = (value, max) => Math.max(0, Math.min(value, max));

  function mount(region, key) {
    const stream = $("#live-stream", region), transcript = $("#live-transcript", region);
    if (!stream || !transcript) return null;
    const host = region.closest(".conversation-pane") || region;
    const seat = $("#live-composer-seat", host), jump = $("#jump-latest", region);
    const cleanup = [], saved = positions.get(key);
    let following = saved?.following !== false, anchor = saved?.anchor || null;
    let observedTop = stream.scrollTop, updating = false, disposed = false, frame = 0;
    let resizeObserver, contentObserver, geometry, promptSize;
    // Writes update this ledger synchronously. Scroll events carry the current
    // offset, not the historical write, even when multiple deliveries coalesce.
    // Clamp-only deliveries therefore never acquire/release reader ownership.
    const floor = () => Math.max(0, stream.scrollHeight - stream.clientHeight);
    const top = () => clamp(stream.scrollTop, floor());
    function listen(target, event, handler, options) {
      target.addEventListener(event, handler, options);
      cleanup.push(() => target.removeEventListener(event, handler, options));
    }
    function write(value) {
      const next = clamp(value, floor());
      if (Math.abs(stream.scrollTop - next) > epsilon) stream.scrollTop = next;
      observedTop = top();
    }
    function rows() {
      // No selector interpolation of server identities; message/activity keys
      // are separate namespaces. A duplicated identity cannot be an anchor.
      const nodes = [...stream.querySelectorAll('[data-message-id],[data-activity-id]')];
      const counts = new Map();
      const keyed = nodes.map(node => ({node, key: node.dataset.messageId ?
        `message:${node.dataset.messageId}` : node.dataset.activityId ? `activity:${node.dataset.activityId}` : ""}));
      for (const item of keyed) counts.set(item.key, (counts.get(item.key) || 0) + 1);
      return keyed.filter(item => item.key && counts.get(item.key) === 1 && !item.node.hidden);
    }
    function capture() {
      const view = stream.getBoundingClientRect(), candidates = [];
      // A small chain survives bounded head trimming if the leading row goes
      // away. The next surviving row retains its original viewport position.
      for (const item of rows()) {
        const rect = item.node.getBoundingClientRect();
        if (rect.bottom <= view.top || rect.height === 0) continue;
        candidates.push({key: item.key, offset: rect.top - view.top});
        if (candidates.length === 8) break;
      }
      return {top: top(), candidates};
    }
    function remember() {
      if (!key) return;
      positions.delete(key);
      positions.set(key, {following, anchor: following ? null : anchor});
      while (positions.size > memoryLimit) positions.delete(positions.keys().next().value);
    }
    function chrome() {
      region.dataset.scrollFollowing = String(following);
      // Any deliberate upward movement, even 1px inside the threshold, reveals
      // the escape hatch immediately. Geometry alone never hides reader intent.
      if (jump) jump.hidden = following || floor() <= epsilon;
    }
    function restore() {
      if (!anchor) { anchor = capture(); return; }
      const keyed = new Map(rows().map(item => [item.key, item.node]));
      const survivor = anchor.candidates.find(item => keyed.has(item.key));
      if (survivor) {
        const offset = keyed.get(survivor.key).getBoundingClientRect().top - stream.getBoundingClientRect().top;
        write(top() + offset - survivor.offset);
      } else write(anchor.top);
      anchor = capture();
    }
    function sampleReader() {
      const current = top(), expected = clamp(observedTop, floor());
      const delta = current - expected;
      if (Math.abs(delta) <= epsilon) { observedTop = current; return false; }
      // Direction matters: tiny upward gestures must not get erased by the
      // next chunk. Only a reader moving DOWN into the tail may auto-rejoin.
      following = delta > 0 && floor() - current <= threshold;
      observedTop = current;
      anchor = following ? null : capture();
      chrome(); remember();
      return true;
    }
    function scroll() {
      if (disposed) return;
      sampleReader();
      chrome();
    }
    function sizePrompt() {
      const prompt = seat && $("#live-prompt", seat);
      // Attention can hide the normal card. Measure its existing draft only
      // once it has a real layout box again; never replace/focus/edit the input.
      if (!prompt || !prompt.getClientRects().length) return;
      const style = getComputedStyle(prompt), width = prompt.clientWidth;
      const minimum = style.minHeight, maximum = style.maxHeight;
      if (promptSize?.node === prompt && promptSize.value === prompt.value &&
          promptSize.width === width && promptSize.minimum === minimum && promptSize.maximum === maximum) return;
      const scrollTop = prompt.scrollTop;
      const border = parseFloat(style.borderTopWidth) + parseFloat(style.borderBottomWidth);
      const padding = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom);
      // Reset only presentation to allow shrinking. CSS keeps the normal 36px
      // floor, short-seat floor and measured seat/336px cap authoritative.
      prompt.style.height = "0px";
      const height = prompt.scrollHeight + (style.boxSizing === "border-box" ? border : -padding);
      prompt.style.height = `${Math.max(parseFloat(minimum) || 0,
        Math.min(height, parseFloat(maximum) || Infinity))}px`;
      prompt.scrollTop = scrollTop;
      promptSize = {node: prompt, value: prompt.value, width: prompt.clientWidth, minimum, maximum};
    }
    function measure() {
      if (seat) seat.style.setProperty("--scroll-seat-limit", `${Math.max(64, host.getBoundingClientRect().height * 0.55)}px`);
      sizePrompt();
      const rect = region.getBoundingClientRect(), view = stream.getBoundingClientRect();
      const content = transcript.getBoundingClientRect();
      // The stream ends above the flow-seated card. Measure that actual edge,
      // not a guessed composer height; margins/safe-area/footer are included.
      const seatTop = seat && !seat.hidden ? seat.getBoundingClientRect().top : rect.bottom;
      const bottom = Math.max(0, rect.bottom - Math.min(view.bottom, seatTop)) + 16;
      const right = Math.max(16, rect.right - Math.min(content.right, view.right - 16));
      region.style.setProperty("--scroll-jump-bottom", `${bottom}px`);
      region.style.setProperty("--scroll-jump-right", `${right}px`);
      return [stream.scrollHeight, stream.clientHeight, stream.clientWidth, rect.height, seatTop, content.width].join(":");
    }
    function reconcile(force = false) {
      if (disposed || updating) return;
      // Input can have changed scrollTop before its queued scroll event fires.
      // Settle it before a stream/resize callback gets a chance to write.
      sampleReader();
      const next = measure();
      if (force || next !== geometry) {
        if (following) write(floor());
        else restore();
      }
      geometry = next;
      chrome(); remember();
    }
    function schedule() {
      if (disposed || frame) return;
      frame = requestAnimationFrame(() => { frame = 0; reconcile(true); });
    }
    function beforeUpdate() {
      if (disposed || updating) return;
      sampleReader();
      // Use the previous stable anchor when ResizeObserver has not delivered
      // a reflow yet. Capturing the already-shifted row would lose that anchor.
      if (!following && !anchor) anchor = capture();
      updating = true;
    }
    function afterUpdate() {
      if (disposed) return;
      updating = false;
      // Structural changes can move rows without changing total content size.
      // Restoring an unchanged anchor is a no-op, never a near-bottom snap.
      reconcile(true);
      observeContent();
    }
    function follow() {
      if (disposed) return;
      following = true; anchor = null;
      geometry = measure();
      write(floor()); chrome(); remember();
    }
    function observeContent() {
      if (!resizeObserver) return;
      // Rebuild a small bounded observation set after reconciliation; removed
      // activity/message nodes must not stay retained by ResizeObserver.
      resizeObserver.disconnect();
      resizeObserver.observe(host); resizeObserver.observe(region); resizeObserver.observe(stream);
      if (seat) resizeObserver.observe(seat);
      for (const child of stream.children) resizeObserver.observe(child);
    }
    function visualViewportChanged() {
      const viewport = window.visualViewport;
      // Dynamic viewport units handle browser chrome. Use the visual height
      // when an unzoomed keyboard reduces it; don't counteract pinch zoom or
      // invent a keyboard inset. Pan changes trigger measurement only: actual
      // iOS/Android focus/pan must be checked on-device, not promised by CSS.
      if (viewport && Math.abs(viewport.scale - 1) < 0.01 && viewport.height > 0) {
        document.documentElement.style.setProperty("--snow-visual-height", `${viewport.height}px`);
      } else document.documentElement.style.removeProperty("--snow-visual-height");
      schedule();
    }
    if (jump) {
      jump.setAttribute("aria-label", "Jump to latest"); jump.title = "Jump to latest";
      // Upgrade the old text pill without replacing the actual control node.
      if (!$("svg", jump)) {
        const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
        svg.setAttribute("viewBox", "0 0 24 24"); svg.setAttribute("aria-hidden", "true");
        svg.setAttribute("fill", "none"); svg.setAttribute("stroke", "currentColor");
        svg.setAttribute("stroke-width", "1.7"); svg.setAttribute("stroke-linecap", "round");
        svg.setAttribute("stroke-linejoin", "round");
        const path = document.createElementNS(svg.namespaceURI, "path");
        path.setAttribute("d", "M12 5v14m-6-6 6 6 6-6"); svg.append(path);
        jump.replaceChildren(svg);
      }
      listen(jump, "click", follow);
    }
    listen(stream, "scroll", scroll, {passive: true});
    if (seat) listen(seat, "input", event => {
      if (event.target.id === "live-prompt") reconcile(true);
    });
    // Stop pending follow writes at the beginning of upward wheel/touch/key
    // intent, not after a racing streaming frame. Do not cancel native input,
    // and do not claim events from nested code/table/form scrollers.
    function nestedScroller(target) {
      for (let node = target; node && node !== stream; node = node.parentElement) {
        if (node.scrollHeight > node.clientHeight + 1 && /auto|scroll/.test(getComputedStyle(node).overflowY)) return true;
      }
      return false;
    }
    function release() {
      if (following && floor() > epsilon) {
        following = false; anchor = capture(); chrome(); remember();
      }
    }
    function wheelIntent(event) {
      if (!event.defaultPrevented && event.deltaY < 0 && !event.ctrlKey && !nestedScroller(event.target)) release();
    }
    listen(stream, "wheel", wheelIntent, {passive: true});
    function wheelFromHandle(event) {
      // Fixed width handles do not join the native overflow scroll chain,
      // despite DOM ancestry. Keep the narrow bridge here with follow/reader
      // intent, not in a competing scroll controller. Ctrl+wheel stays zoom.
      if (event.ctrlKey || event.defaultPrevented || !event.deltaY || !stream.contains(event.target) || !event.target.closest?.("[data-chat-width-handle]")) return;
      wheelIntent(event);
      event.preventDefault();
      const unit = event.deltaMode === 2 ? stream.clientHeight : event.deltaMode === 1 ? parseFloat(getComputedStyle(stream).lineHeight) || 16 : 1;
      stream.scrollTop += event.deltaY * unit;
    }
    let touchY;
    listen(stream, "touchstart", event => { touchY = event.touches.length === 1 ? event.touches[0].clientY : undefined; }, {passive: true});
    listen(stream, "touchmove", event => {
      if (touchY === undefined || event.touches.length !== 1) return;
      const y = event.touches[0].clientY;
      if (y > touchY && !nestedScroller(event.target)) release();
      touchY = y;
    }, {passive: true});
    listen(stream, "keydown", event => {
      if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey || event.target.closest?.('input,textarea,select,[contenteditable="true"]')) return;
      if (["ArrowUp", "PageUp", "Home"].includes(event.key) || event.key === " " && event.shiftKey) {
        if (!nestedScroller(event.target)) release();
      }
    });
    listen(window, "resize", visualViewportChanged, {passive: true});
    if (window.visualViewport) {
      listen(window.visualViewport, "resize", visualViewportChanged, {passive: true});
      listen(window.visualViewport, "scroll", schedule, {passive: true});
    }
    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(schedule); observeContent();
    }
    // Covers same-height keyed replacements and tool disclosure in addition to
    // resize. Attribute filtering excludes our own chrome/style writes.
    if (typeof MutationObserver === "function") {
      contentObserver = new MutationObserver(() => { observeContent(); schedule(); });
      contentObserver.observe(stream, {childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ["open", "hidden"]});
    }
    geometry = measure();
    if (saved && !following) { write(saved.anchor?.top || 0); restore(); }
    else write(floor());
    chrome(); remember(); visualViewportChanged();
    return {
      beforeUpdate, afterUpdate, follow, wheelFromHandle,
      dispose() {
        if (disposed) return;
        sampleReader(); remember(); disposed = true;
        if (frame) cancelAnimationFrame(frame);
        resizeObserver?.disconnect(); contentObserver?.disconnect();
        for (const remove of cleanup) remove();
        region.removeAttribute("data-scroll-following");
        for (const name of ["--scroll-jump-bottom", "--scroll-jump-right"]) region.style.removeProperty(name);
        seat?.style.removeProperty("--scroll-seat-limit");
        document.documentElement.style.removeProperty("--snow-visual-height");
      }
    };
  }
  function dispose() { owner?.dispose(); owner = null; }
  function init(region, key) { dispose(); if (region) owner = mount(region, String(key || "")); }
  window.SnowScroll = Object.freeze({init, dispose, beforeUpdate: () => owner?.beforeUpdate(),
    afterUpdate: () => owner?.afterUpdate(), follow: () => owner?.follow(), wheelFromHandle: event => owner?.wheelFromHandle(event)});
})();
