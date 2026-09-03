# Subagents

Snow can delegate focused tasks to child agents while the main agent coordinates
the work. Subagents are disabled by default and use separate provider requests.

> **Warning:** Subagents are not a sandbox. Children share the working
> directory, filesystem, process side effects, and operating-system privileges
> of the root Snow process.

## On this page

- [Enable subagents](#enable-subagents)
- [Choose roles and models](#choose-roles-and-models)
- [Delegate work](#delegate-work)
- [Inspect and control children](#inspect-and-control-children)
- [Set limits](#set-limits)
- [Work safely](#work-safely)
- [Resume child work](#resume-child-work)
- [Related documents](#related-documents)

## Enable subagents

Enable child agents for one launch:

```sh
snow --subagents
```

Choose an automatic provider and model for children when they should differ
from the root agent:

```sh
snow --subagents \
  --subagent-provider opencode-go \
  --subagent-model model-id
```

Use `--no-subagents` to override enabled configuration. Enabling subagents does
not enable recursion or file mutation, and every delegated turn remains subject
to the root permission mode.

## Choose roles and models

Snow provides these built-in roles:

| Role | Use it for |
|---|---|
| `explorer` | Read-only repository inspection and evidence gathering |
| `general` | Inspection plus permission-gated shell tasks |
| `implementer` | Focused implementation when mutation is enabled by policy |

Roles are capability profiles, not writing styles. Put the assignment and
expected output in the task itself. The retired `default` and `worker` names
are not accepted.

A child normally inherits the parent provider and model. Command-line defaults,
role configuration, or an explicit per-child provider/model selection can
override that choice. Use Snow's available-model inventory before requesting a
specific model or reasoning level rather than guessing an ID.

## Delegate work

When subagents are enabled, the root model can:

- create a named child for one focused assignment;
- inspect available provider/model pairs;
- send attributed messages;
- queue follow-up work to an idle child;
- wait for child activity or all descendants;
- interrupt, close, or resume a child; and
- list child states and retrieve a bounded result.

Child names become stable paths such as `/root/api_review`. Each new child
receives its task, role guidance, project context, and relevant repository
instructions. It does not receive the full parent conversation or active Thread
Goal unless the assignment includes that information.

Use separate children for independent work. For implementation, assign disjoint
file ownership and tell each child not to overwrite peer changes.

## Inspect and control children

Open the TUI fleet inspector with:

```text
/agent
/agent /root/api_review
```

You can also press Alt+A. The inspector shows child state, role, provider,
model, usage, task, final result, and a bounded event transcript. Use the arrow
keys or `j`/`k` to select a child, PageUp/PageDown or the wheel to scroll, `r`
to refresh, and Esc to close.

Closing a finished child releases an open-agent slot while preserving its
identity and history. Resuming reopens that child without starting a turn;
queue a follow-up when you want it to work again. Interrupting a child cancels
only its current turn.

## Set limits

Configure explicit limits when a task may fan out:

```sh
snow --subagents \
  --subagent-max-concurrency 10 \
  --subagent-max-agents 32 \
  --subagent-max-depth 1
```

The default limits are intentionally conservative. Snow also bounds task time,
wait time, result size, mailbox input, and open child count. Recursive spawning
is disabled unless the selected role explicitly permits it.

Change the next-launch concurrency from the TUI with:

```text
/agent concurrency 4
```

## Work safely

Subagent safety uses several independent controls:

- subagents are opt-in;
- delegation has its own permission risk;
- child tools are the intersection of root and role allowlists;
- file mutation requires both global and role permission;
- recursion is disabled by default;
- children do not receive plugins, MCP, Thread Goals, or interactive input;
- count, depth, time, wait, and output are bounded.

These controls limit authority but do not isolate the process. Bash can mutate
files even when direct file-edit tools are absent. Never send parallel mutators
to overlapping files, and use an external container or VM when host isolation
is required.

Plan Mode permits only read-only, non-recursive child profiles. Snow rejects a
transition into Plan Mode while mutation-capable child work is active.

## Resume child work

With a durable root session, Snow restores child identities and completed
history when the session resumes. Work that was running during interruption is
shown as interrupted rather than restarted automatically.

Child histories are private companion data, not ordinary root sessions. Delete
the root session through Snow when you also want its Snow-owned child history
removed.

## Related documents

- [Sessions and branches](sessions.md)
- [Plan Mode](plan-mode.md)
- [Thread Goals](goals.md)
- [Configuration](configuration.md)
- [Go SDK](sdk.md)
- [Security model](security.md)
