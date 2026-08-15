# Security model

Snow is a coding-agent harness that runs with the current user's operating-system
privileges. It provides permission gates, path confinement, bounded I/O, trust-
gated project input, and explicit extension controls. It does **not** sandbox the
whole Snow process. An optional external smolvm backend can route only the
model-facing Bash tool through a project-scoped Linux VM; every other capability
retains its documented host boundary.

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
7. Assume plugins, stdio MCP servers, file tools, and subagents can access their
   documented host resources. Assume Bash can too unless the header/status says
   its smolvm project association is active.
8. Treat the optional smolvm backend as Bash-only containment, never as
   containment for Snow, providers, extensions, webfetch, or file tools.
9. Avoid parallel mutation by subagents unless work ownership is explicit.

## Threat and control summary

| Risk | Snow control | Residual boundary |
|---|---|---|
| Accidental file mutation | `ask|allow|deny`, tool allowlists, role intersections | `allow` intentionally permits the operation |
| Path escape | Pinned `os.Root` handles, canonical roots, and symlink-aware checks | Bind mounts, device files, and processes already running as the user remain outside this boundary |
| Unbounded output/processes | Read/search/tool byte caps, shell timeout, cancellation, and process-group cleanup | Host children still run as the user; smolvm guest cancellation also depends on its CLI/VM behavior |
| Network access | Network risk classification; public-address-only `webfetch`; optional smolvm guest network off unless explicit | Provider traffic and allowed MCP/plugins/host shell can implement their own networking; the Bash VM does not contain those paths |
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
| `ask` | Allowed | Prompt if an interactive asker exists; remembered session rules may apply, and remembered denials hide matching deferred tools |
| `allow` | Allowed | Allowed |

The TUI supplies an interactive asker and exposes `/allow [always]`, `/deny`,
and `/permissions`. Print, JSON, and RPC do not provide a permission-reply
command. Their `ask` mode therefore fails closed. The Go SDK also defaults to
`deny`; `UserInputHandler` answers model questions and is not a permission asker.

`deny` still permits read-risk tools. In the default registry that includes
deferred `session_search`/`session_reference` and
`artifact_read`/`artifact_grep`, which can surface prior same-project session
text, titles, and spilled tool output to the model.

A tool allowlist is an additional upper bound. `--tools read,grep,glob` or
`snowsdk.Options.Tools` removes other built-ins entirely, including direct tools
such as `ask_user` if omitted.

## Project trust

Project trust controls whether Snow loads these project-local inputs:

- `.snow/config.json` plugin/MCP/skill declarations, a confined system-prompt file, and limited preferences;
- `.snow/keybindings.yaml`, `.snow/search.yaml`, and `.snow/themes/*.yaml`;
- project Agent Skills and other trust-gated extension resources.

Snow resolves trust against a canonical project path before runtime construction.
Exact decisions are stored in `~/.snow/trust.json`; a nearest parent decision may
apply until an exact child override exists. Interactive TUI launches ask for an
undecided project. Headless launches never ask and treat `ask` as deny.

A trusted project's configured system-prompt file may replace the global or
embedded base preamble. Its path is confined beneath the canonical project root,
rejects symlink components, and is size-bounded. Project `AGENTS.md` files are
always loaded as model instructions and are not controlled by extension trust.
Both remain prompt-injection inputs.

Allowing project trust means “load these declarations from this path.” It does
not mean:

- the repository is safe;
- a plugin or stdio server is sandboxed;
- shell commands are contained unless an independent operator-owned smolvm
  association is active for that exact project;
- project files cannot contain malicious instructions;
- path confinement is an OS sandbox for shell, plugins, MCP servers, or other processes.

Runtime `/trust allow|deny` changes apply on the next launch because already
loaded executable extensions cannot be safely hot-unloaded.

## File and search confinement

File tools pin allowed directories with Go's `os.Root` handles when the runtime
is built. Read, write, edit, and search file opens then operate relative to those
handles, so replacing a launch alias or racing an ancestor cannot redirect the
operation outside the configured root.

Read/edit/write/grep validate the opened inode rather than trusting a separate
path stat; nonblocking opens ensure a raced FIFO cannot hang the process.
`write` and `edit` stage content inside the rooted destination directory, sync,
and atomically rename the temporary file. New files honor the process umask,
while replacements restore the existing mode. `edit` limits both input and output to 8 MiB, caps
`replace_all` at 10,000 matches, bounds diff previews, and still refuses
ambiguous replacements unless explicitly configured for all matches.

`grep` and `glob`:

- enumerate directories and read ignore files through the pinned root;
- skip symlink entries;
- always exclude `.git`;
- honor hierarchical `.gitignore` and `.ignore` by default;
- apply bounded global and trusted-project policy;
- bound matches/results and output bytes;
- support per-call soft-policy overrides without disabling hard exclusions.

This confinement applies only to built-in file operations. Plugins, stdio MCP
servers, and host-side subagent orchestration run with the user's OS privileges;
Bash does too unless this exact project has an active operator-owned smolvm
association. Go's rooted filesystem API also does not prohibit bind mounts,
device files, or traversal of filesystem boundaries, so Snow still is not a
whole-process sandbox.

## Shell and process execution

The model-facing tool is named `bash`. Without a sandbox association it uses
host `sh -c`, a separate process group, group cancellation, and a bounded
`os/exec` pipe-drain delay. Commands inherit the user's privileges and
environment. Timeouts, cancellation, and output caps reduce runaway behavior but
do not prevent a host command from reading secrets, modifying files, starting
network connections, or affecting other processes before it is stopped.

### Optional project-scoped smolvm backend

Operational setup, profile, persistence, lifecycle, and recovery guidance is in
[Sandboxed Bash with smolvm](sandbox.md). This section is the canonical security
boundary.

An operator may initialize a persistent smolvm Linux machine for the exact
canonical project with `snow sandbox init`. The association lives in the
operator-owned `$SNOW_HOME/sandboxes.json`, not project configuration. It is
exact-keyed (no parent inheritance), versioned, `0600`, atomically replaced, and
protected by context-aware interprocess state and per-project lifecycle locks.
An already running Snow process uses its startup/lifecycle snapshot, so another
process cannot silently switch its execution backend mid-session.

An explicit init may bootstrap a missing default `smolvm` command. Snow fetches
the version-tagged upstream 1.8.1 installer and platform release archive over
HTTPS, enforces hard size caps and embedded SHA-256 values for both, and runs the
installer with a scrubbed environment against only the already verified local
archive. Shell-profile changes are disabled, and Snow validates the resulting
symlink, executable, and version. This is remote code execution with the user's
host privileges, deliberately authorized by the
init command—not guest networking or a permission-gated model tool call. It
writes upstream's `~/.smolvm`, `~/.local/bin`, and platform data locations.
A user-home installation lock serializes concurrent init calls across projects.
Snow never auto-installs for a custom configured executable, checksum drift, or
an existing unsupported binary. The no-argument Ubuntu path also performs an
anonymous host-side registry download (no Docker credential lookup), verifies
the digest through the registry client, preflights declared content and bounds
the archive writer against a 2 GiB cap, and applies a 10-minute deadline. The Docker-save archive is `0600`
inside a `0700` staging directory under operator-owned Snow state; Snow hashes it
again immediately before smolvm imports it and deletes it afterward.

For the currently audited smolvm `1.8.x` line (minimum `1.8.1`), Snow creates a
deterministically named, labeled machine from a digest-pinned Ubuntu 24.04
multi-platform index by default, with:

- exactly one host volume: canonical project directory to `/workspace` (or the
  configured absolute guest path), optionally read-only;
- guest runtime networking off by default; `--network` persists smolvm's `--net`
  authority until the association is deleted/recreated. CLI init without a
  profile or `--network` instead downloads the configured digest-pinned image over host HTTPS to a private temporary
  Docker-save archive, so registry acquisition grants no guest network authority.
  Selecting a built-in development profile is a separate explicit choice that
  always persists guest network authority; profile images remain digest-pinned;
- explicit CPU/memory limits plus optional storage/overlay disk sizes selected
  by global config, CLI init flags, or the supervised TUI setup form;
- no host SSH agent, Docker socket, control socket, secret, home, or extra volume;
- foreground `machine exec --stream` with the existing output/timeout bounds;
- a strict environment-name allowlist (`LANG`, `LC_ALL`, and `TERM` by default),
  not wholesale host environment forwarding into the guest.

Snow validates the pinned executable/version when an active runtime is assembled
and again before status/lifecycle/exec operations. It rejects older and
unaudited future smolvm minor/major versions instead of assuming their
flag/default behavior. An optional installed-CLI contract test
checks the required verbs and flags; ordinary tests use a fake launcher and do
not claim hypervisor containment. On Linux smolvm also requires usable KVM; on
macOS it requires its supported Hypervisor.framework setup.

While a published record is active, failure to resolve the pinned CLI, corrupt
state, VM/start/exec failure, timeout, or cancellation is a Bash error. Snow
**never falls back to host Bash for that call**. `sandbox stop` is a distinct,
explicit policy change: after the backend stop succeeds and the stopped state is
atomically persisted, subsequent Bash routes to the host until `sandbox start`
restores VM routing. A failed start/stop state update retains or rolls back to
the previous routing boundary. Guest cancellation sends SIGINT to the
smolvm process group first, then SIGKILLs the entire launcher group after a
bounded grace. Because guest-process semantics still depend on smolvm, operators
should verify cancellation for critical long-running workloads.

`delete --force` deletes the VM before removing the association; failures retain
the association's current routing policy (active remains fail-closed; stopped
remains explicit host routing). `delete --force --forget` is an explicit recovery
path for a VM removed outside Snow or a stale record. Successful deletion or
forgetting warns that subsequent Bash calls use the host.

This VM boundary applies only to Bash. Snow and its provider traffic, built-in
file/search tools, webfetch, plugins, MCP servers, credentials, and host-side
subagent orchestration remain outside it. A project mounted read-only for Bash
may still be changed by Snow's host `write`/`edit` tools when separately allowed.

## Network access

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

A user-configured `openai-compatible` endpoint is an operator trust decision.
Snow sends conversation context, tool schemas/results, images supported by model
metadata, and an optional Bearer key to that origin. Clipboard images attached
with TUI Ctrl+V are copied into the durable session database and sent to the
selected provider; clipboard access is local to the machine running Snow. The TUI
`/login openai-compatible` flow displays and persists the endpoint in
`config.json` while capturing the key separately through masked input into
`auth.json`. URLs must be absolute HTTP(S) without userinfo/query/fragment;
cross-origin redirects are rejected and
active keys are redacted from bounded provider errors. HTTP and private/local
endpoints are allowed deliberately, so transport security and service behavior
remain the operator's responsibility. Snow does not sandbox or certify the
endpoint.

ChatGPT/Codex requests send a fixed-width hash derived from Snow's session,
branch, and request purpose for provider cache/routing affinity. This avoids
sending raw local identifiers but is pseudonymous metadata, not an anonymity or
access-control boundary. Large Codex request bodies use zstd before TLS; this
reduces long-session transport size but does not attempt to hide compressed
length from the provider or a network observer.

## Credentials and sensitive data

Provider credentials are stored in `~/.snow/auth.json` (or the configured
`SNOW_HOME`) with mode `0600` and atomic replacement. The
`openai-compatible` key is optional; no `Authorization` header is sent when
explicit, stored, and `OPENAI_API_KEY` sources are all empty. ChatGPT refresh uses a
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

Oversized plain-text tool results may be spilled beneath
`$SNOW_HOME/artifacts`. Artifact directories are `0700`, files are immutable
`0600`, and model-visible references are opaque IDs scoped to the active
session. Dedicated `artifact_read`/`artifact_grep` tools return bounded text;
the artifact root is deliberately not added to ordinary `read`, `write`,
`edit`, `grep`, or `glob` roots. Artifacts may contain sensitive command or file
output. Snow does not currently garbage-collect them, and deleting a session
database does not remove its artifact namespace, so protect and clean
`SNOW_HOME` according to the same policy as session databases.

## Plugins and MCP

Statically linked plugins and external JSON-RPC v2 plugins register namespaced
tools through the central registry and permission gate. Stdio MCP servers and
external plugins are ordinary child processes, not sandboxes.

No built-in per-extension sandbox backend is planned now. The optional smolvm
integration routes only the model-facing Bash tool; it does not wrap plugins or
MCP launchers. When containment for extensions or all Snow capabilities is
required, run the whole Snow process inside an appropriately constrained
container or virtual machine. Permissions and project trust remain policy and
input-loading controls, not OS isolation boundaries.

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

The embedded `$plugin-builder` skill is guidance, not an execution capability.
Generated files still need ordinary write approval. Testing, compiling, and
`plugin check` need execution approval, and dependency downloads need network
approval. `plugin check` starts initialization code with user privileges; it is
not passive validation. `plugin add` therefore persists generated declarations
disabled by default, enabling is separate and explicit, and activation occurs
only after restart. Project trust never substitutes for generated-code review.

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
# The allowlist excludes cross-session and spill-artifact readers.
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

The Python and JavaScript/TypeScript SDKs start an external Snow binary directly
without a shell. They do not download binaries or accept API keys as process
arguments; credentials should come from Snow's auth store or a deliberately
controlled inherited environment.

- default to `deny`;
- pass the smallest built-in tool allowlist;
- disable unused plugins, MCP, skills, and subagents;
- set context deadlines;
- consume error/usage/tool events;
- close sessions/processes cleanly;
- move to `allow` only when the embedding host supplies external isolation and
  intends the resulting authority.
