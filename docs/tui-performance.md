# TUI performance

Snow's terminal interface runs on Bubble Tea v2.0.9 and Bubbles v2.2.1, with
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
| `charm.land/bubbletea/v2` | `v2.0.9` | Alternate-screen program loop, `WindowSizeMsg`, mouse reporting |
| `charm.land/bubbles/v2` | `v2.2.1` | Transcript `viewport.Model`, textarea, spinner |
| `charm.land/lipgloss/v2` | `v2.0.6` | Frame styling and width-aware layout |
| `github.com/charmbracelet/glamour` | `v1.0.0` | Markdown-to-ANSI rendering for finalized transcript content |

The textarea component is a reproducible local snapshot of Bubbles v2.2.1 with
printable-ASCII wrapping and visible-row rendering optimizations. Its Unicode path and input lifecycle
are unchanged; the original upstream test suite is included. See the pinned
[source, patch, and sync instructions](../internal/tui/textarea/UPSTREAM.md).

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

The root model returns `tea.View`. Alternate screen, mouse capture, focus
reporting, and bracketed paste are declarative view state. Bubble Tea owns
keyboard enhancement, synchronized output (mode 2026), Unicode width (mode
2027), and terminal restoration. Unsupported terminals retain legacy input.
Bubbles textareas retain virtual cursors; Lip Gloss v2 dimensions include borders.
Glamour still uses its v1 styling dependency internally and returns ANSI strings.

## Render rules

### Viewport ownership

One renderer owns the window. Runtime always enters the alternate screen with
`View.AltScreen` and a 120 FPS program ceiling for pointer-rate drag
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

### Terminal appearance

Bubble Tea owns background queries on startup, focus, and mode 2031 appearance
notifications. Notifications trigger a query for the actual background color.
The model explicitly resolves built-in, custom, and plugin light/dark palettes,
invalidates plugin/Markdown/frame caches, and redraws durable transcript rows
without changing drafts, modal state, scroll position, or live run counters.
Repeated reports with the same light/dark classification do not rebuild content.

Mode 2031 is queried before enabling notifications. Snow enables only a supported,
reset mode, and resets it after `Program.Run` has stopped on quit, cancellation,
or restart. A mode already enabled by the terminal is preserved. Terminals that
do not answer retain the initial dark palette and focus-query fallback.

### Terminal progress, titles, and alerts

`terminal_status.go` derives bounded title/progress metadata from the existing
root reducer. Titles rebuild only when project/state changes. Progress values
are immutable shared objects, avoiding one allocation per streamed frame.
Global settings and focus reports govern generic terminal attention/desktop
notifications. Generation and request/turn identity checks reject delayed or
duplicate work; canceled/continuing turns do not announce success.

Ghostty expires OSC 9;4 progress without keep-alives. A cancelable one-second
timer runs only during work or blocking input. The program filter resolves
the current state immediately before renderer-serialized `RawMsg` output.
These messages reuse the last complete View; the next ordinary update always
invalidates that shortcut. Never put progress escape sequences into frame text
or write to stdout from a separate timer goroutine.

The real renderer regression verifies pulses without intervening frame writes
and cleanup on quit/cancellation. `BenchmarkTerminalHeartbeat` is included in
the normal performance gate. [Recorded comparisons](../benchmarks/results/2026-09-10-terminal-status/README.md)
show no regression in the seven existing fixture medians; the live-app frame
comparison has identical allocations with terminal metadata enabled/disabled.
The heartbeat handler itself takes about 0.09 µs and 72 bytes per one-second
pulse, excluding timer/terminal costs.

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

The composer additionally retains at most 32 KiB of input/rendered output.
Buffer, cursor, selection, focus, and size are cache keys; component updates
(including cursor blink) and theme changes invalidate it. Transcript updates
can reuse an untouched editor view. Layout updates textarea dimensions only
when they change. Frame components are joined without pre-padding every row;
the final frame boundary performs one ANSI-aware padding/clipping pass and
wraps only when content actually overflows.

New output follows only when the viewport is already at bottom. While the user
reads earlier content, source state keeps updating without replacing the
snapshot; returning to bottom catches up once.

Width-only transcript wrapping uses `wrapTranscript`: ANSI-aware wrapping,
batched writes with per-row style/link restoration, and shared padding. Avoid
replacing it with `lipgloss.NewStyle().Width(width).Render(text)` over complete
transcripts: Lip Gloss v2.0.6's wrapping writer allocates once per output byte
and exceeds the existing hydration allocation ceilings. Bounded styled frame
components continue to use Lip Gloss.

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

Bubble Tea v2 decodes fragmented mouse and keyboard sequences before dispatch.
Snow handles key presses only and routes bracketed paste as literal text to the
visible editor. Byte-stream regression tests cover split escape and UTF-8 input;
mouse-looking pasted text remains literal.

Clipboard writes are Bubble Tea commands (`SetClipboard` or multiplexer-wrapped
`Raw`), never control strings prefixed to `View`. Text reads are bounded host
commands with OSC 52 fallback, or direct OSC 52 on SSH. One terminal query owns a
target and generation; timeout/cancellation leaves a tombstone until its response
is drained because the wire protocol provides no correlation ID. Unsolicited or
stale replies cannot enter a different editor. Native bracketed paste remains
available while that query is pending.

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

### Charm v2 migration measurements

Local medians on an Apple M3 Pro (Go 1.27rc3, three default-duration samples)
compare a fresh run of pre-migration commit `8fad893` with the corrected v2
implementation. Benchmarks exercise the root `View`, composer edits, and drag
frames. [Raw samples and medians](../benchmarks/results/2026-09-10-charm-v2-rendering/)
are checked in for review.

| Fixture | Before | v2 |
|---|---:|---:|
| Reflow 10,000 transcript rows | 10.78 ms | 5.85 ms |
| Render 40-column frame | 0.045 ms | 0.020 ms |
| Render 120-column frame | 0.071 ms | 0.027 ms |
| Backspace + frame, 256-byte composer | 0.273 ms | 0.246 ms |
| Backspace + frame, 8 KiB composer | 3.52 ms | 2.48 ms |
| Backspace + frame, 64 KiB composer | 27.25 ms | 17.78 ms |
| Transcript selection drag frame | 0.452 ms | 0.243 ms |

All seven fixtures improve latency and allocated bytes. Transcript reflow
allocations fall from 10,084 to 26 (13.47 MB to 8.86 MB). Short composer edits
still allocate more individual objects (684 versus 358), while allocated bytes
fall from 119 KB to 57 KB and latency improves 10%. Rendering now has its own
allocation limits in the standard guard; all previous limits are unchanged.
These are local measurements, not guarantees for every terminal/workload; live
terminal rendering still needs manual validation.

### Commands and terminal checks

For layout or lifecycle changes, run the following from the repository root,
substituting the changed file names:

```sh
gofmt -w internal/tui/<changed-files>.go
go test ./internal/tui -count=1
go test -race ./internal/tui -count=1
go test ./...
go vet ./...
python3 scripts/check_benchmarks.py
go build -o ./snow ./cmd/snow
python3 scripts/smoke_tui_terminal.py --binary ./snow
```

The PTY smoke uses an isolated fake provider and explicit untrusted-project
startup, with no credentials or network. It exercises enhanced/legacy input,
OSC 52 queries, focus/background queries, resize, F6, normal quit, cancellation,
mode ownership, and a second process on the restored PTY. It checks raw TTY
settings and terminal escape restoration. This controlled protocol test does
not replace visual checks in Ghostty or live SSH/tmux checks.

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

### Stable-draft follow-up

The shrinking-buffer backspace fixture missed sustained edits at a fixed draft
size. `BenchmarkStableComposer` now inserts, renders, deletes, and renders at
approximately 8 KiB for ASCII, accents, CJK, and emoji. The textarea styles only
visible rows while preserving scroll bounds, selection coordinates, and viewport
clamping after resize. Differential tests compare it with the unmodified pinned
upstream component.

Three alternating same-host true-color runs against v1 (`8fad893`) show fixed
latency per edit pair of 1.769/1.896/1.638/2.241 ms respectively, improving
11–34%; allocated bytes improve 20–23%. Allocation counts remain higher than
v1. The new limits reject the pre-fix v2 samples without relaxing existing
limits. See the [raw comparison and method](../benchmarks/results/2026-09-10-recent-changes-audit/fixes/README.md).
