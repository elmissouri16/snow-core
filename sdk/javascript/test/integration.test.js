import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";

import { Snow } from "../src/index.js";

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
    assert((await snow.models()).data.models.length > 0);
    assert.equal((await snow.prompt("JavaScript SDK integration smoke")).status, "completed");
  } finally {
    await snow.close();
    await rm(directory, { recursive: true, force: true });
  }
});
