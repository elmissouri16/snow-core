// Observation only: no replacement controls, styles, or production width logic.
(() => {
  window.$ = selector => document.querySelector(selector);
  window.near = (a, b, tolerance = 2) => Math.abs(a - b) <= tolerance;
  window.box = node => node.getBoundingClientRect().toJSON();
  window.handle = side => $(`[data-chat-width-handle="${side}"]`);
  window.shown = node => !!node && node.getClientRects().length > 0 && getComputedStyle(node).visibility !== "hidden";
  window.widthState = () => {
    const column = box($(".conversation-pane")), transcript = box($("#live-transcript"));
    let stored; try { stored = localStorage.getItem("snow-manager-chat-width"); } catch { stored = "blocked"; }
    return {column, transcript, width: transcript.width, stored,
      inline: $(".conversation-pane").style.getPropertyValue("--chat-content-width"), events: window.widthEvents, focused:document.hasFocus(), reader:window.readerAnchor?.(), savedAnchor:window.savedAnchor, captured:window.widthPointer ? handle(widthPointer.side)?.hasPointerCapture(widthPointer.id) : null,
      auto: Math.min(920, Math.max(680, column.width * .64)), max: Math.max(640, column.width - 176),
      handles: ["left", "right"].map(side => {
        const node = handle(side);
        return node ? {side, visible: shown(node), rect: box(node), tabIndex: node.tabIndex,
          role: node.getAttribute("role"), orientation: node.getAttribute("aria-orientation"),
          min: node.getAttribute("aria-valuemin"), max: node.getAttribute("aria-valuemax"), now: node.getAttribute("aria-valuenow")} : null;
      })};
  };
  window.widthEvents = [];
  for (const type of ["pointerdown", "pointermove", "pointerup", "pointercancel", "gotpointercapture", "lostpointercapture", "blur", "resize", "wheel", "scroll"]) {
    window.addEventListener(type, event => {
      if (widthEvents.length >= 30) widthEvents.shift();
      widthEvents.push({type, target:event.target?.dataset?.chatWidthHandle, x:event.clientX, buttons:event.buttons, deltaY:event.deltaY, dragging:!!document.querySelector('[data-dragging]')});
    }, true);
  }
  window.widthRequestStart = harnessFixture.requests.length;
  window.noWidthAPI = () => harnessFixture.requests.slice(widthRequestStart).every(request =>
    request.method === "GET" && /\/runtime(?:\/events)?$/.test(request.path));
  window.startWidthRequests = () => { widthRequestStart = harnessFixture.requests.length; };
  document.addEventListener("pointerdown", event => {
    if (event.target.closest("[data-chat-width-handle]")) window.widthPointer = {id: event.pointerId, side: event.target.closest("[data-chat-width-handle]").dataset.chatWidthHandle, trusted: event.isTrusted};
  }, true);
  window.readerAnchor = () => {
    const stream = $("#live-stream"), view = box(stream);
    const row = [...stream.querySelectorAll("[data-message-id]")].find(node => box(node).bottom > view.top + 5 && box(node).top < view.bottom);
    return row ? {id: row.dataset.messageId, offset: box(row).top - view.top, following: $("#live-session").dataset.scrollFollowing} : null;
  };
  window.readerPreserved = before => {
    const row = [...$("#live-stream").querySelectorAll("[data-message-id]")].find(node => node.dataset.messageId === before.id);
    return !!row && $("#live-session").dataset.scrollFollowing === "false" && near(box(row).top - box($("#live-stream")).top, before.offset, 3);
  };
})();
