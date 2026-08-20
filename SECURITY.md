# Security policy

Snow handles provider credentials and can execute tools and extensions with the
current user's operating-system privileges. This file explains how to report a
suspected vulnerability. For the product threat model and operating boundaries,
read [`docs/security.md`](docs/security.md).

## Supported versions

During the alpha period, only the most recent published alpha release receives
security fixes. Older alpha builds may be asked to upgrade before a report can
be investigated or fixed.

## Report a vulnerability

Do not disclose an exploitable vulnerability in a public issue, discussion,
pull request, tool transcript, or log.

Use GitHub's private vulnerability reporting flow:

<https://github.com/elmissouri16/snow-core/security/advisories/new>

Include, when available:

- the affected Snow version, operating system, and architecture;
- the provider, tool, extension, SDK, or protocol surface involved;
- minimal reproduction steps and the observed impact;
- whether the issue requires a trusted project, enabled extension, or approval;
- suggested mitigations or a patch, if you have one.

Remove API keys, OAuth tokens, cookies, sensitive headers, private prompts,
provider continuity data, and personal information. Use placeholders instead of
live credentials even when a credential has already been revoked.

## Response and disclosure

Maintainers will acknowledge a report when it is reviewed, coordinate questions
and remediation through the private advisory, and publish an update when a fix
or mitigation is ready. Alpha maintenance is best-effort and does not currently
carry a response-time SLA.

Coordinate public disclosure with the maintainers. Reporters may request credit
or remain anonymous. If private reporting is unavailable, open a public issue
that contains no exploit details or secrets and ask for a private contact path.

## Related documents

- [Security model](docs/security.md) — privileges, trust, and residual risks
- [Release policy](docs/releases.md) — supported alpha and remediation policy
- [Project README](README.md) — installation and security overview
