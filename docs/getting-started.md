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
- [Choose permissions carefully](#choose-permissions-carefully)
- [Related documents](#related-documents)

## Install Snow

Run this command to install the latest published release:

```sh
curl -fsSL https://raw.githubusercontent.com/elmissouri16/snow-core/main/scripts/install.sh | sh
```

### Installation options

Export an option before running the installation command:

```sh
# Choose another absolute installation directory.
export SNOW_INSTALL_DIR="$HOME/bin"

# Install one reviewed release instead of resolving the latest release.
export SNOW_VERSION=v0.1.0-alpha.1

# Do not change a shell startup file.
export SNOW_NO_MODIFY_PATH=1
```

`SNOW_INSTALL_DIR` must be absolute and cannot contain control characters or a
colon. If you disable the automatic `PATH` update, add the selected directory
to `PATH` yourself.

Piping a remotely downloaded script into `sh` trusts the repository content. If
you need to review it first, download
[`scripts/install.sh`](https://github.com/elmissouri16/snow-core/blob/main/scripts/install.sh),
inspect the complete file, and run the reviewed copy locally. Release checksums
protect against corruption or mismatched assets, but they are not an
independent signature.

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

- No credential: `snow --provider opencode-zen`
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
snow --provider opencode-zen
```

Replace `opencode-zen` with `opencode-go`, `chatgpt`, or a configured provider
name when appropriate. A bare `snow` command uses the configured default
provider, which is `opencode-zen` in a fresh configuration.

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
