import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { TextDecoder } from "node:util";

import {
  SnowCancelledError,
  SnowClosedError,
  SnowCommandError,
  SnowProcessError,
  SnowPromptError,
  SnowProtocolError,
  SnowSubscriptionOverflowError,
  SnowTimeoutError,
  SnowVersionError,
} from "./errors.js";

export const RPC_PROTOCOL_VERSION = "1";
const REQUIRED_CAPABILITIES = new Set(["prompt_completion", "session_info"]);
const DEFAULT_MAX_FRAME_BYTES = 16 * 1024 * 1024;
const MAX_STDERR_BYTES = 64 * 1024;
const decoder = new TextDecoder("utf-8", { fatal: true });

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function withTimeout(promise, milliseconds, message) {
  let timer;
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => reject(new SnowTimeoutError(message)), milliseconds);
  });
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timer));
}

class CallbackSubscription {
  constructor(client, listener, capacity, onError) {
    this.client = client;
    this.listener = listener;
    this.capacity = capacity;
    this.onError = onError;
    this.queue = [];
    this.running = false;
    this.closed = false;
    this.controller = new AbortController();
  }

  _push(event) {
    if (this.closed) return;
    if (this.queue.length >= this.capacity) {
      this._finish(new SnowSubscriptionOverflowError("event callback queue overflow"));
      return;
    }
    this.queue.push(structuredClone(event));
    if (!this.running) {
      this.running = true;
      queueMicrotask(() => void this._drain());
    }
  }

  async _drain() {
    while (!this.closed && this.queue.length > 0) {
      try {
        await this.listener(this.queue.shift(), { signal: this.controller.signal });
      } catch (error) {
        this._finish(error instanceof Error ? error : new Error(String(error)));
        return;
      }
    }
    this.running = false;
    if (!this.closed && this.queue.length > 0) {
      this.running = true;
      queueMicrotask(() => void this._drain());
    }
  }

  _finish(error = null) {
    if (this.closed) return;
    this.closed = true;
    this.controller.abort();
    this.queue.length = 0;
    this.client.callbackSubscriptions.delete(this);
    if (error) {
      this.client._diagnose("subscription_error", { message: error.message });
      if (this.onError) queueMicrotask(() => {
        try { this.onError(error); } catch { /* observer errors do not own transport */ }
      });
    }
  }
}

class EventIterator {
  constructor(client, capacity, signal) {
    this.client = client;
    this.capacity = capacity;
    this.queue = [];
    this.waiters = [];
    this.closed = false;
    this.error = null;
    this.abort = () => this.close();
    if (signal) {
      if (signal.aborted) this.close();
      else signal.addEventListener("abort", this.abort, { once: true });
      this.signal = signal;
    }
  }

  [Symbol.asyncIterator]() {
    return this;
  }

  next() {
    if (this.queue.length > 0) return Promise.resolve({ value: this.queue.shift(), done: false });
    if (this.closed) {
      if (this.error) return Promise.reject(this.error);
      return Promise.resolve({ value: undefined, done: true });
    }
    const item = deferred();
    this.waiters.push(item);
    return item.promise;
  }

  return() {
    this.close();
    return Promise.resolve({ value: undefined, done: true });
  }

  close() {
    this.client._iterators.delete(this);
    this._finish(null);
  }

  _push(event) {
    if (this.closed) return;
    const isolated = structuredClone(event);
    const waiter = this.waiters.shift();
    if (waiter) {
      waiter.resolve({ value: isolated, done: false });
      return;
    }
    if (this.queue.length >= this.capacity) {
      this.client._iterators.delete(this);
      this._finish(new SnowSubscriptionOverflowError("event iterator queue overflow"));
      return;
    }
    this.queue.push(isolated);
  }

  _finish(error) {
    if (this.closed) return;
    this.closed = true;
    this.error = error;
    if (error) this.queue.length = 0;
    if (this.signal) this.signal.removeEventListener("abort", this.abort);
    for (const waiter of this.waiters.splice(0)) {
      if (error) waiter.reject(error);
      else waiter.resolve({ value: undefined, done: true });
    }
  }
}

export class Snow {
  constructor(options = {}) {
    this.options = {
      executable: "snow",
      executableArgs: [],
      provider: "fake",
      model: "",
      baseUrl: "",
      session: "",
      permission: "deny",
      thinking: "off",
      cwd: undefined,
      env: undefined,
      inheritEnv: true,
      disablePlugins: true,
      disableMcp: true,
      disableSkills: true,
      disableSubagents: true,
      startupTimeoutMs: 10_000,
      requestTimeoutMs: 120_000,
      closeTimeoutMs: 5_000,
      maxFrameBytes: DEFAULT_MAX_FRAME_BYTES,
      eventQueueSize: 256,
      userInputHandler: undefined,
      permissionHandler: undefined,
      signal: undefined,
      ...options,
    };
    if (typeof this.options.executable !== "string" || this.options.executable.length === 0) {
      throw new TypeError("executable must be a non-empty string");
    }
    if (!Array.isArray(this.options.executableArgs) || this.options.executableArgs.some((value) => typeof value !== "string")) {
      throw new TypeError("executableArgs must be an array of strings");
    }
    for (const [name, value] of [["startupTimeoutMs", this.options.startupTimeoutMs], ["requestTimeoutMs", this.options.requestTimeoutMs], ["closeTimeoutMs", this.options.closeTimeoutMs]]) {
      if (!Number.isFinite(value) || value <= 0) throw new RangeError(`${name} must be finite and positive`);
    }
    for (const [name, value] of [["maxFrameBytes", this.options.maxFrameBytes], ["eventQueueSize", this.options.eventQueueSize]]) {
      if (!Number.isSafeInteger(value) || value <= 0) throw new RangeError(`${name} must be a positive safe integer`);
    }
    this.child = null;
    this.ready = null;
    this.closed = false;
    this.closing = false;
    this.failure = null;
    this.buffer = Buffer.alloc(0);
    this.stderrTail = Buffer.alloc(0);
    this.writeLimit = this.options.maxFrameBytes;
    this.diagnostics = [];
    this.pending = new Map();
    this.prompts = new Map();
    this.abandonedPrompts = new Set();
    this.callbackSubscriptions = new Set();
    this._iterators = new Set();
    this.handlerTasks = new Set();
    this.handlerController = new AbortController();
    this.writeChain = Promise.resolve();
    this.readyState = deferred();
    this.closeState = deferred();
    this.stdoutEndTimer = null;
  }

  static async start(options = {}) {
    const snow = new Snow(options);
    snow._spawn();
    try {
      snow.ready = await withTimeout(snow.readyState.promise, snow.options.startupTimeoutMs, "timed out waiting for rpc_ready");
      return snow;
    } catch (error) {
      await snow.close();
      throw error;
    }
  }

  _spawn() {
    const args = [...this.options.executableArgs, ...this._snowArgs()];
    this.child = spawn(this.options.executable, args, {
      cwd: this.options.cwd,
      env: { ...(this.options.inheritEnv ? process.env : {}), ...(this.options.env ?? {}) },
      signal: this.options.signal,
      shell: false,
      stdio: ["pipe", "pipe", "pipe"],
    });
    this.child.stdout.on("data", (chunk) => this._onStdout(chunk));
    this.child.stdout.on("end", () => {
      if (!this.closing && !this.failure) {
        this.stdoutEndTimer = setTimeout(() => this._fatal(new SnowProcessError("Snow RPC stdout closed unexpectedly")), 50);
      }
    });
    this.child.stdin.on("error", (cause) => this._fatal(new SnowProcessError(`Snow RPC write failed: ${cause.message}`)));
    this.child.stderr.on("data", (chunk) => this._onStderr(chunk));
    this.child.on("error", (error) => this._fatal(new SnowProcessError(`failed to start Snow executable: ${error.message}`)));
    this.child.on("close", (code, signal) => {
      if (this.stdoutEndTimer) {
        clearTimeout(this.stdoutEndTimer);
        this.stdoutEndTimer = null;
      }
      this.closeState.resolve({ code, signal });
      if (!this.closing) {
        const suffix = this.stderrTail.length ? `: ${this.stderrTail.toString("utf8").trim()}` : "";
        this._fatal(new SnowProcessError(`Snow process exited with status ${code ?? signal}${suffix}`));
      }
    });
  }

  _snowArgs() {
    const o = this.options;
    const args = ["--mode", "rpc", "--provider", o.provider, "--permission", o.permission, "--thinking", o.thinking];
    if (o.model) args.push("--model", o.model);
    if (o.baseUrl) args.push("--base-url", o.baseUrl);
    if (o.session) args.push("--session", o.session);
    else args.push("--no-session");
    if (o.disablePlugins) args.push("--no-plugins");
    if (o.disableMcp) args.push("--no-mcp");
    if (o.disableSkills) args.push("--no-skills");
    if (o.disableSubagents) args.push("--no-subagents");
    return args;
  }

  subscribe(listener, { capacity = this.options.eventQueueSize, onError } = {}) {
    this._ensureOpen();
    if (typeof listener !== "function") throw new TypeError("listener must be a function");
    if (!Number.isInteger(capacity) || capacity <= 0) throw new RangeError("capacity must be a positive integer");
    if (onError !== undefined && typeof onError !== "function") throw new TypeError("onError must be a function");
    const subscription = new CallbackSubscription(this, listener, capacity, onError);
    this.callbackSubscriptions.add(subscription);
    return () => subscription._finish();
  }

  events({ capacity = this.options.eventQueueSize, signal } = {}) {
    this._ensureOpen();
    if (!Number.isInteger(capacity) || capacity <= 0) throw new RangeError("capacity must be a positive integer");
    const iterator = new EventIterator(this, capacity, signal);
    if (!iterator.closed) this._iterators.add(iterator);
    return iterator;
  }

  async request(type, fields = {}, { id = `${type}-${randomUUID()}`, timeoutMs = this.options.requestTimeoutMs } = {}) {
    this._ensureOpen();
    if (this.pending.has(id) || (this.prompts.has(id) && type !== "prompt")) {
      throw new SnowProtocolError(`duplicate active request id ${JSON.stringify(id)}`);
    }
    const state = deferred();
    this.pending.set(id, state);
    try {
      await this._send({ id, type, ...fields });
      return await withTimeout(state.promise, timeoutMs, `timed out waiting for ${type} (${id})`);
    } catch (error) {
      void state.promise.catch(() => {});
      throw error;
    } finally {
      this.pending.delete(id);
    }
  }

  async prompt(message = "", { content, mode = "", timeoutMs = this.options.requestTimeoutMs, signal } = {}) {
    const id = `prompt-${randomUUID()}`;
    const terminal = deferred();
    this.prompts.set(id, terminal);
    let abortListener;
    if (signal) {
      abortListener = () => void this.abort().catch(() => {});
      if (signal.aborted) abortListener();
      else signal.addEventListener("abort", abortListener, { once: true });
    }
    let abandon = false;
    try {
      await this.request("prompt", { message, ...(content ? { content } : {}), ...(mode ? { mode } : {}) }, { id, timeoutMs });
      return await withTimeout(terminal.promise, timeoutMs, `timed out waiting for prompt completion (${id})`);
    } catch (error) {
      if (error instanceof SnowTimeoutError) {
        await this._abortAndConsumePrompt(terminal);
        abandon = this.prompts.has(id);
      }
      throw error;
    } finally {
      const pending = this.prompts.delete(id);
      if (pending && abandon) {
        this.abandonedPrompts.add(id);
        while (this.abandonedPrompts.size > 128) this.abandonedPrompts.delete(this.abandonedPrompts.values().next().value);
      }
      if (signal && abortListener) signal.removeEventListener("abort", abortListener);
    }
  }

  async _abortAndConsumePrompt(terminal) {
    try { await this.abort(); } catch { /* transport may already be unavailable */ }
    try { await withTimeout(terminal.promise, this.options.closeTimeoutMs, "timed out consuming prompt cancellation"); } catch { /* terminal status is consumed by routing */ }
  }

  abort() { return this.request("abort"); }
  sessionInfo() { return this.request("session_info"); }
  sessionRename(name) { return this.request("session_rename", { params: { name } }); }
  branchFork(params = {}) { return this.request("branch_fork", { params }); }
  sessionFork(params = {}) { return this.request("session_fork", { params }); }
  sessionWorktreeFork(params = {}) { return this.request("session_worktree_fork", { params }); }
  models() { return this.request("models_list"); }
  subagentModels() { return this.request("subagent_models"); }
  setModel(model) { return this.request("set_model", { model }); }
  setThinking(thinking) { return this.request("set_thinking", { thinking }); }
  setMode(mode) { return this.request("set_mode", { mode }); }
  steer(message) { return this.request("steer", { message }); }
  followUp(message) { return this.request("follow_up", { message }); }
  goalGet() { return this.request("goal_get"); }
  goalCreate(objective, { tokenBudget, replace = false } = {}) {
    return this.request("goal_create", { params: { objective, ...(tokenBudget === undefined ? {} : { token_budget: tokenBudget }), ...(replace ? { replace: true } : {}) } });
  }
  goalEdit(objective) { return this.request("goal_edit", { params: { objective } }); }
  goalPause() { return this.request("goal_pause"); }
  goalResume() { return this.request("goal_resume"); }
  goalClear() { return this.request("goal_clear"); }
  goalContinue() { return this.request("goal_continue"); }
  subagentSpawn(params) { return this.request("subagent_spawn", { params }); }
  subagentSendMessage(target, message) { return this.request("subagent_send_message", { params: { target, message } }); }
  subagentFollowup(target, message) { return this.request("subagent_followup", { params: { target, message } }); }
  subagentWait({ timeoutMs = 0, until = "activity" } = {}) { return this.request("subagent_wait", { params: { timeout_ms: timeoutMs, until } }); }
  subagentInterrupt(target) { return this.request("subagent_interrupt", { params: { target } }); }
  subagentList(pathPrefix = "") { return this.request("subagent_list", pathPrefix ? { params: { path_prefix: pathPrefix } } : {}); }
  subagentGet(target) { return this.request("subagent_get", { params: { target } }); }
  subagentReady() { return this.request("subagent_ready"); }
  replyUserInput(requestId, answers) { return this.request("user_input_reply", { params: { request_id: requestId, answers } }); }
  rejectUserInput(requestId) { return this.request("user_input_reject", { params: { request_id: requestId } }); }
  replyPermission(requestId, decision) { return this.request("permission_reply", { params: { request_id: requestId, decision } }); }
  rejectPermission(requestId) { return this.request("permission_reject", { params: { request_id: requestId } }); }
  compact() { return this.request("compact"); }
  branches() { return this.request("branches_list"); }
  branchSelect(branchId) { return this.request("branch_select", { params: { branch_id: branchId } }); }
  branchRename(branchId, name) { return this.request("branch_rename", { params: { branch_id: branchId, name } }); }
  branchDelete(branchId) { return this.request("branch_delete", { params: { branch_id: branchId } }); }
  messages() { return this.request("messages_list"); }
  usage() { return this.request("usage"); }
  pendingInputs() { return this.request("pending_inputs"); }
  clearPendingInputs() { return this.request("pending_inputs_clear"); }
  configurationDiagnostics() { return this.request("diagnostics"); }
  mcpServers() { return this.request("mcp_servers"); }
  skills() { return this.request("skills"); }
  setReasoningSummary(reasoningSummary) { return this.request("set_reasoning_summary", { reasoning_summary: reasoningSummary }); }
  setTextVerbosity(textVerbosity) { return this.request("set_text_verbosity", { text_verbosity: textVerbosity }); }


  async close() {
    if (this.closed) return;
    this.closing = true;
    this.handlerController.abort();
    if (this.child && this.child.exitCode === null && this.child.signalCode === null) {
      this.child.stdin.end();
      try {
        await withTimeout(this.closeState.promise, this.options.closeTimeoutMs, "timed out closing Snow process");
      } catch (error) {
        if (!(error instanceof SnowTimeoutError)) throw error;
        this.child.kill("SIGTERM");
        try {
          await withTimeout(this.closeState.promise, this.options.closeTimeoutMs, "timed out terminating Snow process");
        } catch (second) {
          if (!(second instanceof SnowTimeoutError)) throw second;
          this.child.kill("SIGKILL");
          await this.closeState.promise;
        }
      }
    }
    if (this.handlerTasks.size > 0) {
      try {
        await withTimeout(Promise.allSettled([...this.handlerTasks]), this.options.closeTimeoutMs, "timed out closing user input handlers");
      } catch (error) {
        if (!(error instanceof SnowTimeoutError)) throw error;
      }
    }
    this._finish(new SnowClosedError("Snow client closed"), null);
    this.closed = true;
  }

  async _send(message) {
    this._ensureOpen();
    const frame = Buffer.from(`${JSON.stringify(message)}\n`, "utf8");
    if (frame.length > this.writeLimit) throw new SnowProtocolError("request frame exceeds configured maximum");
    this.writeChain = this.writeChain.then(() => new Promise((resolve, reject) => {
      if (!this.child?.stdin.writable) {
        const error = new SnowProcessError("Snow RPC stdin is closed");
        this._fatal(error);
        reject(error);
        return;
      }
      this.child.stdin.write(frame, (cause) => {
        if (!cause) {
          resolve();
          return;
        }
        const error = new SnowProcessError(`Snow RPC write failed: ${cause.message}`);
        this._fatal(error);
        reject(error);
      });
    }));
    return this.writeChain;
  }

  _onStdout(chunk) {
    if (this.closed) return;
    this.buffer = Buffer.concat([this.buffer, chunk]);
    if (this.buffer.length > this.options.maxFrameBytes && this.buffer.indexOf(0x0a) < 0) {
      this._fatal(new SnowProtocolError("RPC output frame exceeds configured maximum"));
      return;
    }
    while (true) {
      const newline = this.buffer.indexOf(0x0a);
      if (newline < 0) break;
      if (newline > this.options.maxFrameBytes) {
        this._fatal(new SnowProtocolError("RPC output frame exceeds configured maximum"));
        return;
      }
      const frame = this.buffer.subarray(0, newline);
      this.buffer = this.buffer.subarray(newline + 1);
      if (frame.length === 0) continue;
      try {
        const value = JSON.parse(decoder.decode(frame));
        if (!value || typeof value !== "object" || Array.isArray(value) || typeof value.type !== "string") {
          throw new Error("frame is not an object with a type");
        }
        this._route(value);
      } catch (error) {
        this._fatal(new SnowProtocolError(`invalid RPC output frame: ${error.message}`));
        return;
      }
    }
    if (this.buffer.length > this.options.maxFrameBytes) {
      this._fatal(new SnowProtocolError("RPC output frame exceeds configured maximum"));
    }
  }

  _onStderr(chunk) {
    this.stderrTail = Buffer.concat([this.stderrTail, chunk]);
    if (this.stderrTail.length > MAX_STDERR_BYTES) this.stderrTail = this.stderrTail.subarray(this.stderrTail.length - MAX_STDERR_BYTES);
  }

  _route(message) {
    if (message.type === "rpc_ready") {
      if (this.ready) return this._fatal(new SnowProtocolError("duplicate rpc_ready frame"));
      if (typeof message.protocol_version !== "string") {
        return this.readyState.reject(new SnowProtocolError("rpc_ready protocol_version must be a string"));
      }
      const version = message.protocol_version;
      if (version !== RPC_PROTOCOL_VERSION) return this.readyState.reject(new SnowVersionError(`unsupported RPC protocol version ${JSON.stringify(version)}`));
      if (typeof message.snow_version !== "string") {
        return this.readyState.reject(new SnowProtocolError("rpc_ready snow_version must be a string"));
      }
      if (!Array.isArray(message.capabilities) || message.capabilities.some((value) => typeof value !== "string")) {
        return this.readyState.reject(new SnowProtocolError("rpc_ready capabilities must be an array of strings"));
      }
      const capabilities = [...message.capabilities];
      const missing = [...REQUIRED_CAPABILITIES].filter((capability) => !capabilities.includes(capability));
      if (missing.length) return this.readyState.reject(new SnowVersionError(`Snow RPC is missing capabilities: ${missing.join(", ")}`));
      const maxInputBytes = message.max_input_bytes;
      if (!Number.isSafeInteger(maxInputBytes) || maxInputBytes <= 0) {
        return this.readyState.reject(new SnowProtocolError("rpc_ready has an invalid max_input_bytes"));
      }
      this.writeLimit = Math.min(this.options.maxFrameBytes, maxInputBytes);
      this.ready = { ...message, protocol_version: version, capabilities, max_input_bytes: maxInputBytes };
      this.readyState.resolve(this.ready);
      return;
    }
    if (!this.ready) return this._fatal(new SnowProtocolError("received RPC output before rpc_ready"));
    if (message.type === "response") {
      const id = String(message.id ?? "");
      const state = this.pending.get(id);
      if (!state) {
        this._diagnose("unknown_response", message);
        return;
      }
      this.pending.delete(id);
      if (message.success === true) state.resolve(message);
      else state.reject(new SnowCommandError(String(message.command ?? "unknown"), id, String(message.error ?? "command failed")));
      return;
    }
    if (message.type === "prompt_completed") {
      const id = String(message.request_id ?? "");
      const state = this.prompts.get(id);
      if (!state) {
        if (this.abandonedPrompts.delete(id)) {
          this._diagnose("late_prompt_completion", message);
          return;
        }
        return this._fatal(new SnowProtocolError(`completion for unknown prompt ${JSON.stringify(id)}`));
      }
      this.prompts.delete(id);
      if (message.status === "completed") state.resolve({ requestId: id, status: "completed", raw: message });
      else if (message.status === "canceled") state.reject(new SnowCancelledError(`prompt (${id}) was canceled`));
      else if (message.status === "failed") state.reject(new SnowPromptError(id, String(message.error ?? "prompt failed")));
      else state.reject(new SnowProtocolError(`unknown prompt status ${JSON.stringify(message.status)}`));
      return;
    }
    for (const subscription of this.callbackSubscriptions) subscription._push(message);
    for (const iterator of this._iterators) iterator._push(message);
    if (message.type === "user_input_request" && this.options.userInputHandler) {
      const task = Promise.resolve().then(() => this._handleUserInput(message));
      this.handlerTasks.add(task);
      task.finally(() => this.handlerTasks.delete(task));
    }
    if (message.type === "permission_request" && this.options.permissionHandler) {
      const task = Promise.resolve().then(() => this._handlePermission(message));
      this.handlerTasks.add(task);
      task.finally(() => this.handlerTasks.delete(task));
    }
  }

  async _handlePermission(event) {
    const request = event.permission?.request;
    if (!request || typeof request.id !== "string") {
      return this._fatal(new SnowProtocolError("permission_request omitted request data"));
    }
    try {
      const decision = await this.options.permissionHandler(request, { signal: this.handlerController.signal });
      if (!["allow", "allow_session", "allow_always", "deny"].includes(decision)) {
        throw new SnowProtocolError("permission handler returned an invalid decision");
      }
      await this.replyPermission(request.id, decision);
    } catch {
      try { await this.rejectPermission(request.id); } catch { /* process may be closing */ }
    }
  }

  async _handleUserInput(event) {
    const request = event.user_input;
    if (!request || typeof request.id !== "string") return this._fatal(new SnowProtocolError("user_input_request omitted request data"));
    try {
      const response = await this.options.userInputHandler(request, { signal: this.handlerController.signal });
      const answers = response?.answers;
      if (!Array.isArray(answers)) throw new SnowProtocolError("user input handler must return an answers array");
      const expected = request.questions.map((question) => question.id);
      const actual = answers.map((answer) => answer?.id);
      if (new Set(actual).size !== actual.length || actual.length !== expected.length || actual.some((id) => !expected.includes(id))) {
        throw new SnowProtocolError("user input handler answers do not match question ids");
      }
      if (answers.some((answer) => typeof answer?.answer !== "string")) {
        throw new SnowProtocolError("user input handler answers must be strings");
      }
      await this.replyUserInput(request.id, answers);
    } catch {
      try { await this.rejectUserInput(request.id); } catch { /* process may be closing */ }
    }
  }

  _diagnose(kind, frame) {
    this.diagnostics.push({ kind, frame });
    if (this.diagnostics.length > 128) this.diagnostics.splice(0, this.diagnostics.length - 128);
  }

  _fatal(error) {
    if (this.closed || this.failure) return;
    this.failure = error;
    if (!this.ready) this.readyState.reject(error);
    this._finish(error, error);
    if (!this.closing && this.child && this.child.exitCode === null) this.child.kill("SIGTERM");
  }

  _finish(error, iteratorError) {
    for (const state of this.pending.values()) state.reject(error);
    this.pending.clear();
    for (const state of this.prompts.values()) state.reject(error);
    this.prompts.clear();
    for (const subscription of this.callbackSubscriptions) subscription._finish(iteratorError);
    this.callbackSubscriptions.clear();
    for (const iterator of this._iterators) iterator._finish(iteratorError);
    this._iterators.clear();
  }

  _ensureOpen() {
    if (this.failure) throw this.failure;
    if (this.closed || this.closing) throw new SnowClosedError("Snow client is closed");
    if (!this.child || this.child.exitCode !== null || this.child.signalCode !== null) throw new SnowClosedError("Snow process is not running");
  }
}
