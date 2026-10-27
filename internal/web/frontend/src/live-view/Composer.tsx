import {useEffect, useImperativeHandle, useRef, useState, type Ref} from 'react';
import {flushSync} from 'react-dom';
import type {ControlsState, TurnState} from './model';

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
type Recognition = {lang: string; interimResults: boolean; continuous: boolean; start(): void; stop(): void; onresult: ((event: {resultIndex: number; results: ArrayLike<ArrayLike<{transcript: string}> & {isFinal: boolean}>}) => void) | null; onend: (() => void) | null; onerror: (() => void) | null};
const recognitionClass = (): (new () => Recognition) | null => {
  const w = window as unknown as {SpeechRecognition?: new () => Recognition; webkitSpeechRecognition?: new () => Recognition};
  return window.isSecureContext ? (w.SpeechRecognition || w.webkitSpeechRecognition || null) : null;
};
/** Dictation into the composer through the browser's speech recognition (Chrome, Edge, Safari on a secure origin). */
function Dictate({disabled}: {disabled: boolean}) {
  const [listening, setListening] = useState(false);
  const active = useRef<Recognition | null>(null), base = useRef('');
  const available = recognitionClass() !== null;
  useEffect(() => () => { active.current?.stop(); }, []);
  if (!available) return null;
  function commit(transcript: string) {
    const prompt = document.getElementById('live-prompt') as HTMLTextAreaElement | null;
    if (!prompt) return;
    const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
    setter?.call(prompt, (base.current ? base.current.replace(/\s*$/, ' ') : '') + transcript);
    prompt.dispatchEvent(new Event('input', {bubbles: true}));
  }
  function toggle() {
    if (active.current) { active.current.stop(); return; }
    const Recognizer = recognitionClass(); if (!Recognizer) return;
    const prompt = document.getElementById('live-prompt') as HTMLTextAreaElement | null;
    base.current = prompt?.value || '';
    const recognition = new Recognizer();
    recognition.lang = navigator.language || 'en-US'; recognition.interimResults = true; recognition.continuous = true;
    let finals = '';
    recognition.onresult = event => {
      let interim = '';
      for (let i = event.resultIndex; i < event.results.length; i++) { const result = event.results[i]; if (result.isFinal) finals += result[0].transcript; else interim += result[0].transcript; }
      commit(finals + interim);
    };
    recognition.onend = () => { active.current = null; setListening(false); };
    recognition.onerror = () => { active.current = null; setListening(false); };
    active.current = recognition; setListening(true);
    try { recognition.start(); } catch { active.current = null; setListening(false); }
  }
  return <button type="button" className={'quiet icon-button composer-mic' + (listening ? ' is-listening' : '')} data-composer-dictate="" aria-pressed={listening} aria-label={listening ? 'Stop dictation' : 'Dictate a message'} title={listening ? 'Stop dictation' : 'Dictate (speech recognition in this browser)'} disabled={disabled} onClick={toggle}>
    <svg className="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true"><rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/></svg>
  </button>;
}
export function Actions({view}: {view: ControlsState}) {
  return <>
    <Dictate disabled={view.showStop}/>
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
function Elapsed({running}: {running: boolean}) {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    if (!running) { setSeconds(0); return; }
    const started = Date.now();
    const timer = setInterval(() => setSeconds(Math.floor((Date.now() - started) / 1000)), 1000);
    return () => clearInterval(timer);
  }, [running]);
  return seconds >= 2 ? <span className="turn-elapsed">{seconds}s</span> : null;
}
export function TurnStatus({visible, turn}: {visible: boolean; turn: TurnState}) {
  const thinking = turn.thinking;
  // Reasoning text stays on the host; the browser only learns that the model
  // is thinking and for how long, like a collapsed "Thought for 12s" chip.
  const thought = visible && !!thinking && !thinking.active && thinking.seconds > 0;
  return <div id="live-turn-status-region" className="turn-status-region" hidden={!visible}>
    {thought && <p className="live-thinking" data-active="false"><span className="live-thinking-label">Thought for {thinking.seconds}s</span></p>}
    <div id="live-turn-status" className="turn-status" role="status" aria-live="polite" hidden={!visible}>{thinking?.active ? <span className="live-thinking-dots" aria-hidden="true"><i/><i/><i/></span> : null}{turn.label}<Elapsed running={visible}/></div>
  </div>;
}
