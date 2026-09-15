(() => {
  "use strict";
  // Attachment drafts are tab memory only. Nothing is uploaded before Send.
  const drafts = new Map(), encoder = new TextEncoder();
  const TEXT_LIMIT = 64 * 1024, PROMPT_LIMIT = 128 * 1024, IMAGE_LIMIT = 2 * 1024 * 1024;
  let sequence = 0, active = null;
  const bytes = text => encoder.encode(text).length;
  const node = (tag, className, text) => {
    const result = document.createElement(tag); result.className = className;
    if (text !== undefined) result.textContent = text;
    return result;
  };
  function mentionIcon(kind) {
    const icon = node("span", "composer-mention-icon");
    icon.setAttribute("aria-hidden", "true"); icon.setAttribute("data-kind", kind);
    // Fixed inline geometry works under img-src 'self'; no URLs or parsed HTML.
    const svgNode = (tag, attributes) => {
      const element = document.createElementNS("http://www.w3.org/2000/svg", tag);
      for (const [name, value] of Object.entries(attributes)) element.setAttribute(name, value);
      return element;
    };
    const svg = svgNode("svg", {viewBox: "0 0 24 24", width: "16", height: "16", fill: "none", stroke: "currentColor", "stroke-width": "1.6", "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true", focusable: "false"});
    if (kind === "skill") svg.append(svgNode("path", {d: "m9 5 2 5 5 2-5 2-2 5-2-5-5-2 5-2 2-5Z M19 2l1 3 3 1-3 1-1 3-1-3-3-1 3-1 1-3Z"}));
    else if (kind === "folder") svg.append(svgNode("path", {d: "M3 6a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"}));
    else svg.append(svgNode("rect", {x: "5", y: "3", width: "14", height: "18", rx: "3"}), svgNode("path", {d: "M8 8h8M8 12h6"}));
    icon.append(svg); return icon;
  }
  function validText(text) {
    if (typeof text !== "string" || text.includes("\0")) return false;
    for (let i = 0; i < text.length; i++) {
      const code = text.charCodeAt(i);
      if (code >= 0xd800 && code <= 0xdbff) {
        const next = text.charCodeAt(++i); if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
      } else if (code >= 0xdc00 && code <= 0xdfff) return false;
    }
    return true;
  }
  function imageType(data) {
    const starts = values => values.every((value, index) => data[index] === value);
    if (starts([137, 80, 78, 71, 13, 10, 26, 10])) return "image/png";
    if (starts([255, 216, 255])) return "image/jpeg";
    const ascii = (start, end) => String.fromCharCode(...data.subarray(start, end));
    if (["GIF87a", "GIF89a"].includes(ascii(0, 6))) return "image/gif";
    if (ascii(0, 4) === "RIFF" && ascii(8, 12) === "WEBP") return "image/webp";
    return "";
  }
  function previewDimensionsSafe(data, mime) {
    // Inspect only bounded headers before giving a browser decoder any bytes.
    // These canvas limits mirror validComposerImage; this is not full image
    // validation, and a missing/malformed header only suppresses the preview.
    const ascii = (start, end) => String.fromCharCode(...data.subarray(start, end));
    const be16 = offset => data[offset] * 256 + data[offset + 1];
    const le16 = offset => data[offset] + data[offset + 1] * 256;
    const be32 = offset => be16(offset) * 65536 + be16(offset + 2);
    const le24 = offset => le16(offset) + data[offset + 2] * 65536;
    const le32 = offset => le16(offset) + le16(offset + 2) * 65536;
    let width = 0, height = 0;
    if (mime === "image/png") {
      if (data.length < 33 || be32(8) !== 13 || ascii(12, 16) !== "IHDR") return false;
      width = be32(16); height = be32(20);
    } else if (mime === "image/gif") {
      if (data.length < 13) return false;
      width = le16(6); height = le16(8);
    } else if (mime === "image/jpeg") {
      let offset = 2;
      // Reject unavailable SOF instead of scanning entropy data or decoding.
      for (let segments = 0; offset < data.length && segments < 1024; segments++) {
        if (data[offset++] !== 0xff) return false;
        while (offset < data.length && data[offset] === 0xff) offset++;
        const marker = data[offset++];
        if (marker === 0x01 || (marker >= 0xd0 && marker <= 0xd7)) continue;
        if (!marker || marker === 0xd8 || marker === 0xd9 || marker === 0xda || offset + 2 > data.length) return false;
        const length = be16(offset);
        if (length < 2 || offset + length > data.length) return false;
        if ([0xc0, 0xc1, 0xc2].includes(marker)) {
          if (length < 11 || data[offset + 7] < 1 || data[offset + 7] > 4 || length !== 8 + 3 * data[offset + 7]) return false;
          height = be16(offset + 3); width = be16(offset + 5); break;
        }
        offset += length;
      }
    } else if (mime === "image/webp") {
      if (data.length < 25 || le32(4) + 8 !== data.length) return false;
      const size = le32(16), chunk = ascii(12, 16);
      if (size + size % 2 > data.length - 20) return false;
      if (chunk === "VP8X" && size === 10) {
        width = le24(24) + 1; height = le24(27) + 1;
      } else if (chunk === "VP8L" && size >= 5 && data[20] === 0x2f && (data[24] & 0xe0) === 0) {
        const bits = le32(21);
        width = (bits & 0x3fff) + 1; height = ((bits >>> 14) & 0x3fff) + 1;
      } else if (chunk === "VP8 " && size >= 10 && (data[20] & 1) === 0 && ascii(23, 26) === "\x9d\x01\x2a") {
        width = le16(26) & 0x3fff; height = le16(28) & 0x3fff;
      }
    }
    return width > 0 && height > 0 && width <= 16384 && height <= 16384 && width * height <= 40_000_000;
  }
  function base64(data) {
    let binary = "";
    for (let i = 0; i < data.length; i += 8192) binary += String.fromCharCode(...data.subarray(i, i + 8192));
    return btoa(binary);
  }
  const labelText = item => `Attachment: ${item.label}\n${item.kind === "text" ? item.text : "[Image]"}`;
  const privacyNotice = "Text and image contents will be sent to the provider and persisted in saved conversation history when you send. Images require a vision-capable model.";
  function releasePreview(item) {
    if (item.previewURL) URL.revokeObjectURL(item.previewURL);
    item.previewURL = null; item.previewElement = null;
  }
  function previewURL(item) {
    if (item.state !== "ready" || item.kind !== "image" || item.previewFailed) return "";
    if (item.previewURL) return item.previewURL;
    try {
      // Only decode retained, validated raster content; never use a filename,
      // remote URL or data URL as an image source. Raw draft images total 2 MiB.
      if (item.size > IMAGE_LIMIT || !["image/png", "image/jpeg", "image/gif", "image/webp"].includes(item.mime)) return "";
      const binary = atob(item.data), data = Uint8Array.from(binary, char => char.charCodeAt(0));
      if (data.length !== item.size || imageType(data) !== item.mime || !previewDimensionsSafe(data, item.mime)) {
        item.previewFailed = true; return "";
      }
      item.previewURL = URL.createObjectURL(new Blob([data], {type: item.mime}));
      return item.previewURL;
    } catch (_) {
      // Preview support or browser decoding must never change send eligibility.
      item.previewFailed = true; return "";
    }
  }
  function retire(draft) {
    draft.owner = null;
    for (const item of draft.items) {
      releasePreview(item); item.previewFailed = false;
      if (item.state === "pending") {
        item.state = "error"; item.error = "Read interrupted. Remove this attachment and add it again."; item.version++;
      }
    }
    draft.revision++;
  }
  function forget(key) {
    const draft = drafts.get(key); if (draft) retire(draft);
    drafts.delete(key);
  }
  function init({root, key, instance, request, changed = () => {}, error = () => {}}) {
    active?.dispose(); active = null;
    const find = selector => root.querySelector(selector);
    const prompt = find("#live-prompt"), input = find("[data-composer-file-input]");
    const contextMenu = find("[data-composer-context-menu]");
    const attach = find("[data-composer-attach]"), files = find("[data-composer-files]"), skills = find("[data-composer-skills]");
    const chips = find("[data-composer-context-items]"), notice = find("[data-composer-context-status]"), popup = find("[data-composer-mentions]");
    if (!prompt || !input || !chips || !notice || !popup) throw new Error("Composer context markup is incomplete");
    let draft = drafts.get(key);
    if (draft) { retire(draft); drafts.delete(key); }
    else draft = {items: [], revision: 0};
    const owner = {}; draft.owner = owner; drafts.set(key, draft);
    while (drafts.size > 16) forget(drafts.keys().next().value);
    const listeners = new AbortController(), options = {signal: listeners.signal};
    let disposed = false, authority = {safe: false, editable: false, readable: false};
    let queryRevision = 0, query = null, rows = [], selected = 0, directory = null, skillsPromise = null;
    let localError = "", composing = false, reading = 0, pendingBytes = 0, discoveries = 0, positionObserver, plusMenu = null;
    const alive = () => !disposed && draft.owner === owner && drafts.get(key) === draft;
    const canRemove = () => alive() && authority.editable && !prompt.closest("[inert], [hidden]");
    const canAdd = () => canRemove() && authority.safe;
    const canRead = () => canAdd() && authority.readable;
    const pending = () => alive() && (reading > 0 || discoveries > 0 || draft.items.some(item => item.state === "pending"));
    const hasAttachments = () => alive() && draft.items.length > 0;
    function close() {
      queryRevision++; query = null; rows = []; popup.hidden = true; popup.replaceChildren();
      prompt.setAttribute("aria-expanded", "false"); prompt.removeAttribute("aria-activedescendant");
    }
    function closePlusMenu(restoreFocus = false) {
      if (!plusMenu) return;
      plusMenu = null;
      window.SnowMenus?.close({restoreFocus});
    }
    function openPlusMenu() {
      if (!canRead() || !window.SnowMenus) return;
      if (plusMenu) { closePlusMenu(true); return; }
      close();
      const panel = node("div", "composer-context-menu"), owned = {panel};
      for (const [marker, label, hook] of [["@", "Project files", "files"], ["$", "Installed skills", "skills"]]) {
        const row = node("button", "snow-menu-row composer-context-menu-item"); row.type = "button";
        row.setAttribute("role", "menuitem"); row.setAttribute(`data-composer-${hook}`, "");
        row.append(mentionIcon(hook === "skills" ? "skill" : "folder"), node("span", "snow-menu-row-label", label));
        const shortcut = node("span", "snow-menu-row-value", marker); shortcut.setAttribute("aria-hidden", "true"); row.append(shortcut);
        row.disabled = hook === "files" && draft.items.length >= 8;
        owned[hook] = row;
        row.addEventListener("click", () => {
          if (plusMenu !== owned || !canRead() || (hook === "files" && draft.items.length >= 8)) return;
          closePlusMenu();
          insertMarker(marker);
        }, options);
        panel.append(row);
      }
      plusMenu = owned;
      window.SnowMenus.open({trigger: contextMenu, panel, placement: "top-start", onClose: () => {
        if (plusMenu === owned) plusMenu = null;
      }});
    }
    function report(message) { if (!alive()) return; localError = message; paint(); error(message); changed(); }
    function notify() { if (alive()) { draft.revision++; paint(); changed(); } }
    function paint() {
      if (!alive()) return;
      attach && (attach.disabled = !canAdd() || draft.items.length >= 8);
      input.disabled = !canAdd() || draft.items.length >= 8;
      files && (files.disabled = !canRead() || draft.items.length >= 8);
      skills && (skills.disabled = !canRead());
      contextMenu && (contextMenu.disabled = !canRead());
      if (plusMenu) {
        plusMenu.files.disabled = !canRead() || draft.items.length >= 8;
        plusMenu.skills.disabled = !canRead();
      }
      const focusedItem = document.activeElement?.getAttribute?.("data-context-item-id");
      let restoreFocus = null;
      chips.replaceChildren();
      for (const item of draft.items) {
        const chip = node("div", `composer-context-chip${item.state === "error" ? " is-error" : ""}`);
        const name = node("span", "composer-context-label", item.label); name.title = item.label;
        const url = previewURL(item);
        if (url) {
          const thumbnail = node("span", "composer-context-thumbnail"), image = node("img", "");
          image.alt = ""; image.width = 32; image.height = 32;
          item.previewElement = image;
          image.addEventListener("error", () => {
            if (!alive() || !draft.items.includes(item) || item.previewURL !== url || item.previewElement !== image) return;
            thumbnail.hidden = true; releasePreview(item); item.previewFailed = true;
          }, options);
          image.src = url; thumbnail.append(image); chip.append(thumbnail);
        }
        chip.append(name);
        if (item.state !== "ready") chip.append(node("span", "composer-context-detail", item.state === "pending" ? "Reading…" : item.error));
        const remove = node("button", "composer-context-remove", "×"); remove.type = "button";
        remove.setAttribute("aria-label", `Remove attachment ${item.label}`); remove.disabled = !canRemove();
        remove.setAttribute("data-context-item-id", String(item.id));
        if (focusedItem === String(item.id)) restoreFocus = remove;
        remove.addEventListener("click", () => {
          if (!canRemove()) return;
          releasePreview(item); draft.items = draft.items.filter(candidate => candidate !== item); localError = ""; notify();
        });
        chip.append(remove); chips.append(chip);
      }
      chips.hidden = !draft.items.length;
      if (focusedItem) (restoreFocus || attach)?.focus({preventScroll: true});
      const imageNotice = draft.items.some(item => item.kind === "image") ? " Images need a vision-capable model." : "";
      notice.textContent = [localError, draft.items.length ? `On Send: shared with provider and saved in chat.${imageNotice}` : "", !authority.editable && draft.items.length ? "Attachments are kept; finish or cancel the current operation to change them." : ""].filter(Boolean).join(" ");
      notice.title = draft.items.length ? privacyNotice : "";
      notice.hidden = !notice.textContent; notice.classList.toggle("is-error", !!localError);
      position();
    }
    function render(next) {
      authority = {...authority, ...next};
      if (!canRead()) { close(); closePlusMenu(); }
      paint();
    }
    function reserve(label, size = 0) {
      if (!canAdd()) return null;
      if (draft.items.length >= 8) { report("At most 8 attachments can be added. Remove one before adding another."); return null; }
      if (!validText(label) || bytes(label) > 4096) { report("Attachment name is invalid or too long."); return null; }
      if (!Number.isSafeInteger(size) || size < 0 || size > IMAGE_LIMIT || pendingBytes + size > IMAGE_LIMIT || reading >= 8) {
        report("Attachments being read must total at most 2 MiB. Wait for current reads or choose a smaller file."); return null;
      }
      const item = {id: ++sequence, version: 0, label, state: "pending", size};
      draft.items.push(item); reading++; pendingBytes += size; localError = ""; notify(); return item;
    }
    const owns = item => alive() && draft.items.includes(item) && item.state === "pending";
    function complete(item, content) {
      if (!owns(item)) return false;
      const candidate = {...item, ...content, state: "ready"};
      const ready = draft.items.filter(other => other !== item && other.state === "ready");
      if (ready.reduce((sum, other) => sum + bytes(labelText(other)), bytes(labelText(candidate))) > TEXT_LIMIT) throw new Error("Attachment text and labels must total at most 64 KiB.");
      if (ready.reduce((sum, other) => sum + (other.kind === "image" ? other.size : 0), candidate.kind === "image" ? candidate.size : 0) > IMAGE_LIMIT) throw new Error("Image attachments must total at most 2 MiB.");
      Object.assign(item, candidate); item.version++; notify(); return true;
    }
    function failed(item, message) {
      if (!owns(item)) return;
      item.state = "error"; item.error = message; item.version++; notify();
    }
    function finished(size) {
      reading--; pendingBytes -= size;
      if (alive()) { paint(); changed(); }
    }
    async function localFile(file) {
      const item = reserve(file.name || "Pasted image", file.size); if (!item) return;
      try {
        const data = new Uint8Array(await file.arrayBuffer());
        if (!owns(item)) return;
        if (data.length !== file.size || data.length > IMAGE_LIMIT) throw new Error("File changed or exceeds the attachment size limit.");
        const mime = imageType(data);
        if (mime) complete(item, {kind: "image", mime, data: base64(data), size: data.length});
        else {
          if (/^image\//i.test(file.type || "") || String.fromCharCode(...data.subarray(0, 5)) === "%PDF-") throw new Error("Only PNG, JPEG, GIF, WebP images or UTF-8 text can be attached; PDFs are not supported.");
          if (data.length > TEXT_LIMIT) throw new Error("Text attachments must total at most 64 KiB.");
          let text;
          try { text = new TextDecoder("utf-8", {fatal: true}).decode(data); }
          catch (_) { throw new Error("This file is not valid UTF-8 text or a supported image."); }
          if (!validText(text)) throw new Error("Text attachments cannot contain NUL bytes or invalid Unicode.");
          complete(item, {kind: "text", text, size: data.length});
        }
      } catch (reason) { failed(item, reason.message || "Unable to read attachment. Remove it and try again."); }
      finally { finished(file.size); }
    }
    function addFiles(list) {
      if (!canAdd()) return;
      // Do not retain an unbounded browser FileList or start unbounded reads.
      for (let i = 0; i < Math.min(list.length, 8); i++) localFile(list[i]);
      if (list.length > 8) report("Only the first 8 files were considered. At most 8 attachments are allowed.");
    }
    function token() {
      if (prompt.selectionStart !== prompt.selectionEnd) return null;
      const caret = prompt.selectionStart, before = prompt.value.slice(0, caret);
      const match = /(?:^|\s)([@$])(?:"([^"\n]*)|([^\s"@$]*))$/.exec(before);
      if (!match) return null;
      const marker = match[1], rawValue = match[2] ?? match[3];
      let value = rawValue;
      if (match[2] !== undefined) {
        try { value = JSON.parse(`"${rawValue}"`); } catch (_) { return null; }
      }
      return {marker, value, start: caret - rawValue.length - 1 - (match[2] !== undefined ? 1 : 0), end: caret, text: prompt.value, caret};
    }
    const validQuery = current => alive() && canRead() && query === current && current.revision === queryRevision && prompt.value === current.text && prompt.selectionStart === current.caret && prompt.selectionEnd === current.caret;
    function replace(current, value, reopen = false) {
      if (!validQuery(current)) return false;
      prompt.setRangeText(value, current.start, current.end, "end"); close();
      prompt.dispatchEvent(new Event("input", {bubbles: true})); prompt.focus({preventScroll: true});
      if (!reopen) close();
      return true;
    }
    function select(index) {
      selected = index;
      [...popup.querySelectorAll('[role="option"]')].forEach((row, i) => {
        row.setAttribute("aria-selected", String(i === selected));
        const hint = row.querySelector(".composer-mention-folder-hint");
        if (hint) hint.hidden = i !== selected;
      });
      const row = popup.querySelectorAll('[role="option"]')[selected];
      if (row) {
        prompt.setAttribute("aria-activedescendant", row.id);
        // Scroll this list only; never pull the transcript away from the reader.
        if (row.offsetTop < popup.scrollTop) popup.scrollTop = row.offsetTop;
        else if (row.offsetTop + row.offsetHeight > popup.scrollTop + popup.clientHeight) popup.scrollTop = row.offsetTop + row.offsetHeight - popup.clientHeight;
      } else prompt.removeAttribute("aria-activedescendant");
    }
    function position() {
      if (popup.hidden || !prompt.getBoundingClientRect) return;
      const form = (find("#live-composer") || root).getBoundingClientRect();
      const inputBounds = prompt.getBoundingClientRect(), viewport = window.visualViewport;
      const margin = 8, gap = 8;
      const left = (viewport?.offsetLeft || 0) + margin, top = (viewport?.offsetTop || 0) + margin;
      const width = Math.max(0, (viewport?.width || window.innerWidth) - margin * 2);
      const height = Math.max(0, (viewport?.height || window.innerHeight) - margin * 2), bottom = top + height;
      const minimum = Math.min(120, height), aboveForm = form.top - gap - top;
      const aboveInput = inputBounds.top - gap - top, belowInput = bottom - inputBounds.bottom - gap;
      let clearance, anchor, below = false;
      // Prefer above the composer. Short screens may need an input-adjacent
      // placement; retain the textarea rather than shrinking options to slivers.
      if (aboveForm >= minimum) { clearance = aboveForm; anchor = form.top - gap; }
      else if (aboveInput >= minimum) { clearance = aboveInput; anchor = inputBounds.top - gap; }
      else if (belowInput >= minimum) { clearance = belowInput; anchor = inputBounds.bottom + gap; below = true; }
      else { clearance = height; anchor = bottom; }
      const popupWidth = Math.min(Math.max(0, form.width), width);
      popup.style.width = `${popupWidth}px`;
      popup.style.maxHeight = `${Math.min(320, Math.max(0, clearance))}px`;
      popup.style.left = `${Math.max(left, Math.min(form.left, left + width - popupWidth))}px`;
      const actualHeight = Math.min(popup.offsetHeight, clearance, 320);
      popup.style.top = `${Math.max(top, Math.min(below ? anchor : anchor - actualHeight, bottom - actualHeight))}px`;
      select(selected);
    }
    function show(current, choices, message = "", heading = false) {
      if (!validQuery(current)) return;
      rows = choices; selected = 0; popup.replaceChildren(); popup.hidden = false;
      prompt.setAttribute("aria-expanded", "true");
      if (message) popup.append(node("div", heading ? "composer-mention-heading" : "composer-mention-note", message));
      choices.forEach((choice, index) => {
        const row = node("div", "composer-mention-option"); row.id = `composer-context-option-${++sequence}`;
        row.setAttribute("role", "option"); row.setAttribute("aria-selected", String(index === 0));
        const icon = mentionIcon(choice.folder ? "folder" : current.marker === "$" ? "skill" : "file");
        const main = node("span", "composer-mention-main"), name = node("span", "composer-mention-name", choice.label);
        name.title = choice.label; main.append(name);
        const description = choice.fullDescription ?? choice.description;
        row.title = [choice.title || choice.label, description].filter(Boolean).join(" — ");
        if (choice.description) {
          const detail = node("span", "composer-mention-description", choice.description);
          detail.title = description; main.append(detail);
        }
        row.append(icon, main);
        if (choice.folder) {
          row.append(node("span", "composer-mention-folder-hint", "Browse folder"));
          const chevron = node("span", "composer-mention-chevron", "›"); chevron.setAttribute("aria-hidden", "true");
          row.append(chevron);
        }
        if (choice.disabled) row.setAttribute("aria-disabled", "true");
        if (choice.retrySkills) row.setAttribute("data-composer-skills-retry", "");
        row.addEventListener("mousedown", event => event.preventDefault());
        row.addEventListener("click", () => { if (validQuery(current) && !choice.disabled) choice.choose(); });
        popup.append(row);
      });
      select(0); position();
    }
    function validPath(path) {
      return typeof path === "string" && bytes(path) <= 4096 && validText(path) && !/[\x00-\x1f\x7f\\:]/.test(path) && path.split("/").every(part => part && part !== "." && part !== "..");
    }
    function fileChoices(current, listing) {
      if (!validQuery(current)) return;
      const choices = listing.entries.filter(entry => entry.name.toLocaleLowerCase().includes(current.filter.toLocaleLowerCase())).map(entry => ({
        label: entry.name, title: entry.path, folder: entry.kind === "directory",
        choose: () => {
          if (entry.kind === "directory") replace(current, `@${JSON.stringify(entry.path + "/").replaceAll("$", "\\u0024").slice(0, -1)}`, true);
          else projectFile(current, entry.path);
        }
      }));
      if (listing.hasMore && listing.offset < 4096 && listing.entries.length < 4096) choices.push({label: "More files…", choose: () => loadFiles(current, listing.offset, listing)});
      show(current, choices, listing.limited ? "Directory listing is limited to 4,096 entries. Type a folder path to narrow it." : choices.length ? "Files & folders" : "No matching files in this folder.", !listing.limited && choices.length > 0);
    }
    async function loadFiles(current, offset = 0, previous = null) {
      if (!validQuery(current) || current.loading) return;
      current.loading = true; discoveries++; changed(); show(current, [], "Loading files…");
      try {
        const response = await request("files", {path: current.path, offset});
        if (!validQuery(current)) return;
        if (!response || response.path !== current.path || !Array.isArray(response.entries) || response.entries.length > 256) throw new Error("Invalid directory listing.");
        const entries = response.entries.filter(entry => entry && ["file", "directory"].includes(entry.kind) && validPath(entry.path) && typeof entry.name === "string" && entry.name === entry.path.split("/").at(-1) && !entry.path.includes('"') && (current.path === "." ? !entry.path.includes("/") : entry.path.slice(0, entry.path.lastIndexOf("/")) === current.path));
        const merged = [...(previous?.entries || []), ...entries].slice(0, 4096);
        directory = {path: current.path, entries: merged, offset: response.next_offset, hasMore: response.has_more === true && Number.isSafeInteger(response.next_offset) && response.next_offset > offset, limited: response.limited === true || merged.length >= 4096};
        fileChoices(current, directory);
      } catch (_) { if (validQuery(current)) show(current, [], "Could not list this folder. Edit the @path to try again."); }
      finally { current.loading = false; discoveries--; if (alive()) changed(); }
    }
    async function projectFile(current, path) {
      if (!validQuery(current) || current.reading) return;
      const item = reserve(path); if (!item) return;
      current.reading = true; show(current, [], "Reading selected file…");
      try {
        const response = await request("file", {path});
        if (!owns(item)) return;
        if (!validQuery(current)) throw new Error("Selection changed during the read. Remove this attachment and select the file again.");
        if (!response || response.path !== path || response.truncated !== false) throw new Error("This file preview is truncated or unavailable. Partial files are not attached.");
        if (!validText(response.text) || !Number.isSafeInteger(response.size) || response.size < 0 || response.size > TEXT_LIMIT || bytes(response.text) > TEXT_LIMIT) throw new Error("Selected file must be UTF-8 text without NUL bytes, at most 64 KiB.");
        if (complete(item, {kind: "text", text: response.text, size: response.size})) replace(current, `@${JSON.stringify(path).replaceAll("$", "\\u0024")} `);
      } catch (reason) { failed(item, reason.message || "Could not read selected file."); if (validQuery(current)) close(); }
      finally { current.reading = false; finished(0); }
    }
    async function loadSkills(current) {
      if (!validQuery(current) || current.skillsLoading) return;
      current.skillsLoading = true; discoveries++; changed();
      show(current, [], "Loading installed skills…");
      try {
        // Failed discoveries stay cached until the explicit Retry choice.
        // Typing and reopening never cause a hidden discovery loop.
        if (!skillsPromise) {
          if (draft.skillCatalog?.instance === instance) skillsPromise = draft.skillCatalog.promise;
          else {
            skillsPromise = Promise.resolve().then(() => request("skills", {}));
            draft.skillCatalog = {instance, promise: skillsPromise};
          }
        }
        const response = await skillsPromise;
        if (!validQuery(current)) return;
        if (!response || response.instance_id !== instance || !Array.isArray(response.skills) || response.skills.length > 4096) throw new Error("Invalid installed skill catalog.");
        if (response.enabled !== true) { show(current, [], "Close and start with Enable installed skills to use installed skills."); return; }
        const choices = response.skills.filter(skill => skill && typeof skill.name === "string" && /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(skill.name) && skill.name.toLowerCase().includes(current.value.toLowerCase())).slice(0, 256).map(skill => ({
          label: `$${skill.name}`, description: skill.enabled === true ? String(skill.description || "").slice(0, 512) : `Disabled${typeof skill.disabled_by === "string" ? `: ${skill.disabled_by.slice(0, 128)}` : ""}`,
          fullDescription: skill.enabled === true ? String(skill.description || "") : `Disabled${typeof skill.disabled_by === "string" ? `: ${skill.disabled_by}` : ""}`,
          disabled: skill.enabled !== true, choose: () => replace(current, `$${skill.name} `)
        }));
        show(current, choices, response.limited || choices.length === 256 ? "Catalog is limited. Type a skill name to narrow results." : choices.length ? "Skills" : "No matching enabled installed skills.", !response.limited && choices.length > 0 && choices.length < 256);
      } catch (_) {
        if (validQuery(current)) show(current, [{label: "Retry loading skills", retrySkills: true, choose: () => {
          if (!validQuery(current) || current.skillsLoading) return;
          if (draft.skillCatalog?.promise === skillsPromise) delete draft.skillCatalog;
          skillsPromise = null;
          loadSkills(current);
        }}], "Installed skills could not be loaded for this runtime. Nothing was activated. Retry only when you choose.");
      }
      finally { current.skillsLoading = false; discoveries--; if (alive()) changed(); }
    }
    function inspectToken() {
      close();
      if (!canRead() || composing) return;
      const current = token(); if (!current) return;
      current.revision = queryRevision; query = current;
      if (current.marker === "$") { loadSkills(current); return; }
      const slash = current.value.lastIndexOf("/");
      current.path = slash < 0 ? "." : current.value.slice(0, slash);
      current.filter = current.value.slice(slash + 1);
      if (current.path !== "." && !validPath(current.path)) { close(); return; }
      if (directory?.path === current.path) fileChoices(current, directory); else loadFiles(current);
    }
    function insertMarker(marker) {
      if (!canRead()) return;
      const start = prompt.selectionStart;
      prompt.setRangeText(`${start && !/\s/.test(prompt.value[start - 1]) ? " " : ""}${marker}`, start, prompt.selectionEnd, "end");
      prompt.focus({preventScroll: true}); prompt.dispatchEvent(new Event("input", {bubbles: true}));
    }
    function capture(text = prompt.value) {
      if (!alive()) throw new Error("This attachment draft is no longer active.");
      if (pending()) throw new Error("Wait for attachment reads to finish before sending.");
      if (draft.items.some(item => item.state === "error")) throw new Error("Remove failed attachments before sending; they have not been silently omitted.");
      if (!validText(text)) throw new Error("Prompt must be valid Unicode without NUL bytes.");
      const items = draft.items.map(item => Object.freeze({id: item.id, version: item.version}));
      const fallback = text.trim() ? text : items.length ? "Please review the attached files." : text;
      // RPC prepends the separate Message field. Content contains attachments only,
      // and untrusted filenames must never become explicit skill-activation text.
      const blocks = [];
      for (const item of draft.items) {
        blocks.push({type: "text", text: labelText(item)});
        if (item.kind === "image") blocks.push({type: "image", mime_type: item.mime, data: item.data});
      }
      if (blocks.reduce((sum, block) => sum + (block.type === "text" ? bytes(block.text) : 0), bytes(fallback)) > PROMPT_LIMIT) throw new Error("Prompt and attachment text must total at most 128 KiB, including labels.");
      return Object.freeze({revision: draft.revision, items: Object.freeze(items), content: JSON.stringify(blocks), text: fallback, hasContent: items.length > 0, key, owner});
    }
    function accepted(snapshot) {
      if (!alive() || snapshot?.key !== key || snapshot.owner !== owner) return;
      draft.items = draft.items.filter(item => {
        if (!snapshot.items.some(sent => sent.id === item.id && sent.version === item.version)) return true;
        releasePreview(item); return false;
      });
      localError = ""; notify();
    }
    input.multiple = true; input.removeAttribute("accept"); // Browser text MIME classifications are not authoritative.
    popup.id ||= `composer-context-list-${++sequence}`; popup.setAttribute("role", "listbox"); popup.setAttribute("aria-label", "Files and installed skills");
    prompt.setAttribute("aria-autocomplete", "list"); prompt.setAttribute("aria-controls", popup.id); prompt.setAttribute("aria-expanded", "false");
    window.addEventListener?.("resize", position, options);
    window.addEventListener?.("scroll", position, {...options, capture: true});
    window.visualViewport?.addEventListener("resize", position, options);
    window.visualViewport?.addEventListener("scroll", position, options);
    if (window.ResizeObserver) {
      positionObserver = new ResizeObserver(position);
      positionObserver.observe(find("#live-composer") || root);
    }
    notice.setAttribute("role", "status"); notice.setAttribute("aria-live", "polite");
    contextMenu?.addEventListener("click", openPlusMenu, options);
    attach?.addEventListener("click", () => { if (canAdd()) input.click(); }, options);
    input.addEventListener("change", () => { addFiles(input.files || []); input.value = ""; }, options);
    files?.addEventListener("click", () => insertMarker("@"), options);
    skills?.addEventListener("click", () => insertMarker("$"), options);
    prompt.addEventListener("input", inspectToken, options);
    prompt.addEventListener("compositionstart", () => { composing = true; close(); }, options);
    prompt.addEventListener("compositionend", () => { composing = false; inspectToken(); }, options);
    prompt.addEventListener("click", () => { if (query && !validQuery(query)) close(); }, options);
    prompt.addEventListener("blur", close, options);
    prompt.addEventListener("keyup", () => { if (query && !validQuery(query)) close(); }, options);
    prompt.addEventListener("keydown", event => {
      if (event.isComposing || composing || event.ctrlKey || event.metaKey || event.altKey || popup.hidden) return;
      if (!validQuery(query)) { close(); return; }
      if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); close(); }
      else if (["ArrowDown", "ArrowUp"].includes(event.key) && rows.length) {
        event.preventDefault(); event.stopPropagation(); select((selected + (event.key === "ArrowDown" ? 1 : -1) + rows.length) % rows.length);
      } else if (event.key === "Enter" && rows.length) {
        event.preventDefault(); event.stopPropagation(); if (!rows[selected].disabled) rows[selected].choose();
      } else if (event.key === "Tab") close();
    }, options);
    const composer = find("#live-composer") || root;
    composer.addEventListener("dragover", event => {
      if ([...(event.dataTransfer?.types || [])].includes("Files")) { event.preventDefault(); event.dataTransfer.dropEffect = canAdd() ? "copy" : "none"; }
    }, options);
    composer.addEventListener("drop", event => { if (event.dataTransfer?.files?.length) { event.preventDefault(); addFiles(event.dataTransfer.files); } }, options);
    composer.addEventListener("paste", event => {
      const images = [...(event.clipboardData?.items || [])].filter(item => item.kind === "file" && /^image\//.test(item.type)).slice(0, 8).map(item => item.getAsFile()).filter(Boolean);
      if (images.length) { event.preventDefault(); addFiles(images); }
    }, options);
    function dispose() {
      if (disposed) return;
      close(); closePlusMenu(); listeners.abort(); positionObserver?.disconnect();
      if (alive()) retire(draft);
      disposed = true; input.value = "";
      prompt.removeAttribute("aria-controls"); prompt.removeAttribute("aria-autocomplete"); prompt.removeAttribute("aria-expanded");
    }
    close(); paint();
    active = Object.freeze({render, capture, accepted, hasAttachments, pending, dispose});
    return active;
  }
  window.SnowComposerContext = Object.freeze({
    init, forget,
    render: state => active?.render(state),
    capture: text => { if (!active) throw new Error("Composer context is unavailable."); return active.capture(text); },
    accepted: snapshot => active?.accepted(snapshot),
    hasAttachments: () => active?.hasAttachments() || false,
    pending: () => active?.pending() || false,
    dispose: () => { active?.dispose(); active = null; }
  });
})();
