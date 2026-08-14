# JavaScript RPC SDK example

This Node.js 22+ example imports the checked-in zero-dependency
[`@snow-core/sdk`](../../../sdk/javascript) package, starts an external Snow
binary, inspects the session, streams root text, waits for definitive prompt
completion, and shuts down cleanly.

From the repository root:

```sh
go build -o ./snow ./cmd/snow
node examples/rpc/javascript/client.mjs ./snow "Summarize this repository."
```

The default provider is credential-free `fake`. Select an authenticated provider
through the environment:

```sh
SNOW_PROVIDER=opencode-go \
  node examples/rpc/javascript/client.mjs ./snow "Review this repository."
```

The SDK requires a separately installed or explicitly selected Snow binary; it
does not download one. Its defaults use permission `deny`, thinking `off`, an
ephemeral session, and disabled plugins, MCP, skills, and subagents.
