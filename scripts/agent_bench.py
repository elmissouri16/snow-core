#!/usr/bin/env python3
"""Run reproducible A/B tasks against Snow and Codex.

The runner isolates every repetition in a copied workspace, uses the same
prompt/model for both agents, verifies the resulting workspace, and writes
machine-readable per-run records plus an aggregate summary.
"""

from __future__ import annotations

import argparse
import json
import os
import platform
import re
import shutil
import statistics
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Any


DEFAULT_TOOLS = ("read", "write", "edit", "bash", "grep", "glob")


TIME_PATTERNS = {
    "max_rss_bytes": [
        (re.compile(r"(\d+)\s+maximum resident set size", re.I), 1),
        (re.compile(r"maximum resident set size:\s*(\d+)", re.I), 1),
        (re.compile(r"Maximum resident set size \(kbytes\):\s*(\d+)", re.I), 1024),
    ],
    "cpu_user_s": [
        (re.compile(r"user time \(seconds\):\s*([0-9.]+)", re.I), 1),
        (re.compile(r"([0-9.]+)\s+user\b", re.I), 1),
    ],
    "cpu_system_s": [
        (re.compile(r"system time \(seconds\):\s*([0-9.]+)", re.I), 1),
        (re.compile(r"([0-9.]+)\s+sys\b", re.I), 1),
    ],
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tasks", required=True, type=Path, help="JSON task file")
    parser.add_argument("--repo", type=Path, default=Path.cwd(), help="workspace to copy for each run")
    parser.add_argument("--results", type=Path, default=Path("agent-bench-results.json"))
    parser.add_argument("--model", required=True, help="identical model id passed to both agents")
    parser.add_argument("--provider", default="chatgpt", help="Snow provider")
    parser.add_argument("--tools", default=",".join(DEFAULT_TOOLS), help="common capability profile; comma-separated")
    parser.add_argument("--snow-bin", default="snow")
    parser.add_argument("--codex-bin", default="codex")
    parser.add_argument("--agents", nargs="+", choices=("snow", "codex"), default=("snow", "codex"))
    parser.add_argument("--repetitions", type=int, default=1)
    parser.add_argument("--timeout", type=int, default=1800, help="agent timeout in seconds")
    parser.add_argument("--verify-timeout", type=int, default=300)
    parser.add_argument("--seed", type=int, default=1)
    parser.add_argument("--keep-workspaces", action="store_true")
    parser.add_argument("--workspaces", type=Path, help="workspace run root; defaults to a temporary directory")
    parser.add_argument("--dry-run", action="store_true")
    return parser.parse_args()


def load_tasks(path: Path) -> list[dict[str, Any]]:
    data = json.loads(path.read_text())
    tasks = data.get("tasks", data) if isinstance(data, dict) else data
    if not isinstance(tasks, list) or not tasks:
        raise ValueError("task file must contain a non-empty array or {\"tasks\": [...]}")
    for task in tasks:
        if not isinstance(task, dict) or not task.get("id") or not task.get("prompt"):
            raise ValueError("each task needs id and prompt")
    return tasks


def copy_workspace(source: Path, destination: Path) -> None:
    ignored = shutil.ignore_patterns(
        ".git", ".snow", ".codex", "node_modules", ".venv", "venv", "dist", "target",
        "agent-bench-results.json", "bench-runs", "*-runs", "*-logs",
    )
    shutil.copytree(source, destination, ignore=ignored)


def time_wrapper() -> list[str]:
    time_bin = Path("/usr/bin/time")
    if not time_bin.exists():
        return []
    return [str(time_bin), "-l" if platform.system() == "Darwin" else "-v"]


def parse_time_metrics(stderr: str) -> dict[str, float | int]:
    metrics: dict[str, float | int] = {}
    for key, patterns in TIME_PATTERNS.items():
        for pattern, multiplier in patterns:
            match = pattern.search(stderr)
            if match:
                value = float(match.group(1)) * multiplier
                metrics[key] = int(value) if key == "max_rss_bytes" else value
                break
    return metrics


def run_process(command: list[str], cwd: Path, timeout: int) -> dict[str, Any]:
    wrapped = time_wrapper() + command
    started = time.perf_counter()
    try:
        completed = subprocess.run(
            wrapped,
            cwd=cwd,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=timeout,
            check=False,
        )
        timed_out = False
    except subprocess.TimeoutExpired as exc:
        completed = subprocess.CompletedProcess(
            wrapped,
            returncode=124,
            stdout=exc.stdout or "",
            stderr=(exc.stderr or "") + "\nprocess timed out",
        )
        timed_out = True
    elapsed = time.perf_counter() - started
    stdout = completed.stdout if isinstance(completed.stdout, str) else (completed.stdout or b"").decode(errors="replace")
    stderr = completed.stderr if isinstance(completed.stderr, str) else (completed.stderr or b"").decode(errors="replace")
    metrics: dict[str, Any] = {
        "exit_code": completed.returncode,
        "timed_out": timed_out,
        "wall_time_s": elapsed,
        "stdout_bytes": len(stdout.encode()),
        "stderr_bytes": len(stderr.encode()),
    }
    metrics.update(parse_time_metrics(stderr))
    return {"metrics": metrics, "stdout": stdout, "stderr": stderr}


def normalize_usage(value: Any) -> dict[str, int] | None:
    if not isinstance(value, dict):
        return None
    aliases = {
        "input": ("input", "input_tokens", "prompt_tokens"),
        "output": ("output", "output_tokens", "completion_tokens"),
        "reasoning": ("reasoning", "reasoning_tokens", "reasoning_output_tokens"),
        "cache_read": ("cache_read", "cache_read_input_tokens", "cached_input_tokens"),
        "cache_write": ("cache_write", "cache_creation_input_tokens", "cache_write_input_tokens"),
        "total": ("total", "total_tokens"),
    }
    result: dict[str, int] = {}
    for target, names in aliases.items():
        for name in names:
            raw = value.get(name)
            if isinstance(raw, (int, float)):
                result[target] = int(raw)
                break
    return result or None


def collect_usage(stdout: str) -> dict[str, int]:
    request_records: list[dict[str, int]] = []
    final_records: list[dict[str, int]] = []
    for line in stdout.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not isinstance(event, dict):
            continue
        event_type = event.get("type")
        usage = normalize_usage(event.get("usage"))
        if not usage:
            continue
        if event_type in ("turn_done", "turn.completed", "response.completed"):
            final_records.append(usage)
        elif event_type == "usage":
            request_records.append(usage)
    # Snow emits per-request usage and a cumulative turn_done usage. Codex
    # emits a final turn.completed usage. Prefer that final aggregate to avoid
    # double-counting the same turn.
    totals = dict(final_records[-1]) if final_records else {}
    if not totals:
        for record in request_records:
            for key, value in record.items():
                totals[key] = totals.get(key, 0) + value
    if "total" not in totals:
        totals["total"] = sum(totals.get(key, 0) for key in ("input", "output", "reasoning"))
    return totals


def verify_task(task: dict[str, Any], cwd: Path, timeout: int) -> dict[str, Any]:
    verify = task.get("verify")
    if not verify:
        return {"exit_code": 0, "wall_time_s": 0.0, "skipped": True}
    if isinstance(verify, str):
        command = ["sh", "-lc", verify]
    elif isinstance(verify, list) and all(isinstance(item, str) for item in verify):
        command = verify
    else:
        raise ValueError(f"task {task['id']}: verify must be a string or argv array")
    result = run_process(command, cwd, timeout)
    return {"exit_code": result["metrics"]["exit_code"], "wall_time_s": result["metrics"]["wall_time_s"], "stderr": result["stderr"][-4000:]}


def agent_command(agent: str, args: argparse.Namespace, prompt: str, cwd: Path) -> list[str]:
    tools = [item.strip() for item in args.tools.split(",") if item.strip()]
    if not tools:
        raise ValueError("--tools must contain at least one capability")
    policy = (
        "Benchmark capability policy: use only these capabilities: "
        + ", ".join(tools)
        + ". Do not use network access, delegation, plugins, MCP, or skills.\n\n"
    )
    effective_prompt = policy + prompt
    if agent == "snow":
        return [
            args.snow_bin, "--mode", "json", "--provider", args.provider,
            "--model", args.model, "--permission", "allow", "--session",
            str(cwd / ".snow-benchmark-session.db"), "--tools", ",".join(tools),
            "--no-mcp", "--no-skills", "--no-plugins", "--no-subagents",
            "--prompt", effective_prompt,
        ]
    return [
        args.codex_bin, "exec", "--json", "--ephemeral", "--color", "never",
        "--ignore-user-config", "--ignore-rules", "--model", args.model,
        "--sandbox", "workspace-write", "--cd", str(cwd),
        "--skip-git-repo-check", effective_prompt,
    ]


def percentile(values: list[float], p: float) -> float | None:
    if not values:
        return None
    values = sorted(values)
    index = min(len(values) - 1, int(round((len(values) - 1) * p)))
    return values[index]


def summarize(records: list[dict[str, Any]]) -> list[dict[str, Any]]:
    groups: dict[tuple[str, str], list[dict[str, Any]]] = {}
    for record in records:
        groups.setdefault((record["agent"], record["task_id"]), []).append(record)
    output = []
    for (agent, task_id), group in sorted(groups.items()):
        successful = [r for r in group if r["success"]]
        def metric(name: str) -> list[float]:
            return [float(r["agent_metrics"].get(name, 0)) for r in group if name in r["agent_metrics"]]
        walls = metric("wall_time_s")
        output.append({
            "agent": agent,
            "task_id": task_id,
            "runs": len(group),
            "successes": len(successful),
            "success_rate": len(successful) / len(group),
            "wall_time_s": {"mean": statistics.mean(walls) if walls else None, "p50": percentile(walls, .50), "p95": percentile(walls, .95)},
            "cpu_user_s_mean": statistics.mean(metric("cpu_user_s")) if metric("cpu_user_s") else None,
            "cpu_system_s_mean": statistics.mean(metric("cpu_system_s")) if metric("cpu_system_s") else None,
            "max_rss_mb_mean": statistics.mean(metric("max_rss_bytes")) / 1024 / 1024 if metric("max_rss_bytes") else None,
            "tokens": {key: sum(int(r["usage"].get(key, 0)) for r in group) for key in ("input", "output", "reasoning", "cache_read", "cache_write", "total")},
        })
    return output


def main() -> int:
    args = parse_args()
    if args.repetitions < 1:
        raise SystemExit("--repetitions must be positive")
    args.tools = ",".join(item.strip() for item in args.tools.split(",") if item.strip())
    if not args.tools:
        raise SystemExit("--tools must contain at least one capability")
    tasks = load_tasks(args.tasks)
    source = args.repo.resolve()
    if not source.is_dir():
        raise SystemExit(f"repo does not exist: {source}")

    import random
    jobs = [(task, agent, repetition) for repetition in range(1, args.repetitions + 1) for task in tasks for agent in args.agents]
    random.Random(args.seed).shuffle(jobs)
    args.results = args.results.resolve()
    if args.workspaces:
        results_root = args.workspaces.resolve()
        results_root.mkdir(parents=True, exist_ok=True)
        remove_run_root = False
    else:
        results_root = Path(tempfile.mkdtemp(prefix="snow-agent-bench-"))
        remove_run_root = not args.keep_workspaces
    records: list[dict[str, Any]] = []
    logs_root = args.results.parent / (args.results.stem + "-logs")
    logs_root.mkdir(parents=True, exist_ok=True)

    for index, (task, agent, repetition) in enumerate(jobs, 1):
        run_id = f"{index:04d}-{task['id']}-{agent}-r{repetition}"
        workspace = results_root / run_id
        copy_workspace(source, workspace)
        prompt = str(task["prompt"])
        command = agent_command(agent, args, prompt, workspace)
        print(f"[{index}/{len(jobs)}] {agent} {task['id']} repetition={repetition}", flush=True)
        if args.dry_run:
            print("  "+" ".join(command), flush=True)
            shutil.rmtree(workspace, ignore_errors=True)
            continue
        run = run_process(command, workspace, args.timeout)
        verification = verify_task(task, workspace, args.verify_timeout)
        agent_metrics = run["metrics"]
        success = agent_metrics["exit_code"] == 0 and verification["exit_code"] == 0 and not agent_metrics["timed_out"]
        record = {
            "run_id": run_id,
            "agent": agent,
            "task_id": task["id"],
            "repetition": repetition,
            "model": args.model,
            "success": success,
            "agent_metrics": agent_metrics,
            "verification": verification,
            "usage": collect_usage(run["stdout"]),
        }
        (workspace / "agent.stdout").write_text(run["stdout"])
        (workspace / "agent.stderr").write_text(run["stderr"])
        if args.keep_workspaces or not success:
            stdout_log = logs_root / f"{run_id}.stdout"
            stderr_log = logs_root / f"{run_id}.stderr"
            stdout_log.write_text(run["stdout"])
            stderr_log.write_text(run["stderr"])
            record["stdout_file"] = str(stdout_log.relative_to(args.results.parent))
            record["stderr_file"] = str(stderr_log.relative_to(args.results.parent))
        records.append(record)
        print(f"  success={success} wall={agent_metrics['wall_time_s']:.2f}s tokens={record['usage'].get('total', 0)}", flush=True)
        if not args.keep_workspaces:
            # Keep logs for failures; successful workspaces are disposable.
            if success:
                shutil.rmtree(workspace, ignore_errors=True)

    payload = {
        "schema": 1,
        "model": args.model,
        "provider": args.provider,
        "tools": args.tools.split(","),
        "seed": args.seed,
        "repetitions": args.repetitions,
        "records": records,
        "summary": summarize(records),
    }
    args.results.parent.mkdir(parents=True, exist_ok=True)
    args.results.write_text(json.dumps(payload, indent=2) + "\n")
    print(f"wrote {args.results}")
    if args.keep_workspaces:
        print(f"workspaces: {results_root}")
    elif remove_run_root:
        shutil.rmtree(results_root, ignore_errors=True)
    for row in payload["summary"]:
        print(f"{row['agent']:>5} {row['task_id']:<24} success={row['success_rate']:.0%} p50={row['wall_time_s']['p50'] or 0:.2f}s")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except KeyboardInterrupt:
        raise SystemExit(130)
