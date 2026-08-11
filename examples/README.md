# Snow examples

- [`sdk/`](sdk/) — standalone Go module using the public SDK and protocol packages.
- [`rpc/python/`](rpc/python/) — dependency-free persistent JSONL RPC client.
- [`plugins/javascript/`](plugins/javascript/) and
  [`plugins/python/`](plugins/python/) — external plugin protocol v2 runtimes.

The SDK and RPC examples default to the credential-free fake provider and are
executed on Linux and macOS by the hosted CI workflow.
