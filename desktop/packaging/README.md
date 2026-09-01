# Snow Desktop packaging

These assets create relocatable developer/release-candidate packages for the
independent Rust/GPUI client. Every package contains two executables:

- `snow-desktop-bin`, the native presentation client; and
- a separately built `snow` executable, launched only as `snow --mode rpc`.

Bundling does not embed, link, or duplicate Snow's Go agent loop. The launchers
select the bundled runtime through `SNOW_BINARY`; an explicit operator-provided
`SNOW_BINARY` still overrides it.

This is a local packaging path, not a publishing path. It does not create tags,
GitHub releases, signatures, notarization credentials, or update feeds. The
repository's canonical release policy remains [`../../docs/releases.md`](../../docs/releases.md).
The root guide currently says the existing CI/release workflows are manually
disabled; do not publish a desktop artifact while the required repository gates
are disabled or red.

## Inputs and trust boundary

`package_desktop.py` requires an explicit `--snow-binary`. Before copying it,
the script:

1. resolves a bounded regular executable file;
2. runs `snow version` with a timeout and bounded stdout/stderr;
3. starts a credential-free fake-provider RPC process in a private temporary
   home;
4. requires the first bounded JSONL frame to be RPC protocol v1 with the desktop
   client's complete required capability set (currently 30, including `context_report` and `messages_page`); and
5. checks both input executable formats with `file` against the requested OS
   and architecture.

The compatibility check receives a deliberately minimal environment, so API
keys and normal Snow configuration are not passed to the test process. The
archive records the input SHA-256 digests, secret-free handshake metadata, and
`file` descriptions in `package-manifest.json`. A sidecar `.sha256` covers the
finished archive. This proves which files were selected; it does not establish
source provenance. Build Snow from a reviewed commit and preserve that commit
and build log as separate release evidence.

Inputs and staging are bounded to 512 MiB per executable/file and 1 GiB total.
Commands and the RPC handshake have explicit output and duration limits.
Archives are written through a temporary path and atomically replaced. Setting
`SOURCE_DATE_EPOCH` makes unsigned archives reproducible from identical input
binaries.

## Prerequisites

All hosts need:

- Python 3.9 or newer;
- a current stable Rust toolchain;
- the `file` utility; and
- a compatible, executable Snow binary built for the same target.

### macOS

GPUI uses Metal. Building requires the normal Xcode platform components and
command-line tools (`xcode-select -p` and `xcrun clang`). The crate enables
runtime shader compilation, so the optional standalone Metal Toolchain is not
required for ordinary local builds. The packaging script builds a small native
launcher so Finder-launched apps can select the bundled Snow binary without a
shell wrapper.

The default launcher deployment target is macOS 12.0 and can be changed with
`--macos-deployment-target`. The Rust client and all of its dependencies must
also support the selected target; this script cannot lower their compiled
minimum after the fact.

### Linux

GPUI supports Linux and selects Wayland or X11 from the active session. A Linux
build host needs a C/C++ toolchain, `pkg-config`, and the development packages
reported by the pinned GPUI dependency graph. On Debian/Ubuntu-family systems,
a practical starting set is:

```sh
sudo apt-get install build-essential clang pkg-config \
  libfontconfig1-dev libfreetype-dev libvulkan-dev \
  libwayland-dev libx11-xcb-dev libxcb1-dev \
  libxcb-shape0-dev libxcb-xfixes0-dev \
  libxkbcommon-dev libxkbcommon-x11-dev
```

Package names vary by distribution and GPUI is pre-1.0; treat the current Cargo
build as the authority for additions. At runtime, install a working Vulkan
loader/driver, fontconfig, xkbcommon, and either a Wayland or X11 session. The
application's image-from-clipboard action additionally uses `wl-paste`
(`wl-clipboard`) on Wayland or `xclip` on X11. The rest of the client does not
require those clipboard helpers.

## Build and package

First build the reviewed Snow CLI through the repository's normal path. Then,
from the repository root:

```sh
# macOS arm64; build the Rust client and create a .app ZIP
SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)" \
python3 desktop/scripts/package_desktop.py \
  --platform macos \
  --arch arm64 \
  --snow-binary ./snow

# Linux amd64; run this on a Linux amd64 build host
SOURCE_DATE_EPOCH="$(git show -s --format=%ct HEAD)" \
python3 desktop/scripts/package_desktop.py \
  --platform linux \
  --arch amd64 \
  --snow-binary ./snow
```

The default output directory is `desktop/dist/`. Cargo is invoked with
`--locked --release` and a 30-minute timeout. To package a separately built
client artifact, provide `--desktop-binary`; both supplied binaries still have
their platform and architecture checked. `--target` selects a Cargo target
triple but does not install cross-linkers or native target libraries.

Inspect a non-mutating resolved plan with:

```sh
python3 desktop/scripts/package_desktop.py \
  --platform linux --arch amd64 --snow-binary ./snow --dry-run
```

Validate archive structure, expansion bounds, checksum, manifest, and embedded
binary hashes independently:

```sh
python3 desktop/scripts/verify_desktop_archive.py \
  desktop/dist/snow-desktop_0.1.0_linux_amd64.tar.gz
```

## Package layout and installation

The macOS ZIP contains `Snow Desktop.app`. Move the app to `/Applications` or
`~/Applications`. Unsigned local builds may require the normal Finder
right-click **Open** confirmation. Do not tell users to disable Gatekeeper or
strip quarantine metadata globally.

The Linux tarball is relocatable:

```text
bin/snow-desktop                         launcher
libexec/snow-desktop/snow-desktop-bin    GPUI client
libexec/snow-desktop/snow                 RPC runtime
share/applications/snow-desktop.desktop
share/icons/hicolor/scalable/apps/snow-desktop.svg
share/doc/snow-desktop/...
```

It can run directly from an extracted directory. For a per-user FHS-style
installation:

```sh
tar -xzf snow-desktop_0.1.0_linux_amd64.tar.gz
cd snow-desktop_0.1.0_linux_amd64
cp -R bin libexec share "$HOME/.local/"
update-desktop-database "$HOME/.local/share/applications" 2>/dev/null || true
```

Ensure `$HOME/.local/bin` is on `PATH`. The `.desktop` file intentionally uses
`Exec=snow-desktop`, so system packagers can choose their installation prefix
without patching an absolute build path.

Both launchers default `SNOW_PROJECT` to the user's home when it is unset. Set
`SNOW_PROJECT` before launch to select a different project. `SNOW_BINARY` can
select another compatible runtime without modifying the package.

## Optional signing and notarization

Unsigned packages are suitable for local development, not broad macOS
distribution. A release operator with a Developer ID Application certificate
can sign all nested executables and the app during packaging:

```sh
python3 desktop/scripts/package_desktop.py \
  --platform macos --arch arm64 --snow-binary ./snow \
  --codesign-identity 'Developer ID Application: Example Org (TEAMID)'
```

The command uses hardened-runtime signing, a trusted timestamp, and strict
post-sign verification. Credentials remain in the operator's keychain and are
never accepted by the script. Signing is not notarization. Submit the resulting
ZIP with an operator-managed `xcrun notarytool` keychain profile, wait for
success, and assess/staple the extracted app according to Apple's current
notarization procedure before re-archiving and regenerating the SHA-256
sidecar. Record only secret-free request IDs and outcomes. Never place Apple
credentials in command arguments, repository files, CI logs, or package
manifests.

Linux signing is distribution-specific. Publish detached signatures and
repository metadata through the project's future reviewed release workflow,
not through this local script.

## Verification

Packaging logic is network-free and covered with fake bounded executables:

```sh
python3 -m unittest discover \
  -s desktop/scripts/tests -p 'test_*.py' -v
python3 desktop/scripts/package_desktop.py --help
python3 desktop/scripts/verify_desktop_archive.py --help
```

The tests create and verify reproducible Linux archives, exercise the
relocatable launcher, reject every one-capability-omitted Snow handshake, assert dry-run is
non-mutating, and build a macOS app archive when run on macOS. They do not claim
a Linux GPUI compilation from a macOS host or perform signing/notarization.
