// Dependency-free Snow protocol-v2 template. Replace PLUGIN_ID and echo.
import * as readline from "node:readline";

const manifest = {
  id: "PLUGIN_ID",
  name: "PLUGIN_ID generated plugin",
  version: "0.1.0",
  protocol_version: 2,
};

const tools = [
  {
    name: "echo",
    description: "Replace this example with the reusable capability.",
    parameters: {
      type: "object",
      properties: { text: { type: "string" } },
      required: ["text"],
      additionalProperties: false,
    },
    risk: "read",
  },
];

let writes = Promise.resolve();
function send(frame) {
  const line = `${JSON.stringify(frame)}\n`;
  writes = writes.then(
    () => new Promise((resolve, reject) => process.stdout.write(line, (err) => err ? reject(err) : resolve())),
  );
  return writes;
}

function result(id, value) {
  return send({ jsonrpc: "2.0", id, result: value });
}

function error(id, code, message) {
  return send({ jsonrpc: "2.0", id, error: { code, message: String(message).slice(0, 4096) } });
}

async function dispatch(request, input) {
  if (request.id === undefined || request.id === null) {
    // Add notifications/cancelled handling here for long-running calls.
    return;
  }
  switch (request.method) {
    case "initialize":
      await result(request.id, { manifest, supported_events: [] });
      break;
    case "tools/list":
      await result(request.id, { tools });
      break;
    case "tools/call": {
      const params = request.params ?? {};
      if (params.name !== "echo") {
        await error(request.id, -32602, "unknown tool");
        break;
      }
      await result(request.id, {
        content: [{ type: "text", text: String(params.arguments?.text ?? "") }],
        details: { template: true },
        is_error: false,
      });
      break;
    }
    case "shutdown":
      await result(request.id, {});
      input.close();
      break;
    default:
      await error(request.id, -32601, "method not found");
  }
}

const input = readline.createInterface({ input: process.stdin, crlfDelay: Infinity });
input.on("line", (line) => {
  if (!line.trim()) return;
  let request;
  try {
    request = JSON.parse(line);
  } catch {
    void error(null, -32700, "parse error");
    return;
  }
  void dispatch(request, input).catch((err) => {
    console.error("plugin dispatch error:", err);
    process.exitCode = 1;
  });
});
