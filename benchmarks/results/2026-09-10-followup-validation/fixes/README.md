# Follow-up fixes and verification

Base: `472097f`, branch `feat/ghostty-v2`; fixes remain uncommitted in the working
tree. Verified 2026-09-10 with `go1.27rc3 darwin/arm64`.

## Fixed behavior

- **BUG-072:** Occupied clipboard retries are rejected before selection mutation
  in composer, user-input dialog, and login editors. Selections, text/image
  attachments, request owners, and generations survive pending, timeout, canceled,
  and different-owner retries, including remapped paste bindings.
- **BUG-076:** History can traverse recalled collapsed attachments and restore
  the saved draft. Independent edits/attachments still prevent entering history;
  deleting a recalled attachment ends navigation.
- **BUG-077:** Collapsed large paste goes through ordinary textarea selection
  replacement before attachment pruning. Expanded submitted text is exact across
  rune/line thresholds, Unicode, multiline, and forward/backward selections.
- **BUG-078:** Programmatic composer replacement reconciles wrapping and cursor
  visibility once per replacement, including first and same-height frames. The
  optimized rendering path retains its unchanged-dimension guards.
- **BUG-074:** Command identities are captured during core admission via the same
  manual compaction path, not sampled from a possibly newer operation afterward.
  Epoch/sequence watermarks fence older operations and results. Final errors
  upgrade provisional success; local alerts await final cleanup. Deferral does
  not leak into new local or external operations. Zero-identity pre-admission
  errors settle even after a session/branch event fence. Cancellation, focus
  acknowledgement, and newer-turn isolation remain covered.
- **BUG-079:** An idle-core terminal goal boundary announces pending failure once.
  Non-continuing goal metadata no longer resets that error. Intermediate success,
  cancellation, repeated snapshots, and focus/settings changes do not replay alerts.
- **BUG-080:** Manual compaction rejects a closed agent before admitting a new
  operation. This additional defect was found while verifying the identity-returning
  rejection path; both Compact entry points preserve admission state after shutdown.

## Permanent regression files

- `internal/tui/clipboard_selection_regression_test.go`
- `internal/tui/history_restore_regression_test.go`
- `internal/tui/paste_selection_regression_test.go`
- `internal/tui/terminal_compaction_order_test.go`
- `internal/tui/terminal_goal_alert_test.go`
- `internal/agent/turn_snapshot_test.go`

All seven original overlay probes pass without modifying their source. The
original failing output remains at `../repro-results.txt`; the passing output is
in this directory and in `final-recheck.txt`.

### Stronger failure evidence

`TestTerminalCompactionFinalMailboxErrorWinsEitherDeliveryOrder` queues an actual
mailbox envelope during the provider summary. A Store wrapper rejects only its
final agent-message append: checkpoint persistence and the stream completion
succeed, while the real `Agent.Compact` cleanup returns the persistence error.
Both event-first and result-first reduction report one generic failure alert.

`TestTerminalGoalFailureFromAutomaticSummarizerStream` runs the core automatic
goal worker against a fake provider and SQLite session, crosses the configured
context threshold, fails the actual summary request, and reduces subscribed
core events through the blocked-goal boundary. The generic failure alert occurs
once. No real provider credentials are involved.

## Verification

Passed:

```sh
go test ./... -count=1
go vet ./...
go test -race ./internal/tui/... ./internal/agent ./internal/app ./internal/config -count=1
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
go run scripts/sync_textarea.go -check -source "$(go list -m -f '{{.Dir}}' charm.land/bubbles/v2)"
python3 benchmarks/results/2026-09-10-followup-validation/run-repros.py
python3 scripts/check_benchmarks.py
```

The full Go/vet checks were repeated after the final branch-fence guard. A final
focused race run also covered every manual compaction ordering test and core
identity/closed-admission tests. The full race suite passed before that final
small guard; both outputs are retained. Python: **58 tests passed**.

A temporary binary passed:

```sh
python3 scripts/smoke_tui_terminal.py --binary "$temporary_directory/snow" --cancel-repetitions 10
```

This covers enhanced quit/cancel, preenabled mode, legacy/no replies, ten repeated
signal cancellations with terminal restoration, and same-PTY restart handoff.

### Logs

- `go-tests-vet.txt`: full Go suite and vet before the final branch-fence guard.
- `race.txt`: full affected-package race suite.
- `final-recheck.txt`: final full Go/vet, focused race, textarea snapshot check,
  and all seven original probes.
- `python-textarea.txt`: 58 passing Python tests, plus the initial incorrectly
  invoked textarea check (see below).
- `pty.txt`: temporary-binary terminal smoke matrix.
- `repro-results.txt`: seven passing retained audit probes.
- `benchmarks.txt`: final isolated performance guard and measurements.

### Local installation

After all final checks passed, `./scripts/install-local.sh` atomically installed
`/Users/el/.local/bin/snow`; reported version: `0.1.0-dev`. The uncommitted checkout
is now reflected in the local binary. Output is retained in `install.txt`.

### Failed attempts and reruns

- A benchmark run concurrent with the full/race suites exceeded only the mixed
  session hydration timing ceiling: **787,562,875 ns/op** against **400,000,000**.
  Allocation ceilings passed. An isolated rerun passed at **90,765,583 ns/op**;
  the final isolated run passed at **89,826,250 ns/op**. No threshold was relaxed.
- The initial textarea snapshot command omitted mandatory `-source`, so it
  exited with `panic: -source is required`. The documented source-qualified
  command subsequently passed in `final-recheck.txt`.
- Review exposed additional successor-admission and branch-fence orderings;
  permanent tests and fixes cover them. A new test initially expected Running
  rather than Compacting during a successor manual operation; that fixture
  assertion was corrected before the passing final runs.

## Limits

No live Ghostty visual check, real-provider smoke, cross-platform matrix, or
physical disk-failure injection was performed. Clipboard and editor regressions
use synthetic input through the actual reducer. Mailbox failure is injected at
the Store interface, not by damaging storage. PTY repetitions cover idle SIGTERM,
not every startup/active-turn SIGINT/caller-cancellation combination. These checks
verify the tracked defects; they do not establish that the entire application is
bug-free. No commit or release was prepared.
