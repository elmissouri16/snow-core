# Snow

A coding agent for your terminal. Work through a change, inspect the tools it
runs, and pick up the conversation later. Choose your model provider and keep
sessions on your machine.

Snow is written in Go. Its terminal UI, command-line modes, and embeddable SDK
share one streaming agent loop.

[Documentation](https://elmissouri16.github.io/snow-core/) ·
[Getting started](docs/getting-started.md) ·
[Releases](https://github.com/elmissouri16/snow-core/releases)

[![CI](https://github.com/elmissouri16/snow-core/actions/workflows/ci.yml/badge.svg)](https://github.com/elmissouri16/snow-core/actions/workflows/ci.yml)

> **Note:** Snow is alpha software. APIs, configuration, and file formats may
> change before v1.

## Quick start

Install on macOS or Linux, on amd64 or arm64. Go is not required:

```sh
curl -fsSL https://raw.githubusercontent.com/elmissouri16/snow-core/main/scripts/install.sh | sh
```

The installer verifies the release checksum and binary version, installs to
`~/.local/bin/snow`, and updates your shell path. Set `SNOW_NO_MODIFY_PATH=1`
to leave shell startup files unchanged. Restart your shell, then launch Snow
in a project:

```sh
cd /path/to/project
snow --provider opencode-zen
```

OpenCode Zen supports anonymous access. To use your ChatGPT account instead:

```sh
snow login chatgpt
snow --provider chatgpt
```

Type a task and press Enter. Snow asks before tools that need approval. Use
`/help` for commands, `/model` to choose a model, and `snow resume` to return
to saved work.

The [installation guide](docs/getting-started.md) covers reviewing the install
script, custom paths, updates, and a credential-free check. The
[provider guide](docs/providers.md) also covers OpenCode Go and
OpenAI-compatible endpoints.

## Work with Snow

| When you want to… | Start here |
|---|---|
| Learn the terminal controls | [Using Snow](docs/using-snow.md) |
| Continue or branch a conversation | [Sessions and branches](docs/sessions.md) |
| Investigate before making changes | [Plan Mode](docs/plan-mode.md) |
| Work toward a longer objective | [Thread Goals](docs/goals.md) |
| Add reusable instructions or tools | [Agent Skills](docs/skills.md), [MCP](docs/mcp.md), [Plugins](docs/plugins.md), [JavaScript extensions](docs/plugin-extensions.md) |
| Change models, permissions, or themes | [Configuration](docs/configuration.md) |

Snow runs tools with your operating-system privileges and has no built-in
process sandbox. Review the [security model](docs/security.md) before granting
broad authority or enabling extensions.

## Automate and embed

Use print mode for a single prompt or JSON mode for streamed events:

```sh
snow --permission deny -p "explain this project"
snow --mode json --permission deny -p "summarize recent changes"
```

For applications, use the [Go SDK](docs/sdk.md) and its
[runnable example](examples/sdk), or control a long-lived process through
[JSONL RPC](docs/rpc.md). All surfaces share tools, permissions, and sessions.

## Development

Source builds require the Go 1.27 line; the available toolchain is Go 1.27rc3.
From the repository root:

```sh
go build -o snow ./cmd/snow
go test ./...
go vet ./...
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
```

Read [AGENTS.md](AGENTS.md) for repository rules and affected-area checks.
[Architecture and roadmap](IMPLEMENTATION.md) explains package boundaries and
remaining work. [Maintainer guides](docs/maintaining.md) covers releases,
performance, documentation, and design history. Provider tests use local mocks;
real-provider checks remain manual.

## Related documents

- [Documentation index](docs/README.md): find a guide or complete reference.
- [Changelog](CHANGELOG.md): release history.
- [Security reporting](SECURITY.md): report vulnerabilities privately.
