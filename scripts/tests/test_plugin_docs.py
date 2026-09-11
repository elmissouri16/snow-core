"""Offline checks for deferred plugin-documentation resources and maintenance."""

import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]
BUNDLE = REPO / "internal/plugindocs"
RESOURCES = BUNDLE / "resources"
SPEC = importlib.util.spec_from_file_location(
    "plugin_docs_sync", BUNDLE / "scripts/sync_resources.py"
)
SYNC = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SYNC)


class PluginDocsTests(unittest.TestCase):
    def test_bundled_resources_match_canonical_sources(self):
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(SYNC.sync(REPO, BUNDLE, check=True), 0)
        manifest = json.loads((RESOURCES / "SOURCES.json").read_text())
        self.assertEqual(len(manifest["files"]), 66)
        self.assertEqual(len(manifest["files"]), len(SYNC.sources(REPO)))
        self.assertEqual(len({item["resource"] for item in manifest["files"]}), 66)
        for item in manifest["files"]:
            with self.subTest(resource=item["resource"]):
                content = (RESOURCES / item["resource"]).read_bytes()
                self.assertEqual(content, (REPO / item["source"]).read_bytes())
                self.assertEqual(hashlib.sha256(content).hexdigest(), item["sha256"])
                self.assertLessEqual(len(content), SYNC.MAX_FILE_BYTES)
                content.decode("utf-8")
        self.assertIn("api/snow.d.ts", {item["resource"] for item in manifest["files"]})

    def test_guide_is_tool_resource_not_skill(self):
        text = (RESOURCES / "GUIDE.md").read_text()
        self.assertTrue(text.startswith("# Build and update Snow JavaScript plugins"))
        for obsolete in ("$snow-js-plugin", "activate_skill", "read_skill_resource",
                         "builtin:", "references/", "## Activation", "name: snow-js-plugin"):
            self.assertNotIn(obsolete, text)
        self.assertFalse((BUNDLE / "SKILL.md").exists())
        self.assertFalse((RESOURCES / "SKILL.md").exists())
        self.assertFalse((REPO / "internal/skills/bundled/snow-js-plugin").exists())
        for required in ("snow_plugin_docs", "search_tools", "`overview`", "`list`",
                         "`search`", "`read`", "`plugins`", "`plugin_id`", "`offset`",
                         "`limit`", "1-based line", "not OS paths"):
            self.assertIn(required, text)
        for relative in ("GUIDE.md", "CAPABILITIES.md", "EXAMPLES.md", "api/snow.d.ts",
                         "api/fixtures.md", "docs/plugins.md", "docs/plugin-extensions.md",
                         "docs/plugin-workflows.md", "examples/plugins/workspace-notes/main.js"):
            self.assertTrue((RESOURCES / relative).is_file(), relative)
        self.assertFalse((RESOURCES / "scripts").exists())

    def test_creation_workflow_retains_build_test_security_deliverables(self):
        text = " ".join((RESOURCES / "GUIDE.md").read_text().split())
        for required in ("tests/plugin.json", "snow-plugin.json", "main.js", "README.md",
                         "snow plugin init", "--typescript", "snow plugin test",
                         "actual generated entry", "simulated host", "host_tools",
                         "handler `uses`", "Await host calls serially", "workflow.update",
                         "Hooks are pure and bounded", "permission denials",
                         "OS sandbox", "Goja heap quota", "Subagents incur provider usage",
                         "persistent registration", "/plugins reload", "busy refusal",
                         "**built**", "**validated**", "**registered**", "**loaded**",
                         "Never claim a command passed unless it ran successfully"):
            self.assertIn(required, text)

    def test_existing_plugin_workflow_preserves_contract_and_authority(self):
        text = (RESOURCES / "GUIDE.md").read_text()
        update = " ".join(text.split("## Update an existing plugin safely", 1)[1]
                          .split("## Build workflow", 1)[0].split())
        for required in ("runtime registration metadata", "plugin_id", "loaded inventory",
                         "Offline bundled examples", "never executes disabled paths",
                         "ordinary rooted", "never bypass it with Bash",
                         "Do not scaffold over an existing package", "Preserve the plugin ID",
                         "command/tool IDs", "settings/configuration keys", "storage/workflow keys",
                         "Do not reset real-user state", "compatible migration", "api_version",
                         "plugin version", "host_tools", "per-handler `uses`", "typecheck/build",
                         "fixtures", "requested behavior", "regressions", "simulated host",
                         "Do not silently enable", "reload", "user's authority"):
            self.assertIn(required, update)

    def test_policy_is_tool_allowlist_not_skill_migration(self):
        for path in (BUNDLE / "README.md", REPO / "docs/plugins.md", REPO / "docs/skills.md",
                     REPO / "docs/tool-routing.md", REPO / "IMPLEMENTATION.md"):
            with self.subTest(path=path):
                text = " ".join(path.read_text().split())
                for required in ("snow_plugin_docs", "skills.overrides.snow-js-plugin",
                                 "inert unless", "same-named", "--no-skills", "--no-plugins"):
                    self.assertIn(required, text)
                self.assertIn("allowlist", text.lower())
                self.assertIn("migration", text)

    def test_each_example_is_complete_and_portable(self):
        self.assertEqual(len(SYNC.EXAMPLES), 14)
        for name in SYNC.EXAMPLES:
            with self.subTest(name=name):
                root = RESOURCES / "examples/plugins" / name
                manifest = json.loads((root / "snow-plugin.json").read_text())
                self.assertEqual(manifest["id"], name)
                self.assertTrue((root / manifest["entry"]).is_file())
                self.assertIn(manifest["api_version"], (1, 2))
        for name in ("agent-profiles", "workflow-guard"):
            root = RESOURCES / "examples/plugins" / name
            for relative in ("src/main.ts", "src/entry.ts", "snow.d.ts",
                             "package.json", "tsconfig.json", "tests/plugin.json"):
                self.assertTrue((root / relative).is_file(), str(root / relative))
        for name in ("CAPABILITIES.md", "EXAMPLES.md"):
            self.assertNotIn("references/", (RESOURCES / name).read_text())

    def test_sync_detects_drift_and_preserves_handwritten_resources(self):
        with tempfile.TemporaryDirectory() as temporary:
            bundle = Path(temporary)
            resources = bundle / "resources"
            resources.mkdir()
            for name in ("GUIDE.md", "CAPABILITIES.md", "EXAMPLES.md"):
                (resources / name).write_text("handwritten " + name)
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(SYNC.sync(REPO, bundle), 0)
                self.assertEqual(SYNC.sync(REPO, bundle, check=True), 0)
            (resources / "api/snow.d.ts").write_text("outdated")
            with contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(SYNC.sync(REPO, bundle, check=True), 1)
            self.assertEqual((resources / "api/snow.d.ts").read_text(), "outdated")
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(SYNC.sync(REPO, bundle), 0)
            for name in ("GUIDE.md", "CAPABILITIES.md", "EXAMPLES.md"):
                self.assertEqual((resources / name).read_text(), "handwritten " + name)
            stale = resources / "api/obsolete.md"
            stale.write_text("old API")
            with self.assertRaisesRegex(ValueError, "unexpected resources"):
                SYNC.sync(REPO, bundle, check=True)
            self.assertEqual(stale.read_text(), "old API")

    def test_relocated_script_discovers_checkout_independent_of_cwd(self):
        with tempfile.TemporaryDirectory() as temporary:
            result = subprocess.run(
                [sys.executable, str(BUNDLE / "scripts/sync_resources.py"), "--check"],
                cwd=temporary, capture_output=True, text=True, timeout=30,
            )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("Verified 66 resources; 0 changed.", result.stdout)


if __name__ == "__main__":
    unittest.main()
