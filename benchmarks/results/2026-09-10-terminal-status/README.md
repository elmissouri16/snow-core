# Terminal status performance, 2026-09-10

Baseline: `05ab314`, before terminal status integration. Current: the terminal
title/progress/alerts changes on `feat/ghostty-v2`, including the late prompt
result fix. Apple M3 Pro, macOS arm64, Go 1.27rc3.

The test executables were compiled before measuring. With no concurrent Snow
test/compile jobs, three baseline/current pairs ran with `-test.cpu=1`, default
benchmark duration, and an isolated `SNOW_HOME`. Order was before/after,
after/before, before/after. Raw outputs are `before-{1,2,3}.txt` and
`after-{1,2,3}.txt`; `medians.json` contains latency, bytes, and allocations.
No existing performance limit was relaxed.

| Existing path | Before median | After median | Change |
|---|---:|---:|---:|
| Transcript reflow, 10k lines | 7.046 ms | 7.041 ms | -0.1% |
| Frame, 40 columns | 22.507 µs | 22.007 µs | -2.2% |
| Frame, 120 columns | 29.944 µs | 29.777 µs | -0.6% |
| Composer backspace, 256 bytes | 266.896 µs | 264.297 µs | -1.0% |
| Composer backspace, 8 KiB | 2.605 ms | 2.590 ms | -0.6% |
| Composer backspace, 64 KiB | 18.894 ms | 18.828 ms | -0.4% |
| Selection drag frame | 266.583 µs | 258.516 µs | -3.0% |

`terminal.txt` compares the same live-app frame with terminal metadata disabled
and enabled: 86.909 versus 87.390 µs (+0.6%, overlapping observed timing ranges),
both 18,552 B/op and 259 allocations. Treat this small difference as measurement
variation, not a universal zero-cost guarantee.

The heartbeat filter/update/cached-view path measures 88.62 ns, 72 B/op, and
3 allocations per pulse. This excludes the one-second timer wait, terminal I/O,
and Ghostty's own graphics. The cached View itself allocates zero bytes. A real
Bubble Tea renderer test verifies that consecutive blocked-state pulses write
only the progress escape sequence, and that quit/cancellation clear progress
and the title. The new benchmark gate rejects a return to full frame building
on heartbeats (10 µs, 256 B/op, 8 allocations maximum).

An initial sequential comparison with the default CPU setting showed substantial
timing variability even in unchanged composer code. Its raw outputs are retained
as `diagnostic-before.txt` and `diagnostic-after.txt`; they motivated the
alternating controlled comparison above. These are local microbenchmarks, not a
live Ghostty GUI performance measurement.
