# Charm v2 rendering regression fix

Measured on 2026-09-10, macOS arm64 / Apple M3 Pro, Go 1.27rc3, default
GOMAXPROCS (11). Both runs use the same seven existing fixtures, three samples,
and default benchmark duration. No tests or competing benchmark processes ran
during the samples.

- `before.txt`: fresh isolated checkout of pre-migration commit `8fad893`.
- `after.txt`: corrected `feat/ghostty-v2` implementation, including the pinned
  textarea ASCII wrapping patch and bounded editor cache.
- `medians.json`: medians of CPU time, allocated bytes, and allocation count.

Command in each checkout (with separate temporary `SNOW_HOME`):

```sh
go test ./internal/tui -run '^$' \
  -bench 'Benchmark(TranscriptRefresh10K|ViewNormalAndNarrow|ComposerBackspace|TranscriptSelectionDragFrame)$' \
  -benchmem -count=3
```

All seven fixtures improve time and allocated bytes against v1. The short
composer fixture has more individual allocations despite lower total bytes and
CPU time; all metrics are retained rather than presenting only timing wins.
These samples measure Snow's update/view code, not a live terminal renderer,
every Unicode workload, or end-to-end provider latency.
