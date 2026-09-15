/* Pending attention presentation, adapted from Harness QuestionComposer and
 * ApprovalPanel; see HARNESS-NOTICE.txt. app.js alone owns runtime actions.
 * init({root, project, session, instance, nonce, onLayout}),
 * render(snapshot, {safe, busy, canStop}), dispose({clearDrafts = false}).
 * The native form/radio/textarea and permission/abort data hooks deliberately
 * remain compatible with app.js's delegated action and CSRF handling. */
(() => {
  "use strict";
  const stores = new Map(), MAX_STORES = 8, MAX_AGE = 30 * 60 * 1000;
  const pageNonce = globalThis.crypto?.randomUUID?.() || String(Date.now());
  let view = null;
  const node = (tag, className = "", text = "") => {
    const el = document.createElement(tag); el.className = className; el.textContent = text; return el;
  };
  const button = (label, className = "quiet") => {
    const el = node("button", className, label); el.type = "button"; return el;
  };
  function trimStores() {
    for (const [key, value] of stores) if (Date.now() - value.updated > MAX_AGE) stores.delete(key);
    while (stores.size > MAX_STORES) stores.delete(stores.keys().next().value);
  }
  function save() {
    if (!view?.draft) return;
    view.draft.updated = Date.now();
    stores.delete(view.scope); stores.set(view.scope, view.draft); trimStores();
  }
  function listen(target, name, fn, capture = false) {
    target.addEventListener(name, fn, {capture, signal: view.controller.signal});
  }
  function init(options = {}) {
    dispose();
    const root = options.root || document.querySelector('#live-session[data-runtime="true"]');
    const region = root?.querySelector("#live-attention"), seat = root?.querySelector("#live-composer-seat");
    if (!region || !seat) return;
    const identity = [options.project ?? root.dataset.project, options.session ?? root.dataset.session,
      options.instance ?? root.dataset.instance];
    view = {root, region, seat, normal: root.querySelector("#live-composer-normal"), identity,
      scope: JSON.stringify([options.nonce ?? pageNonce, ...identity]), onLayout: options.onLayout,
      controller: new AbortController(), layoutFrame: 0, layoutMetrics: null, key: "", safe: false, busy: false, canStop: false, draft: null};
    // A worker replacement must not resurrect another instance's answers.
    for (const [key, value] of stores) {
      if (value.identity[0] === identity[0] && value.identity[1] === identity[1] && value.identity[2] !== identity[2]) stores.delete(key);
    }
    trimStores();
    listen(region, "click", onClick);
    listen(region, "change", onChange);
    listen(region, "input", onInput);
    listen(region, "focusin", onFocus);
    listen(region, "keydown", onKeydown);
    listen(region, "compositionstart", () => { view.composing = true; });
    listen(region, "compositionend", () => { view.composing = false; });
    // Capture invalid/unsafe submissions before the document action delegate.
    listen(region, "submit", onSubmit, true);
    listen(window, "resize", layout);
    if (window.visualViewport) { listen(window.visualViewport, "resize", layout); listen(window.visualViewport, "scroll", layout); }
    if (globalThis.ResizeObserver) {
      view.observer = new ResizeObserver(layout);
      view.observer.observe(root);
      for (const child of root.children) if (child !== seat && child.id !== "live-stream" && child.tagName !== "DIALOG") view.observer.observe(child);
    }
  }
  // ResizeObserver only schedules work. Neither our CSS writes nor the parent's
  // scroll reconciliation may run in its delivery phase: both can resize an
  // observed ancestor and otherwise produce an undelivered-notification loop.
  function layout() {
    if (!view || view.region.hidden || view.layoutFrame) return;
    const current = view;
    current.layoutFrame = requestAnimationFrame(() => {
      current.layoutFrame = 0;
      if (view === current && !current.region.hidden) measureLayout(current);
    });
  }
  function measureLayout(current) {
    const {root, seat} = current, rect = root.getBoundingClientRect();
    const viewportBottom = window.visualViewport ? window.visualViewport.offsetTop + window.visualViewport.height : window.innerHeight;
    let remaining = Math.min(rect.bottom, viewportBottom) - rect.top;
    for (const child of root.children) {
      if (child === seat || child.id === "live-stream") continue;
      const style = getComputedStyle(child);
      if (style.display === "none" || style.position === "absolute" || style.position === "fixed") continue;
      remaining -= child.getBoundingClientRect().height + (parseFloat(style.marginTop) || 0) + (parseFloat(style.marginBottom) || 0);
    }
    const compact = remaining < 180 || (window.visualViewport?.height || window.innerHeight) <= 420;
    const compactChanged = root.classList.contains("attention-compact") !== compact;
    if (compactChanged) root.classList.toggle("attention-compact", compact);
    const stream = root.querySelector("#live-stream");
    if (stream) {
      const style = getComputedStyle(stream);
      for (const property of ["paddingTop", "paddingBottom", "borderTopWidth", "borderBottomWidth"]) remaining -= parseFloat(style[property]) || 0;
    }
    // When safety/review warnings consume the viewport, retain them and allow
    // the outer runtime to scroll rather than clipping the fixed action footer.
    const constrained = remaining < 96;
    const constrainedChanged = root.classList.contains("attention-constrained") !== constrained;
    if (constrainedChanged) root.classList.toggle("attention-constrained", constrained);
    const height = Math.max(96, Math.floor(remaining));
    const previousHeight = parseFloat(seat.style.getPropertyValue("--attention-seat-height"));
    if (!Number.isFinite(previousHeight) || Math.abs(previousHeight - height) > 0.5) {
      seat.style.setProperty("--attention-seat-height", height + "px");
    }
    const bounds = seat.getBoundingClientRect();
    const metrics = [bounds.top, bounds.width, bounds.height, rect.height];
    const changed = !current.layoutMetrics || metrics.some((value, index) => Math.abs(value - current.layoutMetrics[index]) > 0.5);
    if (changed || compactChanged || constrainedChanged) {
      current.layoutMetrics = metrics;
      current.onLayout?.();
    }
  }
  function takeover(active) {
    if (view.region.hidden === active) view.region.hidden = !active;
    if (!active) view.layoutMetrics = null;
    if (!active) view.root.classList.remove("attention-compact", "attention-constrained");
    if (view.seat.dataset.attention !== String(active)) view.seat.dataset.attention = String(active);
    if (view.normal) {
      if (view.normal.hidden !== active) view.normal.hidden = active;
      if (view.normal.inert !== active) view.normal.inert = active;
    }
  }
  function render(snapshot, state = {}) {
    if (!view) return;
    view.safe = state.safe === true && !state.busy;
    view.busy = !!state.busy;
    view.canStop = state.canStop === true;
    view.stopping = state.stopping === true;
    if (!snapshot) { controls(); return; }
    const matches = [snapshot.project_id, snapshot.session_id, snapshot.instance_id].every((id, index) => id === undefined || id === view.identity[index]);
    if (!matches) {
      stores.delete(view.scope); view.safe = false; controls(); return;
    }
    const pending = snapshot.permission || snapshot.input;
    if (!pending) {
      stores.delete(view.scope); view.draft = null; view.key = "";
      const wasActive = !view.region.hidden;
      view.region.replaceChildren(); takeover(false); if (wasActive) view.onLayout?.(); return;
    }
    const kind = snapshot.permission ? "permission" : "input";
    const key = JSON.stringify([kind, pending.id, pending]);
    if (key !== view.key || !view.region.firstElementChild) {
      view.key = key; view.kind = kind; view.pending = pending; view.composing = false;
      view.region.replaceChildren();
      const prior = stores.get(view.scope);
      view.draft = prior?.key === key ? prior : {key, identity: view.identity, page: 0, collapsed: false, answers: Object.create(null), updated: Date.now()};
      save();
      if (kind === "input") questions(pending); else permission(pending);
    }
    takeover(true); controls(); layout();
  }
  function stopButton() {
    const stop = button("Stop turn", "attention-stop quiet"); stop.dataset.runtimeAbort = "";
    stop.title = "Stop the current turn without answering"; return stop;
  }
  function questions(pending) {
    const items = pending.questions;
    view.valid = Array.isArray(items) && items.length > 0 && items.length <= 16 &&
      new Set(items.map(q => q.id)).size === items.length && items.every(q => typeof q.id === "string" && q.id &&
        typeof q.question === "string" && (q.options === undefined || Array.isArray(q.options)) &&
        (q.options || []).length <= 32 && (q.options || []).every(o => typeof o.label === "string" && o.label.trim()) &&
        (!q.choices_only || q.options?.length));
    const form = node("form", "attention-card attention-questions"); form.dataset.runtimeInput = "";
    form.dataset.requestId = pending.id; form.noValidate = true;
    const header = node("header", "attention-header"), heading = node("div", "attention-heading");
    heading.append(node("div", "attention-eyebrow", "YOUR INPUT NEEDED"));
    const title = node("h2", "attention-title"); title.id = "attention-question-title"; heading.append(title);
    const actions = node("div", "attention-header-actions"), collapse = button("−", "attention-icon quiet");
    collapse.dataset.attentionCollapse = ""; collapse.setAttribute("aria-controls", "attention-question-body attention-question-footer");
    actions.append(collapse, stopButton()); header.append(heading, actions);
    const body = node("div", "attention-body"); body.id = "attention-question-body";
    const feedback = node("p", "attention-feedback"); feedback.id = "attention-feedback"; feedback.setAttribute("role", "status"); feedback.hidden = true;
    body.append(feedback);
    if (!view.valid) {
      title.textContent = "Question unavailable";
      body.append(node("p", "attention-detail", "This question batch is incomplete or unsupported. Stop the turn or review the workspace; no answers can be sent."));
    } else {
      for (const [index, q] of items.entries()) body.append(questionField(q, index));
    }
    const footer = node("footer", "attention-footer"); footer.id = "attention-question-footer";
    const pager = node("div", "attention-pager"), prev = button("‹", "attention-icon quiet"), next = button("›", "attention-icon quiet");
    prev.dataset.attentionPage = "-1"; next.dataset.attentionPage = "1";
    prev.setAttribute("aria-label", "Previous question"); next.setAttribute("aria-label", "Next question");
    const progress = node("span", "attention-progress"); progress.setAttribute("aria-live", "polite");
    pager.append(prev, progress, next);
    const send = button("Next", "attention-continue primary"); send.dataset.attentionContinue = "";
    footer.append(pager, send); form.append(header, body, footer); view.region.append(form);
    view.form = form; view.body = body; view.footer = footer; view.title = title;
    view.feedback = feedback; view.continue = send; view.progress = progress; view.collapse = collapse;
    showPage(false);
  }
  function questionField(q, index) {
    const field = node("fieldset", "attention-question"); field.dataset.questionId = q.id;
    const legend = node("legend", "sr-only", q.header || `Question ${index + 1}`);
    const detail = node("p", "attention-detail", q.question); detail.id = `attention-detail-${index}`;
    field.setAttribute("aria-describedby", detail.id); field.append(legend, detail);
    const choices = node("div", "attention-options"), options = q.options || [];
    const draft = view.draft.answers[q.id] || {selected: null, custom: ""};
    for (const [i, option] of options.entries()) {
      const row = node("label", "attention-option checkbox-label"), radio = node("input", "attention-radio");
      radio.type = "radio"; radio.name = `question-${index}`; radio.id = `question-${index}-option-${i}`;
      radio.value = option.label; radio.dataset.optionIndex = String(i); radio.checked = draft.selected === i;
      const number = node("span", "attention-number", String(i + 1)); number.setAttribute("aria-hidden", "true");
      const copy = node("span", "attention-option-copy"), line = node("span", "attention-option-line");
      const suffix = /\s*(?:\((?:recommended|推荐)\)|（(?:recommended|推荐)）)\s*$/i;
      const recommended = suffix.test(option.label);
      line.append(node("span", "attention-option-label", recommended ? option.label.replace(suffix, "") : option.label));
      if (recommended) line.append(node("span", "attention-badge", "Recommended"));
      copy.append(line);
      if (option.description) copy.append(node("span", "attention-description", option.description));
      row.append(radio, number, copy); choices.append(row);
    }
    if (!q.choices_only) {
      const row = node("div", "attention-custom" + (!options.length ? " attention-custom-block" : ""));
      const label = node("label", "attention-custom-label");
      label.htmlFor = `question-${index}-answer`; label.title = "Your answer";
      if (options.length) {
        const radio = node("input", "attention-radio"); radio.type = "radio"; radio.name = `question-${index}`;
        radio.value = ""; radio.dataset.other = "true"; radio.checked = draft.selected === "custom";
        radio.setAttribute("aria-label", "Other: write your own answer"); row.append(radio);
        const number = node("span", "attention-number", String(options.length + 1)); number.setAttribute("aria-hidden", "true"); label.append(number);
      }
      label.append(node("span", "sr-only", "Your answer"));
      const wrap = node("div", "attention-answer-field"), mirror = node("div", "attention-answer-mirror", draft.custom + "\n");
      mirror.setAttribute("aria-hidden", "true");
      const input = node("textarea", "attention-answer"); input.id = label.htmlFor; input.rows = 1; input.maxLength = 8192;
      input.placeholder = options.length ? "Other: write your own answer…" : "Write your answer…"; input.value = draft.custom;
      input.setAttribute("aria-describedby", detail.id); wrap.append(mirror, input); row.append(label, wrap); choices.append(row);
    }
    field.append(choices); return field;
  }
  function permission(pending) {
    const truncated = pending.truncated || (pending.paths || []).length > 64 || (pending.effects || []).length > 64;
    const card = node("section", "attention-card attention-permission");
    const strip = node("div", "attention-warning", truncated ? "Incomplete summary · approval blocked" : "APPROVAL REQUIRED");
    const body = node("div", "attention-body attention-permission-body");
    body.tabIndex = 0; body.setAttribute("role", "group"); body.setAttribute("aria-label", "Permission details");
    body.append(node("h2", "attention-title", `${pending.tool || "Tool"} · ${pending.risk || "unspecified risk"}`));
    if (pending.reason) body.append(node("p", "", pending.reason));
    if (pending.scope_label) body.append(node("p", "", pending.scope_label));
    for (const path of (pending.paths || []).slice(0, 64)) body.append(node("pre", "", path));
    if (pending.capabilities?.length) body.append(node("p", "", "Capabilities: " + pending.capabilities.join(", ")));
    for (const effect of (pending.effects || []).slice(0, 64)) body.append(node("pre", "", [effect.type, effect.capability, effect.operation, effect.resource, effect.command, effect.reason].filter(Boolean).join(" · ")));
    if (truncated) body.append(node("p", "attention-authority-warning", "This summary is incomplete and cannot be approved. Reject or stop the turn."));
    if (pending.unknown || truncated) body.append(node("p", "attention-authority-warning", "Some effects are unknown or omitted."));
    // Authority is not hidden behind a scroll fold or a collapsed question.
    const warning = node("p", "attention-authority", "Grants host authority, not sandboxed execution.");
    const footer = node("footer", "attention-footer attention-permission-actions"); footer.append(stopButton());
    for (const [decision, label] of [["deny", "Reject"], ["allow", "Allow once"]]) {
      const action = button(label, decision === "allow" ? "primary" : "quiet danger");
      action.dataset.permission = decision; action.dataset.requestId = pending.id;
      if (decision === "allow" && truncated) { action.dataset.blocked = "true"; action.title = "Incomplete permission summaries cannot be approved. Reject or stop this turn."; }
      footer.append(action);
    }
    card.append(strip, body, warning, footer); view.region.append(card);
  }
  function controls() {
    if (!view) return;
    const locked = !view.safe;
    view.region.setAttribute("aria-busy", String(view.busy));
    for (const el of view.region.querySelectorAll("input, textarea, button, fieldset")) {
      el.disabled = locked || el.dataset.blocked === "true";
      if (el.matches("[data-runtime-abort]")) {
        el.disabled = !view.canStop;
        el.textContent = view.stopping ? "Stopping…" : "Stop turn";
        el.setAttribute("aria-label", el.textContent);
      }
    }
    if (view.kind !== "input" || !view.form?.isConnected) return;
    view.continue.disabled ||= !view.valid;
    const count = view.pending.questions?.length || 0;
    for (const el of view.region.querySelectorAll("[data-attention-page]")) el.disabled ||= !view.valid ||
      (Number(el.dataset.attentionPage) < 0 ? view.draft.page === 0 : view.draft.page >= count - 1);
    view.collapse.setAttribute("aria-expanded", String(!view.draft.collapsed));
    view.collapse.setAttribute("aria-label", view.draft.collapsed ? "Expand questions" : "Collapse questions");
    view.collapse.textContent = view.draft.collapsed ? "+" : "−";
  }
  function showPage(focus) {
    if (!view?.form) return;
    const {draft, pending} = view, fields = [...view.form.querySelectorAll("fieldset")];
    fields.forEach((field, index) => { field.hidden = index !== draft.page; });
    if (view.valid) {
      view.title.textContent = pending.questions[draft.page].header || `Question ${draft.page + 1}`;
      view.progress.textContent = `${draft.page + 1} / ${fields.length}`;
      const last = draft.page === fields.length - 1;
      view.continue.textContent = last ? "Submit answers" : "Next"; view.continue.type = last ? "submit" : "button";
    }
    view.body.hidden = view.footer.hidden = draft.collapsed;
    view.form.classList.toggle("attention-collapsed", draft.collapsed);
    view.body.scrollTop = 0;
    if (focus && !draft.collapsed) {
      const field = fields[draft.page];
      (field?.querySelector("input:checked") || field?.querySelector("input, textarea") || view.collapse).focus({preventScroll: true});
    }
    controls(); save(); layout();
  }
  function remember(field) {
    if (!field) return;
    const radio = field.querySelector("input:checked"), text = field.querySelector("textarea");
    view.draft.answers[field.dataset.questionId] = {selected: radio ? radio.dataset.other ? "custom" : Number(radio.dataset.optionIndex) : null, custom: text?.value || ""};
    const mirror = field.querySelector(".attention-answer-mirror"); if (mirror) mirror.textContent = (text?.value || "") + "\n";
    field.querySelector(".attention-custom")?.classList.toggle("attention-custom-active", !!text && (!radio || !!radio.dataset.other));
    save();
  }
  function answer(field) {
    const selected = field.querySelector('input[type="radio"]:checked');
    return selected && !selected.dataset.other ? selected.value : field.querySelector("textarea")?.value || "";
  }
  function validate(all = false) {
    if (!view.safe || !view.valid) return false;
    const fields = [...view.form.querySelectorAll("fieldset")];
    const invalid = all ? fields.findIndex(field => !answer(field).trim()) : !answer(fields[view.draft.page]).trim() ? view.draft.page : -1;
    if (invalid < 0) { view.feedback.hidden = true; return true; }
    view.draft.page = invalid; view.draft.collapsed = false; showPage(true);
    view.feedback.textContent = all ? "Answer every question before submitting." : "Choose an option or enter an answer to continue.";
    view.feedback.hidden = false; view.body.scrollTop = 0;
    return false;
  }
  function advance() {
    if (!validate()) return;
    if (view.draft.page < view.pending.questions.length - 1) { view.draft.page++; showPage(true); }
    else view.form.requestSubmit(view.continue);
  }
  function onClick(event) {
    const target = event.target.closest("button");
    if (!target) return;
    if (target.disabled || (target.matches("[data-runtime-abort]") ? !view.canStop : !view.safe)) {
      event.preventDefault(); event.stopImmediatePropagation(); return;
    }
    if (target.matches("[data-attention-collapse]")) {
      view.draft.collapsed = !view.draft.collapsed; showPage(false);
    } else if (target.matches("[data-attention-page]")) {
      view.feedback.hidden = true; view.draft.page += Number(target.dataset.attentionPage); showPage(true);
    } else if (target.matches('[data-attention-continue][type="button"]')) advance();
  }
  function onChange(event) {
    if (!view.safe || !event.target.matches('input[type="radio"]')) return;
    const field = event.target.closest("fieldset"); remember(field); view.feedback.hidden = true;
    if (event.target.dataset.other) field.querySelector("textarea")?.focus({preventScroll: true});
    else if (view.draft.page < view.pending.questions.length - 1) { view.draft.page++; showPage(true); }
  }
  function onFocus(event) {
    if (!view.safe || !event.target.matches("textarea")) return;
    const field = event.target.closest("fieldset"), other = field.querySelector("input[data-other]");
    if (other) other.checked = true;
    remember(field);
  }
  function onInput(event) {
    if (!view.safe || !event.target.matches("textarea")) return;
    const field = event.target.closest("fieldset"), other = field.querySelector("input[data-other]");
    if (other) other.checked = true;
    remember(field); view.feedback.hidden = true;
  }
  function onKeydown(event) {
    if (!event.target.matches("textarea") || event.key !== "Enter" || event.shiftKey || event.isComposing || event.keyCode === 229 || view.composing) return;
    event.preventDefault();
    if (view.safe) advance();
  }
  function onSubmit(event) {
    if (!event.target.matches("[data-runtime-input]")) return;
    if (!validate(true)) { event.preventDefault(); event.stopImmediatePropagation(); }
  }
  function dispose({clearDrafts = false} = {}) {
    if (view) {
      save(); view.controller.abort(); view.observer?.disconnect();
      if (view.layoutFrame) cancelAnimationFrame(view.layoutFrame);
      view.seat.style.removeProperty("--attention-seat-height");
      takeover(false); view.region.replaceChildren(); view = null;
    }
    if (clearDrafts) stores.clear(); else trimStores();
  }
  window.SnowAttention = {init, render, dispose};
})();
