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

### Local web manager preview

`snow --mode web` starts an authenticated, direct-numeric-loopback HTTP manager
with optional explicitly configured local TLS. Browsing
host directories, registering projects and reading the inactive catalog do not
activate an agent. The host-directory picker lists only directories on the Snow
host, not the accessing device; host OS permissions apply, without a separate
filesystem sandbox. Directory enumeration and pagination are bounded.

Explicit live activation starts one existing Snow RPC worker for the project,
loads normal host configuration and applicable trusted instructions, and may
perform provider discovery. At most two live projects are admitted. The fixed
`managed-explicit-goals` worker profile starts new sessions with `ask` permissions
and disables plugins, MCP, subagents and debug capture. Skills are disabled by
default. **Enable installed skills** is a separate per-project startup preference,
saved in the manager registry and shared by its paired browsers. It starts unchecked
until explicitly enabled; later activation forms preselect the saved choice.
Unchecking it on startup or changing Settings → Workspaces saves the next-start
preference, without changing a live worker. Archiving/removing the registration
clears it. The submitted startup checkbox still controls that particular start;
omitting it never silently inherits enablement. Enabling skills adds only skill
discovery and the activate/deactivate/resource tools to that worker. It does not
change tool permission policy, write CLI extension trust, enable plugins/MCP/subagents,
or derive authority from remembered manager trust.
Personal/configured skills and already-CLI-trusted project skills follow the
worker's normal catalog policy; metadata can enter provider context and the
model can activate applicable skills. Changing this profile requires closing
and explicitly starting the worker again. Saved goals are
deferred, not automatically resumed. Read/glob/grep/write/edit/bash/ask_user and
process_start/process_status/process_logs/process_stop/process_list are enabled.
Goal schemas and dispatch are suppressed on ordinary prompts; only explicitly
admitted native goal runs can get/update their owning goal, never create/replace
one through model tools.
Browser input cannot select arbitrary RPC commands or execution flags.

Composer attachments are explicit user-provided context, not host paths or
executable uploads. Browser files stay in bounded tab memory until Send; the
manager accepts only typed text/image content and never writes an upload into
the project. PNG/JPEG/GIF/WebP headers, MIME, dimensions, aggregate bytes and
UTF-8 text are checked before prompt admission. The form is bounded at 4 MiB,
raw images at 2 MiB total, eight images, and combined prompt/text content at
128 KiB. Images require an authoritative vision-capable model. Content reaches
the selected provider and is persisted in the append-only session; public web
snapshots omit raw image bytes. Draft thumbnails use only local Blob URLs made
from bounded raster bytes after header/dimension checks; removal, acceptance and
disposal revoke them. CSP permits `blob:` only for images, not scripts or other
resources. Sent thumbnails use authenticated same-origin image reads, separate
from snapshots/SSE, with exact session/message/content-index identity and the
same raster bounds. Live reads stay with the owning runtime; saved reads use the
runtime-free catalog and reject live ownership. They do not activate a runtime,
write files or call a provider. Images cannot enable text-only Edit/reuse actions.
PDFs and arbitrary binary files are unsupported.
`@` suggestions reuse the authenticated pinned-root file-inspection service,
including sensitive-name, symlink, inode and size restrictions. Only explicit
file selection reads a file; truncated previews are never silently attached as
complete files. This does not detect secrets inside ordinary source files.
Attachment labels remain separate content blocks, not explicit skill-mention
input. Exact `$name` activation remains a worker-owned operation on the submitted
message, not a side effect of browsing or selecting a suggestion. Queue next and
Edit/reuse do not silently discard attachments: those text-only actions are
disabled while attachment context is present.

The browser can explicitly remember project activation consent in the private
manager database. Consent binds the registration ID, canonical path, device and
inode, applies to this manager’s paired browsers, and survives restarts. It only
removes repeated warning/checkbox presentation: every activation still needs an
authenticated, CSRF-protected, explicit Start/Resume POST and fresh available
folder identity. An old compact form cannot reuse revoked consent. Registration
and migration do not grant trust; archive/removal clears it atomically, and
restore/re-registration requires fresh consent. Missing/replaced folders cannot
use it. **Settings → Workspaces → Forget trust** revokes consent without stopping
current workers or changing permissions. This is independent of CLI extension
trust and does not authorize tool Allow policies, provider work, or auto-start.

The typed bridge permits text prompts, Stop, allow-once/deny permission decisions,
and validated question replies. Truncated permission summaries cannot be allowed.
Mutations are bound to a fresh activation identity; pending replies must also
match the current interaction. No remembered permission grants, automatic retries,
background reactivation or automatic queue replay are offered. Ordinary runtime permission
checks remain authoritative; these controls do **not** contain OS-privileged tools.
Browser disconnects leave admitted work running; manager shutdown closes workers.

Typed workflow controls additionally permit explicit host-model discovery,
conversation-scoped model selection, rename, create/open and authoritative
Default/Plan Mode changes through the same activated worker. Model choices are
validated against discovered pairs; session-only selection does not persist host
configuration or the operator-owned project selection. Discovery is bounded and
never runs from passive snapshots or stream subscriptions. Telemetry projects
counts and explicitly labeled recorded-cost estimates, not context category text,
account inventory or provider-private metadata. Mixed/invalid currency estimates
stay unknown; priced subtotals may omit unpriced usage and are not a bill or cap.

Saved-session deletion is a separate explicit, authenticated, CSRF-protected
mutation, never a sidebar read. Its bounded form binds an immutable session ID to
the registered workspace and exact live instance (or the absence of one). The
current active session cannot be deleted; live deletion uses existing idle/control
admission and fresh membership checks. Cold deletion uses an allowlisted,
short-lived runtime-free control worker with `inactive_session_delete_v1`, not an
App or agent activation. The manager preempts and joins its own sidebar reads and
retains ownership admission through worker teardown. The launch folder identity,
indexed session identity, exclusive database leases (including other processes),
child ownership, and existing private-data cleanup checks remain authoritative.
No project files are deleted and no replacement conversation is created.
Deletion/cleanup or transport errors may leave changed storage; the UI reports
uncertainty and never automatically repeats the irreversible command.

The composer also permits explicit **session-scoped** permission-policy changes
while the bound runtime is verified and idle. Ask, Deny and Allow are Snow's
approval policies, **not sandbox presets**: read-risk operations remain allowed
in Ask/Deny, and Allow skips permission prompts. Choosing Allow requires an
unchecked risk-acknowledgment checkbox; the typed server route also requires
explicit confirmation. Policy changes use the existing permission service and
persist in the session, not host/project configuration. New sessions retain the
Ask launch baseline; explicitly resuming a saved session can restore its saved
policy. The chip reflects verified worker state, not an optimistic browser
selection. Uncertain mutations are not retried. No arbitrary RPC forwarding,
remembered-grant editor, OS containment, or Harness sandbox preset is added.

Switching away from active work requires an explicit Stop-and-switch confirmation
and definitive prompt completion, not merely an abort acknowledgment. Session
changes rotate the control identity and fence retired events. Nonqueued controls
revalidate their instance after admission, including Close; unverified transitions
fail closed. Each newly bound session is checked against the host project's
identity and a recognized permission policy, and restored goals remain deferred. Browser reconnection
or passive replacement cannot automatically send prompts, approvals or mutations.
Drafts and unknown-outcome guards live only in tab memory, not browser storage.
Public plans are displayable text; leaving Plan Mode does not execute them.

**Activity** is read-only, bounded to 100 active registrations and a 256 KiB
summary. It samples registry/recovery metadata and in-memory runtime counts;
it does not read the session catalog, activate workers, expose prompt/objective/
transcript text, or carry mutation handles. Running/attention/queue/review counts
can overlap and do not attest to browser connectivity. Navigation grants no
control authority; a saved-session link cannot silently retarget another live
session.

Workspace labels, pins and archives affect only manager metadata. Session flags
are scoped to an exact registration and durable session ID, capped at 1,000 per
project / 10,000 total, with fresh bounded catalog membership checks. A project's
worker must be closed before organizing its saved sessions or archiving its
registration. Search/filtering is page-local, never a full transcript index.
Project Restore preserves the original registration ID and rechecks canonical
path, device/inode, active-duplicate exclusion and the 100-project cap. Archive,
registration removal and restore never delete/recreate project or session files,
activate a worker, or grant runtime authority.

**Versions** uses live-worker bounded branch/history reads; previews are not
execution capabilities. Explicit Restore preparation binds source and target
branch/tip, session and worker identity to a two-minute single-use token. Commit
is an idle, compare-and-swap transition; pending attention, goal conflicts and
retained queues block it. Current model/permission authority is preserved, while
the target branch's saved collaboration mode comes from the authoritative core
acknowledgment, not a browser default. Old controls are retired. Restore selects
append-only conversation history without filesystem undo, provider replay or
resuming goals. Uncertain commit outcomes fail closed without retries.

**Start goal / Resume goal** binds consent to the exact worker/session/branch/tip/
expected goal and the reviewed web snapshot revision. Core source checks and
web revision compare-and-swap both apply. Plan Mode and retained queue/review
work reject admission; Start never replaces an unfinished goal. One correlated
native goal handle owns all serial turns and gaps until definitive run completion.
Whole-run Stop is distinct from semantic goal completion; paused, blocked or
limited goals are not falsely labeled complete. Queue next is disabled for goal
runs. Optional token budgets stop substantive work, not in-flight billing.
Reconnects, reads and session transitions do not authorize automatic continuation.

**Processes** exposes bounded inventory (128 records), 32 KiB cursor-based plain-
text logs and explicit Stop, not a command launcher, PTY, PID interface or process
sandbox. Every operation binds the exact current session; log/Stop additionally
bind an opaque managed handle. Operator Stop requires Default mode, the agent's
actual configured hard `InvocationPolicy`, and authorization from the current
noninteractive permission state, including applicable remembered rules. Ask with
no remembered allow fails closed; no second broker is opened while holding the
control lane. Neither Allow nor tool exposure bypasses hard policy. Session
switching and shutdown stop managed processes, but arbitrary detached descendants
and previously performed effects are not guaranteed contained or undone.

Historical **Edit & resend** is an explicit, CSRF-protected mutation, not a browser
copy-and-append or arbitrary parent/branch operation. Read-only preparation binds
an exact active-path plain-text user and a short-lived single-use token. Commit
revalidates that source and admits the replacement under one control transaction.
It keeps the same session and permission authority, while retaining the original
append-only history internally; removing replies from view never reverses tool
side effects or erases stored history. Public projection/instance replacement
retires old events. Ambiguous persistence, rollback or acknowledgment failures
fail closed and never authorize automatic replay. History traversal, source text,
preparation tokens and buffered public events are bounded.

**Queue next** is explicit follow-up admission, not automatic retry. Session,
instance, queue-only token, exact root identity and queue revision bind controls;
per-item changes contend with actual delivery. Pending and review text share
fixed item/byte budgets and are never silently clipped into an editable request.
Only authoritative durable delivery creates a chat input. Unknown append or
transport outcomes never prove non-execution, and retained items cannot be
silently replayed or moved to another chat. New work and context transitions
require resolving retained review; close/shutdown remains an explicit live-only
discard. Tools in each follow-up use the existing permissions—one tool approval
is not authority for another tool. Queue state does not create a durable
scheduler or an independent agent loop.

**Regenerate** uses the same transaction after resolving an exact final assistant
reply and its original user-origin turn. Its token is action-bound; the commit
accepts no replacement text and uses the server-held original input. The browser
requires explicit confirmation that the whole reply, including tool work, will
restart and that following conversation will be replaced. Tools may execute
again under the current permission policy, not restored historical approvals.
No user prompt is duplicated on the active path, and uncertain outcomes never
trigger fallback prompts or automatic retries.

The optional `RuntimeSubscriber` capability exposes a read-only
`GET /projects/{p}/runtime/events?instance_id=...` subscription, authenticated
before admission and bound to that exact live instance. It cannot activate,
switch or control a worker. It sends only the bounded public `RuntimeSnapshot`
projection (including allowlisted Markdown display HTML), not raw RPC events,
thinking text, raw tool arguments or provider-private continuity. RPC/private
wire contracts and core execution remain unchanged by this transport.

Each subscription retains one coalesced wakeup, not a token/event replay log.
Admission is bounded to 16 streams across the HTTP handler, four per browser
credential and 32 subscribers per runtime. Changes coalesce at 75 ms; heartbeats
are sent every 10 seconds. Encoded snapshot JSON is capped at 4 MiB, each frame
write/flush has a five-second deadline, and connections expire after ten minutes.
A slow browser cannot block the RPC event drain; reconnecting reads a fresh
snapshot rather than replaying any POST. Open-stream browser authority is
**periodically** rechecked every five seconds through a bounded durable-store
read (up to three seconds). Revocation/expiry is therefore not an immediate
push cutoff: until that check completes, an already-open stream may still
receive public updates. Failed authority checks end it with `auth_required`.

The browser uses native `fetch` SSE, pauses hidden tabs, and retires old readers
on navigation/replacement. Only an unsupported legacy subscription (HTTP 501,
or a backend advertised without that capability) uses two-second polling; other
transport failures use bounded read-only reconnect backoff. Closed/replaced or
unauthorized subscriptions terminate rather than silently adopting authority.
Mutation controls remain disabled after a POST until a fresh instance-bound
snapshot arrives; uncertain outcomes additionally require explicit review.
Attention takeovers preserve prompt/question drafts in bounded tab memory and
use the existing permission/question brokers. Stop cancels the whole turn;
UI paging, recommended labels and collapse never grant authority or send answers.

Do not publish this preview through a reverse proxy, LAN listener or mesh-VPN
tunnel. Remote TLS and trusted proxy handling are not implemented. Host-side
folder selection is not evidence that remote deployment is supported.

Local TLS requires both `--web-tls-cert` and `--web-tls-key`, clean absolute
paths to bounded regular PEM files (1 MiB each), with no symlink components.
Minimum TLS is 1.2; invalid inputs fail closed. Snow does not generate certificates,
install trust or add DNS/LAN/public-origin/proxy support. TLS protects this direct
local browser transport, not Snow's tools from the host user.

Runtime-free host controls dispatch before `app.New` using a separate control
startup. Allowlisted global/project defaults and coarse local provider status
use local config/auth helpers only: no provider initialization, discovery,
network credential validation/refresh, OAuth or extension loading. Defaults
apply to future workers and use locked revision-checked atomic updates, not a
raw config editor or live-worker policy override. At most two short control
workers run; settings/key writes have a separate nonqueued serialization gate.

API-key entry additionally requires an **actual TLS request**, exact numeric-
loopback HTTPS origin/Host, browser authentication, explicit Origin and CSRF on
POST. Forwarded headers cannot simulate this authority. A provider-specific
metadata inspection grants one write for five minutes, bound to this browser;
explicit save and required replacement confirmations are checked. The key
limit is 4 KiB (16 KiB form-body limit). The write consumes inspection authority
even on uncertainty, compares the auth-file metadata revision under the existing
legacy auth lock, and atomically writes mode 0600. Revisions derive from file
metadata, not credential bytes. No credential value is exported in status/errors;
no OAuth, key deletion, automatic refresh or provider network call is offered.
Existing workers are not reloaded. Keep API keys out of logs, URLs, screenshots
and CLI arguments; use interactive host login when HTTPS entry is unavailable.

Pairing uses a random reusable code, valid for up to 30 days or until rotated,
with bounded persisted attempts and at most eight browser sessions. Browser
credentials also have 30-day absolute/idle limits and survive restarts. A private,
bounded, atomically replaced `access.json` under manager storage keeps token
**hashes**, CSRF values, signing key, expiry metadata and the deliberately
reprintable pairing code. Unsafe/corrupt/replaced storage or failed writes deny
access rather than silently reset it. Grants/revocations persist before success.

Cookies are HttpOnly and SameSite=Strict, Secure on configured HTTPS and
intentionally non-Secure only on loopback HTTP. Exact Host and same Origin are required for mutations, with CSRF
form tokens. Forwarded headers confer no authority. Same-origin referrer policy
preserves ordinary form Origin while suppressing cross-origin referrers.
Authenticated content is no-store; HTMX evaluation/script processing and history
caching are disabled under a self-only CSP. Display data remains untrusted text.

Browser inventory projects independent random public IDs, coarse labels and
creation/approximate-last-used/expiry metadata, not cookies, hashes, raw
User-Agent strings or device/IP fingerprints. Targeted revocation authorizes the
actor and target with the durable commit; the public ID is not a credential.
Revocation does not stop an agent or project-operation job. Open SSE streams
observe it at the periodic five-second authority check, not synchronously.

Sign out durably revokes one browser. Revoke all revokes every browser and rotates
the code; restart reprints its replacement. Rotation alone leaves paired browsers
valid. Terminal pairing output and `access.json` are intentional operator
credentials: keep both private. Restart is **not** a revocation mechanism.
This does not protect against malicious processes already running as the same
host user. The RPC client/process packages provide no process or tool sandbox.

Manager storage requires a private owned directory, 0600 regular single-link
files and a lifetime exclusive lock. Project registration persists canonical
roots plus directory identity; missing/replaced roots fail availability checks.
Removal archives only manager metadata and is rejected while a worker is live.
Workers execute the resolved Snow binary with fixed arguments, inherited
environment and the selected project CWD—not a shell or project PATH search.
Direct workers are closed and reaped; arbitrary detached tool descendants are
not contained or guaranteed cleaned up.

Catalog mode rejects prompts and mutations. At most two short-lived workers read
safely leased, inactive SQLite databases through read-only immutable connections,
without creating leases, schemas or sidecars. Active, unleased, recovery-dependent,
invalid and oversized databases are omitted. Saved conversation projections
exclude tools, thinking, images and provider-private continuity. A separate live
timeline consumes only the explicit public `tool_result` text field, never legacy
`tool_output` previews, arguments or private display/plugin metadata. Private-detail
results suppress that field. Live updates consume normalized RPC events with
definitive prompt completion; there is no second agent loop. Assistant Markdown
passes a strict HTML allowlist: no raw active HTML, images, styles, forms or
embedded objects; explicit HTTP(S) links carry no-referrer/no-follow attributes.
Files, diffs and public tool results are rendered as text, not HTML. Requests, scans, database size, traversal, decoded
messages and encoded pages are bounded. Filesystem operations remain subject to
host OS behavior, including potentially blocking filesystem calls.

Current-session reasoning, ordinary branch/detached-session fork and rename,
manual compaction and native steering are typed capability-gated controls, not
arbitrary RPC forwarding. Reasoning checks full current session/branch/tip/model/
mode/permission authority and only advertised local capabilities; overrides are
in memory, not config or new session-history metadata. Default and Plan thinking
remain independent. History operations recheck source/target saved-tip and name
CAS in the store; branch fork activates its branch, detached-session fork never
automatically opens it, rename is metadata-only, and none replays tools.
Manual compaction owns a captured native run, uses provider work and preserves
Plan mode and exact append-only history. Progress is not terminal completion;
Stop owns the whole run. Steering targets an existing ordinary run and shares
Queue next's eight-item/64 KiB-per-input/256 KiB-total limits. Acceptance is not
delivery; timeout or a late POST failure does not prove input was undelivered.
No control silently retries an uncertain mutation. An uncertain steering receipt
retains Stop authority only for its captured root, never a replacement run. Idle
**Keep draft and dismiss** preserves text and uncertainty; separate shared
**Reviewed** acknowledgement releases Send/Close without asserting delivery.

All four manager worker families—runtime, catalog, CONTROL and project
operations—freeze absolute operator config/auth and independent session roots
before changing CWD. This reads environment/CWD only, not configuration or
credentials; resolution failure disables startup. Manager storage is absolute,
and host-control/project-operation backends receive the registry’s canonical
directory rather than a caller-relative spelling.

Explicit host create/anonymous-HTTPS-clone jobs are separate runtime-free
operator filesystem/network actions. **The host directory browser uses the OS
user's authority, not a startup-approved-root sandbox.** A browser-bound
five-minute selection pins the canonical parent and device/inode, then one
validated child name is created without adopting an existing destination. Durable
admission precedes worker creation; the manager records the prepared child's
identity before acknowledging clone network execution. One operation runs at a
time without queuing; at most 128 records persist, with 32-row bounded pages.

Empty-directory creation does not require Git. Clone admission validates the
fixed absolute executable before allocating a handle, with no PATH lookup or
fallback. A fixed Git binary and trusted descriptor helper disable inherited Git config,
credential prompts/helpers, hooks, recursive submodules and redirects. Only
reviewed anonymous HTTPS locator forms are allowed; SSH and authenticated clones
are unavailable. A process group and worker-liveness pipe supervise cancellation;
TERM gets a two-second grace before forced termination. The ten-minute timeout
and 64 KiB output budget are **not disk or network-byte quotas** or an OS/network
sandbox. Git output is not returned as browser logs. Partial directories remain;
neither cancellation, reconciliation nor metadata Dismiss deletes host files.
Successful completion leaves the operation awaiting registration. Only a separate
explicitly reviewed Register request may add the project, with operation-revision
CAS and directory-identity checks. Success, reads, reconciliation and restart
never register it automatically. Registration never activates an agent or opens
a conversation. Failed registration and interrupted/unknown outcomes require
explicit review; passive recovery/reconciliation never reexecutes clone.

The read-only project inspector requires browser authentication, same-origin
POST and CSRF, with relative paths only in request bodies. File previews pin the
registered root, reject links and special files, and check identity around bounded
reads. `.git`, all `.env*` names and common credential/key names are excluded at
every depth. These exclusions are not general secret detection.

Git inspection uses a private temporary control directory and copied index/HEAD,
a fixed system Git executable and a newly constructed environment. It does not
load project/global Git configuration or enable filters, hooks, text conversion,
external diff, lazy fetching or replacement objects; the original index is not
written. Unsupported/unsafe repository layouts fail closed with an explicit
unavailable state. Commands and output are bounded. Git still reads the live
worktree and original object store with host OS privileges: pre/post identity and
no-link checks do not contain another host process racing filesystem changes.
This is a read-only view, not a Git process sandbox. At most four project-view
HTTP requests and two Git inspections are admitted without queuing.

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

Before authorizing the built-in `bash` or `process_start` tool, Snow parses the POSIX shell source
and publishes bounded, statically inferred effects, capabilities, paths, and
unknowns. High-confidence visible credential reads, SSH authorization changes,
raw-device or container-socket access, persistence writes, and privilege
escalation are denied before the ordinary permission mode. Parser errors,
unsupported structural shell nodes, and exhausted analysis bounds fail closed.
`allow` skips the prompt but does not override those hard denials.

Shell approvals are remembered only for understood invocations, using the exact
source, working directory, launch-environment digest, analyzer/specification
version, protected-path policy, and inferred effects/resources. Environment
values are never included in permission summaries. Existing approvals from the
older analyzer are invalidated by the new scope version; broad legacy allows
cannot authorize either analyzed shell launcher. Current unknown or
non-rememberable analysis never accepts a cached allow.

Command definitions and option roles are compiled once from the embedded
`internal/shellanalysis/commands.json` specification. Unsupported options,
unresolved expansions, uncertain state, and runtime-dependent child effects
remain unknown and permit only one-time approval in `ask` mode. Git, network
clients, recursive traversal, and nested shells have runtime effects the
analyzer cannot prove, so recognizing their names does not enable reusable
approval. Structural omissions and exhausted analysis budgets remain hard
errors. This distinction preserves explicit approval of ordinary opaque
programs without claiming their effects are fully understood.

Protected defaults live separately in
`internal/shellanalysis/protected_paths.json`. Operators can add absolute paths
or directory trees with global `shell_protected_paths`; these additions deny
statically visible reads, writes, and deletes and cannot weaken defaults.
Trusted-project configuration cannot override this global policy. Path and
symlink observations are cached only within one bounded preflight and refreshed
for every later invocation. They do not prevent filesystem races during an
approved process's execution.

Remembered session approvals and static Bash analysis are conveniences, not
containment. A permission decision authorizes the classified operation; it does
not make a command, extension, endpoint, or model response trustworthy.

## Review project trust

Snow asks before loading project-local `.snow/config.json`, theme, keybinding,
MCP, Agent Skills, system-prompt, or trusted-project instruction files.
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

Built-in `edit` and `write` operations serialize within one Snow process,
including across subagents. Before replacing a file, `edit` rechecks the
original file's identity, metadata, and exact contents through its pinned root.
A detected change returns a conflict without replacing the newer file; read
the file again before retrying. `write` still intentionally replaces the entire
file with the supplied content.

External editors, shell commands, plugins, and other Snow processes do not
participate in this coordination. An external write after the final validation
but before rename can still race an edit: portable atomic replacement does not
provide a filesystem compare-and-swap operation. Coordinate external writers
when editing the same file.

Model-facing Bash and managed processes do not share those file-tool
confinement guarantees. Shell preflight can block only effects visible in shell
syntax and recognized command arguments. Once approved, Bash and managed
processes can read or change anything the current user can access, including
operations hidden inside an interpreter or executable. Managed-process
timeouts, output limits, and shutdown cleanup reduce runaway work but cannot
undo side effects. A crash, `SIGKILL`, or deliberately detached process may
leave work running.

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

JavaScript plugins use Goja inside Snow. Their exposed host operations repeat
Snow's built-in permission, Plan Mode, shell-preflight, path, and network checks.
Package loading is explicit; project packages require project trust. Contexts
expire after each call, and initialization/observers/shutdown have no host I/O.
There is no per-plugin heap quota or OS sandbox. Interrupts cannot preempt native
Go functions; approved Bash retains the user's OS privileges. Only install
trusted packages. See [Plugins](plugins.md) for limits and failure behavior.

Go plugins run inside the embedding application; stdio MCP servers run as child
processes. Both have the user's OS privileges. Agent Skills add untrusted
instructions to model context. Subagents start additional agent loops that
share filesystem and process side effects.

Before enabling an extension, review its:

- executable and arguments;
- working directory and environment;
- network destinations and headers;
- registered tools and declared risks; and
- project-trust source.

Risk declarations affect Snow's permission gate but do not constrain what an
extension code can actually do. Go plugin lifecycle methods run outside tool
permission checks. MCP annotations are also untrusted hints.

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

The interactive updater uses the same release assets and integrity model.
Explicit **Check now** contacts GitHub; automatic startup traffic occurs only
after the global **Check for updates on startup** opt-in. Startup checking
fetches bounded release metadata only. A successful check can offer **Install
update** or **Skip for now**, but the release archive is not downloaded and the
executable is not mutated until that explicit confirmation. An approved install
stays visible in a foreground progress card. Print, JSON, RPC, SDK, version, and
management startup never make an implicit update request.

Self-update runs with the user's OS privileges and can replace only the current
supported macOS/Linux official-release executable. It requires a writable
regular non-symlink destination, pins and stages within the executable's parent
directory, rechecks destination identity before atomic replacement, bounds all
network/archive/process work, and never invokes `sudo`. Development builds,
unsupported platforms, symlink destinations, changed targets, malformed
archives, checksum failures, and staged version mismatches fail before replacing
the existing binary. If the atomic replacement succeeds but directory syncing
fails, Snow reports that the executable was replaced but durability could not be
confirmed. The restart prompt appears only after a fully successful replacement;
choosing Later continues safely with the old in-memory code.

The archive and `SHA256SUMS` still come from the same GitHub release. This
protects against corruption and mismatched assets but is not an independent
signature or separate trust root.

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
- [Plugins](plugins.md) — Go plugin registration and privileges
- [MCP](mcp.md) — server setup and credential handling
- [Subagents](subagents.md) — child roles, limits, and shared authority

## JavaScript extension controls

API 2 capabilities narrow individual callbacks; they do not create a sandbox.
Tool calls retain schema validation, Plan checks, preflight, invocation policy,
and permission gates after hook argument transformations. UI contributions are
bounded declarative data and cannot authorize permission requests. Provider
continuity blocks are omitted from plugin message snapshots. Hooks cannot call
host APIs; post-tool failures preserve completed outcomes. Selected child tools
use independent runtimes and stored package/config fingerprints. Scoped plugin
state is a separate SQLite database, with bounded values and transactional quotas.
See [the extension lifecycle](plugin-extensions.md#storage-settings-and-lifecycle).


Branch-aware `ctx.workflow` records are separate from preference KV storage:
they are append-only session metadata, excluded from provider context unless
plugin code explicitly contributes a value as guidance or output. Atomic writes
require an idle root command and commit immediately; later callback failure
does not undo a completed update. Pure hooks receive only requested, bounded,
plugin-owned workflow values. New lifecycle gates cannot perform host I/O,
redirect transitions, or replace compaction boundaries/summaries.

Plugin tool restrictions intersect rather than overwrite one another. They
apply to schemas, deferred discovery, dispatch, nested calls, and descendants,
without loosening Plan Mode, role policy, or permissions. Clearing one plugin's
restriction never clears another's. They are not a universal restriction on
non-tool host controls or arbitrary OS behavior. A loaded runtime failure
retains its committed restriction; corrupt/unavailable projection fails closed.

Single-plugin reload validates a detached candidate with host I/O disabled and
preserves pinned roots and package fingerprints. It refuses active work rather
than cancelling it. Committed generation changes invalidate stale callbacks;
post-commit cleanup/readiness failures are diagnostics, not rollback. New
readiness can perform declared host operations under ordinary permission rules.
See [Plugin workflows and reload](plugin-workflows.md) for complete semantics.
