#!/usr/bin/env python3
from __future__ import annotations

import argparse
import posixpath
import sys
from html.parser import HTMLParser
from pathlib import Path
from typing import Optional
from urllib.parse import unquote, urlsplit


MAX_FILES = 2_000
MAX_HTML_BYTES = 4 * 1024 * 1024
MAX_TOTAL_BYTES = 64 * 1024 * 1024
MAX_PAGE_REFERENCES = 20_000
MAX_TOTAL_REFERENCES = 100_000
MAX_PAGE_FRAGMENTS = 20_000
MAX_TOTAL_FRAGMENTS = 100_000
ALLOWED_EXTERNAL_SCHEMES = {"http", "https", "mailto", "tel"}
FORBIDDEN_PUBLIC_PATHS = {
    "README.html",
    "SECURITY.html",
    "IMPLEMENTATION.html",
    "AGENTS.html",
    "CHANGELOG.html",
    "bugs.html",
    "LICENSE",
    "docs/README.html",
    "docs/releases.html",
    "docs/pages.html",
    "docs/style-guide.html",
    "docs/performance.html",
    "docs/code-audit.html",
    "docs/codex-plan-mode-and-goals.html",
    "docs/lazy-mcp-implementation-plan.html",
    "docs/plugin-js-python-research.html",
    "docs/subagents-implementation-plan.html",
    "docs/tool-routing.html",
    "docs/tui-performance.html",
    "docs/chatgpt-auth-research.html",
    "docs/session-storage-internals.html",
}
FORBIDDEN_PUBLIC_PREFIXES = (".github/", "benchmarks/", "design-plans/")


class PageParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.references: list[tuple[str, str]] = []
        self.fragments: set[str] = set()
        self.reference_count = 0
        self.fragment_count = 0

    def handle_starttag(
        self, tag: str, attrs: list[tuple[str, Optional[str]]]
    ) -> None:
        for raw_name, value in attrs:
            if not value:
                continue
            name = raw_name.lower()
            if name in {"id", "name"}:
                self.fragment_count += 1
                if self.fragment_count <= MAX_PAGE_FRAGMENTS:
                    self.fragments.add(value)
            elif name in {"href", "src"}:
                self.reference_count += 1
                if self.reference_count <= MAX_PAGE_REFERENCES:
                    self.references.append((name, value))


def normalize_base_path(value: str) -> str:
    stripped = value.strip()
    if not stripped or stripped == "/":
        return ""
    return "/" + stripped.strip("/")


def route_for_file(site_root: Path, path: Path) -> str:
    relative = path.relative_to(site_root).as_posix()
    if relative == "index.html":
        return "/"
    if relative.endswith("/index.html"):
        return "/" + relative[: -len("index.html")]
    return "/" + relative


def resolve_site_file(site_root: Path, route: str) -> Optional[Path]:
    normalized = posixpath.normpath("/" + route.lstrip("/"))
    if normalized == "/":
        candidate = site_root / "index.html"
        return candidate if candidate.is_file() else None
    candidate = site_root / normalized.lstrip("/")
    if candidate.is_file():
        return candidate
    if candidate.is_dir() and (candidate / "index.html").is_file():
        return candidate / "index.html"
    html_candidate = Path(str(candidate) + ".html")
    if html_candidate.is_file():
        return html_candidate
    return None


def validate_site(site_root: Path, base_path: str) -> list[str]:
    if not site_root.is_dir():
        return [f"site directory does not exist: {site_root}"]

    files = sorted(path for path in site_root.rglob("*") if path.is_file())
    if len(files) > MAX_FILES:
        return [f"site contains {len(files)} files; limit is {MAX_FILES}"]
    total_bytes = sum(path.stat().st_size for path in files)
    if total_bytes > MAX_TOTAL_BYTES:
        return [f"site contains {total_bytes} bytes; limit is {MAX_TOTAL_BYTES}"]

    pages: dict[Path, PageParser] = {}
    failures: list[str] = []
    for path in files:
        relative = path.relative_to(site_root).as_posix()
        if relative in FORBIDDEN_PUBLIC_PATHS or relative.startswith(
            FORBIDDEN_PUBLIC_PREFIXES
        ):
            failures.append(f"forbidden public artifact path: {relative}")

    total_references = 0
    total_fragments = 0
    for path in files:
        if path.suffix.lower() != ".html":
            continue
        size = path.stat().st_size
        if size > MAX_HTML_BYTES:
            failures.append(
                f"{path.relative_to(site_root)}: {size} bytes exceeds HTML limit "
                f"{MAX_HTML_BYTES}"
            )
            continue
        parser = PageParser()
        try:
            parser.feed(path.read_text(encoding="utf-8"))
            parser.close()
        except (OSError, UnicodeError) as error:
            failures.append(f"{path.relative_to(site_root)}: cannot parse HTML: {error}")
            continue
        if parser.reference_count > MAX_PAGE_REFERENCES:
            failures.append(
                f"{path.relative_to(site_root)}: contains {parser.reference_count} "
                f"references; limit is {MAX_PAGE_REFERENCES}"
            )
        if parser.fragment_count > MAX_PAGE_FRAGMENTS:
            failures.append(
                f"{path.relative_to(site_root)}: contains {parser.fragment_count} "
                f"fragments; limit is {MAX_PAGE_FRAGMENTS}"
            )
        total_references += len(parser.references)
        total_fragments += len(parser.fragments)
        if total_references > MAX_TOTAL_REFERENCES:
            failures.append(
                f"site contains more than {MAX_TOTAL_REFERENCES} retained references"
            )
            return failures
        if total_fragments > MAX_TOTAL_FRAGMENTS:
            failures.append(
                f"site contains more than {MAX_TOTAL_FRAGMENTS} retained fragments"
            )
            return failures
        pages[path] = parser

    for page, parser in pages.items():
        page_route = route_for_file(site_root, page)
        page_directory = posixpath.dirname(page_route) + "/"
        for attribute, raw_reference in parser.references:
            parsed = urlsplit(raw_reference)
            scheme = parsed.scheme.lower()
            if scheme:
                if scheme not in ALLOWED_EXTERNAL_SCHEMES:
                    failures.append(
                        f"{page.relative_to(site_root)}: forbidden {attribute} scheme in "
                        f"{raw_reference!r}"
                    )
                continue
            if parsed.netloc:
                continue

            reference_path = unquote(parsed.path)
            if reference_path.startswith("/"):
                if base_path and reference_path != base_path and not reference_path.startswith(
                    base_path + "/"
                ):
                    failures.append(
                        f"{page.relative_to(site_root)}: local {attribute} escapes base path: "
                        f"{raw_reference!r}"
                    )
                    continue
                route = reference_path[len(base_path) :] if base_path else reference_path
                route = route or "/"
            elif reference_path:
                deployed_directory = (base_path or "") + page_directory
                deployed_route = posixpath.normpath(
                    posixpath.join(deployed_directory, reference_path)
                )
                if base_path and deployed_route != base_path and not deployed_route.startswith(
                    base_path + "/"
                ):
                    failures.append(
                        f"{page.relative_to(site_root)}: relative {attribute} escapes "
                        f"base path: {raw_reference!r}"
                    )
                    continue
                route = deployed_route[len(base_path) :] if base_path else deployed_route
                route = route or "/"
            else:
                route = page_route

            target = resolve_site_file(site_root, route)
            if target is None:
                failures.append(
                    f"{page.relative_to(site_root)}: broken {attribute}: {raw_reference!r}"
                )
                continue
            if parsed.fragment and target.suffix.lower() == ".html":
                target_parser = pages.get(target)
                fragment = unquote(parsed.fragment)
                if target_parser is not None and fragment not in target_parser.fragments:
                    failures.append(
                        f"{page.relative_to(site_root)}: missing fragment "
                        f"{fragment!r} in {raw_reference!r}"
                    )

    return failures


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Validate bounded internal links in a rendered GitHub Pages site."
    )
    parser.add_argument("site_dir", type=Path)
    parser.add_argument("--base-path", default="/snow-core")
    args = parser.parse_args()

    failures = validate_site(args.site_dir.resolve(), normalize_base_path(args.base_path))
    if failures:
        for failure in failures:
            print(f"pages check: {failure}", file=sys.stderr)
        return 1
    print(f"Validated rendered Pages site: {args.site_dir}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
