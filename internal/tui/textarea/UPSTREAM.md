# Pinned Bubbles textarea

This directory contains the textarea component and its two internal helpers
from `charm.land/bubbles/v2 v2.2.1`. Copyright Charmbracelet, Inc.; the original
MIT [license](LICENSE) is included. Other Bubbles components use the upstream
module directly. This copy remains private to the TUI.

The `wrap` patch uses `runeTextWidth` for printable ASCII, avoiding repeated
rune-to-string conversion and grapheme segmentation. All other runes retain
the original `uniseg.StringWidth` path.

The view patch styles only visible rows while preserving an empty row for each
hidden row, including the trailing row used by the viewport. It predicts the
viewport's bottom clamp after edits/resizes and preserves selection offsets for
hidden segments. Update still prepares viewport content before scrolling; View
renders at the resulting offset. The empty-placeholder check avoids building a
full draft string when cursor position already rules it out. Cursor, selection,
paste, and Unicode wrapping logic remain upstream.

`visible_rows_test.go` compares against the unmodified upstream component,
including Unicode, real/virtual cursors, narrow widths, resize, selection,
scrolling, dynamic height, styles, and placeholders.

Source declarations and the complete upstream test suite are split by feature
to respect Snow's 1,000-line file limit. Generated files retain upstream idioms
so changes can be audited against the pinned source; do not edit them directly.

From the repository root, reproduce or verify the snapshot:

```sh
go run scripts/sync_textarea.go -source "$(go list -m -f '{{.Dir}}' charm.land/bubbles/v2)"
go run scripts/sync_textarea.go -check -source "$(go list -m -f '{{.Dir}}' charm.land/bubbles/v2)"
go test ./internal/tui/textarea/... ./internal/tui
```

The sync script checks source SHA-256 hashes before applying the optimization.
When upgrading Bubbles, review the upstream changes and hashes, regenerate,
run the upstream tests plus Snow's layout/input/terminal checks, and compare
composer benchmarks. Remove this copy and return to upstream imports when an
upstream version provides equivalent performance. A Bubbles version bump alone
does not silently update this snapshot.

Upstream: <https://github.com/charmbracelet/bubbles/tree/v2.2.1/textarea>.
