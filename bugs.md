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

- **Status:** Resolved
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

### Remediation

Plan Mode now uses first-class descriptor effects (`read_only`, `mutating`, and
`conditional`) to filter provider schemas and repeats the same authoritative
check immediately before final dispatch, before permission approval. Missing
metadata derives conservatively from risk; arbitrary Bash, file writes,
process lifecycle calls, and mutating or unclassified extensions fail closed.
Conditional tools require a typed runtime guard.

Subagent delegation resolves actual child capabilities instead of trusting role
names. Spawn, messaging, follow-up, and resume reject Bash, write/edit,
recursive, inherited-shell, unknown, or changed persisted authority. The app
also rejects direct and atomic transitions into Plan Mode while unsafe child
work is already active. The embedded Plan contract and compaction context keep
the branch mode explicit, while only an authoritative runtime transition back
to Default restores mutation.

### Resolution evidence

Verified on the current checkout with:

- focused Plan schema/dispatch, explicit transition, compaction, conditional
  tool, recursive delegation, persisted-role, messaging, and active-child app
  integration regressions in `internal/agent`, `internal/subagent`, and
  `internal/app`;
- `go test ./...`;
- `go test -race ./internal/agent ./internal/subagent ./internal/app ./internal/goal ./internal/session ./internal/tools -count=1`;
- `go vet ./...`;
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`;
- `python3 scripts/check_benchmarks.py`;
- `git diff --check`;
- `./scripts/install-local.sh`, which installed the verified `0.1.0-dev` build;
  and
- an independent read-only review, including two follow-up reviews after the
  initial bypass findings were corrected, with no release-blocking issues
  remaining.

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

- **Status:** Resolved
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

### Remediation

Goal conflict handling now uses one typed optimistic-conflict contract across
the controller, memory store, and SQLite store. Conflicts include only the
current goal ID/status, session ID, branch ID, and controller binding generation;
they never include objective text or store paths. Controller enrichment now
covers preflight checks and every store mutation path, including accounting,
edit, clear, replace, and status transitions after a session rebind.

`update_goal` trims and validates canonical goal IDs and returns structured
conflict details directing the caller to refresh with `get_goal`; it does not
silently substitute a replacement. Failed tool results count as failures, not
productive progress. If the same unresolved terminal conflict recurs for three
consecutive automatic turns, Snow durably defers continuation and pauses the
still-current goal, preventing unlimited reinjection even when the assistant
also emitted explanatory text.

### Resolution evidence

Verified on the current checkout with:

- memory and SQLite tests for typed mutation and accounting conflicts, current
  identity, unchanged usage, and privacy-safe diagnostics;
- controller/tool tests for ID normalization, true replacement conflicts,
  session-rebind generation enrichment, and `get_goal`/`update_goal`
  consistency;
- app session-rebind coverage proving the shared controller projection changes
  atomically;
- agent regressions proving failed tool results do not count as progress and
  three repeated automatic terminal conflicts defer and pause safely;
- `go test ./...`;
- `go test -race ./internal/agent ./internal/subagent ./internal/app ./internal/goal ./internal/session ./internal/tools -count=1`;
- `go vet ./...`;
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`;
- `python3 scripts/check_benchmarks.py`;
- `git diff --check`;
- `./scripts/install-local.sh`, which installed the verified `0.1.0-dev` build;
  and
- an independent read-only review, including follow-up review of accounting
  conflict enrichment and the app-level mode-transition integration test, with
  no release-blocking issue remaining.

## BUG-011: OpenCode inference requests omit session affinity

- **Status:** Resolved
- **Severity:** High
- **Surface:** OpenCode Go and OpenCode Zen provider transports
- **Observed:** OpenCode provider notice received 2026-09-03

### Expected behavior

Every inference request to an OpenCode-managed provider should include
`X-Opencode-Session` with a stable per-conversation identifier. OpenCode uses
this value for request correlation and service optimization and warned that
requests omitting it may error starting September 6, 2026.

### Actual behavior and evidence

At discovery, Snow had only the purpose-scoped
`protocol.ChatRequest.SessionAffinityKey` used for provider prompt caching. Its
OpenCode Go Chat Completions adapter and both OpenCode Zen inference transports
did not send the required conversation header. The OpenCode Go request sent only
content type and optional bearer authorization headers.

Current upstream OpenCode source confirms the contract:

- `packages/opencode/src/session/llm/request.ts` sets
  `x-opencode-session` to the active session ID for provider IDs beginning with
  `opencode`; and
- `packages/console/app/src/routes/zen/util/handler.ts` reads the header for
  inference correlation.

### Impact

- OpenCode cannot reliably correlate Snow's requests from one conversation.
- Snow misses provider-side optimization opportunities.
- OpenCode Go or Zen inference may fail once the announced requirement is
  enforced.

### Reproduction

1. Configure a recording HTTP server as the OpenCode Go or Zen base URL.
2. Run an agent turn in a persisted Snow session.
3. Inspect the inference request headers.
4. Observe that `X-Opencode-Session` is absent.

### Required remediation

1. Add a distinct opaque `ConversationAffinityKey`, derived only from the Snow
   session and active branch, and map it to `X-Opencode-Session` on
   OpenCode-managed inference requests.
2. Retain the purpose-scoped `SessionAffinityKey` independently for provider
   prompt caching.
3. Cover both OpenCode Go Chat Completions and both OpenCode Zen inference
   transports.
4. Keep the proprietary header off model catalogs and arbitrary
   OpenAI-compatible endpoints.
5. Preserve the same conversation value across ordinary turns, retries, tool
   continuations, and compaction without exposing raw Snow session or branch
   IDs.

### Required regression coverage

Add tests proving that:

- native OpenCode Go sends the exact affinity value;
- repeated requests with one affinity value remain stable;
- OpenCode Zen sends it through Chat Completions and Responses;
- codec reuse by an unrelated compatible provider does not send it; and
- authentication, streaming, and request bodies remain unchanged.

### Remediation

`protocol.ChatRequest` now carries a dedicated `ConversationAffinityKey` for
provider conversation correlation. The agent derives it as a fixed-width SHA-256
hash of the durable session and active branch, so it remains stable across
ordinary turns, tool continuations, retries, and compaction while rotating for
branches, forks, and subagent sessions. The existing `SessionAffinityKey`
remains separately purpose-scoped for prompt caching.

OpenCode Go Chat Completions and both OpenCode Zen inference transports map the
conversation key to `X-Opencode-Session`. The reusable Chat Completions codec
requires an explicit opt-in outside native OpenCode Go, preventing arbitrary
OpenAI-compatible endpoints from receiving the proprietary identifier. Model
catalog requests also omit it.

### Resolution evidence

Verified on the current checkout with:

- upstream OpenCode source documentation showing
  `packages/opencode/src/session/llm/request.ts` sends the session header for
  OpenCode-managed providers and the Zen handler reads it for correlation;
- agent tests proving one conversation key survives a real turn followed by
  compaction while purpose-scoped request-cache keys remain distinct;
- branch tests proving the value is opaque, stable within a branch, and rotates
  across a fork;
- OpenCode Go and Zen wire tests covering repeated values, both Zen transports,
  compatible-provider isolation, and catalog omission;
- `go test ./...`;
- `go test -race ./internal/provider/opencodego ./internal/provider/opencodezen ./internal/agent ./internal/app ./pkg/protocol ./pkg/snowsdk -count=1`;
- `go vet ./...`;
- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`;
- `python3 scripts/check_benchmarks.py`;
- `git diff --check`; and
- an independent read-only review, including follow-up confirmation that
  conversation correlation is no longer split by request purpose.

## BUG-012: Jekyll output can truncate guides and break heading links

- **Status:** Resolved
- **Severity:** Medium
- **Surface:** GitHub Pages documentation rendering
- **Observed:** Documentation-site implementation and rendered-link validation

### Expected behavior

Every canonical Markdown guide staged for GitHub Pages should render completely,
and its table-of-contents and cross-document fragment links should resolve to
headings in the generated HTML.

### Actual behavior and evidence

The official GitHub Pages Jekyll image rendered the latter portion of
`docs/security.md` as literal Markdown after a multiline inline-code span placed
`<name>` at the beginning of a physical line. Kramdown treated it as raw HTML,
so every later heading disappeared from the generated document. Rendered-link
validation also found stale fragment names in the RPC and performance guides and
single-hyphen phase anchors that disagreed with Kramdown's handling of em dashes.

### Impact

- The published security guide would lose navigation and formatting after the
  MCP cache section.
- Several table-of-contents and cross-guide links would land at the top of a
  page instead of the intended section.
- Source-only relative-link checks would pass despite defects in rendered HTML.

### Reproduction

1. Stage the documentation with `scripts/build-pages.sh`.
2. Build it with the same `actions/jekyll-build-pages` image used by Pages.
3. Inspect the generated `docs/security.html` after the credential section.
4. Validate generated links and fragments.
5. Observe literal Markdown and missing targets for the affected anchors.

### Required remediation

1. Keep the MCP cache command in one inline-code span that does not expose an
   apparent HTML tag at the beginning of a Markdown line.
2. Correct stale table-of-contents and cross-guide fragments.
3. Use heading punctuation that produces the same stable anchor under GitHub
   Markdown and Kramdown.
4. Validate bounded generated HTML links, assets, schemes, and fragments before
   uploading a Pages artifact.

### Required regression coverage

Add tests proving that:

- staged source links resolve within the explicit Pages allowlist;
- site navigation targets the Jekyll output routes;
- rendered internal links and fragments resolve;
- root-relative links cannot escape the `/snow-core` Pages base path;
- unsafe URL schemes are rejected; and
- validation input count and HTML byte size are bounded.

### Remediation

The affected Markdown now avoids a physical-line `<name>` token, stale fragment
links use their rendered Kramdown anchors, and heading punctuation produces
stable GitHub and Pages IDs. `scripts/check-pages-output.py` validates generated
links, assets, fragments, base-path confinement, duplicate URL attributes,
allowed schemes, and bounded file, byte, reference, and fragment counts before
the Pages artifact is uploaded.

The Pages staging allowlist publishes only canonical documents, the standalone
Go SDK example, RPC schemas, and required references. Raw JavaScript/Python
plugin fixtures and removed language-SDK locations remain outside the artifact.
CI and the deployment workflow both use the same pinned official Jekyll action
and rendered-output validator.

### Resolution evidence

Verified on the current checkout with:

- `python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v`;
- a successful `actions/jekyll-build-pages` v1.0.13 container render;
- `python3 scripts/check-pages-output.py ./_site --base-path /snow-core`;
- assertions that the rendered security guide retains its later headings;
- assertions that edit links map back to the real `site/` sources;
- assertions that JavaScript/Python SDK and plugin-fixture paths are absent;
- `go test ./...`;
- `go vet ./...`;
- `python3 scripts/check_benchmarks.py`;
- `git diff --check`; and
- two independent read-only Pages reviews whose actionable findings were fixed.

## BUG-013: Release installer does not persist its PATH entry

- **Status:** Resolved
- **Severity:** Low
- **Surface:** macOS/Linux release installation
- **Observed:** One-line installer usability review

### Expected behavior

After the one-line installer places `snow` in its default or configured
installation directory, a newly opened supported shell should find the binary
without requiring the user to copy a separate `export PATH=...` command.
Repeated installation must not keep appending the same configuration.

### Actual behavior and impact

The installer previously printed a PATH instruction only when the destination
was absent from the current process environment. It could not change its parent
shell, and it did not persist the directory in a startup file. Users therefore
had to run and remember a second command after installation, weakening the
intended one-line experience.

### Remediation

`scripts/install.sh` now writes one safely quoted, idempotent PATH entry to the
configured login shell's `.zshrc`, `.bashrc`, `.bash_profile`, or `.profile`.
It requires an absolute install path, rejects control characters and
PATH-delimiter colons, preserves macOS Bash profile precedence, honors absolute
Zsh `ZDOTDIR`, and warns rather than corrupting a non-regular profile target.
It supports `SNOW_NO_MODIFY_PATH=1` for users who manage PATH themselves. The
public command
is now one bounded, pipefail-protected bootstrap line; the persisted entry takes
effect in a new shell because a child installer cannot mutate its parent
process.

### Resolution evidence

Installer regression tests cover Bash execution, Linux Bash and macOS Zsh
profiles, idempotence, single-quote escaping, invalid configuration, opt-out,
and non-regular profile targets. Shell syntax checks run under both `sh` and
`bash`, and the README, release guide, and security model describe the same
behavior.
