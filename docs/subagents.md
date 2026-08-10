# Subagents

Snow implements one Codex-V2-style subagent tree around the existing agent loop. The feature is **disabled by default**.

```sh
snow --subagents
snow --subagents --subagent-max-concurrency 10 --subagent-max-agents 32 --subagent-max-depth 1
```

`--no-subagents` overrides an enabled config. Enabling subagents does not enable recursion or file mutation. The shell-capable built-in role is named `default`; `general` is accepted as a compatibility alias. Bash remains subject to the root permission mode.

## Model tools

When enabled, the complete direct tool set is always exposed together:

- `spawn_agent`: create an independent child at a canonical path such as `/root/api_review`;
- `send_message`: queue attributed mail without starting a turn;
- `followup_task`: queue a task and run/reuse an idle child;
- `wait_agent`: use `until=activity` for the next lifecycle/mailbox event or `until=all` to join all descendants; it never returns private child content;
- `interrupt_agent`: cancel only the target's current turn;
- `list_agents`: list stable paths and states without prompts or full results.

Paths start at `/root`. Segments are lowercase letters, digits, and underscores, begin with a letter, and are at most 64 bytes. For the model-facing `spawn_agent` tool only, a hyphenated task label is normalized to this canonical form (`api-review` becomes `/root/api_review`). SDK and RPC requests remain strict and must supply canonical underscore-only task names. Targeting tools always use the final canonical path. Children cannot supply or forge their sender identity: every tool instance is bound to its caller by the host.

At the provider boundary, manager-bound model tools also accept a sole `{"_raw":"<JSON object>"}` compatibility envelope produced by some tool-call parsers. `_raw` is not a public tool argument: Snow unwraps it internally, then applies the same strict schema validation. Mixed fields, malformed JSON, non-object values, trailing input, and unknown inner fields are rejected rather than repaired.

A child completion is delivered exactly once as a sealed `agent` role mailbox message containing type, sender, recipient, and payload. Mail is appended only by the receiving agent at a safe loop boundary, so it cannot fork a serial tool-result chain. `wait_agent` does not duplicate the payload: its default/activity mode returns on one event, while `until=all` waits until every descendant is terminal or the bounded timeout expires and returns aggregate counts.

## Context and lifecycle

`fork_turns` accepts `none`, `all` (the default), or a positive integer string. Forks use the current post-compaction context and an independent store. Snow removes thinking, incomplete plans, old collaboration mail, and dangling tool calls/results, rebuilds parent IDs, excludes branch goals, and rebuilds the current trusted system prompt.

Children run concurrently up to the tree execution limit; each individual child still processes turns and tools serially. Defaults are:

| Setting | Default |
|---|---:|
| enabled | false |
| concurrently running children | 4 |
| loaded durable children | 4 (derived from child concurrency) |
| agents per root session | 32 |
| depth | 1 |
| task timeout | 30 minutes |
| result delivered to parent | 64 KiB |
| wait range/default | 10 s–1 h / 30 s |
| durable child sessions | true |
| recursive spawning | false |
| mutation | false |

Parent turn completion or abort does not cancel committed children. In the interactive TUI the composer can therefore become idle while children remain running; lifecycle rows and `/agent` continue to update. Root prompt guidance tells the model to use `wait_agent` with `until=all` instead of finishing when the requested answer depends on outstanding children, and to synthesize repetitive results compactly. `interrupt_agent` leaves a child reusable. App close cancels and joins the full tree before closing the root event bus and shared extensions. Active children block branch switching and root-session switching. Once all children are terminal, switching sessions detaches their in-memory runtimes and restores the target session's persisted topology; durable child databases remain private to their original root session.

`subagents.max_concurrent_threads` is retained as the compatible config key, but its value now means concurrently running **child agents**; the root no longer consumes one slot. Configure it persistently with `/agent concurrency N`, adjust **Concurrent subagents** in `/settings`, edit `~/.snow/config.json`, or override one launch with `--subagent-max-concurrency N`. If necessary Snow raises `max_agents_per_session` to the requested concurrency for the TUI/CLI override; `--subagent-max-agents N` controls that identity cap explicitly.

## Authority and security

Subagents are not a sandbox:

- every process and tool runs with the user's OS privileges;
- all agents see the same working directory and filesystem changes;
- parallel edits can conflict and process/network side effects are shared;
- each provider request incurs independent token/cost usage;
- repository, tool, extension, and child output can contain prompt injection.

Child authority is the intersection of the parent registry, role tools, and operator policy. The built-in roles are intentionally asymmetric:

| Role | Default child capabilities |
|---|---|
| `default`/general | `read`, `grep`, `glob`, skill/resource reads, and permission-gated `bash` |
| `explorer` | `read`, `grep`, `glob`, and skill/resource reads; no `bash` |
| `worker` | The shell-capable surface; `write`/`edit` still require both mutation switches |

All child roles exclude goals, user-input tools, network tools, plugins, and MCP. Mutation requires both `subagents.allow_mutation=true` and a role with `allow_mutation=true`; a parent `Tools` allowlist remains an upper bound. Bash is not sandboxed and can mutate the shared workspace or OS, so `ask` prompts through the attributed TUI FIFO broker, `allow` runs it, and `deny` rejects it. Headless ask mode remains deny-by-default. Read-only children may use a deny-all service because read-risk calls are always allowed. Child `ask_user` input stays excluded. Recursion similarly requires `recursive=true` and remaining depth.

To grant a worker file mutation, enable both switches explicitly (and preserve the parent tool allowlist):

```json
{
  "subagents": {
    "allow_mutation": true,
    "roles": {
      "default": {
        "tools": ["read", "grep", "glob", "activate_skill", "read_skill_resource", "bash"]
      },
      "worker": {
        "allow_mutation": true
      }
    }
  }
}
```

This does not create a sandbox; use `--permission ask` for interactive approvals or `--permission allow` only in a trusted environment.

`spawn_agent` and `followup_task` use the separate `delegate` permission risk. Deny mode hides/rejects delegation, ask mode prompts and can remember the decision for the session, and allow mode permits it. Every child tool call is independently permissioned.

## Persistence

With `durable=true`, every child transcript is a separate private SQLite database:

```text
<root-session>.agents/<thread-id>.db
```

The root database stores only bounded topology/status/usage metadata in `subagent_threads`, including an immutable role-policy fingerprint used to fail safe if trusted role configuration changes. Pre-v6 rows without a fingerprint are reloaded with a conservative read-only role, never with newly granted mutation authority. Child databases do not appear in the normal session picker. On cold open Snow restores topology without loading child runtimes, converts stale running/queued work to interrupted metadata, and never restarts work before `ReadySubagents`. Durable terminal children may be unloaded when the residency cap is exceeded; follow-up, messaging, or transcript inspection lazily reloads them. Set `durable=false` only when intentionally choosing process-local child history; `/agent` then warns that transcripts will not survive restart.

## Surfaces

- TUI: `/settings` persists enablement and child concurrency (applied on the next launch); `/agent` shows aggregate running/queued/finished counts, capacity, role/model/effort, duration, usage, result/error, and durability; `/agent <path-or-id>` displays full state plus a bounded tool-aware transcript; `/agent concurrency N` persists a positive child limit up to the safety cap of 256. The root transcript receives compact lifecycle rows, not child token streams.
- SDK: `ReadySubagents`, `SpawnSubagent`, `SendSubagentMessage`, `FollowupSubagent`, `WaitSubagents`, `WaitSubagentsUntilAll`, `InterruptSubagent`, `Subagents`, `Subagent`, and `SubagentUsage`.
- RPC: `subagent_ready`, `subagent_spawn`, `subagent_send_message`, `subagent_followup`, `subagent_wait`, `subagent_interrupt`, `subagent_list`, and `subagent_get`.
- JSON/events/plugins: ordinary child events carry `agent`; lifecycle events also carry `subagent`, and mailbox events carry `agent_message`. Root events retain omitted correlation fields for compatibility.

SDK and restored-session hosts should subscribe first, then call `ReadySubagents`. CLI, print/JSON, RPC, and TUI do this automatically.
