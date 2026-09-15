import type {Actions, View} from './controller';
export function Panel({view: v, actions: a}: {view: View; actions: Actions}) {
  const pending = v.records.find(p => p.process_id === v.confirmation);
  return <>
    <button type="button" className="quiet" data-processes-open aria-haspopup="dialog" aria-controls="processes-dialog">Managed processes</button>
    <dialog ref={v.dialog} id="processes-dialog" className="runtime-dialog processes-dialog" aria-labelledby="processes-heading" onClose={() => a.toggle(v)} onCancel={event => { if (v.confirmation) { event.preventDefault(); a.cancel(v); } }}>
      <div className="dialog-heading"><h2 id="processes-heading">Managed processes</h2><button type="button" className="quiet icon-button" data-processes-close aria-label="Close managed processes" title="Close"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18"/></svg></button></div>
      <div className="runtime-dialog-body"><details ref={v.panel} id="managed-processes" className="managed-processes" data-process-project={v.project} data-process-instance={v.instance} data-process-session={v.session} onToggle={() => a.toggle(v)}>
        <summary>Managed processes</summary><div className="managed-process-body">
          <p className="fine">Only this live conversation’s managed processes. No commands can be launched here. Logs may contain untrusted application output. Stop requires Default mode and current process permission.</p>
          <div className="process-toolbar"><button type="button" className="quiet" data-process-refresh disabled={v.busy || !!pending} onClick={() => void a.refresh(v)}>Refresh</button><span data-process-status role="status">{v.status}</span></div>
          <p className="error" data-process-error role="alert" hidden={!v.error}>{v.error}</p>
          <ul data-process-list aria-label="Current session managed processes">{v.records.map(p => <li key={p.process_id} className="process-row"><span className="process-identity"><span>{p.name} · {p.status}{p.ready ? ' · ready' : ''}</span><small>{p.process_id}</small></span><button type="button" className="quiet" data-process-logs={p.process_id} disabled={v.busy || !!pending} onClick={() => a.select(v, p.process_id)}>Logs</button><button type="button" className="quiet" data-process-stop={p.process_id} disabled={v.busy || v.unknown || !!pending || p.status !== 'running'} onClick={event => a.prepare(v, p.process_id, event.currentTarget)}>Stop</button></li>)}</ul>
          {pending && <section className="process-stop-confirmation" role="group" aria-labelledby="process-stop-heading"><h3 id="process-stop-heading">Stop managed process?</h3><p>Stop managed process {pending.name} ({pending.process_id}) in this conversation?</p><p className="fine">Requires Default mode and current process permission. This does not stop the conversation.</p><button type="button" className="quiet" data-process-stop-cancel onClick={() => a.cancel(v)}>Cancel</button><button type="button" className="button danger" data-process-stop-confirm onClick={() => void a.stop(v)}>Stop process</button></section>}
          <section data-process-log-panel hidden={!v.logHeading} aria-label="Managed process log"><h3 data-process-log-heading>{v.logHeading}</h3><p data-process-log-status className="fine">{v.logStatus}</p><pre data-process-output tabIndex={0} aria-label="Bounded plain-text process output">{v.output}</pre><button type="button" className="quiet" data-process-log-more disabled={v.busy || v.eof || !!pending || !v.logHeading} onClick={() => void a.logs(v)}>Read next output</button></section>
        </div></details></div>
    </dialog>
  </>;
}
