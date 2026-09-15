(async () => {
  "use strict";
  const results = [], failures = [], requests = [];
  const $ = selector => document.querySelector(selector);
  const tick = () => new Promise(resolve => setTimeout(resolve, 20));
  const assert = (condition, name) => (condition ? results : failures).push(name);

  // Deliberately allow responses after abort. Publication must remain safe even
  // when cancellation loses a race with an already-delivered network response.
  window.fetch = (url, options) => new Promise(resolve => requests.push({
    url, options,
    resolve: data => resolve({ok: true, json: async () => data})
  }));
  const pending = action => requests.filter(request => request.url.endsWith("/" + action));
  const changes = path => ({
    available: true,
    changes: [{path, kind: "unstaged", status: "Modified"}],
    limited: false
  });
  const diff = path => ({available: true, path, kind: "unstaged", text: "diff " + path, truncated: false});
  const refresh = () => $('[data-inspection-refresh="changes"]').click();
  const activate = button => {
    button.click();
    // Exercise the handler guard too, not just native disabled-button behavior.
    button.dispatchEvent(new MouseEvent("click", {bubbles: true}));
  };

  try {
    SnowInspection.init();
    $("#inspection-tab-changes").click();
    pending("changes").at(-1).resolve(changes("old.txt"));
    await tick();

    let old = $("[data-inspection-change]");
    old.click();
    const firstDiff = pending("diff").at(-1);
    refresh();
    assert(firstDiff.options.signal.aborted, "refresh aborts preceding diff");
    const count = pending("diff").length;
    activate(old);
    const duringRefresh = pending("diff").slice(count);
    assert(!duringRefresh.length, "refresh blocks activation of old row");
    for (const request of duringRefresh) request.resolve(diff("stale-pending.txt"));
    firstDiff.resolve(diff("stale-prior.txt"));
    await tick();
    assert($("[data-diff-preview]").hidden, "late diff cannot publish during refresh");
    pending("changes").at(-1).resolve(changes("new.txt"));
    await tick();
    assert($("[data-inspection-change]").dataset.inspectionChange === "new.txt", "refresh publishes replacement list");
    assert($("[data-diff-preview]").hidden, "refresh completion keeps stale preview hidden");

    // Original failure: refresh -> old-row click -> list -> late old diff.
    old = $("[data-inspection-change]");
    refresh();
    const afterCount = pending("diff").length;
    activate(old);
    const late = pending("diff").slice(afterCount);
    pending("changes").at(-1).resolve(changes("latest.txt"));
    await tick();
    for (const request of late) request.resolve(diff("stale-completed.txt"));
    await tick();
    assert($("[data-diff-preview]").hidden && !$("[data-inspection-change][aria-current]"), "late old diff cannot publish after completed refresh");

    const current = $("[data-inspection-change]");
    current.click();
    pending("diff").at(-1).resolve(diff("latest.txt"));
    await tick();
    assert(!$("[data-diff-preview]").hidden && $("[data-diff-content]").textContent === "diff latest.txt", "current generation diff still publishes");
    assert(current.getAttribute("aria-current") === "true", "current selection marked");
    $("#inspection-tab-project").click();
    $("#inspection-tab-changes").click();
    assert($("[data-diff-preview]").hidden && !current.hasAttribute("aria-current"), "tab leave resets completed preview and selection");
    current.click();
    pending("diff").at(-1).resolve(diff("latest.txt"));
    await tick();
    refresh();
    assert(!current.hasAttribute("aria-current") && $("[data-diff-preview]").hidden, "refresh clears preview and selected row");

    // An older list completion must not clear a newer generation's pending flag.
    const priorList = pending("changes").at(-1);
    refresh();
    const latestList = pending("changes").at(-1);
    priorList.resolve(changes("wrong-generation.txt"));
    await tick();
    const beforeClick = pending("diff").length;
    current.dispatchEvent(new MouseEvent("click", {bubbles: true}));
    assert($("[data-changes-list]").getAttribute("aria-busy") === "true" && pending("diff").length === beforeClick, "older list cannot release new generation pending guard");

    latestList.resolve(changes("tab.txt"));
    await tick();
    $("[data-inspection-change]").click();
    const tabDiff = pending("diff").at(-1);
    $("#inspection-tab-project").click();
    tabDiff.resolve(diff("stale-tab.txt"));
    await tick();
    $("#inspection-tab-changes").click();
    assert($("[data-diff-preview]").hidden && !$("[data-inspection-change][aria-current]"), "tab leave prevents late diff and clears selection");
    assert(tabDiff.options.signal.aborted, "tab leave aborts in-flight diff");
    $("[data-inspection-change]").click();
    pending("diff").at(-1).resolve(diff("tab.txt"));
    await tick();
    assert(!$("[data-diff-preview]").hidden, "fresh diff works after tab reentry");
    await window.runInspectionPolish({assert, tick, requests, pending});
  } catch (error) {
    failures.push(error.stack || String(error));
  } finally {
    window.SnowInspection?.dispose();
  }
  $("#test-result").textContent = JSON.stringify({passed: results.length, failures, results});
})();
