// Component fixture only: real DOM and production CSS/scripts; no manager/runtime.
export function installFixture() {
  const config = new URLSearchParams(location.search);
  const projects = {a: '00000000-0000-4000-8000-000000000001', b: '00000000-0000-4000-8000-000000000002'};
  const projectID = project => projects[project] || project;
  const root = document.querySelector('#workspace');
  root.dataset.session = config.get('viewed') ?? 'saved-two';
  const instance = config.get('live') === '1' ? 'worker-a' : '';
  if (instance) {
    const live = document.createElement('section'); live.id = 'live-session';
    Object.assign(live.dataset, {runtime: 'true', project: projects.a, session: 'active', instance});
    const title = document.createElement('span'); title.dataset.liveTitle = ''; title.textContent = 'Active conversation';
    const rename = document.createElement('button'); rename.dataset.workflowRename = ''; rename.textContent = 'Rename';
    live.append(title, rename); root.querySelector('main').append(live);

  }
  const name = 'Permanent <img src=x onerror=alert(1)> & saved conversation ' + 'long-name-'.repeat(14);
  const inventory = project => ({project_id: projectID(project), instance_id: project === 'a' ? instance : '',
    available: true, delete_supported: true, active_session_id: project === 'a' && instance ? 'active' : '',
    sessions: project === 'a' ? [{session_id: 'active', name: 'Active conversation'}, {session_id: 'saved-one', name}, {session_id: 'saved-two', name: 'Untouched saved conversation'}] : [{session_id: 'saved-one', name: 'Other workspace conversation'}],
    has_more: false, next_offset: 0});
  const inventories = {a: inventory('a'), b: inventory('b')};
  if (config.has('supported')) {
    const value = config.get('supported');
    for (const data of Object.values(inventories)) {
      if (value === 'missing') delete data.delete_supported;
      else data.delete_supported = value === 'true' ? true : value === 'false' ? false : value === 'string-true' ? 'true' : value;
    }
  }
  if (config.get('activeField') === 'missing') delete inventories.a.active_session_id;
  if (config.get('activeField') === 'number') inventories.a.active_session_id = 1;
  if (config.get('available') === 'false') inventories.a.available = false;
  const bootstrap = {csrf: 'fixture-csrf-not-a-secret', version: 'fixture', view: 'projects', project: projects.a,
    session: root.dataset.session, hostSettingsEnabled: false, apiKeyEnabled: false, tls: false, pairingCode: '',
    projects: Object.entries(projects).map(([name, id]) => ({id, name: `Workspace ${name}`, path: `/fictional/${name}`, available: true, trustRemembered: false, skillsEnabled: false, pinned: false})),
    sessions: [], live: instance ? {project: projects.a, session: 'active', instance, title: 'Active conversation', renameAvailable: true, renameDisabled: false, newDisabled: false} : null};
  document.querySelector('[data-react-page="shell"]').dataset.reactProps = JSON.stringify(bootstrap);
  const requests = [], pending = [], navigation = [], events = [], violations = [], renames = [];
  const heldProjects = new Set(), inventoryPending = [];
  const nativeSetTimeout = window.setTimeout.bind(window), nativeClearTimeout = window.clearTimeout.bind(window);
  const deadlines = new Map(); let timerID = -1;
  window.setTimeout = (callback, delay, ...args) => {
    if (delay !== 15000) return nativeSetTimeout(callback, delay, ...args);
    const id = timerID--; deadlines.set(id, () => callback(...args)); return id;
  };
  window.clearTimeout = id => { if (!deadlines.delete(id)) nativeClearTimeout(id); };
  const violation = message => { violations.push(message); throw new Error(message); };
  window.fetch = (input, options = {}) => {
    const url = new URL(String(input), location.origin), method = options.method || 'GET';
    const request = {url: url.pathname + url.search, method, credentials: options.credentials,
      headers: options.headers, body: String(options.body ?? ''), aborted: false};
    requests.push(request);
    if (url.origin !== location.origin) return violation('External transport forbidden');
    if (method === 'GET' && request.url === '/access/browsers') {
      if (options.body !== undefined || options.credentials !== 'same-origin' || options.cache !== 'no-store' || options.redirect !== 'error' || options.headers?.Accept !== 'application/json') return violation('Unexpected browser inventory contract');
      return Promise.resolve(new Response(JSON.stringify({limit: 8, browsers: []}), {headers: {'Content-Type': 'application/json'}}));
    }
    const read = /^\/projects\/(00000000-0000-4000-8000-00000000000[12])\/sidebar-sessions\?offset=0$/.exec(request.url);
    if (method === 'GET' && read) {
      const project = Object.keys(projects).find(key => projects[key] === read[1]);
      if (!heldProjects.has(project)) return Promise.resolve(new Response(JSON.stringify(inventories[project]), {headers: {'Content-Type': 'application/json'}}));
      return new Promise((resolve, reject) => {
        inventoryPending.push({project, resolve, reject});
        options.signal.addEventListener('abort', () => { request.aborted = true; reject(new DOMException('Inventory canceled', 'AbortError')); }, {once: true});
      });
    }
    if (method !== 'POST' || !/^\/projects\/00000000-0000-4000-8000-00000000000[12]\/sessions\/(?:active|saved-one|saved-two)\/delete$/.test(url.pathname) || url.search) return violation('Unexpected transport: ' + method + ' ' + request.url);
    return new Promise((resolve, reject) => {
      pending.push({request, resolve, reject});
      options.signal.addEventListener('abort', () => { request.aborted = true; reject(new DOMException('Controlled timeout', 'AbortError')); }, {once: true});
    });
  };
  const nativeHTMX = config.get('htmx') === 'native' ? window.htmx : null;
  const nativeAjax = nativeHTMX?.ajax.bind(nativeHTMX);
  window.htmx = nativeHTMX || {process() {}};
  window.htmx.ajax = async (method, url, options) => {
    navigation.push({method, url, target: options.target, swap: options.swap,
      source: options.source?.closest('[data-sidebar-project]')?.dataset.sidebarProject || options.source?.id,
      push: options.source?.getAttribute('hx-push-url'), sync: options.source?.closest('[hx-sync]')?.getAttribute('hx-sync')});
    if (method !== 'GET' || !/^\/\?view=projects&project=00000000-0000-4000-8000-00000000000[12]&new=1$/.test(url)) violation('Unexpected navigation: ' + method + ' ' + url);
    if (nativeAjax) return nativeAjax(method, url, options);
  };
  document.addEventListener('snow:react-ready', async () => {
    window.SnowConversation = {rename: trigger => renames.push(trigger)};
    const other = [...document.querySelectorAll('[data-sidebar-project]')].find(row => row.dataset.sidebarProject === projects.b);
    other?.querySelector('[data-workspace-toggle]')?.click();
    if (innerWidth < 768) SnowShell.navigation(true);
    await until(() => document.querySelectorAll('[data-shell-session]').length === 4 && !document.querySelector('[data-workspace-sessions][aria-busy]'), 'React inventories');
    window.fixtureReady = true;
  }, {once: true});
  document.addEventListener('snow:session-deleted', event => events.push(structuredClone(event.detail)));
  document.addEventListener('snow:session-select', () => violation('Deletion must not select/activate a session'));
  document.addEventListener('snow:session-new', () => violation('Deletion must not request runtime New'));
  window.addEventListener('error', event => violations.push(event.message));
  window.addEventListener('unhandledrejection', event => violations.push(String(event.reason)));
  const $ = selector => document.querySelector(selector.replace(/\[data-sidebar-project=([ab])\]/g, (_, key) => `[data-sidebar-project="${projects[key]}"]`));
  const row = (project = 'a', session = 'saved-one') => $(`[data-sidebar-project="${projectID(project)}"] [data-shell-session="${session}"]`);
  const button = (project = 'a', session = 'saved-one') => row(project, session)?.querySelector('[data-shell-session-menu]');
  function until(predicate, label) {
    if (predicate()) return Promise.resolve();
    return new Promise((resolve, reject) => {
      const observer = new MutationObserver(() => { if (predicate()) { cleanup(); resolve(); } });
      const timeout = nativeSetTimeout(() => { cleanup(); reject(new Error('DOM condition deadline: ' + label)); }, 3000);
      function cleanup() { observer.disconnect(); nativeClearTimeout(timeout); }
      observer.observe(document, {subtree: true, childList: true, attributes: true, characterData: true});
    });
  }
  window.f = {project: projectID, $, row, button, requests, pending, navigation, events, violations, renames, inventories, name, deadlines,
    newLink: project => [...document.querySelectorAll('[data-sidebar-project]')].find(row => row.dataset.sidebarProject === projectID(project))?.querySelector('[data-shell-project-new]'),
    async replaceRoot(project) {
      const old = $('#workspace'), next = old.cloneNode(true);
      const shellRoot = next.querySelector('[data-react-page="shell"]');
      const props = JSON.parse(shellRoot.dataset.reactProps);
      shellRoot.replaceChildren(); next.querySelector('#shell-navigation-root').replaceChildren();
      if (project) {
        next.dataset.project = projectID(project); props.project = projectID(project);
        props.live = null; next.querySelector('#live-session')?.remove();
      }
      shellRoot.dataset.reactProps = JSON.stringify(props);
      old.dispatchEvent(new CustomEvent('htmx:beforeCleanupElement', {bubbles: true, detail: {elt: old}}));
      old.replaceWith(next);
      next.dispatchEvent(new CustomEvent('htmx:afterSwap', {bubbles: true, detail: {elt: next, target: next}}));
      await this.idle();
    },
    holdInventory: project => heldProjects.add(project),
    releaseInventory(project) {
      heldProjects.delete(project);
      const read = inventoryPending.findIndex(item => item.project === project);
      if (read < 0) throw new Error('No held inventory for ' + project);
      inventoryPending.splice(read, 1)[0].resolve(new Response(JSON.stringify(inventories[project]), {headers: {'Content-Type': 'application/json'}}));
    },
    posts: () => requests.filter(request => request.method === 'POST'),
    until,
    idle: () => until(() => window.SnowShell?.requests.size === 0 && !document.querySelector('[data-workspace-sessions][aria-busy]'), 'inventory idle'),
    async menu(project = 'a', session = 'saved-one') { const trigger = button(project, session); trigger.focus(); trigger.click(); await until(() => !!$('.snow-menu'), 'React menu'); },
    deleteItem: () => $('.snow-menu [data-session-delete]'),
    async open(project = 'a', session = 'saved-one') { await this.menu(project, session); this.deleteItem()?.click(); await until(() => !!$('#session-delete-dialog')?.open, 'React confirmation'); },
    async confirm() { const checkbox = $('[data-delete-confirm]'); if (!checkbox.checked) checkbox.click(); await until(() => !$('[data-delete-submit]').disabled, 'React acknowledgement'); $('[data-delete-submit]').click(); },
    receipt(overrides = {}) { return {project_id: projects.a, session_id: 'saved-one', instance_id: instance, deleted: true, ...overrides}; },
    reply(value, status = 200, contentType = 'application/json') {
      const body = typeof value === 'string' ? value : JSON.stringify(value);
      pending.at(-1).resolve(new Response(body, {status, headers: {'Content-Type': contentType}}));
    },
    succeed(overrides = {}) {
      const receipt = this.receipt(overrides);
      const catalog = inventories[Object.keys(projects).find(key => projects[key] === receipt.project_id)];
      catalog.sessions = catalog.sessions.filter(row => row.session_id !== receipt.session_id);
      this.reply(receipt);
    },
    timeout() { if (deadlines.size !== 1) throw new Error('Expected exactly one deletion deadline'); const callbacks = [...deadlines.values()]; deadlines.clear(); callbacks.forEach(callback => callback()); },
    async failed() { await until(() => $('[data-delete-error]')?.hidden === false, 'deletion failure'); await this.idle(); },
    async succeeded() { await until(() => !$('#session-delete-dialog')?.open, 'deletion closed'); await this.idle(); },
    visibleHit(node) {
      const r = node.getBoundingClientRect(), hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
      return r.width > 0 && r.height > 0 && r.left >= 0 && r.top >= 0 && r.right <= innerWidth + 1 && r.bottom <= innerHeight + 1 && (hit === node || node.contains(hit));
    }
  };
}

export function fixtureHTML(css, source, htmx, appPrefix) {
  return `<!doctype html><html lang="en" data-theme="dark"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="htmx-config" content='{"historyCacheSize":0,"allowScriptTags":false,"includeIndicatorStyles":false}'>${css.map(path => `<link rel="stylesheet" href="${path}">`).join('')}<title>Fictional React deletion component fixture</title></head><body><input type="hidden" name="csrf" value="fixture-csrf-not-a-secret"><div id="workspace" class="app-layout" data-project="00000000-0000-4000-8000-000000000001" data-session="saved-two"><div id="shell-navigation-root"></div><div id="shell-react-root" data-react-page="shell"></div><main class="workspace" id="workspace-content"><textarea id="fixture-draft">Unrelated unsent draft</textarea></main></div><script>${htmx}</script><script>(${installFixture.toString()})();</script><script>${source}</script><script>${appPrefix}</script><script type="module" src="/static/generated/app.js"></script></body></html>`;
}
