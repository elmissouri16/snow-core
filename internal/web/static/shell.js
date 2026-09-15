(() => {
  "use strict";
  // Shell interactions are local UI only. Runtime actions remain owned by
  // conversation.js; workspace links never activate or resume an agent.
  const root = document.documentElement;
  const desktop = matchMedia("(min-width: 768px)");
  const $ = (selector, scope = document) => scope.querySelector(selector);
  let collapsed = false;
  let hideSessions = false;
  let workflowObserver, settingsReturn, sidebarView;

  function syncCollapse() {
    const hidden = desktop.matches && collapsed;
    root.dataset.sidebarCollapsed = String(hidden);
    $("#workspace")?.classList.toggle("sidebar-collapsed", hidden);
    document.querySelectorAll("[data-sidebar-collapse]").forEach(button => {
      button.setAttribute("aria-expanded", String(!hidden));
      button.setAttribute("aria-label", hidden ? "Expand sidebar" : "Collapse sidebar");
    });
  }

  function syncNewSession() {
    const live = $("#live-session[data-runtime=true]");
    if (live) {
      const title = $("[data-live-title]", live)?.textContent || "New conversation";
      // Session IDs belong to rows, not the moving live selection. Promote an
      // existing target in place so the clicked link and its focus survive the
      // switch while the instance-bound inventory refresh is still pending.
      const group = [...document.querySelectorAll("[data-sidebar-project]")].find(row => row.dataset.sidebarProject === live.dataset.project);
      const tree = group && $("[data-workspace-sessions]", group);
      const rows = tree ? [...tree.querySelectorAll("[data-shell-session]")] : [];
      const previous = rows.find(row => row.hasAttribute("data-shell-live-session"));
      let currentRow = rows.find(row => row.dataset.shellSession === live.dataset.session);
      if (tree && !currentRow) {
        currentRow = document.createElement("div"); currentRow.className = "shell-session-row";
        currentRow.dataset.shellSession = live.dataset.session;
        const link = document.createElement("a"); link.append(document.createElement("span")); currentRow.append(link);
        const target = "/?" + new URLSearchParams({view: "projects", project: live.dataset.project, session: live.dataset.session});
        for (const [name, value] of Object.entries({href: target, "hx-get": target, "hx-target": "#workspace", "hx-swap": "outerHTML", "hx-push-url": "true", "hx-sync": "#project-navigation:replace", "data-shell-session-open": "", "data-project": live.dataset.project, "data-instance": live.dataset.instance})) link.setAttribute(name, value);
        tree.prepend(currentRow); window.htmx?.process(link);
      }
      if (currentRow) {
        for (const row of rows) if (row !== currentRow && row.hasAttribute("data-shell-live-session")) row.removeAttribute("data-shell-live-session");
        if (!currentRow.hasAttribute("data-shell-live-session")) currentRow.setAttribute("data-shell-live-session", "");
        const rename = previous && $("[data-shell-session-menu]", previous);
        if (rename && !$("[data-shell-session-menu]", currentRow)) currentRow.append(rename);
      }
      document.querySelectorAll("[data-shell-session]").forEach(row => {
        const link = $("a", row), sameProject = row.closest("[data-sidebar-project]")?.dataset.sidebarProject === live.dataset.project;
        const active = sameProject && row.dataset.shellSession === live.dataset.session;
        if (!link) return;
        // A new click uses the verified live owner; the server still validates
        // target membership. Already-captured intents retain their old nonce.
        // Deletion/other mutation controls remain inventory-owned and untouched.
        if (sameProject && link.hasAttribute("data-shell-session-open") && link.dataset.instance !== live.dataset.instance) link.dataset.instance = live.dataset.instance;
        if (row.hidden) row.hidden = false;
        if (active) {
          if (link.getAttribute("aria-current") !== "page") link.setAttribute("aria-current", "page");
          const label = $("span", link);
          if (label && label.textContent !== title) label.textContent = title;
          if (label && label.title !== title) label.title = title;
        } else if (sameProject && link.hasAttribute("aria-current")) link.removeAttribute("aria-current");
      });
    }
    window.SnowSidebarSessions?.syncCurrent(live);
    const link = $("[data-shell-new-session]");
    const action = $("[data-workflow-new]") || $("[data-workflow-switch-confirm]");
    document.querySelectorAll("[data-shell-session-menu]").forEach(button => {
      const current = button.closest("[data-sidebar-project]")?.dataset.sidebarProject === live?.dataset.project && button.closest("[data-shell-session]")?.dataset.shellSession === live?.dataset.session;
      const rename = $("[data-workflow-rename]") || $('[data-workflow-rename-form] button[type="submit"]');
      const deletion = button.dataset.deleteSupported === "true";
      const hidden = (!current || !rename) && !deletion, disabled = (!current || !rename || rename.disabled) && !deletion;
      if (button.hidden !== hidden) button.hidden = hidden;
      if (button.disabled !== disabled) button.disabled = disabled;
    });
    if (!link) return;
    if (action?.disabled) {
      if (link.getAttribute("aria-disabled") !== "true") link.setAttribute("aria-disabled", "true");
      const title = "Wait for the conversation controls to become available";
      if (link.title !== title) link.title = title;
    } else {
      if (link.hasAttribute("aria-disabled")) link.removeAttribute("aria-disabled");
      if (link.hasAttribute("title")) link.removeAttribute("title");
    }
  }

  function filterWorkspaces() {
    const query = ($("[data-sidebar-search]")?.value || "").trim().toLocaleLowerCase();
    const projects = document.querySelectorAll("[data-sidebar-project]");
    let matches = 0;
    projects.forEach(project => {
      const name = $(".project-link", project)?.textContent || "";
      project.hidden = !!query && !name.toLocaleLowerCase().includes(query);
      if (!project.hidden) matches++;
    });
    const empty = $("[data-sidebar-search-empty]");
    if (empty) empty.hidden = !query || matches > 0;
  }

  function syncSessionView() {
    if (window.SnowSidebarSessions) window.SnowSidebarSessions.syncVisibility(hideSessions);
    else document.querySelectorAll(".session-tree").forEach(tree => { tree.hidden = hideSessions; });
  }

  function initialize() {
    if (!$("#workspace")) return;
    // All project views retain their existing heading and drawer selectors.
    // Supply the same collapse affordance without changing their templates.
    const heading = $(".workspace-heading");
    if (heading && !$(".sidebar-restore", heading)) {
      const source = $(".sidebar-collapse");
      if (source) {
        const restore = source.cloneNode(true);
        restore.className = "quiet sidebar-restore";
        heading.prepend(restore);
      }
    }
    syncCollapse();
    window.SnowSidebarSessions?.initialize();
    syncSessionView();
    filterWorkspaces();
    workflowObserver?.disconnect();
    const live = $("#live-session");
    if (live) {
      workflowObserver = new MutationObserver(syncNewSession);
      workflowObserver.observe(live, {attributes: true, subtree: true, attributeFilter: ["disabled", "data-session"]});
      const title = $("[data-live-title]", live);
      if (title) workflowObserver.observe(title, {childList: true, characterData: true, subtree: true});
      document.querySelectorAll('[data-workflow-switch-confirm], [data-workflow-rename-form] button[type="submit"]').forEach(button => {
        workflowObserver.observe(button, {attributes: true, attributeFilter: ["disabled"]});
      });
    }
    const dialog = $("#settings-dialog");
    dialog?.addEventListener("close", () => {
      if (dialog.open) return;
      const restore = dialog.returnValue === "access" ? $("#workspace-content") : settingsReturn;
      settingsReturn = null;
      queueMicrotask(() => { if (restore?.isConnected) restore.focus({preventScroll: true}); });
    });
    syncNewSession();
    if (sidebarView) {
      const saved = sidebarView; sidebarView = null;
      const input = $("[data-sidebar-search]"), search = $("#sidebar-search");
      if (input && search) {
        input.value = saved.query; search.hidden = !saved.searchOpen;
        $("[data-sidebar-search-toggle]")?.setAttribute("aria-expanded", String(saved.searchOpen));
        filterWorkspaces();
      }
      if ($(".project-tree")) $(".project-tree").scrollTop = saved.listScroll;
      $("#project-navigation").scrollTop = saved.railScroll;
      // Restore only a control that actually owned focus at swap time. If the
      // user moved to the composer while the read was pending, leave it alone.
      const controlRow = saved.controlProject ? [...document.querySelectorAll("[data-sidebar-project]")].find(row => row.dataset.sidebarProject === saved.controlProject) : null;
      const control = saved.control && controlRow ? $(saved.control, controlRow) : null;
      const target = control || (saved.href ? [...document.querySelectorAll('#project-navigation .project-link, #project-navigation .shell-session-row > a, #project-navigation .sidebar-utility')].find(link => link.getAttribute("href") === saved.href) : saved.searchFocused ? input : null);
      target?.focus({preventScroll: true});
      const tree = $(".project-tree");
      if (target && tree?.contains(target)) {
        const bounds = tree.getBoundingClientRect(), row = target.getBoundingClientRect();
        // A newly expanded session list may shift the destination row. Reveal
        // only that list item, without scrolling the document or conversation.
        if (row.top < bounds.top) tree.scrollTop -= bounds.top - row.top;
        else if (row.bottom > bounds.bottom) tree.scrollTop += row.bottom - bounds.bottom;
      }
      if (target === input && saved.selection) input.setSelectionRange(...saved.selection);
    }
  }

  function closeSettings(dialog, access = false) {
    if (!dialog?.open) return;
    const restore = access ? $("#workspace-content") : settingsReturn;
    settingsReturn = null;
    dialog.close();
    if (restore?.isConnected) restore.focus({preventScroll: true});
  }

  function settingsSection(id) {
    document.querySelectorAll("[data-settings-section]").forEach(button => {
      if (button.dataset.settingsSection === id) button.setAttribute("aria-current", "true");
      else button.removeAttribute("aria-current");
    });
    document.querySelectorAll("[data-settings-panel]").forEach(panel => { panel.hidden = panel.dataset.settingsPanel !== id; });
    const options = $(".settings-options");
    if (options) options.scrollTop = 0;
  }

  function menuButton(label, callback) {
    const button = document.createElement("button");
    button.type = "button"; button.className = "snow-menu-row"; button.setAttribute("role", "menuitem");
    button.textContent = label;
    button.addEventListener("click", callback);
    return button;
  }

  function viewMenu(trigger) {
    if (!window.SnowMenus) return;
    const panel = document.createElement("div");
    panel.setAttribute("aria-label", "Workspace view options");
    const row = menuButton("Show saved sessions", () => {
      hideSessions = !hideSessions; syncSessionView(); window.SnowMenus.close();
    });
    row.textContent = `${hideSessions ? "" : "✓ "}Show saved sessions`;
    row.setAttribute("role", "menuitemcheckbox"); row.setAttribute("aria-checked", String(!hideSessions));
    panel.append(row); window.SnowMenus.open({trigger, panel});
  }

  function forwardWorkflow(kind) {
    const legacy = $(`[data-workflow-${kind}]`);
    if (legacy) { if (!legacy.disabled) legacy.click(); return; }
    // The session menu owns the guards and workflow implementation. Its row
    // callbacks are synchronous to mount and revalidate the current instance.
    // Do not copy canSwitch/canChange or invoke hooks.action from the shell.
    const trigger = $("[data-session-menu]");
    if (!trigger || trigger.disabled) return;
    window.SnowMenus?.close({restoreFocus: false});
    trigger.click();
    const action = $(`.snow-menu [data-menu-key="${kind}"]`);
    if (action && !action.disabled) action.click();
    else window.SnowMenus?.close();
  }

  function sessionMenu(trigger) {
    if (!window.SnowMenus || trigger.disabled) return;
    const session = trigger.closest("[data-shell-session]")?.dataset.shellSession;
    const project = trigger.closest("[data-sidebar-project]")?.dataset.sidebarProject;
    if (!session || !project) return;
    const live = $("#live-session[data-runtime=true]"), instance = live?.dataset.instance;
    const current = project === live?.dataset.project && session === live?.dataset.session;
    const panel = document.createElement("div");
    panel.setAttribute("aria-label", "Session actions");
    if (current) {
      const rename = menuButton("Rename", () => {
        window.SnowMenus.close({restoreFocus: false});
        const owner = $("#live-session[data-runtime=true]");
        // Keep the real row launcher and reject callbacks from a retired owner.
        if (trigger.isConnected && project === owner?.dataset.project && session === owner?.dataset.session && instance === owner?.dataset.instance) {
          if (window.SnowConversation?.rename) window.SnowConversation.rename(trigger);
          else forwardWorkflow("rename");
        }
      });
      const control = $("[data-workflow-rename]") || $('[data-workflow-rename-form] button[type="submit"]');
      rename.disabled = !control || control.disabled;
      panel.append(rename);
    }
    window.SnowSessionActions?.appendMenu(panel, trigger);
    if (panel.childElementCount) window.SnowMenus.open({trigger, panel});
  }

  function projectMenu(trigger) {
    if (!window.SnowMenus || trigger.disabled) return;
    const project = trigger.closest("[data-sidebar-project]")?.dataset.sidebarProject;
    if (!project) return;
    const panel = document.createElement("div");
    panel.setAttribute("aria-label", "Workspace actions");
    const workspaceLink = $(".project-link", trigger.closest("[data-sidebar-project]"));
    const path = workspaceLink?.getAttribute("title");
    if (path) {
      const heading = document.createElement("div"); heading.className = "shell-workspace-menu-heading";
      const name = document.createElement("strong"), location = document.createElement("small");
      name.textContent = $("strong", workspaceLink)?.textContent || "Workspace";
      location.textContent = path; heading.append(name, location); panel.append(heading);
    }
    for (const [remove, label] of [[false, "Workspace settings…"], [true, "Remove registration…"]]) {
      const link = document.createElement("a");
      const query = new URLSearchParams({view: "projects", project, inspect: "project"});
      if ($("#project-inspector")?.dataset.project === project) {
        const session = new URLSearchParams(window.location?.search || "").get("session");
        if (session) query.set("session", session);
      }
      const href = "/?" + query + (remove ? "#remove-project" : "");
      link.className = "snow-menu-row";
      link.textContent = label;
      link.setAttribute("role", "menuitem");
      link.setAttribute("href", href); link.setAttribute("hx-get", href);
      link.setAttribute("hx-target", "#workspace"); link.setAttribute("hx-swap", "outerHTML");
      link.setAttribute("hx-push-url", href);
      link.addEventListener("click", event => {
        // A replaced workspace invalidates callbacks held by an old portal.
        if (!trigger.isConnected || trigger.closest("[data-sidebar-project]")?.dataset.sidebarProject !== project) {
          event.preventDefault(); return;
        }
        if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0) return;
        if ($("#project-inspector")?.dataset.project === project) {
          event.preventDefault();
          window.SnowMenus.close({restoreFocus: false});
          // app.js owns inspector/drawer presentation and removal confirmation.
          // This event opens the Project tab; it never submits the remove form.
          document.dispatchEvent(new CustomEvent("snow:inspect-project", {detail: {project, remove, trigger}}));
        } else {
          // Let HTMX inspect the live portal link before teardown. Navigation
          // guards and URL-driven inspector opening remain owned by app.js.
          queueMicrotask(() => window.SnowMenus.close({restoreFocus: false}));
        }
      });
      panel.append(link);
    }
    window.SnowMenus.open({trigger, panel});
    window.htmx?.process(panel);
  }

  function workspaceMenu(trigger, event) {
    if (!window.SnowMenus) return;
    event.preventDefault();
    const details = trigger.closest(".workspace-picker");
    details.open = false;
    const panel = $(".workspace-picker-menu", details)?.cloneNode(true);
    if (!panel) return;
    panel.className = "shell-workspace-menu";
    panel.setAttribute("aria-label", "Choose workspace");
    panel.querySelectorAll("a").forEach(link => {
      link.classList.add("snow-menu-row"); link.setAttribute("role", "menuitem");
      link.addEventListener("click", () => queueMicrotask(() => window.SnowMenus.close({restoreFocus: false})));
    });
    window.SnowMenus.open({trigger, panel, placement: "top-start"});
    window.htmx?.process(panel);
  }

  // Intercept workflow links before their target-level HTMX listeners can
  // navigate. Bubble-phase preventDefault is too late: the GET has started.
  document.addEventListener("click", event => {
    if (!(event.target instanceof Element) || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0) return;
    const target = event.target.closest("[data-shell-project-new],[data-shell-new-session],[data-shell-session-open]");
    if (!target || !target.isConnected) return;
    const row = target.closest("[data-sidebar-project]");
    const project = row?.dataset.sidebarProject || $("#workspace")?.dataset.project;
    if (target.matches("[data-shell-session-open]")) {
      // A cold inventory is a normal read-only link. A live inventory denotes
      // deliberate owner-mediated intent, never an automatic action from a URL.
      const instance = target.dataset.instance;
      if (!project || !instance) return;
      event.preventDefault(); event.stopPropagation();
      $("#project-navigation.nav-open [data-nav-close]")?.click();
      document.dispatchEvent(new CustomEvent("snow:session-select", {detail: {project, session: target.closest("[data-shell-session]")?.dataset.shellSession, instance, trigger: target}}));
      return;
    }
    // With no selected workspace, retain the existing home/picker fallback.
    if (!project) return;
    event.preventDefault(); event.stopPropagation();
    $("#project-navigation.nav-open [data-nav-close]")?.click();
    if (target.getAttribute("aria-disabled") !== "true") {
      document.dispatchEvent(new CustomEvent("snow:session-new", {detail: {project, trigger: target}}));
    }
  }, {capture: true});

  document.addEventListener("click", event => {
    if (!(event.target instanceof Element)) return;
    const dialog = $("#settings-dialog");
    if (event.target === dialog) {
      const rect = dialog.getBoundingClientRect();
      if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) closeSettings(dialog);
      return;
    }
    const target = event.target.closest("button,a,summary");
    if (!target) return;
    if (target.matches("[data-settings-open]")) {
      if (!dialog || dialog.open) return;
      window.SnowMenus?.close({restoreFocus: false});
      settingsReturn = target;
      settingsSection(target.dataset.settingsOpen === "workspaces" ? "workspaces" : "general");
      dialog.returnValue = "";
      dialog.showModal();
      $("[data-settings-close]", dialog)?.focus();
    } else if (target.matches("[data-settings-close]")) {
      closeSettings(dialog, target.hasAttribute("data-settings-access-return"));
    } else if (target.matches("[data-settings-section]")) {
      settingsSection(target.dataset.settingsSection);
    } else if (target.matches(".workspace-picker > summary")) {
      workspaceMenu(target, event);
    } else if (target.matches("[data-shell-project-menu]")) {
      event.preventDefault(); event.stopPropagation(); projectMenu(target);
    } else if (target.matches("[data-shell-session-menu]")) {
      event.preventDefault(); event.stopPropagation(); sessionMenu(target);
    } else if (target.matches("[data-sidebar-collapse]")) {
      if (!desktop.matches) return;
      collapsed = !collapsed;
      syncCollapse();
      $(".sidebar-collapse")?.focus();
    } else if (target.matches("[data-sidebar-search-toggle]")) {
      const panel = $("#sidebar-search");
      if (!panel) return;
      if (desktop.matches && collapsed) {
        collapsed = false;
        syncCollapse();
        panel.hidden = true;
      }
      panel.hidden = !panel.hidden;
      target.setAttribute("aria-expanded", String(!panel.hidden));
      if (!panel.hidden) $("[data-sidebar-search]")?.focus();
      else {
        const input = $("[data-sidebar-search]");
        if (input) input.value = "";
        filterWorkspaces();
      }
    } else if (target.matches("[data-sidebar-view-toggle]")) {
      viewMenu(target);
    }
  });

  document.addEventListener("input", event => {
    if (event.target instanceof Element && event.target.matches("[data-sidebar-search]")) filterWorkspaces();
  });
  document.addEventListener("keydown", event => {
    if (event.key !== "Escape") return;
    document.querySelectorAll(".workspace-picker[open]").forEach(menu => {
      const focused = menu.contains(document.activeElement);
      menu.open = false;
      if (focused) $("summary", menu)?.focus();
    });
  });
  document.addEventListener("click", event => {
    if (!(event.target instanceof Node)) return;
    document.querySelectorAll(".workspace-picker[open]").forEach(menu => {
      if (!menu.contains(event.target)) menu.open = false;
    });
  });
  // Register on body before app.js: the shell restores sidebar focus before
  // the app decides whether content needs its normal navigation focus fallback.
  document.body.addEventListener("htmx:beforeSwap", event => {
    const workspace = $("#workspace");
    if (event.detail?.target !== workspace || event.detail.shouldSwap === false) return;
    const nav = $("#project-navigation"), input = $("[data-sidebar-search]");
    const focused = desktop.matches && nav?.contains(document.activeElement) ? document.activeElement : null;
    sidebarView = {
      query: input?.value || "", searchOpen: !$("#sidebar-search")?.hidden,
      listScroll: $(".project-tree")?.scrollTop || 0, railScroll: nav?.scrollTop || 0,
      href: focused?.matches(".project-link, .shell-session-row > a, .sidebar-utility") ? focused.getAttribute("href") : null,
      controlProject: focused?.closest("[data-sidebar-project]")?.dataset.sidebarProject,
      control: ["[data-workspace-toggle]", "[data-shell-project-new]", "[data-shell-project-menu]"].find(selector => focused?.matches(selector)),
      searchFocused: !!focused && focused === input,
      selection: focused === input && input ? [input.selectionStart, input.selectionEnd] : null
    };
    workflowObserver?.disconnect();
    settingsReturn = null;
    const dialog = $("#settings-dialog");
    if (dialog?.open) dialog.close();
  });
  document.body.addEventListener("htmx:afterSwap", event => {
    if (event.detail?.target?.id === "workspace") initialize();
  });
  desktop.addEventListener("change", syncCollapse);
  initialize();
})();
