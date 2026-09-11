# Embedded Snow plugin documentation

`snow_plugin_docs` is a deferred, read-only native tool for creating and updating
compatible JavaScript plugins and inspecting available plugin registrations.
Snow embeds `internal/plugindocs/resources/` inside the executable. No source
checkout, personal skill installation, project copy, or network fetch is needed.
This is not an Agent Skill and does not add plugin execution authority.

## Use

Ask Snow to build or update a plugin, for example:

```text
Build a plugin that saves project review notes and inserts a selected note into the prompt. Do not install it yet.
```

The agent discovers `snow_plugin_docs` through ordinary deferred-tool routing or
`search_tools`. `overview` orients to the references; `list` and `search` paginate
using `offset`/`limit`; `read` uses a resource-relative `path`, 1-based line
`offset`, and maximum line `limit`; `plugins` lists safe runtime registrations or
returns details for `plugin_id`. Example resource reads:

```json
{"action":"read","path":"GUIDE.md","offset":1,"limit":120}
{"action":"read","path":"api/snow.d.ts","offset":1,"limit":100}
```

Resources are immutable embedded files, not OS paths and not extracted to a
cache or project directory. Use ordinary rooted file tools to inspect and edit
an existing user package. A fresh Snow launch is needed to use the reference
contents of a newly installed binary.

## Behavior and policy

`GUIDE.md` covers the smallest supported design, runnable API 2 packages,
actual-Goja fixtures, validation, usage, authority, loading, and a conservative
update-existing workflow. Existing IDs/configuration/state and behavior must be
preserved unless an intentional change is authorized. Runtime registration
metadata, loaded inventory, and offline bundled examples are distinct; inventory
does not execute disabled packages. Building does not authorize registration,
enabling, reload, or execution, and nothing here provides an OS sandbox.

Included: current API declarations, fixture specification, fifteen host/plugin
guides, fourteen real examples, capability and example selectors, and a source
hash manifest. Resources are read progressively rather than injected in full.

There is no legacy skill alias or automatic policy migration. An old
`skills.overrides.snow-js-plugin` entry is inert unless a user supplies an actual
same-named filesystem skill. The new tool is controlled through the tool
allowlist, not skill policy. `--no-skills` and `--no-plugins` do not disable these
read-only references; plugin runtime execution remains separately controlled.

## Maintain the embedded snapshot

Canonical docs, declarations, and examples remain authoritative. Do not edit
generated copies under `resources/docs/`, `resources/api/`, or
`resources/examples/`. After modifying a bundled source:

```sh
python3 internal/plugindocs/scripts/sync_resources.py
python3 internal/plugindocs/scripts/sync_resources.py --check
python3 -m unittest scripts.tests.test_plugin_docs -v
```

The maintainer script is source-checkout tooling and is not embedded as a runtime
resource. `--repo /path/to/snow-core` selects source explicitly. It never installs
packages, enables plugins, or runs example effects. Stale generated resources
require explicit review/removal. Review handwritten `resources/GUIDE.md`,
`resources/CAPABILITIES.md`, and `resources/EXAMPLES.md` when the API changes.
`resources/SOURCES.json` retains content hashes for all 66 synchronized files;
it is a checkout snapshot, not a released-version compatibility guarantee.
