/* Browser-local conversation width, adapted from Harness ConversationRoot (MIT).
 * See HARNESS-NOTICE.txt. No runtime, provider, or session authority.
 */
(() => {
  "use strict";
  const key = "snow-manager-chat-width", minimum = 640, edgeBudget = 176;
  let owner;
  function readPreference() {
    try {
      const raw = localStorage.getItem(key);
      if (raw === null || raw.length > 64 || !raw.trim()) return null;
      const value = Number(raw);
      return Number.isFinite(value) && value > 0 ? value : null;
    } catch (_) { return null; }
  }
  function mount(region) {
    const column = region.closest(".conversation-pane") || region;
    const stream = region.querySelector("#live-stream"), transcript = region.querySelector("#live-transcript");
    if (!stream || !transcript) return null;
    const listeners = new AbortController(), options = {signal: listeners.signal};
    const handles = [];
    let preference = readPreference(), drag = null, frame = 0, disposed = false, observer;
    const columnWidth = () => column.getBoundingClientRect().width;
    const maximum = () => Math.max(minimum, columnWidth() - edgeBudget);
    const clamp = value => Math.max(minimum, Math.min(value, maximum()));
    const resolved = () => preference === null ? Math.max(680, Math.min(columnWidth() * .64, 920)) : clamp(preference);
    function save(value) {
      preference = value;
      try { if (value === null) localStorage.removeItem(key); else localStorage.setItem(key, String(value)); }
      catch (_) { /* Storage can be disabled; keep the working tab preference. */ }
    }
    function positions() {
      const view = stream.getBoundingClientRect(), content = transcript.getBoundingClientRect(), host = column.getBoundingClientRect();
      const header = region.querySelector(".live-header")?.getBoundingClientRect();
      const composer = region.querySelector("#live-composer-seat")?.getBoundingClientRect();
      const top = Math.max(0, view.top, header?.bottom || 0);
      const bottom = Math.min(innerHeight, view.bottom, composer?.top ?? innerHeight);
      for (const handle of handles) {
        const left = handle.dataset.chatWidthHandle === "left";
        const available = left ? content.left - host.left - 48 : host.right - content.right - 48;
        const width = Math.max(0, Math.min(40, available));
        const hidden = width < 1 || bottom <= top || !stream.getClientRects().length;
        // Fixed children do not extend the scroll range. Their wheel bridge
        // uses the existing scroll owner. Measured bounds keep them below
        // chrome and above the composer; narrow columns have no hit strip.
        handle.hidden = hidden; handle.tabIndex = hidden ? -1 : 0;
        handle.style.left = `${left ? content.left - 24 - width : content.right + 24}px`;
        handle.style.top = `${top}px`; handle.style.width = `${width}px`;
        handle.style.height = `${Math.max(0, bottom - top)}px`;
        handle.setAttribute("aria-valuemin", String(minimum));
        handle.setAttribute("aria-valuemax", String(Math.round(Math.max(maximum(), resolved()))));
        const value = Math.round(drag ? drag.width : resolved());
        handle.setAttribute("aria-valuenow", String(value));
        handle.setAttribute("aria-valuetext", `${value} pixels; ${preference === null && !drag ? "automatic" : "custom"} width`);
      }
    }
    function paint() {
      if (disposed || !region.isConnected) return;
      const width = columnWidth();
      if (!Number.isFinite(width) || width <= 0) return;
      const measured = `${width}px`, content = drag ? `${clamp(drag.width)}px` : preference === null ? "" : `${resolved()}px`;
      if (column.style.getPropertyValue("--conversation-column-width") !== measured || column.style.getPropertyValue("--chat-content-width") !== content) {
        window.SnowScroll?.beforeUpdate();
        column.style.setProperty("--conversation-column-width", measured);
        if (content) column.style.setProperty("--chat-content-width", content); else column.style.removeProperty("--chat-content-width");
        window.SnowScroll?.afterUpdate();
      }
      positions();
    }
    function schedule() {
      if (disposed || frame) return;
      frame = requestAnimationFrame(() => { frame = 0; paint(); });
    }
    function finish(commit = false, x) {
      if (!drag) return;
      const previous = drag;
      if (commit && Number.isFinite(x) && x !== previous.origin) save(clamp(previous.base + (x - previous.origin) * previous.direction * 2));
      drag = null;
      previous.handle.removeAttribute("data-dragging");
      document.documentElement.classList.remove("chat-width-dragging");
      if (previous.handle.hasPointerCapture(previous.pointer)) previous.handle.releasePointerCapture(previous.pointer);
      paint();
    }
    function reset() { finish(); save(null); paint(); }
    for (const side of ["left", "right"]) {
      const handle = document.createElement("div");
      handle.className = "chat-width-handle"; handle.dataset.chatWidthHandle = side;
      handle.setAttribute("role", "separator"); handle.setAttribute("aria-orientation", "vertical");
      handle.setAttribute("aria-label", `Conversation width, ${side} edge`);
      handle.setAttribute("aria-controls", "live-transcript live-composer-seat");
      handle.title = "Drag to resize conversation. Arrow keys adjust; Shift adjusts faster. Home restores automatic width. Escape cancels a drag.";
      handle.addEventListener("wheel", event => window.SnowScroll?.wheelFromHandle(event), {...options, passive: false});
      handle.addEventListener("pointerdown", event => {
        if (event.button !== 0 || event.isPrimary === false || drag || handle.hidden) return;
        event.preventDefault();
        handle.setPointerCapture(event.pointerId);
        handle.focus({preventScroll: true});
        drag = {handle, pointer: event.pointerId, base: resolved(), origin: event.clientX, direction: side === "left" ? -1 : 1, width: resolved()};
        handle.setAttribute("data-dragging", "true");
        document.documentElement.classList.add("chat-width-dragging");
      }, options);
      handle.addEventListener("pointermove", event => {
        handle.style.setProperty("--width-pointer-y", `${event.clientY - handle.getBoundingClientRect().top}px`);
        if (!drag || drag.handle !== handle || drag.pointer !== event.pointerId) return;
        drag.width = clamp(drag.base + (event.clientX - drag.origin) * drag.direction * 2);
        schedule();
      }, options);
      handle.addEventListener("pointerup", event => {
        if (drag?.handle === handle && drag.pointer === event.pointerId) finish(true, event.clientX);
      }, options);
      for (const name of ["pointercancel", "lostpointercapture"]) handle.addEventListener(name, event => {
        if (drag?.handle === handle && drag.pointer === event.pointerId) finish();
      }, options);
      handle.addEventListener("keydown", event => {
        if (event.altKey || event.ctrlKey || event.metaKey) return;
        if (event.key === "Home") { event.preventDefault(); reset(); }
        else if (["ArrowLeft", "ArrowRight"].includes(event.key)) {
          event.preventDefault(); finish();
          const direction = (event.key === "ArrowRight" ? 1 : -1) * (side === "left" ? -1 : 1);
          const current = resolved(), next = clamp(current + direction * (event.shiftKey ? 40 : 10));
          // The automatic width may exceed the dragged edge budget in a
          // partially fitting column. Widen must not unexpectedly narrow it.
          if (direction > 0 && next < current) return;
          save(next); paint();
        }
      }, options);
      stream.append(handle); handles.push(handle);
    }
    document.addEventListener("keydown", event => {
      if (event.key === "Escape" && drag) { event.preventDefault(); event.stopPropagation(); finish(); }
    }, {...options, capture: true});
    window.addEventListener("blur", () => finish(), options);
    document.addEventListener("visibilitychange", () => { if (document.hidden) finish(); }, options);
    window.addEventListener("resize", () => { finish(); schedule(); }, options);
    document.addEventListener("scroll", schedule, {...options, capture: true, passive: true});
    if (window.visualViewport) {
      window.visualViewport.addEventListener("resize", () => { finish(); schedule(); }, options);
      window.visualViewport.addEventListener("scroll", schedule, options);
    }
    if (typeof ResizeObserver === "function") {
      observer = new ResizeObserver(() => {
        // A changed host width invalidates a captured gesture's coordinate
        // system, but never replaces a wider persisted preference with a clamp.
        if (drag && column.style.getPropertyValue("--conversation-column-width") !== `${columnWidth()}px`) finish();
        schedule();
      });
      observer.observe(column); observer.observe(stream); observer.observe(transcript);
    }
    paint();
    return {reset, dispose() {
      if (disposed) return;
      disposed = true; finish(); listeners.abort(); observer?.disconnect();
      if (frame) cancelAnimationFrame(frame);
      for (const handle of handles) handle.remove();
      column.style.removeProperty("--conversation-column-width"); column.style.removeProperty("--chat-content-width");
    }};
  }
  function dispose() { owner?.dispose(); owner = null; }
  function init(region) { dispose(); if (region) owner = mount(region); }
  window.SnowWidth = Object.freeze({init, dispose, reset: () => owner?.reset()});
})();
