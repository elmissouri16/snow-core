# Model Context Protocol

Snow is an MCP host/client built on the official
[`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).
The pinned v1.7.0 SDK negotiates the current `2026-07-28` protocol and supports
the SDK's legacy revisions back through `2024-11-05`. The modern Streamable
HTTP path is stateless (`server/discover` and request `_meta`); older servers
automatically fall back to the legacy initialization lifecycle.

## Configure servers

Global servers live in `~/.snow/config.json`. Project servers use the same
shape in `<project>/.snow/config.json` and are read only after project trust is
allowed.

```json
{
  "mcp_servers": {
    "filesystem": {
      "transport": "stdio",
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "."],
      "env": {
        "OPTIONAL_TOKEN": "${OPTIONAL_TOKEN}"
      }
    },
    "remote": {
      "transport": "streamable-http",
      "url": "https://mcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${MCP_ACCESS_TOKEN}"
      },
      "timeout_ms": 15000,
      "tool_discovery": "deferred"
    }
  }
}
```

Do not put access tokens directly in the file. Header and stdio environment
values expand `$NAME` and `${NAME}` from Snow's environment at connection time.
Interactive MCP OAuth is not yet a Snow surface; an existing bearer token can
be supplied through an expanded header.

Explicit CLI servers can be added repeatedly:

```sh
snow --mcp ./mcp.json
snow --mcp https://mcp.example.com/mcp
snow --mcp /absolute/path/to/stdio-server
snow --no-mcp
snow mcp                    # configured servers; does not start them
snow mcp list --json
snow mcp get chrome-devtools
snow mcp check [name]       # live protocol/capability status
```

`--mcp` accepts a single `ServerSpec`, Snow's `mcp_servers` map, or the common
cross-client `mcpServers` map. The public Go configuration is
`pkg/mcp.ServerSpec`; embedders pass values through
`snowsdk.Options.MCPServers` and inspect `Session.MCPServers()`.

## Manage configuration

The default mutation scope is global `~/.snow/config.json`. Add `--project` to
write `<cwd>/.snow/config.json`; project declarations remain inactive until the
project is trusted. Reads show the effective trusted configuration and identify
its source scope. `snow mcp list --all` also includes shadowed declarations.

```sh
# stdio
snow mcp add chrome-devtools -- npx -y chrome-devtools-mcp@latest

# Streamable HTTP with an environment-backed bearer token
snow mcp add remote --url https://mcp.example.com/mcp \
  --bearer-token-env MCP_ACCESS_TOKEN --timeout 15s

snow mcp disable chrome-devtools
snow mcp enable chrome-devtools
snow mcp remove chrome-devtools

# Project-local declaration or state override
snow mcp add project-tools --project -- ./bin/project-mcp
snow mcp disable remote --project
```

If a project-scope `enable`/`disable` names a declaration that exists only in
global configuration, Snow copies that declaration into the project config with
the requested state.

`add` also accepts repeatable `--env NAME`/`--env NAME=VALUE`, `--header
NAME=VALUE`, `--cwd`, `--discovery deferred|always`, `--disabled`, and
`--replace`. Duplicate adds fail without `--replace`; remove requires an exact
name. `list` and `get` never launch a process or make a network request. All
subcommands support `--json`; the bare `snow mcp` list view accepts legacy
`--mode json` instead. Legacy `--mode json` also remains accepted by inspection
subcommands. Environment values, sensitive headers, credential-like arguments,
and URL credentials are redacted from both text and JSON output.

## Capability bridge

Snow maps negotiated MCP capabilities into its existing registry:

- server tools become `mcp_<server-id>_<sanitized-tool-name>`;
- resources add `list_resources`, `read_resource`, and, when advertised,
  `resource_subscription` bridges in that server namespace;
- prompts add `list_prompts` and `get_prompt` bridges;
- text, image, structured, embedded-resource, resource-link, audio, and binary
  results are preserved in Snow-compatible content blocks or bounded base64
  text when Snow has no native block type;
- pagination cursors remain available on list tools;
- `tools/list_changed` notifications are debounced and coalesced before an
  atomic registry/BM25 refresh; refresh network I/O is timeout-bounded, does
  not hold the runtime lifecycle lock, and identical catalogs skip rebuilding
  the discovery index;
- the project directory is supplied as an MCP root for backward-compatible
  servers. Roots, sampling, and logging are deprecated in `2026-07-28`; Snow
  does not advertise sampling or elicitation handlers it cannot safely honor.

MCP tools default to deferred discovery, so only relevant schemas enter a
provider request. Set `tool_discovery` to `always` per server when every schema
must be sent on every turn. `search_tools` remains the recovery path.

## Permissions and process safety

MCP never bypasses Snow's permission service. Streamable HTTP calls are
classified as network risk; stdio calls are classified as execution risk.
They are hidden in deny mode, prompt in ask mode, and execute in allow mode.
Server annotations are untrusted hints and do not reduce that classification.

A configured stdio server is an executable child process with the user's OS
privileges. Project declarations are trust-gated, but trust is not a sandbox.
Snow discards unsolicited stdio-server stderr by default so startup notices and
diagnostic banners cannot corrupt the interactive TUI; connection failures
remain visible through MCP status and `snow mcp check`.
MCP results and server-provided instructions are external context and cannot
override system or user instructions. Tool results are bounded by
`tool_output_bytes`, static headers are never included in status output, and
shutdown closes each SDK session before the Snow session store.

## Current boundary

Core `2026-07-28` tools, resources, prompts, subscriptions, list-change
notifications, request metadata, stateless HTTP, and legacy lifecycle fallback
are supported through the official SDK. Optional extension-specific product
surfaces such as MCP Apps, Tasks, Enterprise Managed Authorization, and an
interactive OAuth callback UI are not yet exposed by Snow. Static bearer
headers and stdio environment credentials work today.
