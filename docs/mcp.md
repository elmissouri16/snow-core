# Set Up MCP Servers

Connect Snow to a local or remote Model Context Protocol (MCP) server when you
want to use its tools, resources, or prompts. This guide covers only the setup
and management steps required by Snow.

## On this page

- [Add a local server](#add-a-local-server)
- [Add a remote server](#add-a-remote-server)
- [Store credentials safely](#store-credentials-safely)
- [Add a project server](#add-a-project-server)
- [Check or manage servers](#check-or-manage-servers)
- [Current limitations](#current-limitations)
- [Safety](#safety)
- [Related documents](#related-documents)

## Add a local server

Register a local stdio server, then check that Snow can connect to it:

```sh
snow mcp add local-tools -- npx -y package-name
snow mcp check local-tools
```

The command writes the server to `~/.snow/config.json`. The equivalent minimal
configuration is:

```json
{
  "mcp_servers": {
    "local-tools": {
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "package-name"]
    }
  }
}
```

Use an absolute executable path when the server is not reliably available on
`PATH`.

## Add a remote server

Register a Streamable HTTP server and read its bearer token from the
environment:

```sh
export MCP_ACCESS_TOKEN=...
snow mcp add remote-tools \
  --url https://mcp.example.com/mcp \
  --bearer-token-env MCP_ACCESS_TOKEN
snow mcp check remote-tools
```

The equivalent configuration is:

```json
{
  "mcp_servers": {
    "remote-tools": {
      "transport": "streamable-http",
      "url": "https://mcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${MCP_ACCESS_TOKEN}"
      }
    }
  }
}
```

## Store credentials safely

Do not put access tokens directly in `~/.snow/config.json`. Use
`--bearer-token-env` for a remote bearer token, or reference environment
variables in `headers` and local-server `env` values:

```json
{
  "headers": {
    "Authorization": "Bearer ${MCP_ACCESS_TOKEN}"
  }
}
```

Snow expands `$NAME` and `${NAME}` when it connects. Keep the corresponding
variables in your shell, secret manager, or process environment.

## Add a project server

Add `--project` to write `<project>/.snow/config.json` instead of your personal
configuration:

```sh
snow mcp add project-tools --project -- /absolute/path/to/server
```

Snow loads project servers only after you allow project trust. Project trust is
an input-loading decision, not a sandbox.

For one launch, load an MCP declaration file, URL, or executable with `--mcp`:

```sh
snow --mcp ./mcp.json
```

Disable all configured MCP servers for one launch with:

```sh
snow --no-mcp
```

## Check or manage servers

Configuration commands do not start a server unless the command performs a
live check:

```sh
snow mcp list
snow mcp list --all
snow mcp get local-tools
snow mcp check local-tools
snow mcp enable local-tools
snow mcp disable local-tools
snow mcp remove local-tools
```

Use `--project` with `enable`, `disable`, or `remove` to change the current
project's declaration. Add `--json` to inspection and mutation commands when
another program needs structured output.

## Current limitations

- Snow supports local stdio and remote Streamable HTTP servers.
- Snow does not provide an interactive MCP OAuth callback flow.
- Supply an existing bearer token through an environment-backed header.
- Use [Plugins](plugins.md) for Snow-specific lifecycle hooks and agent events.

## Safety

> **Warning:** Local MCP servers run with your operating-system privileges.
> Remote servers receive network requests from Snow, and all server-provided
> instructions and tool results are untrusted input.

Review a server command, URL, requested credentials, and exposed tools before
enabling it. Snow's tool permission checks do not sandbox the server process
itself; use external containment for untrusted code.

## Related documents

- [Agent Skills](skills.md) — install reusable instructions for Snow.
- [Plugins](plugins.md) — register Snow-specific extensions.
- [Configuration](configuration.md) — configure extension and trust settings.
- [Security model](security.md) — understand process and network boundaries.
