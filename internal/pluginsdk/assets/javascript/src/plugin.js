/**
 * Snow protocol-v2 plugin authoring surface.
 *
 * Defines the declarative `definePlugin` API, manifest/tool validation, and
 * lifecycle hooks; the runtime module owns framing and dispatch.
 */
import { ToolError } from "./errors.js";
import { errorResult, result as buildResult, textResult } from "./results.js";

const ID_RE = /^[a-z0-9][a-z0-9_-]{0,63}$/;
const TOOL_RE = /^[a-z][a-z0-9_-]{0,127}$/;
const RISKS = ["read", "write", "exec", "network"];
const DISCOVERY_MODES = ["always", "deferred"];

function assertPluginManifest(manifest) {
  if (typeof manifest !== "object" || manifest === null) throw new TypeError("plugin manifest must be an object");
  if (typeof manifest.id !== "string" || !ID_RE.test(manifest.id)) {
    throw new TypeError("plugin id must match ^[a-z0-9][a-z0-9_-]{0,63}$");
  }
  if (typeof manifest.name !== "string" || manifest.name.trim().length === 0) {
    throw new TypeError("plugin name must be a non-empty string");
  }
  if (typeof manifest.version !== "string" || manifest.version.trim().length === 0) {
    throw new TypeError("plugin version must be a non-empty string");
  }
}

export function defineTool(spec) {
  if (typeof spec !== "object" || spec === null) throw new TypeError("tool definition must be an object");
  if (typeof spec.name !== "string" || !TOOL_RE.test(spec.name)) {
    throw new TypeError("tool name must match ^[a-z][a-z0-9_-]{0,127}$");
  }
  if (typeof spec.description !== "string" || spec.description.trim().length === 0) {
    throw new TypeError("tool description must be a non-empty string");
  }
  const risk = spec.risk ?? "exec";
  if (!RISKS.includes(risk)) throw new TypeError(`tool risk must be one of ${RISKS.join(", ")}`);
  const parameters = spec.parameters ?? { type: "object", properties: {}, additionalProperties: false };
  if (typeof parameters !== "object" || parameters === null || parameters.type !== "object") {
    throw new TypeError("tool parameters must be a JSON Schema object");
  }
  if (spec.discovery !== undefined) {
    if (typeof spec.discovery !== "object" || spec.discovery === null || Array.isArray(spec.discovery)) {
      throw new TypeError("tool discovery must be an object");
    }
    const mode = spec.discovery.mode ?? "always";
    if (!DISCOVERY_MODES.includes(mode)) throw new TypeError(`tool discovery mode must be ${DISCOVERY_MODES.join(", ")}`);
  }
  if (typeof spec.execute !== "function") throw new TypeError("tool execute handler must be a function");
  return { ...spec, name: spec.name, description: spec.description, risk, parameters };
}

export function definePlugin({ manifest, tools = [], events = {}, setup, shutdown } = {}) {
  assertPluginManifest(manifest);
  if (!Array.isArray(tools)) throw new TypeError("tools must be an array");
  const defined = tools.map(defineTool);
  const names = new Set();
  for (const tool of defined) {
    if (names.has(tool.name)) throw new TypeError(`duplicate tool name ${JSON.stringify(tool.name)}`);
    names.add(tool.name);
  }
  if ((events !== undefined && (typeof events !== "object" || events === null)) || Array.isArray(events)) {
    throw new TypeError("events must be an object keyed by event type");
  }
  for (const [type, handler] of Object.entries(events ?? {})) {
    if (typeof handler !== "function") throw new TypeError(`event handler ${JSON.stringify(type)} must be a function`);
  }
  if (setup !== undefined && typeof setup !== "function") throw new TypeError("setup must be a function");
  if (shutdown !== undefined && typeof shutdown !== "function") throw new TypeError("shutdown must be a function");
  return {
    manifest: { ...manifest, protocol_version: 2 },
    tools: defined,
    events: { ...events },
    setup,
    shutdown,
    _state: undefined,
    _setupDone: false,
  };
}

export function toolDescriptor(tool) {
  const descriptor = {
    name: tool.name,
    description: tool.description,
    risk: tool.risk,
    parameters: tool.parameters,
  };
  if (Array.isArray(tool.capabilities) && tool.capabilities.length > 0) descriptor.capabilities = [...tool.capabilities];
  if (tool.discovery !== undefined) descriptor.discovery = tool.discovery;
  return descriptor;
}

export function normalizeResult(value) {
  if (value === null || value === undefined) return errorResult("tool returned no result");
  if (typeof value === "string") return textResult(value);
  if (typeof value === "object" && !Array.isArray(value)) {
    if ("content" in value && "is_error" in value) return value;
    if ("text" in value) return textResult(String(value.text), { details: value.details });
    return errorResult("tool result object requires a text field");
  }
  return errorResult(`unexpected tool result type ${typeof value}`);
}

export { ToolError };
export { buildResult as result, textResult, errorResult };
