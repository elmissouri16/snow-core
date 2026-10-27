export type Identity = Readonly<{project_id: string; instance_id: string; session_id: string}>;
export type Subagent = Readonly<{path: string; parent_path: string; role: string; nickname: string; depth: number; status: string; provider: string; model: string; thinking: string; created_at: number; started_at: number; finished_at: number; result: string; error: string}>;
export type SubagentInventory = Identity & Readonly<{agents: readonly Subagent[]; running: number; queued: number; terminal: number; open: number; concurrent_limit: number; agent_limit: number; truncated: boolean}>;
export type UI = Readonly<{readable: boolean}>;

const record = (value: unknown): value is Record<string, unknown> => typeof value === "object" && value !== null;
const text = (value: unknown, limit: number): value is string => typeof value === "string" && value.length <= limit && !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(value);
const count = (value: unknown): value is number => Number.isSafeInteger(value) && (value as number) >= 0;

export function validInventory(value: unknown, identity: Identity): value is SubagentInventory {
  if (!record(value) || value.project_id !== identity.project_id || value.instance_id !== identity.instance_id || value.session_id !== identity.session_id) return false;
  if (![value.running, value.queued, value.terminal, value.open, value.concurrent_limit, value.agent_limit].every(count) || typeof value.truncated !== "boolean") return false;
  return Array.isArray(value.agents) && value.agents.length <= 128 && value.agents.every(agent => record(agent) && text(agent.path, 512) && agent.path !== "" && text(agent.parent_path, 512) &&
    text(agent.role, 128) && text(agent.nickname, 128) && count(agent.depth) && text(agent.status, 32) && text(agent.provider, 128) && text(agent.model, 256) && text(agent.thinking, 32) &&
    Number.isSafeInteger(agent.created_at) && Number.isSafeInteger(agent.started_at) && Number.isSafeInteger(agent.finished_at) && text(agent.result, 2048) && text(agent.error, 2048));
}

export const active = (status: string) => status === "running" || status === "queued" || status === "pending_init";

export function statusLabel(status: string): string {
  return {pending_init: "Starting", queued: "Queued", running: "Running", interrupted: "Interrupted", completed: "Completed", errored: "Errored", shutdown: "Shut down", closed: "Closed"}[status] ?? (status || "Unknown");
}

export function stateTone(status: string): string {
  if (status === "running") return "configured";
  if (status === "errored" || status === "interrupted") return "invalid";
  return "missing";
}

export function duration(agent: Subagent, now: number): string {
  const start = agent.started_at || agent.created_at;
  if (!start) return "";
  const end = agent.finished_at || now;
  const seconds = Math.max(0, Math.round(end - start));
  return seconds < 60 ? `${seconds}s` : seconds < 3600 ? `${Math.floor(seconds / 60)}m ${seconds % 60}s` : `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
}
