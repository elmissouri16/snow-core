/**
 * Typed per-call context handed to Snow tool handlers.
 */
import { MAX_FRAME_BYTES } from "./protocol.js";

export const MAX_PROGRESS_BYTES = 16 * 1024;

export class ToolContext {
  constructor({ callId, requestId, cwd, sessionId, signal, deadline, config, write, pluginState }) {
    this.callId = callId;
    this.requestId = requestId;
    this.cwd = cwd;
    this.sessionId = sessionId;
    this.signal = signal;
    this.deadline = deadline; // Date or null
    this.config = config;
    this.state = pluginState;
    this._write = write;
  }

  _assertActive() {
    if (typeof this._write !== "function") throw new Error("context used outside an active Snow tool call");
  }

  /**
   * Report bounded progress for this call. Snow ignores progress with an empty
   * call ID; the SDK rejects it before writing a frame.
   */
  async progress(message, { done = false, is_error = false } = {}) {
    this._assertActive();
    if (typeof message !== "string" || message.trim().length === 0) {
      throw new TypeError("progress requires a non-empty string message");
    }
    if (Buffer.byteLength(message, "utf8") > MAX_PROGRESS_BYTES) {
      throw new RangeError(`progress message exceeds the ${MAX_PROGRESS_BYTES}-byte bound`);
    }
    if (!this.callId) throw new Error("progress requires a non-empty call_id");
    await this._write({
      jsonrpc: "2.0",
      method: "notifications/progress",
      params: { call_id: this.callId, message, done: Boolean(done), is_error: Boolean(is_error) },
    });
  }

  /** Report a bounded protocol diagnostic to Snow's bounded log. */
  async log(severity, message) {
    this._assertActive();
    if (!["debug", "info", "warning", "error"].includes(severity)) {
      throw new TypeError("log severity must be debug, info, warning, or error");
    }
    if (typeof message !== "string" || message.trim().length === 0) {
      throw new TypeError("log requires a non-empty string message");
    }
    if (Buffer.byteLength(message, "utf8") > MAX_PROGRESS_BYTES) {
      throw new RangeError(`log message exceeds the ${MAX_PROGRESS_BYTES}-byte bound`);
    }
    await this._write({
      jsonrpc: "2.0",
      method: "notifications/log",
      params: { severity, message },
    });
  }
}

/** Bound applied to internal state objects and frame-size sanity checks. */
export { MAX_FRAME_BYTES };
