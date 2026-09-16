import { useSyncExternalStore } from 'react';
import { shell } from '../shell/controller';
import { menuLauncherARIA } from '../shell/menu-aria';
import type { HomeProps } from './model';
import { projectURL } from './model';
import { useWorkspace, useWorkspaceMounted, workspace } from './bridge';
import { Icon } from './Icons';
export function HomePage({ projects, error }: HomeProps) {
  useWorkspaceMounted();
  const { draft } = useWorkspace();
  const shellView = useSyncExternalStore(
    shell.subscribe,
    shell.getSnapshot,
    shell.getSnapshot,
  );
  return (
    <>
      <header className="workspace-heading home-heading">
        <button
          className="quiet sidebar-restore"
          type="button"
          data-sidebar-restore
          aria-label="Expand sidebar"
          aria-controls="project-navigation"
        >
          <Icon kind="panel" />
        </button>
      </header>
      <section className="home-landing" aria-labelledby="home-title">
        <div className="home-title">
          <span className="home-title-mark" aria-hidden="true">
            <Icon kind="snow" />
          </span>
          <h1 id="home-title">Into the Unknown</h1>
          <span className="preview-pill">Preview</span>
        </div>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <div className="home-controls">
          <details className="workspace-picker">
            <summary
              aria-haspopup="menu"
              {...menuLauncherARIA(shellView.menu, 'workspace')}
            >
              <Icon kind="folder" />
              <span data-home-workspace-label>
                {draft.name || 'Choose workspace'}
              </span>
              <Icon kind="chevron" />
            </summary>
            <div className="workspace-picker-menu">
              <span className="picker-heading">Workspaces</span>
              {projects.length ? (
                projects.map((p) => (
                  <a
                    key={p.id}
                    data-home-project={p.id}
                    data-home-project-name={p.name}
                    data-home-project-available={String(p.available)}
                    href={projectURL(p.id)}
                    data-snow-navigation=""
                  >
                    <span className="folder-icon">
                      <Icon kind="folder" />
                    </span>
                    <span title={`${p.name} · ${p.path}`}>
                      <strong>{p.name}</strong>
                      <small>{p.path}</small>
                      {!p.available && <small>Folder unavailable</small>}
                    </span>
                  </a>
                ))
              ) : (
                <p className="fine">No workspaces registered yet.</p>
              )}
              <div className="snow-menu-footer">
                <a
                  className="picker-add"
                  href="/?view=projects#add-project"
                  data-snow-navigation=""
                >
                  <Icon kind="plus" />
                  <span>Add workspace</span>
                </a>
              </div>
            </div>
          </details>
        </div>
        <form
          id="home-composer"
          className="composer home-composer"
          data-pending={draft.pending ? 'true' : undefined}
        >
          <label className="visually-hidden" htmlFor="home-prompt">
            Draft a message to Snow
          </label>
          <textarea
            id="home-prompt"
            rows={2}
            maxLength={65536}
            disabled={!draft.homeEnabled}
            value={draft.text}
            onChange={(e) => workspace.editDraft(e.currentTarget.value)}
            placeholder="What would you like to work on?"
            aria-describedby="home-privacy"
          />
          <div className="composer-actions home-composer-actions">
            <span className="home-composer-hint">
              Draft first. Choose where to work.
            </span>
            <button
              className="home-send"
              type="submit"
              disabled={!draft.homeEnabled || draft.pending}
            >
              Continue
            </button>
          </div>
        </form>
        <p id="home-privacy" className="home-privacy fine" role="status">
          {draft.privacy}
        </p>
        <noscript>
          <p className="fine">
            Drafting needs JavaScript. Choose a workspace above to browse it.
          </p>
        </noscript>
      </section>
    </>
  );
}
export function DraftNotice() {
  const { draft } = useWorkspace(),
    notice = draft.notice;
  if (!notice) return null;
  return (
    <aside className="notice home-draft-notice" id="home-draft-notice">
      <p>{notice.explanation}</p>
      <pre className="home-draft-preview">{notice.text}</pre>
      <div className="dialog-actions">
        {notice.useVisible && (
          <button
            className="button quiet"
            type="button"
            data-home-draft-use
            disabled={notice.useDisabled}
          >
            Use draft
          </button>
        )}
        <a
          className="button quiet"
          href={notice.url}
          data-snow-navigation=""
        >
          Edit draft
        </a>
        <button className="button quiet" type="button" data-home-draft-discard>
          Discard draft
        </button>
      </div>
    </aside>
  );
}
