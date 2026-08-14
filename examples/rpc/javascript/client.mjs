#!/usr/bin/env node
import { resolve } from "node:path";

import { Snow } from "../../../sdk/javascript/src/index.js";

const executable = resolve(process.argv[2] ?? "./snow");
const provider = process.env.SNOW_PROVIDER ?? "fake";
const prompt = process.argv[3] ?? "Summarize this repository.";

const snow = await Snow.start({ executable, provider });
let sawText = false;
let rejectEvents;
const eventFailure = new Promise((_, reject) => { rejectEvents = reject; });
const unsubscribe = snow.subscribe((event) => {
  if (event.type === "text_delta" && !event.agent) {
    sawText = true;
    process.stdout.write(event.text ?? "");
  }
}, { onError: rejectEvents });

try {
  const info = await snow.sessionInfo();
  if (info.data.provider !== provider) throw new Error("unexpected provider in session_info");
  await Promise.race([snow.prompt(prompt), eventFailure]);
  if (sawText) process.stdout.write("\n");
  else process.stdout.write("agent turn completed (the fake provider emits no text)\n");
} finally {
  unsubscribe();
  await snow.close();
}
