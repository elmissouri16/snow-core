import { RpcTransport, type TransportOptions } from "./transport.js";
import { SdkError, RemoteRpcError } from "./errors.js";
import type {
  RpcCommand, RpcRequestFor, RpcResponseFor, RpcReady, RpcNotification, PromptCompleted,
} from "./protocol.js";

export type RpcInput<C extends RpcCommand> = RpcRequestFor<C>;
export type RpcData<C extends RpcCommand> = RpcResponseFor<C> extends infer R
  ? R extends { success: true; data?: infer D } ? D : never : never;
type Params<C extends RpcCommand> = RpcRequestFor<C> extends infer R
  ? R extends { params?: infer P } ? P : never : never;
export type ThinkingLevel = RpcRequestFor<"set_thinking">["thinking"];
export type PermissionDecision = Params<"permission_reply">["decision"];
export type UserAnswers = Params<"user_input_reply">["answers"];
export type ModelSelection = Omit<RpcRequestFor<"set_model">, "id" | "type">;

/** An RPC wait signal does not cancel remote execution. Never retry an unknown outcome. */
export interface RpcCallOptions { signal?: AbortSignal; timeoutMs?: number }
export interface StartOptions extends Omit<TransportOptions, "executable" | "args"> {
  executable?: string;
  provider?: string;
  model?: string;
  session?: string;
  noSession?: boolean;
  permission?: "deny" | "ask" | "allow";
  /** Nonempty built-in allowlist. Omit to use Snow's configured tools. */
  tools?: string[];
  thinking?: ThinkingLevel;
}
export interface PromptOptions { mode?: "default" | "plan" }
export interface PromptRun {
  readonly requestId: string;
  /** Definitive public completion. Transport loss rejects instead of implying success. */
  readonly result: Promise<PromptCompleted>;
  /** Sends abort only if this is still the active tracked run at write time. */
  cancel(): Promise<void>;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  // Admission may fail before this promise is exposed to the application.
  void promise.catch(() => {});
  return { promise, resolve, reject };
}
interface ActiveRun {
  id: string;
  completion: ReturnType<typeof deferred<PromptCompleted>>;
}

function launchArgs(options: StartOptions): string[] {
  const permission = options.permission ?? "deny";
  if (!["deny", "ask", "allow"].includes(permission)) {
    throw new SdkError("invalid_options", "permission must be deny, ask, or allow");
  }
  if (options.session !== undefined && options.noSession) {
    throw new SdkError("invalid_options", "session and noSession cannot be combined");
  }
  if (options.noSession !== undefined && typeof options.noSession !== "boolean") {
    throw new SdkError("invalid_options", "noSession must be boolean");
  }
  const args = ["--mode", "rpc", "--permission", permission];
  for (const [key, flag] of [["provider", "--provider"], ["model", "--model"],
    ["session", "--session"], ["thinking", "--thinking"]] as const) {
    const value = options[key];
    if (value !== undefined) {
      if (typeof value !== "string" || value.length === 0 || value.includes("\0")) {
        throw new SdkError("invalid_options", `${key} must be a nonempty string without NUL`);
      }
      args.push(`${flag}=${value}`);
    }
  }
  if (options.noSession) args.push("--no-session");
  if (options.tools !== undefined) {
    if (!Array.isArray(options.tools) || options.tools.length === 0 ||
      options.tools.some(value => typeof value !== "string" || !/^[a-z][a-z0-9_]*$/.test(value))) {
      throw new SdkError("invalid_options", "tools must be a nonempty list of built-in tool names");
    }
    args.push(`--tools=${options.tools.join(",")}`);
  }
  return args;
}

/** One explicitly started worker; not a sandbox, agent engine, or failover manager. */
export class SnowClient {
  private active?: ActiveRun;
  private constructor(private readonly transport: RpcTransport) {
    transport.onNotification(frame => {
      const run = this.active;
      if (!run || frame.type !== "prompt_completed" || frame.request_id !== run.id) return;
      this.active = undefined;
      run.completion.resolve(structuredClone(frame) as unknown as PromptCompleted);
    });
    transport.onTerminal(error => {
      const run = this.active;
      if (run) run.completion.reject(new SdkError(error.code, error.message, {
        outcomeUnknown: run.id.length > 0,
      }));
      this.active = undefined;
    });
  }

  static async start(options: StartOptions = {}): Promise<SnowClient> {
    const args = launchArgs(options);
    const transport = await RpcTransport.start({ ...options, executable: options.executable ?? "snow", args });
    return new SnowClient(transport);
  }

  get ready(): Readonly<RpcReady> { return this.transport.ready as RpcReady; }
  events(): AsyncIterable<RpcNotification> {
    return this.transport.events() as AsyncIterable<RpcNotification>;
  }
  close(): Promise<void> { return this.transport.close(); }

  rpc<C extends RpcCommand>(request: RpcInput<C> & { type: C }, options?: RpcCallOptions): Promise<RpcResponseFor<C>> {
    return this.transport.call(request as Record<string, unknown>, options) as Promise<RpcResponseFor<C>>;
  }

  private requireCapability(capability: string): void {
    if (!this.ready.capabilities.includes(capability)) {
      throw new SdkError("unsupported_capability", `Snow does not advertise ${capability}`);
    }
  }

  private async perform<C extends RpcCommand>(
    request: Record<string, unknown>, capability?: string, options?: RpcCallOptions,
  ): Promise<RpcData<C>> {
    if (capability) this.requireCapability(capability);
    const response = await this.transport.call(request, options);
    if (!response.success) throw new RemoteRpcError(response);
    return response.data as RpcData<C>;
  }

  async prompt(message: string, options: PromptOptions = {}): Promise<PromptRun> {
    this.requireCapability("prompt_completion");
    if (typeof message !== "string" || message.length === 0 ||
      (options.mode !== undefined && options.mode !== "default" && options.mode !== "plan")) {
      throw new SdkError("invalid_request", "prompt requires nonempty text and a valid optional mode");
    }
    if (this.active) throw new SdkError("prompt_active", "a root prompt is already active");
    const run: ActiveRun = { id: "", completion: deferred<PromptCompleted>() };
    this.active = run;
    try {
      const response = await this.transport.call(
        { type: "prompt", message, ...(options.mode ? { mode: options.mode } : {}) }, undefined,
        id => { run.id = id; },
      );
      if (!response.success) throw new RemoteRpcError(response);
    } catch (error) {
      // Unknown admission outcome keeps the slot until completion/closure. Do not retry.
      if (!(error instanceof SdkError) || !error.outcomeUnknown) {
        if (this.active === run) this.active = undefined;
      }
      run.completion.reject(error);
      throw error;
    }
    return Object.freeze({
      requestId: run.id,
      result: run.completion.promise,
      cancel: async () => {
        if (this.active !== run) return;
        try {
          const response = await this.transport.call({ type: "abort" }, {
            guard: () => this.active === run,
          });
          if (!response.success) throw new RemoteRpcError(response);
        } catch (error) {
          if (error instanceof SdkError && error.code === "run_finished") return;
          throw error;
        }
      },
    });
  }

  readonly models = {
    list: (options?: RpcCallOptions) => this.perform<"models_list">({ type: "models_list" }, "models_list", options),
    discover: (options?: RpcCallOptions) => this.perform<"models_discover">({ type: "models_discover" }, "model_discovery", options),
    /** set_model durably remembers the selection for the project. */
    set: (selection: ModelSelection, options?: RpcCallOptions) =>
      this.perform<"set_model">({ ...selection, type: "set_model" }, undefined, options),
  };
  readonly sessions = {
    list: (options?: RpcCallOptions) => this.perform<"sessions_list">({ type: "sessions_list" }, "session_management", options),
    info: (options?: RpcCallOptions) => this.perform<"session_info">({ type: "session_info" }, "session_info", options),
    create: (options?: RpcCallOptions) => this.perform<"session_create">({ type: "session_create" }, "session_management", options),
    open: (sessionId: string, options?: RpcCallOptions) =>
      this.perform<"session_open">({ type: "session_open", params: { session_id: sessionId } }, "session_management", options),
    rename: (name: string, sessionId?: string, options?: RpcCallOptions) =>
      this.perform<"session_rename">({ type: "session_rename", params: { name, ...(sessionId ? { session_id: sessionId } : {}) } }, "session_management", options),
    messages: (params?: Params<"messages_page">, options?: RpcCallOptions) =>
      this.perform<"messages_page">({ type: "messages_page", ...(params ? { params } : {}) }, "messages_page", options),
  };
  readonly modes = {
    set: (mode: "default" | "plan", options?: RpcCallOptions) =>
      this.perform<"set_mode">({ type: "set_mode", mode }, undefined, options),
    thinking: (thinking: ThinkingLevel, options?: RpcCallOptions) =>
      this.perform<"set_thinking">({ type: "set_thinking", thinking }, undefined, options),
  };
  readonly permissions = {
    reply: (requestId: string, decision: PermissionDecision, options?: RpcCallOptions) =>
      this.perform<"permission_reply">({ type: "permission_reply", params: { request_id: requestId, decision } }, "permission_interaction", options),
    reject: (requestId: string, options?: RpcCallOptions) =>
      this.perform<"permission_reject">({ type: "permission_reject", params: { request_id: requestId } }, "permission_interaction", options),
  };
  readonly questions = {
    reply: (requestId: string, answers: UserAnswers, options?: RpcCallOptions) =>
      this.perform<"user_input_reply">({ type: "user_input_reply", params: { request_id: requestId, answers } }, "user_input", options),
    reject: (requestId: string, options?: RpcCallOptions) =>
      this.perform<"user_input_reject">({ type: "user_input_reject", params: { request_id: requestId } }, "user_input", options),
  };
}
