All 10 tests pass with `-count=1` (no caching). The implementation is complete.

**Summary of work:**

Implemented `internal/provider/opencodego` — the OpenCode Go provider adapter:

- **`opencodego.go`**: OpenAI-compatible Chat Completions SSE streaming client with `Provider` implementing `ID()`, `ListModels()` (best-effort remote catalog with static fallback), `Resolve()` (key resolution: credential → `OPENCODE_API_KEY` env → config), and `Chat()` (SSE streaming with fragmented tool-call accumulation, usage mapping incl. cached tokens, finish_reason mapping, `event: error` handling, 401 → "invalid API key", ctx cancellation). Channel-backed `EventStream`. Fixed a real race where the reader goroutine closed the done-channel before buffered events drained.
- **`opencodego_test.go`**: 10 httptest-based tests covering the full event sequence, tool-call fragmentation, 401 errors, request-body wire format, thinking-block skipping, catalog fallback/remote parsing, key resolution, cancellation, and SSE error events.