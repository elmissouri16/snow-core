// Production owner/menu/modal regression. Only fixture snapshots and public read
// responses are synthetic: no replacement DOM, renderer, CSS or mutation API.
(async () => {
  "use strict";
  const settleFrame = () => new Promise(resolve => requestAnimationFrame(resolve));
  const fixture = window.harnessFixture, results = [], failures = [], measurements = {};
  const $ = selector => document.querySelector(selector);
  const rect = node => node.getBoundingClientRect();
  const visible = node => !!node && node.getClientRects().length > 0 && getComputedStyle(node).visibility !== "hidden";
  const check = (condition, label) => (condition ? results : failures).push(label);
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  const settle = async (predicate, label) => {
    for (let i = 0; i < 150; i++) { if (predicate()) return; await wait(20); }
    throw new Error(`Production runtime UI did not settle: ${label}`);
  };
  const capture = async name => {
    if (!window.__harnessRuntimeScreenshots) return;
    window.__harnessRuntimeCapture = name;
    await settle(() => !window.__harnessRuntimeCapture, `open ${name} screenshot captured`);
  };
  const inViewport = node => {
    if (!visible(node)) return false;
    const b = rect(node); return b.width > 0 && b.height > 0 && b.left >= -1 && b.right <= innerWidth + 1 && b.top >= -1 && b.bottom <= innerHeight + 1;
  };
  const hit = node => {
    if (!inViewport(node)) return false;
    const b = rect(node), top = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
    return top === node || node.contains(top);
  };
  const focusEvidence = (node = trigger()) => {
    const b = rect(node), top = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
    return {trigger: b.toJSON(), active: document.activeElement?.outerHTML.slice(0, 600), covering: top?.outerHTML.slice(0, 600)};
  };
  const lineCount = node => {
    // Range rects measure rendered glyph lines, not a guessed CSS line-height.
    const walker = document.createTreeWalker(node, NodeFilter.SHOW_TEXT), lines = new Set();
    while (walker.nextNode()) {
      if (!walker.currentNode.textContent.trim() || !visible(walker.currentNode.parentElement)) continue;
      const range = document.createRange(); range.selectNodeContents(walker.currentNode);
      for (const b of range.getClientRects()) if (b.width && b.height) lines.add(Math.round(b.top));
    }
    return lines.size;
  };
  const update = async fields => {
    fixture.update(fields);
    await settle(() => fixture.deliveredRevision === fixture.snapshot.revision, "snapshot delivered");
    await wait(70);
  };
  const trigger = () => $("[data-session-menu]");
  const openMenu = async () => {
    trigger().focus(); trigger().click(); await settleFrame();
    await settle(() => !!$(".conversation-task-menu"), "conversation menu");
    await wait(40);
    return $(".conversation-task-menu");
  };
  const entries = [
    ["versions", "#versions-dialog", "[data-versions-open]", "[data-versions-close]"],
    ["goals", "#goals-dialog", "[data-goals-open]", "[data-goals-close]"],
    ["processes", "#processes-dialog", "[data-processes-open]", "[data-processes-close]"],
    ["reasoning", "#reasoning-dialog", "[data-reasoning-open]", "[data-reasoning-close]"],
    ["steer", "#live-steer-dialog", "[data-steer-open]", "[data-steer-close]"]
  ];
  try {
    await settle(() => document.readyState === "complete" && $("#live-connection")?.textContent === "Live" && fixture.inventoryComplete(), "connected startup");
    await document.fonts.ready;
    check(document.documentElement.dataset.theme === fixture.theme, "Requested theme active");
    check(inViewport(trigger()), "Visible conversation trigger fits viewport");
    check(!$("#managed-processes")?.open, "Managed processes remain closed before explicit menu launch");
    check(fixture.requests.every(r => ["browser-inventory", "sidebar-inventory", "runtime-snapshot"].includes(r.kind)), "Startup performs no panel inspection or mutation");
    const before = JSON.stringify(fixture.snapshot.messages);

    // Both collapsed-rail and expanded navigation labels must belong to their
    // own links. Narrow navigation is explicitly opened through its real owner.
    const sidebarToggle = $(innerWidth < 768 ? "[data-nav-toggle]" : "[data-sidebar-collapse]"), sidebar = $("#project-navigation");
    for (let state = 0; state < 2; state++) {
      if (innerWidth < 768 ? sidebar.inert : state === 1) { sidebarToggle.click(); await settleFrame(); await wait(60); }
      const links = [...document.querySelectorAll(".sidebar-utility")];
      check(links.length >= 2, `Sidebar state ${state}: utility links use owned navigation treatment`);
      for (const link of links) {
        const label = link.querySelector(":scope > span"), icon = link.querySelector("svg");
        if (!visible(link)) continue;
        check(visible(icon), `${link.getAttribute("aria-label") || link.textContent.trim()}: sidebar icon remains visible`);
        check(!!label && visible(label) === (innerWidth < 768 || state === 0), `Sidebar state ${state}: expanded text is visible and collapsed text is hidden`);
        if (visible(label)) {
          const a = rect(link), b = rect(label);
          check(b.left >= a.left - 1 && b.right <= a.right + 1 && b.top >= a.top - 1 && b.bottom <= a.bottom + 1 && lineCount(label) <= 1, `Sidebar ${label.textContent.trim()} label stays inside its link on one line`);
        } else check(!label || getComputedStyle(label).display === "none" || label.hidden, "Collapsed utility label is not rendered outside the rail");
      }
    }
    // Close the mobile overlay; desktop expansion can remain for modal checks.
    if (innerWidth < 768 && !sidebar.inert) { $("[data-nav-close]").click(); await settleFrame(); await wait(60); }

    if (fixture.name === 'runtime-panels') {
      const goal = $('#live-composer [data-goal-toggle]'), prompt = $('#live-prompt'), details = $('[data-goals-open]');
      const originalDraft = prompt.value, originalPlaceholder = prompt.placeholder;
      const draft = 'Unsent shared composer objective';
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(prompt, draft);
      prompt.dispatchEvent(new Event('input', {bubbles: true}));
      prompt.setSelectionRange(3, 9);
      check(document.querySelectorAll('[data-goal-toggle]').length === 1 && !!goal?.closest('.composer-actions') && goal.getAttribute('aria-label') === 'Goal' && goal.textContent.trim() === 'Goal' && goal.getAttribute('aria-pressed') === 'false', 'Goal: one labelled local mode toggle belongs to the composer toolbar');
      check(!!details?.hidden && !visible(details) && !$('[data-goal-objective-draft]') && !$('[data-goal-start]'), 'Goal: hidden secondary details launcher does not duplicate objective input or Start');
      check(!!$('[data-goal-composer-status]') && !$('#live-composer').contains($('[data-goal-composer-status]')), 'Goal: active status has its own host outside the composer form');
      goal.focus(); await settleFrame();
      check(hit(goal) && !!goal.title && !!goal.querySelector('svg[aria-hidden=true]'), 'Goal: labelled toggle and decorative icon remain reachable');
      const goalRequestStart = fixture.requests.length;
      goal.click(); await settle(() => goal.getAttribute('aria-pressed') === 'true', 'local Goal mode enabled');
      check($('#live-prompt') === prompt && document.activeElement === prompt && prompt.placeholder === 'Describe the goal…' && prompt.value === draft && prompt.selectionStart === 3 && prompt.selectionEnd === 9, 'Goal: enabling focuses the same composer with goal guidance and retained draft/selection');
      check(!document.querySelector('dialog[open], :popover-open') && fixture.requests.length === goalRequestStart, 'Goal: enabling opens no dialog, inspection, or work request');
      goal.focus(); goal.click(); await settle(() => goal.getAttribute('aria-pressed') === 'false', 'local Goal mode disabled');
      check($('#live-prompt') === prompt && document.activeElement === prompt && prompt.placeholder === originalPlaceholder && prompt.value === draft && prompt.selectionStart === 3 && prompt.selectionEnd === 9 && fixture.requests.length === goalRequestStart, 'Goal: disabling restores ordinary-message guidance in the same composer without requests');
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set.call(prompt, originalDraft);
      prompt.dispatchEvent(new Event('input', {bubbles: true}));

      const thinking = $('[data-reasoning-open]'), picker = $('#reasoning-picker');
      check(document.querySelectorAll('[data-reasoning-open]').length === 1 && !!thinking?.closest('.composer-actions') && thinking.textContent.includes('Thinking:') && thinking.getAttribute('aria-haspopup') === 'menu' && !!thinking.title, 'Thinking: one labelled current-level dropdown belongs to the composer toolbar');
      thinking.focus(); await settleFrame();
      check(hit(thinking), 'Thinking: composer dropdown is reachable at this viewport');
      const thinkingRequestStart = fixture.requests.length;
      thinking.click();
      await settle(() => picker.matches(':popover-open') && !!picker.querySelector('[data-reasoning-level]:not(:disabled)'), 'direct Thinking capability inspection');
      check(!document.querySelector('dialog[open]') && picker.contains(document.activeElement) && thinking.getAttribute('aria-expanded') === 'true' && $('[data-reasoning-current]').textContent === fixture.snapshot.thinking, 'Thinking: direct launch opens only its focused picker and shows authoritative current effort');
      check(inViewport(picker) && !picker.querySelector('select, [data-reasoning-confirm], [data-reasoning-consent]'), 'Thinking: bounded dropdown has direct levels, not Preference/New Value/Apply');
      check(fixture.requests.slice(thinkingRequestStart).some(r => r.kind === 'runtime-panel-read') && fixture.requests.slice(thinkingRequestStart).every(r => r.kind === 'runtime-panel-read' || r.kind === 'runtime-snapshot'), 'Thinking: opening inspects capabilities without mutation authority');
      picker.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
      await settle(() => !picker.matches(':popover-open') && document.activeElement === thinking, 'Thinking Escape composer focus return');
      check(hit(thinking) && thinking.getAttribute('aria-expanded') === 'false', 'Thinking: Escape returns focus to the visible labelled composer trigger');
    }
    const compact = $('[data-compaction-open]');
    if (fixture.name === 'runtime-panels') {
      check(document.querySelectorAll('[data-compaction-open]').length === 1 && !!compact?.closest('.composer-actions'), 'Manual compact has one launcher in the composer action row');
      check(compact?.getAttribute('aria-label') === 'Compact context' && compact.title.includes('Uses provider tokens') && !!compact.querySelector('svg[aria-hidden=true]') && compact.textContent.trim() === '', 'Direct compaction icon has an accessible name and provider-work tooltip');
      // At 240px the composer has a bounded scroll owner. Keyboard focus, as
      // with native pointer scrolling, brings the action row into view.
      compact.focus();
      check(hit(compact), 'Composer compaction icon is reachable at the current viewport');
      check(!$('#compaction-dialog') && !$('[data-compaction-consent]'), 'Direct compaction has no redundant modal or consent checkbox');
      // This geometry fixture has no mutation authority; native one-click/Stop
      // and exact POST/provider counts are exercised by manager-runtime-controls.
      const previousStatus = fixture.snapshot.status, draft = $('#live-prompt').value;
      // Earlier goal/reasoning inspection uses read-only POSTs. Measure only
      // this busy compaction probe, not unrelated inspection traffic.
      const requestStart = fixture.requests.length;
      await update({status: 'running'});
      check(compact.disabled, 'Direct compaction is disabled while the conversation is busy');
      compact.click();
      check($('#live-prompt').value === draft && !fixture.requests.slice(requestStart).some(r => r.method === 'POST'), 'Busy compaction cannot submit or change the draft');
      await update({status: previousStatus});
      const originalCompaction = fixture.snapshot.compaction, stream = $('#live-stream'), beforeFeedback = rect(stream);
      await update({compaction: {state: 'noop', session_id: fixture.snapshot.session_id, branch_id: fixture.snapshot.goal.branch_id, expected_tip_id: fixture.snapshot.goal.tip_id,
        summarized_messages: 0, retained_messages: 0, progress_done: true, used_fallback: false, request_id: 'fixture-compact-request', compaction_id: 'fixture-compact', turn_id: 'fixture-compact', turn_origin: 'compact', root_epoch: 1, turn_sequence: 1}});
      const feedback = $('[data-compaction-inline]'), dismiss = $('[data-compaction-dismiss]'), afterFeedback = rect(stream);
      check(inViewport(feedback) && hit(dismiss), 'No-op compaction feedback and dismissal remain bounded and reachable');
      check(Math.abs(beforeFeedback.top-afterFeedback.top)<=1 && Math.abs(beforeFeedback.height-afterFeedback.height)<=1, 'First no-op feedback does not resize or move the transcript');
      const beforeDismiss = fixture.requests.length;
      dismiss.click(); await settleFrame();
      check(feedback.hidden && document.activeElement === compact, 'Dismissing compaction feedback hides it and restores its composer control');
      check(fixture.requests.slice(beforeDismiss).every(r => ['runtime-snapshot', 'sidebar-inventory', 'browser-inventory'].includes(r.kind)), 'Dismissal issues no panel or action request; unrelated background reads may finish');
      await update({compaction: originalCompaction});
    } else check(!compact, 'Unsupported manual compaction omits the composer icon');

    const checkHeaderReachability = label => {
      const heading = $("#live-session .workspace-heading"), seat = $("#live-composer-seat");
      check(hit(trigger()), `${label}: conversation menu remains pointer reachable`);
      const handles = [...document.querySelectorAll("#live-session .chat-width-handle")].filter(visible);
      for (const handle of handles) {
        const b = rect(handle);
        check(b.top >= rect(heading).bottom - 1 && b.bottom <= rect(seat).top + 1,
          `${label}: ${handle.dataset.chatWidthHandle} drag handle stays below header and above composer seat`);
      }
      measurements[label] = focusEvidence();
    };
    checkHeaderReachability("initial-header");
    const prompt = $("#live-prompt");
    for (const [label, draft] of [["growing-draft", "Unsent layout review\n".repeat(8)], ["cleared-draft", ""]]) {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value").set.call(prompt, draft);
      prompt.dispatchEvent(new Event("input", {bubbles: true}));
      await wait(90); checkHeaderReachability(label);
    }
    await update({session_name: "Updated public session title"});
    checkHeaderReachability("updated-header");

    let menu = await openMenu();
    if (fixture.name === "runtime-panels") check(menu.querySelector('[data-menu-key="steer"]')?.disabled === true && $("[data-steer-open]").disabled,
      "Supported but idle Steer remains disabled through the resident owner, not enabled by menu presentation");
    const content = menu.querySelector(".snow-menu-content");
    check(inViewport(menu), "Conversation actions menu fits viewport");
    for (const row of menu.querySelectorAll(".snow-menu-row")) check(lineCount(row) <= 2, `${row.textContent.trim()}: menu label does not wrap character-per-line`);
    const rows = [...menu.querySelectorAll('[role="menuitem"]')].filter(row => !row.disabled);
    rows[0].focus(); rows[0].dispatchEvent(new KeyboardEvent("keydown", {key: "End", bubbles: true}));
    await wait(40);
    check(document.activeElement === rows.at(-1) && hit(rows.at(-1)), "End scrolls last enabled conversation action into reachable view");
    if (innerHeight <= 360 && fixture.name === "runtime-panels") check(content.scrollHeight > content.clientHeight && content.scrollTop > 0, "Short conversation menu scrolls internally to its last enabled action");
    if (fixture.name === "runtime-unsupported") {
      for (const [key] of entries) check(!menu.querySelector(`[data-menu-key="${key}"]`), `Unsupported ${key} action absent, not an inert placeholder`);
      window.SnowMenus.close();
      check(document.activeElement === trigger() && hit(trigger()), "Unsupported menu returns focus to visible conversation trigger");
      measurements.returnFocus = focusEvidence();
    } else {
      window.SnowMenus.close();
      for (const [key, dialogSelector, launchSelector, closeSelector] of entries) {
        if (key === "steer") await update({status: "running", steer: {live_steer_token: "fixture-steer", revision: 2, can_steer: true, items: []}});
        menu = await openMenu();
        const row = menu.querySelector(`[data-menu-key="${key}"]`), owner = $(launchSelector), dialog = $(dialogSelector);
        check(!!row && !row.disabled && !!owner && !owner.disabled, `${key}: menu derives enabled state from resident launcher`);
        if (!row || row.disabled) throw new Error(`${key} menu action unavailable`);
        let forwarded = 0;
        const observed = () => forwarded++;
        owner.addEventListener("click", observed);
        row.focus(); row.click(); await settleFrame(); owner.removeEventListener("click", observed);
        check(forwarded === 1 && !$(".conversation-task-menu"), `${key}: menu delegates exactly once to actual owner and closes itself`);
        let returnTarget = trigger();
        if (key === "reasoning") {
          const picker = $('#reasoning-picker');
          await settle(() => picker.matches(':popover-open') && !!picker.querySelector('[data-reasoning-level]:not(:disabled)'), 'menu Thinking capability inspection');
          check(!document.querySelector('dialog[open]') && picker.contains(document.activeElement) && inViewport(picker), 'Thinking: header entry opens the same bounded focused picker, never the advanced modal');
          picker.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
          await settle(() => !picker.matches(':popover-open') && document.activeElement === owner, 'menu Thinking Escape returns to composer');
          check(hit(owner), 'Thinking: menu-origin Escape returns to the visible composer trigger');
          // Reopen through the same menu before the explicitly secondary action.
          menu = await openMenu(); menu.querySelector('[data-menu-key="reasoning"]').click();
          await settle(() => picker.matches(':popover-open') && !!picker.querySelector('[data-reasoning-advanced]:not(:disabled)'), 'secondary response settings available');
          // The dialog adapter selects the header only when it has focus at
          // opening. Native popover dismissal can restore that focus first;
          // capture that exact transition rather than accept any close target.
          let openingFocusObserved = false;
          const openingFocus = event => {
            if (event.newState === 'open') {
              openingFocusObserved = true;
              returnTarget = document.activeElement === trigger() ? trigger() : owner;
            }
          };
          dialog.addEventListener('beforetoggle', openingFocus);
          try { $('[data-reasoning-advanced]').click(); await settle(() => dialog.open, 'explicit secondary response settings'); }
          finally { dialog.removeEventListener('beforetoggle', openingFocus); }
          check(openingFocusObserved && !picker.matches(':popover-open'), 'Thinking: secondary response settings replaces only the picker with a modal and records its exact focus origin');
        }
        await settle(() => dialog.open, `${key} owner dialog`);
        check(document.querySelectorAll("dialog[open]").length === 1 && dialog.contains(document.activeElement), `${key}: only the owner modal is open and owns focus`);
        if (key === "versions") {
          await settle(() => dialog.querySelectorAll("[data-version-preview]").length === 24, "version inventory");
          dialog.querySelector('[data-version-id="fixture-branch-1"]').click(); await settleFrame();
          await settle(() => dialog.querySelectorAll(".version-preview-message").length === 12, "version preview");
        } else if (key === "goals") {
          await settle(() => $("[data-goal-notice]").textContent.includes("Inspection is read-only"), "goal inspection");
          check(!!dialog.querySelector('[data-goal-objective]') && !!dialog.querySelector('[data-goal-token-budget]') && dialog.querySelector('[data-goal-write]')?.textContent === 'Write goal in composer' && !dialog.querySelector('[data-goal-objective-draft], [data-goal-start]'), 'Goal details: read-only objective and optional budget route new goals back to the composer');
          check($('[data-goal-confirmation]').hidden && !$('[data-goal-consent]').checked && $('[data-goal-confirm]').disabled, 'Goal details: browsing does not authorize a resume or bypass its confirmation');
        } else if (key === "reasoning") {
          await settle(() => $("[data-reasoning-field]").options.length === 2, "secondary response choices");
          check([...$('[data-reasoning-field]').options].map(option => option.value).join(',') === 'reasoning_summary,text_verbosity', 'Response settings: only model-owned summary and verbosity preferences, never Thinking levels');
        } else if (key === "processes") {
          check($("#managed-processes").open && dialog.contains($("#managed-processes")), "Processes opens the existing details owner within its modal");
          await settle(() => dialog.querySelectorAll("[data-process-logs]").length === 20, "process inventory");
          dialog.querySelector("[data-process-logs]").click(); await settleFrame();
          await settle(() => !$("[data-process-log-panel]").hidden && $("[data-process-output]").textContent.includes("Public bounded"), "bounded process logs");
        }
        await wait(60);
        const heading = dialog.querySelector(":scope > .dialog-heading"), body = dialog.querySelector(":scope > .runtime-dialog-body"), close = dialog.querySelector(closeSelector);
        check(dialog.classList.contains("runtime-dialog") && !!heading && !!body, `${key}: shared persistent heading and independent body scroll owner`);
        if (!heading || !body) throw new Error(`${key}: missing dialog layout contract`);
        check(inViewport(dialog) && hit(close) && close.classList.contains("icon-button") && !!close.getAttribute("aria-label"), `${key}: bounded modal and named icon Close are pointer reachable`);
        check(lineCount(heading.querySelector("h2")) <= 2, `${key}: heading remains readable, never character-per-line`);
        for (const button of dialog.querySelectorAll("button:not(.icon-button):not(.version-choice)")) if (visible(button)) {
          check(lineCount(button) <= 2 && button.scrollWidth <= button.clientWidth + 1, `${key}: ${button.textContent.trim()} action uses readable text geometry`);
        }
        check(body.scrollWidth <= body.clientWidth + 1 && dialog.scrollWidth <= dialog.clientWidth + 1, `${key}: no horizontal inner overflow`);
        if (innerHeight === 240) check(body.scrollHeight > body.clientHeight, `${key}: short-height content overflows only the inner body`);
        await capture(`${key}-open`);
        const top = rect(heading).top;
        body.scrollTop = body.scrollHeight;
        for (const scroller of body.querySelectorAll(".versions-list, .versions-preview-messages, [data-process-output]")) scroller.scrollTop = scroller.scrollHeight;
        await wait(40);
        check(Math.abs(rect(heading).top - top) <= 1 && hit(close) && dialog.scrollTop === 0, `${key}: scrolling body leaves header Close visible and hit-testable`);
        measurements[key] = {dialog: rect(dialog).toJSON(), bodyHeight: body.clientHeight, bodyScrollHeight: body.scrollHeight, bodyScrollTop: body.scrollTop};
        await capture(`${key}-scrolled`);
        close.click(); await settleFrame();
        await settle(() => !dialog.open && document.activeElement === returnTarget, `${key} close/exact-focus return`);
        await wait(40); // Native dialog close listeners run in a subsequent task.
        check(hit(returnTarget), `${key}: modal returns focus to its exact visible opener, never a hidden launcher`);
        measurements[key].returnFocus = focusEvidence(returnTarget);
        if (key === "processes") check(!$("#managed-processes").open, "Closing Processes also closes details and retires visible polling");
      }
    }
    check(JSON.stringify(fixture.snapshot.messages) === before, "Menu browsing, previews, scrolling and close do not mutate the transcript");
    check(fixture.requests.every(r => ["browser-inventory", "sidebar-inventory", "runtime-snapshot", "runtime-panel-read"].includes(r.kind)), "Every request is an explicitly allowed public read; no mutation authority");
    check(document.documentElement.scrollWidth <= innerWidth + 1 && document.body.scrollWidth <= innerWidth + 1, "Runtime controls do not introduce horizontal page overflow");
  } catch (error) { failures.push(error.stack || String(error)); }
  check(fixture.errors.length === 0, `No uncaught browser errors or rejected/unmocked requests: ${fixture.errors.join("; ")}`);
  return {name: fixture.name, width: innerWidth, height: innerHeight, theme: fixture.theme, passed: results.length, results, failures, measurements, requests: fixture.requests};
})();
