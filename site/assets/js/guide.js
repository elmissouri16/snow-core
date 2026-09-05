(() => {
  "use strict";

  const outline = document.querySelector(".page-outline");
  if (!outline) return;
  const headings = [...document.querySelectorAll(".prose h2[id]")]
    .filter((heading) => heading.id !== "on-this-page");
  if (headings.length < 2) return;

  const details = document.createElement("details");
  const wide = window.matchMedia("(min-width: 76.01rem)");
  details.open = wide.matches;
  wide.addEventListener("change", (event) => { details.open = event.matches; });
  const label = document.createElement("summary");
  label.textContent = "On this page";
  const nav = document.createElement("nav");
  nav.setAttribute("aria-label", "Page sections");
  const links = headings.map((heading) => {
    const link = document.createElement("a");
    link.href = `#${heading.id}`;
    link.textContent = heading.textContent;
    nav.append(link);
    return link;
  });
  details.append(label, nav);
  outline.append(details);
  outline.hidden = false;

  // Keep the Markdown contents list for GitHub and browsers without JavaScript.
  const contents = document.querySelector(".prose #on-this-page");
  if (contents && contents.nextElementSibling?.tagName === "UL") {
    contents.hidden = true;
    contents.nextElementSibling.hidden = true;
  }

  if (!("IntersectionObserver" in window)) return;
  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      links.forEach((link) => {
        if (link.hash === `#${entry.target.id}`) {
          link.setAttribute("aria-current", "location");
        } else {
          link.removeAttribute("aria-current");
        }
      });
    }
  }, { rootMargin: "-10% 0px -65% 0px" });
  headings.forEach((heading) => observer.observe(heading));
})();
