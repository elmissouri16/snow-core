import { useState } from 'react';
import { archivedPageURL, catalogPageURL } from './model';
import type { OrganizationProps, Project, Session } from './model';

function CSRF({ value }: { value: string }) {
  return <input type="hidden" name="csrf" value={value} />;
}

function Badge({ children }: { children: string }) {
  return <span className="organization-badge">{children}</span>;
}

function ActiveProject({ project, csrf }: { project: Project; csrf: string }) {
  const base = `/projects/${project.id}/organization`;
  const nameID = `organization-name-${project.id}`;
  return <li className="organization-item" id={`organization-active-${project.id}`}>
    <div className="organization-item-heading">
      <a href={`/?view=organization&project=${project.id}`}><strong>{project.name}</strong></a>
      {project.pinned && <Badge>Pinned</Badge>}
      {!project.available && <Badge>{project.state}</Badge>}
    </div>
    <p className="fine mono organization-path">{project.path}</p>
    <form method="post" action={`${base}/rename`} className="organization-rename">
      <CSRF value={csrf} />
      <label htmlFor={nameID}>Workspace label</label>
      <input id={nameID} name="name" defaultValue={project.name} maxLength={128} required />
      <button className="button" type="submit">Save label</button>
    </form>
    <div className="organization-actions">
      <form method="post" action={`${base}/${project.pinned ? 'unpin' : 'pin'}`}>
        <CSRF value={csrf} />
        <button className="quiet" type="submit">{project.pinned ? 'Unpin workspace' : 'Pin workspace'}</button>
      </form>
      <details className="organization-confirm">
        <summary>Archive workspace…</summary>
        <p className="fine">Hide this registration from active workspaces, retaining all files and sessions. Restore it explicitly below; the original folder identity must still match.</p>
        <form method="post" action={`${base}/archive`}>
          <CSRF value={csrf} /><input type="hidden" name="confirm" value="archive" />
          <button className="button" type="submit">Confirm archive of {project.name}</button>
        </form>
      </details>
    </div>
  </li>;
}

function ArchivedProject({ project, csrf }: { project: Project; csrf: string }) {
  return <li className="organization-item" id={`organization-archived-${project.id}`}>
    <div className="organization-item-heading">
      <strong>{project.name}</strong><Badge>Archived</Badge>{project.pinned && <Badge>Pinned</Badge>}
    </div>
    <p className="fine mono organization-path">{project.path}</p>
    {!project.available && <p className="fine">{project.issue} Restore requires the original folder.</p>}
    <form method="post" action={`/projects/${project.id}/organization/restore`}>
      <CSRF value={csrf} /><input type="hidden" name="confirm" value="restore" />
      <button className="button" type="submit" disabled={!project.available}>Restore {project.name}</button>
    </form>
  </li>;
}

function SessionFields({ csrf, session, offset }: { csrf: string; session: Session; offset: number }) {
  return <><CSRF value={csrf} />
    <input type="hidden" name="session_id" value={session.id} />
    <input type="hidden" name="offset" value={offset} />
  </>;
}

function SessionRow({ session, project, csrf, offset, hidden }: {
  session: Session; project: Project; csrf: string; offset: number; hidden: boolean;
}) {
  const base = `/projects/${project.id}/sessions/organization`;
  const fields = { csrf, session, offset };
  return <li className="organization-item" id={`organization-session-${project.id}-${session.id}`}
    data-organization-session="" data-archived={String(session.archived)} hidden={hidden}>
    <div className="organization-item-heading">
      <a href={`/?view=projects&project=${project.id}&session=${session.id}`} data-organization-title="">
        <strong>{session.name || 'Untitled session'}</strong>
      </a>
      {session.pinned && <Badge>Pinned</Badge>}{session.archived && <Badge>Archived</Badge>}
    </div>
    <p className="fine">{session.updated}</p>
    <div className="organization-actions">
      <form method="post" action={`${base}/${session.pinned ? 'unpin' : 'pin'}`}>
        <SessionFields {...fields} />
        <button className="quiet" type="submit">{session.pinned ? 'Unpin conversation' : 'Pin conversation'}</button>
      </form>
      {session.archived ? <form method="post" action={`${base}/restore`}>
        <SessionFields {...fields} /><input type="hidden" name="confirm" value="restore" />
        <button className="button" type="submit">Restore conversation</button>
      </form> : <form method="post" action={`${base}/archive`}>
        <SessionFields {...fields} /><input type="hidden" name="confirm" value="archive" />
        <button className="button" type="submit" title="Archive in the manager only. Saved history is kept; Restore reverses this.">Archive conversation</button>
      </form>}
    </div>
  </li>;
}

function SessionCatalog({ project, sessions, offset, nextURL, csrf }: {
  project: Project; sessions: Session[]; offset: number; nextURL: string; csrf: string;
}) {
  const [query, setQuery] = useState('');
  const [showArchived, setShowArchived] = useState(true);
  const normalized = query.trim().toLocaleLowerCase();
  const matches = (session: Session) => (showArchived || !session.archived) &&
    (session.name || 'Untitled session').toLocaleLowerCase().includes(normalized);
  const visible = sessions.filter(matches).length;
  const next = catalogPageURL(nextURL, project.id, offset);
  return <>
    <p className="fine">This is a bounded, read-only catalog page, not a complete inventory. Live, locked or unsupported sessions may be absent. Pins and archives are manager-only metadata; original conversation IDs and transcripts stay unchanged.</p>
    <div className="organization-filters">
      <label htmlFor="organization-title-filter">Search titles on this loaded page only
        <input id="organization-title-filter" type="search" maxLength={128} autoComplete="off"
          data-organization-filter="" aria-describedby="organization-search-help"
          value={query} onChange={event => setQuery(event.currentTarget.value)} />
      </label>
      <label className="organization-checkbox">
        <input type="checkbox" checked={showArchived} data-organization-archived=""
          onChange={event => setShowArchived(event.currentTarget.checked)} />{' '}
        Show archived conversations on this loaded page
      </label>
    </div>
    <p className="fine" id="organization-search-help">Loaded-page search does not search other pages, transcript contents or every saved session.</p>
    <p className="fine" role="status" aria-live="polite" data-organization-status="">
      {visible} of {sessions.length} conversations shown on this loaded page. Other pages are not searched.
    </p>
    <ul className="organization-list">
      {sessions.map(session => <SessionRow key={session.id} session={session} project={project}
        csrf={csrf} offset={offset} hidden={!matches(session)} />)}
      {sessions.length === 0 && <li className="fine">No supported saved conversations on this page.</li>}
    </ul>
    {next && <a className="button" href={next}>Next catalog page →</a>}
  </>;
}

/** The mount validates server bootstrap before handing ownership to React. */
export function OrganizationPage({ csrf, error, organization }: OrganizationProps) {
  const archivedNext = organization && archivedPageURL(organization.archivedNextURL);
  return <section className="organization-panel" aria-labelledby="organization-heading">
    <header className="workspace-heading">
      <div><h1 id="organization-heading">Organize workspaces</h1>
        <p className="fine">Manager labels, pins and archives only. Nothing here starts an agent or deletes project files or saved conversations.</p>
      </div>
      <a className="button" href="/?view=projects">Back to workspaces</a>
    </header>
    {error && <p className="error" role="alert">{error}</p>}
    {organization && <>
      <div className="organization-grid">
        <section className="organization-card" aria-labelledby="organization-active-heading">
          <h2 id="organization-active-heading">Active registrations</h2>
          <p className="fine">Up to 100 registrations. Close a live conversation before archiving its workspace. Unpinning does not hide work.</p>
          <ul className="organization-list">
            {organization.projects.map(project => <ActiveProject key={project.id} project={project} csrf={csrf} />)}
            {organization.projects.length === 0 && <li className="fine">No active registrations.</li>}
          </ul>
        </section>
        <section className="organization-card" aria-labelledby="organization-archive-heading">
          <h2 id="organization-archive-heading">Archived registrations</h2>
          <p className="fine">Includes registrations previously removed from the manager. At most 25 entries per page; bounded navigation ends at offset 10,000. Restore preserves the exact registration ID and never activates a conversation.</p>
          <ul className="organization-list">
            {organization.archived.map(project => <ArchivedProject key={project.id} project={project} csrf={csrf} />)}
            {organization.archived.length === 0 && <li className="fine">No archived registrations on this page.</li>}
          </ul>
          {archivedNext && <a className="button" href={archivedNext}>Next archived registrations →</a>}
        </section>
      </div>
      {organization.project ? <section className="organization-card organization-sessions"
        aria-labelledby="organization-sessions-heading" data-organization-sessions="" data-organization-ready="true">
        <h2 id="organization-sessions-heading">Saved conversations · {organization.project.name}</h2>
        {organization.live ? <p className="notice">This workspace has a live conversation.{' '}
          <a href={`/?view=projects&project=${organization.project.id}`}>Return to live work</a>{' '}
          and close it before organizing saved conversations. Live work is never hidden by an archive.
        </p> : <SessionCatalog key={`${organization.project.id}:${organization.offset}`}
          project={organization.project} sessions={organization.sessions} offset={organization.offset}
          nextURL={organization.nextURL} csrf={csrf} />}
      </section> : <p className="fine">Select an active workspace above to organize a loaded page of its saved conversations.</p>}
    </>}
  </section>;
}
