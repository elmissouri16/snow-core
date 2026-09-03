# Model Context Protocol

Connect Snow to Model Context Protocol (MCP) servers to add external tools,
resources, and prompts. This guide covers local and remote server
configuration, management commands, permissions, connection behavior, and
supported capabilities. For Snow-specific extensions and reusable
instructions, see [Plugins](plugins.md) and [Agent Skills](skills.md).

## On this page

- [Transports and protocol](#transports-and-protocol)
- [Configure servers](#configure-servers)
- [Connect servers on the command line](#connect-servers-on-the-command-line)
- [Manage configuration](#manage-configuration)
- [Connection lifecycle and cache](#connection-lifecycle-and-cache)
- [Capability bridge](#capability-bridge)
- [Permissions and process safety](#permissions-and-process-safety)
- [Current boundary](#current-boundary)

## Transports and protocol

Snow supports two server transports through the official Go SDK:

| Transport | Meaning |
|---|---|
| `stdio` | The server runs as an executable child process on the user's machine. |
| `streamable-http` | The server is reached over HTTP using the `2026-07-28` protocol. |

The modern Streamable HTTP path is stateless (`server/discover` and request
`_meta`); older servers automatically fall back to the SDK's legacy
initialization lifecycle. The public configuration type
is `pkg/mcp.ServerSpec`; embedders pass values through
`snowsdk.Options.MCPServers` and inspect them with `Session.MCPServers()`.

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
      "tool_discovery": "deferred",
      "lifecycle": "lazy",
      "idle_timeout_ms": 600000
    }
  }
}
```

Do not put access tokens directly in the file. Header and stdio environment
values expand `$NAME` and `${NAME}` from Snow's environment at connection time.
Interactive MCP OAuth is not yet a Snow surface; an existing bearer token can
be supplied through an expanded header.

## Connect servers on the command line

Explicit CLI servers can be added repeatedly:

```sh
snow --mcp ./mcp.json
snow --mcp https://mcp.example.com/mcp
snow --mcp /absolute/path/to/stdio-server
snow --no-mcp
snow mcp
snow mcp list --json
snow mcp get chrome-devtools
snow mcp check [name]
snow mcp cache [name]
snow mcp cache status [name]
snow mcp cache refresh <name>
snow mcp cache clear <name>
```

`--mcp` accepts a single `ServerSpec`, Snow's `mcp_servers` map, or the common
cross-client `mcpServers` map. `snow mcp` lists configured servers without
starting them. `snow mcp check` performs a live protocol/capability handshake.
`snow mcp cache` and `cache status` inspect secret-free metadata without
starting servers; `cache refresh` first validates the declaration, then uses an
explicit live operation to atomically replace a catalog only after successful
negotiation and durable storage. A refresh failure preserves the previous cache.
`cache clear` removes the selected server's current and superseded fingerprints
for this project identity.

## Manage configuration

The default mutation scope is global `~/.snow/config.json`. Add `--project` to
write `<cwd>/.snow/config.json`; project declarations remain inactive until the
project is trusted. Reads show the effective trusted configuration and identify
its source scope. `snow mcp list --all` also includes shadowed declarations.

```sh
# stdio
snow mcp add chrome-devtools --lifecycle lazy --cache-bootstrap explicit \
  -- npx -y chrome-devtools-mcp@latest
snow mcp cache refresh chrome-devtools

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
NAME=VALUE`, `--cwd`, `--discovery deferred|always`, `--lifecycle
eager|lazy|lazy-keep-alive`, `--cache-bootstrap auto|explicit`, `--idle-timeout
DURATION`, `--disabled`, and `--replace`. Duplicate adds fail
without `--replace`; remove requires an exact
name. `list` and `get` never launch a process or make a network request. All
subcommands support `--json`; the bare `snow mcp` list view accepts legacy
`--mode json` instead. Legacy `--mode json` also remains accepted by inspection
subcommands. Environment values, sensitive headers, credential-like arguments,
and URL credentials are redacted from both text and JSON output.

## Connection lifecycle and cache

Servers use `lifecycle: "eager"` by default, preserving the original behavior:
Snow connects during startup and keeps the session until shutdown. Set
`lifecycle: "lazy"` to defer an activatable server after its catalog is known.
Tools and the generic resource/prompt bridges can activate a disconnected
server. `idle_timeout_ms` controls how long an unused lazy session stays
connected and defaults to ten minutes. `lifecycle: "lazy-keep-alive"` also
starts from cache but, after its first permitted activation, retains the live
session until shutdown.

With the default `cache_bootstrap: "auto"`, the first lazy launch performs one
bounded live bootstrap to negotiate the server and cache its tool names,
descriptions, input schemas, and bounded
resource/prompt capability flags. Later launches with a valid cache reconstruct
those descriptors without starting a stdio process or making an HTTP request.
The first permitted tool or bridge call shares one connection attempt among
concurrent callers, refreshes the live catalog, validates the requested tool or
capability, and then invokes the operation. If cached metadata changed, Snow
returns a stale-catalog error rather than silently invoking a different
operation. After the last active call and idle timeout, Snow closes the session
but retains the descriptors so a later call can reconnect. A successful
resource subscription pins the live session until the corresponding
unsubscribe operation or final Snow shutdown. Subscriptions are idempotent per
URI and bounded to 128 URIs of at most 4 KiB per server; a failed unsubscribe
retains its lease so Snow does not disconnect a potentially active
subscription.

Set `cache_bootstrap: "explicit"` on a lazy lifecycle for a strict startup
transport guarantee. A valid cache is reconstructed normally. A missing,
expired, mismatched, or invalid cache leaves the server unavailable without
launching a process or making a request. Populate it deliberately with
`snow mcp cache refresh <name>`. This explicit activation path also permits a
cached empty catalog to remain disconnected; it must be refreshed to discover
new descriptors because disconnected servers cannot deliver list-change
notifications.

The versioned cache is stored at `~/.snow/cache/mcp-v1.json` (under the active
Snow home), expires after seven days, and is partitioned by server declaration
shape, scope, project/root identity, and a secret-free configuration
fingerprint. Positional arguments and flag values are represented only by
non-secret shape markers, so the persisted hash cannot act as an offline
credential verifier. Writes use a private directory, mode `0600`, a
cross-process lock, and atomic replacement. Cache records never contain
environment/header values, URL credentials or query strings, argument values,
resource contents, prompt expansions, server instructions, or bearer tokens.
Under automatic bootstrap, a corrupt, expired, mismatched, or missing entry
falls back to a live bootstrap and does not prevent Snow from starting.

Under automatic bootstrap, a server with no cached tools, resources, or prompts
has no descriptor capable of activating it and therefore uses eager fallback
so list-change notifications remain observable. Strict explicit bootstrap
keeps the same empty catalog disconnected and relies on `cache refresh`.
Resource/prompt-only servers can remain lazy because their
cached bridge descriptors provide an activation path. Permission checks still
happen before execution; denying any cached lazy tool or bridge call does not
connect to the server.

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
classified as network risk; stdio calls are classified as execution risk. They
are hidden in deny mode, prompt in ask mode, and execute in allow mode. Server
annotations are untrusted hints and do not reduce that classification.

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
interactive OAuth callback UI are outside Snow's current scope. Static bearer
headers and stdio environment credentials work today.

## Related documents

- [Plugins](plugins.md)
- [Configuration](configuration.md)
- [Security](security.md)
