# Render Snow Desktop as a trustworthy coding conversation

Written against: `10ad391e7c33a2248809cfe3e4cfd44f159d6063` plus the current uncommitted `desktop/` implementation and the user-supplied populated-window screenshot.

## Evidence chain

- Surface: the populated Snow Desktop conversation rendered by `desktop/src/workspace/view.rs`, including streamed and restored assistant messages and the compact top bar.
- Problem: assistant content is rendered as one plain string, so headings, lists, inline code, fenced code, and language-specific source blocks have no coding-oriented presentation. The same screenshot also shows `Current project` even though the desktop was launched with `SNOW_PROJECT=..` against `snow-core`, so the visible project identity is not the authoritative runtime working directory.
- Design evidence: `design-plans/desktop-agent-workspace.md` makes the centered transcript the dominant product surface and requires the top bar to identify the project. `desktop/README.md` explicitly records Markdown rendering as deferred. The rendered screenshot proves both the plain-text assistant surface and incorrect project label. `gpui-component 0.5.1`, already present in `desktop/Cargo.toml` and `desktop/Cargo.lock`, owns `gpui_component::text::TextView::markdown`, GFM parsing, syntax highlighting, selectable text, `TextViewStyle`, and `code_block_actions`; its existing implementation already depends on the locked `markdown 1.0.0` crate, so Snow does not need a parallel parser or a new dependency.
- Owner: `desktop/src/workspace/view.rs` owns assistant and top-bar presentation; `desktop/src/workspace.rs` owns runtime state; `desktop/src/snow/protocol.rs` owns the desktop projection of RPC `session_info`; `desktop/src/snow/client.rs` delivers that projection; `pkg/protocol.RPCSessionInfo.CWD` is the authoritative server-side working directory.
- Scope and affected surfaces: live assistant messages, restored assistant messages, code-block actions, streamed partial Markdown, top-bar project identity, focused desktop tests, and `desktop/README.md`.
- Uncertainty: `TextView::markdown` reparses asynchronously while text streams. Native validation must cover unfinished fences, rapidly appended deltas, long code lines, large completed messages, light/dark theme highlighting, and transcript scrolling before the plain renderer is removed.

## Design decision

Use the Markdown and syntax-highlighting owner already shipped by `gpui-component` for assistant text only. Preserve user messages as literal user-authored text and preserve system rows as status presentation. Make Markdown selectable and add one compact Copy action per fenced code block using `TextView::code_block_actions`; do not create a second Markdown AST, syntax theme, or code-block component. Derive the project label from the absolute `cwd` returned by Snow's `session_info`, falling back to the existing runtime configuration only before session state is available.

## Reuse

- `gpui_component::text::TextView::markdown` for GFM parsing and rendering.
- `gpui_component::text::TextViewStyle` and the active theme's existing highlight theme for typography and syntax colors.
- `TextView::selectable(true)` for native text selection.
- `TextView::code_block_actions` and `CodeBlock::code()`/`CodeBlock::lang()` for bounded code-block chrome and copying.
- Existing `Button`, `ClipboardItem`, `theme.secondary`, `theme.border`, `theme.foreground`, and `theme.muted_foreground` owners for the Copy action and code-block surface.
- Existing append-only `ChatMessage` projection and transcript `ScrollHandle`; Markdown changes presentation, not session persistence or provider content.
- `pkg/protocol.RPCSessionInfo.CWD` as the authoritative project path already emitted by the RPC server.
- Exemplar: the current assistant identity row in `desktop/src/workspace/view.rs`; retain its avatar, Snow label, streaming indicator, readable width, and role separation.

No new dependency or shared primitive is required. The existing component library already owns the parser, AST rendering, highlighting, and code block model.

## Changes

1. `desktop/src/workspace.rs` and `desktop/src/workspace/view.rs` — pass the native window to assistant rendering
   - Change: retain `window` in `impl Render for Workspace` and thread `&mut Window` through `render_workspace`, `render_transcript`, and `render_message`, because `TextView::markdown` requires keyed window state.
   - Change: give every assistant Markdown view a stable key derived from its append-only message index (for example `("assistant-markdown", index)`) so streamed updates reuse one `TextViewState` rather than allocating a new parser state per delta.
   - Preserve: existing transcript width, scroll tracking, role layout, streaming label, automatic bottom scrolling, and message-order behavior.
   - Verify: multiple assistant messages keep independent renderer state and new deltas update only the active assistant message.

2. `desktop/src/workspace/view.rs` — replace assistant plain text with the existing Markdown renderer
   - Change: replace only the assistant message body's `div().child(message.text.clone())` with `TextView::markdown`, configure it as selectable and non-scrollable so the transcript remains the only vertical scrolling owner, and style it using existing theme/component tokens.
   - Change: use the component's GFM support for paragraphs, headings, ordered/unordered lists, task lists, block quotes, links, thematic breaks, tables, inline code, and fenced code blocks. Do not preprocess provider text or reinterpret user/system messages as Markdown.
   - Change: keep paragraph and heading scale restrained within the existing `CONVERSATION_WIDTH`; code blocks may scroll or wrap according to the existing component behavior but must not expand the whole window beyond the conversation width.
   - Preserve: exact provider text, provider-private continuity exclusion, history filtering to public text/plan blocks, and progressive text delta display.
   - Verify: a single response containing prose, nested lists, inline code, a quote, a table, and several fenced languages remains readable in both themes and at the 800 px minimum window width.

3. `desktop/src/workspace/view.rs` — add deterministic Copy actions to fenced code blocks
   - Change: configure `TextView::code_block_actions` to render one compact `Copy` button in each fenced block. Copy exactly `CodeBlock::code()` through GPUI's existing clipboard owner; do not include the fence or language marker.
   - Change: generate each action's element ID from the owning message index plus a stable hash of the block language and code, avoiding code text as an unbounded element ID.
   - Change: keep the language label supplied by `CodeBlock::lang()` visible through the existing component presentation when present; do not infer a language when the fence omits one.
   - Preserve: text selection and normal transcript scrolling.
   - Verify: two identical code blocks in different messages have independent working buttons; a block without a language can still be copied; copied text exactly matches its source.

4. `desktop/src/snow/protocol.rs`, `desktop/src/workspace.rs`, and `desktop/src/workspace/view.rs` — show the authoritative project name
   - Change: add the already-emitted `cwd` field to the desktop `SessionInfo` projection with a default for additive/backward-safe decoding.
   - Change: retain the latest non-empty session `cwd` in `ChatState` when `RuntimeEvent::SessionLoaded` is applied. Make `project_label` use the final normal path component from this authoritative absolute working directory; use the existing runtime-config fallback only while session state is not available.
   - Change: if neither source yields a name, retain the current `Current project` fallback rather than showing a raw empty path or `..`.
   - Preserve: the full working directory remains private to runtime state; the top bar displays only its final component.
   - Verify: `cwd=/Users/example/snow-core` renders `snow-core`; launching with `SNOW_PROJECT=..` no longer renders `Current project` after `session_info` arrives; empty/malformed additive data preserves the fallback.

5. `desktop/src/workspace_tests.rs` and relevant desktop protocol tests — cover the new projections and presentation inputs
   - Change: extend `SessionInfo` fixtures with `cwd` and add focused tests for authoritative project-name extraction, empty fallback, and provider reconnection updating the retained working directory.
   - Change: add pure coverage for deterministic code-block action IDs and exact copy payload extraction where possible without constructing a native window. Do not duplicate `gpui-component`'s Markdown parser tests.
   - Change: preserve existing streaming/coalescing tests and add a state-level case with incomplete then completed fenced Markdown to prove accumulated assistant source remains exact across deltas.
   - Verify: tests assert source preservation and project identity, while native smoke checks assert rendering.

6. `desktop/README.md` — replace the deferred Markdown claim with the verified contract
   - Change: document assistant Markdown, syntax-highlighted fenced code, text selection, code copying, streamed partial rendering, and the fact that user messages remain literal.
   - Change: document that the compact header derives its project label from RPC `session_info.cwd`.
   - Preserve: rich historical tool cards, images, thinking blocks, branch controls, and session picker remain deferred unless separately implemented.
   - Verify: documentation does not claim arbitrary HTML execution, image loading, or unsupported code languages.

## Scope

- Inherit: all live and restored assistant text/plan content rendered by the one Snow Desktop workspace.
- Verify: fake provider, OpenCode Go, ChatGPT, Compatible provider; empty, streaming, completed, failed, restored-history, dark-theme, light-theme, minimum-width, long-line, and multiple-code-block states.
- Exclude: user-message Markdown, arbitrary HTML execution, remote image loading, provider-private reasoning display, historical tool reconstruction, diff review, permissions, sessions, and changes to Go session storage or event normalization.

## Validation

- Product: prompt Snow for a response containing headings, lists, inline code, Rust/Go/JSON fences, a language-less fence, and a table; expect structured selectable output and exact Copy actions without changing message content.
- Interface: inspect empty, sparse, long, streaming incomplete-fence, completed, restored-history, dark/light, and 800 px-wide states; the composer remains anchored and the transcript remains the only vertical scrolling owner.
- System: confirm `gpui_component::text::TextView` remains the single Markdown/highlighting owner and no parser/highlighter dependency or duplicate code-block component is added.
- Repository: `cd desktop && cargo fmt --check && cargo check && cargo test && cargo clippy --all-targets --all-features -- -D warnings` → all pass.
- Repository: `cd desktop && SNOW_TEST_BINARY=../snow cargo test --test rpc_integration -- --ignored` → real Snow history and streaming checks pass.
- Repository: `git diff --check` → no whitespace errors.

## Stop conditions

- Stop if `TextView::markdown` cannot update one keyed view safely during streamed partial Markdown; preserve exact plain streamed text until an existing component-compatible update path is proven.
- Stop if the component executes arbitrary HTML, fetches remote content automatically, or exposes provider-private data; constrain or reject those nodes before shipping.
- Stop if code blocks introduce a nested vertical scroll owner or force the transcript beyond `CONVERSATION_WIDTH`.
- Stop if the RPC `cwd` is absent or not the authoritative active agent working directory; do not guess the project name from a relative `..` string.
- Stop if implementation requires a second Markdown/parser/highlighter dependency; use the already locked component owner or narrow the milestone.

## Design documentation

- After acceptance and native validation: update `desktop/README.md` with the rich assistant rendering contract and authoritative project-label source.
- After acceptance and native validation: update the deferred-capabilities paragraph in `design-plans/desktop-agent-workspace.md` only if that document remains the accepted desktop milestone record.
