# Agent profiles

A branch-aware workflow example, not a new native collaboration mode.

```sh
snow --js-plugin ./examples/plugins/agent-profiles
```

Use `/profile reviewer`, `/profile architect`, `/profile debugger`, or
`/profile off`. All enabled profiles intentionally allow only `read`, `grep`,
and `glob`. The debugger investigates source without running shell commands.
Profile guidance and its tool restriction are committed in one workflow update.
A footer shows the selection. Request hooks read fresh branch state; changing
branches or reopening/reloading does not depend on cached JavaScript globals.

`off` clears only this plugin's restriction. Other plugins, native Plan Mode,
operator policy, and permissions remain authoritative. This is not an OS sandbox
and does not restrict every non-tool host operation. It does not install roles or
change Default/Plan mode.

## Edit, build, reload

The checked-in `main.js` is ready to load without Node. For authoring:

```sh
cd examples/plugins/agent-profiles
npm install --save-dev esbuild typescript
npm run check && npm run build
snow plugin test . --fixtures tests/plugin.json --json
```

Edit the synchronous default factory in `src/main.ts`; `src/entry.ts` invokes it
with the Snow global. From an idle loaded session, use
`/plugins reload agent-profiles`. Reload does not install dependencies or build.

Fixtures execute actual Goja with an explicit mock host. They verify profile
selection, atomic-update arguments, request guidance, fresh saved state, and UI
calls. They do **not** simulate real branch ancestry, enforce tool admission, or
prove permissions. Those boundaries are covered by Snow's Go integration tests.
