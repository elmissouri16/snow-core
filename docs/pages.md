# Documentation site

Snow publishes a concise task-oriented manual for installing, configuring, and
using the agent. The Pages artifact is assembled from an explicit allowlist; it
is not a mirror of the repository's protocol, implementation, research, or
maintainer documentation.

> **Note:** The site becomes reachable after GitHub Pages uses **GitHub
> Actions** as its source and the `Documentation` workflow completes.

## On this page

- [Published location](#published-location)
- [Public content](#public-content)
- [Source and build](#source-and-build)
- [Deployment workflow](#deployment-workflow)
- [Local validation](#local-validation)
- [Security and trust](#security-and-trust)
- [Troubleshooting](#troubleshooting)
- [Related documents](#related-documents)

## Published location

The repository-scoped site is:

```text
https://elmissouri16.github.io/snow-core/
```

A repository administrator enables it once:

1. Open **Settings**, then **Pages**.
2. Select **GitHub Actions** under **Build and deployment**.
3. Run the `Documentation` workflow or merge a documentation change to `main`.
4. Confirm the `github-pages` environment reports the expected URL.

## Public content

Pages is organized around user tasks:

- install Snow and run a first prompt;
- connect any supported provider;
- use sessions, Plan Mode, goals, and subagents;
- add Agent Skills, MCP servers, and plugins;
- configure common options and operate Snow safely;
- embed Snow through the supported Go SDK.

MCP and Agent Skills pages document how to configure and use those standards
with Snow. They do not duplicate the standards or Snow's implementation
internals. The provider guide gives `opencode-zen`, `opencode-go`, `chatgpt`,
and OpenAI-compatible profiles equal setup coverage.

Complete RPC, external plugin protocol, ChatGPT authentication, model-requested
input, and implementation references remain available in the GitHub repository.
They are not primary Pages routes.

The public guide allowlist is:

- `docs/getting-started.md`
- `docs/providers.md`
- `docs/using-snow.md`
- `docs/configuration.md`
- `docs/sessions.md`
- `docs/plan-mode.md`
- `docs/goals.md`
- `docs/subagents.md`
- `docs/skills.md`
- `docs/mcp.md`
- `docs/plugins.md`
- `docs/security.md`
- `docs/sdk.md`

`docs/README.md` is the complete repository documentation index and is not a
Pages route.

## Source and build

Canonical guides live under `docs/`. Site presentation lives under `site/`:

| Path | Responsibility |
|---|---|
| `site/_config.yml` | Jekyll metadata, base URL, and Markdown settings |
| `site/_layouts/` | Shared guide and homepage structures |
| `site/_includes/navigation.html` | Shared desktop and mobile navigation |
| `site/assets/css/style.css` | Responsive and print presentation |
| `site/index.md` | Homepage task directory |
| `scripts/build-pages.sh` | Explicit public staging allowlist |

`scripts/build-pages.sh` creates a fresh staging directory. It copies the site
shell, the allowlisted guides, and the tracked Go SDK example, then adds Jekyll
front matter to staged Markdown copies. It does not rewrite canonical sources
and refuses to replace an existing output directory.

The builder does not stage repository indexes, protocol schemas, architecture,
contributor instructions, release records, audits, research, implementation
plans, workflows, benchmarks, or unsupported SDK fixtures.

The official `actions/jekyll-build-pages` action renders the staged source.
`jekyll-relative-links` resolves links between staged Markdown files, and
`baseurl: /snow-core` keeps routes correct below the user-domain root.

## Deployment workflow

`.github/workflows/pages.yml`:

1. checks out the repository;
2. stages the explicit Pages allowlist with `scripts/build-pages.sh`;
3. renders the staged source with the pinned official Jekyll action;
4. runs `scripts/check-pages-output.py` against the rendered site;
5. uploads the artifact; and
6. deploys it with GitHub's official Pages actions.

Official actions are pinned to full commit SHAs. The workflow receives write
and OpenID Connect permissions only in the deployment job.

The reusable CI workflow runs the same build and rendered-output validation, so
release verification exercises the documentation path without publishing it.

## Local validation

Run the source and support-script tests from the repository root:

```sh
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
```

Stage the curated source into a new directory:

```sh
rm -rf ./_pages_source
./scripts/build-pages.sh ./_pages_source
```

Render it with the same pinned `actions/jekyll-build-pages` image or workflow
used by CI, then validate the result:

```sh
python3 scripts/check-pages-output.py ./_site --base-path /snow-core
```

Also inspect the homepage and representative guides at desktop and mobile
widths, and check print preview. Confirm that navigation is identical across
breakpoints, code blocks remain usable, heading links work, and print headings
remain visible.

## Security and trust

The workflow does not send repository contents to arbitrary third-party build
services. GitHub-hosted actions and Jekyll process the allowlisted files, so
their pinned revisions remain part of the release trust boundary.

Treat Markdown as untrusted build input. Avoid unsafe raw HTML, inline scripts,
untrusted remote includes, or secrets in examples. The renderer may create
anchors and links but must not weaken Snow's runtime permission, credential, or
filesystem protections.

## Troubleshooting

### The homepage shows the repository index

Confirm that Pages uses **GitHub Actions**, not branch deployment from
`main /docs`. The generated homepage comes from `site/index.md`.

### The builder rejects its output directory

Choose a path that does not exist or remove an old local staging directory.
Refusing replacement prevents accidental deletion of unrelated files.

### A rendered link is broken

Run `scripts/check-pages-output.py` with the configured `/snow-core` base path.
Fix the source link or add the specific user-facing dependency to the allowlist.
Do not copy the entire repository into the artifact.

### Styling or navigation is missing

Keep `baseurl: /snow-core` in `site/_config.yml` and use Jekyll's `relative_url`
filter for assets and site routes.

## Related documents

- [Getting started](getting-started.md) — canonical first-run guide.
- [Providers](providers.md) — supported provider setup.
- [Repository documentation index](README.md) — complete public and maintainer
  documentation ownership.
- [Documentation style guide](style-guide.md) — writing conventions.
- [Security model](security.md) — privilege and trust boundaries.
- [Release policy](releases.md) — verification and publication gates.
