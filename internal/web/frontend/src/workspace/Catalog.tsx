import type { CatalogProps } from './model';
import { projectURL } from './model';
import { useWorkspace, useWorkspaceMounted, workspace } from './bridge';
import { Icon } from './Icons';
import { DraftNotice } from './Home';
import { ProjectOperations } from './Operations';
export function FolderPicker() {
  const { folder: f } = useWorkspace();
  return (
    <dialog
      id="folder-picker"
      className="folder-dialog"
      aria-labelledby="folder-title"
      aria-describedby="folder-description"
    >
      <div className="dialog-heading">
        <div>
          <span className="eyebrow">ON THE SNOW HOST</span>
          <h2 id="folder-title">Choose a workspace folder</h2>
        </div>
        <button
          className="quiet"
          type="button"
          data-folder-close
          aria-label="Close folder browser"
        >
          <Icon kind="close" />
        </button>
      </div>
      <div className="folder-content">
        <p id="folder-description" className="fine">
          Browse directories on the machine running Snow. Selecting a folder
          fills the workspace path; it does not add the workspace yet.
        </p>
        <div className="folder-toolbar">
          <button
            className="button"
            type="button"
            data-folder-home
            disabled={f.busy}
          >
            Home
          </button>
          <button
            className="button"
            type="button"
            data-folder-up
            disabled={f.busy || !f.path || f.path === f.parent}
          >
            ↑ Up
          </button>
          <span className="fine">Directories only</span>
        </div>
        <p
          id="folder-current"
          className="mono catalog-path"
          aria-label="Current host folder"
        >
          {f.path || 'Loading host home…'}
        </p>
        <p id="folder-error" className="error" role="alert" hidden={!f.error}>
          {f.error}
        </p>
        <p id="folder-status" className="fine" role="status" aria-live="polite">
          {f.status}
        </p>
        <ul
          id="folder-list"
          className="folder-list"
          aria-label="Host folders"
          aria-busy={f.busy}
        >
          {f.folders.map((row) => (
            <li key={row.path}>
              <button
                type="button"
                data-folder-path={row.path}
                aria-label={`Open folder ${row.name}`}
                disabled={f.busy}
              >
                {row.name}
              </button>
            </li>
          ))}
        </ul>
        <button
          className="button folder-more"
          type="button"
          data-folder-more
          hidden={!f.hasMore}
          disabled={f.busy}
        >
          Load more folders
        </button>
      </div>
      <div className="dialog-actions">
        <button className="quiet" type="button" data-folder-close>
          Cancel
        </button>
        <button
          className="primary"
          type="button"
          data-folder-select
          disabled={f.busy || !f.canSelect}
        >
          Select this folder
        </button>
      </div>
    </dialog>
  );
}
export function WorkspaceCatalog({
  projects,
  error,
  csrf,
  registryEnabled,
  projectOperationsEnabled,
}: CatalogProps) {
  useWorkspaceMounted();
  const { projectPath } = useWorkspace();
  return (
    <>
      <header className="workspace-heading">
        <div>
          <span className="eyebrow">YOUR WORKSPACE</span>
          <h1>Workspaces</h1>
        </div>
        <span className="host-label">
          <span className="status-dot" />
          Folders on this host
        </span>
      </header>
      {error && (
        <p className="error workspace-error" role="alert">
          {error}
        </p>
      )}
      {registryEnabled ? (
        <>
          <div className="projects-content">
            {projects.length ? (
              <section aria-labelledby="registered-heading">
                <div className="section-heading">
                  <h2 id="registered-heading">Pick up where you left off</h2>
                  <span className="fine">{projects.length} registered</span>
                </div>
                <div className="catalog-list project-list">
                  {projects.map((p) => (
                    <a
                      key={p.id}
                      className="catalog-row"
                      href={projectURL(p.id)}
                      hx-get={projectURL(p.id)}
                      hx-target="#workspace"
                      hx-swap="outerHTML"
                      hx-push-url="true"
                    >
                      <span className="row-icon" aria-hidden="true">
                        <Icon kind="folder" />
                      </span>
                      <span>
                        <strong>{p.name}</strong>
                        <span className="mono catalog-path fine">{p.path}</span>
                        {!p.available && (
                          <span className="unavailable">
                            Folder missing or identity changed
                          </span>
                        )}
                      </span>
                      <span aria-hidden="true">→</span>
                    </a>
                  ))}
                </div>
              </section>
            ) : (
              <div className="empty-state project-empty">
                <span className="empty-mark" aria-hidden="true">
                  <Icon kind="snow" />
                </span>
                <span className="eyebrow">START WITH A WORKSPACE</span>
                <h2>Good work starts here.</h2>
                <p>
                  Connect a folder on the machine running Snow.
                  <br />
                  Your workspaces and sessions, one focused workspace.
                </p>
              </div>
            )}
            <section
              id="add-project"
              className="add-project-panel"
              aria-labelledby="add-project-heading"
            >
              <DraftNotice />
              <div>
                <h2 id="add-project-heading">Add a workspace</h2>
                <p className="fine">
                  Choose an existing host folder. Registration creates a manager
                  entry, not a new directory. Your files stay where they are.
                </p>
              </div>
              <form
                method="post"
                action="/projects/add"
                id="add-project-form"
                hx-post="/projects/add"
                hx-target="#workspace"
                hx-select="#workspace"
                hx-swap="outerHTML"
                hx-push-url="true"
              >
                <input type="hidden" name="csrf" value={csrf} />
                <label htmlFor="project-path">Folder on the Snow host</label>
                <div className="path-input">
                  <input
                    id="project-path"
                    name="path"
                    maxLength={4096}
                    required
                    autoComplete="off"
                    spellCheck={false}
                    placeholder="/path/to/project"
                    aria-describedby="path-help"
                    value={projectPath}
                    onChange={(e) =>
                      workspace.editProjectPath(e.currentTarget.value)
                    }
                  />
                  <button className="button" type="button" data-folder-open>
                    Browse folders
                  </button>
                </div>
                <p id="path-help" className="fine">
                  Browse the host filesystem, or enter an absolute path
                  manually. This is not a browser upload.
                </p>
                <label htmlFor="project-name">
                  Display name <span className="optional">optional</span>
                </label>
                <input
                  id="project-name"
                  name="name"
                  maxLength={128}
                  autoComplete="off"
                  placeholder="Defaults to the folder name"
                />
                <div className="form-footer">
                  <span className="fine">No files created or modified</span>
                  <button className="primary" type="submit">
                    Add workspace <span aria-hidden="true">→</span>
                  </button>
                </div>
              </form>
            </section>
            <ProjectOperations csrf={csrf} enabled={projectOperationsEnabled} />
          </div>
          <FolderPicker />
        </>
      ) : (
        <div className="empty-state">
          <span className="empty-mark" aria-hidden="true">
            ▱
          </span>
          <h2>Workspace registry unavailable</h2>
          <p>
            Start the installed Snow binary with <code>snow --mode web</code> to
            enable persistent workspace registration.
          </p>
        </div>
      )}
    </>
  );
}
