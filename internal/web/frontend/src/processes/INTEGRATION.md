# Processes React integration

Import `{processes}` from `./processes/index` and assign `window.SnowProcesses`.
Replace the complete old processes template contents with one empty
`<div data-react-processes></div>` at the same existing template location.
The owning `#live-session` retains `data-project`, `data-instance`, `data-session`
and the existing document CSRF input. `init()` synchronously mounts the launcher,
`#processes-dialog`, and `#managed-processes` details, including matching
`data-process-project/instance/session` metadata. Existing app delegation opens
and closes the same dialog and toggles details.open; bindDialog already binds
new React dialogs on opening. Do not leave old renderer scripts enabled.

Process stop confirmation is React-owned within this dialog (no window.confirm).
No mount/open starts a runtime or process. Only visible open details poll the
existing bounded list endpoint; log reads and Stop stay explicit. dispose retires
the complete scope, aborts requests and closes the old dialog.

Give the mount `display: contents` to retain existing launcher/menu and dialog
layout. Native browser fixtures previously accepting `window.confirm` must now
click `[data-process-stop-confirm]` explicitly (or `[data-process-stop-cancel]`);
process row `[data-process-stop]` only prepares the bounded confirmation.

Focused tests: `node --experimental-strip-types --test src/processes/model.test.ts`.
Parent owns adding it to the configured test command, final production bundling,
native process/execution tests and install-local. This scope never modifies app,
legacy JS, Go, templates or main bootstrap registration.
