# Tool Routing

Snow keeps existing tools directly available and lets future tools opt into
local progressive disclosure. Deferred tools remain fully registered and
executable, but their JSON parameter schemas are sent to the model only when a
Bleve BM25 search selects them, a lifecycle bundle is already active, or the
model explicitly discovers them through `search_tools`.

## On this page

- [Registration](#registration)
- [Built-in deferred web fetch](#built-in-deferred-web-fetch)
- [Runtime behavior](#runtime-behavior)
- [Observability and limits](#observability-and-limits)

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

## Built-in deferred web fetch

`webfetch` uses the `web` namespace and retrieval terms for URLs, pages,
links, fetching, reading, visiting, downloading, HTML, Markdown, JSON, and
webpage summarization. The five managed-process tools are also deferred under
the `managed_process` namespace. Consequently the normal app registers direct
`search_tools` even when no plugins are installed, while actual Bleve index
construction waits for the first non-empty routing search. An explicit SDK/CLI
tool allowlist that excludes every deferred tool retains the original
direct-only/no-router path.

The tool performs a static GET with Surf v1.0.203's Chrome 150 profile; it does
not execute JavaScript. It converts HTML to Markdown, passes textual formats
through as UTF-8, and rejects binary content. It is `RiskNet`, so ask mode
prompts at execution and deny mode filters it before schema exposure.

Only public HTTP(S) destinations are accepted. The transport disables
environment proxies, validates redirects, rejects private/reserved address
ranges, validates all DNS answers, and dials an approved address directly to
prevent ordinary SSRF and DNS-rebinding bypasses. TLS verification, a 30-second
maximum timeout, ten-redirect cap, existing tool-output cap, and an explicit
untrusted-content boundary are always applied.

## Runtime behavior

At startup Snow retains a compact, schema-free snapshot of deferred retrieval
metadata. The first non-empty search lazily builds two in-memory Bleve v2
indexes exactly once. The namespace index has one summary document per
namespace; the tool index has one document per tool. Both contain only
canonical/original names, namespace, description, and keywords. Full parameter
schemas, execution handlers, ownership, and risk stay in the normal registry
and are never retained by or indexed in Bleve.

Namespace summaries are deterministic and capped at 32 KiB each. They prioritize
the namespace identifier and normalized form, normalized tool names, and
deduplicated keywords before bounded descriptions. This keeps large MCP and
plugin catalogs from creating unbounded index documents.

For each user prompt Snow:

1. Searches namespace summaries and selects up to three namespaces.
2. Searches all deferred tools with BM25, preserving the field boosts below.
3. Searches again within the selected namespaces.
4. Fuses global and namespace-scoped ranks with deterministic reciprocal-rank
   fusion (weights 1.0 and 1.15, constant 60).
5. Requests 20 candidates, filters them against permission policy, and doubles
   that window only when needed to find five permitted results or exhaust the
   catalog. This prevents denied high-ranked tools from hiding a permitted
   lower-ranked result without searching the full catalog on every turn.
6. Adds the top five permitted schemas after direct schemas. Selecting any
   managed-process member expands the complete five-tool lifecycle bundle.
7. Keeps that selection for all provider continuations in the turn and runs the
   existing execution-time permission gate for every tool call.

The global result is always retained as a rescue path: the strongest global hit
is guaranteed to remain within the first five fused candidates, so a wrong
namespace choice cannot hide it from normal schema exposure. A one-namespace
catalog, a query with no namespace match, or a namespace-index search failure
preserves the original global BM25 order. A total tool-index build or search
failure invokes the bounded local metadata fallback described below.

There is no additional LLM call for routing. An MCP/plugin catalog refresh
before the first search replaces only the pending compact snapshot and remains
lazy. After initialization, refreshes build a complete replacement pair and
swap it atomically, so removed tools cannot survive in stale namespace
summaries. If a dynamic replacement cannot be built, the last valid pair
remains active until a later refresh succeeds. Both indexes close with the app.

When deferred tools exist, Snow also registers the direct, read-only
`search_tools` meta-tool. A model may search again with a more precise query;
up to five returned schemas become available on the next provider continuation.
Automatic and explicit selections are independently capped at five before
lifecycle-bundle expansion. Once any managed process record exists, its whole
bundle remains visible across turns so status, logs, and stop operations do not
require rediscovery; session rebinding clears that sticky state.

If Bleve cannot build or search, automatic routing uses a deterministic local
name/namespace/keyword/description ranker instead of exposing the complete
catalog. That fallback returns at most five permitted tools and at most 64 KiB
of serialized schemas. Router failures remain visible in `tool_routing` events.

## Observability and limits

JSON mode, RPC, plugins, and SDK subscribers receive `tool_routing` events with
the trigger, bounded selected IDs, candidate/selected/exposed counts,
provider-schema bytes, latency, and fallback state. The query itself is never
copied into the event, and the normal TUI remains quiet.

BM25 fields use boosts of 4 for tool names, 3 for namespaces, 2 for keywords,
and 1 for descriptions. Dots, underscores, and hyphens in names are also indexed
as spaces for technical-identifier matching.

Routing remains local and network-free. It uses no embeddings, remote routing
API, downloaded model, persistent vector index, or embedding cache. Optional
semantic routing remains deferred until a locally downloadable open-source model
can satisfy licensing, cross-platform, binary-size, memory, and startup-time
requirements without making a service mandatory.

MCP tools use this router, while Agent Skills use their own metadata catalog and
activation tool. Other backends can reuse it by registering ordinary tool
descriptors; routing never depends on the execution transport.

## Related documents

- [Plugins](plugins.md)
- [External plugin protocol v2](plugin-protocol.md)
- [MCP](mcp.md)
- [Agent Skills](skills.md)
- [Configuration](configuration.md)
