// Expected-failure regression: a bad browser prerequisite must invalidate an
// older successful artifact and must never print a successful workflow result.
import {spawnSync} from "node:child_process";
import {mkdtemp, readFile, rm, mkdir, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join, dirname, resolve} from "node:path";
import {fileURLToPath} from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repository = resolve(here, "../../../..");
const directory = await mkdtemp(join(tmpdir(), "snow-permission-missing-browser-"));
const artifacts = join(directory, "evidence");
try {
  await mkdir(artifacts, {recursive: true});
  await writeFile(join(artifacts, "report.json"), JSON.stringify({status: "passed", assertions: 999999, failures: 0}));
  const child = spawnSync(process.execPath, [join(here, "run.mjs"), "--output-dir", artifacts], {
    cwd: repository, env: {...process.env, SNOW_CHROME_BIN: join(directory, "missing-chrome")},
    encoding: "utf8", timeout: 240000, maxBuffer: 65536
  });
  if (child.error || child.status !== 1 || !child.stderr.includes("Chrome not found") || child.stdout.includes("assertions passed")) {
    throw Error("Expected missing-browser failure was not reported correctly");
  }
  const report = JSON.parse(await readFile(join(artifacts, "report.json"), "utf8"));
  if (report.status !== "failed" || report.failures !== 1 || "assertions" in report || !report.startedAt) {
    throw Error("Failed run retained stale success evidence");
  }
  console.log("permission-workflow failure-report: expected prerequisite failure invalidated old success");
} finally {
  await rm(directory, {recursive: true, force: true});
}
