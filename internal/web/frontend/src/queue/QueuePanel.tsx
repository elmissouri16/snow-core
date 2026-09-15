import type {RefObject} from "react";
import {canChange, maxItems} from "./model.ts";
import type {Admission, QueueItem} from "./model.ts";
export type DisplayItem = Omit<QueueItem, "state"> & {state: QueueItem["state"] | "missing"};
export interface RowRefs {input: HTMLTextAreaElement | null; edit: HTMLButtonElement | null}
export interface PanelProps {
  current: Admission; panelRef: RefObject<HTMLElement | null>;
  rowRefs: Map<string, RowRefs>;
  onAction: (action: string, id?: string) => void;
  onText: (id: string, text: string) => void;
  onComposition: (id: string, composing: boolean) => void;
}
const labels = {
  pending: "Pending · after current work", starting: "Delivery starting · locked",
  held: "Held for review · not scheduled", uncertain: "Delivery unknown · may already have been delivered",
  missing: "No longer pending · review the conversation; your edit is kept",
};
function QueueRow({item, current, rowRefs, onAction, onText, onComposition}: Omit<PanelProps, "panelRef"> & {item: DisplayItem}) {
  const {store, queue, ui} = current, editor = store.editors.get(item.id);
  const editable = item.state === "pending" && canChange(current) && ui?.status === "running" && (!editor || editor.token === queue?.token);
  const review = ["held", "uncertain"].includes(item.state);
  let refs = rowRefs.get(item.id);
  if (!refs) {refs = {input: null, edit: null}; rowRefs.set(item.id, refs);}
  return <article className="queue-next-item" data-queue-item-id={item.id} data-queue-state={item.state}>
    <div className="queue-next-item-heading"><span className="queue-next-item-state" data-queue-item-state="">{labels[item.state]}</span></div>
    <pre className="queue-next-source" data-queue-source="">{item.text}</pre>
    <div className="queue-next-actions">
      <button ref={node => {refs.edit = node;}} type="button" className="quiet" data-queue-edit="" hidden={!!editor || item.state !== "pending"} disabled={!editable} onClick={() => onAction("edit", item.id)}>Edit</button>
      <button type="button" className="quiet" data-queue-remove="" hidden={item.state !== "pending" && !review} disabled={!!editor || !(editable || review && canChange(current))} title={review ? "Remove from this review list only; this does not undo or revoke delivery" : "Remove this pending message before delivery starts"} onClick={() => onAction("remove", item.id)}>Remove</button>
      <button type="button" className="quiet" data-queue-copy-draft="" data-queue-copy="" hidden={!["held", "uncertain", "missing"].includes(item.state)} disabled={!!store.busy || !!ui?.editing} onClick={() => onAction("copy", item.id)}>Copy to draft</button>
    </div>
    <form className="queue-next-editor" data-queue-editor="" noValidate hidden={!editor} onSubmit={event => {event.preventDefault(); event.stopPropagation(); onAction("save", item.id);}}>
      <textarea ref={node => {refs.input = node;}} data-queue-text="" maxLength={65536} rows={3} aria-label="Edit pending message" value={editor?.text || ""} onChange={event => onText(item.id, event.currentTarget.value)} onCompositionStart={() => onComposition(item.id, true)} onCompositionEnd={() => onComposition(item.id, false)} />
      <div className="queue-next-actions"><button type="submit" className="quiet" data-queue-save="" disabled={!editable || !!editor?.composing}>Save</button><button type="button" className="quiet" data-queue-cancel="" disabled={!!store.busy && store.busy.id === item.id} onClick={() => onAction("cancel", item.id)}>Cancel</button></div>
      <p className="error" data-queue-editor-error="" role="alert" hidden={!editor?.error}>{editor?.error || ""}</p>
    </form>
  </article>;
}
export function QueuePanel(props: PanelProps) {
  const {current, panelRef, onAction} = props, {store, queue, ui, invalid} = current;
  const items: DisplayItem[] = [...queue?.items || []], ids = new Set(items.map(item => item.id));
  for (const [id, editor] of store.editors) if (!ids.has(id)) items.push({id, text: editor.text, state: "missing"});
  const hidden = !items.length && !store.unknown && !store.error && !store.copiedDraft && !invalid;
  return <section ref={panelRef} id="live-queue-next" className="queue-next-panel" aria-labelledby="queue-next-heading" tabIndex={-1} hidden={hidden}>
    <div className="queue-next-heading"><h2 id="queue-next-heading">Pending messages</h2><span data-queue-count="">{queue?.items.length || 0} / {maxItems}</span></div>
    <p className="fine queue-next-boundary">This bounded queue belongs to the current live run. Stopped or uncertain items are review-only, not scheduled. Closing or restarting does not automatically resume them. Remove reviewed items before starting or switching conversations; Copy to draft never sends.</p>
    <p data-queue-error="" className="error" role="alert" hidden={!store.error && !invalid}>{invalid ? "Could not verify the pending queue. Controls are disabled; nothing will be retried." : store.error || ""}</p>
    <div data-queue-unknown="" hidden={!store.unknown}><p className="fine">A queue request’s outcome is unknown. The message may already be queued or delivered. Your draft is kept; nothing will retry.</p><button type="button" className="quiet" data-queue-reviewed="" disabled={!store.unknown || !store.reviewable || !ui?.connected || !!store.busy} onClick={() => onAction("reviewed")}>I’ve reviewed the queue and conversation</button></div>
    <div data-queue-copy-notice="" hidden={!store.copiedDraft}><span className="fine">Copied to draft only. Your earlier draft is kept.</span><button type="button" className="quiet" data-queue-restore-draft="" disabled={!!store.busy || !!ui?.editing} onClick={() => onAction("restore")}>Restore earlier draft</button></div>
    <div data-queue-items="" className="queue-next-items">{items.map(item => <QueueRow key={item.id} {...props} item={item} />)}</div>
  </section>;
}
