# Security model

Snow is a coding-agent harness that runs with the current user's
operating-system privileges. It provides permission gates, path confinement,
bounded I/O, trust-gated project input, and explicit extension controls. This
document is the consolidated privilege and threat-boundary reference for
operators, CI setups, and SDK/RPC embedders.

> **Note:** Snow has no built-in process sandbox. Snow, model-facing Bash,
> plugins, stdio MCP servers, and subagents run with the current user's OS
> privileges. Use an external container, VM, or OS policy for containment.

## On this page

- [Threat model](#threat-model)
- [Permission model](#permission-model)
- [Trust model](#trust-model)
- [Filesystem boundaries](#filesystem-boundaries)
- [Process boundaries](#process-boundaries)
- [Network boundaries](#network-boundaries)
- [Credential security](#credential-security)
- [Extensions risk](#extensions-risk)
- [Plan Mode and goals](#plan-mode-and-goals)
- [Recommended operating profiles](#recommended-operating-profiles)
- [Related documents](#related-documents)

## Threat model

Snow runs with the current user's OS privileges. Permission gates, path
confinement, bounded I/O, trust decisions, and extension controls reduce
accidental and injected damage, but they are not an OS-level sandbox.

| Risk | Snow control | Residual boundary |
|---|---|---|
| Accidental file mutation | `ask`/`allow`/`deny`, tool allowlists, role intersections | `allow` intentionally permits the operation |
| Path escape | Pinned `os.Root` handles, canonical roots, symlink-aware checks | Bind mounts, device files, and already-running user processes remain outside the boundary |
| Unbounded output or processes | Read/search/tool byte caps, foreground shell timeout, managed-process count/output limits, cancellation, process-group cleanup | Host children still run as the user; crashes and deliberate daemonization can escape cleanup |
| Network access | Network risk classification and public-address-only `webfetch` | Provider traffic and allowed MCP, plugins, and host shell can do their own networking |
| Project-supplied executable config | Canonical trust decision before project config or extension loading | Trust is not code signing or sandboxing |
| Credential disclosure | Separate `0600` auth store, redacted inventories, no secret status output | Models and tools can expose secrets placed in readable project files or prompts |
| Prompt injection | System/user authority rules, bounded context, sealed child mail | No model-level prompt-injection defense is absolute |
| Subagent conflicts or cost | Opt-in, role/tool intersections, concurrency/depth/count/time/result limits | Children share filesystem/process side effects and incur separate provider usage |

### Quick safety rules

- Use `deny` for read-oriented headless work.
- Use `ask` only where a real interactive asker exists (the TUI).
- Use `allow` or SDK `AutoApprove` only inside a deliberately trusted
  environment.
- Treat project trust as permission to load project input, not as containment.
- Treat repository text, tool output, skills, extensions, and child-agent
  output as untrusted model context.
- Never place credentials in prompts, static MCP headers committed to source,
  logs, plugin events, or bug reports.
- Assume plugins, stdio MCP servers, file tools, Bash, and subagents can access
  their documented host resources.
- Run Snow inside an external container, VM, or suitable OS policy when host
  process containment is required.
- Avoid parallel mutation by subagents unless work ownership is explicit.

## Permission model

Snow classifies tool actions by risk:

- `read` — file, search, and interaction operations that do not mutate;
- `write` — file creation or modification;
- `exec` — shell or local process execution;
- `network` — public or remote network access;
- `delegate` — starting or following up paid child-agent work.

| Mode | Read | Write/exec/network/delegate |
|---|---|---|
| `deny` | Allowed | Denied; deferred unusable tools are hidden |
| `ask` | Allowed | Prompt if an interactive asker exists; remembered session rules may apply, and remembered denials hide matching deferred tools |
| `allow` | Allowed | Allowed |

The TUI supplies an interactive asker and exposes `/allow [always]`, `/deny`,
and `/permissions`. The Go SDK and RPC can opt into a trusted-host interactive
permission broker: a `PermissionHandler` (Go SDK) or the `permission_reply` /
`permission_reject` RPC commands (gated by the `permission_interaction`
capability). `UserInputHandler` answers model questions and is not a permission
asker.

> **Warning:** Headless callers that select `ask` without an interactive asker
> are denied by default. The permission broker still blocks only when the
> surface is opted in with a handler or manual replies; otherwise ask-mode
> requests deny without ever blocking. SDK and RPC embedders must use `deny` or
> deliberately opt into `allow`/`AutoApprove` or an explicit interactive
> permission broker. Read-only requests never ask and `allow_session` /
> `allow_always` decisions are remembered on the service and never leave the
> process.

`deny` still permits read-risk tools. In the default registry that includes
deferred `session_search`/`session_reference` and
`artifact_read`/`artifact_grep`, which can surface prior same-project session
text, titles, and spilled tool output to the model.

A tool allowlist is an additional upper bound. `--tools read,grep,glob` or
`snowsdk.Options.Tools` removes other built-ins entirely, including direct tools
such as `ask_user` if omitted.

## Trust model

Project trust controls whether Snow loads these project-local inputs:

- `.snow/config.json` plugin, MCP, and skill declarations, a confined
  system-prompt file, and limited preferences;
- `.snow/keybindings.yaml`, `.snow/search.yaml`, and `.snow/themes/*.yaml`;
- project Agent Skills and other trust-gated extension resources.

Snow resolves trust against a canonical project path before runtime
construction. Exact decisions are stored in `~/.snow/trust.json`; a nearest
parent decision may apply until an exact child override exists. Interactive TUI
launches ask for an undecided project. Headless launches never ask and treat
`ask` as deny.

A trusted project's configured system-prompt file may replace the global or
embedded base preamble. Its path is confined beneath the canonical project root,
rejects symlink components, and is size-bounded. Project `AGENTS.md` files are
always loaded as model instructions and are not controlled by extension trust.
Snow opens each one through a pinned parent-directory handle, accepts only a
regular non-symlink file whose identity remains stable across open, and reads no
more than the remaining project-context budget. Both kinds of instruction file
remain prompt-injection inputs.

Allowing project trust means "load these declarations from this path." It does
not mean:

- the repository is safe;
- a plugin or stdio server is sandboxed;
- shell commands are contained;
- project files cannot contain malicious instructions;
- path confinement is an OS sandbox for shell, plugins, MCP servers, or other
  processes.

Runtime `/trust allow|deny` changes apply on the next launch because already
loaded executable extensions cannot be safely hot-unloaded.

### Git worktree forks

A worktree fork is an explicit host-side repository mutation. Snow invokes Git
with argument arrays and `GIT_TERMINAL_PROMPT=0`, requires a clean non-bare
source with a valid commit, validates the new branch with Git, and refuses an
existing, symlink-colliding, or source-overlapping destination. A bounded
context and timeout apply to each Git command. If child-session creation fails,
Snow asks Git to remove only the exact worktree it created and then deletes only
the newly created branch; cleanup failures are joined with the primary error.
Snow never uses `os.RemoveAll` for rollback.

A worktree is a distinct canonical project path. It does not inherit the source
path's exact trust decision. Worktree forks are therefore detached from the
running App and must be resumed in a fresh runtime, which resolves trust,
project extensions, file roots, and search policy for the destination. Git
worktrees contain metadata that refers to the source repository outside Snow's
file-tool root; that does not broaden file-tool confinement.

## Filesystem boundaries

File tools pin allowed directories with Go `os.Root` handles when the runtime is
built. Read, write, edit, and search file opens then operate relative to those
handles, so replacing a launch alias or racing an ancestor cannot redirect the
operation outside the configured root.

Read, edit, write, and grep validate the opened inode rather than trusting a
separate path stat; nonblocking opens ensure a raced FIFO cannot hang the
process. `write` and `edit` stage content inside the rooted destination
directory, sync, and atomically rename the temporary file. New files honor the
process umask, while replacements restore the existing mode. `edit` limits both
input and output to 8 MiB, caps `replace_all` at 10,000 matches, bounds diff
previews, and refuses ambiguous replacements unless configured for all matches.

`grep` and `glob`:

- enumerate directories and read ignore files through the pinned root;
- skip symlink entries;
- always exclude `.git`;
- honor hierarchical `.gitignore` and `.ignore` by default;
- apply bounded global and trusted-project policy;
- bound matches, results, output bytes, regex/glob patterns (4 KiB), and a
  single directory listing (100,000 entries);
- open each regular file once through the pinned root and pass that verified
  handle to the search operation;
- support per-call soft-policy overrides without disabling hard exclusions.

This confinement applies only to built-in file operations. Plugins, stdio MCP
servers, host-side subagent orchestration, and Bash run with the user's OS
privileges. Go's rooted filesystem API also does not prohibit bind mounts,
device files, or traversal of filesystem boundaries, so Snow is still not a
whole-process sandbox.

## Process boundaries

The foreground model-facing tool is named `bash`. It uses host `sh -c`, a
separate process group, group cancellation, and a bounded `os/exec` pipe-drain
delay. Commands inherit the user's privileges and environment.

The `process_start`, `process_status`, `process_logs`, `process_stop`, and
`process_list` tools provide app-owned background work for development servers.
Starts and stops are `exec` risk; secret-safe state and bounded output reads are
`read` risk. Each process has an opaque runtime-local handle, uses the active
project directory and inherited environment, receives `/dev/null` as stdin, and
has combined stdout/stderr continuously drained into a bounded in-memory tail.
TCP and HTTP readiness probes are loopback-only; log readiness uses RE2. Full
commands are intentionally absent from inventories, state results, and
lifecycle diagnostics.

Managed processes live across turns and branches in one active session but are
not reconstructed from PIDs or session history. Session switching is rejected
while one runs. Normal app shutdown sends group termination, escalates to group
kill, and reaps children. External plugins and stdio MCP servers also start as
process-group leaders; after their protocol-level graceful shutdown, Snow
terminates and then kills any remaining group descendants within bounded waits.
A Snow `SIGKILL`, host crash, or command that creates a new session/process group
can still leave work behind; Snow never claims durable supervisor semantics or
attempts PID reattachment after restart.

Timeouts, cancellation, count/output caps, and process-group cleanup reduce
runaway behavior but do not prevent a host command from reading secrets,
modifying files, starting network connections, or affecting other processes
before it is stopped. Snow provides no built-in container or VM backend;
operators requiring process containment must supply it outside Snow.

The TUI treats model, interaction, plugin, MCP, tool, and subprocess text as
untrusted terminal input. It removes terminal control characters before adding
Snow's own ANSI styling, disarming CSI/OSC sequences such as screen, title,
hyperlink, and clipboard controls. This prevents terminal-command injection; it
does not make displayed prose trustworthy or prevent social engineering.
Decision requests and terminal turn boundaries also receive bounded mailbox
backpressure rather than being discarded when the UI event queue is saturated.

## Network boundaries

Built-in `webfetch`:

- accepts only HTTP(S);
- disables environment proxies;
- permits only public addresses at DNS resolution and dial time;
- validates every redirect;
- verifies TLS;
- bounds time, redirects, media types, and response size;
- converts HTML to Markdown and never executes JavaScript.

These guarantees apply to `webfetch`, not to provider traffic, arbitrary shell
commands, plugins, MCP servers, or external tools. Streamable HTTP MCP calls
remain network-risk operations, but an allowed stdio MCP process can
independently access the network with user privileges.

Every user-configured OpenAI-compatible profile endpoint is an operator trust
decision. Snow sends conversation context, tool schemas and results, images
supported by model metadata, and an optional Bearer key to that origin.
Clipboard images attached with TUI `Ctrl+V` are copied into the durable session
database and sent to the selected provider; clipboard access is local to the
machine running Snow. The TUI `/login openai-compatible` flow displays and
persists the profile name and endpoint in `config.json` while capturing its key
separately through masked input into `auth.json`. Profile names isolate
credentials; they are labels, not security boundaries. URLs must be absolute
HTTP(S) without userinfo, query, or fragment; cross-origin redirects are
rejected and active keys are redacted from bounded provider errors. HTTP and
private/local endpoints are allowed deliberately, so transport security and
service behavior remain the operator's responsibility. Snow does not sandbox or
certify the endpoint.

OpenCode Zen is also an external provider boundary. Its promotional free models
have materially different retention and training terms: Ox Alpha Free is
documented as zero-retention/no-training; Big Pickle, MiMo, and Hy3 data may be
used to improve those models; NVIDIA Nemotron free routes are trial endpoints
that must not receive personal or confidential data; Muse Contributor Free may
use prompts and completions for future model training. Snow carries these
notices in model descriptions and shows them in the TUI picker, but a notice is
not a data-loss-prevention control. Do not send secrets, production
credentials, personal data, or proprietary code unless the selected provider's
current terms are acceptable. Promotional terms and availability can change.
Snow's free-only Zen catalog prevents accidental paid selection, not disclosure
to the selected free service.

ChatGPT/Codex requests send a fixed-width hash derived from Snow's session,
branch, and request purpose for provider cache and routing affinity. This avoids
sending raw local identifiers but is pseudonymous metadata, not an anonymity or
access-control boundary. Large Codex request bodies use zstd before TLS; this
reduces long-session transport size but does not attempt to hide compressed
length from the provider or a network observer.

## Credential security

Provider credentials are stored in `~/.snow/auth.json` (or the configured
`SNOW_HOME`) with mode `0600` and atomic replacement. OpenAI-compatible keys are
optional per profile; no `Authorization` header is sent when the selected
profile has no explicit or stored key. The legacy `openai-compatible` profile
may additionally use `OPENAI_API_KEY`; named profiles do not share that
fallback. A provider-scoped auth service binds explicit credentials to one
provider, centralizes explicit/store/environment precedence, and supplies
credentials to both model discovery and inference. `opencode-zen` optionally
uses the same `OPENCODE_API_KEY` environment fallback as `opencode-go`, while
persisted entries remain isolated by provider ID; without a resolved Zen key,
Snow omits `Authorization` and uses anonymous access. Snow does not silently
read or import OpenCode's own credential file. ChatGPT's OAuth driver
exchanges tokens through that service; refresh uses a provider-specific
cross-process lock and atomically persists rotated tokens without holding the
global store lock during network I/O. Trust data is also locked and mode `0600`.

> **Warning:** Auth files are written with `0600` permissions. Never print or
> log `Key`, `Access`, or `Refresh` values, and never include credentials in
> errors or tool output.

Snow inventories redact:

- API keys and OAuth access and refresh tokens;
- sensitive MCP headers and environment values;
- credential-like command arguments and URL credentials.

The lazy MCP catalog cache under `$SNOW_HOME/cache/mcp-v1.json` contains only
bounded negotiation metadata, tool schemas, and resource/prompt capability
flags. It never stores server instructions, resource/prompt content, header or environment values, URL
userinfo/query/fragment data, or argument values. Cache reuse is partitioned by
hashed project/root identity and a secret-free declaration-shape fingerprint;
positional arguments and flag values use non-secret shape markers rather than
digests, preventing offline verification of low-entropy credentials. Entries
expire after seven days. The cache directory is pinned and private, writes use a
cross-process lock plus atomic `0600` replacement, and malformed entries trigger
live bootstrap rather than application failure. A valid lazy cache performs no
transport work until a permissioned tool or resource/prompt bridge call; an
uncached automatic lazy declaration must bootstrap once at startup to create
that metadata. `cache_bootstrap: "explicit"` provides a strict no-transport
startup path: missing, invalid, expired, and mismatched entries remain
unavailable until the operator runs the explicitly live `snow mcp cache refresh
<name>` command. Cache status and clear operations do not start MCP servers.
A successful resource subscription intentionally holds the live MCP
session until unsubscribe or shutdown, so its server does not idle-disconnect.
Per-server subscription URI count and length are bounded; failed unsubscribe
operations retain their lease rather than risking an untracked live
subscription. Live tool catalogs enforce per-tool, count, pagination, and
aggregate byte limits while they are collected; Streamable HTTP response bodies
and newline-delimited stdio JSON-RPC messages are bounded before SDK decoding.

Do not rely on redaction as a data-loss-prevention system. A model can request a
readable secret file if it falls under an allowed root, and an allowed shell or
extension has the user's normal access. Keep secrets outside project roots where
possible and use narrow environment injection.

Provider continuity blocks (`provider_data`) are durable, non-rendered state.
SDK, RPC, TUI, and plugin event consumers must not display or log them.

> **Warning:** Oversized plain-text tool results may be spilled beneath
> `$SNOW_HOME/artifacts`. These artifacts can contain sensitive command or file
> output. Snow does not currently run background garbage collection for
> orphaned artifacts. Deleting a session through Snow removes its artifact
> namespace; manual database removal or interrupted cleanup can leave orphans.

Artifact directories are `0700`, files are immutable `0600`, and model-visible
references are opaque IDs scoped to the active session. Dedicated
`artifact_read`/`artifact_grep` tools return bounded text; the artifact root is
deliberately not added to ordinary `read`, `write`, `edit`, `grep`, or `glob`
roots. Protect and clean `SNOW_HOME` according to the same policy as session
databases.

## Extensions risk

Statically linked plugins and external JSON-RPC v2 plugins register namespaced
tools through the central registry and permission gate. Stdio MCP servers and
external plugins are ordinary child processes, not sandboxes.

> **Warning:** Snow has no built-in process or per-extension sandbox backend.
> Bash, plugins, MCP launchers, and subagents execute with the user's OS
> privileges.

When containment for extensions or all Snow capabilities is required, run the
whole Snow process inside an appropriately constrained container or virtual
machine. Permissions and project trust remain policy and input-loading controls,
not OS isolation boundaries.

Before enabling an extension, review:

- executable and arguments;
- working directory;
- inherited and supplied environment;
- network endpoints and headers;
- registered tool risks and schemas;
- output, time, and concurrency limits;
- project trust source.

External plugin risk defaults to `exec`; a trusted plugin may explicitly declare
`read`, `write`, or `network`. That declaration changes permission
classification but does not constrain what the child process can actually do.
MCP annotations remain untrusted hints. Plugin and MCP results and instructions
are external model context and cannot override system or user authority.

| Extension | Host privileges | Permission gate | Residual boundary |
|---|---|---|---|
| External JSON-RPC v2 plugins | Child process with the user's OS privileges | Registry plus `read`/`write`/`exec`/`network` risk classification | Declared risk is metadata, not containment |
| stdio MCP servers | Child process with the user's OS privileges | Registry plus permission gate | Annotations are untrusted hints |
| Agent Skills | No child process | Project trust for project skills | Activated instructions become model context |
| Subagents | Separate agent loops sharing OS privileges | `delegate` risk plus role and tool intersections | Share filesystem and process side effects |

The embedded `$plugin-builder` skill is guidance, not an execution capability.
Generated files still need ordinary write approval. Testing, compiling, and
`plugin check` need execution approval, and dependency downloads need network
approval. `plugin sdk vendor` performs a root-confined, staged copy of SDK
source embedded in the Snow binary and reports hashes, but the resulting files
are still untrusted executable input requiring review. SDK-first templates load
only that reviewed copy and fail closed when it is absent; unpublished SDK
names must never be resolved from a registry. `plugin check`
starts initialization code with user privileges; it is not passive validation. `plugin add` therefore persists
generated declarations disabled by default, enabling is separate and explicit,
and activation occurs
only after restart. Project trust never substitutes for generated-code review.

Use `snow plugin check` to inspect a runtime's declared tools, risks, subscribed
events, and bounded diagnostics. Diagnostic credential redaction is best effort;
plugins must never emit secrets. See [Plugins](plugins.md), the external
[protocol contract](plugin-protocol.md), and [MCP](mcp.md).

### Agent Skills

Skills disclose instructions progressively, but activated instructions still
become model context. Project skills are trust-gated; bundled resource reads are
confined to the discovered skill directory. Skill files and referenced scripts
are not signatures, capabilities, or sandboxes.

Review a skill before enabling or activating it, especially when it recommends
shell commands, package installation, network access, or secret-bearing tools.
See [Agent Skills](skills.md).

### Subagents

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

## Plan Mode and goals

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
automatic continuation. See [Plan Mode](plan-mode.md) and [Goals](goals.md).

## Recommended operating profiles

### Read-only repository inspection

```sh
snow --permission deny \
  --tools read,grep,glob \
  --no-plugins --no-mcp --no-skills --no-subagents \
  -p "summarize this repository"
```

The allowlist excludes cross-session and spill-artifact readers.

### Interactive coding

```sh
snow --permission ask
```

Review each write, exec, network, and delegate prompt. Use session-scoped
remembered rules narrowly.

### Trusted CI or disposable environment

```sh
snow --permission allow --no-session -p "run the approved verification"
```

> **Caution:** `allow` mode is not the containment mechanism. Use it only inside
> a container, VM, or runner whose OS-level permissions and secrets are already
> constrained.

### Headless SDK/RPC

The Python and JavaScript/TypeScript SDKs start an external Snow binary directly
without a shell. They do not download binaries or accept API keys as process
arguments; credentials should come from Snow's auth store or a deliberately
controlled inherited environment.

- default to `deny`;
- pass the smallest built-in tool allowlist;
- disable unused plugins, MCP, skills, and subagents;
- set context deadlines;
- consume error, usage, and tool events;
- close sessions and processes cleanly;
- move to `allow` only when the embedding host supplies external isolation and
  intends the resulting authority.

## Related documents

- [Security reporting policy](../SECURITY.md) — private vulnerability disclosure
- [Release policy](releases.md) — supported alpha versions and release handling
- [Configuration](configuration.md) — trust, auth, and runtime settings
- [Plugins](plugins.md) — external plugin lifecycle and risk declarations
- [MCP](mcp.md) — MCP transports, capabilities, and permissions
- [Subagents](subagents.md) — child-agent roles and tool intersections
