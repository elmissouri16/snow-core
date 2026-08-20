#!/usr/bin/env node
import { closeSync } from "node:fs";
import { createInterface } from "node:readline";

function emit(value) {
  process.stdout.write(`${JSON.stringify(value)}\n`);
}

const scenario = process.env.FAKE_SNOW_SCENARIO ?? "";
emit({
  type: "rpc_ready",
  protocol_version: scenario === "bad_version_type" ? 1 : (scenario === "bad_version" ? "2" : "1"),
  snow_version: scenario === "bad_snow_type" ? 1 : "fixture",
  capabilities: scenario === "bad_caps_type" ? [1] : ["models_list", "multimodal_prompts", "permission_interaction", "prompt_completion", "session_info", "subagent_models", "user_input"],
  max_input_bytes: scenario === "bad_max_type" ? "128" : (scenario === "small_limit" ? 128 : 16777216),
});
emit({ type: "mode_changed", mode: { mode: "default", reasoning_effort: "off" } });
emit({ id: "unknown-fixture", type: "response", command: "fixture", success: true });
if (scenario === "malformed") process.stdout.write("{not-json}\n");
if (scenario === "oversized") process.stdout.write("x".repeat(2048));
if (scenario === "stdout_closed") process.stdout.end();
if (scenario === "stdin_closed") {
  closeSync(0);
  setTimeout(() => process.exit(0), 5000);
}

let held = null;
let askingPrompt = null;
let permissionPrompt = null;
let waitingPrompt = null;
const lines = createInterface({ input: process.stdin, crlfDelay: Infinity });
for await (const line of lines) {
  if (line.length === 0) continue;
  const request = JSON.parse(line);
  const id = request.id ?? "";
  switch (request.type) {
    case "crash":
      process.stderr.write("fixture process failure\n");
      process.exit(7);
      break;
    case "fail_command":
      emit({ id, type: "response", command: request.type, success: false, error: "fixture command failure" });
      break;
    case "hold":
      held = request;
      break;
    case "release":
      emit({ id, type: "response", command: "release", success: true });
      if (held) {
        emit({ id: held.id, type: "response", command: "hold", success: true });
        held = null;
      }
      break;
    case "session_info":
      emit({ id, type: "response", command: request.type, success: true, data: { provider: "fake", model: "fake-1", reasoning_summary: "auto", text_verbosity: "medium" } });
      break;
    case "compact":
      emit({ id, type: "response", command: request.type, success: true, data: { summarized_messages: 1, retained_messages: 2, summary: "fixture summary" } });
      break;
    case "branches_list":
      emit({ id, type: "response", command: request.type, success: true, data: { branches: [{ id: "branch-1", name: "main", tip_id: "entry-1", messages: 0, created_at: 0, updated_at: 0, active: true }] } });
      break;
    case "branch_select":
    case "branch_delete":
      emit({ id, type: "response", command: request.type, success: true });
      break;
    case "branch_rename":
      emit({ id, type: "response", command: request.type, success: true, data: { id: "branch-1", name: request.params.name, tip_id: "entry-1", messages: 0, created_at: 0, updated_at: 0, active: true } });
      break;
    case "messages_list":
      emit({ id, type: "response", command: request.type, success: true, data: { messages: [{ id: "msg-1", role: "user", content: [{ type: "text", text: "hello" }], ts: 0 }] } });
      break;
    case "usage":
      emit({ id, type: "response", command: request.type, success: true, data: { input: 10, output: 5, cache_read: 0, cache_write: 0, total_tokens: 15, requests: 1 } });
      break;
    case "pending_inputs":
      emit({ id, type: "response", command: request.type, success: true, data: { items: [{ id: "q1", kind: "steer", text: "focus", order: 1 }] } });
      break;
    case "pending_inputs_clear":
      emit({ id, type: "response", command: request.type, success: true, data: { items: [{ id: "q1", kind: "steer", text: "focus", order: 1 }] } });
      break;
    case "diagnostics":
      emit({ id, type: "response", command: request.type, success: true, data: { diagnostics: [{ path: "tui.theme", message: "fixture theme missing" }] } });
      break;
    case "set_reasoning_summary":
    case "set_text_verbosity":
      emit({ id, type: "response", command: request.type, success: true });
      break;
    case "mcp_servers":
      emit({ id, type: "response", command: request.type, success: true, data: { servers: [{ id: "mcp-1", transport: "stdio", connected: true, tool_count: 2 }] } });
      break;
    case "skills":
      emit({ id, type: "response", command: request.type, success: true, data: { skills: [{ name: "caveman", location: "/skills/caveman", scope: "builtin", source: "catalog", enabled: true, description: "compressed mode" }], diagnostics: [{ path: "/skills/broken", level: "error", message: "shadowed" }] } });
      break;
    case "models_list":
    case "subagent_models":
      emit({ id, type: "response", command: request.type, success: true, data: { models: [{ provider: "fake", id: "fake-1" }] } });
      break;
    case "prompt": {
      emit({ id, type: "response", command: "prompt", success: true });
      if (request.content !== undefined) {
        if (request.content.some((block) => block.type !== "image")) {
          emit({ id, type: "response", command: "prompt", success: false, error: "fixture only accepts image content" });
          emit({ type: "prompt_completed", request_id: id, status: "failed", error: "fixture only accepts image content" });
          break;
        }
        emit({ type: "text_delta", text: "image received", turn_id: "turn-img" });
        emit({ type: "turn_done", turn_id: "turn-img" });
        emit({ type: "prompt_completed", request_id: id, status: "completed" });
        break;
      }
      if (request.message === "wait") {
        waitingPrompt = id;
        break;
      }
      if (request.message === "permission") {
        permissionPrompt = id;
        emit({
          type: "permission_request",
          permission: {
            request: {
              id: "perm-handler-1", tool: "bash", args: { command: "echo ok" },
              paths: [], risk: "exec", reason: "fixture",
            },
          },
        });
        break;
      }
      if (request.message === "ask") {
        askingPrompt = id;
        emit({ type: "user_input_request", user_input: { id: "ask-1", questions: [{ id: "choice", header: "Choice", question: "Choose?" }] } });
        break;
      }
      emit({ type: "text_delta", text: "fixture text", turn_id: "turn-1" });
      if (request.message === "many") {
        emit({ type: "text_delta", text: "fixture text 2", turn_id: "turn-1" });
        emit({ type: "text_delta", text: "fixture text 3", turn_id: "turn-1" });
      }
      emit({ type: "turn_done", turn_id: "turn-1" });
      if (request.message === "fail") {
        emit({ id, type: "response", command: "prompt", success: false, error: "fixture failure" });
        emit({ type: "prompt_completed", request_id: id, status: "failed", error: "fixture failure" });
      } else {
        emit({ type: "prompt_completed", request_id: id, status: "completed" });
      }
      break;
    }
    case "permission_reply":
    case "permission_reject":
      emit({ id, type: "response", command: request.type, success: true });
      if (permissionPrompt) {
        emit({ type: "turn_done", turn_id: "turn-permission" });
        emit({ type: "prompt_completed", request_id: permissionPrompt, status: "completed" });
        permissionPrompt = null;
      }
      break;
    case "user_input_reply":
    case "user_input_reject":
      emit({ id, type: "response", command: request.type, success: true });
      if (askingPrompt) {
        emit({ type: "turn_done", turn_id: "turn-ask" });
        emit({ type: "prompt_completed", request_id: askingPrompt, status: "completed" });
        askingPrompt = null;
      }
      break;
    case "abort":
      emit({ id, type: "response", command: request.type, success: true });
      if (waitingPrompt) {
        emit({ type: "aborted", turn_id: "turn-wait" });
        emit({ type: "turn_done", turn_id: "turn-wait" });
        emit({ type: "prompt_completed", request_id: waitingPrompt, status: "canceled" });
        waitingPrompt = null;
      }
      break;
    default:
      emit({ id, type: "response", command: request.type, success: true });
  }
}
