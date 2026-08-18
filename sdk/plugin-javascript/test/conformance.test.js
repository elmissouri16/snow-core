import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

import { fileURLToPath } from "node:url";
import { ToolError } from "../src/index.js";
const pluginRoot = fileURLToPath(new URL("../", import.meta.url)).replace(/[\/]+$/, "");

const pluginSource = `import { definePlugin, defineTool, serve, textResult, ToolError } from "${pluginRoot}/src/index.js";

const plugin = definePlugin({
  manifest: { id: "conformance-js", name: "Conformance JS", version: "1.0.0" },
  setup(context) {
    if (context.sessionId !== "session-js" || context.hostVersion !== "test-host" || context.hostCapabilities[0] !== "tools") throw new Error("bad setup context");
    return { ready: true };
  },
  tools: [
    defineTool({
      name: "echo", description: "Echo text", risk: "read",
      parameters: { type: "object", properties: { text: { type: "string" }, delay_ms: { type: "integer" } }, required: ["text"], additionalProperties: false },
      async execute(args, ctx) {
        console.log("handler diagnostic");
        await ctx.progress("Preparing echo");
        await ctx.log("debug", "echo starting");
        if (Number(args.delay_ms) > 0) await new Promise((resolve) => setTimeout(resolve, Number(args.delay_ms)));
        ctx.signal.throwIfAborted();
        return textResult(String(args.text), { details: { runtime: "node", length: String(args.text).length, config: ctx.config.marker, state: ctx.state.ready } });
      },
    }),
    defineTool({
      name: "fail", description: "Expected failure", risk: "read",
      parameters: { type: "object", properties: {}, additionalProperties: false },
      execute() { throw new ToolError("record not found"); },
    }),
    defineTool({
      name: "boom", description: "Unexpected failure", risk: "read",
      parameters: { type: "object", properties: {}, additionalProperties: false },
      execute() { throw new Error("secret explosion"); },
    }),
    defineTool({
      name: "wait", description: "Cancellable wait", risk: "read",
      parameters: { type: "object", properties: {}, additionalProperties: false },
      async execute(_args, ctx) {
        await new Promise((resolve, reject) => {
          const timer = setTimeout(() => { clearTimeout(timer); resolve(); }, 10000);
          ctx.signal.addEventListener("abort", () => { clearTimeout(timer); reject(new Error("cancelled")); }, { once: true });
        });
        return textResult("done");
      },
    }),
  ],
});

await serve(plugin);
`;

function spawnFixture() {
  return mkdtemp(join(tmpdir(), "snow-plugin-js-")).then(async (d) => {
    const script = join(d, "plugin.mjs");
    await writeFile(script, pluginSource);
    const child = spawn(process.execPath, [script], { cwd: join(import.meta.dirname, ".."), stdio: ["pipe", "pipe", "pipe"] });
    const pending = new Map();
    const waiters = [];
    let buf = "";
    let nextId = 1;
    let stderr = "";
    child.stderr.on("data", (chunk) => { stderr += chunk; });
    child.stdout.on("data", (chunk) => {
      buf += chunk.toString();
      let idx;
      while ((idx = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, idx);
        buf = buf.slice(idx + 1);
        if (!line.trim()) continue;
        let frame;
        try { frame = JSON.parse(line); } catch { continue; }
        for (const waiter of waiters.splice(0)) {
          if (waiter.pred(frame)) { waiter.resolve(frame); return; }
        }
        if (frame.id !== undefined && pending.has(String(frame.id))) {
          const { resolve } = pending.get(String(frame.id));
          pending.delete(String(frame.id));
          resolve(frame);
        }
      }
    });
    const write = (obj) => new Promise((res, rej) => child.stdin.write(`${JSON.stringify(obj)}\n`, (e) => (e ? rej(e) : res())));
    const request = (method, params = {}, timeoutMs = 3000) => {
      const id = `req-${nextId++}`;
      const frame = new Promise((resolve, reject) => {
        pending.set(id, { resolve, reject });
        setTimeout(() => { if (pending.delete(id)) reject(new Error("timeout waiting for response")); }, timeoutMs);
      });
      write({ jsonrpc: "2.0", id, method, params }).catch(() => {});
      return frame;
    };
    const waitFor = (pred, ms = 3000) => new Promise((resolve, reject) => {
      const waiter = { pred, resolve, reject };
      waiters.push(waiter);
      setTimeout(() => {
        const i = waiters.indexOf(waiter);
        if (i >= 0) { waiters.splice(i, 1); reject(new Error("timeout waiting for frame")); }
      }, ms);
    });
    return { child, dir: d, getStderr: () => stderr, write, request, waitFor, script };
  });
}

test("ToolError enforces its provider-facing byte bound", () => {
  assert.throws(() => new ToolError("x".repeat(4097)), /4096-byte bound/);
});

test("conformance: protocol v2 lifecycle, tools, errors, cancellation, shutdown", async () => {
  const fixture = await spawnFixture();
  try {
    const init = fixture.request("initialize", { protocol_version: 2, config: { marker: "private" }, host_capabilities: ["tools"], session_id: "session-js", host_version: "test-host" });
    const initFrame = await init;
    assert.equal(initFrame.result.manifest.id, "conformance-js");
    assert.equal(initFrame.result.manifest.protocol_version, 2);

    const list = await fixture.request("tools/list", {});
    assert.equal(list.result.tools.length, 4);
    assert.equal(list.result.tools[0].name, "echo");
    assert.equal(list.result.tools[0].risk, "read");

    const call = await fixture.request("tools/call", { name: "echo", call_id: "call-1", arguments: { text: "hello", delay_ms: 20 } });
    assert.equal(call.result.content[0].text, "hello");
    assert.equal(call.result.details.runtime, "node");
    assert.equal(call.result.details.config, "private");
    assert.equal(call.result.details.state, true);
    assert.equal(call.result.is_error, false);
    assert.match(fixture.getStderr(), /handler diagnostic/);

    const fail = await fixture.request("tools/call", { name: "fail", call_id: "call-2", arguments: {} });
    assert.equal(fail.result.is_error, true);
    assert.match(fail.result.content[0].text, /record not found/);

    const boom = await fixture.request("tools/call", { name: "boom", call_id: "call-3", arguments: {} });
    assert.equal(boom.error.code, -32000);
    assert.equal(boom.error.message, "tool failed: Error");
    assert.doesNotMatch(JSON.stringify(boom), /secret explosion/);

    const cancelled = fixture.request(
      "tools/call",
      { name: "wait", call_id: "call-4", arguments: {} },
      300,
    );
    await fixture.write({
      jsonrpc: "2.0",
      method: "notifications/cancelled",
      params: { call_id: "call-4", request_id: "req-6", reason: "test" },
    });
    await assert.rejects(cancelled, /timeout waiting for response/);

    const progress = fixture.waitFor((f) => f.method === "notifications/progress");
    fixture.request("tools/call", { name: "echo", call_id: "call-5", arguments: { text: "prog" } }).catch(() => {});
    const progressFrame = await progress;
    assert.equal(progressFrame.params.call_id, "call-5");

    const shutdownCall = fixture.request(
      "tools/call",
      { name: "wait", call_id: "call-6", arguments: {} },
      300,
    );
    await new Promise((resolve) => setTimeout(resolve, 20));
    const shutdown = fixture.request("shutdown", {});
    const shutdownFrame = await shutdown;
    assert.deepEqual(shutdownFrame.result, {});
    await assert.rejects(shutdownCall, /timeout waiting for response/);
  } finally {
    const { child } = fixture;
    child.stdin.end();
    await new Promise((resolve) => {
      const timer = setTimeout(() => { child.kill("SIGKILL"); resolve(); }, 3000);
      child.on("close", () => { clearTimeout(timer); resolve(); });
    });
    await rm(fixture.dir, { recursive: true, force: true });
  }
});
