import {LIMITS, boundedText, canDelete, validateInventory} from './model.ts';
import type {DeleteAuthority, LiveSelection, Navigate, SessionRow, ShellBootstrap, ShellCommand} from './model.ts';
export interface Group {
  project: string; expanded: boolean; rows: SessionRow[]; instance: string; loaded: boolean;
  available: boolean; deleteSupported: boolean; activeSession: string; loading: boolean;
  error: string; nextOffset: number; hasMore: boolean; truncated: boolean; pages: number;
}
export type ShellMenu = {kind: 'session' | 'project' | 'view' | 'workspace'; project: string; session: string; instance: string; trigger: HTMLElement; owner: object};
export interface DeleteAttempt {
  project: string; session: string; instance: string; name: string; trigger: HTMLElement;
  owner: object; pending: boolean; submitted: boolean; error: string;
}
export interface ViewState {
  bootstrap: ShellBootstrap | null; groups: Map<string, Group>; live: LiveSelection | null;
  query: string; searchOpen: boolean; hideSessions: boolean; pinnedOnly: boolean;
  collapsed: boolean; navOpen: boolean; narrow: boolean; theme: 'light' | 'dark';
  settings: 'general' | 'workspaces' | 'access' | null; settingsReturn: HTMLElement | null;
  menu: ShellMenu | null; deletion: DeleteAttempt | null;
}
const initialGroup = (project: string, expanded: boolean): Group => ({project, expanded, rows: [], instance: '', loaded: false,
  available: true, deleteSupported: false, activeSession: '', loading: false, error: '', nextOffset: 0, hasMore: false, truncated: false, pages: 0});
export class ShellController {
  snapshot: ViewState = {bootstrap: null, groups: new Map(), live: null, query: '', searchOpen: false, hideSessions: false, pinnedOnly: false,
    collapsed: false, navOpen: false, narrow: false, theme: 'dark', settings: null, settingsReturn: null, menu: null, deletion: null};
  listeners = new Set<() => void>();
  requests = new Map<string, AbortController>();
  sidebarMemory: {list: number; rail: number; focused: {project: string; session: string; selector: string; href: string} | null; selection: [number, number] | null} | null = null;
  rememberSidebar() {
    const nav = document.getElementById('project-navigation');
    if (!nav) return;
    const active = document.activeElement instanceof HTMLElement && nav.contains(document.activeElement) ? document.activeElement : null;
    const input = nav.querySelector<HTMLInputElement>('[data-sidebar-search]');
    this.sidebarMemory = {list: nav.querySelector('.project-tree')?.scrollTop || 0, rail: nav.scrollTop,
      focused: active ? {project: active.closest<HTMLElement>('[data-sidebar-project]')?.dataset.sidebarProject || '', session: active.closest<HTMLElement>('[data-shell-session]')?.dataset.shellSession || '',
        selector: ['[data-sidebar-search]', '[data-workspace-toggle]', '[data-shell-project-new]', '[data-shell-project-menu]', '[data-shell-session-menu]', '[data-settings-open]', '[data-sidebar-collapse]'].find(selector => active.matches(selector)) || '', href: active.getAttribute('href') || ''} : null,
      selection: active === input && input ? [input.selectionStart || 0, input.selectionEnd || 0] : null};
  }
  restoreSidebar() {
    const memory = this.sidebarMemory, nav = document.getElementById('project-navigation');
    if (!memory || !nav) return;
    this.sidebarMemory = null;
    const tree = nav.querySelector<HTMLElement>('.project-tree');
    if (tree) tree.scrollTop = memory.list;
    nav.scrollTop = memory.rail;
    const focused = memory.focused, active = document.activeElement;
    if (!focused || active && active !== document.body && active.id !== 'workspace' && active.isConnected) return;
    const project = [...nav.querySelectorAll<HTMLElement>('[data-sidebar-project]')].find(node => node.dataset.sidebarProject === focused.project);
    const row = project && [...project.querySelectorAll<HTMLElement>('[data-shell-session]')].find(node => node.dataset.shellSession === focused.session);
    const scope = row || project || nav;
    const target = focused.selector ? scope.querySelector<HTMLElement>(focused.selector) : [...scope.querySelectorAll<HTMLAnchorElement>('a')].find(node => node.getAttribute('href') === focused.href);
    target?.focus({preventScroll: true});
    if (memory.selection && target instanceof HTMLInputElement) target.setSelectionRange(...memory.selection);
    if (target && tree?.contains(target)) {
      const bounds = tree.getBoundingClientRect(), rect = target.getBoundingClientRect();
      if (rect.top < bounds.top) tree.scrollTop -= bounds.top - rect.top;
      else if (rect.bottom > bounds.bottom) tree.scrollTop += rect.bottom - bounds.bottom;
    }
  }
  owner: object = {}; mounted = false; reads = 0; observer?: MutationObserver;
  navigate: Navigate = href => { window.location.assign(href); };
  command: (value: ShellCommand) => void = value => document.dispatchEvent(new CustomEvent('snow:shell-command', {detail: value}));
  getSnapshot = () => this.snapshot;
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; };
  publish(patch: Partial<ViewState>) { this.snapshot = {...this.snapshot, ...patch}; this.listeners.forEach(fn => fn()); }
  group(project: string, patch: Partial<Group>) {
    const previous = this.snapshot.groups.get(project);
    if (!previous) return;
    const groups = new Map(this.snapshot.groups); groups.set(project, {...previous, ...patch}); this.publish({groups});
  }
  mount(bootstrap: ShellBootstrap, navigate?: Navigate, command?: (value: ShellCommand) => void) {
    this.owner = {}; this.mounted = true; this.reads = 0;
    if (navigate) this.navigate = navigate;
    if (command) this.command = command;
    const groups = new Map<string, Group>();
    for (const project of bootstrap.projects) {
      const old = this.snapshot.groups.get(project.id);
      groups.set(project.id, old ? {...old, loading: false, deleteSupported: false, loaded: false, pages: 0} : initialGroup(project.id, project.id === bootstrap.project));
    }
    const selected = groups.get(bootstrap.project);
    if (selected) {
      const rows = new Map(selected.rows.map(row => [row.session_id, row]));
      bootstrap.sessions.forEach(row => rows.set(row.session_id, row));
      selected.rows = [...rows.values()].slice(0, LIMITS.rows);
    }
    this.publish({bootstrap, groups, live: null, deletion: null, menu: null, settings: null,
      theme: document.documentElement.dataset.theme === 'light' ? 'light' : 'dark'});
    this.updateLive(bootstrap.live);
    this.initialize();
    for (const group of this.snapshot.groups.values()) if (group.expanded && !this.snapshot.hideSessions) void this.load(group.project);
  }
  dispose() {
    this.rememberSidebar(); this.mounted = false; this.owner = {}; this.observer?.disconnect(); this.preemptInventory();
    this.publish({menu: null, deletion: null, settings: null});
  }
  initialize = () => {
    this.observer?.disconnect();
    const live = document.querySelector<HTMLElement>('#live-session[data-runtime="true"]');
    if (!live) return;
    this.observer = new MutationObserver(() => this.syncCurrent(live));
    this.observer.observe(live, {attributes: true, subtree: true, childList: true, characterData: true,
      attributeFilter: ['disabled', 'data-session', 'data-instance']});
    document.querySelectorAll('[data-workflow-new], [data-workflow-switch-confirm], [data-workflow-rename], [data-workflow-rename-form] button[type=submit]').forEach(control => this.observer?.observe(control, {attributes: true, attributeFilter: ['disabled']}));
    this.syncCurrent(live);
  };
  syncCurrent = (live: HTMLElement | null) => {
    if (!live?.isConnected || live.dataset.runtime !== 'true') return;
    const rename = document.querySelector<HTMLButtonElement>('[data-workflow-rename], [data-workflow-rename-form] button[type="submit"]');
    const create = document.querySelector<HTMLButtonElement>('[data-workflow-new], [data-workflow-switch-confirm]');
    this.updateLive({project: live.dataset.project || '', session: live.dataset.session || '', instance: live.dataset.instance || '',
      title: live.querySelector('[data-live-title]')?.textContent || 'New conversation', renameAvailable: !!rename, renameDisabled: !rename || rename.disabled, newDisabled: !!create?.disabled});
  };
  updateLive = (live: LiveSelection | null) => {
    if (!this.mounted || JSON.stringify(this.snapshot.live) === JSON.stringify(live)) return;
    if (live && (!live.project || !live.session || !live.instance || !this.snapshot.groups.has(live.project))) return;
    const old = this.snapshot.live;
    this.publish({live});
    if (!live) return;
    const state = this.snapshot.groups.get(live.project)!;
    // Row IDs are immutable. Never turn the prior active row into the target.
    // Retain both through an instance change while the inventory is pending.
    const rows = state.rows.some(row => row.session_id === live.session)
      ? state.rows.map(row => row.session_id === live.session ? {...row, name: live.title} : row)
      : [{session_id: live.session, name: live.title}, ...state.rows].slice(0, LIMITS.rows);
    const changedOwner = state.instance !== live.instance;
    if (changedOwner) { this.requests.get(live.project)?.abort(); this.requests.delete(live.project); }
    this.group(live.project, {rows, activeSession: live.session, ...(changedOwner ? {instance: live.instance, deleteSupported: false, loaded: false, loading: false} : {})});
    if (changedOwner && old && state.expanded && !this.snapshot.hideSessions) void this.load(live.project);
  };
  preemptInventory = () => {
    for (const [project, request] of this.requests) {
      request.abort(); this.group(project, {loading: false, deleteSupported: false, loaded: false});
    }
    this.requests.clear();
  };
  async load(project: string, offset = 0) {
    const state = this.snapshot.groups.get(project);
    if (!this.mounted || !state || state.loading) return;
    if (this.reads >= LIMITS.reads || state.pages >= LIMITS.pages) {
      this.group(project, {truncated: true, error: 'Session inventory read limit reached. Reload to refresh.'}); return;
    }
    this.requests.get(project)?.abort();
    const request = new AbortController(), owner = this.owner, instance = state.instance;
    this.requests.set(project, request); this.reads++;
    this.group(project, {loading: true, error: '', pages: state.pages + 1});
    const valid = () => this.mounted && this.owner === owner && this.requests.get(project) === request;
    const timer = setTimeout(() => request.abort(), 10000);
    try {
      const response = await fetch(`/projects/${encodeURIComponent(project)}/sidebar-sessions?offset=${offset}`, {credentials: 'same-origin', signal: request.signal, headers: {Accept: 'application/json'}});
      if (!response.ok) throw new Error('Inventory unavailable');
      const value = JSON.parse(await boundedText(response, LIMITS.response));
      if (!valid()) return;
      const data = validateInventory(value, project, offset, instance, this.snapshot.live);
      const rows = new Map((offset ? this.snapshot.groups.get(project)!.rows : []).map(row => [row.session_id, row]));
      data.rows.forEach(row => rows.set(row.session_id, row));
      const live = this.snapshot.live;
      if (live?.project === project) rows.set(live.session, {session_id: live.session, name: live.title});
      let ordered = [...rows.values()];
      if (live?.project === project && ordered.length > LIMITS.rows) ordered = [rows.get(live.session)!, ...ordered.filter(row => row.session_id !== live.session)];
      this.group(project, {...data, rows: ordered.slice(0, LIMITS.rows), loaded: true, loading: false});
    } catch {
      if (valid()) this.group(project, {error: 'Sessions could not be loaded.', deleteSupported: false, loading: false});
    } finally {
      clearTimeout(timer);
      if (valid()) this.requests.delete(project);
    }
  }
  toggle(project: string) {
    const group = this.snapshot.groups.get(project); if (!group) return;
    this.group(project, {expanded: !group.expanded});
    if (!group.expanded && !group.loaded && !this.snapshot.hideSessions) void this.load(project);
  }
  syncVisibility = (hide: boolean) => {
    this.publish({hideSessions: hide});
    if (!hide) for (const group of this.snapshot.groups.values()) if (group.expanded && !group.loaded && !group.loading) void this.load(group.project);
  };
  invalidate = (project: string) => {
    this.requests.get(project)?.abort(); this.requests.delete(project);
    this.group(project, {loading: false, loaded: false, deleteSupported: false});
    const group = this.snapshot.groups.get(project);
    if (group?.expanded && !this.snapshot.hideSessions) void this.load(project);
  };
  deleted = (project: string, session: string) => {
    const group = this.snapshot.groups.get(project); if (!group) return;
    this.group(project, {rows: group.rows.filter(row => row.session_id !== session)}); this.invalidate(project);
  };
  authority(project: string, session: string): DeleteAuthority | undefined {
    const group = this.snapshot.groups.get(project), live = this.snapshot.live;
    if (!group || !group.rows.some(row => row.session_id === session)) return;
    return {supported: group.deleteSupported, available: group.loaded && group.available,
      active: group.activeSession === session || live?.project === project && live.session === session, instance: group.instance};
  }
  eligible(attempt: Pick<DeleteAttempt, 'project' | 'session' | 'instance' | 'owner' | 'trigger'>) {
    const authority = this.authority(attempt.project, attempt.session);
    const marker = attempt.trigger.dataset;
    // Typed inventory grants authority; a changed presentation owner may only
    // revoke it. A retained popup must not act through a repurposed launcher.
    return this.mounted && attempt.owner === this.owner && attempt.trigger.isConnected &&
      marker.project === attempt.project && marker.session === attempt.session && marker.instance === attempt.instance &&
      marker.deleteSupported === 'true' && marker.deleteAvailable === 'true' && marker.deleteActive === 'false' &&
      canDelete(authority) && authority?.instance === attempt.instance;
  }
  closeMenu = (restoreFocus = true) => {
    const trigger = this.snapshot.menu?.trigger;
    this.publish({menu: null});
    if (restoreFocus && trigger?.isConnected) trigger.focus({preventScroll: true});
  };
  openDelete(menu: ShellMenu) {
    if (!this.eligible(menu) || this.snapshot.deletion?.pending) return;
    const name = this.snapshot.groups.get(menu.project)?.rows.find(row => row.session_id === menu.session)?.name || 'Untitled session';
    this.closeMenu(false);
    this.publish({deletion: {...menu, name, pending: false, submitted: false, error: ''}});
  }
  closeDelete = () => {
    const previous = this.snapshot.deletion; if (!previous || previous.pending) return;
    this.publish({deletion: null});
    queueMicrotask(() => {
      const trigger = previous.trigger as HTMLButtonElement;
      const link = trigger.closest('[data-shell-session]')?.querySelector<HTMLElement>('a');
      const fallback = [...document.querySelectorAll<HTMLElement>('[data-sidebar-project]')].find(row => row.dataset.sidebarProject === previous.project)?.querySelector<HTMLElement>('[data-workspace-toggle]');
      (trigger.isConnected ? (!trigger.hidden && !trigger.disabled ? trigger : link) : fallback)?.focus({preventScroll: true});
    });
  };
  async remove(confirmed: boolean) {
    const attempt = this.snapshot.deletion, csrf = this.snapshot.bootstrap?.csrf;
    if (!attempt || !confirmed || !csrf || attempt.pending || attempt.submitted) return;
    if (!this.eligible(attempt)) { this.closeDelete(); return; }
    // Synchronous admission protects double clicks before React commits.
    attempt.pending = true; attempt.submitted = true;
    this.publish({deletion: {...attempt}});
    const request = new AbortController(), timer = setTimeout(() => request.abort(), 15000);
    let failure = 'Deletion could not be confirmed. It may have completed. Refresh the session list before trying again; no automatic retry was sent.';
    try {
      const response = await fetch(`/projects/${encodeURIComponent(attempt.project)}/sessions/${encodeURIComponent(attempt.session)}/delete`, {
        method: 'POST', credentials: 'same-origin', signal: request.signal, headers: {Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded'},
        body: new URLSearchParams({csrf, confirm: 'delete', instance_id: attempt.instance})});
      const text = await boundedText(response, 65536);
      if (!response.ok) {
        if ([400, 401, 403, 404, 409].includes(response.status) && response.headers.get('content-type')?.startsWith('text/plain') && text.length <= 1024) failure = text.trim() + ' No automatic retry was sent.';
        throw new Error('Deletion not confirmed');
      }
      const receipt = JSON.parse(text);
      if (receipt.project_id !== attempt.project || receipt.session_id !== attempt.session || receipt.instance_id !== attempt.instance || receipt.deleted !== true) throw new Error('Invalid receipt');
      document.dispatchEvent(new CustomEvent('snow:session-deleted', {detail: {project: attempt.project, session: attempt.session, instance: attempt.instance}}));
      if (!this.mounted) return;
      if (this.owner !== attempt.owner) {
        // A correlated receipt may retire only its original inventory metadata.
        // It must never close a new owner's dialog or navigate its workspace.
        if (this.snapshot.groups.get(attempt.project)?.instance === attempt.instance) this.deleted(attempt.project, attempt.session);
        return;
      }
      this.deleted(attempt.project, attempt.session);
      this.publish({deletion: {...attempt, pending: false}}); this.closeDelete();
      const data = this.snapshot.bootstrap;
      if (!this.snapshot.live && data?.project === attempt.project && data.session === attempt.session) {
        const source = [...document.querySelectorAll<HTMLElement>('[data-sidebar-project]')]
          .find(row => row.dataset.sidebarProject === attempt.project)?.querySelector<HTMLElement>('[data-shell-project-new]');
        if (source?.isConnected) await this.navigate('/?' + new URLSearchParams({view: 'projects', project: attempt.project, new: '1'}), source);
      }
    } catch {
      if (this.mounted && this.owner === attempt.owner) {
        this.invalidate(attempt.project);
        this.publish({deletion: {...attempt, pending: false, error: failure}});
      }
    } finally { clearTimeout(timer); }
  }
  collapse = () => { if (this.snapshot.narrow) return; const collapsed = !this.snapshot.collapsed; this.publish({collapsed}); this.command({type: 'collapse', collapsed}); };
  navigation = (open: boolean) => { open = open && this.snapshot.narrow; this.publish({navOpen: open}); this.command({type: 'navigation', open}); };
  setTheme = (theme: 'light' | 'dark') => { this.publish({theme}); };
  theme(theme: 'light' | 'dark') { this.setTheme(theme); this.command({type: 'theme', theme}); }
  openWorkspacePicker = (trigger: HTMLElement) => {
    if (this.snapshot.menu?.trigger === trigger) { this.closeMenu(); return; }
    this.publish({menu: {kind: 'workspace', project: '', session: '', instance: '', trigger, owner: this.owner}});
  };
  openSettings(section: 'general' | 'workspaces' | 'access', trigger: HTMLElement) { this.closeMenu(false); this.publish({settings: section, settingsReturn: trigger}); }
  closeSettings = () => { const trigger = this.snapshot.settingsReturn; this.publish({settings: null}); queueMicrotask(() => { if (trigger?.isConnected) trigger.focus({preventScroll: true}); }); };
}
export const shell = new ShellController();
export const sidebarSessions = Object.freeze({initialize: shell.initialize, syncVisibility: shell.syncVisibility, syncCurrent: shell.syncCurrent, invalidate: shell.invalidate, deleted: shell.deleted, preemptInventory: shell.preemptInventory});
// Legacy callers can submit a public row projection, but never write React DOM.
export const sessionActions = Object.freeze({
  renderRow(row: HTMLElement, value: {project: string; session: string; name: string; instance: string; active: boolean; available: boolean; supported: boolean}) {
    const group = shell.snapshot.groups.get(value.project);
    // An unversioned compatibility callback cannot restore authority after a
    // fresh inventory withdrew it. Only validated load() may grant capability.
    if (!row.isConnected || row.dataset.shellSession !== value.session || !group || group.instance !== value.instance) return;
    shell.group(value.project, {available: group.available && value.available, deleteSupported: group.deleteSupported && value.supported,
      activeSession: value.active ? value.session : group.activeSession});
  },
  appendMenu() { /* JSX owns menu contents. Retained only for API compatibility. */ },
});
