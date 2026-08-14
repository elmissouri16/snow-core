# Snow JavaScript/TypeScript SDK

`@snow-core/sdk` is a zero-runtime-dependency Node.js 22+ client for a local Snow
process. Runtime code is native ESM JavaScript and the package ships TypeScript
declarations. The Go `snow` binary remains the agent runtime.

> Pre-alpha: npm publication is intentionally deferred. The package is marked
> private while RPC protocol v1 and cross-language conformance settle.

## Requirements

- Node.js 22 or newer.
- A compatible `snow` executable on `PATH`, or an explicit executable path.

The SDK never downloads a binary.

## Minimal example

```js
import { Snow } from "@snow-core/sdk";

const snow = await Snow.start({
  executable: "snow",
  provider: "opencode-go",
  cwd: "/path/to/project",
});

let rejectEvents;
const eventFailure = new Promise((_, reject) => { rejectEvents = reject; });
const unsubscribe = snow.subscribe((event) => {
  if (event.type === "text_delta" && !event.agent) {
    process.stdout.write(event.text ?? "");
  }
}, { onError: rejectEvents });

try {
  await Promise.race([
    snow.prompt("Review this repository"),
    eventFailure,
  ]);
} finally {
  unsubscribe();
  await snow.close();
}
```

`prompt()` resolves only after the definitive `prompt_completed` frame. The
agent's earlier `turn_done` event remains available to observers but is not
mistaken for transport-level success.

## Safe defaults

The client starts Snow with permission `deny`, thinking `off`, an ephemeral
session, and plugins, MCP, skills, and subagents disabled. These defaults do not
create a sandbox: any enabled Snow tool runs with the current user's OS
privileges.

Credentials should come from Snow's auth store or a caller-controlled inherited
environment. The SDK deliberately does not expose an API-key argument that could
leak through process listings.

## Events and discovery

```js
for await (const event of snow.events({ capacity: 256 })) {
  console.log(event.type);
}

const info = await snow.sessionInfo();
const models = await snow.models();
const childModels = await snow.subagentModels();
```

Callback subscriptions and async iterators are independent. Iterators are
bounded and fail explicitly on overflow. Unknown event types and additive fields
are preserved.

## Development

```sh
cd sdk/javascript
npm test
npm run pack:check
SNOW_TEST_BINARY=/path/to/snow npm run test:integration
```
