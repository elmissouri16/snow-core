# Manager host-controls browser fixture

This opt-in native-browser fixture verifies the Web Manager's runtime-free host
settings and project-operation flows over an isolated numeric-loopback HTTP
listener. It uses a private temporary Chrome profile, real pairing, and no
injected cookies.

The fixture intentionally contains no TLS exception, generated certificate,
trusted proxy, saved network profile, or browser API-key entry. Provider login
and API-key management remain host-terminal/RPC responsibilities.

Run from the repository root with the Chrome binary selected by the existing
browser-test environment:

```sh
node scripts/tests/browser/manager-host-controls/run.mjs
```

The fixture keeps its temporary manager registry, worker wrappers, pairing
credential, and browser profile private, removes them on exit, and refuses to
capture screenshots while sensitive form fields contain values.
