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
README = REPOSITORY_ROOT / "README.md"
GETTING_STARTED_GUIDE = REPOSITORY_ROOT / "docs" / "getting-started.md"
RELEASE_GUIDE = REPOSITORY_ROOT / "docs" / "releases.md"
SECURITY_GUIDE = REPOSITORY_ROOT / "docs" / "security.md"
INSTALL_COMMAND = (
    "curl -fsSL "
    "https://raw.githubusercontent.com/elmissouri16/snow-core/main/scripts/install.sh "
    "| sh"
)
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
        for variable in ("SNOW_NO_MODIFY_PATH", "SNOW_VERSION", "ZDOTDIR"):
            environment.pop(variable, None)
        home = self.root / "home"
        home.mkdir(exist_ok=True)
        environment.update(
            {
                "FIXTURE_DIR": str(self.fixture_dir),
                "HOME": str(home),
                "PATH": f"{self.fake_bin}{os.pathsep}{environment['PATH']}",
                "SHELL": "/bin/bash",
                "SNOW_INSTALL_DIR": str(self.install_dir),
                "TEST_UNAME_S": system,
                "TMPDIR": str(self.temporary_dir),
                "TEST_UNAME_M": machine,
            }
        )
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
        self,
        system: str,
        machine: str,
        version: Optional[str] = None,
        environment_overrides: Optional[dict[str, str]] = None,
        interpreter: str = "sh",
    ) -> subprocess.CompletedProcess[str]:
        environment = self._environment(system, machine)
        if version is not None:
            environment["SNOW_VERSION"] = version
        if environment_overrides:
            environment.update(environment_overrides)
        return subprocess.run(
            [interpreter, str(INSTALLER)],
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

    def test_user_facing_bash_invocation_installs(self) -> None:
        self._write_release("linux", "amd64")

        result = self._run("Linux", "x86_64", TAG, interpreter="bash")

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.install_dir / "snow").is_file())
        self.assertTrue((self.root / "home" / ".bashrc").is_file())

    def test_updates_bash_path_idempotently(self) -> None:
        self._write_release("linux", "amd64")

        first = self._run("Linux", "x86_64", TAG)
        second = self._run("Linux", "x86_64", TAG)

        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertEqual(second.returncode, 0, second.stderr)
        profile = self.root / "home" / ".bashrc"
        path_line = f"export PATH='{self.install_dir}':\"$PATH\""
        profile_content = profile.read_text(encoding="utf-8")
        self.assertEqual(profile_content.count(path_line), 1)
        self.assertEqual(profile_content.count("# Added by the Snow installer."), 1)
        self.assertIn("Added", first.stdout)
        self.assertIn("already configured", second.stdout)

    def test_updates_zsh_path_on_macos(self) -> None:
        self._write_release("darwin", "arm64")

        result = self._run(
            "Darwin", "arm64", TAG, {"SHELL": "/bin/zsh"}
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        profile = self.root / "home" / ".zshrc"
        self.assertIn(
            f"export PATH='{self.install_dir}':\"$PATH\"",
            profile.read_text(encoding="utf-8"),
        )

    def test_honors_absolute_zdotdir(self) -> None:
        self._write_release("darwin", "arm64")
        zdotdir = self.root / "zsh configuration"
        zdotdir.mkdir()

        result = self._run(
            "Darwin",
            "arm64",
            TAG,
            {"SHELL": "/bin/zsh", "ZDOTDIR": str(zdotdir)},
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((zdotdir / ".zshrc").is_file())
        self.assertFalse((self.root / "home" / ".zshrc").exists())

    def test_macos_bash_preserves_existing_profile_precedence(self) -> None:
        self._write_release("darwin", "amd64")
        home = self.root / "home"
        home.mkdir()
        profile = home / ".profile"
        profile.write_text("export PRESERVED_SETTING=yes\n", encoding="utf-8")

        result = self._run(
            "Darwin", "x86_64", TAG, {"SHELL": "/bin/bash"}
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((home / ".bash_profile").exists())
        content = profile.read_text(encoding="utf-8")
        self.assertIn("export PRESERVED_SETTING=yes", content)
        self.assertIn(f"export PATH='{self.install_dir}':\"$PATH\"", content)

    def test_can_skip_shell_path_update(self) -> None:
        self._write_release("linux", "amd64")

        result = self._run(
            "Linux", "x86_64", TAG, {"SNOW_NO_MODIFY_PATH": "1"}
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.root / "home" / ".bashrc").exists())
        self.assertIn("Skipped shell PATH update", result.stdout)

    def test_nonregular_shell_profile_warns_without_undoing_install(self) -> None:
        self._write_release("linux", "amd64")
        home = self.root / "home"
        home.mkdir()
        (home / ".bashrc").mkdir()

        result = self._run("Linux", "x86_64", TAG)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.install_dir / "snow").is_file())
        self.assertIn("is not a regular file", result.stderr)

    def test_shell_profile_symlink_handling_is_explicit(self) -> None:
        self._write_release("linux", "amd64")
        home = self.root / "home"
        home.mkdir()
        target = self.root / "dotfiles" / "bashrc"
        target.parent.mkdir()
        target.write_text("# managed dotfile\n", encoding="utf-8")
        profile = home / ".bashrc"
        profile.symlink_to(target)

        result = self._run("Linux", "x86_64", TAG)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(profile.is_symlink())
        self.assertIn(
            f"export PATH='{self.install_dir}':\"$PATH\"",
            target.read_text(encoding="utf-8"),
        )

        profile.unlink()
        profile.symlink_to(self.root / "missing-bashrc")
        result = self._run("Linux", "x86_64", TAG)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("is not a regular file", result.stderr)

    def test_rejects_invalid_path_configuration(self) -> None:
        invalid_opt_out = self._run(
            "Linux", "x86_64", TAG, {"SNOW_NO_MODIFY_PATH": "yes"}
        )
        self.assertNotEqual(invalid_opt_out.returncode, 0)
        self.assertIn("SNOW_NO_MODIFY_PATH must be 0 or 1", invalid_opt_out.stderr)

        invalid_paths = {
            ".": "absolute path",
            "bin": "absolute path",
            "-bin": "absolute path",
            "/tmp/snow:bin": "must not contain a colon",
        }
        for install_directory, expected_error in invalid_paths.items():
            with self.subTest(install_directory=install_directory):
                result = self._run(
                    "Linux",
                    "x86_64",
                    TAG,
                    {"SNOW_INSTALL_DIR": install_directory},
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected_error, result.stderr)

    def test_shell_path_update_quotes_install_directory(self) -> None:
        self.install_dir = self.root / "installer's bin"
        self._write_release("linux", "amd64")

        result = self._run("Linux", "x86_64", TAG)

        self.assertEqual(result.returncode, 0, result.stderr)
        profile = self.root / "home" / ".bashrc"
        environment = os.environ.copy()
        environment["PATH"] = "/usr/bin:/bin"
        sourced = subprocess.run(
            ["sh", "-c", f'. "{profile}"; printf %s "$PATH"'],
            env=environment,
            text=True,
            capture_output=True,
            check=True,
        )
        self.assertEqual(sourced.stdout.split(os.pathsep, 1)[0], str(self.install_dir))

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


class InstallerBootstrapTests(unittest.TestCase):
    def _run_bootstrap(self, mode: str) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as temporary_directory:
            fake_bin = Path(temporary_directory)
            curl = fake_bin / "curl"
            curl.write_text(
                """#!/bin/sh
case "$BOOTSTRAP_MODE" in
  success) printf '%s\\n' '#!/bin/sh' ':' ;;
  script_failure) printf '%s\\n' '#!/bin/sh' 'exit 23' ;;
  *) exit 99 ;;
esac
""",
                encoding="utf-8",
            )
            curl.chmod(0o755)
            environment = os.environ.copy()
            environment["BOOTSTRAP_MODE"] = mode
            environment["PATH"] = f"{fake_bin}{os.pathsep}{environment['PATH']}"
            return subprocess.run(
                ["sh", "-c", INSTALL_COMMAND],
                env=environment,
                text=True,
                capture_output=True,
                check=False,
                timeout=10,
            )

    def test_bootstrap_runs_downloaded_script(self) -> None:
        self.assertEqual(self._run_bootstrap("success").returncode, 0)
        self.assertEqual(self._run_bootstrap("script_failure").returncode, 23)


class InstallerDocumentationTests(unittest.TestCase):
    def test_install_guides_use_simple_one_line_command(self) -> None:
        for document in (README, GETTING_STARTED_GUIDE, RELEASE_GUIDE):
            with self.subTest(document=document):
                content = document.read_text(encoding="utf-8")
                self.assertIn(INSTALL_COMMAND, content)
                self.assertNotIn(f"{INSTALL_COMMAND} &&", content)
                self.assertNotIn("bash -o pipefail -c", content)
                self.assertNotIn("IFS= read -r first", content)
                self.assertIn("SNOW_NO_MODIFY_PATH=1", content)

        getting_started = GETTING_STARTED_GUIDE.read_text(encoding="utf-8")
        self.assertNotIn("The installer:", getting_started)
        self.assertNotIn("The command requires", getting_started)

        security = SECURITY_GUIDE.read_text(encoding="utf-8")
        self.assertIn("streams `scripts/install.sh` into `sh`", security)
        self.assertIn("persistently adds its directory", security)


if __name__ == "__main__":
    unittest.main()
