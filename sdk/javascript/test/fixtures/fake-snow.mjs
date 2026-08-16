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
  capabilities: scenario === "bad_caps_type" ? [1] : ["models_list", "permission_input", "prompt_completion", "session_info", "session_messages", "subagent_models", "user_input"],
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
      emit({ id, type: "response", command: request.type, success: true, data: { provider: "fake", model: "fake-1" } });
      break;
    case "session_messages":
      emit({ id, type: "response", command: request.type, success: true, data: { messages: [{ id: "u-1", role: "user", content: [], ts: 1 }] } });
      break;
    case "models_list":
    case "subagent_models":
      emit({ id, type: "response", command: request.type, success: true, data: { models: [{ provider: "fake", id: "fake-1" }] } });
      break;
    case "prompt": {
      emit({ id, type: "response", command: "prompt", success: true });
      if (request.message === "wait") {
        waitingPrompt = id;
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
