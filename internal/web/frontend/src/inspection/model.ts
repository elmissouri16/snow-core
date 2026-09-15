export type Tab = 'files' | 'changes' | 'project';
export type Entry = {name: string; path: string; kind: 'file' | 'directory'};
export type Change = {path: string; kind: 'staged' | 'unstaged' | 'untracked'; status: string};
export type Files = {path: string; entries: Entry[]; next_offset: number; has_more: boolean; limited: boolean};
export type File = {path: string; text: string; size: number; truncated: boolean};
export type Changes = {available: boolean; reason: string; changes: Change[]; limited: boolean};
export type Diff = {available: boolean; reason: string; path: string; kind: string; text: string; truncated: boolean};
export type Bootstrap = {project: {id: string; name: string; path: string; available: boolean}; csrf: string; live?: {session_id: string; provider: string; model: string}};
export type InspectionIdentity = {project_id: string; instance_id: string; session_id: string};
export type InspectionRefresh = InspectionIdentity & {provider: string; model: string};
export const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const str = (v: unknown, max = 4096): v is string => typeof v === 'string' && v.length <= max;
const integer = (v: unknown) => Number.isSafeInteger(v) && (v as number) >= 0;
export function bootstrap(v: unknown): Bootstrap | null {
  if (!object(v) || !object(v.project) || !str(v.project.id, 128) || !v.project.id || !str(v.project.name, 128) || !str(v.project.path) || typeof v.project.available !== 'boolean' || !str(v.csrf, 512)) return null;
  if (v.live !== undefined && (!object(v.live) || !str(v.live.session_id, 512) || !str(v.live.provider, 256) || !str(v.live.model, 512))) return null;
  return v as Bootstrap;
}
// Copy only bounded public presentation fields. Identity is supplied by the
// active controller owner, never adopted from a late projection.
export function liveProjection(value: unknown, registeredProject: string, owner: InspectionIdentity): Bootstrap['live'] | null {
  if (!object(value) || !str(value.project_id, 128) || !value.project_id || !str(value.instance_id, 256) || !value.instance_id || !str(value.session_id, 512) || !value.session_id || !str(value.provider, 256) || !str(value.model, 512)) return null;
  if (value.project_id !== registeredProject || value.project_id !== owner.project_id || value.instance_id !== owner.instance_id || value.session_id !== owner.session_id) return null;
  return {session_id: value.session_id, provider: value.provider, model: value.model};
}
export function validFiles(v: unknown): v is Files {
  return object(v) && str(v.path) && Array.isArray(v.entries) && v.entries.length <= 256 && v.entries.every(e => object(e) && str(e.name) && str(e.path) && (e.kind === 'file' || e.kind === 'directory')) && integer(v.next_offset) && typeof v.has_more === 'boolean' && typeof v.limited === 'boolean';
}
export function validFile(v: unknown): v is File {
  return object(v) && str(v.path) && str(v.text, 65536) && integer(v.size) && typeof v.truncated === 'boolean';
}
export function validChanges(v: unknown): v is Changes {
  return object(v) && typeof v.available === 'boolean' && str(v.reason, 2048) && typeof v.limited === 'boolean' && Array.isArray(v.changes) && v.changes.length <= 256 && v.changes.every(c => object(c) && str(c.path) && ['staged', 'unstaged', 'untracked'].includes(String(c.kind)) && str(c.status, 32));
}
export function validDiff(v: unknown): v is Diff {
  return object(v) && typeof v.available === 'boolean' && str(v.reason, 2048) && str(v.path) && str(v.kind, 32) && str(v.text, 65536) && typeof v.truncated === 'boolean';
}
const order = new Intl.Collator(undefined, {numeric: true, sensitivity: 'base'});
export function mergeFiles(previous: Entry[], incoming: Entry[], append: boolean): Entry[] {
  const entries = new Map<string, Entry>();
  for (const entry of [...(append ? previous : []), ...incoming]) { if (entries.size >= 4096) break; entries.set(entry.path, entry); }
  return [...entries.values()].sort((a, b) => Number(b.kind === 'directory') - Number(a.kind === 'directory') || order.compare(a.path, b.path));
}
export function diffLines(text: string): {kind: string; text: string}[] {
  const content = text.slice(0, 65536), lines = []; let offset = 0;
  for (const match of content.matchAll(/[^\n]*\n|[^\n]+$/g)) {
    if (lines.length === 4096) { lines.push({kind: 'context', text: content.slice(offset)}); break; }
    const line = match[0]; offset += line.length;
    lines.push({kind: /^(diff |index |--- |\+\+\+ )/.test(line) ? 'meta' : line.startsWith('@@') ? 'hunk' : line.startsWith('+') ? 'added' : line.startsWith('-') ? 'removed' : 'context', text: line});
  }
  return lines;
}
