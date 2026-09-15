import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react';
import { OperationController } from './operationController';
import type { OperationDraft } from './operationController';
import { activeStates, destination, labels } from './operationModel';
const controllers = new WeakMap<Element, OperationController>();
export const projectOperations = {
  init(_root: Element) {
    /* React mount owns initialization. */
  },
  setParentPath(root: Element, value: string) {
    return controllers.get(root)?.setParentPath(value) || false;
  },
  refresh(root: Element) {
    return controllers.get(root)?.refresh(0);
  },
  dispose(root: Element) {
    controllers.get(root)?.retire();
  },
};
export function ProjectOperations({
  csrf,
  enabled,
}: {
  csrf: string;
  enabled: boolean;
}) {
  return (
    <OperationsView
      key={csrf + String(enabled)}
      csrf={csrf}
      enabled={enabled}
    />
  );
}
function OperationsView({ csrf, enabled }: { csrf: string; enabled: boolean }) {
  const [controller] = useState(() => new OperationController(csrf, enabled));
  const state = useSyncExternalStore(
      controller.subscribe,
      controller.getSnapshot,
      controller.getSnapshot,
    ),
    root = useRef<HTMLDetailsElement>(null),
    back = useRef<HTMLButtonElement>(null),
    refresh = useRef<HTMLButtonElement>(null),
    browse = useRef<HTMLButtonElement>(null),
    parent = useRef<HTMLInputElement>(null);
  const composing = useRef(false);
  const { draft, grant, review, folder } = state,
    busy = state.pending || state.reading,
    unavailable = !enabled || busy,
    locked = unavailable || !!review || !!state.uncertain;
  useEffect(() => {
    const element = root.current;
    if (!element) return;
    controllers.set(element, controller);
    const retire = () => controller.retire();
    const dialog = element.closest('dialog');
    dialog?.addEventListener('close', retire);
    window.addEventListener('pagehide', retire);
    return () => {
      controllers.delete(element);
      dialog?.removeEventListener('close', retire);
      window.removeEventListener('pagehide', retire);
      controller.dispose();
    };
  }, [controller]);
  useLayoutEffect(() => {
    if (review) back.current?.focus();
  }, [review]);
  const edit = (field: keyof OperationDraft, value: string) =>
    controller.edit(field, value);
  return (
    <details
      ref={root}
      className="project-operations"
      data-project-operations
      data-enabled={String(enabled)}
      aria-busy={busy}
      onToggle={(e) => {
        if (!e.currentTarget.open) controller.retire();
      }}
    >
      <summary>Create / clone projects &amp; operations</summary>
      <div className="project-operations-content">
        <input type="hidden" data-op-csrf value={csrf} />
        <p className="fine">
          Create a new folder or clone an anonymous HTTPS repository on the Snow
          host. After completion, separately review and confirm registration.
          Registration never activates an agent or opens a conversation.
        </p>
        <p className="project-operations-authority">
          Host-user OS authority: this is not confined to Snow’s startup folder
          or a filesystem sandbox. Creation writes a directory; cloning also
          accesses the network and downloads files. No disk quota is provided.
          SSH and embedded credentials are not supported.
        </p>
        {!enabled && (
          <p className="error">
            Project operations are unavailable in this manager. No alternative
            command or activation will be attempted.
          </p>
        )}
        <fieldset
          className="project-operation-draft"
          data-op-draft
          disabled={locked}
          onCompositionStart={() => {
            composing.current = true;
          }}
          onCompositionEnd={() => {
            composing.current = false;
          }}
          onKeyDown={(e) => {
            if (
              e.key === 'Enter' &&
              (e.target instanceof HTMLInputElement ||
                e.target instanceof HTMLSelectElement) &&
              !e.nativeEvent.isComposing &&
              !composing.current
            )
              e.preventDefault();
          }}
        >
          <legend>New destination</legend>
          <label>
            Absolute parent folder on the Snow host
            <input
              ref={parent}
              data-op-parent
              type="text"
              maxLength={4096}
              placeholder="/absolute/host/folder"
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              value={draft.parent}
              onChange={(e) => edit('parent', e.currentTarget.value)}
            />
          </label>
          <div className="project-operation-actions">
            <button
              ref={browse}
              type="button"
              className="button"
              data-op-browse
              onClick={() => void controller.browse(draft.parent)}
            >
              Browse host folders
            </button>
            <button
              type="button"
              className="button"
              data-op-select
              onClick={() => {
                if (!composing.current) void controller.select();
              }}
            >
              Select parent
            </button>
          </div>
          <section
            className="project-operation-folders"
            data-op-folders
            hidden={!state.foldersOpen}
            aria-label="Browse host parent folders"
          >
            <p className="fine">
              Browsing only reads directories. Choosing a path fills the draft;
              select it explicitly to grant creation in that parent.
            </p>
            <div className="project-operation-actions">
              <button
                type="button"
                className="quiet"
                data-op-home
                onClick={() => void controller.browse()}
              >
                Home
              </button>
              <button
                type="button"
                className="quiet"
                data-op-up
                disabled={!folder || folder.path === folder.parent}
                onClick={() => {
                  if (folder) void controller.browse(folder.parent);
                }}
              >
                Up
              </button>
              <button
                type="button"
                className="quiet"
                data-op-folder-close
                onClick={() => {
                  controller.closeFolders();
                  browse.current?.focus();
                }}
              >
                Close folder list
              </button>
            </div>
            <p className="mono" data-op-folder-path>
              {folder?.path}
            </p>
            <ul data-op-folder-list aria-label="Host folders">
              {folder?.folders.map((row) => (
                <li key={row.path}>
                  <button
                    type="button"
                    className="quiet"
                    onClick={() => void controller.browse(row.path)}
                  >
                    {row.name}
                  </button>
                </li>
              ))}
            </ul>
            <div className="project-operation-actions">
              <button
                type="button"
                className="button"
                data-op-folder-more
                hidden={!folder?.has_more}
                onClick={() => {
                  if (folder)
                    void controller.browse(folder.path, folder.next_offset);
                }}
              >
                Next folder batch
              </button>
              <button
                type="button"
                className="button"
                data-op-folder-use
                disabled={!folder || state.reading}
                onClick={() => {
                  if (folder) {
                    controller.setParentPath(folder.path);
                    parent.current?.focus();
                  }
                }}
              >
                Use this path in draft
              </button>
            </div>
          </section>
          <p className="fine" data-op-grant role="status">
            {grant
              ? `Selected ${grant.path}. Expires ${new Date(grant.expires_at).toLocaleTimeString()}. One request only; host-user OS authority.`
              : 'No parent selected. A selection lasts five minutes and authorizes only one request.'}
          </p>
          <div className="project-operation-fields">
            <label>
              Operation
              <select
                data-op-kind
                value={draft.kind}
                onChange={(e) => edit('kind', e.currentTarget.value)}
              >
                <option value="create">Create empty folder</option>
                <option value="clone">Clone anonymous HTTPS repository</option>
              </select>
            </label>
            <label>
              New folder name
              <input
                data-op-name
                type="text"
                maxLength={128}
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
                placeholder="new-project"
                value={draft.name}
                onChange={(e) => edit('name', e.currentTarget.value)}
              />
            </label>
          </div>
          <p className="fine">
            One name only, 1–128 UTF-8 bytes. No slashes, backslashes, control
            characters, surrounding whitespace, “.” or “..”. The destination
            must not already exist.
          </p>
          <label data-op-remote-label hidden={draft.kind !== 'clone'}>
            Anonymous HTTPS repository URL
            <input
              data-op-remote
              type="text"
              maxLength={512}
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              placeholder="https://example.com/team/repository.git"
              aria-describedby="project-operation-remote-help"
              value={draft.remote}
              onChange={(e) => edit('remote', e.currentTarget.value)}
            />
          </label>
          <p
            id="project-operation-remote-help"
            className="fine"
            data-op-remote-help
            hidden={draft.kind !== 'clone'}
          >
            No username, password, token query, fragment, SSH or local path.
            Rejected URLs are not retained as drafts.
          </p>
          <button
            type="button"
            className="button primary"
            data-op-review
            disabled={!grant || Date.now() >= grant.expires_at}
            onClick={() => {
              if (!composing.current) controller.reviewCreation();
            }}
          >
            Review destination &amp; effects…
          </button>
        </fieldset>
        <section
          className="project-operation-review"
          data-op-confirmation
          hidden={!review}
          aria-label="Confirm project operation"
        >
          <h3 data-op-confirm-title>{review?.title || 'Review operation'}</h3>
          <p className="mono" data-op-confirm-detail>
            {review?.detail}
          </p>
          <p data-op-confirm-effects>{review?.effects}</p>
          <label className="project-operation-checkbox">
            <input
              type="checkbox"
              data-op-confirm-check
              checked={state.checked}
              disabled={unavailable}
              onChange={(e) => controller.checkConsent(e.currentTarget.checked)}
            />{' '}
            I reviewed this exact destination and the stated effects.
          </label>
          <div className="project-operation-actions">
            <button
              ref={back}
              type="button"
              className="quiet"
              data-op-back
              disabled={state.pending}
              onClick={() => {
                controller.back();
                refresh.current?.focus();
              }}
            >
              Back without changing anything
            </button>
            <button
              type="button"
              className="primary"
              data-op-confirm
              disabled={unavailable || !review || !state.checked}
              onClick={() => {
                if (!composing.current) void controller.confirm();
              }}
            >
              {review?.button || 'Confirm operation'}
            </button>
          </div>
        </section>
        <p
          className="project-operation-status"
          data-op-status
          role="status"
          aria-live="polite"
        >
          {state.status}
        </p>
        <div
          className="project-operation-uncertain"
          data-op-uncertain
          hidden={!state.uncertain}
        >
          <p data-op-uncertain-text>
            {state.uncertain &&
              `Request ${state.uncertain}. Its response was not reviewed here. Check the durable record; do not recreate or retry a clone to discover its outcome.`}
          </p>
          <button
            type="button"
            className="button"
            data-op-check
            disabled={unavailable}
            onClick={() => void controller.check()}
          >
            Check recorded request (read only)
          </button>
        </div>
        <section
          className="project-operation-inventory"
          aria-label="Durable project operations"
        >
          <div className="section-heading">
            <h3>Operations</h3>
            <button
              ref={refresh}
              type="button"
              className="button"
              data-op-refresh
              disabled={unavailable}
              onClick={() => void controller.refresh()}
            >
              Refresh operations
            </button>
          </div>
          <p className="fine">
            One operation runs at a time; there is no queue. Up to 128 retained
            records. Pages contain at most 32 records and 64 KiB. Partial
            destinations are retained; dismissing a record never deletes files.
          </p>
          <ul data-op-list className="project-operation-list">
            {state.operations.map((op) => {
              const action = (
                label: string,
                verb: 'cancel' | 'reconcile' | 'register' | 'dismiss',
              ) => (
                <button
                  type="button"
                  className="button"
                  data-op-row-action={verb}
                  disabled={locked}
                  onClick={() => controller.reviewAction(op, verb)}
                >
                  {label}
                </button>
              );
              return (
                <li key={op.id} data-operation-id={op.id}>
                  <strong>
                    {op.name} · {labels[op.state]}
                  </strong>
                  <small>
                    {op.kind === 'clone' ? 'Clone' : 'Create'} · outcome{' '}
                    {op.outcome} · revision {op.revision}
                  </small>
                  <small className="mono">Reference {op.id}</small>
                  <small className="mono">
                    {op.child?.path || destination(op.parent.path, op.name)}
                  </small>
                  {op.remote && <small className="mono">{op.remote}</small>}
                  {op.project_id && (
                    <small className="mono">
                      Registered project {op.project_id}. No agent was
                      activated.
                    </small>
                  )}
                  {op.outcome === 'unknown' && (
                    <p className="fine">
                      Ownership or completion is unknown. Observe the
                      destination before deciding what to register; never retry
                      the clone as recovery.
                    </p>
                  )}
                  <div className="project-operation-actions">
                    {activeStates.has(op.state) ? (
                      op.state !== 'cancel_requested' &&
                      action('Request stop…', 'cancel')
                    ) : (
                      <>
                        {action('Observe identity…', 'reconcile')}
                        {!op.project_id &&
                          action(
                            op.state === 'awaiting_registration' &&
                              op.outcome === 'observed'
                              ? 'Register retained destination…'
                              : 'Review ordinary registration…',
                            'register',
                          )}
                        {action('Dismiss record only…', 'dismiss')}
                      </>
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
          <div className="project-operation-actions">
            <button
              type="button"
              className="quiet"
              data-op-first
              hidden={state.offset === 0}
              disabled={unavailable}
              onClick={() => void controller.refresh()}
            >
              First page
            </button>
            <button
              type="button"
              className="button"
              data-op-next
              hidden={!state.more}
              disabled={unavailable}
              onClick={() => void controller.refresh(state.next)}
            >
              Next page
            </button>
            <span className="fine" data-op-page>
              {state.operations.length
                ? `Records ${state.offset + 1}–${state.offset + state.operations.length}.`
                : 'No records on this page.'}
            </span>
          </div>
        </section>
      </div>
    </details>
  );
}
