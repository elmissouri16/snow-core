# TUI responsiveness and rendering guide

`snow` uses the Charmbracelet stack pinned in `go.mod`:

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) `v1.3.10` — MVU runtime,
  messages, commands, renderer, terminal resize events.
- [Bubbles](https://github.com/charmbracelet/bubbles) `v1.0.0` — viewport,
  textarea, and spinner components.
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) — styling, sizing, and
  wrapping.
- [Glamour](https://github.com/charmbracelet/glamour) — cached Markdown rendering.

## Official patterns used

The implementation follows the current upstream examples and component APIs:

- Bubble Tea's [Model lifecycle](https://github.com/charmbracelet/bubbletea/blob/main/tea.go):
  `Init` starts commands, `Update` owns state transitions, and `View` only
  describes the current frame.
- `WindowSizeMsg` is handled centrally before layout is recalculated.
- External streams use the
  [command-fed event pattern](https://github.com/charmbracelet/bubbletea/tree/main/examples):
  a command waits on Snow's coalescing mailbox, delivers one bounded batch, and
  `Update` immediately re-arms it.
- `tea.Batch` is used for independent startup/spinner commands. Ordered work
  should use `tea.Sequence`, not an unordered batch.
- Bubbles' [viewport](https://github.com/charmbracelet/bubbles/blob/main/viewport/viewport.go)
  uses `SetContent`, `AtBottom`, and `GotoBottom`; content updates should not
  force the user back to the tail after they scroll up. Snow uses the current
  `PageUp`/`PageDown` APIs rather than the deprecated `ViewUp`/`ViewDown` aliases.
- Bubbles' `help.Model` supplies width-aware footer shortcuts, while the TUI
  keeps picker navigation in one shared key map (`↑/↓` or `j`/`k`).

> Upstream Context7 results also include Bubble Tea v2 examples. This project is
> on Bubble Tea v1, so keep the v1 `View() string` and direct Bubbles width/height
> fields until an intentional dependency upgrade is made.

## Snow's rendering rules

1. **Coalesce stream bursts without blocking producers.** Agent callbacks push
   into an ordered mailbox rather than a fixed-capacity channel. Adjacent text,
   thinking, and same-plan deltas coalesce as string parts, materialize once,
   and reach `Update` in batches of at most 256 logical events. Lifecycle events
   are never dropped or reordered.
2. **Render on a separate cadence.** Ingestion updates model state immediately,
   while ordinary deltas schedule at most one viewport flush. The interval
   adapts from about 33 ms below 64 KiB live text to 300 ms above 1 MiB;
   lifecycle boundaries flush immediately when following the tail.
3. **Refresh only when dirty.** The TUI caches the last rendered transcript and
   skips `viewport.SetContent` when content and dimensions are unchanged.
4. **Preserve scroll intent.** New output follows the tail only when the viewport
   was already at the bottom. While the user is off-tail, Snow freezes the
   viewport snapshot and accumulates content off-screen; PageDown/End (or the
   wheel when `tui.mouse` is enabled) performs one catch-up refresh when the old
   bottom is reached.
5. **Finalize live text at visible boundaries.** Streaming assistant/thinking
   text stays in buffers until a tool, permission prompt, error, abort, or turn
   boundary is reached. The buffer is promoted before the boundary line so the
   transcript remains chronological; a post-tool response starts a new segment.
6. **Cache expensive formatting.** Assistant Markdown rendering is conditional;
   reasoning uses a separate muted Glamour renderer so inline Markdown is styled
   consistently while streaming and after session hydration. Both renderers are
   cached by source text and width and run once per coalesced update.
7. **Bound terminal work.** Tool output previews are bounded and sanitized before
   entering the transcript. Full results remain in the session/protocol path.
8. **Avoid render-side effects.** Update handlers mutate state; `View` composes
   the frame. Layout work belongs to `WindowSizeMsg` or state transitions.
9. **Keep the composer responsive.** Provider/app construction and agent work run
   through commands; the Bubble Tea event loop must not perform network, file,
   or process work directly. Mention discovery, fallback model lookup, session
   listing/open/create, and branch listing are generation-tagged commands; stale
   results are ignored after a picker closes or a newer request starts.
10. **Fit exactly inside the terminal.** Startup and ready states share one frame
   calculation based on `WindowSizeMsg`. Header, overlays, composer, and footer
   are subtracted from the transcript viewport, and the final frame is bounded
   to the reported width and height so output never creates terminal scrollback.
11. **Grow the composer only when needed.** The composer is three rows while
    empty, grows with explicit or soft-wrapped input, and caps at six rows.
    `Ctrl+V` runs Bubbles' clipboard paste command and routes its asynchronous
    result back to the textarea that requested it. Terminal-managed `Cmd+V`,
    `Ctrl+Shift+V`, and equivalent shortcuts arrive as literal bracketed paste.
    `Ctrl+J` inserts newlines while plain Enter submits. `Option+Return` is also
    accepted when a macOS terminal reports Option as Meta/Alt, including the
    split Escape-then-Return form. Additional input scrolls inside the textarea
    instead of shrinking or displacing the transcript unpredictably.
    Shift+Enter is not a distinct key in Bubble Tea v1's standard terminal key
    model, so it is not advertised as a binding.
12. **Keep active-run control visible.** While an agent prompt is running, one
    measured row above the composer shows elapsed wall time and `esc to
    interrupt`. The row disappears at `turn_done` or abort, and its height is
    included in the same exact-frame calculation as other chrome.
13. **Keep startup failures escapable.** App construction errors switch the
    header, composer, and footer into a terminal error state. `Ctrl+C` and
    `Ctrl+D` are handled before app-readiness checks so a failed or still-booting
    TUI can always leave the alt-screen cleanly.
14. **Use adaptive semantic themes.** The `tui.theme` setting supports
    `default`, `dark`, `light`, and `high-contrast`. Default uses Lip Gloss
    adaptive colors; selected/error/tool states retain text and glyph markers
    so color is never the only meaning.
15. **Prefer native selection by default.** The existing `tui.mouse` config
    defaults to `false`, so Bubble Tea does not claim the mouse and ordinary
    terminal drag selection/copy remains available. `tui.mouse: true` opts into
    cell-motion reporting for wheel/trackpad transcript scrolling; terminals
    may then require Shift or another configured override to select text. If the
    early preference read fails, Snow leaves mouse capture off and lets normal
    asynchronous startup display the configuration error.

## Tool event integration

The agent emits `tool_start`, `tool_progress`, and `tool_end` events through the
same subscription used by SDK, JSON, RPC, and TUI consumers. The TUI displays a
simple native card:

```text
▶ grep
  ↳ searching text files
✔ grep (4ms)
  │ internal/tui/tui.go:330: ...
```

Call IDs remain in protocol/session data for correlation but are intentionally
hidden from the native transcript. Tool completion does not unlock the composer;
only `turn_done` does, preventing a second prompt from racing a serial tool loop.
PageUp/PageDown, Home/End, and Ctrl+Up/Ctrl+Down move only the transcript
viewport; mouse wheel events do the same when `tui.mouse` is enabled. Bubble Tea
v1 can expose a split SGR mouse report as text, so Snow retains its defensive
complete/split report reconstruction (and fragmented Shift+Tab handling) before
the textarea. Pasted mouse-looking text remains literal, while invalid partial
reports time out and replay literally. Snow does not draw a scrollbar; a
transient scrollbar at the edge of the window is terminal-emulator chrome
rather than part of the TUI.

During an active prompt, `Esc` cancels the run just like `Ctrl+C`. Modal states
keep their focused behavior: `Esc` closes pickers/login and denies the current
permission request instead of cancelling through the modal.

## Reasoning stream presentation

ChatGPT Responses reasoning summary/text deltas use the same mailbox-fed event
path as answer text. Snow keeps their accumulated text append-only, renders it
as muted Markdown, and retains the completed `think:` block in the transcript
and resumed sessions. Completed Responses snapshots only fill a missing suffix;
they never replace or duplicate already displayed deltas.

Provider cadence is not guaranteed. While a model request is active but has not
emitted reasoning or answer text, the transcript shows an animated `thinking…`
placeholder. The placeholder is hidden during tools and permissions and returns
after a tool completes while the follow-up model response is pending.

## Verification

```sh
gofmt -w internal/tui/*.go
go test ./internal/tui -count=1
go test ./...
go test -race ./internal/...
```

When changing event batching, viewport behavior, layout, or Markdown rendering,
add a model-level test under `internal/tui/` and verify both a narrow terminal
and a normal terminal. Benchmarks cover mailbox ingestion, mention discovery,
transcript refresh with large histories, and narrow/normal `View` layout. Do not
benchmark by counting `View` calls alone: Bubble Tea invokes `View` after
updates; the expensive operation to avoid here is rebuilding/reflowing
transcript content and resetting viewport state.
