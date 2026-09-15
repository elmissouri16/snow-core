# Compact public tool rows

Run from the repository root with Node 22+, Go, and installed Chrome/Chromium:

```sh
node scripts/tests/browser/tool-rows/run.mjs
# Optional open/closed screenshot evidence in dist/tool-row-evidence/:
SNOW_TOOL_ROW_EVIDENCE=1 node scripts/tests/browser/tool-rows/run.mjs
```

The runner reuses the composer-layout fixture-export/Chrome lifecycle and the
harness-layout `fixture.js` transport. It exports actual embedded Go templates
and assets with `TestExportHarnessVisualFixtures`, serves those assets on an
ephemeral loopback port, and loads the live `chat` and inactive `saved-markdown`
pages. Public snapshot changes are test-only. No manager, worker, provider,
tool execution, persistent profile, or user-configuration change is involved.

The 12 reports cover 390px mobile, 900px narrow desktop, and 1280px desktop in
both themes. Checks include:

- Single-line 24px geometry, lengthy/custom wire names, duplicate public-summary
  omission, tool-kind glyphs, and bounded nonduplicate summary truncation.
- Successful outcomes quiet but accessible, with running/rejected/failure,
  contradictory error/unknown, and unrecognized statuses still visible.
- Literal public output, no raw arguments or HTML/Markdown interpretation,
  empty-output nondisclosures, saved missing/unresolved provenance, retained
  64-tool/8KiB UTF-8 per-output/128KiB aggregate history budgets and notices.
- Real CDP pointer opening, Enter collapse, Tab/Shift-Tab focus, Space reopening,
  hover/focus chevrons, and stable keyed disclosure/output/focus across actual
  snapshot polling and saved-DOM enhancement.
- Chrome's accessibility tree retaining the wire name, outcome, and native
  expanded state. Live runtime rows remain outside assistant articles.

These focused checks do not replace the full harness-layout matrix, real-provider
smokes, manual screen-reader testing, or pixel-by-pixel screenshot review.
