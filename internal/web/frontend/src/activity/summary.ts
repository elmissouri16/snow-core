/** Read-only metadata, not a runtime snapshot or control capability. */
export const MAX_SUMMARY_BYTES = 256 * 1024;
export const MAX_PROJECTS = 100;
export const POLL_INTERVAL_MS = 2000;
export const REQUEST_TIMEOUT_MS = 5000;

export const stateLabels = {
  inactive: "No live worker",
  opening: "Opening on host",
  idle: "Host worker idle",
  running: "Running on host",
  permission: "Running on host · approval needed",
  input: "Running on host · question needs an answer",
  switching: "Host worker switching conversation",
  closing: "Host worker closing",
  failed: "Host worker failed · review required",
  unknown: "Host state unavailable",
} as const;
export const countLabels = {
  registered: "Registered projects",
  running: "Host-running projects",
  permissions: "Approvals needed",
  questions: "Question requests",
  failed: "Failed projects",
  recovery: "Recovery reviews",
  queued: "Queued follow-ups",
  review: "Queue items to review",
} as const;
const recoveryStates = ["", "bound", "admission_unknown", "admitted", "completed", "failed", "canceled", "rejected"] as const;
const folderStates = ["available", "missing", "changed", "unavailable"] as const;
export type ActivityCounts = Record<keyof typeof countLabels, number>;
export interface ActivityProject {
  project_id: string;
  name: string;
  project_url: string;
  session_id: string;
  session_url: string;
  folder_state: typeof folderStates[number];
  runtime_state: keyof typeof stateLabels;
  host_running: boolean;
  permissions: number;
  questions: number;
  failed: boolean;
  recovery: boolean;
  recovery_state: typeof recoveryStates[number];
  queued: number;
  review: number;
  unavailable: boolean;
}
export interface ActivitySummary {
  updated_at: string;
  projects: ActivityProject[];
  counts: ActivityCounts;
}

function object(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
export function identifier(value: unknown): value is string {
  return typeof value === "string" && /^[A-Za-z0-9_-]{1,128}$/.test(value);
}
function bounded(value: unknown, max: number): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 && value <= max;
}

/** Construct links from validated IDs, never render a URL supplied by the wire. */
export function projectLink(projectID: string, sessionID = ""): string {
  if (!identifier(projectID) || (sessionID !== "" && !identifier(sessionID))) throw Error("identifier");
  const params = new URLSearchParams({view: "projects", project: projectID});
  if (sessionID) params.set("session", sessionID);
  return "/?" + params.toString();
}

/** Only the exact relative project/session navigation contract is accepted. */
export function validNavigationURL(value: unknown, projectID: string, sessionID = ""): value is string {
  if (typeof value !== "string" || value.length > 512 || !value.startsWith("/?") || /[\s\\#]/.test(value)) return false;
  const url = new URL(value, "https://activity.invalid");
  const params = url.searchParams;
  return url.origin === "https://activity.invalid" && url.pathname === "/" &&
    params.size === (sessionID ? 3 : 2) && params.getAll("view").length === 1 &&
    params.get("view") === "projects" && params.getAll("project").length === 1 &&
    params.get("project") === projectID &&
    (sessionID ? params.getAll("session").length === 1 && params.get("session") === sessionID : !params.has("session"));
}

export function validateSummary(data: unknown): ActivitySummary {
  if (!object(data) || !Array.isArray(data.projects) || data.projects.length > MAX_PROJECTS ||
      typeof data.updated_at !== "string" || data.updated_at.length > 64 || !Number.isFinite(Date.parse(data.updated_at))) throw Error("summary");
  if (!object(data.counts)) throw Error("counts");
  for (const key of Object.keys(countLabels) as (keyof ActivityCounts)[]) {
    if (!bounded(data.counts[key], key === "queued" || key === "review" ? 800 : 100)) throw Error("counts");
  }
  const ids = new Set<string>();
  for (const item of data.projects) {
    if (!object(item) || !identifier(item.project_id) || ids.has(item.project_id) ||
        typeof item.name !== "string" || item.name.length > 128 ||
        typeof item.runtime_state !== "string" || !Object.hasOwn(stateLabels, item.runtime_state) ||
        !recoveryStates.some(state => state === item.recovery_state) ||
        !folderStates.some(state => state === item.folder_state)) throw Error("project");
    if (item.session_id !== "" && !identifier(item.session_id)) throw Error("session");
    if (!validNavigationURL(item.project_url, item.project_id) ||
        (item.session_id === "" ? item.session_url !== "" : !validNavigationURL(item.session_url, item.project_id, item.session_id as string))) throw Error("navigation");
    if (["host_running", "failed", "recovery", "unavailable"].some(key => typeof item[key] !== "boolean") ||
        !bounded(item.permissions, 1) || !bounded(item.questions, 1) || !bounded(item.queued, 8) ||
        !bounded(item.review, 8) || item.queued + item.review > 8) throw Error("state");
    ids.add(item.project_id);
  }
  // Every field consumed by the UI has been checked above. Unknown metadata is not rendered.
  return data as unknown as ActivitySummary;
}

export async function boundedJSON(response: Response, signal: AbortSignal): Promise<ActivitySummary> {
  signal.throwIfAborted();
  const length = response.headers.get("Content-Length");
  if (length !== null && (!/^\d+$/.test(length) || Number(length) > MAX_SUMMARY_BYTES)) throw Error("size");
  if (response.headers.get("Content-Type")?.split(";", 1)[0]?.trim().toLowerCase() !== "application/json") throw Error("type");
  if (!response.body) throw Error("body");
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  // Abort the reader as well as fetch, including a response whose headers have already arrived.
  const abort = () => { void reader.cancel().catch(() => {}); };
  signal.addEventListener("abort", abort, {once: true});
  try {
    while (true) {
      signal.throwIfAborted();
      const {done, value} = await reader.read();
      signal.throwIfAborted();
      if (done) break;
      size += value.byteLength;
      if (size > MAX_SUMMARY_BYTES) throw Error("size");
      chunks.push(value);
    }
    const bytes = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
    signal.throwIfAborted();
    return validateSummary(JSON.parse(new TextDecoder("utf-8", {fatal: true}).decode(bytes)));
  } finally {
    signal.removeEventListener("abort", abort);
    // Cancellation is best-effort; a broken stream must not hold the polling slot forever.
    void reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}

export class ActivityAccessEnded extends Error {}

export async function fetchActivity(signal: AbortSignal, request: typeof fetch = fetch): Promise<ActivitySummary> {
  signal.throwIfAborted();
  const response = await request("/activity", {
    method: "GET", credentials: "same-origin", cache: "no-store", redirect: "error",
    headers: {Accept: "application/json"}, signal,
  });
  try {
    signal.throwIfAborted();
    if (response.status === 401 || response.status === 403) throw new ActivityAccessEnded();
    if (!response.ok) throw Error("refresh");
    return await boundedJSON(response, signal);
  } catch (error) {
    // Also stop unconsumed bodies rejected by status, media type or advertised size.
    void response.body?.cancel().catch(() => {});
    throw error;
  }
}
