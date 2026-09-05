# External plugin protocol removed

Snow no longer starts external plugin executables. The subprocess host,
JSON-RPC plugin protocol, `snow plugin` commands, `--plugin` flag, and public
`PluginSpec` API have been removed. Existing `plugins` configuration keys are
ignored and do not start processes.

Use [Go plugins](plugins.md) for tools and event handlers inside an embedded
Snow application, or [MCP](mcp.md) to connect an external tool server.

The [previous protocol reference](https://github.com/elmissouri16/snow-core/blob/8839ea0/docs/plugin-protocol.md)
is available for historical reference only.
