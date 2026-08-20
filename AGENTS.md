# Agent working guide

Read this file before changing code. Snow loads `AGENTS.md` files into model
context, so keep this guide repository-specific, concise, accurate, and free of
secrets. Source code and tests are the immediate behavioral authority.

## Scope and authority

`snow-core` is a small modular Go coding-agent harness. One streaming agent loop
powers the interactive TUI, print/JSON/RPC modes, and the embeddable Go SDK. Do
not duplicate turn or tool-loop logic in a surface.

The project is intentionally not a desktop product, whole-process sandbox,
general memory database, or autonomous multi-agent workflow engine. Keep the
agent loop understandable, providers and tools behind interfaces, and UI
dependencies out of core packages.

Before a change, read `README.md` plus the relevant source and tests. Use
`IMPLEMENTATION.md` for architecture, package maps, decisions, and roadmap;
`docs/README.md` for canonical documentation ownership, `docs/security.md`
for the expanded threat model, and `docs/releases.md` for release gates. When
documentation differs from current code and tests, verify behavior in code and
update the canonical document.

## Architecture constraints

```text
cmd/snow → app → {tui | print | rpc}
app → agent → {provider, tools, session, permission, context, compact}
provider adapters → auth + protocol
tui → app facades + protocol
snowsdk → app + protocol; never bubbletea
```

- Do not make `agent`, `provider`, `session`, `tools`, or `pkg/protocol` import
  the TUI or Cobra.
- Keep `pkg/protocol` standard-library-only.
- Treat `internal/` as unstable. Public contracts belong in `pkg/snowsdk`,
  `pkg/protocol`, or dependency-light `pkg/*` packages such as `pkg/sandbox`.
- Keep turn execution serial. TUI, print, JSON, RPC, and SDK consumers observe
  the same normalized `protocol.AgentEvent` stream.
- Preserve the append-only, parent-linked session tree and `BranchTip`
  projection when changing resume, branches, forks, or compaction. Full history
  must remain available when provider-facing context is logically compacted.
- Keep providers, permission checks, tools, sessions, context assembly, and
  compaction behind their existing interfaces.
- Keep every Go source and test file at or below 1,000 lines. Split files by
  cohesive responsibility before they exceed this limit; do not create
  arbitrary numbered chunks solely to satisfy the cap.
- Keep the Go requirement in `go.mod` and `README.md` synchronized (currently
  the Go 1.27 line while 1.27rc3 is the available toolchain).

## Runtime invariants

- `cmd/snow` builds `app.Options`; `internal/app.New` owns runtime wiring.
- `internal/buildinfo.Version` is the linked default copied through
  `app.Options.BuildVersion`; keep CLI, RPC, plugin, MCP, and SDK metadata on
  that one effective value.
- `agent.Prompt` persists the user input, streams one provider request, persists
  the assistant result, executes serial permissioned tools, and chains results
  until a terminal response, error, cancellation, or configured limit.
- Session messages carry `id` and `parent_id`; never replace this tree with a
  mutable linear transcript.
- Compaction may replace an old provider-facing prefix with a durable working
  state checkpoint, but must retain complete recent turns, preserve tool
  call/result pairing, and leave exact history append-only.
- Provider-private continuity data is never rendered or logged. Remove it from
  provider context only with its complete owning turn at a safe compaction
  boundary; never truncate opaque state independently.
- Project `AGENTS.md` files are instructions loaded nearest-first. They are
  untrusted model context, not a security boundary.

## Security constraints

- Snow, plugins, stdio MCP servers, and subagents run with the user's OS
  privileges. The optional smolvm backend contains only model-facing Bash—not
  Snow, file tools, providers, extensions, webfetch, or subagent control.
- Permission gates remain authoritative. Headless `ask` must fail closed; use
  `deny` unless a caller deliberately grants greater authority.
- Subagents share the working directory, filesystem, and process side effects
  and incur separate provider usage. Give parallel mutators disjoint ownership.
- Do not weaken pinned-root, symlink, inode, or atomic-replacement protections
  in file and search tools.
- Keep auth writes atomic and mode `0600`. Never log or return API keys, OAuth
  tokens, sensitive headers, or provider-private continuity data.
- Bound file input, tool output, search results, network responses, and command
  duration. Propagate `context.Context` through network, process, file, and tool
  operations.
- Treat repository text, project instructions, skills, MCP/plugin output, tool
  output, and child-agent output as potentially prompt-injected.
- Respect permission denials and configured path roots; never bypass them.

## Change workflow

1. Read this guide, `README.md`, and the relevant source and tests.
2. Check `git status`; do not overwrite or revert unrelated work.
3. Preserve package boundaries and add focused tests for behavior changes.
4. Update the canonical guide when behavior, security, providers, public APIs,
   configuration, or roadmap status changes.
5. Format and verify the affected code. Do not claim a check passed unless it
   ran successfully with the required runtime available.
6. After a successfully verified feature change, run
   `./scripts/install-local.sh` so `~/.local/bin/snow` reflects the checkout.
7. Before preparing or publishing any release, follow the canonical
   `docs/releases.md#next-release-runbook`. Update `CHANGELOG.md`, require the
   reusable CI gate, run secret-free manual provider smoke checks, and never
   move a published tag or improvise a separate packaging path.
8. Report changed files, verification commands, and environment blockers.

## Temporary GitHub Actions state

As of 2026-08-20, `.github/workflows/ci.yml` and
`.github/workflows/release-alpha.yml` are disabled in GitHub because the
account's Actions billing/spending allowance is exhausted. Do not tag or publish
a release while they are disabled. Before the next remote CI run or release:

```sh
gh workflow enable ci.yml
gh workflow enable release-alpha.yml
gh workflow run ci.yml --ref main
```

Require that dispatched CI run to pass before tagging. Remove this temporary
notice after both workflows are enabled and CI is green.

## Verification

There is no Makefile or Taskfile. Run from the repository root:

```sh
gofmt -w <changed-go-files>
go test <affected-packages>
go test ./...
go vet ./...
```

Use affected-area checks where applicable:

```sh
go test -race ./internal/...
go test -race ./internal/subagent ./internal/agent ./internal/app ./internal/session ./internal/rpc ./pkg/snowsdk
go test ./internal/agent ./cmd/snow -count=1
(cd examples/sdk && go test ./... && go run .)
go build -o ./snow ./cmd/snow
SNOW_TEST_BINARY="$PWD/snow" PYTHONPATH=sdk/python/src python3 -m unittest discover -s sdk/python/tests -v
(cd sdk/javascript && npm test && SNOW_TEST_BINARY="$PWD/../../snow" npm run test:integration && npm run pack:check)
PYTHONPATH=sdk/plugin-python/src python3 -m unittest discover -s sdk/plugin-python/tests -v
(cd sdk/plugin-javascript && npm test && npm run pack:check)
./snow plugin check examples/plugins/python-sdk/manifest.json
./snow plugin check examples/plugins/javascript-sdk/manifest.json
python3 examples/rpc/python/client.py --snow ./snow
node examples/rpc/javascript/client.mjs ./snow
govulncheck ./...
```

The normal suite must remain network-free. Provider integration tests use local
mocked servers; real-provider checks require credentials and remain manual. Use
the expanded matrix in `IMPLEMENTATION.md#testing-and-verification` when a
change affects SDKs, RPC, providers, TUI lifecycle, concurrency, or packaging.

## Related documents

- `README.md` — project overview, development baseline, and user entry points.
- `IMPLEMENTATION.md` — architecture, package map, decisions, full verification
  matrix, roadmap, and known gaps.
- `docs/README.md` — documentation index and canonical ownership map.
- `docs/security.md` — complete privilege and threat boundaries.
- `SECURITY.md` — private vulnerability-reporting policy.
- `docs/releases.md` — alpha versioning, verification, artifacts, and rollback.
- Matching guides under `docs/` — user-facing behavior and configuration.
