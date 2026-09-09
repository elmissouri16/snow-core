/** Snow JavaScript plugin API v1. Distribute compiled JavaScript, not this file. */
type SnowHostTool = "read" | "write" | "edit" | "grep" | "glob" | "bash" | "webfetch";
type SnowEventType =
  | "session_updated" | "text_delta" | "thinking_delta"
  | "tool_start" | "tool_progress" | "tool_end" | "tool_routing"
  | "permission_request" | "user_input_request" | "usage" | "turn_done"
  | "error" | "aborted" | "model_changed" | "mode_changed"
  | "plan_started" | "plan_delta" | "plan_completed" | "plan_update"
  | "compaction_started" | "compaction_done" | "thread_goal_updated";
interface SnowResult {
  content: { type: "text"; text: string }[];
  isError?: boolean;
}
interface SnowToolContext {
  readonly sessionId: string;
  readonly cwd: string;
  readonly toolCallId: string;
  progress(message: string): void;
  /** Only declared uses are available. The normal Snow tool schema applies. */
  callTool(name: SnowHostTool, args: Record<string, unknown>): SnowResult;
}
interface SnowEvent {
  version: number;
  type: SnowEventType;
  payload: Readonly<Record<string, unknown>>;
}
declare const snow: {
  readonly config: Record<string, unknown>;
  registerTool(definition: {
    name: string;
    description: string;
    parameters: Record<string, unknown>;
    uses?: SnowHostTool[];
    execute(args: Record<string, unknown>, ctx: SnowToolContext): SnowResult;
  }): void;
  on(type: SnowEventType, handler: (event: SnowEvent) => void): void;
  onClose(handler: () => void): void;
  log(level: "info" | "warning" | "error", message: string): void;
};
