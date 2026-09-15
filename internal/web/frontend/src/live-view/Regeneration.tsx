import type {ControlsState} from './model';

// Native showModal/close/cancel and return-focus remain with app's bindDialog.
// React does not set `open` or install duplicate action/submit handlers.
export function Regeneration({view}: {view: ControlsState['dialog']}) {
  return <dialog id="message-regenerate-dialog" className="folder-dialog" aria-labelledby="message-regenerate-title" aria-describedby="message-regenerate-description">
    <div className="dialog-heading"><h2 id="message-regenerate-title">Regenerate this reply?</h2></div>
    <p id="message-regenerate-description">Regenerating restarts this whole reply from its original prompt, including its earlier text and tool work, and replaces the following conversation. Tools may run again; earlier file changes are not undone. Your unsent draft is kept.</p>
    <p className="fine" data-message-regenerate-status="" role="status" aria-live="polite">{view.status}</p>
    <div className="dialog-actions"><button type="button" className="quiet" data-message-regenerate-cancel="" autoFocus disabled={view.cancelDisabled}>Cancel</button><button type="button" className="button danger" data-message-regenerate-confirm="" disabled={view.confirmDisabled}>Regenerate</button></div>
  </dialog>;
}
