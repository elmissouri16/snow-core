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

1. user `.claude/skills/` when `skills.include_claude` is enabled;
2. `~/.agents/skills/`;
3. `~/.snow/skills/` (or `$SNOW_HOME/skills/`);
4. explicit global/CLI skill directories;
5. project `.claude/skills/` when enabled and project trust is allowed;
6. `<project>/.agents/skills/` after project trust is allowed;
7. `<project>/.snow/skills/` after project trust is allowed.

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
`activate_skill`/`deactivate_skill` enums, resource reader, or restored
active-skill context.
`--no-skills`, trust revocation, and named policy changes therefore also filter
activations persisted by an older session. The interactive TUI's `/skills`
picker is read-only; `/skills clear` durably clears every active skill on the
current branch, and `/settings` persists the global Agent Skills enable/disable
toggle, applied on the next launch.

## Progressive disclosure

Snow follows all three disclosure tiers:

1. Only each skill's name and description enter the startup system catalog.
2. `activate_skill` loads the current `SKILL.md` body when the model decides a
   task matches. An exact, whitespace-delimited `$skill-name` token anywhere in
   a prompt, steer, or follow-up is treated as an explicit activation reference
   before the next provider request, without relying on a model tool call. In
   the TUI, typing `$` after whitespace at the end of the composer opens
   autocomplete over enabled skill names and descriptions; Enter or Tab replaces
   the current token without
   submitting. Because inline references are intentional activation syntax,
   pasted text containing an exact enabled `$skill-name` token activates it too;
   wrap examples in backticks or attach punctuation when activation is not
   intended. `deactivate_skill` removes one named active skill, or `*` when the
   user explicitly requests clearing all active skills.
3. `read_skill_resource` reads one referenced script, reference, or asset on
   demand. Filesystem skills use a pinned `os.Root`; each operation verifies
   the directory identity recorded at discovery, preventing traversal, symlink
   escape, and ancestor-replacement races without retaining one file
   descriptor per skill. The same path, size, file-count, depth, and
   cancellation bounds apply to every discovered skill.

Activation returns structured, XML-escaped `<skill_content>` with the skill
directory and a bounded resource inventory. Resource files are listed through a
cancellation-aware streaming walk capped at 200 files, 2,000 directory entries,
and five levels of depth; `.git` and the language-neutral generated directories
from the default search policy are skipped.
Resources are not eagerly loaded. The dedicated reader avoids broadening the
normal `read`/`write` filesystem roots merely because a user-level skill exists
outside the project.

Activated instructions are tracked by the agent and reattached to every later
provider request. They are reconstructed from persisted activation results on
resume, so manual compaction cannot silently remove active behavioral guidance.
Repeated activation replaces the in-memory copy rather than multiplying it in
the system context. Before either a model-called or direct `$skill-name`
activation is persisted, Snow serializes the projected final system prompt and
exposed schemas against `fixed_context_budget_percent`. An activation that
would increase the runtime above that model-aware budget returns an actionable
tool error; the body, activation marker, and active set remain unchanged. Snow
never partially truncates a skill. Existing resumed active sets are preserved
for compatibility and shown as over budget by `/context` until the operator
clears or reduces them. A successful direct `$skill-name` activation writes a
branch-scoped, provider-hidden marker and emits ordinary tool lifecycle events;
resume rehydrates only those markers, never historical text that merely happens
to contain a matching token.

Active skills persist independently of collaboration mode. When the user
explicitly exits a skill workflow or requests a handoff to work that conflicts
with it, the model can call `deactivate_skill`. Snow removes the named skill
before the next provider continuation and atomically stores a branch-scoped,
provider-hidden marker with the successful tool result, so the deactivation
survives resume and compaction. `name: "*"` clears all active skills only when
the user requests that broader transition. Deactivation changes session-active
instructions; it does not disable discovery, alter policy, or delete skill
files. A later activation of the same skill takes precedence.

Every transition from Plan to Default mode clears all session-active skills
automatically. This includes Shift+Tab, `/default`, the **Implement this plan**
handoff, SDK/RPC mode changes, and prompts atomically submitted in Default mode.
It prevents a planner or read-only audit skill from surviving the planning
boundary and blocking implementation. The agent appends a branch-scoped,
provider-hidden clear marker, so the skills stay inactive after resume without
deleting their historical activation records. `/skills clear` remains available
as an optional mode-independent recovery operation, not a required handoff
step.

An explicit `--tools`/SDK tool allowlist is an upper bound for the three skill
tools as well. If `activate_skill` is omitted, skills remain in inventory with a
runtime-disabled reason and are excluded from provider context; resource-only
or deactivation-only allowlists also drop those incoherent tools. If activation
is present but `read_skill_resource` or `deactivate_skill` is omitted, the
catalog advertises only the lifecycle capabilities actually available.

Skill instructions and resources remain untrusted input. Bundled scripts run
only through normal Snow tools and their permission policy; discovering or
activating a skill never executes a script automatically.

## Related documents

- [Plugins](plugins.md)
- [MCP](mcp.md)
- [Tool routing](tool-routing.md)
- [Configuration](configuration.md)
- [Security](security.md)
