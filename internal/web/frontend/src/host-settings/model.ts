export const choices = {
  thinking: ["off", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"],
  reasoning_summary: ["off", "auto", "concise", "detailed"],
  text_verbosity: ["low", "medium", "high"],
} as const;
export type Scope = "global" | "project";
export type StringField = keyof typeof choices;
export type FieldName = "provider_model" | StringField;
export type Operation = "unchanged" | "set" | "reset";
export interface Target { scope: Scope; project: string }
export interface ProviderModel { provider: string; model: string }
export interface DefaultValue<T> { explicit: T | null; effective: T; source: "builtin" | "global" | "project" }
export interface DefaultsGroup {
  provider_model: DefaultValue<ProviderModel>;
  thinking: DefaultValue<string>;
  reasoning_summary?: DefaultValue<string>;
  text_verbosity?: DefaultValue<string>;
}
export interface LoadedDefaults extends Target { revision: string; group: DefaultsGroup }
export type DraftRow =
  | {name: "provider_model"; op: Operation; value: ProviderModel; saved: DefaultValue<ProviderModel>}
  | {name: StringField; op: Operation; value: string; saved: DefaultValue<string>};
export interface ProviderStatus { provider_id: string; state: "configured" | "expired" | "unavailable"; reason: string; checked_locally: true }

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
export function providerID(value: unknown): value is string {
  return typeof value === "string" && /^[a-z0-9][a-z0-9_.-]{0,63}$/.test(value);
}
export function fieldNames(scope: Scope): FieldName[] {
  return scope === "global" ? ["provider_model", "thinking", "reasoning_summary", "text_verbosity"] : ["provider_model", "thinking"];
}
function pair(value: unknown, explicit: boolean): value is ProviderModel {
  return record(value) && (providerID(value.provider) || explicit && value.provider === "") &&
    typeof value.model === "string" && value.model.length <= 256;
}
function field<T>(value: unknown, scope: Scope, check: (value: unknown, explicit: boolean) => value is T): value is DefaultValue<T> {
  return record(value) && (value.source === "builtin" || value.source === "global" || scope === "project" && value.source === "project") &&
    check(value.effective, false) && (value.explicit === null || check(value.explicit, true));
}
export function parseDefaults(data: unknown, target: Target): LoadedDefaults {
  const invalid = () => new Error("Invalid host defaults projection");
  if (!record(data) || data.scope !== target.scope || (data.project_id ?? "") !== target.project ||
      typeof data.revision !== "string" || !data.revision || data.revision.length > 128 ||
      data.applies_to !== "future_runtime" || data.availability !== "not_network_verified") throw invalid();
  const group = data[target.scope];
  if (!record(group) || !field(group.provider_model, target.scope, pair)) throw invalid();
  const result: DefaultsGroup = {provider_model: group.provider_model, thinking: {explicit: null, effective: "off", source: "builtin"}};
  for (const name of fieldNames(target.scope)) {
    if (name === "provider_model") continue;
    const value = group[name];
    if (!field(value, target.scope, (candidate): candidate is string => typeof candidate === "string" && (choices[name] as readonly string[]).includes(candidate))) throw invalid();
    result[name] = value;
  }
  return {...target, revision: data.revision, group: result};
}
export function draftRows(loaded: LoadedDefaults): DraftRow[] {
  return fieldNames(loaded.scope).map(name => {
    if (name === "provider_model") {
      const saved = loaded.group.provider_model, current = saved.explicit ?? saved.effective;
      return {name, op: "unchanged", saved, value: {provider: current.provider || saved.effective.provider, model: current.model}};
    }
    const saved = loaded.group[name];
    if (!saved) throw new Error("Missing host defaults field");
    return {name, op: "unchanged", saved, value: saved.explicit ?? saved.effective};
  });
}
export function saveBody(csrf: string, loaded: LoadedDefaults, rows: DraftRow[]): URLSearchParams | null {
  const body = new URLSearchParams({csrf, scope: loaded.scope, expected_revision: loaded.revision});
  if (loaded.project) body.set("project", loaded.project);
  let changed = false;
  for (const row of rows) {
    if (row.op === "unchanged") continue;
    changed = true;
    body.set(`${row.name}_op`, row.op);
    if (row.op !== "set") continue;
    if (row.name === "provider_model") {
      body.set("provider", row.value.provider); body.set("model", row.value.model);
    } else body.set(row.name, row.value);
  }
  return changed ? body : null;
}
export function parseProviders(data: unknown): ProviderStatus[] {
  if (!record(data) || data.checked_locally !== true || !Array.isArray(data.providers) || data.providers.length > 128) throw new Error("Invalid provider status");
  const seen = new Set<string>();
  return data.providers.map((value: unknown) => {
    if (!record(value) || !providerID(value.provider_id) || seen.has(value.provider_id) || value.checked_locally !== true) throw new Error("Invalid provider status");
    const {state, reason} = value;
    if (typeof reason !== "string" || !(state === "configured" && ["credential_present", "anonymous_access"].includes(reason) ||
        state === "expired" && reason === "credential_expired" ||
        state === "unavailable" && ["credential_missing", "credential_invalid", "auth_store_unavailable"].includes(reason))) throw new Error("Invalid provider status");
    seen.add(value.provider_id);
    return {provider_id: value.provider_id, state: state as ProviderStatus["state"], reason, checked_locally: true};
  });
}
/** Read only bounded JSON; cancellation must never wait for an uncooperative body. */
async function readResponse(response: Response, signal: AbortSignal): Promise<unknown> {
  signal.throwIfAborted();
  const length = response.headers.get("Content-Length");
  if (length !== null && (!/^\d+$/.test(length) || Number(length) > 65536)) throw new Error("size");
  if (response.headers.get("Content-Type")?.split(";", 1)[0]?.trim().toLowerCase() !== "application/json") throw new Error("type");
  if (!response.body) throw new Error("body");
  const reader = response.body.getReader(), decoder = new TextDecoder("utf-8", {fatal: true});
  let bytes = 0, text = "";
  let rejectAbort: (reason: unknown) => void = () => {};
  const aborted = new Promise<never>((_, reject) => { rejectAbort = reject; });
  const abort = () => {
    rejectAbort(signal.reason);
    void reader.cancel().catch(() => {});
  };
  signal.addEventListener("abort", abort, {once: true});
  try {
    while (true) {
      signal.throwIfAborted();
      const {done, value} = await Promise.race([reader.read(), aborted]);
      signal.throwIfAborted();
      if (done) break;
      bytes += value.byteLength;
      if (bytes > 65536) throw new Error("size");
      text += decoder.decode(value, {stream: true});
    }
    text += decoder.decode();
    signal.throwIfAborted();
    return JSON.parse(text) as unknown;
  } finally {
    signal.removeEventListener("abort", abort);
    // Best effort only: stream cancellation itself can remain pending forever.
    void reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}

/** Explicit callers only. No polling, retries, login, refresh, or worker activation. */
export async function request(url: string, signal: AbortSignal, options: RequestInit = {}): Promise<unknown> {
  const deadline = AbortSignal.any([signal, AbortSignal.timeout(6500)]);
  deadline.throwIfAborted();
  const response = await fetch(url, {credentials: "same-origin", cache: "no-store", redirect: "error", ...options, signal: deadline});
  try {
    deadline.throwIfAborted();
    if (!response.ok || response.redirected) throw new Error(response.status === 409 ? "conflict" : "request");
    return await readResponse(response, deadline);
  } catch (error) {
    // Also retire bodies rejected before a reader was acquired (including late headers).
    void response.body?.cancel().catch(() => {});
    throw error;
  }
}
