# TUI performance

Snow's terminal interface runs on Bubble Tea v1.3.10 and Bubbles v1.0.0, with
Lip Gloss for frame styling and Glamour for Markdown rendering. This guide
records the renderer contract that `internal/tui` must preserve: one owner of
the alternate screen, bounded streaming work, and constant-time status updates.
It is for maintainers changing TUI layout, rendering, or lifecycle code; product
usage guidance lives in [Using Snow](using-snow.md).

## On this page

- [Goals](#goals)
- [Pinned dependencies](#pinned-dependencies)
- [Upstream examples consulted](#upstream-examples-consulted)
- [Render rules](#render-rules)
- [Verification](#verification)
- [Related documents](#related-documents)

## Goals

- Responsive rendering keeps streaming text cheap to draw; finalized content
  receives full Markdown rendering only at a boundary.
- Bounded work caps each update at 256 logical events, bounds the event
  mailbox bytes, and truncates tool previews.
- Streaming updates hand off losslessly and in order: lifecycle events are
  never dropped or reordered while adjacent stream deltas coalesce.

## Pinned dependencies

The versions below are load-bearing and must not drift during refactors.

| Package | Version | Purpose |
|---|---|---|
| `github.com/charmbracelet/bubbletea` | `v1.3.10` | Alternate-screen program loop, `WindowSizeMsg`, mouse reporting |
| `github.com/charmbracelet/bubbles` | `v1.0.0` | Transcript `viewport.Model`, textarea, spinner |
| `github.com/charmbracelet/lipgloss` | `v1.1.1-0.20250404203927-76690c660834` | Frame styling and width-aware layout |
| `github.com/charmbracelet/glamour` | `v1.0.0` | Markdown-to-ANSI rendering for finalized transcript content |

## Upstream examples consulted

Bubble Tea's pager example composes a header, `viewport.Model`, and footer in
one `View`, sizes the viewport from `WindowSizeMsg`, enters the alternate
screen, and optionally enables cell-motion mouse reporting for wheel input. Its
chat example uses the same app-owned viewport plus a textarea. Snow follows this
pattern.

Bubble Tea also provides `tea.Println` for unmanaged normal-screen output, but
its renderer documentation says those lines print above the managed program.
That is suitable for logs above a small program, not for combining immutable
history with a terminal-height sticky frame. A terminal-height normal-screen
frame plus `tea.Println` causes prior frames, including headers and composer
chrome, to enter terminal scrollback. Snow therefore does not use its historical
inline/`tea.Println` path at runtime.

> **Note:** Context7 currently returns Bubble Tea v2 examples as well. Snow
> remains on v1, so `View() string`, `tea.WithAltScreen`, and direct Bubbles
> width/height fields stay correct until an intentional v2 migration.

## Render rules

### Viewport ownership

One renderer owns the window. Runtime always enters the alternate screen with
`tea.WithAltScreen` and a 120 FPS program ceiling for pointer-rate drag
feedback. `View` composes a sticky header, the transcript viewport,
overlays/run status, the composer, and the footer. Scrolling is confined to the
transcript viewport and cannot reveal stale rendered frames.

Sizing comes from `WindowSizeMsg`: header, footer, composer, and overlay heights
are subtracted from terminal height, and the remainder is assigned to the
Bubbles viewport. The final terminal column is left unused to avoid physical
autowrap artifacts.

### Mouse and native mode

Mouse mode owns viewport scrolling. `tui.mouse` defaults to `true` so wheel and
trackpad gestures stay inside Snow instead of moving terminal scrollback.
Cell-motion reports also drive transcript highlighting/copy and edge
auto-scroll.
Apple Terminal provides Fn-drag as its terminal-native selection override. A
reported right-click opens Snow's bounded **Copy selection** context menu
without changing mouse reporting, preserving viewport wheel ownership. F6 toggles
explicitly. In native mode, wheel behavior belongs to the terminal and may move
its scrollback. This
split reflects the protocol: portable native drag/context menus and application
wheel events cannot coexist.

### Streaming and coalescing

Agent callbacks enter an ordered mailbox. Adjacent text, thinking, and plan
deltas coalesce, and updates consume at most 256 logical events. The common
root/attributed delta envelope uses explicit scalar correlation instead of
reflectively boxing the complete event for every token. Root session snapshots
and same-turn usage/model snapshots coalesce in-place within one UI batch;
debug usage history and child events remain complete. Lifecycle, interaction,
queue, and tool-progress events are not dropped or reordered.

Ordinary live deltas schedule one refresh on a bounded cadence: 33 ms for
content at or below 64 KiB, 75 ms above 64 KiB, 150 ms above 256 KiB, and
300 ms above 1 MiB. Lifecycle boundaries flush immediately.

### Formatting cache and scroll intent

Stable transcript rendering is cached by content and width. Markdown and
thinking renderers are reused; streaming text stays cheap and receives final
Markdown rendering at a boundary. The normal Bubbles viewport string is reused
until content generation, scroll offset, width, or height changes. The final
managed-frame fit is also reused only when its exact composed input and
terminal dimensions match, avoiding repeated ANSI width/grapheme parsing for
status updates that do not change the frame. Retained viewport strings and the
frame input/output pair are each capped at 256 KiB; larger PTYs render without
those caches. Content publication, resize,
scroll, selection, and session replacement invalidate the relevant cache.

New output follows only when the viewport is already at bottom. While the user
reads earlier content, source state keeps updating without replacing the
snapshot; returning to bottom catches up once.

### Async domain work

`Update` mutates domain state and schedules commands; `View` performs no domain
I/O and mutates only exact render caches while composing strings. Provider,
file, session, and model discovery work runs asynchronously with
generation-tagged results. Composer hints use the cached active branch identity
rather than rich branch listings, and the spinner timer is armed only while an
animation is visible.

Interaction state stays explicit. Correlated root turn IDs prevent delayed
events from settling newer runs. Automatic compaction retains goal ownership.
Blocking permission and user-input requests exclusively own the overlay and
keys.

### Bounded output

Tool progress and previews are sanitized and capped; complete results remain in
session and protocol data. The live subagent fleet inspector retains at most 128
activity rows or 32 KiB per observed child, and 24 recent transcript messages.
Authoritative lists and selected transcripts are fetched only by asynchronous,
generation-guarded commands; `View` and fleet key navigation consume in-memory
snapshots and never access session storage.

### Transcript controls

PageUp/PageDown, Home/End, and Ctrl+Up/Ctrl+Down always update the transcript
viewport. In the default mouse mode, the wheel scrolls and drag selects
ANSI/grapheme-aware transcript cells; releasing writes the host clipboard with
OSC 52 fallback and detected tmux/screen passthrough. Apple Terminal users can
Fn-drag for zero-lag
native selection. Double-click selects a word, triple-click a line, and edge
dragging continues through off-screen rows. Right-click opens the in-frame
**Copy selection** menu; mouse click, Enter, or `c` copies through the host
clipboard (`pbcopy`/available Linux utility, then OSC 52 fallback), while Esc,
outside click, or wheel dismisses it. Viewport mouse reporting remains enabled. F6 toggles
reporting explicitly.

The viewport follows new output only while already at bottom, and active
application selections freeze their source snapshot. Keyboard viewport scrolling
remains available in native mouse mode.

Bubble Tea v1 can expose fragmented SGR mouse reports as text in some terminals,
so Snow retains defensive reconstruction before input reaches the textarea.
Pasted mouse-looking text remains literal.

Composer editing has a dedicated hot path: ordinary typing and deletion skip
submission-only image, queue, goal, and whitespace processing. Once a pasted
composer value already requires the six-row maximum, a bounded grapheme scan
avoids re-wrapping the complete value merely to recompute its height. The
`BenchmarkComposerBackspace` benchmark covers short, 8 KiB, and 64 KiB inputs.

### Tool and run presentation

`tool_start`, `tool_progress`, and `tool_end` share the same correlated event
stream used by every surface. Tool completion does not unlock the composer;
`turn_done`, abort, or a terminal goal boundary does. During active work, a
measured status row shows elapsed time, queued input count, and the interrupt
hint.

Root `session_updated` events are idempotent invalidations. Snow coalesces
bursts inside each UI batch and never reloads the complete SQLite branch while
a turn is live; provider `usage` events update the context counter in constant
time. Durable `run_stats_updated` events schedule an asynchronous lightweight
branch-statistics refresh as turn and step markers are appended. A usage-less
terminal boundary schedules one asynchronous projected-context refresh, so
SQLite decoding cannot block keyboard handling. Idle hydration remains available for external session mutations, while
explicit context refreshes read the projected branch only once. Built-in stores
first scan a lightweight root-to-tip hydration projection containing exact row
counts, input-history state, plan/context scalars, durable IDs, and tool-call
identifiers. The TUI then requests visible message blobs in pages of at most 256
and decodes only the newest 1,999 full-screen rows (or 2,000 inline segments),
plus focused legacy tool-call lookbehind. The omission count, partial boundary
row, complete composer history, latest plan, compaction usage, and branch-prefix
semantics remain exact. Custom stores retain the complete-history fallback.
This keeps long, tool-heavy sessions from blocking keyboard handling with
repeated full-history JSON decoding.

## Verification

For layout or lifecycle changes, run the following from the repository root,
substituting the changed file names:

```sh
gofmt -w internal/tui/<changed-files>.go
go test ./internal/tui -count=1
go test -race ./internal/tui -count=1
go test ./...
go vet ./...
```

Manual checks should cover wheel and keyboard scrolling,
drag/double/triple-click selection and clipboard copy, edge auto-scroll, the F6
native-selection fallback, streaming while scrolled away, resize, long composer
input, modal replacement, abort, and clean alternate-screen restoration on exit.

## Related documents

- [Using Snow](using-snow.md)
- [Configuration](configuration.md)
- [Sessions](sessions.md)
- [Architecture and roadmap](../IMPLEMENTATION.md)
- [README](../README.md)
