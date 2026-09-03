---
layout: home
title: Snow documentation
description: Install, configure, extend, and safely operate the Snow coding-agent harness.
permalink: /
---

## Start where you are

Snow is one streaming agent loop exposed through an interactive terminal,
print and JSON modes, JSONL RPC, and a pure-Go SDK. These guides describe the
same runtime from first prompt through advanced integration.

- New users should begin with [installation and quick start][quick-start] and
  then read [Using Snow][using-snow].
- Operators should review [Configuration][configuration] and the
  [Security model][security] before granting write, process, network, plugin,
  MCP, or subagent authority.
- Integrators can choose the [Go SDK][sdk] or [JSONL RPC][rpc] without creating
  a second agent loop.
- Contributors can open the [complete documentation index][docs-index] for
  architecture, release, extension, and design-history references.

## One runtime, deliberate control

Every surface observes the same normalized event stream, permission service,
session tree, provider adapters, tools, and compaction behavior. Start with the
smallest authority required for the task, keep provider credentials out of
project files, and use external containment when process isolation matters.

> **Note:** Snow is alpha software. Public APIs, protocols, configuration, and
> persisted formats may change before v1. Current source and tests remain the
> behavioral authority.

## Popular guides

| Goal | Guide |
|---|---|
| Resume, branch, fork, or compact a conversation | [Sessions][sessions] |
| Separate planning from implementation | [Plan Mode][plan-mode] |
| Continue a bounded objective across turns | [Thread Goals][goals] |
| Delegate bounded child work | [Subagents][subagents] |
| Connect external tools and context | [Model Context Protocol][mcp] |
| Add trusted extension behavior | [Plugins][plugins] and [Agent Skills][skills] |
| Prepare and verify a release | [Release policy][releases] |

[quick-start]: {{ '/README.html#quick-start' | relative_url }}
[using-snow]: {{ '/docs/using-snow.html' | relative_url }}
[configuration]: {{ '/docs/configuration.html' | relative_url }}
[security]: {{ '/docs/security.html' | relative_url }}
[sdk]: {{ '/docs/sdk.html' | relative_url }}
[rpc]: {{ '/docs/rpc.html' | relative_url }}
[docs-index]: {{ '/docs/README.html' | relative_url }}
[sessions]: {{ '/docs/sessions.html' | relative_url }}
[plan-mode]: {{ '/docs/plan-mode.html' | relative_url }}
[goals]: {{ '/docs/goals.html' | relative_url }}
[subagents]: {{ '/docs/subagents.html' | relative_url }}
[mcp]: {{ '/docs/mcp.html' | relative_url }}
[plugins]: {{ '/docs/plugins.html' | relative_url }}
[skills]: {{ '/docs/skills.html' | relative_url }}
[releases]: {{ '/docs/releases.html' | relative_url }}
