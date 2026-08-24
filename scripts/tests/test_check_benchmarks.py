from __future__ import annotations

import importlib.util
import subprocess
import sys
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / "check_benchmarks.py"
SPEC = importlib.util.spec_from_file_location("check_benchmarks", SCRIPT)
assert SPEC and SPEC.loader
check_benchmarks = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = check_benchmarks
SPEC.loader.exec_module(check_benchmarks)


class ParseBenchmarkOutputTests(unittest.TestCase):
    def test_preserves_numeric_subbenchmark_suffix_and_extra_metric(self) -> None:
        parsed = check_benchmarks.parse_benchmark_output(
            "BenchmarkBuild/plain/size-2  1  123 ns/op  42.5 MB/s  456 B/op  7 allocs/op\n"
        )
        self.assertEqual(list(parsed), ["BenchmarkBuild/plain/size-2"])
        sample = parsed["BenchmarkBuild/plain/size-2"][0]
        self.assertEqual(sample.ns_per_op, 123)
        self.assertEqual(sample.bytes_per_op, 456)
        self.assertEqual(sample.allocs_per_op, 7)

    def test_collects_multiple_samples(self) -> None:
        output = "\n".join(
            [
                "BenchmarkOne  1  30 ns/op  300 B/op  3 allocs/op",
                "BenchmarkOne  1  10 ns/op  100 B/op  1 allocs/op",
                "BenchmarkOne  1  20 ns/op  200 B/op  2 allocs/op",
            ]
        )
        parsed = check_benchmarks.parse_benchmark_output(output)
        median = check_benchmarks.median_sample("BenchmarkOne", parsed["BenchmarkOne"])
        self.assertEqual((median.ns_per_op, median.bytes_per_op, median.allocs_per_op), (20, 200, 2))

    def test_rejects_missing_allocation_metric(self) -> None:
        with self.assertRaisesRegex(ValueError, "missing metrics"):
            check_benchmarks.parse_benchmark_output(
                "BenchmarkOne  1  10 ns/op  100 B/op"
            )

    def test_rejects_malformed_metric_value(self) -> None:
        with self.assertRaisesRegex(ValueError, "invalid B/op"):
            check_benchmarks.parse_benchmark_output(
                "BenchmarkOne  1  10 ns/op  nope B/op  1 allocs/op"
            )

    def test_rejects_nonfinite_or_negative_metrics(self) -> None:
        for value in ("NaN", "Infinity", "-1"):
            with self.subTest(value=value):
                with self.assertRaisesRegex(ValueError, "invalid B/op"):
                    check_benchmarks.parse_benchmark_output(
                        f"BenchmarkOne 1 10 ns/op {value} B/op 1 allocs/op"
                    )


class EvaluateBenchmarkTests(unittest.TestCase):
    def test_reports_missing_samples_and_threshold_failures(self) -> None:
        group = {
            "benchmarks": {
                "BenchmarkOne": {
                    "max_ns_per_op": 5,
                    "max_bytes_per_op": 150,
                    "max_allocs_per_op": 1,
                },
                "BenchmarkMissing": {
                    "max_bytes_per_op": 1,
                    "max_allocs_per_op": 1,
                },
            }
        }
        parsed = check_benchmarks.parse_benchmark_output(
            "\n".join(
                [
                    "BenchmarkOne  1  10 ns/op  200 B/op  2 allocs/op",
                    "BenchmarkOne  1  11 ns/op  210 B/op  3 allocs/op",
                    "BenchmarkOne  1  12 ns/op  220 B/op  4 allocs/op",
                ]
            )
        )
        medians, failures = check_benchmarks.evaluate_group(group, parsed, 3)
        self.assertEqual(len(medians), 1)
        self.assertTrue(any("ns/op exceeds" in failure for failure in failures))
        self.assertTrue(any("B/op exceeds" in failure for failure in failures))
        self.assertTrue(any("allocs/op exceeds" in failure for failure in failures))
        self.assertTrue(any("result missing" in failure for failure in failures))

    def test_rejects_wrong_sample_count(self) -> None:
        group = {
            "benchmarks": {
                "BenchmarkOne": {
                    "max_bytes_per_op": 1000,
                    "max_allocs_per_op": 10,
                }
            }
        }
        parsed = check_benchmarks.parse_benchmark_output(
            "BenchmarkOne  1  10 ns/op  100 B/op  1 allocs/op"
        )
        _, failures = check_benchmarks.evaluate_group(group, parsed, 3)
        self.assertEqual(failures, ["BenchmarkOne: got 1 samples, expected 3"])


class ConfigurationAndRunnerTests(unittest.TestCase):
    def test_validate_config_rejects_non_object_and_typed_group_fields(self) -> None:
        with self.assertRaisesRegex(ValueError, "JSON object"):
            check_benchmarks.validate_config([])
        for field in ("package", "pattern"):
            group = {
                "package": "./internal/tui",
                "pattern": "BenchmarkOne",
                "benchmarks": {
                    "BenchmarkOne": {
                        "max_bytes_per_op": 1,
                        "max_allocs_per_op": 1,
                    }
                },
            }
            group[field] = 123
            config = {
                "version": 1,
                "go_version": "go1.27rc3",
                "groups": [group],
            }
            with self.subTest(field=field):
                with self.assertRaisesRegex(ValueError, "non-empty string"):
                    check_benchmarks.validate_config(config)

    def test_validate_config_rejects_nonpositive_limit(self) -> None:
        config = {
            "version": 1,
            "go_version": "go1.27rc3",
            "groups": [
                {
                    "package": "./internal/tui",
                    "pattern": "BenchmarkOne",
                    "benchmarks": {
                        "BenchmarkOne": {
                            "max_bytes_per_op": 0,
                            "max_allocs_per_op": 1,
                        }
                    },
                }
            ],
        }
        with self.assertRaisesRegex(ValueError, "finite positive max_bytes_per_op"):
            check_benchmarks.validate_config(config)

    def test_validate_config_rejects_nonfinite_and_boolean_limits(self) -> None:
        for value in (float("nan"), float("inf"), True):
            config = {
                "version": 1,
                "go_version": "go1.27rc3",
                "groups": [
                    {
                        "package": "./internal/tui",
                        "pattern": "BenchmarkOne",
                        "benchmarks": {
                            "BenchmarkOne": {
                                "max_bytes_per_op": value,
                                "max_allocs_per_op": 1,
                            }
                        },
                    }
                ],
            }
            with self.subTest(value=value):
                with self.assertRaisesRegex(ValueError, "finite positive"):
                    check_benchmarks.validate_config(config)

    @mock.patch.object(check_benchmarks.subprocess, "run")
    def test_go_version_must_match(self, run: mock.Mock) -> None:
        run.return_value = subprocess.CompletedProcess(
            args=["go"], returncode=0, stdout="go1.28\n"
        )
        with self.assertRaisesRegex(RuntimeError, "expected go1.27rc3"):
            check_benchmarks.check_go_version(Path.cwd(), "go", "go1.27rc3")

    @mock.patch.object(check_benchmarks.subprocess, "run")
    def test_runner_surfaces_subprocess_failure(self, run: mock.Mock) -> None:
        run.return_value = subprocess.CompletedProcess(
            args=["go"], returncode=1, stdout="compile failed"
        )
        with self.assertRaisesRegex(RuntimeError, "compile failed"):
            check_benchmarks.run_benchmark_group(
                Path.cwd(), "go", "./internal/tui", "BenchmarkOne", 3, "1x"
            )

    @mock.patch.object(check_benchmarks.subprocess, "run")
    def test_runner_surfaces_launch_error(self, run: mock.Mock) -> None:
        run.side_effect = OSError("missing go")
        with self.assertRaisesRegex(RuntimeError, "cannot launch benchmark command"):
            check_benchmarks.run_benchmark_group(
                Path.cwd(), "go", "./internal/tui", "BenchmarkOne", 3, "1x"
            )

    @mock.patch.object(check_benchmarks.subprocess, "run")
    def test_runner_surfaces_timeout(self, run: mock.Mock) -> None:
        run.side_effect = subprocess.TimeoutExpired(cmd=["go"], timeout=5)
        with self.assertRaisesRegex(RuntimeError, "timed out after 5s"):
            check_benchmarks.run_benchmark_group(
                Path.cwd(), "go", "./internal/tui", "BenchmarkOne", 3, "1x", 5
            )


if __name__ == "__main__":
    unittest.main()
