import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

import { Snow, SnowCommandError } from "../src/index.js";

const binary = process.env.SNOW_TEST_BINARY;

test("real Snow fake-provider lifecycle", { skip: !binary }, async () => {
  const directory = await mkdtemp(join(tmpdir(), "snow-js-sdk-"));
  const snow = await Snow.start({
    executable: binary,
    cwd: directory,
    env: { SNOW_HOME: join(directory, "snow-home") },
    startupTimeoutMs: 10_000,
    requestTimeoutMs: 30_000,
  });
  try {
    assert.equal(snow.ready.protocol_version, "1");
    assert(snow.ready.snow_version);
    assert.equal((await snow.sessionInfo()).data.provider, "fake");
    assert(snow.ready.capabilities.includes("multimodal_prompts"));
    const info = (await snow.sessionInfo()).data;
    assert.equal((await snow.models()).data.models.length > 0, true);
    const active = (await snow.models()).data.models.find((model) => model.id === info.model) ?? {};
    const image = { type: "image", mime_type: "image/png", data: "AAAA" };
    if (active.supports_vision) {
      assert.equal((await snow.prompt("", { content: [image] })).status, "completed");
    } else {
      await assert.rejects(snow.prompt("", { content: [image] }), SnowCommandError);
    }
    assert.equal((await snow.prompt("JavaScript SDK integration smoke")).status, "completed");

    assert(snow.ready.capabilities.includes("compaction"));
    assert(snow.ready.capabilities.includes("response_controls"));
    await snow.setReasoningSummary("concise");
    await snow.setTextVerbosity("high");
    const later = (await snow.sessionInfo()).data;
    assert.equal(later.reasoning_summary, "concise");
    assert.equal(later.text_verbosity, "high");

    const messages = (await snow.messages()).data.messages;
    assert(messages.length >= 2);
    for (const message of messages) {
      assert(!message.content.some((block) => block.type === "provider_data"));
    }
    const usage = (await snow.usage()).data;
    assert.equal(typeof usage.total_tokens, "number");
    assert.equal(typeof usage.input, "number");
    assert.deepEqual((await snow.pendingInputs()).data.items, []);
    assert.deepEqual((await snow.clearPendingInputs()).data.items, []);
    assert(Array.isArray((await snow.configurationDiagnostics()).data.diagnostics));
    assert(Array.isArray((await snow.mcpServers()).data.servers));
    assert(Array.isArray((await snow.skills()).data.skills));
    assert.equal(typeof (await snow.sandboxStatus()).data.status.configured, "boolean");

    const branches = (await snow.branches()).data.branches;
    const main = branches.find((branch) => branch.active);
    assert(main);
    const child = (await snow.branchFork({ name: "javascript-sdk-child" })).data;
    assert.equal((await snow.branchRename(child.id, "javascript-sdk-renamed")).data.name, "javascript-sdk-renamed");
    await snow.branchSelect(main.id);
    await snow.branchDelete(child.id);
    assert(!(await snow.branches()).data.branches.some((branch) => branch.id === child.id));

    const compacted = (await snow.compact()).data;
    assert.equal(typeof compacted.summarized_messages, "number");
    assert.equal(typeof compacted.retained_messages, "number");
  } finally {
    await snow.close();
    await rm(directory, { recursive: true, force: true });
  }
});
