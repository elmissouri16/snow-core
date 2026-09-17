// HTTP/SSE-only fixture: real production Go HTML, queue controller and browser.
import {createServer} from "node:http";
import {extname} from "node:path";

export const originals = [
  {id: "root-user", role: "user", text: "Original active root prompt.", can_edit: false},
  {id: "root-answer", role: "assistant", text: "Original answer still running.", html: "<p>Original answer still running.</p>", can_regenerate: false}
];
export const queueItem = (id, text = `Pending ${id} text.`, state = "pending") => ({id, text, state});
export function transport(files, fixture) {
  const state = {streams: new Set(), requests: [], errors: [], reads: 0, held: null};
  const json = (response, value, status = 200) => { response.writeHead(status, {"Content-Type": "application/json", "Cache-Control": "no-store"}); response.end(JSON.stringify(value)); };
  state.emit = (value = state.snapshot, instance = value.instance_id) => {
    for (const stream of state.streams) if (stream.instance === instance && !stream.response.destroyed && !stream.response.writableEnded) stream.response.write(`event: snapshot\ndata: ${JSON.stringify(value)}\n\n`);
  };
  state.update = fields => { Object.assign(state.snapshot, structuredClone(fields), {revision: state.snapshot.revision + 1}); state.emit(); };
  state.queue = fields => state.update({queue: {...state.snapshot.queue, ...structuredClone(fields), revision: state.snapshot.queue.revision + 1}});
  state.release = () => { const held = state.held; state.held = null; held?.(); };
  state.reset = () => {
    state.release(); for (const stream of state.streams) stream.response.end(); state.streams.clear();
    Object.assign(state, {snapshot: structuredClone(fixture.snapshot), requests: [], errors: [], reads: 0, subscriptions: 0, behavior: null, held: null, nextID: 1});
    Object.assign(state.snapshot, {status: "running", cancel_token: "root-cancel-token", cancel_requested: false, messages: structuredClone(originals), activities: [], permission: null, input: null,
      queue: {token: "root-queue-token", revision: 1, can_enqueue: true, items: []}});
  };
  state.deliver = id => {
    const item = state.snapshot.queue.items.find(item => item.id === id);
    if (!item) throw Error("Cannot deliver missing fixture item");
    state.update({queue: {...state.snapshot.queue, revision: state.snapshot.queue.revision + 1, items: state.snapshot.queue.items.filter(item => item.id !== id)},
      messages: [...state.snapshot.messages,
        {id: `delivered-${id}`, role: "user", text: item.text, can_edit: true},
        {id: `step-${id}`, role: "assistant", text: "Queued delivery tool step.", html: "<p>Queued delivery tool step.</p>", can_regenerate: false,
          tools: [{id: `tool-${id}`, owner_id: `step-${id}`, tool: "read", status: "completed", output: "Queued tool output.", output_available: true}]},
        {id: `answer-${id}`, role: "assistant", text: "Answer for delivered queued input.", html: "<p>Answer for delivered queued input.</p>", can_regenerate: true}]});
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
        const project = [state.snapshot.project_id, "00000000-0000-4000-8000-000000000001"].find(id => path === `/projects/${id}/sidebar-sessions`);
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
        const stream = {instance, response}; state.subscriptions++; state.streams.add(stream); response.on("close", () => state.streams.delete(stream));
        response.write(`event: snapshot\ndata: ${JSON.stringify(state.snapshot)}\n\n`); return;
      }
      let body = "";
      for await (const chunk of request) { body += chunk; if (Buffer.byteLength(body) > 512 * 1024) throw Error("Request exceeds fixture bound"); }
      const params = new URLSearchParams(body), fields = Object.fromEntries(params);
      state.requests.push({path, method: request.method, fields});
      if (state.requests.length > 1000) throw Error("Unexpected mutation loop");
      if (request.method !== "POST" || fields.csrf !== "test-csrf" || fields.instance_id !== state.snapshot.instance_id) throw Error("Expected CSRF-protected, instance-bound POST");
      const exact = keys => { if ([...params.keys()].length !== keys.length || [...params.keys()].some(key => !keys.includes(key))) throw Error("Unexpected or duplicate request fields"); };
      if (path === prefix + "/cancel") {
        exact(["csrf", "instance_id", "cancel_token"]);
        if (!["running", "permission", "input"].includes(state.snapshot.status) || fields.cancel_token !== state.snapshot.cancel_token) throw Error("Stop must target observed root turn");
        state.update({cancel_requested: true, queue: {...state.snapshot.queue, revision: state.snapshot.queue.revision + 1, can_enqueue: false, items: state.snapshot.queue.items.map(item => ({...item, state: item.state === "pending" ? "held" : item.state}))}});
        json(response, {success: true}); return;
      }
      if (!["queue-enqueue", "queue-update", "queue-remove"].some(action => path === prefix + "/" + action)) throw Error(`Forbidden request: ${request.method} ${path}; queue never prompts, branches, edits, regenerates, switches or activates`);
      const action = path.split("/").at(-1), base = ["csrf", "instance_id", "session_id", "queue_token", "queue_revision"];
      exact([...base, ...(action === "queue-enqueue" ? ["text"] : action === "queue-update" ? ["item_id", "text"] : ["item_id"])]);
      if (fields.session_id !== state.snapshot.session_id || fields.queue_token !== state.snapshot.queue.token || !/^(0|[1-9][0-9]*)$/.test(fields.queue_revision) || !Number.isSafeInteger(Number(fields.queue_revision))) throw Error("Queue mutation must bind exact session/token and canonical safe CAS revision");
      if (fields.queue_revision !== String(state.snapshot.queue.revision)) { json(response, {error: "Queue revision changed; review before explicitly saving again"}, 409); return; }
      const behavior = state.behavior; state.behavior = null;
      if (behavior === "reject" || behavior === "closing") {
        if (behavior === "closing") state.queue({can_enqueue: false});
        json(response, {error: "Queue changed or closed; review pending input"}, 409); return;
      }
      if (behavior === "hold-reject") { state.held = () => json(response, {error: "Item already delivered; your local text is retained for review"}, 409); return; }
      const reviewRemoval = action === "queue-remove" && state.snapshot.queue.items.some(item => item.id === fields.item_id && ["held", "uncertain"].includes(item.state));
      if (!reviewRemoval && (!state.snapshot.queue.can_enqueue || !["running", "permission", "input"].includes(state.snapshot.status))) throw Error("Client attempted a closed queue mutation");
      let items = structuredClone(state.snapshot.queue.items);
      if (action !== "queue-remove" && (typeof fields.text !== "string" || !fields.text.trim() || fields.text.includes("\0") || Buffer.byteLength(fields.text) > 65536)) throw Error("Invalid queued text bypassed browser guard");
      if (action === "queue-enqueue") {
        if (items.length >= 8 || items.reduce((sum, item) => sum + Buffer.byteLength(item.text), 0) + Buffer.byteLength(fields.text) > 256 * 1024) { json(response, {error: "Queue full: 8 items / 256 KiB total"}, 409); return; }
        items.push(queueItem(`queued-${state.nextID++}`, fields.text));
      } else {
        const index = items.findIndex(item => item.id === fields.item_id && (item.state === "pending" || reviewRemoval));
        if (index < 0) throw Error("Cannot mutate a consumed, held, uncertain or unknown item");
        if (action === "queue-remove") items.splice(index, 1); else items[index].text = fields.text;
      }
      state.queue({items});
      const ack = structuredClone(state.snapshot);
      const reply = () => {
        if (behavior === "unknown") json(response, {error: "Outcome unknown; may already be delivered"}, 503);
        else if (behavior === "malformed") json(response, {success: true});
        else if (behavior === "missing-queue") { delete ack.queue; json(response, ack); }
        else if (behavior === "wrong-instance") json(response, {...ack, instance_id: "foreign-instance"});
        else if (behavior === "wrong-session") json(response, {...ack, session_id: "foreign-session"});
        else if (behavior === "wrong-token") json(response, {...ack, queue: {...ack.queue, token: "foreign-root-token"}});
        else if (behavior === "unsafe-revision") json(response, {...ack, queue: {...ack.queue, revision: Number.MAX_SAFE_INTEGER + 1}});
        else json(response, ack);
      };
      if (behavior === "hold") { state.held = reply; return; }
      reply();
    } catch (error) { state.errors.push(error.message); json(response, {error: error.message}, 400); }
  });
  return {server, state};
}
