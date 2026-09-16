// Only public DTOs and HTTP are simulated. The shell and all executable assets
// come from the production Go exporter; React and native navigation are never replaced or patched.
import {createServer} from 'node:http';
import {extname} from 'node:path';

export const projectID = '11111111-1111-4111-8111-111111111111';
export const archivedID = '22222222-2222-4222-8222-222222222222';
export const missingID = '33333333-3333-4333-8333-333333333333';
export const csrf = 'a'.repeat(64); // Deliberately fake, public fixture value.
export const malicious = '<img src=x onerror="window.fixtureXSS=1"> & café';
const project = {id: projectID, name: malicious, path: '/fixture/workspaces/alpha', available: true, state: 'available', issue: '', pinned: false};
export const organization = {
  csrf, error: '', organization: {
    projects: [project],
    archived: [
      {...project, id: archivedID, name: 'Archived workspace', pinned: true},
      {...project, id: missingID, name: 'Missing workspace', available: false, state: 'missing', issue: 'Original folder unavailable'},
    ],
    project, sessions: [
      {id: 'session-active', name: 'Alpha investigation', updated: 'Today', pinned: false, archived: false},
      {id: 'session-archived', name: malicious, updated: 'Yesterday', pinned: true, archived: true},
      {id: 'session-empty', name: '', updated: 'Yesterday', pinned: false, archived: false},
    ],
    offset: 25, nextURL: `/?offset=50&project=${projectID}&view=organization`,
    archivedNextURL: '/?archived_offset=25&view=organization', live: false,
  },
};
export function summary(name = 'Alpha running') {
  return {
    updated_at: '2026-07-10T12:00:00Z',
    counts: {registered: 1, running: 1, permissions: 1, questions: 0, failed: 0, recovery: 0, queued: 2, review: 1},
    projects: [{project_id: projectID, name, project_url: `/?view=projects&project=${projectID}`,
      session_id: 'session-active', session_url: `/?view=projects&project=${projectID}&session=session-active`,
      folder_state: 'available', runtime_state: 'permission', host_running: true, permissions: 1,
      questions: 0, failed: false, recovery: false, recovery_state: '', queued: 2, review: 1, unavailable: false}],
  };
}
const escape = value => value.replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');

export function transport(files, shell) {
  const state = {requests: [], errors: [], allErrors: [], next: null, held: [], current: summary(), organization: structuredClone(organization), active: 0, maxActive: 0, aborts: 0};
  const root = (view, props) => `<div data-react-page="${view}" data-react-props="${escape(typeof props === 'string' ? props : JSON.stringify(props))}"></div>`;
  state.page = (view, props) => {
    const settingsPage = view === 'host-settings' || view === 'browser-access';
    const content = settingsPage ? '<p>Isolated settings fixture workspace</p>' : root(view, props ?? (view === 'activity' ? {registryEnabled: true, error: ''} : state.organization));
    // Keep the production shell/head, asset order, and navigation boundary;
    // only fixture presentation data and the owned root are supplied.
    let html = shell.replace('data-view="overview"', `data-view="${view}"`)
      .replace(/<main id="workspace-content"[^>]*>[\s\S]*?<\/main>/, `<main id="workspace-content" class="workspace" tabindex="-1">${content}</main>`);
    // Settings is now an ordinary React child of the one production shell root.
    // Preserve its public bootstrap shape; no artificial nested roots are added.
    let shellFound = false;
    html = html.replace(/(<div\b[^>]*data-react-page="shell"[^>]*data-react-props=")[^"]*(")/, (attribute, start, end) => {
      shellFound = true;
      const encoded = attribute.slice(start.length, -end.length);
      const decode = value => value.replaceAll('&quot;', '"').replaceAll('&#34;', '"').replaceAll('&#39;', "'").replaceAll('&lt;', '<').replaceAll('&gt;', '>').replaceAll('&amp;', '&');
      const bootstrap = JSON.parse(decode(encoded));
      bootstrap.view = settingsPage ? 'overview' : view;
      if (settingsPage) {
        const settings = state.hostProps || {csrf, enabled: true, projects: [{id: projectID, name: malicious}]};
        Object.assign(bootstrap, {csrf: settings.csrf, hostSettingsEnabled: settings.enabled,
          project: '', session: '', sessions: [], live: null,
          projects: settings.projects.map(project => ({...project, path: '/fixture/workspaces/alpha', available: true, trustRemembered: false, skillsEnabled: false, pinned: false}))});
      }
      return start + escape(JSON.stringify(bootstrap)) + end;
    });
    if (!shellFound) throw Error('Production React shell bootstrap marker changed');
    if (!html.includes(content)) throw Error('Production shell main marker changed');
    return html;
  };
  state.release = () => { for (const release of state.held.splice(0)) release(); };
  state.reset = () => {
    state.release(); state.requests = []; state.errors = []; state.next = null;
    state.current = summary(); state.organization = structuredClone(organization); state.maxActive = state.active; state.aborts = 0; state.invalidNextNavigation = false; state.holdNextNavigation = false; state.resetSettings?.();
  };
  const json = (response, value, status = 200) => {
    response.writeHead(status, {'Content-Type': 'application/json', 'Cache-Control': 'no-store'}); response.end(JSON.stringify(value));
  };
  const server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url, 'http://localhost'), path = url.pathname;
      if (request.method === 'GET' && files.has(path)) {
        response.writeHead(200, {'Content-Type': {'.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml', '.woff2': 'font/woff2'}[extname(path)] || 'application/octet-stream'});
        response.end(files.get(path)); return;
      }
      if (path === '/favicon.ico') { response.writeHead(204); response.end(); return; }
      const record = {method: request.method, path, query: url.search, headers: request.headers}; state.requests.push(record);
      if (state.handleSettings && await state.handleSettings(request, response, record, url)) return;
      if (path === '/' && request.method === 'GET') {
        const view = url.searchParams.get('view') || 'activity';
        if (!['activity', 'organization', 'host-settings', 'browser-access'].includes(view)) throw Error(`Unexpected fixture page ${view}`);
        let props;
        if (url.searchParams.has('malformed')) props = state.badProps;
        const html = state.page(view, props);
        response.writeHead(200, {'Content-Type': 'text/html', 'Cache-Control': 'no-store'});
        const nativeNavigation = request.headers['x-snow-navigation'] === 'workspace';
        if (nativeNavigation && state.invalidNextNavigation) {
          state.invalidNextNavigation = false;
          response.end('<p>Invalid fixture fragment</p>');
          return;
        }
        // Production native navigation receives only the replaceable workspace.
        const body = nativeNavigation ? html.match(/<div id="workspace"[\s\S]*<\/div>\s*<\/body>/)?.[0].replace(/\s*<\/body>$/, '') || html : html;
        if (nativeNavigation && state.holdNextNavigation) {
          state.holdNextNavigation = false;
          state.held.push(() => { if (!response.destroyed) response.end(body); });
          return;
        }
        response.end(body); return;
      }
      if (path === '/access/browsers' && request.method === 'GET') { json(response, {browsers: [], limit: 8}); return; } // Shared production settings dialog reads a public empty inventory.
      if (path === '/activity' && request.method === 'GET') {
        if (url.search || request.headers.accept !== 'application/json') throw Error('Activity must use the exact read-only JSON GET contract');
        state.active++; state.maxActive = Math.max(state.maxActive, state.active);
        let done = false;
        const finish = () => { if (!done) { done = true; state.active--; } };
        response.on('finish', finish);
        response.on('close', () => { if (!response.writableFinished) state.aborts++; finish(); });
        const behavior = state.next || {}; state.next = null;
        const payload = behavior.raw ?? JSON.stringify(behavior.body ?? state.current);
        const send = () => {
          if (response.destroyed) return;
          response.writeHead(behavior.status || 200, {'Content-Type': 'application/json', 'Cache-Control': 'no-store', ...behavior.headers});
          if (behavior.holdBody) { response.write(payload.slice(0, Math.max(1, payload.length >> 1))); state.held.push(() => response.end(payload.slice(Math.max(1, payload.length >> 1)))); }
          else response.end(payload);
        };
        if (behavior.holdHeaders) state.held.push(send); else send();
        return;
      }
      if (request.method === 'POST') {
        let body = ''; for await (const chunk of request) { body += chunk; if (body.length > 16384) throw Error('POST body too large'); }
        if (request.headers['content-type'] !== 'application/x-www-form-urlencoded') throw Error('Expected native URL-encoded form');
        const params = new URLSearchParams(body), fields = Object.fromEntries(params); record.fields = fields;
        const session = path.match(new RegExp(`^/projects/${projectID}/sessions/organization/(pin|unpin|archive|restore)$`));
        const workspace = path.match(/^\/projects\/([0-9a-f-]{36})\/organization\/(rename|pin|unpin|archive|restore)$/);
        if (!session && !workspace) throw Error(`Forbidden mutation: ${path}`);
        const action = session?.[1] || workspace[2];
        const keys = ['csrf', ...(session ? ['session_id', 'offset'] : []), ...(action === 'rename' ? ['name'] : []), ...(['archive', 'restore'].includes(action) ? ['confirm'] : [])].sort();
        if (params.size !== keys.length || JSON.stringify(Object.keys(fields).sort()) !== JSON.stringify(keys)) throw Error(`Unexpected POST fields: ${body}`);
        if (fields.csrf !== csrf) throw Error('CSRF mismatch');
        if (session && (fields.offset !== '25' || !organization.organization.sessions.some(row => row.id === fields.session_id))) throw Error('Session identity/offset mismatch');
        if (workspace && ![projectID, archivedID].includes(workspace[1])) throw Error('Unavailable/unknown workspace cannot mutate');
        if (keys.includes('confirm') && fields.confirm !== action) throw Error('Confirmation mismatch');
        response.writeHead(204, {'Cache-Control': 'no-store'}); response.end(); return; // Record only; mutate nothing.
      }
      throw Error(`Forbidden request: ${request.method} ${path}`);
    } catch (error) { state.errors.push(error.message); state.allErrors.push(error.message); if (!response.headersSent) json(response, {error: error.message}, 400); else response.end(); }
  });
  return {server, state};
}
