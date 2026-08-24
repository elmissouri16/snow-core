#!/usr/bin/env python3
"""Check deterministic Go benchmark allocation ceilings.

Allocation ceilings provide the precise gate. Optional, deliberately generous
ns/op ceilings catch catastrophic CPU regressions without treating noisy hosted
runner timing as a product latency contract.
"""

from __future__ import annotations

import argparse
import json
import math
import re
import statistics
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any


GO_VERSION = re.compile(r"go1\.\d+(?:\.\d+)?(?:(?:beta|rc)\d+)?$")
METRICS = ("ns/op", "B/op", "allocs/op")


@dataclass(frozen=True)
class BenchmarkSample:
    name: str
    ns_per_op: float
    bytes_per_op: float
    allocs_per_op: float


def normalize_benchmark_name(name: str) -> str:
    # The checker always runs with -cpu=1, which emits no synthetic CPU suffix.
    # Preserve numeric suffixes because they may be part of a subbenchmark name.
    return name


def parse_benchmark_output(output: str) -> dict[str, list[BenchmarkSample]]:
    samples: dict[str, list[BenchmarkSample]] = {}
    for raw_line in output.splitlines():
        line = raw_line.strip()
        if not line.startswith("Benchmark"):
            continue
        fields = line.split()
        if len(fields) < 4:
            raise ValueError(f"malformed benchmark line: {raw_line!r}")
        name = normalize_benchmark_name(fields[0])
        values: dict[str, float] = {}
        metric_fields = fields[2:]
        for index in range(0, len(metric_fields) - 1, 2):
            value, unit = metric_fields[index : index + 2]
            if unit not in METRICS:
                continue
            try:
                parsed = float(value)
            except ValueError as exc:
                raise ValueError(f"invalid {unit} value in line: {raw_line!r}") from exc
            if not math.isfinite(parsed) or parsed < 0:
                raise ValueError(f"invalid {unit} value in line: {raw_line!r}")
            values[unit] = parsed
        missing = [metric for metric in METRICS if metric not in values]
        if missing:
            raise ValueError(f"benchmark {name} missing metrics: {', '.join(missing)}")
        samples.setdefault(name, []).append(
            BenchmarkSample(
                name=name,
                ns_per_op=values["ns/op"],
                bytes_per_op=values["B/op"],
                allocs_per_op=values["allocs/op"],
            )
        )
    return samples


def median_sample(name: str, samples: list[BenchmarkSample]) -> BenchmarkSample:
    return BenchmarkSample(
        name=name,
        ns_per_op=statistics.median(sample.ns_per_op for sample in samples),
        bytes_per_op=statistics.median(sample.bytes_per_op for sample in samples),
        allocs_per_op=statistics.median(sample.allocs_per_op for sample in samples),
    )


def validate_config(config: Any) -> None:
    if not isinstance(config, dict):
        raise ValueError("performance limits must be a JSON object")
    if config.get("version") != 1:
        raise ValueError("performance limits version must be 1")
    go_version = config.get("go_version")
    if not isinstance(go_version, str) or not GO_VERSION.fullmatch(go_version):
        raise ValueError("go_version must be an exact Go toolchain version")
    samples = config.get("samples", 3)
    if isinstance(samples, bool) or not isinstance(samples, int) or samples < 1:
        raise ValueError("samples must be a positive integer")
    benchtime = config.get("benchtime", "1x")
    if not isinstance(benchtime, str) or not benchtime.strip():
        raise ValueError("benchtime must be a non-empty string")
    timeout = config.get("benchmark_timeout_seconds", 900)
    if isinstance(timeout, bool) or not isinstance(timeout, int) or timeout < 1:
        raise ValueError("benchmark_timeout_seconds must be a positive integer")
    groups = config.get("groups")
    if not isinstance(groups, list) or not groups:
        raise ValueError("groups must be a non-empty array")
    seen: set[str] = set()
    for group in groups:
        if not isinstance(group, dict):
            raise ValueError("each group must be an object")
        package = group.get("package")
        pattern = group.get("pattern")
        if not isinstance(package, str) or not package.strip():
            raise ValueError("each group needs a non-empty string package")
        if not isinstance(pattern, str) or not pattern.strip():
            raise ValueError("each group needs a non-empty string pattern")
        benchmarks = group.get("benchmarks")
        if not isinstance(benchmarks, dict) or not benchmarks:
            raise ValueError(f"group {group.get('package')} needs benchmarks")
        for name, limits in benchmarks.items():
            if not isinstance(name, str) or not name:
                raise ValueError("benchmark names must be non-empty strings")
            if name in seen:
                raise ValueError(f"duplicate benchmark limit: {name}")
            seen.add(name)
            if not isinstance(limits, dict):
                raise ValueError(f"benchmark {name} limits must be an object")
            for key in ("max_bytes_per_op", "max_allocs_per_op"):
                value = limits.get(key)
                if not finite_positive_number(value):
                    raise ValueError(f"benchmark {name} needs finite positive {key}")
            if "max_ns_per_op" in limits and not finite_positive_number(limits["max_ns_per_op"]):
                raise ValueError(f"benchmark {name} needs finite positive max_ns_per_op")


def finite_positive_number(value: Any) -> bool:
    return (
        not isinstance(value, bool)
        and isinstance(value, (int, float))
        and math.isfinite(float(value))
        and value > 0
    )


def check_go_version(root: Path, go_binary: str, expected: str) -> None:
    try:
        completed = subprocess.run(
            [go_binary, "env", "GOVERSION"],
            cwd=root,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
            timeout=30,
        )
    except OSError as exc:
        raise RuntimeError(f"cannot launch {go_binary}: {exc}") from exc
    except subprocess.TimeoutExpired as exc:
        raise RuntimeError(f"{go_binary} env GOVERSION timed out") from exc
    if completed.returncode != 0:
        raise RuntimeError(
            f"cannot read Go version with {go_binary}:\n{completed.stdout.rstrip()}"
        )
    actual = completed.stdout.strip()
    if actual != expected:
        raise RuntimeError(f"Go version is {actual or 'unknown'}, expected {expected}")


def run_benchmark_group(
    root: Path,
    go_binary: str,
    package: str,
    pattern: str,
    samples: int,
    benchtime: str,
    timeout_seconds: int = 900,
) -> str:
    command = [
        go_binary,
        "test",
        package,
        "-run",
        "^$",
        "-bench",
        pattern,
        "-benchmem",
        f"-benchtime={benchtime}",
        f"-count={samples}",
        "-cpu=1",
    ]
    try:
        completed = subprocess.run(
            command,
            cwd=root,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
            timeout=timeout_seconds,
        )
    except OSError as exc:
        raise RuntimeError(f"cannot launch benchmark command: {exc}") from exc
    except subprocess.TimeoutExpired as exc:
        raise RuntimeError(
            f"benchmark command timed out after {timeout_seconds}s: {' '.join(command)}"
        ) from exc
    if completed.returncode != 0:
        raise RuntimeError(
            f"benchmark command failed ({' '.join(command)}):\n{completed.stdout.rstrip()}"
        )
    return completed.stdout


def evaluate_group(
    group: dict[str, Any],
    parsed: dict[str, list[BenchmarkSample]],
    expected_samples: int,
) -> tuple[list[BenchmarkSample], list[str]]:
    medians: list[BenchmarkSample] = []
    failures: list[str] = []
    for name, limits in group["benchmarks"].items():
        samples = parsed.get(name)
        if not samples:
            failures.append(f"{name}: benchmark result missing")
            continue
        if len(samples) != expected_samples:
            failures.append(
                f"{name}: got {len(samples)} samples, expected {expected_samples}"
            )
            continue
        result = median_sample(name, samples)
        medians.append(result)
        max_bytes = float(limits["max_bytes_per_op"])
        max_allocs = float(limits["max_allocs_per_op"])
        max_ns = limits.get("max_ns_per_op")
        if max_ns is not None and result.ns_per_op > float(max_ns):
            failures.append(
                f"{name}: {result.ns_per_op:,.0f} ns/op exceeds {float(max_ns):,.0f}"
            )
        if result.bytes_per_op > max_bytes:
            failures.append(
                f"{name}: {result.bytes_per_op:,.0f} B/op exceeds {max_bytes:,.0f}"
            )
        if result.allocs_per_op > max_allocs:
            failures.append(
                f"{name}: {result.allocs_per_op:,.0f} allocs/op exceeds {max_allocs:,.0f}"
            )
    return medians, failures


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--config",
        type=Path,
        default=Path("benchmarks/performance-limits.json"),
        help="checked-in allocation limit file",
    )
    parser.add_argument("--go", default="go", help="Go executable")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    root = Path(__file__).resolve().parent.parent
    config_path = args.config if args.config.is_absolute() else root / args.config
    try:
        config = json.loads(config_path.read_text(encoding="utf-8"))
        validate_config(config)
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        print(f"benchmark configuration error: {exc}", file=sys.stderr)
        return 2

    expected_samples = int(config.get("samples", 3))
    benchtime = str(config.get("benchtime", "1x"))
    timeout_seconds = int(config.get("benchmark_timeout_seconds", 900))
    try:
        check_go_version(root, args.go, config["go_version"])
    except RuntimeError as exc:
        print(f"benchmark toolchain error: {exc}", file=sys.stderr)
        return 2
    all_results: list[BenchmarkSample] = []
    failures: list[str] = []
    for group in config["groups"]:
        package = group["package"]
        print(f"\n==> {package}", flush=True)
        try:
            output = run_benchmark_group(
                root,
                args.go,
                package,
                group["pattern"],
                expected_samples,
                benchtime,
                timeout_seconds,
            )
            parsed = parse_benchmark_output(output)
            medians, group_failures = evaluate_group(
                group, parsed, expected_samples
            )
            all_results.extend(medians)
            failures.extend(group_failures)
        except (RuntimeError, ValueError) as exc:
            failures.append(f"{package}: {exc}")

    if all_results:
        print("\nPerformance medians (timing ceilings are catastrophe guards):")
        for result in all_results:
            print(
                f"  {result.name:<58} "
                f"{result.ns_per_op:>12,.0f} ns/op  "
                f"{result.bytes_per_op:>12,.0f} B/op  "
                f"{result.allocs_per_op:>9,.0f} allocs/op"
            )
    if failures:
        print("\nPerformance regression guard failed:", file=sys.stderr)
        for failure in failures:
            print(f"  - {failure}", file=sys.stderr)
        return 1
    print("\nPerformance regression guard passed.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
