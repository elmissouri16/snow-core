# Set Up Agent Skills

Agent Skills add reusable instructions and resources to Snow. This guide covers
where to place a skill, how to inspect it, and how to activate or disable it in
Snow. For the portable file format, see the
[Agent Skills specification](https://agentskills.io).

## On this page

- [Create or install a skill](#create-or-install-a-skill)
- [Choose a skill directory](#choose-a-skill-directory)
- [Check installed skills](#check-installed-skills)
- [Activate a skill](#activate-a-skill)
- [JavaScript plugin references](#javascript-plugin-references-are-a-tool-not-a-skill)
- [Enable or disable skills](#enable-or-disable-skills)
- [Safety](#safety)
- [Related documents](#related-documents)

## Create or install a skill

A skill is a directory containing `SKILL.md`. Create the file yourself or copy a
trusted skill directory into one of the locations below.

```text
.agents/skills/pdf-processing/
└── SKILL.md
```

Start with a name, a clear description, and the instructions Snow should load:

```markdown
---
name: pdf-processing
description: Extract and transform PDFs. Use for PDF files, forms, or tables.
---

# PDF processing

Follow these steps...
```

Keep the directory name the same as the skill name.

## Choose a skill directory

Install personal skills in either location:

- `~/.agents/skills/`
- `~/.snow/skills/` or `$SNOW_HOME/skills/`

Install project skills in either location:

- `<project>/.agents/skills/`
- `<project>/.snow/skills/`

Snow loads project skills only after you allow project trust. A project skill
with the same name as a personal skill takes precedence.

Add another trusted directory for one launch with:

```sh
snow --skill-dir /path/to/skills
```

To include compatible `.claude/skills/` directories, add this to
`~/.snow/config.json`:

```json
{
  "skills": {
    "include_claude": true
  }
}
```

## Check installed skills

List every discovered skill or inspect one skill:

```sh
snow skills list
snow skills get pdf-processing
```

Snow reports invalid files, blocked project directories, and disabled skills in
the command output. Add `--json` when another program needs structured output.

## Activate a skill

Mention an installed skill with its exact `$name` token in a prompt:

```text
$pdf-processing extract the tables from report.pdf
```

In the optional web manager, select **Enable installed skills** when explicitly
starting a runtime. The choice is saved per project on this manager and preselects
the checkbox on later starts and resumes, including after manager restarts.
New and existing projects default to disabled until you opt in. Uncheck it on
startup to disable skills and save that preference, or change **Settings →
Workspaces → Installed skills** for future starts. Settings are shared by this
manager's paired browsers; changing them does not alter a running worker.
Archiving/removing the registration clears the preference.

Type `$` in the composer to browse the worker's enabled/disabled skill catalog
and insert an exact mention. Picking a suggestion does not activate or execute
it: activation occurs when you send the prompt. Enabling the catalog permits
normal applicable skill activation too, not just explicit mentions. The saved
preference is separate from project activation consent and does not grant CLI
extension trust or change tool permissions. Project skills still follow the
existing trust rules above.

Snow can also activate an applicable skill while handling a request. Active
skills supply specialized methods, style, or intermediate artifacts; they do
not replace or narrow the enclosing user request. When a skill contributes one
part of explicitly requested work, Snow applies it and then continues the
remaining work with other available capabilities. Conversely, activation does
not authorize extra side effects: when the user requests only the skill's
deliverable, Snow stops after providing it. Collaboration-mode restrictions,
safety requirements, and tool permissions remain authoritative.

In the interactive TUI, run `/skills` to inspect available and active skills.
Run `/skills clear` to clear active skills for the current session branch.

## JavaScript plugin references are a tool, not a skill

For plugin creation, updates, and safe runtime registration inspection, Snow
provides the deferred read-only `snow_plugin_docs` tool. See
[Plugin authoring references](plugins.md#plugin-authoring-references) for actions
and usage. No skill installation or activation is required.

The built-in `snow-js-plugin` skill has been removed. There is no legacy alias or
automatic policy migration: an old `skills.overrides.snow-js-plugin` entry is
inert unless you provide an actual same-named filesystem skill. Such a skill
follows ordinary skill discovery, activation, and policy; it is not an alias for
the tool. Control `snow_plugin_docs` with the tool allowlist instead.
`--no-skills` and `--no-plugins` do not disable these read-only references.

## Enable or disable skills

In the TUI, open `/skills`, select a skill with the arrow keys, and press
**Enter or Space** to enable or disable its saved policy. The panel stays open
and preserves the selection. Escape closes it. The action hint changes between
`enable` and `disable`; saving errors appear in the panel without changing the
shown setting.

**Restart Snow to apply saved policy changes.** Rows display saved enablement;
`(restart)` marks a difference from the running catalog, and the detail shows the
current runtime state. Saving does not deactivate an active skill or alter
running root/child agents, tool schemas, or completion lists. Use `/skills clear`
when you want to clear branch-active instructions rather than change discovery
policy.

The detail identifies the policy scope. Personal and explicitly
located skills normally save to global configuration. Project skills save to the
startup-trusted project's configuration. An existing project named override or
project-wide default also makes the action project-scoped, so the new setting
is not silently masked by higher-precedence policy. Untrusted project policy is
never read or written by this panel. File contents are never modified.

From the CLI, enable or disable a personal skill without deleting its files:

```sh
snow skills disable pdf-processing
snow skills enable pdf-processing
```

Use `--project` to write the current project's trust-gated policy:

```sh
snow skills disable pdf-processing --project
snow skills enable pdf-processing --project
```

Disable all skill discovery for one launch with:

```sh
snow --no-skills
```

## Safety

> **Warning:** Skills are untrusted instructions. Installing or activating a
> skill does not bypass Snow's normal tool permissions, and discovery does not
> automatically run bundled scripts.

Review a skill's instructions and bundled resources before using it. Project
trust controls whether project-local skills are loaded; it is not a process or
filesystem sandbox.

## Related documents

- [MCP](mcp.md) — connect interoperable external tools and resources.
- [Plugins](plugins.md) — add Snow-specific tools and lifecycle hooks.
- [Configuration](configuration.md) — configure extension discovery and trust.
- [Security model](security.md) — understand permissions and process authority.
