import {flushSync} from 'react-dom';
import {createRoot} from 'react-dom/client';
import type {Root} from 'react-dom/client';
import {Panel} from './Panel';
import {bootstrap, liveProjection, mergeFiles, validChanges, validDiff, validFile, validFiles} from './model';
import type {Bootstrap, Change, Entry, Tab} from './model';
type Channel = 'files' | 'file' | 'changes' | 'diff';
type Notice = {status: string; error: string};
type Preview = {path: string; text: string; notice: string};
export type View = {
  panel: HTMLElement; mount: HTMLElement; root: Root; props: Bootstrap; project: string; tab: Tab; path: string; next: number;
  owner: HTMLElement | null; instance: string; session: string; observer: MutationObserver; lifecycle: AbortController;
  requests: Partial<Record<Channel, AbortController>>;
  files: Entry[]; changes: Change[]; filesPending: boolean; changesPending: boolean; filesLoaded: boolean; changesLoaded: boolean;
  filesFresh: boolean; changesFresh: boolean; filesGeneration: number; changesGeneration: number; more: boolean;
  fileGeneration: number; diffGeneration: number; fileSelected: string; fileBusy: string; diffSelected: string; diffBusy: string; file: Preview | null; diff: Preview | null;
  notices: {files: Notice; changes: Notice};
};
let view: View | null = null;
const routes = Object.freeze({files: 'inspect/files', file: 'inspect/file', changes: 'inspect/changes', diff: 'inspect/diff'});
function same(v: View) {
  return view === v && v.mount.isConnected && v.panel.contains(v.mount) && document.querySelector('#project-inspector') === v.panel && v.panel.dataset.project === v.project && String(v.props.project.available) === v.panel.dataset.available &&
    (!v.owner || (document.querySelector('#live-session') === v.owner && v.owner.dataset.project === v.project && (v.owner.dataset.instance || '') === v.instance && (v.owner.dataset.session || '') === v.session));
}
function scope(v: View) { if (same(v)) return true; if (view === v) dispose(); return false; }
function visible(v: View) { return scope(v) && !v.panel.hidden && document.visibilityState === 'visible'; }
function paint(v: View) { if (same(v)) flushSync(() => v.root.render(<Panel view={v} actions={actions}/>)); }
function notice(v: View, tab: 'files' | 'changes', message: string, failed = false) { v.notices[tab] = {status: failed ? '' : message, error: failed ? message : ''}; }
function abort(v: View, channel: Channel) { v.requests[channel]?.abort(); delete v.requests[channel]; }
function resetPreview(v: View, channel: 'file' | 'diff') { v[channel === 'file' ? 'fileGeneration' : 'diffGeneration']++; abort(v, channel); if (channel === 'file') { v.file = null; v.fileBusy = ''; v.fileSelected = ''; } else { v.diff = null; v.diffBusy = ''; v.diffSelected = ''; } }
function suspend(v: View) {
  for (const channel of ['files', 'file', 'changes', 'diff'] as const) abort(v, channel);
  v.filesGeneration++; v.changesGeneration++; v.filesPending = false; v.changesPending = false;
  v.fileGeneration++; v.diffGeneration++; v.fileBusy = ''; v.diffBusy = ''; paint(v);
}
export function init() {
  const panel = document.querySelector<HTMLElement>('#project-inspector'); if (view?.panel === panel && same(view)) return;
  dispose(); const mount = panel?.querySelector<HTMLElement>('[data-react-inspection]'); if (!panel || !mount) return;
  let props: Bootstrap | null = null;
  try { const encoded = mount.dataset.reactProps || ''; if (encoded.length <= 32768) props = bootstrap(JSON.parse(encoded)); } catch { /* Private bootstrap is never logged. */ }
  if (!props || props.project.id !== panel.dataset.project || String(props.project.available) !== panel.dataset.available) return;
  const owner = document.querySelector<HTMLElement>('#live-session');
  const v: View = {panel, mount, root: createRoot(mount), props, project: props.project.id, tab: 'files', path: '.', next: 0,
    owner, instance: owner?.dataset.instance || '', session: owner?.dataset.session || '', observer: new MutationObserver(() => { if (scope(v) && !visible(v)) suspend(v); }), lifecycle: new AbortController(), requests: {},
    files: [], changes: [], filesPending: false, changesPending: false, filesLoaded: false, changesLoaded: false, filesFresh: false, changesFresh: false, filesGeneration: 0, changesGeneration: 0, more: false,
    fileGeneration: 0, diffGeneration: 0, fileSelected: '', fileBusy: '', diffSelected: '', diffBusy: '', file: null, diff: null,
    notices: {files: {status: 'Open Files to inspect this host project.', error: ''}, changes: {status: 'Open Changes to inspect Git changes.', error: ''}}};
  view = v; paint(v);
  v.observer.observe(panel, {attributes: true, attributeFilter: ['hidden', 'data-project', 'data-available']});
  if (owner) v.observer.observe(owner, {attributes: true, attributeFilter: ['data-project', 'data-instance', 'data-session']});
  const workspace = panel.closest('#workspace'); if (workspace?.parentElement) v.observer.observe(workspace.parentElement, {childList: true});
  document.addEventListener('visibilitychange', () => { if (!scope(v)) return; if (!visible(v)) suspend(v); }, {signal: v.lifecycle.signal});
}
// Presentation only: callers publish the new immutable owner via init first.
// This path neither initiates reads nor opens/selects any inspection tab.
export function refresh(value: unknown): boolean {
  const v = view;
  if (!v || !same(v) || !v.owner?.isConnected) return false;
  const live = liveProjection(value, v.props.project.id, {project_id: v.project, instance_id: v.instance, session_id: v.session});
  if (!live) return false;
  v.props = {...v.props, live};
  paint(v);
  return true;
}
export function dispose() { const old = view; view = null; if (!old) return; old.observer.disconnect(); old.lifecycle.abort(); for (const controller of Object.values(old.requests)) controller.abort(); flushSync(() => old.root.unmount()); }
async function inspect(v: View, channel: Channel, fields: Record<string, string>): Promise<unknown> {
  abort(v, channel); const controller = new AbortController(); v.requests[channel] = controller;
  const timer = setTimeout(() => controller.abort(), 10000);
  const current = () => visible(v) && v.requests[channel] === controller && !controller.signal.aborted;
  try {
    const response = await fetch(`/projects/${encodeURIComponent(v.project)}/${routes[channel]}`, {method: 'POST', credentials: 'same-origin', cache: 'no-store', redirect: 'error',
      headers: {'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8', Accept: 'application/json'}, body: new URLSearchParams({csrf: v.props.csrf, ...fields}), signal: controller.signal});
    if (!response.ok) throw new Error('unavailable');
    // Server listing/preview projections are bounded; cap JSON before parsing as well.
    const body = await response.text(); if (controller.signal.aborted && v.requests[channel] === controller) throw new Error('timeout'); if (!current()) return null;
    if (body.length > 3 * 1024 * 1024) throw new Error('oversized');
    return JSON.parse(body) as unknown;
  } catch {
    if (!scope(v) || v.requests[channel] !== controller) return null;
    throw new Error(controller.signal.aborted ? 'Inspection timed out. Refresh to try again.' : 'Inspection unavailable. The path may be protected, unsupported, too large, or changed. Refresh to try again.');
  } finally { clearTimeout(timer); }
}
function readable(v: View, tab: 'files' | 'changes') {
  if (!visible(v) || v.tab !== tab) return false;
  if (v.panel.dataset.available !== 'true') { notice(v, tab, 'Project folder is missing or its identity changed. Re-register the folder before inspecting it.', true); paint(v); return false; }
  return true;
}
async function files(v: View, path = '.', offset = 0, append = false) {
  if (!readable(v, 'files')) return;
  const generation = ++v.filesGeneration; v.filesPending = true; v.filesFresh = false; resetPreview(v, 'file'); notice(v, 'files', 'Reading project files…'); paint(v);
  try {
    const data = await inspect(v, 'files', {path, offset: String(offset)});
    if (data === null || !visible(v) || v.filesGeneration !== generation) return;
    if (!validFiles(data)) throw new Error('Unexpected file listing. Refresh to try again.');
    v.path = data.path; v.next = data.next_offset; v.filesLoaded = true; v.filesFresh = true;
    v.files = mergeFiles(v.files, data.entries, append); v.more = data.has_more && v.files.length < 4096;
    notice(v, 'files', data.limited ? 'Listing limit reached. Refresh or open a subfolder to inspect more.' : v.files.length ? `${v.files.length} entries shown. Protected names, links and special files are omitted.` : 'No visible files in this folder. Protected names, links and special files are omitted.');
  } catch (error) { if (same(v) && v.filesGeneration === generation) notice(v, 'files', (error as Error).message + (v.files.length ? ' Previously listed rows are stale until refresh succeeds.' : ''), true); }
  finally { if (same(v) && v.filesGeneration === generation) { v.filesPending = false; paint(v); const trail = v.panel.querySelector<HTMLElement>('[data-inspection-path]'); if (trail) trail.scrollLeft = trail.scrollWidth; } }
}
async function changes(v: View) {
  if (!readable(v, 'changes')) return;
  const generation = ++v.changesGeneration; v.changesPending = true; v.changesFresh = false; resetPreview(v, 'diff'); notice(v, 'changes', 'Reading working changes…'); paint(v);
  try {
    const data = await inspect(v, 'changes', {});
    if (data === null || !visible(v) || v.changesGeneration !== generation) return;
    if (!validChanges(data)) throw new Error('Unexpected changes response. Refresh to try again.');
    v.changesLoaded = true; v.changesFresh = true; v.changes = data.available ? data.changes : [];
    notice(v, 'changes', !data.available ? data.reason || 'Git changes are unavailable for this project.' : data.limited ? 'Change listing truncated. Only a bounded set is shown.' : v.changes.length ? `${v.changes.length} changes shown. Select a file for a read-only diff.` : 'No working changes reported.');
  } catch (error) { if (same(v) && v.changesGeneration === generation) notice(v, 'changes', (error as Error).message + (v.changes.length ? ' Previously listed rows are stale until refresh succeeds.' : ''), true); }
  finally { if (same(v) && v.changesGeneration === generation) { v.changesPending = false; paint(v); } }
}
async function file(v: View, entry: Entry, generation: number) {
  if (!readable(v, 'files') || v.filesPending || !v.filesFresh || v.filesGeneration !== generation || !v.files.includes(entry)) return;
  if (entry.kind === 'directory') { void files(v, entry.path); return; }
  resetPreview(v, 'file'); v.fileBusy = entry.path; notice(v, 'files', 'Reading text preview…'); paint(v);
  const selection = v.fileGeneration;
  const current = () => same(v) && v.fileGeneration === selection && v.filesGeneration === generation && v.tab === 'files' && v.fileBusy === entry.path;
  try {
    const data = await inspect(v, 'file', {path: entry.path}); if (data === null || !current()) return;
    if (!validFile(data) || data.path !== entry.path) throw new Error('Unexpected file preview. Refresh to try again.');
    v.file = {path: data.path, text: data.text, notice: data.truncated ? 'Preview truncated to 64 KiB. The original file is unchanged.' : data.text ? 'Read-only UTF-8 text preview.' : 'This file is empty.'};
    v.fileSelected = entry.path; notice(v, 'files', 'Preview loaded. Files on disk are unchanged.');
  } catch (error) { if (current()) notice(v, 'files', (error as Error).message, true); }
  finally { if (current()) { v.fileBusy = ''; paint(v); } }
}
async function diff(v: View, change: Change, generation: number) {
  if (!readable(v, 'changes') || v.changesPending || !v.changesFresh || v.changesGeneration !== generation || !v.changes.includes(change)) return;
  const key = `${change.kind}:${change.path}`; resetPreview(v, 'diff'); v.diffBusy = key; notice(v, 'changes', 'Reading diff preview…'); paint(v);
  const selection = v.diffGeneration;
  const current = () => same(v) && v.diffGeneration === selection && v.changesGeneration === generation && v.tab === 'changes' && v.diffBusy === key;
  try {
    const data = await inspect(v, 'diff', {path: change.path, kind: change.kind}); if (data === null || !current()) return;
    if (!validDiff(data)) throw new Error('Unexpected diff response. Refresh to try again.');
    if (!data.available) { notice(v, 'changes', data.reason || 'A diff is unavailable for this file.'); return; }
    if (data.path !== change.path || data.kind !== change.kind) throw new Error('Unexpected diff response. Refresh to try again.');
    v.diff = {path: data.path, text: data.text, notice: data.truncated ? 'Diff truncated for bounded display.' : data.text ? `Read-only diff · ${change.kind}` : 'No text diff is available for this file.'};
    v.diffSelected = key; notice(v, 'changes', 'Diff loaded. No files or Git state were changed.');
  } catch (error) { if (current()) notice(v, 'changes', (error as Error).message, true); }
  finally { if (current()) { v.diffBusy = ''; paint(v); } }
}
function selectTab(v: View, name: Tab, focus = false) {
  if (!scope(v)) return;
  if (v.tab !== name) {
    if (v.tab === 'files') { resetPreview(v, 'file'); abort(v, 'files'); v.filesGeneration++; v.filesPending = false; if (/^(Reading text preview|Preview loaded)/.test(v.notices.files.status)) notice(v, 'files', 'Select a file for a read-only preview.'); }
    if (v.tab === 'changes') { resetPreview(v, 'diff'); abort(v, 'changes'); v.changesGeneration++; v.changesPending = false; if (/^(Reading diff preview|Diff loaded)/.test(v.notices.changes.status)) notice(v, 'changes', 'Select a file for a read-only diff.'); }
  }
  v.tab = name; paint(v);
  if (focus) v.panel.querySelector<HTMLButtonElement>(`[data-inspection-tab="${name}"]`)?.focus();
  if (!visible(v) || name === 'project') return;
  if (!readable(v, name)) return;
  if (name === 'files' && !v.filesLoaded && !v.filesPending) void files(v);
  if (name === 'changes' && !v.changesLoaded && !v.changesPending) void changes(v);
}
export function opened() { if (view) selectTab(view, view.tab); }
export function select(name: string, project: string) { if (!view || !scope(view) || view.project !== project || !['files', 'changes', 'project'].includes(name)) return false; selectTab(view, name as Tab); return true; }
const actions = {files, changes, file, diff, selectTab};
export type Actions = typeof actions;
export const inspection = Object.freeze({init, opened, select, refresh, dispose});
