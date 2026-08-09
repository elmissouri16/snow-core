# Snow vs Codex agent benchmarks

`../scripts/agent_bench.py` runs the same task prompts against Snow and Codex
in isolated workspace copies. It records:

- success/failure from both the agent exit code and the task verifier;
- wall-clock time;
- user/system CPU time and maximum resident memory when `/usr/bin/time` is available;
- token/cache usage parsed from Snow/Codex JSONL events;
- stdout/stderr logs for failed runs (and all logs when `--keep-workspaces` is used).

## Run

Use a model available to both clients and authenticate both clients first:

```sh
python3 scripts/agent_bench.py \
  --tasks benchmarks/tasks.example.json \
  --model gpt-5.4-mini \
  --provider chatgpt \
  --repetitions 3 \
  --results benchmarks/results.json \
  --keep-workspaces
```

Preview commands without running agents:

```sh
python3 scripts/agent_bench.py \
  --tasks benchmarks/tasks.example.json \
  --model gpt-5.4-mini \
  --dry-run
```

The example suite covers exact file creation, nested directories, JSON and
CSV/JSONL serialization, repository inspection, Python implementations and
unit tests, shell scripting, and standalone Go programs. The runner randomizes
agent/task order with
`--seed` to reduce warm-cache and time-of-day bias. Keep task prompts identical
and make verifiers deterministic. For coding tasks, use an isolated fixture or
copied repository and verify with a specific test command. `--sandbox
workspace-write` is used for Codex and Snow runs with `--permission allow`, so
only run trusted task files in disposable workspaces.

The runner defaults to the common capability profile
`read,write,edit,bash,grep,glob`. Snow enforces this profile with its `--tools`
allowlist; Codex has no equivalent per-tool CLI allowlist, so the runner uses
its workspace-write sandbox, ignores user config/rules, and gives both agents
the same no-network/no-delegation policy. This equalizes capabilities and
optional extensions, but does not make the underlying tool implementations
identical.

Results are JSON for later analysis. Compare `summary[].success_rate` first,
then p50/p95 wall time, CPU/RSS, and token totals. Do not compare one-off runs;
use multiple repetitions and report the model/provider/version, capability
profile, and seed.
