// Every state below opens a real production control on an exported Go page.
// The runner captures the result before closing it, including short viewports.
(async () => {
  "use strict";
  const state = window.__harnessControl, fixture = window.harnessFixture;
  const results = [], failures = [], measurements = {};
  const $ = selector => document.querySelector(selector);
  const check = (value, label) => (value ? results : failures).push(label);
  const rect = node => node.getBoundingClientRect();
  const hit = node => { const box = rect(node), target = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2); return target === node || node.contains(target); };
  const wait = async predicate => { for (let i = 0; i < 100; i++) { if (predicate()) return; await new Promise(resolve => setTimeout(resolve, 20)); } throw new Error(`Control did not settle: ${state}`); };
  const item = key => $(`[data-menu-key="${key}"]`);
  const posts = () => fixture.requests.filter(request => request.method === "POST");
  // Synthetic DOM clicks must yield for React's event commit before observation.
  const settleFrame = () => new Promise(resolve => requestAnimationFrame(resolve));
  const enter = (input, value) => {
    const prototype = input instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, "value").set.call(input, value);
    input.dispatchEvent(new Event("input", {bubbles: true}));
  };
  const open = async kind => { $(`[data-${kind}-menu]`).click(); await settleFrame(); await settleFrame(); };
  const escape = () => document.dispatchEvent(new KeyboardEvent("keydown", {key: "Escape", bubbles: true, cancelable: true}));
  const bounds = (panel, label, margin = 12) => {
    const box = rect(panel); measurements.panel = box.toJSON();
    check(box.left >= margin - 1 && box.right <= innerWidth - margin + 1 && box.top >= margin - 1 && box.bottom <= innerHeight - margin + 1, `${label} stays inside viewport clearances`);
    check(panel.scrollWidth <= panel.clientWidth + 1, `${label} has no internal horizontal overflow`);
  };
  try {
    window.SnowMenus.close();
    for (const dialog of document.querySelectorAll("dialog[open]")) {
      await new Promise(resolve => {
        dialog.addEventListener("close", resolve, {once: true});
        // Match native dismissal: let the React cancel owner update its state
        // before the native dialog closes. Direct close() bypasses that owner.
        if (dialog.dispatchEvent(new Event("cancel", {cancelable: true}))) dialog.close();
      });
    }
    await settleFrame();
    if (state === "workspace-actions") {
      const panel = $("#project-inspector"), project = panel.dataset.project;
      const row = [...document.querySelectorAll("[data-sidebar-project]")].find(node => node.dataset.sidebarProject === project);
      const trigger = row.querySelector("[data-shell-project-menu]"), add = row.querySelector("[data-shell-project-new]");
      const before = posts().length;
      if (innerWidth < 768) $("[data-nav-toggle]").click(); await settleFrame();
      trigger.focus(); trigger.click(); await settleFrame();
      const menu = $(".snow-menu"), links = [...menu.querySelectorAll('[role="menuitem"]')];
      check(menu.parentElement === document.body && !trigger.closest("a") && !add.closest(".project-link"), "Workspace actions are sibling controls and use the shared portal");
      bounds(menu, "Workspace actions");
      check(links.map(node => node.textContent).join("|") === "Workspace settings…|Remove registration…", "Workspace menu exposes only supported project actions");
      links[1].click(); await settleFrame();
      check(!panel.hidden && $("#inspection-tab-project").getAttribute("aria-selected") === "true", "Workspace removal opens the actual Project inspector tab");
      const details = $(".remove-project"), form = details.querySelector("form");
      check(details.open && document.activeElement === details.querySelector("summary"), "Removal entrypoint reveals and focuses existing confirmation");
      check(!form.checkValidity() && !form.querySelector('[name="confirm"]').checked, "Removal remains explicitly unchecked and invalid");
      check(posts().length === before, "Project menu opening performs no filesystem, provider or mutation requests");
      const close = panel.querySelector("[data-inspector-toggle]"); close.focus(); check(hit(close), "Project menu inspector close is reachable at this viewport"); close.click(); await settleFrame();
      check(panel.hidden && !$(".conversation-pane").inert, "Workspace inspector closes without leaving conversation inert");
      document.dispatchEvent(new CustomEvent("snow:inspect-project", {detail: {project: "not-mounted", remove: true}}));
      check(panel.hidden && posts().length === before, "Stale project presentation request cannot open another project's inspector");
    } else if (state === "composer-growth") {
      const area = $("#live-prompt"), stream = $("#live-stream");
      const value = area.value, before = posts().length;
      const type = async text => { enter(area, text); await new Promise(resolve => setTimeout(resolve, 80)); };
      area.focus(); await type("First line\nSecond line\nThird line");
      const grown = rect(area).height;
      if (innerHeight === 740) check(grown >= 76 && grown < 82, "Three-line draft grows to reference 76px text geometry");
      check(area.value === "First line\nSecond line\nThird line" && document.activeElement === area, "Draft growth preserves text and editing focus");
      await type("A long draft line\n".repeat(100));
      check(rect(area).height <= 337 && area.scrollHeight > area.clientHeight, "Long draft remains capped with native text overflow");
      const send = $("#live-send"); send.focus(); check(hit(send), "Long draft keeps Send reachable at normal and short heights");
      await type("");
      check(rect(area).height <= 37 && (innerHeight === 740 ? rect(area).height < grown : rect(area).height <= grown), "Cleared draft shrinks to the normal or short-viewport minimum");
      check(getComputedStyle(stream).paddingTop === "16px" && getComputedStyle(stream).paddingBottom === "16px", "Transcript uses reference 16px vertical insets at every width");
      check(posts().length === before, "Editing and clearing draft never sends or changes runtime state");
      await type(value);
    } else if (state.startsWith("folder-")) {
      fixture.folderMode = state.slice(7);
      const trigger = $("[data-folder-open]"); trigger.focus(); trigger.click(); await settleFrame();
      const dialog = $("#folder-picker");
      await wait(() => dialog.open && $("#folder-list").getAttribute("aria-busy") === "false");
      check(dialog.contains(document.activeElement), "Host folder picker contains keyboard focus");
      bounds(dialog, "Host folder picker", 12);
      if (state === "folder-error") check(!$("#folder-error").hidden && $("[data-folder-select]").disabled, "Inaccessible host folder displays error and disables selection");
      else {
        check(!$("[data-folder-select]").disabled && $("#folder-current").textContent === "/fixture", "Resolved host folder enables explicit selection without registration");
        check(state === "folder-empty" ? $("#folder-list").children.length === 0 : $("#folder-list").children.length === 40, "Folder rows reflect populated versus empty host response");
        if (state === "folder-limited") check(!$("[data-folder-more]").hidden && /limit/i.test($("#folder-status").textContent), "Bounded folder listing has explicit limit notice and Load more");
        const select = $("[data-folder-select]"); select.focus();
        check(document.activeElement === select && hit(select), "Folder selection is keyboard and pointer reachable after long rows");
      }
      const cancel = $("[data-folder-close]"); cancel.focus();
      check(hit(cancel), "Folder Cancel remains reachable at short height");
      cancel.click(); await settleFrame(); await wait(() => !dialog.open && document.activeElement === trigger);
      check(fixture.nonInventoryRequests().every(r => r.path === "/projects/folders"), "Folder browsing never registers a project or activates a worker");
    } else if (state.startsWith("inspector-")) {
      const trigger = $(".workspace-heading [data-inspector-toggle]"), panel = $("#project-inspector");
      if (panel.hidden) trigger.click(); await settleFrame();
      const name = state.slice(10), tab = $(`[data-inspection-tab="${name}"]`); tab.click(); await settleFrame();
      check(tab.getAttribute("aria-selected") === "true" && !$(`#inspection-${name}`).hidden, "Inspector activates exactly the requested real tab");
      if (name === "files") {
        await wait(() => !!$('[data-inspection-entry="README.md"]'));
        $('[data-inspection-entry="README.md"]').click(); await settleFrame();
        await wait(() => !$("[data-file-preview]").hidden);
        check($("[data-file-content]").textContent.includes("Public read-only fixture") && /truncated/i.test($("[data-file-notice]").textContent), "Files displays literal read-only text and bounded-preview warning");
        $("[data-file-content]").focus(); check(document.activeElement === $("[data-file-content]"), "Long file content can receive keyboard scrolling focus");
      } else if (name === "changes") {
        await wait(() => !!$("[data-inspection-change]")); $("[data-inspection-change]").click(); await settleFrame();
        await wait(() => !$("[data-diff-preview]").hidden);
        check($("[data-diff-content]").textContent.includes("+public fixture change"), "Changes renders actual public mocked diff content");
      } else {
        const details = $(".remove-project"); details.open = true;
        const form = details.querySelector("form");
        check(form.method === "post" && !form.checkValidity() && form.querySelector('[name="confirm"]').required, "Project removal keeps explicit unchecked confirmation and POST authority");
        check($("#inspection-project").textContent.includes("/fixture/workspaces/snow-core"), "Project pane displays authoritative host path, not browser filesystem");
      }
      tab.focus(); tab.dispatchEvent(new KeyboardEvent("keydown", {key: "ArrowRight", bubbles: true, cancelable: true}));
      check(document.activeElement.matches('[role="tab"]') && document.activeElement !== tab, "Inspector tablist supports arrow-key focus movement");
      const close = panel.querySelector("[data-inspector-toggle]"); close.focus(); check(hit(close), "Inspector close remains keyboard and pointer reachable");
      close.click(); await settleFrame();
      check(panel.hidden && !$(".conversation-pane").inert && document.activeElement === trigger, "Inspector restores conversation and exact trigger focus");
    } else if (state.startsWith("settings-")) {
      const trigger = $("[data-settings-open]");
      if (innerWidth < 768 && $("#project-navigation").inert) $("[data-nav-toggle]").click(); await settleFrame();
      const before = posts().length;
      trigger.click(); await settleFrame();
      const dialog = $("#settings-dialog"), section = state.slice(9);
      check(dialog.open && dialog.contains(document.activeElement), "Settings opens a native focus-contained modal");
      $(`[data-settings-section="${section}"]`).click(); await settleFrame();
      check(!$(`[data-settings-panel="${section}"]`).hidden && [...document.querySelectorAll("[data-settings-panel]")].filter(node => !node.hidden).length === 1, "Settings section navigation shows exactly its real panel");
      bounds(dialog, "Settings modal", 24);
      const close = $("[data-settings-close]"); close.focus();
      check(document.activeElement === close && hit(close), "Settings close remains keyboard and pointer reachable at short height");
      const panel = $(`[data-settings-panel="${section}"]`), scroll = $(".settings-options");
      scroll.scrollTop = scroll.scrollHeight;
      check(hit(close) && panel.textContent.trim().length > 80, "Contentful settings panel scroll never covers persistent close control");
      if (innerWidth >= 1280) check(Math.abs(rect(dialog).width - 800) <= 2, "Desktop Settings ports the 800px reference width");
      if (section === "general") {
        check(document.querySelectorAll("[data-theme-choice]").length === 2 && !!$('[data-theme-choice="light"]') && !!$('[data-theme-choice="dark"]'), "Only functional Light and Dark appearance choices exist");
        $('[data-theme-choice="light"]').click(); await settleFrame();
        check(localStorage.getItem("snow-manager-theme") === "light" && document.documentElement.dataset.theme === "light", "Light appearance changes actual theme and persists browser preference");
        const light = getComputedStyle(document.documentElement);
        check(light.getPropertyValue("--text").trim() === "#0f1115" && light.getPropertyValue("--accent").trim() === "#4176e6" && light.getPropertyValue("--composer").trim() === "#fff", "Light palette follows resolved reference label, send and input tokens");
        $('[data-theme-choice="dark"]').click(); await settleFrame();
        check(localStorage.getItem("snow-manager-theme") === "dark", "Dark appearance is restored and persisted");
        check($("#settings-general").textContent.includes("privileges") && $("#settings-general").textContent.includes("sandbox"), "General keeps the real host privilege boundary visible");
      } else if (section === "workspaces") {
        check(!!$('#settings-workspaces a[href*="00000000-0000-4000-8000-000000000001"]') && !!$('#settings-workspaces a[href="/?view=projects#add-project"]'), "Workspaces uses registered host folders and the actual add flow");
      } else {
        for (const route of ["/access/pair", "/access/revoke-all", "/logout"]) {
          const form = $(`#settings-access form[action="${route}"]`);
          check(form?.method === "post" && form.querySelector('[name="csrf"]')?.value === "fixture-only-not-a-credential", `Browser access reuses CSRF-protected ${route} POST`);
        }
        check($('#settings-access input[name="confirm"]').required && !$('#settings-access input[name="confirm"]').checked, "Revocation still requires explicit unchecked confirmation");
      }
      check(posts().length === before, "Opening and browsing Settings neither activates a worker nor submits an access action");
      $("[data-settings-close]").click(); await settleFrame();
      await wait(() => !dialog.open && document.activeElement === trigger);
      check(!dialog.open && document.activeElement === trigger, "Closing Settings restores its exact trigger focus");
      trigger.click(); await settleFrame(); $(`[data-settings-section="${section}"]`).click(); await settleFrame();
      $(`[data-theme-choice="${fixture.theme}"]`).click(); await settleFrame();
    } else {
      if (state === "model-disconnected") { fixture.offline = true; await wait(() => $("#live-connection").textContent === "Reconnecting…"); }
      const before = posts().length;
      const kind = state.startsWith("model") ? "model" : state === "rename" || state === "sessions" ? "session" : state;
      open(kind);
      const discoveryStarted = kind === "model" && $("[data-workflow-load-status]")?.textContent === "Loading host models…";
      const expectedPosts = before + (discoveryStarted ? 1 : 0);
      if (kind !== "model") check(posts().length === before, "Opening other task menus performs no discovery, activation, or mutation");
      if (["model-empty", "model-unavailable", "model-disconnected"].includes(state)) {
        if (state === "model-disconnected") {
          check(item("load").disabled && $("#live-send").disabled, "Disconnected host allows menu inspection but disables discovery and sending");
          check(posts().length === before, "Disconnected menu never retries or autoactivates a runtime");
        } else {
          await wait(() => item("load") && !item("load").disabled);
          check(!$("[data-model-id]"), "Empty or unavailable discovery never fabricates model choices");
          check($(".snow-menu").textContent.includes(state === "model-empty" ? "No host models discovered" : "Host choices unavailable"), "Discovery state has a truthful in-menu explanation and explicit retry");
          check(posts().length === before + 1, "One deliberate load makes one bounded metadata request, without automatic retry");
        }
      } else if (state === "model-unloaded") {
        check(!!$("[data-model-search]") && !!$("[data-workflow-load-status]"), "Opening an uncached picker directly shows search and discovery status");
      } else if (state.startsWith("model")) {
        await wait(() => !!$("[data-model-id]"));
        check(posts().length === expectedPosts, "Model-trigger click loads only uncached choices, exactly once");
        check(document.activeElement === $("[data-model-search]"), "Search retains focus when asynchronous model discovery completes");
        if (state === "model-root") {
          escape();
          check(!$(".snow-menu") && document.activeElement === $("[data-model-menu]"), "Escape closes the picker and restores model trigger focus");
          open("model"); open("mode");
          check(document.querySelectorAll(".snow-menu").length === 1 && $("[data-model-menu]").getAttribute("aria-expanded") === "false", "Opening a different task closes its predecessor");
          document.body.dispatchEvent(new PointerEvent("pointerdown", {bubbles: true}));
          check(!$(".snow-menu"), "Outside pointer dismissal leaves no orphaned portal");
          open("model");
        } else {
          check(document.querySelectorAll(".snow-menu-group").length === 2, "Direct model list groups exact host choices by provider");
          check(document.querySelectorAll('[data-model-id="host-model"]').length === 2, "Duplicate model IDs stay separate by provider identity");
          const selected = $('[data-model-provider="host-provider"][data-model-id="host-model"]');
          check(selected.getAttribute("aria-checked") === "true", "Only the authoritative provider/model pair is checked");
          const search = $("[data-model-search]"), menu = $(".snow-menu");
          const type = value => enter(search, value);
          type("OTHER-HOST");
          check([...document.querySelectorAll('[data-model-provider]')].every(row => row.dataset.modelProvider === "other-host") && !!$("[data-model-id]"), "Case-insensitive search filters provider identity locally");
          type("no-such-model-unique-query");
          check(!$("[data-model-id]") && menu.textContent.includes("No models match"), "Search has an explicit no-match state without fabricating choices");
          type("");
          check(document.activeElement === search && posts().length === expectedPosts, "Search preserves focus and makes no network request or selection");
          search.dispatchEvent(new KeyboardEvent("keydown", {key: "ArrowDown", bubbles: true, cancelable: true}));
          check(document.activeElement.dataset.modelProvider === "host-provider", "ArrowDown enters the provider model list without selecting");
          document.activeElement.dispatchEvent(new KeyboardEvent("keydown", {key: "End", bubbles: true, cancelable: true}));
          check(document.activeElement === item("load"), "End reaches the explicit Refresh action");
          document.activeElement.dispatchEvent(new KeyboardEvent("keydown", {key: "Home", bubbles: true, cancelable: true}));
          check(document.activeElement.dataset.modelProvider === "host-provider", "Home reaches the first model row");
          document.activeElement.dispatchEvent(new KeyboardEvent("keydown", {key: "ArrowUp", bubbles: true, cancelable: true}));
          check(document.activeElement === search, "ArrowUp from the first model returns to search");
          check(rect(search).top >= rect(menu).top && rect(search).bottom <= rect(menu).bottom, "Search remains visible while the model list scrolls");
          escape(); check(!$(".snow-menu"), "Escape closes directly without an unnecessary model submenu");
          open("model");
          check(posts().length === expectedPosts, "Reopening cached models does not repeat discovery");
        }
      } else if (state === "sessions") {
        item("sessions").click(); await settleFrame();
        check(!!item("fixture-previous"), "Session menu lists real host session DTOs");
      } else if (state === "rename") {
        item("rename").click(); await settleFrame();
        check($("#workflow-rename-dialog").open && $("#workflow-rename-dialog").contains(document.activeElement), "Rename opens the real accessible current-session dialog");
        check($("#workflow-name").value === fixture.snapshot.session_name, "Rename prefill uses authoritative current session name");
      } else if (state === "mode") {
        check(item("default").getAttribute("aria-checked") === "true" && !!item("plan"), "Mode menu exposes only real authoritative Default and Plan choices");
      } else if (state === "telemetry") {
        check($("[data-workflow-usage]").textContent === "Unknown" && $("[data-workflow-context]").textContent === "Unknown" && !!item("telemetry-details") && !$(".telemetry-menu .snow-menu-note,.telemetry-menu .session-cost-note") && getComputedStyle($(".telemetry-menu dl > div")).paddingTop === "0px", "Compact telemetry keeps Unknown, defers explanations to Details and resets inspector padding");
      }
      const panel = $(state === "rename" ? "#workflow-rename-dialog" : ".snow-menu");
      bounds(panel, state);
      if (state !== "rename") {
        check(rect(panel).height <= Math.min(360, innerHeight - (kind === "model" ? 24 : 96)) + 1, "Task menu height is bounded; searchable model picker uses 12px viewport clearances");
        check(document.querySelectorAll(".snow-menu").length === 1 && panel.parentElement === document.body, "Exactly one task menu is portaled outside clipping owners");
        check(parseFloat(getComputedStyle(panel).borderTopLeftRadius) === 20, "Task menu retains reference 20px material radius");
      }
      check(!$("#workflow-provider,#workflow-model,[data-workflow-model-apply]"), "No hidden legacy model form compatibility remains");
      check($("#live-composer").getBoundingClientRect().bottom <= innerHeight + 1, `Open controls keep the docked composer within the viewport (composer=${JSON.stringify(rect($("#live-composer")).toJSON())}, seat=${JSON.stringify(rect($("#live-composer-seat")).toJSON())}, region=${JSON.stringify(rect($("#live-session")).toJSON())})`);
    }
    check(document.documentElement.scrollWidth <= innerWidth + 1, "Open control does not overflow the viewport horizontally");
    check(fixture.inventoryComplete(), "Controls do not repeat the exact startup browser-inventory read");
    check(fixture.errors.length === 0, `No fixture errors: ${fixture.errors.join("; ")}`);
  } catch (error) { failures.push(error.stack || String(error)); }
  return {name: `${fixture.name}-${state}`, width: innerWidth, height: innerHeight, theme: fixture.theme, passed: results.length, results, failures, measurements};
})()
