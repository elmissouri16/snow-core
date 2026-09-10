# Pinned Bubbles textarea

This directory contains the textarea component and its two internal helpers
from `charm.land/bubbles/v2 v2.2.1`. Copyright Charmbracelet, Inc.; the original
MIT [license](LICENSE) is included. Other Bubbles components use the upstream
module directly. This copy remains private to the TUI.

The only algorithm change is in `wrap`: `runeTextWidth` uses the slice length
for printable ASCII, avoiding repeated rune-to-string conversion and grapheme
segmentation. All other runes use the original `uniseg.StringWidth` path.
Update, cursor, selection, scrolling, paste, and Unicode wrapping logic are
unchanged. Large composer edit benchmarks identified this as a CPU hot path.

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
