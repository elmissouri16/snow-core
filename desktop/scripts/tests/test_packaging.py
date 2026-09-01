from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path

DESKTOP_DIR = Path(__file__).resolve().parents[2]
PACKAGE = DESKTOP_DIR / "scripts/package_desktop.py"
VERIFY = DESKTOP_DIR / "scripts/verify_desktop_archive.py"
PROTOCOL_SOURCE = DESKTOP_DIR / "src/snow/protocol.rs"


def desktop_required_capabilities() -> list[str]:
    source = PROTOCOL_SOURCE.read_text(encoding="utf-8")
    declaration = re.search(
        r"pub const REQUIRED_CAPABILITIES:\s*\[&str;\s*(\d+)\]\s*=\s*\[(.*?)\];",
        source,
        re.DOTALL,
    )
    if declaration is None:
        raise RuntimeError("could not derive REQUIRED_CAPABILITIES from desktop protocol")
    declared_count = int(declaration.group(1))
    capabilities = re.findall(r'"([a-z0-9_]+)"', declaration.group(2))
    if len(capabilities) != declared_count:
        raise RuntimeError(
            f"desktop declares {declared_count} capabilities but contains {len(capabilities)}"
        )
    return capabilities


CAPABILITIES = desktop_required_capabilities()


class PackagingTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="snow-package-test-")
        self.root = Path(self.temporary.name)
        self.desktop = self.root / "snow-desktop"
        self.desktop.write_text(
            "#!/bin/sh\nprintf '%s\\n%s\\n' \"$SNOW_BINARY\" \"$SNOW_PROJECT\"\n",
            encoding="utf-8",
        )
        self.desktop.chmod(0o755)
        self.snow = self.root / "snow"
        self.write_snow(CAPABILITIES)
        tools = self.root / "tools"
        tools.mkdir()
        file_tool = tools / "file"
        file_tool.write_text(
            "#!/bin/sh\n"
            "if [ \"${FAKE_FILE_PLATFORM:-linux}\" = macos ]; then\n"
            "  echo 'Mach-O 64-bit executable arm64'\n"
            "else\n"
            "  echo 'ELF 64-bit LSB pie executable, ARM aarch64'\n"
            "fi\n",
            encoding="utf-8",
        )
        file_tool.chmod(0o755)
        self.env = os.environ.copy()
        self.env["SOURCE_DATE_EPOCH"] = "1700000000"
        self.env["PATH"] = str(tools) + os.pathsep + self.env.get("PATH", "")

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def write_snow(self, capabilities: list[str]) -> None:
        ready = json.dumps(
            {
                "type": "rpc_ready",
                "protocol_version": "1",
                "snow_version": "package-test",
                "capabilities": capabilities,
                "max_input_bytes": 1048576,
            },
            separators=(",", ":"),
        )
        self.snow.write_text(
            "#!/bin/sh\n"
            "if [ \"${1:-}\" = version ]; then echo 'snow package-test'; exit 0; fi\n"
            f"printf '%s\\n' '{ready}'\n"
            "cat >/dev/null\n",
            encoding="utf-8",
        )
        self.snow.chmod(0o755)

    def run_package(self, output: Path, platform: str = "linux") -> subprocess.CompletedProcess[str]:
        env = self.env.copy()
        env["FAKE_FILE_PLATFORM"] = platform
        return subprocess.run(
            [
                sys.executable,
                str(PACKAGE),
                "--platform",
                platform,
                "--arch",
                "arm64",
                "--snow-binary",
                str(self.snow),
                "--desktop-binary",
                str(self.desktop),
                "--output",
                str(output),
                "--version",
                "1.2.3-test.1",
                "--verify-timeout",
                "5",
            ],
            env=env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=30,
        )

    def test_linux_package_is_relocatable_verified_and_reproducible(self) -> None:
        first = self.root / "first"
        second = self.root / "second"
        first_result = self.run_package(first)
        self.assertEqual(first_result.returncode, 0, first_result.stderr)
        second_result = self.run_package(second)
        self.assertEqual(second_result.returncode, 0, second_result.stderr)
        archive_name = "snow-desktop_1.2.3-test.1_linux_arm64.tar.gz"
        first_archive = first / archive_name
        second_archive = second / archive_name
        self.assertEqual(
            hashlib.sha256(first_archive.read_bytes()).hexdigest(),
            hashlib.sha256(second_archive.read_bytes()).hexdigest(),
        )

        verified = subprocess.run(
            [sys.executable, str(VERIFY), str(first_archive)],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=15,
        )
        self.assertEqual(verified.returncode, 0, verified.stderr)
        report = json.loads(verified.stdout)
        self.assertEqual(report["manifest"]["platform"], "linux")
        self.assertEqual(
            report["manifest"]["snow"]["compatibility_check"]["rpc_protocol_version"],
            "1",
        )

        extracted = self.root / "extracted"
        extracted.mkdir()
        with tarfile.open(first_archive, "r:gz") as source:
            source.extractall(extracted)
        package_root = extracted / "snow-desktop_1.2.3-test.1_linux_arm64"
        launched = subprocess.run(
            [str(package_root / "bin/snow-desktop")],
            env={"HOME": str(self.root / "home"), "PATH": os.environ.get("PATH", "")},
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=5,
        )
        self.assertEqual(launched.returncode, 0, launched.stderr)
        lines = launched.stdout.splitlines()
        self.assertEqual(
            Path(lines[0]).resolve(),
            (package_root / "libexec/snow-desktop/snow").resolve(),
        )
        self.assertEqual(lines[1], str(self.root / "home"))

    def test_packaging_gate_matches_all_desktop_required_capabilities(self) -> None:
        self.assertEqual(len(CAPABILITIES), 32)
        source = PACKAGE.read_text(encoding="utf-8")
        packaging_declaration = re.search(
            r"REQUIRED_RPC_CAPABILITIES\s*=\s*frozenset\(\{(.*?)\}\)",
            source,
            re.DOTALL,
        )
        self.assertIsNotNone(packaging_declaration)
        packaged = set(re.findall(r'"([a-z0-9_]+)"', packaging_declaration.group(1)))
        self.assertEqual(packaged, set(CAPABILITIES))

    def test_each_missing_desktop_capability_is_rejected_without_archive(self) -> None:
        self.assertEqual(len(CAPABILITIES), 32)
        for omitted in CAPABILITIES:
            with self.subTest(omitted=omitted):
                self.write_snow([item for item in CAPABILITIES if item != omitted])
                output = self.root / f"missing-{omitted}"
                result = self.run_package(output)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertIn("missing required capabilities", result.stderr)
                self.assertIn(omitted, result.stderr)
                self.assertFalse(list(output.glob("*.tar.gz")))

    def test_dry_run_is_non_mutating_and_does_not_require_binaries(self) -> None:
        output = self.root / "dry-output"
        result = subprocess.run(
            [
                sys.executable,
                str(PACKAGE),
                "--platform",
                "linux",
                "--snow-binary",
                str(self.root / "missing-snow"),
                "--output",
                str(output),
                "--dry-run",
            ],
            env=self.env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=5,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout)["platform"], "linux")
        self.assertFalse(output.exists())

    @unittest.skipUnless(sys.platform == "darwin", "macOS app packaging requires xcrun")
    def test_macos_app_archive_has_required_bundle_files(self) -> None:
        output = self.root / "mac"
        result = self.run_package(output, platform="macos")
        self.assertEqual(result.returncode, 0, result.stderr)
        archive = output / "snow-desktop_1.2.3-test.1_darwin_arm64.zip"
        verified = subprocess.run(
            [sys.executable, str(VERIFY), str(archive)],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=15,
        )
        self.assertEqual(verified.returncode, 0, verified.stderr)
        self.assertEqual(json.loads(verified.stdout)["manifest"]["platform"], "macos")


if __name__ == "__main__":
    unittest.main()
