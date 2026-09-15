import {useImperativeHandle, useRef, useState, type Ref} from 'react';
import {flushSync} from 'react-dom';
import type {ControlsState} from './model';

export interface Suggestions {controls?: string; expanded: boolean; activeDescendant?: string}
export interface DraftHandle { update(text: string): boolean; suggestions(state: Suggestions): void; goalMode(enabled: boolean): void }

/** The textarea is never remounted for snapshots, notices, or admission changes.
 * Input only updates presentation here: the document input listener remains the
 * sole owner of the controller's revision and draft store. No submission handler.
 */
export function Editor({ref}: {ref: Ref<DraftHandle>}) {
  const [text, setText] = useState('');
  const [goalMode, setGoalMode] = useState(false);
  const [suggestions, setSuggestions] = useState<Suggestions>({expanded: false});
  const composing = useRef(false);
  useImperativeHandle(ref, () => ({update(value) {
    // A pending ACK/prepared read must never replace an active IME composition.
    if (composing.current) return false;
    setText(value); return true;
  }, suggestions: setSuggestions, goalMode: setGoalMode}), []);
  return <><label htmlFor="live-prompt" className="sr-only">{goalMode ? 'Goal objective' : 'Message Snow'}</label><textarea
    id="live-prompt" name="text" rows={1} maxLength={65536} required
    placeholder={goalMode ? 'Describe the goal…' : 'Message Snow…'} aria-describedby="composer-hint composer-state live-reuse-notice"
    aria-controls={suggestions.controls} aria-expanded={suggestions.expanded} aria-activedescendant={suggestions.activeDescendant}
    value={text} onChange={event => setText(event.currentTarget.value)}
    // Target-level mention discovery may synchronously repaint suggestion ARIA.
    // Commit the native text first, or that repaint restores the previous draft
    // before React's bubble handler sees the input. This adds no discovery owner.
    onInputCapture={event => flushSync(() => setText(event.currentTarget.value))}
    onCompositionStart={() => { composing.current = true; }}
    onCompositionEnd={event => { composing.current = false; setText(event.currentTarget.value); }}
  /></>;
}

export function Notices({view}: {view: ControlsState}) {
  return <>
    <div id="live-regenerate-notice" className="message-edit-notice" role="status" hidden={!view.regenerate.visible}><span data-message-regenerate-outcome="">{view.regenerate.text}</span><button type="button" className="quiet" data-message-regenerate-dismiss="" hidden={!view.regenerate.dismiss}>Dismiss</button></div>
    <div id="live-edit-notice" className="message-edit-notice" role="status" hidden={!view.edit.visible}><span data-message-edit-status="">{view.edit.text}</span><button type="button" className="quiet" data-message-edit-cancel="" disabled={view.edit.disabled}>Cancel</button></div>
    <div id="live-reuse-notice" className="message-reuse-notice" role="status" hidden={!view.reuse.visible}><span data-message-reuse-status="">{view.reuse.text}</span><button type="button" className="quiet" data-message-reuse-cancel="" disabled={view.reuse.disabled}>Cancel edit · restore draft</button></div>
  </>;
}
export function Actions({view}: {view: ControlsState}) {
  return <>
    <button className={'primary composer-send icon-button' + (view.sending ? ' is-sending' : '')} type="submit" id="live-send" aria-label={view.sendLabel} title={view.sendLabel} disabled={view.sendDisabled} hidden={view.showStop}>
      <svg className={'icon' + (view.sending ? ' composer-send-progress' : '')} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">{view.sending ? <circle cx="12" cy="12" r="7" strokeDasharray="32 12" /> : <path d="M12 19V5m-6 6 6-6 6 6"/>}</svg>
      <span data-sending-label="" className="sr-only" hidden={!view.sending}>Sending…</span>
    </button>
    <button type="button" className="primary composer-send composer-stop" data-runtime-abort="" data-stop-label={view.stopLabel} aria-label={view.stopLabel} title={view.stopTitle} hidden={!view.showStop} disabled={!view.canStop}><svg className="icon" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="7" y="7" width="10" height="10" rx="1.5" /></svg><span className="sr-only">{view.stopLabel}</span></button>
  </>;
}
export function Status({view}: {view: ControlsState}) {
  return <><span className={'composer-meta' + (view.statusIdle ? ' is-idle' : '')} id="composer-state" role="status">{view.status}</span>{view.queue.enabled && <button type="button" className="quiet queue-next-button" data-queue-next="" hidden={view.queue.hidden} disabled={view.queue.disabled} title={view.queue.title}>{view.queue.label}</button>}<span className="composer-hint" id="composer-hint">{view.queue.hint}</span></>;
}
export function TurnStatus({visible}: {visible: boolean}) {
  return <div id="live-turn-status" className="turn-status" role="status" aria-live="polite" hidden={!visible}>Working…</div>;
}
