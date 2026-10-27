# Performance regression guard

Snow keeps performance checks deterministic enough for local development and
GitHub-hosted CI by using allocation metrics as the precise primary gate plus
generous wall-clock ceilings that catch only catastrophic slowdowns.
This guide owns the benchmark ceiling policy; TUI renderer invariants remain in
[TUI performance](tui-performance.md).

The [2026-09-05 runtime measurements](runtime-fixes-performance.md) cover small
checkpoint, terminal-text, and process-capture changes, including allocated
bytes, live retained heap, and whole-process peak resident memory.

## Run the guard

From the repository root:

```sh
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
python3 scripts/check_benchmarks.py
```

The checker uses only the Python standard library and rejects a Go toolchain
that does not exactly match the version pinned in the limits file. Each
configured benchmark runs three samples on one logical CPU. Expensive hydration
fixtures use one iteration per sample:

```text
go test <package> -run ^$ -bench <pattern> -benchmem \
  -benchtime=1x -count=3 -cpu=1
```

The rendering group overrides `benchtime` to `500ms`, measuring steady frame
reuse and enough composer edits to expose repeated work. It covers normal and
narrow frames, 10,000-row transcript reflow, short/8 KiB/64 KiB composer edits,
selection frames, and stable 8 KiB insert/delete pairs using ASCII, accents, CJK,
and emoji. Allocation ceilings catch the Charm v2 regressions;
timing ceilings retain platform headroom. Compare local latency samples against
the prior checkout when changing rendering, not only against these broad limits.

The median `B/op` and `allocs/op` values must remain below the reviewed ceilings
in [`benchmarks/performance-limits.json`](../benchmarks/performance-limits.json).
The hydration byte ceiling is intentionally below the pre-pagination result, so
restoring a complete-history blob decode fails even with platform headroom.
`B/op` and `allocs/op` are the precise gates. Selected `ns/op` limits are set
several times above local medians only to catch catastrophic CPU regressions;
hosted-runner CPU allocation, virtualization, scheduler load, and SQLite
storage latency are not stable enough for a tight timing contract.

The initial ceilings were seeded from the documented Apple M3 Pro measurements
with explicit platform headroom. The first re-enabled `ubuntu-latest` run is the
authoritative Linux validation and must pass before release. A platform-only
adjustment still requires captured CI evidence and review; limits must never be
raised automatically.

## Covered paths

The checked set focuses on recurring or historically expensive operations:

- assistant-heavy and mixed user/tool 5,000-message TUI hydration plus mailbox
  ingestion;
- normal/narrow frames, transcript reflow and selection, and composer editing;
- lightweight SQLite branch hydration, 1,500-entry atomic batch append,
  cold/warm context projection, and compacted in-memory context projection;
- 256-event subscriber delivery;
- 1,500-message OpenAI-compatible request construction;
- Chat Completions and Responses SSE ingestion.

The hydration tests also assert the algorithmic bound directly: message blobs
are requested in pages of at most 256, only the newest 1,999 full-screen rows
are decoded, and legacy tool calls outside that suffix use focused lookups.
Exact input history, plans, context usage, compaction boundaries, inline branch
prefixes, tool pairing, and omission counts remain covered by parity tests.

Additional search benchmarks run independently of the ceiling guard:

```sh
go test ./internal/tools/builtin -run '^$' \
  -bench '^(BenchmarkGrep10MB|BenchmarkGlob2000|BenchmarkReadSearchLines100000|BenchmarkSearchIgnore2000)$' \
  -benchmem -benchtime=10x -count=3 -cpu=1
```

They distinguish full grep/glob operations from line reading and ignore-rule
evaluation alone. The fixtures contain a 10 MB / 100,000-line text file or
2,000 files in 50 directories with 60 ignore rules. Ignore patterns are prepared
once per search, and inherited directory-rule reuse is limited to 256 directories
and 4,096 rule slots, charging backing-array capacity. When either limit is
reached, rule assembly continues uncached with unchanged precedence. Caches do
not survive between searches. Complete buffered grep lines need only one owned
string copy; fragmented lines and oversized-line draining retain their existing
bounded behavior.

## Ceiling policy

Allocation ceilings are regression limits, not targets or generated snapshots.
When intentionally changing a covered path:

1. Run the focused benchmark at least three times with the pinned toolchain.
2. Explain an allocation increase in the change review; first look for an
   accidental full-history decode, clone, temporary map, or buffer growth.
3. If the increase is required, set a manually reviewed ceiling with enough
   headroom for platform/toolchain variation, normally about 20–25% over a
   stable median.
4. Never raise limits automatically or merely to make CI green.
5. Record material improvements or intentional tradeoffs in
   [`docs/architecture.md`](architecture.md#performance-evidence-2026-08-24).

The generous timing ceilings automatically catch catastrophic slowdowns;
smaller timing regressions still require local profiling. Use repeated runs and
`benchstat` when comparing two commits, and do not infer a product latency
guarantee from one hosted runner.

## CI and releases

The Linux-only **Performance regression guard** job in
[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) runs the checker and
its parser tests. The reusable CI workflow makes it part of the release gate.

Local success is not remote CI evidence. The canonical local verification
commands live in the [agent working guide](../AGENTS.md#verification). Before a
release, require the complete remote CI run—including this regression guard—to
pass as described in the [release policy](releases.md).

## Web Manager responsiveness targets

The Web Manager is judged against the feel of current agent apps (ChatGPT,
Claude, DeepSeek): feedback the moment you send, tokens appearing as they
arrive, a steady frame rate while streaming, and no main-thread stalls. The
targets and the latest local measurement, taken on 2026-10-27 in Chrome on a
500×679 viewport against a deterministic OpenAI-compatible mock streaming 300
tokens at 30 tokens/s (`scratchpad/mockllm/mock_openai.py` style harness, not
a hosted model), are:

| Measure | Target | Measured |
|---|---:|---:|
| Visible feedback after Send (turn indicator) | < 200 ms | 18 ms |
| First answer token on screen after Send | < 500 ms + model latency | 96 ms |
| Incremental DOM updates for a 300-token answer | ≥ 10 per second | 159 updates, 75 ms median gap (p95 78 ms, max 80 ms) |
| Long tasks (> 50 ms) during streaming | 0 | 0 |
| Animation frame interval while streaming | p95 ≤ 20 ms | p95 19 ms, max 19 ms |
| Repeat page load (warm cache) | DOMContentLoaded < 200 ms | 113 ms, 32 requests, 11 KB transferred (304 revalidation) |
| Production bundle | ≤ 700 KB minified | 632 KB (185 KB gzip), Activity and Organization pages lazy |
| Event-stream bytes for one 300-token answer (161 snapshot frames) | ≤ 100 KB on the wire | 26 KB gzip-compressed per frame, versus 1,109 KB uncompressed |

The same run on an emulated phone (headless Chrome, 390×844, touch, 2× DPR)
measured 34 ms to the turn indicator, 109 ms to the first token, 160 updates
at a 75 ms median gap (max 79 ms), no long tasks, frame p95 17 ms, and the
transcript stayed scrolled to the newest text for 100% of updates.

The server coalesces snapshot publication at 75 ms, which sets the update
cadence; the browser renders each frame without long tasks. Event-stream
frames are full public snapshots and are gzip-compressed per frame when the
browser accepts it, so the cadence stays affordable for phones on the LAN.
Reasoning phases show a "Thinking…" indicator with elapsed time because
reasoning text never reaches the browser by design; the wait there is the
model's, not the UI's.

### How this compares with ChatGPT, Claude, and DeepSeek

The reference points are the public web apps as of late 2026. The comparison
is behavioral, not a measurement of those products, which run on their own
servers and cannot be instrumented here.

| Behavior | ChatGPT / Claude / DeepSeek web | Snow Web Manager |
|---|---|---|
| Send on Enter, newline on Shift+Enter | Yes | Yes (touch keyboards keep Enter as newline) |
| Immediate "working" feedback after Send | Yes | Yes, 18–34 ms |
| Tokens appear as they stream | Yes, batched at tens of milliseconds | Yes, 75 ms batches, no long tasks |
| Visible reasoning phase | Collapsible "Thinking" with text (ChatGPT, DeepSeek); summarized (Claude) | "Thinking…" with elapsed time and a "Thought for Ns" note; reasoning text stays on the host by design |
| Tool activity while working | Named steps ("Searching…", "Reading…") | "Reading a file…", "Running a command…" labels; tool rows without arguments by design |
| Stop generation, regenerate, edit and resend | Yes | Yes |
| Markdown, code blocks with copy | Yes | Yes |
| Conversation sidebar, rename, switch, mobile drawer | Yes | Yes |
| Attachments and context mentions | Files and images | Images and text files, `@` project files, `$` skills |
| Command palette | Slash commands (some) | `/` palette with every web-native command |
| Sign-in inside the app | Yes | Yes (device code anywhere; browser OAuth and API keys on the local URL) |
| Light and dark theme | Yes | Yes |
| Voice input | Yes | Yes, dictation through the browser's speech recognition on the local URL (secure context required) |
| In-conversation search | Yes | Yes: Find in conversation (menu, `/find`, or ⌘/Ctrl+F inside the chat) with match count, next/previous, and highlights |
| Canvases/artifacts | Yes | No; Snow edits files in the project itself and shows them in the Files inspector |
| Smoothed token reveal | ChatGPT smooths tokens visually | Yes; each newly streamed batch fades in over 220 ms (off with reduced motion) |

Canvases are the known gap. Everything else is in place and measured above.

## Shell preflight measurements

`BenchmarkAnalyzeShell` covers representative invocations without running their
commands or making network requests:

```sh
go test ./internal/shellanalysis -run '^$' -bench BenchmarkAnalyzeShell -benchmem -count=3
```

On 2026-09-05, three-sample local medians on an Apple M3 Pro with Go 1.27rc3
compared commit `8755986` with the specification/state/policy implementation:

| Invocation | Previous time | Current time | Previous bytes | Current bytes |
|---|---:|---:|---:|---:|
| `cat README.md` | 58.5 us | 127.2 us | 30,888 | 70,144 |
| `grep -n -e TODO -- README.md` | 85.1 us | 124.9 us | 43,391 | 70,433 |
| `git status --short` | 30.0 us | 112.8 us | 20,147 | 73,691 |
| `cat a b c d e f g h` | 250.6 us | 143.0 us | 116,784 | 88,781 |

The original package was loaded through a temporary Go overlay with the same
benchmark body; the checkout was not switched. These are analyzer-only local
measurements, not a performance gate or a claim about all shell workloads.
Simple commands incur additional fixed work to prepare protected resources and
bind approval identity to current policy. Bounded per-invocation ancestor
caching reduces repeated path work: the eight-file case uses about 24% fewer
bytes and takes about 43% less time. The specification is compiled once; no
command execution, help scraping, network lookup, or filesystem cache shared
between invocations is involved.
