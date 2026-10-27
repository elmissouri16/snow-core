export type Identity = Readonly<{project_id: string; instance_id: string; session_id: string}>;

export type AuthStatus = Readonly<{state: string; method: string; account_id: string; expires_at: number; summary: string}>;
export type AuthMethod = Readonly<{id: string; display_name: string; kind: string}>;
export type AuthProvider = Readonly<{id: string; display_name: string; required: boolean; environment: readonly string[]; methods: readonly AuthMethod[]; status: AuthStatus}>;
export type AuthInventory = Identity & Readonly<{providers: readonly AuthProvider[]}>;
export type AuthProgress = Readonly<{kind: string; message: string; url: string; user_code: string}>;
export type AuthJob = Identity & Readonly<{job_id: string; provider_id: string; method: string; state: string; progress: readonly AuthProgress[]; status?: AuthStatus; error: string}>;

export type UI = Readonly<{readable: boolean; safe: boolean}>;

const record = (value: unknown): value is Record<string, unknown> => typeof value === "object" && value !== null;
const text = (value: unknown, limit = 4096): value is string => typeof value === "string" && value.length <= limit && !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(value);
const identifier = (value: unknown): value is string => typeof value === "string" && /^[A-Za-z0-9_-]{1,128}$/.test(value);

function sameIdentity(value: Record<string, unknown>, identity: Identity): boolean {
  return value.project_id === identity.project_id && value.instance_id === identity.instance_id && value.session_id === identity.session_id;
}

function validStatus(value: unknown): value is AuthStatus {
  return record(value) && ["missing", "configured", "expired", "invalid", "unknown"].includes(value.state as string) &&
    text(value.method, 64) && text(value.account_id, 256) && Number.isSafeInteger(value.expires_at) && text(value.summary);
}

export function validInventory(value: unknown, identity: Identity): value is AuthInventory {
  if (!record(value) || !sameIdentity(value, identity) || !Array.isArray(value.providers) || value.providers.length > 32) return false;
  return value.providers.every(provider => record(provider) && identifier(provider.id) && text(provider.display_name, 256) &&
    typeof provider.required === "boolean" && Array.isArray(provider.environment) && provider.environment.every(name => identifier(name)) &&
    Array.isArray(provider.methods) && provider.methods.every(method => record(method) && identifier(method.id) && text(method.display_name, 256) && text(method.kind, 64)) &&
    validStatus(provider.status));
}

export function validJob(value: unknown, identity: Identity): value is AuthJob {
  if (!record(value) || !sameIdentity(value, identity) || !identifier(value.job_id) || !text(value.provider_id, 256) || !text(value.method, 64)) return false;
  if (!["running", "completed", "failed", "canceled", "unknown"].includes(value.state as string) || !text(value.error)) return false;
  if (value.status !== undefined && !validStatus(value.status)) return false;
  return Array.isArray(value.progress) && value.progress.length <= 16 && value.progress.every(item => record(item) && text(item.kind, 64) && text(item.message) &&
    typeof item.url === "string" && (item.url === "" || /^https:\/\/[^\s]{1,2040}$/.test(item.url)) && text(item.user_code, 64));
}

export const terminal = (job: AuthJob | null): boolean => !!job && job.state !== "running";

/**
 * Latest provider instruction worth showing. A device code outranks the
 * generic open-URL step that follows it; the code's own URL is kept with it.
 */
export function instruction(job: AuthJob | null): AuthProgress | null {
  if (!job) return null;
  const latest = (match: (item: AuthProgress) => boolean) => job.progress.findLast(match) ?? null;
  const code = latest(item => !!item.user_code);
  if (code) return code.url ? code : {...code, url: latest(item => !!item.url)?.url ?? ""};
  return latest(item => !!item.url) ?? job.progress.at(-1) ?? null;
}

/**
 * Providers in the order a person looks for them: the conversation's own
 * provider, then required ones, then anything signed in. Entries that offer no
 * sign-in method and hold no credential are noise and are left out.
 */
export function orderedProviders(providers: readonly AuthProvider[], currentProvider: string): AuthProvider[] {
  const rank = (provider: AuthProvider) => provider.id === currentProvider ? 0 : provider.required ? 1 : provider.status.state !== "missing" ? 2 : 3;
  return providers.filter(provider => provider.methods.length > 0 || provider.status.state !== "missing").slice().sort((a, b) => rank(a) - rank(b) || a.display_name.localeCompare(b.display_name));
}

export function describeStatus(status: AuthStatus): string {
  const expiry = status.expires_at > 0 ? new Date(status.expires_at * 1000).toLocaleString() : "";
  switch (status.state) {
    case "configured": return (status.account_id ? `Signed in as ${status.account_id}` : "Signed in") + (expiry ? ` · token valid until ${expiry}` : "");
    case "expired": return "Credential expired · sign in again" + (expiry ? ` (expired ${expiry})` : "");
    case "invalid": return "Credential rejected · sign in again";
    case "missing": return "Not signed in";
    default: return "Status unknown";
  }
}

export function stateLabel(state: string): string {
  return {configured: "Signed in", expired: "Expired", invalid: "Invalid", missing: "Not signed in"}[state] ?? "Unknown";
}

/**
 * Methods this browser can drive. A device code needs no callback and works from
 * any paired device. Browser OAuth redirects to a callback port on the Snow host
 * and an API key would travel in the clear, so both need the local origin.
 */
export function usableMethods(provider: AuthProvider, localOrigin: boolean): AuthMethod[] {
  const order = (method: AuthMethod) => method.id === "device" ? 0 : method.kind === "oauth" ? 1 : 2;
  return provider.methods.filter(method => method.id === "device" || (localOrigin && (method.kind === "oauth" || method.kind === "api_key"))).sort((a, b) => order(a) - order(b));
}
