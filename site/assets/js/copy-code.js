(() => {
  "use strict";

  function copyWithSelection(text) {
    const input = document.createElement("textarea");
    input.value = text;
    input.setAttribute("readonly", "");
    input.style.position = "fixed";
    input.style.opacity = "0";
    document.body.append(input);
    input.select();

    try {
      if (!document.execCommand("copy")) {
        throw new Error("copy command was rejected");
      }
    } finally {
      input.remove();
    }
  }

  async function copyText(text) {
    if (navigator.clipboard && window.isSecureContext) {
      try {
        await navigator.clipboard.writeText(text);
        return;
      } catch {
        // Fall back for browsers that expose but deny the Clipboard API.
      }
    }
    copyWithSelection(text);
  }

  document.querySelectorAll(".prose pre > code").forEach((code) => {
    const block = code.parentElement;
    if (!block || block.querySelector(".copy-code-button")) {
      return;
    }

    const button = document.createElement("button");
    button.type = "button";
    button.className = "copy-code-button";
    button.textContent = "Copy";
    button.setAttribute("aria-label", "Copy code to clipboard");
    button.setAttribute("aria-live", "polite");
    block.classList.add("copy-enabled");
    block.append(button);

    button.addEventListener("click", async () => {
      button.disabled = true;
      try {
        const text = code.textContent.replace(/\n$/, "");
        await copyText(text);
        button.textContent = "Copied";
        button.setAttribute("aria-label", "Code copied to clipboard");
      } catch {
        button.textContent = "Copy failed";
        button.setAttribute("aria-label", "Could not copy code to clipboard");
      } finally {
        button.disabled = false;
        window.setTimeout(() => {
          button.textContent = "Copy";
          button.setAttribute("aria-label", "Copy code to clipboard");
        }, 1600);
      }
    });
  });
})();
