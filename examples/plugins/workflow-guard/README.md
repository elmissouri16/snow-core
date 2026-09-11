# Workflow guard

A minimal branch-local unfinished-work marker demonstrating pure lifecycle gates.
It is **off by default**.

```sh
snow --js-plugin ./examples/plugins/workflow-guard
```

`/workflow-guard on` saves the unfinished marker and updates the footer. While
on, active-session replacement, branch switching/forking, and compaction are
blocked before they commit. Automatic compaction also stops rather than bypassing
the gate. Run **`/workflow-guard off`** to allow these operations again.

The gates receive a fresh, plugin-owned workflow snapshot and cannot call host
APIs. State survives reopen/reload and follows branch ancestry. A committed
workflow write is not undone if a later UI update fails.

## Authoring and mock tests

The checked-in `main.js` requires no compiler at runtime:

```sh
cd examples/plugins/workflow-guard
npm install --save-dev esbuild typescript
npm run check && npm run build
snow plugin test . --fixtures tests/plugin.json --json
```

Edit the synchronous default factory in `src/main.ts`. While Snow is idle, apply
its built artifact using `/plugins reload workflow-guard`.

Fixtures use actual Goja and only declared fake host operations. They verify
marker reads/writes, both gate results, the off recovery command, and the session
notification's UI callback. They do not prove real lifecycle ordering or durable
branch semantics; the Go integration suite covers those host boundaries.
