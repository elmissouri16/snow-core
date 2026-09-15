# Inspector React integration

Import `{inspection}` from `./inspection/index` and assign `window.SnowInspection`.
No `SnowVisibility` exports or tool-timeline rendering live here.

Keep the existing app-owned `<aside id="project-inspector" class="inspector project-inspector" aria-label="Project inspector" data-project="…" data-available="true|false" hidden>`.
Its only child is `<div data-react-inspection data-react-props="JSON"></div>`.
JSON shape: `{project:{id,name,path,available},csrf,live?:{session_id,provider,model}}`.
Use the existing escaped JSON bootstrap helper; no secrets beyond existing CSRF.
`init()` reads the current mount synchronously. The aside's hidden/role/inert
metadata remains exclusively app-owned. `opened()`/`select(name, project)` flush
synchronously; selecting Project before opening never starts Files inspection.
Close control retains `data-inspector-toggle`; app retains open/close and focus
return. The live-view owner must project its inspector heading/indicator aria
state when app `setInspector` changes the outer aside; no child mutation needed.

The mount wrapper should use `display: contents` to preserve the aside's existing
layout. Scope is fenced to the current live owner node and its project/instance/
session (when present), including late response-body completion. If app replaces
those attributes in-place during conversation switching, call `SnowInspection.init()`
after publishing the new owner metadata, alongside `SnowProcesses.init()`, and
refresh bootstrap `live` metadata if Project tab must reflect the new session.
Unavailable non-live `#live-session` placeholders need no instance/session values.

Focused new tests: `node --experimental-strip-types --test src/inspection/model.test.ts`
(from frontend). Parent owns adding that path to the configured test command and
running final generated-asset/native browser gates. Existing read-only inspection
browser tests should remain; no tool renderer is imported or duplicated.

`refresh({project_id, instance_id, session_id, provider, model}): boolean` is now
available after `init()` for authoritative identity replacement. It projects only
bounded session/model presentation into the Project tab and performs no read,
discovery, tab selection, opening, or activation. It rejects disconnected/moved
mounts, replaced DOM owners, mismatched registered project, and stale project /
instance / session identities. `InspectionRefresh` and `InspectionIdentity` are
exported types. No need to rewrite the original bootstrap's live metadata after
calling this presentation-only method successfully.
