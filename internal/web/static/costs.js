(() => {
  "use strict";
  // Only worker-projected Usage.Cost is accepted. Never multiply tokens by
  // frontend model prices or imply this known subtotal is a billing charge.
  function presentation(telemetry) {
    const cost = telemetry?.cost;
    if (!telemetry?.available || cost?.known !== true || !/^[A-Z]{3}$/.test(cost.currency) || !Number.isFinite(cost.total) || cost.total < 0) {
      return {known: false, value: "Unknown · cost not available"};
    }
    const total = cost.total;
    // Avoid rendering a tiny positive estimate as a verified zero. Scientific
    // notation also bounds the width of very large finite provider values.
    const amount = total === 0 ? "0.00" : total < 0.000001 || total >= 1e9 ? total.toExponential(3) : total.toLocaleString(undefined, {minimumFractionDigits: 2, maximumFractionDigits: 6});
    return {known: true, value: `${cost.currency} ${amount}`};
  }
  function render(container, telemetry) {
    const result = presentation(telemetry), pair = document.createElement("div"), label = document.createElement("dt"), value = document.createElement("dd");
    pair.className = "session-cost";
    label.textContent = "Recorded cost estimate";
    value.dataset.workflowCost = "";
    value.dataset.known = String(result.known);
    value.textContent = result.value;
    const note = document.createElement("p");
    note.className = "session-cost-note";
    note.textContent = "May exclude unpriced requests; aggregate currency coverage is not verified. An estimate, not a billing charge. Unknown is not zero.";
    pair.append(label, value, note);
    container.append(pair);
  }
  window.SnowCosts = Object.freeze({render, presentation});
})();
