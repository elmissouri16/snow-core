export type ThinkingLevel = "off" | "minimal" | "low" | "medium" | "high" | "xhigh" | "max" | "ultra";
export type CollaborationMode = "default" | "plan";

export interface RPCReady {
  type: "rpc_ready";
  protocol_version: "1";
  snow_version?: string;
  capabilities: string[];
  max_input_bytes: number;
}

export interface AgentEvent {
  type: string;
  text?: string;
  message?: string;
  agent?: AgentRef;
  user_input?: UserInputRequest;
  [key: string]: unknown;
}

export interface AgentRef {
  thread_id: string;
  path: string;
  depth: number;
  [key: string]: unknown;
}

export interface UserInputOption {
  label: string;
  description?: string;
}

export interface UserInputQuestion {
  id: string;
  header: string;
  question: string;
  options?: UserInputOption[];
}

export interface UserInputRequest {
  id: string;
  tool_call_id?: string;
  questions: UserInputQuestion[];
}

export interface UserInputAnswer {
  id: string;
  answer: string;
}

export interface RPCResponse<T = unknown> {
  id?: string;
  type: "response";
  command?: string;
  success: boolean;
  error?: string;
  error_code?: "canceled" | "conflict" | "destination_exists" | "git_dirty" | "git_failure" | "invalid" | "not_found" | "not_git_repository" | "session_busy" | "subagents_active" | "unsupported";
  data?: T;
}

export interface PromptResult {
  requestId: string;
  status: "completed";
  raw: Record<string, unknown>;
}

export interface Model {
  provider: string;
  id: string;
  display_name?: string;
  description?: string;
  supports_tools?: boolean;
  supports_thinking?: boolean;
  supports_vision?: boolean;
  thinking_levels?: ThinkingLevel[];
  [key: string]: unknown;
}

export interface ModelList {
  provider?: string;
  current?: string;
  enabled?: boolean;
  models: Model[];
}

export interface SessionInfo {
  session_id: string;
  name: string;
  path: string;
  cwd: string;
  provider: string;
  model: string;
  thinking: ThinkingLevel;
  thinking_levels: ThinkingLevel[];
  collaboration_mode: CollaborationMode;
  [key: string]: unknown;
}

export interface SessionBranch {
  id: string;
  name?: string;
  parent_branch_id?: string;
  forked_from_id?: string;
  tip_id: string;
  messages: number;
  preview?: string;
  created_at: number;
  updated_at: number;
  active: boolean;
}

export interface BranchForkParams {
  source_branch_id?: string;
  from_entry_id?: string;
  name?: string;
}

export interface SessionForkParams extends BranchForkParams {
  destination_path?: string;
}

export interface SessionWorktreeForkParams extends SessionForkParams {
  worktree_path?: string;
  git_branch?: string;
}

export interface SessionForkResult {
  source_session_id: string;
  source_branch_id: string;
  source_entry_id: string;
  session_id: string;
  session_path: string;
  cwd: string;
  name?: string;
  branch: SessionBranch;
  worktree?: {path: string; branch: string; commit?: string};
}

export interface SnowOptions {
  executable?: string;
  executableArgs?: string[];
  provider?: string;
  model?: string;
  baseUrl?: string;
  session?: string;
  permission?: "ask" | "allow" | "deny";
  thinking?: ThinkingLevel;
  cwd?: string;
  env?: Record<string, string>;
  inheritEnv?: boolean;
  disablePlugins?: boolean;
  disableMcp?: boolean;
  disableSkills?: boolean;
  disableSubagents?: boolean;
  startupTimeoutMs?: number;
  requestTimeoutMs?: number;
  closeTimeoutMs?: number;
  maxFrameBytes?: number;
  eventQueueSize?: number;
  userInputHandler?: (request: UserInputRequest, context: {signal: AbortSignal}) => Promise<{answers: UserInputAnswer[]}>;
  signal?: AbortSignal;
}

export interface EventIterator extends AsyncIterableIterator<AgentEvent> {
  close(): void;
}

export declare const RPC_PROTOCOL_VERSION: "1";

export declare class Snow {
  private constructor(options?: SnowOptions);
  static start(options?: SnowOptions): Promise<Snow>;
  readonly options: Readonly<SnowOptions>;
  readonly ready: RPCReady;
  readonly closed: boolean;
  readonly diagnostics: Array<{kind: string; frame: Record<string, unknown>}>;

  subscribe(
    listener: (event: AgentEvent, context: {signal: AbortSignal}) => void | Promise<void>,
    options?: {capacity?: number; onError?: (error: Error) => void},
  ): () => void;
  events(options?: {capacity?: number; signal?: AbortSignal}): EventIterator;
  request<T = unknown>(type: string, fields?: Record<string, unknown>, options?: {id?: string; timeoutMs?: number}): Promise<RPCResponse<T>>;
  prompt(message: string, options?: {mode?: CollaborationMode; timeoutMs?: number; signal?: AbortSignal}): Promise<PromptResult>;
  abort(): Promise<RPCResponse>;
  sessionInfo(): Promise<RPCResponse<SessionInfo>>;
  sessionRename(name: string): Promise<RPCResponse>;
  branchFork(params?: BranchForkParams): Promise<RPCResponse<SessionBranch>>;
  sessionFork(params?: SessionForkParams): Promise<RPCResponse<SessionForkResult>>;
  sessionWorktreeFork(params?: SessionWorktreeForkParams): Promise<RPCResponse<SessionForkResult>>;
  models(): Promise<RPCResponse<ModelList>>;
  subagentModels(): Promise<RPCResponse<ModelList>>;
  setModel(model: string): Promise<RPCResponse>;
  setThinking(thinking: ThinkingLevel): Promise<RPCResponse>;
  setMode(mode: CollaborationMode): Promise<RPCResponse>;
  steer(message: string): Promise<RPCResponse>;
  followUp(message: string): Promise<RPCResponse>;
  goalGet(): Promise<RPCResponse>;
  goalCreate(objective: string, options?: {tokenBudget?: number; replace?: boolean}): Promise<RPCResponse>;
  goalEdit(objective: string): Promise<RPCResponse>;
  goalPause(): Promise<RPCResponse>;
  goalResume(): Promise<RPCResponse>;
  goalClear(): Promise<RPCResponse>;
  goalContinue(): Promise<RPCResponse>;
  subagentSpawn(params: Record<string, unknown>): Promise<RPCResponse>;
  subagentSendMessage(target: string, message: string): Promise<RPCResponse>;
  subagentFollowup(target: string, message: string): Promise<RPCResponse>;
  subagentWait(options?: {timeoutMs?: number; until?: "activity" | "all"}): Promise<RPCResponse>;
  subagentInterrupt(target: string): Promise<RPCResponse>;
  subagentList(pathPrefix?: string): Promise<RPCResponse>;
  subagentGet(target: string): Promise<RPCResponse>;
  subagentReady(): Promise<RPCResponse>;
  replyUserInput(requestId: string, answers: UserInputAnswer[]): Promise<RPCResponse>;
  rejectUserInput(requestId: string): Promise<RPCResponse>;
  close(): Promise<void>;
}

export declare class SnowError extends Error {}
export declare class SnowClosedError extends SnowError {}
export declare class SnowProcessError extends SnowError {}
export declare class SnowProtocolError extends SnowError {}
export declare class SnowVersionError extends SnowProtocolError {}
export declare class SnowTimeoutError extends SnowError {}
export declare class SnowCancelledError extends SnowError {}
export declare class SnowSubscriptionOverflowError extends SnowError {}
export declare class SnowCommandError extends SnowError { readonly command: string; readonly requestId: string; }
export declare class SnowPromptError extends SnowError { readonly requestId: string; }
