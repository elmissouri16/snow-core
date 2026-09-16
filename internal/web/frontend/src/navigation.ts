export type NavigationHistory = 'push' | 'replace' | 'none';

export interface NavigationOptions {
  source?: Element | null;
  history?: NavigationHistory;
  body?: BodyInit | null;
  method?: 'GET' | 'POST';
}

interface NavigationLifecycle {
  beforeReplace(target: HTMLElement): void;
  afterReplace(target: HTMLElement): void;
}

interface ScrollPosition {x: number; y: number}
interface InternalNavigationOptions extends NavigationOptions {
  entryID?: string;
  restoreScroll?: ScrollPosition | null;
}

interface NavigationDetail {
  target: HTMLElement;
  source: Element | null;
  requestConfig: {path: string; method: 'GET' | 'POST'};
  aborted?: boolean;
}

const maximumResponseBytes = 8 * 1024 * 1024;
const navigationTimeoutMilliseconds = 10_000;
let active: AbortController | null = null;
let lifecycle: NavigationLifecycle = {beforeReplace() {}, afterReplace() {}};
if ('scrollRestoration' in window.history) window.history.scrollRestoration = 'manual';

function historyState(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

const maximumScrollEntries = 256;
const scrollPositions = new Map<string, ScrollPosition>();

function entryID(value: unknown): string | null {
  const id = historyState(value).snowNavigationEntry;
  return typeof id === 'string' && id.length > 0 && id.length <= 64 ? id : null;
}

function newEntryID(): string {
  if (typeof window.crypto.randomUUID === 'function') return window.crypto.randomUUID();
  const value = new Uint8Array(16);
  window.crypto.getRandomValues(value);
  value[6] = (value[6] & 0x0f) | 0x40;
  value[8] = (value[8] & 0x3f) | 0x80;
  const hex = Array.from(value, byte => byte.toString(16).padStart(2, '0')).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

function currentScroll(): ScrollPosition {
  return {x: Math.min(Math.max(0, window.scrollX), 10_000_000), y: Math.min(Math.max(0, window.scrollY), 10_000_000)};
}

function rememberScroll(id: string, scroll = currentScroll()) {
  if (!scrollPositions.has(id) && scrollPositions.size >= maximumScrollEntries) {
    const oldest = scrollPositions.keys().next().value;
    if (oldest) scrollPositions.delete(oldest);
  }
  scrollPositions.set(id, scroll);
}

const initialEntryID = entryID(window.history.state);
let committedEntryID = initialEntryID || newEntryID();
if (!initialEntryID) {
  window.history.replaceState({...historyState(window.history.state), snowNavigation: true,
    snowNavigationEntry: committedEntryID}, '', window.location.href);
}
rememberScroll(committedEntryID);
window.addEventListener('scroll', () => {
  if (!active) rememberScroll(committedEntryID);
}, {passive: true});

function dispatch(type: string, detail: NavigationDetail) {
  document.dispatchEvent(new CustomEvent(type, {detail}));
}

async function boundedHTML(response: Response, signal: AbortSignal): Promise<string> {
  if (!response.body) throw new Error('Navigation response is empty');
  const reader = response.body.getReader();
  const decoder = new TextDecoder('utf-8', {fatal: true});
  let size = 0;
  let value = '';
  try {
    while (true) {
      const chunk = await reader.read();
      if (chunk.done) break;
      signal.throwIfAborted();
      size += chunk.value.byteLength;
      if (size > maximumResponseBytes) throw new Error('Navigation response is too large');
      value += decoder.decode(chunk.value, {stream: true});
    }
    return value + decoder.decode();
  } finally {
    await reader.cancel();
    reader.releaseLock();
  }
}

function destination(value: string): URL {
  if (typeof value !== 'string' || !value || value.length > 8192) throw new Error('Invalid navigation destination');
  const url = new URL(value, window.location.href);
  if (url.origin !== window.location.origin || url.username || url.password) throw new Error('Cross-origin navigation is unavailable');
  return url;
}

function responseWorkspace(markup: string): HTMLElement {
  const parsed = new DOMParser().parseFromString(markup, 'text/html');
  const matches = parsed.querySelectorAll<HTMLElement>('#workspace');
  if (matches.length !== 1) throw new Error('Navigation response has no unique workspace');
  return document.importNode(matches[0], true);
}

async function navigate(value: string, options: InternalNavigationOptions = {}): Promise<void> {
  const targetURL = destination(value);
  const method = options.method || 'GET';
  if ((method === 'GET' && targetURL.pathname !== '/') || (method === 'POST' && targetURL.pathname !== '/projects/add')) throw new Error('Unsupported navigation destination');
  const target = document.getElementById('workspace');
  if (!target) throw new Error('Workspace is unavailable');
  // Capture the committed entry before start listeners can close overlays or
  // move focus. A superseding request keeps the first request's same-DOM
  // capture rather than attributing pending-navigation scroll to another URL.
  if (!active) rememberScroll(committedEntryID);
  active?.abort();
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(new DOMException('Navigation timed out', 'TimeoutError')), navigationTimeoutMilliseconds);
  active = controller;
  const detail: NavigationDetail = {
    target,
    source: options.source || null,
    requestConfig: {path: targetURL.pathname, method},
  };
  dispatch('snow:navigation-start', detail);
  let topLevelNavigation = false;
  try {
    const response = await fetch(targetURL, {
      method,
      body: method === 'POST' ? options.body || null : null,
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'follow',
      signal: controller.signal,
      headers: {Accept: 'text/html', 'X-Snow-Navigation': 'workspace'},
    });
    controller.signal.throwIfAborted();
    if (response.status === 401) {
      topLevelNavigation = true;
      window.location.assign('/login');
      throw new DOMException('Browser pairing is required', 'AbortError');
    }
    if (!response.ok || !/^text\/html(?:;|$)/i.test(response.headers.get('Content-Type') || '')) throw new Error('Navigation request failed');
    const finalURL = destination(response.url);
    if (finalURL.pathname !== '/') throw new Error('Unexpected navigation response');
    const replacement = responseWorkspace(await boundedHTML(response, controller.signal));
    controller.signal.throwIfAborted();
    if (active !== controller || !target.isConnected || target !== document.getElementById('workspace')) throw new DOMException('Navigation was superseded', 'AbortError');
    // The URL may already name a popstate destination while the committed DOM
    // still belongs to another entry. Keep scroll ownership out of history.state
    // and attach it only to the DOM entry that is actually being retired.
    lifecycle.beforeReplace(target);
    dispatch('snow:navigation-before-swap', detail);
    target.replaceWith(replacement);
    const historyMode = options.history || 'none';
    const historyURL = method === 'GET' ? targetURL : finalURL;
    let nextEntryID = options.entryID || entryID(window.history.state);
    if (historyMode === 'push') {
      nextEntryID = newEntryID();
      window.history.pushState({snowNavigation: true, snowNavigationEntry: nextEntryID}, '', historyURL);
    } else if (historyMode === 'replace') {
      nextEntryID = newEntryID();
      window.history.replaceState({...historyState(window.history.state), snowNavigation: true,
        snowNavigationEntry: nextEntryID}, '', historyURL);
    } else if (!nextEntryID) {
      nextEntryID = newEntryID();
      window.history.replaceState({...historyState(window.history.state), snowNavigation: true,
        snowNavigationEntry: nextEntryID}, '', window.location.href);
    }
    committedEntryID = nextEntryID;
    lifecycle.afterReplace(replacement);
    dispatch('snow:navigation-after-swap', {...detail, target: replacement});
    // Traversal restores after this request's terminal lifecycle event. Chrome
    // may otherwise apply its own popstate scroll after this synchronous commit.
    if (options.restoreScroll === undefined) {
      if (historyURL.hash) document.getElementById(historyURL.hash.slice(1))?.scrollIntoView({block: 'start'});
      else window.scrollTo({left: 0, top: 0, behavior: 'auto'});
      rememberScroll(committedEntryID);
    }
  } catch (error) {
    const aborted = controller.signal.aborted || error instanceof DOMException && error.name === 'AbortError';
    if (active === controller) {
      dispatch('snow:navigation-error', {...detail, aborted});
      // A popstate changes the address before its replacement commits. If a
      // superseding request then terminates, the old DOM cannot safely remain
      // under the destination entry; repair with an ordinary document load.
      if (!topLevelNavigation && entryID(window.history.state) !== committedEntryID) window.location.reload();
    }
    throw error;
  } finally {
    window.clearTimeout(timeout);
    // A superseded request emits no terminal lifecycle event: the newer owner
    // keeps polling islands fenced until its own request finishes.
    if (active === controller) {
      active = null;
      dispatch('snow:navigation-end', {...detail, aborted: controller.signal.aborted});
    }
  }
}

export const workspaceNavigation = Object.freeze({
  configure(next: NavigationLifecycle) { lifecycle = next; },
  visit(value: string, options: Omit<NavigationOptions, 'method' | 'body'> = {}) {
    return navigate(value, {...options, method: 'GET'});
  },
  submit(value: string, body: BodyInit, options: Omit<NavigationOptions, 'method' | 'body'> = {}) {
    return navigate(value, {...options, method: 'POST', body});
  },
  abort() { active?.abort(); },
});

window.addEventListener('popstate', event => {
  if (!document.getElementById('workspace')) return;
  let destinationEntryID = entryID(event.state);
  if (!destinationEntryID) {
    destinationEntryID = newEntryID();
    window.history.replaceState({...historyState(event.state), snowNavigation: true,
      snowNavigationEntry: destinationEntryID}, '', window.location.href);
  }
  const restoreScroll = scrollPositions.get(destinationEntryID) || null;
  const historyURL = new URL(window.location.href);
  void navigate(historyURL.href, {method: 'GET', history: 'none', entryID: destinationEntryID, restoreScroll}).then(() => {
    if (active || committedEntryID !== destinationEntryID) return;
    if (restoreScroll) window.scrollTo({left: restoreScroll.x, top: restoreScroll.y, behavior: 'auto'});
    else if (historyURL.hash) document.getElementById(historyURL.hash.slice(1))?.scrollIntoView({block: 'start'});
    else window.scrollTo({left: 0, top: 0, behavior: 'auto'});
    rememberScroll(destinationEntryID);
  }).catch(error => {
    if (!(error instanceof DOMException && error.name === 'AbortError')) window.location.reload();
  });
});
