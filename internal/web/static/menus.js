(() => {
  "use strict";
  // Shared presentation only: runtime authority and menu content stay with owners.
  let current, sequence = 0;
  const items = panel => [...panel.querySelectorAll('[role="menuitem"], [role="menuitemradio"], [role="menuitemcheckbox"]')].filter(node => !node.disabled && node.getAttribute("aria-disabled") !== "true" && !node.hidden && node.getClientRects().length);
  function close({restoreFocus = true} = {}) {
    const menu = current;
    if (!menu) return;
    current = null;
    menu.listeners.abort(); menu.observer?.disconnect();
    if (!menu.managedTrigger) {
      menu.trigger.setAttribute("aria-expanded", "false");
      menu.trigger.removeAttribute("aria-controls");
    }
    menu.panel.remove();
    menu.onClose?.();
    if (restoreFocus && menu.trigger.isConnected) menu.trigger.focus({preventScroll: true});
  }
  function contentViewport(menu) {
    // Owners may replace menu children while a request is pending. Rebuild the
    // presentation wrapper without changing their nodes, actions or scroll state.
    if (menu.content?.parentNode === menu.panel) return;
    menu.content = menu.panel.querySelector(":scope > .snow-menu-content") || menu.content;
    if (menu.content?.parentNode === menu.panel) return;
    const scroll = menu.content?.scrollTop || 0;
    const content = document.createElement("div");
    content.className = "snow-menu-content";
    content.tabIndex = -1;
    for (const child of [...menu.panel.childNodes]) {
      if (!(child instanceof Element) || (!child.classList.contains("snow-menu-footer") && !child.classList.contains("snow-menu-header"))) content.append(child);
    }
    content.querySelectorAll(".snow-menu-group-label").forEach(label => { label.title ||= label.textContent; });
    const header = menu.panel.querySelector(":scope > .snow-menu-header");
    if (header) header.after(content); else menu.panel.prepend(content);
    menu.content = content;
    content.scrollTop = scroll;
  }
  function revealFocused(menu) {
    const row = document.activeElement, content = menu.content;
    if (!content?.contains(row) || row === content) return;
    const bounds = content.getBoundingClientRect(), rect = row.getBoundingClientRect();
    // Scroll only the menu, never the document or its anchored conversation.
    const heading = row.closest(".snow-menu-group")?.querySelector(".snow-menu-group-label");
    const top = bounds.top + (heading?.offsetHeight || 0);
    if (rect.top < top) content.scrollTop -= top - rect.top;
    else if (rect.bottom > bounds.bottom) content.scrollTop += rect.bottom - bounds.bottom;
  }
  function focusRow(menu, row) {
    (row || menu.panel).focus({preventScroll: true});
    revealFocused(menu);
  }
  function reposition(event) {
    const menu = current;
    if (!menu) return;
    if (!menu.trigger.isConnected) { close({restoreFocus: false}); return; }
    contentViewport(menu);
    const viewport = window.visualViewport;
    const left = viewport?.offsetLeft || 0, top = viewport?.offsetTop || 0;
    const width = viewport?.width || window.innerWidth, height = viewport?.height || window.innerHeight;
    const margin = 12, gap = 8, rect = menu.trigger.getBoundingClientRect();
    menu.panel.style.minWidth = `${Math.max(0, Math.min(menu.panel.classList.contains("telemetry-menu") ? 280 : 240, width - margin * 2))}px`;
    menu.panel.style.maxWidth = `${Math.max(0, Math.min(420, width - margin * 2))}px`;
    const clearance = menu.panel.classList.contains("model-picker-menu") ? margin * 2 : 96;
    menu.panel.style.maxHeight = `${Math.max(0, Math.min(360, height - clearance))}px`;
    const w = menu.panel.offsetWidth, h = menu.panel.offsetHeight;
    const alignEnd = menu.placement.endsWith("end");
    let x = alignEnd ? rect.right - w : rect.left;
    let y = menu.placement.startsWith("top") ? rect.top - gap - h : rect.bottom + gap;
    x = Math.max(left + margin, Math.min(x, left + width - w - margin));
    y = Math.max(top + margin, Math.min(y, top + height - h - margin));
    menu.panel.style.left = `${x}px`; menu.panel.style.top = `${y}px`;
    menu.panel.style.visibility = "visible";
    // Background React repaints and ResizeObserver deliveries must not pull
    // a pointer-scrolled menu back to an older keyboard focus target. Explicit
    // focus/navigation already reveals its row; viewport resize still does so.
    if (event?.type === "resize") revealFocused(menu);
  }
  function open({trigger, panel, placement = "bottom-end", onClose, onBack, managedTrigger = false}) {
    if (!trigger?.isConnected || !panel || trigger.disabled) return;
    if (current?.trigger === trigger) { close(); return; }
    close({restoreFocus: false});
    const listeners = new AbortController(), options = {signal: listeners.signal};
    const menu = current = {trigger, panel, placement, onClose, onBack, listeners, managedTrigger};
    panel.classList.add("snow-menu"); panel.id ||= `snow-menu-${++sequence}`;
    panel.setAttribute("role", panel.getAttribute("role") || "menu"); panel.tabIndex = -1;
    panel.style.visibility = "hidden";
    if (!managedTrigger) { trigger.setAttribute("aria-expanded", "true"); trigger.setAttribute("aria-controls", panel.id); }
    document.body.append(panel); reposition();
    focusRow(menu, panel.querySelector("[data-menu-autofocus]") || items(panel)[0]);
    document.addEventListener("pointerdown", event => {
      if (!panel.contains(event.target) && !trigger.contains(event.target)) close({restoreFocus: false});
    }, {...options, capture: true});
    document.addEventListener("focusin", event => {
      if (!panel.contains(event.target) && !trigger.contains(event.target)) close({restoreFocus: false});
      else revealFocused(menu);
    }, options);
    document.addEventListener("keydown", event => {
      if (event.isComposing) return;
      if (event.key === "Escape") {
        event.preventDefault(); event.stopPropagation();
        if (menu.onBack?.()) { reposition(); focusRow(menu, items(panel)[0]); }
        else close();
        return;
      }
      if (event.key === "Tab") { close(); return; }
      if (!panel.contains(event.target)) return;
      const rows = items(panel), index = rows.indexOf(document.activeElement);
      const search = panel.querySelector("[data-menu-autofocus]");
      if (event.target.matches("input,textarea") && event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
      if (search && event.key === "ArrowUp" && index === 0) {
        event.preventDefault(); search.focus({preventScroll: true}); return;
      }
      // Read-only usage and trailing explanatory notes must also be reachable
      // without a pointer, even when there are no actionable menu rows.
      const content = menu.content;
      if (event.key === "PageDown" || event.key === "PageUp" || (!rows.length && ["ArrowDown", "ArrowUp", "Home", "End", " "].includes(event.key))) {
        event.preventDefault();
        if (event.key === "Home") content.scrollTop = 0;
        else if (event.key === "End") content.scrollTop = content.scrollHeight;
        else content.scrollTop += (event.key === "ArrowUp" || event.key === "PageUp" || (event.key === " " && event.shiftKey) ? -1 : 1) * (event.key.startsWith("Arrow") ? 40 : content.clientHeight);
        return;
      }
      let next;
      if (event.key === "ArrowDown") next = (index + 1) % rows.length;
      else if (event.key === "ArrowUp") next = index < 0 ? rows.length - 1 : (index - 1 + rows.length) % rows.length;
      else if (event.key === "Home") next = 0;
      else if (event.key === "End") next = rows.length - 1;
      else return;
      event.preventDefault(); focusRow(menu, rows[next]);
    }, {...options, capture: true});
    window.addEventListener("resize", reposition, options);
    window.addEventListener("scroll", reposition, {...options, capture: true});
    window.visualViewport?.addEventListener("resize", reposition, options);
    window.visualViewport?.addEventListener("scroll", reposition, options);
    if (window.ResizeObserver) { menu.observer = new ResizeObserver(reposition); menu.observer.observe(panel); menu.observer.observe(trigger); }
    document.addEventListener("snow:navigation-before-swap", () => close({restoreFocus: false}), options);
  }
  // Menu owners supply fresh descriptions, but unchanged controls remain live.
  // Keys are local to each parent and each menu lifetime, never across sessions.
  function reconcile(panel, fragment) {
    const content = document.createElement("div"); content.className = "snow-menu-content"; content.tabIndex = -1;
    const desired = document.createDocumentFragment();
    for (const child of [...fragment.childNodes]) {
      if (child.nodeType === 1 && child.classList.contains("snow-menu-header")) desired.append(child);
      else if (!(child.nodeType === 1 && child.classList.contains("snow-menu-footer"))) content.append(child);
    }
    desired.append(content);
    for (const child of [...fragment.childNodes]) desired.append(child);
    const key = node => node.nodeType === 1 ? `${node.tagName}:${node.dataset.menuKey ?? (node.id || node.getAttribute("aria-label") || node.className)}` : `node:${node.nodeType}`;
    function patch(parent, next) {
      const existing = new Map();
      for (const child of parent.childNodes) { const id = key(child); if (!existing.has(id)) existing.set(id, []); existing.get(id).push(child); }
      const keep = new Set();
      let at = parent.firstChild;
      for (const source of [...next.childNodes]) {
        const target = existing.get(key(source))?.shift() || source;
        if (target !== source) {
          if (target.nodeType === 1) {
            for (const attr of [...target.attributes]) if (!source.hasAttribute(attr.name)) target.removeAttribute(attr.name);
            for (const attr of source.attributes) if (target.getAttribute(attr.name) !== attr.value) target.setAttribute(attr.name, attr.value);
            // A retained row must invoke today's validated action, not a stale
            // closure from a previous inventory or permission snapshot.
            if (Object.hasOwn(source, "_snowMenuAction")) target._snowMenuAction = source._snowMenuAction;
            patch(target, source);
          } else if (target.nodeValue !== source.nodeValue) target.nodeValue = source.nodeValue;
        }
        keep.add(target);
        if (target !== at) {
          if (parent.moveBefore && target.isConnected) parent.moveBefore(target, at);
          else parent.insertBefore(target, at);
        }
        at = target.nextSibling;
      }
      for (const child of [...parent.childNodes]) if (!keep.has(child)) child.remove();
    }
    patch(panel, desired);
    if (current?.panel === panel) current.content = panel.querySelector(":scope > .snow-menu-content");
  }
  const dispose = () => close({restoreFocus: false});
  window.SnowMenus = Object.freeze({open, close, reposition, reconcile, dispose});
})();
