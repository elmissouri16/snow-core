from __future__ import annotations

import hashlib
import io
import os
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path
from typing import Optional


REPOSITORY_ROOT = Path(__file__).resolve().parents[2]
INSTALLER = REPOSITORY_ROOT / "scripts" / "install.sh"
VERSION = "1.2.3-alpha.4"
TAG = f"v{VERSION}"


class InstallerTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.root = Path(self.temp_dir.name)
        self.fixture_dir = self.root / "fixtures"
        self.fake_bin = self.root / "bin"
        self.install_dir = self.root / "install path"
        self.temporary_dir = self.root / "temporary files"
        self.fixture_dir.mkdir()
        self.fake_bin.mkdir()
        self.temporary_dir.mkdir()
        self._write_executable(
            self.fake_bin / "uname",
            """#!/bin/sh
case "$1" in
  -s) printf '%s\\n' "$TEST_UNAME_S" ;;
  -m) printf '%s\\n' "$TEST_UNAME_M" ;;
  *) exit 2 ;;
esac
""",
        )
        self._write_executable(
            self.fake_bin / "curl",
            """#!/bin/sh
output=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output)
      output=$2
      shift 2
      ;;
    *)
      url=$1
      shift
      ;;
  esac
done
[ -n "$output" ] || exit 2
case "$url" in
  *'/releases?per_page=1') source=$FIXTURE_DIR/releases.json ;;
  */SHA256SUMS) source=$FIXTURE_DIR/SHA256SUMS ;;
  *.tar.gz) source=$FIXTURE_DIR/${url##*/} ;;
  *) exit 3 ;;
esac
cp "$source" "$output"
""",
        )

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def _write_executable(self, path: Path, content: str) -> None:
        path.write_text(content, encoding="utf-8")
        path.chmod(0o755)

    def _environment(self, system: str, machine: str) -> dict[str, str]:
        environment = os.environ.copy()
        environment.update(
            {
                "FIXTURE_DIR": str(self.fixture_dir),
                "HOME": str(self.root / "home"),
                "PATH": f"{self.fake_bin}{os.pathsep}{environment['PATH']}",
                "SNOW_INSTALL_DIR": str(self.install_dir),
                "TEST_UNAME_S": system,
                "TMPDIR": str(self.temporary_dir),
                "TEST_UNAME_M": machine,
            }
        )
        environment.pop("SNOW_VERSION", None)
        return environment

    def _write_release(
        self,
        operating_system: str,
        architecture: str,
        reported_version: str = VERSION,
        readme_content: bytes = b"# Test release\n",
        extra_member: Optional[str] = None,
    ) -> Path:
        archive_name = f"snow_{VERSION}_{operating_system}_{architecture}.tar.gz"
        archive_path = self.fixture_dir / archive_name
        root_name = f"snow_{VERSION}_{operating_system}_{architecture}"
        binary = f"#!/bin/sh\nprintf '%s\\n' '{reported_version}'\n".encode()
        files = {
            f"{root_name}/LICENSE": b"test license\n",
            f"{root_name}/README.md": readme_content,
            f"{root_name}/snow": binary,
        }
        with tarfile.open(archive_path, "w:gz") as archive:
            root = tarfile.TarInfo(f"{root_name}/")
            root.type = tarfile.DIRTYPE
            root.mode = 0o755
            archive.addfile(root)
            for name, content in files.items():
                info = tarfile.TarInfo(name)
                info.mode = 0o755 if name.endswith("/snow") else 0o644
                info.size = len(content)
                archive.addfile(info, io.BytesIO(content))
            if extra_member is not None:
                content = b"unexpected\n"
                info = tarfile.TarInfo(extra_member)
                info.size = len(content)
                archive.addfile(info, io.BytesIO(content))
        self._write_release_metadata(archive_name, archive_path)
        return archive_path

    def _write_release_metadata(self, archive_name: str, archive_path: Path) -> None:
        digest = hashlib.sha256(archive_path.read_bytes()).hexdigest()
        (self.fixture_dir / "SHA256SUMS").write_text(
            f"{digest}  {archive_name}\n", encoding="utf-8"
        )
        (self.fixture_dir / "releases.json").write_text(
            "[\n"
            "  {\n"
            f'    "tag_name": "{TAG}",\n'
            '    "body": "example with \\\"tag_name\\\": \\\"not-a-release\\\""\n'
            "  }\n"
            "]\n",
            encoding="utf-8",
        )

    def _run(
        self, system: str, machine: str, version: Optional[str] = None
    ) -> subprocess.CompletedProcess[str]:
        environment = self._environment(system, machine)
        if version is not None:
            environment["SNOW_VERSION"] = version
        return subprocess.run(
            ["sh", str(INSTALLER)],
            cwd=REPOSITORY_ROOT,
            env=environment,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
            timeout=20,
        )

    def test_installs_latest_release_for_supported_platforms(self) -> None:
        platforms = (
            ("Linux", "x86_64", "linux", "amd64"),
            ("Linux", "aarch64", "linux", "arm64"),
            ("Darwin", "x86_64", "darwin", "amd64"),
            ("Darwin", "arm64", "darwin", "arm64"),
        )
        for system, machine, operating_system, architecture in platforms:
            with self.subTest(system=system, machine=machine):
                self._write_release(operating_system, architecture)
                result = self._run(system, machine)
                self.assertEqual(result.returncode, 0, result.stderr)
                installed = self.install_dir / "snow"
                self.assertTrue(installed.is_file())
                version = subprocess.run(
                    [str(installed), "version"],
                    text=True,
                    capture_output=True,
                    check=True,
                )
                self.assertEqual(version.stdout.strip(), VERSION)
                self.assertIn(f"Installed Snow {VERSION}", result.stdout)
                installed.unlink()

    def test_installs_an_explicit_version_and_replaces_existing_binary(self) -> None:
        self._write_release("linux", "amd64")
        (self.fixture_dir / "releases.json").unlink()
        self.install_dir.mkdir()
        installed = self.install_dir / "snow"
        installed.write_text("existing binary\n", encoding="utf-8")

        result = self._run("Linux", "x86_64", TAG)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(f"Installed Snow {VERSION}", result.stdout)
        self.assertTrue(installed.is_file())
        self.assertNotEqual(installed.read_text(encoding="utf-8"), "existing binary\n")
        self.assertEqual(list(self.install_dir.glob(".snow.*")), [])

    def test_failed_atomic_move_preserves_existing_binary_and_cleans_stage(self) -> None:
        self._write_release("linux", "amd64")
        self.install_dir.mkdir()
        installed = self.install_dir / "snow"
        installed.write_text("existing binary\n", encoding="utf-8")
        self._write_executable(self.fake_bin / "mv", "#!/bin/sh\nexit 23\n")

        result = self._run("Linux", "x86_64", TAG)

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(installed.read_text(encoding="utf-8"), "existing binary\n")
        self.assertEqual(list(self.install_dir.glob(".snow.*")), [])

    def test_rejects_unexpected_traversal_path_before_extraction(self) -> None:
        self._write_release("linux", "amd64", extra_member="../escape")

        result = self._run("Linux", "x86_64", TAG)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unexpected paths", result.stderr)
        self.assertFalse((self.root / "escape").exists())
        self.assertFalse(self.install_dir.exists())

    def test_rejects_unsafe_archive_member_type_before_extraction(self) -> None:
        archive_name = f"snow_{VERSION}_linux_amd64.tar.gz"
        archive_path = self.fixture_dir / archive_name
        root_name = f"snow_{VERSION}_linux_amd64"
        with tarfile.open(archive_path, "w:gz") as archive:
            root = tarfile.TarInfo(f"{root_name}/")
            root.type = tarfile.DIRTYPE
            archive.addfile(root)
            for leaf in ("LICENSE", "README.md"):
                content = leaf.encode()
                info = tarfile.TarInfo(f"{root_name}/{leaf}")
                info.size = len(content)
                archive.addfile(info, io.BytesIO(content))
            link = tarfile.TarInfo(f"{root_name}/snow")
            link.type = tarfile.SYMTYPE
            link.linkname = "/bin/sh"
            archive.addfile(link)
        self._write_release_metadata(archive_name, archive_path)

        result = self._run("Linux", "x86_64", TAG)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unsafe member types", result.stderr)
        self.assertFalse(self.install_dir.exists())

    def test_wrong_binary_version_is_rejected_before_installation(self) -> None:
        self._write_release("linux", "amd64", reported_version="9.9.9")

        result = self._run("Linux", "x86_64", TAG)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f"reports 9.9.9, expected {VERSION}", result.stderr)
        self.assertFalse(self.install_dir.exists())

    def test_rejects_oversized_archive_member_before_extraction(self) -> None:
        self._write_release(
            "linux", "amd64", readme_content=b"x" * (4 * 1024 * 1024 + 1)
        )

        result = self._run("Linux", "x86_64", TAG)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unsafe member types or sizes", result.stderr)
        self.assertFalse(self.install_dir.exists())

    def test_rejects_oversized_release_api_response(self) -> None:
        (self.fixture_dir / "releases.json").write_bytes(b"x" * (1024 * 1024 + 1))

        result = self._run("Linux", "x86_64")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("download exceeds 1048576-byte limit", result.stderr)
        self.assertFalse(self.install_dir.exists())

    def test_checksum_failure_does_not_replace_existing_binary(self) -> None:
        self._write_release("linux", "amd64")
        archive_name = f"snow_{VERSION}_linux_amd64.tar.gz"
        (self.fixture_dir / "SHA256SUMS").write_text(
            f"{'0' * 64}  {archive_name}\n", encoding="utf-8"
        )
        self.install_dir.mkdir()
        installed = self.install_dir / "snow"
        installed.write_text("existing binary\n", encoding="utf-8")

        result = self._run("Linux", "x86_64")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("checksum verification failed", result.stderr)
        self.assertEqual(installed.read_text(encoding="utf-8"), "existing binary\n")

    def test_rejects_non_file_install_destination(self) -> None:
        self._write_release("linux", "amd64")
        (self.install_dir / "snow").mkdir(parents=True)

        result = self._run("Linux", "x86_64", TAG)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("install destination exists and is not a regular file", result.stderr)
        self.assertTrue((self.install_dir / "snow").is_dir())
        self.assertEqual(list(self.install_dir.glob(".snow.*")), [])

    def test_rejects_unsupported_operating_system(self) -> None:
        result = self._run("Windows_NT", "x86_64")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unsupported operating system: Windows_NT", result.stderr)
        self.assertFalse((self.install_dir / "snow").exists())

    def test_rejects_unsupported_architecture(self) -> None:
        result = self._run("Linux", "riscv64")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unsupported architecture: riscv64", result.stderr)
        self.assertFalse((self.install_dir / "snow").exists())


if __name__ == "__main__":
    unittest.main()
