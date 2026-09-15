"""Keep fixture/export failures visible when writing first-run browser reports."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


class HarnessLayoutErrorTests(unittest.TestCase):
    def test_new_output_directory_preserves_manifest_error(self):
        node = shutil.which("node")
        if not node:
            self.skipTest("Node 22+ is unavailable")
        version = subprocess.run([node, "--version"], capture_output=True, text=True, timeout=10, check=True)
        if int(version.stdout.strip().lstrip("v").split(".")[0]) < 22:
            self.skipTest("Node 22+ is unavailable")
        repository = Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory(prefix="snow-layout-error-") as directory:
            root = Path(directory)
            fixtures = root / "fixtures"
            fixtures.mkdir()
            (fixtures / "fixtures.json").write_text("[]", encoding="utf-8")
            evidence = root / "new-evidence"
            # Binary discovery precedes manifest checking, but no browser is
            # launched on this error path. This fixture needs no Chrome/network.
            environment = dict(os.environ, SNOW_CHROME_BIN=sys.executable)
            result = subprocess.run(
                [node, "scripts/tests/browser/harness-layout/run.mjs", "--fixtures-dir", str(fixtures), "--output-dir", str(evidence)],
                cwd=repository, env=environment, capture_output=True, text=True, timeout=20,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Unexpected fixture manifest", result.stderr)
            self.assertNotIn("ENOENT", result.stderr)
            self.assertEqual(json.loads((evidence / "layout-report.json").read_text(encoding="utf-8")), [])


if __name__ == "__main__":
    unittest.main()
