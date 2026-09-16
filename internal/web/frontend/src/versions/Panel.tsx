import type { HistoryView, VersionsView } from './types';
import type { Selection } from './model';
import { sameSelection } from './model';

// Rendering is presentation only: the synchronous controllers own authority.
export function Panel({v, h, readable, restorable, historySafe, selection, historyChanged}: {
  v: VersionsView; h: HistoryView | null; readable: boolean; restorable: boolean; historySafe: boolean; selection: Selection | null; historyChanged(): void;
}) {
  const {ui, page, preview, phase} = v;
  const shownPage = page || v.retained?.page;
  const shownPreview = preview || v.retained?.preview;
  const selected = v.selected || v.retained?.selected;
  const previewIndex = preview ? v.previewIndex : v.retained?.previewIndex ?? v.previewIndex;
  return <>
    <button type="button" className="quiet" data-versions-open="" aria-haspopup="dialog" aria-controls="versions-dialog" hidden={!ui?.supported} disabled={!ui?.readable}>Versions</button>
    <dialog ref={element => { v.dialog = element; }} id="versions-dialog" className="versions-dialog runtime-dialog" aria-labelledby="versions-heading" aria-describedby="versions-boundary">
      <div className="dialog-heading">
        <h2 id="versions-heading">Conversation versions</h2>
        <button type="button" className="quiet" data-versions-refresh="" disabled={!readable}>Refresh</button>
        <button type="button" className="quiet" data-runtime-abort="" hidden={!ui?.showStop} disabled={!ui?.canStop} title={ui?.stopTitle}>{ui?.stopLabel || 'Stop'}</button>
        <button type="button" className="quiet icon-button" data-versions-close="" aria-label="Close versions" title="Close"><svg className="icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg></button>
      </div>
      <div className="runtime-dialog-body">
        <p id="versions-boundary" className="fine">Preview saved conversation history without changing the active chat. Restoring changes conversation history only: it does not undo file changes or tool effects, and it never replays a prompt.</p>
        <p data-versions-status="" className="fine" role="status" aria-live="polite">{v.error || (v.operation ? 'Loading a read-only version view…' : !ui?.readable ? 'This runtime is unavailable or has changed. No restore will be retried.' : v.retained ? 'Previous read-only view retained. Select a version to verify it again; restore remains disabled.' : !ui.restoreSafe ? 'Read-only browsing. Restore is unavailable while work, queued/review items, or another action is pending.' : 'Read-only preview; the active chat is unchanged.')}</p>
        <div className="versions-columns">
          <section className="versions-list-region" aria-label="Saved versions">
            <div data-versions-list="" className="versions-list">{shownPage?.versions.map(item => <button key={JSON.stringify([item.branch_id, item.tip_id])} type="button" className="version-choice" data-version-id={item.branch_id} data-tip-id={item.tip_id} data-version-preview="" aria-pressed={v.selected?.branch_id === item.branch_id && v.selected?.tip_id === item.tip_id} disabled={!readable || !page}><span>{item.name || 'Unnamed version'}{item.current ? ' · Current' : ''}</span><code>{`Branch: ${item.branch_id}\nTip: ${item.tip_id || '(empty history)'}`}</code></button>)}{shownPage && !shownPage.versions.length && <p className="fine">No saved versions on this page.</p>}</div>
            <button type="button" className="quiet" data-versions-back="" hidden={v.listIndex <= 0} disabled={!readable || !page}>Previous versions page</button>
            <button type="button" className="quiet" data-versions-more="" hidden={!shownPage?.has_more} disabled={!readable || !page}>Next versions page</button>
          </section>
          <section className="versions-preview" aria-labelledby="version-preview-heading">
            <h3 id="version-preview-heading">Read-only preview</h3>
            <p data-version-preview-title="" className="fine">{selected ? `${selected.name || 'Unnamed version'} · read-only preview page ${previewIndex + 1}` : 'Choose a saved version.'}</p>
            <code data-version-tip="" className="version-tip">{selected ? `Branch: ${selected.branch_id}\nTip: ${selected.tip_id || '(empty history)'}` : ''}</code>
            <p data-version-preview-notice="" className="fine" hidden={!shownPreview}>{shownPreview ? `Only this bounded preview page is shown. Restore selects the complete saved version.${shownPreview.history_truncated ? ' Some saved history is omitted from this preview.' : ''}${shownPreview.history_tools_truncated ? ' Some tool details are omitted.' : ''}` : ''}</p>
            <div data-version-preview-messages="" className="versions-preview-messages" aria-label="Read-only saved conversation">{shownPreview?.messages.map((message, index) => <article key={index} className="version-preview-message"><strong>{message.role}</strong><pre>{message.text}</pre>{(message.tools || []).map((tool, index) => <pre key={index}>{`Tool: ${typeof tool.tool === 'string' ? tool.tool.slice(0, 256) : 'saved tool'} · ${typeof tool.status === 'string' ? tool.status.slice(0, 64) : 'saved'}`}</pre>)}</article>)}</div>
            <div className="versions-preview-pagination">
              <button type="button" className="quiet" data-version-preview-back="" hidden={v.previewIndex <= 0} disabled={!readable || !preview}>Previous preview page</button>
              <button type="button" className="quiet" data-version-preview-more="" hidden={!shownPreview?.has_more} disabled={!readable || !preview}>Next preview page</button>
            </div>
            <button type="button" className="button" data-version-restore="" hidden={!shownPreview || !!selected?.current || !!phase} disabled={!restorable} title={ui?.restoreSafe ? 'Prepare an explicit conversation-history-only restore' : 'Restore requires an idle, connected conversation with no pending/review queue or conflicting action'}>Restore this version…</button>
            <section data-version-restore-confirmation="" className="version-restore-confirmation" aria-labelledby="version-restore-heading" hidden={!phase}>
              <h3 id="version-restore-heading">Restore this conversation version?</h3>
              <p>Only the active conversation history changes. Earlier file changes and tool effects are not undone. No prompt is replayed and no new answer is generated. Your unsent draft is kept.</p>
              <p data-version-restore-target="" className="fine">{v.restoreTarget}</p>
              <p data-version-restore-status="" className="fine" role="status">{phase === 'preparing' ? 'Preparing confirmation only. Nothing has changed.' : phase === 'ready' ? 'Confirm to select this saved history without starting a turn.' : phase === 'committing' ? 'Restoring conversation history…' : ''}</p>
              <div className="dialog-actions"><button type="button" className="quiet" data-version-restore-cancel="" disabled={phase === 'committing'}>Cancel restore</button><button type="button" className="button danger" data-version-restore-confirm="" disabled={phase !== 'ready' || !restorable || !v.token || Date.now() >= (v.expires ?? 0)}>Restore conversation version</button></div>
            </section>
          </section>
        </div>
        {v.api.root.dataset.historyControlEnabled === 'true' && <HistoryPanel h={h} selected={selection} safe={historySafe} changed={historyChanged} />}
      </div>
    </dialog>
  </>;
}

function HistoryPanel({h, selected, safe, changed}: {h: HistoryView | null; selected: Selection | null; safe: boolean; changed(): void}) {
  const ready = safe && !!selected, confirmation = h?.confirmation;
  return <section data-history-controls="" className="history-controls" aria-labelledby="history-controls-heading" hidden={h?.ui?.supported !== true}>
    <h3 id="history-controls-heading">Use this saved history</h3>
    <p className="fine">Fork a branch here, create a detached conversation, or rename the selected branch. These are conversation-history actions, not filesystem undo: no workspace files or worktrees are created or restored, and no prompts or tools are replayed.</p>
    <p data-history-notice="" className="fine" role="status" aria-live="polite">{h?.error || (h?.busy ? 'Applying history change…' : !selected ? 'Preview a saved version at the current revision before choosing an action. Refresh Versions if this selection has changed.' : !h?.ui?.safe ? 'History changes require an idle, connected conversation with no pending goal, unfinished turn, queued/review work or conflicting action.' : 'These actions change saved conversation history only. Your unsent message draft is kept.')}</p>
    <form data-history-form="" autoComplete="off">
      <label htmlFor="history-control-name">New branch or conversation name</label>
      <input id="history-control-name" data-history-name="" name="history_name_draft" type="text" maxLength={256} autoComplete="off" enterKeyHint="done" placeholder="Name for the selected action" required value={h?.draft.name || ''} onChange={event => { if (h) { h.draft.name = event.currentTarget.value; event.currentTarget.setCustomValidity(''); changed(); } }} disabled={!!confirmation || !!h?.busy} />
      <p className="fine">Branch names: up to 64 characters. Conversation names: up to 72. Both allow up to 256 UTF-8 bytes; no surrounding spaces or control characters.</p>
      <div className="dialog-actions history-control-actions">
        <button type="button" className="button" data-history-review="history-branch-fork" disabled={!ready || !!confirmation}>Fork branch here…</button>
        <button type="button" className="button" data-history-action="history-session-fork" disabled={!ready || !!confirmation}>Create detached conversation</button>
        <button type="button" className="quiet" data-history-action="history-branch-rename" disabled={!ready || !!confirmation}>Rename selected branch</button>
      </div>
      <section data-history-confirmation="" className="history-control-confirmation" aria-labelledby="history-confirm-heading" hidden={!confirmation}>
        <h4 id="history-confirm-heading">Confirm this history action</h4>
        <p data-history-target="">{h?.targetText}</p>
        <p>No workspace files are undone, no tools are replayed, and no message is sent. Forking a branch activates it; forking a detached conversation does not open it. Your unsent composer draft is kept.</p>
        <label className="history-control-consent"><input type="checkbox" data-history-consent="" checked={!!h?.consent} onChange={event => { if (h) { h.consent = event.currentTarget.checked; changed(); } }} /> I confirm this action on the exact saved branch and tip shown above.</label>
        <div className="dialog-actions"><button type="button" className="quiet" data-history-cancel="" disabled={!!h?.busy}>Cancel</button><button type="submit" className="button primary" data-history-confirm="" disabled={!ready || !confirmation || !sameSelection(confirmation.target, selected) || !h?.consent}>{h?.confirmLabel || 'Confirm history action'}</button></div>
      </section>
    </form>
    {h?.inventory && <div className="fine" data-history-inventory="">
      <p>{h.inventory.text}</p>
      {!!h.inventory.rows.length && <ul>{h.inventory.rows.map((row, index) => <li key={`${row.url}:${index}`}><a href={row.url} data-snow-navigation="">Open saved conversation: {row.name || 'Untitled'}</a></li>)}</ul>}
    </div>}
  </section>;
}
