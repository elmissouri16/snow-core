# Security model

Snow runs with the current user's operating-system privileges. Permission
gates, project trust, path confinement, bounded I/O, and extension controls
reduce accidental or injected damage, but they do not create a sandbox.

> **Warning:** Snow, model-facing Bash, plugins, stdio MCP servers, and
> subagents can act with the current user's OS privileges. Use an external
> container, VM, or OS policy when you need process containment.

## On this page

- [Understand the boundary](#understand-the-boundary)
- [Choose a permission mode](#choose-a-permission-mode)
- [Review project trust](#review-project-trust)
- [Protect files and processes](#protect-files-and-processes)
- [Control network access](#control-network-access)
- [Protect credentials and diagnostics](#protect-credentials-and-diagnostics)
- [Review extensions and subagents](#review-extensions-and-subagents)
- [Use Plan Mode and goals safely](#use-plan-mode-and-goals-safely)
- [Verify release installation](#verify-release-installation)
- [Choose an operating profile](#choose-an-operating-profile)
- [Related documents](#related-documents)

## Understand the boundary

Snow treats repository text, project instructions, skills, MCP and plugin
output, tool results, retrieved history, and child-agent output as untrusted
model context. No model-level prompt-injection defense is absolute.

Use these basic rules:

- keep `deny` for read-oriented headless work;
- use `ask` only when a trusted interactive permission broker exists;
- use `allow` only in a deliberately trusted or externally isolated
  environment;
- grant project trust only after reviewing project-local Snow configuration;
- disable extension families you do not need;
- never put credentials in prompts, repository configuration, logs, or bug
  reports;
- give parallel subagents disjoint ownership; and
- run Snow inside external containment when host access must be restricted.

## Choose a permission mode

Snow classifies tool work as `read`, `write`, `exec`, `network`, or `delegate`.
Choose the mode with `--permission` or `/permissions`:

| Mode | Read | Other risks |
|---|---|---|
| `deny` | Allowed | Denied |
| `ask` | Allowed | Ask through the trusted interactive broker |
| `allow` | Allowed | Allowed |

The TUI can ask interactively. Print and JSON modes fail closed for `ask`
because they have no permission broker. SDK and RPC hosts must explicitly
provide a trusted broker; otherwise `ask` also denies.

Remembered session approvals are conveniences, not containment. A permission
decision authorizes the classified operation; it does not make a command,
extension, endpoint, or model response trustworthy.

## Review project trust

Snow asks before loading project-local `.snow/config.json`, theme, keybinding,
plugin, MCP, Agent Skills, system-prompt, or trusted-project instruction files.
Trust applies only to the exact canonical project root and is checked again if
that identity changes.

Project `AGENTS.md` files are different: Snow loads them as untrusted model
instructions within a bounded context budget. They do not grant tool authority.

Use `/trust` to inspect the current decision. A Git worktree fork starts with
its own trust decision because it has a different path and working tree.

> **Note:** Project trust permits Snow to load project input. It is not code
> signing, permission approval, or a process sandbox.

## Protect files and processes

Snow's built-in file and search tools stay within configured roots, reject
symlink escapes, and bound input and output. Tool-result artifacts are private,
session-scoped files under `SNOW_HOME`; protect that directory like a session
database.

Model-facing Bash and managed processes do not share those file-tool
confinement guarantees. They can read or change anything the current user can
access. Managed-process timeouts, output limits, and shutdown cleanup reduce
runaway work but cannot undo side effects. A crash, `SIGKILL`, or deliberately
detached process may leave work running.

The TUI strips terminal control sequences from untrusted output before adding
its own styling. Displayed prose can still mislead a user, so review commands
and permission prompts rather than trusting presentation alone.

## Control network access

The built-in `webfetch` tool:

- accepts HTTP(S) only;
- blocks private and local addresses;
- validates redirects;
- verifies TLS; and
- bounds time, redirects, media type, and response size.

These restrictions do not apply to provider traffic, Bash, plugins, MCP
servers, or other external processes.

Every OpenAI-compatible endpoint is an operator trust decision. Snow sends the
conversation, tool schemas and results, and supported attachments to that
origin. Private/local and plain HTTP endpoints are allowed deliberately, so
you must evaluate transport and service security.

OpenCode Zen promotional models can have different retention and training
terms. Read the current notice in Snow's model picker before sending personal,
confidential, or proprietary data. Availability and terms can change.

## Protect credentials and diagnostics

Snow stores credentials separately from configuration in
`$SNOW_HOME/auth.json` with mode `0600`. Prefer `snow login`, masked TUI login,
or environment variables instead of command-line keys, which can appear in
shell history or process listings.

Snow does not print API keys, OAuth tokens, or secret header values in normal
status output. Do not put credentials in:

- `config.json`;
- project files readable by the agent;
- static MCP headers committed to source;
- prompts or goals;
- plugin events or tool output; or
- diagnostics and public issue reports.

Use environment expansion for MCP bearer headers. ChatGPT OAuth uses a local
callback when available and supports device-code fallback; do not paste codes
or tokens into prompts.

Diagnostic capture is opt-in and bounded, but dumps can still contain prompts,
model output, tool previews, paths, URLs, errors, and model identifiers. Review
a dump before sharing it and delete it when no longer needed.

## Review extensions and subagents

Plugins and stdio MCP servers are child processes with user privileges. Agent
Skills add untrusted instructions to model context. Subagents start additional
agent loops that share filesystem and process side effects.

Before enabling an extension, review its:

- executable and arguments;
- working directory and environment;
- network destinations and headers;
- registered tools and declared risks; and
- project-trust source.

Risk declarations affect Snow's permission gate but do not constrain what an
extension process can actually do. `snow plugin check` starts plugin code; it
is not passive validation. MCP annotations are also untrusted hints.

Disable unused capabilities with:

```sh
snow --no-plugins --no-mcp --no-skills --no-subagents
```

Review Agent Skills before activation, especially instructions that recommend
shell commands, downloads, or secret-bearing tools. Avoid parallel subagent
mutation unless each child has explicit, disjoint ownership.

## Use Plan Mode and goals safely

Plan Mode adds a non-mutation policy independent of ordinary permission
approval. Snow hides and rejects mutating tools, arbitrary Bash, process
lifecycle operations, unsafe extensions, and mutation-capable child work until
the controlling surface explicitly switches to Default.

This is defense in depth, not OS isolation. Read tools and the Snow process
still have user-level access.

Thread Goals may continue without further user prompts in Default mode. Use an
optional token budget, monitor usage, and pause or clear a goal when work should
stop. Plan Mode and user aborts stop automatic continuation until it is
explicitly eligible again.

## Verify release installation

The release installer downloads the requested archive and `SHA256SUMS`,
verifies the SHA-256 checksum, checks the binary-reported version, and replaces
the destination atomically. It does not provide an independent signature.

The one-line installer command streams `scripts/install.sh` into `sh`. Unless
`SNOW_NO_MODIFY_PATH=1` is set, the installer attempts a profile update that
persistently adds its directory to `PATH`. A skipped or failed profile update
produces a warning and may require manual `PATH` configuration. Review the
script before piping it into a shell. Use an exact `SNOW_VERSION` when
reproducibility matters, and obtain the release checksum through a separately
trusted channel when you need stronger provenance.

## Choose an operating profile

### Read-only repository inspection

```sh
snow --permission deny \
  --tools read,grep,glob \
  --no-plugins --no-mcp --no-skills --no-subagents
```

### Interactive coding

```sh
snow --permission ask
```

Review each mutation or command before approval. Keep unfamiliar extensions
disabled.

### Trusted CI or disposable environment

```sh
snow --permission allow --no-session -p "run the approved verification"
```

Use this only in an externally isolated environment with short-lived
credentials, a restricted working directory, and explicit network controls.

### Headless SDK or RPC host

Start with `deny`, use the smallest tool allowlist, set context deadlines, and
disable unused capabilities. Install a trusted permission broker before using
`ask`; move to `allow` only when the host deliberately supplies equivalent
external isolation.

## Related documents

- [Security reporting
  policy](https://github.com/elmissouri16/snow-core/blob/main/SECURITY.md) —
  report vulnerabilities privately
- [Published
  releases](https://github.com/elmissouri16/snow-core/releases) — available
  alpha versions
- [Configuration](configuration.md) — trust, credentials, and runtime settings
- [Agent Skills](skills.md) — install and activate trusted skills
- [Plugins](plugins.md) — plugin setup and process risks
- [MCP](mcp.md) — server setup and credential handling
- [Subagents](subagents.md) — child roles, limits, and shared authority
