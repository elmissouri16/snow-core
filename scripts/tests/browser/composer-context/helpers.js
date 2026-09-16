/* Shared transport comes from conversation-workflow/fixture.js. These helpers
 * drive the production DOM only; they do not implement composer behavior. */
(() => {
  "use strict";
  const $ = selector => document.querySelector(selector);
  const tick = (ms = 30) => new Promise(resolve => setTimeout(resolve, ms));
  const wait = async (predicate, label = "browser state") => {
    for (let attempt = 0; attempt < 100; attempt++) { if (predicate()) return; await tick(); }
    throw new Error("Timed out waiting for " + label + " " + JSON.stringify({connection: $("#live-connection")?.textContent, prompt: $("#live-prompt")?.value, popup: $("[data-composer-mentions]")?.textContent, disabled: $("#live-send")?.disabled, errors: fixture.errors, posts: fixture.requests.filter(request => request.options.method === "POST").map(request => ({url: request.url, settled: request.settled, fields: Object.fromEntries(Object.entries(request.fields).filter(([key]) => !["content", "text"].includes(key)))}))}));
  };
  const posts = action => fixture.requests.filter(request => request.options.method === "POST" && (!action || request.url.endsWith("/" + action)));
  const latest = action => posts(action).at(-1);
  const visible = node => !!node && !node.hidden && node.getClientRects().length > 0 && getComputedStyle(node).visibility !== "hidden";
  const draft = (text, caret = text.length) => {
    const input = $("#live-prompt"); input.focus(); input.value = text; input.setSelectionRange(caret, caret);
    input.dispatchEvent(new Event("input", {bubbles: true})); return input;
  };
  const nativeInsert = async text => {
    fixture.nativeInsert = text;
    await wait(() => fixture.nativeInsert === null, "native browser text input");
  };
  const key = (name, options = {}) => {
    const event = new KeyboardEvent("keydown", {key: name, bubbles: true, cancelable: true, ...options});
    document.activeElement.dispatchEvent(event); return event;
  };
  const openContext = async kind => {
    if (!visible($("[data-composer-files]"))) {
      $("[data-composer-context-menu]").click();
      await wait(() => visible($("[data-composer-files]")) && visible($("[data-composer-skills]")), "plus context menu");
    }
    if (kind) $(`[data-composer-${kind}]`).click();
  };
  const closeContext = async () => { key("Escape"); await wait(() => !visible($("[data-composer-files]")), "closed plus menu"); };
  const chips = () => [...$("[data-composer-context-items]").querySelectorAll("button")];
  const clear = async () => { for (const button of chips()) button.click(); draft(""); await tick(); };
  const transfer = files => { const data = new DataTransfer(); for (const file of files) data.items.add(file); return data; };
  const upload = files => {
    const input = $("[data-composer-file-input]"); input.files = transfer(files).files;
    input.dispatchEvent(new Event("change", {bubbles: true}));
  };
  const paste = files => $("#live-prompt").dispatchEvent(new ClipboardEvent("paste", {clipboardData: transfer(files), bubbles: true, cancelable: true}));
  const drop = files => $("#live-prompt").dispatchEvent(new DragEvent("drop", {dataTransfer: transfer(files), bubbles: true, cancelable: true}));
  const textFile = (name = "notes.txt", text = "snow context α\nline two\n") => new File([text], name, {type: "text/plain"});
  // Valid bounded 48x32 RGB PNG (fixture.png); browser checks require decode().
  const png = "iVBORw0KGgoAAAANSUhEUgAAADAAAAAgCAIAAADbtmxLAAAAS0lEQVR4nO3OMQ0AIBBD0TOECKwhDBt4YcLAdSQd+pOOP+mru2e7sU67330BAgQoDuQ6Vj0gQIDyQK5j1QMCBCgP5DpWPSBAgOJAD6EufZf9oYJUAAAAAElFTkSuQmCC";
  const image = (name = "pixel.png") => new File([Uint8Array.from(atob(png), char => char.charCodeAt(0))], name, {type: "image/png"});
  const status = () => [$("[data-composer-context-status]").textContent, ...[...document.querySelectorAll(".composer-context-chip.is-error")].map(node => node.textContent)].join(" ").trim();
  const reading = () => [...document.querySelectorAll(".composer-context-detail")].some(node => node.textContent === "Reading…");
  const rows = () => [...$("[data-composer-mentions]").querySelectorAll('[role="option"]')].filter(visible);
  const choose = label => {
    const row = rows().find(row => label.endsWith("/")
      ? row.querySelector('.composer-mention-icon[data-kind="folder"]') && row.querySelector(".composer-mention-name")?.textContent === label.slice(0, -1)
      : row.textContent.includes(label));
    if (!row) throw new Error("Missing mention option: " + label);
    (row.matches("button") ? row : row.querySelector("button") || row).click();
  };
  const update = fields => Object.assign(fixture.snapshot, fields, {revision: fixture.snapshot.revision + 1});
  const ready = () => wait(() => $("#live-connection")?.textContent === "Live", "live connection");
  const swap = async fields => { update(fields || {}); await window.testNavigate(); await ready(); await tick(); };
  const send = () => $("#live-composer").requestSubmit();
  const accept = async action => { update({status: "idle"}); latest(action).resolve(fixture.snapshot); await ready(); await tick(); };
  const files = (path = ".", entries = [
    {name: "src", path: "src", kind: "directory"},
    {name: "notes.txt", path: "notes.txt", kind: "file"},
    {name: "other.txt", path: "other.txt", kind: "file"}
  ]) => ({path, entries, next_offset: 0, has_more: false, limited: false});
  const skills = extra => ({project_id: fixture.snapshot.project_id, session_id: fixture.snapshot.session_id, instance_id: fixture.snapshot.instance_id,
    enabled: true, skills: [{name: "review", description: "Review a change", enabled: true, disabled_by: ""}, {name: "build", description: "Build carefully", enabled: true, disabled_by: ""}, {name: "disabled", description: "Not available", enabled: false, disabled_by: "configuration"}], limited: false, ...extra});
  window.contextTest = {openContext, closeContext, $, tick, wait, posts, latest, visible, draft, nativeInsert, key, chips, clear, upload, paste, drop, textFile, png, image, status, reading, rows, choose, update, ready, swap, send, accept, files, skills};
})();
