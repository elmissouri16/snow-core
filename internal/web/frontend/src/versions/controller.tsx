import { createRoot } from 'react-dom/client';
import { flushSync } from 'react-dom';
import { Panel } from './Panel';
import { actions, cursor, historyID, record, sameMetadata, sameSelection, validHistoryInventory, validName, validPage, validPreparation, validPreview, verifiedSelection } from './model';
import type { HistoryAction, Selection, Version } from './model';
import type { HistoryAPI, HistoryUI, HistoryView, Operation, Snapshot, VersionsAPI, VersionsUI, VersionsView } from './types';

// These synchronous controller objects, not React effects or render snapshots,
// own captured identities, read lifetimes and one-use mutation confirmations.
let view: VersionsView | null = null;
let history: HistoryView | null = null;
const drafts = new Map<string, {name: string}>();
const $ = <T extends HTMLElement = HTMLElement>(selector: string, scope: ParentNode) => scope.querySelector<T>(selector);
function publish() {
  const current = view;
  if (!current) return;
  flushSync(() => current.root.render(<Panel v={current} h={history} readable={canRead()} restorable={canRestore()} historySafe={historySafe()} selection={historySelection()} historyChanged={publish} />));
}
function init(api: VersionsAPI) {
  dispose();
  const host = $<HTMLElement>('[data-react-live-panel="versions"]', api.root);
  if (!host || api.root.dataset.versionsEnabled !== 'true') return;
  const controller = new AbortController();
  const current: VersionsView = {api: {...api, identity: Object.freeze({...api.identity})}, host, root: createRoot(host), dialog: null, controller, page: null, selected: null, preview: null, listCursors: [''], previewCursors: [''], listIndex: 0, previewIndex: 0};
  view = current; publish();
  const dialog = current.dialog;
  if (!dialog) { dispose(); return; }
  api.root.addEventListener('click', onClick, {signal: controller.signal});
  dialog.addEventListener('cancel', event => { event.preventDefault(); close(); }, {signal: controller.signal});
  dialog.addEventListener('close', () => { if (view === current && current.phase !== 'committing') cancelPreparation(); }, {signal: controller.signal});
}
function dispose() {
  if (!view) return;
  const old = view; view = null;
  old.operation?.controller.abort(); old.controller.abort(); old.token = '';
  if (history?.api.root === old.api.root) historyDispose();
  if (old.dialog) old.api.closeDialog(old.dialog);
  flushSync(() => old.root.unmount());
}
function canRead() { return !!view?.ui?.readable && !view.operation && !view.phase; }
function canRestore() { return !!view?.ui?.restoreSafe && !view.stale && !!view.selected && !!view.preview && !view.selected.current && !view.operation && view.preview.branch_id === view.selected.branch_id && view.preview.tip_id === view.selected.tip_id; }
function render(snapshot: Snapshot | null, ui: VersionsUI) { if (view) { view.ui = ui; view.snapshot = snapshot; publish(); } }
function selection(): Readonly<Selection> | null { return verifiedSelection(view); }
function refresh() { return list(0, true); }
function cancelPreparation() {
  if (!view || view.phase === 'committing') return;
  view.operation?.controller.abort(); view.operation = null; view.phase = null; view.token = '';
  view.api.restoreState(null); publish();
  if (view?.dialog?.open) $('[data-version-restore]', view.dialog)?.focus({preventScroll: true});
}
function close() { if (view?.phase !== 'committing') { cancelPreparation(); if (view?.dialog) view.api.closeDialog(view.dialog); } }
function begin(current: VersionsView, kind: string): Operation {
  const operation = {kind, controller: new AbortController()};
  current.operation?.controller.abort(); current.operation = operation; current.error = ''; publish(); return operation;
}
function currentOperation(current: VersionsView, operation: Operation) { return view === current && current.operation === operation && !operation.controller.signal.aborted && current.dialog?.open; }
async function list(index = 0, refresh = false) {
  if (!view || !canRead() || !view.dialog?.open || index < 0 || index >= 64) return;
  if (refresh) {
    // Retain presentation only; even an identical refresh revokes authority.
    if (view.page) view.retained = {page: view.page, selected: view.selected || view.retained?.selected, preview: view.preview || view.retained?.preview, previewIndex: view.previewIndex};
    view.stale = true; view.listCursors = ['']; view.listIndex = 0; view.page = null; view.preview = null; view.selected = null;
  }
  const requested = view.listCursors[index]; if (!cursor(requested)) return;
  const current = view, operation = begin(current, 'list');
  try {
    const value = await current.api.list(requested, operation.controller.signal);
    if (!currentOperation(current, operation)) return;
    if (!validPage(value, current.api.identity) || index > 0 && (value.current_branch_id !== current.page?.current_branch_id || value.current_tip_id !== current.page?.current_tip_id) || value.has_more && current.listCursors.slice(0, index + 1).includes(value.next_cursor)) throw new Error('Unverified versions page');
    current.page = value; if (refresh) current.stale = false; current.listIndex = index; current.listCursors = current.listCursors.slice(0, index + 1);
    if (value.has_more) current.listCursors.push(value.next_cursor);
  } catch {
    if (currentOperation(current, operation)) { current.stale = true; current.error = 'Could not verify this versions page. Nothing changed. Refresh explicitly to read again.'; }
  } finally { if (view === current && current.operation === operation) { current.operation = null; publish(); } }
}
async function preview(target: Version | null | undefined, index = 0) {
  if (!view || !canRead() || !target || index < 0 || index >= 64) return;
  if (target !== view.selected) {
    if (target.branch_id !== view.retained?.selected?.branch_id || target.tip_id !== view.retained?.selected?.tip_id) view.retained = null;
    view.selected = target; view.preview = null; view.previewCursors = ['']; view.previewIndex = 0;
  }
  const requested = view.previewCursors[index]; if (!cursor(requested)) return;
  const current = view, operation = begin(current, 'preview');
  try {
    const value = await current.api.preview(target, requested, operation.controller.signal);
    if (!currentOperation(current, operation)) return;
    if (!validPreview(value, target, current.api.identity) || value.has_more && current.previewCursors.slice(0, index + 1).includes(value.next_cursor)) throw new Error('Unverified preview');
    current.preview = value; current.retained = null; current.previewIndex = index; current.previewCursors = current.previewCursors.slice(0, index + 1);
    if (value.has_more) current.previewCursors.push(value.next_cursor);
  } catch {
    if (currentOperation(current, operation)) { current.stale = true; current.error = 'Could not verify the selected branch and tip. The active chat is unchanged; refresh explicitly to read again.'; }
  } finally { if (view === current && current.operation === operation) { current.operation = null; publish(); } }
}
async function prepare() {
  if (!view || !canRestore() || view.phase || !view.dialog?.open || !view.selected || !view.page) return;
  const target = view.selected, origin = view.page, current = view, operation = begin(current, 'prepare');
  current.phase = 'preparing'; current.restoreTarget = `Selected branch: ${target.branch_id}; exact tip: ${target.tip_id || '(empty history)'}.`;
  current.api.restoreState('preparing'); publish();
  if (current.dialog) $('[data-version-restore-cancel]', current.dialog)?.focus({preventScroll: true});
  try {
    const value = await current.api.prepare(target, origin, operation.controller.signal);
    if (!currentOperation(current, operation)) return;
    if (!validPreparation(value, target, origin, current.api.identity)) throw new Error('Unverified restore preparation');
    current.token = value.restore_token; current.expires = Date.parse(value.expires_at); current.phase = 'ready'; current.api.restoreState('ready');
  } catch {
    if (currentOperation(current, operation)) { current.token = ''; current.phase = null; current.stale = true; current.error = 'Restore preparation was rejected or could not be verified. Nothing changed. Refresh before explicitly preparing again.'; current.api.restoreState(null); }
  } finally { if (view === current && current.operation === operation) { current.operation = null; publish(); } }
}
async function commit() {
  if (!view || !canRestore() || view.phase !== 'ready' || !view.token || Date.now() >= (view.expires ?? 0) || !view.dialog?.open) return;
  const current = view, token = view.token, dialog = view.dialog;
  current.token = ''; current.phase = 'committing'; current.api.restoreState('committing');
  current.api.closeDialog(dialog);
  const result = await current.api.commit(token);
  if (view !== current) return;
  current.phase = null; current.api.restoreState(null);
  current.error = result ? 'Saved conversation version restored. No prompt was replayed.' : 'Restore outcome needs review. Your draft is kept; nothing will be retried automatically. Review the workspace before continuing.';
  publish();
}
function onClick(event: Event) {
  const button = event.target instanceof Element ? event.target.closest('button') : null;
  if (!view || !button) return;
  if (button.matches('[data-versions-open]') && view.ui?.readable && view.dialog) { view.api.openDialog(view.dialog, button); void list(0, true); }
  else if (button.matches('[data-versions-close]')) close();
  else if (button.matches('[data-versions-refresh]')) void list(0, true);
  else if (button.matches('[data-versions-more]')) void list(view.listIndex + 1);
  else if (button.matches('[data-versions-back]')) void list(view.listIndex - 1);
  else if (button.matches('[data-version-preview]')) void preview(view.page?.versions.find(item => item.branch_id === button.dataset.versionId && item.tip_id === button.dataset.tipId));
  else if (button.matches('[data-version-preview-more]')) void preview(view.selected, view.previewIndex + 1);
  else if (button.matches('[data-version-preview-back]')) void preview(view.selected, view.previewIndex - 1);
  else if (button.matches('[data-version-restore]')) void prepare();
  else if (button.matches('[data-version-restore-cancel]')) cancelPreparation();
  else if (button.matches('[data-version-restore-confirm]')) void commit();
}

function historyInit(api: HistoryAPI) {
  historyDispose();
  const panel = $('[data-history-controls]', api.root), dialog = $<HTMLDialogElement>('#versions-dialog', api.root);
  if (!view || view.api.root !== api.root || !panel || !dialog || api.root.dataset.historyControlEnabled !== 'true') return;
  const key = JSON.stringify([api.identity.project_id, api.identity.session_id]), draft = drafts.get(key) || {name: ''};
  drafts.delete(key); drafts.set(key, draft); while (drafts.size > 16) drafts.delete(drafts.keys().next().value!);
  const controller = new AbortController();
  const current: HistoryView = {api: {...api, identity: Object.freeze({...api.identity})}, panel, dialog, draft, controller, consent: false};
  history = current;
  panel.addEventListener('click', historyClick, {signal: controller.signal});
  panel.addEventListener('submit', event => { event.preventDefault(); void historyCommit(); }, {signal: controller.signal});
  panel.addEventListener('input', event => {
    if (history !== current) return;
    if (event.target instanceof HTMLInputElement && event.target.matches('[data-history-name]')) { draft.name = event.target.value; event.target.setCustomValidity(''); }
    // A checkbox's native input precedes React's controlled-state restoration.
    if (event.target instanceof HTMLInputElement && event.target.matches('[data-history-consent]')) current.consent = event.target.checked;
    publish();
  }, {signal: controller.signal});
  panel.addEventListener('change', event => {
    if (history !== current) return;
    if (event.target instanceof HTMLInputElement && event.target.matches('[data-history-consent]')) current.consent = event.target.checked;
    publish();
  }, {signal: controller.signal});
  for (const type of ['compositionstart', 'compositionend']) panel.addEventListener(type, () => { if (history === current) { current.composing = type === 'compositionstart'; publish(); } }, {signal: controller.signal});
  dialog.addEventListener('close', () => { if (history === current && !current.busy) historyCancel(); }, {signal: controller.signal});
  publish();
}
function historyDispose() {
  if (!history) return;
  const old = history; history = null; old.controller.abort(); publish();
  // Never abort an admitted mutation; app owns its uncertain-outcome fence.
}
function historySelection(): Readonly<Selection> | null {
  const selected = selection(), identity = history?.api.identity;
  return selected && identity && (['project_id', 'instance_id', 'session_id'] as const).every(key => selected[key] === identity[key]) && Number.isSafeInteger(selected.revision) && selected.revision > 0 && selected.revision === history?.snapshot?.revision ? selected : null;
}
function renderInventory(value: unknown) {
  const current = history;
  if (!current || !view || current.api.root !== view.api.root || current.dialog !== view.dialog || !view.host.isConnected || !current.panel.isConnected || !validHistoryInventory(value, current.api.identity.project_id)) return;
  current.inventory = {text: value.text, rows: value.rows.map(({name, url}) => ({name, url}))};
  publish();
}
function historySafe() { return !!history?.ui?.safe && !history.busy && !history.invalid && !history.composing; }
function historyRender(snapshot: Snapshot | null, ui: HistoryUI) { if (history) { history.snapshot = snapshot; history.ui = ui; publish(); } }
function historyPrepare(action: HistoryAction) {
  if (!history || !actions.has(action) || !historySafe() || history.confirmation || !history.dialog.open) return;
  const target = historySelection(); if (!target) return;
  const input = $<HTMLInputElement>('[data-history-name]', history.panel); if (!input) return;
  const name = input.value;
  if (!validName(name, action)) { input.setCustomValidity(`Use a nonempty name with no surrounding spaces or control characters, at most ${action === 'history-session-fork' ? 72 : 64} characters and 256 UTF-8 bytes.`); input.reportValidity(); return; }
  if (action === 'history-branch-rename' && !validName(target.name, action)) { history.error = 'The saved label cannot authorize this rename. Refresh Versions.'; publish(); return; }
  history.error = '';
  if (action !== 'history-branch-fork') { void historyApply(action, target, name); return; }
  history.confirmation = {action, target: Object.freeze({...target}), name}; history.consent = false;
  const description = "Create and activate a new branch in this conversation. It inherits the selected history's mode and effective thinking; current provider, model and permissions stay unchanged.";
  history.targetText = `Selected branch ${target.branch_id}; exact saved tip ${target.tip_id || '(empty history)'}. Name: ${name}. ${description}`;
  history.confirmLabel = 'Create and activate branch';
  if (history.api.reserve(true) === false) { history.confirmation = null; publish(); return; }
  publish();
  if (history) $('[data-history-consent]', history.panel)?.focus({preventScroll: true});
}
function historyCancel() {
  if (!history || history.busy) return;
  history.confirmation = null; history.consent = false; history.api.reserve(false); publish();
}
async function historyCommit() {
  if (!history || !historySafe() || !history.confirmation || !history.dialog.open || !history.consent || !sameSelection(history.confirmation.target, historySelection())) return;
  const {action, target, name} = history.confirmation;
  await historyApply(action, target, name);
}
async function historyApply(action: HistoryAction, target: Selection, name: string) {
  if (!history || !historySafe() || !history.dialog.open || !sameSelection(target, historySelection())) return;
  const current = history;
  current.busy = true;
  if (current.api.reserve(true) === false) { current.busy = false; publish(); return; }
  current.confirmation = null; current.consent = false;
  const fields: Record<string, string> = {session_id: target.session_id, expected_revision: String(target.revision), current_branch_id: target.current_branch_id, current_tip_id: target.current_tip_id, branch_id: target.branch_id, tip_id: target.tip_id, name};
  if (action === 'history-branch-rename') fields.old_name = target.name;
  publish();
  let result: unknown;
  try { result = await current.api.mutate(action, fields); } catch { result = null; }
  if (history !== current) return;
  current.busy = false; current.api.reserve(false); current.invalid = true;
  if (!result) { current.error = 'History action outcome needs review. Your drafts are kept. Nothing will be retried automatically; reload the workspace before continuing.'; publish(); return; }
  if (action !== 'history-branch-fork' && (!sameMetadata(result, target, name) || action === 'history-session-fork' && (!historyID(result.child_session_id) || result.child_session_id === target.session_id))) {
    current.error = 'Unverified history response. Reload the workspace; do not retry this mutation.'; publish(); return;
  }
  current.invalid = false;
  current.error = action === 'history-session-fork' ? 'Detached conversation created. Refreshing saved-conversation inventory; choose Open explicitly to use it. Current history is unchanged.' : action === 'history-branch-rename' ? 'Selected branch renamed. Refresh Versions before reviewing another action.' : 'New branch activated. No prompt or tools were replayed.';
  if (action === 'history-session-fork' && record(result) && typeof result.child_session_id === 'string') current.api.refreshInventory?.(result.child_session_id);
  if (action === 'history-branch-rename') void refresh();
  publish();
}
function historyClick(event: Event) {
  const button = event.target instanceof Element ? event.target.closest('button') : null;
  if (!history || !button) return;
  if (button.matches('[data-history-review], [data-history-action]')) historyPrepare((button.dataset.historyAction || button.dataset.historyReview) as HistoryAction);
  else if (button.matches('[data-history-cancel]')) historyCancel();
}
export const versions = Object.freeze({init, render, dispose, selection, refresh});
export const historyControls = Object.freeze({init: historyInit, render: historyRender, dispose: historyDispose, selectionChanged: publish, validName, renderInventory});
