# Node SDK example

Credential-free JavaScript (ESM and CommonJS) and TypeScript examples use the
same `SnowClient`, stream public events, wait for definitive prompt completion,
and close the owned subprocess. The CLI `fake` provider completes successfully
without generating text. These examples do not grant tool execution authority.

## Run from a checkout

The package is not published to npm yet. From the repository root, build Snow
and the SDK, then install the local tarball without lifecycle scripts:

```sh
go build -o build/bin/snow ./cmd/snow
(cd sdk/node && npm ci --ignore-scripts && npm run build)
mkdir -p build/dist
(cd build/sdk-node/package && npm pack --ignore-scripts --pack-destination ../../dist)
(cd examples/sdk-node && npm install --ignore-scripts --no-save --package-lock=false ../../build/dist/elmissouri16-snow-sdk-0.1.0-dev.tgz)
SNOW_EXECUTABLE="$PWD/build/bin/snow" node examples/sdk-node/example.mjs
SNOW_EXECUTABLE="$PWD/build/bin/snow" node examples/sdk-node/example.cjs
SNOW_EXECUTABLE="$PWD/build/bin/snow" node --experimental-strip-types examples/sdk-node/example.ts
```

Use Node >=22.12.0. ESM uses `import { SnowClient }`; CommonJS uses
`require('@elmissouri16/snow-sdk')`. TypeScript declarations ship in the package.
Node's type stripping runs the example but does not typecheck it. To check it
with the SDK's development TypeScript installation:

```sh
sdk/node/node_modules/.bin/tsc --noEmit --strict --target ES2022 --module NodeNext --moduleResolution NodeNext --types node --typeRoots sdk/node/node_modules/@types examples/sdk-node/example.ts
```

## Opt in to a real provider

Log in separately, then deliberately choose ChatGPT. This may incur provider
usage; the example never logs in or installs Snow on your behalf:

```sh
snow login chatgpt
SNOW_PROVIDER=chatgpt SNOW_PROMPT="Say hello briefly." node examples/sdk-node/example.mjs
```

Use `SNOW_EXECUTABLE` for a non-PATH binary. `SNOW_PROMPT` overrides the prompt;
`SNOW_PROVIDER` defaults to `fake`. The example uses the launch directory and an
ephemeral session, and keeps `permission: 'deny'` for real providers too.

## Related documents

- [Node SDK guide](../../docs/node-sdk.md)
- [JSONL RPC](../../docs/rpc.md)
- [Security model](../../docs/security.md)
