import type {RefObject} from 'react';
import type {Item} from './model.ts';
import {labels} from './model.ts';

export type Presentation = {
  text: string; busy: boolean; composing: boolean; triggerHidden: boolean; canOpen: boolean; canSubmit: boolean;
  error: string; reviewHidden: boolean; canReview: boolean; dismiss: boolean; items: Item[];
};
export type Actions = {
  open: (trigger: HTMLElement) => void; close: () => void; closed: () => void;
  review: () => void; copy: (request: string, trigger: HTMLElement) => void;
  text: (value: string) => void; compose: (composing: boolean) => void; submit: () => void;
};
export function SteerPanel({state: s, actions: a, dialog, input}: {
  state: Presentation; actions: Actions; dialog: RefObject<HTMLDialogElement | null>; input: RefObject<HTMLTextAreaElement | null>;
}) {
  return <>
    <button type="button" className="quiet" data-steer-open="" hidden={s.triggerHidden} disabled={!s.canOpen} title="Direct the current run at its next safe native boundary. Queue next is a separate follow-up." onClick={event => { event.preventDefault(); a.open(event.currentTarget); }}>Steer current run…</button>
    <section id="live-steer-history" className="steer-history" aria-label="Current-run steering" hidden={!s.items.length}>
      <h3>Current-run steering</h3>
      <p className="muted">Acceptance is not delivery. Only native delivery or discard confirms the outcome.</p>
      <ol data-steer-items="">{s.items.map(item => <li key={item.request_id} data-steer-request={item.request_id}>
        <p data-steer-state="">{labels[item.status]}</p><pre data-steer-source="">{item.text}</pre>
        <button type="button" className="quiet" data-steer-copy="" disabled={s.busy} onClick={event => { event.preventDefault(); a.copy(item.request_id, event.currentTarget); }}>Review text</button>
      </li>)}</ol>
    </section>
    <dialog ref={dialog} id="live-steer-dialog" className="steer-dialog runtime-dialog" aria-labelledby="steer-heading" aria-describedby="steer-explanation" onCancel={event => { event.preventDefault(); a.close(); }} onClose={a.closed}>
      <div className="dialog-heading"><h2 id="steer-heading">Steer current run</h2><button type="button" className="quiet icon-button" data-steer-close="" aria-label="Close steering and keep draft" title="Close" onClick={a.close}><svg className="icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg></button></div>
      <form className="runtime-dialog-body" data-steer-form="" onSubmit={event => { event.preventDefault(); event.stopPropagation(); a.submit(); }}>
        <p id="steer-explanation">Send a correction or direction into the current run. Snow delivers literal text at a safe boundary after the current assistant response or tool batch, not in the middle of a tool. Provider failure can still be followed by delivery.</p>
        <p className="muted"><strong>Queue next</strong> is different: it schedules a natural follow-up after current work. Steering does not create a separate queued follow-up or an optimistic chat message.</p>
        <label htmlFor="live-steer-text">Direction for this run</label>
        <textarea ref={input} id="live-steer-text" data-steer-text="" rows={6} placeholder="For the current task, focus on…" aria-describedby="steer-draft-hint" spellCheck value={s.text} readOnly={s.busy} onChange={event => a.text(event.currentTarget.value)} onCompositionStart={() => a.compose(true)} onCompositionEnd={() => a.compose(false)} onKeyDown={event => {
          if (event.key === 'Enter' && (event.ctrlKey || event.metaKey) && !event.nativeEvent.isComposing && !s.composing) { event.preventDefault(); a.submit(); }
        }} />
        <p id="steer-draft-hint" className="muted">Up to 64 KiB. Enter adds a line; Ctrl/⌘ + Enter sends. Closing keeps this draft in this tab.</p>
        <p data-steer-error="" role="status" aria-live="polite" hidden={!s.error}>{s.error}</p>
        <footer><button type="button" className="quiet" data-steer-review="" hidden={s.reviewHidden} disabled={!s.canReview} onClick={a.review}>{s.dismiss ? 'Keep draft and dismiss' : 'Review current run'}</button><button type="submit" data-steer-submit="" disabled={!s.canSubmit}>{s.busy ? 'Sending steering…' : 'Send steering'}</button></footer>
      </form>
    </dialog>
  </>;
}
