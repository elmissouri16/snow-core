# Recent changes audit, 2026-09-10

Follow-up implementation and measurements are in [fixes/README.md](fixes/README.md).
The original findings and pre-fix samples below remain as audit evidence.

Compared `8fad893` (before Charm v2) with `344fbe4` on `feat/ghostty-v2`.
The audit covers 13 commits: the migration, terminal input/appearance,
rendering optimizations, and terminal status integration. No implementation
changes were made. Findings are tracked in `bugs.md`: reopened BUG-067 and
new BUG-070 through BUG-074.

## Stable draft editing benchmark

The existing backspace benchmark shrinks the draft as it runs. This additional
fixture retains an approximately 8 KiB draft: insert `x`, layout/render, then
delete `x`, layout/render. Each operation is one **pair of edits and frames**.
It uses the same Snow model initialization on both revisions, at 120x40.
This measures model/frame construction, not terminal output or Ghostty's GPU.

Apple M3 Pro, macOS arm64, Go 1.27rc3. Both test binaries were compiled before
measurement. With other Snow tests/compilation finished, three pairs ran with
`-test.cpu=1 -test.benchtime=500ms`, isolated `SNOW_HOME`, and order
before/after, after/before, before/after. Both variants force legacy Lip Gloss
to true color and a dark background: v1 otherwise suppresses styles under
captured output, while v2 always constructs them. V2's own palette starts dark
and renders true-color ANSI. No thresholds were changed.

| Text | Before median | After median | Change | Before allocations | After allocations |
| --- | ---: | ---: | ---: | ---: | ---: |
| ASCII | 2.495 ms | 4.952 ms | +98.5% | 1,911 | 2,835 |
| Accented | 2.707 ms | 4.848 ms | +79.1% | 1,730 | 2,967 |
| CJK | 2.478 ms | 4.119 ms | +66.3% | 1,678 | 3,294 |
| Emoji | 2.516 ms | 3.117 ms | +23.9% | 1,074 | 6,304 |

Raw samples are `before-{1,2,3}.txt` and `after-{1,2,3}.txt`; medians include
bytes and allocations in `medians.json`. A separate three-second ASCII profile
attributes 53% of current sampled CPU to textarea `view`, reached from both
component Update and View. This is
evidence of a remaining rendering workload regression; it does not invalidate
the narrower improvements in the previously recorded shrinking-buffer results.

The initial unmatched-color samples are retained only in
`diagnostic-unmatched-colors/`; the final comparison above supersedes them.

## Reproduction

`composer_benchmark.go.txt` is the exact current benchmark source. Copy it into
an isolated checkout's `internal/tui/` as a `_test.go` file. For the baseline,
change only:

- `charm.land/bubbletea/v2` to `github.com/charmbracelet/bubbletea`;
- `tea.KeyPressMsg{Code: 'x', Text: "x"}` to
  `tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}`;
- `tea.KeyPressMsg{Code: tea.KeyBackspace}` to
  `tea.KeyMsg{Type: tea.KeyBackspace}`.

Compile each checkout with `go test -c ./internal/tui -o <binary>` and run the
binaries serially with:

```sh
SNOW_HOME="$(mktemp -d /tmp/snow-audit-bench.XXXXXX)" <binary> \
  -test.run '^$' -test.bench '^BenchmarkAuditUnicodeComposer$' \
  -test.benchmem -test.count=1 -test.cpu=1 -test.benchtime=500ms
```

`repro_tests.go.txt` contains intentionally failing functional probes for the
five additional bugs. Copy it into an isolated current checkout as
`internal/tui/recent_audit_test.go` and run
`go test ./internal/tui -run '^TestAudit' -count=1 -v`. These probes are retained
as text so the audit does not add known failures to the normal test suite.

The session/branch paste and Shift+Up probes were also run against `8fad893`
using v1's equivalent `KeyMsg{Type: KeyRunes, Paste:true}` and `KeyShiftUp`;
both pass there. Current paste/copy probes use the pinned terminal byte decoder.
Manual compaction probes use the real agent and a local fake provider; no
credentials or external provider calls are involved.

Live Ghostty GUI behavior was not exercised. Protocol and renderer tests are
separate from visual verification in the terminal emulator.

## Verification

- Existing `go test ./...` and `go vet ./...` passed.
- Fresh `go test -race ./internal/tui/... ./internal/app ./internal/config -count=1`
  passed, including 140 seconds for the root TUI package.
- All 58 Python script tests and the vendored textarea source check passed.
- The existing benchmark guard passed with isolated `SNOW_HOME`/`GOCACHE`.
  An initial run without `SNOW_HOME` could not access the operator artifact
  directory; the isolated rerun resolved that environment failure.
- Functional audit probes fail as expected on current HEAD. V1 name-paste and
  Shift+Up probes pass. Their outputs are retained alongside the source.
- One initial enhanced-cancellation PTY check timed out during exit; two full
  matrix reruns and ten additional cancellation/resize/long-draft stress runs
  passed. This remains an unconfirmed intermittent observation, separate from
  the six reproduced findings above.
