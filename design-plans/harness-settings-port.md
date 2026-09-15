# Port the actual sectioned Harness Settings surface

Written against Snow db7b2185c86a6506006ff797dce3129e2de385f0 plus uncommitted web work; reference c291e7961a515f6d7af9304e7fd1d257929aef26 in dist/harness-reference/.

## Evidence chain
- Surface: bottom Settings in `internal/web/templates/pages.html`, owned by `static/shell.js` and `harness.css`.
- User asks to port menus and supporting surfaces after rejecting the first approximation.
- Reference `packages/client/ui-settings-general/src/client/SettingsRoot.{tsx,module.css}`, `GeneralSection.{tsx,module.css}`, `ui-theme/src/client/AppearanceRow.{tsx,module.css}`.
- Direct browser observation at localhost:3080: Settings opens a centered 800×692 modal at1512×740, not Snow's bottom links dropdown. Feature rows are mounted in sectioned navigation.
- Uncertainty: Snow intentionally lacks upstream provider/default permission/plugin/preset editors. Port available controls rather than copying unsupported features.

## Decision and reuse
Use native dialog for the actual modal boundary and reference composition: width800,max-width viewport−48,height min800/viewport−48,r32; nav188,padding22px12px0; nav row40/r12/font14x22; content header54; independent scrolling options padding0 24 24. Close28px, focus on open and restore trigger on close. Narrow layout must remain usable at360px.

Sections: General (real light/dark appearance preference and host privilege/profile explanation), Workspaces (real registered workspace navigation plus manage/add via existing host folder flow), Browser access (existing CSRF-protected pairing/revocation/sign-out capabilities). Reuse canonical forms/owners rather than duplicate field IDs. No fake Models/Plugins/Preset editors or permission controls. Retain direct access routes as progressive-enhancement fallbacks.

## Changes and validation
- `templates/pages.html`, optional `templates/settings.html`, `static/shell.js`, new settings CSS if cohesive: replace bottom details with real button/dialog; reuse current theme key and existing HTTP routes.
- Parent integrates asset allowlist/head order. Shared menu controller remains separate; modal menus must render above dialog if used.
- Preserve shell collapse/search/HTMX and current-session New action authority; no duplicate runtime handler.
- Validate actual template settings-open state at360/768/1280/1512, section switching, Escape/mask/X close, focus return, theme persistence, form actions/CSRF, and no auto network activity from opening General.
- Run Go HTTP tests and browser regression suites; include upstream MIT notice for adapted source.

## Stop conditions and documentation
This settings-port increment did not change the web runtime's then ask-only policy or add upstream authority options. The later, explicitly user-authorized composer permission selector adds Snow's Ask/Deny/Allow session policies; see `docs/security.md` for its current authority boundary, which still does not include upstream sandbox presets. If a section cannot reuse real capability, omit it instead of inventing controls. After verification update canonical web guide and retire its dropdown-Settings description.
