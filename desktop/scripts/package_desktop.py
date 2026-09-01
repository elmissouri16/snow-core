#!/usr/bin/env python3
"""Build and package Snow Desktop with a verified external Snow RPC binary."""

from __future__ import annotations

import argparse
import gzip
import hashlib
import json
import os
import platform as host_platform
import re
import selectors
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import zipfile
from pathlib import Path
from typing import BinaryIO

DESKTOP_DIR = Path(__file__).resolve().parents[1]
REPO_DIR = DESKTOP_DIR.parent
PACKAGING_DIR = DESKTOP_DIR / "packaging"
MAX_SOURCE_BYTES = 512 * 1024 * 1024
MAX_STAGE_BYTES = 1024 * 1024 * 1024
MAX_COMMAND_BYTES = 1024 * 1024
MAX_RPC_FRAME_BYTES = 16 * 1024 * 1024
# Keep this immutable compatibility gate synchronized with
# desktop/src/snow/protocol.rs::REQUIRED_CAPABILITIES. The packaging tests
# derive the Rust list and fail on any drift or missing omission case.
REQUIRED_RPC_CAPABILITIES = frozenset({
    "active_input",
    "authentication",
    "branch_management",
    "compaction",
    "context_report",
    "debug_diagnostics",
    "diagnostics",
    "goals",
    "managed_processes",
    "mcp_servers",
    "messages_list",
    "messages_page",
    "models_list",
    "multimodal_prompts",
    "pending_inputs",
    "permission_interaction",
    "permission_mode",
    "presentation_settings",
    "project_init",
    "project_trust",
    "prompt_completion",
    "response_controls",
    "session_forks",
    "session_info",
    "session_management",
    "settings",
    "skills",
    "subagent_messages",
    "subagent_models",
    "subagents",
    "usage",
    "user_input",
})

if len(REQUIRED_RPC_CAPABILITIES) != 32:
    raise RuntimeError("desktop packaging must enforce exactly 32 RPC capabilities")


class PackagingError(RuntimeError):
    pass


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Build a relocatable Snow Desktop package. The selected snow binary "
            "is tested with a credential-free RPC handshake before bundling."
        )
    )
    parser.add_argument("--platform", choices=("macos", "linux"), required=True)
    parser.add_argument("--arch", choices=("amd64", "arm64"), default=host_arch())
    parser.add_argument("--snow-binary", required=True, type=Path)
    parser.add_argument(
        "--desktop-binary",
        type=Path,
        help="use a prebuilt snow-desktop executable instead of cargo build",
    )
    parser.add_argument("--target", help="Cargo target triple for the desktop build")
    parser.add_argument("--output", type=Path, default=DESKTOP_DIR / "dist")
    parser.add_argument("--version", help="package version (defaults to Cargo.toml)")
    parser.add_argument(
        "--codesign-identity",
        help="macOS Developer ID identity; signing is skipped when omitted",
    )
    parser.add_argument(
        "--macos-deployment-target",
        default="12.0",
        help="minimum macOS version encoded in the native launcher (default: 12.0)",
    )
    parser.add_argument(
        "--build-timeout",
        type=positive_int,
        default=1800,
        help="maximum cargo build seconds (default: 1800)",
    )
    parser.add_argument(
        "--verify-timeout",
        type=positive_int,
        default=15,
        help="maximum seconds for each Snow compatibility check (default: 15)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="print the resolved plan without building, checking, or writing",
    )
    return parser.parse_args(argv)


def positive_int(value: str) -> int:
    parsed = int(value)
    if parsed <= 0:
        raise argparse.ArgumentTypeError("must be positive")
    return parsed


def host_arch() -> str:
    machine = host_platform.machine().lower()
    if machine in {"x86_64", "amd64"}:
        return "amd64"
    if machine in {"aarch64", "arm64"}:
        return "arm64"
    return "arm64"


def host_os() -> str:
    if sys.platform == "darwin":
        return "macos"
    if sys.platform.startswith("linux"):
        return "linux"
    return "unsupported"


def package_version(explicit: str | None) -> str:
    if explicit:
        version = explicit.strip()
    else:
        cargo = (DESKTOP_DIR / "Cargo.toml").read_text(encoding="utf-8")
        match = re.search(r'^version\s*=\s*"([^"]+)"\s*$', cargo, re.MULTILINE)
        if not match:
            raise PackagingError("could not read package version from desktop/Cargo.toml")
        version = match.group(1)
    if not re.fullmatch(r"[0-9A-Za-z][0-9A-Za-z.+-]{0,63}", version):
        raise PackagingError("version must be 1-64 safe semver-style characters")
    return version


def source_epoch() -> int:
    raw = os.environ.get("SOURCE_DATE_EPOCH", "0")
    try:
        epoch = int(raw)
    except ValueError as error:
        raise PackagingError("SOURCE_DATE_EPOCH must be an integer") from error
    if epoch < 0:
        raise PackagingError("SOURCE_DATE_EPOCH must not be negative")
    return epoch


def resolve_binary(path: Path, label: str) -> Path:
    try:
        resolved = path.expanduser().resolve(strict=True)
    except OSError as error:
        raise PackagingError(f"{label} does not exist: {path}") from error
    try:
        info = resolved.stat()
    except OSError as error:
        raise PackagingError(f"could not inspect {label}: {resolved}") from error
    if not stat.S_ISREG(info.st_mode):
        raise PackagingError(f"{label} is not a regular file: {resolved}")
    if info.st_size <= 0 or info.st_size > MAX_SOURCE_BYTES:
        raise PackagingError(
            f"{label} size must be between 1 byte and {MAX_SOURCE_BYTES} bytes"
        )
    if not os.access(resolved, os.X_OK):
        raise PackagingError(f"{label} is not executable: {resolved}")
    return resolved


def clean_check_environment(home: Path) -> dict[str, str]:
    env = {
        "HOME": str(home),
        "SNOW_HOME": str(home / ".snow"),
        "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
        "LANG": os.environ.get("LANG", "C.UTF-8"),
        "LC_ALL": os.environ.get("LC_ALL", "C.UTF-8"),
        "RUST_BACKTRACE": "0",
    }
    if "TMPDIR" in os.environ:
        env["TMPDIR"] = os.environ["TMPDIR"]
    return env


def bounded_command(
    argv: list[str], timeout: int, env: dict[str, str], cwd: Path
) -> tuple[int, bytes, bytes]:
    process = subprocess.Popen(
        argv,
        cwd=cwd,
        env=env,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    assert process.stdout is not None and process.stderr is not None
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ, "stdout")
    selector.register(process.stderr, selectors.EVENT_READ, "stderr")
    output = {"stdout": bytearray(), "stderr": bytearray()}
    deadline = time.monotonic() + timeout
    try:
        while selector.get_map():
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise PackagingError(f"command timed out after {timeout}s: {argv[0]}")
            events = selector.select(min(remaining, 0.1))
            if not events and process.poll() is not None:
                events = selector.select(0)
            for key, _ in events:
                chunk = os.read(key.fileobj.fileno(), 65536)
                if not chunk:
                    selector.unregister(key.fileobj)
                    continue
                bucket = output[key.data]
                bucket.extend(chunk)
                if len(bucket) > MAX_COMMAND_BYTES:
                    raise PackagingError(
                        f"command {key.data} exceeded {MAX_COMMAND_BYTES} bytes: {argv[0]}"
                    )
        remaining = max(0.0, deadline - time.monotonic())
        returncode = process.wait(timeout=remaining)
        return returncode, bytes(output["stdout"]), bytes(output["stderr"])
    except Exception:
        terminate_process(process)
        raise
    finally:
        selector.close()


def terminate_process(process: subprocess.Popen[bytes]) -> None:
    if process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=2)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=2)


def verify_snow(snow: Path, timeout: int) -> dict[str, object]:
    with tempfile.TemporaryDirectory(prefix="snow-desktop-verify-") as temporary:
        root = Path(temporary)
        env = clean_check_environment(root / "home")
        (root / "home").mkdir(mode=0o700)
        code, stdout, stderr = bounded_command(
            [str(snow), "version"], timeout, env, root
        )
        if code != 0:
            detail = stderr.decode("utf-8", "replace").strip()
            raise PackagingError(f"snow version failed ({code}): {detail[:500]}")
        version = stdout.decode("utf-8", "replace").strip()
        if not version:
            raise PackagingError("snow version returned empty output")

        argv = [
            str(snow),
            "--mode",
            "rpc",
            "--provider",
            "fake",
            "--project",
            str(root),
            "--permission",
            "deny",
            "--thinking",
            "off",
            "--no-session",
            "--no-plugins",
            "--no-mcp",
            "--no-skills",
            "--no-subagents",
        ]
        process = subprocess.Popen(
            argv,
            cwd=root,
            env=env,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        assert process.stdin is not None
        assert process.stdout is not None
        assert process.stderr is not None
        selector = selectors.DefaultSelector()
        selector.register(process.stdout, selectors.EVENT_READ, "stdout")
        selector.register(process.stderr, selectors.EVENT_READ, "stderr")
        stdout_buffer = bytearray()
        stderr_buffer = bytearray()
        deadline = time.monotonic() + timeout
        line: bytes | None = None
        try:
            while line is None:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise PackagingError(f"Snow RPC handshake timed out after {timeout}s")
                events = selector.select(min(remaining, 0.1))
                if not events and process.poll() is not None:
                    raise PackagingError(
                        f"Snow exited before rpc_ready (exit {process.returncode})"
                    )
                for key, _ in events:
                    chunk = os.read(key.fileobj.fileno(), 65536)
                    if not chunk:
                        selector.unregister(key.fileobj)
                        continue
                    if key.data == "stderr":
                        stderr_buffer.extend(chunk)
                        if len(stderr_buffer) > MAX_COMMAND_BYTES:
                            raise PackagingError("Snow RPC stderr exceeded its 1 MiB limit")
                        continue
                    stdout_buffer.extend(chunk)
                    if len(stdout_buffer) > MAX_RPC_FRAME_BYTES:
                        raise PackagingError("Snow rpc_ready frame exceeded 16 MiB")
                    newline = stdout_buffer.find(b"\n")
                    if newline >= 0:
                        line = bytes(stdout_buffer[:newline])
                        break
            process.stdin.close()
            try:
                process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                terminate_process(process)
        except Exception:
            terminate_process(process)
            raise
        finally:
            selector.close()

        try:
            ready = json.loads(line.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError) as error:
            raise PackagingError("Snow RPC handshake was not valid UTF-8 JSON") from error
        if not isinstance(ready, dict) or ready.get("type") != "rpc_ready":
            raise PackagingError("Snow RPC first frame was not rpc_ready")
        if ready.get("protocol_version") != "1":
            raise PackagingError(
                f"unsupported Snow RPC protocol version: {ready.get('protocol_version')!r}"
            )
        capabilities = ready.get("capabilities")
        if not isinstance(capabilities, list) or not all(
            isinstance(item, str) for item in capabilities
        ):
            raise PackagingError("Snow rpc_ready capabilities were invalid")
        missing = REQUIRED_RPC_CAPABILITIES.difference(capabilities)
        if missing:
            raise PackagingError(
                "Snow RPC is missing required capabilities: " + ", ".join(sorted(missing))
            )
        max_input = ready.get("max_input_bytes")
        if not isinstance(max_input, int) or max_input <= 0:
            raise PackagingError("Snow rpc_ready max_input_bytes was invalid")
        return {
            "version_output": version[:1000],
            "rpc_protocol_version": "1",
            "snow_version": str(ready.get("snow_version", ""))[:256],
            "capabilities": sorted(capabilities),
            "max_input_bytes": max_input,
        }



def verify_binary_format(
    binary: Path, target_os: str, arch: str, timeout: int
) -> str:
    env = os.environ.copy()
    code, stdout, stderr = bounded_command(
        ["file", "-b", str(binary)], timeout, env, REPO_DIR
    )
    if code != 0:
        detail = stderr.decode("utf-8", "replace").strip()
        raise PackagingError(f"file could not inspect {binary.name}: {detail[:500]}")
    description = stdout.decode("utf-8", "replace").strip()
    if target_os == "macos":
        platform_matches = "Mach-O" in description
        arch_matches = (
            "arm64" in description
            if arch == "arm64"
            else "x86_64" in description
        )
    else:
        platform_matches = "ELF" in description
        arch_matches = (
            bool(re.search(r"(?:aarch64|ARM64)", description, re.IGNORECASE))
            if arch == "arm64"
            else bool(re.search(r"(?:x86[-_]64|AMD x86-64)", description, re.IGNORECASE))
        )
    if not platform_matches or not arch_matches:
        raise PackagingError(
            f"{binary.name} does not match {target_os}/{arch}: {description[:500]}"
        )
    return description[:1000]

def build_desktop(args: argparse.Namespace) -> Path:
    if args.desktop_binary is not None:
        return resolve_binary(args.desktop_binary, "desktop binary")
    if host_os() != args.platform:
        raise PackagingError(
            f"building a {args.platform} desktop binary requires a {args.platform} host; "
            "pass --desktop-binary with a separately built artifact"
        )
    command = [
        "cargo",
        "build",
        "--manifest-path",
        str(DESKTOP_DIR / "Cargo.toml"),
        "--locked",
        "--release",
    ]
    if args.target:
        command.extend(("--target", args.target))
    try:
        build_env = os.environ.copy()
        build_env["CARGO_TARGET_DIR"] = str(DESKTOP_DIR / "target")
        subprocess.run(
            command, cwd=REPO_DIR, env=build_env, check=True, timeout=args.build_timeout
        )
    except FileNotFoundError as error:
        raise PackagingError("cargo was not found on PATH") from error
    except subprocess.TimeoutExpired as error:
        raise PackagingError(
            f"cargo build timed out after {args.build_timeout}s"
        ) from error
    except subprocess.CalledProcessError as error:
        raise PackagingError(f"cargo build failed with exit {error.returncode}") from error
    relative = Path("release/snow-desktop")
    if args.target:
        relative = Path(args.target) / relative
    return resolve_binary(DESKTOP_DIR / "target" / relative, "desktop binary")


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def copy_executable(source: Path, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source, destination)
    destination.chmod(0o755)


def copy_asset(source: Path, destination: Path, mode: int = 0o644) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source, destination)
    destination.chmod(mode)


def write_text(path: Path, content: str, mode: int = 0o644) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8", newline="\n") as target:
        target.write(content)
    path.chmod(mode)


def stage_size(root: Path) -> int:
    total = 0
    for path in root.rglob("*"):
        if path.is_symlink():
            raise PackagingError(f"package staging contains a symlink: {path}")
        if path.is_file():
            size = path.stat().st_size
            if size > MAX_SOURCE_BYTES:
                raise PackagingError(f"staged file exceeds 512 MiB: {path}")
            total += size
            if total > MAX_STAGE_BYTES:
                raise PackagingError("package staging exceeds 1 GiB")
    return total


def render_info_plist(version: str) -> str:
    template = (PACKAGING_DIR / "macos/Info.plist.in").read_text(encoding="utf-8")
    numbers = re.findall(r"\d+", version)[:3] or ["1"]
    return template.replace("@VERSION@", version).replace(
        "@BUILD_VERSION@", ".".join(numbers)
    )


def package_manifest(
    version: str,
    target_os: str,
    arch: str,
    epoch: int,
    snow: Path,
    desktop: Path,
    snow_check: dict[str, object],
    signed: bool,
    binary_formats: dict[str, str],
) -> dict[str, object]:
    return {
        "format": "snow-desktop-package-v1",
        "version": version,
        "platform": target_os,
        "architecture": arch,
        "source_date_epoch": epoch,
        "desktop": {
            "filename": "snow-desktop-bin",
            "sha256": sha256(desktop),
        },
        "snow": {
            "filename": "snow",
            "sha256": sha256(snow),
            "compatibility_check": snow_check,
        },
        "macos_signed": signed,
        "binary_formats": binary_formats,
        "architecture_note": (
            "Artifact labels describe the selected input binaries; this script does not "
            "rewrite or emulate their machine architecture."
        ),
    }



def compile_macos_launcher(
    destination: Path, arch: str, deployment_target: str, timeout: int
) -> None:
    if sys.platform != "darwin":
        raise PackagingError("macOS application packaging requires a macOS host")
    if not re.fullmatch(r"[0-9]+(?:\.[0-9]+){0,2}", deployment_target):
        raise PackagingError("--macos-deployment-target must contain numeric components")
    clang_arch = {"amd64": "x86_64", "arm64": "arm64"}[arch]
    destination.parent.mkdir(parents=True, exist_ok=True)
    run_checked(
        [
            "xcrun",
            "clang",
            "-Os",
            "-Wall",
            "-Wextra",
            "-Werror",
            f"-mmacosx-version-min={deployment_target}",
            "-arch",
            clang_arch,
            str(PACKAGING_DIR / "macos/launcher.c"),
            "-o",
            str(destination),
        ],
        timeout,
        "macOS launcher build",
    )
    destination.chmod(0o755)

def sign_macos(app: Path, identity: str, timeout: int) -> None:
    if sys.platform != "darwin":
        raise PackagingError("macOS code signing requires a macOS host")
    targets = [
        app / "Contents/Resources/snow",
        app / "Contents/MacOS/snow-desktop-bin",
        app / "Contents/MacOS/Snow Desktop",
    ]
    for target in targets:
        run_checked(
            [
                "codesign",
                "--force",
                "--options",
                "runtime",
                "--timestamp",
                "--sign",
                identity,
                str(target),
            ],
            timeout,
            "codesign",
        )


def sign_macos_bundle(app: Path, identity: str, timeout: int) -> None:
    run_checked(
        [
            "codesign",
            "--force",
            "--options",
            "runtime",
            "--timestamp",
            "--sign",
            identity,
            str(app),
        ],
        timeout,
        "codesign bundle",
    )
    run_checked(
        ["codesign", "--verify", "--deep", "--strict", "--verbose=2", str(app)],
        timeout,
        "codesign verification",
    )


def run_checked(argv: list[str], timeout: int, label: str) -> None:
    try:
        subprocess.run(argv, check=True, timeout=timeout)
    except FileNotFoundError as error:
        raise PackagingError(f"{argv[0]} was not found") from error
    except subprocess.TimeoutExpired as error:
        raise PackagingError(f"{label} timed out after {timeout}s") from error
    except subprocess.CalledProcessError as error:
        raise PackagingError(f"{label} failed with exit {error.returncode}") from error


def normalize_mtimes(root: Path, epoch: int) -> None:
    archive_epoch = max(epoch, 315532800)  # ZIP cannot represent dates before 1980.
    for path in sorted(root.rglob("*"), reverse=True):
        os.utime(path, (archive_epoch, archive_epoch), follow_symlinks=False)
    os.utime(root, (archive_epoch, archive_epoch), follow_symlinks=False)


def create_zip(root: Path, archive: Path, epoch: int) -> None:
    normalize_mtimes(root, epoch)
    with zipfile.ZipFile(
        archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9
    ) as target:
        for path in [root, *sorted(root.rglob("*"))]:
            arcname = path.relative_to(root.parent).as_posix()
            if path.is_dir():
                info = zipfile.ZipInfo(arcname.rstrip("/") + "/")
                info.date_time = time.gmtime(max(epoch, 315532800))[:6]
                info.create_system = 3
                info.external_attr = (0o40755 << 16) | 0x10
                target.writestr(info, b"")
            else:
                target.write(path, arcname)


def reset_tar_info(info: tarfile.TarInfo, epoch: int) -> tarfile.TarInfo:
    info.uid = 0
    info.gid = 0
    info.uname = ""
    info.gname = ""
    info.mtime = epoch
    info.pax_headers = {}
    return info


def create_tar_gz(root: Path, archive: Path, epoch: int) -> None:
    with archive.open("wb") as raw:
        with gzip.GzipFile(
            filename="", mode="wb", fileobj=raw, compresslevel=9, mtime=epoch
        ) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as target:
                for path in [root, *sorted(root.rglob("*"))]:
                    arcname = path.relative_to(root.parent).as_posix()
                    info = reset_tar_info(target.gettarinfo(str(path), arcname), epoch)
                    if info.isfile():
                        with path.open("rb") as source:
                            target.addfile(info, source)
                    else:
                        target.addfile(info)


def write_checksum(archive: Path) -> Path:
    checksum = archive.with_name(archive.name + ".sha256")
    temporary = checksum.with_name(checksum.name + ".tmp")
    temporary.write_text(f"{sha256(archive)}  {archive.name}\n", encoding="ascii")
    os.replace(temporary, checksum)
    return checksum


def build_macos_stage(
    temporary: Path,
    version: str,
    snow: Path,
    desktop: Path,
    epoch: int,
    arch: str,
    check: dict[str, object],
    identity: str | None,
    binary_formats: dict[str, str],
    deployment_target: str,
    timeout: int,
) -> Path:
    app = temporary / "Snow Desktop.app"
    contents = app / "Contents"
    copy_executable(desktop, contents / "MacOS/snow-desktop-bin")
    compile_macos_launcher(
        contents / "MacOS/Snow Desktop", arch, deployment_target, timeout
    )
    copy_executable(snow, contents / "Resources/snow")
    write_text(contents / "Info.plist", render_info_plist(version))
    write_text(contents / "PkgInfo", "APPL????\n")
    if identity:
        sign_macos(app, identity, timeout)
    manifest = package_manifest(
        version, "macos", arch, epoch,
        contents / "Resources/snow", contents / "MacOS/snow-desktop-bin", check,
        bool(identity),
        binary_formats,
    )
    write_text(
        contents / "Resources/package-manifest.json",
        json.dumps(manifest, indent=2, sort_keys=True) + "\n",
    )
    if identity:
        sign_macos_bundle(app, identity, timeout)
    return app


def build_linux_stage(
    temporary: Path,
    version: str,
    snow: Path,
    desktop: Path,
    epoch: int,
    arch: str,
    check: dict[str, object],
    binary_formats: dict[str, str],
) -> Path:
    root = temporary / f"snow-desktop_{version}_linux_{arch}"
    copy_executable(PACKAGING_DIR / "linux/launcher.sh", root / "bin/snow-desktop")
    copy_executable(desktop, root / "libexec/snow-desktop/snow-desktop-bin")
    copy_executable(snow, root / "libexec/snow-desktop/snow")
    copy_asset(
        PACKAGING_DIR / "linux/snow-desktop.desktop",
        root / "share/applications/snow-desktop.desktop",
    )
    copy_asset(
        PACKAGING_DIR / "linux/snow-desktop.svg",
        root / "share/icons/hicolor/scalable/apps/snow-desktop.svg",
    )
    manifest = package_manifest(
        version,
        "linux",
        arch,
        epoch,
        root / "libexec/snow-desktop/snow",
        root / "libexec/snow-desktop/snow-desktop-bin",
        check,
        False,
        binary_formats,
    )
    write_text(
        root / "share/doc/snow-desktop/package-manifest.json",
        json.dumps(manifest, indent=2, sort_keys=True) + "\n",
    )
    write_text(
        root / "share/doc/snow-desktop/INSTALL.txt",
        "Snow Desktop is relocatable. Run bin/snow-desktop from the extracted tree.\n"
        "For a user installation, copy the tree under ~/.local and ensure ~/.local/bin is on PATH.\n"
        "The launcher selects the bundled Snow RPC binary unless SNOW_BINARY overrides it.\n",
    )
    return root


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    version = package_version(args.version)
    epoch = source_epoch()
    if args.codesign_identity and args.platform != "macos":
        raise PackagingError("--codesign-identity is valid only for --platform macos")
    plan = {
        "platform": args.platform,
        "architecture": args.arch,
        "version": version,
        "snow_binary": str(args.snow_binary.expanduser()),
        "desktop_binary": (
            str(args.desktop_binary.expanduser()) if args.desktop_binary else "cargo --release"
        ),
        "cargo_target": args.target,
        "output": str(args.output.expanduser()),
        "codesign": bool(args.codesign_identity),
        "macos_deployment_target": args.macos_deployment_target,
        "source_date_epoch": epoch,
    }
    if args.dry_run:
        print(json.dumps(plan, indent=2, sort_keys=True))
        return 0

    snow = resolve_binary(args.snow_binary, "snow binary")
    desktop = build_desktop(args)
    check = verify_snow(snow, args.verify_timeout)
    binary_formats = {
        "snow": verify_binary_format(
            snow, args.platform, args.arch, args.verify_timeout
        ),
        "desktop": verify_binary_format(
            desktop, args.platform, args.arch, args.verify_timeout
        ),
    }
    output = args.output.expanduser().resolve()
    output.mkdir(parents=True, exist_ok=True)

    with tempfile.TemporaryDirectory(prefix=".snow-desktop-package-", dir=output) as temp_name:
        temporary = Path(temp_name)
        if args.platform == "macos":
            root = build_macos_stage(
                temporary,
                version,
                snow,
                desktop,
                epoch,
                args.arch,
                check,
                args.codesign_identity,
                binary_formats,
                args.macos_deployment_target,
                min(args.build_timeout, 600),
            )
            archive_name = f"snow-desktop_{version}_darwin_{args.arch}.zip"
        else:
            root = build_linux_stage(
                temporary, version, snow, desktop, epoch, args.arch, check,
                binary_formats,
            )
            archive_name = f"snow-desktop_{version}_linux_{args.arch}.tar.gz"
        stage_size(root)

        temporary_archive = output / (archive_name + ".tmp")
        temporary_archive.unlink(missing_ok=True)
        try:
            if args.platform == "macos":
                create_zip(root, temporary_archive, epoch)
            else:
                create_tar_gz(root, temporary_archive, epoch)
            archive = output / archive_name
            os.replace(temporary_archive, archive)
        finally:
            temporary_archive.unlink(missing_ok=True)
    checksum = write_checksum(archive)
    print(
        json.dumps(
            {
                "archive": str(archive),
                "checksum": str(checksum),
                "sha256": sha256(archive),
                "snow_sha256": sha256(snow),
                "desktop_sha256": sha256(desktop),
                "snow_rpc_protocol": check["rpc_protocol_version"],
            },
            indent=2,
            sort_keys=True,
        )
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except PackagingError as error:
        print(f"package_desktop.py: error: {error}", file=sys.stderr)
        raise SystemExit(2)
