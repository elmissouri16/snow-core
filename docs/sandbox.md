# Sandboxed Bash with smolvm

Snow can route the model-facing `bash` tool through a persistent Linux virtual
machine managed by [smolvm](https://github.com/smol-machines/smolvm). The
machine is scoped to one exact canonical project directory and is optional.

This guide covers setup, environment profiles, lifecycle, persistence, resource
controls, project scoping, and recovery. See [Security](security.md) for the
complete threat model and [Configuration](configuration.md) for every global
setting.

> **Note:** The VM covers Bash only. It does not sandbox Snow itself, the file
> tools, providers, plugins, MCP servers, webfetch, or subagent orchestration.

## On this page

- [Overview](#overview)
- [Requirements and installation](#requirements-and-installation)
- [Environment profiles](#environment-profiles)
- [Lifecycle commands](#lifecycle-commands)
- [Project scoping and operator store](#project-scoping-and-operator-store)
- [Guest environment](#guest-environment)
- [Process behavior](#process-behavior)
- [Persistence and recovery](#persistence-and-recovery)
- [Troubleshooting](#troubleshooting)
- [Configuration](#configuration)
- [Related documents](#related-documents)

## Overview

### Bash-only boundary

The VM covers only model-facing Bash commands.

| Runs in the VM while active | Remains on the host |
|---|---|
| The built-in `bash` tool | Snow itself and provider traffic |
| Processes started by guest Bash | `read`, `write`, `edit`, `grep`, and `glob` |
| Guest package managers and their network traffic | Plugins and MCP servers |
| Guest files outside the project mount | `webfetch` and host-side subagent orchestration |

> **Caution:** The VM is not a whole-process sandbox. A read-only Bash mount
> does not prevent a separately approved host-side `write` or `edit` tool from
> changing the project.

### Routing indicator

Snow's wide TUI header continuously displays the Bash routing boundary:

- green `shell:vm` means model-facing Bash routes through smolvm;
- warning-yellow `shell:host` means Bash runs directly on the host.

## Requirements and installation

Snow supports the audited smolvm 1.8.x CLI line beginning at 1.8.1. macOS
needs smolvm's supported Hypervisor.framework environment and `mkfs.ext4` from
Homebrew's `e2fsprogs`; Linux needs usable KVM. Install the macOS disk
formatter before initialization:

```sh
brew install e2fsprogs
```

Snow adds the standard Apple Silicon and Intel Homebrew `e2fsprogs` sbin paths
to smolvm's process environment, including when the keg-only formula is not on
the shell `PATH`. A custom Homebrew prefix must put its `sbin` directory on
`PATH`. Snow checks this prerequisite before creating or starting a machine so
a first boot cannot appear successful with disk state that disappears on
restart.

When the default `smolvm` command is absent, an explicit sandbox initialization
can install Snow's pinned smolvm 1.8.1 release into the user's normal smolvm and
`~/.local/bin` locations. Snow verifies embedded checksums for the upstream
installer and release archive, disables shell-profile changes, and validates the
installed version. A custom `sandbox.executable` is never installed or replaced
automatically.

The default no-profile path stages the configured digest-pinned image over host
HTTPS before creating the machine. A local `.smolmachine` pack can be used when
registry bootstrap must remain offline.

## Environment profiles

Built-in profiles provide supervised, digest-pinned development environments:

| Profile ID | Environment |
|---|---|
| `ubuntu` | Minimal Ubuntu base with core system tools and apt |
| `go` | Official Go development environment |
| `node` | Official Node.js environment with npm |
| `python` | Official Python environment with the uv package manager |

Profiles are treated uniformly:

- every image is pinned by version and multi-platform image digest;
- every profile deliberately enables persistent guest networking so its package
  manager can reach registries;
- a profile may provide resource recommendations, which remain editable in the
  form and can be overridden with CLI flags;
- selecting another profile requires replacing the existing VM;
- profile identity, image, resources, mount policy, and network authority are
  persisted in the project association.

Custom and configured images are not profiles. They retain their separately
chosen network policy and global resource defaults.

## Lifecycle commands

### Initialize

Run the interactive TUI form with:

```text
/sandbox init
```

The form controls:

- environment profile or custom/configured image;
- virtual CPUs;
- memory in MiB;
- storage disk in GiB;
- root overlay disk in GiB;
- read-write or read-only project mount;
- guest networking for a custom image.

Controls:

```text
↑/↓ or Tab     Select a field
←/→            Change a value
Space          Toggle mount/network choices
Enter          Create the machine
Esc            Cancel
```

The form starts on the custom/configured image and preserves its configured
network choice. Moving the Environment field to a built-in profile is an
explicit profile and network-authority selection.

From the CLI:

```sh
# Configured default image, guest networking off.
snow sandbox init

# Built-in environment profile.
snow sandbox init --profile ubuntu
snow sandbox init --profile go
snow sandbox init --profile node
snow sandbox init --profile python

# Explicit resources. Memory is MiB; disks are GiB.
snow sandbox init --profile PROFILE \
  --cpus 4 \
  --memory 8192 \
  --storage 40 \
  --overlay 20

# Custom image or local pack.
snow sandbox init registry.example/image@sha256:<digest>
snow sandbox init --from ./dev.smolmachine

# Optional mount and network authority for custom sources.
snow sandbox init --read-only --network
```

`--profile` and an explicit image/pack source are mutually exclusive. Omitting a
resource flag uses the applicable profile or global default. An explicit
`--storage 0` or `--overlay 0` asks smolvm to use its own disk default, even
when the global Snow configuration contains a nonzero value.

Other init flags: `--guest-cwd` sets the guest project mount path (default from
global config), and `--from` treats the positional source as a local
`.smolmachine` pack rather than an image reference.

### Status

Inspect the current project's association:

```text
/sandbox
/sandbox status
```

or:

```sh
snow sandbox status
snow sandbox status --json
```

### Stop

```text
/sandbox stop
```

or:

```sh
snow sandbox stop
```

Stopping the VM:

- releases its running CPU and memory resources;
- preserves the VM, overlay, installed packages, and caches;
- atomically changes subsequent Bash routing to the host;
- never causes a Bash command to auto-start the VM.

Exiting Snow does not implicitly stop an active machine. Run `sandbox stop` when
you want to release VM resources but preserve its state.

### Start

```text
/sandbox start
```

or:

```sh
snow sandbox start
```

Starting restores VM routing and reuses the persistent machine state.

### Delete

Deletion is destructive and requires confirmation:

```text
/sandbox delete confirm
```

or:

```sh
snow sandbox delete --force
```

> **Caution:** Deletion removes the guest VM, overlay, installed packages, and
> guest caches. It does not delete the host project mounted at `/workspace`.

Plain `/sandbox delete` only displays the confirmation syntax; it does not
remove anything. Wait for the deletion-completed message before initializing
another machine.

### Switch profiles

Profiles cannot be hot-swapped because the image is the VM's filesystem base.
Switch profiles in the same TUI session with:

```text
/sandbox delete confirm
```

Wait for deletion to complete, then run:

```text
/sandbox init
```

Choose the new Environment value and submit the form. Switching deletes the old
guest packages and caches, while the host-mounted project remains intact.

The equivalent CLI workflow is:

```sh
snow sandbox delete --force
snow sandbox init --profile PROFILE
```

### Headless and SDK use

Headless callers can require an existing association with `--require-sandbox` or
explicitly bypass association loading with `--no-sandbox`. The Go SDK exposes
the corresponding `RequireSandbox` and `DisableSandbox` options plus a
secret-free `SandboxStatus()` snapshot.

These options select or validate routing; they do not expand the VM boundary
beyond Bash. See [Go SDK](sdk.md) for lifecycle and readiness behavior.

## Project scoping and operator store

Associations are keyed by exact canonical project path in:

```text
$SNOW_HOME/sandboxes.json
```

The file is operator-owned, atomically replaced, interprocess locked, and mode
`0600`. Parent associations do not apply to child projects. Running
`sandbox init` from a home directory and from a repository therefore creates two
separate machines.

A stopped current-project sandbox does not imply every smolvm machine is
stopped. List all machines with:

```sh
smolvm machine ls --json
```

Manage another project's Snow machine by running the command from that exact
project directory:

```sh
(cd /path/to/project && snow sandbox status)
(cd /path/to/project && snow sandbox stop)
```

An active smolvm `_boot-vm` process in Activity Monitor or `ps` belongs to a
running machine. A properly stopped machine has no VM PID. Stopping preserves
its disks; deleting removes them.

## Guest environment

CPU, memory, storage, and overlay values are creation-time settings. Use the TUI
form or CLI flags to override profile or global recommendations. Changing
resources currently requires deleting and recreating the VM.

Snow validates the limits before creating a machine: virtual CPUs must be in
`1..64`, memory in `128..262144` MiB, and storage/overlay disks in
`0..1048576` GiB.

Snow mounts exactly the canonical project directory into the guest at
`/workspace` by default (or the configured `guest_cwd`). Files changed there are
host files and survive sandbox deletion. The mount can be made read-only for
guest Bash.

Snow forwards only a strict environment-name allowlist into the guest:
`LANG`, `LC_ALL`, and `TERM` by default. It never forwards the wholesale host
environment, and it does not add a separate host-home, SSH-agent, Docker-socket,
smolvm-control, or credential-store mount. If the selected project root is the
home directory, the exact project mount naturally includes that directory. Host
and guest user IDs may differ, so software that distrusts repositories owned by
another numeric user may require an explicit per-tool trust configuration.

### Networking

Guest network authority is fixed when the association is created:

- built-in profiles always enable persistent guest networking;
- a custom or configured source enables it only when explicitly selected;
- networking stays enabled across stop/start;
- changing it requires deleting and recreating the association.

Guest network access is full outbound access provided by smolvm, not a
package-manager-only exception. It is independent of Snow's provider and
`webfetch` traffic, which remain host-side.

For no-profile initialization without guest networking, Snow downloads the
configured digest-pinned registry image on the host, writes a private bounded
Docker-save archive, imports it into smolvm, and deletes the temporary archive.

### Build failures and memory

If a guest build is killed without a language-level error, inspect memory before
assuming the source failed:

```sh
free -h
```

Increase the profile's memory in the next initialization or reduce the build's
parallelism. Interrupted downloads or out-of-memory compilation can also leave a
language cache incomplete; use that language's normal cache-cleaning command
before retrying.

Storage and overlay files are commonly sparse: their configured logical size can
be much larger than current host disk usage. Deleting the VM releases those
guest disks.

## Process behavior

Snow validates the pinned executable and version when an active runtime is
assembled and again before status, lifecycle, and exec operations. It rejects
older and unaudited future smolvm minor/major versions instead of assuming their
flag or default behavior. On Linux smolvm also requires usable KVM; on macOS it
requires its supported Hypervisor.framework setup.

While a published record is active, failure to resolve the pinned CLI, corrupt
state, VM or exec failure, timeout, or cancellation is a Bash error. Snow never
falls back to host Bash for that call.

`stop` and `start` are explicit policy changes:

- after a successful `sandbox stop`, Snow atomically persists the stopped state
  and routes subsequent Bash to the host until `sandbox start` restores VM
  routing;
- a failed start/stop state update retains or rolls back to the previous
  routing boundary.

Guest cancellation sends SIGINT to the smolvm process group first, then
SIGKILLs the entire launcher group after a bounded grace. Because guest-process
semantics still depend on smolvm, verify cancellation for critical long-running
workloads.

`delete --force` deletes the VM before removing the association; failures retain
the association's current routing policy (active remains fail-closed; stopped
remains explicit host routing). `delete --force --forget` is an explicit
recovery path for a VM removed outside Snow or a stale record. Successful
deletion or forgetting warns that subsequent Bash calls use the host.

## Persistence and recovery

A sandbox combines a persistent guest filesystem with one host mount.

### Preserved across stop/start

- guest-installed packages;
- language and package-manager caches;
- compiler and build caches;
- files under the guest root filesystem;
- guest configuration;
- storage and overlay disk contents.

Temporary-directory cleanup remains guest operating-system policy, so do not use
`/tmp` as the only copy of an important artifact.

### Stale association or externally removed VM

Use the explicit operator recovery path:

```sh
snow sandbox delete --force --forget
```

`--forget` removes Snow's association without requiring backend machine
deletion. Use it only when the VM was already removed outside Snow or state
cleanup is otherwise intentional.

## Troubleshooting

| Symptom | Cause | Resolution |
|---|---|---|
| "sandbox is already initialized" | An association already exists | Stop/start it, or delete with explicit confirmation before reinitializing |
| Stale association or externally removed VM | The VM was removed outside Snow | Run `snow sandbox delete --force --forget` |
| Built-in profile no longer matches its policy | Snow updated a profile's digest-pinned image or network policy | Delete and recreate the sandbox with the same profile; Snow fails closed until then |
| macOS machine cannot restart after stop | First boot completed without host `mkfs.ext4`, so persistent disks were not formatted | Install `e2fsprogs`, then delete and recreate the association |
| Active backend failure | smolvm, the machine, its record, or guest execution failed | The Bash call errors (fail closed); check status, then stop or delete explicitly |
| Updated binary is not applied | A running Snow process cannot load newly installed code | Quit and restart the TUI |

### Built-in profile updates

Built-in profiles are tied to exact audited image digests and network policy.
When a Snow update changes either value, an existing association cannot be
silently relabeled as the new profile: its VM was created under the old policy.
Snow therefore fails closed with an actionable stale-record error.

Delete and recreate the machine from the affected project directory:

```sh
snow sandbox delete --force
snow sandbox init --profile PROFILE
```

For example, use `--profile go` for the built-in Go sandbox. Host project files
remain in place, but guest-only packages, caches, and configuration are removed
with the old VM. If the VM was already removed outside Snow, use the explicit
association-only recovery path instead:

```sh
snow sandbox delete --force --forget
snow sandbox init --profile PROFILE
```

Stale profile policy for one project does not block unrelated projects in the
same operator store. Ordinary startup, status, and execution still reject the
stale current-project association until the operator deletes it.

### macOS restart failure details

smolvm 1.8.1 can complete a first image boot without host `mkfs.ext4`, while
its warnings report that the persistent storage and overlay disks could not be
formatted. After stop or host-process exit, the next boot can then fail with
`start background CMD`, `image not found`, or `connection closed` because the
image store was not preserved.

Install the formatter before retrying:

```sh
brew install e2fsprogs
```

Snow automatically exposes the standard Homebrew formula path to smolvm. If the
machine was already stopped after an unformatted first boot and still cannot
start, its missing image/overlay data cannot be reconstructed safely in place.
Delete and recreate that association:

```sh
snow sandbox delete --force
snow sandbox init --profile PROFILE
```

Files in the host project mount remain intact; guest-only installed packages and
caches must be recreated. Snow now blocks new boots before this failure mode can
create another apparently usable but non-persistent machine.

## Configuration

Global sandbox defaults live in the operator-owned Snow configuration:

```json
{
  "sandbox": {
    "executable": "smolvm",
    "default_image": "index.docker.io/library/ubuntu:24.04@sha256:<digest>",
    "cpus": 4,
    "memory_mib": 8192,
    "storage_gib": 40,
    "overlay_gib": 20,
    "guest_cwd": "/workspace",
    "env_allowlist": ["LANG", "LC_ALL", "TERM"]
  }
}
```

Only global operator configuration controls this policy. Project configuration
cannot create or weaken a sandbox association. See
[Configuration](configuration.md#smolvm-bash-sandbox-defaults) for defaults,
ranges, and precedence.

## Related documents

- [Security](security.md) — complete privilege and threat boundaries
- [Configuration](configuration.md) — sandbox fields and storage paths
- [Using Snow](using-snow.md) — all CLI and TUI commands and workflows
- [Go SDK](sdk.md) — embedding options and status API
- [Project README](../README.md#optional-smolvm-shell-sandbox) — quick start
