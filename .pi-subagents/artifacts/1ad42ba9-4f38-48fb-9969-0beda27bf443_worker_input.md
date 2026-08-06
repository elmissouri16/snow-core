# Task for worker

You are a delegated subagent running from a fork of the parent session. Treat the inherited conversation as reference-only context, not a live thread to continue. Do not continue or answer prior messages as if they are waiting for a reply. Your sole job is to execute the task below and return a focused result for that task using your tools.

Task:
Write additional Go regression tests for recently fixed bugs in the snow-core repo at /Users/el/Coding/open-source/snow-core.

IMPORTANT: Go is NOT on PATH by default. Start every bash command with: export PATH=$PATH:/Users/el/go/goroot/bin

These bugs were found by a code review and FIXED in the working tree (committed as "review fixes"). Your job is to add tests that would have caught each bug, verifying the fix holds. Do NOT modify non-test files. Add tests only in these packages:

1. internal/agent/agent_test.go — add:
   - TestConcurrentPromptNoGhostMessage: start a turn with a blocking provider (use the existing blockingProvider/newBlockingProvider helpers in the test file), then call Prompt again; assert the SECOND prompt fails with "already running" AND the session has exactly 1 user message (no ghost). Also assert the first turn completes cleanly after cancel.
   - TestToolLoopCancelStopsRemaining: scripted provider emits two tool calls (tool_call_done for call-1 and call-2, stop tool_use). Use a cancelable ctx. Make a tool that records whether it ran. Cancel the ctx after the first tool runs (e.g. cancel inside the tool's runFunc via a sync.Once). Assert the second tool never ran (run count == 1) and Prompt returns a ctx error.
   - TestToolCallLimitEmitsErrorResults: CallLimit=1 with two tool calls; assert the skipped call produced an error tool_result message in the session (IsError true) and both tool calls have results (no dangling tool_calls).

2. internal/tools/builtin/bash_test.go — add TestBashModelTimeoutBoundedByCap: construct Bash with Timeout = 50ms (simulating operator cap), then invoke Run with args {"command":"sleep 1","timeout_ms":60000} and assert the command is killed quickly (Run returns within ~2s, error/truncated result mentions timeout). This proves the model cannot override the operator cap.

3. internal/tools/builtin/read_test.go — add TestReadRejectsFIFO: create a FIFO with syscall.Mkfifo (skip on windows), run the read tool against it, assert it returns an error result (not a hang). Also TestReadUtf8TruncationBoundary: file with multi-byte UTF-8 chars (e.g. "é" repeated) larger than MaxOutputBytes (set tool MaxOutputBytes small, e.g. 10), assert the returned content is valid UTF-8 (utf8.ValidString) and ends with the truncation marker.

4. internal/provider/opencodego/opencodego_test.go — add:
   - TestChatDoneThenKeepalive: server sends a normal chunk + finish stop, then "data: [DONE]", then keeps the connection open (does NOT close the response; write a comment or sleep briefly then block on a channel until test cleanup). Assert drain() completes with done(stop) WITHOUT hanging (use a context timeout of e.g. 5s).
   - TestChatEmptyToolCallID: server sends tool_calls with NO "id" field (only index + function), finish tool_calls. Assert the stream emits tool_call_done with a non-empty sticky id ("tc-0" or similar) and the tool_call_delta/done share the same id.
   - TestChatFinishFirstWins: server sends a chunk with finish_reason "tool_calls" and then a later chunk with finish_reason "stop". Assert the done event carries tool_use (first wins), not stop.

5. internal/auth/filestore_test.go — add TestConcurrentPutsNoLostUpdate: create FileStore in temp dir, run 20 goroutines each Put-ing a distinct provider, wait, then Get each and assert all 20 present. (The mutex fix makes this safe within one process.)

Run: go test ./internal/agent/ ./internal/tools/builtin/ ./internal/provider/opencodego/ ./internal/auth/ and make sure ALL pass including the new tests. Also go vet ./... must stay clean. Read the existing test files first to reuse their helpers (drain, mustNew, sseChunk, chunkWith, etc.) and match package conventions. Report which tests you added and the test output.

## Acceptance Contract
Acceptance level: checked
Completion is not accepted from prose alone. End with a structured acceptance report.

Criteria:
- criterion-1: Implement the requested change without widening scope
- criterion-2: Return evidence sufficient for an independent acceptance review

Required evidence: changed-files, tests-added, commands-run, residual-risks, no-staged-files

Review gate: required by reviewer.

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
    },
    {
      "id": "criterion-2",
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