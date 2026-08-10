// Dependency-free Snow protocol-v2 example for Node.js.
// stdout is reserved for JSON-RPC frames; write diagnostics to stderr.
import * as readline from "node:readline";

const manifest = {
  id: "example-js",
  name: "Snow JavaScript example",
  version: "1.0.0",
  protocol_version: 2,
};

const tools = [
  {
    name: "echo",
    description: "Echo text after an optional delay",
    parameters: {
      type: "object",
      properties: {
        text: { type: "string" },
        delay_ms: { type: "integer", minimum: 0, maximum: 5000 },
      },
      required: ["text"],
      additionalProperties: false,
    },
    risk: "read",
  },
];

const input = readline.createInterface({
  input: process.stdin,
  crlfDelay: Infinity,
});
const activeRequests = new Map();
const activeCalls = new Map();
let writes = Promise.resolve();
let closing = false;

function writeFrame(frame) {
  const line = `${JSON.stringify(frame)}\n`;
  writes = writes.then(
    () =>
      new Promise((resolve, reject) => {
        process.stdout.write(line, (error) => (error ? reject(error) : resolve()));
      }),
  );
  return writes;
}

function respond(id, result) {
  return writeFrame({ jsonrpc: "2.0", id, result });
}

function respondError(id, code, message) {
  return writeFrame({ jsonrpc: "2.0", id, error: { code, message } });
}

function progress(callId, message, done = false, isError = false) {
  if (!callId) throw new Error("progress requires a non-empty call_id");
  return writeFrame({
    jsonrpc: "2.0",
    method: "notifications/progress",
    params: { call_id: callId, message, done, is_error: isError },
  });
}

function delay(milliseconds, signal) {
  if (!milliseconds) return Promise.resolve();
  return new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, milliseconds);
    signal.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        reject(new Error("cancelled"));
      },
      { once: true },
    );
  });
}

async function callTool(request) {
  const params = request.params ?? {};
  if (params.name !== "echo") {
    await respondError(request.id, -32602, `unknown tool ${params.name}`);
    return;
  }
  const requestId = String(request.id);
  const callId = String(params.call_id ?? "");
  if (!callId) {
    await respondError(request.id, -32602, "tools/call requires a non-empty call_id");
    return;
  }
  const controller = new AbortController();
  const activeCall = { requestId, callId, controller };
  activeRequests.set(requestId, activeCall);
  activeCalls.set(callId, activeCall);
  const timeout = Number(params.timeout_ms) || 0;
  const timeoutHandle = timeout > 0 ? setTimeout(() => controller.abort(), timeout) : null;
  try {
    await progress(callId, "Preparing echo");
    await delay(Number(params.arguments?.delay_ms) || 0, controller.signal);
    if (controller.signal.aborted) return;
    const text = String(params.arguments?.text ?? "");
    await respond(request.id, {
      content: [{ type: "text", text }],
      details: { runtime: "node", length: text.length },
      is_error: false,
    });
  } catch (error) {
    if (!controller.signal.aborted) {
      await respondError(request.id, -32000, String(error?.message ?? error));
    }
  } finally {
    if (timeoutHandle) clearTimeout(timeoutHandle);
    activeRequests.delete(requestId);
    activeCalls.delete(callId);
  }
}

async function dispatch(message) {
  if (message.method === "notifications/cancelled") {
    const requestId = String(message.params?.request_id ?? "");
    const callId = String(message.params?.call_id ?? "");
    const activeCall = activeRequests.get(requestId) ?? activeCalls.get(callId);
    if (activeCall) activeCall.controller.abort();
    return;
  }
  if (message.method === "notifications/event") {
    // Observation delivery is best effort. This example intentionally does
    // nothing with the subscribed tool_end event.
    return;
  }
  if (message.id === undefined || message.id === null) return;

  switch (message.method) {
    case "initialize":
      await respond(message.id, {
        manifest,
        supported_events: ["tool_end"],
      });
      break;
    case "tools/list":
      await respond(message.id, { tools });
      break;
    case "tools/call":
      await callTool(message);
      break;
    case "shutdown":
      closing = true;
      for (const call of new Set(activeRequests.values())) call.controller.abort();
      await respond(message.id, {});
      input.close();
      break;
    default:
      await respondError(message.id, -32601, `unknown method ${message.method}`);
  }
}

input.on("line", (line) => {
  if (closing || line.trim() === "") return;
  let message;
  try {
    message = JSON.parse(line);
  } catch {
    void respondError(null, -32700, "parse error");
    return;
  }
  void dispatch(message).catch((error) => {
    console.error("snow plugin dispatch error:", error);
    process.exitCode = 1;
  });
});

input.on("close", () => {
  for (const call of new Set(activeRequests.values())) call.controller.abort();
});
