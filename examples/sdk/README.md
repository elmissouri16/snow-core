# Standalone Go SDK example

This directory is a separate Go module that imports Snow only through its public
`pkg/snowsdk` and `pkg/protocol` packages. The checked-in `replace` directive
points at the repository root so CI verifies the example against the current
checkout.

Run the credential-free lifecycle smoke test:

```sh
cd examples/sdk
go run .
```

The built-in fake provider deliberately emits no text; the example still checks
the complete open, subscribe, readiness, prompt, `turn_done`, and close lifecycle.
Run a real streaming prompt with an authenticated provider:

```sh
export OPENCODE_API_KEY=oc-...
go run . -provider opencode-go -prompt "Summarize this repository."
```

When copying this module outside the repository, remove the `replace` line from
`go.mod` and select a published Snow version:

```sh
go get github.com/snow-core/snow@latest
go mod tidy
```

The example uses permission mode `deny` and disables plugins, MCP, skills, and
subagents. Deliberately relax those options only in an appropriately trusted or
isolated environment.
