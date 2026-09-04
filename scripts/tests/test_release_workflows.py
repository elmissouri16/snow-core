#!/usr/bin/env python3
"""Regression tests for the CI and alpha-release workflow contracts."""

from __future__ import annotations

import pathlib
import re
import unittest


REPOSITORY_ROOT = pathlib.Path(__file__).resolve().parents[2]
CI_WORKFLOW = REPOSITORY_ROOT / ".github" / "workflows" / "ci.yml"
RELEASE_WORKFLOW = REPOSITORY_ROOT / ".github" / "workflows" / "release-alpha.yml"
ACTION_REFERENCE = re.compile(r"uses:\s+[^@\s]+@([^\s#]+)")


def step_block(workflow: str, name: str) -> str:
    marker = f"      - name: {name}\n"
    start = workflow.index(marker)
    end = workflow.find("\n      - name:", start + len(marker))
    if end == -1:
        end = len(workflow)
    return workflow[start:end]


class ReleaseWorkflowTests(unittest.TestCase):
    def test_release_requires_exact_successful_main_push_checks(self) -> None:
        workflow = RELEASE_WORKFLOW.read_text(encoding="utf-8")

        self.assertNotIn("uses: ./.github/workflows/ci.yml", workflow)
        self.assertIn("name: Validate alpha tag and CI provenance", workflow)
        self.assertIn("actions: read", workflow)
        self.assertIn('git rev-parse "refs/tags/$tag^{commit}"', workflow)
        self.assertIn(
            'git merge-base --is-ancestor "$commit" origin/main', workflow
        )

        provenance = step_block(workflow, "Require successful main checks")
        for expected in (
            'require_successful_run ci.yml CI',
            'require_successful_run pages.yml Documentation',
            '--workflow "$workflow"',
            '--commit "$COMMIT"',
            '--event push',
            '.headBranch == "main"',
            '.headSha == "',
            '.status == "completed"',
            '.conclusion == "success"',
            'if [[ -z "$successful_runs" ]]',
        ):
            self.assertIn(expected, provenance)

    def test_release_preserves_validation_and_narrow_write_permission(self) -> None:
        workflow = RELEASE_WORKFLOW.read_text(encoding="utf-8")

        for expected in (
            "alpha releases require an annotated tag",
            "CHANGELOG.md must contain a dated $version heading",
            "release tags must not contain project-local .snow state",
            "- metadata\n    runs-on: ubuntu-latest",
            "Smoke-test Linux amd64 release binary",
            "Generate and verify checksums",
            "Publish GitHub prerelease",
        ):
            self.assertIn(expected, workflow)

        self.assertEqual(workflow.count("contents: write"), 1)
        self.assertGreater(
            workflow.index("contents: write"),
            workflow.index("  release:\n"),
        )

        references = ACTION_REFERENCE.findall(workflow)
        self.assertTrue(references)
        self.assertTrue(all(re.fullmatch(r"[0-9a-f]{40}", ref) for ref in references))

    def test_ci_keeps_cross_platform_tests_but_focuses_macos(self) -> None:
        workflow = CI_WORKFLOW.read_text(encoding="utf-8")

        self.assertNotIn("workflow_call:", workflow)
        self.assertIn("- ubuntu-latest", workflow)
        self.assertIn("- macos-latest", workflow)
        self.assertNotIn("name: Documentation build (Linux)", workflow)

        self.assertNotIn("if:", step_block(workflow, "Test"))
        self.assertNotIn(
            "if:", step_block(workflow, "Test standalone Go SDK example")
        )
        self.assertIn(
            "if: runner.os == 'macOS'",
            step_block(workflow, "Test installer on macOS"),
        )
        self.assertIn(
            "-p 'test_install.py'", step_block(workflow, "Test installer on macOS")
        )
        self.assertIn(
            "-p 'test_check_benchmarks.py'",
            step_block(workflow, "Test benchmark guard"),
        )

        for name in (
            "Check formatting",
            "Vet",
            "Test support scripts",
            "Build snow",
            "Smoke-test snow",
            "Run standalone Go SDK example",
        ):
            with self.subTest(step=name):
                self.assertIn("if: runner.os == 'Linux'", step_block(workflow, name))

        for required_job in (
            "Performance regression guard (Linux)",
            "Race detector (Linux)",
            "Cross-build (${{ matrix.goos }}/${{ matrix.goarch }})",
            "Vulnerability scan",
        ):
            self.assertIn(required_job, workflow)


if __name__ == "__main__":
    unittest.main()
