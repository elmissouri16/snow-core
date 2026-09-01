# Hide Snow's internal fake provider from the desktop presentation

Written against: `fa365dc65ff4c5ea001a42e7a0ecff08bab4f0a1` plus the current uncommitted Snow Desktop workspace redesign.

## Evidence chain

- Surface: Snow Desktop's empty-thread composer footer and provider chooser while the desktop runtime is launched with the internal `fake` provider.
- Problem: the user-supplied rendered screenshot shows `fake` as the prominent provider label and a synthetic `fake` picker row with `Status unknown · Not present in the current provider inventory`. That exposes a test adapter as if it were a user-configurable Snow provider and weakens Snow's product identity.
- Design evidence: the user explicitly requested removal of the fake provider from the UI. `desktop/README.md` documents `fake` as the credential-free local testing default, while real user providers come from Snow's authoritative `auth_providers` catalog. `desktop/src/provider_catalog.rs::build_provider_catalog` currently synthesizes an absent active provider, which is the direct source of the screenshot row.
- Owner: `desktop/src/provider_catalog.rs` owns bounded provider presentation; `desktop/src/workspace.rs::provider_catalog` owns the active desktop projection; `desktop/src/workspace/view.rs::render_composer` and `render_provider_picker` consume it.
- Scope and affected surfaces: provider trigger label, provider picker rows/search/selection, model label/picker while the hidden test provider is active, focused provider-catalog/workspace tests, and desktop behavior documentation.
- Uncertainty: none. The exact canonical ID to hide is `fake`; unknown non-test custom provider IDs must remain visible.

## Design decision

Treat the exact canonical provider ID `fake` as an internal test/runtime adapter: preserve it for local fake-provider execution and automated tests, but never project its ID, synthetic status row, or fake model names into the interactive desktop UI. When it is the effective runtime provider, keep the real provider chooser available with a neutral `Choose provider` label; keep model presentation neutral until the user selects a real provider. Do not change Snow's runtime default, provider protocol, provider replacement semantics, or fake-provider test coverage.

## Reuse

- Existing bounded `ProviderCatalogItem`, `build_provider_catalog`, `search_provider_catalog`, and `provider_label` presentation layer.
- Existing `auth_providers` RPC inventory and `Workspace::select_provider` replacement path.
- Existing disabled/neutral composer-control vocabulary and `cx.theme()` roles.
- Exemplar: configured real-provider rows in `render_provider_picker`; those remain the only user-facing provider choices.

No new dependency, provider registry, or runtime configuration owner is required.

## Changes

1. `desktop/src/provider_catalog.rs` — exclude the internal test adapter at the presentation boundary
   - Change: add one narrow, documented predicate for whether a provider ID is user-visible; return false only for the canonical `fake` ID.
   - Change: skip `fake` entries supplied by `auth_providers` and do not synthesize an active row when the absent active provider is `fake`.
   - Change: resolve the hidden active ID to the neutral label `Choose provider` rather than falling back to the raw ID. Preserve raw fallback labels for every other unknown/custom provider.
   - Preserve: bounded input, server ordering, deduplication, status/auth metadata, search behavior, custom providers, and selected-row semantics.
   - Verify: neither an explicit nor synthesized `fake` row can appear; an unknown real/custom active provider still appears and remains selectable.

2. `desktop/src/workspace.rs` and `desktop/src/workspace/view.rs` — keep test execution internal without leaking test labels
   - Change: use the presentation predicate for the footer's current-provider label and provider chooser.
   - Change: while `fake` remains the effective runtime provider, render a neutral model label and prevent fake model IDs from being opened/rendered in the model picker. Keep the provider chooser enabled whenever existing switching gates allow it so a real provider can be selected.
   - Change: once a real provider is selected and its correlated runtime replacement completes, restore the ordinary provider/model/thinking presentation from authoritative runtime state.
   - Preserve: fake-provider prompt execution for local tests, provider replacement shutdown ordering, RPC request correlation, authentication flows, real custom-provider fallback, model/thinking transitions, permission controls, and composer submission.
   - Verify: no visible footer, picker, empty state, status row, or model row contains `fake` during fake-runtime startup/readiness; selecting a real provider behaves exactly as before.

3. `desktop/src/provider_catalog.rs` tests and `desktop/src/workspace_tests.rs`
   - Change: add tests for explicit and synthesized `fake` filtering, neutral hidden-provider labeling, preservation of unknown custom providers, neutral model presentation under the fake runtime, and ordinary real-provider presentation after switching.
   - Preserve: existing fake-provider runtime fixtures and RPC integration coverage; do not rewrite functional tests to avoid the fake adapter.
   - Verify: tests establish that only presentation is hidden.

4. `desktop/README.md` and `desktop/PARITY.md`
   - Change: clarify that `fake` remains the credential-free test runtime but is intentionally absent from user-facing provider/model selectors; real choices come from `auth_providers`.
   - Preserve: documented local fake-provider launch and honest test behavior.
   - Verify: documentation does not claim removal of the fake runtime itself.

## Scope

- Inherit: empty and active composer states, expanded/collapsed sidebar, light/dark themes, startup/ready/failed connections, and real provider switching.
- Verify: an empty real provider inventory; real configured/unconfigured providers; an unknown custom provider; runtime startup with `SNOW_PROVIDER=fake`; provider replacement from fake to real.
- Exclude: deleting the fake provider implementation, changing `SNOW_PROVIDER`, changing Snow CLI defaults, changing RPC schemas, changing provider authentication, or hiding unknown non-test providers.

## Validation

- Product: launch with the fake runtime, confirm the composer says `Choose provider` and exposes no fake row/model ID, open the real provider chooser, switch to an available real provider, and confirm its provider/model/thinking state appears normally.
- Interface: validate empty and active threads at 1280×820 and 900×600, both sidebar widths, startup and ready states, and an empty provider catalog.
- System: confirm the runtime still uses the fake provider for credential-free tests and that only the desktop presentation projection filters it.
- Repository: `cargo fmt --manifest-path desktop/Cargo.toml -- --check` → clean; `cargo check --manifest-path desktop/Cargo.toml --all-targets` → success; `cargo test --manifest-path desktop/Cargo.toml` → success; `SNOW_TEST_BINARY="$PWD/snow" cargo test --manifest-path desktop/Cargo.toml --test rpc_integration -- --ignored --test-threads=1` → all real-Snow tests pass; `git diff --check` → clean.

## Stop conditions

- Stop if hiding `fake` requires removing or renaming the runtime provider, changing RPC/provider schemas, or weakening fake-provider integration coverage.
- Stop if a generic unknown/custom provider would also be hidden; the exception is exact and test-adapter-specific.

## Design documentation

- After acceptance and validation: record the hidden-test-adapter presentation rule in `desktop/README.md` and the provider-picker evidence row in `desktop/PARITY.md`.
