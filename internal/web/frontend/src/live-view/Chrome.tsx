import {useState, type MouseEvent} from 'react';
import type {ChromeState} from './model';

function DismissibleError({error}: {error: string}) {
  const [dismissed, setDismissed] = useState(false);
  function dismiss(event: MouseEvent<HTMLButtonElement>) {
    const prompt = event.currentTarget.closest('#live-session')?.querySelector<HTMLTextAreaElement>('#live-prompt');
    if (prompt && !prompt.disabled) prompt.focus({preventScroll: true});
    else event.currentTarget.blur();
    setDismissed(true);
  }
  return <div id="live-error" className="error live-error live-error-message" role="alert" hidden={!error || dismissed}>
    <span>{error}</span>
    <button type="button" className="quiet icon-button" data-live-error-dismiss="" aria-label="Dismiss error" title="Dismiss error" onClick={dismiss}>×</button>
  </div>;
}

export function Chrome({view}: {view: ChromeState}) {
  return <>
    <div className="connection-line"><span id="live-connection" role="status" data-connected={String(view.connected)}>{view.connection}</span><button className="quiet" type="button" data-runtime-reload="" hidden={!view.reload}>Review / reload workspace</button></div>
    <div className="unknown-outcome" id="live-unknown" role="alert" hidden={!view.unknown}><p>A request’s outcome is unknown. Nothing will be retried or replayed. Review the conversation and runtime state before sending anything again; the original request may have succeeded. Your draft is kept.</p><button type="button" data-runtime-reviewed="" disabled={view.reviewDisabled}>I’ve reviewed the current conversation</button></div>
    <aside id="live-recovery" className="notice recovery-notice" role="status" hidden={!view.recoveryVisible}><p id="live-recovery-message">{view.recoveryMessage}</p><p className="fine">Tools may already have had effects. Close this runtime to review saved history, then explicitly resume if appropriate. Nothing will be replayed.</p></aside>
    <DismissibleError key={view.error} error={view.error}/>
    <p id="live-turn-outcome" className="notice live-error" role="status" hidden={!view.canceled}>{view.canceled ? 'The last turn was canceled before completion. Nothing will retry automatically. You can send another message.' : ''}</p>
  </>;
}
