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
public command is a compact `curl -fsSL … | sh` bootstrap; the persisted entry
takes effect in a new shell because a child installer cannot mutate its parent
process.

### Resolution evidence

Installer regression tests cover Bash execution, Linux Bash and macOS Zsh
profiles, idempotence, single-quote escaping, invalid configuration, opt-out,
and non-regular profile targets. Shell syntax checks run under both `sh` and
`bash`, and the README, release guide, and security model describe the same
behavior.

## BUG-014: Pages publishes the repository documentation index

- **Status:** Resolved
- **Severity:** Medium
- **Surface:** Public GitHub Pages documentation
- **Observed:** Public-site review after enabling the repository Pages URL
- **Resolved:** 2026-09-03

### Expected behavior

The repository Pages URL should open an organized end-user manual that starts
with installation and a first agent prompt, groups supported agent workflows by
user task, and publishes only documentation required to use, extend, integrate,
or operate Snow safely.

### Actual behavior and impact

GitHub Pages was configured to deploy `main /docs`, so the live homepage rendered
`docs/README.md` instead of the generated landing page under `site/`. That index
exposes maintainer design history, release operations, audits, research, and
canonical repository ownership before giving users a coherent first-run path.
The custom builder also copied every tracked document plus root architecture,
contributor, changelog, workflow, and benchmark files, so switching the Pages
source alone would still publish repository internals.

Users arriving from the README cannot quickly distinguish installation and
daily agent guidance from contributor records. Internal implementation material
also becomes part of the supported-looking public navigation and artifact even
though it is not intended as a user contract.

### Reproduction

1. Open `https://elmissouri16.github.io/snow-core/` while Pages uses the
   `main /docs` branch source.
2. Observe that the first paragraph matches `docs/README.md` and describes the
   documentation directory rather than a first-use task.
3. Run `scripts/build-pages.sh` against a fresh directory.
4. Observe that the staged artifact contains all `docs/*.md` files together with
   `IMPLEMENTATION.md`, `AGENTS.md`, `CHANGELOG.md`, workflow YAML, and benchmark
   configuration.

### Remediation requirements

- Configure Pages to deploy through the existing `Documentation` GitHub Actions
  workflow rather than directly from `main /docs`.
- Add a canonical getting-started guide and task-oriented homepage/navigation.
- Replace broad staging with an explicit allowlist of public user and integration
  guides while keeping maintainer documents available only in the repository.
- Add tests that fail if internal indexes, implementation records, audits,
  research, release procedures, workflows, or benchmarks re-enter the public
  artifact.

### Verification status

Resolved by `d581369` (`docs(pages): publish curated user manual`). The focused
Pages suite, the complete 52-test support-script suite, `go test ./...`,
`go vet ./...`, the benchmark guard, the official GitHub Pages Jekyll image,
and the rendered-output validator all passed. Documentation workflow run
[`33797490365`](https://github.com/elmissouri16/snow-core/actions/runs/33797490365)
built and deployed the site successfully. Live verification confirmed the
curated homepage and getting-started guide return HTTP 200, while bug,
implementation, research, session-internals, Pages, release, benchmark, and
design-plan routes return HTTP 404.

## BUG-015: Printed Pages guides hide their headings

- **Status:** Resolved in the working tree
- **Severity:** Low
- **Surface:** GitHub Pages print and PDF output
- **Observed:** Documentation presentation audit

### Expected behavior

Printed and PDF versions of every public guide should retain a visible heading
hierarchy on the white print background.

### Actual behavior and impact

The screen stylesheet assigns `#f6faff` to `.prose h1` through `.prose h4`.
The print rules change the page background to white and paragraphs, lists, and
table cells to dark text, but do not override the explicit heading color.
Headings therefore render almost white on white in print and PDF output, making
the document structure difficult to read.

### Reproduction

1. Open the homepage or any public guide.
2. Open the browser print preview or save the page as PDF.
3. Observe that prose headings retain their near-white screen color on the
   white print background.

### Remediation requirements

- Set an explicit dark print color for `.prose h1` through `.prose h4`.
- Keep screen styling unchanged.
- Add regression coverage that checks the print block owns all four heading
  levels.

### Verification status

Resolved by the Pages documentation overhaul in this working tree. The print
stylesheet now assigns dark colors to all prose heading levels and other
screen-specific text, with light backgrounds for quotes, tables, and code.
The focused Pages tests assert the heading color and other print selectors; the
complete support-script suite, official GitHub Pages Jekyll image, and rendered
site validator pass. A headless Chrome print of the provider guide produced a
PDF whose content stream contains the expected `#111` text drawing color.

## BUG-016: Native updater rejects valid release archives

- **Status:** Resolved in the working tree
- **Severity:** High
- **Surface:** Interactive native self-update installation
- **Observed:** Installing published `v0.1.0-alpha.3` from `v0.1.0-alpha.2`

### Expected behavior

After the release archive passes checksum and strict member validation, the
native updater should validate the staged binary and atomically install it.

### Actual behavior and impact

The updater reports `release archive has trailing or invalid data` for the
valid published archive even though the release workflow and external checksum
verification accept it. The tar reader reaches the end-of-archive marker before
the gzip reader consumes and validates the remaining gzip trailer, so the
underlying-reader length check mistakes unread valid framing for appended data.
Users cannot install the release through the new native update path.

### Reproduction

1. Run the published `v0.1.0-alpha.2` binary on supported macOS/Linux hardware.
2. Check for `v0.1.0-alpha.3` and select installation, or enable automatic
   installation.
3. Observe `update: release archive has trailing or invalid data`.
4. Independently verify the same archive against `SHA256SUMS` and extract it
   successfully.

### Remediation requirements

- Drain the validated gzip member through EOF after tar reaches its end marker
  so its checksum/trailer is consumed before testing for appended bytes.
- Continue rejecting a second gzip member and arbitrary trailing bytes.
- Add regression coverage using a sufficiently large valid release archive so
  gzip buffering cannot hide unread valid trailer bytes.
- Re-run native updater, focused TUI, race, full-suite, and vet checks.

### Verification status

Resolved in the working tree by draining the single gzip member through EOF
before closing it and checking the byte reader for appended data. Regression
coverage accepts a large valid archive while continuing to reject arbitrary
trailing bytes and a second gzip member. A live test copied the installed
`v0.1.0-alpha.2` executable to a temporary directory, discovered the published
`v0.1.0-alpha.3` release, installed it through the native updater, and verified
the resulting binary reports `0.1.0-alpha.3`; the user-local executable remained
unchanged. Focused updater/config/app/RPC/protocol/TUI/CLI tests, updater/app and
TUI/RPC race tests, `go test ./...`, `go vet ./...`, the 53-test support-script
suite, the benchmark guard, the production build, and `git diff --check` pass.

## BUG-017: Updater install test flakes under host load

- **Status:** Resolved in the working tree
- **Severity:** Low
- **Surface:** Local and CI updater verification
- **Observed:** Full-suite verification on macOS after other resource-intensive
  checks

### Expected behavior

`TestInstallVerifiedReleaseAtomically` should reliably execute its tiny staged
version script during a full `go test ./...` run.

### Actual behavior and impact

The test configures a two-second service command timeout and a separate
one-second final version-check timeout. Under host load, the subprocess can miss
those test-only deadlines and report `binary version check failed: signal:
killed`. The same test passes immediately in isolation, so verification can
produce a misleading failure and require an unnecessary rerun even though the
production updater defaults to a ten-second command timeout.

### Reproduction

1. Run resource-intensive benchmark or Go verification on a loaded macOS host.
2. Run `go test ./...`.
3. Observe `TestInstallVerifiedReleaseAtomically` occasionally fail during the
   staged binary version check with `signal: killed`.
4. Run `go test ./internal/update -run
   '^TestInstallVerifiedReleaseAtomically$' -count=1` and observe it pass.

### Remediation requirements

- Give this success-path test the same ten-second command allowance as the
  production updater default.
- Keep production timeout behavior unchanged.
- Re-run the focused updater test and the full Go suite.

### Verification status

Resolved in the working tree by using the production-equivalent ten-second
allowance for both success-path version checks. Production behavior is
unchanged. The focused test passed ten consecutive runs, and `go test ./...`
passed afterward.

## BUG-018: Headless output can be silently truncated after subscriber eviction

- **Status:** Resolved in the working tree, including the RPC variant
- **Severity:** High
- **Surface:** Print, JSON, and RPC output
- **Observed:** Repository-wide reliability audit

### Expected behavior

Print and JSON commands must either deliver their complete normalized event
stream or return an explicit output failure.

### Actual behavior and impact

Both modes write directly from a bounded event-subscriber callback. If stdout
blocks longer than the subscriber deadline, the event bus evicts the callback
but does not report that eviction to the command. Later events are discarded,
and the process can return success after emitting incomplete text or JSONL.

### Reproduction

1. Run print or JSON mode with stdout connected to a consumer that stops reading.
2. Keep the output write blocked for longer than the event-subscriber timeout.
3. Resume the consumer and observe that later events are missing even though the
   command reports success.

### Remediation and regression coverage

Expose monitored subscription failure state without changing ordinary
subscriber behavior. Print and JSON modes must use it and return the eviction
error after draining events. Tests gate both output modes beyond the deadline
and require `ErrEventSubscriberEvicted`; ordinary end-to-end output and event
ordering remain covered.

### Verification status

Focused agent/CLI tests, targeted race tests, the full Go suite, vet,
support-script tests, the benchmark guard, installation, and diff checks pass.

### 2026-09-04 audit: RPC variant

The audit found `internal/rpc/main.go:33` forwarding events through an unmonitored
`Agent.Subscribe`. The one-second RPC write timeout does not cover waiting for
`Server.writeMu` (`internal/rpc/server.go:855`), whereas the one-second event
subscriber deadline covers the complete callback. Waiting behind another RPC
response and then writing an event can therefore evict the subscriber even
when every individual write succeeds within its own timeout.

A temporary `TestAuditRPCSubscriberLoss` reproduced this with two serialized
650 ms writes: one response followed by one agent event. Later text and
`turn_done` were absent, while `Server.Serve` returned nil and no write error
was recorded. This can also strand an interactive client when subsequent
permission or user-input requests are no longer forwarded. This reproduction
used local bounded output only and required no provider credentials.

RPC must monitor subscription failure, terminate active work/transport with an
explicit error on eviction, and account for writer contention when enforcing
its delivery deadline. Add coverage for overlapping responses and events whose
individual writes remain below the output timeout. The reproduction failed
against the audited implementation.

### RPC resolution evidence

RPC now uses monitored forwarding with a lifetime-scoped watcher. Eviction
wakes the server's existing write-failure shutdown path even while stdin stays
open, and final cleanup drains events and checks failure synchronously before
unsubscribing. Regression coverage exercises two contending 650 ms writes
through the real process output wrapper, requires the eviction error, and
verifies the active provider prompt is canceled and durably aborted. Normal
cleanup still delivers final text and `turn_done` and is safe to call twice.

`go test ./internal/rpc ./internal/subagent`, `go test ./...`,
`go test -race ./internal/subagent ./internal/agent ./internal/app
./internal/session ./internal/rpc ./pkg/snowsdk`, `go vet ./...`, all 56
support-script tests, and `python3 scripts/check_benchmarks.py` passed. Tests
requiring session/artifact directory access were rerun outside the filesystem
sandbox after their initial access failures.

## BUG-019: Section-specific configuration updates can lose concurrent writes

- **Status:** Resolved in the working tree
- **Severity:** High
- **Surface:** Plugin, MCP, and Agent Skill configuration management
- **Observed:** Repository-wide concurrency audit

### Expected behavior

Every configuration read-modify-write operation must serialize against other
Snow processes and apply its mutation to the latest committed file.

### Actual behavior and impact

`updateSection` reads and atomically replaces configuration without acquiring
the lock used by `config.Update`. Concurrent valid plugin, MCP, or skill changes
can therefore use stale snapshots, with the last rename silently discarding an
earlier update.

### Reproduction

Run two Snow configuration-management operations concurrently against the same
file and inspect the resulting JSON. Without serialization, only one unrelated
mutation may survive.

### Remediation and regression coverage

Use one shared process-wide and cross-process lock helper around both typed and
raw-section updates, while retaining raw unknown fields and atomic replacement.
Tests require mutation callbacks to serialize and mix section writers with
ordinary settings writers across helper processes.

### Verification status

Focused configuration tests, targeted race tests, the full Go suite, vet,
support-script tests, the benchmark guard, installation, and diff checks pass.

## BUG-020: TUI shutdown can strand a ChatGPT OAuth worker

- **Status:** Resolved in the working tree
- **Severity:** Medium
- **Surface:** Interactive ChatGPT OAuth lifecycle
- **Observed:** Repository-wide TUI lifecycle audit

### Expected behavior

Closing the TUI must cancel and join its OAuth worker without breaking the
in-session Escape flow that waits for a cancellation completion event.

### Actual behavior and impact

OAuth completion performs an unconditional send to a small progress channel.
If the channel is full after Bubble Tea exits, no consumer remains and the
worker can block forever. The model does not own or join that goroutine before
closing app resources.

### Reproduction

1. Start ChatGPT OAuth and allow progress to fill its event channel.
2. Exit the TUI before login completes.
3. Let login return and observe the worker block while sending its completion.

### Remediation and regression coverage

Give the model a lifetime cancellation function and OAuth wait group. Completion
must select between event delivery and TUI-lifetime cancellation; `Model.Close`
must cancel and join the worker before closing the app. Tests cover a full
channel on shutdown, operation cancellation in a live TUI, and close-time join.

### Verification status

Focused TUI tests, affected-area race tests, the full Go suite, vet,
support-script tests, the benchmark guard, installation, and diff checks pass.

## BUG-021: Active-session writes repeatedly rebuild prior-session search

- **Status:** Resolved in the working tree
- **Severity:** Medium
- **Surface:** Prior-session FTS search performance
- **Observed:** Repository-wide performance audit

### Expected behavior

Writes to the active session, which is excluded from `session_search` results,
must not invalidate or consume capacity in the derived prior-session corpus.
Historical-session changes and active-session switches must still invalidate it.

### Actual behavior and impact

The cache identity includes every session database, WAL, and journal. Each
active-session append can therefore force the next search to reopen and decode
the bounded historical corpus and rebuild its in-memory FTS index, even though
the active session is discarded by the final SQL predicate.

### Reproduction

1. Search prior sessions while the current SQLite session remains open.
2. Append another current-session message, changing its WAL metadata.
3. Search again and observe the derived index rebuild count increase.

### Remediation and regression coverage

Pass the active session ID and path into search, omit that database and its
sidecars from corpus selection and cache identity, and bind the cache to the
exclusion. Tests require no rebuild after an active WAL append, a rebuild after
a historical append, correct exclusion, and a rebuild after switching sessions.

### Verification status

Focused session/built-in-tool tests, affected-area race tests, the full Go
suite, vet, support-script tests, the benchmark guard, installation, and diff
checks pass.

## BUG-022: Physical session forks leak staging lock files

- **Status:** Resolved in the working tree
- **Severity:** Medium
- **Surface:** Independent session forks
- **Observed:** Repository-wide session resource audit

### Expected behavior

A successful or failed physical fork must remove every randomly named staging
file while preserving the published destination's normal lifetime lease.

### Actual behavior and impact

Fork cleanup removes the staging database and SQLite sidecars but omits the
staging `.lock` created by `NewSQLiteStore`. Every populated fork can leave an
orphan hidden file, causing unbounded directory and inode clutter over time.

### Reproduction

Create a non-empty independent session fork and list hidden files beside the
new database. A `.<destination>.tmp-*.lock` file remains.

### Remediation and regression coverage

Include `.lock` in staging cleanup and explicitly remove the successful staging
lease after publishing the database. The fork test now rejects any remaining
staging pathname while continuing to reopen and use the destination.

### Verification status

Focused session tests, affected-area race tests, the full Go suite, vet,
support-script tests, the benchmark guard, installation, and diff checks pass.

## BUG-023: Subagent provider timeouts are reported as successful completion

- **Status:** Resolved in the working tree
- **Severity:** High
- **Surface:** Subagent task lifecycle, final-result delivery, SDK/RPC/TUI status
- **Observed:** 2026-09-04 codebase audit; reproduced with a real agent loop and
  a local provider fixture

### Expected behavior

A child whose task deadline expires during provider streaming must finish as
interrupted, preserve the timeout reason, and identify any returned text as a
partial result rather than completed work.

### Actual behavior and impact

`internal/agent/streaming_tools.go` persists provider-stream cancellation as
`StopAborted`, and `internal/agent/lifecycle_run.go:594` returns nil for that
stop reason. In `internal/subagent/operations.go:628-654`, the worker only
checks an explicit interrupt flag or a non-nil returned error. It never checks
whether the task context expired before its own cleanup cancellation.

Consequently, a child that reaches `task_timeout_ms` during a provider request
is marked `completed`, with an empty error, and its last partial assistant text
is delivered to the parent as the final result. Parents and integrations can
accept unfinished implementation or verification work as successfully done.
The default task deadline is 1,800,000 ms (30 minutes).

### Reproduction

A temporary `TestAuditSubagentTimeoutStatus` used the real `agent.Agent` inside
the subagent manager with a 40 ms task deadline. Its local provider emitted
`Starting the requested work...`, then blocked until the request context
expired. `WaitAll` returned nil and the child state was:

```text
status=completed error="" result="Starting the requested work..."
```

The assertion requiring `interrupted` failed. No network or external side
effects were involved.

### Remediation and required regression coverage

Capture the task context error before calling the worker's cleanup `cancel()`
and use it when classifying completion, including when the agent returns nil.
Preserve the interruption reason in status and parent-facing final delivery.
Test real-agent deadlines both before any provider output and after partial
output, for initial prompts and mailbox follow-ups. Keep successful completion
and explicit interruption covered separately.

### Verification status

The worker now captures the task context error before cleanup cancellation,
marks expired work interrupted even when the agent returns nil, and preserves
the interruption reason. Parent-facing final delivery explicitly labels any
partial text as incomplete work.

Permanent real-agent regression tests cover deadlines in `Chat`, before the
first stream output, and after partial output, for both initial prompts and
mailbox follow-ups. They assert child status/error and parent mailbox content;
successful initial work remains completed. Existing explicit-interruption
coverage also passes. The focused package tests, full Go suite, affected-area
race checks, vet, 56 support-script tests, and benchmark guard all passed.

## BUG-024: Repository searches repeat line copies and ignore-rule preparation

- **Status:** Resolved in the working tree
- **Severity:** Performance
- **Surface:** Built-in grep and glob tools
- **Observed:** 2026-09-04 performance audit

Complete buffered grep lines were copied into a temporary byte slice and then
copied again into their returned string. Ignore checks also recompiled patterns
and rebuilt the same inherited directory-rule lists for every file. A local
10 MB / 100,000-line grep allocated about 22.47 MB; a 2,000-file glob with 60
ignore rules allocated about 19.90 MB.

The fix returns an owned string directly for complete in-limit lines and keeps
the original fragmented-line, oversized-line drain, error, and cancellation
paths. Ignore patterns are prepared once, and each search caches inherited
rules for at most 256 directories and 4,096 backing-array rule slots. Exhausting
either cache budget falls back to uncached rule assembly without dropping rules
or changing precedence. No path-guard or ignore-file opening checks change.

Regression coverage checks line boundaries, ownership after reader-buffer reuse,
oversized-line recovery, errors, cancellation, pattern semantics, both cache
limits, and fresh rules between searches. Permanent benchmarks cover the line
reader, full 10 MB grep, ignore evaluation, and full 2,000-file glob.

Verification passed: `go test ./internal/tools/builtin -count=1`,
`go test ./...`, `go test -race ./internal/tools/builtin -count=1`,
`go vet ./...`, all 56 support-script tests, and
`python3 scripts/check_benchmarks.py`. Three-sample before/after benchmarks
confirmed about 50% less allocation volume for full grep and 59% less for
full glob. The line-reading stage took 40% less time and ignore evaluation
28% less; whole-glob timing was variable. Exact commands and measurements
are recorded in `docs/performance.md` and `IMPLEMENTATION.md`. Nothing was
installed.

## BUG-025: Process-log rendering and mention matching repeat avoidable work

- **Status:** Resolved in the working tree
- **Severity:** Performance
- **Surface:** TUI process logs and file mention completion

Process-output sanitizing repeatedly grew its buffer despite knowing an output
size lower bound after normalization. Mention basename matching lowercased text
already contained in the lowercase full path. Two production-line changes
reserve that buffer and reuse the lowercase path. They preserve escaping,
UTF-8 repair, original result casing, sorting, and existing output bounds.

Benchmarks and before/after measurements are recorded in `IMPLEMENTATION.md`.
Regression tests check control escaping, CRLF, invalid UTF-8, mixed-case and
Unicode mentions, and path-prefix priority. Existing process-fleet and mention
tests cover display and interaction behavior.

Verification passed: focused TUI tests, `go test ./...`,
`go test -race ./internal/tui -count=1`, `go vet ./...`, all 56 support-script
tests, and the performance regression guard. Three-sample benchmarks confirmed
30% fewer allocated bytes for process sanitizing plus wrapping and 6–23% less
mention-matching time. No installation was performed.

## BUG-026: Context preparation copies unchanged tool results and checkpoint text

- **Status:** Resolved in the working tree
- **Severity:** Performance
- **Surface:** Historical tool-result pruning and persisted context-token checks

When one oversized tool result triggers pruning, the pruning helper also copies
single-text results that are below the threshold. Checkpoint detection similarly
copies a whole single-text message just to inspect its prefix. Two three-line
fast paths skip these copies; multi-block handling, pruning thresholds,
artifact callbacks, and defensive projection ownership retain their existing
behavior. Focused regression tests and repeatable benchmarks cover both paths.

Verification passed: `go test ./internal/compact ./internal/agent -count=1`,
`go test ./...`, `go test -race ./internal/compact ./internal/agent -count=1`,
`go vet ./...`, all 56 support-script tests, and
`python3 scripts/check_benchmarks.py`. Repeated benchmarks measured about 74%
less time and allocation volume for pruning 50 small results plus one oversized
result in an owned projection. Persisted-token lookup with a 32 KiB checkpoint
and 1,500 retained messages dropped from 11.00 to 0.752 µs and from 40,960 to
zero allocated bytes. Scope and reproduction commands are in
`IMPLEMENTATION.md`. No installation was performed.

## BUG-027: Composer Select All can retain or hide selection

- **Status:** Resolved in the working tree
- **Severity:** Low
- **Surface:** TUI composer and application-owned transcript selection

### Expected behavior

Pressing `Ctrl+A` in the ordinary composer visibly selects only its current
draft. Any earlier Snow-managed transcript drag selection and copy menu should
be cleared so selection belongs to one visible surface at a time.

### Actual behavior and impact

The composer correctly tracked the entire draft as selected, but it did not
clear existing transcript selection state. A prior app-mouse drag selection
could therefore remain highlighted alongside the composer and make Select All
appear to include transcript content.

The attempted visual treatment also changed only Bubbles' public `Text` styles.
Bubbles renders the active row with `CursorLine`, and its copied textarea kept a
private active-style pointer aimed at the original unmodified style. A typical
one-line draft therefore had working replacement semantics but no visible
selection highlight.

This does not cover terminal-native `Command+A`/`Super+A`: major terminal
emulators commonly reserve that shortcut and select their own screen before a
TUI process receives an input event. Snow's portable composer shortcut is
`Ctrl+A` on every supported OS.

### Remediation and regression coverage

Composer Select All clears app-owned transcript selection and its context menu
before selecting the draft. Rendering applies reverse video to `Text` and
`CursorLine` for focused and blurred states, then rebinds the copied textarea's
active style before rendering. Focused regressions require the transcript state
to clear, the composer value to remain intact, and a one-line active draft to
contain a reverse-video selection sequence. In-app help and the canonical TUI
guide identify `Ctrl+A` as the cross-platform shortcut and explain how a
terminal-specific `Command+A` mapping can send the same control character.

### Verification status

Verified with the focused visual-selection regression,
`go test ./internal/tui -count=1`, `go test -race ./internal/tui -count=1`,
`go test ./...`, `go vet ./...`, all 56 support-script tests,
`python3 scripts/check_benchmarks.py`, and `git diff --check`. The verified local
binary was then installed with `./scripts/install-local.sh`.

## BUG-028: Multi-command Bash permission cards are noisy and misleading

- **Status:** Resolved
- **Severity:** Medium
- **Surface:** TUI permission picker and static Bash effect presentation
- **Observed:** User-reported screenshot during the Bash permission alpha

### Expected behavior

A permission card for a compound Bash command should present a concise,
distinguishable summary of each meaningful effect. Repeated operations should
identify their command or be grouped, dynamic effects should explain their
source without occupying several identical rows, and shell-test operators must
not be displayed as filesystem paths.

### Actual behavior and impact

A create-then-delete temporary-file command displayed several indistinguishable
`execute` rows, four indistinguishable `unknown (dynamic)` rows, and a bogus
filesystem read ending in `/!`. The command source also consumed multiple wide
rows without a compact label. The analyzer treats the shell `test` builtin as
an external file-reading command and the picker omits effect command names and
reasons, making a correct approval difficult to review when multiple commands
are present.

This is presentation and conservative-analysis behavior rather than a sandbox
escape, but the noisy card obscures the operation a user is being asked to
authorize and undermines the permission system's review value.

### Reproduction

Run in `ask` mode a Bash tool invocation equivalent to:

```sh
tmp_file="${TMPDIR:-/tmp}/snow-test-$$.txt"; printf x > "$tmp_file"; \
  test -f "$tmp_file"; rm -- "$tmp_file"; test ! -e "$tmp_file"
```

### Required remediation and regression coverage

- Treat `test` and `[` as shell builtins and do not infer their predicates as
  filesystem reads.
- Give process effects command-specific labels and dynamic effects concise,
  reason-bearing labels.
- Group identical rendered effect labels with counts and enforce a compact row
  budget independent of the raw protocol effect limit.
- Bound or summarize the displayed Bash source to keep the decision controls
  prominent.
- Add analyzer and TUI regressions reproducing the reported compound command.

### Resolution and verification

Resolved by recognizing only unqualified `test` and `[` command tokens as
shell builtins, preserving external execution for qualified names and wrappers,
and attaching command names to process effects. The permission picker now
groups equivalent effects while retaining every distinct dynamic reason,
escapes layout/control bytes without collapsing path identity, and budgets
command/effect details against the actual overlay height while reserving the
decision rows and footer. Blocking permission requests own small frames; when
the terminal cannot show both the safety context and at least one Bash
command/effect detail, approval is disabled until the terminal is resized.

Verified with focused analyzer and permission-picker regressions, including the
reported compound command, qualified and wrapped `test` executables, narrow and
short default-frame layouts, truncated unknown effects at seven rows, and
control-byte paths. Also verified with `go test ./...`, `go vet ./...`,
`go test -race ./internal/tui ./internal/shellanalysis ./internal/permission
./internal/agent -count=1`, all 56 support-script tests,
`python3 scripts/check_benchmarks.py`, `go build -o ./snow ./cmd/snow`, and
`git diff --check`.

## BUG-029: Bash grep summaries include the search pattern as a file

- **Status:** Resolved; verified in the working tree
- **Severity:** Low
- **Surface:** Static Bash effect summary and permission-card resources
- **Observed:** 2026-09-05 review of Bash command classification

For `grep needle README`, the analyzer reports both `needle` and `README` as
high-confidence filesystem reads. The first ordinary operand is the search
pattern in this invocation, so the extra `needle` path is misleading. This
is separate from the resolved shell-test builtin presentation issue in BUG-028.

The shared command specification now declares pattern, pattern-file, option-value,
and file-operand roles. The generic option parser consumes short clusters,
attached values, long equals values, and `--`; the grep handler distinguishes
an ordinary pattern from file operands. Permanent regressions cover ordinary
and explicit patterns, pattern files, context-count options, multiple files,
and filenames beginning with a dash.

Verified with the focused shell regression suite, the full Go suite, affected
race tests, vet, all 56 support-script tests, and the performance guard. No
security-sensitive reproduction details are included in this public tracker.

## BUG-030: SDK example omits the shell parser module dependency

- **Status:** Resolved; verified in the working tree
- **Severity:** Low
- **Surface:** Standalone `examples/sdk` module
- **Observed:** 2026-09-05 shell-preflight verification

The root module already required `mvdan.cc/sh/v3`, but the standalone SDK
example lacked its indirect requirement and checksums. Running `go test ./...`
in `examples/sdk` failed with a missing go.sum entry for the shell syntax
package. `go mod tidy` synchronized the example's module graph with the current
checkout. Both its test/build step and `go run .` now complete successfully
with the offline fake provider and an isolated temporary Snow home.

## BUG-031: Caller deadlines can return success and restart an active goal

- **Status:** Resolved; verified 2026-09-05
- **Severity:** High
- **Surface:** Core prompt lifecycle, direct SDK prompts, active goal continuation
- **Observed:** 2026-09-05 audit of revision `4d35f52`

When the caller's context expires during provider startup or streaming,
`streamTurnWithErrors` persists an aborted assistant boundary and returns
`StopAborted` with no error. `run` then returns nil at
`internal/agent/lifecycle_run.go:593-594`. The prompt finalizer passes that nil
to `finalizeGoalTurn` without checking the caller's context. An eligible active
goal therefore calls `ContinueGoal`, whose next provider request uses a fresh
background context.

Direct SDK `Prompt` callers can interpret incomplete work as successful, and
an active goal can incur additional provider usage and execute subsequently
approved tools after the host's deadline. RPC separately checks its prompt
context when reporting completion; that status check does not fix the core
continuation decision. This is distinct from BUG-023's resolved child-worker
timeout classification.

### Reproduction and verification

A temporary Go overlay test used a real agent, a temporary SQLite session,
and a local provider that either waited for cancellation in `Chat`, or emitted
`Partial work` and then waited in `Next`. With a 40 ms caller deadline, both
ordinary prompts returned nil with `ctx.Err() == context.DeadlineExceeded`.
With a persisted active goal, both cases also started provider request number
two after the deadline. All four cases reproduced in three consecutive runs.
The second request was held until agent cleanup; no network or tools ran.

### Required remediation and regression coverage

Preserve caller cancellation/deadline outcomes before finalizing a prompt and
deciding goal continuation. Prevent canceled work from launching an automatic
turn; distinguish cancellation from a provider failure when updating durable
goal status. Cover startup and partial-stream deadlines, direct SDK errors,
active goals, queued input, explicit Abort, and successful completion. Existing
tests that intentionally accept nil for cancellation need an explicit contract
decision; changing only the subagent or RPC wrapper is insufficient.

### Verified resolution

Caller context errors are joined into the core prompt and mailbox outcomes
after pending mail is persisted. A matching active goal pauses on caller
cancellation and cannot launch automatic continuation. Pre-canceled or
canceled admission waits return without persisting input; explicit Snow Abort
retains its distinct contract. Startup and partial-stream deadlines, attached
goals, all four SDK prompt methods, and existing queue/Abort behavior pass
focused tests and the agent/SDK race suites. See `docs/sdk.md` and
`docs/goals.md` for the public cancellation contract.

## BUG-032: Checkpoint normalization repeatedly copies growing section bodies

- **Status:** Resolved; verified 2026-09-05
- **Severity:** Performance
- **Surface:** Local checkpoint normalization during compaction
- **Observed:** 2026-09-05 audit of revision `4d35f52`

`canonicalizeWorkingStateCheckpoint` in `internal/compact/planner.go:370-373`
appends each line and separator to an immutable string. A section with many
lines repeatedly copies its entire accumulated body, producing quadratic
allocation volume. The public normalization path canonicalizes provider and
local summaries, so this cost is part of real compaction processing.

A candidate with six added and three removed production lines accumulates the
current section in a `strings.Builder`, assigns its string on flush, then resets
the builder. Existing compaction tests and 1,005 differential cases passed with
the candidate, including duplicate/unknown headings, blank lines, and Unicode.

Three-sample medians on Go 1.27rc3 / macOS arm64 / Apple M3 Pro, `-cpu=1`,
`-benchtime=500ms`, measuring the full `NormalizeWorkingStateCheckpoint` call:

| Fixture | Current time | Candidate time | Current B/op | Candidate B/op |
| --- | ---: | ---: | ---: | ---: |
| 7.5 KB, 100 lines across twelve sections | 109.31 us | 92.60 us | 185,528 | 127,408 |
| 28.8 KB, 400 lines across twelve sections | 547.46 us | 284.02 us | 1,446,408 | 509,936 |
| 28.5 KB, 400 lines in one section | 2,128.51 us | 346.92 us | 12,510,000 | 669,016 |
| 114 KB, 1,600 lines in one section | 22,277.87 us | 1,242.85 us | 193,995,808 | 2,389,576 |

The large fixtures are stress cases; the default provider summary target is
2,000 tokens. These measurements isolate local normalization with no historical
messages and exclude provider latency, summary generation, and persistence.
They do not imply an equivalent speedup for an entire agent turn.

Before adopting the candidate, retain focused equivalence coverage and add a
permanent benchmark for both concentrated and distributed section bodies.

### Verified resolution

Adopted the section-local builder in `internal/compact/planner.go` with the
1,005-case equivalence test and permanent concentrated/distributed benchmarks.
The final three-sample 28.5 KB concentrated case improved from 2.121 ms to
0.343 ms and 12,510,000 to 669,016 B/op. The stress case improved 17.22x;
whole-process peak RSS fell 14%. Full measurements and limitations are in
`docs/runtime-fixes-performance.md`. The compact tests and race suite pass.

## BUG-033: Goal controls can deadlock with subagent manager tools

- **Status:** Resolved; verified 2026-09-05
- **Severity:** High
- **Surface:** Plan-mode transitions and manual compaction during automatic goals
- **Observed:** 2026-09-05 follow-up audit of revision `4d35f52`

`Agent.SetMode` acquires root admission at `internal/agent/configuration.go:212`
and holds it while `StopGoal` cancels and joins automatic work at line 254.
`Manager.List` acquires the same lock at `internal/subagent/operations.go:326`.
If a running automatic goal reaches `list_agents` after the control acquires
admission, the control waits for the turn, and the turn waits for admission.
Canceling the turn cannot interrupt this mutex wait. No child needs to exist.

Entering Plan mode has no deadline on this join, so both calls remain blocked.
Manual `Compact` uses the same lock-and-join pattern at
`internal/agent/session_context.go:771-789`; a caller deadline releases that
control with an error, but a background context permits an indefinite hang.
Branch selection and forking also call `stopAutomaticForControl` while holding
admission; these share the source-level risk but were not separately reproduced.

### Reproduction and verification

A temporary Go overlay used a real agent, SQLite goal, subagent manager, and
the actual `list_agents` tool with a local fake provider. A scheduling gate
paused immediately before delegating to the real manager tool, then resumed
when the control canceled the turn. This forces the relevant legal interleaving
without adding a lock to the tool or creating a child.

The Plan transition hit a three-second test timeout. Its goroutine dump shows
`SetMode -> StopGoal -> stopWork` waiting for completion and the goal's
`managerTool.Run -> Manager.List -> LockAdmission` waiting for the mutex.
Manual compaction hit its 150 ms caller deadline in all three race-enabled
runs and unwound after releasing admission. No data race was reported; this
is a lock dependency cycle.

### Required remediation and regression coverage

Do not join running work while holding an admission lock that its tools need.
Separate cancellation/join from the admitted state transition, then reacquire
admission and revalidate the target state. Preserve transition guards and
prevent another turn from entering the gap. A context check before mutex
acquisition alone does not eliminate the race.

Cover Plan transitions and manual compaction racing with real manager tools,
plus branch/fork controls and concurrent prompt admission. Include no-child
fixtures and bounded completion assertions.

### Verified resolution

The initial proposal above was replaced by a smaller fix that retains atomic
control transactions: admission now supports cancellation while waiting, and
all context-bearing manager operations use it. Canceled tools leave the queue
so the control can finish joining the turn without releasing its transition
guards. Prompt and manual compaction admission also honor caller cancellation.
Permanent race-enabled tests force the original real-manager interleaving for
Plan mode, compaction, branch selection, fork, and replacement prompt, with
bounded completion. A separate test cancels an already-waiting admission.
The full internal race suite and final affected-area race checks pass.

## BUG-034: Compaction usage is missing from session and goal accounting

- **Status:** Resolved; verified 2026-09-05
- **Severity:** Medium
- **Surface:** Provider-backed compaction, usage/cost totals, automatic goal budgets
- **Observed:** 2026-09-05 follow-up audit of revision `4d35f52`

`readCompactionSummary` at `internal/agent/compaction.go:466-468` handles
`EvStreamUsage` only by setting an activity flag. `summarizeForCompaction`
returns summary text without accounting for the provider's reported tokens or
cost. Applying the checkpoint does not persist that usage either. Session
totals and automatic goal accounting therefore omit these provider requests.

This undercounts work as well as displayed cost: an automatic goal can continue
without recognizing that compaction crossed its token budget. The defect is
the discarded usage, not the ordinary possibility that one in-flight request
overshoots a budget. Repeated compactions can repeatedly escape accounting.

### Reproduction and verification

A temporary overlay extended the existing automatic-compaction fixture with
a 150-token goal budget and explicit local provider usage events. The ordinary
request reported 95 tokens and USD 0.10; the summary request reported another
100 tokens and USD 0.20. The fixture verified that request two was the actual
checkpoint request, then observed further normal goal work and completion.

In all three race-enabled runs, both the persisted goal and SQLite session
aggregate reported only 95 tokens and USD 0.10, despite the provider reporting
195 tokens and USD 0.30. The goal completed without recognizing the budget
crossing. Assertions for full usage and cost failed consistently; no data race
was reported. No network requests or real provider charges were involved.

### Required remediation and regression coverage

Collect provider summary usage, persist it in branch accounting, and attribute
automatic compaction to its owning goal before deciding further continuation.
Keep this separate from conversational context-occupancy measurements. Preserve
goal identity across between-turn compaction; simply calling a turn-accounting
helper may not have the correct active-turn attribution there.

Cover manual and automatic compaction, within-turn and between-turn boundaries,
budget crossings, reopen/branch aggregation, and reported usage on failed or
retried summaries. Avoid double counting cumulative usage events or adding
synthetic conversational turns solely for accounting.

### Verified resolution

Compaction captures the last cumulative usage snapshot once per provider
attempt, including reported usage on failed attempts, and appends branch-local
`provider_usage_v1` metadata. Memory and SQLite aggregates include it without
creating conversation turns or context-occupancy events. Automatic compaction
charges the admitted goal, including between-turn work; crossing a budget stops
normal continuation and invokes the existing budget-completion path. Manual
compaction charges only the session. Accounting failures remain fatal instead
of being hidden by local fallback and emit a terminal compaction error event.
Permanent tests verify 195 tokens / USD 0.30, cumulative snapshots, both
automatic boundaries, manual retries, budget status, forks, reopen, and failure
handling. Full tests and agent/session race checks pass. See `docs/goals.md`
and `docs/session-storage-internals.md`.

## BUG-035: Full process log buffers shift retained output on every small write

- **Status:** Resolved; verified 2026-09-05
- **Severity:** Performance
- **Surface:** Managed subprocess stdout/stderr capture
- **Observed:** 2026-09-05 performance audit of revision `4d35f52`

Once the retained log buffer is full, `outputRing.Write` at
`internal/process/output.go:37-38` shifts its surviving contents for every
write smaller than the retention cap. With the default 1 MiB cap, a 4 KiB
write copies approximately 1 MiB while holding the output mutex.

A one-file candidate with 12 added and two removed lines advances the data
slice and occasionally compacts it into reusable storage with 25% spare
capacity. Reads, cursors, notification, and the retained-byte cap remain
unchanged. This trades 256 KiB of reserved capacity per full default buffer
for far fewer copies; it is not a free memory optimization.

Three-sample median write times on Go 1.27rc3 / macOS arm64 / Apple M3 Pro,
with a full 1 MiB buffer, `-cpu=1 -benchtime=300ms`:

| Incoming chunk | Current | Candidate | Speedup |
| --- | ---: | ---: | ---: |
| 64 bytes | 23.647 us | 0.070 us | 338x |
| 4 KiB | 25.049 us | 0.599 us | 41.8x |
| 32 KiB | 24.943 us | 3.742 us | 6.7x |

These are steady-state capture-helper measurements, not subprocess or agent
turn speedups. At a smaller 64 KiB cap with 32 KiB writes the candidate showed
no gain, so retention/chunk size matters. Per-write notification still costs
one allocation; the candidate does not address it.

A separate local benchmark captured 32 MiB from `head -c 33554432 /dev/zero`
through `os/exec` stdout/stderr wired to the output ring, as in the runtime.
Including subprocess startup, pipe transfer, buffer growth, and cleanup,
three-sample median time fell from 47.679 ms to 26.744 ms (44% less time).
Total allocated bytes increased from 5,577,096 to 6,901,426 for that capture,
including the one-time reusable-storage allocation. This isolates output
capture; it does not predict the speedup of a build or agent turn.

The candidate passed the complete process package race suite and a randomized
5,500-write oracle across five capacities, including zero-length writes,
oversized writes, byte contents, cursors, full reads, and notifications.
Permanent regressions and a benchmark should accompany adoption.

### Verified resolution

Adopted reusable sliding storage with permanent write, cursor, notification,
and repeated-compaction regressions. Final three-sample full-buffer 4 KiB
writes improved 44.34x. Complete 32 MiB local subprocess capture took 41% less
time (45.27 to 26.56 ms), with 24% more total allocated bytes. The live heap
probe confirms about 256 KiB additional memory per full default buffer;
whole-process capture peak RSS rose 8%. This is an explicit speed/memory
tradeoff. The full process suite and race checks pass. Raw evidence and
reproduction are in `docs/runtime-fixes-performance.md`.

## BUG-036: Terminal sanitization allocates copies for already-safe text

- **Status:** Resolved; verified 2026-09-05
- **Severity:** Performance
- **Surface:** TUI text/thinking/plan deltas, labels, and bounded previews
- **Observed:** 2026-09-05 performance audit of revision `4d35f52`

`sanitizeTerminalTextLimit` at `internal/tui/tools_info.go:226` always builds a
new string for nonempty output. Ordinary text needs no transformation. A
five-line fast path returns the original string only when it fits the byte
limit and contains neither a disallowed control nor a replacement rune.
All other input uses the existing sanitizer, including malformed UTF-8.

Three-sample medians, `-cpu=1 -benchtime=200ms`, on the same machine as BUG-035:

| Input | Current | Candidate | Current B/op | Candidate B/op |
| --- | ---: | ---: | ---: | ---: |
| 20-byte ordinary delta | 148.8 ns | 35.1 ns | 24 | 0 |
| 4,400-byte ordinary text | 26.667 us | 8.153 us | 4,864 | 0 |
| 3,900-byte Unicode text | 21.982 us | 6.845 us | 4,096 | 0 |

Control-heavy input remained effectively unchanged. These measurements cover
sanitization, not provider latency or complete TUI rendering. This is separate
from the already-adopted process-output builder reservation in BUG-025.

The candidate and BUG-037 together passed the full TUI race suite and byte-for-
byte differential checks over 10,010 inputs at eight limits, plus rendered
preview/diff cases. Keep control removal, Unicode behavior, and byte-limit
regressions when adopting the fast path.

### Verified resolution

Adopted the safe-text return with permanent differential and benchmark
coverage. Final three-sample safe 4,400-byte text improved 3.04x and eliminated
4,864 B/op; 20-byte deltas improved 3.60x with zero allocation. Control-heavy
input measured 7% slower with unchanged allocation volume, which is retained
in the report rather than treated as a gain. Full TUI tests and race checks
pass. See `docs/runtime-fixes-performance.md` for all results and scope.

## BUG-037: Short display previews decode entire long strings into runes

- **Status:** Resolved; verified 2026-09-05
- **Severity:** Performance
- **Surface:** TUI rune-limited labels, tool previews, and subagent summaries
- **Observed:** 2026-09-05 performance audit of revision `4d35f52`

`truncateRunes` at `internal/tui/view.go:928` converts the complete input to a
rune slice before retaining a short prefix. A candidate with 11 added and
seven removed lines stops scanning when truncation is established and converts
only the needed prefix. It preserves the existing ellipsis, one-rune-limit,
short-string, and malformed-UTF-8 behavior.

Three-sample medians for a 120-rune limit, using the BUG-036 benchmark settings:

| Input | Current | Candidate | Current B/op | Candidate B/op |
| --- | ---: | ---: | ---: | ---: |
| 4 KiB ASCII | 9.986 us | 0.990 us | 16,640 | 736 |
| 128 KiB ASCII | 290.594 us | 0.984 us | 524,544 | 736 |
| Approximately 128 KiB Unicode | 300.705 us | 2.239 us | 180,992 | 1,248 |

The large ratios isolate truncation of long inputs; short strings improved
only from 43.1 ns to 26.5 ns. Combining this candidate with BUG-036 reduced
the actual `renderToolOutputPreview` benchmark for 45 ordinary result lines
at width 120 from 31.046 us to 12.417 us, and from 7,856 to 3,920 B/op.
The complete TUI race suite and the differential checks described in BUG-036
passed with both candidates. Add permanent bounded-prefix coverage on adoption.

### Verified resolution

Adopted bounded-prefix scanning with permanent original-behavior parity
coverage. Final three-sample 4 KiB truncation improved 9.58x and reduced
16,640 to 736 B/op. With BUG-036, an ordinary 45-line rendered tool preview
improved 2.30x and reduced 7,856 to 3,920 B/op. Full TUI tests and race checks
pass. Larger helper-only ratios, raw samples, and reproduction commands are
recorded in `docs/runtime-fixes-performance.md`.
