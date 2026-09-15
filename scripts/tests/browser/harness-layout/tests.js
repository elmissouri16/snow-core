// Assertions run against production-rendered pages, never fixture DOM copies.
(async () => {
  "use strict";
  // Observe rendered state after the synthetic event's React commit.
  const settleFrame = () => new Promise(resolve => requestAnimationFrame(resolve));
  const enter = (input, value) => {
    const prototype = input instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    Object.getOwnPropertyDescriptor(prototype, "value").set.call(input, value);
    input.dispatchEvent(new Event("input", {bubbles: true}));
  };
  const fixture = window.harnessFixture;
  const results = [], failures = [], measurements = {};
  const $ = selector => document.querySelector(selector);
  const rect = node => node.getBoundingClientRect();
  const visible = node => !!node && node.getClientRects().length > 0 && getComputedStyle(node).visibility !== "hidden";
  const check = (condition, label) => { (condition ? results : failures).push(label); };
  const near = (actual, expected, tolerance = 2) => Math.abs(actual - expected) <= tolerance;
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  const settle = async predicate => {
    for (let i = 0; i < 100; i++) { if (predicate()) return; await wait(30); }
    throw new Error("Production UI did not settle");
  };
  const noOverflow = label => check(document.documentElement.scrollWidth <= innerWidth + 1 && document.body.scrollWidth <= innerWidth + 1, label);
  const desktop = innerWidth >= 768;
  const home = ["home", "empty-home"].includes(fixture.name);
  const inactive = fixture.name.startsWith("inactive");
  const polishNames = ["many-home", "registration", "registration-error", "pairing", "login", "login-error", "questions", "permission-unknown", "permission-truncated", "stream", "usage-known"];
  if (polishNames.includes(fixture.name)) return await polish();
  async function polish() {
    const inViewport = node => { const box = rect(node); return box.width > 0 && box.height > 0 && box.left >= -1 && box.right <= innerWidth + 1 && box.top >= -1 && box.bottom <= innerHeight + 1; };
    const hit = node => { const box = rect(node); const top = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2); return top === node || node.contains(top); };
    const update = async fields => { fixture.update(fields); await settle(() => fixture.deliveredRevision === fixture.snapshot.revision); await wait(50); };
    const activeField = () => [...document.querySelectorAll("#live-attention fieldset")].find(node => !node.hidden);
    async function stableSeat(body, footer) {
      // Live transport readiness and fonts.ready can precede the attention
      // owner's rAF layout. Establish a settled baseline before attributing a
      // footer movement to scrolling; keep the post-scroll assertion strict.
      let previous = "", unchanged = 0;
      await settle(() => {
        const seat = $("#live-composer-seat"), box = rect(footer);
        const current = JSON.stringify([box.top, box.width, box.height, body.clientHeight, body.scrollHeight, seat.style.getPropertyValue("--attention-seat-height")]);
        unchanged = current === previous ? unchanged + 1 : 0; previous = current;
        return unchanged >= 3 && parseFloat(seat.style.getPropertyValue("--attention-seat-height")) > 0;
      });
    }
    try {
      await settle(() => document.readyState === "complete" && (!fixture.snapshot || $("#live-connection")?.textContent === "Live"));
      await document.fonts.ready;
      check(document.documentElement.dataset.theme === fixture.theme, "Requested light/dark theme is active");
      noOverflow("Contentful fixture has no horizontal page overflow");
      if (fixture.name.startsWith("login")) {
        const form = $('form[action="/login"]'), code = $("#code");
        check(code.type === "password" && code.required && code.maxLength === 128 && form.method === "post", "Login uses bounded password input and explicit POST, never URL credentials");
        code.focus(); code.value = "fixture-only";
        check(document.activeElement === code && form.checkValidity(), "Pairing field accepts keyboard focus and valid input");
        form.querySelector("button").focus();
        check(document.activeElement === form.querySelector("button") && hit(form.querySelector("button")), "Login submit is keyboard reachable and not covered after focus scroll");
        check(fixture.name !== "login-error" || visible($('[role="alert"]')), "Invalid pairing has an accessible error");
        check(fixture.nonInventoryRequests().length === 0, "Inspecting login never submits authentication");
      } else if (fixture.name === "pairing") {
        check($("#pairing-code").readOnly && $("#pairing-code").value === "fixture-public-pairing-code-not-a-credential", "Pairing result renders only fictional explicit code");
        const revoke = $('form[action="/access/revoke-all"]');
        check(!revoke.checkValidity() && revoke.querySelector('[name="confirm"]').required, "Revocation is invalid until operator confirms");
        for (const form of document.querySelectorAll(".workspace form")) check(form.method === "post" && !!form.querySelector('[name="csrf"]'), "Access action retains POST and CSRF");
        check(fixture.nonInventoryRequests().length === 0, "Pairing page never performs implicit access mutation");
      } else if (fixture.name === "many-home" || fixture.name.startsWith("registration")) {
        check(document.querySelectorAll("[data-sidebar-project]").length === 100, "One hundred real long-name project rows are rendered");
        if (innerWidth < 768) $("[data-nav-toggle]").click(); await settleFrame();
        $("[data-sidebar-search-toggle]").click(); await settleFrame();
        const search = $("[data-sidebar-search]"); enter(search, "Workspace 098"); await settleFrame();
        check([...document.querySelectorAll("[data-sidebar-project]")].filter(node => !node.hidden).length === 1 && document.activeElement === search, "Long catalog search narrows content and retains editor focus");
        $("[data-sidebar-search-toggle]").click(); await settleFrame();
        if (innerWidth < 768) $("[data-nav-close]").click(); await settleFrame();
        if (fixture.name.startsWith("registration")) {
          const form = $('form[action="/projects/add"]');
          check(form?.method === "post" && !!form.querySelector('[name="csrf"]'), "Project registration remains explicit CSRF-bound POST");
          check(fixture.name !== "registration-error" || visible($('[role="alert"]')), "Registration failure is visible and announced");
        }
        check(fixture.nonInventoryRequests().length === 0, "Long project browsing starts no worker or registration");
      } else if (fixture.name.startsWith("permission-")) {
        const card = $("#live-attention .attention-card"), body = $("#live-attention .attention-body"), footer = $("#live-attention .attention-footer");
        await stableSeat(body, footer);
        check(visible(card) && !visible($("#live-composer")), "Approval replaces rather than stacks the normal composer");
        check(!!$("#live-composer-seat") && $("#live-composer-seat").contains(card), "Approval occupies the single composer seat");
        check(inViewport(footer) && hit(footer.querySelector("button")), "Approval actions are visible and pointer-reachable at short height");
        const before = rect(footer).top;
        measurements.approvalBefore = {footer: before, bodyHeight: body.clientHeight, scrollHeight: body.scrollHeight, seatHeight: $("#live-composer-seat").style.getPropertyValue("--attention-seat-height")};
        body.scrollTop = body.scrollHeight; await wait(30);
        measurements.approvalAfter = {footer: rect(footer).top, bodyHeight: body.clientHeight, scrollTop: body.scrollTop, seatHeight: $("#live-composer-seat").style.getPropertyValue("--attention-seat-height")};
        check(near(rect(footer).top, before) && body.scrollTop > 0, "Long approval scrolls only its body, keeping actions fixed");
        const allow = $('[data-permission="allow"]'), reject = $('[data-permission="deny"]');
        check(!!reject && !reject.disabled, "Reject remains an explicit enabled safe escape");
        check(fixture.name !== "permission-truncated" || allow?.disabled, "Truncated approval cannot be allowed");
        check(fixture.name !== "permission-unknown" || /unknown|incomplete|not.*sandbox/i.test(card.textContent), "Unknown effects retain a visible authority warning");
        reject.focus(); check(document.activeElement === reject && hit(reject), "Approval reject is reachable by focus after body scrolling");
        fixture.offline = true; await settle(() => $("#live-connection").textContent === "Reconnecting…");
        check([...card.querySelectorAll("button,input,textarea")].every(node => node.disabled), "Disconnected approval disables all mutation controls");
      } else if (fixture.name === "questions") {
        const region = $("#live-attention"), body = region.querySelector(".attention-body"), footer = region.querySelector(".attention-footer");
        await stableSeat(body, footer);
        check(!!window.SnowAttention && !!$("#live-composer-seat"), "Production attention owner and single composer seat are mounted");
        check(!visible($("#live-composer")) && visible(region), "Pending question replaces normal composer without removing it");
        check(region.querySelectorAll("fieldset").length === 16 && region.querySelectorAll('fieldset:not([hidden])').length === 1, "Sixteen-question batch displays exactly one page");
        check(activeField().querySelectorAll('input[type="radio"]:not([data-other])').length === 32, "First page retains all 32 bounded options");
        check(inViewport(footer) && hit(footer.querySelector("button:not(:disabled)")), "Question footer is visible and hit-test reachable before scrolling");
        const footerTop = rect(footer).top; body.scrollTop = body.scrollHeight; await wait(30);
        check(body.scrollTop > 0 && near(rect(footer).top, footerTop), "Long options scroll independently of the fixed footer");
        const normal = $("#live-prompt"); enter(normal, "retained ordinary composer draft");
        const collapse = $("[data-attention-collapse]"); collapse.click(); await settleFrame();
        check(!visible(body) && !visible(footer) && !visible($("#live-composer")), "Collapsed question is the only resident seat, not a restored regular composer");
        collapse.click(); await settleFrame();
        const field = activeField(), other = field.querySelector("textarea");
        other.focus(); enter(other, "Custom draft survives snapshot");
        await update({messages: [...fixture.snapshot.messages, {id: "during-question", role: "assistant", text: "Public incremental activity while waiting"}]});
        check(activeField() === field && other.value === "Custom draft survives snapshot" && document.activeElement === other, "Same-request incremental snapshot preserves exact question DOM, draft, and focus");
        other.dispatchEvent(new CompositionEvent("compositionstart", {bubbles: true}));
        other.dispatchEvent(new KeyboardEvent("keydown", {key: "Enter", isComposing: true, bubbles: true, cancelable: true}));
        check(activeField() === field && !fixture.requests.some(r => r.path.endsWith("/input")), "IME composition Enter neither advances nor submits");
        other.dispatchEvent(new CompositionEvent("compositionend", {bubbles: true}));
        const recommended = field.querySelector('input[type="radio"]:not([data-other])');
        check(recommended.value === fixture.snapshot.input.questions[0].options[0].label, "Recommendation badge never alters the exact transport option label");
        recommended.click(); await settleFrame(); await wait(30);
        check(activeField().dataset.questionId === "question-01" && !activeField().querySelector("textarea,[data-other]"), "Selecting option advances to choices-only page without custom input");
        $("[data-attention-page='-1']").click(); await settleFrame();
        check(activeField() === field && recommended.checked && other.value === "Custom draft survives snapshot", "Paging back preserves selected answer and custom draft");
        $("[data-attention-page='1']").click(); await settleFrame();
        activeField().querySelector('input[type="radio"]').click(); await settleFrame(); await wait(30);
        check(activeField().dataset.questionId === "question-02" && !!activeField().querySelector("textarea") && !activeField().querySelector('input[type="radio"]'), "Freeform-only page has textarea without fabricated choices");
        const text = activeField().querySelector("textarea"); text.focus(); enter(text, "First line\n" + "Long answer line\n".repeat(15));
        check(text.scrollHeight >= text.clientHeight && inViewport(footer), "Growing custom answer has bounded editor overflow and visible actions");
        await update({input: {...fixture.snapshot.input, id: "replacement-request", tool_call_id: "replacement-request"}});
        check(activeField().dataset.questionId === "question-00" && !activeField().querySelector("textarea").value, "New request identity resets answers and page instead of inheriting old draft");
        check(normal.value === "retained ordinary composer draft", "Attention arrival and replacement retain normal composer draft");
        fixture.offline = true; await settle(() => $("#live-connection").textContent === "Reconnecting…");
        check([...region.querySelectorAll("button,input,textarea")].every(node => node.disabled), "Disconnected questions disable editing, paging, collapse, submit, and Stop");
        fixture.offline = false; await settle(() => $("#live-connection").textContent === "Live");
        const request = structuredClone(fixture.snapshot.input), form = region.querySelector("form");
        form.requestSubmit();
        check(!fixture.requests.some(r => r.path.endsWith("/input")) && !$("#attention-feedback").hidden, "Incomplete batch cannot submit, and validation identifies a missing answer");
        fixture.allowActions = true;
        for (let i = 0; i < 16; i++) {
          const field = activeField(), radio = field.querySelector('input[type="radio"]:not([data-other])');
          if (radio) { radio.click(); await settleFrame(); }
          else {
            const editor = field.querySelector("textarea"); editor.focus(); enter(editor, `Exact custom answer ${i}\nSecond line`);
            editor.dispatchEvent(new KeyboardEvent("keydown", {key: "Enter", shiftKey: true, bubbles: true, cancelable: true}));
            check(activeField() === field, `Question ${i + 1}: Shift+Enter retains the multiline custom page`);
            $("[data-attention-continue]").click(); await settleFrame();
          }
        }
        $("[data-attention-continue]").click(); await settleFrame();
        await settle(() => fixture.requests.some(r => r.path.endsWith("/input")));
        const posts = fixture.requests.filter(r => r.path.endsWith("/input")), submitted = JSON.parse(posts[0].fields.answers);
        check(posts.length === 1 && posts[0].fields.request_id === request.id && posts[0].fields.instance_id === fixture.snapshot.instance_id, "Complete batch makes exactly one request-ID and immutable-instance-bound action");
        check(submitted.length === 16 && submitted.every((answer, i) => answer.id === request.questions[i].id && answer.answer === (request.questions[i].options?.[0].label || `Exact custom answer ${i}\nSecond line`)), "All sixteen answers preserve exact recommended labels, IDs, and multiline custom text on the real delegated POST");
        await settle(() => !visible(region));
        check(visible($("#live-composer")) && normal.value === "retained ordinary composer draft", "Successful answer removal restores the retained regular composer draft");
      } else if (fixture.name === "usage-known") {
        $("[data-telemetry-menu]").click(); await settleFrame();
        check(!$("[data-workflow-usage]").textContent.includes("Unknown") && /1[,.]?500|1.5k/i.test($("[data-workflow-usage]").textContent), "Known usage displays actual 1500 token total, not unknown or fabricated zero");
        check(!$("[data-workflow-context]").textContent.includes("Unknown"), "Known context estimate is rendered separately from usage");
        window.SnowMenus.close();
      } else if (fixture.name === "stream") {
        const stream = $("#live-stream"), floor = () => stream.scrollHeight - stream.clientHeight;
        await wait(100);
        check(!!window.SnowScroll && near(stream.scrollTop, floor(), 3), `Initial contentful transcript starts pinned at the real floor (top=${stream.scrollTop}, floor=${floor()}, following=${$("#live-session").dataset.scrollFollowing})`);
        const saved = fixture.snapshot.messages.filter(message => message.source_id === "fixture-saved-interleaved");
        const savedRows = saved.map(message => document.querySelector(`[data-message-id="${message.id}"]`));
        check(saved.length === 3 && new Set(saved.map(message => message.id)).size === 3 && saved.map(message => message.role).join(",") === "assistant,plan,assistant" && savedRows.every((node, index) => node && node.querySelector(".markdown-body").textContent.includes(saved[index].text.replace("## ", ""))), "Actual Go saved-history text/plan/text projection produces three distinct correctly ordered DOM identities and public bodies");
        const tail = fixture.snapshot.messages.at(-1), row = document.querySelector(`[data-message-id="${tail.id}"]`), copy = row.querySelector(".copy-code");
        const table = row.querySelector(".message-table-scroll");
        table.scrollLeft = 180;
        const tableLeft = table.scrollLeft;
        check(tableLeft > 0 && table.scrollWidth > table.clientWidth && row.querySelector("table td").textContent === "Initial", "Go-rendered streamed table has real horizontally scrollable content before growth");
        copy.focus({preventScroll: true});
        await update({messages: fixture.updates[0].messages});
        check(savedRows.every((node, index) => document.querySelector(`[data-message-id="${saved[index].id}"]`) === node) && [...$("#live-transcript").querySelectorAll("[data-message-id]")].filter(node => saved.some(message => message.id === node.dataset.messageId)).every((node, index) => node === savedRows[index]), "Streaming update retains exact saved text/plan/text DOM nodes and order without merging or duplicating source-message identities");
        check(row.querySelector(".message-table-scroll") === table && near(table.scrollLeft, tableLeft, 1) && table.textContent.includes("Server-rendered table continuation"), "Growing sanitized server HTML preserves exact table scroll wrapper and horizontal reader position while updating its cells");
        check(document.querySelector(`[data-message-id="${tail.id}"]`) === row && document.activeElement === copy, "Streaming Markdown growth preserves stable message and focused Copy code node");
        copy.click(); await settleFrame(); await wait(20);
        check(fixture.clipboard.at(-1) === 'fmt.Println(59)\nfmt.Println("incremental")\n', "Focused preserved Copy code button copies newly streamed server-rendered code, not stale prefix");
        check(near(stream.scrollTop, floor(), 3), "Pinned streaming growth follows the new content floor");
        stream.scrollTop = Math.max(0, floor() - 700); stream.dispatchEvent(new Event("scroll")); await wait(40);
        const view = rect(stream), anchor = [...stream.querySelectorAll("[data-message-id]")].find(node => rect(node).bottom > view.top + 10 && rect(node).top < view.bottom - 10);
        check(!!anchor && visible($("#jump-latest")), "Manual reading exposes a visible content anchor and Jump control");
        const offset = rect(anchor).top, original = anchor.textContent;
        await update({messages: [...fixture.snapshot.messages, {id: "new-tail", role: "assistant", text: "New tail\n\n" + "Incremental public output.\n".repeat(20)}]});
        check(near(rect(anchor).top, offset, 3) && anchor.textContent === original, "Incremental append preserves the visible reader anchor, not just a numeric scroll offset");
        const jump = $("#jump-latest");
        check(near(rect(jump).width, 34) && near(rect(jump).height, 34) && hit(jump), "Jump is a reachable 34px control outside composer and content occlusion");
        jump.click(); await settleFrame();
        check(near(stream.scrollTop, floor(), 3), "Explicit Jump reaches the actual latest content synchronously, without smooth delay");
        await update({messages: [...fixture.snapshot.messages, {id: "after-jump", role: "assistant", text: "Fresh output after explicit jump"}]});
        check(near(stream.scrollTop, floor(), 3), "Explicit Jump re-enables following for subsequent output");
        stream.scrollTop = floor() - 500; stream.dispatchEvent(new Event("scroll")); await wait(40);
        const anchor2 = [...stream.querySelectorAll("[data-message-id]")].find(node => rect(node).bottom > rect(stream).top + 10 && rect(node).top < rect(stream).bottom - 10), before = rect(anchor2).top;
        copy.focus({preventScroll: true});
        check(document.activeElement === copy && row.querySelector(".copy-code") === copy, "Surviving code-copy target owns actual browser focus immediately before bounded head eviction");
        await update({messages: fixture.snapshot.messages.slice(10), history_truncated: true});
        check(document.activeElement === copy && copy.isConnected && document.querySelector(`[data-message-id="${tail.id}"]`) === row && row.querySelector(".copy-code") === copy && row.querySelector(".message-table-scroll") === table && near(table.scrollLeft, tableLeft, 1), "Head eviction preserves actual activeElement, exact surviving message/copy/table identities and horizontal table position");
        copy.click(); await settleFrame(); await wait(20);
        check(fixture.clipboard.at(-1) === 'fmt.Println(59)\nfmt.Println("incremental")\n', "Exact focused Copy code target still copies updated code after head eviction");
        check(anchor2.isConnected && near(rect(anchor2).top, before, 3), `Bounded head trimming preserves surviving stable-ID content anchor (before=${before}, after=${rect(anchor2).top}, following=${$("#live-session").dataset.scrollFollowing})`);
        const input = {id: "stream-question", questions: [{id: "stream-answer", header: "Answer while reading", question: "Keep my place?"}]};
        await update({status: "input", input});
        check(near(rect(anchor2).top, before, 3), "Question arrival and seat resize preserve manual reader anchor");
        $("[data-attention-collapse]").click(); await settleFrame(); await wait(60);
        check(near(rect(anchor2).top, before, 3), "Collapsing attention preserves manual reader ownership and visible anchor");
      }
      noOverflow("Final contentful fixture has no horizontal overflow");
      check(fixture.inventoryComplete(), "Exactly one startup public inventory read per mounted browser-inventory root; no interaction reload");
      check(fixture.errors.length === 0, `No script errors or unexpected requests: ${fixture.errors.join("; ")}`);
    } catch (error) { failures.push(error.stack || String(error)); }
    return {name: fixture.name, width: innerWidth, height: innerHeight, theme: fixture.theme, passed: results.length, results, failures, measurements};
  }
  try {
    await settle(() => document.readyState === "complete" && (!fixture.snapshot || $("#live-connection")?.textContent === "Live"));
    await document.fonts.ready;
    check(!!$("#workspace") && !!$("#workspace-content") && !!$("#project-navigation"), "Actual Go workspace and navigation IDs exist");
    const ids = [...document.querySelectorAll("[id]")].map(node => node.id);
    check(new Set(ids).size === ids.length, "Production page IDs are unique");
    check(!$(".preview-dashboard,.dashboard-grid,.stat-grid,.session-metrics"), "No fabricated dashboard or metric cards");
    check(!!document.querySelector('script[type="module"][src="/static/generated/app.js"]') && document.querySelector('[data-react-page="shell"]')?.dataset.reactMounted === 'true', "Current production React shell is loaded and mounted");
    check(fixture.theme === "light" ? document.documentElement.dataset.theme === "light" : getComputedStyle(document.body).backgroundColor === "rgb(21, 21, 23)", "Requested persisted theme is rendered");
    noOverflow("Initial page has no horizontal viewport overflow");
    const sidebar = $("#project-navigation");
    measurements.sidebar = rect(sidebar).toJSON();
    if (desktop) {
      check(visible(sidebar) && !sidebar.inert, "Desktop sidebar is visible and interactive");
      check(near(rect(sidebar).x, 0) && near(rect(sidebar).y, 0), "Desktop sidebar begins at viewport origin");
      check(near(rect(sidebar).width, 280) && near(rect(sidebar).height, innerHeight), "Desktop sidebar is 280px and full height");
      check(fixture.theme === "light" || getComputedStyle(sidebar).backgroundColor === "rgb(27, 27, 28)", "Dark sidebar is #1b1b1c");
      check(!visible($(".topbar")), "No global desktop top bar");
      check(near(rect($("#workspace-content")).x, 280), "Desktop content starts directly after sidebar");
      const add = $(".sidebar-heading .add-project-link");
      check(visible(add) && rect(add).left >= 0 && rect(add).right <= rect(sidebar).right, "Add workspace remains visible at every desktop width including 768px");
      const collapse = $(".sidebar-collapse"); collapse.click(); await settleFrame();
      check(near(rect(sidebar).width, 56) && collapse.getAttribute("aria-expanded") === "false", "Desktop sidebar collapse control produces compact rail");
      const brand = $(".sidebar-brand-row .brand");
      check(visible(brand.querySelector(".brand-mark")) && !visible(brand.querySelector(".brand-name")) && !visible(brand.querySelector(".brand-label")), "Collapsed rail retains Snowflake home link without wordmark or badge");
      check(visible(collapse) && rect(brand).bottom <= rect(collapse).top && near(rect(brand).x + rect(brand).width / 2, rect(sidebar).width / 2), "Collapsed Snowflake is centered above a separate nonoverlapping expand control");
      measurements.collapsedBrand = rect($(".sidebar-brand-row")).toJSON();
      const railHeight = sidebar.style.height, settings = $(".sidebar-settings-trigger");
      sidebar.style.height = "240px"; settings.focus();
      const settingsBox = rect(settings);
      check(sidebar.scrollTop > 0 && settingsBox.bottom <= rect(sidebar).bottom && settings.contains(document.elementFromPoint(settingsBox.x + settingsBox.width / 2, settingsBox.y + settingsBox.height / 2)), "Short collapsed rail scrolls Settings into view without shrinking or hiding controls");
      sidebar.style.height = railHeight; sidebar.scrollTop = 0; collapse.focus();
      check(!visible($(".sidebar-new-session > span")) && $(".sidebar-new-session").getAttribute("aria-label") === "New session", "Collapsed New session retains an accessible name when its text is hidden");
      check(!visible($("[data-settings-open] > span")) && $("[data-settings-open]").getAttribute("aria-label") === "Settings", "Collapsed Settings retains an accessible name when its text is hidden");
      $("[data-sidebar-search-toggle]").click(); await settleFrame();
      check(near(rect(sidebar).width, 280) && visible($("[data-sidebar-search]")) && document.activeElement === $("[data-sidebar-search]"), "Collapsed rail Search expands sidebar and focuses a visible input");
      $("[data-sidebar-search-toggle]").click(); await settleFrame();
      collapse.click(); await settleFrame();
      check(near(rect(sidebar).width, 56) && !visible($("[data-sidebar-search]")), "Closing rail Search permits recollapse without leaving a hidden focused input");
      collapse.click(); await settleFrame();
      check(near(rect(sidebar).width, 280) && collapse.getAttribute("aria-expanded") === "true", "Desktop sidebar restores full reference width");
    } else {
      check(visible($("[data-nav-toggle]")) && sidebar.inert, "Mobile drawer starts closed and inert");
      const toggle = $("[data-nav-toggle]"); toggle.focus(); toggle.click(); await settleFrame();
      check(sidebar.getAttribute("aria-modal") === "true" && sidebar.contains(document.activeElement), "Mobile navigation opens with modal focus");
      document.dispatchEvent(new KeyboardEvent("keydown", {key: "Escape", bubbles: true})); await settleFrame();
      check(sidebar.inert && document.activeElement === toggle, "Escape closes navigation and restores focus");
    }

    if (desktop) {
      const searchToggle = $("[data-sidebar-search-toggle]"); searchToggle.click(); await settleFrame();
      const search = $("[data-sidebar-search]");
      check(visible(search) && document.activeElement === search, "Workspace search control reveals and focuses real search input");
      enter(search, "no-workspace-matches-this"); await settleFrame();
      check([...document.querySelectorAll("[data-sidebar-project]")].every(project => project.hidden), "Workspace search filters registered project rows");
      searchToggle.click(); await settleFrame();
      check(!visible(search) && [...document.querySelectorAll("[data-sidebar-project]")].every(project => !project.hidden), "Closing workspace search restores project rows");
    }

    const composer = $(home ? ".home-composer" : "form.composer");
    const attention = !!fixture.snapshot?.permission;
    if (!attention && (home || fixture.snapshot)) check(visible(composer), "Compact composer is visible");
    if (composer && !attention) {
      measurements.composer = rect(composer).toJSON();
      check(near(parseFloat(getComputedStyle(composer).borderTopLeftRadius), 22), "Composer has 22px corners");
      const column = rect($(home ? ".home-landing" : ".conversation-pane")).width;
      const expectedWidth = Math.min(Math.max(680, .64 * column), 920) + 32;
      if (home) {
        check(rect(composer).width <= 822, "Hero composer remains bounded to its 820px design width");
        check(rect(composer).height >= 108 && rect(composer).height <= 126, "Hero composer remains approximately 114px tall");
      } else {
        check(near(rect(composer).width, Math.min(expectedWidth, column - 32), 3), "Docked composer follows adaptive transcript width plus 32px and narrow 16px clearances");
        check(rect(composer).height >= 90 && rect(composer).height <= 106 && composer.querySelectorAll(".composer-actions").length === 1 && composer.querySelector(".composer-actions").contains(composer.querySelector(".composer-context-tools")), "Docked composer is approximately 98px with one action row containing context controls");
        check(near(rect($("#live-prompt")).height, 36), "Docked editor has a 36px minimum input height");
        const transcript = rect($("#live-transcript"));
        check(near(transcript.width, Math.min(Math.max(680, .64 * column), 920, column - 64), 3), "Transcript follows adaptive .64 column clamp680..920 with narrow 32px clearances");
      }
      check(rect(composer).left >= (desktop ? 280 : 0) && rect(composer).right <= innerWidth + 1, "Composer fits available canvas");
      if (home && innerWidth === 1512) check(near(rect(composer).width, 820), "1512px reference uses an 820px composer");
    }
    if (home) {
      const title = $("#home-title"), controls = $(".home-controls"), landing = $(".home-landing");
      check(title?.textContent === "Into the Unknown" && !!$(".home-title-mark svg"), "Landing has central Snow mark and reference title");
      check(visible(controls) && rect(controls).bottom <= rect(composer).top + 1, "Workspace and mode row sits above composer");
      check(rect(title).bottom < rect(controls).top, "Title precedes workspace controls");
      measurements.landing = rect(landing).toJSON();
      if (desktop) check(near(rect(composer).left + rect(composer).width / 2, 280 + (innerWidth - 280) / 2 - 5), "Landing composer follows the reference-centered canvas with 5px optical offset");
      check(!$("#home-prompt")?.disabled && !$(".home-send")?.disabled && $(".home-send").textContent === "Continue", "Landing accepts a draft and offers Continue, not an automatic send");
      const picker = $(".workspace-picker"); $(".workspace-picker > summary").click(); await settleFrame();
      check(visible($(".shell-workspace-menu")) && $(".shell-workspace-menu").parentElement === document.body, "Workspace picker opens the production portaled menu");
      check(fixture.name === "home" ? !!$(".shell-workspace-menu")?.querySelector('a[href*="00000000-0000-4000-8000-000000000001"]') : $(".shell-workspace-menu")?.textContent.includes("No workspaces registered"), "Picker reflects actual registered or empty fixture state");
      $(".workspace-picker > summary").focus();
      document.dispatchEvent(new KeyboardEvent("keydown", {key: "Escape", bubbles: true})); await settleFrame();
      check(!$(".shell-workspace-menu") && !picker.open, "Escape closes workspace picker");
      check(fixture.nonInventoryRequests().length === 0, "Landing interactions make no requests beyond the one startup browser-inventory read");
    } else if (fixture.snapshot) {
      const heading = $("#live-session .workspace-heading");
      check(document.querySelectorAll(".workspace-heading").length === 1 && !!heading, "Activated conversation has one workspace header, owned by the live session");
      measurements.liveHeader = rect(heading).toJSON();
      check(near(rect(heading).height, 76), "Single live workspace header retains the 76px reference height");
      check(!!heading.querySelector("[data-inspector-toggle]") && !!heading.querySelector("[data-runtime-close]"), "Single header owns Files / Changes and Close runtime controls");
      check(!!$("#live-composer-seat [data-runtime-abort]") && !heading.querySelector("[data-runtime-abort]") && [...document.querySelectorAll("[data-runtime-abort]")].filter(visible).length <= 1, "Stop has one visible seat owner and no duplicate header control");
      check(!visible($(".connection-line")) && $("#live-connection").dataset.connected === "true", "Healthy live connection does not create an extra header/status row");
      check(["live-prompt", "live-send", "composer-state"].every(id => document.getElementById(id)), "Live composer public IDs are preserved");
      check(!$("#workflow-provider,#workflow-model,[data-workflow-model-apply]"), "Legacy mixed native model form is removed rather than hidden");
      check($("#live-transcript").children.length === fixture.snapshot.messages.length, "Live poll retains production snapshot transcript");
      check(!!$("#live-transcript .markdown-body ul,#live-transcript .markdown-body ol"), "Live snapshot preserves production-rendered Markdown structure");
      check(!visible($("#live-unknown")), "Matching instance poll does not enter uncertain-outcome state");
      check(!$(".snow-menu"), "Task menus start closed");
      const prompt = $("#live-prompt"); enter(prompt, "Keep this unsent layout-review draft");
      prompt.dispatchEvent(new Event("input", {bubbles: true})); prompt.focus();
      if (fixture.name === "attention") {
        check(visible($("#live-attention")) && $("#live-attention").textContent.toUpperCase().includes("APPROVAL REQUIRED"), "Pending permission stays visible");
        check($("#live-send").disabled, "Attention state disables sending");
        check(!visible($("#live-send")) && visible($("#live-attention [data-runtime-abort]")) && !$("#live-attention [data-runtime-abort]").disabled, "Active permission state replaces normal composer with one enabled attention Stop");
        check(visible($("#live-activities")), "Public tool attention remains visible beside approval");
      } else {
        check(!$("#live-send").disabled, "Idle composer accepts an explicit prompt");
        check(visible($("#live-send")) && !visible($("[data-runtime-abort]")) && $("[data-runtime-abort]").disabled, "Idle composer shows Send and hides/disables Stop");
      }
      if (fixture.name === "markdown") {
        check(!!$("#live-transcript pre code") && !!$("#live-transcript table"), "Real Go Markdown fixture includes long code and a wide table");
        check(!!$("#live-transcript [data-copy-code]"), "Long Markdown code has an actual copy control");
        $("#live-transcript [data-copy-code]").click(); await settleFrame();
        await settle(() => fixture.clipboard.length > 0);
        check(fixture.clipboard.at(-1) === $("#live-transcript pre code").textContent, "Code copy uses full public code text, not the visually clipped line");
        $("#live-transcript .message-copy").click(); await settleFrame();
        await settle(() => fixture.clipboard.length > 1);
        check(fixture.clipboard.at(-1) === fixture.snapshot.messages[0].text, "Message copy preserves public message text");
        check(rect($("#live-transcript pre")).right <= rect($(".conversation-pane")).right, "Long code scrolls inside the conversation rather than escaping its column");
      }
      check(prompt.value === "Keep this unsent layout-review draft", "Conversation rendering retains unsent draft");
      if (fixture.name === "plan") check($("#live-transcript").textContent.includes("No implementation has started."), "Plan fixture renders actual plan message");
      if (fixture.name === "inspector") {
        const trigger = $(".workspace-heading [data-inspector-toggle]"), panel = $("#project-inspector");
        trigger.focus(); trigger.click(); await settleFrame();
        await settle(() => $("[data-files-list]").children.length === 3);
        check(visible(panel) && panel.contains(document.activeElement), "Inspector opens real file list and moves focus inside");
        if (!desktop) {
          check(panel.getAttribute("aria-modal") === "true" && $(".conversation-pane").inert, "Mobile inspector keeps modal/inert boundary");
          const close = panel.querySelector("[data-inspector-toggle]"); close.focus();
          document.dispatchEvent(new KeyboardEvent("keydown", {key: "Tab", shiftKey: true, bubbles: true, cancelable: true}));
          check(panel.contains(document.activeElement) && document.activeElement !== close, "Inspector backward Tab wraps focus inside modal");
          document.dispatchEvent(new KeyboardEvent("keydown", {key: "Escape", bubbles: true})); await settleFrame();
        } else panel.querySelector("[data-inspector-toggle]").click(); await settleFrame();
        check(panel.hidden && document.activeElement === trigger && !$(".conversation-pane").inert, "Inspector close restores trigger focus and conversation interactivity");
        check(prompt.value === "Keep this unsent layout-review draft", "Inspector preserves conversation draft");
        // Leave the real inspector open for its optional screenshot.
        trigger.click(); await settleFrame();
        measurements.inspector = rect(panel).toJSON();
      }
      check(fixture.requests.every(request => request.method === "GET" || request.path.endsWith("/runtime/choices") || request.path.includes("/inspect/")), "Visual exercise never submits or activates agent work");
      enter(prompt, "");
    }
    if (inactive) {
      const group = document.querySelector("[data-sidebar-project]"), tree = group.querySelector("[data-workspace-sessions]");
      const panel = $(".workspace-session-start"), pane = $(".inactive-conversation"), draft = $("#workspace-prompt");
      await settle(() => !tree.hasAttribute("aria-busy") && tree.querySelectorAll("[data-shell-session]").length === 25);
      check(group.querySelector("[data-workspace-toggle]").getAttribute("aria-expanded") === "true" && !tree.hidden, "Cold selected workspace has an independently expanded grouped session branch");
      check(!$(".project-session-list .catalog-row") && !$("#live-session[data-runtime]") && !$("#live-composer"), "Cold session surface has no central catalog, live runtime or pretend live composer");
      check(!!draft && !draft.disabled && draft.maxLength === 65536 && panel.contains(draft), "Cold session has a bounded editable tab-local draft in its explicit start seat");
      enter(draft, "Keep this cold workspace draft");
      if (!desktop) $("[data-nav-toggle]").click(); await settleFrame();
      const more = tree.querySelector("[data-sidebar-session-more]");
      more.focus(); more.scrollIntoView({block: "center"}); await wait(30);
      check(visible(more) && document.elementFromPoint(rect(more).x + rect(more).width / 2, rect(more).y + rect(more).height / 2) === more, "Bounded cold session inventory Load more is keyboard and hit-test reachable");
      more.click(); await settleFrame();
      await settle(() => tree.querySelectorAll("[data-shell-session]").length === 35 && !tree.hasAttribute("aria-busy"));
      const rows = [...tree.querySelectorAll("[data-shell-session]")], last = rows.at(-1), lastLink = last.querySelector("a");
      check(last.textContent.includes("Saved conversation 35") && !visible(tree.querySelector("[data-sidebar-session-more]")), "Two read-only pages expose all 35 saved sessions in the workspace branch, not the conversation area");
      check(rows.every(row => row.querySelector("a").dataset.instance === "" && row.querySelector("a").hasAttribute("data-shell-session-open")), "Cold saved links remain read-only without fabricated live instance authority");
      lastLink.focus(); lastLink.scrollIntoView({block: "center"}); await wait(30);
      const rowBox = rect(lastLink), rowHit = document.elementFromPoint(rowBox.x + rowBox.width / 2, rowBox.y + rowBox.height / 2);
      check(rowBox.top >= 0 && rowBox.bottom <= innerHeight + 1 && lastLink.contains(rowHit), "Last grouped saved session stays focusable and uncovered in the real sidebar scroll owner");
      measurements.inactiveSidebar = {lastRow: rect(last).toJSON(), list: rect(tree).toJSON(), navigation: rect($("#project-navigation")).toJSON()};
      if (!desktop) $("#project-navigation [data-nav-close]").click(); await settleFrame();
      const activate = panel.querySelector('button[type="submit"]');
      activate.focus(); activate.scrollIntoView({block: "center"}); await wait(30);
      const buttonBox = rect(activate), buttonHit = document.elementFromPoint(buttonBox.x + buttonBox.width / 2, buttonBox.y + buttonBox.height / 2);
      measurements.inactiveStart = {activation: rect(panel).toJSON(), pane: rect(pane).toJSON(), button: buttonBox.toJSON()};
      check(buttonBox.top >= 0 && buttonBox.bottom <= innerHeight + 1 && activate.contains(buttonHit), "Explicit cold Start remains focusable and hit-test reachable in narrow and short viewports");
      check(draft.value === "Keep this cold workspace draft", "Paging the workspace branch preserves the unsent cold draft");
      if (fixture.name === "inactive-trusted") {
        check(!panel.querySelector('input[type="checkbox"][name="confirm"]') && panel.querySelector('input[name="confirm"]').value === "trusted" && panel.querySelector('input[name="enable_skills"]')?.checked === false, "Remembered project has no repeated trust checkbox; explicit activation remains and skill opt-in is never remembered");
        check(!panel.querySelector('.activation-boundary') && !!panel.querySelector('[data-settings-open="workspaces"]'), "Remembered activation is compact with discoverable trust management");
        panel.querySelector(".activation-model-help").open = true;
        const manageTrust = panel.querySelector("[data-settings-open=workspaces]"); manageTrust.focus(); manageTrust.click(); await settleFrame(); await wait(60);
        const settings = $("#settings-dialog"), forget = settings.querySelector('[data-project-trust-revoke] button');
        check(settings.open && !$("#settings-workspaces").hidden && !!forget, "Manage trust opens real Workspaces Settings with explicit revocation");
        forget.scrollIntoView({block: "center"});
        check(rect(forget).left >= 0 && rect(forget).right <= innerWidth + 1 && rect(forget).top >= 0 && rect(forget).bottom <= innerHeight + 1, "Forget trust remains reachable at narrow and short viewports");
        $("[data-settings-close]").click(); await settleFrame();
      } else {
        check(panel.querySelector('input[name="confirm"]').required && !panel.querySelector('input[name="confirm"]').checked, "Inactive activation still requires explicit unchecked confirmation");
      }
      check(fixture.nonInventoryRequests().length === 0, "Paging cold saved sessions and editing a draft starts no worker, provider discovery, inspection or mutation beyond bounded public inventories");
    }
    if (fixture.name === "saved-markdown" || fixture.name === "saved-user") {
      const pane = $(".inactive-conversation > .conversation-pane"), stream = pane.querySelector('[data-react-page="workspace-cold"] > .conversation-stream'), panel = $(".workspace-session-start");
      const resume = panel.querySelector('button[type="submit"]'), draft = $("#workspace-prompt");
      check(!$("#live-session[data-runtime]") && !!panel.querySelector('input[name="session_id"]') && resume.textContent.includes("Resume"), "Saved history uses the normal conversation and an explicit session-bound Resume seat without a worker");
      check(!draft.disabled && draft.classList.contains("activation-draft"), "Saved conversation has an editable cold draft with shared composer-seat styling");
      enter(draft, "Keep saved-session draft");
      if (innerHeight > 480) {
        const before = rect(panel).toJSON();
        check(rect(stream).height >= 64 && rect(panel).bottom <= innerHeight + 1 && rect(resume).top >= 0 && rect(resume).bottom <= innerHeight + 1, "At normal heights the Resume action is visible beside an independently scrollable saved transcript");
        if (fixture.name === "saved-markdown") check(stream.scrollHeight > stream.clientHeight, "Long saved Markdown stays inside the bounded transcript scroller rather than pushing Resume below history");
        stream.scrollTop = stream.scrollHeight; await wait(30);
        check(near(rect(panel).top, before.top) && near(rect(panel).bottom, before.bottom), "Reading the end of saved history does not move the Resume composer seat");
      }
      resume.focus(); resume.scrollIntoView({block: "center"}); await wait(30);
      const button = rect(resume), hit = document.elementFromPoint(button.x + button.width / 2, button.y + button.height / 2);
      check(button.top >= 0 && button.bottom <= innerHeight + 1 && resume.contains(hit), "Resume is keyboard and hit-test reachable even in short saved-history views");
      const skills = panel.querySelector('input[name="enable_skills"]'); skills.focus(); skills.scrollIntoView({block: "center"}); await wait(30);
      check(document.activeElement === skills && rect(skills).top >= 0 && rect(skills).bottom <= innerHeight + 1 && !skills.checked, "Saved-start skills choice remains reachable and unchecked without granting hidden authority");
      check(draft.value === "Keep saved-session draft" && fixture.nonInventoryRequests().length === 0, "Reading saved history and navigating Resume controls retains the draft without mutation, provider discovery or inspection");
    }
    if (fixture.name === "saved-markdown") {
      const message = $(".catalog-history .conversation-message");
      check(!$("#live-session[data-runtime]") && !!message, "Saved Markdown comes from the real inactive Go history template, not a live DTO");
      check(!!message.querySelector(".code-banner .copy-code") && !!message.querySelector(".markdown-body h2"), "Saved history renders Markdown and real Copy code presentation chrome");
      check(message.querySelector(".message-source")?.hidden && message.querySelector(".message-source").textContent === fixture.source, "Saved history retains escaped exact public source independently of rendered chrome");
      message.querySelector("[data-message-copy]").click(); await settleFrame(); await wait(30);
      check(fixture.clipboard.at(-1) === fixture.source && fixture.clipboard.at(-1).includes('```go\nfmt.Println("<public> & exact")\n```'), "Saved Copy message preserves exact Markdown fences and escaped source, without Copy code/banner contamination");
      message.querySelector(".copy-code").click(); await settleFrame(); await wait(30);
      check(fixture.clipboard.at(-1) === 'fmt.Println("<public> & exact")\n', "Saved Copy code remains code-only, without message or banner chrome");
      check(fixture.nonInventoryRequests().length === 0, "Browsing and copying saved history starts no runtime or request beyond startup browser inventory");
    }
    noOverflow("Final interactive page has no horizontal viewport overflow");
    check(fixture.inventoryComplete(), "Exactly one startup public inventory read per mounted browser-inventory root; no interaction reload");
    check(fixture.errors.length === 0, `No script errors or unmocked requests (${fixture.errors.join("; ")})`);
  } catch (error) { failures.push(error.stack || String(error)); failures.push(`state=${document.readyState}; connection=${$("#live-connection")?.textContent || "none"}; requests=${fixture.requests.length}`); if (fixture.errors.length) failures.push(...fixture.errors); }
  return {name: fixture.name, width: innerWidth, height: innerHeight, theme: fixture.theme, passed: results.length, results, failures, measurements};
})()
