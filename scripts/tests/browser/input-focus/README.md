# Native field-edge focus regression

Run from the repository root with **Node 22+** and installed Chrome/Chromium:

```sh
node scripts/tests/browser/input-focus/run.mjs
```

Set `SNOW_CHROME_BIN` to the browser executable if it is not at a standard
location supported by `../live-stream/cdp.mjs`. No npm dependencies, provider
credentials, Go build, running manager, or Snow installation are required.
The runner owns a temporary browser profile and loopback HTTP server; it closes
both and removes its profile on completion. A 120-second deadline bounds the run.

## What is exercised

The fixture loads **all 26 current production stylesheets**, in their exact
`{{define "head"}}` order extracted from `internal/web/templates/pages.html`.
It never copies or substitutes focus CSS. Its exported component snippets use
real template/script class names and relevant ancestry. Inline styles only
position the isolated fixture, native dialogs, inspector, and blur/Tab anchors;
there are no fixture focus, outline, border, or shadow rules.

The matrix includes light/dark themes, 320/1280 CSS-pixel viewports, and normal
plus native Chromium CDP `forced-colors: active` media emulation. **37 component
cases × 8 combinations = 296 scenarios** currently make **4,211 assertions**:

- Explicit text, search, password, email, URL, number, date, telephone, and
  implicit-text inputs; select and textarea controls.
- Sidebar/model searches, organization rename/search fields, reasoning/runtime
  dialogs, project inspector, login, enabled cold-start draft, host defaults,
  and host API-key fields. Values are fictional; no authentication is attempted.
- Pointer and native Tab focus produce a `1px solid var(--focus)` outline at
  `outline-offset: -1px`. Theme RGB values are checked in normal colors; forced
  colors may substitute system colors but must retain a visible solid edge.
- Home and stacked composers, plus attention inline and block custom answers,
  have exactly one wrapper focus outline, no inner textarea outline/shadow, no
  extra wrapper halo, and no second blue border. Existing neutral shadows stay.
- Native pointer blur restores the original outline. Focusing does not change
  field or wrapper dimensions. Disabled fields reject pointer focus, are skipped
  by native Tab, and do not retain a field or wrapper focus edge.
- Buttons, links, checkboxes, radios, organization controls, composer Send, and
  attention's visually hidden radio retain their existing visible keyboard
  outline (the attention radio's indicator belongs to its wrapper).

A hidden Tab anchor is focused programmatically **before** each target; the
actual target receives focus only through native CDP keyboard/pointer events.
Unexpected HTTP requests or browser exceptions fail the suite. Failure output
is bounded to the first 30 assertions, with the total failure count retained.

This is a focused production-CSS cascade regression, not a manager workflow,
full-page geometry, screenshot, OS high-contrast, or cross-engine certification.
Chromium forced-colors emulation verifies browser CSS behavior, not every native
accessibility theme. Source edits had already landed before this runner's first
execution, so no pre-change failure count is claimed.
