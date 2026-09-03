# Rebuild GitHub Pages as a concise Snow setup guide

Written against: `c449c8a`

## Status

Implemented and verified in the working tree. This revision supersedes the
site's original content and presentation plan, which was written before the
curated Pages artifact was deployed.

## Evidence chain

- Surface: the GitHub Pages homepage, shared desktop/mobile navigation, public
  setup guides, and printed/PDF guide output.
- User evidence: the published site was reported as visually unprofessional,
  difficult to scan, provider-imbalanced, and overfilled with implementation
  details.
- Source evidence: `site/index.md` repeated navigation through ordered steps and
  three tables; `site/_layouts/home.html` added a large marketing hero and four
  more cards; `site/_includes/navigation.html` promoted 16 routes, including
  wire protocols and a dedicated ChatGPT page; `docs/mcp.md`, `docs/skills.md`,
  and `docs/configuration.md` mixed setup with protocol and runtime internals.
- Format evidence: `docs/style-guide.md` reserves tables for reference data,
  but the homepage used them as its main task directory.
- Print evidence: `.prose h1` through `.prose h4` had an explicit near-white
  screen color with no dark override on the white print background.
- Owners: `site/`, the public guides under `docs/`, `scripts/build-pages.sh`,
  the Pages validators, `docs/pages.md`, and `docs/README.md`.

## Decision

Keep Snow's current Jekyll implementation and dark navy, cyan, and mint visual
identity. Improve professionalism through a calmer hierarchy, fewer competing
navigation patterns, concise task-oriented copy, and a smaller public
allowlist. Do not add a framework, external font, icon package, analytics, or
client-side application.

GitHub Pages serves ordinary Snow users. Complete API, protocol, research, and
implementation references remain in the repository and are linked only when a
public task requires them.

## Public information architecture

The shared navigation uses four groups:

```text
Start
- Overview
- Install and first prompt
- Providers
- Using Snow

Workflows
- Sessions and branches
- Plan Mode
- Thread Goals
- Subagents

Add capabilities
- Agent Skills
- MCP
- Plugins

Reference
- Configuration
- Go SDK
- Security model
```

The public guide allowlist is:

- `docs/getting-started.md`
- `docs/providers.md`
- `docs/using-snow.md`
- `docs/configuration.md`
- `docs/sessions.md`
- `docs/plan-mode.md`
- `docs/goals.md`
- `docs/subagents.md`
- `docs/skills.md`
- `docs/mcp.md`
- `docs/plugins.md`
- `docs/security.md`
- `docs/sdk.md`

Detailed ChatGPT authentication, JSONL RPC, model-requested input, the external
plugin protocol, RPC schemas, and the complete SDK reference stay on GitHub but
are not staged as Pages routes.

## Provider documentation

`docs/providers.md` owns provider setup. It gives `opencode-zen`,
`opencode-go`, `chatgpt`, and OpenAI-compatible or named profiles the same
hierarchy:

1. credential requirement;
2. minimal setup command;
3. minimal launch command;
4. optional model selection; and
5. one short provider-specific note.

`docs/getting-started.md` contains only a provider chooser and links to the
provider guide. `docs/configuration.md` retains user-editable provider fields
but not adapter construction, metadata merging, caching, affinity, reasoning
inference, or transport internals.

## Extension documentation

`docs/skills.md` explains where Snow discovers skills, how to create or install
one, how to inspect and activate it, how to enable or disable it, project trust,
and safety. It links to the Agent Skills specification rather than teaching the
standard.

`docs/mcp.md` explains local stdio setup, remote HTTP setup,
environment-backed credentials, project declarations, one-launch flags,
management commands, current Snow limitations, and process safety. It does not
teach protocol negotiation, caching, connection state, or capability bridging.

`docs/plugins.md` explains choosing Plugins versus MCP or Agent Skills, adding
and managing external plugins, validation, Go embedding, and process safety.
The complete plugin protocol remains a repository reference.

## Homepage and visual treatment

The homepage has one compact documentation hero, two primary actions, four
highlight cards, and four ordinary task lists. It does not repeat those links
through onboarding steps and table grids.

The hero reuses the normal documentation heading scale, one elevated surface,
and one border. Decorative rings, layered hero gradients, and the heavy hero
shadow are removed. The existing cards, typography, responsive navigation,
content width, focus styling, and color tokens remain.

The print stylesheet gives all prose heading levels an explicit dark color.
Desktop and mobile continue to consume one shared navigation include.

## Repository references

- `docs/sdk-reference.md` owns the complete Go SDK behavior.
- `docs/rpc.md` owns the JSONL RPC contract.
- `docs/user-input.md` owns the cross-surface input contract.
- `docs/plugin-protocol.md` owns the external plugin process contract.
- `docs/chatgpt-auth.md` owns detailed ChatGPT authentication behavior.
- `pkg/protocol/schema/rpc/v1/` owns machine-readable RPC schemas.
- `IMPLEMENTATION.md` and source/tests own runtime implementation details.
- `docs/README.md` distinguishes Pages guides from repository references.

## Verification

Required checks:

```sh
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
./scripts/build-pages.sh /fresh/pages/source
python3 scripts/check-pages-output.py /rendered/site --base-path /snow-core
python3 scripts/check_benchmarks.py
go test ./...
go vet ./...
git diff --check
```

Rendered acceptance covers desktop, tablet, mobile, and print. Confirm the same
navigation destinations across breakpoints, no task-table overflow, visible
print headings, balanced provider sections, working setup commands, and no
links to removed Pages routes.

## Stop conditions

- Do not remove a current public contract from the repository merely because it
  leaves Pages.
- Do not document a provider, MCP, skill, plugin, or SDK command without
  verifying it against current source or tests.
- Do not restore broad documentation or schema globs to silence a broken link;
  fix the link or add only the required public file.
- Do not weaken security warnings to reduce page length.
