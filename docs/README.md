# Snow documentation

New to Snow? Follow [Getting started](getting-started.md), then
[Using Snow](using-snow.md). The same user guides are available on the
[documentation website](https://elmissouri16.github.io/snow-core/).

## Get started

- [Install and first prompt](getting-started.md): install, launch, and update.
- [Providers](providers.md): connect OpenCode, ChatGPT, or your own endpoint.
- [Using Snow](using-snow.md): terminal controls, commands, and CLI modes.
- [Security model](security.md): understand permissions and host privileges.

## Daily work

- [Sessions and branches](sessions.md): resume, fork, and revisit earlier work.
- [Plan Mode](plan-mode.md): investigate and review a plan before implementing.
- [Thread Goals](goals.md): continue an objective across turns with a budget.
- [Subagents](subagents.md): delegate focused tasks and inspect their progress.
- [Configuration](configuration.md): models, paths, permissions, and themes.

## Extend Snow

- [Agent Skills](skills.md): add reusable instructions.
- [MCP](mcp.md): connect local or remote tools and resources.
- [Plugins](plugins.md): install JavaScript extensions or embed Go plugins.
- [JavaScript extensions](plugin-extensions.md): TUI views, commands, hooks, and agent workflows.
- [Tool routing](tool-routing.md): discover tools only when needed.

## Build with Snow

| Task | Guide | Complete reference |
|---|---|---|
| Embed in Go | [Go SDK](sdk.md) · [Example](../examples/sdk) | [SDK reference](sdk-reference.md) |
| Control Snow from another process | [CLI modes](using-snow.md#choose-a-runtime-mode) | [JSONL RPC](rpc.md) |
| Write a JavaScript or Go plugin | [Plugins](plugins.md) | [SDK reference](sdk-reference.md) |
| Handle model questions in a host app | [Answer model questions](using-snow.md#answer-model-questions) | [User input](user-input.md) |
| Diagnose ChatGPT login | [Providers](providers.md) | [ChatGPT authentication](chatgpt-auth.md) |

## Maintain and contribute

Start with [AGENTS.md](../AGENTS.md) for the change workflow, or
[Architecture and roadmap](../IMPLEMENTATION.md) for the codebase structure.
The [maintainer index](maintaining.md) holds release procedures, performance
checks, documentation ownership, and historical research.

Source code and tests define current behavior. Design plans and research record
past decisions and may describe work that has since changed.

## Related documents

- [Known bugs](../bugs.md): reproducible defects and verified fixes.
- [Changelog](../CHANGELOG.md): release history.
- [Security reporting](../SECURITY.md): private vulnerability disclosure.
