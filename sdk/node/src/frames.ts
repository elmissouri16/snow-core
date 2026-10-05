import { TextDecoder } from 'node:util';
import { posix } from 'node:path';
import { SdkError } from './errors.js';

export interface RpcReady {
  readonly type: 'rpc_ready';
  readonly protocol_version: '1';
  readonly snow_version: string;
  readonly capabilities: readonly string[];
  readonly max_input_bytes: number;
}

function malformed(): never {
  throw new SdkError('malformed_frame', 'Malformed Snow RPC frame.');
}

/** Focused JSON scanner: key equality includes escapes; recursion is bounded. */
function rejectDuplicateKeys(text: string): void {
  let i = 0;
  const whitespace = () => { while (i < text.length && /[\x20\t\r\n]/.test(text[i]!)) i++; };
  const string = (): string => {
    if (text[i++] !== '"') return malformed();
    const start = i - 1;
    while (i < text.length) {
      const c = text[i++];
      if (c === '"') {
        try { return JSON.parse(text.slice(start, i)) as string; } catch { return malformed(); }
      }
      if (c === '\\') i++;
    }
    return malformed();
  };
  const value = (depth: number): void => {
    if (depth > 128) malformed();
    whitespace();
    if (text[i] === '{') {
      i++;
      whitespace();
      const names = new Set<string>();
      if (text[i] === '}') { i++; return; }
      for (;;) {
        whitespace();
        const key = string();
        if (names.has(key)) malformed();
        names.add(key);
        whitespace();
        if (text[i++] !== ':') malformed();
        value(depth + 1);
        whitespace();
        if (text[i] === '}') { i++; return; }
        if (text[i++] !== ',') malformed();
      }
    } else if (text[i] === '[') {
      i++;
      whitespace();
      if (text[i] === ']') { i++; return; }
      for (;;) {
        value(depth + 1);
        whitespace();
        if (text[i] === ']') { i++; return; }
        if (text[i++] !== ',') malformed();
      }
    } else if (text[i] === '"') {
      string();
    } else {
      const start = i;
      while (i < text.length && !/[\x20\t\r\n,}\]]/.test(text[i]!)) i++;
      if (i === start) malformed();
      // JSON.parse below checks primitive token grammar.
    }
  };
  value(0);
  whitespace();
  if (i !== text.length) malformed();
}

export function decodeFrame(bytes: Uint8Array): Record<string, unknown> {
  let text: string;
  try { text = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes); }
  catch { return malformed(); }
  rejectDuplicateKeys(text);
  let frame: unknown;
  try { frame = JSON.parse(text); } catch { return malformed(); }
  if (!frame || typeof frame !== 'object' || Array.isArray(frame)) malformed();
  const object = frame as Record<string, unknown>;
  if (typeof object.type !== 'string' || object.type.length === 0) malformed();
  return object;
}

/** LF-only byte framing. Bounds are checked before copying into the accumulator. */
export class FrameDecoder {
  private buffer = Buffer.alloc(0);
  private length = 0;

  constructor(private readonly maxBytes: number) {}

  write(chunk: Buffer, onFrame: (frame: Record<string, unknown>, bytes: number) => void): void {
    let start = 0;
    while (start < chunk.length) {
      const lf = chunk.indexOf(10, start);
      const end = lf < 0 ? chunk.length : lf;
      const part = chunk.subarray(start, end);
      if (part.length > this.maxBytes - this.length) {
        throw new SdkError('frame_too_large', 'Snow RPC frame exceeds its byte limit.');
      }
      if (lf >= 0 && this.length === 0) {
        if (part.length > 0) onFrame(decodeFrame(part), part.length);
      } else {
        const needed = this.length + part.length;
        if (needed > this.buffer.length) {
          const size = Math.min(this.maxBytes, Math.max(needed, 1024, this.buffer.length * 2));
          const next = Buffer.allocUnsafe(size);
          this.buffer.copy(next, 0, 0, this.length);
          this.buffer = next;
        }
        part.copy(this.buffer, this.length);
        this.length = needed;
        if (lf >= 0) {
          const size = this.length;
          this.length = 0;
          if (size > 0) onFrame(decodeFrame(this.buffer.subarray(0, size)), size);
        }
      }
      if (lf < 0) break;
      start = lf + 1;
    }
  }

  /** Internal cleanup; only reset a live transport after stdout draining ends. */
  reset(): void {
    this.buffer = Buffer.alloc(0);
    this.length = 0;
  }

  end(): void {
    const partial = this.length !== 0;
    this.reset();
    if (partial) malformed();
  }
}

const nonempty = (value: unknown): value is string => typeof value === 'string' && value.length > 0;
const integer = (value: unknown, min: number): value is number =>
  typeof value === 'number' && Number.isSafeInteger(value) && value >= min;
const identity = (value: unknown): value is string =>
  nonempty(value) && Buffer.byteLength(value) <= 256 && value.trim() === value && !/[\0\r\n\t]/.test(value);

export function validateReady(frame: Record<string, unknown>): RpcReady {
  if (frame.type !== 'rpc_ready') malformed();
  if (frame.protocol_version !== '1') {
    throw new SdkError('protocol_version', 'Unsupported Snow RPC protocol version.');
  }
  if (typeof frame.snow_version !== 'string' || !Array.isArray(frame.capabilities) ||
      !frame.capabilities.every((item) => typeof item === 'string') || !integer(frame.max_input_bytes, 1)) malformed();
  return Object.freeze({
    type: 'rpc_ready', protocol_version: '1', snow_version: frame.snow_version,
    capabilities: Object.freeze([...frame.capabilities] as string[]), max_input_bytes: frame.max_input_bytes,
  });
}

/** Additive unknown fields/types are preserved, but known envelopes remain valid. */
export function validateNotification(frame: Record<string, unknown>): void {
  switch (frame.type) {
    case 'rpc_ready': malformed();
    case 'response':
      if (!nonempty(frame.id) || !nonempty(frame.command) || typeof frame.success !== 'boolean') malformed();
      return;
    case 'prompt_completed':
      if (!nonempty(frame.request_id) || !['completed', 'failed', 'canceled'].includes(frame.status as string)) malformed();
      return;
    case 'goal_run_completed':
      if (![frame.request_id, frame.goal_run_id, frame.goal_id].every(nonempty) ||
          !['finished', 'failed', 'canceled'].includes(frame.status as string)) malformed();
      return;
    case 'compaction_completed':
      if (![frame.request_id, frame.compaction_id, frame.session_id, frame.branch_id, frame.turn_id].every(identity) ||
          frame.compaction_id !== frame.turn_id || frame.turn_origin !== 'compact' ||
          !integer(frame.root_epoch, 1) || !integer(frame.turn_sequence, 1) ||
          !integer(frame.summarized_messages, 0) || !integer(frame.retained_messages, 0) ||
          typeof frame.used_fallback !== 'boolean' ||
          !['completed', 'noop', 'fallback', 'canceled', 'failed'].includes(frame.status as string)) malformed();
      return;
    case 'project_completed': {
      const child = frame.child;
      if (!identity(frame.request_id) || !identity(frame.operation_id) || !child ||
          typeof child !== 'object' || Array.isArray(child)) malformed();
      const c = child as Record<string, unknown>;
      if (typeof c.path !== 'string' || Buffer.byteLength(c.path) > 4096 || !c.path.startsWith('/') ||
          posix.normalize(c.path) !== c.path || c.path.includes('\0') ||
          ![c.device, c.inode].every((n) => typeof n === 'string' && n.length <= 20 && /^(0|[1-9][0-9]*)$/.test(n) && BigInt(n) <= 18446744073709551615n) ||
          !['succeeded', 'failed', 'canceled', 'timed_out', 'output_limit', 'cleanup_failed'].includes(frame.status as string)) malformed();
      return;
    }
  }
}
