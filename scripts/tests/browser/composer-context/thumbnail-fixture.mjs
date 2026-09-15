// Loopback-only asset transport, not a manager or an alternate renderer. Sent
// images take a real HTTP/cookie/PNG decode path; public DTO fetches still use
// the existing recorder. Only preloaded fixture files can be requested.
import {createServer} from "node:http";
import {readFile, readdir} from "node:fs/promises";
import {join} from "node:path";
import {pathToFileURL} from "node:url";

export async function thumbnailFixture(directory, here) {
  const assets = new Map(), requests = [];
  const types = {js: "text/javascript", css: "text/css", html: "text/html", png: "image/png", woff2: "font/woff2", svg: "image/svg+xml"};
  const load = async (path, url) => {
    const data = await readFile(path);
    if (data.length > 4 * 1024 * 1024) throw new Error("Oversized thumbnail fixture asset: " + url);
    assets.set(url, {data, type: types[path.split(".").at(-1)] || "application/octet-stream"});
  };
  const tree = async (path, url) => {
    for (const item of await readdir(path, {withFileTypes: true})) {
      if (item.isDirectory()) await tree(join(path, item.name), url + "/" + item.name);
      else if (item.isFile()) await load(join(path, item.name), url + "/" + item.name);
    }
  };
  await tree(join(directory, "static"), "/static");
  for (const item of await readdir(here)) if (item.endsWith(".js")) await load(join(here, item), "/browser/composer-context/" + item);
  // Keep the shared recorder implementation, binding its historical fixture ID
  // to the current Go-exported identity without editing another suite's source.
  const manifest = JSON.parse(await readFile(join(directory, "fixtures.json"), "utf8"));
  const project = manifest.find(item => item.name === "workflow").snapshot.project_id;
  const recorder = await readFile(join(here, "../conversation-workflow/fixture.js"), "utf8");
  const imagePath = `/projects/${project}/runtime/images/thumbnail-user/1?instance_id=instance-one&session_id=session-one`;
  // The React image owner reads bounded bytes with fetch before creating a
  // decoded Blob preview. Let only this exact authenticated image GET escape
  // the public-DTO recorder; all other traffic retains its existing guards.
  const imageTransport = `(() => {
    const recorded = window.fetch;
    window.fetch = (input, options = {}) => {
      const url = new URL(String(input), location.href);
      if ((options.method || 'GET') === 'GET' && url.origin === location.origin &&
          url.pathname + url.search === ${JSON.stringify(imagePath)} && !url.hash)
        return nativeImageFetch(input, options);
      return recorded(input, options);
    };
  })();`;
  assets.set("/browser/conversation-workflow/fixture.js", {data: Buffer.from(
    "const nativeImageFetch = window.fetch.bind(window);\n" + recorder.replaceAll("project-one", project) + imageTransport), type: "text/javascript"});
  for (const page of ["workflow", "workflow-queue"]) {
  let html = await readFile(join(directory, page + ".html"), "utf8");
  html = html.replaceAll(pathToFileURL(join(directory, "static")).href, "/static")
    .replaceAll(pathToFileURL(join(here, "..")).href, "/browser");
  assets.set("/" + page + ".html", {data: Buffer.from(html), type: "text/html"});
  }
  const png = await readFile(join(here, "fixture.png"));
  const server = createServer({requestTimeout: 5000, headersTimeout: 5000}, (request, response) => {
    const url = new URL(request.url, "http://127.0.0.1");
    const image = request.method === "GET" && url.pathname === "/projects/00000000-0000-4000-8000-000000000002/runtime/images/thumbnail-user/1" &&
      url.search === "?instance_id=instance-one&session_id=session-one";
    if (image) {
      const authenticated = (request.headers.cookie || "").split("; ").includes("snow-thumbnail-fixture=paired");
      if (requests.length >= 256) { response.writeHead(429); response.end(); return; }
      requests.push({url: request.url, authenticated});
      response.writeHead(authenticated ? 200 : 401, {"Content-Type": authenticated ? "image/png" : "text/plain", "Cache-Control": "no-store", "X-Content-Type-Options": "nosniff"});
      response.end(authenticated ? png : "Pairing required");
      return;
    }
    const asset = assets.get(url.pathname);
    if (!asset || request.method !== "GET") { response.writeHead(404); response.end(); return; }
    response.writeHead(200, {"Content-Type": asset.type, "Cache-Control": "no-store", ...(url.pathname === "/workflow.html" ? {"Set-Cookie": "snow-thumbnail-fixture=paired; HttpOnly; SameSite=Strict; Path=/"} : {})});
    response.end(asset.data);
  });
  await new Promise((resolve, reject) => { server.once("error", reject); server.listen(0, "127.0.0.1", resolve); });
  const origin = `http://127.0.0.1:${server.address().port}`;
  try {
    const denied = await fetch(origin + "/projects/00000000-0000-4000-8000-000000000002/runtime/images/thumbnail-user/1?instance_id=instance-one&session_id=session-one", {signal: AbortSignal.timeout(5000)});
    await denied.arrayBuffer();
    if (denied.status !== 401) throw new Error("Thumbnail fixture failed its unauthenticated-image denial check.");
  } catch (error) { server.closeAllConnections(); server.close(); throw error; }
  requests.length = 0;
  return {url: origin + "/workflow.html", urls: [origin + "/workflow.html", origin + "/workflow-queue.html"], requests, close: () => new Promise((resolve, reject) => { server.close(error => error ? reject(error) : resolve()); server.closeAllConnections(); })};
}
