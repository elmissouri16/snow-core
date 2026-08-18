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
## Multimodal prompts

When the binary announces `multimodal_prompts`, `prompt` accepts an additive
`content` array of `{type: "text"|"image", ...}` blocks via
`await snow.prompt("", { content: [imageBlock] })`. The legacy message argument
stays valid; an empty message is allowed only when an image block is present.

## Session and runtime commands

The client exposes typed wrappers for the full RPC command surface:

```js
await snow.compact();                              // {summarized_messages, retained_messages, ...}
const { branches } = (await snow.branches()).data; // [{id, name, active, ...}]
await snow.branchSelect("branch-1");
await snow.branchRename("branch-1", "new name");
await snow.branchDelete("branch-1");
const { messages } = (await snow.messages()).data; // message content blocks and usage
const usage = (await snow.usage()).data;           // {input, output, total_tokens, ...}
const pending = (await snow.pendingInputs()).data; // {items: [{kind, text, ...}]}
await snow.clearPendingInputs();
const { diagnostics } = (await snow.configurationDiagnostics()).data;
await snow.setReasoningSummary("concise");         // off|auto|concise|detailed
await snow.setTextVerbosity("high");               // low|medium|high
```

Before using these methods with an older Snow binary, inspect
`snow.ready.capabilities` for `compaction`, `branch_management`,
`messages_list`, `usage`, `pending_inputs`, `diagnostics`, and
`response_controls` as applicable. The wrappers reuse the standard `request()`
correlation and error handling.

## Interactive permissions

Permission mode `ask` remains fail-closed unless a trusted host installs a
handler or manually replies to events:

```js
const snow = await Snow.start({
  permission: "ask",
  permissionHandler: async (request, { signal }) => "allow_session",
});
```

The correlated `permission_request` event is published before the handler runs.
Handler errors or invalid decisions send `permission_reject`. Event-loop hosts
can instead call `replyPermission(requestId, decision)` or
`rejectPermission(requestId)`.

## Development

```sh
cd sdk/javascript
npm test
npm run pack:check
SNOW_TEST_BINARY=/path/to/snow npm run test:integration
```
