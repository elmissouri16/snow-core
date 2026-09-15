import {useLayoutEffect, useRef} from 'react';
import {HostSettingsPanel} from '../host-settings/HostSettingsPanel';
import {BrowserInventory} from '../browser-access/BrowserInventory';
import type {ShellController, ViewState} from './controller';
import type {ShellBootstrap} from './model';
import {projectURL} from './model';
import {NavigationLink} from './Sidebar';
import {Icon} from './Icons';
const CSRF = ({value}: {value: string}) => <input type="hidden" name="csrf" value={value} />;
export function BrowserAccess({data}: {data: ShellBootstrap}) {
  return <div className="settings-content">
    <BrowserInventory csrf={data.csrf} />
    <section className="settings-panel"><h2>Pair another browser</h2><p>Rotate the reusable pairing code, then open this host's Snow URL in the other browser. The replacement code lasts up to 30 days and survives Snow restarts. Rotation invalidates the previous code, not browsers already paired.</p>
      <form method="post" action="/access/pair"><CSRF value={data.csrf} /><button className="primary" type="submit">Rotate pairing code</button></form>
      {data.pairingCode && <div className="pairing-result" role="status"><label htmlFor="pairing-code">Reusable · expires in 30 days</label><input id="pairing-code" type="text" value={data.pairingCode} readOnly autoComplete="off" spellCheck={false} /><p className="fine">Treat this code as a credential. It is never placed in a URL or saved in browser storage.</p></div>}
    </section>
    <section className="settings-panel"><h2>Access boundaries</h2><dl>
      <div><dt>Network</dt><dd>{data.tls ? 'Direct numeric-loopback HTTPS only. LAN and proxy access are not enabled.' : 'Direct loopback HTTP only. LAN, TLS, and proxy access are not enabled.'}</dd></div>
      <div><dt>Browser lifetime</dt><dd>Paired browsers stay connected for up to 30 days. Up to 8 browsers may be paired.</dd></div>
      <div><dt>Restart behavior</dt><dd>Pairing survives Snow restarts. Signing out revokes this browser's access.</dd></div>
      <div><dt>Host authority</dt><dd>Snow has no process sandbox. Agent workers run with the host user's privileges.</dd></div>
    </dl></section>
    <section className="settings-panel"><h2>Revoke all browsers</h2><p>Sign out every paired browser, including this one, and rotate the pairing code. Restart Snow to see the replacement code in the terminal.</p>
      <form method="post" action="/access/revoke-all"><CSRF value={data.csrf} /><label className="checkbox-label"><input type="checkbox" name="confirm" value="revoke" required /> I understand all browsers will need to pair again.</label><button className="button danger" type="submit">Revoke all browser access</button></form>
    </section>
    <section className="settings-panel"><h2>This browser</h2><p>Sign out to revoke this browser’s access. Other paired browsers stay connected.</p><form method="post" action="/logout"><CSRF value={data.csrf} /><button className="button" type="submit">Sign out</button></form></section>
  </div>;
}
export function Settings({controller: c, view}: {controller: ShellController; view: ViewState}) {
  const dialog = useRef<HTMLDialogElement>(null), options = useRef<HTMLDivElement>(null), close = useRef<HTMLButtonElement>(null);
  const data = view.bootstrap;
  useLayoutEffect(() => {
    const node = dialog.current;
    if (!node) return;
    if (view.settings && !node.open) { node.returnValue = ''; node.showModal(); close.current?.focus(); }
    else if (!view.settings && node.open) node.close();
  }, [!!view.settings]);
  useLayoutEffect(() => { if (options.current) options.current.scrollTop = 0; }, [view.settings]);
  useLayoutEffect(() => () => { if (dialog.current?.open) dialog.current.close(); }, []);
  if (!data) return null;
  const navigate = (href: string, source?: HTMLElement) => { c.closeSettings(); c.navigation(false); return c.navigate(href, source); };
  return <dialog ref={dialog} id="settings-dialog" className="settings-dialog" aria-labelledby="settings-title"
    onCancel={event => { event.preventDefault(); c.closeSettings(); }}
    onClick={event => { if (event.target !== event.currentTarget) return; const rect = event.currentTarget.getBoundingClientRect(); if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) c.closeSettings(); }}>
    <div className="settings-layout">
      <nav className="settings-nav" aria-label="Settings sections"><h2 id="settings-title">Settings</h2><div className="settings-nav-list">
        {(['general', 'workspaces', 'access'] as const).map(section => <button key={section} type="button" data-settings-section={section} aria-controls={`settings-${section}`} aria-current={view.settings === section ? 'true' : undefined}
          onClick={event => { event.stopPropagation(); c.publish({settings: section}); }}><Icon name={section === 'general' ? 'settings' : section === 'workspaces' ? 'folder' : 'panel'} /><span>{section === 'access' ? 'Browser access' : section === 'general' ? 'General' : 'Workspaces'}</span></button>)}
      </div></nav>
      <div className="settings-column"><header className="settings-header"><button ref={close} type="button" className="settings-close" data-settings-close="" aria-label="Close Settings" autoFocus onClick={event => { event.stopPropagation(); c.closeSettings(); }}><Icon name="close" /></button></header>
        <div className="settings-options" ref={options}>
          <section id="settings-general" data-settings-panel="general" aria-labelledby="settings-general-title" hidden={view.settings !== 'general'}>
            <h3 id="settings-general-title" className="settings-section-title">General</h3>
            <div className="settings-group"><h4>Appearance</h4><div className="settings-appearance" role="group" aria-label="Appearance">
              {(['light', 'dark'] as const).map(theme => <button key={theme} type="button" data-theme-choice={theme} aria-pressed={view.theme === theme} onClick={event => { event.stopPropagation(); c.theme(theme); }}><Icon name={theme} className="appearance-icon" />{theme === 'light' ? 'Light' : 'Dark'}</button>)}
            </div><p className="fine">Saved in this browser. Does not change the host’s terminal theme.</p></div>
            <HostSettingsPanel csrf={data.csrf} enabled={data.hostSettingsEnabled} apiKeyEnabled={data.apiKeyEnabled && data.tls} projects={data.projects.map(({id, name}) => ({id, name}))} />
            <div className="settings-group"><h4>On your machine</h4><p>Snow runs tools with your host account’s privileges, not in a process sandbox. Opening Settings does not start a worker or contact a model provider.</p><p>Live browser conversations require explicit project activation and use permission prompts. Provider configuration, plugins and host permissions are managed on the host, not here.</p><p className="fine"><a href="/static/HARNESS-NOTICE.txt">Third-party notices</a></p><div className="settings-host"><span className="status-dot" />{data.tls ? 'Direct HTTPS connection' : 'Direct loopback connection'}<span className="version">Snow {data.version}</span></div></div>
          </section>
          <section id="settings-workspaces" data-settings-panel="workspaces" aria-labelledby="settings-workspaces-title" hidden={view.settings !== 'workspaces'}>
            <h3 id="settings-workspaces-title" className="settings-section-title">Workspaces</h3><p>Registered folders on this host. Choosing a workspace only opens its saved view; it does not activate an agent.</p>
            <nav className="settings-workspace-list" aria-label="Registered workspaces">
              {data.projects.map(project => <NavigationLink key={project.id} href={projectURL(project.id)} navigate={navigate}><Icon name="folder" /><span title={`${project.name} · ${project.path}`}><strong>{project.name}</strong><small>{project.path}</small>{!project.available && <small>Folder unavailable</small>}</span><Icon name="chevron" /></NavigationLink>)}
              {!data.projects.length && <p className="fine">No workspaces registered yet.</p>}
            </nav>
            <section className="settings-group project-trust-settings" aria-labelledby="project-trust-heading"><h4 id="project-trust-heading">Remembered project trust</h4><p>Trusted projects still need an explicit Start or Resume. Forgetting trust restores confirmation on the next activation; it does not stop a running worker or change tool permissions.</p>
              <ul className="project-trust-list">{data.projects.filter(project => project.trustRemembered).map(project => <li key={project.id}><span><strong>{project.name}</strong><small>{project.available ? 'Trust remembered' : 'Folder unavailable · confirmation required'}</small></span><form method="post" action={`/projects/${encodeURIComponent(project.id)}/trust/revoke`} data-project-trust-revoke=""><CSRF value={data.csrf} /><input type="hidden" name="confirm" value="revoke" /><button type="submit" className="button quiet" aria-label={`Forget trust for ${project.name}`}>Forget trust</button></form></li>)}</ul>
              <p className="fine project-trust-empty" hidden={data.projects.some(project => project.trustRemembered)}>No remembered project trust. Projects ask for confirmation before their first activation.</p>
            </section>
            <section className="settings-group" aria-labelledby="workspace-skills-title"><h4 id="workspace-skills-title">Installed skills</h4><p>Remembered per project for future starts, across this manager’s paired browsers. Changes do not affect a running worker or grant project trust or tool permissions.</p>
              <ul className="project-trust-list">{data.projects.map(project => <li key={project.id}><span><strong>{project.name}</strong><small>{project.skillsEnabled ? 'Enabled for future starts' : 'Disabled for future starts'}{!project.available && ' · Folder unavailable'}</small></span><form method="post" action={`/projects/${encodeURIComponent(project.id)}/skills`}><CSRF value={data.csrf} /><input type="hidden" name="enable_skills" value={project.skillsEnabled ? '' : 'runtime'} /><button type="submit" className="button quiet" aria-label={`${project.skillsEnabled ? 'Disable' : 'Enable'} installed skills for ${project.name}`} disabled={!project.available}>{project.skillsEnabled ? 'Disable skills' : 'Enable skills'}</button></form></li>)}</ul>
              {!data.projects.length && <p className="fine">Add a workspace to choose its startup preference.</p>}
            </section>
            <div className="settings-workspace-actions"><NavigationLink className="button" href="/?view=projects#add-project" navigate={navigate}><Icon name="plus" />Add workspace</NavigationLink><NavigationLink className="button quiet" href="/?view=projects" navigate={navigate}>Manage workspaces</NavigationLink></div><p className="fine">Add workspace uses the existing host folder picker. Folder paths refer to the Snow host, not this browser’s device.</p>
          </section>
          <section id="settings-access" data-settings-panel="access" aria-labelledby="settings-access-title" hidden={view.settings !== 'access'}>
            <h3 id="settings-access-title" className="settings-section-title">Browser access</h3>
            {data.view === 'access' ? <><p>Browser access controls are open in the workspace behind Settings.</p><button className="button" type="button" data-settings-close="" data-settings-access-return="" onClick={event => { event.stopPropagation(); c.publish({settingsReturn: document.getElementById('workspace-content')}); c.closeSettings(); }}>Show browser access controls</button></> : <BrowserAccess data={data} />}
          </section>
        </div>
      </div>
    </div>
  </dialog>;
}
