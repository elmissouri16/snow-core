export interface ShellProject {
  id: string; name: string; path: string; available: boolean;
  trustRemembered: boolean; skillsEnabled: boolean; pinned: boolean;
}
export interface SessionRow {session_id: string; name: string}
export interface LiveSelection {
  project: string; session: string; instance: string; title: string;
  renameAvailable: boolean; renameDisabled: boolean; newDisabled: boolean;
}
export interface ShellBootstrap {
  csrf: string; version: string; view: string; project: string; session: string;
  hostSettingsEnabled: boolean; apiKeyEnabled: boolean; tls: boolean; pairingCode: string;
  projects: ShellProject[]; sessions: SessionRow[]; live: LiveSelection | null;
}
export type ShellCommand = {type: 'theme'; theme: 'light' | 'dark'} | {type: 'collapse'; collapsed: boolean} | {type: 'navigation'; open: boolean};
export type Navigate = (href: string, source?: HTMLElement) => void | Promise<unknown>;
export const LIMITS = {rows: 100, projects: 100, pages: 25, reads: 100, response: 256 * 1024};
const uuid = /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/;
export function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid shell data');
  return value as Record<string, unknown>;
}
export function text(value: unknown, max: number): string {
  if (typeof value !== 'string' || value.length > max) throw new Error('Invalid shell text');
  return value;
}
function flag(value: unknown): boolean {
  if (typeof value !== 'boolean') throw new Error('Invalid shell flag');
  return value;
}
export function sessionRows(value: unknown): SessionRow[] {
  if (!Array.isArray(value) || value.length > LIMITS.rows) throw new Error('Invalid session rows');
  const seen = new Set<string>();
  return value.map(item => {
    const row = record(item), id = text(row.session_id, 256);
    if (!id || seen.has(id)) throw new Error('Invalid session identity');
    seen.add(id);
    return {session_id: id, name: text(row.name, 4096)};
  });
}
export function validateLive(value: unknown): LiveSelection | null {
  if (value === null) return null;
  const live = record(value), project = text(live.project, 36), session = text(live.session, 256), instance = text(live.instance, 256);
  if (!uuid.test(project) || !session || !instance) throw new Error('Invalid live identity');
  return {project, session, instance, title: text(live.title, 4096), renameAvailable: flag(live.renameAvailable), renameDisabled: flag(live.renameDisabled), newDisabled: flag(live.newDisabled)};
}
export function validateShellBootstrap(value: unknown): ShellBootstrap {
  const data = record(value);
  if (!Array.isArray(data.projects) || data.projects.length > LIMITS.projects) throw new Error('Invalid shell projects');
  const ids = new Set<string>();
  const projects = data.projects.map(value => {
    const row = record(value), id = text(row.id, 36), name = text(row.name, 128);
    if (!uuid.test(id) || ids.has(id) || new TextEncoder().encode(name).length > 128) throw new Error('Invalid project identity');
    ids.add(id);
    return {id, name, path: text(row.path, 4096), available: flag(row.available), trustRemembered: flag(row.trustRemembered), skillsEnabled: flag(row.skillsEnabled), pinned: flag(row.pinned)};
  });
  const project = text(data.project, 36), live = validateLive(data.live);
  if (project && !ids.has(project) || live && (live.project !== project || !ids.has(live.project))) throw new Error('Invalid selected project');
  return {csrf: text(data.csrf, 512), version: text(data.version, 128), view: text(data.view, 32), project,
    session: text(data.session, 256), projects, sessions: sessionRows(data.sessions), live,
    hostSettingsEnabled: flag(data.hostSettingsEnabled), apiKeyEnabled: flag(data.apiKeyEnabled), tls: flag(data.tls), pairingCode: text(data.pairingCode, 128)};
}
export interface Inventory {
  project: string; instance: string; rows: SessionRow[]; available: boolean;
  deleteSupported: boolean; activeSession: string; nextOffset: number; hasMore: boolean; truncated: boolean;
}
export function validateInventory(value: unknown, project: string, offset: number, instance: string, live: LiveSelection | null): Inventory {
  const data = record(value), owner = text(data.instance_id, 256);
  if (data.project_id !== project || offset > 0 && owner !== instance || live?.project === project && owner !== live.instance) throw new Error('Stale inventory');
  const nextOffset = Number.isSafeInteger(data.next_offset) && (data.next_offset as number) > offset ? data.next_offset as number : 0;
  return {project, instance: owner, rows: sessionRows(data.sessions), available: data.available === true,
    deleteSupported: data.delete_supported === true && typeof data.active_session_id === 'string',
    activeSession: typeof data.active_session_id === 'string' ? text(data.active_session_id, 256) : '',
    nextOffset, hasMore: data.has_more === true && nextOffset > 0, truncated: data.truncated === true};
}
export async function boundedText(response: Response, maximum: number): Promise<string> {
  if (!response.body) throw new Error('Missing response');
  const reader = response.body.getReader(), decoder = new TextDecoder();
  let size = 0, value = '';
  try {
    while (true) {
      const chunk = await reader.read();
      if (chunk.done) break;
      size += chunk.value.length;
      if (size > maximum) throw new Error('Response too large');
      value += decoder.decode(chunk.value, {stream: true});
    }
    return value + decoder.decode();
  } finally { await reader.cancel(); reader.releaseLock(); }
}
export const projectURL = (project: string, extra: Record<string, string> = {}) => '/?' + new URLSearchParams({view: 'projects', project, ...extra});
export interface DeleteAuthority {supported: boolean; available: boolean; active: boolean; instance: string}
export function canDelete(authority: DeleteAuthority | undefined): boolean {
  return !!authority && authority.supported && authority.available && !authority.active;
}
