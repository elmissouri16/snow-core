// Only public HTTP is simulated. HTML, renderer, edit controller and SSE client
// are the unmodified Go export. No provider, worker, user config or real manager.
import {createServer} from "node:http";
import {extname} from "node:path";

export const originals = [
  {id: "first-user", role: "user", text: "First displayed request.", can_edit: true},
  {id: "first-answer", role: "assistant", text: "First answer retained."},
  {id: "middle-user", role: "user", text: "Second displayed request.", can_edit: true},
  {id: "middle-plan", role: "plan", text: "Obsolete middle plan."},
  {id: "middle-answer", role: "assistant", text: "Obsolete middle answer.", tools: [{id: "obsolete-tool", owner_id: "middle-answer", tool: "read", status: "completed", output: "Obsolete tool output.", output_available: true}]},
  {id: "latest-user", role: "user", text: "Latest displayed request.", can_edit: true},
  {id: "latest-answer", role: "assistant", text: "Obsolete latest answer."},
  {id: "truncated-user", role: "user", text: "Only a bounded prefix…", truncated: true, can_edit: false},
  {id: "nontext-user", role: "user", text: "[Attachment]", can_edit: false}
].map(message => message.role === "user" ? message : {...message, html: `<p>${message.text}</p>`});
export const sourceText = Object.fromEntries(originals.filter(row => row.can_edit).map(row => [row.id,
  `${row.id}: authoritative **literal** text & <source>.\nFull source absent from displayed copy; café 😀.`]));

export function transport(files, fixture) {
  const savedProject = /data-project="([a-f0-9-]{36})"/.exec(files.get("/saved-user.html").toString())?.[1];
  if (!savedProject) throw Error("Saved fixture lacks its exported project identity");
  const state = {streams: new Set(), requests: [], errors: [], reads: 0, held: null};
  const json = (response, value, status = 200) => {
    response.writeHead(status, {"Content-Type": "application/json", "Cache-Control": "no-store"}); response.end(JSON.stringify(value));
  };
  state.emit = (value = state.snapshot, instance = value.instance_id) => {
    for (const stream of state.streams) if (stream.instance === instance && !stream.response.destroyed) stream.response.write(`event: snapshot\ndata: ${JSON.stringify(value)}\n\n`);
  };
  state.update = fields => { Object.assign(state.snapshot, structuredClone(fields), {revision: state.snapshot.revision + 1}); state.emit(); };
  state.release = () => { const held = state.held; state.held = null; held?.(); };
  state.reset = () => {
    state.release(); for (const stream of state.streams) stream.response.end(); state.streams.clear();
    Object.assign(state, {snapshot: structuredClone(fixture.snapshot), requests: [], errors: [], reads: 0, prepared: null, prepareBehavior: null, commitBehavior: null, held: null});
    Object.assign(state.snapshot, {status: "idle", cancel_token: "", cancel_requested: false, messages: structuredClone(originals), permission: null, input: null});
    state.archive = structuredClone(originals);
  };
  state.reset();
  const server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url, "http://localhost"), path = url.pathname;
      if (request.method === "GET" && files.has(path)) {
        response.writeHead(200, {"Content-Type": {".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".svg": "image/svg+xml"}[extname(path)] || "application/octet-stream", "Cache-Control": "no-store"});
        response.end(files.get(path)); return;
      }
      if (path === "/favicon.ico") { response.writeHead(204); response.end(); return; }
      // Full Shell inventories are read-only and must not enter mutation counts.
      if (request.method === "GET" && path === "/access/browsers" && !url.search) {
        json(response, {limit: 8, browsers: []}); return;
      }
      if (request.method === "GET" && url.search === "?offset=0") {
        const project = [state.snapshot.project_id, savedProject].find(id => path === `/projects/${id}/sidebar-sessions`);
        if (project) {
          const active = project === state.snapshot.project_id;
          json(response, {project_id: project, instance_id: active ? state.snapshot.instance_id : "",
            sessions: active ? [{session_id: state.snapshot.session_id, name: "Fixture conversation"}] : [], available: true,
            active_session_id: active ? state.snapshot.session_id : "", delete_supported: false, has_more: false, next_offset: active ? 1 : 0}); return;
        }
      }
      const prefix = `/projects/${state.snapshot.project_id}/runtime`;
      if (path === prefix && request.method === "GET") { state.reads++; json(response, state.snapshot); return; }
      if (path === prefix + "/events" && request.method === "GET") {
        const instance = url.searchParams.get("instance_id");
        if (instance !== state.snapshot.instance_id) { json(response, {error: "Instance replaced"}, 409); return; }
        response.writeHead(200, {"Content-Type": "text/event-stream", "Cache-Control": "no-store", Connection: "keep-alive"});
        const stream = {instance, response}; state.streams.add(stream);
        response.on("close", () => state.streams.delete(stream));
        response.write(`event: snapshot\ndata: ${JSON.stringify(state.snapshot)}\n\n`); return;
      }
      let body = "";
      for await (const chunk of request) { body += chunk; if (Buffer.byteLength(body) > 256 * 1024) throw Error("Request exceeds fixture bound"); }
      const params = new URLSearchParams(body), fields = Object.fromEntries(params);
      state.requests.push({path, method: request.method, fields});
      if (state.requests.length > 1000) throw Error("Unexpected mutation loop");
      if (request.method !== "POST" || fields.csrf !== "test-csrf" || fields.instance_id !== state.snapshot.instance_id) throw Error("Expected CSRF-protected, instance-bound POST");
      const exact = keys => { if ([...params.keys()].length !== keys.length || [...params.keys()].some(key => !keys.includes(key))) throw Error("Unexpected or duplicate request fields"); };
      if (path === prefix + "/message-edit-prepare") {
        exact(["csrf", "instance_id", "message_id"]);
        if (state.snapshot.status !== "idle" || !sourceText[fields.message_id]) throw Error("Invalid edit selection");
        const prepared = {instance_id: state.snapshot.instance_id, project_id: state.snapshot.project_id, session_id: state.snapshot.session_id, message_id: fields.message_id, edit_token: `edit-${state.requests.length}`, text: sourceText[fields.message_id]};
        const behavior = state.prepareBehavior; state.prepareBehavior = null;
        if (behavior === "reject") { json(response, {error: "Source changed; prepare again"}, 409); return; }
        state.prepared = prepared;
        const reply = () => {
          if (behavior === "hold-reject") json(response, {error: "Source changed while preparing"}, 409);
          else if (behavior === "empty") json(response, {...prepared, text: ""});
          else if (behavior === "nontext") json(response, {...prepared, text: {text: "not a string"}});
          else if (behavior === "oversize") json(response, {...prepared, text: "x".repeat(65537)});
          else if (behavior === "wrong-source") json(response, {...prepared, message_id: "latest-user"});
          else json(response, prepared);
        };
        if (behavior === "hold" || behavior === "hold-reject") { state.held = reply; return; }
        reply(); return;
      }
      if (path === prefix + "/message-edit-commit") {
        exact(["csrf", "instance_id", "edit_token", "text"]);
        const prepared = state.prepared; state.prepared = null;
        if (!prepared || prepared.edit_token !== fields.edit_token || state.snapshot.status !== "idle" || !fields.text.trim() || fields.text.includes("\0") || Buffer.byteLength(fields.text) > 65536) throw Error("Invalid, stale or replayed commit");
        const behavior = state.commitBehavior; state.commitBehavior = null;
        if (behavior === "reject") { json(response, {error: "Source changed; prepare again"}, 409); return; }
        if (behavior === "unknown") { json(response, {error: "Commit outcome unknown; review conversation"}, 503); return; }
        if (behavior === "malformed") { json(response, {success: true}); return; }
        const prefixRows = state.snapshot.messages.slice(0, state.snapshot.messages.findIndex(row => row.id === prepared.message_id));
        const old = structuredClone(state.snapshot);
        state.update({status: "switching"});
        Object.assign(state.snapshot, {instance_id: "replacement-instance", revision: 1, status: "running", cancel_token: "replacement-turn-token", cancel_requested: false,
          messages: [...prefixRows, {id: "replacement-user", role: "user", text: fields.text, can_edit: true}]});
        const ack = structuredClone(state.snapshot);
        if (behavior === "fast-terminal") Object.assign(state.snapshot, {revision: 2, status: "idle", cancel_token: "", messages: [...state.snapshot.messages, {id: "replacement-answer", role: "assistant", text: "Replacement finished once.", html: "<p>Replacement finished once.</p>"}]});
        state.previous = old;
        if (behavior === "hold" || behavior === "fast-terminal") { state.held = () => json(response, ack); return; }
        json(response, ack); return;
      }
      if (path === prefix + "/cancel") {
        exact(["csrf", "instance_id", "cancel_token"]);
        if (state.snapshot.status !== "running" || fields.cancel_token !== "replacement-turn-token") throw Error("Stop must target replacement turn");
        state.update({cancel_requested: true}); json(response, {success: true}); return;
      }
      throw Error(`Forbidden request: ${request.method} ${path}; edit never prompts, branches, forks, switches or activates`);
    } catch (error) { state.errors.push(error.message); json(response, {error: error.message}, 400); }
  });
  return {server, state};
}
