# Task for reviewer

Review the freshly implemented Go packages in /Users/el/Coding/open-source/snow-core that were written by parallel worker agents. This is a code review of other agents' work — find bugs, races, spec violations, and test gaps.

Go is at /Users/el/go/goroot/bin/go (add to PATH).

Review these packages (git log shows they were added in commits 545d49c and earlier; current HEAD is clean):
1. internal/tools/builtin/ — read/write/edit/bash tools + pathguard (security-critical: path traversal, symlink escapes)
2. internal/auth/filestore.go — credential file store (security-critical: 0600 perms, atomic writes, corrupt-file handling) and memorystore.go
3. internal/provider/fake/ — scripted provider for tests
4. internal/provider/opencodego/ — OpenAI-compatible SSE streaming client (protocol-critical: SSE parsing, tool call accumulation, finish_reason mapping, cancellation)
5. internal/agent/ — the turn loop (concurrency: running flag, pending map, event bus; correctness: tool loop, aborts, MaxTurns)

Focus areas:
- Path safety in builtin tools (symlink escape via PathGuard, .. traversal, unicode)
- Bash tool process-group kill on cancel, output caps, timeout
- Filestore atomicity and permission correctness (file mode 0600 enforced on every write)
- SSE parser edge cases in opencodego (CRLF, multi-line data, [DONE], fragmented args, cancellation race)
- Agent loop races (event bus publish during close, pending map, tool execution under cancel)
- Spec compliance with the interfaces in pkg/protocol and internal/tools, internal/provider

For each finding: file:line, severity (critical/major/minor/nit), description, and concrete fix suggestion. If a package is clean, say so explicitly. Run `go vet ./...` and `go test ./...` and report results. Do NOT modify any files — review only. Return a structured findings report.

## Acceptance Contract
Acceptance level: attested
Completion is not accepted from prose alone. End with a structured acceptance report.

Criteria:
- criterion-1: Return concrete findings with file paths and severity when applicable

Required evidence: review-findings, residual-risks

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