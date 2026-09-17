// Browser-side assertions only. All rendering remains in the exported production
// Go templates/assets; snapshots cross the harness' mocked HTTP fetch boundary.
(async () => {
  const results = [], failures = [];
  const check = (value, label) => (value ? results : failures).push(label);
  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];
  const fixture = window.harnessFixture, live = !!fixture.snapshot;
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  const clone = value => structuredClone(value);
  const update = async fields => {
    fixture.update(fields);
    for (let i = 0; i < 150; i++) {
      if (fixture.deliveredRevision === fixture.snapshot.revision) {
        await wait(90); return;
      }
      await wait(30);
    }
    throw new Error(`Timeline snapshot revision ${fixture.snapshot.revision} did not settle`);
  };
  // These fixture messages are plain prose. Supply the same paragraph shape as
  // the Go Markdown renderer, rather than the shared transport mock's <pre>.
  const message = (id, role, text = "") => {
    const paragraph = document.createElement("p"); paragraph.textContent = text;
    return {id, role, text, ...(role === "assistant" && text ? {html: paragraph.outerHTML} : {})};
  };
  const marker = id => ({...message(id, "tool_activity"), html: '<img data-timeline-injected src=x onerror="window.timelineExecuted=true">TIMELINE_PRIVATE_SENTINEL'});
  const literal = '<script>window.timelineExecuted=true</script>\n<img src=x onerror="window.timelineExecuted=true">\n**literal public text** & exact\n';
  const secret = "TIMELINE_PRIVATE_SENTINEL";
  const activity = (id, message_id, fields = {}) => ({
    id, message_id, tool: "glob", status: "completed", summary: "glob", output: literal,
    tool_call_id: "provider-reused-raw-id", arguments: secret, root: secret,
    html: `<img data-timeline-injected src=x onerror="window.timelineExecuted=true">${secret}`,
    metadata: {private: secret}, tool_output: secret, ...fields
  });
  const owner = id => $$("#live-transcript > [data-message-id]").find(row => row.dataset.messageId === id);
  const row = id => $$("[data-activity-id]").find(item => item.dataset.activityId === id);
  const order = () => $$("#live-transcript > [data-message-id]").map(item => item.dataset.messageId);
  const equals = (actual, expected) => JSON.stringify(actual) === JSON.stringify(expected);
  const associated = (id, ownerID) => !!owner(ownerID)?.contains(row(id));
  const fallback = id => !!$("#live-activities")?.contains(row(id)) && !row(id)?.closest("[data-message-id]");
  const geometry = label => {
    check(document.documentElement.scrollWidth <= innerWidth + 1, `${label}: no document horizontal overflow at ${innerWidth}px`);
    const summaries = $$("[data-activity-id] > summary, [data-history-tool-id] > summary");
    check(summaries.length > 0 && summaries.every(node => {
      const box = node.getBoundingClientRect();
      return box.width > 0 && box.left >= -1 && box.right <= innerWidth + 1;
    }), `${label}: all ${summaries.length} tool summaries stay inside viewport`);
  };
  let initialMessages, initialActivities, savedState;
  if (live) {
    initialMessages = [message("user-1", "user", "Find matching files."), marker("step-1"), message("final-1", "assistant", "Found the matching files.")];
    initialActivities = [activity("prompt-1-call-a", "step-1"), activity("prompt-1-call-b", "step-1", {summary: "Second public glob"})];
    await update({status: "idle", messages: initialMessages, activities: initialActivities, activities_truncated: false});
    check(equals(order(), ["user-1", "step-1", "final-1"]), "First user turn orders the tool-only step before its final answer");
    check(associated("prompt-1-call-a", "step-1") && associated("prompt-1-call-b", "step-1"), "Two glob calls share one actual ordered step marker");
    check(owner("step-1")?.dataset.messageRole === "tool_activity" && !$(".message-body, .message-copy", owner("step-1") || document.createElement("div")), "Empty tool-only marker is a real tool group, not a blank assistant message with copy controls");
    const oldGroup = owner("step-1"), oldRow = row("prompt-1-call-a"), oldFinal = owner("final-1");
    initialMessages.push(message("user-2", "user", "Write the selected file."), marker("step-2"), message("final-2", "assistant", "Wrote the selected file."));
    initialActivities.push(activity("prompt-2-call-a", "step-2", {tool: "write", summary: "Write selected file"}));
    await update({messages: initialMessages, activities: initialActivities});
    check(equals(order(), ["user-1", "step-1", "final-1", "user-2", "step-2", "final-2"]), "Two explicit user turns retain user/glob/final/user/write/final chronology");
    check(owner("step-1") === oldGroup && row("prompt-1-call-a") === oldRow && owner("final-1") === oldFinal, "A new prompt retains the previous group, tool row and final-answer DOM identities");
    check(associated("prompt-1-call-a", "step-1") && associated("prompt-2-call-a", "step-2"), "Reused raw provider call IDs do not merge distinct public activity IDs across prompts");
    check($$("[data-activity-id]").length === 3 && $("#live-activities").hidden && !$("#live-activities [data-activity-id]"), "Fully associated activity has exactly three rows and no duplicate global footer");
    check($$("[data-message-role='assistant'] [data-activity-id]").length === 0, "Runtime tools are not appended inside either unrelated assistant final answer");
    geometry("Two-turn chronology");
  } else {
    const tools = $$("[data-history-tool-id]");
    savedState = tools.map(tool => ({tool, owner: tool.closest("[data-message-id]"), id: tool.dataset.historyToolId, output: $(".activity-output", tool)?.textContent}));
    check(tools.length === 3 && savedState.every(item => item.owner?.dataset.messageId === "fixture-saved-answer"), "Actual Go saved-history fixture retains all three authoritative assistant/tool associations");
    check(!$("[data-activity-id], [data-message-role='tool_activity']"), "Saved history is not reinterpreted as runtime step markers or live activity");
    check($("[data-history-tool-id='fixture-unresolved-tool'] .activity-status").textContent === "Outcome unknown", "Unresolved saved history stays truthful rather than claiming completion");
    geometry("Saved history");
  }
  const target = live ? row("prompt-1-call-a") : $("[data-history-tool-id='fixture-public-tool']");
  if (!target) throw new Error("Production tool disclosure missing from the fixture");
  const summary = $("summary", target);
  window.toolTimelineChecks = {
    results, failures, check, target, summary,
    point() { summary.scrollIntoView({block: "center"}); const r = summary.getBoundingClientRect(); return {x: r.x + Math.min(60, r.width / 2), y: r.y + r.height / 2}; },
    async retain() {
      const group = target.closest("[data-message-id]"), output = $(".activity-output", target), open = target.open;
      if (live) {
        const activities = clone(fixture.snapshot.activities);
        activities.find(item => item.id === target.dataset.activityId).output += "Updated public output\n";
        await update({activities});
      } else { window.SnowMessages.enhance(document); window.SnowMessages.enhance(document); }
      check(target.isConnected && target.closest("[data-message-id]") === group && $("summary", target) === summary && $(".activity-output", target) === output && target.open === open && document.activeElement === summary, "Polling/repeated enhancement retains the exact group, disclosure, output node, open state and keyboard focus");
    },
    async finish() {
      if (live) {
        // Interleaved nonempty assistant segments and two tool-only markers must
        // stay in chronological order, including a late terminal update.
        const messages = [...initialMessages, message("user-3", "user", "Inspect, explain, then edit."),
          message("intro-3", "assistant", "First I will inspect."), marker("step-3"),
          message("followup-3", "assistant", "Now I will edit."), marker("step-4"),
          message("final-3", "assistant", "Inspection and edit complete.")];
        const activities = [...initialActivities, activity("prompt-3-read", "step-3", {tool: "read", status: "running"}), activity("prompt-3-edit", "step-4", {tool: "edit", status: "running"})];
        await update({messages, activities, status: "running"});
        check(equals(order(), messages.map(item => item.id)), "Assistant intro/tools/follow-up/tools/final are interleaved at their actual message positions");
        check(associated("prompt-3-read", "step-3") && associated("prompt-3-edit", "step-4"), "Interleaved tool calls use their explicit step owners, not the last assistant segment");
        check(associated("prompt-1-call-a", "step-1") && associated("prompt-2-call-a", "step-2"), "Later interleaved prompt does not move earlier prompt groups");
        const terminalRow = row("prompt-3-edit"), terminalGroup = owner("step-4"), terminalSummary = $("summary", terminalRow), terminalOutput = $(".activity-output", terminalRow);
        terminalRow.open = true; terminalSummary.focus();
        activities[activities.length - 1] = {...activities.at(-1), status: "completed", output: "Terminal public result"};
        await update({activities, status: "idle"});
        await update({telemetry: clone(fixture.snapshot.telemetry)});
        check(row("prompt-3-edit") === terminalRow && owner("step-4") === terminalGroup && $("summary", terminalRow) === terminalSummary && $(".activity-output", terminalRow) === terminalOutput && terminalRow.open && document.activeElement === terminalSummary, "Running-to-terminal update followed by another poll preserves DOM, disclosure open state and focused summary");
        check($(".activity-status", terminalRow).textContent === "Completed" && terminalOutput.textContent === "Terminal public result", "Retained terminal row updates its status and literal public output");
        // Root/private fields are deliberately not part of the public contract.
        check(!document.body.textContent.includes(secret) && !document.documentElement.outerHTML.includes(secret), "Raw arguments, root/private metadata and untrusted activity HTML remain absent from rendered text and DOM");
        check(!window.timelineExecuted && !$("[data-timeline-injected], .activity-output script, .activity-output img") && $(".activity-output", target).textContent === literal, "Public output containing script/img markup remains literal and never executes or creates HTML nodes");
        const history = {id: "saved-tool", owner_id: "saved-owner", result_id: "saved-result", tool: "read", status: "completed", output_available: true, output: "Authoritatively associated saved output"};
        messages.push({...message("saved-owner", "assistant", "Saved answer"), tools: [history]});
        const mixed = [...activities, activity("legacy-unowned", ""), activity("missing-owner", "step-missing"),
          activity("assistant-is-not-owner", "final-3"), activity("user-is-not-owner", "user-3"),
          activity("canceled-orphan", "step-missing", {status: "canceled", output: ""}),
          activity("failed-orphan", "step-missing", {status: "failed", is_error: true, output: "Public failure"}),
          activity("unknown-orphan", "step-missing", {status: "unknown", output: ""})];
        mixed.find(item => item.id === "prompt-3-read").status = "canceled";
        await update({messages, activities: mixed, status: "idle", error: "Synthetic runtime failure"});
        for (const id of ["legacy-unowned", "missing-owner", "assistant-is-not-owner", "user-is-not-owner", "canceled-orphan", "failed-orphan", "unknown-orphan"]) check(fallback(id), `${id}: unmatched or invalid owner uses the truthful runtime fallback outside assistant messages`);
        check(associated("prompt-3-read", "step-3") && $(".activity-status", row("prompt-3-read")).textContent === "Cancelled", "Cancellation preserves known step association without claiming successful completion");
        for (const [id, status] of [["canceled-orphan", "Cancelled"], ["failed-orphan", "Failed"], ["unknown-orphan", "Outcome unknown"]]) check($(".activity-status", row(id)).textContent === status, `${id}: fallback status truthfully remains ${status}`);
        check(!$("#live-activities").hidden && $("#live-activities").textContent.includes("runtime") && !$("#live-activities").textContent.includes("Assistant message"), "Visible fallback discloses runtime provenance rather than inventing assistant ownership");
        check($$("[data-activity-id]").length === mixed.length && new Set($$("[data-activity-id]").map(item => item.dataset.activityId)).size === mixed.length, "Mixed grouped/orphan activity has exactly one DOM row per public ID and no duplicate footer rows");
        const saved = $("[data-history-tool-id='saved-tool']"), savedOwner = owner("saved-owner");
        check(savedOwner.contains(saved) && !saved.contains(row("missing-owner")) && !$("[data-activity-id]", savedOwner), "Authoritative saved tools keep their assistant owner and never absorb orphan runtime rows");
        check(equals(fixture.snapshot.messages.at(-1).tools, [history]), "Rendering does not rewrite saved-history associations in the HTTP fixture source");
        await update({messages: messages.filter(item => item.id !== "step-4")});
        check(fallback("prompt-3-edit") && row("prompt-3-edit") === terminalRow && terminalRow.open, "A pruned owner moves its retained row to fallback, never to the adjacent assistant answer");
        // Browser defensive bounds, not a substitute for runtime projection tests.
        const bounded = Array.from({length: 140}, (_, i) => activity(`budget-${i}`, "step-budget", {tool: "read", summary: "s".repeat(2000), output: "😀".repeat(12000)}));
        await update({messages: [message("budget-user", "user", "Bounded output"), marker("step-budget")], activities: bounded, activities_truncated: true, error: ""});
        const budgetRows = $$("[data-activity-id]");
        check(budgetRows.length === 128, "Oversized synthetic activity array is defensively limited to 128 rows");
        check(equals(budgetRows.map(item => item.dataset.activityId), bounded.slice(-128).map(item => item.id)), "Activity clipping retains the newest bounded public IDs in source order");
        check(budgetRows.every(item => associated(item.dataset.activityId, "step-budget")), "Every bounded activity row remains within its explicit marker rather than a footer");
        check(budgetRows.every(item => $(".activity-output", item).textContent.length <= 16384 && !$(".activity-output", item).textContent.includes("�") && $(".activity-summary", item).textContent.length <= 1024), "Public output and summaries remain bounded without broken astral Unicode");
        check(budgetRows.every(item => !$(".activity-truncated", item).hidden), "Client-clipped public tool output is explicitly marked truncated");
        check($$("#live-stream p").some(node => !node.hidden && /truncat|omitted|bounded/i.test(node.textContent) && !node.closest("details")), "Snapshot activity truncation has a visible non-output notice");
        geometry("Bounded grouped output");
        const surviving = row("budget-139"), survivingSummary = $("summary", surviving);
        surviving.open = true; survivingSummary.focus();
        const longMessages = [marker("step-budget"), ...Array.from({length: 99}, (_, i) => message(`bounded-answer-${i}`, "assistant", "Public bounded answer")), marker("step-surviving"), marker("step-empty"), message("bounded-final", "assistant", "Final")];
        await update({messages: longMessages, activities: [bounded.at(-1), activity("surviving-step-tool", "step-surviving")], history_truncated: true, activities_truncated: false});
        check(order().length === 100 && equals(order(), longMessages.slice(-100).map(item => item.id)), "Message bound retains the last 100 real chronological entries including tool markers");
        check(fallback("budget-139") && row("budget-139") === surviving && surviving.open && document.activeElement === survivingSummary, "Message-limit eviction preserves the orphan tool disclosure and focus in truthful fallback");
        check(associated("surviving-step-tool", "step-surviving") && owner("step-empty")?.hidden, "Retained owner still groups its tool while an empty marker introduces no visible blank message");
        check(!$("#live-history-notice").hidden && fixture.snapshot.messages.length === longMessages.length, "History clipping is disclosed without mutating the source snapshot or saved history");
      } else {
        window.SnowMessages.enhance(document); window.SnowMessages.enhance(document);
        check(savedState.every(item => item.tool.isConnected && item.tool.closest("[data-message-id]") === item.owner && item.tool.dataset.historyToolId === item.id && $(".activity-output", item.tool).textContent === item.output), "Repeated saved-page enhancement preserves exact original ownership, IDs, rows and output");
        check(!fixture.nonInventoryRequests().length, "Saved history does not activate or request a runtime");
      }
      check(!fixture.requests.some(request => request.method !== "GET" && !request.path.endsWith("/runtime/choices")), "No checks submit prompts, permissions, tools, manager or provider mutations");
      check(fixture.errors.length === 0, "Production scripts report no fixture errors or unhandled rejections");
      return {results, failures};
    }
  };
  return {ready: true};
})();
