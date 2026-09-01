#!/usr/bin/env python3
"""Statically validate a Snow Desktop packaging archive and checksum."""

from __future__ import annotations

import argparse
import hashlib
import json
import stat
import sys
import tarfile
import zipfile
from pathlib import Path, PurePosixPath

MAX_ARCHIVE_BYTES = 1024 * 1024 * 1024
MAX_ENTRY_BYTES = 512 * 1024 * 1024
MAX_TOTAL_BYTES = 1024 * 1024 * 1024
MAX_ENTRIES = 256
MAX_COMMAND_BYTES = 1024 * 1024


class VerificationError(RuntimeError):
    pass


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        while chunk := source.read(1024 * 1024):
            digest.update(chunk)
    return digest.hexdigest()


def safe_name(name: str) -> PurePosixPath:
    path = PurePosixPath(name)
    if path.is_absolute() or ".." in path.parts or not path.parts:
        raise VerificationError(f"unsafe archive path: {name!r}")
    return path


def check_checksum(archive: Path, checksum: Path | None) -> None:
    if checksum is None:
        candidate = archive.with_name(archive.name + ".sha256")
        checksum = candidate if candidate.exists() else None
    if checksum is None:
        return
    line = checksum.read_text(encoding="ascii").strip()
    fields = line.split()
    if len(fields) != 2 or fields[1].lstrip("*") != archive.name:
        raise VerificationError("checksum sidecar must contain '<sha256>  <archive-name>'")
    if fields[0] != sha256(archive):
        raise VerificationError("archive checksum mismatch")



def hash_stream(source, limit: int) -> str:
    digest = hashlib.sha256()
    consumed = 0
    while chunk := source.read(1024 * 1024):
        consumed += len(chunk)
        if consumed > limit:
            raise VerificationError("archive entry exceeded its declared size limit")
        digest.update(chunk)
    return digest.hexdigest()

def validate_manifest(raw: bytes, expected_platform: str) -> dict[str, object]:
    try:
        manifest = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise VerificationError("package manifest is not valid UTF-8 JSON") from error
    if not isinstance(manifest, dict):
        raise VerificationError("package manifest must be an object")
    if manifest.get("format") != "snow-desktop-package-v1":
        raise VerificationError("unknown package manifest format")
    if manifest.get("platform") != expected_platform:
        raise VerificationError("manifest platform does not match archive")
    for binary in ("desktop", "snow"):
        value = manifest.get(binary)
        if not isinstance(value, dict) or not isinstance(value.get("sha256"), str):
            raise VerificationError(f"manifest {binary} checksum is missing")
    snow = manifest["snow"]
    assert isinstance(snow, dict)
    check = snow.get("compatibility_check")
    if not isinstance(check, dict) or check.get("rpc_protocol_version") != "1":
        raise VerificationError("manifest does not record a successful RPC v1 check")
    return manifest


def validate_zip(archive: Path) -> tuple[dict[str, object], int]:
    required = {
        "Snow Desktop.app/Contents/Info.plist",
        "Snow Desktop.app/Contents/MacOS/Snow Desktop",
        "Snow Desktop.app/Contents/MacOS/snow-desktop-bin",
        "Snow Desktop.app/Contents/Resources/snow",
        "Snow Desktop.app/Contents/Resources/package-manifest.json",
    }
    total = 0
    manifest_raw: bytes | None = None
    desktop_digest: str | None = None
    snow_digest: str | None = None
    with zipfile.ZipFile(archive) as source:
        infos = source.infolist()
        if len(infos) > MAX_ENTRIES:
            raise VerificationError("archive contains too many entries")
        names = set()
        for info in infos:
            name = safe_name(info.filename).as_posix().rstrip("/")
            names.add(name)
            if info.file_size > MAX_ENTRY_BYTES:
                raise VerificationError(f"archive entry exceeds 512 MiB: {name}")
            total += info.file_size
            if total > MAX_TOTAL_BYTES:
                raise VerificationError("archive expands beyond 1 GiB")
            mode = (info.external_attr >> 16) & 0o177777
            if stat.S_ISLNK(mode):
                raise VerificationError(f"archive contains a symlink: {name}")
            if name.endswith("package-manifest.json"):
                if info.file_size > MAX_COMMAND_BYTES:
                    raise VerificationError("package manifest exceeds 1 MiB")
                manifest_raw = source.read(info)
            elif name.endswith("MacOS/snow-desktop-bin"):
                with source.open(info) as binary:
                    desktop_digest = hash_stream(binary, MAX_ENTRY_BYTES)
            elif name.endswith("Resources/snow"):
                with source.open(info) as binary:
                    snow_digest = hash_stream(binary, MAX_ENTRY_BYTES)
        missing = required.difference(names)
        if missing:
            raise VerificationError("archive is missing: " + ", ".join(sorted(missing)))
    if manifest_raw is None or desktop_digest is None or snow_digest is None:
        raise VerificationError("archive binary or manifest could not be read")
    manifest = validate_manifest(manifest_raw, "macos")
    verify_binary_hashes(manifest, desktop_digest, snow_digest)
    return manifest, total


def validate_tar(archive: Path) -> tuple[dict[str, object], int]:
    total = 0
    manifest_raw: bytes | None = None
    desktop_digest: str | None = None
    snow_digest: str | None = None
    names: set[str] = set()
    root: str | None = None
    with tarfile.open(archive, mode="r:gz") as source:
        members = source.getmembers()
        if len(members) > MAX_ENTRIES:
            raise VerificationError("archive contains too many entries")
        for member in members:
            path = safe_name(member.name)
            if root is None:
                root = path.parts[0]
            if path.parts[0] != root:
                raise VerificationError("archive must contain exactly one top-level directory")
            name = path.as_posix().rstrip("/")
            names.add(name)
            if member.issym() or member.islnk() or member.isdev():
                raise VerificationError(f"archive contains a link or device: {name}")
            if member.size > MAX_ENTRY_BYTES:
                raise VerificationError(f"archive entry exceeds 512 MiB: {name}")
            total += member.size
            if total > MAX_TOTAL_BYTES:
                raise VerificationError("archive expands beyond 1 GiB")
            if not member.isfile():
                continue
            extracted = source.extractfile(member)
            if extracted is None:
                raise VerificationError(f"could not read archive entry: {name}")
            if name.endswith("package-manifest.json"):
                if member.size > MAX_COMMAND_BYTES:
                    raise VerificationError("package manifest exceeds 1 MiB")
                manifest_raw = extracted.read(MAX_COMMAND_BYTES + 1)
            elif name.endswith("libexec/snow-desktop/snow-desktop-bin"):
                desktop_digest = hash_stream(extracted, MAX_ENTRY_BYTES)
            elif name.endswith("libexec/snow-desktop/snow"):
                snow_digest = hash_stream(extracted, MAX_ENTRY_BYTES)
        if root is None:
            raise VerificationError("archive is empty")
    required_suffixes = {
        f"{root}/bin/snow-desktop",
        f"{root}/libexec/snow-desktop/snow-desktop-bin",
        f"{root}/libexec/snow-desktop/snow",
        f"{root}/share/applications/snow-desktop.desktop",
        f"{root}/share/icons/hicolor/scalable/apps/snow-desktop.svg",
        f"{root}/share/doc/snow-desktop/package-manifest.json",
    }
    missing = required_suffixes.difference(names)
    if missing:
        raise VerificationError("archive is missing: " + ", ".join(sorted(missing)))
    if manifest_raw is None or desktop_digest is None or snow_digest is None:
        raise VerificationError("archive binary or manifest could not be read")
    manifest = validate_manifest(manifest_raw, "linux")
    verify_binary_hashes(manifest, desktop_digest, snow_digest)
    return manifest, total


def verify_binary_hashes(
    manifest: dict[str, object], desktop_digest: str, snow_digest: str
) -> None:
    desktop = manifest["desktop"]
    snow = manifest["snow"]
    assert isinstance(desktop, dict) and isinstance(snow, dict)
    if desktop_digest != desktop["sha256"]:
        raise VerificationError("desktop binary does not match the manifest")
    if snow_digest != snow["sha256"]:
        raise VerificationError("Snow binary does not match the manifest")


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    parser.add_argument("--checksum", type=Path)
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    archive = args.archive.expanduser().resolve(strict=True)
    if not archive.is_file() or archive.stat().st_size > MAX_ARCHIVE_BYTES:
        raise VerificationError("archive must be a regular file no larger than 1 GiB")
    check_checksum(archive, args.checksum)
    if archive.name.endswith(".zip"):
        manifest, expanded = validate_zip(archive)
    elif archive.name.endswith(".tar.gz"):
        manifest, expanded = validate_tar(archive)
    else:
        raise VerificationError("archive must end in .zip or .tar.gz")
    print(
        json.dumps(
            {
                "archive": str(archive),
                "sha256": sha256(archive),
                "expanded_bytes": expanded,
                "manifest": manifest,
            },
            indent=2,
            sort_keys=True,
        )
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main(sys.argv[1:]))
    except (OSError, VerificationError, tarfile.TarError, zipfile.BadZipFile) as error:
        print(f"verify_desktop_archive.py: error: {error}", file=sys.stderr)
        raise SystemExit(2)
