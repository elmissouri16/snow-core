import {useLayoutEffect, useRef, useState} from 'react';
import type {ShellController} from './controller';
/** Workspace removal: one modal, one checkbox, inline errors; files and sessions stay on disk. */
export function RemoveDialog({controller: c}: {controller: ShellController}) {
  const attempt = c.snapshot.removal;
  const dialog = useRef<HTMLDialogElement>(null);
  const [confirmed, setConfirmed] = useState(false);
  useLayoutEffect(() => {
    const node = dialog.current;
    if (!node || !attempt) return;
    if (!node.open) node.showModal();
    return () => { if (node.open) node.close(); };
  }, [!!attempt]);
  useLayoutEffect(() => { setConfirmed(false); }, [attempt?.project]);
  if (!attempt) return null;
  return <dialog id="workspace-remove-dialog" className="folder-dialog session-delete-dialog" ref={dialog} aria-labelledby="workspace-remove-title" aria-describedby="workspace-remove-note"
    onCancel={event => { event.preventDefault(); c.closeRemoval(); }}>
    <div className="dialog-heading"><h2 id="workspace-remove-title">Remove workspace?</h2><button type="button" className="quiet" data-remove-cancel="" disabled={attempt.pending} onClick={c.closeRemoval}>Cancel</button></div>
    <p className="session-delete-name" data-remove-name="">{attempt.name}</p>
    <p className="fine mono catalog-path">{attempt.path}</p>
    <p className="fine" id="workspace-remove-note">Only the manager entry goes away. Project files and every saved session stay on disk, and you can add the folder again at any time.{attempt.live ? ' Its live session is closed first.' : ''}</p>
    <label className="checkbox-label"><input type="checkbox" data-remove-confirm="" checked={confirmed} disabled={attempt.submitted} onChange={event => setConfirmed(event.currentTarget.checked)} /><span>Remove this workspace from the manager{attempt.live ? ' and close its live session' : ''}.</span></label>
    <p className="error" data-remove-error="" role="alert" hidden={!attempt.error}>{attempt.error}</p>
    <div className="dialog-actions"><button type="button" className="button danger" data-remove-submit="" disabled={!confirmed || attempt.submitted} onClick={() => void c.removeWorkspace(confirmed)}>{attempt.pending ? 'Removing…' : 'Remove workspace'}</button></div>
  </dialog>;
}
