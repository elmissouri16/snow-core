# Subagents

Snow implements one Codex-V2-style subagent tree around the existing agent
loop. The feature is disabled by default. This document covers enablement, the
model-facing tools, roles and permissions, limits, persistence, and the
surfaces that observe the tree.

> **Warning:** Subagents are not a sandbox. Every child runs with the user's
> OS privileges, shares the working directory and filesystem side effects, and
> incurs separate model usage.

## On this page

- [Enabling subagents](#enabling-subagents)
- [Model tools](#model-tools)
- [Paths, names, and identity](#paths-names-and-identity)
- [Context and lifecycle](#context-and-lifecycle)
- [Limits](#limits)
- [Authority and security](#authority-and-security)
- [Persistence](#persistence)
- [Surfaces](#surfaces)
- [Related documents](#related-documents)

## Enabling subagents

```sh
snow --subagents
snow --subagents --subagent-provider opencode-go --subagent-model model-id
snow --subagents --subagent-max-concurrency 10 --subagent-max-agents 32 \
  --subagent-max-depth 1
```

`--no-subagents` overrides an enabled config. Enabling subagents does not
enable recursion or file mutation. The built-in roles are `general`,
`explorer`, and `implementer`; the former `default` and `worker` role names
are rejected with migration guidance rather than retained as aliases. Bash
remains subject to the root permission mode.

`--subagent-provider PROVIDER --subagent-model MODEL` sets one automatic
provider/model default for every child, so an orchestrator can use ChatGPT
while children use OpenCode Go or another configured provider. Role-specific
and per-spawn selections take precedence; otherwise children inherit the
parent selection. Provider credentials remain isolated and resolve through
that provider's normal auth entry.

## Model tools

When enabled, the complete direct tool set is always exposed together:

| Tool | Purpose |
|---|---|
| `spawn_agent` | Create an independent child at a canonical path such as `/root/api_review`, optionally with an exact `provider`/`model` pair. |
| `list_subagent_models` | Return the exact available provider/model IDs, optionally filtered by provider. |
| `send_message` | Queue attributed mail without starting a turn. |
| `followup_task` | Queue a task and run or reuse an idle child. |
| `wait_agent` | Use `until=activity` for the next lifecycle/mailbox event or `until=all` to join all descendants; it never returns private child content. |
| `interrupt_agent` | Cancel only the target's current turn. |
| `close_agent` | Close a terminal child, releasing its open-agent slot while preserving its identity and history. |
| `resume_agent` | Reopen a closed child without starting a turn; this consumes an open-agent slot. |
| `list_agents` | List stable paths and states without prompts or full results, including closed children. |

`spawn_agent` uses distinct fields: `name` is the child's stable identity,
`task` is its assignment, `role` selects its capability profile, and optional
`provider`/`model` select its runtime. Root guidance tells the orchestrator to
call `list_subagent_models` rather than guess IDs. Discovery results include
each model's supported reasoning levels and the exact available provider IDs.
If an inherited parent effort is unsupported by the selected child model, Snow
automatically uses that model's supported default (falling back to `off`); an
explicitly requested per-spawn or role effort remains strict and returns a
supported-level diagnostic.

Snow no longer fetches every unrelated provider catalog during root startup.
The active provider and providers explicitly referenced by configured defaults
or roles are validated immediately. `list_subagent_models` resolves the
remaining catalogs on demand, and a direct spawn against an unloaded provider
performs the same contextual lookup before committing the child identity.
Concurrent requests share the cached result; an unavailable inactive provider
does not delay or fail ordinary root work.

At the provider boundary, manager-bound model tools also accept a sole
`{"_raw":"<JSON object>"}` compatibility envelope produced by some tool-call
parsers. `_raw` is not a public tool argument: Snow unwraps it internally,
then applies the same strict schema validation. Mixed fields, malformed JSON,
non-object values, trailing input, and unknown inner fields are rejected
rather than repaired.

A child completion is delivered exactly once as a sealed `agent` role mailbox
message containing type, sender, recipient, and payload. Mail is appended only
by the receiving agent at a safe loop boundary, so it cannot fork a serial
tool-result chain. `wait_agent` does not duplicate the payload: its
default/activity mode returns on one event, while `until=all` waits until
every descendant is terminal or the bounded timeout expires and returns
aggregate counts.

## Paths, names, and identity

Paths start at `/root`. Segments are lowercase letters, digits, and
underscores, begin with a letter, and are at most 64 bytes. For the
model-facing tool only, a hyphenated name is normalized to this canonical form
(`api-review` becomes `/root/api_review`). SDK and RPC requests remain strict
and require canonical underscore-only names. Targeting tools always use the
final canonical path.

Children cannot supply or forge their sender identity: every tool instance is
bound to its caller by the host.

## Context and lifecycle

`fork_turns` accepts `none`, `all` (the default), or a positive integer
string. Forks use the current post-compaction context and an independent
store. Snow removes thinking, incomplete plans, old collaboration mail, and
dangling tool calls/results, rebuilds parent IDs, excludes branch goals, and
rebuilds the current trusted system prompt. Child prompt assembly starts from
that project/base prompt and then adds only guidance supported by the finalized
child registry: shell guidance requires `bash`, mutation guidance requires
`write`/`edit`, and recursive delegation guidance requires an exposed
`spawn_agent`. Root-only MCP and managed-process instructions are not inherited
by children that cannot call those tools.

Children run concurrently up to the tree execution limit; each individual
child still processes turns and tools serially. Parent turn completion or
abort does not cancel committed children. In the interactive TUI the composer
can therefore become idle while children remain running; lifecycle rows and
`/agent` continue to update. Root prompt guidance tells the model to use
`wait_agent` with `until=all` instead of finishing when the requested answer
depends on outstanding children, and to synthesize repetitive results
compactly.

`interrupt_agent` leaves a child reusable and still counted as open. A terminal
child may be passed to `close_agent`, which changes its status to `closed`,
releases its open-agent slot, and keeps its path, thread ID, bounded result,
usage, topology, and durable transcript. Closed paths remain reserved and
cannot be reused by `spawn_agent`. `resume_agent` reopens a closed identity
without starting work; `followup_task` performs the same admission
automatically before queuing the requested turn. Either operation fails if the
open-agent limit is already full. `send_message` requires an open target so a
caller must resume a closed child first.

App close cancels and joins the full tree before closing the root event bus and shared extensions. Active
children block branch switching and root-session switching. Once all children
are terminal, switching sessions detaches their in-memory runtimes and
restores the target session's persisted topology; durable child databases
remain private to their original root session.

## Limits

| Setting | Default |
|---|---:|
| Enabled | false |
| Concurrently running children | 4 |
| Loaded durable children | 4 (derived from child concurrency) |
| Open agents per root session | 32 |
| Depth | 1 |
| Task timeout | 30 minutes |
| Result delivered to parent | 64 KiB |
| Wait range / default | 10 s to 1 h / 30 s |
| Durable child sessions | true |
| Recursive spawning | false |
| Mutation | false |

`subagents.max_concurrent_threads` is retained as the compatible config key,
but its value now means concurrently running child agents; the root no longer
consumes one slot. Configure it persistently with `/agent concurrency N`,
adjust the "Concurrent subagents" setting in `/settings`, edit
`~/.snow/config.json`, or override one launch with
`--subagent-max-concurrency N`. If necessary Snow raises
`max_agents_per_session` to the requested concurrency for the TUI/CLI
override; `--subagent-max-agents N` controls the open-identity cap explicitly.
Historical closed identities do not count toward this cap, although they remain
listed and keep their stable paths reserved. Separate non-configurable stored
identity bounds prevent unbounded history growth: durable sessions allow up to
4,096 identities, while non-durable sessions retain at most twice the configured
open-agent limit (capped at 4,096) because their closed transcripts cannot be
unloaded safely. These are storage/residency bounds, not execution capacity.

## Authority and security

Subagents are not a sandbox:

- every process and tool runs with the user's OS privileges;
- all agents see the same working directory and filesystem changes;
- parallel edits can conflict, and process/network side effects are shared;
- each provider request incurs independent token/cost usage;
- repository, tool, extension, and child output can contain prompt injection.

Child authority is the intersection of the parent registry, role tools, and
operator policy. The built-in roles are intentionally asymmetric:

| Role | Default child capabilities |
|---|---|
| `general` | `read`, `grep`, `glob`, bounded `artifact_read`/`artifact_grep`, skill/resource reads, and permission-gated `bash`. |
| `explorer` | `read`, `grep`, `glob`, bounded `artifact_read`/`artifact_grep`, and skill/resource reads; no `bash`. |
| `implementer` | The shell-capable surface; `write`/`edit` still require both mutation switches. |

All child roles exclude goals, user-input tools, network tools, plugins, and
Model Context Protocol (MCP). Mutation requires both
`subagents.allow_mutation=true` and a role with `allow_mutation=true`; a
parent `Tools` allowlist remains an upper bound.
While the root is in Plan mode, `spawn_agent` permits only an `explorer` role
whose policy remains read-only; other roles are rejected. Bash is not
sandboxed and can mutate the shared workspace or OS, so `ask` prompts through
the attributed TUI first-in, first-out (FIFO) broker, `allow` runs it, and
`deny` rejects it.
Headless ask mode remains deny-by-default. Read-only children may use a
deny-all service because read-risk calls are always allowed. Child `ask_user`
input stays excluded. Recursion similarly requires `recursive=true` and
remaining depth.

To grant an implementer file mutation, enable both switches explicitly (and
preserve the parent tool allowlist):

```json
{
  "subagents": {
    "allow_mutation": true,
    "roles": {
      "general": {
        "tools": ["read", "grep", "glob", "artifact_read", "artifact_grep", "activate_skill", "deactivate_skill", "read_skill_resource", "bash"]
      },
      "implementer": {
        "allow_mutation": true
      }
    }
  }
}
```

This does not create a sandbox; use `--permission ask` for interactive
approvals or `--permission allow` only in a trusted environment.

`spawn_agent` and `followup_task` use the separate `delegate` permission risk.
Deny mode hides or rejects delegation, ask mode prompts and can remember the
decision for the session, and allow mode permits it. Every child tool call is
independently permissioned.

## Persistence

With `durable=true`, every child transcript is a separate private SQLite
database:

```text
<root-session>.agents/<thread-id>.db
```

The root database stores only bounded topology/status/usage metadata in
`subagent_threads`, including an immutable role-policy fingerprint used to
fail safe if trusted role configuration changes. Pre-v6 rows without a
fingerprint are reloaded with a conservative read-only role, never with newly
granted mutation authority. A stored nonempty fingerprint that no longer
matches the trusted role policy is not restored under a different policy; the
child enters an errored state instead.

Child databases do not appear in the normal session picker. On cold open Snow
restores topology without loading child runtimes, converts stale
running/queued work to interrupted metadata, preserves `closed` status, and
never restarts work before `ReadySubagents`. Durable terminal children may be
unloaded when the residency cap is exceeded; follow-up, messaging, or
transcript inspection lazily reloads them. Closing a durable child immediately
unloads its runtime while retaining its database and topology row; resuming it
re-admits the identity and leaves runtime loading lazy until work, mail, or
inspection requires it. Unloadability follows each identity's stored child
database locator, so existing durable children remain unloadable if a later
configuration disables durability for newly spawned children.

Set `durable=false` only when intentionally choosing process-local child
history; `/agent` then warns that transcripts will not survive restart. Because
there is no durable state from which to restore those children, non-durable
terminal and closed runtimes retain their process-local transcripts in memory
until shutdown. The open-agent limit bounds non-closed identities, but closed
non-durable identities can still consume memory, so durable mode remains the
recommended default.

## Surfaces

- TUI: `/settings` persists enablement and child concurrency (applied on the
  next launch). `/agent` opens the live fleet inspector with aggregate
  capacity and a selectable agent list; `/agent <path-or-id>` opens the same
  inspector preselected. Wide terminals use list/detail panes, while narrow
  terminals stack them. Each two-line list entry keeps the agent path, status,
  model, and role visible; the detail pane combines role/provider/model/effort,
  Markdown-rendered model results and conversation messages, a bounded
  tool-aware transcript, and bounded live thinking/response/tool activity. Use
  ↑/↓ or `j`/`k` to
  select, wheel/trackpad or PageUp/PageDown and Home/End to scroll detail,
  `r` to refresh authoritative state, and Esc to close. Lists and transcripts
  load asynchronously and attributed events update the in-memory view;
  blocking permission/user-input prompts still take precedence. Restored
  lifecycle snapshots hydrate the fleet inspector without appending fresh
  lifecycle rows to the resumed root transcript.
  `/agent concurrency N` persists a positive child limit up to the safety cap
  of 256. The root transcript receives compact lifecycle rows, never child
  token streams.
- SDK: `ReadySubagents`, `SubagentModels`, `SpawnSubagent`,
  `SendSubagentMessage`, `FollowupSubagent`, `WaitSubagents`,
  `WaitSubagentsUntilAll`, `InterruptSubagent`, `CloseSubagent`,
  `ResumeSubagent`, `Subagents`, `Subagent`, and `SubagentUsage`.
- RPC: `subagent_ready`, `subagent_spawn`, `subagent_send_message`,
  `subagent_followup`, `subagent_wait`, `subagent_interrupt`,
  `subagent_close`, `subagent_resume`, `subagent_list`, `subagent_get`, and
  `subagent_models`.
- JSON/events/plugins: ordinary child events carry `agent`; lifecycle events
  also carry `subagent`, and mailbox events carry `agent_message`. A restored
  lifecycle publication carries `snapshot: true`, distinguishing observer
  initialization from a transition that just occurred. Root events retain
  omitted correlation fields for compatibility.

SDK and restored-session hosts should subscribe first, then call
`ReadySubagents`. CLI, print/JSON, RPC, and TUI do this automatically.

## Related documents

- [Sessions](sessions.md)
- [Plan Mode](plan-mode.md)
- [Thread Goals](goals.md)
- [Configuration](configuration.md)
- [SDK](sdk.md)
