# Agent Skills

Snow implements the open [Agent Skills specification](https://agentskills.io)
as a resource and progressive-disclosure layer, not as an executable plugin
type. A skill is a directory with a required `SKILL.md`, optional `scripts/`,
`references/`, and `assets/`, and YAML frontmatter followed by Markdown.

## On this page

- [Skill format](#skill-format)
- [Discovery and precedence](#discovery-and-precedence)
- [Manage skills](#manage-skills)
- [Progressive disclosure](#progressive-disclosure)

## Skill format

```text
.agents/skills/pdf-processing/
├── SKILL.md
├── scripts/
├── references/
└── assets/
```

```markdown
---
name: pdf-processing
description: Extract and transform PDFs. Use for PDF files, forms, or tables.
license: Apache-2.0
compatibility: Requires pdftotext for OCR-free extraction.
metadata:
  author: example
  version: "1.0"
allowed-tools: Bash(pdftotext:*) Read
---

# PDF processing

Follow these steps...
```

Snow parses the standard `name`, `description`, `license`, `compatibility`,
`metadata`, and experimental `allowed-tools` fields. It strictly validates the
required fields, standard top-level keys, field types, Unicode character limits,
NFKC-normalized lowercase alphanumeric/hyphen names, and parent-directory
match. Nonconformant or unparseable files are diagnosed and excluded from the
runtime catalog. `allowed-tools` is preserved as metadata only and never
bypasses Snow permissions.

## Discovery and precedence

The startup scan is bounded and metadata-only. Sources, from lower to higher
precedence, are:

1. immutable skills embedded in the Snow binary;
2. user `.claude/skills/` when `skills.include_claude` is enabled;
3. `~/.agents/skills/`;
4. `~/.snow/skills/` (or `$SNOW_HOME/skills/`);
5. explicit global/CLI skill directories;
6. project `.claude/skills/` when enabled and project trust is allowed;
7. `<project>/.agents/skills/` after project trust is allowed;
8. `<project>/.snow/skills/` after project trust is allowed.

Snow currently embeds `plugin-builder`, a supervised playbook and template set
for creating external protocol-v2 plugins when a reusable capability is missing.
It can be activated explicitly with `$plugin-builder`. Like every skill, it is
instructional only: file creation, validation, configuration changes, and shell
execution remain separate permissioned operations. A valid same-named
filesystem skill shadows the built-in.

Set `skills.include_claude` to also scan user/project `.claude/skills/`
locations. Project skills always override same-named user skills. Snow-native
locations override cross-client locations within the same scope. Collisions,
malformed files, bounds, and trust-blocked project directories are retained as
diagnostics. The global discovery limit counts every candidate directory,
including malformed and duplicate-name skills, and stops all remaining roots
when reached so shadowing cannot bypass the startup I/O bound. A separate 64 KiB
catalog budget admits higher-precedence skills first. Overflow entries remain in
inventory with an explicit disabled reason instead of being partially disclosed;
every enabled skill therefore contributes its complete name and description.

```json
{
  "skills": {
    "dirs": ["/opt/company-agent-skills"],
    "include_claude": true,
    "overrides": {
      "legacy-review": false
    }
  }
}
```

## Manage skills

```sh
snow --skill-dir /opt/company-agent-skills
snow --no-skills
snow skills list
snow skills get pdf-processing
snow skills disable pdf-processing
snow skills enable pdf-processing
snow skills disable --all
snow skills enable --all
snow skills disable pdf-processing --project
snow skills list --json
```

SDK callers use `snowsdk.Options.SkillDirs`, `NoSkills`, and `Session.Skills()`
for active entries. `Session.SkillInventory()` also returns policy-disabled
entries for management/status surfaces.

`enable` and `disable` update policy only; they never modify or delete skill
files. Mutations target global configuration by default, while `--project`
writes the current project's trust-gated `.snow/config.json`. Project policy
overrides global policy, named overrides take precedence over scope-wide
`disabled`, and runtime `--no-skills` remains absolute. Disabled skills appear
in inventory with their reason but never enter the startup catalog,
`activate_skill` enum, resource reader, or restored active-skill context.
`--no-skills`, trust revocation, and named policy changes therefore also filter
activations persisted by an older session. The interactive TUI's `/skills`
picker is read-only; `/settings` persists the global Agent Skills enable/disable
toggle, applied on the next launch.

## Progressive disclosure

Snow follows all three disclosure tiers:

1. Only each skill's name and description enter the startup system catalog.
2. `activate_skill` loads the current `SKILL.md` body when the model decides a
   task matches. A prompt, steer, or follow-up beginning with `$skill-name` is
   treated as an explicit activation directive before the next provider
   request, without relying on a model tool call. Requiring the directive at
   the start avoids activating tokens embedded in pasted or quoted untrusted
   text. In the TUI, typing a leading `$` opens autocomplete over enabled skill
   names and descriptions; Enter or Tab inserts the selected directive without
   submitting.
3. `read_skill_resource` reads one referenced script, reference, or asset on
   demand. Filesystem skills use a pinned `os.Root`; each operation verifies
   the directory identity recorded at discovery, preventing traversal, symlink
   escape, and ancestor-replacement races without retaining one file
   descriptor per skill. Built-in resources use the immutable embedded
   filesystem and the same path, size, file-count, depth, and cancellation
   bounds.

Activation returns structured, XML-escaped `<skill_content>` with the skill
directory and a bounded resource inventory. Resource files are listed through a
cancellation-aware streaming walk capped at 200 files, 2,000 directory entries,
and five levels of depth; `.git` and `node_modules` directories are skipped.
Resources are not eagerly loaded. The dedicated reader avoids broadening the
normal `read`/`write` filesystem roots merely because a user-level skill exists
outside the project.

Activated instructions are tracked by the agent and reattached to every later
provider request. They are reconstructed from persisted activation results on
resume, so manual compaction cannot silently remove active behavioral guidance.
Repeated activation replaces the in-memory copy rather than multiplying it in
the system context. A successful direct `$skill-name` activation writes a
branch-scoped, provider-hidden marker and emits ordinary tool lifecycle events;
resume rehydrates only those markers, never historical text that merely happens
to contain a matching token.

An explicit `--tools`/SDK tool allowlist is an upper bound for the two skill
tools as well. If `activate_skill` is omitted, skills remain in inventory with a
runtime-disabled reason and are excluded from provider context; a resource-only
allowlist also drops the incoherent names-only reader. If only the resource
reader is omitted, activation remains available without advertising that
reader.

Skill instructions and resources remain untrusted input. Bundled scripts run
only through normal Snow tools and their permission policy; discovering or
activating a skill never executes a script automatically.

## Related documents

- [Plugins](plugins.md)
- [MCP](mcp.md)
- [Tool routing](tool-routing.md)
- [Configuration](configuration.md)
- [Security](security.md)
