(async () => {
  "use strict";
  if (window.__composerThumbnailsOnly) { await window.testComposerThumbnails(); return; }
  if (window.__composerVisualOnly) { await window.testComposerVisuals(); return; }
  const results = [], failures = [];
  const assert = (condition, name) => (condition ? results : failures).push(name);
  try {
    await contextTest.ready();
    await window.testComposerAttachments(assert);
    await window.testComposerMentions(assert);
    await window.testComposerRaces(assert);
    await window.testComposerRecovery(assert);
    await window.testComposerLimits(assert);
    const {$, visible} = contextTest;
    const composer = $("#live-composer").getBoundingClientRect();
    assert(composer.left >= -1 && composer.right <= innerWidth + 1 && composer.bottom <= innerHeight + 1, "composer remains bounded in requested viewport");
    assert(document.documentElement.scrollWidth <= innerWidth + 1, "attachments and mentions never create horizontal page overflow");
    assert(["attach", "context-menu"].every(name => {
      const button = $(`[data-composer-${name}]`), rect = button.getBoundingClientRect();
      return visible(button) && rect.left >= 0 && rect.right <= innerWidth && rect.top >= 0 && rect.bottom <= innerHeight;
    }), "all context affordances remain reachable at narrow widths/heights");
    assert(fixture.errors.length === 0, "no uncaught browser errors or promise rejections");
    assert(!fixture.storageWrites.some(key => /draft|prompt|attachment|composer|context/i.test(key)), "context and prompt drafts never enter persistent browser storage");
  } catch (error) { failures.push(error.stack || String(error)); }
  document.querySelector("#test-result").textContent = JSON.stringify({passed: results.length, failures, results});
})();
