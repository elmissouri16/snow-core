import type {Controller} from './controller.tsx';
import {basename, language} from './model.ts';

const Close = () => <svg className="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6"/></svg>;

export function CanvasPanel({controller: c}: {controller: Controller}) {
  const file = c.files.find(entry => entry.path === c.selected) || null;
  const lines = file?.text ? file.text.split('\n') : [];
  return <>
    <div className="canvas-heading">
      <div className="canvas-title">
        <span className="canvas-label">Canvas</span>
        {c.running && <span className="canvas-live" role="status"><i aria-hidden="true"/>Live</span>}
      </div>
      <label className="canvas-auto"><input type="checkbox" checked={c.auto} onChange={event => c.setAuto(event.currentTarget.checked)} /> Open on edits</label>
      <button type="button" className="quiet icon-button" data-canvas-close="" aria-label="Close canvas" title="Close canvas" onClick={c.close}><Close /></button>
    </div>
    <div className="canvas-tabs" role="tablist" aria-label="Files the agent edited">
      {c.files.map(entry => <button key={entry.path} type="button" role="tab" aria-selected={entry.path === c.selected} className={'canvas-tab' + (entry.stale ? ' is-stale' : '')} data-canvas-file={entry.path} title={entry.path} onClick={() => c.select(entry.path)}>
        <span>{basename(entry.path)}</span>{entry.stale && <i className="canvas-dot" aria-label="Changed since last read" />}
      </button>)}
    </div>
    {!file && <p className="fine canvas-empty">When the agent writes or edits a file, it opens here and stays current while the turn runs. Nothing is editable from the browser; the file on disk is the source of truth.</p>}
    {file && <section className="canvas-file" aria-label={`File ${file.path}`}>
      <div className="canvas-file-meta">
        <span className="canvas-path" title={file.path}>{file.path}</span>
        <span className="canvas-meta">{language(file.path)}{file.text ? ` · ${lines.length} lines` : ''}{file.truncated ? ' · truncated at 64 KiB' : ''}{file.stale ? ' · refreshing…' : ''}</span>
        <button type="button" className="quiet canvas-remove" data-canvas-remove="" title="Remove from canvas" onClick={() => c.remove(file.path)}>Remove</button>
      </div>
      {file.error && <p className="error" role="alert">{file.error}</p>}
      {!file.error && <pre className="inspection-code canvas-code" tabIndex={0} aria-label={`Read-only contents of ${file.path}`}>{lines.map((line, index) => <span key={index} className="canvas-line"><span className="canvas-line-number" aria-hidden="true">{index + 1}</span>{line}{'\n'}</span>)}</pre>}
    </section>}
  </>;
}
