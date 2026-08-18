---
name: plugin-builder
description: Build a reusable Snow external protocol-v2 plugin with Snow's embedded Python or JavaScript SDK when a needed capability is missing. Use only after checking built-in tools, configured plugins, and MCP; do not build a plugin for a one-off shell task. Always stage, validate, review, and require explicit enablement and restart.
license: MIT
compatibility: Requires Snow protocol v2 and Python 3.9+ or Node.js 22+; Snow embeds the private authoring SDK sources for offline vendoring.
metadata:
  author: snow-core
  version: "1.2"
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
2. Propose the plugin ID, tools, JSON schemas, declared risks, runtime, files, exact SDK-vendoring command, build/check commands, dependencies, and whether network access is required.
3. Obtain explicit user approval before creating files, vendoring the SDK, or running build/check commands. Normal Snow write/exec/network permissions still apply.
4. Stage project-local files under `.snow/generated-plugins/<plugin-id>/`. Keep the initial manifest `enabled: false`.
5. Prefer dependency-free Python or JavaScript with Snow's embedded private authoring SDK. After approval, vendor the selected SDK beside the plugin without network access:

   ```sh
   snow plugin sdk vendor --runtime <python|javascript> .snow/generated-plugins/<plugin-id> --json
   ```

   The command copies files but does not import, build, validate, register, enable, or execute them. Never fetch the private SDK names from npm or PyPI.
6. Reserve stdout exclusively for one JSON-RPC object per line. Send diagnostics only to stderr and never print secrets.
7. Let the SDK own protocol v2 `initialize`, `tools/list`, `tools/call`, `shutdown`, cancellation, progress, bounded output, concurrency, and unexpected-error sanitization. Generated source should focus on manifest, tool schemas, risks, and handlers. Do not hand-roll protocol framing.
8. Declare the least-privileged truthful tool risk (`read`, `write`, `exec`, or `network`), but treat the generated runtime itself as executable code regardless of that declaration.
9. Review the vendoring receipt and `vendor/<runtime>/snow-sdk.json`, then compute and record SHA-256 hashes for the manifest and executable source. Ask again before executing validation, re-check every reported SDK/source/manifest hash immediately before launch, and stop if anything changed. Validate with:

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

## Runtime selection

Use an SDK template by default:

- Python: `snow_plugin.Plugin`, tool decorators, `text_result`, and `ToolError` from the private `sdk/plugin-python` package.
- JavaScript: `definePlugin`, `defineTool`, `serve`, `textResult`, and `ToolError` from the private `sdk/plugin-javascript` package.

The SDK templates load only `vendor/python` or `vendor/javascript` beside the
plugin and fail closed when the reviewed copy is absent. They never fall back
to project-relative or globally installed packages. The packages are private
and unpublished: never fetch a similarly named registry package. `snow plugin
sdk vendor` copies the SDK snapshot embedded in the current Snow binary using
staged, root-confined replacement and reports every file hash. It still mutates
executable source and therefore requires approval and review. If vendoring is
unavailable or the SDK lacks a required protocol feature, stop rather than
hand-writing a partial runtime.

## Templates

Read the SDK template, matching manifest, and protocol checklist for the chosen
runtime:

- Python SDK: `assets/plugin.py` and `assets/manifest-python.json`
- JavaScript SDK: `assets/plugin.mjs` and `assets/manifest-javascript.json`
- Shared checklist: `references/protocol-v2.md`

Replace every `PLUGIN_ID` and example tool before validation. Keep Python's
`-B` flag so validation does not add unreviewed bytecode beneath `vendor/`.
Keep `max_concurrent: 1` unless the runtime and handlers safely support
overlapping calls.

## Stop conditions

Stop and ask the user instead of continuing when:

- required behavior or authorization is unclear;
- a dependency download, credential, network endpoint, or destructive capability is needed;
- SDK vendoring fails or would replace an existing SDK without explicit `--replace` approval;
- the generated process would need broader filesystem or environment access than described;
- validation output differs from the proposed schemas/risks;
- the command or artifact changed after review;
- tests or `plugin check` fail.
