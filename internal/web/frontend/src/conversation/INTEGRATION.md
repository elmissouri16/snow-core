# Conversation React integration contract

Facade: `import {conversation} from "./conversation"` and register as
`SnowConversation`; methods `init(root, hooks)`, `render(snapshot, controls)`,
`dispose()`, `select({project, session, instance, trigger})`, `rename(trigger)`.
No legacy `conversation.js` may execute alongside this facade.

## Empty foreign mount points (under `#live-session`)

- `[data-react-conversation="heading"]`: replaces **all** `live-heading` markup.
- `[data-react-conversation="leading"]`: replaces permission-policy and mode
  launchers (or non-workflow workspace chip), **not** composer context tools.
- `[data-react-conversation="trailing"]`: replaces model and telemetry launchers
  (or non-workflow model span), **not** Send / Stop / pending labels.
- `[data-react-conversation="dialogs"]`: replaces permission-policy, workflow
  switch and rename dialogs. Runtime Close dialog remains parent-owned.

All mounts must be empty and `display: contents` (parent CSS or inline style),
so existing flex/grid geometry and heading direct-child layout are preserved.
Mount all four even with WorkflowEnabled=false; flags choose fallback JSX.

Bootstrap `#live-session` attributes:
`data-workflow-enabled`, `data-permission-policy-enabled` (literal true/false),
`data-project-name`, `data-project-path`, `data-session-name`, `data-provider`,
`data-model`; existing `data-project`, `data-instance`, `data-session`,
`data-status` remain required. No additional HTTP/API.

## Parent writers to remove/project

1. app `applySnapshot` writes to `#live-status`, `#live-model`,
   `[data-live-title]`: remove; facade renders snapshot fields/status.
2. app `updateControls` write to `[data-runtime-close].disabled`: remove;
   pass `closeDisabled: !safe && !canCloseFailed()` in controls.
3. app inspector visibility's `.workspace-heading [data-inspector-toggle]`
   `aria-expanded` write: remove; pass
   `inspectorExpanded: !!panel && !panel.hidden`. Ensure inspector toggle calls
   facade `render`/updateControls after visibility changes. Keep delegated click
   behavior, focus restoration and inspector children with existing owners.
4. Keep existing control props `safe`, `connected`, `verified`, `busy`, `setting`,
   `invalid`, `status`; these do not transfer admission/known-ACK/draft logic.
5. Do not leave old conversation delegated document click/submit handlers active.
6. Other live-status/title/model readers (shell/sidebar/focus anchors) may remain:
   initialization/render use flushSync, preserving the synchronous DOM contract.

Runtime feature launchers remain resident with their owners. Conversation only
reads capability/hidden/disabled state and invokes `.click()`; never edits their
children. Goal's hidden details proxy is the deliberate exception to visible
launcher discovery: its owner advertises `data-goals-available`. Both menu
rendering and click-time revalidation use that same availability rule. The
primary Goal toggle stays composer-owned; menu delegation only opens Details.
SnowMenus remains the external geometry/focus host using open/close/reposition;
React owns the panel children and direct `.snow-menu-content` wrapper, never
calls reconcile, and unmounts its popup root from onClose. Conversation passes
`managedTrigger: true` to the menu host: JSX exclusively owns trigger
`aria-expanded` and `aria-controls`, using the canonical menu/trigger identity and
a stable popup ID assigned before synchronous publication. Opening, closing and
ordinary snapshot publication all project these attributes through React.

## Verification ownership

Conversation-only unit tests: `node --experimental-strip-types --test
src/conversation/model.test.ts` from frontend directory. Parent add this to
package test script. Parent owns bundle generation, native integration tests,
canonical migration docs and install-local after integrated verification.
