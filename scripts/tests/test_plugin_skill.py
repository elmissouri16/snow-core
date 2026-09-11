"""Offline checks for the portable, explicit-invocation plugin-builder skill."""

import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]
SKILL = REPO / "internal/skills/bundled/snow-js-plugin"
SPEC = importlib.util.spec_from_file_location(
    "plugin_skill_sync", SKILL / "scripts/sync_resources.py"
)
SYNC = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SYNC)


class PluginSkillTests(unittest.TestCase):
    def test_bundled_resources_match_canonical_sources(self):
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(SYNC.sync(REPO, SKILL, check=True), 0)
        manifest = json.loads((SKILL / "references/SOURCES.json").read_text())
        self.assertEqual(len(manifest["files"]), len(SYNC.sources(REPO)))
        self.assertTrue(any(item["resource"] == "api/snow.d.ts" for item in manifest["files"]))

    def test_explicit_activation_and_build_deliverables(self):
        text = (SKILL / "SKILL.md").read_text()
        frontmatter = text.split("---", 2)[1]
        self.assertIn("name: snow-js-plugin", frontmatter)
        self.assertIn("Use ONLY when the user explicitly mentions the exact $snow-js-plugin token", frontmatter)
        self.assertNotIn("disable-model-invocation:", frontmatter)
        for required in ("tests/plugin.json", "snow-plugin.json", "main.js", "README.md",
                         "not a security boundary", "Do not claim",
                         "references/CAPABILITIES.md", "references/EXAMPLES.md"):
            self.assertIn(required, text)

    def test_each_example_is_complete_and_portable(self):
        for name in SYNC.EXAMPLES:
            with self.subTest(name=name):
                root = SKILL / "references/examples/plugins" / name
                manifest = json.loads((root / "snow-plugin.json").read_text())
                self.assertEqual(manifest["id"], name)
                self.assertTrue((root / manifest["entry"]).is_file())
                self.assertIn(manifest["api_version"], (1, 2))
        for name in ("agent-profiles", "workflow-guard"):
            root = SKILL / "references/examples/plugins" / name
            for relative in ("src/main.ts", "src/entry.ts", "snow.d.ts",
                             "package.json", "tsconfig.json", "tests/plugin.json"):
                self.assertTrue((root / relative).is_file(), str(root / relative))

    def test_sync_detects_drift_and_preserves_handwritten_resources(self):
        with tempfile.TemporaryDirectory() as temporary:
            skill = Path(temporary)
            references = skill / "references"
            references.mkdir()
            hand = references / "CAPABILITIES.md"
            hand.write_text("handwritten")
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(SYNC.sync(REPO, skill), 0)
                self.assertEqual(SYNC.sync(REPO, skill, check=True), 0)
            (references / "api/snow.d.ts").write_text("outdated")
            with contextlib.redirect_stderr(io.StringIO()):
                self.assertEqual(SYNC.sync(REPO, skill, check=True), 1)
            with contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(SYNC.sync(REPO, skill), 0)
            self.assertEqual(hand.read_text(), "handwritten")
            stale = references / "api/obsolete.md"
            stale.write_text("old API")
            with self.assertRaisesRegex(ValueError, "unexpected resources"):
                SYNC.sync(REPO, skill, check=True)
            self.assertEqual(stale.read_text(), "old API")


if __name__ == "__main__":
    unittest.main()
