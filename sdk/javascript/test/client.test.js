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
