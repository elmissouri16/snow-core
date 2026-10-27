import {useEffect, useImperativeHandle, useRef, useState, type Ref} from 'react';
import {download, engine, microphone, resetEngine, status, supported} from '../dictation/whistle';
import type {SpeechStatus} from '../dictation/whistle';
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
/** Dictation into the composer through the browser's speech recognition (Chrome, Edge, Safari on a secure origin). */
function Dictate({disabled}: {disabled: boolean}) {
  const [listening, setListening] = useState(false);
  const [phase, setPhase] = useState<'idle' | 'checking' | 'consent' | 'downloading' | 'loading' | 'error'>('idle');
  const [note, setNote] = useState('');
  const [progress, setProgress] = useState(0);
  const run = useRef<AbortController | null>(null), base = useRef(''), dialog = useRef<HTMLDialogElement>(null);
  const available = supported();
  useEffect(() => () => { run.current?.abort(); }, []);
  const open = phase === 'consent' || phase === 'downloading' || phase === 'loading' || phase === 'error';
  useEffect(() => {
    const node = dialog.current; if (!node) return;
    if (open && !node.open) node.showModal(); else if (!open && node.open) node.close();
  }, [open]);
  if (!available) return null;
  function write(committed: string, pending: string) {
    const prompt = document.getElementById('live-prompt') as HTMLTextAreaElement | null;
    if (!prompt) return;
    const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')?.set;
    const lead = base.current ? base.current.replace(/\s*$/, ' ') : '';
    setter?.call(prompt, lead + committed + (committed && pending ? ' ' : '') + pending);
    prompt.dispatchEvent(new Event('input', {bubbles: true}));
  }
  function fail(error: unknown) {
    resetEngine();
    setNote(error instanceof Error && error.message ? error.message : 'Dictation failed.');
    setPhase('error');
  }
  async function start() {
    if (run.current) { run.current.abort(); return; }
    setPhase('checking');
    let state: SpeechStatus | null = null;
    try { state = await status(); } catch { state = null; }
    if (!state) { fail(new Error('On-device dictation is unavailable on this manager.')); return; }
    if (!state.available) { setPhase('consent'); return; }
    await listen();
  }
  async function fetchModel() {
    const csrf = (document.querySelector('input[name="csrf"]') as HTMLInputElement | null)?.value || '';
    setPhase('downloading'); setNote('');
    try { await download(csrf); } catch (error) { fail(error); return; }
    await listen();
  }
  async function listen() {
    const controller = new AbortController(); run.current = controller;
    setPhase('loading'); setProgress(0);
    try {
      const prompt = document.getElementById('live-prompt') as HTMLTextAreaElement | null;
      base.current = prompt?.value || '';
      const [speech, stream] = await Promise.all([engine(p => setProgress(p.percentage)), navigator.mediaDevices.getUserMedia({audio: {channelCount: 1, echoCancellation: true, noiseSuppression: true}})]);
      if (controller.signal.aborted) { for (const track of stream.getTracks()) track.stop(); setPhase('idle'); return; }
      setPhase('idle'); setListening(true);
      let committed = '';
      // Stopping ends the microphone generator, which flushes the stream and
      // leaves the session reusable; an abort signal here would close it.
      for await (const update of speech.stream(microphone(stream, controller.signal))) {
        if (update.text) committed = committed ? committed + ' ' + update.text : update.text;
        write(committed, update.pending || '');
      }
      write(committed, '');
      setPhase('idle');
    } catch (error) {
      if (!controller.signal.aborted) fail(error); else { resetEngine(); setPhase('idle'); }
    } finally {
      if (run.current === controller) run.current = null;
      setListening(false);
    }
  }
  const busy = phase === 'checking' || phase === 'loading';
  const label = listening ? 'Stop dictation' : busy ? 'Starting dictation…' : 'Dictate a message (on-device)';
  const step = phase === 'downloading' ? 'Downloading the model to the host and verifying it…' : phase === 'loading' ? (progress > 0 && progress < 100 ? `Loading the model in this browser… ${progress}%` : 'Starting the speech engine and microphone…') : '';
  return <>
    <button type="button" className={'quiet icon-button composer-mic' + (listening ? ' is-listening' : '')} data-composer-dictate="" aria-pressed={listening} aria-label={label} title={label} disabled={disabled} aria-busy={busy} onClick={() => void start()}>
      <svg className="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true"><rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/></svg>
    </button>
    <dialog ref={dialog} id="speech-dialog" className="folder-dialog speech-dialog" aria-labelledby="speech-dialog-title" aria-busy={phase === 'downloading' || phase === 'loading'}
      onCancel={event => { event.preventDefault(); if (phase === 'consent' || phase === 'error') setPhase('idle'); else run.current?.abort(); }}>
      <div className="dialog-heading"><h2 id="speech-dialog-title">On-device dictation</h2>
        <button type="button" className="quiet" data-speech-cancel="" onClick={() => { if (phase === 'consent' || phase === 'error') setPhase('idle'); else run.current?.abort(); }}>Cancel</button></div>
      {phase === 'error' ? <p className="error" role="alert">{note}</p> : <>
        <p>Dictation runs the Whistle speech model (Cactus Compute, Apache-2.0) inside this browser with WebAssembly. Audio never leaves your device.</p>
        <p className="fine">The host downloads the 17 MB model once from huggingface.co, verifies its checksum, and keeps it in the manager directory for every paired browser. Nothing else is sent anywhere.</p>
      </>}
      {step && <div className="speech-progress" role="status" aria-live="polite"><progress max={100} value={phase === 'loading' && progress > 0 && progress < 100 ? progress : undefined} /><span>{step}</span></div>}
      <div className="dialog-actions">
        {phase === 'consent' && <button type="button" className="button primary" data-speech-download="" onClick={() => void fetchModel()}>Download and start</button>}
        {phase === 'error' && <button type="button" className="button primary" data-speech-retry="" onClick={() => { setPhase('idle'); void start(); }}>Try again</button>}
      </div>
    </dialog>
  </>;
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
