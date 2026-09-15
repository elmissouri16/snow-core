export const MAX_INVENTORY_BYTES = 16 * 1024;
export const REQUEST_TIMEOUT_MS = 15_000;
export interface PairedBrowser {
  id: string;
  label: string;
  created: string;
  last_used: string;
  expires: string;
  current: boolean;
}
function object(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
export function validBrowserID(value: unknown): value is string {
  return typeof value === "string" && /^browser_[a-f0-9]{32}$/.test(value);
}
function validDate(value: unknown): value is string {
  if (typeof value !== "string" || value.length > 64) return false;
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(?:Z|[+-](\d{2}):(\d{2}))$/.exec(value);
  if (!match || !Number.isFinite(Date.parse(value))) return false;
  const [, year, month, day, hour, minute, second, zoneHour, zoneMinute] = match;
  const y = Number(year), m = Number(month), d = Number(day);
  const leap = y % 4 === 0 && (y % 100 !== 0 || y % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return m >= 1 && m <= 12 && d >= 1 && d <= days[m - 1]! &&
    Number(hour) <= 23 && Number(minute) <= 59 && Number(second) <= 59 &&
    (zoneHour === undefined || (Number(zoneHour) <= 23 && Number(zoneMinute) <= 59));
}
export function validateInventory(data: unknown): PairedBrowser[] {
  if (!object(data) || data.limit !== 8 || !Array.isArray(data.browsers) || data.browsers.length > 8) throw Error("inventory");
  const ids = new Set<string>();
  let current = 0;
  return data.browsers.map((browser: unknown) => {
    if (!object(browser) || !validBrowserID(browser.id) || ids.has(browser.id) ||
      typeof browser.label !== "string" || browser.label.length === 0 || browser.label.trim() !== browser.label ||
      new TextEncoder().encode(browser.label).byteLength > 80 || /[\p{Cc}\p{Cf}\p{Cs}]/u.test(browser.label) ||
      typeof browser.current !== "boolean" || !validDate(browser.created) || !validDate(browser.last_used) || !validDate(browser.expires) ||
      Date.parse(browser.last_used) < Date.parse(browser.created) || Date.parse(browser.expires) <= Date.parse(browser.created)) throw Error("browser");
    ids.add(browser.id);
    if (browser.current && ++current > 1) throw Error("current browser");
    // Copy only public, checked fields; unknown wire metadata never reaches the UI.
    return {id: browser.id, label: browser.label, current: browser.current,
      created: browser.created, last_used: browser.last_used, expires: browser.expires};
  });
}
export function validateReceipt(data: unknown, id: string): {revoked_id: string; signed_out: boolean} {
  if (!validBrowserID(id) || !object(data) || data.revoked_id !== id || typeof data.signed_out !== "boolean") throw Error("receipt");
  return {revoked_id: id, signed_out: data.signed_out};
}
/** Bound the decoded stream, not just an optional Content-Length header. */
export async function boundedJSON(response: Response, signal: AbortSignal): Promise<unknown> {
  signal.throwIfAborted();
  const length = response.headers.get("Content-Length");
  if (length !== null && (!/^\d+$/.test(length) || Number(length) > MAX_INVENTORY_BYTES)) throw Error("size");
  if (response.headers.get("Content-Type")?.split(";", 1)[0]?.trim().toLowerCase() !== "application/json" || !response.body) throw Error("body");
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  const abort = () => { void reader.cancel().catch(() => {}); };
  signal.addEventListener("abort", abort, {once: true});
  try {
    while (true) {
      signal.throwIfAborted();
      const {done, value} = await reader.read();
      signal.throwIfAborted();
      if (done) break;
      size += value.byteLength;
      if (size > MAX_INVENTORY_BYTES) throw Error("size");
      chunks.push(value);
    }
    const bytes = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
    return JSON.parse(new TextDecoder("utf-8", {fatal: true}).decode(bytes));
  } finally {
    signal.removeEventListener("abort", abort);
    void reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}
