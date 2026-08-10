# Agent Skills

Snow implements the open [Agent Skills specification](https://agentskills.io)
as a resource/progressive-disclosure layer, not as an executable plugin type.
A skill is a directory with a required `SKILL.md`, optional `scripts/`,
`references/`, and `assets/`, and YAML frontmatter followed by Markdown.

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
`metadata`, and experimental `allowed-tools` fields. `name` and `description`
are required. Cosmetic naming violations are diagnosed but loaded leniently
for cross-client compatibility; missing descriptions or unparseable YAML are
skipped. `allowed-tools` is metadata only and never bypasses Snow permissions.

## Discovery and precedence

The startup scan is bounded and metadata-only. Default locations, from lower
to higher precedence, are:

1. `~/.agents/skills/`
2. `~/.snow/skills/` (or `$SNOW_HOME/skills/`)
3. explicit global/CLI skill directories
4. `<project>/.agents/skills/` after project trust is allowed
5. `<project>/.snow/skills/` after project trust is allowed

Set `skills.include_claude` to also scan user/project `.claude/skills/`
locations. Project skills always override same-named user skills. Snow-native
locations override cross-client locations within the same scope. Collisions,
malformed files, bounds, and trust-blocked project directories are retained as
diagnostics. The global discovery limit counts every candidate directory,
including malformed and duplicate-name skills, and stops all remaining roots
when reached so shadowing cannot bypass the startup I/O bound.

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

SDK callers use `snowsdk.Options.SkillDirs`, `NoSkills`, and
`Session.Skills()` for active entries. `Session.SkillInventory()` also returns
policy-disabled entries for management/status surfaces.

`enable` and `disable` update policy only; they never modify or delete skill
files. Mutations target global configuration by default, while `--project`
writes the current project's trust-gated `.snow/config.json`. Project policy
overrides global policy, named overrides take precedence over scope-wide
`disabled`, and runtime `--no-skills` remains absolute. Disabled skills appear
in inventory with their reason but never enter the startup catalog,
`activate_skill` enum, or resource reader. The interactive TUI's `/skills`
picker is read-only; `/settings` persists the global Agent Skills enable/disable
toggle, applied on the next launch.

## Progressive disclosure

Snow follows all three disclosure tiers:

1. Only each skill's name and description enter the startup system catalog.
2. `activate_skill` loads the current `SKILL.md` body when the model decides a
   task matches, or when the user explicitly names `$skill-name`.
3. `read_skill_resource` reads one referenced script, reference, or asset on
   demand and confines the path to that skill directory.

Activation returns structured `<skill_content>` with the skill directory and a
bounded resource inventory. Resource files are listed but not eagerly loaded.
The dedicated reader avoids broadening the normal `read`/`write` filesystem
roots merely because a user-level skill exists outside the project.

Activated instructions are tracked by the agent and reattached to every later
provider request. They are reconstructed from persisted activation results on
resume, so manual compaction cannot silently remove active behavioral guidance.
Repeated activation replaces the in-memory copy rather than multiplying it in
the system context.

Skill instructions and resources remain untrusted input. Bundled scripts run
only through normal Snow tools and their permission policy; discovering or
activating a skill never executes a script automatically.
