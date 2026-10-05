import { createHash } from 'node:crypto';
import { readFile, readdir, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { compile } from 'json-schema-to-typescript';

const schemaDir = new URL('../../../pkg/protocol/schema/rpc/v1/', import.meta.url);
const generated = new URL('../src/generated/rpc.ts', import.meta.url);
const canonicalBase = 'https://snow-core.dev/schemas/rpc/v1/';
const files = (await readdir(schemaDir)).filter((name) => name.endsWith('.schema.json')).sort();
const schemas = new Map();
const digest = createHash('sha256');
for (const file of files) {
  const text = await readFile(new URL(file, schemaDir), 'utf8');
  digest.update(file).update('\0').update(text).update('\0');
  schemas.set(file, JSON.parse(text));
}
const key = (file) => file.replace('.schema.json', '').replaceAll('-', '_');

// Flatten only canonical, repository-local references into one document. The
// compiler's network and filesystem resolvers are disabled as a second guard.
function localize(value, owner) {
  if (Array.isArray(value)) return value.map((entry) => localize(entry, owner));
  if (!value || typeof value !== 'object') return value;
  const result = {};
  for (const [name, entry] of Object.entries(value)) {
    if (name === '$id' || name === '$schema') continue;
    if (name !== '$ref') {
      result[name] = localize(entry, owner);
      continue;
    }
    const [target, fragment = ''] = entry.split('#');
    const file = target ? target.startsWith(canonicalBase) ? target.slice(canonicalBase.length) : target : owner;
    if (!schemas.has(file) || (fragment && !fragment.startsWith('/'))) {
      throw new Error(`Nonlocal or unsupported schema reference: ${owner}: ${entry}`);
    }
    result.$ref = `#/$defs/${key(file)}${fragment}`;
  }
  return result;
}
const definitions = Object.fromEntries(files.map((file) => [key(file), localize(schemas.get(file), file)]));
function dereference(schema) {
  if (!schema.$ref) return schema;
  let value = { $defs: definitions };
  for (const segment of schema.$ref.slice(2).split('/')) {
    value = value[segment.replaceAll('~1', '/').replaceAll('~0', '~')];
  }
  return dereference(value);
}
// Some DTO documents are aliases of their own $defs. Resolve those root
// aliases before compilation; retaining a root $ref beside $defs confuses
// the compiler when that same DTO is reached from an allOf and directly.
for (const [name, schema] of Object.entries(definitions)) {
  if (schema.$ref) {
    const target = dereference(schema);
    const { $ref: _ref, ...metadata } = schema;
    definitions[name] = { ...target, ...metadata };
  }
}
function discriminators(branch, property) {
  const field = dereference(branch).properties?.[property];
  return field?.const !== undefined ? [field.const] : field?.enum ?? [];
}
const pascal = (name) => name.split('_').map((word) => word[0].toUpperCase() + word.slice(1)).join('');
function narrow(branch, field, value, title) {
  const clone = structuredClone(dereference(branch));
  clone.properties[field] = { const: value };
  // Anonymous grouped branches get unique deterministic names after expansion.
  clone.title = title;
  return clone;
}
function object(properties) {
  return { type: 'object', properties, required: Object.keys(properties), additionalProperties: false };
}
const requestGroups = new Map();
for (const branch of definitions.request.oneOf) {
  const names = discriminators(branch, 'type');
  if (!names.length) throw new Error('Request branch lacks a finite command discriminator');
  for (const command of names) {
    const branches = requestGroups.get(command) ?? [];
    branches.push(narrow(branch, 'type', command, `WireRequest${pascal(command)}`));
    requestGroups.set(command, branches);
  }
}
const responseBranches = definitions.response.oneOf.map(dereference);
const requestMap = {};
const responseMap = {};
for (const command of [...requestGroups.keys()].sort()) {
  const branches = requestGroups.get(command);
  requestMap[command] = branches.length === 1 ? branches[0] : { anyOf: branches };
  const failures = responseBranches.filter((branch) => branch.properties.success.const === false);
  let successes = responseBranches.filter((branch) => branch.properties.success.const === true && discriminators(branch, 'command').includes(command));
  // The schema's generic branch has a `not: {enum: ...}` guard. Never let
  // that broad fallback overshadow an explicit, typed success branch.
  if (!successes.length) {
    successes = responseBranches.filter((branch) => branch.properties.success.const === true &&
      !discriminators(branch, 'command').length && !branch.properties.command.not?.enum?.includes(command));
  }
  if (!successes.length) throw new Error(`No success response branch for ${command}`);
  responseMap[command] = {
    anyOf: [...successes, ...failures].map((branch) => narrow(branch, 'command', command, undefined)),
  };
}
const ref = (file) => ({ $ref: `#/$defs/${key(file + '.schema.json')}` });
const umbrella = {
  title: 'GeneratedRpcTypes',
  ...object({
    Request: ref('request'), Response: ref('response'), Ready: ref('handshake'),
    PromptCompleted: ref('prompt-completed'), Output: ref('output'),
    AgentEvent: ref('agent-event'), Model: ref('model'), Message: ref('message'),
    SessionSummary: ref('session-summary'), SessionInfo: ref('session-info'),
    RequestMap: object(requestMap), ResponseMap: object(responseMap),
  }),
  $defs: definitions,
};
const result = await compile(umbrella, 'GeneratedRpcTypes', {
  cwd: fileURLToPath(schemaDir),
  bannerComment: `/* Generated by sdk/node/scripts/generate.mjs; do not edit.\n * Source: pkg/protocol/schema/rpc/v1 (SHA-256 ${digest.digest('hex')}).\n * Static TypeScript projections are not runtime JSON Schema validators. */`,
  unknownAny: true,
  additionalProperties: false,
  $refOptions: { resolve: { http: false, file: false } },
  style: { singleQuote: true, printWidth: 100 },
});
if (process.argv.includes('--check')) {
  const checkedIn = await readFile(generated, 'utf8').catch(() => '');
  if (checkedIn !== result) throw new Error('Generated RPC types are stale; run npm run generate');
  console.log('Generated RPC types match canonical local schemas.');
} else {
  await writeFile(generated, result);
  console.log(`Generated ${requestGroups.size} eager RPC command mappings.`);
}
