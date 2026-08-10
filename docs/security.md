# Security model

Snow is a coding-agent harness that runs with the current user's operating-system
privileges. It provides permission gates, path confinement, bounded I/O, trust-
gated project input, and explicit extension controls. It does **not** provide an
OS sandbox, container, virtual machine, or privilege boundary.

Use this document when operating Snow in CI, embedding the SDK/RPC server, or
enabling shell, network, plugins, MCP, skills, goals, or subagents.

## Quick safety rules

1. Use `deny` for read-oriented headless work.
2. Use `ask` only where a real interactive permission asker exists—the TUI.
3. Use `allow`/SDK `AutoApprove` only inside a deliberately trusted environment.
4. Treat project trust as permission to load project input, not as containment.
5. Treat repository text, tool output, skills, extensions, and child-agent output
   as untrusted model context.
6. Never place credentials in prompts, static MCP headers committed to source,
   logs, plugin events, or bug reports.
7. Assume shell, plugins, stdio MCP servers, and subagents can access anything
   the Snow process can access.
8. Avoid parallel mutation by subagents unless work ownership is explicit.

## Threat and control summary

| Risk | Snow control | Residual boundary |
|---|---|---|
| Accidental file mutation | `ask|allow|deny`, tool allowlists, role intersections | `allow` intentionally permits the operation |
| Path escape | Pinned `os.Root` handles, canonical roots, symlink-aware checks, Windows alias rejection | Bind mounts, device files, and processes already running as the user remain outside this boundary |
| Unbounded output/processes | Read/search/tool byte caps, shell timeout, cancellation, process-group/job cleanup | Child processes still run as the user before cancellation |
| Network access | Network risk classification; public-address-only `webfetch` | Allowed MCP/plugins/shell can implement their own networking |
| Project-supplied executable config | Canonical trust decision before project config/extension loading | Trust is not code signing or sandboxing |
| Credential disclosure | Separate `0600` auth store, redacted inventories, no secret status output | Models/tools can expose secrets the user places in readable project files or prompts |
| Prompt injection | System/user authority rules, bounded context, sealed child mail | No model-level prompt-injection defense is absolute |
| Subagent conflicts/cost | Opt-in, role/tool intersections, concurrency/depth/count/time/result limits | Children share filesystem/process side effects and incur separate provider usage |

## Permission modes

Snow classifies tool actions as:

- `read` — file/search/interaction operations that do not mutate;
- `write` — file creation or modification;
- `exec` — shell or local process execution;
- `network` — public/remote network access;
- `delegate` — starting or following up paid child-agent work.

| Mode | Read | Write/exec/network/delegate |
|---|---|---|
| `deny` | Allowed | Denied; deferred unusable tools are hidden |
| `ask` | Allowed | Prompt if an interactive asker exists; remembered session rules may apply |
| `allow` | Allowed | Allowed |

The TUI supplies an interactive asker and exposes `/allow [always]`, `/deny`,
and `/permissions`. Print, JSON, and RPC do not provide a permission-reply
command. Their `ask` mode therefore fails closed. The Go SDK also defaults to
`deny`; `UserInputHandler` answers model questions and is not a permission asker.

A tool allowlist is an additional upper bound. `--tools read,grep,glob` or
`snowsdk.Options.Tools` removes other built-ins entirely, including direct tools
such as `ask_user` if omitted.

## Project trust

Project trust controls whether Snow loads these project-local inputs:

- `.snow/config.json` plugin/MCP/skill declarations and limited preferences;
- `.snow/keybindings.yaml`, `.snow/search.yaml`, and `.snow/themes/*.yaml`;
- project Agent Skills and other trust-gated extension resources.

Snow resolves trust against a canonical project path before runtime construction.
Exact decisions are stored in `~/.snow/trust.json`; a nearest parent decision may
apply until an exact child override exists. Interactive TUI launches ask for an
undecided project. Headless launches never ask and treat `ask` as deny.

Project `AGENTS.md` files are always loaded as model instructions and are not
controlled by extension trust. They remain a prompt-injection input.

Allowing project trust means “load these declarations from this path.” It does
not mean:

- the repository is safe;
- a plugin or stdio server is sandboxed;
- shell commands are contained;
- project files cannot contain malicious instructions;
- path confinement is an OS sandbox for shell, plugins, MCP servers, or other processes.

Runtime `/trust allow|deny` changes apply on the next launch because already
loaded executable extensions cannot be safely hot-unloaded.

## File and search confinement

File tools pin allowed directories with Go's `os.Root` handles when the runtime
is built. Read, write, edit, and search file opens then operate relative to those
handles, so replacing a launch alias or racing an ancestor cannot redirect the
operation outside the configured root. Windows validation also rejects alternate
names and reserved device aliases.

Read/edit/write/grep validate the opened inode rather than trusting a separate
path stat; Unix-like hosts use nonblocking opens so a raced FIFO cannot hang the
process. `write` and `edit` stage content inside the rooted destination directory,
preserve existing modes, sync, and atomically rename the temporary file. On
Windows replacement also reopens that exact temporary handle with `WRITE_DAC`
and copies the destination DACL plus its protected/inheritable state before
rename. `edit` still requires exact matching and refuses ambiguous
replacements unless explicitly configured for all matches.

`grep` and `glob`:

- enumerate directories and read ignore files through the pinned root;
- skip symlink entries;
- always exclude `.git`;
- honor hierarchical `.gitignore` and `.ignore` by default;
- apply bounded global and trusted-project policy;
- bound matches/results and output bytes;
- support per-call soft-policy overrides without disabling hard exclusions.

This confinement applies only to built-in file operations. Shell, plugins,
stdio MCP servers, and subagents run with the user's OS privileges. Go's rooted
filesystem API also does not prohibit bind mounts, device files, or traversal of
filesystem boundaries, so Snow still is not a sandbox.

## Shell and process execution

The model-facing tool is named `bash` on every platform:

- Unix uses `sh -c`, a separate process group, and group cancellation.
- Windows uses PowerShell by default, creates the shell suspended, assigns it to
  a kill-on-close Job Object, then resumes it so descendant cleanup is covered.

Commands inherit the user's privileges and environment. Timeouts, cancellation,
and output caps reduce runaway behavior but do not prevent a command from
reading secrets, modifying files, starting network connections, or affecting
other processes before it is stopped.

An explicit Windows executable override is global-only. Project configuration
cannot choose a shell binary.

## Network access

Built-in `webfetch`:

- accepts only HTTP(S);
- disables environment proxies;
- permits only public addresses at DNS resolution and dial time;
- validates every redirect;
- verifies TLS;
- bounds time, redirects, media types, and response size;
- converts HTML to Markdown and never executes JavaScript.

These guarantees apply to `webfetch`, not to arbitrary shell commands, plugins,
MCP servers, or external tools. Streamable HTTP MCP calls remain network-risk
operations, but an allowed stdio MCP process can independently access the
network with user privileges.

## Credentials and sensitive data

Provider credentials are stored in `~/.snow/auth.json` (or the configured
`SNOW_HOME`) with mode `0600` and atomic replacement. ChatGPT refresh uses a
cross-process lock and persists rotated refresh tokens atomically. Trust data is
also locked and mode `0600`.

Snow inventories redact:

- API keys and OAuth access/refresh tokens;
- sensitive MCP headers and environment values;
- credential-like command arguments and URL credentials.

Do not rely on redaction as a data-loss-prevention system. A model can request a
readable secret file if it falls under an allowed root, and an allowed shell or
extension has the user's normal access. Keep secrets outside project roots where
possible and use narrow environment injection.

Provider continuity blocks (`provider_data`) are durable, non-rendered state.
SDK/RPC/TUI/plugin event consumers must not display or log them.

## Plugins and MCP

Statically linked plugins and external JSON-RPC v2 plugins register namespaced
tools through the central registry and permission gate. Stdio MCP servers and
external plugins are ordinary child processes, not sandboxes.

Snow's sandbox design investigation is complete, and no built-in per-extension
sandbox backend is planned now. When containment is required, run the whole Snow
process inside an appropriately constrained container or virtual machine. This
also contains bash and other in-process capabilities; wrapping only one plugin
or MCP launcher would not. Permissions and project trust remain policy and input-
loading controls, not OS isolation boundaries.

Before enabling one, review:

- executable and arguments;
- working directory;
- inherited/supplied environment;
- network endpoints and headers;
- registered tool risks and schemas;
- output/time/concurrency limits;
- project trust source.

External plugin risk defaults to `exec`; a trusted plugin may explicitly declare
`read`, `write`, or `network`. That declaration changes permission
classification but does not constrain what the child process can actually do.
MCP annotations remain untrusted hints. Plugin/MCP results and instructions are
external model context and cannot override system or user authority.

Use `snow plugin check` to inspect a runtime's declared tools, risks, subscribed
events, and bounded diagnostics. Diagnostic credential redaction is best effort;
plugins must never emit secrets. See [Plugins](plugins.md), the external
[protocol contract](plugin-protocol.md), and [MCP](mcp.md).

## Agent Skills

Skills disclose instructions progressively, but activated instructions still
become model context. Project skills are trust-gated; bundled resource reads are
confined to the discovered skill directory. Skill files and referenced scripts
are not signatures, capabilities, or sandboxes.

Review a skill before enabling or activating it, especially when it recommends
shell commands, package installation, network access, or secret-bearing tools.
See [Agent Skills](skills.md).

## Subagents

Subagents are independent agent loops with separate provider calls and optional
SQLite histories. They share the root process environment, working directory,
filesystem, and external side effects.

Safety layers include:

- disabled by default;
- separate `delegate` permission risk;
- concurrency, identity, depth, task-time, wait, and result-size bounds;
- parent tool allowlist as an upper bound;
- role-specific tool intersections;
- recursion disabled by default;
- file mutation requiring both global and role opt-in;
- goals, user input, MCP, plugins, and network tools excluded from child roles;
- persisted role-policy fingerprints that fail safe after policy changes.

Bash is still capable of mutation even when `write`/`edit` are absent. Parallel
children can race or overwrite one another. Treat role labels as policy presets,
not isolation boundaries. See [Subagents](subagents.md).

## Plan Mode, goals, and automation

Plan Mode is a collaboration instruction, not a permission boundary or OS
sandbox. Ordinary `write`, `edit`, `bash`, plugin, and MCP capabilities remain
exposed behind their normal permission gates; any allowed capability retains its
user-level power.

Persistent goals may automatically issue additional provider requests and tool
calls until completion, blocking, pause, limit, budget, abort, or failure.
Objectives and usage are branch-scoped and persisted. Large objectives are
materialized in descriptor-confined private files under `SNOW_HOME`, but goal
text still becomes trusted host-generated model context.

Use explicit budgets, restrictive permissions, and observability when enabling
automatic continuation.

## Recommended operating profiles

### Read-only repository inspection

```sh
snow --permission deny \
  --tools read,grep,glob \
  --no-plugins --no-mcp --no-skills --no-subagents \
  -p "summarize this repository"
```

### Interactive coding

```sh
snow --permission ask
```

Review each write/exec/network/delegate prompt. Use session-scoped remembered
rules narrowly.

### Trusted CI or disposable environment

```sh
snow --permission allow --no-session -p "run the approved verification"
```

Use only inside a container/VM/runner whose OS-level permissions and secrets are
already constrained. Snow's `allow` mode is not the containment mechanism.

### Headless SDK/RPC

- default to `deny`;
- pass the smallest built-in tool allowlist;
- disable unused plugins, MCP, skills, and subagents;
- set context deadlines;
- consume error/usage/tool events;
- close sessions/processes cleanly;
- move to `allow` only when the embedding host supplies external isolation and
  intends the resulting authority.
