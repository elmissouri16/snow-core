# snow-core

**snow** is a minimal, modular coding-agent harness written in Go — a fast
terminal client and embeddable library in one binary, inspired by pi,
OpenCode, and Codex.

> **Status:** pre-alpha (Phase 1–3 of the [IMPLEMENTATION.md](./IMPLEMENTATION.md)
> roadmap). Core loop, sessions, tools, OpenCode Go adapter, TUI, print/JSON/RPC
> modes, and the SDK are functional and tested.

## Highlights

- **Small core** — agent loop, sessions, tools, providers, permissions. No Electron, no DB.
- **Streaming-first** — tokens and tool events flow to the UI/SDK without buffering full turns.
- **Modular** — `Tool`, `Provider`, `permission.Service`, `session.Store` Go interfaces;
  out-of-process JSON-RPC plugins are a later phase.
- **Three surfaces, one loop** — interactive TUI, print/JSON/RPC CLI modes, and a
  pure-Go SDK (`pkg/snowsdk`) with no TUI dependency.
- **Sessions** — append-only JSONL with tree branching (`id`/`parentId`), fork/resume.

## Install & run

Requires Go 1.22+.

```bash
go build ./cmd/snow
# or
go install ./cmd/snow
```

```bash
# Interactive TUI
snow

# Print mode
snow -p "summarize this repo"

# JSONL event stream
snow --mode json -p "list the files"

# RPC mode (JSONL over stdin/stdout)
echo '{"id":"1","type":"prompt","message":"hello"}' | snow --mode rpc
```

## Providers & auth

| Provider | Auth | Notes |
|----------|------|-------|
| `opencode-go` | `OPENCODE_API_KEY` env, `~/.snow/auth.json` (`opencode-go`), or `--api-key` | OpenAI-compatible streaming adapter |
| `fake` | none | Deterministic scripted provider for tests/demos |

```bash
export OPENCODE_API_KEY=oc-...
snow -p "hello"
```

Credentials resolution order: explicit flag/SDK option → `~/.snow/auth.json` →
environment. The auth file is created with `0600` permissions.

## SDK

```go
package main

import (
    "context"
    "fmt"

    "github.com/snow-core/snow/pkg/snowsdk"
)

func main() {
    ctx := context.Background()
    s, err := snowsdk.Open(ctx, snowsdk.Options{
        Provider:       "opencode-go",
        NoSession:      true,
        PermissionMode: "deny",
    })
    if err != nil { panic(err) }
    defer s.Close()

    s.Subscribe(func(ev protocol.AgentEvent) {
        if ev.Type == protocol.EvTextDelta {
            fmt.Print(ev.Text)
        }
    })

    if err := s.Prompt(ctx, "List the Go files in this repo."); err != nil {
        panic(err)
    }
}
```

## Permissions & security

- Runs **as the user**; no in-process sandbox (see
  [IMPLEMENTATION.md §9](./IMPLEMENTATION.md#9-security-model)).
- `--permission ask|allow|deny` gates write/edit/bash tools. `read` is always
  allowed. Headless default is `deny`.
- File tools enforce path roots (cwd + explicit allows) with symlink resolution.
- Auth secrets are never logged; the auth file is `0600`.
- Prompt injection from repo files / tool output is a documented residual risk.

## Development

```bash
go test ./...
go vet ./...
go test -race ./internal/...
```

See [IMPLEMENTATION.md](./IMPLEMENTATION.md) for the full architecture,
interfaces, phased roadmap (0–4), and the provider verification checklist.

## Non-goals (v1)

- No Electron/desktop shell, no notes/tasks/memory product surfaces.
- No full pi/OpenCode provider catalog (only OpenCode Go + fake today).
- No built-in sandbox/container backend.
- No multi-agent orchestration.
