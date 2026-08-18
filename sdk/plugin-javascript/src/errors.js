/**
 * Expected, bounded failure types for the Snow protocol-v2 plugin runtime.
 *
 * `ToolError` is the single public expected-error type. Raising it from a tool
 * handler produces a structured `is_error` tool result instead of failing the
 * JSON-RPC request. Unexpected exceptions are reported as bounded JSON-RPC
 * errors on the wire.
 */
export class PluginError extends Error {
  constructor(message, code) {
    super(message);
    this.name = new.target.name;
    this.code = code;
  }
}

export class ToolError extends PluginError {
  constructor(message) {
    super(message, "TOOL_ERROR");
  }
}

export class ProtocolError extends PluginError {
  constructor(message) {
    super(message, "PROTOCOL_ERROR");
  }
}

export class BoundsError extends PluginError {
  constructor(message) {
    super(message, "BOUNDS_ERROR");
  }
}
