// Isolated public HTTP transport. Never starts Snow, an RPC worker, or a provider.
import {createServer} from "node:http";
import {extname} from "node:path";

export function transport(files, fixture) {
  const streams = new Set();
  const state = {snapshot: structuredClone(fixture.snapshot), requests: [], errors: [], offline: false, next: null, held: null, reads: 0, streaming: false, streamOpens: 0};
  state.push = () => { for (const response of streams) response.write(`event: snapshot\ndata: ${JSON.stringify(state.snapshot)}\n\n`); };
  state.terminate = kind => { for (const response of streams) { response.write(`event: ${kind}\ndata: {}\n\n`); response.end(); } streams.clear(); };
  state.reset = () => {
    if (state.held) throw new Error("Cannot reset a held mutation");
    for (const response of streams) response.end();
    streams.clear();
    Object.assign(state, {snapshot: structuredClone(fixture.snapshot), requests: [], errors: [], offline: false, next: null, reads: 0, streaming: false, streamOpens: 0});
    state.snapshot.permission_mode = "ask";
  };
  state.update = fields => Object.assign(state.snapshot, fields, {revision: state.snapshot.revision + 1});
  const json = (response, value, status = 200) => {
    response.writeHead(status, {"Content-Type": "application/json", "Cache-Control": "no-store"});
    response.end(JSON.stringify(value));
  };
  const server = createServer(async (request, response) => {
    try {
      const path = new URL(request.url, "http://localhost").pathname;
      const body = files.get(path);
      if (path === "/" && body && request.method === "GET") {
        const entry = {query: new URL(request.url, "http://localhost").search, aborted: false};
        (state.navigation ||= []).push(entry);
        if (state.navigation.length > 1000) throw new Error("Unexpected navigation loop");
        response.on("close", () => { entry.aborted = !response.writableEnded; });
        // Exported documents include the shell; mirror production's workspace-only swap.
        const reply = (status = 200) => { response.writeHead(status, {"Content-Type": "text/html", "Cache-Control": "no-store", "HX-Reselect": "#workspace"}); response.end(body); };
        if (state.holdNavigation) { state.holdNavigation = false; state.releaseNavigation = reply; }
        else reply();
        return;
      }
      if (body && request.method === "GET") {
        response.writeHead(200, {"Content-Type": {".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".svg": "image/svg+xml"}[extname(path)] || "application/octet-stream", "Cache-Control": "no-store"});
        response.end(body); return;
      }
      if (path === "/favicon.ico") { response.writeHead(204); response.end(); return; }
      if (path === "/access/browsers" && request.method === "GET") { json(response, {browsers: []}); return; }
      if (path === `/projects/${state.snapshot.project_id}/sidebar-sessions` && request.method === "GET") {
        const url = new URL(request.url, "http://localhost");
        if ([...url.searchParams.keys()].some(key => key !== "offset") || (url.searchParams.get("offset") || "0") !== "0") throw new Error("Unexpected sidebar inventory page");
        json(response, {project_id: state.snapshot.project_id, instance_id: state.snapshot.instance_id, available: true,
          sessions: [{session_id: state.snapshot.session_id, name: state.snapshot.session_name || "Fixture session", updated_at: "2026-01-01T00:00:00Z"}],
          truncated: false, has_more: false, next_offset: 0}); return;
      }
      const prefix = `/projects/${state.snapshot.project_id}/runtime`;
      if (path === prefix && request.method === "GET") {
        state.reads++;
        json(response, state.offline ? {error: "Fixture host disconnected"} : state.snapshot, state.offline ? 503 : 200); return;
      }
      if (path === prefix + "/events" && request.method === "GET") {
        if (state.streaming) {
          state.streamOpens++;
          response.writeHead(200, {"Content-Type": "text/event-stream", "Cache-Control": "no-store"});
          streams.add(response); state.push();
          response.on("close", () => streams.delete(response));
          return;
        }
        json(response, {error: "Fixture exercises public polling fallback"}, 501); return;
      }
      let text = "";
      for await (const chunk of request) {
        text += chunk;
        if (Buffer.byteLength(text) > 128 * 1024) throw new Error("Request body exceeds fixture bound");
      }
      const params = new URLSearchParams(text), fields = Object.fromEntries(params);
      state.requests.push({path, method: request.method, fields, htmx: request.headers["hx-request"]});
      if (state.requests.length > 1000) throw new Error("Unexpected mutation loop");
      if (request.method !== "POST" || fields.csrf !== "test-csrf" || fields.instance_id !== state.snapshot.instance_id) throw new Error(`Missing explicit instance/CSRF-bound POST: ${request.method} ${path}`);
      if (path === prefix + "/choices") {
        if (Object.keys(fields).sort().join(",") !== "csrf,instance_id" || request.headers["hx-request"] !== "true") throw new Error("Invalid HTMX discovery request");
        state.held = (choices, status = 200) => { state.held = null; json(response, choices, status); }; return;
      }
      if (path === prefix + "/model") {
        if (Object.keys(fields).sort().join(",") !== "csrf,instance_id,model,provider" || request.headers["hx-request"] !== "true") throw new Error("Invalid HTMX model request");
        if (state.next === "model-success-only") { state.next = null; json(response, {success: true}); return; }
        state.update({provider: fields.provider, model: fields.model}); json(response, state.snapshot); return;
      }
      if (path === prefix + "/mode") {
        if (state.snapshot.status !== "idle" || !["default", "plan"].includes(fields.mode) || Object.keys(fields).sort().join(",") !== "csrf,instance_id,mode" || request.headers["hx-request"] !== "true") throw new Error("Invalid HTMX mode request");
        const behavior = state.next; state.next = null;
        if (behavior === "hold") {
          state.held = mode => { state.held = null; state.update({mode}); json(response, state.snapshot); }; return;
        }
        if (behavior === "lost") {
          state.update({mode: fields.mode}); state.push();
          // Lose an in-progress response, not an idle keep-alive connection:
          // browsers may transparently retry a socket reset before any headers.
          response.writeHead(200, {"Content-Type": "application/json"});
          response.write('{"mode":'); setTimeout(() => response.destroy(), 20); return;
        }
        if (behavior === "malformed") { json(response, {...state.snapshot, mode: fields.mode}); return; }
        state.update({mode: fields.mode}); json(response, state.snapshot); return;
      }
      if (path === prefix + "/permission-mode") {
        if (state.snapshot.status !== "idle" || !["ask", "deny", "allow"].includes(fields.mode) || fields.session_id !== state.snapshot.session_id) throw new Error("Invalid policy/identity/status");
        if (fields.mode === "allow" ? fields.confirm_allow !== "allow" : params.has("confirm_allow")) throw new Error("Invalid Allow acknowledgement");
        const expected = ["csrf", "instance_id", "session_id", "mode", ...(fields.mode === "allow" ? ["confirm_allow"] : [])];
        if ([...params.keys()].length !== expected.length || [...params.keys()].some(key => !expected.includes(key))) throw new Error("Unexpected or duplicate policy fields");
        const behavior = state.next; state.next = null;
        if (behavior === "empty") { json(response, {}); return; }
        if (["missing-status", "invalid-status"].includes(behavior)) {
          const malformed = {...state.snapshot, permission_mode: fields.mode, revision: state.snapshot.revision + 1};
          if (behavior === "missing-status") delete malformed.status;
          else malformed.status = 42;
          json(response, malformed); return;
        }
        if (behavior === "fail") { json(response, {error: "Fixture mutation outcome uncertain"}, 503); return; }
        if (behavior === "hold") {
          state.held = mode => { state.held = null; state.update({permission_mode: mode}); json(response, state.snapshot); };
          return;
        }
        state.update({permission_mode: fields.mode}); json(response, state.snapshot); return;
      }
      if (path === prefix + "/prompt") {
        if (!fields.text?.trim() || Buffer.byteLength(fields.text) > 65536 || state.snapshot.status !== "idle") throw new Error("Invalid prompt bypassed production guard");
        state.update({status: "idle"}); json(response, state.snapshot); return;
      }
      throw new Error(`Unexpected public request: ${request.method} ${path}`);
    } catch (error) {
      state.errors.push(error.message);
      json(response, {error: error.message}, 400);
    }
  });
  state.reset();
  return {server, state};
}
