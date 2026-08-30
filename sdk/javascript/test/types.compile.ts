import type {
  ContentBlock,
  DebugDumpResult,
  DebugStatus,
  Message,
  PromptContentBlock,
  PromptOptions,
  RPCErrorCode,
  RPCResponse,
  Snow,
  SnowCommandError,
} from "../src/index.js";

const promptBlock: PromptContentBlock = { type: "text", text: "hello" };
const prompt: PromptOptions = { content: [promptBlock] };
const toolBlock: ContentBlock = {
  type: "tool_call",
  tool_call_id: "call-1",
  name: "read",
  arguments: { path: "README.md" },
};
const message: Message = {
  id: "message-1",
  role: "assistant",
  content: [toolBlock],
  ts: 0,
};

void prompt;
void message;

declare const snow: Snow;
const debugStatus: Promise<RPCResponse<DebugStatus>> = snow.debugStatus();
const debugDump: Promise<RPCResponse<DebugDumpResult>> = snow.debugDump("/tmp/debug.json");
const goalSet: Promise<RPCResponse> = snow.goalSet("compatibility goal", { tokenBudget: 100 });
declare const commandError: SnowCommandError;
const errorCode: RPCErrorCode | undefined = commandError.errorCode;
const failedResponse: RPCResponse = commandError.response;

void debugStatus;
void debugDump;
void goalSet;
void errorCode;
void failedResponse;
