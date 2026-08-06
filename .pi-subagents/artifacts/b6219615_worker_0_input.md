# Task for worker

[Read from: /var/folders/_j/d8w7d_sn23348hmv9trrjm480000gn/T/pi-worktree-b6219615-0/context.md, /var/folders/_j/d8w7d_sn23348hmv9trrjm480000gn/T/pi-worktree-b6219615-0/plan.md]

You are a delegated subagent running from a fork of the parent session. Treat the inherited conversation as reference-only context, not a live thread to continue. Do not continue or answer prior messages as if they are waiting for a reply. Your sole job is to execute the task below and return a focused result for that task using your tools.

Task:
Implement the snow-core OpenCode Go provider adapter in the Go repo at /Users/el/Coding/open-source/snow-core.

IMPORTANT: Go is NOT on PATH by default. Start every bash command with: export PATH=$PATH:/Users/el/go/goroot/bin
Work only in your worktree directory. Create files only in internal/provider/opencodego/.

Context (already committed):
- internal/provider: Provider interface {ID() string; ListModels(ctx)([]protocol.Model, error); Resolve(ctx, auth.Credential) error; Chat(ctx, auth.Credential, protocol.ChatRequest)(protocol.EventStream, error)}
- internal/auth: Credential{Type, Key, Access, Refresh, Expires, Extra} with CredentialAPIKey
- pkg/protocol: Model, ChatRequest{Model,Messages,Tools,System,MaxTokens,Temperature,Thinking}, StreamEvent{Type,Text,ToolCallID,ToolName,Arguments json.RawMessage,Usage,StopReason,Err}, EventStream{Next(ctx)(StreamEvent,error); Close() error}, StreamEventType consts EvStreamTextDelta/EvStreamThinkingDelta/EvStreamToolCallDelta/EvStreamToolCallDone/EvStreamUsage/EvStreamDone/EvStreamError, StopReason stop|length|tool_use|error|aborted, Message/Role/ContentBlock/Usage/Cost, ThinkingLevel off|low|medium|high

Deliverable: internal/provider/opencodego/opencodego.go implementing an OpenAI-compatible Chat Completions streaming client for the OpenCode Go endpoint.

Spec:
- New(cfg Config) (*Provider, error) where Config{BaseURL string (default "https://opencode.ai/api/v1" — keep as the DEFAULT constant but note the real URL must be verified later; expose OverrideBaseURL via config), APIKey string, HTTPClient *http.Client, DefaultModel string}
- Provider.ID() returns "opencode-go"
- ListModels returns a static default catalog: at least [{Provider:"opencode-go", ID: default model, DisplayName:"OpenCode Go Default", SupportsTools:true, SupportsThinking:true, ContextWindow:200000}] plus any models from a catalog fetched from GET {base}/models (best-effort; on error return the static catalog, never fail ListModels on network error). Parse OpenAI-style {"data":[{"id":...}]}.
- Resolve(ctx, creds) returns nil when creds.Key is non-empty, else a descriptive error.
- Chat: POST {base}/chat/completions with JSON body {model, messages: [...], stream:true, tools: [...], temperature?, max_tokens?, stream_options:{include_usage:true}}. Authorization: Bearer <creds.Key> (if Config.APIKey set use it, else creds.Key).
  - Map protocol.Message to OpenAI messages: role user/assistant/tool; content for assistant includes text blocks and tool_calls (id, type:"function", function:{name, arguments}); tool role messages use {role:"tool", tool_call_id, content}. Thinking blocks are SKIPPED for OpenCode Go (no reasoning_content support assumed).
  - Map protocol.ToolSchema to OpenAI {type:"function", function:{name, description, parameters}}.
  - Parse the SSE stream (text/event-stream, data: lines, [DONE] terminator). Chunk format: choices[0].delta {content, reasoning_content?, tool_calls: [{index, id, type, function:{name, arguments}}]}. Emit:
    - delta.content non-empty -> EvStreamTextDelta
    - delta.reasoning_content non-empty -> EvStreamThinkingDelta
    - tool_calls: accumulate per-index tool calls across chunks (map index -> {id,name,argsBuf}); when finish_reason tool_calls arrives, emit EvStreamToolCallDone for each accumulated call with complete arguments (if args fail JSON parse, wrap them as {"_raw": "..."}).
    - usage chunk (delta empty, choices empty, usage present) -> EvStreamUsage with Usage{Input,Output,CacheRead,CacheWrite,Total} from prompt_tokens/completion_tokens (+ prompt_tokens_details.cached_tokens if present)
    - finish_reason "stop" -> EvStreamDone with StopReason stop; "length" -> length; "tool_calls" -> after emitting ToolCallDone events, EvStreamDone with tool_use
    - HTTP errors: non-2xx -> EvStreamError with a descriptive error including status code and response body snippet (truncated 500 bytes). 401 -> error mentioning invalid API key.
  - Handle SSE events named "error" (OpenAI error events) -> EvStreamError.
  - Support request cancellation via ctx (cancel body read).
  - Implement EventStream over a channel: Next(ctx) blocks on channel; Close() closes the stream.
- Chat credential handling: if creds.Key is empty, fall back to env OPENCODE_API_KEY (os.Getenv). Config.APIKey is a fallback last.

Write internal/provider/opencodego/opencodego_test.go using net/http/httptest:
- fake OpenAI-compatible SSE server that streams: a text delta chunk, a tool_call with fragmented arguments across two chunks, finish_reason tool_calls, then a final assistant text + stop chunk, and a usage chunk. Assert the emitted StreamEvent sequence in order: text_delta, tool_call_delta, tool_call_done (with complete parsed arguments JSON matching the intended args), text_delta, usage, done(stop or tool_use per script).
- test 401 error path returns EvStreamError with descriptive message.
- test request body: verify Authorization header, model field, tools mapping (tool schema function name/description/parameters), message roles mapping (user/assistant/tool with tool_call_id).
- test thinking blocks are skipped (no reasoning_content).
- test ListModels fallback to static catalog when server returns 500.
- test cancellation: cancel ctx mid-stream returns ctx error.
Use only the Go standard library (net/http, bufio, strings, encoding/json). Do NOT add golang.org/x/net or any external deps. Return a summary of files created and test results (paste the go test output for ./internal/provider/opencodego/).

---
Update progress at: /Users/el/Coding/open-source/snow-core/.pi-subagents/artifacts/progress/b6219615/progress.md

## Acceptance Contract
Acceptance level: checked
Completion is not accepted from prose alone. End with a structured acceptance report.

Criteria:
- criterion-1: Implement the requested change without widening scope

Required evidence: changed-files, tests-added, commands-run, residual-risks, no-staged-files

Finish with a fenced JSON block tagged `acceptance-report` in this shape:
Use empty arrays when no items apply; array fields contain strings unless object entries are shown.
`criteriaSatisfied[].status` must be exactly one of: satisfied, not-satisfied, not-applicable.
`commandsRun[].result` must be exactly one of: passed, failed, not-run.
`manualNotes` and `notes` are optional strings; an empty string means no note and does not satisfy `manual-notes` evidence.
```acceptance-report
{
  "criteriaSatisfied": [
    {
      "id": "criterion-1",
      "status": "satisfied",
      "evidence": "specific proof"
    }
  ],
  "changedFiles": [
    "src/file.ts"
  ],
  "testsAddedOrUpdated": [
    "test/file.test.ts"
  ],
  "commandsRun": [
    {
      "command": "command",
      "result": "passed",
      "summary": "short result"
    }
  ],
  "validationOutput": [
    "validation output or concise summary"
  ],
  "residualRisks": [
    "none"
  ],
  "noStagedFiles": true,
  "diffSummary": "short description of the diff",
  "reviewFindings": [
    "blocker: file.ts:12 - issue found, or no blockers"
  ],
  "manualNotes": "anything else the parent should know"
}
```