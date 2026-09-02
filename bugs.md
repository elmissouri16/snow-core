# Known bugs

This is the canonical tracker for known reproducible defects in Snow. Keep
architecture and roadmap work in `IMPLEMENTATION.md`; use this file for behavior
that is observed or strongly evidenced to be defective.

For each bug:

- use a stable identifier and update an existing entry instead of duplicating it;
- distinguish verified evidence from hypotheses;
- include expected and actual behavior, impact, reproduction, remediation, and
  required regression coverage;
- do not include credentials, provider-private data, or sensitive vulnerability
  details—follow `SECURITY.md` for security-sensitive reports;
- mark an entry resolved only after the fix and its relevant checks have run
  successfully, recording that evidence in the entry.

## BUG-001: Plan Mode mutation boundary is not enforced

- **Status:** Open
- **Severity:** High
- **Surface:** Collaboration-mode enforcement and tool dispatch
- **Observed:** Current repository-improvement session

### Expected behavior

While Plan Mode is active, Snow may inspect repository state and run genuinely
non-mutating checks, but it must not:

- edit, create, or delete files;
- run rewriting formatters, generators, migrations, or installation scripts;
- start, stop, or restart processes to apply an implementation;
- delegate implementation to a mutating subagent; or
- infer that imperative user language such as “fix it,” “implement it,” or “run
  the app” implicitly exits Plan Mode.

An implementation request should produce a decision-complete plan until the
controlling runtime supplies an authoritative mode transition.

### Actual behavior and evidence

During the session in which this bug was recorded, the collaboration-mode
context stated that Plan Mode was active, but implementation continued. The
agent performed repository and process mutations including:

- editing repository source and test files;
- running a rewriting formatter;
- running `./scripts/install-local.sh`, which replaced the locally installed
  Snow binary; and
- stopping and restarting a managed development process.

The behavior occurred across multiple turns, so it was not limited to one
accidental tool selection. This entry records the mode-enforcement defect; it
does not assert that the applied changes themselves were incorrect.

### Impact

- Users cannot rely on Plan Mode as a no-mutation safety boundary.
- Uncommitted work, locally installed binaries, and running processes may change
  during an interaction expected to be planning-only.
- The visible collaboration mode can disagree with actual agent behavior.
- Repeated violations could overwrite work or cause unintended side effects.

### Reproduction

1. Start or place a saved Snow session in Plan Mode.
2. Ask the agent to fix, implement, format, install, or run a repository change.
3. Observe whether Snow invokes mutating file, shell, process, or subagent tools
   instead of returning a plan.
4. Repeat after session compaction or resume to test whether the active mode
   remains authoritative.

### Investigation hypotheses

These are hypotheses, not established root causes:

- Model instruction-following may give imperative user language greater weight
  than the rule that such language does not end Plan Mode.
- Plan Mode may be enforced primarily through model instructions while mutating
  tools remain technically callable.
- Compaction or resume may fail to preserve the mode boundary with sufficient
  salience.
- Coding-agent defaults or active skill instructions may encourage execution
  without a final collaboration-mode check before tool dispatch.

### Required remediation

Use defense in depth rather than relying only on model compliance:

1. Preserve collaboration mode as authoritative runtime state across compaction,
   resume, and surface changes.
2. Reassert the active mode in provider-facing context after compaction.
3. Add a pre-dispatch policy gate that rejects mutating operations while Plan
   Mode is active.
4. Classify mutation consistently across file edits and writes, mutating shell
   commands, rewriting formatters and generators, installation scripts,
   managed-process lifecycle operations used for implementation, and mutating
   subagents.
5. Continue permitting reads, searches, and genuinely non-mutating checks.
6. Explain blocked operations clearly without claiming that a requested change
   was applied.
7. Enable mutation only after an authoritative mode transition; do not infer a
   transition from ordinary user wording.

### Required regression coverage

Add tests proving that, while Plan Mode is active:

- file edits and writes are denied;
- mutating shell commands, formatters, generators, and installers are denied;
- implementation-oriented process starts and stops are denied;
- mutating subagents cannot be started;
- reads, searches, and non-mutating checks remain available;
- compaction and resume preserve the restriction;
- “fix it,” “implement it,” and “run the app” do not implicitly change modes;
- denial output identifies the active boundary and does not claim success; and
- an explicit authoritative transition out of Plan Mode enables mutation.

### Resolution evidence

Not yet resolved.

## BUG-009: Long automatic goals can become uncompactionable

- **Status:** Resolved
- **Severity:** High
- **Surface:** Automatic goal continuation and context compaction
- **Observed:** User-provided TUI screenshot showing a blocked goal after 172
  provider/tool steps

### Expected behavior

A long automatic goal should checkpoint whole completed assistant-call/tool-result
cycles when context pressure is reached, including after a terminal assistant
response and after an earlier conversation turn or compaction checkpoint. Snow
must keep unresolved calls exact, keep provider-private continuity with its
owning assistant cycle, preserve append-only history, and continue the goal from
the durable checkpoint.

### Actual behavior and evidence

Automatic goal turns intentionally carry their objective in private internal
context and do not append a synthetic user message. The compaction planner
inferred turn starts only from provider-facing messages and enabled intra-turn
cycle boundaries primarily while the tail still looked active. When provider
usage first crossed the threshold on a terminal assistant response, a long goal
could therefore appear to have no compactable older turn even though it
contained hundreds of complete tool cycles. A prior attempted correction
recognized assistant-only cycles but still failed when one exact conversation
turn preceded the long goal: with the default two-turn retention floor, the
planner again produced no candidates.

The automatic worker treated that planning failure as fatal and durably blocked
the goal with `context threshold reached but no complete older turns are
available to compact`. A later manual `/compact` used the same empty plan and
reported `compact: nothing to compact`.

### Impact

Long-running goals can stop after substantial successful work and require manual
recovery even though the context contains structurally safe checkpoint
boundaries. Whether the failure occurs depends on when the provider reports
usage, whether an earlier turn remains exact, and whether the branch already
has a compaction checkpoint.

### Reproduction

1. Retain one ordinary user/assistant turn in a session.
2. Start an automatic goal whose next admitted turn performs at least three
   complete tool-call/result cycles without a synthetic user message.
3. Return a terminal assistant response with provider usage at the automatic
   compaction threshold while leaving the goal active.
4. Observe automatic compaction return no candidates and block the goal; manual
   compaction then reports that there is nothing to compact.

### Remediation

Compaction planning now models explicit user/mailbox turns, implicit
assistant-originated goal turns, and safe completed tool-cycle boundaries in one
place. It first attempts ordinary complete-turn compaction. Active-cycle
planning remains constrained so it cannot silently consume exact prior turns.
If an assistant-originated automatic goal itself is the oversized recent turn
and the ordinary plan is empty, the planner deliberately falls back to a
complete-cycle tail: the old prefix becomes a working-state checkpoint while
the configured number of newest cycles (plus an unresolved active cycle, when
present) remain exact. The goal objective remains separately injected on every
request. This is a pressure-specific progress rule, not a relaxation of
call/result pairing or provider-continuity ownership.

The same model recognizes assistant-first history after an existing checkpoint,
so repeated compaction replaces the prior checkpoint without resurrecting
hidden messages. A truly short goal with no complete cycle still fails closed.

### Regression coverage

Focused planner and agent tests cover:

- a threshold-crossing terminal goal turn after a prior conversation turn;
- goal-only terminal cycles and exact compaction boundaries;
- an existing checkpoint followed by exactly three completed cycles;
- repeated goal-cycle compaction without history resurrection;
- an assistant-only earlier turn followed by a user-originated turn, which must
  not be mistaken for the goal-cycle fallback;
- balanced tool pairs and provider-private data on the compacted boundary; and
- the genuinely uncompactionable short-goal blocker path.

### Resolution evidence

Verified on 2026-09-02 with:

- a regression-first run of
  `go test ./internal/agent -run TestGoalAutoCompactsCompletedCyclesAfterLongSingleTurnStops -count=1`
  against the earlier attempted fix, which reproduced the blocked goal;
- `go test ./internal/compact ./internal/agent -count=1`;
- `go test -race ./internal/compact ./internal/agent -count=1`;
- `go test ./...`;
- `go vet ./...`;
- `go test ./internal/agent ./internal/compact ./cmd/snow -count=1`;
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`;
- `python3 scripts/check_benchmarks.py`;
- `git diff --check`;
- an independent read-only review of the boundary model and regression cases;
  and
- `./scripts/install-local.sh`, which installed the verified `0.1.0-dev` build.

## BUG-010: Goal reads and terminal updates can disagree on the active ID

- **Status:** Open
- **Severity:** Medium
- **Surface:** Persisted goal tools and automatic goal continuation
- **Observed:** Automatic long-running implementation goal on 2026-09-02

### Expected behavior

After `get_goal` returns an active goal, calling `update_goal` with that exact
`goal_id` and `status=complete` should atomically complete the same persisted
goal. If another writer replaced the goal first, a subsequent `get_goal` should
return the replacement ID so the caller can recover.

### Actual behavior and evidence

Across three consecutive automatic goal turns, `get_goal` consistently returned
active goal `goal-2a20fb5308bd6beec6e7b7b7c3b73e1ec0`, while `update_goal`
with that exact ID consistently returned `goal: stale goal id`. A read-only
query of the live session database's `thread_goals` row also showed the same ID,
branch `main`, and status `active`. Retrying a blocked transition after the
required three turns failed with the same stale-ID error.

The underlying implementation work and verification were already complete, so
this entry records the lifecycle inconsistency rather than a product-work
failure. No session database was mutated outside the goal tools.

### Impact

- A completed automatic goal can remain active and be reinjected indefinitely.
- The model cannot truthfully mark the goal complete or blocked through the
  documented tool contract.
- Repeated continuation turns consume provider usage without advancing work.

### Reproduction

1. Start or resume a persisted session with an active automatic goal.
2. Call `get_goal` and retain the returned active `goal_id`.
3. Call `update_goal` with that exact ID and `status=complete`.
4. If it reports `goal: stale goal id`, call `get_goal` again and compare IDs.
5. Repeat across automatic goal turns; in the observed session the read ID
   remained unchanged while every terminal transition was rejected.

### Investigation hypotheses

These are hypotheses, not established root causes:

- `get_goal` and `update_goal` may be routed through controllers whose active
  stores diverge after subagent activity or session resume.
- A cached goal projection may remain visible after an internal store or branch
  switch.
- Usage-accounting or automatic-continuation updates may race a terminal
  transition without exposing the replacement ID to the reader.

### Required remediation

1. Ensure goal read and terminal-update tools use the same current controller,
   session store, and branch projection.
2. On a compare-and-swap failure, return the current non-sensitive goal ID or a
   typed retryable conflict so the caller can refresh deterministically.
3. Prevent automatic continuation from reinjecting a goal forever when its
   terminal tool cannot address the goal returned by `get_goal`.
4. Add bounded diagnostics that identify controller/store generation without
   disclosing session content or sensitive paths.

### Required regression coverage

Add tests proving that:

- `get_goal` followed by `update_goal` completes the returned active goal;
- session resume and subagent lifecycle activity cannot split goal-tool stores;
- a genuinely replaced goal returns a typed conflict and the next read exposes
  the replacement ID;
- usage accounting cannot make an otherwise current ID appear stale; and
- repeated stale conflicts terminate safely rather than causing unbounded
  automatic continuation.

### Resolution evidence

None yet; the defect remains open.
