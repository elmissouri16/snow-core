# Runtime fixes: performance measurements, 2026-09-05

Four small changes improve checkpoint normalization, terminal previews, and
process-output capture. Together they change 34 added and 12 removed production
lines across four files. Correctness fixes for caller cancellation, admission
waiting, and compaction usage accounting accompany them; their behavioral
contracts are in [SDK](sdk.md), [goals](goals.md), and
[session storage](session-storage-internals.md).

## Method and scope

These are local macOS arm64 / Apple M3 Pro / Go 1.27rc3 measurements. Each timing
and allocation result is the median of three samples, with `-cpu=1`,
`-benchtime=300ms`, and packages run serially using `-p=1`. Baseline measurements
ran first, followed by the changed code, without competing test workloads.
Timing remains subject to host load and scheduling; three samples do not
establish a statistical performance guarantee.

The baseline loads the four original performance files from
`4d35f5273f994aaab4fd0cbd87bd09ce39dc12df` through a temporary Go overlay. All other
code and benchmark fixtures are common to both runs. The changed performance
implementations are in `internal/compact/planner.go`,
`internal/process/output.go`, `internal/tui/tools_info.go`, and
`internal/tui/view.go`.

Raw [before](../benchmarks/results/2026-09-05-runtime-fixes/bench-before.txt) and
[after](../benchmarks/results/2026-09-05-runtime-fixes/bench-after.txt) outputs,
[benchmark medians](../benchmarks/results/2026-09-05-runtime-fixes/benchmark-medians.json),
and [memory results](../benchmarks/results/2026-09-05-runtime-fixes/memory-results.json)
are retained. `B/op` measures total allocation volume per operation, not live
RAM. RAM measurements are separate below.

## Speed and allocation volume

| Operation | Before | After | Speedup | B/op before → after |
|---|---:|---:|---:|---:|
| Checkpoint: 7.5 KB, 100 lines across sections | 113.66 µs | 88.16 µs | 1.29x | 185,528 → 127,408 |
| Checkpoint: 28.8 KB, 400 lines across sections | 515.71 µs | 287.96 µs | 1.79x | 1,446,408 → 509,936 |
| Checkpoint: 28.5 KB, 400 lines in one section | 2.121 ms | 0.343 ms | 6.19x | 12,510,000 → 669,016 |
| Checkpoint stress: 114 KB, 1,600 lines in one section | 21.526 ms | 1.250 ms | 17.22x | 193,995,808 → 2,389,576 |
| Safe terminal delta: 20 bytes | 127.60 ns | 35.49 ns | 3.60x | 24 → 0 |
| Safe terminal text: 4,400 bytes | 24.94 µs | 8.20 µs | 3.04x | 4,864 → 0 |
| Truncate 4 KiB ASCII to 120 runes | 9.90 µs | 1.03 µs | 9.58x | 16,640 → 736 |
| Truncate 128 KiB ASCII to 120 runes | 278.78 µs | 1.03 µs | 270.66x | 524,544 → 736 |
| Render ordinary tool preview: 45 lines, width 120 | 28.83 µs | 12.55 µs | 2.30x | 7,856 → 3,920 |
| Full 1 MiB output buffer: 4 KiB write | 25.88 µs | 0.58 µs | 44.34x | 112 → 114 |
| Capture 32 MiB from a local subprocess | 45.27 ms | 26.56 ms | 1.70x | 5,577,149 → 6,901,739 |

Checkpoint assembly now accumulates each section in a builder instead of
recopying its growing body for every line. The default provider summary target
is 2,000 tokens: larger checkpoint cases are stress fixtures. Normalization
excludes provider latency, history scanning, and persistence.

Terminal sanitization reuses already-safe strings. Truncation scans only far
enough to establish the prefix and converts that prefix instead of the full
input. The very large truncation ratio measures one helper; the ordinary tool
preview benchmark is the more representative combined path. Existing Unicode,
malformed UTF-8, control removal, byte limits, and ellipsis behavior are checked
against the original implementations.

The output buffer advances a slice and occasionally compacts it into reusable
storage. Its 44x result isolates steady-state writes into a full buffer. The
32 MiB capture includes `head -c 33554432 /dev/zero`, subprocess startup,
`os/exec` pipe transfer to the same stdout/stderr sink used by the runtime,
buffer growth, and cleanup. It took 41% less time but allocated 24% more total
bytes. Neither measurement predicts an entire build or agent turn's speedup.

Tradeoffs remain visible: control-heavy sanitization measured 19.18 → 20.58 µs
(7% slower), with unchanged 4,096 B/op, because the fast-path check adds work
before falling back. A small 64 KiB retention buffer with 32 KiB writes improved
only 1.16x. Per-write notification still allocates; the new storage allocation
is amortized into steady-state B/op.

## Actual RAM

| Measurement | Before | After | Change |
|---|---:|---:|---:|
| Live heap per full default output buffer | 1.000 MiB | 1.250 MiB | +256 KiB / +25% |
| Peak process RSS: one checkpoint stress operation | 15.45 MiB | 13.22 MiB | −14% |
| Peak process RSS: one 32 MiB subprocess capture | 16.95 MiB | 18.39 MiB | +8% |

Live heap is the median of three post-GC `runtime.MemStats.HeapAlloc`
measurements. Each sample creates 16 buffers, fills each with 1 MiB, performs
300 further 4 KiB writes per buffer, collects garbage, and divides the retained
heap delta by 16. The input payload is allocated before the baseline snapshot
and kept alive throughout. Buffer structs/channels and runtime noise are
included. The [probe source](../benchmarks/results/2026-09-05-runtime-fixes/heap-probe.go.txt)
is retained; it was loaded only through a temporary test overlay.

RSS uses macOS `/usr/bin/time -l` around compiled test binaries, with three
separate processes per side and case. Before/after executions alternate for
each sample. Each process runs one benchmark iteration. The reported maximum
resident set size is in bytes and includes the test harness and Go runtime;
it is not Snow's idle or typical production RAM. Raw heap and RSS output is in
[memory-raw.txt](../benchmarks/results/2026-09-05-runtime-fixes/memory-raw.txt).
Large reductions in allocation churn need not produce equally large RSS drops.

## Reproduce

From the repository root, prepare an overlay without switching the checkout:

```sh
snow_bench_tmp=$(mktemp -d)
python3 - "$snow_bench_tmp" <<'PY'
import json, pathlib, subprocess, sys
root = pathlib.Path.cwd()
tmp = pathlib.Path(sys.argv[1])
files = ['internal/compact/planner.go', 'internal/process/output.go',
         'internal/tui/tools_info.go', 'internal/tui/view.go']
replace = {}
for file in files:
    target = tmp / pathlib.Path(file).name
    target.write_bytes(subprocess.check_output(['git', 'show', '4d35f5273f994aaab4fd0cbd87bd09ce39dc12df:' + file]))
    replace[str(root / file)] = str(target)
(tmp / 'before.json').write_text(json.dumps({'Replace': replace}))
probe = root / 'benchmarks/results/2026-09-05-runtime-fixes/heap-probe.go.txt'
extra = {str(root / 'internal/process/measure_heap_test.go'): str(probe)}
(tmp / 'before-heap.json').write_text(json.dumps({'Replace': replace | extra}))
(tmp / 'after-heap.json').write_text(json.dumps({'Replace': extra}))
PY
snow_bench_pattern='^(BenchmarkNormalizeCheckpoint|BenchmarkSanitizeTerminalText|BenchmarkTruncateRunes|BenchmarkToolOutputPreview|BenchmarkCompactAgentText|BenchmarkFullOutputRingWrite|BenchmarkProcessCapture32MiB)$'
go test -p=1 -overlay "$snow_bench_tmp/before.json" \
  ./internal/tui ./internal/compact ./internal/process -run '^$' \
  -bench "$snow_bench_pattern" -benchmem -benchtime=300ms -count=3 -cpu=1
go test -p=1 ./internal/tui ./internal/compact ./internal/process -run '^$' \
  -bench "$snow_bench_pattern" -benchmem -benchtime=300ms -count=3 -cpu=1
```

For each side, compile with its heap overlay and measure live heap:

```sh
for side in before after; do
  go test -c -overlay "$snow_bench_tmp/$side-heap.json" \
    -o "$snow_bench_tmp/process-$side.test" ./internal/process
  go test -c -overlay "$snow_bench_tmp/$side-heap.json" \
    -o "$snow_bench_tmp/compact-$side.test" ./internal/compact
  "$snow_bench_tmp/process-$side.test" \
    -test.run '^TestMeasureRetainedOutputHeap$' -test.v -test.count=3
done
```

For RSS, run `/usr/bin/time -l` on those compiled binaries with
`-test.run '^$' -test.benchtime=1x -test.benchmem -test.cpu=1`. Use
`-test.bench '^BenchmarkNormalizeCheckpoint/concentrated/lines1600_'` for the
compact binary and `-test.bench '^BenchmarkProcessCapture32MiB$'` for the process
binary. Repeat three times, alternating before and after. Keep other test and
build workloads stopped while collecting timing and RAM samples.

## Verification

Permanent regressions cover 1,005 checkpoint equivalence cases, 10,010 terminal
inputs at eight limits plus rendered previews, 5,500 output writes against a
retained-byte oracle, and repeated buffer compaction. Cancellation, real
manager-tool admission races, automatic/manual summary usage, failed attempts,
budget crossings, branch ancestry, reopen, and accounting failure events have
focused coverage. The full Go suite, affected race suites, vet, 56 support-script
tests, and the unchanged allocation regression guard passed locally. These
results do not substitute for the remote release CI gate.

Successful verification commands included:

```sh
go test ./...
go test -race ./internal/... ./pkg/snowsdk -count=1
go vet ./...
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
python3 scripts/check_benchmarks.py
(cd examples/sdk && go test ./... && go run .)
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
git diff --check
./scripts/install-local.sh
```

The final agent and session changes also passed fresh affected-package race
runs. The vulnerability scan found no reachable vulnerabilities. All changed
Go files are formatted and remain below the 1,000-line cap. The installer built
and installed `~/.local/bin/snow` reporting `0.1.0-dev`; its offline smoke test
exited successfully using `--provider fake --model fake-1 --thinking off
--permission deny --no-session --no-plugins --no-mcp --no-skills --no-subagents
--no-debug -p hello` and an isolated `SNOW_HOME`. Measurements were collected before the release commit.
