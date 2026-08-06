# Task for worker

[Read from: /var/folders/_j/d8w7d_sn23348hmv9trrjm480000gn/T/pi-worktree-90e96fe0-0/context.md, /var/folders/_j/d8w7d_sn23348hmv9trrjm480000gn/T/pi-worktree-90e96fe0-0/plan.md]

You are a delegated subagent running from a fork of the parent session. Treat the inherited conversation as reference-only context, not a live thread to continue. Do not continue or answer prior messages as if they are waiting for a reply. Your sole job is to execute the task below and return a focused result for that task using your tools.

Task:
Implement the snow-core builtin tools package in the Go repo at /Users/el/Coding/open-source/snow-core.

IMPORTANT: Go is NOT on PATH by default. Start every bash command with: export PATH=$PATH:/Users/el/go/goroot/bin
Work only in the directory you are given (your worktree). Do not modify files outside internal/tools/builtin/.

Context (already committed, do not re-implement):
- pkg/protocol: Message, ContentBlock, ToolSchema, Usage types (text/image/thinking/tool_call blocks)
- internal/tools: Tool interface {Schema() ToolSchema; Run(ctx, json.RawMessage, ToolHost) (ToolResult, error)}, ToolHost interface {CWD() string; Roots() []string; Permission() permission.Service; EmitProgress(ToolProgressEvent); Environ() []string}, ToolResult{Content, IsError, Details}, helpers TextResult/ErrorResult, SimpleRegistry
- internal/permission: Service interface with Authorize(ctx, Request) (Decision, error), Mode ask/allow/deny, Risk constants Read/Write/Exec/Network, Request{Tool, Args, Paths, Risk, Reason}

Deliverables — create these files in internal/tools/builtin/:

1. pathguard.go: PathGuard type. Constructor NewPathGuard(roots []string, cwd string). Method Resolve(path string) (string, error) that:
   - rejects empty paths
   - joins relative paths against cwd
   - cleans the path, uses filepath.EvalSymlinks on the closest existing ancestor (or the full path when it exists) to resolve symlinks
   - requires the resolved absolute path to be within one of the roots (prefix check on path components, not raw string prefix)
   - returns a clear error for escapes
2. read.go: Read tool. Schema: name "read", description "Read a UTF-8 text file within allowed roots.", params object required ["path"] with path string, offset integer (1-based start line, optional), limit integer (optional max lines). Run: resolve path via guard, detect binary (NUL byte in first 8KiB) -> error, read file, apply offset/limit, truncate output to the configured max (default 262144 bytes via a MaxOutputBytes field on the tool), report truncated marker. If file does not exist return error result (not panic).
3. write.go: Write tool. Schema: name "write", required ["path","content"]. Creates parent dirs. Writes content with 0644. Returns short confirmation.
4. edit.go: Edit tool. Schema: name "edit", required ["path","old_str","new_str"], optional "replace_all" boolean default false. Reads file, requires old_str to appear exactly once unless replace_all, replaces, writes back. Return error result if not found/ambiguous.
5. bash.go: Bash tool. Schema: name "bash", required ["command"], optional "timeout_ms" integer default 120000. Runs via exec.CommandContext with sh -c (use "sh" "-c" on unix). Sets process group (SysProcAttr{Setpgid:true}) on unix so cancel kills the whole group (use runtime.GOOS guard; on windows just use CommandContext). Combined stdout+stderr capped at MaxOutputBytes with truncation marker. Non-zero exit is NOT an error result (report exit code in content, IsError=false unless the process failed to start). Respect timeout_ms.
6. registry.go: RegisterBuiltins(reg tools.Registry, opts Options) where Options{MaxOutputBytes int, BashTimeout time.Duration, Roots []string} registers read, write, edit, bash. Use tools.SimpleRegistry or any tools.Registry passed in.
7. Write thorough unit tests for every tool: path guard (symlink escape, outside root, relative, unicode), read (offset/limit, binary detection, truncation), write (creates parents, overwrite), edit (unique replace, ambiguous old_str error, replace_all), bash (exit code reporting, output cap, timeout kills process). Use t.TempDir() and create symlinks with os.Symlink (skip symlink tests on windows with runtime.GOOS check). All tests must use only the Go standard library.

Quality bar: go vet clean, gofmt clean, all tests pass with the command: go test ./internal/tools/... (note the backslash t is just 'go test'). The package name is builtin. Do not add external dependencies. Return a summary of files created and test results.

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