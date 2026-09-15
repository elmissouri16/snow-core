import {accessSync, constants} from "node:fs";

export function chromeBinary() {
  for (const path of process.env.SNOW_CHROME_BIN ? [process.env.SNOW_CHROME_BIN] : [
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/usr/bin/google-chrome",
    "/usr/bin/google-chrome-stable", "/usr/bin/chromium", "/usr/bin/chromium-browser"
  ]) {
    try { accessSync(path, constants.X_OK); return path; } catch { /* next fixed executable */ }
  }
  throw new Error("Chrome not found; set SNOW_CHROME_BIN to an executable path.");
}

export function debuggingURL(child) {
  return new Promise((resolve, reject) => {
    let output = "";
    child.stderr.on("data", chunk => {
      output = (output + chunk).slice(-65536);
      const match = output.match(/DevTools listening on (ws:\/\/[^\s]+)/);
      if (match) resolve(match[1]);
    });
    child.once("error", reject);
    child.once("exit", () => reject(new Error("Chrome exited before debugger startup.")));
  });
}

export async function connect(url) {
  const socket = new WebSocket(url);
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = () => reject(new Error("CDP connection failed")); });
  let sequence = 0;
  const pending = new Map(), listeners = new Set();
  socket.onmessage = event => {
    const message = JSON.parse(event.data);
    if (message.id) {
      const handlers = pending.get(message.id);
      if (!handlers) return;
      pending.delete(message.id);
      if (message.error) handlers.reject(new Error(message.error.message)); else handlers.resolve(message.result);
    } else for (const handler of listeners) handler(message);
  };
  socket.onclose = () => { for (const handler of pending.values()) handler.reject(new Error("CDP closed")); pending.clear(); };
  return {
    onEvent(handler) { listeners.add(handler); return () => listeners.delete(handler); },
    close() { socket.close(); },
    send(method, params = {}, sessionId) {
      return new Promise((resolve, reject) => { const id = ++sequence; pending.set(id, {resolve, reject}); socket.send(JSON.stringify({id, method, params, sessionId})); });
    }
  };
}
