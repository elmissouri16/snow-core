All verification complete. Here is the structured findings report.

---

## Review

**Scope verified**: commits `545d49c` + `849fc9d` (HEAD `eb45a9a`, working tree clean apart from untracked `.pi-subagents/` artifacts). All five requested packages plus their tests inspected line-by-line. No files were modified (review-only).

**Commands run**: `go vet ./...` — **passed** (no output). `go test ./...` — **passed** (all packages, incl. `cmd/snow`, `internal/app`, `internal/tui` which have no test files). `go test -race` on `internal/agent`, `internal/provider/...`, `internal/auth`, `internal/tools/builtin` — **passed**, no races detected.

---

## Findings by package

### 1. internal/tools/builtin

**MAJOR — bash.go:61-67 — model-supplied `timeout_ms` overrides the operator-configured cap.** `b.Timeout` is documented as "Timeout caps execution", and the app wires `config.bash_timeout_ms` into it, but `if a.TimeoutMS != nil && *a.TimeoutMS > 0 { timeout = time.Duration(*a.TimeoutMS) * time.Millisecond }` replaces the cap unconditionally. A model can pass `timeout_ms: 86400000` and run a command for 24h, defeating the operator's 120s (or lower) configured cap. The existing test `TestBash_CustomTimeoutMS` even codifies override-beats-config. Fix: treat `b.Timeout` as an upper bound — `if t := time.Duration(*a.TimeoutMS) * time.Millisecond; t < timeout { timeout = t }` — or reject `timeout_ms > b.Timeout`.

**MAJOR — read.go:75-82 (also write.go:70-75, edit.go:100) — file tools never observe `ctx`.** `os.ReadFile` on a FIFO or device node inside an allowed root blocks forever; nothing can interrupt it (spec IMPLEMENTATION.md:219 requires "Cancel everywhere — ctx on HTTP, bash, file IO timeouts"). The agent's abort cannot stop a blocked read. Fix: `os.Stat` first and reject non-regular files (`!info.Mode().IsRegular()`), which also eliminates the FIFO hang.

**MINOR — registry.go:31-33 + Options doc — the documented host-time root fallback is dead code.** `RegisterBuiltins` always constructs `NewPathGuard(opts.Roots, "")` even when `opts.Roots` is empty, so the `if guard == nil && host != nil { guard = NewPathGuard(host.Roots(), host.CWD()) }` fallback in read/write/edit never triggers for registry-built tools (empty roots ⇒ deny-all, not "host provides roots at call time" as the doc claims). Separately, the guard's `cwd` is captured at registration time as the *process* cwd, not the host's CWD; the CLI always sets `opts.CWD = mustCWD()` so this is latent today, but any SDK use with a different CWD makes file tools and bash operate in different directories. Fix: only build the guard when `len(opts.Roots) > 0`, and/or resolve the guard's cwd from the host at call time.

**MINOR — read.go:84-85 — byte-level truncation can split a multi-byte UTF-8 rune**, returning invalid UTF-8 to the model. Fix: trim to a rune boundary (`utf8.ValidString` loop or `[]rune`).

**MINOR — bash.go:99 — `cmd.Env = host.Environ()`; `app.toolHost.Environ()` (app.go:274) returns nil**, so the model-controlled shell inherits the full parent environment including `OPENCODE_API_KEY`; conversely a host returning `[]string{}` would silently break `sh` (no PATH). At minimum document the inheritance; ideally pass an explicit scrubbed env with a PATH default.

**MINOR — bash.go:113-116 — truncated output doesn't record the full artifact path in `Details`** (IMPLEMENTATION.md:634 requires "store full artifact path in Details if truncated"; no builtin ever populates `Details`).

**NIT — bash.go:106-108 — user cancel and timeout produce the identical message** ("timed out or cancelled after 120s"), misleading on user abort. **NIT — write.go:70-75 / edit.go:100 — `MkdirAll`+`WriteFile` is non-atomic; a crash mid-write truncates an existing file**; temp+rename is safer for a code-editing tool.

**PathGuard core — CORRECT.** Component-boundary check via `filepath.Rel` (correctly rejects `/root/foo2` inside `/root/foo`), ancestor symlink resolution for not-yet-existing targets, deny-by-default on empty roots, `..` traversal rejection, unicode passthrough. Tests cover symlink escape, prefix siblings, nested, relative, empty roots, unicode — all pass. Residual risks (not bugs): resolve-then-open TOCTOU (a concurrent symlink swap between `Resolve` and the `os` call escapes the guard — irrelevant to the model-is-attacker threat model since bash exists, but worth documenting), and deny-direction false negatives on case-insensitive filesystems.

### 2. internal/auth

**filestore.go — CORRECT on the security-critical axes.** Atomic temp-file+rename in the same dir; 0600 enforced on every write path (temp `Chmod` before write + belt-and-braces `Chmod` after rename); corrupt files refuse `Put`/`Delete` (never silently overwritten); `persistCredential` avoids the redacting `MarshalJSON` on disk. Tests cover perms restoration after chmod-0644, no temp leftovers, corrupt-file handling, round-trips, env fallback priority.

**MINOR — filestore.go:26-28 / 73-107 — no locking around load-modify-save.** Two concurrent `Put`s (parallel login flows, or a CLI and an SDK sharing `auth.json`) race on the read-modify-write cycle and one update is lost (each rename is atomic, so no torn file, but lost updates). Fix: an internal `sync.Mutex` for same-process callers; `flock` for cross-process.

**NIT — filestore.go:37-49 — `Get` silently maps a corrupt file to "no credential"** (interface can't return the error), falling back to env with no diagnostic — consider a logged warning since the package doc emphasizes corrupt-file safety. **NIT — save doesn't fsync the containing directory** after rename (crash may lose the rename). **NIT — a zero-byte file is treated as a valid empty store.**

**memorystore.go — CLEAN.** Mutex-protected, correct copy semantics.

### 3. internal/provider/fake

**MINOR — fake.go:70-72 — `Next` returns `ErrExhausted` (an error) at script end instead of `io.EOF`.** The agent treats it as a stream error (`StopError` + `EvError` persisted) rather than clean completion whenever a script omits `StepDone` — a footgun for script authors, and inconsistent with the `EventStream` contract ("blocks until the next event or EOF/error"). Fix: return `io.EOF` at exhaustion (update the test loop accordingly).

**NIT — fake.go:93-95 — `Close` doesn't unblock a pending `Next`** (`done` flag is never read by `Next`). Otherwise CLEAN: recording copy semantics, per-call replay, cancellation checks — all correct.

### 4. internal/provider/opencodego

**MAJOR — opencodego.go:598-600 vs agent.go:250-261 — delta contract mismatch: the adapter sends *cumulative* args in each `EvStreamToolCallDelta` (`acc.argsBuf.String()`), while the agent *appends* each delta (`cb.Arguments = append(cb.Arguments, ev.Arguments...)`).** For a fragmented tool call the intermediate block is corrupted JSON (`{"path": "a{"path": "abc.txt"}`); only the final `EvStreamToolCallDone` (which replaces) masks it. If the stream dies between deltas and done (connection drop mid-call), the persisted assistant message contains garbage arguments with no done event. Two workers built opposite assumptions; only one contract can hold. Fix: adapter emits only the fragment (`json.RawMessage(tc.Function.Arguments)`) in deltas — matching the agent's append semantics — and keeps `finalArgs()` in the done event.

**MAJOR — opencodego.go:554-563 — unparseable `data:` lines are silently dropped.** Any chunk that fails `json.Unmarshal` (and isn't an error envelope) disappears with no error event. SSE-spec multi-line data fields (a JSON object split across two `data:` lines, legal SSE) or a mangled chunk → silent loss of model text/tool arguments while the agent continues. Fix: accumulate multi-line data per SSE spec (append with `\n` until a blank line) and/or emit `EvStreamError` on unmarshal failure instead of returning silently.

**MAJOR — opencodego.go:594-600 — tool calls are keyed by `tc.Index` internally but emitted with `acc.id`, which may be empty in early deltas.** The agent keys blocks by `ev.ToolCallID`, so an empty-id delta creates a phantom block that is never merged with the later id'd one (duplicate tool call in the assistant message — one with partial garbage args — plus a bogus execution attempt when `stop == tool_use`). OpenAI proper always sends the id first, but this adapter explicitly targets "local/dev gateways" (DefaultBaseURL comment) where compatible servers defer/omit ids. Fix: sticky fallback id — `if acc.id == "" { acc.id = fmt.Sprintf("tc-%d", acc.index) }` before the first send, so deltas and done share a stable id.

**MINOR — opencodego.go:605-618 — `finish_reason` is last-wins.** A trailing chunk carrying `"stop"` after `"tool_calls"` (some compatible servers emit one) overwrites the finish → the agent never stashes `pending`, tool calls are persisted in the assistant message but never executed, and the next provider call violates tool-call/result pairing. Fix: first-wins (`if *finish == ""` before assignment).

**MINOR — opencodego.go:610-616 + agent.go:306/319/354 — `for _, acc := range accums` and `for _, cb := range toolCalls` iterate maps in random order**: done-event order, persisted assistant tool-call order, and execution order are all nondeterministic for multi-call turns. Fix: keep an ordered index slice.

**MINOR — opencodego.go:550-552 — `[DONE]` does not emit the done event; the parser waits for EOF.** A server that sends `[DONE]` but keeps the connection open hangs the turn indefinitely (agent blocks in `Next`). Fix: emit `EvStreamDone` immediately upon `[DONE]`, then drain/ignore the remainder.

**NIT — a UTF-8 BOM before the first `data:` line silently drops the first chunk** (CRLF itself is handled correctly via `TrimSpace`). **NIT — after `Close`, `Next` may return buffered events or `io.EOF` nondeterministically** (select over `ch` and `done`); the "drained before EOF" comment only holds while the consumer keeps calling `Next`.

**CORRECT**: cancellation propagation (request ctx + `resp.Body.Close()` unblocking the reader + `done` channel), 401 special-casing, thinking-block leak protection (`TestThinkingSkipped`), usage mapping with total fallback, tool schema passthrough with empty-params default, `finalArgs` malformed-wrap, error-event normalization. Test suite is strong (fragmented args, request-body wire format, cancel mid-stream, error events, model fallback).

### 5. internal/agent

**MAJOR — agent.go:121-135 — `Prompt` appends the user message *before* the running check.** A concurrent second `Prompt` (SDK surface) persists a ghost user message into the session, then fails with "already running"; the ghost message is never processed and is silently sent to the provider on the next turn. `TestAlreadyRunning` doesn't assert message count, so the bug is uncovered. Fix: check `a.running` (under the lock) before appending.

**MAJOR — agent.go:354-360 — `executeToolCalls` never checks `ctx.Err()` between calls.** After cancel, remaining pending tools still execute to completion (read/write/edit ignore ctx entirely); only bash honors cancellation. An abort mid-tool-loop still performs all remaining file mutations. Fix: `if err := ctx.Err(); err != nil { return err }` at the top of the loop.

**MAJOR — agent.go:335, 380, 389, 409, 430 — every assistant/tool-result session `Append` error is swallowed (`_ =`).** A persistence failure (disk full, closed store, or — given `newID`'s 16-bit entropy at agent.go:498-500 — an ID collision) silently drops conversation history while the loop continues, producing a session the provider will choke on. The user-message append propagates its error; the rest should too.

**MINOR — agent.go:250-261 — delta append contract mismatches opencodego's cumulative deltas** (cross-package; see opencodego #1).

**MINOR — agent.go:306-308 / 319-323 / 354 — map iteration order** ⇒ nondeterministic tool-call order in the persisted assistant message and random execution order (see opencodego #5).

**MINOR — agent.go:355-357 — `CallLimit` silently drops unexecuted tool calls** (no results, no event), while the assistant message already contains them → provider sees tool_calls without results on the next call. Fix: emit error results for skipped calls.

**MINOR — agent.go:217-221 — on the abort path `textBlock(textBuf)` is appended even when `textBuf == ""`** (empty text block persisted); on error/abort paths `thinkingBuf` is dropped, inconsistent with the normal path that prepends thinking first. **NIT — agent.go:189-196 — the `StopError` case in `run()` is unreachable** (streamTurn always pairs StopError with a non-nil error). **NIT — synchronous event bus:** a slow subscriber stalls the agent loop; no bus Close, so subscribers leak if the agent is discarded without unsubscribing (publish/unsubscribe themselves are race-safe — copy under lock, invoke outside).

**CORRECT**: `running` flag + `pending` map fully mutex-protected; panic recovery in `runTool`; permission gate before `EvToolStart`; aborted assistant persisted with `StopAborted`; MaxTurns off-by-one correct (1 = one provider call); `-race` clean including `TestAlreadyRunning`'s concurrent prompts.

### Test gaps (highest value to close)

1. Concurrent `Prompt` ghost-message assertion (`TestAlreadyRunning` only checks the error).
2. Tool-loop abort under cancellation (cancel between tool calls).
3. opencodego: CRLF, multi-line data, malformed chunk (silent drop), empty tool-call id, `tool_calls`→`stop` finish overwrite, `[DONE]`-then-keepalive, multi-choice ordering.
4. bash: `timeout_ms` exceeding the configured cap; process-group reaping of grandchildren; cancel-during-run.
5. filestore: concurrent `Put` lost-update; corrupt-but-valid-JSON file (e.g. `{"p": 123}`).

---

## Acceptance report