/**
 * Public result builders for Snow tool calls.
 *
 * Snow's tool result contract is:
 *
 *   { content: [...], details: {...}, is_error: false }
 *
 * `content` is the provider-facing content block list; `details` is private
 * host metadata that Snow preserves but never surfaces to the model or logs.
 * The builders below are the portable way to produce results.
 */

export const MAX_TEXT_BYTES = 256 * 1024; // 256 KiB host output bound
export const MAX_ERROR_TEXT_BYTES = 4096;

function utf8ByteLength(value) {
  return Buffer.byteLength(value, "utf8");
}

function blockText(text) {
  if (typeof text !== "string" || text.trim().length === 0) {
    throw new TypeError("text_result requires a non-empty string");
  }
  if (utf8ByteLength(text) > MAX_TEXT_BYTES) {
    throw new RangeError(`text result exceeds the ${MAX_TEXT_BYTES}-byte bound`);
  }
  return { type: "text", text };
}

/**
 * Build a validated tool result from a non-empty content block list.
 * `details` is preserved as private host metadata and never provider-facing.
 */
export function result(content, { details, is_error = false } = {}) {
  if (!Array.isArray(content) || content.length === 0) {
    throw new TypeError("result requires a non-empty content block list");
  }
  for (const block of content) {
    if (typeof block !== "object" || block === null || typeof block.type !== "string" || block.type.length === 0) {
      throw new TypeError("result content blocks must be objects with a string type");
    }
  }
  if (details !== undefined && (typeof details !== "object" || details === null || Array.isArray(details))) {
    throw new TypeError("result details must be a JSON object");
  }
  return { content, details: details ?? {}, is_error: Boolean(is_error) };
}

/** Return a simple text tool result with optional private details. */
export function textResult(text, options = {}) {
  return result([blockText(text)], options);
}

/** Return a structured, bounded tool error while completing the JSON-RPC request. */
export function errorResult(message, options = {}) {
  const text = String(message);
  if (text.trim().length === 0) throw new TypeError("error_result requires a non-empty string message");
  if (utf8ByteLength(text) > MAX_ERROR_TEXT_BYTES) {
    throw new RangeError(`error_result message exceeds the ${MAX_ERROR_TEXT_BYTES}-byte bound`);
  }
  return textResult(text, { ...options, is_error: true });
}
