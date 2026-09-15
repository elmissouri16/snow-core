import type {ChromeState} from './model';

export function Chrome({view}: {view: ChromeState}) {
  return <>
    <div className="connection-line"><span id="live-connection" role="status" data-connected={String(view.connected)}>{view.connection}</span><button className="quiet" type="button" data-runtime-reload="" hidden={!view.reload}>Review / reload workspace</button></div>
    <div className="unknown-outcome" id="live-unknown" role="alert" hidden={!view.unknown}><p>A request’s outcome is unknown. Nothing will be retried or replayed. Review the conversation and runtime state before sending anything again; the original request may have succeeded. Your draft is kept.</p><button type="button" data-runtime-reviewed="" disabled={view.reviewDisabled}>I’ve reviewed the current conversation</button></div>
    <aside id="live-recovery" className="notice recovery-notice" role="status" hidden={!view.recoveryVisible}><p id="live-recovery-message">{view.recoveryMessage}</p><p className="fine">Tools may already have had effects. Close this runtime to review saved history, then explicitly resume if appropriate. Nothing will be replayed.</p></aside>
    <p id="live-error" className="error live-error" role="alert" hidden={!view.error}>{view.error}</p>
    <p id="live-turn-outcome" className="notice live-error" role="status" hidden={!view.canceled}>{view.canceled ? 'The last turn was canceled before completion. Nothing will retry automatically. You can send another message.' : ''}</p>
  </>;
}
