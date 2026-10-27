import {createWhistle} from 'cactus-needle/browser';
import type {Whistle} from 'cactus-needle/browser';

/** The model the host serves after its one-time, user-confirmed download. */
export const MODEL_URL = '/speech/model';
export const MODEL_SHA256 = 'b6e02f048568ac5d01a2042556c658061e699acbc0aa2a1439f52f3d461dffeb';
export const SAMPLE_RATE = 16000;
export type SpeechStatus = {available: boolean; downloading: boolean; size: number; license: string; model: string};
export type Progress = {loaded: number; total: number; percentage: number};

export function supported(): boolean {
  return typeof window !== 'undefined' && window.isSecureContext && typeof WebAssembly === 'object' && typeof Worker === 'function' && !!navigator.mediaDevices?.getUserMedia;
}
export async function status(): Promise<SpeechStatus | null> {
  const response = await fetch('/speech/status', {credentials: 'same-origin', headers: {Accept: 'application/json'}, cache: 'no-store'});
  if (!response.ok) return null;
  const data = await response.json() as Partial<SpeechStatus>;
  return {available: data.available === true, downloading: data.downloading === true, size: Number(data.size) || 0, license: String(data.license || ''), model: String(data.model || '')};
}
export async function download(csrf: string): Promise<SpeechStatus> {
  const response = await fetch('/speech/download', {method: 'POST', credentials: 'same-origin', headers: {Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded'}, body: new URLSearchParams({csrf, confirm: 'download'})});
  if (!response.ok) throw new Error((await response.text()).trim().slice(0, 200) || 'Download failed');
  const data = await response.json() as Partial<SpeechStatus>;
  if (data.available !== true) throw new Error('The model is not available yet');
  return {available: true, downloading: false, size: Number(data.size) || 0, license: String(data.license || ''), model: String(data.model || '')};
}

let session: Promise<Whistle> | null = null;
/** One engine per page; the model and WebAssembly are cached by the browser after the first load. */
export function engine(onProgress?: (progress: Progress) => void): Promise<Whistle> {
  session ??= createWhistle({model: MODEL_URL, modelSha256: MODEL_SHA256, cache: true,
    onDownloadProgress: onProgress ? progress => onProgress({loaded: progress.loaded, total: progress.total ?? 0, percentage: progress.percentage ?? 0}) : undefined,
  }).catch(error => { session = null; throw error; });
  return session;
}
/** A session that failed or was closed is replaced on the next start. */
export function resetEngine(): void {
  const old = session; session = null;
  void old?.then(speech => speech.close()).catch(() => undefined);
}

/** Linear resampling is enough for speech when the context cannot run at 16 kHz. */
export function resample(input: Float32Array, from: number, to = SAMPLE_RATE): Float32Array {
  if (from === to) return input;
  const length = Math.max(1, Math.round(input.length * to / from)), output = new Float32Array(length), ratio = from / to;
  for (let i = 0; i < length; i++) {
    const position = i * ratio, index = Math.floor(position), next = Math.min(index + 1, input.length - 1), t = position - index;
    output[i] = (input[index] ?? 0) * (1 - t) + (input[next] ?? 0) * t;
  }
  return output;
}

/** Silence handling: dictation ends by itself after a pause once something was said, or after a longer quiet start. */
export const SILENCE = {level: 0.012, afterSpeechSeconds: 4, withoutSpeechSeconds: 12};
export type Quiet = {heardSpeech: boolean; lastLoudAt: number; startedAt: number};
export function rms(samples: Float32Array): number {
  let sum = 0;
  for (let i = 0; i < samples.length; i++) sum += samples[i]! * samples[i]!;
  return samples.length ? Math.sqrt(sum / samples.length) : 0;
}
/** Returns true when the capture should stop: silence has lasted long enough for the current situation. */
export function quietEnough(state: Quiet, level: number, now: number, silence = SILENCE): boolean {
  if (level >= silence.level) { state.heardSpeech = true; state.lastLoudAt = now; return false; }
  const quietFor = (now - (state.heardSpeech ? state.lastLoudAt : state.startedAt)) / 1000;
  return quietFor >= (state.heardSpeech ? silence.afterSpeechSeconds : silence.withoutSpeechSeconds);
}

/** Yields about one second of 16 kHz mono audio per chunk until the signal aborts or the room has gone quiet. */
export async function* microphone(stream: MediaStream, signal: AbortSignal, chunkSeconds = 1, onQuiet?: () => void): AsyncGenerator<Float32Array> {
  const AudioContextClass = window.AudioContext || (window as unknown as {webkitAudioContext?: typeof AudioContext}).webkitAudioContext;
  if (!AudioContextClass) throw new Error('Audio capture is unavailable in this browser');
  let context: AudioContext;
  try { context = new AudioContextClass({sampleRate: SAMPLE_RATE}); } catch { context = new AudioContextClass(); }
  // Autoplay policy may hand back a suspended context when the click that
  // started dictation is no longer the active gesture; capture needs it running.
  if (context.state !== 'running') { try { await context.resume(); } catch { /* reported by the empty transcript */ } }
  const source = context.createMediaStreamSource(stream);
  const processor = context.createScriptProcessor(4096, 1, 1);
  const queue: Float32Array[] = [];
  let pending: Float32Array[] = [], pendingLength = 0, wake: (() => void) | null = null, quiet = false;
  const target = SAMPLE_RATE * chunkSeconds;
  const state: Quiet = {heardSpeech: false, lastLoudAt: 0, startedAt: performance.now()};
  processor.onaudioprocess = event => {
    if (signal.aborted || quiet) return;
    const samples = resample(event.inputBuffer.getChannelData(0).slice(), context.sampleRate);
    // Web Audio does not clip; the engine requires finite samples in [-1, 1].
    for (let i = 0; i < samples.length; i++) { const v = samples[i]!; samples[i] = Number.isFinite(v) ? (v > 1 ? 1 : v < -1 ? -1 : v) : 0; }
    if (quietEnough(state, rms(samples), performance.now())) { quiet = true; onQuiet?.(); wake?.(); return; }
    pending.push(samples); pendingLength += samples.length;
    if (pendingLength >= target) {
      const chunk = new Float32Array(pendingLength); let offset = 0;
      for (const part of pending) { chunk.set(part, offset); offset += part.length; }
      pending = []; pendingLength = 0; queue.push(chunk); wake?.();
    }
  };
  source.connect(processor); processor.connect(context.destination);
  const stop = () => { wake?.(); };
  signal.addEventListener('abort', stop, {once: true});
  try {
    while (!signal.aborted && !quiet) {
      if (!queue.length) await new Promise<void>(resolve => { wake = resolve; });
      wake = null;
      while (queue.length) yield queue.shift()!;
    }
    if (pendingLength) { const chunk = new Float32Array(pendingLength); let offset = 0; for (const part of pending) { chunk.set(part, offset); offset += part.length; } yield chunk; }
  } finally {
    signal.removeEventListener('abort', stop);
    processor.disconnect(); source.disconnect();
    for (const track of stream.getTracks()) track.stop();
    void context.close();
  }
}
