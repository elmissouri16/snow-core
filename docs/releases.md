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

The interactive TUI also has an opt-in native updater under `/settings`. It
queries the same prerelease-aware GitHub Releases endpoint and accepts only the
same four archive targets and exact archive layout. It bounds metadata,
checksum, compressed, and expanded input; verifies SHA-256 and the staged
binary's exact reported version; and stages in the executable's directory
before atomic replacement. It never invokes `sudo`, follows a destination
symlink, or replaces a development build. Custom writable install destinations
are supported because eligibility is based on the running regular executable,
not a fixed path. Startup checks are disabled by default and run only in the
interactive TUI. Startup checks fetch release metadata only. Every available
update requires an explicit **Install update** or **Skip for now** decision
before the archive is downloaded; an approved install remains visible with
byte, percentage, verification, and installation progress.

After replacement, the current process still contains the old code. The TUI
therefore offers **Restart now** or **Later**. Choosing **Restart now** first
shuts Snow down gracefully and then asks the CLI to execute the new binary;
choosing **Later** uses the new executable on the next launch. An update-check
or pre-replacement installation failure is nonfatal and leaves the existing
executable intact. If replacement succeeds but the executable directory cannot
be durably synced, Snow reports that narrower post-replacement failure
explicitly instead of claiming a fully durable success.

## Release requirements

A release commit must pass one GitHub Actions `CI` workflow run and one
`Documentation` workflow run triggered by its push to `main`. Every release
changes `CHANGELOG.md`, so the path-filtered documentation gate always applies.
The alpha-tag workflow fails closed unless it can identify both completed
successful runs by the exact tagged commit SHA, `main` branch, `push` event, and
respective `ci.yml` or `pages.yml` workflow. It does not rerun either suite
after the immutable tag is created. Every third-party or GitHub-maintained
action is pinned to a reviewed full commit SHA; version comments document the
corresponding upstream major.

The gate includes:

- formatting and `go vet` on Linux;
- all Go tests and standalone SDK tests on Linux and macOS;
- focused native installer coverage on macOS;
- the full support-script suite on Linux;
- the Linux production-build and credential-free lifecycle smoke;
- the Linux race suite;
- the Linux performance-regression guard and its parser tests;
- installer syntax, checksum, and atomic-replacement tests;
- cgo-disabled cross-builds for every release target;
- the standalone Go SDK example on Linux;
- a reachable-code scan with the pinned `govulncheck` version.

The path-filtered `Documentation` workflow is the canonical rendered-site gate.
It builds and validates relevant pull requests, then builds, validates, and
deploys relevant `main` pushes. A pull request never uploads or deploys a Pages
artifact. The alpha-tag provenance gate requires its exact successful `main`
push run alongside CI.

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

Run affected checks while developing. Before the release push, run one local
baseline rather than launching overlapping full, race, and build suites in
parallel; the exact `main` CI run is the authoritative complete multi-platform
release gate:

```sh
git status --short
git diff --check
go test ./...
go vet ./...
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
python3 scripts/check_benchmarks.py
test -z "$(git ls-files .snow)"
git diff -- CHANGELOG.md README.md docs/ .github/workflows/
```

Use the additional affected-area commands in `AGENTS.md` and
`IMPLEMENTATION.md` when the release changes those areas. Run manual
live-provider/authentication smokes outside CI and record only secret-free
outcomes. Do not run multiple resource-intensive Go verification commands
concurrently on the same workstation; resource contention can produce
misleading process-killed failures and duplicate investigation work.

Review and stage only the intended release files—never use a broad add that
could include credentials, project-local `.snow` state, or unrelated changes.
For a changelog-only release commit:

```sh
git add -- CHANGELOG.md
git diff --cached --check
git diff --cached
git commit -m "chore(release): $tag"
release_commit=$(git rev-parse HEAD)
git push origin HEAD:main
```

Explicitly name any other intended paths if the release needs more than the
changelog. Query the `CI` workflow by the exact pushed commit instead of holding
`gh run watch` open:

```sh
gh run list \
  --workflow ci.yml \
  --commit "$release_commit" \
  --event push \
  --limit 10 \
  --json databaseId,headBranch,headSha,status,conclusion,url
```

Query the canonical rendered-documentation gate the same way; every release
commit changes `CHANGELOG.md`, so this run is required:

```sh
gh run list \
  --workflow pages.yml \
  --commit "$release_commit" \
  --event push \
  --limit 10 \
  --json databaseId,headBranch,headSha,status,conclusion,url
```

If either exact run is queued or in progress, keep its URL and query again
later. A local command timeout is not evidence that a workflow failed. Require
completed, successful `push` runs on `main` whose `headSha` equals
`$release_commit` before tagging; never approve merely the newest runs.

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
peels the annotated tag to its commit, confirms that commit is on `main`, and
requires the exact successful `main` push CI and Documentation runs. It then
builds all four targets in parallel, verifies checksums, and creates a GitHub
prerelease.

Observe the release workflow with another bounded exact-commit query:

```sh
gh run list \
  --workflow release-alpha.yml \
  --commit "$release_commit" \
  --limit 10 \
  --json databaseId,headBranch,headSha,status,conclusion,url
```

Do not use a blocking watch command under a shorter outer command timeout. If a
query reports `queued` or `in_progress`, report that state and URL rather than
calling it a failure.

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
`SHA256SUMS` before announcing it. Confirm the required `Documentation` workflow
on that `main` commit passed and inspect the published GitHub Pages release
guide.

Keep the completion report concise and link to durable workflow evidence rather
than reproducing command transcripts. Record:

- the release version, URL, and exact commit SHA;
- the main CI, Documentation, and alpha-release workflow URLs and conclusions;
- the expected archive/checksum asset count;
- checksum, current-platform binary, installer, and fake-provider smoke results;
- secret-free manual provider outcomes;
- working-tree and intentionally retained local-binary version state;
- only genuine remaining blockers.

### 5. Clean up or recover

From the original checkout, remove the temporary worktree when verification is
complete:

```sh
git worktree remove ../snow-release
```

If any pre-tag step fails, fix it before creating the tag. If the tag workflow
runs before the exact main CI and Documentation runs have completed, wait for
them and rerun the same failed workflow; never move the tag. Transient
hosted-runner failures may also be rerun against the same immutable tag. If the
tagged code, workflow definition, or published release is defective, do not
move or recreate it: document the problem, fix it on a new commit, and publish
the next alpha number.

## Publish an alpha

1. Update `CHANGELOG.md` with the version and release date.
2. Run the local baseline plus affected-area checks on the intended commit.
3. Complete the manual live-provider smoke checks and record only secret-free
   outcomes in the release notes.
4. Push the release commit and require its exact successful `main` push `CI`
   and `Documentation` runs; use one-shot status queries rather than a blocking
   watch.
5. Create and push an annotated tag matching `v0.1.0-alpha.N`.
6. The `Alpha release` workflow validates the tag and prior CI provenance,
   builds the four archives, smoke-tests the Linux amd64 binary, generates
   checksums, and creates a GitHub prerelease.
7. Download the published bundle on a clean host, verify `SHA256SUMS`, run
   `snow version`, and run a credential-free fake-provider prompt.

Do not move or recreate a published tag. A not-yet-ready provenance check or
transient hosted-runner failure may be rerun for the same tag; fix tagged code
or workflow defects on a new commit and use the next alpha number.

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
