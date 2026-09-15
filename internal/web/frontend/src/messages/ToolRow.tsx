/* Conversation presentation adapted from Harness (MIT); see HARNESS-NOTICE.txt. */
import { useLayoutEffect, useRef } from 'react';
import type { ToolData } from './model';
const paths: Record<string, string> = {
  file: 'M6 2h8l4 4v16H6z M14 2v5h4', tool: 'm8 6-6 6 6 6 M16 6l6 6-6 6 M14 4l-4 16', chevron: 'm9 5 7 7-7 7',
  search: 'M16 10a6 6 0 1 1-12 0 6 6 0 0 1 12 0Zm-2 4 6 6', terminal: 'm4 6 6 6-6 6 M13 18h7', edit: 'm4 16 12-12 4 4-12 12-5 1z M13 7l4 4'
};
const kinds: Record<string, [string, string]> = {glob: ['search', 'Find files'], grep: ['search', 'Search'], read: ['file', 'Read'], bash: ['terminal', 'Run'], write: ['edit', 'Write'], edit: ['edit', 'Edit']};
function Icon({kind, className}: {kind: string; className: string}) {
  return <svg viewBox="0 0 24 24" aria-hidden="true" className={`inspection-icon ${className}`} data-kind={kind} fill="none" stroke="currentColor" strokeWidth="1.5"><path d={paths[kind] || paths.tool}/></svg>;
}
export function ToolContents({data}: {data: ToolData}) {
  const wireName = data.tool || 'Tool', [kind, title] = Object.hasOwn(kinds, wireName) ? kinds[wireName] : ['tool', wireName];
  const normalized = (value: string) => value.trim().replace(/\s+/g, ' ').toLowerCase();
  const detail = [wireName, title].some(value => normalized(value) === normalized(data.summary)) ? '' : data.summary;
  return <><summary aria-label={`${title === wireName ? wireName : `${title} (${wireName})`}${detail ? ` · ${detail}` : ''} · ${data.label}`} aria-disabled={!data.expandable} tabIndex={data.expandable ? 0 : -1} onClick={event => { if (!data.expandable) event.preventDefault(); }}>
    <span className="activity-leading"><Icon kind={kind} className="activity-kind"/><Icon kind="chevron" className="activity-chevron"/></span><span className="activity-tool" title={wireName}>{title}</span><span className="activity-separator" aria-hidden="true" hidden={!detail}/><span className="activity-summary" title={detail} hidden={!detail}>{detail}</span><span className={`activity-status${!data.error && ['completed', 'complete', 'success', 'done'].includes(data.status) ? ' activity-status-quiet' : ''}`}>{data.label}</span>
  </summary><div className="activity-body" hidden={!data.expandable}><pre className="activity-output">{data.output}</pre><p className="fine activity-truncated" hidden={!data.truncated}>Tool output truncated for bounded display.</p></div></>;
}
export function ToolRow({data, history = false}: {data: ToolData; history?: boolean}) {
  const row = useRef<HTMLDetailsElement>(null), initial = useRef(true);
  useLayoutEffect(() => {
    if (row.current && (!data.expandable || initial.current && data.open)) row.current.open = data.expandable && !!data.open;
    initial.current = false;
  }, [data.expandable, data.open]);
  return <details ref={row} className={`tool-activity${history ? ' history-tool' : ''}${data.error ? ' activity-error' : ''}${!data.expandable ? ' activity-no-output' : ''}`} data-history-tool-id={history ? data.id : undefined} data-activity-id={!history ? data.id : undefined} data-tool-name={data.tool || 'Tool'} data-status={data.status} data-output-available={history ? String(data.available) : undefined} data-truncated={history ? String(data.rawTruncated) : undefined}><ToolContents data={data}/></details>;
}
