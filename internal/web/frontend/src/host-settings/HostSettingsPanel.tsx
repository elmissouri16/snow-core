import {useLayoutEffect, useState} from "react";
import {APIKeyPanel} from "../host-api-key/APIKeyPanel";
import {choices, draftRows, parseDefaults, parseProviders, request, saveBody} from "./model.ts";
import type {DraftRow, LoadedDefaults, Operation, ProviderModel, ProviderStatus, Target} from "./model.ts";

export interface HostSettingsPanelProps {
  csrf: string;
  enabled: boolean;
  apiKeyEnabled: boolean;
  projects: {id: string; name: string}[];
}
type Pending = "load" | "save" | "providers" | null;
interface View {
  target: Target;
  selectedProject: string;
  loaded: LoadedDefaults | null;
  rows: DraftRow[];
  writable: boolean;
  pending: Pending;
  status: string;
  providerStatus: string;
  providers: ProviderStatus[];
}
const initialView = (): View => ({
  target: {scope: "global", project: ""}, selectedProject: "", loaded: null, rows: [], writable: false, pending: null,
  status: "Not loaded. Opening General does not read host defaults or start a worker.", providerStatus: "", providers: [],
});
const sameTarget = (a: Target, b: Target) => a.scope === b.scope && a.project === b.project;
const displayPair = (value: ProviderModel) => `${value.provider || "inherited provider"} / ${value.model || "provider default"}`;

function DefaultRow({row, locked, update}: {row: DraftRow; locked: boolean; update: (row: DraftRow) => void}) {
  const title = row.name.replaceAll("_", " ");
  const explicit = row.saved.explicit === null ? "Inherited" : row.name === "provider_model" ? displayPair(row.saved.explicit!) : row.saved.explicit;
  const effective = row.name === "provider_model" ? displayPair(row.saved.effective) : row.saved.effective;
  const disabled = locked || row.op !== "set";
  return <div className="host-default-row">
    <strong>{title}</strong>
    <p className="fine">{`Explicit: ${explicit}. Effective: ${effective}. Source: ${row.saved.source}. Availability: not network verified.`}</p>
    <label>{title} operation<select data-host-edit="" value={row.op} disabled={locked}
      onChange={event => update({...row, op: event.target.value as Operation})}>
      <option value="unchanged">Leave unchanged</option><option value="set">Set explicit value</option><option value="reset">Reset to inherited default</option>
    </select></label>
    {row.name === "provider_model" ? <>
      <label>Provider (saved together with model)<input type="text" data-host-edit="" value={row.value.provider} maxLength={64} autoComplete="off" disabled={disabled}
        onChange={event => update({...row, value: {...row.value, provider: event.target.value}})} /></label>
      <label>Model ID<input type="text" data-host-edit="" value={row.value.model} maxLength={256} autoComplete="off" disabled={disabled}
        onChange={event => update({...row, value: {...row.value, model: event.target.value}})} /></label>
    </> : <label>{title} value<select data-host-edit="" value={row.value} disabled={disabled}
      onChange={event => update({...row, value: event.target.value})}>
      {choices[row.name].map(value => <option key={value} value={value}>{value}</option>)}
    </select></label>}
  </div>;
}

export function HostSettingsPanel({csrf, enabled, apiKeyEnabled, projects}: HostSettingsPanelProps) {
  const [view, setView] = useState(initialView);
  // This owner holds lifecycle/admission tokens only; all editable and displayed
  // state belongs to React. Synchronous admission also rejects duplicate events
  // before React commits a disabled control. No DOM queries or manual rendering.
  const [owner] = useState(() => ({live: false, generation: 0, pending: null as AbortController | null,
    target: {scope: "global", project: ""} as Target}));
  const projectsKey = JSON.stringify(projects.map(project => project.id));
  useLayoutEffect(() => {
    owner.live = enabled;
    setView(previous => ({...previous, pending: null, writable: false}));
    return () => {
      owner.live = false;
      owner.generation++;
      const pending = owner.pending;
      owner.pending = null;
      pending?.abort();
    };
  }, [owner, enabled, csrf, projectsKey]);

  const locked = !enabled || view.pending !== null;
  function setTarget(target: Target, selectedProject: string) {
    if (!owner.live || owner.pending) return;
    if (target.project && !projects.some(project => project.id === target.project)) return;
    // A refreshed server list must not strand the user on a removed project.
    if (!projects.some(project => project.id === selectedProject)) selectedProject = "";
    owner.target = target;
    owner.generation++;
    setView(previous => ({...previous, target, selectedProject, loaded: null, rows: [], writable: false,
      status: "Not loaded for this scope. Select Load host settings to continue."}));
  }
  function updateRow(row: DraftRow) {
    if (!owner.live || owner.pending) return;
    setView(previous => ({...previous, rows: previous.rows.map(current => current.name === row.name ? row : current)}));
  }
  function begin(action: Exclude<Pending, null>, target: Target) {
    if (!owner.live || owner.pending || !sameTarget(target, owner.target)) return null;
    const controller = new AbortController(), generation = ++owner.generation;
    owner.pending = controller;
    setView(previous => ({...previous, pending: action, writable: action === "providers" ? previous.writable : false}));
    const active = () => owner.live && owner.pending === controller && owner.generation === generation && sameTarget(owner.target, target);
    return {signal: controller.signal, active, finish: () => {
      if (!active()) return;
      owner.pending = null;
      setView(previous => ({...previous, pending: null}));
    }};
  }
  function projectAvailable(target: Target) {
    return target.scope === "global" || !!target.project && projects.some(project => project.id === target.project);
  }
  async function load() {
    if (!owner.live || owner.pending) return;
    const target = view.target;
    if (!projectAvailable(target)) {
      setView(previous => ({...previous, status: "Choose a registered project first."})); return;
    }
    const operation = begin("load", target);
    if (!operation) return;
    setView(previous => ({...previous, status: "Loading host defaults locally…"}));
    const query = new URLSearchParams({scope: target.scope});
    if (target.project) query.set("project", target.project);
    try {
      const data = await request(`/settings/host?${query}`, operation.signal, {headers: {Accept: "application/json"}});
      if (!operation.active()) return;
      const loaded = parseDefaults(data, target);
      setView(previous => ({...previous, loaded, rows: draftRows(loaded), writable: true,
        status: "Loaded. Edits apply only to future workers; current workers and new conversations within them are unchanged."}));
    } catch {
      if (operation.active()) setView(previous => ({...previous,
        status: "Unable to load. Existing form edits were preserved. Explicitly load again before saving."}));
    } finally { operation.finish(); }
  }
  async function save() {
    if (!owner.live || owner.pending || !view.writable || !view.loaded || !sameTarget(view.target, view.loaded) || !projectAvailable(view.target)) return;
    const target = view.target, body = saveBody(csrf, view.loaded, view.rows);
    if (!body) {
      setView(previous => ({...previous, status: "Choose an explicit set or reset operation first."})); return;
    }
    const operation = begin("save", target);
    if (!operation) return;
    setView(previous => ({...previous, status: "Saving defaults for future workers…"}));
    try {
      const data = await request("/settings/host", operation.signal, {method: "POST", headers: {Accept: "application/json", "Content-Type": "application/x-www-form-urlencoded"}, body});
      if (!operation.active()) return;
      const loaded = parseDefaults(data, target);
      setView(previous => ({...previous, loaded, rows: draftRows(loaded), writable: true,
        status: "Saved for future workers only. Existing workers and their new conversations are unchanged."}));
    } catch {
      if (operation.active()) setView(previous => ({...previous,
        status: "Save could not be confirmed; it may have completed. Edits are preserved, not replayed. Explicitly load and review the latest revision before retrying."}));
    } finally { operation.finish(); }
  }
  async function loadProviders() {
    const operation = begin("providers", view.target);
    if (!operation) return;
    setView(previous => ({...previous, providerStatus: "Checking local provider status…"}));
    try {
      const data = await request("/settings/providers", operation.signal, {headers: {Accept: "application/json"}});
      if (!operation.active()) return;
      const providers = parseProviders(data);
      setView(previous => ({...previous, providers,
        providerStatus: "Checked locally. No login, refresh, or provider network request was made."}));
    } catch {
      if (operation.active()) setView(previous => ({...previous,
        providerStatus: "Status unavailable. Explicitly load again to retry; no login was attempted."}));
    } finally { operation.finish(); }
  }

  return <section className="settings-group host-settings" data-host-settings="" data-enabled={String(enabled)} aria-busy={view.pending !== null}>
    <h4>Current session</h4>
    <p>Host defaults below do not change the active worker or a new conversation created inside that worker. Use the conversation’s reasoning controls for current-session settings.</p>
    <h4>Host defaults</h4>
    <p>Changes apply only to future worker activation. Loading uses a short-lived, runtime-free host control worker, not an agent. Availability is checked locally, never verified with a provider network request.</p>
    <input type="hidden" data-host-csrf="" value={csrf} />
    <div className="host-settings-target">
      <label>Defaults scope <select data-host-scope="" disabled={locked} value={view.target.scope}
        onChange={event => { const scope = event.target.value; if (scope === "global" || scope === "project") setTarget({scope, project: scope === "project" ? view.selectedProject : ""}, view.selectedProject); }}>
        <option value="global">Global defaults</option><option value="project">Project defaults</option>
      </select></label>
      <label data-host-project-label="" hidden={view.target.scope !== "project"}>Registered project <select data-host-project="" disabled={locked} value={view.selectedProject}
        onChange={event => setTarget({scope: view.target.scope, project: view.target.scope === "project" ? event.target.value : ""}, event.target.value)}>
        <option value="">Choose a registered project</option>
        {projects.map(project => <option key={project.id} value={project.id}>{project.name}</option>)}
      </select></label>
      <button type="button" className="button" data-host-load="" disabled={locked} onClick={() => { void load(); }}>Load host settings</button>
    </div>
    <p className="fine" data-host-status="" role="status" aria-live="polite">{view.status}</p>
    <form data-host-form="" hidden={!view.loaded} onSubmit={event => { event.preventDefault(); void save(); }}>
      <div data-host-fields="">{view.rows.map(row => <DefaultRow key={row.name} row={row} locked={locked} update={updateRow} />)}</div>
      <button type="submit" className="button primary" data-host-save="" disabled={locked || !view.writable}>Save defaults for future workers</button>
    </form>
    <h4>Provider status</h4>
    <p>Local credential presence or expiry does not establish network access or model availability.</p>
    <button type="button" className="button" data-host-providers-load="" disabled={locked} onClick={() => { void loadProviders(); }}>Load provider status</button>
    <p data-host-providers-status="" className="fine" role="status">{view.providerStatus}</p>
    <ul data-host-providers-list="">{view.providers.map(provider => <li key={provider.provider_id}>{`${provider.provider_id}: ${provider.state} — ${provider.reason === "anonymous_access" ? "anonymous access; " : ""}local check only, not network verified`}</li>)}</ul>
    <h4>Connect providers on the Snow host</h4>
    <p>Do not put passwords or API keys in CLI arguments. In a terminal on the Snow host, run the appropriate interactive login, then check local authentication:</p>
    <ul className="host-login-instructions">{["snow login opencode-go", "snow login opencode-zen", "snow login chatgpt", "snow login openai-compatible", "snow auth check"].map(command => <li key={command}><code>{command}</code></li>)}</ul>
    <APIKeyPanel csrf={csrf} enabled={apiKeyEnabled} />
  </section>;
}
