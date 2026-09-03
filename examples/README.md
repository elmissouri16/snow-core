# Snow examples

- [`sdk/`](sdk/) — standalone Go module using the public SDK and protocol packages.
- [`plugins/javascript/`](plugins/javascript/) and
  [`plugins/python/`](plugins/python/) — external plugin protocol v2 runtimes.

The Go SDK example defaults to the credential-free fake provider and is
executed on Linux and macOS by the hosted CI workflow. The plugin examples use
only their language runtimes and the external protocol.
