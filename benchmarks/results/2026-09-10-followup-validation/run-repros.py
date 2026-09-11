#!/usr/bin/env python3
"""Run expected-to-fail audit probes without adding tests to product source."""

import json
from pathlib import Path
import subprocess
import tempfile

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def main():
    with tempfile.TemporaryDirectory(prefix="snow-validation-overlay-") as directory:
        overlay = Path(directory) / "overlay.json"
        overlay.write_text(json.dumps({"Replace": {
            str(ROOT / "internal/tui/followup_validation_test.go"):
                str(HERE / "repro_tests.go.txt"),
        }}))
        return subprocess.run([
            "go", "test", "-vet=off", "-overlay", str(overlay),
            "./internal/tui", "-run", "^TestFollowup", "-count=1", "-v",
        ], cwd=ROOT, timeout=120, check=False).returncode


if __name__ == "__main__":
    raise SystemExit(main())
