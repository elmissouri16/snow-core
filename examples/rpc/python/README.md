# Standalone Python RPC example

`client.py` is a dependency-free Python 3 client for Snow's persistent JSONL RPC
mode. It starts Snow, continuously reads the mixed response/event stream,
correlates `session_info` and `prompt` responses by ID, answers model-requested
questions, waits for the root `turn_done` event, and closes stdin for orderly
shutdown. It is a lifecycle example, not a reusable RPC library: `turn_done`
ends the agent turn, while a same-ID prompt failure can still arrive later as the
prompt unwinds. Long-lived hosts must keep routing responses after `turn_done`.

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

Or connect to an OpenAI-compatible endpoint:

```sh
python3 examples/rpc/python/client.py \
  --snow ./snow \
  --provider openai-compatible \
  --base-url https://gateway.example/v1 \
  --model model-id
```

The API key is optional and resolves through Snow's normal auth store or
`OPENAI_API_KEY` fallback. Pass `--session /path/to/session.db` to create or
resume a durable conversation; otherwise the client requests an ephemeral one.

The client selects permission mode `deny` and disables plugins, MCP, skills,
and subagents. It is intentionally small; a
production host should additionally define its own restart policy, structured
logging, event persistence, and provider-specific credential handling.

See [`docs/rpc.md`](../../../docs/rpc.md) for the complete protocol and ordering
rules.
