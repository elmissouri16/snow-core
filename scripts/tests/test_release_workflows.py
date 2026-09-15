#!/usr/bin/env python3
"""Regression tests for the CI and alpha-release workflow contracts."""

from __future__ import annotations

import json
import pathlib
import re
import subprocess
import tempfile
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

    def test_ci_requires_reproducible_frontend_without_changing_go_jobs(self) -> None:
        workflow = CI_WORKFLOW.read_text(encoding="utf-8")
        frontend = workflow[workflow.index("  frontend:\n"):workflow.index("  test:\n")]
        for expected in (
            "working-directory: internal/web/frontend",
            "node-version: '24.16.0'",
            "cache-dependency-path: internal/web/frontend/package-lock.json",
            "npm ci --ignore-scripts",
            "npm run typecheck",
            "run: npm test\n",
            "npm run check",
        ):
            self.assertIn(expected, frontend)
        self.assertNotIn("continue-on-error:", frontend)
        self.assertNotIn("if:", frontend)
        self.assertNotIn("|| true", frontend)
        unit = step_block(frontend, "Test frontend source and asset verification")
        self.assertIn("run: npm test\n", unit)
        package = json.loads((REPOSITORY_ROOT / "internal/web/frontend/package.json").read_text(encoding="utf-8"))
        self.assertIn("scripts/assets.test.mjs", package["scripts"]["test"])
        self.assertIn("src/", package["scripts"]["test"])
        references = ACTION_REFERENCE.findall(workflow)
        self.assertTrue(all(re.fullmatch(r"[0-9a-f]{40}", ref) for ref in references))
        go_jobs = workflow[workflow.index("  test:\n"):]
        self.assertNotIn("npm ", go_jobs)
        self.assertNotIn("setup-node@", go_jobs)
        self.assertNotIn("npm ", RELEASE_WORKFLOW.read_text(encoding="utf-8"))
        self.assertTrue((REPOSITORY_ROOT / "internal/web/frontend/scripts/assets.test.mjs").is_file())

    def test_frontend_native_gate_requires_browser_and_production_go_fixtures(self) -> None:
        workflow = CI_WORKFLOW.read_text(encoding="utf-8")
        frontend = workflow[workflow.index("  frontend:\n"):workflow.index("  test:\n")]
        setup = step_block(frontend, "Set up Go for native React fixtures")
        self.assertIn("uses: actions/setup-go@", setup)
        self.assertIn("go-version: '1.27.0-rc.3'", setup)
        native = step_block(frontend, "Test native React pages")
        for expected in (
            "working-directory: .\n",
            "shell: bash",
            "set -euo pipefail",
            "command -v google-chrome",
            "command -v chromium",
            "Chrome/Chromium is required",
            "exit 1",
            'test -x "$chrome"',
            'export SNOW_CHROME_BIN="$chrome"',
            '"$SNOW_CHROME_BIN" --version',
            "node scripts/tests/browser/react-pages/run.mjs",
        ):
            self.assertIn(expected, native)
        self.assertNotIn("continue-on-error:", native)
        self.assertNotIn("if:", native)
        self.assertNotIn("|| true", native)
        self.assertLess(frontend.index("run: npm run check"), frontend.index("name: Test native React pages"))
        self.assertTrue((REPOSITORY_ROOT / "scripts/tests/browser/react-pages/run.mjs").is_file())

    def test_release_license_includes_all_shipped_web_notices_and_is_bounded(self) -> None:
        workflow = RELEASE_WORKFLOW.read_text(encoding="utf-8")
        build = step_block(workflow, "Build release binary")
        sources = (
            "internal/web/static/HARNESS-NOTICE.txt",
            "internal/web/static/vendor/htmx-LICENSE",
            "internal/web/static/generated/THIRD-PARTY-NOTICES.txt",
        )
        for source in sources:
            self.assertIn(source, build)
        self.assertIn('cp LICENSE README.md "dist/$root/"', build)
        self.assertIn('-le 1048576', build)
        # Exercise the actual workflow shell, not a duplicate implementation.
        start = build.index('          cp LICENSE README.md')
        end = build.index('          tar --sort=name', start)
        script = "\n".join(line[10:] for line in build[start:end].splitlines())
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary)
            original = b"Snow root copyright and license\n"
            (root / "LICENSE").write_bytes(original)
            (root / "README.md").write_text("README\n", encoding="utf-8")
            output = root / "dist" / "release"
            output.mkdir(parents=True)
            for index, source in enumerate(sources):
                path = root / source
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(f"Full license fixture {index}\n", encoding="utf-8")

            def run_packaging() -> subprocess.CompletedProcess[str]:
                return subprocess.run(
                    ["bash", "-euo", "pipefail", "-c", 'root=release\n' + script],
                    cwd=root, capture_output=True, text=True, timeout=10,
                )

            result = run_packaging()
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((root / "LICENSE").read_bytes(), original)
            license_text = (output / "LICENSE").read_text(encoding="utf-8")
            self.assertTrue(license_text.startswith(original.decode()))
            for index, source in enumerate(sources):
                self.assertIn(pathlib.Path(source).name, license_text)
                self.assertIn(f"Full license fixture {index}\n", license_text)
            # The binary is created by the preceding Go build; no fourth
            # archive member is introduced for notices.
            self.assertEqual(sorted(path.name for path in output.iterdir()), ["LICENSE", "README.md"])
            (root / sources[-1]).write_bytes(b"x" * 1048576)
            result = run_packaging()
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("1 MiB limit", result.stderr)
            self.assertEqual((root / "LICENSE").read_bytes(), original)
            (root / sources[-1]).unlink()
            self.assertNotEqual(run_packaging().returncode, 0)

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
