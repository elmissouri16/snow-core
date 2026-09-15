// Public HTTP fixture patterned after permission-policy/fixture.mjs. Assets are
// copied verbatim from Go's exporter; no DOM, fetch, worker or provider mocks.
import {createServer} from "node:http";
import {extname} from "node:path";

export const originals = [
  {id: "older-user", role: "user", text: "First request: keep **literal** text & <source>.\nSecond line."},
  {id: "older-answer", role: "assistant", text: "First answer retained."},
  {id: "middle-user", role: "user", text: "Second request: café 😀 and another detail."},
  {id: "middle-answer", role: "assistant", text: "Second answer retained."},
  {id: "latest-user", role: "user", text: "Third request, not the message selected for editing."},
  {id: "latest-answer", role: "assistant", text: "Third answer retained."},
  {id: "truncated-user", role: "user", text: "Only a bounded prefix…", truncated: true}
];

export function transport(files, fixture) {
  const savedProject = /data-project="([a-f0-9-]{36})"/.exec(files.get("/saved-user.html").toString())?.[1];
  if (!savedProject) throw Error("Saved fixture lacks its exported project identity");
  const state = {snapshot: null, requests: [], errors: [], reads: 0, offline: false,
    promptBehavior: null, cancelBehavior: null, heldPrompt: null, heldCancel: null, closed: false};
  state.update = fields => Object.assign(state.snapshot, structuredClone(fields), {revision: state.snapshot.revision + 1});
  state.release = () => { state.heldPrompt?.(); state.heldCancel?.(); };
  state.reset = () => {
    state.release();
    Object.assign(state, {snapshot: structuredClone(fixture.snapshot), requests: [], errors: [], reads: 0, offline: false,
      promptBehavior: null, cancelBehavior: null, heldPrompt: null, heldCancel: null, closed: false});
    Object.assign(state.snapshot, {status: "idle", cancel_token: "", cancel_requested: false, messages: structuredClone(originals), permission: null, input: null});
  };
  state.reset();
  const json = (response, value, status = 200) => {
    response.writeHead(status, {"Content-Type": "application/json", "Cache-Control": "no-store"}); response.end(JSON.stringify(value));
  };
  const server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url, "http://localhost"), path = url.pathname;
      if (request.method === "GET" && path === "/" && state.closed && url.searchParams.get("view") === "projects" &&
          url.searchParams.get("project") === state.snapshot.project_id && url.searchParams.get("session") === state.snapshot.session_id) {
        response.writeHead(200, {"Content-Type": "text/html", "Cache-Control": "no-store"});
        response.end(files.get("/saved-user.html")); return;
      }
      if (request.method === "GET" && files.has(path)) {
        response.writeHead(200, {"Content-Type": {".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".svg": "image/svg+xml"}[extname(path)] || "application/octet-stream", "Cache-Control": "no-store"});
        response.end(files.get(path)); return;
      }
      if (path === "/favicon.ico") { response.writeHead(204); response.end(); return; }
      // The full production Shell also makes bounded, read-only inventories.
      // Handle those exact GET contracts separately from mutation accounting.
      if (request.method === "GET" && path === "/access/browsers" && !url.search) {
        json(response, {limit: 8, browsers: []}); return;
      }
      if (request.method === "GET" && url.search === "?offset=0") {
        const project = [state.snapshot.project_id, savedProject].find(id => path === `/projects/${id}/sidebar-sessions`);
        if (project) {
          const active = project === state.snapshot.project_id && !state.closed;
          json(response, {project_id: project, instance_id: active ? state.snapshot.instance_id : "",
            sessions: active ? [{session_id: state.snapshot.session_id, name: "Fixture conversation"}] : [], available: true,
            active_session_id: active ? state.snapshot.session_id : "", delete_supported: false, has_more: false, next_offset: active ? 1 : 0}); return;
        }
      }
      const prefix = `/projects/${state.snapshot.project_id}/runtime`;
      if (path === prefix && request.method === "GET") {
        state.reads++; json(response, state.offline ? {error: "Fixture disconnected"} : state.snapshot, state.offline ? 503 : 200); return;
      }
      if (path === prefix + "/events" && request.method === "GET") {
        json(response, {error: "Fixture exercises production public polling fallback"}, 501); return;
      }
      let text = "";
      for await (const chunk of request) { text += chunk; if (Buffer.byteLength(text) > 128 * 1024) throw Error("Request body exceeds fixture bound"); }
      const params = new URLSearchParams(text), fields = Object.fromEntries(params);
      state.requests.push({path, method: request.method, fields});
      if (state.requests.length > 1000) throw Error("Unexpected mutation loop");
      if (request.method !== "POST" || fields.csrf !== "test-csrf" || fields.instance_id !== state.snapshot.instance_id) throw Error(`Missing explicit CSRF/instance-bound POST: ${request.method} ${path}`);
      const exactFields = expected => {
        if ([...params.keys()].length !== expected.length || [...params.keys()].some(key => !expected.includes(key))) throw Error("Unexpected or duplicate request fields");
      };
      if (path === prefix + "/prompt") {
        exactFields(["csrf", "instance_id", "text"]);
        if (state.snapshot.status !== "idle" || !fields.text?.trim() || Buffer.byteLength(fields.text) > 65536) throw Error("Invalid prompt bypassed production guard");
        const behavior = state.promptBehavior; state.promptBehavior = null;
        if (behavior === "unknown") { json(response, {error: "Fixture prompt outcome unknown"}, 503); return; }
        if (behavior === "malformed") { json(response, {}); return; }
        state.update({status: "running", cancel_token: "turn-one-token", cancel_requested: false,
          messages: [...state.snapshot.messages, {id: "sent-user", role: "user", text: fields.text}]});
        if (behavior === "hold") { state.heldPrompt = () => { state.heldPrompt = null; json(response, {success: true}); }; return; }
        json(response, {success: true}); return;
      }
      if (path === prefix + "/cancel") {
        exactFields(["csrf", "instance_id", "cancel_token"]);
        if (!["running", "permission", "input"].includes(state.snapshot.status) || !fields.cancel_token || fields.cancel_token !== state.snapshot.cancel_token) throw Error("Cancel must target exactly the observed active turn");
        const behavior = state.cancelBehavior; state.cancelBehavior = null;
        if (behavior === "reject") { json(response, {error: "Fixture rejects stale turn"}, 409); return; }
        if (behavior === "unknown") { json(response, {error: "Fixture cancel outcome unknown"}, 503); return; }
        if (behavior === "malformed") { json(response, {}); return; }
        if (behavior === "false-success") { json(response, {success: false}); return; }
        if (behavior === "extra-ack-fields") { json(response, {success: true, cancel_requested: true}); return; }
        if (behavior === "wrong-token") { json(response, {...state.snapshot, cancel_token: "another-turn"}); return; }
        if (behavior === "missing-status") { const malformed = {...state.snapshot}; delete malformed.status; json(response, malformed); return; }
        const accepted = () => { state.heldCancel = null; state.update({cancel_requested: true}); json(response, {success: true}); };
        if (behavior === "hold") {
          state.update({cancel_requested: true});
          const acknowledgement = {success: true};
          state.heldCancel = () => { state.heldCancel = null; json(response, acknowledgement); }; return;
        }
        accepted(); return;
      }
      if (path === prefix + "/close") {
        exactFields(["csrf", "instance_id"]);
        if (state.snapshot.status !== "failed" || state.closed) throw Error("Close requires the observed failed worker and explicit one-shot confirmation");
        state.closed = true;
        state.update({status: "closing", cancel_token: "", cancel_requested: false});
        json(response, {success: true}); return;
      }
      throw Error(`Unexpected request (including forbidden branch): ${request.method} ${path}`);
    } catch (error) { state.errors.push(error.message); json(response, {error: error.message}, 400); }
  });
  return {server, state};
}
