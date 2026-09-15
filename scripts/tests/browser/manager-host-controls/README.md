# Native manager host-control acceptance

Runs unmodified production HTML/JS/CSS against two actual `web.Run` listeners:
private ephemeral HTTP (API-key controls must remain disabled) and direct TLS
(native form pairing, settings, write-only API keys, and project operations).

The real manager starts cold CONTROL subprocesses. Those use the real dispatcher,
`hostcontrol`, `hostops`, and private clone helper. Only Git is replaced with a
fictional executable that makes **no network requests** and checks the actual
written clone ACK plus the durable manager child identity before recording its
execution. A separately and explicitly activated fake worker allows checking
that host defaults do not mutate the current worker. No model calls are needed.

## Run

Requires Go 1.27, Node with built-in `node:test`, and native Chrome. No npm install.
The default matrix is **320/1280 × dark/light**, with HTTP-disabled smoke coverage
and the complete TLS flow in every report.

```sh
node --test scripts/tests/browser/manager-host-controls/privacy.test.mjs
node scripts/tests/browser/manager-host-controls/run.mjs
```

Reuse an already compiled checkout test executable rather than competing for a
build slot:

```sh
SNOW_HOST_CONTROLS_BINARY=/absolute/private/snow-fixture.test \
  node scripts/tests/browser/manager-host-controls/run.mjs
```

Optional fixture selectors and output directory:

- `SNOW_HOST_CONTROLS_WIDTH=320|1280`
- `SNOW_HOST_CONTROLS_THEME=dark|light`
- `SNOW_HOST_CONTROLS_ARTIFACTS=/absolute/private/output`

Artifacts are safe assertion/route-name reports and screenshots captured only
when password fields are empty and canaries are absent from DOM, URL and browser
storage. No network bodies, cookies, pairing codes, key values, private runtime
files, or certificate/key material are copied. Private manager stores, auth,
configuration, Chrome profile and ephemeral TLS key are deleted on shutdown.

The self-signed TLS exception uses **only the generated certificate's SPKI hash**
in the fixture Chrome process. It does not install OS trust, disable TLS checks
globally, or touch an existing browser profile. Do not change this to
`--ignore-certificate-errors` or an OS trust-store write.

## Assertions

- HTTP cannot inspect, enter or submit API keys; opening Settings is passive.
- HTTPS pairing uses the actual login form, not injected cookies.
- Explicit global/project load, set and reset preserve inheritance and current
  worker instance/model/thinking. Local provider status is not network proof.
- Exact-provider API-key inspection, masked password input and explicit consent;
  missing consent clears without sending; successful save clears and retires the
  inspection. A second native tab consumes a fresh inspection, and stale original
  submission is rejected with 409, cleared, and never automatically retried.
- Native project parent selection, reviewed destination/effects and checkbox
  confirmation; real create/clone wait for separate registration, never activate.
- Cancel retains partial files; real host-side path replacement plus explicit
  reconciliation yields uncertainty; metadata dismissal preserves all files.
- Reopening/navigation never clones again. Narrow modal/inventory overflow and
  native keyboard focus are checked.

## Evidence status

Authored coverage is not browser execution evidence. The fixture certificate and
actual private HTTP/TLS-startup Go checks, Node privacy tests, and module syntax
checks have run. The initial parent-managed matrix passed both **320px** reports
(**52 assertions each**) but exposed a fixture keyboard-encoding defect in both
1280px reports: manually supplied macOS native keycodes caused a second native
Escape cancellation after Settings reopened. The fixture now leaves platform
translation to Chromium's CDP key/code fields. The corrected **1280px dark**
report passed **52 assertions, zero failures**, including a clean rerun without
diagnostic listeners. The subsequent parent-managed **320/1280 × dark/light**
matrix passed **208 assertions across four reports, zero failures**. All fixtures
remain private and fictional; this does not establish real-provider, external
Git-network, user-data or physical-device coverage.
