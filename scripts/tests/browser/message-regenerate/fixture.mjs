// Public HTTP/SSE only; production Go HTML/assets and native browser behavior.
import {createServer} from "node:http";
import {extname} from "node:path";

const tool = (id, owner) => ({id, owner_id: owner, tool: "read", status: "completed", output: `Recorded ${id} output.`, output_available: true});
export const originals = [
  {id: "first-user", role: "user", text: "First original prompt — keep exactly once.", can_edit: true},
  {id: "first-preface", role: "assistant", text: "First response preface.", can_regenerate: false},
  {id: "first-step", role: "assistant", text: "First tool step.", tools: [tool("first-tool", "first-step")]},
  {id: "first-answer", role: "assistant", text: "First final answer.", can_regenerate: true},
  {id: "middle-user", role: "user", text: "Middle original prompt **literal** & <source> 😀.", can_edit: true},
  {id: "middle-plan", role: "plan", text: "Middle obsolete plan."},
  {id: "middle-step-one", role: "assistant", text: "Middle tool step one.", tools: [tool("middle-tool-one", "middle-step-one")]},
  {id: "middle-step-two", role: "assistant", text: "Middle tool step two.", tools: [tool("middle-tool-two", "middle-step-two")]},
  {id: "middle-answer", role: "assistant", text: "Middle final answer.", can_regenerate: true},
  {id: "latest-user", role: "user", text: "Latest original prompt, never copied into composer.", can_edit: true},
  {id: "latest-answer", role: "assistant", text: "Latest final answer.", can_regenerate: true},
  {id: "truncated-answer", role: "assistant", text: "Bounded incomplete answer…", truncated: true},
  {id: "orphan-answer", role: "assistant", text: "Imported response without exact owning turn."}
].map(message => message.role === "user" ? message : {...message, html: `<p>${message.text}</p>`});
export const owners = {"first-answer": "first-user", "middle-answer": "middle-user", "latest-answer": "latest-user"};

export function transport(files, fixture) {
  const savedProject = /data-project="([a-f0-9-]{36})"/.exec(files.get("/saved-user.html").toString())?.[1];
  if (!savedProject) throw Error("Saved fixture lacks its exported project identity");
  const state = {streams: new Set(), requests: [], errors: [], reads: 0, held: null};
  const json = (response, value, status = 200) => { response.writeHead(status, {"Content-Type": "application/json", "Cache-Control": "no-store"}); response.end(JSON.stringify(value)); };
  state.emit = (value = state.snapshot, instance = value.instance_id) => {
    for (const stream of state.streams) if (stream.instance === instance && !stream.response.destroyed) stream.response.write(`event: snapshot\ndata: ${JSON.stringify(value)}\n\n`);
  };
  state.update = fields => { Object.assign(state.snapshot, structuredClone(fields), {revision: state.snapshot.revision + 1}); state.emit(); };
  state.release = () => { const held = state.held; state.held = null; held?.(); };
  state.reset = () => {
    state.release(); for (const stream of state.streams) stream.response.end(); state.streams.clear();
    Object.assign(state, {snapshot: structuredClone(fixture.snapshot), requests: [], errors: [], reads: 0, prepared: null, prepareBehavior: null, commitBehavior: null, held: null});
    Object.assign(state.snapshot, {status: "idle", cancel_token: "", cancel_requested: false, messages: structuredClone(originals), activities: [], permission: null, input: null});
    state.archive = structuredClone(originals);
  };
  state.reset();
  const server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url, "http://localhost"), path = url.pathname;
      if (request.method === "GET" && files.has(path)) {
        response.writeHead(200, {"Content-Type": {".js": "text/javascript", ".css": "text/css", ".html": "text/html", ".svg": "image/svg+xml"}[extname(path)] || "application/octet-stream", "Cache-Control": "no-store"}); response.end(files.get(path)); return;
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
        const stream = {instance, response}; state.streams.add(stream); response.on("close", () => state.streams.delete(stream));
        response.write(`event: snapshot\ndata: ${JSON.stringify(state.snapshot)}\n\n`); return;
      }
      let body = "";
      for await (const chunk of request) { body += chunk; if (Buffer.byteLength(body) > 256 * 1024) throw Error("Request exceeds fixture bound"); }
      const params = new URLSearchParams(body), fields = Object.fromEntries(params);
      state.requests.push({path, method: request.method, fields});
      if (state.requests.length > 1000) throw Error("Unexpected mutation loop");
      if (request.method !== "POST" || fields.csrf !== "test-csrf" || fields.instance_id !== state.snapshot.instance_id) throw Error("Expected CSRF-protected, instance-bound POST");
      const exact = keys => { if ([...params.keys()].length !== keys.length || [...params.keys()].some(key => !keys.includes(key))) throw Error("Unexpected or duplicate request fields"); };
      if (path === prefix + "/message-regenerate-prepare") {
        exact(["csrf", "instance_id", "message_id"]);
        if (state.snapshot.status !== "idle" || !owners[fields.message_id]) throw Error("Invalid final-response selection");
        const prepared = {project_id: state.snapshot.project_id, session_id: state.snapshot.session_id, instance_id: state.snapshot.instance_id, message_id: fields.message_id, edit_token: `regenerate-${state.requests.length}`};
        const behavior = state.prepareBehavior; state.prepareBehavior = null;
        state.prepared = prepared;
        const reply = () => {
          if (behavior === "reject" || behavior === "hold-reject") { state.prepared = null; json(response, {error: "Source changed or regeneration expired"}, 409); }
          else if (behavior === "missing-token") json(response, {...prepared, edit_token: ""});
          else if (behavior === "wrong-source") json(response, {...prepared, message_id: "latest-answer"});
          else if (behavior === "wrong-instance") json(response, {...prepared, instance_id: "orphan-instance"});
          else if (behavior === "wrong-session") json(response, {...prepared, session_id: "other-chat"});
          else if (behavior === "malformed") json(response, {success: true});
          else json(response, prepared);
        };
        if (behavior === "hold" || behavior === "hold-reject") { state.held = reply; return; }
        reply(); return;
      }
      if (path === prefix + "/message-regenerate-commit") {
        exact(["csrf", "instance_id", "edit_token", "confirm"]);
        const prepared = state.prepared; state.prepared = null;
        if (!prepared || prepared.edit_token !== fields.edit_token || !fields.edit_token.startsWith("regenerate-") || fields.confirm !== "regenerate" || state.snapshot.status !== "idle") throw Error("Invalid, stale or replayed regeneration commit");
        const behavior = state.commitBehavior; state.commitBehavior = null;
        if (behavior === "reject" || behavior === "expired") { json(response, {error: "Source changed or token expired; prepare again"}, 409); return; }
        if (behavior === "unknown") { json(response, {error: "Regeneration outcome unknown; review conversation"}, 503); return; }
        if (behavior === "malformed") { json(response, {success: true}); return; }
        if (behavior === "wrong-session") { json(response, {...state.snapshot, session_id: "other-chat", instance_id: "other-instance"}); return; }
        const end = state.snapshot.messages.findIndex(message => message.id === owners[prepared.message_id]);
        const prefixRows = state.snapshot.messages.slice(0, end + 1), old = structuredClone(state.snapshot);
        state.update({status: "switching"});
        Object.assign(state.snapshot, {instance_id: "regenerated-instance", revision: 1, status: "running", cancel_token: "regenerated-turn-token", cancel_requested: false,
          messages: prefixRows, activities: []});
        const ack = structuredClone(state.snapshot);
        if (behavior === "fast-terminal") Object.assign(state.snapshot, {revision: 2, status: "idle", cancel_token: "", messages: [...prefixRows, {id: "regenerated-answer", role: "assistant", text: "Fresh regenerated answer exactly once.", html: "<p>Fresh regenerated answer exactly once.</p>", can_regenerate: true}]});
        state.previous = old;
        if (behavior === "hold" || behavior === "fast-terminal") { state.held = () => json(response, ack); return; }
        json(response, ack); return;
      }
      if (path === prefix + "/cancel") {
        exact(["csrf", "instance_id", "cancel_token"]);
        if (state.snapshot.status !== "running" || fields.cancel_token !== "regenerated-turn-token") throw Error("Stop must target regenerated turn");
        state.update({cancel_requested: true}); json(response, {success: true}); return;
      }
      throw Error(`Forbidden request: ${request.method} ${path}; regeneration never prompts, edits, branches, forks, switches or activates`);
    } catch (error) { state.errors.push(error.message); json(response, {error: error.message}, 400); }
  });
  return {server, state};
}
