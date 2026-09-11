#!/usr/bin/env python3
"""Refresh/check the portable skill's explicit, repository-owned resource snapshot.

Does not run examples, install packages, activate plugins, or change source files.
From a copied skill, pass --repo /path/to/snow-core explicitly.
"""

import argparse
import hashlib
import json
from pathlib import Path
import sys

DOCS = (
    "plugins", "plugin-extensions", "plugin-workflows", "security",
    "configuration", "tool-routing", "subagents", "goals", "sessions",
    "sdk", "sdk-reference", "rpc", "user-input", "plan-mode", "skills",
)
EXAMPLES = (
    "agent-profiles", "workflow-guard", "project-helper-v2",
    "workspace-dashboard", "ui-studio", "workspace-notes", "prompt-recipes",
    "session-pilot", "review-team", "text-tools", "project-helper",
    "project-context", "todo-radar", "git-review",
)
SHARED = ("README.md", "TRY.md", "snow.d.ts", "snow-v2.d.ts", "smoke.py",
          "extensions_smoke.py", "try.sh")
SUFFIXES = {".md", ".js", ".ts", ".json"}
MAX_FILE_BYTES = 2 << 20


def sources(repo):
    result = {f"docs/{name}.md": f"docs/{name}.md" for name in DOCS}
    result.update({
        "internal/plugin/javascript/scaffold/snow.d.ts": "api/snow.d.ts",
        "internal/plugin/javascript/scaffold/fixtures.md": "api/fixtures.md",
    })
    for name in SHARED:
        source = f"examples/plugins/{name}"
        result[source] = source
    for name in EXAMPLES:
        root = repo / "examples/plugins" / name
        if not root.is_dir() or root.is_symlink():
            raise ValueError(f"missing or unsafe example: {root}")
        for item in sorted(root.rglob("*")):
            if item.is_symlink():
                raise ValueError(f"symlink in example: {item}")
            if any(part in {"node_modules", ".git", "dist"} for part in item.relative_to(root).parts):
                continue
            if item.is_file() and item.suffix in SUFFIXES:
                source = item.relative_to(repo).as_posix()
                result[source] = source
    return dict(sorted(result.items()))


def snapshot(repo):
    files = {}
    records = []
    for source, destination in sources(repo).items():
        path = repo / source
        if not path.is_file() or path.is_symlink() or not path.resolve().is_relative_to(repo):
            raise ValueError(f"missing or unsafe source: {source}")
        if path.stat().st_size > MAX_FILE_BYTES:
            raise ValueError(f"oversized resource: {source}")
        content = path.read_bytes()
        content.decode("utf-8")
        files[destination] = content
        records.append({"source": source, "resource": destination,
                        "sha256": hashlib.sha256(content).hexdigest()})
    files["SOURCES.json"] = (json.dumps({
        "version": 1,
        "note": "Snapshot of the listed checkout files; hashes, not a released-version compatibility guarantee.",
        "files": records,
    }, indent=2) + "\n").encode()
    return files


def sync(repo, skill, check=False):
    files = snapshot(repo)
    references = skill / "references"
    changed = []
    for relative, expected in files.items():
        destination = references / relative
        if not destination.resolve().is_relative_to(references.resolve()):
            raise ValueError(f"resource escapes skill: {relative}")
        if destination.is_symlink():
            raise ValueError(f"symlink destination: {relative}")
        actual = destination.read_bytes() if destination.is_file() else None
        if actual == expected:
            continue
        changed.append(relative)
        if not check:
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(expected)
    # Reject stale generated resources rather than silently retaining obsolete API
    # claims. Never remove files automatically; the maintainer reviews each one.
    stale = []
    for directory in ("api", "docs", "examples"):
        root = references / directory
        if root.exists():
            stale.extend(p.relative_to(references).as_posix() for p in root.rglob("*")
                         if p.is_file() and p.relative_to(references).as_posix() not in files)
    if stale:
        raise ValueError("unexpected resources (review/remove explicitly): " + ", ".join(sorted(stale)))
    if check and changed:
        print("Resource drift:\n" + "\n".join(changed), file=sys.stderr)
        return 1
    print(f"{'Verified' if check else 'Synced'} {len(files) - 1} resources; {len(changed)} changed.")
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[5])
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    repo = args.repo.resolve()
    if not (repo / "go.mod").is_file() or not (repo / "docs/plugin-extensions.md").is_file():
        parser.error("--repo must point to a Snow source checkout")
    try:
        return sync(repo, Path(__file__).resolve().parents[1], args.check)
    except (OSError, ValueError) as error:
        print(f"Resource sync failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
