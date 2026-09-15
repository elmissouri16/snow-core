/* Independent decoded-image schedule. No fabricated load events, DOM styling,
 * image bypasses, or changes to the 100-assertion consent/functional workflow. */
window.testComposerThumbnails = async () => {
  "use strict";
  const {$, tick, wait, posts, latest, visible, draft, chips, clear, upload, image, png, reading, swap, update, send, accept} = contextTest;
  const results = [], failures = [], measurements = {};
  const check = (condition, name) => (condition ? results : failures).push(name);
  const rect = node => node.getBoundingClientRect();
  const near = (value, expected) => Math.abs(value - expected) <= 1;
  const preview = () => $(".composer-context-thumbnail img");
  const created = [], revoked = [];
  const create = URL.createObjectURL, revoke = URL.revokeObjectURL;
  URL.createObjectURL = function(blob) { const url = create.call(this, blob); created.push(url); return url; };
  URL.revokeObjectURL = function(url) { revoked.push(url); return revoke.call(this, url); };
  const decoded = async (selector, label) => {
    await wait(() => { const img = $(selector); return img && img.complete && img.naturalWidth > 0 && !img.hidden; }, label);
    const img = $(selector); await img.decode(); return img;
  };
  const snapshot = async name => {
    await document.fonts.ready;
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    window.composerScreenshot = name;
    await wait(() => window.composerScreenshot === null, "CDP screenshot " + name);
  };
  try {
    await contextTest.ready(); await document.fonts.ready; await clear(); await tick(150);
    check(posts().length === 0, "Passive thumbnail page performs no admission or discovery POST");
    upload([image("workspace-checker.png")]); await tick(150);
    const first = await decoded(".composer-context-thumbnail img", "decoded draft PNG");
    check(first.naturalWidth === 48 && first.naturalHeight === 32 && first.src.startsWith("blob:"), "Draft PNG is actually decoded from a local blob (48x32 source), not a fake load event");
    const thumbnail = rect(first.closest(".composer-context-thumbnail"));
    check(near(thumbnail.width, 28) && near(thumbnail.height, 28) && rect(first).width <= 28 && rect(first).height <= 28, "Draft thumbnail paints inside a compact 28x28 rectangle");
    const label = $(".composer-context-label"), notice = $("[data-composer-context-status]");
    check(near(parseFloat(getComputedStyle(label).fontSize), 11) && label.title === "workspace-checker.png", "Attachment label uses smaller 11px text and retains its full filename title");
    check(notice.textContent === "On Send: shared with provider and saved in chat. Images need a vision-capable model.", "Visible image disclosure stays short without removing provider, saved-history or vision notice");
    check(/Text and image contents.*provider.*persisted in saved conversation history.*when you send/i.test(notice.title) && /vision-capable model/i.test(notice.title), "Disclosure keeps the complete privacy and vision explanation in its native title");
    check(near(parseFloat(getComputedStyle(notice).fontSize), 10) && near(parseFloat(getComputedStyle(notice).lineHeight), 14), "Disclosure uses compact 10px text with a legible 14px line height");
    check(near(parseFloat(getComputedStyle($("#live-composer")).gap), 6), "Attached composer reduces its inter-row gap to 6px");
    check(visible(notice) && rect(notice).height >= 14 && rect(notice).bottom <= innerHeight && rect(first).top >= 0 && document.documentElement.scrollWidth <= innerWidth + 1, "Thumbnail and disclosure remain in the viewport without horizontal overflow, including short seats");
    measurements.draft = {composer: rect($("#live-composer")).toJSON(), thumbnail: thumbnail.toJSON(), label: {rect: rect(label).toJSON(), font: getComputedStyle(label).fontSize}, notice: {rect: rect(notice).toJSON(), font: getComputedStyle(notice).fontSize, lineHeight: getComputedStyle(notice).lineHeight, text: notice.textContent, title: notice.title}};
    await snapshot("draft-image");
    const firstURL = first.src; update({revision: fixture.snapshot.revision + 1}); await tick(150);
    check(preview()?.src === firstURL && !revoked.includes(firstURL), "Ordinary snapshot repaint reuses the active draft object URL");
    chips()[0].click();
    check(!preview() && revoked.includes(firstURL), "Removing a draft attachment revokes its local object URL");
    check(posts().length === 0, "Draft image selection, rendering and removal never send image bytes");

    upload([image()]); const outgoing = await decoded(".composer-context-thumbnail img", "draft before remount"); const outgoingURL = outgoing.src;
    await swap(); const remounted = await decoded(".composer-context-thumbnail img", "remounted draft");
    check(revoked.includes(outgoingURL), "Workspace disposal revokes the outgoing draft preview URL");
    check(remounted.src !== outgoingURL && remounted.naturalWidth === 48, "Remount regenerates and decodes a fresh preview from retained tab-only draft bytes");
    const remountedURL = remounted.src;
    await swap({session_id: "session-two"});
    check(!preview() && chips().length === 0 && revoked.includes(remountedURL), "Switching sessions disposes the preview and never leaks attachments into another draft");
    await swap({session_id: "session-one"});
    const restored = await decoded(".composer-context-thumbnail img", "restored original draft");
    check(restored.src !== remountedURL && restored.naturalWidth === 48, "Returning to the original session restores and decodes its own image");
    await clear();

    // Signature-valid but undecodable raster: browser error path, not a manual
    // error/load dispatch. Admission remains separate from preview availability.
    upload([new File([Uint8Array.from(atob(png).slice(0, 24), char => char.charCodeAt(0))], "broken-preview.png", {type: "image/png"})]);
    await wait(() => chips().length === 1 && !reading() && created.length > 0 && revoked.includes(created.at(-1)), "decoder failure cleanup");
    check(visible($(".composer-context-label")) && $(".composer-context-label").textContent === "broken-preview.png" && !visible($(".composer-context-thumbnail")), "Real decoder failure leaves a usable filename-only attachment fallback");
    check(!$(".composer-context-chip.is-error") && !$("#live-send").disabled && posts().length === 0, "Preview failure alone does not silently discard the draft or authorize any upload");
    await clear();

    upload([image()]); const pending = await decoded(".composer-context-thumbnail img", "image for explicit send"); const pendingURL = pending.src;
    draft("Please inspect this image."); send(); await wait(() => !!latest("prompt-content"), "explicit rich admission");
    check(JSON.parse(latest("prompt-content").fields.content).some(block => block.type === "image" && block.data === png) && !revoked.includes(pendingURL) && chips().length === 1, "Only explicit Send transports exact PNG bytes and pending admission retains its preview");
    await accept("prompt-content");
    check(chips().length === 0 && revoked.includes(pendingURL), "Accepted admission clears the captured draft and revokes its URL");

    const sentURL = "/projects/00000000-0000-4000-8000-000000000002/runtime/images/thumbnail-user/1?instance_id=instance-one&session_id=session-one";
    const message = {id: "thumbnail-user", role: "user", text: "Please inspect this image.", html: "<p>Please inspect this image.</p>", truncated: false, can_edit: false, images: [{index: 1, mime_type: "image/png", url: sentURL}]};
    update({messages: [message]});
    const sent = await decoded(".message-image-preview", "authenticated HTTP sent PNG");
    check(sent.naturalWidth === 48 && sent.naturalHeight === 32 && $(".message-image")?.dataset.imageState === "loaded", "Server-shaped message image metadata loads and actually decodes its HTTP PNG");
    check(sent.src.startsWith("blob:") && new URL(sent.src).origin === location.origin && created.includes(sent.src), "Sent preview decodes an owner-created Blob after the exact authenticated manager image GET, never caller-supplied blob metadata");
    // In short viewports the transcript is scrollable; choose the actual image
    // as the reading anchor before measuring/capturing it.
    sent.scrollIntoView({block: "start", inline: "nearest"}); await tick(100);
    const sentRect = rect(sent);
    check(sentRect.width > 0 && sentRect.width <= 96 && sentRect.height > 0 && sentRect.height <= 96 && sentRect.left >= 0 && sentRect.right <= innerWidth && sentRect.top >= 0 && sentRect.bottom <= innerHeight && document.documentElement.scrollWidth <= innerWidth + 1, "Sent image paints a bounded thumbnail without narrow-screen horizontal overflow");
    check($("[data-message-id='thumbnail-user'] .message-body")?.textContent.includes(message.text), "Sent image preserves its accompanying prompt text");
    check(!JSON.stringify(fixture.snapshot).includes(png) && Object.keys(message.images[0]).sort().join(",") === "index,mime_type,url", "Public message snapshot carries only bounded image metadata, not base64 image bytes");
    check([...document.querySelectorAll("[data-message-id='thumbnail-user'] [data-message-edit],[data-message-id='thumbnail-user'] [data-message-reuse]")].every(button => button.disabled || button.hidden), "Image messages cannot enable text-only Edit or Reuse actions");
    measurements.sent = {image: sentRect.toJSON(), natural: {width: sent.naturalWidth, height: sent.naturalHeight}, message: rect($("[data-message-id='thumbnail-user']")).toJSON(), url: sentURL};
    await snapshot("sent-image");
    await swap(); const replayed = await decoded(".message-image-preview", "sent image after workspace remount");
    check(replayed.naturalWidth === 48 && replayed.src.startsWith("blob:") && created.includes(replayed.src) && revoked.includes(sent.src) && posts("prompt-content").length === 1, "Fresh snapshot/remount reloads sent image metadata, revokes the old preview and never replays the accepted prompt");
    update({messages: [{...message, images: [{index: 1, mime_type: "image/png", url: "blob:forbidden-sent-image"}]}]});
    await tick(180);
    check($(".message-image")?.dataset.imageState === "unavailable" && !$(".message-image-preview").getAttribute("src") && visible($(".message-image-fallback")) && $(".message-image-fallback").textContent === "Image unavailable", "Forbidden blob sent-image metadata is rejected with a visible unavailable fallback");
    check(created.every(url => revoked.includes(url)), "All created draft object URLs are released after remove, dispose, decoder failure and acceptance");
    check(fixture.errors.length === 0, "Thumbnail lifecycle raises no uncaught browser errors or rejected promises");
    check(!fixture.storageWrites.some(key => /draft|prompt|attachment|composer|context/i.test(key)), "Thumbnail rendering never persists draft bytes or text in browser storage");
    check(posts("prompt-content").length === 1 && posts().length === 1, "Image display, remount and fallback do not create extra mutations beyond the one explicit Send");
  } catch (error) { failures.push(error.stack || String(error)); }
  finally { URL.createObjectURL = create; URL.revokeObjectURL = revoke; }
  $("#test-result").textContent = JSON.stringify({passed: results.length, failures, results, measurements, thumbnails: true});
};
