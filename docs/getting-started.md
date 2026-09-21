# Install Snow and run your first prompt

This guide takes you from installation to a working Snow coding-agent session.
Snow supports macOS and Linux on amd64 and arm64. Prebuilt releases do not
require Go.

> **Note:** Snow is alpha software. Commands, configuration, and public APIs may
> change before v1.

## On this page

- [Install Snow](#install-snow)
- [Check the installation](#check-the-installation)
- [Try Snow without credentials](#try-snow-without-credentials)
- [Choose a provider](#choose-a-provider)
- [Start the interactive agent](#start-the-interactive-agent)
- [Check for updates](#check-for-updates)
- [Installation options](#installation-options)
- [Choose permissions carefully](#choose-permissions-carefully)
- [Related documents](#related-documents)

## Install Snow

Run this command to install the latest published release:

```sh
curl -fsSL https://raw.githubusercontent.com/elmissouri16/snow-core/main/scripts/install.sh | sh
```

The default location is `~/.local/bin/snow`. The installer verifies the release
checksum and binary version, then adds the directory to your shell path.
Review the [install script](https://github.com/elmissouri16/snow-core/blob/main/scripts/install.sh)
before running it if your environment requires it. Checksums verify integrity;
they are not an independent signature.

## Check the installation

Open a new terminal and run:

```sh
snow version
```

If your shell cannot find `snow`, restart the shell or confirm that the install
directory appears in `PATH`:

```sh
printf '%s\n' "$PATH"
```

The default executable path is `~/.local/bin/snow`.

## Try Snow without credentials

Use the deterministic fake provider to check the local agent loop without
sending a request to a hosted model:

```sh
snow --provider fake --no-session -p "hello"
```

This command should print a response and exit. It does not create a durable
session.

## Choose a provider

Choose the setup that matches the account or endpoint you want to use:

- OpenCode API key: `snow login opencode-go`
- ChatGPT subscription: `snow login chatgpt`
- Another compatible endpoint:
  `snow login openai-compatible --name NAME --base-url URL`

See [Providers](providers.md) for the complete setup and launch commands. Review
a provider's privacy and training policy before sending private code.

## Start the interactive agent

Launch Snow from the project you want the agent to work on, selecting the
provider you configured:

```sh
cd /path/to/project
snow --provider opencode-go
```

Replace `opencode-go` with `chatgpt` or a configured provider name when
appropriate. A bare `snow` command uses the configured default provider, which
is `opencode-go` in a fresh configuration. OpenCode Zen is disabled because its
models are restricted to OpenCode clients.

On the first launch in a project, Snow asks whether it may load project-local
configuration. This is a trust decision about input; it is not an operating
system sandbox.

Type a request in the composer and press Enter. For example:

```text
Explain the project structure and identify the main entry point.
```

Snow streams the response and asks before tools that require permission. Open
`/help` inside the TUI for current commands and keyboard shortcuts.

For a one-shot prompt without the full-screen interface, run:

```sh
snow -p "summarize this project"
```

## Check for updates

Open `/settings` and choose **Check for updates now**. To install a newer
release, use **Update now** and review the confirmation. After installation,
choose **Restart now** to resume your saved session, or **Later** to keep working.
An ephemeral `--no-session` conversation cannot survive a restart.

Startup checks are opt-in and only fetch release metadata. Snow never downloads
or installs an update automatically. Self-update supports writable, regular
non-symlink official release binaries; development builds are never replaced.
See [Using Snow](using-snow.md#use-slash-commands) for the full update behavior.

## Installation options

Export an option before running the install command:

| Variable | Purpose |
|---|---|
| `SNOW_INSTALL_DIR` | Set a different absolute installation directory |
| `SNOW_VERSION` | Pin an exact release, such as `v0.1.0-alpha.1` |
| `SNOW_NO_MODIFY_PATH=1` | Leave shell startup files unchanged |

For example:

```sh
export SNOW_INSTALL_DIR="$HOME/bin"
```

The directory must be absolute and cannot contain control characters or a
colon. If you disable the automatic path update, add the directory to `PATH`
yourself.

## Choose permissions carefully

Snow and every allowed process run with your operating-system privileges. Snow
does not provide a built-in process sandbox.

- Start interactive work in the default `ask` permission mode.
- Read tool details before approving filesystem, process, or network access.
- Use `--permission deny` for headless inspection when no trusted permission
  broker is available.
- Use a container, virtual machine, or operating-system policy when you need
  process isolation.

Read the [Security model](security.md) before enabling plugins, MCP servers,
subagents, or broad tool authority. Report suspected vulnerabilities through
the repository's private
[security policy](https://github.com/elmissouri16/snow-core/blob/main/SECURITY.md).

## Related documents

- [Providers](providers.md) — connect OpenCode, ChatGPT, or another endpoint.
- [Using Snow](using-snow.md) — learn TUI, CLI, and slash-command workflows.
- [Configuration](configuration.md) — set models, permissions, and themes.
- [Sessions and branches](sessions.md) — return to previous work.
- [Security model](security.md) — understand permissions and process authority.
