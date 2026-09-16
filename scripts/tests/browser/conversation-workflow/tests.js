(async () => {
  "use strict";
  const results = [], failures = [];
  const $ = selector => document.querySelector(selector);
  const visible = node => !!node && node.getClientRects().length > 0 && getComputedStyle(node).visibility !== "hidden";
  const tick = (ms = 30) => new Promise(resolve => setTimeout(resolve, ms));
  const assert = (condition, name) => (condition ? results : failures).push(name);
  const wait = async predicate => { for (let i = 0; i < 100; i++) { if (predicate()) return; await tick(); } throw new Error("Timed out waiting for browser state"); };
  const posts = action => fixture.requests.filter(request => request.options.method === "POST" && (!action || request.url.endsWith("/" + action)));
  const latest = action => posts(action).at(-1);
  const closeMenu = () => window.SnowMenus.close();
  const menu = kind => { closeMenu(); $(`[data-${kind}-menu]`).click(); };
  const item = key => $(`[data-menu-key="${key}"]`);
  const disabled = (kind, key) => { menu(kind); const value = key === "model-row" ? $("[data-model-id]")?.disabled : item(key)?.disabled; closeMenu(); return value; };
  const selectSession = id => { menu("session"); item("sessions").click(); item(id).click(); };
  const mode = value => { menu("mode"); item(value).click(); };
  const newSession = () => { menu("session"); item("new").click(); };
  const enter = (input, value) => {
    const prototype = input instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, "value").set.call(input, value);
    input.dispatchEvent(new Event("input", {bubbles: true}));
  };
  const draft = value => enter($("#live-prompt"), value);
  const update = fields => { Object.assign(fixture.snapshot, fields, {revision: fixture.snapshot.revision + 1}); };
  const respond = action => latest(action).resolve(fixture.snapshot);
  const sidebarCurrent = (id, title) => {
    const row = $("[data-shell-live-session]"), link = row?.querySelector("a"), label = link?.querySelector("span");
    const currentRows = [...document.querySelectorAll('[data-shell-session]')].filter(node => !node.hidden && node.querySelector('a[aria-current="page"]'));
    return row?.dataset.shellSession === id && currentRows.length === 1 && currentRows[0] === row && label?.textContent === title && label.title === title && new URL(link.getAttribute("href"), "http://fixture.invalid").searchParams.get("session") === id && link.hasAttribute("data-snow-navigation");
  };
  const load = async () => {
    menu("model");
    if (!latest("choices") || latest("choices").settled) item("load").click();
    fixture.choices.instance_id = fixture.snapshot.instance_id;
    latest("choices").resolve(fixture.choices);
    await wait(() => !!$("[data-model-id]"));
  };
  const search = value => {
    const input = $("[data-model-search]"); input.focus(); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set.call(input, value);
    input.dispatchEvent(new Event("input", {bubbles: true}));
  };
  const rows = () => [...document.querySelectorAll("[data-model-id]")].filter(visible);
  const key = (value, options = {}) => {
    const event = new KeyboardEvent("keydown", {key: value, bubbles: true, cancelable: true, ...options});
    document.activeElement.dispatchEvent(event); return event;
  };
  try {
    await wait(() => $("#live-connection").textContent === "Live");
    await window.testSavedHistoryTools(assert);
    const liveHeader = $("#live-session .workspace-heading"), liveHeaderRect = liveHeader?.getBoundingClientRect();
    assert(document.querySelectorAll(".workspace-heading").length === 1 && !!liveHeader, "live runtime owns the only workspace header");
    assert(Math.abs(liveHeaderRect.height - (innerWidth < 768 ? 40 : 76)) <= 2, "single live header uses compact mobile and 76px desktop geometry");
    assert([liveHeader.querySelector(".conversation-title"), ...liveHeader.querySelectorAll(".live-controls > button")].every(node => { const box = node.getBoundingClientRect(); return box.top >= liveHeaderRect.top - 1 && box.bottom <= liveHeaderRect.bottom + 1; }), "live header title, status and controls remain within its responsive bounds");
    assert(!!$("#live-composer [data-runtime-abort]") && !$(".workspace-heading [data-runtime-abort]") && document.querySelectorAll("[data-runtime-abort]").length === 1, "Stop exists only once inside the composer, never in the header");
    assert(!visible($(".connection-line")) && $("#live-connection").dataset.connected === "true", "healthy connection line is hidden without removing accessible runtime state");
    assert(visible($("#live-send")) && !visible($("[data-runtime-abort]")) && $("[data-runtime-abort]").disabled, "idle composer displays Send and hides disabled Stop");
    assert(posts().length === 0, "passive activation view and saved history never discover models or mutate runtime");
    menu("telemetry");
    assert($("[data-workflow-usage]").textContent === "Unknown" && $("[data-workflow-context]").textContent === "Unknown" && $("[data-workflow-cost]").textContent === "Unknown" && !!item("telemetry-details") && !$(".telemetry-menu .snow-menu-note,.telemetry-menu .session-cost-note"), "compact unavailable telemetry stays unknown, with explanations behind Details");
    assert($("[data-live-title]").textContent.includes("First task"), "current session has authoritative title before choices load");
    menu("model");
    assert(posts("choices").length === 1 && posts().length === 1, "explicit model trigger click automatically sends exactly one discovery request");
    assert(visible($("[data-model-search]")) && document.activeElement === $("[data-model-search]"), "direct model list search autofocuses while discovery is pending");
    assert(!item("models") && !item("back") && !document.querySelector(".snow-menu").textContent.includes("Load host models"), "model picker has no Model/back submenu or extra Load host models step");
    assert(!item("load") || item("load").disabled, "pending discovery offers no enabled duplicate loading action");
    item("load")?.click(); menu("model");
    assert(posts("choices").length === 1, "closing and reopening while discovery is pending never duplicates its request");
    search("OtHeR");
    update({session_name: "First task", model: "temporary-model"});
    await wait(() => $("[data-model-menu]").title.includes("temporary-model"));
    assert($("[data-model-search]").value === "OtHeR" && document.activeElement === $("[data-model-search]"), "authoritative snapshot repaint preserves typed query and search focus");
    update({model: "host-model"});
    latest("choices").resolve(fixture.choices);
    await wait(() => rows().length === 1);
    assert($("[data-model-search]").value === "OtHeR" && document.activeElement === $("[data-model-search]") && rows()[0].dataset.modelProvider === "other-host", "async discovery preserves query/focus and immediately filters its response");
    assert(posts("model").length === 0, "a single search match never automatically selects a model");
    search("");
    assert(latest("choices").fields.csrf === "test-csrf" && latest("choices").fields.instance_id === "instance-one", "explicit choices request binds CSRF and immutable instance");
    assert(document.querySelectorAll(".snow-menu-group").length === 2 && !!$('[data-model-provider="host-provider"][data-model-id="host-model"]'), "provider groups use actual host model inventory");
    assert(item("load").textContent.trim() === "Refresh models" && !item("load").disabled, "cached inventory exposes explicit Refresh models");
    const discovered = posts("choices").length;
    for (const [query, provider, id] of [["OTHER-HOST", "other-host", "host-model"], ["SPECIAL-ID", "host-provider", "special-id"], ["DISTINCT DISPLAY", "host-provider", "special-id"]]) {
      search(query);
      assert(rows().length === 1 && rows()[0].dataset.modelProvider === provider && rows()[0].dataset.modelId === id, "local case-insensitive search matches " + query);
    }
    search("no-model-matches-this-query");
    assert(rows().length === 0 && /no .*match/i.test($(".snow-menu").textContent), "unmatched search shows an explicit no-match state and no model rows");
    for (const value of ["Home", "End", " "]) {
      const event = key(value);
      assert(!event.defaultPrevented && document.activeElement === $("[data-model-search]"), "search preserves native " + JSON.stringify(value) + " editing even with no matches");
    }
    search("  host-model  ");
    assert(rows().length === 2, "search trims surrounding whitespace and matches duplicate IDs across providers");
    search("host-model");
    const composingInput = $("[data-model-search]"), beforeComposition = posts().length;
    for (const value of ["Escape", "ArrowDown"]) {
      const event = key(value, {isComposing: true});
      assert(!event.defaultPrevented && visible($(".snow-menu")) && document.activeElement === composingInput && composingInput.value === "host-model" && posts().length === beforeComposition, "composing " + value + " preserves input focus/query without closing, navigating, or submitting");
    }
    search(""); key("ArrowDown");
    assert(document.activeElement === rows()[0], "ArrowDown from search focuses the first model row rather than Refresh");
    key("ArrowUp");
    assert(document.activeElement === $("[data-model-search]"), "ArrowUp from first model row returns to search");
    assert(posts("choices").length === discovered && posts("model").length === 0, "typing and keyboard navigation filter locally without discovery or selection");
    key("Escape");
    assert(!$(".snow-menu") && document.activeElement === $("[data-model-menu]"), "Escape closes picker directly and restores its trigger focus");
    menu("model");
    assert(rows().length === 3 && posts("choices").length === discovered && document.activeElement === $("[data-model-search]"), "reopening cached picker shows choices directly with search autofocus and no discovery");
    search("DISTINCT"); item("load").click();
    assert(posts("choices").length === discovered + 1 && item("load").disabled && rows().every(row => row.disabled), "explicit refresh sends one request and disables model switching and duplicate refresh");
    $("[data-model-search]").focus();
    latest("choices").reject();
    await wait(() => item("load") && !item("load").disabled);
    assert(item("load").textContent.trim() === "Retry loading models" && $("[data-workflow-load-status]").textContent.trim(), "discovery error exposes explicit Retry loading models and visible status");
    assert($("[data-model-search]").value === "DISTINCT" && document.activeElement === $("[data-model-search]"), "refresh error preserves search query and focus");
    closeMenu(); menu("model");
    assert(posts("choices").length === discovered + 1, "reopening after refresh failure does not retry discovery implicitly");
    item("load").click(); $("[data-model-search]").focus(); search("DISTINCT");
    latest("choices").resolve(fixture.choices);
    await wait(() => item("load")?.textContent.trim() === "Refresh models" && !item("load").disabled);
    assert(rows().length === 1 && $("[data-model-search]").value === "DISTINCT" && document.activeElement === $("[data-model-search]"), "explicit retry restores inventory without losing query/focus or selecting a match");
    item("load").click();
    latest("choices").resolve({...fixture.choices, instance_id: "stale-instance", models: [{provider: "stale-provider", id: "stale-model", name: "Stale result"}]});
    await wait(() => !item("load").disabled);
    search("");
    assert(rows().length === 3 && !$("[data-model-provider=stale-provider]") && posts("model").length === 0, "discovery response with a stale nonce cannot replace cached inventory or select a model");
    const postCount = posts().length;
    $('[data-model-provider="host-provider"][data-model-id="host-model"]').click();
    assert(posts().length === postCount && !$(".snow-menu"), "selecting current exact pair closes without startup or mutation");
    menu("model");
    $('[data-model-provider="other-host"][data-model-id="host-model"]').click();
    assert(posts("model").length === 1 && latest("model").fields.provider === "other-host" && latest("model").fields.model === "host-model", "direct model row sends exact host provider/model pair without Apply");
    assert($("#live-send").disabled && disabled("mode", "plan"), "mutation disables composer and competing controls");
    update({provider: "other-host", model: "host-model"}); respond("model");
    await wait(() => !$("#live-send").disabled);
    mode("plan");
    assert(latest("mode").fields.mode === "plan" && $("[data-mode-label]").textContent === "Default", "mode mutation is explicit and UI waits for authoritative snapshot");
    update({mode: "plan", activities: [{id: "activity-1", tool: "read", status: "completed", summary: "Read source", output: "Public source excerpt", truncated: false}], messages: [{id: "plan-1", role: "plan", text: "## Public plan\n\nReview the code.", html: "<h2>Public plan</h2><p>Review the code.</p>", truncated: false}], telemetry: {available: true, context_available: true, estimated: true, input_tokens: 120, output_tokens: 30, total_tokens: 150, context_tokens: 500, context_window: 65536}}); respond("mode");
    await wait(() => $("[data-mode-label]").textContent === "Plan Mode");
    menu("mode");
    assert(item("plan").getAttribute("aria-checked") === "true", "runtime Plan Mode is clearly authoritative");
    assert($("#live-transcript h2")?.textContent === "Public plan", "public plan-role message renders through Markdown");
    assert(!$("#live-activities").hidden && $(".tool-activity").dataset.toolName === "read" && $(".activity-tool").textContent === "Read" && $(".activity-output").textContent.includes("Public source excerpt"), "public tool timeline remains visible alongside plan messages");
    menu("telemetry");
    assert($("[data-workflow-usage]").textContent.includes("150 total") && $("[data-workflow-context]").textContent.includes("500 tokens"), "usage totals and estimated context render available telemetry");
    menu("session"); item("rename").click();
    assert($("#workflow-rename-dialog").open && document.activeElement.closest("#workflow-rename-dialog"), "rename uses accessible native modal focus containment");
    enter($("#workflow-name"), "Renamed conversation");
    $("[data-workflow-rename-form]").requestSubmit();
    assert(latest("rename").fields.name === "Renamed conversation", "rename targets current conversation via typed action");
    update({session_name: "Renamed conversation"}); respond("rename");
    await wait(() => !$("#workflow-rename-dialog").open);
    assert($("[data-live-title]").textContent.includes("Renamed conversation") && sidebarCurrent("session-one", "Renamed conversation"), "rename updates actual header and unique current sidebar row title, identity and route without page reload");

    draft("Unsent first-session draft");
    update({status: "running"});
    await wait(() => $("#live-status").textContent === "Working");
    assert(!visible($("#live-send")) && visible($("#live-composer [data-runtime-abort]")) && !$("[data-runtime-abort]").disabled, "active turn replaces Send with the enabled composer Stop");
    fixture.offline = true;
    await wait(() => $("#live-connection").textContent === "Reconnecting…");
    assert(visible($(".connection-line")) && $("#live-connection").dataset.connected === "false", "unhealthy connection restores the visible reconnect row");
    assert(visible($("[data-runtime-abort]")) && $("[data-runtime-abort]").disabled && !visible($("#live-send")), "unsafe disconnected active turn keeps Stop visible but disabled and Send hidden");
    const beforeStopReconnect = posts().length;
    $("[data-runtime-abort]").click();
    assert(posts().length === beforeStopReconnect, "disabled composer Stop cannot submit an abort while disconnected");
    fixture.offline = false;
    await wait(() => $("#live-connection").textContent === "Live");
    assert(!visible($(".connection-line")) && !$("[data-runtime-abort]").disabled && posts().length === beforeStopReconnect, "reconnection hides the healthy row and reenables Stop without replay");
    $("[data-runtime-abort]").click();
    assert(latest("abort").fields.csrf === "test-csrf" && latest("abort").fields.instance_id === "instance-one", "composer Stop uses the existing explicit CSRF/instance-bound abort action");
    assert($("[data-runtime-abort]").disabled && visible($("[data-runtime-abort]")) && !visible($("#live-send")), "pending abort disables Stop without prematurely restoring Send");
    update({status: "idle"}); respond("abort");
    await wait(() => !$("#live-send").disabled);
    assert(visible($("#live-send")) && !visible($("[data-runtime-abort]")) && $("[data-runtime-abort]").disabled && $("#live-prompt").value === "Unsent first-session draft", "authoritative idle abort response restores Send, hides Stop, and preserves the unsent draft");
    update({status: "running"});
    await wait(() => $("#live-status").textContent === "Working");
    assert(disabled("mode", "plan") && disabled("model", "model-row") && disabled("model", "load") && !disabled("session", "sessions"), "active turn blocks model/mode but permits confirmed session switching");
    selectSession("session-two");
    assert($("#workflow-switch-dialog").open && posts("switch").length === 0, "active switch first asks native confirmation and sends no mutation");
    $("#workflow-switch-dialog [data-workflow-cancel]").click();
    assert(!$("#workflow-switch-dialog").open && posts("switch").length === 0, "canceling switch preserves active conversation without stop");
    selectSession("session-two");
    $("[data-workflow-switch-confirm]").click();
    assert(latest("switch").fields.session_id === "session-two" && latest("switch").fields.confirm_stop === "stop" && latest("switch").fields.instance_id === "instance-one", "only acknowledged switch carries confirm_stop and old immutable identity");
    update({session_id: "session-two", instance_id: "instance-two", session_name: "Second task", status: "idle", messages: [], mode: "default"}); respond("switch");
    await wait(() => $("#live-session").dataset.instance === "instance-two" && !$("#live-send").disabled);
    assert($("#live-prompt").value === "" && $("[data-live-title]").textContent.includes("Second task") && sidebarCurrent("session-two", "Second task"), "intentional switch adopts nonce, isolated draft and actual sidebar current-row identity/title/route without a duplicate active catalog row");
    menu("model");
    assert(!!$("[data-model-search]") && !$("[data-model-id]") && $("[data-mode-label]").textContent === "Default", "switch invalidates nonce-bound choices and applies actual restored mode");
    draft("Second session draft"); await load();
    selectSession("session-one");
    assert(!latest("switch").fields.confirm_stop && latest("switch").fields.instance_id === "instance-two", "idle switch needs no stop confirmation and binds new instance");
    update({session_id: "session-one", instance_id: "instance-three", session_name: "Renamed conversation", messages: []}); respond("switch");
    await wait(() => $("#live-session").dataset.instance === "instance-three");
    assert($("#live-prompt").value === "Unsent first-session draft" && sidebarCurrent("session-one", "Renamed conversation"), "switching back restores prior draft and the exact sidebar current identity/title/route");

    fixture.offline = true;
    await wait(() => $("#live-connection").textContent === "Reconnecting…");
    const beforeReconnect = posts().length;
    assert($("#live-send").disabled && $("[data-runtime-close]").disabled && disabled("session", "new"), "offline connection disables all runtime mutations");
    assert($("#live-prompt").value === "Unsent first-session draft", "disconnect preserves draft");
    fixture.offline = false;
    await wait(() => !$("#live-send").disabled);
    assert(posts().length === beforeReconnect, "reconnection polls state only and never replays actions");
    $("#live-composer").requestSubmit(); latest("prompt").reject();
    await wait(() => !$("#live-unknown").hidden && !$("[data-runtime-reviewed]").disabled);
    const promptCount = posts("prompt").length;
    assert($("#live-send").disabled && $("#live-prompt").value === "Unsent first-session draft", "unknown prompt outcome retains draft but blocks duplicate send despite idle polls");
    $("#live-composer").dispatchEvent(new Event("submit", {bubbles: true, cancelable: true}));
    assert(posts("prompt").length === promptCount, "unknown-outcome guard blocks synthetic submit too");
    await testNavigate();
    await wait(() => !$("[data-runtime-reviewed]").disabled);
    assert(!$("#live-unknown").hidden && $("#live-send").disabled && $("#live-prompt").value === "Unsent first-session draft", "Native navigation preserves draft and unknown-outcome guard in memory");
    $("[data-runtime-reviewed]").click();
    assert(!$("#live-send").disabled && posts("prompt").length === promptCount, "explicit review unlocks sending without replaying prompt");

    menu("model");
    const disposedChoices = latest("choices"), beforeDisposal = posts("choices").length;
    await testNavigate();
    await wait(() => !$("#live-send").disabled);
    assert(posts("choices").length === beforeDisposal, "passive native replacement does not repeat an abandoned discovery request");
    await load();
    disposedChoices.resolve({...fixture.choices, models: [{provider: "disposed-provider", id: "disposed-model", name: "Disposed result"}]});
    await tick(100);
    assert(rows().length === 3 && !$("[data-model-provider=disposed-provider]") && !item("load").disabled, "late discovery from disposed workspace cannot replace fresh choices or loading state");
    newSession();
    assert(latest("switch").fields.session_id === "" && !latest("switch").fields.confirm_stop, "New conversation is an explicit empty-session switch, not close/reopen");
    update({session_id: "session-new", instance_id: "instance-new", session_name: "", messages: []}); respond("switch");
    await wait(() => $("#live-session").dataset.instance === "instance-new");
    draft("New conversation draft");
    update({status: "input", input: {id: "old-instance-question", questions: [{id: "old-answer", header: "Old runtime question", question: "Keep this draft scoped to the old runtime"}]}});
    await wait(() => visible($("#live-attention")) && !visible($("#live-composer")));
    const oldAnswer = $("#live-attention textarea");
    oldAnswer.value = "Private old question draft"; oldAnswer.dispatchEvent(new Event("input", {bubbles: true}));
    update({instance_id: "foreign-instance", session_id: "foreign-session", session_name: "Foreign task", status: "idle", input: null});
    await wait(() => $("#live-connection").textContent === "Session changed");
    assert(visible($(".connection-line")), "passive runtime replacement keeps the review/reload connection row visible");
    assert($("#live-session").dataset.instance === "instance-new" && $("#live-send").disabled && !$("[data-runtime-reload]").hidden, "passive replacement freezes old identity and requires explicit review/reload");
    assert($("#live-prompt").value === "New conversation draft" && visible($("#live-composer")) && !visible($("#live-attention")) && !oldAnswer.isConnected && !$("#live-attention").querySelector("form") && $("#live-send").disabled, "foreign snapshot removes old attention takeover and question DOM, releases preserved normal draft, and never grants the replacement runtime send authority");
    $("[data-runtime-reload]").click();
    await wait(() => $("#live-session").dataset.instance === "foreign-instance" && !$("#live-send").disabled);
    assert($("#live-prompt").value === "", "explicit workspace review adopts replacement without leaking old draft");
    fixture.unauthorized = true;
    await wait(() => $("#live-connection").textContent === "Login required");
    assert(visible($(".connection-line")), "expired authentication keeps the login-required connection row visible");
    assert($("#live-send").disabled && !$("[data-runtime-reload]").hidden, "expired browser authentication gets distinct login-required connection status");
    fixture.unauthorized = false;
    $("[data-runtime-reload]").click();
    await wait(() => !$("#live-send").disabled);

    // Stale poll replies after cancellation must not invalidate intentional switch.
    fixture.deferPoll = true;
    await wait(() => fixture.deferredPolls.length > 0);
    const stalePoll = fixture.deferredPolls.at(-1), staleSnapshot = {...fixture.snapshot};
    newSession();
    update({session_id: "last-session", instance_id: "last-instance", session_name: "Last task"}); respond("switch");
    await wait(() => $("#live-session").dataset.instance === "last-instance");
    fixture.deferPoll = false; stalePoll.resolve(staleSnapshot);
    for (const request of fixture.deferredPolls) if (request !== stalePoll) request.resolve(fixture.snapshot);
    await tick(100);
    assert($("#live-connection").textContent === "Live" && $("#live-session").dataset.instance === "last-instance", "late canceled poll cannot overwrite intentional switch nonce");
    assert(posts().every(request => request.fields.csrf === "test-csrf" && request.fields.instance_id), "every runtime mutation and explicit metadata request is instance-bound and CSRF-bound");
    assert(posts("prompt").length === 1 && posts("close").length === 0 && posts("open").length === 0, "session workflow sends no implicit prompt, worker activation or manual close");

    await load();
    assert($("#live-composer").getBoundingClientRect().bottom <= innerHeight && document.documentElement.scrollWidth <= innerWidth, "expanded workflow keeps sticky composer reachable at responsive width");
    closeMenu();
    const composer = $("#live-composer").getBoundingClientRect();
    assert(document.documentElement.scrollWidth <= innerWidth && composer.left >= 0 && composer.right <= innerWidth && composer.bottom <= innerHeight, "responsive workspace has no horizontal overflow and composer stays visible");
    if (innerWidth < 768) {
      $("[data-nav-toggle]").click();
      await tick();
      assert($("#project-navigation").getAttribute("aria-modal") === "true" && $("#workspace-content").inert, "mobile project drawer retains modal behavior below 768px");
      $("[data-nav-close]").click();
    } else assert(!$("#project-navigation").inert && getComputedStyle($("#project-navigation")).visibility === "visible", "desktop project navigation remains accessible from 768px");
    $(".workspace-heading [data-inspector-toggle]").click();
    assert(!$("#project-inspector").hidden && $("#project-inspector").contains(document.activeElement) && (innerWidth < 768 ? $(".conversation-pane").inert && $("#project-inspector").getAttribute("aria-modal") === "true" : !$(".conversation-pane").inert && !$("#project-inspector").hasAttribute("aria-modal")), "Files / Changes inspector isolates mobile focus and leaves desktop conversation available");
    latest("files").resolve({path: "", entries: [], next_offset: 0, has_more: false, limited: false});
    $("#inspection-tab-changes").click(); latest("changes").resolve({available: true, reason: "", changes: [{path: "source.js", kind: "unstaged", status: "Modified"}], limited: false});
    await wait(() => !!$("[data-inspection-change]"));
    assert($("[data-inspection-change]").dataset.inspectionChange === "source.js", "Files / Changes inspection still loads read-only changes without activating a worker");
    $("#project-inspector [data-inspector-toggle]").click();
    assert(!$(".conversation-pane").inert && $("#project-inspector").hidden && document.activeElement === $(".workspace-heading [data-inspector-toggle]"), "closing inspector restores conversation interaction and trigger focus");
    // Prior workflow dialogs must not steal focus from Settings. Run both close
    // paths before and after a genuine production native workspace lifecycle.
    for (const lifecycle of ["initial", "after native replacement"]) {
      if (lifecycle !== "initial") {
        await testNavigate();
        await wait(() => $("#live-connection").textContent === "Live" && !$("#live-send").disabled);
      }
      for (const dismiss of ["Close button", "native Escape"]) {
        menu("session"); item("rename").click();
        await wait(() => $("#workflow-rename-dialog").open);
        const renameClosed = new Promise(resolve => $("#workflow-rename-dialog").addEventListener("close", resolve, {once: true}));
        $("#workflow-rename-dialog [data-workflow-cancel]").click();
        await renameClosed;
        await wait(() => !$("#workflow-rename-dialog").open && document.activeElement === $("[data-session-menu]"));
        assert(document.activeElement === $("[data-session-menu]"), `${lifecycle}: canceled Rename restores the session trigger before Settings`);
        if (innerWidth < 768 && $("#project-navigation").inert) $("[data-nav-toggle]").click();
        const settingsTrigger = $("[data-settings-open]");
        settingsTrigger.focus(); settingsTrigger.click();
        await wait(() => $("#settings-dialog").open && $("#settings-dialog").contains(document.activeElement));
        if (dismiss === "Close button") $("[data-settings-close]").click();
        else {
          fixture.nativeEscape = true;
          await wait(() => !fixture.nativeEscape);
        }
        await wait(() => !$("#settings-dialog").open);
        await tick(50);
        assert(document.activeElement === settingsTrigger && document.activeElement !== $("[data-session-menu]"), `${lifecycle}: Settings ${dismiss} returns focus to Settings, not the prior Rename/session trigger (actual ${document.activeElement?.outerHTML.slice(0, 300)})`);
        if (innerWidth < 768) $("[data-nav-close]").click();
      }
    }
    // A mutation response is not the next authoritative read. Hold that read
    // past an immediately terminal prompt response to expose stale-idle Send.
    fixture.deferPoll = true;
    await wait(() => fixture.deferredPolls.length > 0);
    draft("Fast terminal response");
    $("#live-composer").dispatchEvent(new Event("submit", {bubbles: true, cancelable: true}));
    assert($("#live-send").disabled && latest("prompt").fields.text === "Fast terminal response", "Delayed prompt POST immediately disables duplicate sends");
    update({status: "idle", messages: [{id: "fast-terminal", role: "assistant", text: "Already complete"}]}); respond("prompt");
    await wait(() => $("#live-connection").textContent === "Synchronizing…");
    assert($("#live-send").disabled && $('[data-runtime-abort]').disabled, "Terminal POST response cannot authorize Send or Stop before a fresh bound read");
    fixture.deferPoll = false;
    for (const pending of fixture.deferredPolls) pending.resolve(fixture.snapshot);
    await wait(() => $("#live-connection").textContent === "Live" && !$("#live-send").disabled);
    assert($("#live-transcript").textContent.includes("Already complete") && !visible($("[data-runtime-abort]")), "Fresh terminal snapshot restores idle Send without synthetic running state overwriting a fast reply");
    fixture.closed = true;
    const beforeClosed = posts().length;
    await wait(() => $("#live-connection").textContent === "Runtime closed");
    assert($("#live-send").disabled && !$("[data-runtime-reload]").hidden && visible($(".connection-line")), "external 404 closes runtime authority and exposes explicit review without activation");
    fixture.closed = false;
    await tick(180);
    assert($("#live-connection").textContent === "Runtime closed" && posts().length === beforeClosed, "closed runtime never auto-reopens, resumes polling authority, or replays actions");
    assert(fixture.storageWrites.every(key => key === "snow-manager-theme"), "drafts and action guards are never written to localStorage or sessionStorage");
    assert(fixture.errors.length === 0, "no uncaught browser errors: " + fixture.errors.join("; "));
  } catch (error) { failures.push(error.stack || String(error)); }
  $("#test-result").textContent = JSON.stringify({passed: results.length, failures, results});
})();
