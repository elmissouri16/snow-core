import {createRoot} from 'react-dom/client';
import type {Root} from 'react-dom/client';
import {flushSync} from 'react-dom';
import {activeTurn, choices, isPolicy, record, sessionChoices, validName} from './model';
import type {Choices, Controls, Hooks, SessionChoice, Snapshot} from './model';
import {Heading, Leading, Trailing, Dialogs} from './Surfaces';
import {Menu} from './Menu';
import type {Glyph} from './Icons';

export type MenuKind = 'session' | 'model' | 'mode' | 'permission-policy' | 'telemetry';
type MenuHost = {
  open(options: {trigger: HTMLElement; panel: HTMLElement; placement: string; managedTrigger: true; onClose(): void; onBack(): boolean}): void;
  close(options?: {restoreFocus?: boolean}): void;
  reposition(): void;
};
// The legacy host owns geometry, focus and the external panel lifetime only.
const menus = () => (window as Window & {SnowMenus?: MenuHost}).SnowMenus;
type Popup = {kind: MenuKind; pane: string; query: string; trigger: HTMLElement; panel: HTMLDivElement; root: Root};
let state: Controller | null = null;
let menuSequence = 0;
export class Controller {
  readonly element: HTMLElement;
  readonly hooks: Hooks;
  readonly roots = new Map<string, Root>();
  readonly workflow: boolean;
  readonly policy: boolean;
  readonly projectName: string;
  readonly projectPath: string;
  snapshot: Snapshot;
  controls: Controls = {};
  choices: Choices | null = null;
  sessionChoices: SessionChoice[] | null = null;
  loading = false;
  loadError = '';
  generation = 0;
  menu: Popup | null = null;
  observer: MutationObserver | null = null;
  renameDialog: HTMLDialogElement | null = null;
  switchDialog: HTMLDialogElement | null = null;
  policyDialog: HTMLDialogElement | null = null;
  nameInput: HTMLInputElement | null = null;
  renameInstance = '';
  renameSession = '';
  renameDraft = '';
  switchTarget: {sessionID: string; instance: string} | null = null;
  policyTarget: {instance: string; session: string; previous: string} | null = null;
  consent = false;
  constructor(element: HTMLElement, hooks: Hooks) {
    this.element = element; this.hooks = hooks;
    this.workflow = element.dataset.workflowEnabled === 'true';
    this.policy = element.dataset.permissionPolicyEnabled === 'true';
    this.projectName = element.dataset.projectName || ''; this.projectPath = element.dataset.projectPath || '';
    this.snapshot = {project_id: element.dataset.project || '', instance_id: element.dataset.instance || '', session_id: element.dataset.session || '', session_name: element.dataset.sessionName, provider: element.dataset.provider, model: element.dataset.model, status: element.dataset.status || 'opening'};
    for (const kind of ['heading', 'leading', 'trailing', 'dialogs']) {
      const mount = element.querySelector(`[data-react-conversation="${kind}"]`);
      if (mount) this.roots.set(kind, createRoot(mount));
    }
  }
  current = () => state === this && this.element.isConnected;
  valid = (instance: string) => this.current() && this.snapshot.instance_id === instance;
  canChange = () => this.current() && !!this.controls.safe && !this.loading && this.snapshot.status === 'idle';
  canSwitch = () => this.current() && !!this.controls.safe && !this.loading && (this.snapshot.status === 'idle' || activeTurn(this.snapshot.status));
  canSetPolicy = () => this.canChange() && isPolicy(this.snapshot.permission_mode) && !this.snapshot.permission && !this.snapshot.input && !['admitted', 'admission_unknown'].includes(this.snapshot.recovery?.state || '');
  modelName = () => this.choices?.models.find(item => item.provider === this.snapshot.provider && item.id === this.snapshot.model)?.name || this.snapshot.model || 'Model unknown';
  publish = () => {
    if (!this.current()) return;
    flushSync(() => {
      this.roots.get('heading')?.render(<Heading c={this} />);
      this.roots.get('leading')?.render(<Leading c={this} />);
      this.roots.get('trailing')?.render(<Trailing c={this} />);
      this.roots.get('dialogs')?.render(<Dialogs c={this} />);
    });
    this.paintMenu();
  };
  paintMenu = () => {
    const menu = this.menu; if (!menu || !this.current()) return;
    const focused = menu.panel.contains(document.activeElement);
    const active = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const key = active?.dataset.menuKey, search = active?.matches('[data-model-search]');
    menu.panel.setAttribute('aria-busy', String(menu.kind !== 'telemetry' && this.loading));
    flushSync(() => menu.root.render(<Menu c={this} menu={menu} />));
    if (focused && (!menu.panel.contains(document.activeElement) || (document.activeElement instanceof HTMLButtonElement && document.activeElement.disabled))) {
      const buttons = [...menu.panel.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')];
      const input = menu.panel.querySelector<HTMLInputElement>('[data-model-search]');
      (search ? input : buttons.find(button => button.dataset.menuKey === key) || input || buttons[0] || menu.panel)?.focus({preventScroll: true});
    }
    menus()?.reposition();
  };
  dismiss = (restoreFocus = true) => { if (this.menu) menus()?.close({restoreFocus}); };
  pane = (pane: string) => {
    if (!this.menu) return;
    this.menu.pane = pane; this.paintMenu();
    this.menu?.panel.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus();
  };
  search = (query: string) => {
    if (!this.menu) return;
    this.menu.query = query; this.paintMenu();
    const content = this.menu?.panel.querySelector('.snow-menu-content'); if (content) content.scrollTop = 0;
  };
  showMenu = (kind: MenuKind, trigger: HTMLElement) => {
    const host = menus(); if (!host || !this.current() || !trigger.isConnected) return;
    if (this.menu?.trigger === trigger) { this.dismiss(); return; }
    host.close({restoreFocus: false});
    const panel = document.createElement('div');
    panel.id = `snow-conversation-menu-${++menuSequence}`;
    panel.className = `conversation-task-menu${kind === 'telemetry' ? ' telemetry-menu' : ''}${kind === 'model' ? ' model-picker-menu' : ''}`;
    panel.setAttribute('aria-label', {'model': 'Model selection', session: 'Conversation actions', mode: 'Collaboration mode', 'permission-policy': 'Session permissions', telemetry: 'Context and usage'}[kind]);
    if (kind === 'telemetry' || kind === 'model') panel.setAttribute('role', 'dialog');
    const menu: Popup = {kind, trigger, panel, root: createRoot(panel), pane: 'root', query: ''};
    this.menu = menu; this.publish();
    host.open({trigger, panel, managedTrigger: true, placement: kind === 'session' ? 'bottom-end' : 'top-end', onClose: () => {
      if (this.menu === menu) this.menu = null;
      flushSync(() => menu.root.unmount());
      this.publish();
    }, onBack: () => { if (this.menu !== menu || menu.pane === 'root') return false; this.pane('root'); return true; }});
    if (kind === 'model' && !this.choices && !this.loading) void this.load();
  };
  runtimeActions = () => {
    const definitions: [Glyph, string, string][] = [
      ['versions', 'Conversation versions', '[data-versions-open]'], ['goals', 'Thread goal', '[data-goals-open]'],
      ['processes', 'Managed processes', '[data-processes-open]'], ['reasoning', 'Thinking & response', '[data-reasoning-open]'],
      ['compaction', 'Compact context', '[data-compaction-open]'], ['steer', 'Steer current run…', '[data-steer-open]'],
    ];
    return definitions.flatMap(([key, label, selector]) => {
      const source = this.element.querySelector<HTMLButtonElement>(selector);
      // Goal's primary control is a composer-mode toggle. Its details proxy is
      // intentionally hidden, but only an advertised goal exposes the menu action.
      return source && (!source.hidden || key === 'goals' && source.dataset.goalsAvailable === 'true') ? [{key, label, source}] : [];
    });
  };
  mutate = async (instance: string, kind: string, fields: Record<string, string>) => {
    if (!this.valid(instance) || !this.canChange()) return;
    this.dismiss(); await this.hooks.action(kind, fields);
  };
  load = async () => {
    if (!this.canChange()) return;
    this.loading = true; this.loadError = '';
    const generation = ++this.generation, {instance_id: instance, project_id: project} = this.snapshot;
    this.publish();
    try {
      const response = await this.hooks.choices();
      if (!this.valid(instance) || generation !== this.generation) return;
      const value = choices(response, instance, project); if (!value) throw new Error('Invalid choices');
      this.choices = value; this.sessionChoices = value.sessions;
    } catch { if (this.current() && generation === this.generation) this.loadError = 'Host choices unavailable. Try again.'; }
    finally { if (this.current() && generation === this.generation) { this.loading = false; this.publish(); } }
  };
  switchTo = async (sessionID: string, trigger: HTMLElement, sidebarTarget = false) => {
    if (!this.canSwitch()) return false;
    if (sessionID === this.snapshot.session_id) { this.dismiss(); return true; }
    if (sessionID && !sidebarTarget && !(this.sessionChoices || this.choices?.sessions)?.some(item => item.session_id === sessionID)) return false;
    this.dismiss();
    if (activeTurn(this.snapshot.status)) {
      if (!this.switchDialog) return false;
      this.switchTarget = {sessionID, instance: this.snapshot.instance_id}; this.publish();
      this.hooks.openDialog(this.switchDialog, trigger); return true;
    }
    return await this.hooks.action('switch', {session_id: sessionID});
  };
  openRename = (trigger?: HTMLElement) => {
    if (!this.canChange() || !trigger?.isConnected || !this.renameDialog) return;
    this.dismiss(false); this.renameInstance = this.snapshot.instance_id; this.renameSession = this.snapshot.session_id;
    this.renameDraft = this.snapshot.session_name || ''; this.publish();
    this.nameInput?.setCustomValidity(''); this.hooks.openDialog(this.renameDialog, trigger);
  };
  rename = async () => {
    if (!this.canChange() || this.renameInstance !== this.snapshot.instance_id || this.renameSession !== this.snapshot.session_id || !this.renameDialog?.open) return;
    const name = this.renameDraft.trim();
    if (!validName(name)) { this.nameInput?.setCustomValidity('Enter a name of at most 256 UTF-8 bytes.'); this.nameInput?.reportValidity(); return; }
    const dialog = this.renameDialog, instance = this.snapshot.instance_id;
    await this.hooks.action('rename', {name});
    if (this.valid(instance)) this.hooks.closeDialog(dialog);
  };
  closeWorkflow = (kind: 'switch' | 'rename') => {
    this.switchTarget = null;
    const dialog = kind === 'switch' ? this.switchDialog : this.renameDialog;
    if (dialog) this.hooks.closeDialog(dialog);
  };
  confirmSwitch = async () => {
    const target = this.switchTarget;
    if (!this.switchDialog?.open || !this.canSwitch() || !target || target.instance !== this.snapshot.instance_id) return;
    this.switchTarget = null; this.hooks.closeDialog(this.switchDialog);
    await this.hooks.action('switch', {session_id: target.sessionID, confirm_stop: 'stop'});
  };
  choosePolicy = (mode: string, trigger: HTMLElement, instance: string) => {
    if (!this.valid(instance) || !this.canSetPolicy() || !isPolicy(mode)) return;
    if (this.snapshot.permission_mode === mode) { this.dismiss(); return; }
    if (mode !== 'allow') { void this.mutate(instance, 'permission-mode', {mode, session_id: this.snapshot.session_id}); return; }
    this.dismiss(); if (!this.policyDialog) return;
    this.policyTarget = {instance, session: this.snapshot.session_id, previous: this.snapshot.permission_mode!};
    this.consent = false; this.publish(); this.hooks.openDialog(this.policyDialog, trigger);
  };
  cancelPolicy = () => {
    this.policyTarget = null; this.consent = false;
    if (this.policyDialog) this.hooks.closeDialog(this.policyDialog);
    this.publish();
  };
  confirmPolicy = async () => {
    const target = this.policyTarget;
    if (!target || !this.policyDialog?.open || !this.consent || !this.canSetPolicy() || target.instance !== this.snapshot.instance_id || target.session !== this.snapshot.session_id || target.previous !== this.snapshot.permission_mode) return;
    this.cancelPolicy();
    await this.hooks.action('permission-mode', {mode: 'allow', session_id: target.session, confirm_allow: 'allow'});
  };
}
export function init(element: HTMLElement, hooks: Hooks) {
  dispose(); if (!element?.querySelector('[data-react-conversation]')) return;
  const c = state = new Controller(element, hooks); c.publish();
  c.observer = new MutationObserver(() => { if (c.current() && c.menu?.kind === 'session') c.paintMenu(); });
  const actions = element.querySelector('.live-manager-controls');
  if (actions) c.observer.observe(actions, {subtree: true, childList: true, attributes: true, attributeFilter: ['disabled', 'hidden']});
}
export function render(snapshot: Snapshot | null, controls: Controls = {}) {
  const c = state; if (!c?.current()) return;
  c.controls = controls;
  if (snapshot) {
    if (c.snapshot.instance_id !== snapshot.instance_id || c.snapshot.project_id !== snapshot.project_id) {
      c.choices = null; c.sessionChoices = null; c.loading = false; c.generation++; c.loadError = ''; c.switchTarget = null;
      c.dismiss(false);
      if (c.switchDialog) c.hooks.closeDialog(c.switchDialog);
      if (c.renameDialog) c.hooks.closeDialog(c.renameDialog);
    } else if (c.snapshot.session_id !== snapshot.session_id) {
      c.dismiss(false); c.switchTarget = null;
      if (c.switchDialog) c.hooks.closeDialog(c.switchDialog);
      if (c.renameDialog) c.hooks.closeDialog(c.renameDialog);
    }
    c.snapshot = snapshot;
  }
  if ((!c.canSetPolicy() && c.menu?.kind === 'permission-policy') || (!c.canChange() && c.menu?.kind === 'mode')) c.dismiss(false);
  const target = c.policyTarget;
  if (target && (!c.canSetPolicy() || target.instance !== c.snapshot.instance_id || target.session !== c.snapshot.session_id || target.previous !== c.snapshot.permission_mode)) c.cancelPolicy();
  c.publish();
}
export function dispose() {
  const c = state; if (!c) return;
  c.dismiss(false); state = null; c.observer?.disconnect(); c.generation++;
  for (const dialog of [c.renameDialog, c.switchDialog, c.policyDialog]) if (dialog) c.hooks.closeDialog(dialog);
  flushSync(() => { for (const root of c.roots.values()) root.unmount(); });
}
export async function select({project, session = '', instance, trigger}: {project: string; session?: string; instance?: string; trigger: HTMLElement}) {
  const c = state;
  if (!c?.canSwitch() || c.snapshot.project_id !== project || (instance && c.snapshot.instance_id !== instance)) return false;
  const currentInstance = c.snapshot.instance_id;
  if (session && session !== c.snapshot.session_id) {
    const sidebarTarget = trigger?.dataset.project === project && trigger.dataset.instance === currentInstance && trigger.closest<HTMLElement>('[data-shell-session]')?.dataset.shellSession === session;
    if (sidebarTarget) return await c.switchTo(session, trigger, true);
    if (activeTurn(c.snapshot.status) || !c.hooks.sessions) return false;
    c.loading = true; const generation = ++c.generation; c.publish();
    try {
      const inventory = await c.hooks.sessions(session);
      if (!c.valid(currentInstance) || generation !== c.generation || !record(inventory) || inventory.project_id !== project || inventory.instance_id !== currentInstance || !inventory.available) return false;
      const sessions = sessionChoices(inventory.sessions); if (!sessions) return false;
      c.sessionChoices = sessions;
    } catch { return false; }
    finally { if (c.current() && generation === c.generation) { c.loading = false; c.publish(); } }
    if (!c.canSwitch() || !c.sessionChoices?.some(item => item.session_id === session)) return false;
  }
  if (!c.valid(currentInstance)) return false;
  return await c.switchTo(session, trigger);
}
export const conversation = Object.freeze({init, render, dispose, select, rename: (trigger: HTMLElement) => state?.openRename(trigger)});
