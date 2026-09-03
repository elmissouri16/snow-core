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
NAV_SECTION = re.compile(
    r"<section>\s*<h2>([^<]+)</h2>(.*?)</section>", re.DOTALL
)
NAV_LINK = re.compile(
    r'''<a href="\{\{\s*'([^']+)'\s*\|\s*relative_url\s*\}\}"[^>]*>([^<]+)</a>'''
)
PUBLIC_DOCUMENTS = (
    "getting-started.md",
    "providers.md",
    "using-snow.md",
    "configuration.md",
    "sessions.md",
    "plan-mode.md",
    "goals.md",
    "subagents.md",
    "skills.md",
    "mcp.md",
    "plugins.md",
    "security.md",
    "sdk.md",
)
INTERNAL_DOCUMENTS = (
    "README.md",
    "chatgpt-auth.md",
    "rpc.md",
    "user-input.md",
    "plugin-protocol.md",
    "sdk-reference.md",
    "releases.md",
    "pages.md",
    "style-guide.md",
    "performance.md",
    "code-audit.md",
    "codex-plan-mode-and-goals.md",
    "lazy-mcp-implementation-plan.md",
    "plugin-js-python-research.md",
    "subagents-implementation-plan.md",
    "tool-routing.md",
    "tui-performance.md",
    "chatgpt-auth-research.md",
    "session-storage-internals.md",
)
ROOT_INTERNAL_DOCUMENTS = (
    "README.md",
    "SECURITY.md",
    "IMPLEMENTATION.md",
    "AGENTS.md",
    "CHANGELOG.md",
    "bugs.md",
    "LICENSE",
)


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

    def test_builder_stages_only_public_user_documentation(self) -> None:
        result = self._build()

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Prepared GitHub Pages source", result.stdout)
        for relative_path in (
            "_config.yml",
            "_layouts/default.html",
            "_layouts/home.html",
            "_includes/navigation.html",
            "assets/css/style.css",
            "assets/js/copy-code.js",
            "index.md",
            "404.html",
            "examples/index.md",
            "examples/sdk/index.md",
            "pkg/snowsdk/index.md",
            "examples/sdk/go.mod",
        ):
            self.assertTrue((self.output / relative_path).exists(), relative_path)

        staged_docs = sorted(
            path.name for path in (self.output / "docs").glob("*.md")
        )
        self.assertEqual(staged_docs, sorted(PUBLIC_DOCUMENTS))
        for document in PUBLIC_DOCUMENTS:
            staged = (self.output / "docs" / document).read_text(encoding="utf-8")
            self.assertTrue(staged.startswith("---\nlayout: default\n"), document)
        for document in INTERNAL_DOCUMENTS:
            self.assertFalse((self.output / "docs" / document).exists(), document)
        for document in ROOT_INTERNAL_DOCUMENTS:
            self.assertFalse((self.output / document).exists(), document)
        for excluded_path in (
            "benchmarks",
            ".github",
            "sdk/javascript",
            "sdk/python",
            "examples/rpc/javascript",
            "examples/rpc/python",
            "examples/plugins",
            "pkg/protocol/schema/rpc/v1",
        ):
            self.assertFalse((self.output / excluded_path).exists(), excluded_path)

        staged_alias = (self.output / "examples" / "index.md").read_text(
            encoding="utf-8"
        )
        self.assertIn("edit_path: site/examples/index.md", staged_alias)
        layout = (self.output / "_layouts" / "default.html").read_text(
            encoding="utf-8"
        )
        self.assertIn("page.edit_path | default: page.path", layout)
        self.assertIn("blob/main/{{ edit_path }}", layout)
        self.assertIn("'/assets/js/copy-code.js' | relative_url", layout)
        self.assertEqual(
            sorted(path.name for path in (self.output / "examples" / "sdk").iterdir()),
            ["README.md", "go.mod", "go.sum", "index.md", "main.go"],
        )
        self.assertEqual(
            sorted(path.name for path in (self.output / "pkg" / "snowsdk").iterdir()),
            ["index.md"],
        )
        published_scripts = sorted(
            path.relative_to(self.output).as_posix()
            for path in self.output.rglob("*")
            if path.suffix.lower() in {".js", ".mjs", ".cjs", ".py"}
        )
        self.assertEqual(published_scripts, ["assets/js/copy-code.js"])

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

        markdown_files = sorted(self.output.rglob("*.md"))
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

    def test_site_routes_resolve_within_curated_staging(self) -> None:
        result = self._build()
        self.assertEqual(result.returncode, 0, result.stderr)

        failures = []
        for source in sorted(self.output.rglob("*")):
            if not source.is_file() or source.suffix not in {".html", ".md"}:
                continue
            content = source.read_text(encoding="utf-8")
            for raw_target in RELATIVE_URL.findall(content):
                route = raw_target.split("#", 1)[0]
                if route == "/":
                    target = self.output / "index.md"
                elif route.endswith(".html"):
                    target = self.output / f"{route.strip('/')[:-5]}.md"
                elif route.startswith("/assets/"):
                    target = self.output / route.lstrip("/")
                elif route.endswith("/"):
                    target = self.output / route.strip("/") / "index.md"
                else:
                    failures.append(f"{source}: unsupported local route {raw_target}")
                    continue
                if not target.is_file():
                    failures.append(f"{source}: missing staged source for {raw_target}")
        self.assertEqual(failures, [])

    def test_home_and_navigation_are_user_focused(self) -> None:
        navigation = (
            REPOSITORY_ROOT / "site" / "_includes" / "navigation.html"
        ).read_text(encoding="utf-8")
        expected_navigation = (
            (
                "Start",
                (
                    ("Overview", "/"),
                    ("Install and first prompt", "/docs/getting-started.html"),
                    ("Providers", "/docs/providers.html"),
                    ("Using Snow", "/docs/using-snow.html"),
                ),
            ),
            (
                "Workflows",
                (
                    ("Sessions and branches", "/docs/sessions.html"),
                    ("Plan Mode", "/docs/plan-mode.html"),
                    ("Thread Goals", "/docs/goals.html"),
                    ("Subagents", "/docs/subagents.html"),
                ),
            ),
            (
                "Add capabilities",
                (
                    ("Agent Skills", "/docs/skills.html"),
                    ("MCP", "/docs/mcp.html"),
                    ("Plugins", "/docs/plugins.html"),
                ),
            ),
            (
                "Reference",
                (
                    ("Configuration", "/docs/configuration.html"),
                    ("Go SDK", "/docs/sdk.html"),
                    ("Security model", "/docs/security.html"),
                ),
            ),
        )
        actual_navigation = tuple(
            (
                heading,
                tuple((label, route) for route, label in NAV_LINK.findall(body)),
            )
            for heading, body in NAV_SECTION.findall(navigation)
        )
        self.assertEqual(actual_navigation, expected_navigation)
        self.assertNotIn("Releases", navigation)
        self.assertNotIn("All documentation", navigation)

        home_layout = (
            REPOSITORY_ROOT / "site" / "_layouts" / "home.html"
        ).read_text(encoding="utf-8")
        homepage = (REPOSITORY_ROOT / "site" / "index.md").read_text(
            encoding="utf-8"
        )
        self.assertIn("/docs/getting-started.html", home_layout)
        self.assertIn("/docs/getting-started.html", homepage)
        self.assertIn("Advanced references on GitHub", homepage)
        self.assertIn(
            "https://github.com/elmissouri16/snow-core/blob/main/docs/README.md",
            homepage,
        )
        for internal_copy in (
            "Contributors",
            "complete documentation index",
            "Prepare and verify a release",
            "one streaming agent loop",
            "Current source and tests",
        ):
            self.assertNotIn(internal_copy, homepage)
        self.assertNotIn("| Goal | Guide |", homepage)
        self.assertNotIn("ChatGPT authentication", navigation)
        self.assertNotIn("JSONL RPC", navigation)
        self.assertNotIn("Plugin protocol", navigation)

    def test_public_setup_guides_cover_required_tasks(self) -> None:
        providers = (REPOSITORY_ROOT / "docs" / "providers.md").read_text(
            encoding="utf-8"
        )
        for provider in (
            "opencode-zen",
            "opencode-go",
            "chatgpt",
            "openai-compatible",
        ):
            self.assertIn(provider, providers)
        for command in (
            "snow --provider opencode-zen",
            "snow login opencode-go",
            "snow login chatgpt",
            "snow login openai-compatible",
            "snow --provider my-provider",
        ):
            self.assertIn(command, providers)

        skills = (REPOSITORY_ROOT / "docs" / "skills.md").read_text(
            encoding="utf-8"
        )
        for setup_detail in (
            "~/.agents/skills/",
            "snow skills list",
            "$pdf-processing",
            "snow skills disable",
            "project trust",
        ):
            self.assertIn(setup_detail, skills)

        mcp = (REPOSITORY_ROOT / "docs" / "mcp.md").read_text(encoding="utf-8")
        for setup_detail in (
            "snow mcp add local-tools",
            "snow mcp add remote-tools",
            "--bearer-token-env",
            "--project",
            "snow mcp check",
            "snow mcp disable",
            "snow mcp remove",
        ):
            self.assertIn(setup_detail, mcp)

        stylesheet = (
            REPOSITORY_ROOT / "site" / "assets" / "css" / "style.css"
        ).read_text(encoding="utf-8")
        print_block = stylesheet[stylesheet.index("@media print") :]
        self.assertRegex(
            print_block,
            r"\.prose h1,\s*\.prose h2,\s*\.prose h3,\s*\.prose h4,"
            r"\s*\.prose strong,\s*\.prose th\s*\{\s*color: #111;\s*\}",
        )
        for readable_selector in (
            ".prose blockquote p",
            ".prose a",
            ".prose code",
            ".prose pre code",
        ):
            self.assertIn(readable_selector, print_block)

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
