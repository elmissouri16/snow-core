import {createRef} from 'react';
import type {RefObject} from 'react';
import {flushSync} from 'react-dom';
import {createRoot} from 'react-dom/client';
import type {Root} from 'react-dom/client';
import {GoalPanel} from './GoalPanel.tsx';
import {budget, facts, record, sameScope, terminal, validGoal} from './model.ts';
import type {Action, API, Fields, Goal, Inspection, Snapshot, UI} from './model.ts';

export {validGoal} from './model.ts';
type Draft = {enabled: boolean; budget: string};
type Confirmation = Readonly<{action: Action; fields: Fields; expectedRevision: number; target: string}>;
export type View = {
  api: API; root: Root; launcher: HTMLElement; statusHost: HTMLElement; draft: Draft; snapshot: Snapshot | null; goal: Goal | null;
  ui: UI | null; inspected: Inspection | null; invalid: boolean; error: string;
  read: {controller: AbortController} | null; confirmation: Confirmation | null;
  committing: boolean; submitting: boolean; composing: boolean; consent: boolean; reserved: boolean;
  dialog: RefObject<HTMLDialogElement | null>;
  consentInput: RefObject<HTMLInputElement | null>;
  closeButton: RefObject<HTMLButtonElement | null>;
  startButton: RefObject<HTMLButtonElement | null>;
  resumeButton: RefObject<HTMLButtonElement | null>;
};
const drafts = new Map<string, Draft>();
let view: View | null = null;

export function init(api: API): void {
  dispose();
  const container = api.root?.querySelector<HTMLElement>('[data-react-live-panel="goals"]');
  const launcher = api.root?.querySelector<HTMLElement>('[data-goals-launcher]');
  const statusHost = api.root?.querySelector<HTMLElement>('[data-goal-composer-status]');
  if (!container || !launcher || !statusHost || api.root?.dataset.goalsEnabled !== 'true') return;
  const key = JSON.stringify([api.identity.project_id, api.identity.session_id]);
  const draft = drafts.get(key) || {enabled: false, budget: ''};
  drafts.delete(key); drafts.set(key, draft);
  while (drafts.size > 16) { const oldest = drafts.keys().next(); if (!oldest.done) drafts.delete(oldest.value); }
  view = {api, root: createRoot(container), launcher, statusHost, draft, snapshot: null, goal: null, ui: null, inspected: null,
    invalid: false, error: '', read: null, confirmation: null, committing: false, submitting: false, composing: false, consent: false, reserved: false,
    dialog: createRef(), consentInput: createRef(), closeButton: createRef(), startButton: createRef(), resumeButton: createRef()};
  controls(view);
}
export function dispose(): void {
  if (!view) return;
  const old = view; view = null;
  old.read?.controller.abort();
  if (old.dialog.current) old.api.closeDialog(old.dialog.current);
  // The adapter fences this release to this panel and immutable runtime owner.
  if (old.reserved) { old.reserved = false; old.api.reserve(false); }
  flushSync(() => old.root.unmount());
}
export function render(snapshot: Snapshot | null, ui: UI): void {
  if (!view) return;
  const projected = snapshot?.goal;
  const goal = validGoal(projected, view.api.identity.session_id) ? projected : null;
  if (view.goal?.running && !goal?.running) view.inspected = null;
  view.snapshot = snapshot; view.ui = ui; view.invalid = snapshot?.goal != null && !goal; view.goal = goal;
  controls(view);
}
export function canCompose(current: View): boolean {
  return !!current.ui?.supported && !!current.ui.safe && !current.invalid && !current.read && !current.confirmation && !current.committing && !current.submitting && !!current.goal && (!current.goal.goal_id || terminal(current.goal)) && current.snapshot?.mode === 'default';
}
export function composerState() {
  return {enabled: !!view?.draft.enabled, busy: !!view?.submitting, available: !!view && canCompose(view)};
}
function toggle(current: View): void {
  if (view !== current || current.submitting || current.committing || (!current.draft.enabled && !canCompose(current))) return;
  current.draft.enabled = !current.draft.enabled; current.error = '';
  current.api.changed(); current.api.focusPrompt();
}
export function ready(current: View): boolean {
  const snapshot = current.snapshot;
  return !!current.ui?.safe && !current.invalid && !current.read && !current.committing && !current.composing && sameScope(current.inspected, snapshot, current.goal) && !current.goal?.running && snapshot?.mode === 'default' && ['ask', 'allow', 'deny'].includes(String(snapshot.permission_mode)) && !!snapshot.provider && !!snapshot.model;
}
function controls(current: View): void {
  if (view === current) flushSync(() => current.root.render(<GoalPanel view={current} actions={actions} />));
}
function release(current: View): void {
  if (current.reserved) { current.reserved = false; current.api.reserve(false); }
}
async function inspect(current: View, composer = false): Promise<void> {
  if (view !== current || !current.ui?.readable || current.invalid || current.read || current.confirmation || current.committing || !current.goal) return;
  const branch = current.goal.branch_id, operation = {controller: new AbortController()};
  current.read = operation; current.error = ''; controls(current);
  try {
    const result = await current.api.inspect(branch, operation.controller.signal);
    if (view !== current || current.read !== operation || operation.controller.signal.aborted || !(composer ? current.draft.enabled : current.dialog.current?.open)) return;
    if (!record(result) || !validGoal(result.goal, current.api.identity.session_id) || result.goal.branch_id !== branch || typeof result.revision !== 'number' || !Number.isSafeInteger(result.revision) || result.revision <= 0) throw new Error('Goal scope changed');
    current.inspected = {goal: result.goal, facts: facts(result), revision: result.revision};
  } catch {
    if (view === current && current.read === operation && !operation.controller.signal.aborted) {
      current.inspected = null; current.error = 'Goal inspection failed or changed scope. Nothing started. Refresh explicitly to inspect again.';
    }
  } finally { if (view === current && current.read === operation) { current.read = null; controls(current); } }
}
function cancel(current: View): void {
  if (view !== current || current.committing) return;
  const restoreFocus = document.activeElement?.closest('[data-goal-confirmation]');
  current.read?.controller.abort(); current.read = null; current.confirmation = null; current.consent = false;
  release(current); controls(current);
  if (current.dialog.current?.open && restoreFocus) (current.goal?.goal_id && !terminal(current.goal) ? current.resumeButton : current.startButton).current?.focus({preventScroll: true});
}
function close(current: View): void {
  if (view !== current || current.committing) return;
  cancel(current); if (current.dialog.current) current.api.closeDialog(current.dialog.current);
}
function prepare(current: View, action: Action): void {
  if (view !== current || !ready(current) || current.confirmation || !current.inspected) return;
  const goal = current.inspected.goal;
  if (action !== 'goal-resume' || !goal.goal_id || terminal(goal) || goal.budget_remaining === 0) return;
  const fields: Fields = {session_id: goal.session_id, branch_id: goal.branch_id, expected_tip_id: goal.tip_id, expected_goal_id: goal.goal_id};
  current.confirmation = {action, fields, expectedRevision: current.inspected.revision,
    target: `Resume on branch ${goal.branch_id}, exact tip ${goal.tip_id || '(empty history)'}, reviewed goal ${goal.goal_id}. Existing objective and budget remain unchanged.`};
  current.consent = false;
  if (!current.api.reserve(true)) { current.confirmation = null; return; }
  current.reserved = true;
  controls(current); current.consentInput.current?.focus({preventScroll: true});
}
async function commit(current: View): Promise<void> {
  if (view !== current || !ready(current) || !current.confirmation || !current.consent || !current.dialog.current?.open) return;
  const confirmation = current.confirmation; current.confirmation = null; current.committing = true; current.consent = false;
  controls(current); current.api.closeDialog(current.dialog.current);
  let result = false;
  // A rejected/lost receipt is uncertain, never an invitation to resend.
  try { result = await current.api.run(confirmation.action, confirmation.fields, confirmation.expectedRevision); } catch { /* retain draft and require review */ }
  if (view !== current) return;
  current.committing = false; current.inspected = null; release(current);
  current.error = result ? 'The goal run was admitted. Follow its current status; admission does not mean the goal is complete.' : 'Goal run outcome needs review. Your objective and message draft are kept. Nothing will be retried automatically.';
  controls(current);
}
/** Send is the explicit goal-start intent. Inspection may validate that intent,
 * never retarget it or replay a write after a changed draft/scope or lost ACK. */
export async function submit(objective: string, draftUnchanged: () => boolean): Promise<boolean> {
  const current = view;
  if (!current?.draft.enabled || !canCompose(current)) return false;
  const tokens = budget(current.draft.budget);
  if (!current.api.validText(objective) || [...objective].length > 32768 || tokens === false) {
    current.api.notice(tokens === false ? 'Use a positive whole-token budget in Goal details, or leave it empty.' : 'A goal needs valid text within 32,768 characters and 64 KiB.');
    return false;
  }
  const revision = current.snapshot?.revision;
  if (!Number.isSafeInteger(revision) || typeof revision !== 'number' || !current.goal) return false;
  const expected: Inspection = {goal: current.goal, facts: facts(current.snapshot!), revision};
  current.submitting = true; current.inspected = null;
  if (!current.api.reserve(true)) { current.submitting = false; current.api.changed(); return false; }
  current.reserved = true;
  try {
    await inspect(current, true);
    if (view !== current) return false;
    const inspected = current.inspected as Inspection | null; // inspect fills this asynchronously.
    // A successful native goal inspection publishes exactly one new revision.
    // Accept only that read's advance, with every captured goal/authority fact
    // unchanged, then bind admission to its exact inspected revision.
    if (!draftUnchanged() || !current.draft.enabled || !inspected || inspected.revision !== expected.revision + 1 || !ready(current) || !sameScope({...expected, revision: inspected.revision}, current.snapshot, current.goal)) {
      current.api.notice(current.error || 'The draft or session changed before goal submission. Nothing started. Review it and submit again.');
      return false;
    }
    const goal = expected.goal;
    const fields: Fields = {session_id: goal.session_id, branch_id: goal.branch_id, expected_tip_id: goal.tip_id, expected_goal_id: goal.goal_id,
      objective, ...(tokens ? {token_budget: tokens} : {})};
    current.committing = true; controls(current);
    const admitted = await current.api.run('goal-start', fields, inspected.revision);
    if (view !== current) return false;
    if (admitted && draftUnchanged()) current.draft.enabled = false;
    if (!admitted) current.api.notice('The goal run could not be confirmed. Your draft is kept. Review the runtime before trying again; nothing will be retried automatically.');
    return admitted;
  } catch {
    if (view === current) current.api.notice('The goal run could not be confirmed. Your draft is kept; nothing will be retried automatically.');
    return false;
  } finally {
    if (view === current) { current.submitting = false; current.committing = false; current.inspected = null; release(current); controls(current); }
  }
}
const actions = {
  inspect, cancel, close, prepare, commit, toggle,
  write(current: View) {
    if (view !== current || !canCompose(current)) return;
    close(current); current.draft.enabled = true; current.api.changed(); current.api.focusPrompt();
  },
  open(current: View, trigger: HTMLButtonElement) {
    if (view !== current || !current.ui?.readable || current.invalid || !current.dialog.current) return;
    current.api.openDialog(current.dialog.current, trigger);
    // Native initial focus lands on Refresh, which inspection disables. Move
    // it to a stable control without changing the parent-owned return target.
    current.closeButton.current?.focus({preventScroll: true});
    void inspect(current);
  },
  draft(current: View, field: 'budget', value: string) { if (view === current) { current.draft[field] = value; controls(current); } },
  consent(current: View, value: boolean) { if (view === current) { current.consent = value; controls(current); } },
  compose(current: View, value: boolean) { if (view === current) { current.composing = value; controls(current); } },
};
export type Actions = typeof actions;
export const goals = Object.freeze({init, render, dispose, validGoal, composerState, submit});
export default goals;
