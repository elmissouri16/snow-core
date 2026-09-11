# Mock-host plugin fixtures

Run `snow plugin test . --fixtures tests/plugin.json` (add `--json` for a stable
versioned report). No configured provider, live host, process, or real storage is
used. Each case creates a fresh actual Goja runtime; steps share that case's JS
and in-memory state. Dependencies are never installed by the test command.

```json
{
  "version": 1,
  "tests": [{
    "name": "command returns a greeting",
    "storage": {"project": {"visits": 4}},
    "workflow": {"profile": "reviewer"},
    "steps": [{
      "kind": "command",
      "name": "hello",
      "input": "world",
      "calls": [
        {"operation":"storage.get","args":{"key":"visits"},"memory":true},
        {"operation":"storage.set","args":{"key":"visits","value":5},"memory":true},
        {"operation":"ui.update","args":{"name":"status","content":{"type":"text","text":"Hello world · visit 5","tone":"accent"}}}
      ],
      "select": "/content/0/text",
      "expect": "Hello world · visit 5"
    }]
  }]
}
```

## Steps and assertions

- `command`: unqualified registered `name`, optional string `input`.
- `tool`: unqualified registered `name`, optional JSON `args` (defaults to `{}`).
- `hook`: a `request` object containing the hook `phase` and its normal input.
  Registered `workflowKeys` are preloaded from this case's workflow memory,
  unless an explicit `request.workflow` overrides the snapshot for that step.
- `ready`: explicitly invokes `onReady`; it is not run automatically.
- `event`: an `event` object with `type` and `payload`; `version` defaults to the
  current event protocol. Registered observers execute serially, each awaited.
  This tests production callbacks, not the asynchronous event queue.
- `metadata`: returns `{plugin, tools, events}`. `plugin` is the extension info;
  tools expose name, description, parameters, and risk. For example, select
  `/plugin/commands/0/name` or `/tools/0/name` to assert registrations.

`expect` checks an exact JSON result. `select` is an optional JSON Pointer into
that result (including array indexes and `~0`/`~1` escapes). Tool and command
results use `{content, isError?, details?}`; hooks use their normal result shape;
readiness/events return `null`. A missing `expect` skips the result assertion.

Alternatively, `error` expects an error containing the specified nonempty
substring. Do not combine `error` and `expect`.

## Explicit host-call ledger

Every host operation must match the next entry in the step's `calls` array,
including exact JSON arguments (`args` defaults to `{}`). Unexpected, missing,
misordered, or unauthorized calls fail—even when plugin code catches the error.
Parallel host calls must have a deterministic order to use an ordered fixture.

An entry may contain one of:

- `result`: copied JSON returned by the fake host (defaults to `null`).
- `error`: an intentional fake host error, which JS may catch or propagate.
- `memory: true`: use the case's fake state for `storage.get/set/delete`,
  `workflow.get/set/delete/update`, or `tools.restrict/clearRestriction`.

Omitted storage scope defaults to `project`, matching the real host.
Storage memory writes return `null`; reads return the saved JSON value or `null`
for a missing key. Workflow/restriction writes return synthetic receipts such as
`{"branchId":"fixture","tipId":"fixture-1"}` with an incrementing per-case
revision. These receipts model the API shape, not real ancestry. Workflow writes
are immediate; hooks in subsequent steps see them. Other host
operations, including all UI calls and tools, require explicit results/errors.
Host-tool calls use operation `tools.call` with arguments
`{"name":"read","arguments":{"path":"example.txt"}}`.

The adapter validates handler authority and the fake host checks manifest and
invocation capabilities before satisfying a mock. However, mocks do **not**
prove real permissions, tool schemas/admission, branch ancestry, workflow
quotas, cross-plugin intersections, reload transactions, or lifecycle ordering.
Use Snow's Go integration tests and credential-free manual checks for those.

## Limits

Files are regular UTF-8 JSON files with unknown fields rejected, bounded to
1 MiB and 128 tests. Cases have unique names (at most 256 bytes) and 1–1,024
steps. Each case has five seconds; the suite has 60 seconds. Normal Goja output,
CPU, callback, and host-call limits also apply. Reports bound per-case error
text and omit nondeterministic durations. Any failure produces a nonzero CLI
exit status; `--json` still prints the report for completed cases.
