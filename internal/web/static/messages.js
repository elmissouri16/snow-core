/* Conversation/message presentation adapted from Harness (MIT); see HARNESS-NOTICE.txt. */
(() => {
  "use strict";
  const roles = new Set(["user", "assistant", "plan"]);
  const textLimit = 64 * 1024, htmlLimit = 128 * 1024, messageLimit = 100;
  const toolLimit = 64, toolOutputLimit = 8 * 1024, toolTotalLimit = 128 * 1024;
  let copyGeneration = 0;
  const copyTimers = new Map();
  const imageTypes = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);
  const imageHandlers = new Map(), imageQueue = new Set();
  const imageLoadLimit = 800, imageLoadTimeout = 8000;
  let activeImage = null, imagePumpScheduled = false;
  const $ = (selector, scope) => scope.querySelector(selector);
  function element(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }
  function bounded(value, limit = textLimit) { return typeof value === "string" ? value.slice(0, limit) : ""; }
  function icon(copied = false) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 24 24"); svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("fill", "none"); svg.setAttribute("stroke", "currentColor");
    svg.setAttribute("stroke-width", "1.7"); svg.setAttribute("stroke-linecap", "round"); svg.setAttribute("stroke-linejoin", "round");
    const path = document.createElementNS(svg.namespaceURI, "path");
    path.setAttribute("d", copied ? "m5 12 4 4L19 6" : "M9 8V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-3M5 8h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-9a2 2 0 0 1 2-2Z");
    svg.append(path); return svg;
  }
  function copyButton() {
    const button = element("button", "message-copy");
    button.type = "button"; button.dataset.messageCopy = "";
    button.setAttribute("aria-label", "Copy message"); button.title = "Copy message";
    button.append(icon()); return button;
  }
  function decorate(row, role) {
    row.classList.add("conversation-message");
    row.classList.toggle("user-message", role === "user");
    row.dataset.messageRole = role;
    row.setAttribute("aria-label", role === "user" ? "Your message" : role === "plan" ? "Assistant plan" : "Assistant message");
    // Role stays accessible through the article name, never an uppercase visual header.
    $(".message-label", row)?.remove();
    if (!$(".message-actions", row)) {
      const actions = element("div", "message-actions");
      const feedback = element("span", "message-copy-feedback");
      feedback.setAttribute("role", "status"); feedback.setAttribute("aria-live", "polite");
      actions.append(copyButton(), feedback); row.append(actions);
    } else if (!$(".message-copy svg", row)) {
      $(".message-copy", row)?.replaceChildren(icon());
    }
  }
  function reuseControl(row, role) {
    const capable = document.querySelector('#live-session[data-message-edit-enabled="true"]');
    if (role !== "user" || row._snowHasImages) { $("[data-message-reuse]", row)?.remove(); $("[data-message-edit]", row)?.remove(); return; }
    if (capable) {
      $("[data-message-reuse]", row)?.remove();
      if (!row._snowEditable) { $("[data-message-edit]", row)?.remove(); return; }
      if (!$("[data-message-edit]", row)) {
        const button = element("button", "message-edit", "Edit & resend");
        button.type = "button"; button.dataset.messageEdit = ""; button.disabled = true;
        button.title = "Edit this message and replace the following conversation when you Send.";
        $(".message-actions", row)?.prepend(button);
      }
      return;
    }
    $("[data-message-edit]", row)?.remove();
    let button = $("[data-message-reuse]", row);
    if (!button) {
      button = element("button", "message-reuse", "Use as new prompt");
      button.type = "button"; button.dataset.messageReuse = "";
      button.title = "Use a copy as a new prompt. Saved history stays unchanged.";
      button.disabled = true; button.hidden = !row.closest('#live-session[data-runtime="true"]');
      $(".message-actions", row)?.prepend(button);
    }
  }
  function regenerateControl(row, role) {
    const capable = document.querySelector('#live-session[data-message-regenerate-enabled="true"]');
    if (!capable || role !== "assistant" || !row._snowRegeneratable) {
      $("[data-message-regenerate]", row)?.remove(); return;
    }
    if (!$("[data-message-regenerate]", row)) {
      const button = element("button", "message-regenerate", "Regenerate");
      button.type = "button"; button.dataset.messageRegenerate = ""; button.disabled = true;
      button.title = "Regenerate this reply and replace the following conversation after confirmation.";
      $(".message-actions", row)?.prepend(button);
    }
  }
  function imageScope(root) {
    return root ? JSON.stringify([root.dataset.project, root.dataset.instance, root.dataset.session]) : "";
  }
  function imageURL(raw, root, messageID, index, mime) {
    if (!root || !imageTypes.has(mime) || !Number.isSafeInteger(index) || index < 0 || index > 10000 ||
        typeof raw !== "string" || !raw || raw.length > 4096 || !messageID) return "";
    const {project, instance, session} = root.dataset;
    if (!project || !session) return "";
    try {
      const url = new URL(raw, window.location.origin);
      if (url.origin !== window.location.origin || url.username || url.password || url.hash) return "";
      const path = `/projects/${encodeURIComponent(project)}`;
      if (root.id === "live-session") {
        if (!instance || url.pathname !== `${path}/runtime/images/${encodeURIComponent(messageID)}/${index}` ||
            url.searchParams.getAll("instance_id").length !== 1 || url.searchParams.get("instance_id") !== instance ||
            url.searchParams.getAll("session_id").length !== 1 || url.searchParams.get("session_id") !== session ||
            url.searchParams.getAll("turn_id").length > 1 ||
            [...url.searchParams.keys()].some(key => !["instance_id", "session_id", "turn_id"].includes(key))) return "";
      } else if (url.pathname !== `${path}/sessions/${encodeURIComponent(session)}/images/${encodeURIComponent(messageID)}/${index}` || url.search) return "";
      // Accept only the exact server route, never normalized dot segments,
      // protocol-relative URLs, backslashes or whitespace-smuggled sources.
      if (raw !== url.pathname + url.search && raw !== url.href) return "";
      return url.pathname + url.search;
    } catch (_) { return ""; }
  }
  function scheduleImages() {
    if (imagePumpScheduled) return;
    imagePumpScheduled = true;
    // render() builds some rows detached. Admit only after reconciliation has
    // connected the whole batch, never while a sibling row is still being built.
    queueMicrotask(() => {
      imagePumpScheduled = false;
      if (activeImage && !activeImage.current()) releaseImage(activeImage.img);
      if (activeImage) return;
      for (const job of imageQueue) {
        imageQueue.delete(job);
        if (!job.current()) { releaseImage(job.img); continue; }
        activeImage = job; job.start(); break;
      }
    });
  }
  function unavailableImage(tile) {
    tile.dataset.imageState = "unavailable";
    $(".message-image-preview", tile).hidden = true;
    const fallback = $(".message-image-fallback", tile);
    fallback.hidden = false; fallback.textContent = "Image unavailable";
  }
  function releaseImage(img) {
    const job = imageHandlers.get(img);
    if (!job) return;
    job.cleanup(); imageQueue.delete(job); imageHandlers.delete(img);
    if (activeImage === job) {
      activeImage = null;
      // Cancel the browser read as well as its callbacks; retirement must not
      // leave a hidden request holding the shared catalog worker slot.
      img.removeAttribute("src");
    }
    if (["queued", "loading"].includes(job.tile.dataset.imageState)) unavailableImage(job.tile);
    scheduleImages();
  }
  function releaseImages(scope) {
    for (const img of scope.querySelectorAll(".message-image-preview")) releaseImage(img);
  }
  function watchImage(tile, root) {
    const img = $(".message-image-preview", tile), fallback = $(".message-image-fallback", tile);
    const url = tile._snowImageURL;
    if (!img || !fallback || !url || tile.dataset.imageState === "unavailable" || imageHandlers.has(img)) return;
    if (imageHandlers.size >= imageLoadLimit) { unavailableImage(tile); return; }
    const generation = copyGeneration, scope = imageScope(root), identity = tile._snowImageIdentity;
    let timer;
    const job = {img, tile};
    job.current = () => generation === copyGeneration && img.isConnected &&
      tile._snowImageIdentity === identity && imageScope(root) === scope;
    const finished = () => {
      clearTimeout(timer); imageQueue.delete(job);
      if (activeImage === job) activeImage = null;
      scheduleImages();
    };
    const failed = () => { if (job.current()) unavailableImage(tile); finished(); };
    const loaded = () => {
      if (job.current() && tile.dataset.imageState !== "unavailable") {
        tile.dataset.imageState = "loaded"; img.hidden = false; fallback.hidden = true;
      }
      finished();
    };
    job.cleanup = () => {
      clearTimeout(timer);
      img.removeEventListener("error", failed); img.removeEventListener("load", loaded);
    };
    job.start = () => {
      tile.dataset.imageState = "loading";
      // No lazy loading here: an offscreen active image must not block all
      // following images. The bounded queue, not viewport heuristics, admits it.
      img.loading = "eager";
      timer = setTimeout(() => {
        if (activeImage !== job) return;
        if (job.current()) unavailableImage(tile);
        job.cleanup(); img.removeAttribute("src"); finished();
      }, imageLoadTimeout);
      if (img.getAttribute("src") !== url) img.setAttribute("src", url);
      // Cached server markup may have completed before enhancement. The normal
      // template uses data-image-url and cannot start a read before admission.
      if (img.complete) { if (img.naturalWidth > 0) loaded(); else failed(); }
    };
    img.addEventListener("error", failed); img.addEventListener("load", loaded);
    imageHandlers.set(img, job);
    if (tile.dataset.imageState === "loaded") return;
    tile.dataset.imageState = "queued"; imageQueue.add(job); scheduleImages();
  }
  function renderImages(row, images, root) {
    let strip = $(".message-images", row);
    row._snowHasImages = Array.isArray(images) && images.length > 0;
    row.dataset.messageHasImages = String(row._snowHasImages);
    if (!row._snowHasImages) { if (strip) { releaseImages(strip); strip.remove(); } return; }
    if (!strip) {
      strip = element("div", "message-images");
      strip.setAttribute("role", "group"); strip.setAttribute("aria-label", "Attached images");
      row.insertBefore(strip, $(".message-body", row));
    }
    const input = images.slice(0, 8);
    for (const [position, image] of input.entries()) {
      const index = Number.isSafeInteger(image?.index) && image.index >= 0 && image.index <= 10000 ? image.index : -1;
      const mime = imageTypes.has(image?.mime_type) ? image.mime_type : "";
      const url = imageURL(image?.url, root, row.dataset.messageId, index, mime);
      let tile = strip.children[position];
      if (!tile) {
        tile = element("div", "message-image");
        const img = element("img", "message-image-preview");
        img.width = 96; img.height = 96; img.decoding = "async"; img.hidden = true;
        img.alt = `Attached image ${position + 1}`;
        tile.append(img, element("span", "message-image-fallback")); strip.append(tile);
      }
      const img = $(".message-image-preview", tile), fallback = $(".message-image-fallback", tile);
      const pending = imageTypes.has(mime) && index >= 0 && image?.url === "";
      const identity = JSON.stringify([imageScope(root), index, mime, url, pending]);
      if (tile._snowImageIdentity !== identity) {
        releaseImage(img); tile._snowImageIdentity = identity; tile._snowImageURL = url;
        tile.dataset.imageIndex = String(index); tile.dataset.imageMime = typeof mime === "string" ? mime : "";
        tile.dataset.imageState = url ? "loading" : pending ? "pending" : "unavailable";
        img.hidden = true; fallback.hidden = false;
        fallback.textContent = url ? "Loading image" : pending ? "Image pending" : "Image unavailable";
        if (!url) img.removeAttribute("src");
        else if (img.getAttribute("src") !== url) img.removeAttribute("src");
        if (url) img.dataset.imageUrl = url; else delete img.dataset.imageUrl;
      }
      watchImage(tile, root);
    }
    while (strip.children.length > input.length) { releaseImages(strip.lastChild); strip.lastChild.remove(); }
  }
  function enhanceImages(row) {
    const strip = $(".message-images", row);
    row._snowHasImages = row.dataset.messageHasImages === "true" || !!strip;
    if (!strip) return;
    const root = row.closest("#live-session") || row.closest(".catalog-history");
    // Saved HTML is server-generated; it never borrows image URLs from RPC.
    // Run the same route checks before retaining/enhancing its image sources.
    const images = [...strip.children].slice(0, 8).map(tile => ({
      index: Number(tile.dataset.imageIndex), mime_type: tile.dataset.imageMime,
      url: tile._snowImageIdentity !== undefined ? tile._snowImageURL || (tile.dataset.imageState === "pending" ? "" : null) :
        $(".message-image-preview", tile)?.dataset.imageUrl || $(".message-image-preview", tile)?.getAttribute("src") || ""
    }));
    renderImages(row, images, root);
  }
  function reconcileContent(current, next) {
    // The detached tree was created only by SnowMarkdown from the server's
    // bounded sanitized projection. Reconcile it rather than replacing a live
    // code banner on every prefix; copy controls and reading focus keep identity.
    const wanted = [...next.childNodes];
    for (let i = 0; i < wanted.length; i++) {
      const source = wanted[i], node = current.childNodes[i];
      const compatible = node && node.nodeType === source.nodeType && (node.nodeType !== Node.ELEMENT_NODE || (node.tagName === source.tagName && node.className === source.className));
      if (!compatible) {
        if (node) node.replaceWith(source); else current.append(source);
        continue;
      }
      if (node.nodeType === Node.TEXT_NODE) {
        if (node.data !== source.data) node.data = source.data;
      } else if (node.nodeType === Node.ELEMENT_NODE) {
        // Existing copy-code listeners close over this retained pre, not the
        // detached source node. Preserve temporary clipboard feedback too.
        if (node.classList.contains("code-banner") || node.classList.contains("copy-code")) continue;
        for (const attr of [...node.attributes]) if (!source.hasAttribute(attr.name)) node.removeAttribute(attr.name);
        for (const attr of source.attributes) if (node.getAttribute(attr.name) !== attr.value) node.setAttribute(attr.name, attr.value);
        reconcileContent(node, source);
      }
    }
    while (current.childNodes.length > wanted.length) current.lastChild.remove();
  }
  // Saved public output is literal text, never a Markdown/HTML projection. Count
  // UTF-8 bytes without encoding an unbounded, potentially malformed DTO first.
  function toolOutput(value, limit) {
    const source = typeof value === "string" ? value : "";
    let bytes = 0, end = 0;
    for (const point of source) {
      const code = point.codePointAt(0);
      const size = code <= 0x7f ? 1 : code <= 0x7ff ? 2 : code <= 0xffff ? 3 : 4;
      if (bytes + size > limit) break;
      bytes += size; end += point.length;
    }
    return {text: source.slice(0, end), bytes, truncated: end < source.length};
  }
  function toolBudget() { return {count: 0, bytes: 0, ids: new Set()}; }
  function renderTools(row, tools, budget, previouslyOmitted = false) {
    let list = $(".message-tools", row);
    const input = row.dataset.messageRole === "assistant" && Array.isArray(tools) ? tools : [];
    const selected = [];
    let omitted = previouslyOmitted || input.length > toolLimit;
    for (const tool of input.slice(0, toolLimit)) {
      if (!tool || typeof tool !== "object") continue;
      const id = bounded(tool.id, 256);
      if (!id || budget.ids.has(id)) continue;
      if (budget.count >= toolLimit) { omitted = true; break; }
      budget.count++; budget.ids.add(id);
      selected.push({tool, id});
    }
    if (!selected.length && !omitted) { list?.remove(); return; }
    if (!list) {
      list = element("div", "message-tools tool-timeline activity-list");
      list.setAttribute("role", "group"); list.setAttribute("aria-label", "Saved tool history");
      row.insertBefore(list, $(".message-actions", row));
    }
    const current = new Map([...list.querySelectorAll(":scope > .history-tool")].map(node => [node.dataset.historyToolId, node]));
    const wanted = new Set(selected.map(({id}) => id)), retained = new Set();
    // Remove evicted heads before positioning survivors, preserving focus when
    // a bounded history window advances. Never replace a retained disclosure.
    for (const node of [...list.children]) {
      if (!node.matches(".history-tool")) continue;
      const id = node.dataset.historyToolId;
      if (!wanted.has(id) || retained.has(id)) node.remove(); else retained.add(id);
    }
    let position = 0;
    for (const {tool, id} of selected) {
      let node = current.get(id);
      if (!node || !node.isConnected && node.parentElement !== list) {
        node = element("details", "tool-activity history-tool"); node.dataset.historyToolId = id;
        const summary = element("summary");
        summary.append(element("span", "activity-tool"), element("span", "activity-status"));
        const body = element("div", "activity-body");
        body.append(element("pre", "activity-output"), element("p", "fine activity-truncated", "Tool output truncated for bounded display."));
        node.append(summary, body);
      }
      const status = ["completed", "failed"].includes(tool.status) ? tool.status : "unresolved";
      const available = tool.output_available === true;
      const output = toolOutput(status !== "unresolved" && available ? tool.output : "", Math.min(toolOutputLimit, toolTotalLimit - budget.bytes));
      budget.bytes += output.bytes;
      node.dataset.outputAvailable = String(available);
      node.dataset.truncated = String(!!tool.truncated || output.truncated);
      const detail = status === "unresolved" ? "No result recorded; execution outcome unknown" : !available ? "Public output was not recorded" : output.text;
      window.SnowVisibility.renderToolRow(node, {
        tool: bounded(tool.tool, 128), status, error: status === "failed",
        label: status === "completed" ? "Completed" : status === "failed" ? "Failed" : "Outcome unknown",
        output: detail, expandable: !!detail || !!tool.truncated || output.truncated,
        truncated: status !== "unresolved" && available && (tool.truncated || output.truncated)
      });
      const at = list.children[position++];
      if (at !== node) list.insertBefore(node, at || null);
    }
    let notice = $(".history-tool-limit", list);
    if (omitted && !notice) { notice = element("p", "fine history-tool-limit", "Saved tool history truncated for bounded display."); list.append(notice); }
    if (notice) notice.hidden = !omitted;
  }
  function update(row, message, budget, root) {
    const role = message.role, text = bounded(message.text);
    // Only the server's explicit presentation projection may enter SnowMarkdown.
    // Over-limit HTML is rejected whole, never sliced into invalid markup.
    const html = typeof message.html === "string" && message.html.length <= htmlLimit ? message.html : "";
    decorate(row, role);
    let body = $(".message-body", row);
    if (!body) { body = element("div", "message-body"); row.prepend(body); }
    if (body._snowText !== text || body._snowHTML !== html || body._snowRole !== role) {
      body._snowText = text; body._snowHTML = html; body._snowRole = role;
      body.classList.toggle("markdown-body", role !== "user");
      if (role !== "user" && window.SnowMarkdown && (text || html)) {
        const next = element("div", "message-body");
        window.SnowMarkdown.render(next, text, html); enhanceCode(next);
        reconcileContent(body, next);
      } else if (body.textContent !== text) body.textContent = text;
      enhanceCode(row);
    }
    // This is the bounded public source text, not rendered HTML, tool metadata,
    // accessibility chrome or provider-private continuation data.
    row._snowCopyText = text;
    renderImages(row, message.images, root);
    body.hidden = !text && row._snowHasImages;
    row._snowEditable = !row._snowHasImages && role === "user" && message.can_edit === true;
    row._snowRegeneratable = role === "assistant" && message.can_regenerate === true && !message.truncated;
    row._snowReusable = !row._snowHasImages && role === "user" && !message.truncated && typeof message.text === "string" && message.text.length <= textLimit && new TextEncoder().encode(message.text).length <= textLimit;
    reuseControl(row, role); regenerateControl(row, role);
    const source = $(".message-source", row);
    if (source && source.textContent !== text) source.textContent = text;
    let notice = $(".message-truncated", row);
    if (!notice) { notice = element("p", "fine message-truncated", "Message truncated for bounded display."); body.after(notice); }
    notice.hidden = !message.truncated && (message.text || "").length <= textLimit;
    renderTools(row, message.tools, budget);
  }
  function messageKey(message, index) {
    if (!message) return "";
    // Runtime markers are explicit event-step identities, never inferred from
    // position, saved message IDs, or the nearest assistant presentation row.
    if (message.role === "tool_activity") {
      return typeof message.id === "string" && message.id.length <= 256 ? message.id : "";
    }
    return roles.has(message.role) ? bounded(message.id, 256) || `message-${index}` : "";
  }
  function render(transcript, messages) {
    if (!transcript) return;
    const focused = transcript.contains(document.activeElement) ? document.activeElement : null;
    const current = new Map([...transcript.children].map(row => [row.dataset.messageId, row]));
    const input = (Array.isArray(messages) ? messages.slice(-messageLimit) : []);
    const wanted = new Set(input.map(messageKey).filter(Boolean));
    // Evict old heads first: surviving rows are already in order. Moving B over
    // obsolete A would detach focused B descendants despite stable identities.
    for (const row of [...transcript.children]) if (!wanted.has(row.dataset.messageId)) { releaseImages(row); row.remove(); }
    const keep = new Set(), budget = toolBudget();
    let position = 0;
    for (const [index, message] of input.entries()) {
      const key = messageKey(message, index);
      if (!key || keep.has(key)) continue;
      keep.add(key);
      const activity = message.role === "tool_activity";
      let row = current.get(key);
      if (row && row.hasAttribute("data-runtime-activity-group") !== activity) { releaseImages(row); row.remove(); row = null; }
      if (!row) {
        row = element(activity ? "section" : "article", activity ? "runtime-tool-group tool-timeline" : "catalog-message");
        row.dataset.messageId = key;
        if (activity) {
          row.dataset.messageRole = "tool_activity"; row.dataset.runtimeActivityGroup = "";
          row.setAttribute("role", "group"); row.setAttribute("aria-label", "Runtime tool activity");
          row.append(element("div", "activity-list")); row.hidden = true;
        }
      }
      if (!activity) update(row, message, budget, transcript.closest("#live-session"));
      // Reconcile actual order as well as identity. Unchanged nodes (including
      // focused copy controls and tool disclosures) are never reinserted.
      const at = transcript.children[position++];
      if (at !== row) {
        if (transcript.moveBefore && row.isConnected) transcript.moveBefore(row, at || null);
        else transcript.insertBefore(row, at || null);
      }
    }
    for (const row of [...transcript.children]) if (!keep.has(row.dataset.messageId)) { releaseImages(row); row.remove(); }
    if (focused?.isConnected && document.activeElement !== focused) focused.focus({preventScroll: true});
  }
  function enhanceCode(scope) {
    // SnowMarkdown already creates functional copy controls from sanitized DOM.
    // Add a presentation banner without replacing its listener or code node.
    for (const block of scope.querySelectorAll(".markdown-body .code-block")) {
      const button = $(".copy-code", block), pre = $("pre", block);
      if (!button || !pre || $(".code-banner", block)) continue;
      button.dataset.copyCode = "";
      const banner = element("div", "code-banner");
      const label = element("span", "code-language", "Code");
      // Sanitizer strips language classes; do not guess a language from source.
      block.prepend(banner); banner.append(label, button);
    }
    for (const table of scope.querySelectorAll(".markdown-body table")) {
      if (table.parentElement.classList.contains("message-table-scroll")) continue;
      const wrap = element("div", "message-table-scroll");
      wrap.tabIndex = 0; wrap.setAttribute("role", "region"); wrap.setAttribute("aria-label", "Scrollable table");
      table.before(wrap); wrap.append(table);
    }
  }
  function enhance(scope = document) {
    window.SnowMarkdown?.enhance(scope);
    const budgets = new Map();
    for (const row of scope.querySelectorAll(".catalog-message")) {
      const role = row.dataset.messageRole || (row.classList.contains("user-message") ? "user" : $(".message-label", row)?.textContent.trim().toLowerCase() || "assistant");
      if (!roles.has(role)) continue;
      const source = $(".message-source", row), body = $(".message-body", row) || $("pre", row);
      if (body) body.classList.add("message-body");
      // Server partial supplies exact source. Older saved templates can only
      // offer their public visible text; never infer raw Markdown from HTML.
      if (row._snowCopyText === undefined) row._snowCopyText = bounded(source ? source.textContent : body?.textContent);
      decorate(row, role);
      enhanceImages(row);
      if (body) body.hidden = !row._snowCopyText && row._snowHasImages;
      if (row._snowHasImages) { row._snowReusable = false; row._snowEditable = false; }
      if (row._snowReusable === undefined) {
        const raw = source ? source.textContent : body?.textContent;
        row._snowReusable = role === "user" && !!source && $(".message-truncated", row)?.hidden !== false && typeof raw === "string" && new TextEncoder().encode(raw).length <= textLimit;
      }
      if (row._snowEditable === undefined) row._snowEditable = role === "user" && row.dataset.messageEditable === "true";
      if (row._snowRegeneratable === undefined) row._snowRegeneratable = role === "assistant" && row.dataset.messageRegeneratable === "true" && $(".message-truncated", row)?.hidden !== false;
      reuseControl(row, role); regenerateControl(row, role);
      const transcript = row.parentElement;
      if (!budgets.has(transcript)) budgets.set(transcript, toolBudget());
      const tools = [...row.querySelectorAll(".message-tools > .history-tool")].map(node => ({
        id: node.dataset.historyToolId, tool: node.dataset.toolName || $(".activity-tool", node)?.textContent,
        status: node.dataset.status, output_available: node.dataset.outputAvailable === "true",
        output: $(".activity-output", node)?.textContent, truncated: node.dataset.truncated === "true"
      }));
      // Enhancement reads an already bounded DOM projection; it cannot infer
      // omitted records from the retained rows. Keep its existing count notice.
      const notice = $(".history-tool-limit", row);
      renderTools(row, tools, budgets.get(transcript), !!notice && !notice.hidden);
    }
    enhanceCode(scope);
  }
  async function copy(event) {
    const button = event.target.closest?.("[data-message-copy]");
    if (!button) return;
    const row = button.closest(".conversation-message");
    if (!row || typeof row._snowCopyText !== "string") return;
    const feedback = $(".message-copy-feedback", row);
    clearTimeout(copyTimers.get(button)); copyTimers.delete(button);
    const generation = copyGeneration;
    const reset = () => {
      copyTimers.delete(button);
      if (!button.isConnected) return;
      button.replaceChildren(icon()); button.title = "Copy message";
      button.setAttribute("aria-label", "Copy message"); feedback.textContent = "";
    };
    try {
      await navigator.clipboard.writeText(row._snowCopyText);
      if (!button.isConnected || generation !== copyGeneration) return;
      button.replaceChildren(icon(true)); button.title = "Copied";
      button.setAttribute("aria-label", "Message copied"); feedback.textContent = "Copied";
    } catch (_) {
      if (!button.isConnected || generation !== copyGeneration) return;
      feedback.textContent = "Could not copy. Select the message and copy manually.";
    }
    copyTimers.set(button, setTimeout(reset, 2500));
  }
  function dispose() {
    copyGeneration++;
    for (const timer of copyTimers.values()) clearTimeout(timer);
    copyTimers.clear();
    for (const img of imageHandlers.keys()) releaseImage(img);
  }
  function init(region) {
    dispose();
    if (!region) return;
    enhance(region);
  }
  document.addEventListener("click", copy);
  window.SnowMessages = Object.freeze({render, enhance, init, dispose});
})();
