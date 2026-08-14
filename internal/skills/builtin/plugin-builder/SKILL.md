---
name: plugin-builder
description: Build a reusable Snow external protocol-v2 plugin when a needed capability is missing. Use only after checking built-in tools, configured plugins, and MCP; do not build a plugin for a one-off shell task. Always stage, validate, review, and require explicit enablement and restart.
license: MIT
compatibility: Requires Snow protocol v2 and a locally installed Python 3.9+ or Node.js runtime.
metadata:
  author: snow-core
  version: "1.0"
---
# Supervised Snow plugin builder

Use this workflow only for a missing, reusable capability with a stable typed interface. Prefer, in order:

1. an existing built-in/deferred tool;
2. an already configured plugin or MCP server;
3. ordinary `bash` for a one-off operation;
4. MCP for a reusable integration that should work across agent hosts;
5. a Snow plugin for Snow-specific lifecycle, progress, private configuration, or observation events.

Never silently execute or persist generated code. Project trust is not execution approval, plugin risk labels are self-declared metadata, and even `snow plugin check` starts the runtime with the user's OS privileges.

## Required workflow

1. Search the available tools first. Explain why a plugin is preferable to a one-off command or MCP.
2. Propose the plugin ID, tools, JSON schemas, declared risks, runtime, files, commands, dependencies, and whether network access is required.
3. Obtain explicit user approval before creating files or running build/check commands. Normal Snow write/exec/network permissions still apply.
4. Stage project-local files under `.snow/generated-plugins/<plugin-id>/`. Keep the initial manifest `enabled: false`.
5. Prefer dependency-free Python or JavaScript. Do not install packages or forward credentials unless separately approved.
6. Reserve stdout exclusively for one JSON-RPC object per line. Send diagnostics only to stderr and never print secrets.
7. Implement protocol v2 `initialize`, `tools/list`, `tools/call`, and `shutdown`. Handle malformed input, cancellation where practical, bounded outputs, and tool errors.
8. Declare the least-privileged truthful tool risk (`read`, `write`, `exec`, or `network`), but treat the generated runtime itself as executable code regardless of that declaration.
9. Test the runtime, then compute and record SHA-256 hashes for the reviewed manifest and executable source/artifact. Ask again before executing validation, re-check the hashes immediately before launch, and stop if anything changed. Validate with:

   ```sh
   snow plugin check .snow/generated-plugins/<plugin-id>/manifest.json --json
   ```

10. Report the source diff, artifact hashes, exact command/CWD/environment, dependencies, declared tools/risks, validation output, and remaining limitations.
11. Register it disabled:

    ```sh
    snow plugin add .snow/generated-plugins/<plugin-id>/manifest.json --project
    ```

    This step mutates project configuration but does not run the plugin.
12. Only after explicit review approval, enable it for the next launch:

    ```sh
    snow plugin enable <plugin-id> --project
    ```

13. Report that restart is required. Never attempt same-session hot loading or automatically retry a previously failed operation after restart.

## Templates

Read only the template needed for the chosen runtime:

- `assets/plugin.py`
- `assets/manifest-python.json`
- `assets/plugin.mjs`
- `assets/manifest-javascript.json`
- `references/protocol-v2.md`

Replace every `PLUGIN_ID` and example tool before validation. Keep `max_concurrent: 1` unless the runtime safely correlates concurrent requests and writes.

## Stop conditions

Stop and ask the user instead of continuing when:

- required behavior or authorization is unclear;
- a dependency download, credential, network endpoint, or destructive capability is needed;
- the generated process would need broader filesystem or environment access than described;
- validation output differs from the proposed schemas/risks;
- the command or artifact changed after review;
- tests or `plugin check` fail.
