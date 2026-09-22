import type { ReactNode } from 'react';
import type { ColdProps } from './model';
import { DraftNotice } from './Home';
import { Icon } from './Icons';
import { shell } from '../shell';
import { useWorkspace, useWorkspaceMounted, workspace } from './bridge';
export function ColdHeader({
  project,
  sessionID,
  sessionTitle,
  error,
}: ColdProps) {
  const { flowError, inspectorOpen } = useWorkspace();
  return (
    <>
      <header className="workspace-heading project-chat-heading">
        <div className="project-heading">
          <h1>
            {sessionID
              ? sessionTitle || 'Untitled session'
              : error
                ? 'Workspace'
                : 'New session'}
          </h1>
          <span className="fine">{project.name}</span>
        </div>
        <button
          className="quiet icon-button"
          type="button"
          data-inspector-toggle
          aria-controls="project-inspector"
          aria-expanded={inspectorOpen}
          aria-label="Files and changes"
          title="Files and changes"
        >
          <Icon kind="panel" />
        </button>
      </header>
      {error && (
        <p className="error workspace-error" role="alert">
          {error}
        </p>
      )}
      {flowError && (
        <p className="error" id="workspace-flow-error" role="alert">
          {flowError}
        </p>
      )}
    </>
  );
}
export function Activation({
  project,
  csrf,
  sessionID,
}: Pick<ColdProps, 'project' | 'csrf' | 'sessionID'>) {
  const { draft, activation } = useWorkspace();
  return (
    <section
      className={`runtime-activation activation-panel workspace-session-start${project.trusted ? ' activation-trusted' : ''}`}
      aria-labelledby="activation-heading"
    >
      <DraftNotice />
      <h2 id="activation-heading">
        {sessionID ? 'Paused · resume to continue' : 'Start a session'}
      </h2>
      <form
        method="post"
        action={`/projects/${project.id}/runtime/open`}
        data-runtime-open
      >
        <input type="hidden" name="csrf" value={csrf} />
        {sessionID && (
          <input type="hidden" name="session_id" value={sessionID} />
        )}
        <label className="visually-hidden" htmlFor="workspace-prompt">
          Session draft
        </label>
        <textarea
          id="workspace-prompt"
          className="activation-draft"
          rows={2}
          maxLength={65536}
          data-draft-project={project.id}
          data-draft-session={sessionID}
          data-draft-name={project.name}
          placeholder="What would you like to work on?"
          aria-describedby="workspace-draft-help"
          disabled={!draft.workspaceEnabled || activation.busy}
          value={draft.workspaceText}
          onChange={(e) => workspace.editDraft(e.currentTarget.value, true)}
        />
        <p className="fine" id="workspace-draft-help">
          Drafts stay in this tab. {sessionID ? 'Resume' : 'Start'}, then review
          and send.
        </p>
        <p className="activation-boundary">
          Starting loads this workspace’s configuration and instructions,
          connects enabled MCP servers, and allows model-directed subagents.
          Tools, local MCP servers, and child agents run with your host account’s
          privileges, not in a sandbox. Browsing does not start an agent or MCP
          server.
        </p>
        {project.trusted ? (
          <input type="hidden" name="confirm" value="trusted" />
        ) : (
          <>
            <input type="hidden" name="remember_trust" value="project" />
            <label className="checkbox-label">
              <input
                name="confirm"
                type="checkbox"
                value="activate"
                disabled={activation.busy}
                required
              />{' '}
              I trust this workspace. Remember my choice for future starts in
              this manager.
            </label>
            <p className="fine">
              Trust does not grant tools Allow permissions. Forget it in
              Settings → Workspaces.
            </p>
          </>
        )}
        <details className="activation-model-help">
          <summary>Startup settings</summary>
          {sessionID && (
            <p className="fine" data-saved-policy-note>
              Resuming restores saved session permissions. A saved Allow policy
              skips tool approval prompts.
            </p>
          )}
          <p className="fine" data-skills-startup-policy>
            Installed skills are {project.skillsEnabled ? 'enabled' : 'disabled'}
            {' '}for this workspace. Change future starts in{' '}
            <button
              type="button"
              className="quiet"
              data-settings-open="workspaces"
              onClick={(event) => {
                event.stopPropagation();
                shell.openSettings('workspaces', event.currentTarget);
              }}
            >
              Settings → Workspaces
            </button>
            . Project skills still require separate CLI extension trust.
          </p>
          <p className="fine">
            Uses the host’s configured provider and model. Change models while
            idle after starting. If the default cannot start, configure it on
            the host first.
          </p>
          {project.trusted && (
            <p className="fine">
              Workspace trust remembered. Manage it in{' '}
              <button
                type="button"
                className="quiet"
                data-settings-open="workspaces"
                onClick={(event) => {
                  event.stopPropagation();
                  shell.openSettings('workspaces', event.currentTarget);
                }}
              >
                Settings → Workspaces
              </button>
              .
            </p>
          )}
        </details>
        <p
          className="error"
          data-action-error
          role="alert"
          hidden={!activation.error}
        >
          {activation.error}
        </p>
        <div className="form-footer">
          <span className="fine">
            {!sessionID && 'New sessions start in Ask'}
          </span>
          <button className="primary" type="submit" disabled={activation.busy}>
            {sessionID
              ? 'Resume session'
              : project.trusted
                ? 'Start session'
                : 'Trust & start'}{' '}
            <span aria-hidden="true">→</span>
          </button>
        </div>
      </form>
      <noscript>
        <p className="fine">
          Live controls need JavaScript. Workspace registration and saved
          history remain available without it.
        </p>
      </noscript>
    </section>
  );
}
export function ColdConversation(props: ColdProps & { history?: ReactNode }) {
  useWorkspaceMounted();
  const {
    project,
    sessionID,
    error,
    hasHistory,
    nextURL,
    recoveryMessage,
    recoveryURL,
    runtimeEnabled,
    history,
  } = props;
  return (
    <>
      {recoveryMessage && (
        <aside
          className="notice recovery-notice"
          aria-label="Saved conversation recovery"
        >
          <p>{recoveryMessage}</p>
          <p className="fine">
            Tools may already have had effects. Reads do not reactivate workers,
            replay prompts, or restore approvals.
          </p>
          {recoveryURL && (
            <a
              href={recoveryURL}
              data-snow-navigation=""
            >
              Review the last opened saved conversation
            </a>
          )}
        </aside>
      )}
      {hasHistory ? (
        <div className="conversation-stream">
          {history ?? <div id="workspace-history-view" />}
          {nextURL && (
            <a
              className="button"
              href={nextURL}
              data-snow-navigation=""
            >
              Next page →
            </a>
          )}
        </div>
      ) : (
        <div
          id="live-session"
          className="live-session project-session-list"
          data-project={project.id}
        >
          <div className="conversation-stream">
            <div className="empty-state compact">
              <h2>
                {sessionID
                  ? 'Session history unavailable'
                  : error
                    ? 'Sessions unavailable'
                    : 'What would you like to work on?'}
              </h2>
              <p>
                {sessionID
                  ? 'Retry this read, or explicitly resume below. Nothing has started automatically.'
                  : error
                    ? 'Saved sessions could not be read. Nothing has started. Starting below creates a new session; it does not resume an unread session.'
                    : `A new session in ${project.name}. Your other sessions stay in the sidebar.`}
              </p>
            </div>
          </div>
        </div>
      )}
      {runtimeEnabled && project.available ? (
        <Activation {...props} />
      ) : (
        !hasHistory && (
          <div className="empty-state compact">
            <h2>
              {!project.available
                ? 'Workspace folder unavailable'
                : 'Live sessions unavailable'}
            </h2>
            <p>
              {!project.available
                ? 'The folder is missing or its identity has changed. Check the host folder before starting this workspace.'
                : 'You can browse saved conversations without starting an agent.'}
            </p>
          </div>
        )
      )}
    </>
  );
}
export function ColdWorkspace(props: ColdProps & { history?: ReactNode }) {
  return (
    <>
      <ColdHeader {...props} />
      <ColdConversation {...props} />
    </>
  );
}
