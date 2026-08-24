import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const declarationsURL = new URL("../src/index.d.ts", import.meta.url);

test("ContentBlock declarations do not merge incompatible prompt and message types", async () => {
  const source = await readFile(declarationsURL, "utf8");
  assert.equal(source.match(/export interface ContentBlock\s*\{/g)?.length, 1);
  assert.match(source, /export interface PromptContentBlock extends ContentBlock\s*\{\s*type: "text" \| "image";/);
  assert.match(source, /content\?: PromptContentBlock\[\];/);
});
