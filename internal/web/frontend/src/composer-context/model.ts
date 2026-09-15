import type {Item} from "./types.ts";
export const TEXT_LIMIT = 64 * 1024, PROMPT_LIMIT = 128 * 1024, IMAGE_LIMIT = 2 * 1024 * 1024;
const encoder = new TextEncoder();
export const bytes = (text: string) => encoder.encode(text).length;
export function validText(text: unknown): text is string {
  if (typeof text !== "string" || text.includes("\0")) return false;
  for (let i = 0; i < text.length; i++) {
    const code = text.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = text.charCodeAt(++i); if (!(next >= 0xdc00 && next <= 0xdfff)) return false;
    } else if (code >= 0xdc00 && code <= 0xdfff) return false;
  }
  return true;
}
export function imageType(data: Uint8Array) {
  const starts = (values: number[]) => values.every((value, index) => data[index] === value);
  if (starts([137, 80, 78, 71, 13, 10, 26, 10])) return "image/png";
  if (starts([255, 216, 255])) return "image/jpeg";
  const ascii = (start: number, end: number) => String.fromCharCode(...data.subarray(start, end));
  if (["GIF87a", "GIF89a"].includes(ascii(0, 6))) return "image/gif";
  if (ascii(0, 4) === "RIFF" && ascii(8, 12) === "WEBP") return "image/webp";
  return "";
}
export function previewDimensionsSafe(data: Uint8Array, mime: string) {
  // Inspect only bounded headers before giving a browser decoder any bytes.
  // These canvas limits mirror validComposerImage; this is not full image
  // validation, and a missing/malformed header only suppresses the preview.
  const ascii = (start: number, end: number) => String.fromCharCode(...data.subarray(start, end));
  const be16 = (offset: number) => data[offset] * 256 + data[offset + 1];
  const le16 = (offset: number) => data[offset] + data[offset + 1] * 256;
  const be32 = (offset: number) => be16(offset) * 65536 + be16(offset + 2);
  const le24 = (offset: number) => le16(offset) + data[offset + 2] * 65536;
  const le32 = (offset: number) => le16(offset) + le16(offset + 2) * 65536;
  let width = 0, height = 0;
  if (mime === "image/png") {
    if (data.length < 33 || be32(8) !== 13 || ascii(12, 16) !== "IHDR") return false;
    width = be32(16); height = be32(20);
  } else if (mime === "image/gif") {
    if (data.length < 13) return false;
    width = le16(6); height = le16(8);
  } else if (mime === "image/jpeg") {
    let offset = 2;
    // Reject unavailable SOF instead of scanning entropy data or decoding.
    for (let segments = 0; offset < data.length && segments < 1024; segments++) {
      if (data[offset++] !== 0xff) return false;
      while (offset < data.length && data[offset] === 0xff) offset++;
      const marker = data[offset++];
      if (marker === 0x01 || (marker >= 0xd0 && marker <= 0xd7)) continue;
      if (!marker || marker === 0xd8 || marker === 0xd9 || marker === 0xda || offset + 2 > data.length) return false;
      const length = be16(offset);
      if (length < 2 || offset + length > data.length) return false;
      if ([0xc0, 0xc1, 0xc2].includes(marker)) {
        if (length < 11 || data[offset + 7] < 1 || data[offset + 7] > 4 || length !== 8 + 3 * data[offset + 7]) return false;
        height = be16(offset + 3); width = be16(offset + 5); break;
      }
      offset += length;
    }
  } else if (mime === "image/webp") {
    if (data.length < 25 || le32(4) + 8 !== data.length) return false;
    const size = le32(16), chunk = ascii(12, 16);
    if (size + size % 2 > data.length - 20) return false;
    if (chunk === "VP8X" && size === 10) {
      width = le24(24) + 1; height = le24(27) + 1;
    } else if (chunk === "VP8L" && size >= 5 && data[20] === 0x2f && (data[24] & 0xe0) === 0) {
      const bits = le32(21);
      width = (bits & 0x3fff) + 1; height = ((bits >>> 14) & 0x3fff) + 1;
    } else if (chunk === "VP8 " && size >= 10 && (data[20] & 1) === 0 && ascii(23, 26) === "\x9d\x01\x2a") {
      width = le16(26) & 0x3fff; height = le16(28) & 0x3fff;
    }
  }
  return width > 0 && height > 0 && width <= 16384 && height <= 16384 && width * height <= 40_000_000;
}
export function base64(data: Uint8Array) {
  let binary = "";
  for (let i = 0; i < data.length; i += 8192) binary += String.fromCharCode(...data.subarray(i, i + 8192));
  return btoa(binary);
}
export const labelText = (item: Item) => `Attachment: ${item.label}\n${item.kind === "text" ? item.text : "[Image]"}`;
export const privacyNotice = "Text and image contents will be sent to the provider and persisted in saved conversation history when you send. Images require a vision-capable model.";
export function releasePreview(item: Item) {
  if (item.previewURL) URL.revokeObjectURL(item.previewURL);
  item.previewURL = null; item.previewElement = null;
}
export function previewURL(item: Item) {
  if (item.state !== "ready" || item.kind !== "image" || item.previewFailed) return "";
  if (item.previewURL) return item.previewURL;
  try {
    // Only decode retained, validated raster content; never use a filename,
    // remote URL or data URL as an image source. Raw draft images total 2 MiB.
    if (item.size > IMAGE_LIMIT || !["image/png", "image/jpeg", "image/gif", "image/webp"].includes(item.mime || "")) return "";
    const binary = atob(item.data || ""), data = Uint8Array.from(binary, char => char.charCodeAt(0));
    if (data.length !== item.size || imageType(data) !== item.mime || !previewDimensionsSafe(data, item.mime || "")) {
      item.previewFailed = true; return "";
    }
    item.previewURL = URL.createObjectURL(new Blob([data], {type: item.mime}));
    return item.previewURL;
  } catch (_) {
    // Preview support or browser decoding must never change send eligibility.
    item.previewFailed = true; return "";
  }
}
