# Snow

A coding agent for your terminal. Work through a change, inspect the tools it
runs, and pick up the conversation later. Choose your model provider and keep
sessions on your machine.

Snow is written in Go. Its terminal UI, command-line modes, and embeddable SDK
share one streaming agent loop.

[Documentation](https://elmissouri16.github.io/snow-core/) ·
[Getting started](docs/getting-started.md) ·
[Releases](https://github.com/elmissouri16/snow-core/releases)

[![CI](https://github.com/elmissouri16/snow-core/actions/workflows/ci.yml/badge.svg)](https://github.com/elmissouri16/snow-core/actions/workflows/ci.yml)

> **Note:** Snow is alpha software. APIs, configuration, and file formats may
> change before v1.

## Quick start

Install on macOS or Linux, on amd64 or arm64. Go is not required:

```sh
curl -fsSL https://raw.githubusercontent.com/elmissouri16/snow-core/main/scripts/install.sh | sh
```

The installer verifies the release checksum and binary version, installs to
`~/.local/bin/snow`, and updates your shell path. Set `SNOW_NO_MODIFY_PATH=1`
to leave shell startup files unchanged. Restart your shell, then launch Snow
in a project:

```sh
cd /path/to/project
snow --provider opencode-zen
```

OpenCode Zen supports anonymous access. To use your ChatGPT account instead:

```sh
snow login chatgpt
snow --provider chatgpt
```

Type a task and press Enter. Snow asks before tools that need approval. Use
`/help` for commands, `/model` to choose a model, and `snow resume` to return
to saved work.

The [installation guide](docs/getting-started.md) covers reviewing the install
script, custom paths, updates, and a credential-free check. The
[provider guide](docs/providers.md) also covers OpenCode Go and
OpenAI-compatible endpoints.

## Work with Snow

| When you want to… | Start here |
|---|---|
| Learn the terminal controls | [Using Snow](docs/using-snow.md) |
| Continue or branch a conversation | [Sessions and branches](docs/sessions.md) |
| Investigate before making changes | [Plan Mode](docs/plan-mode.md) |
| Work toward a longer objective | [Thread Goals](docs/goals.md) |
| Add reusable instructions or tools | [Agent Skills](docs/skills.md), [MCP](docs/mcp.md), [Plugins](docs/plugins.md), [JavaScript extensions](docs/plugin-extensions.md) |
| Change models, permissions, or themes | [Configuration](docs/configuration.md) |

Snow runs tools with your operating-system privileges and has no built-in
process sandbox. Review the [security model](docs/security.md) before granting
broad authority or enabling extensions.

## Automate and embed

Use print mode for a single prompt or JSON mode for streamed events:

```sh
snow --permission deny -p "explain this project"
snow --mode json --permission deny -p "summarize recent changes"
```

For applications, use the [Go SDK](docs/sdk.md) and its
[runnable example](examples/sdk), or control a long-lived process through
[JSONL RPC](docs/rpc.md). All execution surfaces share tools, permissions, and
sessions. The optional [local web manager preview](docs/using-snow.md#try-the-local-web-manager-shell)
starts with `snow --mode web`: browse folders on the Snow host, register projects,
read saved sessions, and explicitly activate RPC-backed conversations with live
Markdown answers and plans, a public tool timeline, stop controls, approvals and
questions. Explicit **Queue next** follow-ups have an editable pending-work panel;
Stop or failure retains unsent work for review rather than automatically replaying it. Live updates use an instance-bound, read-only SSE subscription;
reconnects fetch a fresh public snapshot rather than replaying commands. Pending
questions and approvals take over the composer seat without losing its draft,
and reader-controlled scrolling keeps earlier text anchored during updates.
Host-backed model choices, conversation creation/renaming/switching,
authoritative Plan Mode controls and usage/context indicators share that worker.
Click the model selector to load a searchable, provider-grouped list directly;
filter by name, model ID or provider without a separate loading step.
The composer accepts bounded text/image attachments, browser-native `/` command
shortcuts, hierarchical `@` project file/folder selection, and `$` installed-skill
suggestions. Installed skills follow the workspace's saved next-start policy;
file contents are sent only with your prompt, not when merely browsing choices.
A read-only Files / Changes inspector shows bounded host file previews and Git
diffs without activating an agent. Browser pairing survives restarts; reconnects
never automatically replay work. Read-only **Activity** summarizes registered
projects without granting controls. **Organize workspaces** provides manager-only
labels, pins and archives; **Versions** previews saved conversation branches and
offers explicit idle Restore, not filesystem undo. Explicit **Start goal / Resume
goal** runs a branch-bound Thread Goal through the same serial agent loop, with
whole-run Stop and optional token budgets (not billing caps). A **Processes**
inspector lists managed handles, bounded logs and permission-gated Stop. Runtime
panels open from the header’s conversation-actions menu, with persistent Close
controls and internally scrolling bodies; Activity/Organize retain icon controls
in collapsed navigation. Process tools are enabled in the fixed worker profile;
plugins, MCP and subagents remain disabled. Skills are disabled unless explicitly
enabled when starting that worker. Historical **Edit & resend**, **Regenerate** and ordinary **Queue next**
remain available under their admission rules; Queue next is disabled during goal
runs. Project activation trust can be explicitly remembered across manager
restarts; later visits show compact Start/Resume controls, never auto-start.
Forget trust in Settings → Workspaces. This does not change tool permissions or
CLI extension trust. After installing an updated build, restart the manager and
its workers: reloading a browser does not update the running executable.
The current source also adds browser inventory/targeted revocation, runtime-free
global/project defaults and local provider status. Provider login and API-key
management stay on the Snow host rather than in the browser. Ordinary
`snow --mode web` needs no networking setup:
when a private host address is available, Snow automatically binds its first
active private IPv4 address (or IPv6 ULA) on port 7331 and also binds
`127.0.0.1:7331`. It serves the same manager directly on both exact origins and
prints organized access, pairing and security sections with both URLs. Local
browsing remains on localhost; LAN devices use `http://<private-ip>:7331`.
Interactive terminals that are wide enough also show a compact QR code for
opening that LAN URL from another device; the pairing code stays separate and is never embedded in the QR code.
With no private address it serves numeric loopback directly. Pairing-code
authentication remains required, but normal LAN traffic is plain HTTP and can be
observed by other devices on that network; use this only on
a trusted home/work LAN, never public Wi-Fi or the Internet. Snow has no Web
Manager TLS, certificate, saved-network-profile, DNS, or trusted-proxy path and
does not modify firewalls or routers. A private IP is not authentication. Explicit current-session
reasoning, branch/detached-conversation forks and rename, manual compaction,
native Steer (distinct from Queue next), and recorded-cost estimates reuse the
existing worker. Durable host create/anonymous-HTTPS-clone operations retain
explicit cancellation/review and separate registration, and never activate agents.
Empty-directory creation does not require Git. Lost steering receipts preserve
drafts and exact-run Stop, with explicit idle review rather than automatic retry.
Native browser-access, runtime-control and host-control matrices have passed
(12 reports, 616 assertions), alongside fresh local workflow/execution, layout
and conversation checks. These verify the bounded local source, not remote or
live-provider compatibility, reusable CI or release readiness. Using an updated
checkout requires a local build/install and manager/worker restart; verification
does not update existing user processes. Public-Internet access remains unsupported.
Automatic LAN HTTP is intentionally unencrypted and restricted to one assigned
private interface. Encrypted Web Manager deployment was removed and is
unsupported; broader real-device/network acceptance remains separate from
loopback tests.

## Development

Source builds require the Go 1.27 line; the available toolchain is Go 1.27rc3.
Ordinary Go builds use checked-in, embedded web assets and do not require Node.
Editing the React frontend requires Node >=22.12.0 (CI pins 24.16.0) and npm.
See [Web frontend development](docs/web-frontend.md) for the pinned React 19.3,
TypeScript 7 and Vite 8 toolchain, generated-asset workflow, standalone workbench,
and in-progress migration boundaries.

From the repository root:

```sh
go build -o snow ./cmd/snow
go test ./...
go vet ./...
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v

# Web transport unit tests (Node 22+; no browser or network):
node scripts/tests/browser/stream-client/run.mjs
# Web end-to-end and layout checks (Node 22+ and installed Chrome/Chromium):
node scripts/tests/browser/live-stream/run.mjs
node scripts/tests/browser/harness-layout/run.mjs
```

After frontend edits, explicitly rebuild and verify the generated tree before
building Go:

```sh
(cd internal/web/frontend && npm ci --ignore-scripts && npm run build && npm test && npm run check)
go build -o snow ./cmd/snow
```

`npm run check` builds into a temporary directory and compares filenames and
bytes; it never repairs checked-in assets. Review and commit the generated bundle
and third-party notices with their source changes. Rebuilding does not update a
running web manager: install the verified binary and explicitly restart the
manager/workers before testing that build. The React migration is not yet complete;
source-level ports and earlier native-browser evidence are not acceptance of the
latest generated artifact.

Read [AGENTS.md](AGENTS.md) for repository rules and affected-area checks.
[Architecture and roadmap](IMPLEMENTATION.md) explains package boundaries and
remaining work. [Maintainer guides](docs/maintaining.md) covers releases,
performance, documentation, and design history. Provider tests use local mocks;
real-provider checks remain manual. The live-stream fixture drives the real
agent/RPC/HTTP/browser path with a gated fake provider on loopback. The layout
runner passed all 2,058 viewport/state reports across light/dark, seven widths,
and normal/short heights in the recorded local run, including 84 enabled/unsupported
runtime-panel reports and 42 remembered-trust startup reports. It checks readable button labels, open dialogs, scrollable
bodies, collapsed navigation and visible header hit targets. It uses strict mocked
public DTOs with production templates/assets, not live-provider or whole-product
parity evidence. Reduced smoke runs do not replace that gate. See the
[local acceptance evidence](docs/web-manager-implementation-plan.md#expanded-controls-acceptance-evidence)
for the distinct executed suites. Set `SNOW_CHROME_BIN` for a nonstandard browser location.

## Related documents

- [Documentation index](docs/README.md): find a guide or complete reference.
- [Changelog](CHANGELOG.md): release history.
- [Security reporting](SECURITY.md): report vulnerabilities privately.
