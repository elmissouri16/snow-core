// Production Go template/assets, synthetic public snapshots only. The runner
// sends real CDP pointer/keyboard events; never invoke a provider or tool.
(async () => {
  const results = [], failures = [];
  const check = (value, label) => (value ? results : failures).push(label);
  const $ = (selector, root = document) => root.querySelector(selector);
  const fixture = window.harnessFixture, wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  const live = !!fixture.snapshot;
  const update = async fields => {
    fixture.update(fields);
    for (let i = 0; i < 100; i++) {
      if (fixture.deliveredRevision === fixture.snapshot.revision) { await wait(100); return; }
      await wait(30);
    }
    throw new Error("Tool fixture update did not settle");
  };
  const literal = '<script>window.toolRowExecuted=true</script>\n<img src=x onerror="window.toolRowExecuted=true">\n**literal** & exact\n+ not an invented diff\n';
  const activity = (id, fields = {}) => ({id, tool: "glob", status: "completed", summary: "glob", output: literal, ...fields});
  const historyTool = (id, fields = {}) => ({id, tool: "read", status: "completed", output_available: true, output: literal, ...fields});
  const get = id => $(`[data-activity-id="${id}"], [data-history-tool-id="${id}"]`);
  if (live) {
    await update({messages: [{id: "tool-owner", role: "assistant", text: "Public answer", tools: [
      historyTool("saved-read"), historyTool("saved-empty", {output: ""}),
      historyTool("saved-unknown", {status: "unresolved", output: "PRIVATE_SENTINEL"}),
      historyTool("saved-missing", {output_available: false, output: "PRIVATE_SENTINEL"})
    ]}, {id: "unrelated-answer", role: "assistant", text: "Another answer"}], activities: [
      activity("dedup"), activity("title-dedup", {summary: " Find files "}),
      activity("search", {tool: "grep", summary: "public path: internal/web/**/*.js"}),
      activity("long", {tool: "custom_extremely_long_tool_name_".repeat(4), summary: "A long public summary " .repeat(80)}),
      activity("running", {tool: "bash", status: "running", output: "", summary: "bash"}),
      activity("rejected", {tool: "write", status: "denied", output: "", summary: "write"}),
      activity("unknown", {status: "unknown"}), activity("failure", {status: "failed"}),
      activity("error-flag", {is_error: true}), activity("empty", {output: ""}),
      activity("unknown-error", {status: "unknown", is_error: true}),
      activity("custom", {tool: "constructor", status: "constructor", summary: "constructor"}),
      activity("bounded", {tool: "read", output: "x".repeat(20000), arguments: "ARGUMENT_SENTINEL"})
    ]});
    check(get("dedup").querySelector(".activity-summary").hidden && get("dedup").querySelector(".activity-separator").hidden, "Tool-only duplicate summary omits text and dot entirely");
    check(get("title-dedup").querySelector(".activity-summary").hidden, "Action-title duplicate summary also omitted");
    check(!get("search").querySelector(".activity-summary").hidden && !get("search").querySelector(".activity-separator").hidden, "Distinct bounded public summary retains its separator");
    check(get("dedup").querySelector(".activity-tool").textContent === "Find files" && get("dedup").querySelector("summary").getAttribute("aria-label").includes("glob"), "Action title retains original wire tool name in accessible summary");
    check(get("dedup").querySelector(".activity-kind").dataset.kind === "search" && get("running").querySelector(".activity-kind").dataset.kind === "terminal" && get("rejected").querySelector(".activity-kind").dataset.kind === "edit", "Public tool types receive search, terminal and edit glyphs");
    for (const [id, label] of [["running", "Running"], ["rejected", "Rejected"], ["unknown", "Outcome unknown"], ["failure", "Failed"], ["error-flag", "Failed"], ["saved-unknown", "Outcome unknown"], ["unknown-error", "Outcome unknown"], ["custom", "Status unavailable"]]) {
      const status = $(".activity-status", get(id));
      check(status.textContent === label && !status.classList.contains("activity-status-quiet") && status.getBoundingClientRect().width > 1, `${id}: non-success outcome remains visible`);
    }
    for (const id of ["empty", "running", "rejected", "saved-empty"]) {
      const row = get(id); $("summary", row).click();
      check(!row.open && row.classList.contains("activity-no-output") && $("summary", row).tabIndex === -1 && $(".activity-body", row).hidden, `${id}: absent public output has no false disclosure target`);
    }
    check($(".activity-output", get("saved-unknown")).textContent === "No result recorded; execution outcome unknown" && $(".activity-output", get("saved-missing")).textContent === "Public output was not recorded", "Saved unresolved/missing provenance is truthful, not fabricated output");
    check(!$("#live-stream").textContent.includes("PRIVATE_SENTINEL") && !$("#live-stream").textContent.includes("ARGUMENT_SENTINEL"), "Unavailable result text and raw arguments stay absent");
    check(get("saved-read").closest("article").dataset.messageId === "tool-owner" && !get("dedup").closest("article") && !$('.conversation-message[data-message-id="unrelated-answer"] .history-tool'), "Only authoritative message tools are associated; live runtime activity stays unassociated");
    check($(".activity-output", get("bounded")).textContent.length === 16384 && !$(".activity-truncated", get("bounded")).hidden, "Live output retains bounded literal display and explicit truncation notice");
    check($(".activity-output", get("dedup")).textContent === literal && !get("dedup").querySelector("script,img,strong,.code-block,.inspection-diff-line") && !window.toolRowExecuted, "Public output remains literal text without HTML, Markdown or invented result cards");
  } else {
    check(get("fixture-public-tool") && get("fixture-unresolved-tool"), "Saved fixture is actual Go-exported history markup");
    check($(".activity-status", get("fixture-unresolved-tool")).textContent === "Outcome unknown" && $(".activity-output", get("fixture-unresolved-tool")).textContent === "No result recorded; execution outcome unknown", "Saved unresolved history preserves unknown outcome and explanatory disclosure");
    check($(".activity-output", get("fixture-legacy-tool")).textContent === "Public output was not recorded", "Saved legacy failure preserves missing-public-output provenance");
    check(!get("fixture-public-tool").querySelector("script"), "Go-exported saved literal markup remains inert after enhancement");
  }
  await document.fonts.ready;
  const rows = [...document.querySelectorAll(".tool-activity")];
  check(rows.length > 0 && rows.every(row => Math.abs($("summary", row).getBoundingClientRect().height - 24) < 1), "Every live/saved collapsed row is one 24px line at this viewport/theme");
  check(rows.every(row => $("summary", row).scrollWidth <= $("summary", row).clientWidth + 1), "Long tool names and summaries never overflow the narrow row");
  check(rows.filter(row => row.dataset.status === "completed" && !row.classList.contains("activity-error")).every(row => $(".activity-status", row).classList.contains("activity-status-quiet") && $(".activity-status", row).getBoundingClientRect().width === 1 && $("summary", row).getAttribute("aria-label").includes("Completed")), "Completed is screen-reader available without a visible success badge");
  check(document.documentElement.scrollWidth <= innerWidth + 1, "Tool rows do not introduce page horizontal overflow");
  const target = get(live ? "dedup" : "fixture-public-tool"), summary = $("summary", target);
  window.toolRowChecks = {
    results, failures, check, target, summary,
    point() { summary.scrollIntoView({block: "center"}); const r = summary.getBoundingClientRect(); return {x: r.x + Math.min(60, r.width / 2), y: r.y + r.height / 2}; },
    async retain() {
      const output = $(".activity-output", target);
      if (live) {
        const activities = structuredClone(fixture.snapshot.activities); activities.find(row => row.id === "dedup").output += "Updated literal public output";
        await update({activities, revision: fixture.snapshot.revision + 1});
      } else { window.SnowMessages.enhance(document); window.SnowMessages.enhance(document); }
      check(target.isConnected && $("summary", target) === summary && $(".activity-output", target) === output && target.open && document.activeElement === summary, "Polling/enhancement retains keyed disclosure, output node, open state and keyboard focus");
      check(target.dataset.toolName === (live ? "glob" : "read"), "Repeated enhancement preserves the wire name rather than remapping the action title");
    },
    async finish() {
      if (live) {
        // Exercise the existing shared UTF-8/count budgets without executing tools.
        const messages = [{id: "budget-owner", role: "assistant", text: "", tools: Array.from({length: 70}, (_, i) => historyTool(`budget-${i}`, {output: "😀".repeat(9000)}))}];
        await update({messages});
        const output = [...document.querySelectorAll(".history-tool .activity-output")].map(node => node.textContent);
        check(output.length === 64 && output.every(value => new TextEncoder().encode(value).length <= 8192) && output.reduce((total, value) => total + new TextEncoder().encode(value).length, 0) === 131072 && output.every(value => !value.includes("�")), "Saved history retains count, per-output UTF-8 and aggregate output budgets");
        check(!$(".history-tool-limit").hidden && [...document.querySelectorAll(".history-tool")].every(row => !$(".activity-truncated", row).hidden), "Saved browser budget truncation remains explicitly disclosed, even after output budget exhaustion");
      }
      check(!fixture.requests.some(request => request.method === "POST" && !request.path.endsWith("/runtime/choices")), "Checks never submit runtime, manager, provider or user-configuration mutations");
      check(fixture.errors.length === 0, "Production scripts report no fixture/runtime errors");
      return {results, failures};
    }
  };
  return {ready: true};
})();
