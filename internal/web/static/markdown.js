(() => {
  "use strict";
  function enhance(scope) {
    for (const pre of scope.querySelectorAll(".markdown-body pre")) {
      if (pre.parentElement.classList.contains("code-block")) continue;
      const block = document.createElement("div"), button = document.createElement("button");
      block.className = "code-block";
      button.className = "quiet copy-code"; button.type = "button";
      button.textContent = "Copy code"; button.setAttribute("aria-label", "Copy code to clipboard");
      pre.before(block); block.append(button, pre);
      button.addEventListener("click", async () => {
        try {
          await navigator.clipboard.writeText(pre.textContent);
          button.textContent = "Copied";
        } catch (_) { button.textContent = "Select and copy manually"; }
        setTimeout(() => { if (button.isConnected) button.textContent = "Copy code"; }, 2500);
      });
    }
  }
  function render(container, text, sanitizedHTML) {
    container.classList.add("markdown-body");
    // HTML comes exclusively from Snow's bounded goldmark + explicit sanitizer
    // projection. Never pass provider HTML, tool output, file text or raw Markdown
    // as this third argument. Missing presentation data falls back to plain text.
    if (typeof sanitizedHTML === "string" && sanitizedHTML) container.innerHTML = sanitizedHTML;
    else {
      const pre = document.createElement("pre"); pre.textContent = text;
      container.replaceChildren(pre);
    }
    enhance(container.parentElement || container);
  }
  window.SnowMarkdown = Object.freeze({render, enhance});
  document.addEventListener("DOMContentLoaded", () => enhance(document));
  document.addEventListener("htmx:afterSwap", event => enhance(event.detail.target || document));
})();
