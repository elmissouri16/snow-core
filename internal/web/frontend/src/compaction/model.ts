/** Public compaction DTOs only. Native private summaries never enter this panel. */
export interface Identity { project_id: string; instance_id: string; session_id: string }
export interface Scope { session_id: string; branch_id: string; expected_tip_id: string; expected_revision: string }
export type CompactionState = "pending" | "running" | "completed" | "noop" | "fallback" | "canceled" | "failed" | "uncertain";
export interface Compaction {
  state: CompactionState; session_id: string; branch_id: string; expected_tip_id: string;
  summarized_messages: number; retained_messages: number; progress_done: boolean; used_fallback: boolean;
  request_id: string; compaction_id: string; turn_id: string; turn_origin: string; root_epoch: number; turn_sequence: number;
}
export interface Snapshot {
  revision?: unknown; status?: string; cancel_requested?: boolean; permission?: unknown; input?: unknown;
  queue?: {items?: readonly unknown[]} | null;
  goal?: {session_id?: unknown; branch_id?: unknown; tip_id?: unknown; running?: boolean; goal_id?: string; status?: string} | null;
  compaction?: unknown; provider?: string; model?: string; mode?: string;
}
export interface UI {
  supported: boolean; readable: boolean; safe: boolean; canStop?: boolean;
  // The parent may supply its exact cancellation presentation during HTTP admission.
  showStop?: boolean; stopLabel?: string; stopTitle?: string;
}
const states = new Set<string>(["pending", "running", "completed", "noop", "fallback", "canceled", "failed", "uncertain"]);
function object(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
export const id = (value: unknown, empty = false): value is string => typeof value === "string" && (empty || !!value) && value.length <= 256 && !/[\u0000-\u001f\u007f]/.test(value);
const count = (value: unknown): value is number => typeof value === "number" && Number.isSafeInteger(value) && value >= 0 && value <= 1073741824;
export const positive = (value: unknown): value is number => typeof value === "number" && Number.isSafeInteger(value) && value > 0;
export function validCompaction(c: unknown, session: string): c is Compaction {
  if (!object(c) || typeof c.state !== "string" || !states.has(c.state) || c.session_id !== session || !id(c.branch_id) || !id(c.expected_tip_id, true) || !count(c.summarized_messages) || !count(c.retained_messages) || typeof c.progress_done !== "boolean" || typeof c.used_fallback !== "boolean") return false;
  const captured = id(c.request_id) && id(c.compaction_id) && c.turn_id === c.compaction_id && c.turn_origin === "compact" && positive(c.root_epoch) && positive(c.turn_sequence);
  return captured || ["pending", "failed", "uncertain"].includes(c.state) && c.request_id === "" && c.compaction_id === "" && c.turn_id === "" && c.turn_origin === "" && c.root_epoch === 0 && c.turn_sequence === 0;
}
export function validACK(result: unknown, identity: Identity, fields: Scope): boolean {
  if (!object(result)) return false;
  const a = result.compaction_ack;
  return result.project_id === identity.project_id && result.instance_id === identity.instance_id && result.session_id === identity.session_id && positive(result.revision) && result.revision >= Number(fields.expected_revision) && object(a) && id(a.compaction_id) && a.turn_id === a.compaction_id && a.turn_origin === "compact" && positive(a.root_epoch) && positive(a.turn_sequence) && a.session_id === fields.session_id && a.branch_id === fields.branch_id;
}
export function scope(snapshot: Snapshot | null, session: string): Scope | null {
  const g = snapshot?.goal;
  if (!g || g.session_id !== session || !id(g.branch_id) || !id(g.tip_id, true) || !positive(snapshot?.revision)) return null;
  return {session_id: session, branch_id: g.branch_id, expected_tip_id: g.tip_id, expected_revision: String(snapshot.revision)};
}
export function idleSafe(s: Snapshot | null, ui: UI | null, invalid: boolean): boolean {
  const g = s?.goal;
  const state = object(s?.compaction) ? s.compaction.state : undefined;
  return !!ui?.safe && !invalid && s?.status === "idle" && !s.cancel_requested && !s.permission && !s.input && !s.queue?.items?.length && !!g && !g.running && (!g.goal_id || ["complete", "budget_limited"].includes(g.status ?? "")) && !["pending", "running", "uncertain"].includes(String(state));
}
export function unchanged(reviewed: Scope | null, snapshot: Snapshot | null, session: string): boolean {
  return !!reviewed && JSON.stringify(reviewed) === JSON.stringify(scope(snapshot, session));
}
export function description(c: Compaction | null): string {
  if (!c) return "No manual compaction has been requested in this live conversation.";
  const counts = `${c.summarized_messages.toLocaleString()} messages summarized · ${c.retained_messages.toLocaleString()} retained`;
  switch (c.state) {
    case "pending": return "Request reserved. Waiting for the native operation receipt. Stop cancels this operation.";
    case "running": return c.progress_done ? "Compaction progress received; waiting for native cleanup and authoritative context refresh. Stop remains available." : "Manual compaction is running. It may use provider tokens. Stop cancels this operation.";
    case "completed": return `Manual compaction completed · ${counts}. Context and usage refreshed.`;
    case "noop": return "No compaction was needed. Context and usage refreshed; no message was sent.";
    case "fallback": return `Compaction used a fallback checkpoint · ${counts}. Context and usage refreshed; this was not a full provider summary.`;
    case "canceled": return "Manual compaction canceled. Context and usage refreshed; provider usage or a checkpoint may already have been saved.";
    case "failed": return "Manual compaction failed or was rejected. Review the current conversation; nothing will be retried automatically.";
    case "uncertain": return "The worker disconnected before the result could be verified. Close and explicitly reopen this project to inspect saved context. Nothing will be retried.";
  }
}
