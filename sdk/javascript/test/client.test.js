import assert from "node:assert/strict";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import {
  Snow,
  SnowCancelledError,
  SnowClosedError,
  SnowCommandError,
  SnowProcessError,
  SnowPromptError,
  SnowProtocolError,
  SnowSubscriptionOverflowError,
  SnowTimeoutError,
  SnowVersionError,
} from "../src/index.js";

const fixture = fileURLToPath(new URL("./fixtures/fake-snow.mjs", import.meta.url));
const fixtureOptions = {
  executable: process.execPath,
  executableArgs: [fixture],
  startupTimeoutMs: 3_000,
  requestTimeoutMs: 3_000,
  closeTimeoutMs: 1_000,
};

async function nextEvent(iterator, type) {
  for await (const event of iterator) {
    if (event.type === type) return event;
  }
  throw new Error(`event iterator closed before ${type}`);
}

test("handshake, discovery, and definitive prompt completion", async () => {
  const snow = await Snow.start(fixtureOptions);
  try {
    assert.equal(snow.ready.protocol_version, "1");
    assert(snow.ready.capabilities.includes("prompt_completion"));
    assert.equal((await snow.sessionInfo()).data.model, "fake-1");
    assert.equal(snow.diagnostics[0].kind, "unknown_response");
    assert.equal((await snow.models()).data.models[0].id, "fake-1");

    const seen = [];
    const unsubscribe = snow.subscribe((event) => seen.push(event.type));
    const result = await snow.prompt("hello");
    unsubscribe();
    assert.equal(result.status, "completed");
    assert(seen.includes("text_delta"));
    assert(seen.includes("turn_done"));
  } finally {
    await snow.close();
  }
});

test("event subscribers are isolated and callback queues overflow explicitly", async () => {
  const snow = await Snow.start(fixtureOptions);
  try {
    const first = snow.events();
    const second = snow.events();
    const prompt = snow.prompt("hello");
    const left = await nextEvent(first, "text_delta");
    const right = await nextEvent(second, "text_delta");
    left.text = "mutated";
    assert.equal(right.text, "fixture text");
    await first.return();
    await second.return();
    await prompt;

    const controller = new AbortController();
    controller.abort();
    const closed = snow.events({ signal: controller.signal });
    assert.equal((await closed.next()).done, true);
    assert.equal(snow._iterators.has(closed), false);

    let release;
    const gate = new Promise((resolve) => { release = resolve; });
    let overflowResolve;
    const overflowed = new Promise((resolve) => { overflowResolve = resolve; });
    const unsubscribe = snow.subscribe(async () => gate, {
      capacity: 1,
      onError: (error) => overflowResolve(error),
    });
    await snow.prompt("many");
    const error = await overflowed;
    assert(error instanceof SnowSubscriptionOverflowError);
    release();
    unsubscribe();
  } finally {
    await snow.close();
  }
});

test("out-of-order responses remain correlated", async () => {
  const snow = await Snow.start(fixtureOptions);
  try {
    const held = snow.request("hold");
    await new Promise((resolve) => setImmediate(resolve));
    const released = await snow.request("release");
    assert.equal(released.command, "release");
    assert.equal((await held).command, "hold");
  } finally {
    await snow.close();
  }
});

test("prompt content is sent on the wire and honors resource failure", async () => {
  const snow = await Snow.start(fixtureOptions);
  try {
    const result = await snow.prompt("", { content: [{ type: "image", mime_type: "image/png", data: "AAAA" }], mode: "plan" });
    assert.equal(result.status, "completed");
    assert(snow.ready.capabilities.includes("multimodal_prompts"));
    await assert.rejects(snow.prompt("", { content: [{ type: "text", text: "hi" }] }), SnowPromptError);
  } finally {
    await snow.close();
  }
});

test("turn_done does not hide terminal prompt failure", async () => {
  const snow = await Snow.start(fixtureOptions);
  try {
    await assert.rejects(snow.prompt("fail"), SnowPromptError);
  } finally {
    await snow.close();
  }
});

test("AbortSignal aborts a prompt and consumes terminal canceled status", async () => {
  const snow = await Snow.start(fixtureOptions);
  try {
    const controller = new AbortController();
    const prompt = snow.prompt("wait", { signal: controller.signal });
    await new Promise((resolve) => setTimeout(resolve, 25));
    controller.abort();
    await assert.rejects(prompt, SnowCancelledError);
    assert.equal((await snow.sessionInfo()).data.model, "fake-1");
  } finally {
    await snow.close();
  }
});

test("user input is published before the async handler replies", async () => {
  let observed = false;
  let handled = false;
  const snow = await Snow.start({
    ...fixtureOptions,
    userInputHandler: async (request) => {
      assert.equal(request.id, "ask-1");
      assert.equal(observed, true);
      handled = true;
      return { answers: [{ id: "choice", answer: "A" }] };
    },
  });
  const unsubscribe = snow.subscribe((event) => {
    if (event.type === "user_input_request") observed = true;
  });
  try {
    assert.equal((await snow.prompt("ask")).status, "completed");
    assert.equal(handled, true);
  } finally {
    unsubscribe();
    await snow.close();
  }
});

test("permission request is published before the async handler replies", async () => {
  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  let handled = false;
  const snow = await Snow.start({
    ...fixtureOptions,
    permissionHandler: async (request) => {
      assert.equal(request.id, "perm-handler-1");
      assert.equal(request.tool, "bash");
      handled = true;
      await gate;
      return "allow_session";
    },
  });
  const events = snow.events();
  try {
    assert(snow.ready.capabilities.includes("permission_interaction"));
    const prompt = snow.prompt("permission");
    const event = await nextEvent(events, "permission_request");
    assert.equal(event.permission.request.id, "perm-handler-1");
    assert.equal(handled, true);
    release();
    assert.equal((await prompt).status, "completed");
  } finally {
    await events.return();
    await snow.close();
  }
});

test("close aborts and joins model input handlers", async () => {
  let startedResolve;
  const started = new Promise((resolve) => { startedResolve = resolve; });
  let aborted = false;
  const snow = await Snow.start({
    ...fixtureOptions,
    userInputHandler: async (_request, { signal }) => {
      startedResolve();
      await new Promise((resolve, reject) => {
        signal.addEventListener("abort", () => {
          aborted = true;
          reject(new Error("closed"));
        }, { once: true });
      });
      return { answers: [] };
    },
  });
  const prompt = snow.prompt("ask").catch(() => {});
  await started;
  await snow.close();
  await prompt;
  assert.equal(aborted, true);
});

test("version, protocol, process, and overflow errors are typed", async () => {
  await assert.rejects(
    Snow.start({ ...fixtureOptions, env: { FAKE_SNOW_SCENARIO: "bad_version" } }),
    SnowVersionError,
  );
  for (const scenario of ["bad_version_type", "bad_snow_type", "bad_caps_type", "bad_max_type"]) {
    await assert.rejects(
      Snow.start({ ...fixtureOptions, env: { FAKE_SNOW_SCENARIO: scenario } }),
      SnowProtocolError,
    );
  }

  for (const scenario of ["malformed", "oversized"]) {
    const snow = await Snow.start({
      ...fixtureOptions,
      env: { FAKE_SNOW_SCENARIO: scenario },
      maxFrameBytes: 1024,
    });
    try {
      await new Promise((resolve) => setTimeout(resolve, 50));
      await assert.rejects(snow.sessionInfo(), SnowProtocolError);
    } finally {
      await snow.close();
    }
  }

  const limited = await Snow.start({ ...fixtureOptions, env: { FAKE_SNOW_SCENARIO: "small_limit" } });
  try {
    assert.equal(limited.ready.max_input_bytes, 128);
    await assert.rejects(limited.request("echo", { message: "x".repeat(256) }), SnowProtocolError);
    assert.equal((await limited.sessionInfo()).data.model, "fake-1");
  } finally {
    await limited.close();
  }

  for (const scenario of ["stdout_closed", "stdin_closed"]) {
    const snow = await Snow.start({ ...fixtureOptions, env: { FAKE_SNOW_SCENARIO: scenario } });
    try {
      await new Promise((resolve) => setTimeout(resolve, 50));
      await assert.rejects(snow.sessionInfo(), SnowProcessError);
    } finally {
      await snow.close();
    }
  }

  const commands = await Snow.start(fixtureOptions);
  try {
    await assert.rejects(commands.request("fail_command"), SnowCommandError);
    await assert.rejects(commands.request("hold", {}, { timeoutMs: 10 }), SnowTimeoutError);
    await commands.request("release");
    await assert.rejects(commands.prompt("wait", { timeoutMs: 10 }), SnowTimeoutError);
    assert.equal((await commands.sessionInfo()).data.model, "fake-1");
  } finally {
    await commands.close();
  }
  await assert.rejects(commands.sessionInfo(), SnowClosedError);

  const crashed = await Snow.start(fixtureOptions);
  try {
    await assert.rejects(
      crashed.request("crash"),
      (error) => error instanceof SnowProcessError && error.message.includes("fixture process failure"),
    );
  } finally {
    await crashed.close();
  }

  const overflow = await Snow.start(fixtureOptions);
  try {
    const events = overflow.events({ capacity: 1 });
    await overflow.prompt("hello");
    await assert.rejects(events.next(), SnowSubscriptionOverflowError);
  } finally {
    await overflow.close();
  }
});

test("parity command wrappers use the expected RPC frames and typed data", async () => {
  const snow = await Snow.start(fixtureOptions);
  try {
    const compact = await snow.compact();
    assert.equal(compact.command, "compact");
    assert.equal(compact.data.summarized_messages, 1);
    assert.equal(compact.data.retained_messages, 2);

    const branches = await snow.branches();
    assert.equal(branches.command, "branches_list");
    assert.equal(branches.data.branches[0].id, "branch-1");
    assert.equal((await snow.branchSelect("branch-1")).success, true);

    const renamed = await snow.branchRename("branch-1", "new name");
    assert.equal(renamed.command, "branch_rename");
    assert.equal(renamed.data.name, "new name");
    assert.equal((await snow.branchDelete("branch-1")).success, true);

    const messages = await snow.messages();
    assert.equal(messages.command, "messages_list");
    assert.equal(messages.data.messages[0].role, "user");
    assert.equal(messages.data.messages[0].content[0].text, "hello");

    const usage = await snow.usage();
    assert.equal(usage.data.total_tokens, 15);

    const pending = await snow.pendingInputs();
    assert.equal(pending.data.items[0].kind, "steer");
    const cleared = await snow.clearPendingInputs();
    assert.equal(cleared.data.items[0].text, "focus");

    const diagnostics = await snow.configurationDiagnostics();
    assert.equal(diagnostics.command, "diagnostics");
    assert.equal(diagnostics.data.diagnostics[0].path, "tui.theme");

    assert.equal((await snow.setReasoningSummary("concise")).command, "set_reasoning_summary");
    assert.equal((await snow.setTextVerbosity("high")).command, "set_text_verbosity");
    assert.equal((await snow.subagentClose("/root/reviewer")).command, "subagent_close");
    assert.equal((await snow.subagentResume("/root/reviewer")).command, "subagent_resume");

    const mcp = await snow.mcpServers();
    assert.equal(mcp.data.servers[0].id, "mcp-1");
    const skills = await snow.skills();
    assert.equal(skills.data.skills[0].name, "caveman");
    assert.equal(skills.data.diagnostics[0].level, "error");

    assert.equal((await snow.replyPermission("perm-1", "allow")).command, "permission_reply");
    assert.equal((await snow.rejectPermission("perm-1")).command, "permission_reject");
  } finally {
    await snow.close();
  }
});

test("safe defaults require an external binary and never put keys in argv", () => {
  assert.throws(() => new Snow({ maxFrameBytes: 1.5 }), RangeError);
  assert.throws(() => new Snow({ eventQueueSize: 1.5 }), RangeError);
  const snow = new Snow({ executable: "/opt/snow" });
  const args = snow._snowArgs();
  for (const flag of ["--permission", "--thinking", "--no-session", "--no-plugins", "--no-mcp", "--no-skills", "--no-subagents"]) {
    assert(args.includes(flag), flag);
  }
  assert(!args.includes("--api-key"));
});
