# External plugin protocol removed

Snow no longer starts external plugin executables. The subprocess host,
JSON-RPC plugin protocol, executable-management commands, `--plugin` flag, and public
`PluginSpec` API have been removed. Existing `plugins` configuration keys are
ignored and do not start processes.

The current `snow plugin` commands manage local Goja JavaScript packages and
do not restore the retired executable protocol.

Use [Plugins](plugins.md) for the current JavaScript API and Go tools inside an embedded
Snow application, or [MCP](mcp.md) to connect an external tool server.

The [previous protocol reference](https://github.com/elmissouri16/snow-core/blob/8839ea0/docs/plugin-protocol.md)
is available for historical reference only.

## JavaScript API 2

The Go `Plugin` interface remains compatible. `pkg/plugin.Extension` is optional
and carries command, hook, readiness, host binding, and renderer contracts.
Manifest `api_version: 2` selects the promise-based local JavaScript adapter;
it is distinct from the Go plugin protocol version and the RPC wire version.
The canonical API guide is [JavaScript extensions](plugin-extensions.md).
