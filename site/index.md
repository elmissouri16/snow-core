---
layout: home
title: Snow documentation
description: Install Snow and learn to use, configure, extend, and integrate its coding agent.
permalink: /
---

## Start with a working agent

Follow the guides in order if this is your first time using Snow:

1. [Install Snow and run your first prompt][getting-started].
2. [Configure your provider, model, and permissions][configuration].
3. [Learn the interactive terminal and command-line workflows][using-snow].
4. [Review the security boundaries][security] before granting broader access.

> **Note:** Snow is alpha software. Commands, configuration, and public APIs may
> change before v1, so use the guides for the version you have installed.

## Choose what you want to do

### Work with the agent

| Goal | Guide |
|---|---|
| Return to a conversation or create another branch | [Sessions and branches][sessions] |
| Separate investigation from implementation | [Plan Mode][plan-mode] |
| Continue a bounded objective across turns | [Thread Goals][goals] |
| Delegate focused work to child agents | [Subagents][subagents] |

### Add capabilities

| Goal | Guide |
|---|---|
| Load reusable instructions and supporting resources | [Agent Skills][skills] |
| Connect external tools and context servers | [Model Context Protocol][mcp] |
| Add trusted extension behavior | [Plugins][plugins] |

### Integrate Snow

| Goal | Guide |
|---|---|
| Embed the agent in a Go application | [Go SDK][sdk] |
| Control Snow from another process or language | [JSONL RPC][rpc] |
| Handle questions requested by the model | [Model-requested input][user-input] |
| Build a language-neutral external plugin | [Plugin protocol][plugin-protocol] |

[getting-started]: {{ '/docs/getting-started.html' | relative_url }}
[using-snow]: {{ '/docs/using-snow.html' | relative_url }}
[configuration]: {{ '/docs/configuration.html' | relative_url }}
[security]: {{ '/docs/security.html' | relative_url }}
[sessions]: {{ '/docs/sessions.html' | relative_url }}
[plan-mode]: {{ '/docs/plan-mode.html' | relative_url }}
[goals]: {{ '/docs/goals.html' | relative_url }}
[subagents]: {{ '/docs/subagents.html' | relative_url }}
[skills]: {{ '/docs/skills.html' | relative_url }}
[mcp]: {{ '/docs/mcp.html' | relative_url }}
[plugins]: {{ '/docs/plugins.html' | relative_url }}
[sdk]: {{ '/docs/sdk.html' | relative_url }}
[rpc]: {{ '/docs/rpc.html' | relative_url }}
[user-input]: {{ '/docs/user-input.html' | relative_url }}
[plugin-protocol]: {{ '/docs/plugin-protocol.html' | relative_url }}
