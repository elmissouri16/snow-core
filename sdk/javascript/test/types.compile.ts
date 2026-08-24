import type {
  ContentBlock,
  Message,
  PromptContentBlock,
  PromptOptions,
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
