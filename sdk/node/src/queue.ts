import { SdkError } from './errors.js';

/** Single-consumer queue bounded by both wire bytes and frame count. */
export class EventQueue {
  private readonly items: { frame: Record<string, unknown>; bytes: number }[] = [];
  private bytes = 0;
  private claimed = false;
  private ended = false;
  private error: SdkError | undefined;
  private wake: (() => void) | undefined;

  constructor(private readonly maxCount: number, private readonly maxBytes: number) {}

  push(frame: Record<string, unknown>, bytes: number): void {
    if (this.ended) return;
    if (this.items.length >= this.maxCount || bytes > this.maxBytes - this.bytes) {
      throw new SdkError('event_overflow', 'Snow event buffer overflow.');
    }
    this.items.push({ frame, bytes });
    this.bytes += bytes;
    this.wake?.();
    this.wake = undefined;
  }

  finish(error?: SdkError): void {
    if (this.ended) return;
    this.ended = true;
    this.error = error;
    this.wake?.();
    this.wake = undefined;
  }

  events(): AsyncIterable<Record<string, unknown>> {
    if (this.claimed) throw new SdkError('event_consumer', 'Snow events have one consumer.');
    this.claimed = true;
    const queue = this;
    return (async function* () {
      for (;;) {
        const item = queue.items.shift();
        if (item) {
          queue.bytes -= item.bytes;
          yield item.frame;
        } else if (queue.ended) {
          if (queue.error) throw queue.error;
          return;
        } else {
          await new Promise<void>((resolve) => { queue.wake = resolve; });
        }
      }
    })();
  }
}
