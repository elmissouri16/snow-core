# Standalone Python RPC example

`client.py` is a dependency-free Python 3 client for Snow's persistent JSONL RPC
mode. It starts Snow, continuously reads the mixed response/event stream, tracks
responses by ID, answers model-requested questions, waits for the root
`turn_done` event, and closes stdin for orderly shutdown.

Build Snow and run a credential-free lifecycle smoke test from the repository
root:

```sh
go build -o ./snow ./cmd/snow
python3 examples/rpc/python/client.py --snow ./snow
```

Run a real streaming prompt:

```sh
export OPENCODE_API_KEY=oc-...
python3 examples/rpc/python/client.py \
  --snow ./snow \
  --provider opencode-go \
  --prompt "Summarize this repository."
```

The client selects permission mode `deny`, uses an ephemeral session, and
disables plugins, MCP, skills, and subagents. It is intentionally small; a
production host should additionally define its own restart policy, structured
logging, event persistence, and provider-specific credential handling.

See [`docs/rpc.md`](../../../docs/rpc.md) for the complete protocol and ordering
rules.
