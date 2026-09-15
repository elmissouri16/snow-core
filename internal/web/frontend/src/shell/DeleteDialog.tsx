import {useLayoutEffect, useRef, useState} from 'react';
import type {ShellController} from './controller';
export function DeleteDialog({controller: c}: {controller: ShellController}) {
  const attempt = c.snapshot.deletion;
  const dialog = useRef<HTMLDialogElement>(null);
  const [confirmed, setConfirmed] = useState(false);
  useLayoutEffect(() => {
    const node = dialog.current;
    if (!node || !attempt) return;
    if (!node.open) node.showModal();
    return () => { if (node.open) node.close(); };
  }, [!!attempt]);
  useLayoutEffect(() => { setConfirmed(false); }, [attempt?.owner, attempt?.project, attempt?.session]);
  // A capability withdrawal invalidates the confirmation synchronously at submit.
  // Also retire a not-yet-submitted dialog and return focus to its surviving link.
  useLayoutEffect(() => {
    if (attempt && !attempt.pending && !attempt.submitted && !c.eligible(attempt)) c.closeDelete();
  });
  if (!attempt) return null;
  return <dialog id="session-delete-dialog" className="folder-dialog session-delete-dialog" ref={dialog} aria-labelledby="session-delete-title" aria-describedby="session-delete-warning"
    onCancel={event => { event.preventDefault(); c.closeDelete(); }}>
    <div className="dialog-heading"><h2 id="session-delete-title">Delete session?</h2><button type="button" className="quiet" data-delete-cancel="" disabled={attempt.pending} onClick={c.closeDelete}>{attempt.submitted ? 'Close' : 'Cancel'}</button></div>
    <p className="session-delete-name" data-delete-name="">{attempt.name}</p>
    <p className="fine" id="session-delete-warning">Permanently delete this saved conversation and its managed private session data. This cannot be undone. Workspace files are not deleted. Unsent drafts remain in this tab.</p>
    <label className="checkbox-label"><input type="checkbox" data-delete-confirm="" checked={confirmed} disabled={attempt.submitted} onChange={event => setConfirmed(event.currentTarget.checked)} /><span>I understand this permanently deletes the saved session.</span></label>
    <p className="error" data-delete-error="" role="alert" hidden={!attempt.error}>{attempt.error}</p>
    <div className="dialog-actions"><button type="button" className="button danger" data-delete-submit="" disabled={!confirmed || attempt.submitted || !c.eligible(attempt)} onClick={() => void c.remove(confirmed)}>{attempt.pending ? 'Deleting…' : 'Delete session'}</button></div>
  </dialog>;
}
