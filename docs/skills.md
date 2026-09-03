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

Snow can also activate an applicable skill while handling a request. In the
interactive TUI, run `/skills` to inspect available and active skills. Run
`/skills clear` to clear active skills for the current session branch.

## Enable or disable skills

Enable or disable a personal skill without deleting its files:

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
