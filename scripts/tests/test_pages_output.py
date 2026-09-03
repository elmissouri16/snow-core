from __future__ import annotations

import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / "check-pages-output.py"
SPEC = importlib.util.spec_from_file_location("check_pages_output", SCRIPT)
assert SPEC and SPEC.loader
check_pages_output = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = check_pages_output
SPEC.loader.exec_module(check_pages_output)


class RenderedPagesValidationTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.site = Path(self.temp_dir.name)

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def _write(self, relative_path: str, content: str) -> None:
        path = self.site / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def test_accepts_internal_pages_assets_and_fragments(self) -> None:
        self._write(
            "index.html",
            """<!doctype html>
<html><body>
<a href="/snow-core/docs/guide.html#start">Guide</a>
<a href="docs/topic/">Topic</a>
<link href="/snow-core/assets/style.css" rel="stylesheet">
</body></html>
""",
        )
        self._write(
            "docs/guide.html",
            '<!doctype html><html><body><h1 id="start">Guide</h1></body></html>',
        )
        self._write(
            "docs/topic/index.html",
            '<!doctype html><html><body><a href="../guide.html">Guide</a></body></html>',
        )
        self._write("assets/style.css", "body {}\n")

        failures = check_pages_output.validate_site(self.site, "/snow-core")

        self.assertEqual(failures, [])

    def test_accepts_curated_public_documentation_routes(self) -> None:
        public_guides = (
            "getting-started",
            "providers",
            "using-snow",
            "configuration",
            "sessions",
            "plan-mode",
            "goals",
            "subagents",
            "skills",
            "mcp",
            "plugins",
            "security",
            "sdk",
        )
        guide_links = "".join(
            f'<a href="/snow-core/docs/{guide}.html">{guide}</a>'
            for guide in public_guides
        )
        self._write(
            "index.html",
            f"<!doctype html><html><body>{guide_links}"
            '<a href="/snow-core/examples/sdk/">Example</a>'
            '<a href="/snow-core/pkg/snowsdk/">SDK package</a>'
            "</body></html>",
        )
        for guide in public_guides:
            self._write(
                f"docs/{guide}.html",
                f"<!doctype html><html><body><h1>{guide}</h1></body></html>",
            )
        for route in (
            "examples/sdk/index.html",
            "pkg/snowsdk/index.html",
        ):
            self._write(
                route,
                '<!doctype html><html><body><a href="/snow-core/">Home</a></body></html>',
            )

        failures = check_pages_output.validate_site(self.site, "/snow-core")

        self.assertEqual(failures, [])

    def test_rejects_internal_repository_artifact_paths(self) -> None:
        for relative_path in sorted(check_pages_output.FORBIDDEN_PUBLIC_PATHS):
            self._write(relative_path, "repository-only\n")
        for relative_path in (
            ".github/workflows/pages.yml",
            "benchmarks/performance-limits.json",
            "design-plans/site-redesign.md",
            "pkg/protocol/schema/rpc/v1/agent-event.schema.json",
        ):
            self._write(relative_path, "repository-only\n")

        failures = check_pages_output.validate_site(self.site, "/snow-core")

        expected_paths = sorted(check_pages_output.FORBIDDEN_PUBLIC_PATHS) + [
            ".github/workflows/pages.yml",
            "benchmarks/performance-limits.json",
            "design-plans/site-redesign.md",
            "pkg/protocol/schema/rpc/v1/agent-event.schema.json",
        ]
        for relative_path in expected_paths:
            self.assertIn(
                f"forbidden public artifact path: {relative_path}", failures
            )

    def test_reports_broken_links_fragments_base_escapes_and_schemes(self) -> None:
        self._write(
            "index.html",
            """<!doctype html>
<html><body>
<a href="/snow-core/missing.html">Missing</a>
<a href="/snow-core/target.html#absent">Fragment</a>
<a href="/other/site.html">Absolute escape</a>
<a href="../escape.html">Relative escape</a>
<a href="javascript:alert(1)">Unsafe</a>
<a href="javascript:alert(2)" href="/snow-core/">Duplicate unsafe</a>
</body></html>
""",
        )
        self._write(
            "target.html",
            '<!doctype html><html><body><h1 id="present">Target</h1></body></html>',
        )

        failures = check_pages_output.validate_site(self.site, "/snow-core")

        self.assertTrue(any("broken href" in failure for failure in failures))
        self.assertTrue(any("missing fragment" in failure for failure in failures))
        self.assertTrue(any("local href escapes base path" in failure for failure in failures))
        self.assertTrue(any("relative href escapes base path" in failure for failure in failures))
        self.assertGreaterEqual(
            sum("forbidden href scheme" in failure for failure in failures), 2
        )

    def test_rejects_unbounded_html_and_file_counts(self) -> None:
        self._write("index.html", "x" * 32)
        with mock.patch.object(check_pages_output, "MAX_HTML_BYTES", 16):
            failures = check_pages_output.validate_site(self.site, "/snow-core")
        self.assertTrue(any("exceeds HTML limit" in failure for failure in failures))

        self._write("asset.txt", "asset\n")
        with mock.patch.object(check_pages_output, "MAX_FILES", 1):
            failures = check_pages_output.validate_site(self.site, "/snow-core")
        self.assertEqual(len(failures), 1)
        self.assertIn("site contains 2 files", failures[0])

        with mock.patch.object(check_pages_output, "MAX_FILES", 10), mock.patch.object(
            check_pages_output, "MAX_TOTAL_BYTES", 1
        ):
            failures = check_pages_output.validate_site(self.site, "/snow-core")
        self.assertEqual(len(failures), 1)
        self.assertIn("bytes; limit is 1", failures[0])

    def test_rejects_reference_and_fragment_bounds(self) -> None:
        self._write(
            "index.html",
            """<!doctype html><html><body>
<a href="/snow-core/">One</a><a href="/snow-core/">Two</a>
<h1 id="one">One</h1><h2 id="two">Two</h2>
</body></html>
""",
        )

        with mock.patch.object(check_pages_output, "MAX_PAGE_REFERENCES", 1):
            failures = check_pages_output.validate_site(self.site, "/snow-core")
        self.assertTrue(any("references; limit is 1" in failure for failure in failures))

        with mock.patch.object(check_pages_output, "MAX_PAGE_FRAGMENTS", 1):
            failures = check_pages_output.validate_site(self.site, "/snow-core")
        self.assertTrue(any("fragments; limit is 1" in failure for failure in failures))

        with mock.patch.object(check_pages_output, "MAX_TOTAL_REFERENCES", 1):
            failures = check_pages_output.validate_site(self.site, "/snow-core")
        self.assertTrue(any("retained references" in failure for failure in failures))

        with mock.patch.object(check_pages_output, "MAX_TOTAL_FRAGMENTS", 1):
            failures = check_pages_output.validate_site(self.site, "/snow-core")
        self.assertTrue(any("retained fragments" in failure for failure in failures))

    def test_rejects_missing_site_directory(self) -> None:
        failures = check_pages_output.validate_site(
            self.site / "missing", "/snow-core"
        )

        self.assertEqual(len(failures), 1)
        self.assertIn("site directory does not exist", failures[0])


if __name__ == "__main__":
    unittest.main()
