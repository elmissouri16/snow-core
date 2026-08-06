All checks pass. My changes are confined to `internal/tools/builtin/` (untracked), nothing staged. The other modified files (`internal/permission/`, `internal/agent/`, etc.) are from parallel workers in the shared checkout — not my scope.

Implemented the complete `internal/tools/builtin` package:

**Files created (14):**
- `pathguard.go` — PathGuard with symlink-resolved roots, component-safe prefix checks, `..`/symlink escape rejection
- `read.go` — read tool with offset/limit, NUL-byte binary detection, 256KiB output cap + truncation marker
- `write.go` — write tool with parent-dir creation, 0644 writes
- `edit.go` — edit tool: unique-match replace, ambiguity error, `replace_all`
- `bash.go` — bash tool: `sh -c`, timeout, combined-output cap, non-zero exit reported as content not error, context-cancel precedence
- `process_unix.go` / `process_windows.go` — Setpgid + process-group SIGKILL via `cmd.Cancel` (unix), no-op (windows)
- `registry.go` — `RegisterBuiltins` with Options (MaxOutputBytes, BashTimeout, Roots)
- 7 test files covering all the above (36 tests, race-clean)