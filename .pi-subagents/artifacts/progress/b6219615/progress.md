# Progress — opencodego adapter (worktree b6219615)

## Status: COMPLETE

## Files created (untracked, not staged)
- `internal/provider/opencodego/opencodego.go` — OpenAI-compatible Chat Completions SSE streaming adapter
- `internal/provider/opencodego/opencodego_test.go` — 10 httptest-based tests

## What was implemented
- `New(Config)` with BaseURL default `https://opencode.ai/api/v1` (placeholder; must be verified — noted in code), APIKey fallback, HTTPClient override, DefaultModel default `kimi-k2.6` (matches pi's current opencode-go default; flagged for verification).
- `ID()` = `"opencode-go"`; `Resolve` accepts creds.Key → env `OPENCODE_API_KEY` → Config.APIKey.
- `ListModels`: best-effort GET {base}/models parsing `{"data":[{"id":...}]}`; never fails — falls back to static catalog on network/HTTP error.
- `Chat`: POST {base}/chat/completions with `stream:true`, `stream_options.include_usage:true`, tools/function mapping, message role mapping (user/assistant/tool; thinking blocks skipped for OpenCode Go), SSE parsing with `[DONE]`, fragmented tool-call accumulation per index, `EvStreamToolCallDone` with complete args (wrapped `{"_raw":...}` if malformed), usage mapping incl. cached tokens, finish_reason → stop/length/tool_use, `event: error` handling, HTTP error mapping (401 → "invalid API key"), ctx cancellation.
- Channel-backed `EventStream` (`Next`/`Close`); fixed a race where the reader goroutine closed the done-channel before draining buffered events — only the user's `Close()` closes `s.done`.

## Test coverage (10 tests, all pass, race-clean)
1. TestChatStreamSequence — full event order incl. fragmented tool args
2. TestChatToolUseDone — done(tool_use) at EOF after tool_calls
3. TestChatUnauthorized — 401 → descriptive EvStreamError
4. TestChatRequestBody — auth header, model, tools, message roles
5. TestThinkingSkipped — thinking blocks not on wire; reasoning_content → thinking_delta
6. TestListModelsFallback — 500 → static catalog
7. TestListModelsRemote — catalog parse + auth header
8. TestResolve — key resolution order
9. TestChatCancellation — ctx cancel → context.Canceled
10. TestErrorEvent — `event: error` SSE → EvStreamError (terminal, no done)

## Verification commands
- `go build ./...` — passed
- `go vet ./...` — passed
- `gofmt -l internal/provider/opencodego/` — clean
- `go test ./internal/provider/opencodego/ -v` — 10/10 PASS
- `go test -race ./internal/provider/opencodego/` — PASS

## Residual risks
- DefaultBaseURL and DefaultModelID are unverified placeholders (documented in code; must be confirmed against live OpenCode Go docs/catalog — see IMPLEMENTATION.md §5.6 checklist).
- Images in user messages are not yet serialized (documented in textContent).
