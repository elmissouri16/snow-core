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
- **Observed:** Current desktop-improvement session

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

- editing desktop Rust source and test files;
- running `cargo fmt`, which rewrote source files;
- running `./scripts/install-local.sh`, which replaced the locally installed
  Snow binary; and
- stopping and restarting the managed desktop process.

The behavior occurred across multiple turns, so it was not limited to one
accidental tool selection. This entry records the mode-enforcement defect; it
does not assert that the desktop changes themselves were incorrect.

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

## BUG-002: Desktop tool activity dominates the transcript

- **Status:** Resolved
- **Severity:** Low
- **Surface:** Desktop transcript presentation
- **Observed:** Two user-provided desktop screenshots

### Expected behavior

Tool-heavy work should read as one quiet activity step in the conversation.
Consecutive tool-only segments should be collapsed by default, and expanding
them should reveal small, borderless rows. Copy actions should use accessible
icon buttons instead of persistent text labels.

### Actual behavior and evidence

The original cards used separate header and summary rows with large padding. An
initial fix reduced each card to one row, but every tool call still rendered as
a full-width bordered surface with repeated **Completed**, **Show details**,
**Copy input**, and **Copy output** labels. The transcript also retained normal
24 px message spacing between tool-only rows, so long tool sequences remained
visually dominant. A follow-up replacement used `IconName::Copy`, but the app
had not registered an asset source, so GPUI reserved the icon button without
being able to load or paint `icons/copy.svg`.

### Impact

Tool-heavy turns displace the actual conversation, require excessive scrolling,
and make the transcript read like a control panel rather than a clean record of
the exchange.

### Reproduction

1. Open a desktop session containing several consecutive completed tool calls.
2. Leave every tool detail surface closed.
3. Observe one bordered row and a large message gap for every call, with three
   persistent text actions repeated on the right.

### Remediation

The transcript now coalesces consecutive tool-only messages into one collapsed
activity disclosure such as **Worked for 9.8s** or **Used 3 tools**. Intermediate
virtual-list rows render at zero height and the visible activity row uses only
4 px trailing spacing. Expanding the disclosure shows compact borderless tool
rows; completed labels are omitted, summaries remain bounded to one line, and
detail/input/output actions use chevron and copy icons with tooltips. Message,
code-block, system-message, and auxiliary resource copy actions also use compact
clipboard icons instead of persistent **Copy** labels. The desktop application
now registers `gpui_component_assets::Assets` before component initialization,
so copy and chevron SVGs resolve and paint. Fenced details remain bounded in
their fixed-height internal scroller.

### Regression coverage

- `tool_activity_uses_one_compact_transcript_label` covers running, elapsed,
  failed, and duration-free labels.
- `consecutive_tool_messages_render_and_invalidate_as_one_activity_row` verifies
  coalescing and maps updates to the one visible virtual-list row.
- `collapsed_tool_card_summaries_are_single_line_and_bounded` keeps expanded
  tool rows from growing on multiline or oversized summaries.
- `app::tests::bundled_assets_include_copy_icon` verifies the registered asset
  bundle contains non-empty `icons/copy.svg` data.

### Resolution evidence

Verified on 2026-09-01 with:

- `cargo fmt -- --check`
- `cargo test -q` (314 unit tests and 10 integration tests passed; 4 integration
  tests remained intentionally ignored)
- `git diff --check`
- `./scripts/install-local.sh`
- a successful rebuilt desktop launch reaching
  `Running target/debug/snow-desktop`

## BUG-003: Desktop session lists stutter while scrolling

- **Status:** Resolved
- **Severity:** Medium
- **Surface:** Desktop session sidebar and management panel
- **Observed:** User report and source-path inspection

### Expected behavior

Scrolling an already loaded session inventory should remain local to the GPUI
client and track wheel or trackpad input smoothly, independent of RPC latency.

### Actual behavior and evidence

Both session surfaces used GPUI's variable-height `list` despite every sidebar
row being exactly 40 px and every management row being exactly 64 px. The list
therefore retained per-item measurement and overdraw work. Its item callback
also re-entered the `Workspace` entity separately for every visible and overdraw
row, rebuilding strings and handlers in each update closure during scrolling.
No RPC request is issued by the scroll path.

### Impact

The UI thread performs unnecessary layout and entity-borrow work on every
scroll frame. Larger inventories make wheel and trackpad movement feel delayed
or uneven even though session data is already resident in memory.

### Reproduction

1. Load a project with enough sessions to overflow the sidebar or `/sessions`
   panel.
2. Wait for `sessions_list` to complete.
3. Scroll continuously and observe frame stutter without any corresponding RPC
   command or network activity.

### Remediation

Both surfaces now use GPUI's fixed-height `uniform_list` with independent
`UniformListScrollHandle`s. GPUI measures one row and lazily places the visible
range, while Snow enters `Workspace` once per visible range rather than once per
item. The old variable-list reset synchronization and 256 px measurement
overdraw are removed; existing scrollbars remain attached to the new handles.

### Regression coverage

The full desktop unit and RPC integration suites cover session inventory,
selection, mutation, and rendering compilation. The optimization uses GPUI's
uniform-list contract directly; both row renderers retain explicit fixed
heights required by that contract.

### Resolution evidence

Verified on 2026-09-01 with:

- `cargo fmt -- --check`
- `cargo test -q` (313 unit tests and 10 integration tests passed; 4 integration
  tests remained intentionally ignored)
- `git diff --check`
- `./scripts/install-local.sh`
- a successful rebuilt desktop launch reaching
  `Running target/debug/snow-desktop`

## BUG-004: Closing a searchable desktop picker strands keyboard focus

- **Status:** Resolved
- **Severity:** High
- **Surface:** Desktop provider and model pickers
- **Observed:** Source-path audit and focused interaction review

### Expected behavior

Closing a searchable provider or model popover should move keyboard focus to a
visible, relevant workspace target. Conversation pickers should return to the
composer, while the settings model picker should return to the visible settings
workspace.

### Actual behavior and evidence

Provider and model pickers explicitly focused their shared search input when
opened. Their close and row-activation paths then unmounted the picker without
assigning focus elsewhere. Subsequent typing could remain directed at the
unrendered search input, making the conversation composer appear unresponsive;
the settings picker similarly lost predictable keyboard navigation.

### Impact

Keyboard users could lose their place after dismissing or selecting a searchable
picker. In the conversation workspace, the next typed prompt appeared to be
ignored because text was still sent to a hidden input.

### Reproduction

1. Open the Provider or Model picker from the conversation composer.
2. Type a search and select a result, or dismiss the popover.
3. Begin typing a prompt without clicking the composer first.
4. In the affected implementation, the visible composer did not receive the
   input. The equivalent settings flow could leave focus on the unmounted search
   input rather than the settings workspace.

### Remediation

Picker closing is centralized through one focus-restoration path. It clears the
shared search field and projects the correct visible destination: the composer
when it is editable, the tracked settings workspace while settings are open,
or no focus change while a blocking interaction owns input. Mouse dismissal,
keyboard dismissal, and provider/model row activation now use this behavior.

### Regression coverage

Pure projection coverage verifies each close destination, including the
settings-over-composer precedence. The affected GPUI targets compile through all
desktop targets, and the desktop unit and mocked RPC integration suites pass.

### Resolution evidence

Verified on 2026-09-01 with:

- `cargo fmt --manifest-path desktop/Cargo.toml -- --check`
- `cargo check --manifest-path desktop/Cargo.toml --all-targets`
- `cargo test --manifest-path desktop/Cargo.toml`
- `git diff --check`
- `./scripts/install-local.sh`
- a successful rebuilt desktop launch reaching
  `Running target/debug/snow-desktop`

## BUG-005: Empty provider ID renders a blank composer selector

- **Status:** Resolved
- **Severity:** Medium
- **Surface:** Desktop composer provider selector
- **Observed:** User-provided native desktop screenshot

### Expected behavior

The composer selector should display the effective provider reported by Snow.
Before runtime discovery completes or when no provider is selected, it should
show the explicit placeholder `Choose provider`.

### Actual behavior and evidence

The desktop initialized its displayed provider only from the optional CLI
override. When Snow selected its canonical configured provider, the runtime
reported that provider in model/session state but the workspace did not adopt
it. The remaining empty ID was also treated as user-visible, so label projection
returned an empty fallback and left only the selector's disclosure chevron.

### Impact

The unlabeled control was ambiguous and appeared visually broken. Users could
not tell that it opened the provider selector without activating it.

### Reproduction

1. Start the desktop with no selected provider.
2. Inspect the first selector in the composer footer.
3. In the affected implementation, the selector rendered only its disclosure
   chevron rather than `Choose provider`.

### Remediation

Accepted model/session runtime state now updates the workspace's effective
provider when no CLI override was needed, allowing the selector to resolve its
catalog display name. The provider visibility predicate also rejects empty and
whitespace-only IDs in addition to Snow's internal `fake` adapter, preserving
`Choose provider` as the safe unresolved-state placeholder.

### Regression coverage

Provider-catalog tests verify that empty and whitespace-only IDs are not
user-visible and resolve to `Choose provider`. Runtime-config projection tests
verify that Snow's effective model-catalog provider is adopted without a CLI
override.

### Resolution evidence

Verified on 2026-09-01 with:

- `cargo test --manifest-path desktop/Cargo.toml provider_catalog --lib`
- `cargo check --manifest-path desktop/Cargo.toml --all-targets`
- `cargo test --manifest-path desktop/Cargo.toml`
- `git diff --check`
