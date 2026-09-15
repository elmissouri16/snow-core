/* Rendered compactness checks run on a fresh production page, independently of
 * the unchanged 100-assertion functional workflow. Screenshots use a CDP
 * handshake; this file never styles or rebuilds product markup. */
window.testComposerVisuals = async () => {
  "use strict";
  const { $, tick, wait, posts, latest, visible, draft, key, clear, rows, files, skills, openContext, closeContext, upload, update } = contextTest;
  const results = [], failures = [], measurements = {};
  const check = (condition, name) => (condition ? results : failures).push(name);
  const box = node => node.getBoundingClientRect();
  const near = (a, b) => Math.abs(a - b) <= 2;
  const singleLine = node => !!node && box(node).height - parseFloat(getComputedStyle(node).paddingTop) - parseFloat(getComputedStyle(node).paddingBottom) <= parseFloat(getComputedStyle(node).lineHeight) + 1;
  const painted = node => visible(node) && box(node).height > 1 && box(node).width > 1;
  const snapshot = async name => {
    await document.fonts.ready;
    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    window.composerScreenshot = name;
    await wait(() => window.composerScreenshot === null, "CDP screenshot " + name);
  };
  const popupBounds = name => {
    const composer = box($("#live-composer")), popup = box($("[data-composer-mentions]"));
    measurements[name] = {composer: composer.toJSON(), popup: popup.toJSON()};
    check(near(popup.width, Math.min(composer.width, innerWidth - 16)) && near(popup.left, Math.max(8, Math.min(composer.left, innerWidth - popup.width - 8))), name + " popup matches composer width and alignment except viewport clamping");
    check(popup.top >= 0 && popup.bottom <= innerHeight + 1 && popup.left >= 0 && popup.right <= innerWidth + 1 && popup.height >= 40, name + " popup fits viewport, including 240px height, with a usable choice area");
    check($("[data-composer-mentions]").scrollWidth <= $("[data-composer-mentions]").clientWidth + 1 && document.documentElement.scrollWidth <= innerWidth + 1, name + " listing has no popup or page horizontal overflow");
  };
  try {
    await contextTest.ready(); await document.fonts.ready; await tick(100);
    await clear();
    const composer = $("#live-composer"), actions = composer.querySelectorAll(".composer-actions");
    measurements.empty = box(composer).toJSON();
    measurements.emptyChildren = [...composer.children].filter(visible).map(node => ({tag: node.tagName, id: node.id, className: node.className, rect: box(node).toJSON(), gap: getComputedStyle(node).gap, flex: getComputedStyle(node).flex}));
    measurements.actionChildren = [...actions[0].children].map(node => ({className: node.className, rect: box(node).toJSON(), children: [...node.children].map(child => ({className: child.className, rect: box(child).toJSON()}))}));
    check(innerHeight <= 420 ? box(composer).height <= 106 : box(composer).height >= 90 && box(composer).height <= 106, "Idle composer is approximately 98px, not the former 138px card (short seats may shrink)");
    check(actions.length === 1 && actions[0].contains($("[data-composer-context-menu]")) && actions[0].contains($("[data-composer-attach]")) && actions[0].contains($("#live-send")), "Exactly one action row owns plus, paperclip and Send");
    check(composer.querySelectorAll(".composer-context-tools").length === 1 && actions[0]?.contains(composer.querySelector(".composer-context-tools")), "Context toolbar is a child of the single action row, not a separate row");
    check(!$("[data-composer-files]") && !$("[data-composer-skills]") && !painted($(".composer-context-hint")), "Idle composer has no standalone at/dollar controls or helper copy");
    check(!painted($("#composer-hint")) && !painted($("#composer-state")), "Default ready footer and keyboard hint are visually hidden");
    check(document.documentElement.scrollWidth <= innerWidth + 1 && box(composer).left >= 0 && box(composer).right <= innerWidth + 1, "Empty composer fits even the 320px viewport without horizontal overflow");
    check(posts().length === 0, "Visual idle fixture has no passive discovery or runtime mutations");
    await snapshot("empty");

    await openContext();
    check(["files", "skills"].every(name => $("[data-composer-" + name + "]")?.getAttribute("role") === "menuitem" && painted($("[data-composer-" + name + "]"))), "Plus exposes both context actions as visible dynamic menu rows");
    check(posts().length === 0, "Opening plus does not discover files or skills");
    await closeContext();
    draft("@"); await wait(() => !!latest("files"), "visual files query");
    latest("files").resolve(files(".", [
      {name: "src", path: "src", kind: "directory"},
      ...["README.md", "composer-context.js", "a-long-project-filename-that-must-not-widen-the-popup.test.js", "package.json", "styles.css", "CHANGELOG.md", "notes.txt"].map(name => ({name, path: name, kind: "file"}))
    ]));
    await wait(() => rows().length === 8, "visual file listing"); await tick(80);
    popupBounds("files");
    check(rows().every(row => painted(row.querySelector(".composer-mention-icon")) && painted(row.querySelector(".composer-mention-name")) && (!row.querySelector('.composer-mention-icon[data-kind="folder"]') || painted(row.querySelector(".composer-mention-chevron")) && box(row.querySelector(".composer-mention-chevron")).left > box(row.querySelector(".composer-mention-name")).right)), "Every file/folder row has a leading icon/name; navigable folders have a trailing chevron");
    check(rows().every(row => box(row).height <= 60), "File rows remain compact rather than card-sized");
    const heading = $(".composer-mention-heading");
    check(painted(heading) && heading.textContent.length <= 40 && singleLine(heading), "Normal file header is a short single line, not instructions");
    await snapshot("files");
    key("Escape"); await clear();

    const description = "Review the implementation for correctness, security, maintainability and careful handling of asynchronous state. ".repeat(16).trim();
    draft("$"); await wait(() => !!latest("skills"), "visual skill query");
    latest("skills").resolve(skills({skills: Array.from({length: 20}, (_, index) => ({name: `${index < 14 ? "review" : "build"}-long-installed-skill-${String(index + 1).padStart(2, "0")}`, description, enabled: true, disabled_by: ""}))}));
    await wait(() => rows().length === 20, "twenty long-description skills"); await tick(80);
    popupBounds("skills");
    measurements.skills.rows = rows().map(row => ({height: box(row).height, descriptionHeight: box(row.querySelector(".composer-mention-description")).height}));
    check(rows().every(row => box(row).height <= 60), "All twenty long-description skill rows stay at or below 60px");
    check(rows().every(row => { const css = getComputedStyle(row.querySelector(".composer-mention-name")); return css.fontSize === "14px" && parseFloat(css.lineHeight) <= 22; }), "Skill names retain compact readable 14px typography");
    check(rows().every(row => { const detail = row.querySelector(".composer-mention-description"), css = getComputedStyle(detail); return box(detail).height <= parseFloat(css.lineHeight) + 1 && css.whiteSpace === "nowrap" && ["hidden", "clip"].includes(css.overflowX) && detail.scrollWidth > detail.clientWidth; }), "Long skill descriptions are one truncated line, never expanded paragraphs");
    check(rows().every(row => row.querySelector(".composer-mention-description").title === description && row.title.includes(description)), "Complete skill paragraphs remain available through native titles");
    check($("[data-composer-mentions]").scrollHeight > $("[data-composer-mentions]").clientHeight, "Many skills scroll inside the bounded popup");
    check(painted($(".composer-mention-heading")) && $(".composer-mention-heading").textContent.length <= 40 && singleLine($(".composer-mention-heading")), "Normal skills header stays a short single line");
    await snapshot("skills-long");
    draft("$review"); await wait(() => rows().length === 14, "fourteen filtered long skills");
    check(rows().every(row => box(row).height <= 60 && singleLine(row.querySelector(".composer-mention-description"))), "Fourteen matching long skills remain compact single-summary rows");
    check(posts("skills").length === 1, "Filtering the twenty-skill inventory to fourteen remains local");
    key("Escape"); await clear();

    upload([new File(["%PDF-1.7"], "unsupported.pdf", {type: "application/pdf"})]);
    await wait(() => !!$(".composer-context-chip.is-error"), "visible context error");
    check(painted($(".composer-context-chip.is-error")) && painted($(".composer-context-detail")), "Actionable attachment error remains visually present");
    await clear(); update({status: "running"});
    await wait(() => /progress|Turn/.test($("#composer-state").textContent), "non-ready composer state");
    check(painted($("#composer-state")), "Non-ready actionable composer status remains visually present");
    check(posts("file").length === 0 && posts("prompt").length === 0 && posts("prompt-content").length === 0 && fixture.errors.length === 0, "Visual listing checks never read file bodies, submit prompts or raise browser errors");
  } catch (error) { failures.push(error.stack || String(error)); }
  $("#test-result").textContent = JSON.stringify({passed: results.length, failures, results, measurements, visual: true});
};
