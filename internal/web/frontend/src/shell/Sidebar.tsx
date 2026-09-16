import {useLayoutEffect, useRef} from 'react';
import type {MouseEvent, ReactNode} from 'react';
import {Icon} from './Icons';
import type {Group, ShellController, ViewState} from './controller';
import type {Navigate, SessionRow, ShellProject} from './model';
import {projectURL} from './model';
import {menuLauncherARIA} from './menu-aria';
function ordinary(event: MouseEvent) { return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey; }
export function NavigationLink({href, navigate, children, ...props}: {href: string; navigate: Navigate; children: ReactNode} & Omit<React.AnchorHTMLAttributes<HTMLAnchorElement>, 'href' | 'onClick'>) {
  return <a {...props} href={href} data-snow-navigation="" onClick={event => { if (!ordinary(event)) return; event.preventDefault(); event.stopPropagation(); void navigate(href, event.currentTarget); }}>{children}</a>;
}
function Session({c, group, row}: {c: ShellController; group: Group; row: SessionRow}) {
  const link = useRef<HTMLAnchorElement>(null), trigger = useRef<HTMLButtonElement>(null);
  const triggerFocused = useRef(false);
  const live = c.snapshot.live, current = live?.project === group.project && live.session === row.session_id;
  const active = c.authority(group.project, row.session_id)?.active || false;
  const name = current ? live!.title || 'New conversation' : row.name || 'Untitled session';
  const rename = current && live!.renameAvailable;
  const hasActions = group.deleteSupported || rename;
  const menu = c.snapshot.menu;
  const ownMenu = menu?.kind === 'session' && menu.project === group.project && menu.session === row.session_id;
  // Keep the hidden launcher node stable long enough to identify old focus. Do
  // not close another workspace's popup when this capability is withdrawn.
  useLayoutEffect(() => {
    if (!hasActions) {
      // Applying hidden can move native focus to body before layout effects.
      // Remember only this launcher's focus, never reclaim another control's.
      const focused = document.activeElement === trigger.current || ownMenu || triggerFocused.current && document.activeElement === document.body;
      triggerFocused.current = false;
      if (ownMenu) c.closeMenu(false);
      if (focused) link.current?.focus({preventScroll: true});
    }
  }, [c, hasActions, ownMenu]);
  const selected = current || !live && c.snapshot.bootstrap?.project === group.project && c.snapshot.bootstrap.session === row.session_id;
  const instance = live?.project === group.project ? live.instance : group.instance;
  return <div className="shell-session-row" data-shell-session={row.session_id} data-shell-live-session={current ? '' : undefined}>
    <a ref={link} href={projectURL(group.project, {session: row.session_id})} data-snow-navigation="" data-shell-session-open="" data-project={group.project} data-instance={instance} aria-current={selected ? 'page' : undefined}
      onClick={event => {
        if (!ordinary(event)) return;
        event.preventDefault(); event.stopPropagation(); c.navigation(false);
        if (instance) document.dispatchEvent(new CustomEvent('snow:session-select', {detail: {project: group.project, session: row.session_id, instance, trigger: event.currentTarget}}));
        else void c.navigate(event.currentTarget.href, event.currentTarget);
      }}><Icon name="chat" /><span title={name}>{name}</span></a>
    <button ref={trigger} type="button" onFocus={() => { triggerFocused.current = true; }} onBlur={event => { if (!event.currentTarget.hidden) triggerFocused.current = false; }} className="quiet shell-session-more" data-shell-session-menu="" hidden={!hasActions} disabled={!hasActions || !group.deleteSupported && !!rename && !!live?.renameDisabled}
      data-project={group.project} data-session={row.session_id} data-instance={group.instance} data-session-name={name}
      data-delete-supported={String(group.deleteSupported)} data-delete-available={String(group.loaded && group.available)} data-delete-active={String(active)}
      aria-label={`Session actions for ${name}`} title="Session actions" aria-haspopup="menu" {...menuLauncherARIA(menu, 'session', group.project, row.session_id, !!hasActions)}
      onClick={event => { event.stopPropagation(); if (hasActions) c.publish({menu: {kind: 'session', project: group.project, session: row.session_id, instance: current ? live!.instance : group.instance, trigger: event.currentTarget, owner: c.owner}}); }}>⋯</button>
  </div>;
}
function Branch({c, project, group, hidden}: {c: ShellController; project: ShellProject; group: Group; hidden: boolean}) {
  const selected = c.snapshot.bootstrap?.project === project.id;
  const more = useRef<HTMLButtonElement>(null), toggle = useRef<HTMLButtonElement>(null);
  const retry = !!group.error || group.loaded && !group.available;
  const loadMore = !group.loading && !retry && group.hasMore && group.rows.length < 100;
  const priorMore = useRef(false);
  const showButton = retry || loadMore || group.loading && priorMore.current;
  useLayoutEffect(() => {
    if (priorMore.current && !showButton && document.activeElement === more.current) toggle.current?.focus({preventScroll: true});
    priorMore.current = showButton;
  }, [showButton]);
  const status = group.loading ? 'Loading sessions…' : group.error || (group.loaded && !group.available ? 'Saved sessions are unavailable.' : group.loaded && !group.rows.length ? 'No saved sessions' : (group.truncated || group.hasMore) && !loadMore ? 'Showing a limited session list' : '');
  return <div className="project-group" data-sidebar-project={project.id} hidden={hidden}>
    <div className="shell-project-row">
      <button ref={toggle} type="button" className="quiet shell-project-action workspace-disclosure" data-workspace-toggle="" aria-label={`Sessions in ${project.name}`} aria-expanded={group.expanded} aria-controls={`workspace-sessions-${project.id}`} onClick={event => { event.stopPropagation(); c.toggle(project.id); }}><Icon name="chevron" /></button>
      <NavigationLink className={`project-link${selected ? ' selected' : ''}`} href={projectURL(project.id)} navigate={(href, source) => { c.navigation(false); return c.navigate(href, source); }} title={project.path} aria-current={selected ? 'page' : undefined}>
        <span className="folder-icon" aria-hidden="true"><Icon name="folder" /></span><span className="project-link-text"><strong>{project.name}</strong><span className="project-link-path">{project.path}</span>{!project.available && <span className="unavailable">Folder unavailable</span>}</span>
      </NavigationLink>
      <span className="shell-project-actions"><a className="quiet shell-project-action" data-shell-project-new="" href={projectURL(project.id, {new: '1'})} data-snow-navigation="" aria-label={`New session in ${project.name}`} title={`New session in ${project.name}`}
        aria-disabled={c.snapshot.live?.project === project.id && c.snapshot.live.newDisabled ? 'true' : undefined}
        onClick={event => { if (!ordinary(event)) return; event.preventDefault(); event.stopPropagation(); if (c.snapshot.live?.project === project.id && c.snapshot.live.newDisabled) return; c.preemptInventory(); c.navigation(false); document.dispatchEvent(new CustomEvent('snow:session-new', {detail: {project: project.id, trigger: event.currentTarget}})); }}><Icon name="new" /></a>
        <button type="button" className="quiet shell-project-action" data-shell-project-menu="" aria-label={`Workspace actions for ${project.name}`} aria-haspopup="menu" {...menuLauncherARIA(c.snapshot.menu, 'project', project.id)} title="Workspace actions"
          onClick={event => { event.stopPropagation(); c.publish({menu: {kind: 'project', project: project.id, session: '', instance: '', trigger: event.currentTarget, owner: c.owner}}); }}>⋯</button>
      </span>
    </div>
    <div className="session-tree" id={`workspace-sessions-${project.id}`} data-workspace-sessions="" hidden={!group.expanded || c.snapshot.hideSessions} aria-busy={group.loading || undefined}>
      {group.rows.map(row => <Session key={row.session_id} c={c} group={group} row={row} />)}
      {status && <p data-sidebar-session-status="" role="status">{status}</p>}
      <button ref={more} type="button" className="quiet" data-sidebar-session-more="" data-offset={loadMore ? group.nextOffset : 0} hidden={!showButton} aria-disabled={group.loading || undefined}
        onClick={event => { event.stopPropagation(); if (!group.loading) void c.load(project.id, loadMore ? group.nextOffset : 0); }}>{loadMore ? 'Load more' : 'Retry'}</button>
    </div>
  </div>;
}
export function Sidebar({controller: c, view}: {controller: ShellController; view: ViewState}) {
  const input = useRef<HTMLInputElement>(null), data = view.bootstrap;
  const wasSearchOpen = useRef(false);
  useLayoutEffect(() => { c.restoreSidebar(); }, [c]);
  useLayoutEffect(() => {
    if (view.searchOpen && !wasSearchOpen.current) input.current?.focus();
    wasSearchOpen.current = view.searchOpen;
  }, [view.searchOpen]);
  if (!data) return null;
  const query = view.query.trim().toLocaleLowerCase();
  const matches = (project: ShellProject) => (!query || project.name.toLocaleLowerCase().includes(query) || project.path.toLocaleLowerCase().includes(query)) && (!view.pinnedOnly || project.pinned);
  const navigate: Navigate = (href, source) => { c.navigation(false); return c.navigate(href, source); };
  return <>
    <aside id="project-navigation" className={`sidebar${view.navOpen ? ' nav-open' : ''}`} aria-label="Projects and workspace" inert={view.narrow && !view.navOpen} role={view.navOpen ? 'dialog' : undefined} aria-modal={view.navOpen ? true : undefined}>
      <div className="sidebar-brand-row"><NavigationLink href="/" navigate={navigate} className="brand" aria-label="Snow workspace home"><span className="brand-mark" aria-hidden="true"><Icon name="snow" /></span><span className="brand-name">snow</span><span className="brand-label">WORKSPACE</span></NavigationLink>
        <button className="quiet sidebar-collapse" type="button" data-sidebar-collapse="" aria-label={view.collapsed ? 'Expand sidebar' : 'Collapse sidebar'} aria-controls="project-navigation" aria-expanded={!view.collapsed} onClick={event => { event.stopPropagation(); c.collapse(); }}><Icon name="panel" /></button>
        <button className="quiet mobile-nav-close" type="button" data-nav-close="" aria-label="Close project navigation" onClick={event => { event.stopPropagation(); c.navigation(false); }}><Icon name="close" /></button>
      </div>
      <a className="sidebar-new-session" href={data.project ? projectURL(data.project, {new: '1'}) : '/'} data-snow-navigation="" data-shell-new-session="" aria-label="New session" aria-disabled={view.live?.newDisabled || undefined}
        onClick={event => { if (!ordinary(event)) return; event.preventDefault(); event.stopPropagation(); if (view.live?.newDisabled) return; c.preemptInventory(); c.navigation(false); if (data.project) document.dispatchEvent(new CustomEvent('snow:session-new', {detail: {project: data.project, trigger: event.currentTarget}})); else void navigate('/', event.currentTarget); }}><Icon name="new" /><span>New session</span></a>
      <div className="sidebar-heading"><span className="sidebar-title">Workspaces</span><div className="sidebar-tools">
        <button className="quiet" type="button" data-sidebar-search-toggle="" aria-label="Search workspaces" aria-controls="sidebar-search" aria-expanded={view.searchOpen} onClick={event => { event.stopPropagation(); if (view.collapsed) c.collapse(); c.publish({searchOpen: !view.searchOpen, query: view.searchOpen ? '' : view.query}); }}><Icon name="search" /></button>
        <button className="quiet" type="button" data-sidebar-view-toggle="" aria-label="Workspace view options" aria-haspopup="menu" {...menuLauncherARIA(view.menu, 'view')} onClick={event => { event.stopPropagation(); c.publish({menu: {kind: 'view', project: '', session: '', instance: '', trigger: event.currentTarget, owner: c.owner}}); }}><Icon name="list" /></button>
        <NavigationLink className="quiet add-project-link" href="/?view=projects#add-project" navigate={navigate} aria-label="Add workspace"><Icon name="plus" /></NavigationLink>
      </div></div>
      <div id="sidebar-search" className="sidebar-search" hidden={!view.searchOpen}><label className="visually-hidden" htmlFor="workspace-search">Search registered workspaces</label><input ref={input} id="workspace-search" type="search" placeholder="Search workspaces…" autoComplete="off" maxLength={200} data-sidebar-search="" value={view.query} onChange={event => c.publish({query: event.currentTarget.value})} /></div>
      <nav className="project-tree" aria-label="Project navigation">
        {data.projects.map(project => <Branch key={project.id} c={c} project={project} group={view.groups.get(project.id)!} hidden={!matches(project)} />)}
        {!data.projects.length && <div className="sidebar-empty"><p>No workspaces yet</p><span className="fine">Add a folder to get started.</span></div>}
        <p className="sidebar-search-empty fine" data-sidebar-search-empty="" hidden={(!query && !view.pinnedOnly) || data.projects.some(matches)}>No matching workspaces.</p>
      </nav>
      <div className="sidebar-bottom">
        <NavigationLink className="sidebar-utility" href="/?view=activity" navigate={navigate} aria-label="Activity & attention" title="Activity & attention" aria-current={data.view === 'activity' ? 'page' : undefined}><Icon name="activity" /><span>Activity &amp; attention</span></NavigationLink>
        <NavigationLink className="sidebar-utility" href="/?view=organization" navigate={navigate} aria-label="Organize workspaces" title="Organize workspaces" aria-current={data.view === 'organization' ? 'page' : undefined}><Icon name="folder" /><span>Organize workspaces</span></NavigationLink>
        <div className="sidebar-settings"><button className="sidebar-settings-trigger" type="button" data-settings-open="" aria-label="Settings" aria-haspopup="dialog" aria-controls="settings-dialog" onClick={event => { event.stopPropagation(); c.openSettings('general', event.currentTarget); }}><Icon name="settings" /><span>Settings</span></button></div>
      </div>
    </aside>
    <button className="nav-backdrop" type="button" data-nav-close="" aria-label="Close project navigation" tabIndex={-1} hidden={!view.navOpen} onClick={event => { event.stopPropagation(); c.navigation(false); }} />
  </>;
}
