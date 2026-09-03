# Release policy

This guide defines Snow's alpha versioning, compatibility, verification, and
binary distribution policy. It applies to the Go CLI, JSONL RPC protocol, and
Go SDK in this repository.

> **Note:** Alpha releases are suitable for evaluation and early integration.
> Public APIs, RPC details, configuration, and persisted formats may change
> before v1, with changes recorded in release notes.

## On this page

- [Versioning and compatibility](#versioning-and-compatibility)
- [Supported targets](#supported-targets)
- [Install a published release](#install-a-published-release)
- [Release requirements](#release-requirements)
- [Next-release runbook](#next-release-runbook)
- [Publish an alpha](#publish-an-alpha)
- [Artifacts and checksums](#artifacts-and-checksums)
- [Support and rollback](#support-and-rollback)
- [Related documents](#related-documents)

## Versioning and compatibility

Alpha tags use `vMAJOR.MINOR.PATCH-alpha.N`, beginning with
`v0.1.0-alpha.1`. The Go module tag, GitHub release tag, binary-reported
version, and release heading must agree. Untagged builds report `0.1.0-dev`
unless `SNOW_VERSION` or the release linker flag supplies another value.

Snow follows semantic versioning for releases, but the `0.x` line does not
promise backward compatibility. A release may change `pkg/snowsdk`,
`pkg/protocol`, RPC details, configuration, plugin/MCP integration details, or
the SQLite schema when needed. Release notes must identify user-visible
breaking changes and any required migration or rollback steps. Exact session
history remains append-only under the runtime invariants documented elsewhere.

## Supported targets

Alpha binary archives are produced for:

| Operating system | Architecture | Archive target |
|---|---|---|
| Linux | amd64 | `linux_amd64` |
| Linux | arm64 | `linux_arm64` |
| macOS | amd64 | `darwin_amd64` |
| macOS | arm64 | `darwin_arm64` |

The source build currently requires Go 1.27; `go.mod` uses `1.27rc3` while that
is the toolchain available for the pinned dependency set. CI uses the same
release-candidate toolchain explicitly. Binary users do not need Go installed.

Windows is not currently supported.

## Install a published release

The public installer resolves the newest published GitHub release, including an
alpha prerelease, and selects the archive matching the host. Anonymous use
requires a public repository and at least one published GitHub Release; a Git
tag without an associated release is not installable:

```sh
curl -fsSL https://raw.githubusercontent.com/elmissouri16/snow-core/main/scripts/install.sh | sh
```

The one-line command requires `sh` plus a standard userland with `curl`,
`tar`, `uname`, `mktemp`, `rm`, `sed`, `awk`, `grep`, `cat`, and `wc`, and either
`sha256sum` or `shasum`. The installer bounds every download, verifies the selected
archive against the release's `SHA256SUMS`, validates the archive's exact paths,
regular-file member types, and declared/expanded size ceilings, checks the
version reported by the extracted binary, and atomically replaces
`${SNOW_INSTALL_DIR:-$HOME/.local/bin}/snow`.

After installation, the script adds one idempotent `export PATH=...` entry to
`.zshrc`, `.bashrc`, `.bash_profile`, `.bash_login`, or `.profile`, based on the
configured login shell and operating system. It honors an absolute `ZDOTDIR`
for Zsh and preserves Bash login-profile precedence on macOS. The entry takes
effect in a new shell. The installer does not invoke `sudo`. Set
`SNOW_NO_MODIFY_PATH=1` to skip the startup-file change.

Use `SNOW_VERSION` to select an immutable release instead of resolving the
latest one, and `SNOW_INSTALL_DIR` to choose another absolute destination. The
install path cannot contain control characters or the PATH-delimiter colon.
Export any values before running the installation command:

```sh
export SNOW_VERSION=v0.1.0-alpha.1
export SNOW_INSTALL_DIR="$HOME/bin"
export SNOW_NO_MODIFY_PATH=1 # optional
```

The one-line command trusts the installer currently stored on `main`. Operators
who require review or reproducibility should download and inspect the script
before execution and may fetch it from a reviewed immutable tag. `SHA256SUMS`
is delivered by the same GitHub release as the archive and therefore detects
transfer corruption or mismatched assets, but is not an independent signature.

## Release requirements

A release commit must pass the reusable GitHub Actions CI workflow. Every
third-party or GitHub-maintained action is pinned to a reviewed full commit SHA;
version comments document the corresponding upstream major. The gate includes:

- formatting and `go vet`;
- all Go tests on Linux and macOS;
- the Linux race suite;
- the Linux performance-regression guard and its parser tests;
- installer syntax plus mocked Linux/macOS, checksum, and atomic-replacement tests;
- cgo-disabled cross-builds for every release target;
- the standalone Go SDK example;
- a reachable-code scan with the pinned `govulncheck` version.

The default suite remains credential-free and uses local mock servers. Before a
maintainer creates a tag, manually smoke-test each advertised live provider and
authentication path for which credentials are available: OpenCode Go, an
OpenAI-compatible endpoint, and ChatGPT/Codex OAuth. Never put those credentials
in CI logs or release evidence.

Review the release diff, update `CHANGELOG.md`, verify documentation links, and
confirm that the working tree contains no private `.snow` configuration,
credentials, generated local plugins, or unrelated files.

## Next-release runbook

Use this procedure for every alpha after `v0.1.0-alpha.1`. Substitute the next
unused alpha number; never reuse or move a published tag.

### 1. Prepare a clean release worktree

Start from the intended `main` commit in a separate worktree so project-local
`.snow` state and unrelated changes cannot enter the release:

```sh
git fetch origin
git worktree add --detach ../snow-release origin/main
cd ../snow-release

tag=v0.1.0-alpha.2
version=${tag#v}
```

Confirm that the version does not already exist locally or remotely:

```sh
! git rev-parse -q --verify "refs/tags/$tag"
remote_tag=$(git ls-remote --tags origin "refs/tags/$tag") || exit 1
test -z "$remote_tag"
```

The local check expects `git rev-parse` to fail because the tag is unused. The
remote lookup must succeed and return no matching tag.

### 2. Prepare the release commit

Add a new topmost version section to `CHANGELOG.md` using the release date:

```text
## [0.1.0-alpha.2] - YYYY-MM-DD
```

Summarize user-visible changes, breaking changes, migrations, security fixes,
and known alpha limitations. Keep the Go requirement synchronized across
`go.mod`, `README.md`, and CI if the toolchain changed.

Run the verification commands in `AGENTS.md` and the expanded matrix in
`IMPLEMENTATION.md`. Run the manual live-provider/authentication smokes outside
CI and record only secret-free outcomes. Then audit the exact release changes:

```sh
git status --short
git diff --check
test -z "$(git ls-files .snow)"
git diff -- CHANGELOG.md README.md docs/ .github/workflows/
```

Review and stage only the intended release files—never use a broad add that
could include credentials, project-local `.snow` state, or unrelated changes.
For a changelog-only release commit:

```sh
git add -- CHANGELOG.md
git diff --cached --check
git diff --cached
git commit -m "chore(release): $tag"
git push origin HEAD:main
```

Explicitly name any other intended paths if the release needs more than the
changelog. Wait for the `CI` workflow on the pushed `main` commit to pass before
tagging.

### 3. Create the immutable annotated tag

Tag the exact commit that passed review and CI:

```sh
git tag -a "$tag" -m "Snow $tag"
test "$(git cat-file -t "$tag")" = tag
git show --format=fuller "$tag"
git push origin "$tag"
```

Pushing the tag starts `.github/workflows/release-alpha.yml`; do not invoke a
separate manual packaging process. The workflow validates the tag and changelog,
reuses CI, builds all four targets, verifies checksums, and creates a GitHub
prerelease.

### 4. Verify the published release

After the workflow succeeds, verify the release from a clean directory:

```sh
tmp=$(mktemp -d)
gh release download "$tag" --dir "$tmp" --repo elmissouri16/snow-core
(
  cd "$tmp"
  sha256sum --check SHA256SUMS       # Linux
  # shasum -a 256 -c SHA256SUMS     # macOS
)
tar -xzf "$tmp/snow_${version}_linux_amd64.tar.gz" -C "$tmp"
"$tmp/snow_${version}_linux_amd64/snow" version
(
  cd "$tmp"
  SNOW_HOME="$tmp/home" "./snow_${version}_linux_amd64/snow" \
    --provider fake --no-session -p "release smoke"
)
SNOW_VERSION="$tag" SNOW_INSTALL_DIR="$tmp/installed" ./scripts/install.sh
test "$("$tmp/installed/snow" version)" = "$version"
```

Review the GitHub prerelease title, notes, target commit, four archives, and
`SHA256SUMS` before announcing it. When release documentation changed, also
require the `Documentation` workflow on that `main` commit to pass and inspect
the published GitHub Pages release guide.

### 5. Clean up or recover

From the original checkout, remove the temporary worktree when verification is
complete:

```sh
git worktree remove ../snow-release
```

If any pre-publish step fails, fix it before creating the tag. If a pushed tag
or published release is defective, do not move or recreate it: document the
problem, fix it on a new commit, and publish the next alpha number.

## Publish an alpha

1. Update `CHANGELOG.md` with the version and release date.
2. Run the complete verification matrix from `AGENTS.md` and
   `IMPLEMENTATION.md` on the intended commit.
3. Complete the manual live-provider smoke checks and record only secret-free
   outcomes in the release notes.
4. Create and push an annotated tag matching `v0.1.0-alpha.N`.
5. The `Alpha release` workflow reuses CI for the exact tag, validates the tag,
   builds the four archives, smoke-tests the Linux amd64 binary, generates
   checksums, and creates a GitHub prerelease.
6. Download the published bundle on a clean host, verify `SHA256SUMS`, run
   `snow version`, and run a credential-free fake-provider prompt.

Do not move or recreate a published tag. If a workflow fails, fix the problem
on a new commit and use the next alpha number.

## Artifacts and checksums

Each GitHub prerelease contains these assets:

```text
snow_<version>_linux_amd64.tar.gz
snow_<version>_linux_arm64.tar.gz
snow_<version>_darwin_amd64.tar.gz
snow_<version>_darwin_arm64.tar.gz
SHA256SUMS
```

Each archive contains the `snow` binary, `README.md`, and `LICENSE`. Verify an
archive after downloading it:

```sh
sha256sum --check SHA256SUMS       # Linux
shasum -a 256 -c SHA256SUMS        # macOS
```

Alpha binaries are not currently code-signed or notarized. Checksums prove file
integrity against the GitHub release but are not an independent signature.

## Support and rollback

Only the latest alpha is supported. Security reports follow
[`SECURITY.md`](../SECURITY.md). Maintainers may mark a release as affected,
publish a fixed successor, and document mitigations; they do not silently
replace assets under an existing tag.

If a release contains a serious defect, mark it clearly in the GitHub release
notes and publish a new alpha. Go module versions may be retracted in `go.mod`
when necessary. Preserve old release notes and checksums for auditability unless
removal is required to contain active harm.

## Related documents

- [Project README](../README.md)
- [Security reporting policy](../SECURITY.md)
- [Security model](security.md)
- [Documentation site](pages.md)
- [Architecture and verification](../IMPLEMENTATION.md)
