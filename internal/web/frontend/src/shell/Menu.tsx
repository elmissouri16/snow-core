import {useLayoutEffect, useRef, useState} from 'react';
import {createPortal} from 'react-dom';
import {canDelete, projectURL} from './model';
import {SHELL_MENU_ID} from './menu-aria';
import type {ShellController, ShellMenu} from './controller';
export interface MenusHost {
  open(options: {trigger: HTMLElement; panel: HTMLElement; placement?: string; managedTrigger: true; onClose(): void}): void;
  close(options?: {restoreFocus?: boolean}): void;
}
export function menusHost() { return (window as unknown as {SnowMenus?: MenusHost}).SnowMenus; }
export function ShellPopup({controller: c, menu}: {controller: ShellController; menu: ShellMenu}) {
  // Only the empty external portal host is imperative. JSX owns every child.
  const focused = useRef<HTMLElement | null>(null);
  const [panel] = useState(() => { const host = document.createElement('div'); host.id = SHELL_MENU_ID; return host; });
  useLayoutEffect(() => {
    panel.classList.toggle('shell-workspace-menu', menu.kind === 'workspace');
    panel.setAttribute('aria-label', menu.kind === 'session' ? 'Session actions' : menu.kind === 'project' ? 'Workspace actions' : menu.kind === 'workspace' ? 'Choose workspace' : 'Workspace view options');
    const host = menusHost();
    if (!host || !menu.trigger.isConnected || c.owner !== menu.owner) { c.closeMenu(false); return; }
    host.open({trigger: menu.trigger, panel, managedTrigger: true, placement: menu.kind === 'workspace' ? 'top-start' : 'bottom-start', onClose: () => { if (c.snapshot.menu === menu) c.closeMenu(false); }});
    return () => { if (panel.isConnected) host.close({restoreFocus: false}); };
  }, [c, menu, panel]);
  useLayoutEffect(() => {
    const prior = focused.current;
    if (!prior || !panel.isConnected) return;
    if ((!prior.isConnected || prior instanceof HTMLButtonElement && prior.disabled) && (document.activeElement === document.body || document.activeElement === prior)) {
      (panel.querySelector<HTMLElement>('[role=menuitem]:not(:disabled)') || panel).focus({preventScroll: true});
    }
  });
  const live = c.snapshot.live;
  const current = menu.project === live?.project && menu.session === live.session;
  const authority = c.authority(menu.project, menu.session);
  const valid = () => c.owner === menu.owner && menu.trigger.isConnected && c.snapshot.menu === menu;
  const project = c.snapshot.bootstrap?.projects.find(project => project.id === menu.project);
  return createPortal(<div className="snow-menu-content" tabIndex={-1} onFocusCapture={event => { focused.current = event.target as HTMLElement; }}>
    {menu.kind === 'session' && <>
      {current && live?.renameAvailable && <button type="button" className="snow-menu-row" role="menuitem" disabled={live.renameDisabled} onClick={() => {
        if (!valid() || c.snapshot.live?.instance !== menu.instance || c.snapshot.live.session !== menu.session || c.snapshot.live.project !== menu.project) return;
        c.closeMenu(false);
        (window as unknown as {SnowConversation?: {rename(trigger: HTMLElement): void}}).SnowConversation?.rename(menu.trigger);
      }}>Rename</button>}
      {authority?.supported && <button type="button" className="snow-menu-row danger" role="menuitem" data-session-delete="" disabled={!canDelete(authority)}
        title={authority.active ? 'Switch to another session or close this workspace before deleting the active session' : undefined}
        onClick={() => { if (valid()) c.openDelete(menu); }}>Delete session</button>}
    </>}
    {menu.kind === 'project' && project && <>
      <div className="shell-workspace-menu-heading"><strong>{project.name}</strong><small>{project.path}</small></div>
      {[false, true].map(remove => {
        const href = projectURL(project.id, {inspect: 'project', ...(c.snapshot.bootstrap?.project === project.id && c.snapshot.bootstrap.session ? {session: c.snapshot.bootstrap.session} : {})}) + (remove ? '#remove-project' : '');
        return <a key={String(remove)} className="snow-menu-row" role="menuitem" href={href} data-snow-navigation="" onClick={event => {
          if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0) return;
          event.preventDefault(); event.stopPropagation(); if (!valid()) return;
          const localInspector = document.getElementById('project-inspector')?.dataset.project === project.id;
          c.closeMenu(false);
          if (localInspector) {
            document.dispatchEvent(new CustomEvent('snow:inspect-project', {detail: {project: project.id, remove, trigger: menu.trigger}}));
          } else {
            c.navigation(false); void c.navigate(href, event.currentTarget);
          }
        }}>{remove ? 'Remove registration…' : 'Workspace settings…'}</a>;
      })}
    </>}
    {menu.kind === 'workspace' && <>
      <span className="picker-heading">Workspaces</span>
      {c.snapshot.bootstrap?.projects.map(project => <a key={project.id} className="snow-menu-row" role="menuitem" data-home-project={project.id} data-home-project-name={project.name} data-home-project-available={String(project.available)} href={projectURL(project.id)} data-snow-navigation="" onClick={event => {
        if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0) return;
        event.preventDefault(); if (!valid()) return; c.closeMenu(false); void c.navigate(projectURL(project.id), event.currentTarget);
      }}><span title={`${project.name} · ${project.path}`}><strong>{project.name}</strong><small>{project.path}</small>{!project.available && <small>Folder unavailable</small>}</span></a>)}
      {!c.snapshot.bootstrap?.projects.length && <p className="fine">No workspaces registered yet.</p>}
      <a className="snow-menu-row picker-add" role="menuitem" href="/?view=projects#add-project" data-snow-navigation="" onClick={event => {
        if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey || event.button !== 0) return;
        event.preventDefault(); if (!valid()) return; c.closeMenu(false); void c.navigate('/?view=projects#add-project', event.currentTarget);
      }}>Add workspace</a>
    </>}
    {menu.kind === 'view' && <>
      <button type="button" className="snow-menu-row" role="menuitemcheckbox" aria-checked={!c.snapshot.hideSessions} onClick={() => { if (valid()) c.syncVisibility(!c.snapshot.hideSessions); }}>Show saved sessions</button>
      <button type="button" className="snow-menu-row" role="menuitemcheckbox" aria-checked={c.snapshot.pinnedOnly} onClick={() => { if (valid()) c.publish({pinnedOnly: !c.snapshot.pinnedOnly}); }}>Pinned workspaces only</button>
    </>}
  </div>, panel);
}
