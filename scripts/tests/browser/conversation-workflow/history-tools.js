// Focused DOM checks of the production saved-message renderer. No tool executes.
window.testSavedHistoryTools = async assert => {
  const host = document.createElement("section");
  host.className = "catalog-history"; document.body.append(host);
  const render = messages => window.SnowMessages.render(host, messages);
  const tool = (id, fields = {}) => ({id, owner_id: "answer", result_id: "result-" + id, tool: "read", status: "completed", output_available: true, output: "public output", ...fields});
  const danger = '<script>window.historyToolExecuted=true</script><img src=x onerror="window.historyToolExecuted=true">\n**literal**, <private> & exact';
  const source = "## Exact **answer**\n\nText & <source>\n";
  const messages = [{id: "question", role: "user", text: "Question"}, {id: "answer", role: "assistant", text: source, tools: [
    tool("danger", {output: danger, arguments: "ARGUMENT_SENTINEL"}),
    tool("empty", {output: ""}), tool("private", {output_available: false, output: "PRIVATE_SENTINEL"}),
    tool("unresolved", {status: "unresolved", output: "UNKNOWN_SENTINEL"}), tool("failed", {status: "failed", truncated: true})
  ]}, {id: "tool-only", role: "assistant", text: "", tools: [tool("only")]}, {id: "later", role: "assistant", text: "Later answer"}];
  const get = id => [...host.querySelectorAll(".history-tool")].find(node => node.dataset.historyToolId === id);
  try {
    render(messages);
    const article = host.querySelector('[data-message-id="answer"]'), disclosure = get("danger"), summary = disclosure.querySelector("summary");
    assert(host.querySelectorAll(".history-tool").length === 6 && article.querySelectorAll(".history-tool").length === 5 && !host.lastElementChild.querySelector(".history-tool"), "saved tools stay inside their actual owning assistant, never latest answer or live activity");
    assert(!!(article.querySelector(".message-body").compareDocumentPosition(disclosure) & Node.DOCUMENT_POSITION_FOLLOWING) && !!(disclosure.compareDocumentPosition(article.querySelector(".message-actions")) & Node.DOCUMENT_POSITION_FOLLOWING), "saved disclosures follow answer text and precede copy actions");
    assert(disclosure.querySelector("pre").textContent === danger && !disclosure.querySelector("script,img,strong") && !window.historyToolExecuted, "dangerous tool text remains escaped literal pre text, never HTML or Markdown");
    assert(get("empty").querySelector("pre").textContent === "", "available empty public output stays empty");
    assert(get("private").querySelector("pre").textContent === "Public output was not recorded" && !host.textContent.includes("PRIVATE_SENTINEL"), "private legacy result ignores bogus output and explains missing public record");
    assert(get("unresolved").querySelector("pre").textContent === "No result recorded; execution outcome unknown" && !host.textContent.includes("UNKNOWN_SENTINEL") && !get("unresolved").textContent.match(/Running|Cancelled|Canceled/), "unresolved record reports unknown outcome, never fabricated running or canceled state");
    assert(get("failed").dataset.status === "failed" && !get("failed").querySelector(".activity-truncated").hidden, "failed saved result retains status and truncation notice");
    assert(get("only").closest("article").dataset.messageId === "tool-only" && get("only").closest("article").querySelector(".message-body pre").textContent === "", "empty-text assistant with tools is displayable");
    assert(!host.textContent.includes("ARGUMENT_SENTINEL") && !host.querySelector(".message-tools button,.message-tools a,.message-tools input"), "saved tool disclosures are inert and exclude raw arguments or retry controls");
    disclosure.open = true; summary.focus();
    render(messages);
    messages[1].tools[0].output = danger + "\nUpdated result"; render(messages);
    assert(get("danger") === disclosure && disclosure.open && document.activeElement === summary && host.querySelectorAll(".history-tool").length === 6 && disclosure.querySelector("pre").textContent.endsWith("Updated result"), "repeated and changed snapshots retain stable disclosure identity, open state, focus and unique tool nodes");
    messages[1].tools[0].output = danger;
    render(messages.slice(1));
    assert(get("danger") === disclosure && document.activeElement === summary, "evicting old transcript head preserves saved disclosure focus");
    window.SnowMessages.enhance(host);
    assert(get("danger") === disclosure && disclosure.open && document.activeElement === summary && disclosure.querySelector("pre").textContent === danger, "server enhancement path preserves saved disclosure identity and literal output");
    const original = Object.getOwnPropertyDescriptor(navigator, "clipboard"), copied = [];
    Object.defineProperty(navigator, "clipboard", {configurable: true, value: {writeText: async value => copied.push(value)}});
    try {
      article.querySelector("[data-message-copy]").click(); await Promise.resolve();
      assert(copied.length === 1 && copied[0] === source, "Copy message remains exact source without tool output, status or metadata");
    } finally { if (original) Object.defineProperty(navigator, "clipboard", original); else delete navigator.clipboard; }
    render(Array.from({length: 2}, (_, owner) => ({id: "bounds-" + owner, role: "assistant", text: "", tools: Array.from({length: 40}, (_, i) => tool(`bounded-${owner}-${i}`, {output: "😀".repeat(9000)}))})));
    const outputs = [...host.querySelectorAll(".activity-output")].map(node => node.textContent);
    assert(host.querySelectorAll(".history-tool").length === 64 && !!host.querySelector(".history-tool-limit:not([hidden])"), "browser caps saved history at 64 tools with explicit bounded notice");
    assert(outputs.every(value => new TextEncoder().encode(value).length <= 8192) && outputs.reduce((total, value) => total + new TextEncoder().encode(value).length, 0) === 131072 && !outputs.some(value => value.includes("�")), "browser enforces UTF-8 8KiB per output and 128KiB aggregate without split characters");
    render([{id: "a", role: "assistant", tools: [tool("shared"), tool("shared")]}, {id: "b", role: "assistant", tools: [tool("shared"), tool("second")]}]);
    assert(host.querySelectorAll(".history-tool").length === 2, "duplicate tool IDs are suppressed within and across owning articles");
    render([{id: "a", role: "user", text: "Not assistant", tools: [tool("ignored")]}]);
    assert(!host.querySelector(".history-tool"), "tools are only rendered on assistant articles and obsolete tools are removed");
  } finally { host.remove(); }
};
