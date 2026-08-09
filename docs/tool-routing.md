# Tool routing

Snow keeps existing tools directly available and lets future tools opt into
local progressive disclosure. Deferred tools remain fully registered and
executable, but their JSON parameter schemas are sent to the model only when a
Bleve BM25 search selects them.

## Registration

The zero value is direct/always-loaded behavior. This keeps every existing
native and plugin tool backward-compatible. A Go plugin opts in per tool:

```go
plugin.ToolDefinition{
    Name:        "inventory_adjust",
    Description: "Increase or decrease inventory for a Shopify variant.",
    Parameters:  schema,
    Risk:        "network",
    Discovery: &protocol.ToolDiscovery{
        Mode:     protocol.ToolDiscoveryDeferred,
        Keywords: []string{"inventory", "stock", "sku", "variant"},
    },
    Executor: execute,
}
```

The namespace defaults to the plugin/server ID. Native or SDK descriptors that
do not have an owning plugin must provide a lowercase namespace explicitly.
Keywords are optional and should contain short, high-value retrieval terms.

External JSON-RPC v2 runtimes use the same optional `discovery` object in each
`tools/list` entry:

```json
{
  "name": "inventory_adjust",
  "description": "Increase or decrease inventory for a Shopify variant.",
  "parameters": {"type": "object"},
  "discovery": {
    "mode": "deferred",
    "namespace": "shopify",
    "keywords": ["inventory", "stock", "sku", "variant"]
  }
}
```

Omit `discovery`, leave its mode empty, or set the mode to `always` for normal
direct exposure.

### Built-in deferred web fetch

`webfetch` is Snow's first built-in deferred tool. It uses the `web` namespace
and retrieval terms for URLs, pages, links, fetching, reading, visiting,
downloading, HTML, Markdown, JSON, and webpage summarization. Consequently the
normal app builds the Bleve index and registers direct `search_tools` even when
no plugins are installed. An explicit SDK/CLI tool allowlist that excludes
`webfetch` retains the original direct-only/no-router path.

The tool performs a static GET with Surf v1.0.203's Windows Chrome 150 profile;
it does not execute JavaScript. It converts HTML to Markdown, passes textual
formats through as UTF-8, and rejects binary content. It is `RiskNet`, so ask
mode prompts at execution and deny mode filters it before schema exposure.

Only public HTTP(S) destinations are accepted. The transport disables
environment proxies, validates redirects, rejects private/reserved address
ranges, validates all DNS answers, and dials an approved address directly to
prevent ordinary SSRF and DNS-rebinding bypasses. TLS verification, a 30-second
maximum timeout, ten-redirect cap, existing tool-output cap, and an explicit
untrusted-content boundary are always applied.

## Runtime behavior

At startup Snow builds an in-memory Bleve v2 index containing only deferred
metadata: canonical/original name, namespace, description, and keywords. Full
parameter schemas, execution handlers, ownership, and risk stay in the normal
tool registry and are never indexed.

For each user prompt Snow:

1. Runs one local BM25 search over the current prompt.
2. Retrieves up to 20 candidates and removes deferred tools already known to be
   denied by permission policy.
3. Adds the top five permitted schemas after the direct schemas.
4. Keeps that selection for all provider continuations in the turn.
5. Runs the existing execution-time permission gate for every tool call.

There is no additional LLM call for routing. The index is rebuilt from the
authoritative startup registry for every process and is closed with the app.

When deferred tools exist, Snow also registers the direct, read-only
`search_tools` meta-tool. A model may search again with a more precise query;
up to five returned schemas become available on the next provider continuation.
The original automatic selection plus the latest explicit search are capped at
ten deferred schemas. If the Bleve index cannot build or search, Snow fails open
for functionality by exposing every permission-eligible deferred schema for
that turn.

## Observability and limits

JSON mode, RPC, plugins, and SDK subscribers receive `tool_routing` events with
the trigger, bounded selected IDs, candidate/selected/exposed counts,
provider-schema bytes, latency, and fallback state. The query itself is never
copied into the event, and the normal TUI remains quiet.

BM25 fields use boosts of 4 for tool names, 3 for namespaces, 2 for keywords,
and 1 for descriptions. Dots, underscores, and hyphens in names are also
indexed as spaces for technical-identifier matching.

Embeddings, vector fields, two-level namespace routing, persistent indexes,
and embedding caches are later layers. MCP tools now use this router, while Agent
Skills use their own metadata catalog and activation tool. Other
backends can reuse this router by registering ordinary tool descriptors; the
router never depends on the execution transport.
