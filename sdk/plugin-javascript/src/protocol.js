/**
 * Wire-level JSON-RPC 2.0 constants and validation helpers for Snow plugins.
 *
 * This module is intentionally small and dependency-free. It implements the
 * exact message shapes Snow's protocol-v2 accepts; it is not a general
 * JSON-RPC framework.
 */

export const PROTOCOL_VERSION = 2;
export const MAX_FRAME_BYTES = 4 * 1024 * 1024; // 4 MiB upper bound (host default)

/** Validate a parsed request; returns an error string or empty string. */
export function validateRequest(message) {
  if (typeof message !== "object" || message === null || Array.isArray(message)) {
    return "invalid request: not an object";
  }
  if (message.jsonrpc !== "2.0") return 'invalid request: jsonrpc must be "2.0"';
  if (typeof message.method !== "string" || message.method.length === 0) {
    return "invalid request: missing string method";
  }
  if ("id" in message && (typeof message.id !== "string" || message.id.length === 0)) {
    return "invalid request: id must be a non-empty string";
  }
  return "";
}

/** Encode one JSON object as UTF-8 plus a trailing LF. */
export function encodeFrame(payload) {
  const encoded = Buffer.from(`${JSON.stringify(payload)}\n`, "utf8");
  if (encoded.byteLength > MAX_FRAME_BYTES) {
    throw new RangeError(`frame exceeds the ${MAX_FRAME_BYTES}-byte bound`);
  }
  return encoded;
}
