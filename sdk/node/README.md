# Snow Node.js SDK

`@elmissouri16/snow-sdk` is an alpha, dependency-free Node.js client for an
explicitly started Snow JSONL RPC worker. It requires Node **>=22.12.0** and a
separately installed Snow executable supporting RPC protocol v1. It does not
embed Snow, download a binary, run an install hook, or publish anything during
its build. The current package version is `0.1.0-dev`.

Both ESM and CommonJS are supported, with separate declaration trees and
conditional `import`/`require` exports:

```js
import { SnowClient } from '@elmissouri16/snow-sdk';
// CommonJS: const { SnowClient } = require('@elmissouri16/snow-sdk');

const client = await SnowClient.start({
  executable: '/absolute/path/to/snow',
  provider: 'opencode-go',
  permission: 'deny',
});
const drain = (async () => {
  for await (const event of client.events()) {
    if (event.type === 'text_delta' && typeof event.text === 'string') {
      process.stdout.write(event.text);
    }
  }
})();
// Observe stream errors immediately, including when prompt admission fails.
const observedDrain = drain.catch((error) => {
  console.error('Event stream failed:', error);
  process.exitCode = 1;
});

try {
  const run = await client.prompt('Explain the repository architecture.');
  const completion = await run.result;
  console.log(completion.status);
} finally {
  await client.close();
  await observedDrain;
}
```

## Protocol and lifecycle

`SnowClient.start()` performs an explicit worker launch and waits for the
`rpc_ready` handshake. The default permission mode is **deny**; granting more
privilege is an application decision. Snow and its tools run with the caller's
OS privileges: this SDK is **not a sandbox**. Provider credentials and settings
remain Snow's responsibility, not this package's configuration database.

Use `events()` to consume the bounded asynchronous notification stream. Prompt
acknowledgement is not completion: `prompt()` returns a `PromptRun`, whose
`result` resolves on the matching `prompt_completed` frame. The result carries
`completed`, `failed`, or `canceled`; transport loss rejects the wait rather than
claiming success. `run.cancel()` targets only that still-active root run.
`close()` is safe to call repeatedly. Always close workers in `finally`.

Convenience methods cover common model/session operations and interactive
replies. The escape hatch is `client.rpc({ type: '…', ... }, options)`, with the
**request object**, not a positional command and arbitrary parameters. Caller
IDs are forbidden; correlation IDs belong to the transport. An abort signal or
RPC wait timeout stops the local wait, not remote execution. Do not automatically
retry an unknown outcome, switch providers, or assume an interrupted mutation
was rolled back. Headless `ask` remains fail-closed unless the application
explicitly implements the worker's trusted interactive reply flow.

## Generated public types

The canonical contract lives in
[`pkg/protocol/schema/rpc/v1`](../../pkg/protocol/schema/rpc/v1/README.md).
`npm run generate` reads only those checked-in JSON schemas. References must be
local files in that directory or canonical IDs for those files; the generator
rejects other references and disables filesystem/network resolver fallback.
`npm run generate:check` compares the entire generated source without writing.
A source-schema digest in the header makes drift visible.

Public aliases are stable even when schema titles produce different internal
compiler names: `RpcRequest`, `RpcResponse`, `RpcReady`, `PromptCompleted`,
`RpcOutput`, `AgentEvent`, `Model`, `Message`, `SessionSummary`, and `SessionInfo`
project named fields of the generated umbrella interface. Generated internal
names derive from schema titles, while expanded request branches use stable
command-derived names. Prefer the public aliases over generated internals.

`RpcCommand` is the eager-runtime command union. `RpcRequestFor<C>` preserves all
request alternatives for command `C`, omits its wire ID, and forbids caller IDs.
`RpcResponseFor<C>` includes explicit success and failure branches. Grouped
schema enums are expanded per discriminator rather than using `Extract`, which
would silently lose grouped commands. Explicit success branches take precedence
over the generic fallback, so `models_list`, `subagent_models`, and
`sessions_list` retain their DTOs. Unrestricted data is **unknown**, never `any`.
Catalog-startup-only commands are not included in the eager command map.

`RpcNotification` excludes known ready/response frames and also preserves
unknown future event frames as `{ type: string; [field: string]: unknown }`.
Narrow/check unknown fields before using them. This forward-compatible branch
is deliberately broad; reserved ready/response frames are routed separately by
the transport at runtime.

These are **static projections**, not complete runtime JSON Schema validation.
In particular, string patterns, numeric/size bounds, `not`, and conditional
`if`/`then` constraints are not all expressible by this generator. For example,
a conditional `next_cursor` requirement need not become a TypeScript
required property. Snow remains the authoritative request validator; transport
checks protect frame parsing and required handshake/correlation invariants.

## Develop, verify, and pack locally

From this directory:

```sh
npm ci --ignore-scripts
npm run generate:check
npm run typecheck
npm run build
npm test
# Equivalent generation/type/build/unit/package gate:
npm run check
# Explicit opt-in: no provider calls in the normal suite
SNOW_BIN=/absolute/path/to/snow npm run test:integration
# Local tarball only, never npm publish:
npm run pack
```

Pinned TypeScript, esbuild, Node 22 typings, and json-schema-to-typescript are
**development-only** dependencies. There are no runtime dependencies or install
hooks. Installing development dependencies uses `--ignore-scripts`.

Every artifact stays under the repository's ignored `build/` directory:

- `build/sdk-node/test/`: compiler-emitted ESM modules for source tests, with an
  explicit `type: module` marker (the root build directory is not an ESM package).
- `build/sdk-node/types/`: compiler declaration staging.
- `build/sdk-node/package/{esm,cjs}/`: bundled runtime entry points and copied
  declarations; the CJS subtree has its own `type: commonjs` marker.
- `build/dist/`: npm tarballs, created from the staged package with
  `npm pack --ignore-scripts`.
- `build/sdk-node/consumer/`: isolated package-smoke consumer installed from the
  tarball with `--ignore-scripts --offline`, not from the source package.

The normal tests are network/provider-free. Package smoke tests load the
installed tarball through both `import` and `require` and compile ESM `.mts` and
CJS `.cts` consumers using NodeNext resolution and the local pinned compiler.
Integration tests require `SNOW_BIN`; they never silently download a worker.
