# Documentation site

Snow publishes its user, integration, extension, security, and maintainer guides
as a GitHub Pages site. The site is generated from the canonical Markdown in
this repository rather than maintaining a second copy of product behavior.

> **Note:** The deployment becomes publicly reachable after the repository is
> public, GitHub Pages is enabled with **GitHub Actions** as its source, and the
> `Documentation` workflow completes successfully.

## On this page

- [Published location](#published-location)
- [Enable GitHub Pages](#enable-github-pages)
- [Source and build](#source-and-build)
- [Deployment workflow](#deployment-workflow)
- [Local validation](#local-validation)
- [Security and trust](#security-and-trust)
- [Troubleshooting](#troubleshooting)
- [Related documents](#related-documents)

## Published location

The project site uses the repository-scoped URL:

```text
https://elmissouri16.github.io/snow-core/
```

The URL returns an error until Pages is enabled and the first deployment has
completed. A custom domain is not currently configured.

## Enable GitHub Pages

A repository administrator performs this one-time setup after making the
repository public:

1. Open **Settings**, then **Pages**.
2. Under **Build and deployment**, select **GitHub Actions** as the source.
3. Open the **Actions** tab and run the `Documentation` workflow, or push a
   documentation change to `main`.
4. Confirm that the `github-pages` environment reports the expected deployment
   URL.

Public repositories can use GitHub Pages without exposing a separate deployment
credential. The workflow uses GitHub's short-lived OpenID Connect token and the
repository-scoped `GITHUB_TOKEN` permissions required by the official Pages
actions.

## Source and build

Canonical guides remain under `docs/`, with the project overview and selected
maintainer references at the repository root. Site-only presentation files live
under `site/`:

| Path | Responsibility |
|---|---|
| `site/_config.yml` | Jekyll metadata, base URL, Markdown, and relative-link settings |
| `site/_layouts/` | Accessible page and landing-page structure |
| `site/_includes/navigation.html` | Shared guide navigation |
| `site/assets/css/style.css` | Responsive visual design and print styles |
| `site/index.md` | Documentation landing-page content |
| `scripts/build-pages.sh` | Allowlisted staging and generated front matter |

`scripts/build-pages.sh` creates a fresh staging directory and copies only the
site presentation, canonical documentation, the tracked Go SDK example, and
schemas that those documents reference. Raw JavaScript or Python plugin
fixtures are not included in the Pages artifact. It adds Jekyll front matter to
staged Markdown copies; it never rewrites the canonical source files. The build
refuses to replace an existing output directory.

The official `actions/jekyll-build-pages` action converts the staged Markdown to
static HTML. `jekyll-relative-links` preserves links between repository
Markdown files in the published output, and the configured `/snow-core` base
path keeps navigation and assets valid for a project Pages site.

## Deployment workflow

`.github/workflows/pages.yml` runs after relevant documentation or site files
change on `main` and can also be dispatched manually. It performs six bounded
steps:

1. Check out the exact repository commit.
2. Prepare the allowlisted site source with `scripts/build-pages.sh`.
3. Build the static output with GitHub's supported Jekyll environment.
4. Validate rendered links, fragments, assets, schemes, and byte/count bounds.
5. Upload one validated Pages artifact.
6. Configure Pages and deploy that artifact to the protected `github-pages`
   environment.

Every official action is pinned to a reviewed full commit SHA. The build job has
only `contents: read`; `pages: write` and `id-token: write` are scoped to the
deployment job. The workflow does not receive provider credentials or Snow auth
data. Deployment concurrency is serialized so two documentation publishes
cannot race.

## Local validation

From the repository root, validate staging, relative links, pinned actions, and
workflow structure without network access:

```sh
sh -n scripts/build-pages.sh
python3 -m unittest scripts.tests.test_pages -v
```

To inspect the exact Jekyll input, choose a new output directory:

```sh
./scripts/build-pages.sh /tmp/snow-pages-source
```

The builder intentionally fails when the destination already exists. Remove a
previous disposable output yourself before rerunning it; the script never uses
a recursive delete on a caller-provided path.

After producing `_site` with the official Jekyll image, validate the rendered
artifact exactly as the deployment workflow does:

```sh
python3 scripts/check-pages-output.py ./_site --base-path /snow-core
```

The normal repository verification also runs all support-script unit tests:

```sh
python3 -m unittest discover -s scripts/tests -p 'test_*.py' -v
```

CI runs the support-script tests on both Linux and macOS. A dedicated Linux CI
job also uses the same pinned Jekyll build action and rendered-output validator
as the Pages workflow, so pull requests fail before merge on Liquid, routing,
fragment, or artifact-bound regressions. The Pages workflow remains the only job
that uploads and deploys the validated artifact.

## Security and trust

Repository documentation is untrusted text, not executable Snow policy. The
Pages builder copies tracked and explicitly allowlisted files into a fresh
staging directory, and the published artifact contains static documentation;
it does not include `.snow` state, credentials, session databases, or provider
continuity data.

GitHub's build and deployment actions execute with the repository permissions
defined in the workflow. Keep action references pinned to full SHAs, review
updates before changing those pins, and do not add secrets to the Pages job.
The public site must link to `SECURITY.md` for vulnerability reporting rather
than encouraging disclosure through public issues.

## Troubleshooting

### The site returns 404

Confirm that the repository is public, Pages uses **GitHub Actions** as its
source, and the most recent `Documentation` workflow completed. A committed site
without an enabled Pages setting is not deployed automatically.

### Configure Pages fails

Open **Settings**, then **Pages**, and select **GitHub Actions**. Organization
policy or a repository plan can prevent Pages publication while the repository
is private; make the repository public or update that policy before retrying.

### A documentation link fails validation

Fix the canonical relative link or add the referenced tracked documentation,
example, schema, or workflow to the staging allowlist in
`scripts/build-pages.sh`. Do not silence the validation by copying the entire
repository into the public artifact.

### Styling or navigation is missing

Confirm that `_config.yml` retains `baseurl: /snow-core` and that layout asset
URLs use Jekyll's `relative_url` filter. Hard-coded root-relative asset paths
break when the site is hosted below the user domain root.

## Related documents

- [Documentation index](README.md) — canonical ownership and every guide.
- [Documentation style guide](style-guide.md) — source-writing conventions.
- [Release policy](releases.md) — artifacts, checksums, and release gates.
- [Security model](security.md) — privilege and trust boundaries.
- [Project README](../README.md) — installation and product overview.
