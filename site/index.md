---
layout: home
title: Snow documentation
description: >-
  Install Snow and learn to use, configure, and extend its coding agent.
permalink: /
---

> **Note:** Snow is alpha software. Commands, configuration, and public APIs may
> change before v1, so use the guides for the version you have installed.

## Start

- [Install and first prompt][getting-started] — Install Snow and confirm that
  the agent runs.
- [Providers][providers] — Connect one of Snow's supported model providers.
- [Using Snow][using-snow] — Learn the TUI and essential command-line controls.
- [Configuration][configuration] — Set common defaults after the first run.

## Workflows

- [Sessions and branches][sessions] — Return to previous work or create a
  branch.
- [Plan Mode][plan-mode] — Investigate and prepare a plan without implementing.
- [Thread Goals][goals] — Continue a bounded objective across turns.
- [Subagents][subagents] — Delegate focused work to child agents.

## Add capabilities

- [Agent Skills][skills] — Install reusable instructions for Snow.
- [MCP][mcp] — Connect a local or remote MCP server.
- [Plugins][plugins] — Register a trusted Snow-specific extension.

## Develop with Snow

- [Go SDK][sdk] — Embed Snow in a Go application.
- [Go SDK example][sdk-example] — Run the maintained standalone example.
- [Advanced references on GitHub][advanced-references] — Find RPC, plugin,
  model-input, and complete SDK references.

[getting-started]: {{ '/docs/getting-started.html' | relative_url }}
[providers]: {{ '/docs/providers.html' | relative_url }}
[using-snow]: {{ '/docs/using-snow.html' | relative_url }}
[configuration]: {{ '/docs/configuration.html' | relative_url }}
[sessions]: {{ '/docs/sessions.html' | relative_url }}
[plan-mode]: {{ '/docs/plan-mode.html' | relative_url }}
[goals]: {{ '/docs/goals.html' | relative_url }}
[subagents]: {{ '/docs/subagents.html' | relative_url }}
[skills]: {{ '/docs/skills.html' | relative_url }}
[mcp]: {{ '/docs/mcp.html' | relative_url }}
[plugins]: {{ '/docs/plugins.html' | relative_url }}
[sdk]: {{ '/docs/sdk.html' | relative_url }}
[sdk-example]: {{ '/examples/sdk/' | relative_url }}
[advanced-references]: https://github.com/elmissouri16/snow-core/blob/main/docs/README.md
