# Set Up Snow Plugins

Plugins add Snow-specific tools, lifecycle hooks, progress, and agent events.
Use an external executable with the Snow CLI, or register an in-process Go
plugin when embedding Snow. Only install plugins you trust.

## On this page

- [Choose plugins, MCP, or Agent Skills](#choose-plugins-mcp-or-agent-skills)
- [Add an external plugin](#add-an-external-plugin)
- [Enable or disable plugins](#enable-or-disable-plugins)
- [Validate a plugin](#validate-a-plugin)
- [Use an in-process Go plugin](#use-an-in-process-go-plugin)
- [Safety](#safety)
- [Related documents](#related-documents)

## Choose plugins, MCP, or Agent Skills

Choose the smallest extension type that fits the task:

| Capability | Use |
|---|---|
| Reusable instructions and resources | [Agent Skills](skills.md) |
| Interoperable external tools, resources, or prompts | [MCP](mcp.md) |
| Snow-specific tools, hooks, progress, or events | Plugins |

## Add an external plugin

An external plugin declaration identifies the process Snow should run. A
minimal `manifest.json` is:

```json
{
  "id": "my-tools",
  "command": ["/absolute/path/to/plugin-executable"],
  "enabled": false
}
```

Register the declaration, review it, and enable it for the next Snow launch:

```sh
snow plugin add manifest.json
snow plugin get my-tools
snow plugin enable my-tools
snow
```

`snow plugin add` stages a plugin disabled by default. Use `--enable` only when
you have already reviewed the executable and want it enabled on the next
launch:

```sh
snow plugin add manifest.json --enable
```

Load a manifest or executable for one launch without saving it:

```sh
snow --plugin /absolute/path/to/plugin-executable
snow --plugin manifest.json
```

Use a manifest when an interpreter and script require separate command
arguments. The `command` field is an argument array; Snow does not interpret a
shell command string.

## Enable or disable plugins

Inspect and change saved declarations without starting them:

```sh
snow plugin list
snow plugin list --all
snow plugin get my-tools
snow plugin enable my-tools
snow plugin disable my-tools
snow plugin remove my-tools
```

Add `--project` to `add`, `enable`, `disable`, or `remove` to edit the current
project's `.snow/config.json`. Snow loads project plugins only after project
trust is allowed. Restart Snow after a saved enable, disable, or removal.

Disable every configured plugin for one launch with:

```sh
snow --no-plugins
```

## Validate a plugin

Start one plugin in isolation and inspect the tools and capabilities it reports:

```sh
snow plugin check manifest.json
```

Use `--json` for structured output. Validation starts the executable, so apply
the same trust decision you would use for a normal launch.

Plugin authors should use the
[advanced plugin protocol reference](https://github.com/elmissouri16/snow-core/blob/main/docs/plugin-protocol.md)
for the complete external process contract.

## Use an in-process Go plugin

Go applications can pass implementations of `pkg/plugin.Plugin` through the
Snow SDK:

```go
session, err := snowsdk.Open(ctx, snowsdk.Options{
    NoSession: true,
    GoPlugins: []plugin.Plugin{myPlugin},
})
```

See the [Go SDK](sdk.md) for session setup and lifecycle. In-process plugins are
part of the embedding application and are not managed by `snow plugin`.

## Safety

> **Warning:** Plugin processes and in-process plugins run with the user's
> operating-system privileges. Project trust controls whether a declaration is
> loaded; it does not sandbox the loaded code.

Review the executable, arguments, working directory, environment, private
configuration, and requested capabilities before enabling a plugin. Use a
container, virtual machine, or operating-system sandbox for untrusted code.

## Related documents

- [External plugin protocol v2](https://github.com/elmissouri16/snow-core/blob/main/docs/plugin-protocol.md)
  — implement an external plugin runtime.
- [MCP](mcp.md) — connect interoperable external tools and resources.
- [Agent Skills](skills.md) — install reusable instructions and resources.
- [Security model](security.md) — understand extension authority and trust.
