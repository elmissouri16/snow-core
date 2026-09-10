# Verified fixes for the recent-change audit

The stable-draft benchmark is now `BenchmarkStableComposer` in
`internal/tui/stable_composer_bench_test.go`. The fixture is unchanged from the
audit except for its name: one insertion/frame and one deletion/frame at a
constant approximately 8 KiB, 120x40, with matched true-color/dark styling.

Three alternating runs used the precompiled `8fad893` baseline and the fixed
checkout, one CPU, 500 ms per case, with no other builds/tests during timing.
`baseline-*.txt`, `fixed-*.txt`, and `medians.json` retain the measurements.

| Text | v1 baseline ms/pair | Fixed ms/pair | Latency change | Fixed bytes/pair |
| --- | ---: | ---: | ---: | ---: |
| ASCII | 2.662 | 1.769 | -33.5% | 768,726 |
| Accented | 2.714 | 1.896 | -30.1% | 768,167 |
| CJK | 2.485 | 1.638 | -34.1% | 771,929 |
| Emoji | 2.508 | 2.241 | -10.7% | 862,722 |

Allocated bytes improve by 20–23% versus v1. Allocation counts remain higher
(especially for emoji), so this is a latency/allocated-bytes improvement, not
allocation-count parity or a terminal GPU measurement. The new allocation gates
reject the audit's pre-fix v2 samples, with existing limits unchanged. Generous
CPU limits remain a catastrophic-regression check; local baseline comparisons
are required for rendering changes.

The pinned textarea preserves Update/View ordering and every viewport row but
skips styling hidden rows. It accounts for SetContent clamping after a resize,
and only computes hidden-row selection coordinates when a selection exists.
An independent differential test compares output and editor state to the
unmodified upstream component across Unicode, widths, cursor modes, scrolling,
selection, geometry/style changes, dynamic height, and placeholders.

The intermittent cancellation timeout was also reproduced with a stack:
Bubble Tea's signal sender blocked after external context cancellation ended
its event loop, while shutdown waited for that sender. The TUI now owns signals
through context cancellation and disables Bubble Tea's internal signal handler.
The PTY smoke adds repeated cancellation/restoration checks; see BUG-075 and
`cancellation-deadlock.txt`.

Functional regressions cover modified copy, name-field bracketed paste,
modified history arrows, repeated OSC 52 requests, and manual compaction
completion/failure/cancellation and event/result ordering. See `bugs.md` for the
final verification status of BUG-067 and BUG-070 through BUG-075.

## Changed areas and verification

- Input routing: `input.go`, `paste.go`, `input_history.go`, `user_input.go`,
  `terminal_clipboard.go`, and top-level key dispatch.
- Compaction terminal state: `terminal_compaction.go`, `terminal_status.go`,
  event reduction, and command-result identity tracking.
- Rendering: reproducible textarea view patch in `scripts/sync_textarea.go`,
  upstream differential tests, and the stable composer benchmark/gates.
- Shutdown: `runtime.go` signal ownership and repeated PTY cancellation checks.
- Canonical guides and `bugs.md` describe the resulting behavior.

Verified commands (logs retained here):

```sh
go test ./...
go vet ./...
go test -race ./internal/tui/... ./internal/app ./internal/config -count=1
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
python3 scripts/check_benchmarks.py
python3 scripts/smoke_tui_terminal.py --binary /tmp/snow-fixes-bin --cancel-repetitions 30
go run scripts/sync_textarea.go -check -source /Users/el/go/pkg/mod/charm.land/bubbles/v2@v2.2.1
```

Go checks used `GOCACHE=/tmp/snow-zen-gocache` and isolated `SNOW_HOME` values.
Mock-server tests used local network permission; no real providers were called.
The initial race run found a race in the new test's event collector; replacing
that slice with a channel resolved it, and the final full race run passes.
Live Ghostty GUI visuals remain unverified; these checks exercise the protocol,
renderer, and real PTY process lifecycle.

`./scripts/install-local.sh` installed version `0.1.0-dev` from the modified
`feat/ghostty-v2` checkout (base revision `344fbe4`). The installed
`~/.local/bin/snow` passed the complete PTY matrix and three additional signal
cancellations. These measurements were collected before the fixes were committed.
