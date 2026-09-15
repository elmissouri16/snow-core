# Focused composer layout regression

Runs the production Go-rendered stream fixture, production assets and existing
harness transport mock in installed Chrome/Chromium. Requires the repository Go
toolchain and Node 22+ with native WebSocket; no npm dependencies or provider calls.

```sh
node scripts/tests/browser/composer-layout/run.mjs
```

The matrix uses 390/1280 widths, 740/360/240 heights, dark/light: **12 reports /
252 assertions**. It checks the approximately 98px idle card (90–106px except
short-height shrink), one action row containing plus/paperclip context controls,
no standalone file/skill buttons or overflow, and the pre-existing autogrow,
caret/focus/selection, reader anchor, clear/restore, attention-seat, and message
control visibility behavior. The maximum textarea height is always read from the
**computed CSS cap**, not hard-coded to a former toolbar's overhead.

For the dedicated 320px attachment/mention workflow, paragraph-length skills and
retained screenshots, use `../composer-context/run.mjs`; its README documents the
separate functional/visual assertion counts and `dist/compact-composer/` artifacts.

Latest verification: this focused command passes **252 assertions / 12 reports**.
The full harness and composer-context narrow checks remain separate gates.
