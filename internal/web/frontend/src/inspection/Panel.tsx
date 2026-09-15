import {Fragment} from 'react';
import type {Actions, View} from './controller';
import type {Tab} from './model';
import {diffLines} from './model';
const paths = {folder: 'M2 5h5l2 2h13v12H2z', file: 'M6 2h8l4 4v16H6z M14 2v5h4', chevron: 'm9 5 7 7-7 7', close: 'm6 6 12 12M18 6 6 18', up: 'm5 11 7-7 7 7M12 4v16', refresh: 'M20 7v5h-5M4 17v-5h5M5 8a8 8 0 0 1 14-2l1 6M4 12l1 6a8 8 0 0 0 14-2'};
function Icon({kind}: {kind: keyof typeof paths}) { return <svg className="inspection-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true"><path d={paths[kind]}/></svg>; }
function PathLabel({value}: {value: string}) {
  const safe = value.slice(0, 4096), slash = safe.lastIndexOf('/'), name = safe.slice(slash + 1), dot = name.lastIndexOf('.');
  return <>{slash >= 0 && <span className="inspection-directory">{safe.slice(0, slash + 1)}</span>}<span className="inspection-basename"><span className="inspection-stem">{dot > 0 ? name.slice(0, dot) : name}</span>{dot > 0 && <span className="inspection-extension">{name.slice(dot)}</span>}</span></>;
}
function Notice({view: v, kind}: {view: View; kind: 'files' | 'changes'}) {
  const message = v.notices[kind];
  return <><p className="fine" {...{[`data-${kind}-status`]: ''}} role="status" data-state={message.error ? 'error' : message.status.startsWith('Reading') ? 'loading' : 'ready'}>{message.status}</p><p className="error" {...{[`data-${kind}-error`]: ''}} role="alert" hidden={!message.error}>{message.error}</p></>;
}
function Preview({view: v, kind}: {view: View; kind: 'file' | 'diff'}) {
  const preview = v[kind], diff = kind === 'diff';
  return <section className="inspection-preview" {...{[`data-${kind}-preview`]: ''}} hidden={!preview} aria-label={diff ? 'Change preview' : 'File preview'}><div className="inspection-preview-heading"><span className="fine">{diff ? 'Diff' : 'File'} preview · read only</span></div><h3 className="inspection-path" {...{[`data-${kind}-title`]: ''}} title={preview?.path}><PathLabel value={preview?.path || ''}/></h3><p className="fine" {...{[`data-${kind}-notice`]: ''}}>{preview?.notice}</p><pre className="inspection-code" {...{[`data-${kind}-content`]: ''}} tabIndex={0} aria-label={diff ? 'Read-only diff' : 'Read-only file content'}>{diff ? diffLines(preview?.text || '').map((line, i) => <span key={i} className={`inspection-diff-line inspection-diff-${line.kind}`}>{line.text}</span>) : preview?.text}</pre></section>;
}
export function Panel({view: v, actions: a}: {view: View; actions: Actions}) {
  const names: Tab[] = ['files', 'changes', 'project'], parts = v.path === '.' ? [] : v.path.split('/').filter(part => part && part !== '.');
  const props = v.props, filesGeneration = v.filesGeneration, changesGeneration = v.changesGeneration;
  return <>
    <div className="section-heading"><span className="inspection-heading">Project inspector</span><button className="quiet" type="button" data-inspector-toggle aria-label="Close project details" title="Close project details"><Icon kind="close"/></button></div>
    <div className="inspector-tabs" role="tablist" aria-label="Project inspection">{names.map(name => <button key={name} type="button" id={`inspection-tab-${name}`} role="tab" aria-controls={`inspection-${name}`} aria-selected={v.tab === name} tabIndex={v.tab === name ? 0 : -1} data-inspection-tab={name} onClick={() => a.selectTab(v, name)} onKeyDown={event => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
      event.preventDefault(); const current = names.indexOf(name), index = event.key === 'Home' ? 0 : event.key === 'End' ? 2 : (current + (event.key === 'ArrowRight' ? 1 : 2)) % 3; a.selectTab(v, names[index]!, true);
    }}>{name[0]!.toUpperCase() + name.slice(1)}</button>)}</div>
    <p className="fine inspection-boundary">Read-only inspection · no agent activation</p><input type="hidden" name="csrf" value={props.csrf}/>
    <section id="inspection-files" role="tabpanel" aria-labelledby="inspection-tab-files" tabIndex={0} hidden={v.tab !== 'files'}>
      <div className="inspection-toolbar"><h2>Project files</h2><button className="quiet" type="button" data-inspection-refresh="files" title="Refresh from disk" onClick={() => void a.files(v, v.path)}><Icon kind="refresh"/><span>Refresh</span></button></div>
      <div className="inspection-toolbar inspection-location"><button className="quiet inspection-up" type="button" data-inspection-up aria-label="Open parent folder" title="Open parent folder" disabled={v.filesPending || v.path === '.'} onClick={() => void a.files(v, v.path.includes('/') ? v.path.slice(0, v.path.lastIndexOf('/')) : '.')}><Icon kind="up"/></button><nav className="inspection-breadcrumbs" data-inspection-path aria-label="File location" title={v.path}>{['Project root', ...parts].map((name, index) => { const path = index ? parts.slice(0, index).join('/') : '.'; return <Fragment key={path}>{index > 0 && <span className="inspection-crumb-separator">/</span>}<button className="inspection-crumb" type="button" data-inspection-crumb={path} title={path} aria-current={index === parts.length ? 'location' : undefined} disabled={v.filesPending || index === parts.length} onClick={() => void a.files(v, path)}>{name}</button></Fragment>; })}</nav></div>
      <Notice view={v} kind="files"/>
      <ul className="inspection-list" data-files-list aria-label="Project files" aria-busy={v.filesPending}>{v.files.map(entry => <li key={entry.path}><button type="button" title={`${entry.name} · ${entry.kind === 'directory' ? 'Folder' : 'File'}`} data-inspection-entry={entry.path} data-entry-kind={entry.kind} data-files-generation={filesGeneration} disabled={v.filesPending || !v.filesFresh} aria-current={v.fileSelected === entry.path ? 'true' : undefined} aria-busy={v.fileBusy === entry.path ? 'true' : undefined} onClick={() => void a.file(v, entry, filesGeneration)}><Icon kind={entry.kind === 'directory' ? 'folder' : 'file'}/><span className="inspection-entry-name" title={entry.name}><PathLabel value={entry.name}/></span><span className="inspection-entry-kind">{entry.kind === 'directory' ? 'Folder' : 'File'}</span>{entry.kind === 'directory' && <Icon kind="chevron"/>}</button></li>)}</ul>
      <button type="button" className="button" data-files-more hidden={!v.more} disabled={v.filesPending || !v.filesFresh} onClick={() => void a.files(v, v.path, v.next, true)}>Load more files</button>
      <Preview view={v} kind="file"/>
    </section>
    <section id="inspection-changes" role="tabpanel" aria-labelledby="inspection-tab-changes" tabIndex={0} hidden={v.tab !== 'changes'}>
      <div className="inspection-toolbar"><h2>Working changes</h2><button className="quiet" type="button" data-inspection-refresh="changes" title="Refresh from disk" onClick={() => void a.changes(v)}><Icon kind="refresh"/><span>Refresh</span></button></div>
      <p className="fine">Current files on disk, not a per-turn change log.</p><Notice view={v} kind="changes"/>
      <ul className="inspection-list" data-changes-list aria-label="Changed files" aria-busy={v.changesPending}>{v.changes.map(change => { const key = `${change.kind}:${change.path}`, kind = change.kind[0]!.toUpperCase() + change.kind.slice(1) + (change.status ? ` · ${change.status}` : ''); return <li key={key}><button type="button" title={`${change.path} · ${kind}`} data-inspection-change={change.path} data-change-kind={change.kind} data-changes-generation={changesGeneration} disabled={v.changesPending || !v.changesFresh} aria-current={v.diffSelected === key ? 'true' : undefined} aria-busy={v.diffBusy === key ? 'true' : undefined} onClick={() => void a.diff(v, change, changesGeneration)}><Icon kind="file"/><span className="inspection-entry-name" title={change.path}><PathLabel value={change.path}/></span><span className="inspection-entry-kind">{kind}</span></button></li>; })}</ul>
      <Preview view={v} kind="diff"/>
    </section>
    <section id="inspection-project" role="tabpanel" aria-labelledby="inspection-tab-project" tabIndex={0} hidden={v.tab !== 'project'}>
      <h2 className="inspection-project-name">{props.project.name}</h2><dl><div><dt>Host folder</dt><dd className="mono catalog-path">{props.project.path}</dd></div><div><dt>Availability</dt><dd>{props.project.available ? 'Folder available' : 'Folder missing or identity changed'}</dd></div>{props.live ? <><div><dt>Live session</dt><dd className="mono catalog-path">{props.live.session_id}</dd></div><div><dt>Model</dt><dd>{props.live.provider} / {props.live.model}</dd></div></> : <div><dt>Saved history</dt><dd>Read-only, current branch</dd></div>}</dl>
      <details className="remove-project"><summary>Remove project registration</summary><p className="fine">Only the manager entry is removed. Project files and every saved session are retained.</p><form method="post" action={`/projects/${encodeURIComponent(v.project)}/remove`}><input type="hidden" name="csrf" value={props.csrf}/><label className="checkbox-label"><input type="checkbox" name="confirm" value="remove" required/> Remove this registration; keep all files and sessions</label><button type="submit" className="button danger">Remove from manager</button></form></details>
    </section>
    <noscript><p className="fine">Files and Changes need JavaScript. Inspection never starts an agent.</p></noscript>
  </>;
}
