from __future__ import annotations

import re
import subprocess
import tempfile
import unittest
from pathlib import Path
from urllib.parse import unquote


REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
BUILDER = REPOSITORY_ROOT / "scripts" / "build-pages.sh"
WORKFLOW = REPOSITORY_ROOT / ".github" / "workflows" / "pages.yml"
CI_WORKFLOW = REPOSITORY_ROOT / ".github" / "workflows" / "ci.yml"
MARKDOWN_LINK = re.compile(r"!?\[[^\]]*\]\(([^)]+)\)")
ACTION_REFERENCE = re.compile(r"uses:\s+actions/[^@\s]+@([^\s]+)")
RELATIVE_URL = re.compile(r"\{\{\s*'([^']+)'\s*\|\s*relative_url\s*\}\}")


class PagesBuildTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.root = Path(self.temp_dir.name)
        self.output = self.root / "pages source"

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def _build(self) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["sh", str(BUILDER), str(self.output)],
            cwd=REPOSITORY_ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
            timeout=30,
        )

    def test_builder_stages_site_and_canonical_documentation(self) -> None:
        result = self._build()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Prepared GitHub Pages source", result.stdout)
        for relative_path in (
            "_config.yml",
            "_layouts/default.html",
            "_layouts/home.html",
            "_includes/navigation.html",
            "assets/css/style.css",
            "index.md",
            "404.html",
            "README.md",
            "SECURITY.md",
            "IMPLEMENTATION.md",
            "AGENTS.md",
            "CHANGELOG.md",
            "docs/README.md",
            "docs/using-snow.md",
            "docs/configuration.md",
            "docs/security.md",
            "docs/sdk.md",
            "docs/rpc.md",
            "examples/sdk/go.mod",
            "pkg/protocol/schema/rpc/v1/agent-event.schema.json",
            "benchmarks/performance-limits.json",
            ".github/workflows/ci.yml",
        ):
            self.assertTrue((self.output / relative_path).exists(), relative_path)

        staged_readme = (self.output / "docs" / "README.md").read_text(
            encoding="utf-8"
        )
        self.assertTrue(staged_readme.startswith("---\nlayout: default\n"))
        self.assertIn('title: "Snow documentation"', staged_readme)
        staged_alias = (self.output / "examples" / "index.md").read_text(
            encoding="utf-8"
        )
        self.assertIn("edit_path: site/examples/index.md", staged_alias)
        layout = (self.output / "_layouts" / "default.html").read_text(
            encoding="utf-8"
        )
        self.assertIn("page.edit_path | default: page.path", layout)
        self.assertIn("blob/main/{{ edit_path }}", layout)
        self.assertEqual(
            len(list((self.output / "docs").glob("*.md"))),
            len(list((REPOSITORY_ROOT / "docs").glob("*.md"))),
        )
        self.assertFalse((self.output / "examples" / "plugins").exists())
        self.assertEqual(
            sorted(path.name for path in (self.output / "examples" / "sdk").iterdir()),
            ["README.md", "go.mod", "go.sum", "index.md", "main.go"],
        )
        self.assertEqual(
            sorted(path.name for path in (self.output / "pkg" / "snowsdk").iterdir()),
            ["index.md"],
        )
        for excluded_path in (
            "sdk/javascript",
            "sdk/python",
            "examples/rpc/javascript",
            "examples/rpc/python",
            "examples/plugins",
        ):
            self.assertFalse((self.output / excluded_path).exists(), excluded_path)
        published_scripts = [
            path
            for path in self.output.rglob("*")
            if path.suffix.lower() in {".js", ".mjs", ".cjs", ".py"}
        ]
        self.assertEqual(published_scripts, [])

    def test_builder_refuses_to_replace_an_existing_output(self) -> None:
        self.output.mkdir()
        marker = self.output / "keep"
        marker.write_text("unchanged\n", encoding="utf-8")

        result = self._build()

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("output already exists", result.stderr)
        self.assertEqual(marker.read_text(encoding="utf-8"), "unchanged\n")

    def test_staged_repository_document_links_resolve(self) -> None:
        result = self._build()
        self.assertEqual(result.returncode, 0, result.stderr)

        markdown_files = [self.output / "README.md"]
        markdown_files.extend(sorted((self.output / "docs").glob("*.md")))
        failures = []
        for markdown_file in markdown_files:
            content = markdown_file.read_text(encoding="utf-8")
            for match in MARKDOWN_LINK.finditer(content):
                raw_target = match.group(1).strip().strip("<>")
                if (
                    not raw_target
                    or raw_target.startswith(("#", "http://", "https://", "mailto:"))
                    or "{{" in raw_target
                ):
                    continue
                target_without_anchor = raw_target.split("#", 1)[0]
                target_without_query = target_without_anchor.split("?", 1)[0]
                target_path = unquote(target_without_query)
                if not target_path:
                    continue
                resolved = (markdown_file.parent / target_path).resolve()
                try:
                    resolved.relative_to(self.output.resolve())
                except ValueError:
                    failures.append(f"{markdown_file}: escapes site: {raw_target}")
                    continue
                if not resolved.exists():
                    failures.append(f"{markdown_file}: missing {raw_target}")
        self.assertEqual(failures, [])

    def test_site_navigation_routes_match_jekyll_outputs(self) -> None:
        failures = []
        for source in sorted((REPOSITORY_ROOT / "site").rglob("*")):
            if not source.is_file() or source.suffix not in {".html", ".md"}:
                continue
            content = source.read_text(encoding="utf-8")
            for raw_target in RELATIVE_URL.findall(content):
                route = raw_target.split("#", 1)[0]
                if route == "/":
                    target = REPOSITORY_ROOT / "site" / "index.md"
                elif route == "/README.html":
                    target = REPOSITORY_ROOT / "README.md"
                elif route.startswith("/docs/") and route.endswith(".html"):
                    target = REPOSITORY_ROOT / f"{route.strip('/')[:-5]}.md"
                elif route.endswith(".html"):
                    target = REPOSITORY_ROOT / f"{route.strip('/')[:-5]}.md"
                elif route.startswith("/assets/"):
                    target = REPOSITORY_ROOT / "site" / route.lstrip("/")
                elif route.endswith("/"):
                    target = (
                        REPOSITORY_ROOT
                        / "site"
                        / route.strip("/")
                        / "index.md"
                    )
                else:
                    failures.append(f"{source}: unsupported local route {raw_target}")
                    continue
                if not target.is_file():
                    failures.append(f"{source}: missing source for {raw_target}")
        self.assertEqual(failures, [])

    def test_pages_workflow_pins_official_actions_and_deploys_artifact(self) -> None:
        workflow = WORKFLOW.read_text(encoding="utf-8")
        references = ACTION_REFERENCE.findall(workflow)

        self.assertEqual(len(references), 6)
        self.assertTrue(all(re.fullmatch(r"[0-9a-f]{40}", ref) for ref in references))
        deploy_index = workflow.index("  deploy:")
        self.assertGreater(workflow.index("pages: write"), deploy_index)
        self.assertGreater(workflow.index("id-token: write"), deploy_index)
        self.assertGreater(workflow.index("actions/configure-pages@"), deploy_index)
        self.assertIn("environment:\n      name: github-pages", workflow)
        self.assertIn("- '.github/workflows/ci.yml'", workflow)
        self.assertIn("- 'LICENSE'", workflow)
        self.assertIn("./scripts/build-pages.sh", workflow)
        self.assertIn("scripts/check-pages-output.py ./_site", workflow)
        self.assertIn("actions/jekyll-build-pages@", workflow)
        self.assertIn("actions/upload-pages-artifact@", workflow)
        self.assertIn("actions/deploy-pages@", workflow)

    def test_ci_builds_and_validates_rendered_pages(self) -> None:
        workflow = CI_WORKFLOW.read_text(encoding="utf-8")
        self.assertIn("name: Documentation build (Linux)", workflow)
        self.assertIn("./scripts/build-pages.sh", workflow)
        self.assertIn("actions/jekyll-build-pages@", workflow)
        self.assertIn("scripts/check-pages-output.py ./_site", workflow)


if __name__ == "__main__":
    unittest.main()
