import { imageTypes } from './model.ts';
export type ImageScope = {project: string; instance: string; session: string; live: boolean};
export function scopeOf(root: HTMLElement | null): ImageScope {
  return {project: root?.dataset.project || '', instance: root?.dataset.instance || '', session: root?.dataset.session || '', live: root?.id === 'live-session'};
}
export function scopeKey(scope: ImageScope) { return JSON.stringify([scope.project, scope.instance, scope.session, scope.live]); }
export function imageURL(raw: unknown, scope: ImageScope, messageID: string, index: number, mime: string, origin: string): string {
  if (!imageTypes.has(mime) || !Number.isSafeInteger(index) || index < 0 || index > 10000 || typeof raw !== 'string' || !raw || raw.length > 4096 || !messageID) return '';
  const {project, instance, session, live} = scope;
  if (!project || !session) return '';
  try {
    const url = new URL(raw, origin);
    if (url.origin !== origin || url.username || url.password || url.hash) return '';
    const path = `/projects/${encodeURIComponent(project)}`;
    if (live) {
      if (!instance || url.pathname !== `${path}/runtime/images/${encodeURIComponent(messageID)}/${index}` || url.searchParams.getAll('instance_id').length !== 1 || url.searchParams.get('instance_id') !== instance || url.searchParams.getAll('session_id').length !== 1 || url.searchParams.get('session_id') !== session || url.searchParams.getAll('turn_id').length > 1 || [...url.searchParams.keys()].some(key => !['instance_id', 'session_id', 'turn_id'].includes(key))) return '';
    } else if (url.pathname !== `${path}/sessions/${encodeURIComponent(session)}/images/${encodeURIComponent(messageID)}/${index}` || url.search) return '';
    if (raw !== url.pathname + url.search && raw !== url.href) return '';
    return url.pathname + url.search;
  } catch { return ''; }
}
const maxBytes = 2 * 1024 * 1024, jobs = new Set<Job>(), waiting = new Set<Job>();
let active: Job | undefined, scheduled = false;
type Job = {current: () => boolean; start: () => void; release: () => void};
function pump() {
  if (scheduled) return;
  scheduled = true;
  queueMicrotask(() => {
    scheduled = false;
    if (active && !active.current()) active.release();
    if (active) return;
    for (const job of waiting) {
      waiting.delete(job);
      if (!job.current()) { job.release(); continue; }
      active = job; job.start(); break;
    }
  });
}
export type ImageRead = {release: () => void; decoded: (success: boolean) => void};
export function loadImage(url: string, mime: string, current: () => boolean, update: (state: 'loading' | 'loaded' | 'unavailable', src: string) => void): ImageRead {
  let retired = false, finished = false, objectURL = '';
  const controller = new AbortController();
  let timer: ReturnType<typeof setTimeout> | undefined;
  const valid = () => !retired && current();
  const done = () => { finished = true; clearTimeout(timer); waiting.delete(job); if (active === job) active = undefined; pump(); };
  const decoded = (success: boolean) => {
    if (finished || retired) return;
    if (valid()) update(success ? 'loaded' : 'unavailable', success ? objectURL : '');
    if (!success && objectURL) { URL.revokeObjectURL(objectURL); objectURL = ''; }
    done();
  };
  const job: Job = {current: valid, release: () => {
    if (retired) return;
    retired = true; controller.abort(); clearTimeout(timer);
    if (objectURL) URL.revokeObjectURL(objectURL);
    objectURL = ''; jobs.delete(job); done();
  }, start: () => {
    update('loading', '');
    timer = setTimeout(() => { controller.abort(); decoded(false); }, 8000);
    void (async () => {
      try {
        const response = await fetch(url, {credentials: 'same-origin', cache: 'no-store', redirect: 'error', signal: controller.signal, headers: {Accept: mime}});
        if (!response.ok || response.headers.get('content-type')?.split(';')[0].trim() !== mime || Number(response.headers.get('content-length')) > maxBytes || !response.body) throw new Error('Unavailable image');
        const reader = response.body.getReader(), chunks: Uint8Array<ArrayBuffer>[] = [];
        let bytes = 0;
        try {
          for (;;) {
            const {value, done} = await reader.read();
            if (done) break;
            bytes += value.byteLength;
            if (bytes > maxBytes || !valid()) throw new Error('Unavailable image');
            chunks.push(new Uint8Array(value));
          }
        } finally { await reader.cancel().catch(() => {}); reader.releaseLock(); }
        if (!bytes || !valid() || finished) { decoded(false); return; }
        objectURL = URL.createObjectURL(new Blob(chunks, {type: mime}));
        update('loading', objectURL);
      } catch { controller.abort(); decoded(false); }
    })();
  }};
  if (jobs.size >= 800) { queueMicrotask(() => { if (valid()) update('unavailable', ''); }); return {release: job.release, decoded}; }
  jobs.add(job); waiting.add(job); pump();
  return {release: job.release, decoded};
}
export function disposeImages() { for (const job of jobs) job.release(); }
