# Repository layout

Snow is one Go module containing a command-line application and public embedding
contracts, plus a standalone SDK example module. Organize by package responsibility
and dependency direction, not by a universal application template.

## Layout

```text
.
├── cmd/
│   ├── snow/main.go          # thin executable entry point; calls cli.Main
│   └── sync-textarea/        # maintainer command for the pinned TUI component
├── internal/
│   ├── cli/                  # flags, commands, surface dispatch, CLI tests
│   ├── app/                  # runtime construction and application facades
│   ├── agent/                # the single streaming turn/tool loop
│   ├── provider/             # provider interface, adapters, shared codecs
│   ├── tools/                # tool contracts, builtins, discovery router
│   ├── session/              # append-only storage, branches, inventory
│   ├── rpc/                  # JSONL control surface
│   ├── tui/                  # terminal surface and private textarea snapshot
│   ├── web/                  # RPC-backed web manager, templates, assets
│   │   └── frontend/         # React source and pinned Node build tooling
│   └── …                     # focused private packages; see package map
├── pkg/                      # supported public import paths
│   ├── agentclient/           # manager-to-worker client contracts/adapters
│   ├── mcp/                  # dependency-light MCP configuration/status
│   ├── plugin/               # extension contracts
│   ├── protocol/             # standard-library-only messages/events/schemas
│   └── snowsdk/              # embeddable Go SDK
├── examples/                 # standalone SDK module and plugin examples
├── docs/
│   ├── architecture.md       # package map, architecture, and roadmap
│   ├── bugs.md               # reproducible defects and verification evidence
│   ├── design/               # design handoffs and historical plans
│   └── site/                 # documentation-site source, not generated output
├── scripts/                  # shell/Python maintenance and browser test runners
├── benchmarks/               # allocation limits and recorded measurements
└── build/                    # ignored local output; never source or dependencies
    ├── bin/                  # locally built snow executable
    ├── tests/                # compiled *.test executables
    ├── dist/                 # release archives and browser evidence
    └── pages/
        ├── source/           # curated Jekyll staging tree
        └── site/             # rendered documentation site
```

The full maintained package map and runtime flow live in
[Architecture and roadmap](architecture.md#repository-and-package-map).
[Maintaining Snow](maintaining.md) owns the documentation index and canonical
references. Keep only standard entry points and tool-owned manifests at the
root: `README.md`, `AGENTS.md`, `CHANGELOG.md`, `LICENSE`, `SECURITY.md`, `go.mod`,
`go.sum`, and `skills-lock.json`. Architecture and the bug tracker live in
`docs/`; the security reporting policy stays at the root for repository tooling.

## Why this structure

The official Go guide, [Organizing a Go module](https://go.dev/doc/modules/layout),
recommends `cmd/` for commands in mixed repositories and `internal/` for private
supporting packages. A package may span multiple files in the same directory;
Go does not require splitting it merely because its file count is high. The
[Go FAQ](https://go.dev/doc/faq#testing_framework) places `_test.go` files next
to the package they exercise.

For Snow, those recommendations mean:

- **Keep executable entry points separate from implementation.** `cmd/snow`
  delegates to `internal/cli.Main`. CLI commands and their unit/subprocess tests
  stay together. CLI dispatch builds `app.Options`; `app.New` still owns runtime
  wiring. The private host-clone helper runs before Cobra/config/app setup.
- **Keep public paths stable.** Go does not require a `pkg/` directory, but Snow
  already publishes these import paths. Moving them for cosmetic consistency
  would break consumers without improving the dependency boundaries.
- **Split by independent responsibility, not file count.** Provider adapters
  and tool routing already have real package boundaries. Files that share a
  receiver, lifecycle, or filesystem security guard stay together until an
  independent contract justifies extraction. Do not introduce generic `utils`,
  `services`, or `models` buckets, or numbered source-file fragments.
- **Keep tests with their owning Go package.** Browser orchestration stays under
  `scripts/tests/browser`; CLI-backed fixtures compile `internal/cli`, not the
  executable wrapper. Production builds still target `./cmd/snow`.
- **Make Go maintenance commands ordinary packages.** The textarea generator
  lives at `cmd/sync-textarea`, so normal Go test/vet traversal compiles it. Its
  generated snapshot stays beside the TUI that owns it; shell and Python tools
  remain under `scripts/`.
- **Keep assets with their owner.** Web frontend sources, templates, and embedded
  assets remain under `internal/web`; ordinary Go builds need no Node tooling.
  Design plans live under `docs/design` and are not published in the curated
  user-facing site.

## Generated output and local state

Source and generated output must not compete at the root. Use these paths:

| Output | Location |
|---|---|
| Local binary | `build/bin/snow` |
| Compiled Go test executable | `build/tests/<package>.test` |
| Release archives and browser evidence | `build/dist/` |
| Pages staging source | `build/pages/source/` |
| Rendered Pages site | `build/pages/site/` |

For a compiled test binary, name the output explicitly rather than letting
`go test -c` place it beside source:

```sh
mkdir -p build/tests
go test -c -o build/tests/session.test ./internal/session
```

The Pages builder defaults to `build/pages/source` and still refuses to overwrite
an existing directory. Existing `_pages_source`, `_site`, `dist`, root `snow`,
and root `*.test` artifacts were moved into the matching `build/` locations;
none were deleted. Historical recorded benchmark logs may show their original
paths, while current commands use the new locations.

`build/` is ignored by Git. `.git/`, `.github/`, `.agents/`, and `.snow/` have
distinct repository/tool/runtime purposes and are not build output. In
particular, do not move or delete `.snow` to make a checkout look cleaner: it
can contain active sessions and project-local agent state. `skills-lock.json`
is managed by the skill installer and retains its expected root location.

This reorganization changes source locations, not CLI flags, provider behavior,
RPC contracts, SDK imports, session formats, build targets, or the linked version
symbol `internal/buildinfo.Version`.

## Verification commands

Run from the repository root:

```sh
go test ./...
go vet ./...
go test ./internal/cli ./internal/agent -count=1
go run ./cmd/sync-textarea -check -source "$(go list -m -f '{{.Dir}}' charm.land/bubbles/v2)"
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
python3 scripts/check_benchmarks.py
go build -o build/bin/snow ./cmd/snow
```

The CLI browser runners continue using their existing `node
scripts/tests/browser/<suite>/run.mjs` commands. See [AGENTS.md](../AGENTS.md)
for race, SDK, installation, and affected-area checks, and
[Web frontend development](web-frontend.md) for asset verification.
