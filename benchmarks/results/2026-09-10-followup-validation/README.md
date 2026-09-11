# Follow-up validation of recent TUI changes

## Fix follow-up

The subsequent working-tree fixes make all seven retained probes pass and add
broader permanent regression tests. See [fix verification](fixes/README.md) for
implementation details, actual core failure injection, checks, and limitations.
The original failure sources and output below are preserved as historical evidence.

## Original audit

Audited checkout: `472097f` (`feat/ghostty-v2`), Go `go1.27rc3 darwin/arm64`.
The working tree was clean before the audit. Scope: recent terminal status,
manual compaction settlement, clipboard/input, visible composer rows, and signal
shutdown changes, plus directly related history/layout interactions. Not every
finding is established to have originated in the latest commits.

No product source was changed. These diagnostic tests are stored as text and
injected with Go's build overlay, so the ordinary test suite does not silently
acquire known failing tests. They are executable reproduction evidence, not fixes.

## Reproduce

From the repository root:

```sh
python3 benchmarks/results/2026-09-10-followup-validation/run-repros.py
```

Requires the checkout's Go toolchain. The runner creates a temporary overlay and
runs only `TestFollowup*` in `internal/tui`; no real provider or clipboard account
is used. Exit status **1 is expected on the audited checkout**. Full captured
output is in `repro-results.txt`, and the test source is `repro_tests.go.txt`.
The overlay disables incidental vet; this audit did not run linters.

## Findings

| Probe | Tracker | Observed result |
|---|---|---|
| `TestFollowupBlockedClipboardPreservesSelection` | BUG-072 reopened | Timed-out clipboard retry changes selected composer `draft` to empty, or dialog `draft` to `draf`, without starting a read. |
| `TestFollowupLargeHistoryRestoresDraft` | BUG-076 | Down leaves a collapsed recalled prompt in place instead of restoring `new draft`. |
| `TestFollowupLargePasteReplacesPartialSelection` | BUG-077 | Large paste retains the selected `c` and the selection; small-paste control passes. |
| `TestFollowupHistoryKeepsCursorVisible` | BUG-078 | Twenty-line history recall leaves cursor on line 19 with viewport offset 0 and height 6. |
| `TestFollowupGoalCompactionFailureAlerts` | BUG-079 | Automatic compaction error/blocked-goal reducer sequence ends Failed without an alert. |
| `TestFollowupTwoCompactionResultsBeforeEvents` | BUG-074 reopened | Two actual core compactions, both results delivered first, followed by acknowledgement and delayed lifecycle events, reopen Done. |
| `TestFollowupCompactionFinalErrorOverridesSuccess` | BUG-074 follow-up | Injected final command error cannot override earlier stream success for the same compaction identity. |

All seven top-level probes fail with the expected behavioral assertions; the
small-paste control passes. Clipboard/history/paste probes exercise the actual
TUI reducer with synthetic input, not a physical terminal. The two-operation
compaction test uses real `Agent.Compact` results and subscribed events. The goal
failure probe injects a source-traced event sequence. The final-error probe injects
an error result: `Agent.Compact` can join deferred mailbox-persistence errors after
its lifecycle event, but actual storage failure was not injected end-to-end here.

## Fresh verification

Passed against product source at the audited checkout:

- `go test ./... -count=1` — all packages passed. An initial bounded
  `go test ./...` attempt hit the shell's 120-second timeout; the complete fresh
  run was restarted as a managed process and exited successfully.
- `go test -race ./internal/tui/... ./internal/app ./internal/config -count=1`
  — all affected packages passed.
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`
  — 58 tests passed.
- `go build -o "$temporary_directory/snow" ./cmd/snow` — passed.
- `python3 scripts/smoke_tui_terminal.py --binary "$temporary_directory/snow" --cancel-repetitions 10`
  — enhanced quit/cancel, preenabled mode, legacy/no replies, ten repeated signal
  cancellations with restored PTYs, and same-PTY restart handoff passed.
- Reviewer runs of the complete textarea suite and targeted existing input,
  clipboard, composer/history/cache/layout, and late-prompt regressions passed.

The clean ordinary suite does not cover the retained failing paths. This audit
was not a real-provider smoke, live Ghostty visual check, cross-platform matrix,
benchmark rerun, or end-to-end mailbox disk-failure injection. The existing PTY
script's repeated signal coverage is idle SIGTERM, not all startup/active-turn
SIGINT/caller-cancellation permutations. No local installation was performed
because no feature or fix was implemented.
