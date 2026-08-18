/** Public type declarations for @snow-core/plugin. */

export type Risk = "read" | "write" | "exec" | "network";
export type LogSeverity = "debug" | "info" | "warning" | "error";
export type DiscoveryMode = "always" | "deferred";

export interface Discovery {
  mode: DiscoveryMode;
  namespace?: string;
  keywords?: string[];
}

export interface ToolDefinition<Arguments = Record<string, unknown>> {
  name: string;
  description: string;
  risk?: Risk;
  parameters: Record<string, unknown>;
  capabilities?: string[];
  discovery?: Discovery;
  execute(params: Arguments, context: ToolContext): unknown | Promise<unknown>;
}

export interface ToolDescriptor {
  name: string;
  description: string;
  risk: Risk;
  parameters: Record<string, unknown>;
  capabilities?: string[];
  discovery?: Discovery;
}

export interface PluginManifest {
  id: string;
  name: string;
  version: string;
  protocol_version: number;
  capabilities?: string[];
  supported_events?: string[];
  allowed_tools?: string[];
}

export interface SetupContext {
  cwd: string;
  sessionId: string;
  hostVersion: string;
  hostCapabilities: string[];
  config: unknown;
}

export interface ShutdownState {
  state?: unknown;
}

export interface EventContext {
  state?: unknown;
}

export interface Plugin<TState = unknown> {
  manifest: PluginManifest;
  tools: ToolDefinition[];
  events: Record<string, (event: unknown, context: EventContext) => unknown | Promise<unknown>>;
  setup?: (context: SetupContext) => TState | Promise<TState>;
  shutdown?: (context: ShutdownState) => void | Promise<void>;
  _state?: TState;
  _config?: unknown;
  _setupDone?: boolean;
}

export interface ToolContext {
  callId: string;
  requestId: string;
  cwd: string;
  sessionId: string;
  signal: AbortSignal;
  deadline: Date | null;
  config: unknown;
  state: unknown;
  progress(message: string, options?: { done?: boolean; is_error?: boolean }): Promise<void>;
  log(severity: LogSeverity, message: string): Promise<void>;
}

export interface ToolResult {
  content: Array<{ type: string; text?: string; [key: string]: unknown }>;
  details?: Record<string, unknown>;
  is_error: boolean;
}

export function definePlugin<TState = unknown>(options: {
  manifest: Omit<PluginManifest, "protocol_version"> & Partial<Pick<PluginManifest, "protocol_version">>;
  tools?: ToolDefinition[];
  events?: Record<string, (event: unknown, context: EventContext) => unknown | Promise<unknown>>;
  setup?: (context: SetupContext) => TState | Promise<TState>;
  shutdown?: (context: ShutdownState) => void | Promise<void>;
}): Plugin<TState>;

export function defineTool<Arguments = Record<string, unknown>>(definition: ToolDefinition<Arguments>): ToolDefinition<Arguments>;

export function toolDescriptor(tool: ToolDefinition): ToolDescriptor;

export function normalizeResult(value: unknown): ToolResult;

export function result(
  content: Array<Record<string, unknown>>,
  options?: { details?: Record<string, unknown>; is_error?: boolean },
): ToolResult;

export function textResult(text: string, options?: { details?: Record<string, unknown>; is_error?: boolean }): ToolResult;

export function errorResult(message: string, options?: { details?: Record<string, unknown> }): ToolResult;

export function serve(
  plugin: Plugin,
  options?: { maxConcurrency?: number; maxEventQueue?: number },
): Promise<void>;

export class PluginError extends Error {
  name: string;
  code: string;
}
export class ToolError extends PluginError {}
export class ProtocolError extends PluginError {}
export class BoundsError extends PluginError {}

export const PROTOCOL_VERSION: 2;
export const MAX_PROGRESS_BYTES: number;
export function validateRequest(message: unknown): string;
export function encodeFrame(payload: Record<string, unknown>): Buffer;
