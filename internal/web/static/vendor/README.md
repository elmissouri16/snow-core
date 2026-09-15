# Vendored browser assets

HTMX 2.0.10 is embedded and served locally. No CDN or Node runtime is used.
The vendored files are unmodified from the upstream version tag:

- https://github.com/bigskysoftware/htmx/tree/v2.0.10
- `dist/htmx.min.js` → `htmx-2.0.10.min.js`
- `LICENSE` → `htmx-LICENSE` (Zero-Clause BSD)

SHA-256:

```text
71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de  htmx-2.0.10.min.js
d3d2456f76414f2456104660ebd65aff1c04cd7966b942bdabd63f3cdb316a38  htmx-LICENSE
```

Updates require reviewing upstream changes and license, updating the pinned
filename/route/template/hash together, and verifying real browser navigation
under the shell's strict CSP and disabled HTMX history cache. The SSE extension
is not vendored yet because no live agent event stream is connected.
