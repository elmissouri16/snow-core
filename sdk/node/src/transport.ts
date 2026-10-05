import { spawn, type ChildProcess } from 'node:child_process';
import type { Readable, Writable } from 'node:stream';
import { SdkError } from './errors.js';
import { FrameDecoder, validateNotification, validateReady, type RpcReady } from './frames.js';
import { EventQueue } from './queue.js';

export interface TransportOptions {
  executable: string;
  args: string[];
  cwd?: string;
  /** Replaces the inherited environment, including when {} is supplied. */
  env?: NodeJS.ProcessEnv;
  /** Governs the entire owned worker lifetime, not just startup. */
  signal?: AbortSignal;
  maxFrameBytes?: number;
  eventBuffer?: number;
  maxEventBytes?: number;
  maxPending?: number;
  /** Aggregate queued/in-flight request buffer bytes, including LF. */
  maxPendingBytes?: number;
  handshakeTimeoutMs?: number;
  writeTimeoutMs?: number;
  responseTimeoutMs?: number;
  shutdownTimeoutMs?: number;
}

export interface CallOptions {
  signal?: AbortSignal;
  timeoutMs?: number;
  /** Internal admission guard, checked after the write gate, immediately before send. */
  guard?: () => boolean;
}

type Limits = Required<Pick<TransportOptions, 'maxFrameBytes' | 'eventBuffer' | 'maxEventBytes' |
  'maxPending' | 'maxPendingBytes' | 'handshakeTimeoutMs' | 'writeTimeoutMs' | 'responseTimeoutMs' | 'shutdownTimeoutMs'>>;

type Pending = {
  id: string;
  command: string;
  payload: Buffer | undefined;
  options: CallOptions;
  beforeSend: ((id: string) => void) | undefined;
  started: boolean;
  writing: boolean;
  written: boolean;
  response?: Record<string, unknown>;
  resolve: (frame: Record<string, unknown>) => void;
  reject: (error: SdkError) => void;
  timer?: ReturnType<typeof setTimeout>;
  abort?: () => void;
};

const MAX_TIMER = 2_147_483_647;
function bounded(value: unknown, fallback: number, max: number): number {
  if (value === undefined) return fallback;
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 1 || value > max) {
    throw new SdkError('invalid_options', 'Invalid Snow transport resource bounds.');
  }
  return value;
}
function validSignal(signal: unknown): signal is AbortSignal {
  return !!signal && typeof signal === 'object' && typeof (signal as AbortSignal).aborted === 'boolean' &&
    typeof (signal as AbortSignal).addEventListener === 'function' && typeof (signal as AbortSignal).removeEventListener === 'function';
}
function validateOptions(options: TransportOptions): Limits {
  if (!options || typeof options.executable !== 'string' || options.executable.length === 0 ||
      options.executable.includes('\0') || !Array.isArray(options.args) ||
      ![...options.args].every((arg) => typeof arg === 'string' && !arg.includes('\0')) ||
      (options.cwd !== undefined && (typeof options.cwd !== 'string' || options.cwd.includes('\0'))) ||
      (options.signal !== undefined && !validSignal(options.signal))) {
    throw new SdkError('invalid_options', 'Invalid Snow subprocess options.');
  }
  if (options.env !== undefined) {
    if (!options.env || typeof options.env !== 'object' || Array.isArray(options.env) ||
        !Object.entries(options.env).every(([key, value]) => key.length > 0 && !/[=\0]/.test(key) &&
          (value === undefined || (typeof value === 'string' && !value.includes('\0'))))) {
      throw new SdkError('invalid_options', 'Invalid Snow subprocess environment.');
    }
  }
  return {
    maxFrameBytes: bounded(options.maxFrameBytes, 16 * 1024 * 1024, 64 * 1024 * 1024),
    eventBuffer: bounded(options.eventBuffer, 64, 4096),
    maxEventBytes: bounded(options.maxEventBytes, 64 * 1024 * 1024, 256 * 1024 * 1024),
    maxPending: bounded(options.maxPending, 64, 4096),
    maxPendingBytes: bounded(options.maxPendingBytes, 64 * 1024 * 1024, 256 * 1024 * 1024),
    handshakeTimeoutMs: bounded(options.handshakeTimeoutMs, 10_000, MAX_TIMER),
    writeTimeoutMs: bounded(options.writeTimeoutMs, 10_000, MAX_TIMER),
    responseTimeoutMs: bounded(options.responseTimeoutMs, 30_000, MAX_TIMER),
    shutdownTimeoutMs: bounded(options.shutdownTimeoutMs, 1000, MAX_TIMER),
  };
}

/** Owns one direct subprocess. No shell, retry/restart, stderr capture or logging. */
export class RpcTransport {
  private readonly child: ChildProcess;
  private readonly input: Writable;
  private readonly output: Readable;
  private readonly frames: FrameDecoder;
  private readonly queue: EventQueue;
  private readonly pending = new Map<string, Pending>();
  private readonly writes: Pending[] = [];
  private pendingPayloadBytes = 0;
  private pumping = false;
  private nextId = 0n;
  private readiness: RpcReady | undefined;
  private terminal: SdkError | undefined;
  private stopping = false;
  private explicit = false;
  private exited = false;
  private outputEnded = false;
  private cleanup: Promise<void> | undefined;
  private readonly exitedPromise: Promise<void>;
  private readonly outputPromise: Promise<void>;
  private exitResolve!: () => void;
  private outputResolve!: () => void;
  private readonly handshake: Promise<void>;
  private readyResolve!: () => void;
  private readyReject!: (error: SdkError) => void;
  private handshakeTimer: ReturnType<typeof setTimeout> | undefined;
  private readonly notifications = new Set<(frame: Record<string, unknown>) => void>();
  private readonly terminals = new Set<(error: SdkError) => void>();
  private readonly lifetimeSignal: AbortSignal | undefined;
  private readonly lifetimeAbort = () => this.fail(new SdkError('aborted', 'Snow worker lifetime aborted.'));
  private cancelWrite: ((error: SdkError) => void) | undefined;

  private constructor(options: TransportOptions, private readonly limits: Limits) {
    this.lifetimeSignal = options.signal;
    this.frames = new FrameDecoder(limits.maxFrameBytes);
    this.queue = new EventQueue(limits.eventBuffer, limits.maxEventBytes);
    this.exitedPromise = new Promise((resolve) => { this.exitResolve = resolve; });
    this.outputPromise = new Promise((resolve) => { this.outputResolve = resolve; });
    this.handshake = new Promise((resolve, reject) => { this.readyResolve = resolve; this.readyReject = reject; });
    this.child = spawn(options.executable, [...options.args], {
      shell: false, stdio: ['pipe', 'pipe', 'ignore'],
      ...(options.cwd === undefined ? {} : { cwd: options.cwd }),
      ...(options.env === undefined ? {} : { env: { ...options.env } }),
    });
    this.input = this.child.stdin!;
    this.output = this.child.stdout!;
    this.child.on('error', () => {
      // A failed spawn has no exit event. Do not retain the raw OS diagnostic.
      if (this.child.pid === undefined) { this.exited = true; this.exitResolve(); }
      this.fail(new SdkError('spawn_failed', 'Unable to start Snow subprocess.'));
    });
    this.child.once('exit', () => {
      this.exited = true;
      this.exitResolve();
      // Exit is not EOF: preserve final stdout bytes before declaring termination.
      this.beginShutdown();
    });
    this.input.on('error', () => {
      if (!this.stopping) this.fail(new SdkError('io_error', 'Snow request stream failed.'));
    });
    this.output.on('error', () => {
      if (!this.outputEnded) this.fail(new SdkError('io_error', 'Snow output stream failed.'));
      this.endOutput();
    });
    this.output.on('data', (chunk: Buffer) => {
      if (this.terminal) return;
      try { this.frames.write(chunk, (frame, bytes) => this.dispatch(frame, bytes)); }
      catch (error) { this.fail(error instanceof SdkError ? error : new SdkError('malformed_frame', 'Malformed Snow RPC frame.')); }
    });
    this.output.once('end', () => {
      if (!this.terminal) {
        try { this.frames.end(); } catch (error) { this.fail(error as SdkError); }
      }
      this.endOutput();
      if (!this.explicit && !this.terminal) this.fail(new SdkError('process_exit', 'Snow subprocess output ended.'));
      this.beginShutdown();
    });
    this.output.once('close', () => { this.endOutput(); });
    this.handshakeTimer = setTimeout(() => {
      this.fail(new SdkError('handshake_timeout', 'Snow RPC readiness timed out.'));
    }, limits.handshakeTimeoutMs);
    options.signal?.addEventListener('abort', this.lifetimeAbort, { once: true });
    if (options.signal?.aborted) this.lifetimeAbort();
  }

  static async start(options: TransportOptions): Promise<RpcTransport> {
    const limits = validateOptions(options);
    if (options.signal?.aborted) throw new SdkError('aborted', 'Snow worker lifetime aborted.');
    let transport: RpcTransport;
    try { transport = new RpcTransport(options, limits); }
    catch { throw new SdkError('spawn_failed', 'Unable to start Snow subprocess.'); }
    try {
      await transport.handshake;
      if (transport.terminal) throw transport.terminal;
      return transport;
    } catch (error) {
      await transport.close();
      throw error;
    }
  }

  get ready(): RpcReady {
    if (!this.readiness) throw new SdkError('not_ready', 'Snow RPC is not ready.');
    return Object.freeze({ ...this.readiness, capabilities: Object.freeze([...this.readiness.capabilities]) });
  }
  get terminalError(): SdkError | undefined { return this.terminal; }
  get closed(): boolean { return this.stopping; }
  events(): AsyncIterable<Record<string, unknown>> { return this.queue.events(); }

  onNotification(handler: (frame: Record<string, unknown>) => void): () => void {
    this.notifications.add(handler);
    return () => { this.notifications.delete(handler); };
  }
  onTerminal(handler: (error: SdkError) => void): () => void {
    if (this.terminal) handler(this.terminal);
    else this.terminals.add(handler);
    return () => { this.terminals.delete(handler); };
  }

  call(request: Record<string, unknown>, options: CallOptions = {}, beforeSend?: (id: string) => void): Promise<Record<string, unknown>> {
    if (this.stopping) return Promise.reject(this.terminal ?? new SdkError('closed', 'Snow transport is closed.'));
    if (!request || typeof request !== 'object' || Array.isArray(request) || typeof request.type !== 'string' || request.type.length === 0) {
      return Promise.reject(new SdkError('invalid_request', 'Invalid Snow RPC request.'));
    }
    if (!options || (options.signal !== undefined && !validSignal(options.signal)) ||
        (options.guard !== undefined && typeof options.guard !== 'function') ||
        (beforeSend !== undefined && typeof beforeSend !== 'function')) {
      return Promise.reject(new SdkError('invalid_options', 'Invalid Snow RPC call options.'));
    }
    let timeout: number;
    try { timeout = bounded(options.timeoutMs, this.limits.responseTimeoutMs, MAX_TIMER); }
    catch (error) { return Promise.reject(error); }
    if (options.signal?.aborted) return Promise.reject(new SdkError('aborted', 'Snow RPC call aborted.'));
    if (this.pending.size >= this.limits.maxPending) {
      return Promise.reject(new SdkError('too_many_pending', 'Too many pending Snow RPC calls.'));
    }
    const command = request.type;
    const id = (++this.nextId).toString();
    let payload: Buffer;
    try {
      const encoded = JSON.stringify({ ...request, id });
      // Check the actual serialized envelope (toJSON must not bypass correlation).
      const bytes = Buffer.byteLength(encoded, 'utf8');
      if (bytes > Math.min(this.limits.maxFrameBytes, this.readiness!.max_input_bytes)) {
        return Promise.reject(new SdkError('frame_too_large', 'Snow RPC request exceeds its byte limit.'));
      }
      // Bound aggregate SDK buffers before parsing another envelope or allocating.
      if (bytes + 1 > this.limits.maxPendingBytes - this.pendingPayloadBytes) {
        return Promise.reject(new SdkError('pending_bytes_exceeded', 'Snow pending request byte budget exceeded.'));
      }
      const envelope = JSON.parse(encoded) as Record<string, unknown>;
      if (!envelope || envelope.id !== id || envelope.type !== command) throw new Error();
      payload = Buffer.from(encoded + '\n', 'utf8');
    } catch { return Promise.reject(new SdkError('invalid_request', 'Snow RPC request could not be encoded.')); }
    this.pendingPayloadBytes += payload.length;
    let p!: Pending;
    const promise = new Promise<Record<string, unknown>>((resolve, reject) => {
      p = { id, command, payload, options, beforeSend, started: false,
        writing: false, written: false, resolve, reject };
    });
    this.pending.set(id, p);
    const stopWait = (code: string, message: string) => {
      if (!this.pending.has(p.id)) return;
      const error = new SdkError(code, message, { outcomeUnknown: p.started });
      if (p.writing) this.fail(error); // An interrupted partial frame poisons framing.
      else this.settle(p, undefined, error); // No implicit remote abort or retry.
    };
    p.timer = setTimeout(() => stopWait('response_timeout', 'Snow RPC response timed out.'), timeout);
    p.abort = () => stopWait('aborted', 'Snow RPC call aborted.');
    options.signal?.addEventListener('abort', p.abort, { once: true });
    this.writes.push(p);
    void this.pump();
    return promise;
  }

  private dispatch(frame: Record<string, unknown>, bytes: number): void {
    if (this.terminal) return;
    if (!this.readiness) {
      this.readiness = validateReady(frame);
      clearTimeout(this.handshakeTimer);
      this.handshakeTimer = undefined;
      this.readyResolve();
      return;
    }
    validateNotification(frame);
    if (frame.type === 'response') {
      const p = this.pending.get(frame.id as string);
      if (!p || p.response) return; // First correlated response wins, including legacy duplicates.
      if (p.command !== frame.command) throw new SdkError('malformed_frame', 'Snow RPC response command mismatch.');
      p.response = frame;
      if (p.written) this.settle(p, frame);
      return;
    }
    for (const handler of [...this.notifications]) {
      try { handler(frame); }
      catch { throw new SdkError('notification_handler', 'Snow notification handler failed.'); }
    }
    this.queue.push(frame, bytes);
  }

  private settle(p: Pending, frame?: Record<string, unknown>, error?: SdkError): void {
    if (!this.pending.delete(p.id)) return;
    clearTimeout(p.timer);
    if (p.abort) p.options.signal?.removeEventListener('abort', p.abort);
    const queued = this.writes.indexOf(p);
    if (queued >= 0) this.writes.splice(queued, 1);
    if (!p.writing) this.releasePayload(p);
    if (error) p.reject(error);
    else p.resolve(frame!);
  }

  private releasePayload(p: Pending): void {
    if (!p.payload) return;
    this.pendingPayloadBytes -= p.payload.length;
    p.payload = undefined;
  }

  private async pump(): Promise<void> {
    if (this.pumping) return;
    this.pumping = true;
    try {
      while (this.writes.length > 0 && !this.stopping) {
        const p = this.writes.shift()!;
        if (!this.pending.has(p.id)) continue;
        if (p.options.signal?.aborted) {
          this.settle(p, undefined, new SdkError('aborted', 'Snow RPC call aborted.'));
          continue;
        }
        try {
          if (p.options.guard && !p.options.guard()) {
            this.settle(p, undefined, new SdkError('run_finished', 'Snow prompt run has already finished.'));
            continue;
          }
          p.beforeSend?.(p.id); // Synchronous, after the gate and before any bytes.
        } catch {
          this.settle(p, undefined, new SdkError('invalid_request', 'Snow request admission callback failed.'));
          continue;
        }
        if (!this.pending.has(p.id) || this.stopping) continue;
        p.started = true;
        p.writing = true;
        try { await this.write(p.payload!); }
        catch (error) { this.fail(error as SdkError); break; }
        finally {
          p.writing = false;
          // Terminal settlement may already have removed this call. The writer
          // owns its buffer until this finally, so release it exactly once here.
          this.releasePayload(p);
        }
        p.written = true;
        if (p.response) this.settle(p, p.response);
      }
    } finally { this.pumping = false; }
  }

  private write(payload: Buffer): Promise<void> {
    return new Promise((resolve, reject) => {
      let finished = false;
      let callbackDone = false;
      let drained = false;
      const finish = (error?: SdkError) => {
        if (finished) return;
        if (!error && (!callbackDone || !drained)) return;
        finished = true;
        clearTimeout(timer);
        this.input.removeListener('drain', onDrain);
        this.cancelWrite = undefined;
        if (error) reject(error); else resolve();
      };
      const onDrain = () => { drained = true; finish(); };
      const timer = setTimeout(() => finish(new SdkError('write_timeout', 'Snow RPC request write timed out.', { outcomeUnknown: true })), this.limits.writeTimeoutMs);
      this.cancelWrite = (error) => finish(error);
      this.input.once('drain', onDrain);
      try {
        drained = this.input.write(payload, (error) => {
          callbackDone = true;
          finish(error ? new SdkError('io_error', 'Snow RPC request write failed.', { outcomeUnknown: true }) : undefined);
        });
        finish();
      } catch { finish(new SdkError('io_error', 'Snow RPC request write failed.', { outcomeUnknown: true })); }
    });
  }

  private fail(error: SdkError): void {
    if (this.terminal) return;
    this.setTerminal(error);
    this.queue.finish(error);
    this.beginShutdown();
  }

  private setTerminal(error: SdkError): void {
    if (this.terminal) return;
    this.terminal = error;
    clearTimeout(this.handshakeTimer);
    this.handshakeTimer = undefined;
    this.readyReject(error);
    this.cancelWrite?.(error);
    for (const p of [...this.pending.values()]) {
      this.settle(p, undefined, new SdkError(error.code, error.message, { outcomeUnknown: p.started }));
    }
    for (const handler of [...this.terminals]) {
      try { handler(error); } catch { /* Never let an observer prevent owned-child cleanup. */ }
    }
    this.terminals.clear();
  }

  private endOutput(): void {
    if (this.outputEnded) return;
    this.outputEnded = true;
    this.outputResolve();
  }

  private async waitBounded(promise: Promise<void>, ms: number): Promise<boolean> {
    let timer: ReturnType<typeof setTimeout> | undefined;
    try {
      return await Promise.race([
        promise.then(() => true),
        new Promise<boolean>((resolve) => { timer = setTimeout(() => resolve(false), ms); }),
      ]);
    } finally { clearTimeout(timer); }
  }

  private beginShutdown(): void {
    if (this.cleanup) return;
    this.stopping = true;
    // EOF flushes a write already in progress. New/queued writes cannot start.
    this.input.end();
    this.cleanup = (async () => {
      if (!this.exited && !await this.waitBounded(this.exitedPromise, this.limits.shutdownTimeoutMs)) {
        this.child.kill('SIGKILL');
      }
      await this.exitedPromise; // Always reap the direct child, not descendants.
      if (!this.outputEnded && !await this.waitBounded(this.outputPromise, this.limits.shutdownTimeoutMs)) {
        this.output.destroy(); // Descendants retaining stdout cannot block cleanup.
        this.endOutput();
      }
      if (!this.terminal) {
        if (this.explicit) {
          this.setTerminal(new SdkError('closed', 'Snow transport is closed.'));
          this.queue.finish();
        } else {
          this.fail(new SdkError('process_exit', 'Snow subprocess exited.'));
        }
      }
      this.input.destroy();
      this.output.destroy();
      this.frames.reset();
      this.lifetimeSignal?.removeEventListener('abort', this.lifetimeAbort);
      this.notifications.clear();
    })();
  }

  /** EOF, bounded grace, SIGKILL if necessary, reap, then bounded stdout drain. */
  close(): Promise<void> {
    if (!this.cleanup) { this.explicit = true; this.beginShutdown(); }
    return this.cleanup!;
  }
}
