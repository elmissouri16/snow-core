/**
 * JSON-RPC 2.0 runtime for the Snow protocol-v2 plugin subprocess.
 *
 * Owns framing, dispatch, progress/log writes, cancellation, deadlines,
 * concurrency bounds, event delivery, graceful shutdown, and stdout discipline.
 * User handlers must never write to stdout directly; route diagnostics to
 * stderr.
 */
import * as readline from "node:readline";
import { Buffer } from "node:buffer";
import { format } from "node:util";

import { ToolContext } from "./context.js";
import { ToolError } from "./errors.js";
import { errorResult, normalizeResult, toolDescriptor } from "./plugin.js";
import { encodeFrame, validateRequest } from "./protocol.js";

const DEFAULT_MAX_CONCURRENCY = 8;
const DEFAULT_MAX_EVENT_QUEUE = 64;
const MAX_ERROR_TEXT_BYTES = 4096;

function serialize(payload) {
  return encodeFrame(payload);
}

function redirectConsoleToStderr() {
  const methods = ["debug", "info", "log", "warn", "error"];
  const originals = new Map(methods.map((method) => [method, console[method]]));
  for (const method of methods) {
    console[method] = (...args) => { process.stderr.write(`${format(...args)}\n`); };
  }
  return () => {
    for (const [method, original] of originals) console[method] = original;
  };
}

/** Serialized, backpressure-aware protocol stdout writer. */
class Writer {
  constructor(stream) {
    this.stream = stream;
    this.chain = Promise.resolve();
  }

  write(payload) {
    const line = serialize(payload);
    this.chain = this.chain.then(
      () =>
        new Promise((resolve, reject) => {
          const ok = this.stream.write(line, (error) => (error ? reject(error) : resolve()));
          if (!ok) {
            this.stream.once("drain", resolve);
          }
        }),
    );
    return this.chain;
  }
}

class EventDispatcher {
  constructor(plugin, { maxQueue, log }) {
    this.plugin = plugin;
    this.queue = [];
    this.maxQueue = maxQueue;
    this._log = log;
    this.droppedReported = false;
  }

  /** Enqueue a subscribed event; drops (bounded, rate-limited) on overflow. */
  dispatch(type, params) {
    const handler = this.plugin.events[type];
    if (typeof handler !== "function") return;
    if (this.queue.length >= this.maxQueue) {
      if (!this.droppedReported) {
        this.droppedReported = true;
        void this.log("warning", "event queue overflow; dropping best-effort events");
      }
      return;
    }
    this.queue.push({ type, params });
    this._start();
  }

  _start() {
    queueMicrotask(() => {
      if (this._running) return;
      this._running = true;
      void this._drain();
    });
  }

  async _drain() {
    try {
      while (this.queue.length > 0) {
        const { type, params } = this.queue.shift();
        try {
          await this.plugin.events[type](params, { state: this.plugin._state });
        } catch {
          // Observation-only handlers must never break the loop.
        }
      }
    } finally {
      this._running = false;
      if (this.queue.length > 0) this._start();
    }
  }

  async log(severity, message) {
    try {
      await this._log(severity, message);
    } catch {
      // Best-effort diagnostic; writer failures should not propagate.
    }
  }
}

const Service = {
  CONCURRENCY_LIMIT: DEFAULT_MAX_CONCURRENCY,
  MAX_RESULT_BYTES: 256 * 1024,
};

function bounded(resultValue) {
  const size = Buffer.byteLength(JSON.stringify(resultValue ?? {}), "utf8");
  if (size > Service.MAX_RESULT_BYTES) {
    return errorResult("tool result exceeds the configured output limit");
  }
  return resultValue;
}

export async function serve(plugin, { maxConcurrency = DEFAULT_MAX_CONCURRENCY, maxEventQueue = DEFAULT_MAX_EVENT_QUEUE } = {}) {
  if (typeof plugin !== "object" || plugin === null) throw new TypeError("plugin must be a definePlugin result");
  if (plugin.tools.length === 0) throw new TypeError("plugin must declare at least one tool");
  if (!Number.isInteger(maxConcurrency) || maxConcurrency <= 0) throw new RangeError("maxConcurrency must be a positive integer");
  if (!Number.isInteger(maxEventQueue) || maxEventQueue <= 0) throw new RangeError("maxEventQueue must be a positive integer");

  const restoreConsole = redirectConsoleToStderr();
  const writer = new Writer(process.stdout);
  const activeRequests = new Map();
  const activeCalls = new Map();
  const controllers = [];
  let concurrency = 0;
  let closing = false;
  let shutdownResponded = false;
  const dispatcher = new EventDispatcher(plugin, {
    maxQueue: maxEventQueue,
    log: (severity, message) => writer.write({
      jsonrpc: "2.0",
      method: "notifications/log",
      params: { severity, message },
    }),
  });

  async function respond(id, resultValue) {
    if (closing && id === undefined) return;
    await writer.write({ jsonrpc: "2.0", id, result: bounded(resultValue) });
  }
  async function respondError(id, code, message) {
    if (closing && id === undefined) return;
    await writer.write({ jsonrpc: "2.0", id, error: { code, message: String(message).slice(0, MAX_ERROR_TEXT_BYTES) } });
  }
  async function notify(method, params) {
    await writer.write({ jsonrpc: "2.0", method, params });
  }

  async function handleInitialize(id, params = {}) {
    const protocolVersion = params.protocol_version;
    if (protocolVersion !== 2) {
      await respondError(id, -32600, `unsupported protocol version ${JSON.stringify(protocolVersion)}; this runtime speaks 2`);
      return;
    }
    const manifest = { ...plugin.manifest };
    manifest.capabilities = manifest.capabilities ?? [];
    const supportedEvents = Object.keys(plugin.events ?? {});
    const context = {
      cwd: typeof params.cwd === "string" ? params.cwd : process.cwd(),
      sessionId: typeof params.session_id === "string" ? params.session_id : "",
      hostVersion: typeof params.host_version === "string" ? params.host_version : "",
      hostCapabilities: Array.isArray(params.host_capabilities) ? [...params.host_capabilities] : [],
      config: params.config,
    };
    plugin._config = params.config;
    if (typeof plugin.setup === "function") {
      try {
        plugin._state = await plugin.setup(context);
      } catch (error) {
        await respondError(id, -32000, `plugin setup failed: ${error?.constructor?.name ?? "Error"}`);
        return;
      }
    }
    plugin._setupDone = true;
    await respond(id, {
      manifest,
      capabilities: [...manifest.capabilities],
      supported_events: supportedEvents,
      limits: { max_active_calls: maxConcurrency, max_result_bytes: Service.MAX_RESULT_BYTES },
    });
  }

  async function handleToolsCall(id, params = {}) {
    const { name, call_id: callId, arguments: args } = params;
    if (typeof name !== "string" || name.length === 0) {
      await respondError(id, -32602, "tools/call requires a string name");
      return;
    }
    if (typeof callId !== "string" || callId.length === 0) {
      await respondError(id, -32602, "tools/call requires a non-empty call_id");
      return;
    }
    if (args !== undefined && (typeof args !== "object" || args === null || Array.isArray(args))) {
      await respondError(id, -32602, "tools/call arguments must be an object");
      return;
    }
    const tool = plugin.tools.find((candidate) => candidate.name === name);
    if (!tool) {
      await respondError(id, -32602, `unknown tool ${name}`);
      return;
    }
    if (closing) {
      await respondError(id, -32000, "plugin is shutting down");
      return;
    }
    const requestKey = String(id);
    if (activeRequests.has(requestKey) || activeCalls.has(callId)) {
      await respondError(id, -32602, "duplicate request or call id");
      return;
    }
    if (concurrency >= maxConcurrency) {
      await respondError(id, -32000, "concurrency limit reached");
      return;
    }
    concurrency += 1;

    const controller = new AbortController();
    const timeoutMs = Number.isFinite(params.timeout_ms) && params.timeout_ms > 0 ? params.timeout_ms : 0;
    const timeoutHandle = timeoutMs > 0 ? setTimeout(() => controller.abort(), timeoutMs) : null;
    let settleCall;
    const done = new Promise((resolve) => { settleCall = resolve; });
    const entry = { controller, requestId: requestKey, callId, done };
    activeRequests.set(requestKey, entry);
    activeCalls.set(callId, entry);

    const context = new ToolContext({
      callId,
      requestId: String(id),
      cwd: typeof params.cwd === "string" ? params.cwd : process.cwd(),
      sessionId: typeof params.session_id === "string" ? params.session_id : "",
      signal: controller.signal,
      deadline: timeoutMs > 0 ? new Date(Date.now() + timeoutMs) : null,
      config: plugin._config,
      write: (payload) => writer.write(payload),
      pluginState: plugin._state,
    });

    try {
      const resultValue = await tool.execute(args ?? {}, context);
      context._write = null;
      if (controller.signal.aborted) return;
      await respond(id, normalizeResult(resultValue));
    } catch (error) {
      context._write = null;
      if (error instanceof ToolError) {
        await respond(id, errorResult(error.message, { details: { is_error: true } }));
      } else if (error?.name === "AbortError" || controller.signal.aborted) {
        // Snow has stopped waiting; a late response is ignored by the host.
        return;
      } else {
        await respondError(id, -32000, `tool failed: ${error?.constructor?.name ?? "Error"}`);
      }
    } finally {
      concurrency -= 1;
      if (timeoutHandle) clearTimeout(timeoutHandle);
      activeRequests.delete(requestKey);
      activeCalls.delete(callId);
      settleCall();
    }
  }

  async function handleShutdown(id) {
    closing = true;
    const active = [...new Set(activeRequests.values())];
    for (const entry of active) entry.controller.abort();
    if (active.length > 0) {
      await Promise.race([
        Promise.allSettled(active.map((entry) => entry.done)),
        new Promise((resolve) => setTimeout(resolve, 1000)),
      ]);
    }
    if (typeof plugin.shutdown === "function" && plugin._setupDone) {
      try {
        await Promise.race([
          Promise.resolve(plugin.shutdown({ state: plugin._state })),
          new Promise((resolve) => setTimeout(resolve, 1000)),
        ]);
      } catch {
        // Best effort; never fail the shutdown response.
      }
    }
    await respond(id, {});
    shutdownResponded = true;
    input.close();
  }

  const input = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
  const dispatch = async (line) => {
    if (closing || line.trim().length === 0) return;
    let message;
    try {
      message = JSON.parse(line);
    } catch {
      await respondError(null, -32700, "parse error");
      return;
    }
    const validation = validateRequest(message);
    if (validation) {
      await respondError(message.id ?? null, -32600, validation);
      return;
    }
    const id = message.id ?? null;
    if (message.method === "notifications/cancelled") {
      const requestId = String(message.params?.request_id ?? "");
      const callId = String(message.params?.call_id ?? "");
      const active = activeRequests.get(requestId) ?? activeCalls.get(callId);
      if (active) active.controller.abort();
      return;
    }
    if (message.method === "notifications/event") {
      const params = message.params ?? {};
      if (typeof params.type === "string") dispatcher.dispatch(params.type, params);
      return;
    }
    if (id === null) return; // other notifications receive no response
    switch (message.method) {
      case "initialize":
        await handleInitialize(id, message.params ?? {});
        break;
      case "tools/list":
        await respond(id, { tools: plugin.tools.map((tool) => toolDescriptor(tool)) });
        break;
      case "tools/call":
        await handleToolsCall(id, message.params ?? {});
        break;
      case "shutdown":
        await handleShutdown(id);
        break;
      default:
        await respondError(id, -32601, `unknown method ${message.method}`);
    }
  };

  input.on("line", (line) => void dispatch(line).catch(() => {}));
  input.on("close", () => {
    for (const entry of new Set(activeRequests.values())) entry.controller.abort();
  });

  try {
    await new Promise((resolve) => {
      input.on("close", () => {
        if (!shutdownResponded && !closing) {
          // stdin EOF without explicit shutdown: cancel active work then exit.
        }
        resolve();
      });
    });
  } finally {
    restoreConsole();
  }
}

export { ToolContext, ToolError };
export { errorResult };
