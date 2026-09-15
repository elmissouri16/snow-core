/** Public RuntimeQueue DTO from internal/web/queue_http.go. Never a scheduler. */
export const maxItems = 8;
export type ItemState = "pending" | "starting" | "held" | "uncertain";
export interface QueueItem {id: string; text: string; state: ItemState}
export interface Queue {token: string; revision: number; can_enqueue: boolean; items: QueueItem[]}
export interface Snapshot {revision: number; queue?: unknown}
export interface UI {connected: boolean; status: string; busy: boolean; unknown: boolean; stopping: boolean; editing: boolean}
export interface Draft {value: string; revision: number; start: number; end: number; direction: "forward" | "backward" | "none"}
export interface Editor {text: string; revision: number; baseRevision: number; token: string; composing?: boolean; error?: string}
export interface Operation {action: Action; id: string; revision: number; token: string; focus: HTMLElement | null; verified?: boolean}
export type Action = "queue-enqueue" | "queue-update" | "queue-remove";
export interface Store {
  editors: Map<string, Editor>; unknown: boolean; busy?: Operation | false;
  reviewable?: boolean; unknownRevision?: number; error?: string; copiedDraft?: Draft | null;
}
export interface Admission {ui: UI | null; queue: Queue | null; store: Store; invalid: boolean; composing: boolean}
export function validQueue(raw: unknown, validText: (text: unknown) => boolean): raw is Queue {
  if (!raw || typeof raw !== "object") return false;
  const q = raw as Queue;
  if (typeof q.token !== "string" || q.token.length > 256 || !Number.isSafeInteger(q.revision) || q.revision < 0 || typeof q.can_enqueue !== "boolean" || !Array.isArray(q.items) || q.items.length > maxItems) return false;
  const ids = new Set<string>(); let bytes = 0;
  for (const item of q.items) {
    if (!item || typeof item.id !== "string" || !item.id || item.id.length > 256 || ids.has(item.id) || !["pending", "starting", "held", "uncertain"].includes(item.state) || typeof item.text !== "string" || !validText(item.text)) return false;
    ids.add(item.id); bytes += new TextEncoder().encode(item.text).length;
  }
  return bytes <= 256 * 1024;
}
export function sameDraft(left: Draft, right: Draft): boolean {
  return left.value === right.value && left.revision === right.revision && left.start === right.start && left.end === right.end && left.direction === right.direction;
}
export function retainedReview(queue: Queue | null): boolean {return !!queue?.items.some(item => ["held", "uncertain"].includes(item.state));}
export function canChange(current: Admission): boolean {
  const {ui, store, queue, invalid} = current;
  return !!ui && ui.connected && !ui.busy && !ui.unknown && !ui.stopping && !ui.editing && !invalid && !store.busy && !store.unknown && !!queue?.token;
}
export function canEnqueue(current: Admission): boolean {
  return canChange(current) && !current.composing && current.ui?.status === "running" && current.queue?.can_enqueue === true && current.queue.items.length < maxItems;
}
export function blocking(current: Admission): boolean {return !!(current.store.busy || current.store.unknown || current.invalid || retainedReview(current.queue));}
/** Return this to the composer owner; the queue never writes foreign DOM. */
export function presentation(current: Admission) {
  const {ui, store, queue} = current, enabled = canEnqueue(current);
  const hidden = !["running", "permission", "input"].includes(ui?.status || "");
  return {
    hidden, disabled: !enabled, label: store.busy && store.busy.action === "queue-enqueue" ? "Queuing…" : "Queue next",
    title: enabled ? "Queue this draft after the current work; it is not sent immediately" : "Queue next needs a connected running turn accepting follow-ups; approval and question takeovers pause queuing",
    hint: hidden ? "Ctrl / ⌘ + Enter to send · Enter for a new line" : enabled ? "Ctrl / ⌘ + Enter to queue next · Enter for a new line" : "Queue next is paused · your draft is kept",
    state: hidden ? retainedReview(queue) ? "Remove reviewed pending items before starting or switching conversations" : null : store.unknown ? "Review the queue request outcome; nothing will retry" : store.busy ? "Updating pending messages · Stop remains available" : queue?.items.some(item => item.state === "pending") ? "Pending messages are separate from your draft" : "Turn in progress · Queue next is an explicit action",
  };
}
export type Presentation = ReturnType<typeof presentation>;
/** A receipt may lag a newer SSE snapshot, but must still belong to this nonce. */
export function validAcknowledgement(raw: unknown, operation: Pick<Operation, "token" | "revision">, current: Queue | null, validText: (text: unknown) => boolean): raw is Queue {
  return validQueue(raw, validText) && raw.token === operation.token && current?.token === operation.token && raw.revision > operation.revision;
}
