// Focused presentation checks. Evaluate after harness-layout/fixture.js and the
// production page settle, on an idle populated conversation (chat or stream).
// The caller owns viewport/theme selection and restores a fresh page per run.
(async () => {
  "use strict";
  const results = [], failures = [], measurements = {};
  const check = (condition, label) => (condition ? results : failures).push(label);
  const near = (a, b, tolerance = 2) => Math.abs(a - b) <= tolerance;
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  const $ = selector => document.querySelector(selector);
  const box = node => node.getBoundingClientRect();
  const fixture = window.harnessFixture, region = $("#live-session");
  const prompt = $("#live-prompt"), seat = $("#live-composer-seat"), stream = $("#live-stream");
  if (!fixture?.snapshot || !window.SnowScroll || !prompt?.getClientRects().length) {
    throw new Error("Composer tests require a settled, idle production conversation fixture");
  }
  const originalValue = prompt.value, originalMessages = structuredClone(fixture.snapshot.messages);
  const update = async fields => {
    fixture.update(fields);
    for (let i = 0; i < 100; i++) {
      if (fixture.deliveredRevision === fixture.snapshot.revision) { await wait(70); return; }
      await wait(30);
    }
    throw new Error("Composer fixture update did not settle");
  };
  const input = value => { prompt.value = value; prompt.dispatchEvent(new Event("input", {bubbles: true})); };
  // Composer-owned chrome participates in the computed CSS cap. Keep the
  // short-seat cap authoritative rather than freezing a former toolbar budget.
  const cap = () => Math.min(336, parseFloat(getComputedStyle(prompt).maxHeight));
  const key = region.dataset.project + ":" + region.dataset.session;
  const initialRequests = fixture.requests.length;
  try {
    await document.fonts.ready;
    input(""); await wait(60);
    const empty = box(prompt).height;
    measurements.empty = empty;
    const composer = $("#live-composer"), actions = composer.querySelectorAll(".composer-actions");
    measurements.emptyComposer = box(composer).height;
    check(innerHeight <= 420 ? box(composer).height <= 106 : box(composer).height >= 90 && box(composer).height <= 106, "Idle card stays approximately 98px, never the former 138px two-row composer");
    check(actions.length === 1 && actions[0].contains(composer.querySelector(".composer-context-tools")) && actions[0].contains($("[data-composer-context-menu]")) && actions[0].contains($("[data-composer-attach]")), "One action row contains plus and paperclip context tools");
    check(!composer.querySelector("[data-composer-files],[data-composer-skills]") && document.documentElement.scrollWidth <= innerWidth + 1, "No standalone mention buttons or horizontal viewport overflow");
    check(near(empty, innerHeight <= 420 ? Math.min(28, cap()) : 36), "Empty draft retains normal 36px / bounded short-seat floor");
    check(getComputedStyle(stream).paddingTop === "16px" && getComputedStyle(stream).paddingBottom === "16px", "Populated transcript has 16px vertical padding at every width");

    prompt.focus({preventScroll: true});
    input("First line\nSecond line"); prompt.setSelectionRange(2, 8, "backward"); await wait(60);
    measurements.multiline = box(prompt).height;
    check(box(prompt).height >= Math.min(52, cap()) - 1, "Two-line draft grows to at least 52px unless the short seat caps it");
    check(prompt.value === "First line\nSecond line" && prompt.selectionStart === 2 && prompt.selectionEnd === 8 && prompt.selectionDirection === "backward" && document.activeElement === prompt, "Autogrow preserves the existing textarea value, focus and selection");

    input("Long draft line\n".repeat(100)); await wait(60);
    measurements.capped = box(prompt).height;
    check(box(prompt).height <= cap() + 1 && box(prompt).height <= 336 && prompt.scrollHeight > prompt.clientHeight, "Huge draft scrolls inside the 336px / measured short-seat cap");
    check(box(seat).height <= parseFloat(getComputedStyle(seat).getPropertyValue("--scroll-seat-limit")) + 1, "Huge draft never grows the composer seat beyond its measured limit");
    prompt.setSelectionRange(4, 10); prompt.scrollTop = 120;
    const priorTop = prompt.scrollTop;
    window.SnowScroll.beforeUpdate(); window.SnowScroll.afterUpdate(); await wait(60);
    check(near(prompt.scrollTop, priorTop) && prompt.selectionStart === 4 && prompt.selectionEnd === 10 && document.activeElement === prompt, "Unrelated reconciliations preserve draft scrollTop, caret and focus");

    // Establish deliberate reader ownership before changing composer geometry.
    const floor = () => Math.max(0, stream.scrollHeight - stream.clientHeight);
    if (floor() > 150) {
      stream.scrollTop = floor() / 2; stream.dispatchEvent(new Event("scroll")); await wait(30);
      const view = box(stream), anchor = [...stream.querySelectorAll("[data-message-id]")].find(row => box(row).bottom > view.top && box(row).top < view.bottom);
      if (anchor) {
        const offset = box(anchor).top - box(stream).top;
        input("Small draft"); await wait(60);
        check(region.dataset.scrollFollowing === "false" && near(box(anchor).top - box(stream).top, offset), "Composer shrink preserves the reader's message offset and does not rejoin the tail");
        input("Long draft line\n".repeat(100)); await wait(60);
        check(region.dataset.scrollFollowing === "false" && near(box(anchor).top - box(stream).top, offset), "Composer growth preserves the reader anchor without a tail snap");
      }
    }
    input("Short"); await wait(60);
    check(near(box(prompt).height, empty), "Deleting multiline content shrinks the same textarea to its empty floor");
    input("One\nTwo\nThree"); await wait(60);
    window.SnowScroll.beforeUpdate(); prompt.value = ""; window.SnowScroll.afterUpdate(); await wait(60);
    check(near(box(prompt).height, empty), "Programmatic accepted-send clear shrinks via afterUpdate without a synthetic input event");

    window.SnowScroll.dispose();
    prompt.value = "Restored first line\nRestored second line\nRestored third line";
    prompt.style.removeProperty("height");
    window.SnowScroll.init(region, key); await wait(60);
    check($("#live-prompt") === prompt && box(prompt).height >= Math.min(76, cap()) - 1, "Mount measures a restored multiline draft without replacing the textarea");
    const draft = prompt.value, stack = seat.querySelector(".composer-stack");
    window.SnowScroll.beforeUpdate(); stack.hidden = true; window.SnowScroll.afterUpdate();
    await wait(40);
    window.SnowScroll.beforeUpdate(); stack.hidden = false; window.SnowScroll.afterUpdate(); await wait(60);
    check(prompt.value === draft && box(prompt).height >= Math.min(76, cap()) - 1, "Restoring the normal card after a hidden attention seat remeasures its retained draft");
    window.dispatchEvent(new Event("resize")); await wait(60);
    check(prompt.value === draft && box(prompt).height <= cap() + 1, "Resize reconciliation keeps the restored draft within its measured cap");

    await update({messages: [
      {id: "composer-old-user", role: "user", text: "Earlier user"},
      {id: "composer-old-assistant", role: "assistant", text: "Earlier answer"},
      {id: "composer-latest-user", role: "user", text: "Latest user"},
      {id: "composer-latest-assistant", role: "assistant", text: "Latest answer"}
    ]});
    prompt.focus({preventScroll: true}); await wait(100);
    const row = id => document.querySelector(`[data-message-id="composer-${id}"]`);
    const opacity = id => Number(getComputedStyle(row(id).querySelector(".message-actions")).opacity);
    check(opacity("latest-user") === 1 && opacity("latest-assistant") === 1, "Latest user action row stays visible when an assistant follows, alongside latest assistant actions");
    if (matchMedia("(hover: hover)").matches) {
      check(opacity("old-user") === 0 && opacity("old-assistant") === 0, "Earlier user and assistant actions hide at rest on hover-capable devices");
      row("old-user").querySelector(".message-copy").focus({preventScroll: true}); await wait(100);
      check(opacity("old-user") === 1, "Keyboard focus reveals older user actions despite role-specific recency");
    } else check(opacity("old-user") === 1 && opacity("old-assistant") === 1, "Touch/non-hover devices keep older message actions discoverable");
    check(!fixture.requests.slice(initialRequests).some(request => request.method === "POST" && !request.path.endsWith("/runtime/choices")), "Presentation checks never submit a prompt or runtime mutation");
  } catch (error) { failures.push(error.stack || String(error)); }
  finally {
    const stack = seat.querySelector(".composer-stack"); if (stack) stack.hidden = false;
    window.SnowScroll.beforeUpdate(); prompt.value = originalValue; window.SnowScroll.afterUpdate();
    await update({messages: originalMessages});
  }
  return {results, failures, measurements};
})();
