# Task for worker

[Read from: /var/folders/_j/d8w7d_sn23348hmv9trrjm480000gn/T/pi-worktree-6b80915e-0/context.md, /var/folders/_j/d8w7d_sn23348hmv9trrjm480000gn/T/pi-worktree-6b80915e-0/plan.md]

You are a delegated subagent running from a fork of the parent session. Treat the inherited conversation as reference-only context, not a live thread to continue. Do not continue or answer prior messages as if they are waiting for a reply. Your sole job is to execute the task below and return a focused result for that task using your tools.

Task:
Implement the snow-core auth filestore and fake provider in the Go repo at /Users/el/Coding/open-source/snow-core.

IMPORTANT: Go is NOT on PATH by default. Start every bash command with: export PATH=$PATH:/Users/el/go/goroot/bin
Work only in your worktree directory. Create files only in internal/auth/ and internal/provider/fake/.

Context (already committed):
- internal/auth: Credential{Type api_key|oauth, Key, Access, Refresh, Expires int64, Extra map[string]any}, CredentialType consts, Store interface {Get(provider)(Credential,bool); Put(provider, Credential) error; Delete(provider) error; Path() string}, Credential.Valid()
- internal/provider: Provider interface {ID() string; ListModels(ctx)([]protocol.Model, error); Resolve(ctx, auth.Credential) error; Chat(ctx, auth.Credential, protocol.ChatRequest)(protocol.EventStream, error)}
- pkg/protocol: Model{Provider,ID,DisplayName,ContextWindow,MaxOutputTokens,SupportsTools,SupportsThinking,SupportsVision}, ChatRequest{Model,Messages,Tools,System,MaxTokens,Temperature,Thinking,Extra}, StreamEvent{Type,Text,ToolCallID,ToolName,Arguments json.RawMessage,Usage,StopReason,Err}, StreamEventType consts EvStreamTextDelta/EvStreamThinkingDelta/EvStreamToolCallDelta/EvStreamToolCallDone/EvStreamUsage/EvStreamDone/EvStreamError, EventStream interface {Next(ctx)(StreamEvent,error); Close() error}, Message/Role/StopReason/Usage

Deliverables:

1. internal/auth/filestore.go: FileStore implementing auth.Store. NewFileStore(path string) (*FileStore, error). Stores a map[string]Credential marshaled as JSON (same shape as the doc: {"opencode-go": {"type":"api_key","key":"..."}, "chatgpt": {"type":"oauth","access":"...","refresh":"...","expires":0}}). On Put: load current file (or empty map), update, marshal with json.MarshalIndent, write atomically (write temp file then rename) with file mode 0600, and chmod the file to 0600 on every Put. Get/Delete mutate and persist for Delete. Path() returns the path. If the file is missing treat as empty. If the file is corrupt return an error from Get/Delete/Put (do not silently overwrite corrupt data — return error). Also add helper ResolveAPIKey(store Store, envVar, provider string) (auth.Credential, error) that checks store entry first, then env var (t.Setenv in tests), else returns ErrNoCredential. Export ErrNoCredential.

2. internal/provider/fake/fake.go: Fake provider for tests and demo. New(script []Step) and NewWithModels(models []protocol.Model). Step type: {Kind StepKind, Text string, Thinking string, ToolCallID string, ToolName string, Arguments json.RawMessage, Usage *protocol.Usage, Stop protocol.StopReason, Err error} where StepKind in {StepText, StepThinking, StepToolCall, StepUsage, StepDone, StepError}. Chat: return an EventStream that replays the script steps as StreamEvents in order (each Chat call replays the full script; track call count). The fake does NOT need to read messages, just replay script. ListModels returns NewWithModels models or a default set [{Provider:"fake", ID:"fake-1", SupportsTools:true, ContextWindow:128000}]. Resolve returns nil always. ID() returns "fake". Also add a recorded mode: NewRecorded() returns a provider where every Chat call records the ChatRequest into a thread-safe slice. Provide accessor RecordedCalls() []protocol.ChatRequest.

3. Write unit tests:
   - internal/auth/filestore_test.go: round-trip put/get/delete, 0600 permission check (stat mode on unix, skip on windows), atomics (no temp files left), env fallback via ResolveAPIKey with t.Setenv, auth.json priority over env, corrupt file returns error, missing file returns ErrNoCredential from ResolveAPIKey.
   - internal/provider/fake/fake_test.go: replay order matches script, tool_call_done event has parsed Arguments, call count increments, recorded requests capture messages/tools, Resolve nil.

Quality bar: go vet clean, gofmt clean, all tests pass (go test ./internal/auth/... ./internal/provider/fake/...). Standard library only — no external deps. Return a summary of files created and test results.

---
Update progress at: /Users/el/Coding/open-source/snow-core/.pi-subagents/artifacts/progress/6b80915e/progress.md

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