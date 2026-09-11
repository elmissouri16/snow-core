# Built-in Snow JS plugin builder

Snow ships this skill and all its resources **inside the executable**. No source
checkout, personal skill installation, project copy, or network fetch is needed.

## Invoke

In any project:

```text
$snow-js-plugin Build a plugin that saves project review notes and inserts a selected note into the prompt. Do not install it yet.
```

The exact whitespace-delimited `$snow-js-plugin` token activates the skill.
Ordinary model `activate_skill` calls cannot activate the bundled skill without
that explicit user path. The active skill continues through follow-ups and
session resume until deactivated; `/skills clear` clears active skills.

Inspect availability without activation:

```sh
snow skills get snow-js-plugin
```

Built-in inventory reports `source: builtin`, `scope: builtin`, and a virtual
`builtin:` location. Resources are immutable embedded files, accessed through
`read_skill_resource`, not OS paths. A fresh Snow launch is needed to use the
contents of a newly installed binary.

## Behavior

The agent chooses the smallest supported design, writes a runnable API 2 plugin,
adds actual-Goja fixtures, validates what it can, and documents usage, authority,
and loading. It uses commands, tools, hooks, workflow state, restrictions, UI,
storage, agent/session/goal controls, and bounded subagent composition as needed.
Building does not imply permission to register the plugin or execute its effects.

Included: current API declarations, fixture specification, fifteen host/plugin
guides, fourteen real examples, capability and example selectors, and a source
hash manifest. Resources are loaded progressively, not injected in full.

## Policy and overrides

Normal named skill enable/disable policy and `--no-skills` apply. User,
explicit-directory, and trusted project skills can override a built-in with the
same name using the existing precedence rules. Such replacements are ordinary
filesystem skills; the built-in's explicit-only runtime policy belongs to the
bundled implementation, not a new portable frontmatter field. Project overrides
still require project trust.

## Maintain the embedded snapshot

Canonical source: `internal/skills/bundled/snow-js-plugin/` in the Snow checkout.
The Go embed includes this skill directory. Do not maintain a second project
copy under `.agents/skills/`, which would shadow the built-in in this checkout.

After modifying bundled canonical docs, declarations, or examples:

```sh
python3 internal/skills/bundled/snow-js-plugin/scripts/sync_resources.py
python3 internal/skills/bundled/snow-js-plugin/scripts/sync_resources.py --check
python3 -m unittest scripts.tests.test_plugin_skill -v
```

The maintainer script is source-checkout tooling; it cannot be run from a
virtual embedded location. `--repo /path/to/snow-core` selects source explicitly.
It never installs packages, activates plugins, or runs example effects. Stale
generated resources require explicit review/removal. Review the handwritten
builder instructions and capability/example maps when the API changes.
