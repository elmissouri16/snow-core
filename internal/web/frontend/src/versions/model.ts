// Public DTO validation only; network and mutation admission remain in app.js.
export interface Identity { project_id: string; session_id: string; instance_id: string }
export interface Version { branch_id: string; tip_id: string; name: string; current: boolean }
export interface Page extends Identity { revision: number; current_branch_id: string; current_tip_id: string; versions: Version[]; has_more: boolean; next_cursor: string }
export interface PreviewMessage { role: string; text: string; tools?: {tool: string; status?: unknown}[] }
export interface Preview extends Identity { revision: number; branch_id: string; tip_id: string; messages: PreviewMessage[]; has_more: boolean; next_cursor: string; history_truncated?: unknown; history_tools_truncated?: unknown }
export interface Selection extends Identity { revision: number; current_branch_id: string; current_tip_id: string; branch_id: string; tip_id: string; name: string }
export interface Preparation extends Identity { branch_id: string; tip_id: string; current_branch_id: string; current_tip_id: string; restore_token: string; expires_at: string }
export type HistoryAction = 'history-branch-fork' | 'history-session-fork' | 'history-branch-rename';
export const actions = new Set<HistoryAction>(['history-branch-fork', 'history-session-fork', 'history-branch-rename']);
export const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object';
export const id = (value: unknown, empty = false): value is string => typeof value === 'string' && (empty || value.length > 0) && value.length <= 256 && !/[\u0000-\u001f\u007f]/.test(value);
export const historyID = (value: unknown): value is string => id(value) && !/[\u0080-\u009f]/.test(value);
export const cursor = (value: unknown): value is string => typeof value === 'string' && value.length <= 4096 && !/[\u0000-\u001f\u007f]/.test(value);
export const validRevision = (value: unknown): value is number => Number.isSafeInteger(value) && (value as number) >= 0;
export function bound(value: unknown, identity: Identity): value is Record<string, unknown> & Identity {
  return record(value) && value.project_id === identity.project_id && value.session_id === identity.session_id && value.instance_id === identity.instance_id;
}
export function validPage(value: unknown, identity: Identity): value is Page {
  if (!bound(value, identity) || !validRevision(value.revision) || !id(value.current_branch_id) || !id(value.current_tip_id, true) || !Array.isArray(value.versions) || value.versions.length > 100 || typeof value.has_more !== 'boolean' || !cursor(value.next_cursor) || value.has_more !== !!value.next_cursor) return false;
  const seen = new Set();
  return value.versions.every((item: unknown) => {
    if (!record(item) || !id(item.branch_id) || !id(item.tip_id, true) || typeof item.name !== 'string' || item.name.length > 512 || typeof item.current !== 'boolean') return false;
    if (item.current !== (item.branch_id === value.current_branch_id) || item.current && item.tip_id !== value.current_tip_id || seen.has(item.branch_id)) return false;
    seen.add(item.branch_id); return true;
  });
}
export function validPreview(value: unknown, target: Pick<Version, 'branch_id' | 'tip_id'>, identity: Identity): value is Preview {
  if (!bound(value, identity) || !validRevision(value.revision) || value.branch_id !== target.branch_id || value.tip_id !== target.tip_id || !Array.isArray(value.messages) || value.messages.length > 256 || typeof value.has_more !== 'boolean' || !cursor(value.next_cursor) || value.has_more !== !!value.next_cursor) return false;
  let bytes = 0;
  return value.messages.every((message: unknown) => {
    if (!record(message) || typeof message.role !== 'string' || !['user', 'assistant', 'tool', 'system', 'tool_activity'].includes(message.role) || typeof message.text !== 'string') return false;
    bytes += new TextEncoder().encode(message.text).length;
    return bytes <= 1024 * 1024 && (!message.tools || Array.isArray(message.tools) && message.tools.length <= 128 && message.tools.every((tool: unknown) => record(tool) && typeof tool.tool === 'string' && tool.tool.length <= 256));
  });
}
export function validPreparation(value: unknown, target: Version, origin: Page, identity: Identity, now = Date.now()): value is Preparation {
  return bound(value, identity) && value.branch_id === target.branch_id && value.tip_id === target.tip_id && value.current_branch_id === origin.current_branch_id && value.current_tip_id === origin.current_tip_id && id(value.restore_token) && typeof value.expires_at === 'string' && Number.isFinite(Date.parse(value.expires_at)) && Date.parse(value.expires_at) > now;
}
export function validName(value: unknown, action: string): value is string {
  return typeof value === 'string' && !!value && value === value.trim() && new TextEncoder().encode(value).length <= 256 && [...value].length <= (action === 'history-session-fork' ? 72 : 64) && !/[\u0000-\u001f\u007f-\u009f]/.test(value);
}
export function sameSelection(a: Selection | null | undefined, b: Selection | null | undefined): boolean {
  return !!a && !!b && (['project_id', 'instance_id', 'session_id', 'revision', 'current_branch_id', 'current_tip_id', 'branch_id', 'tip_id', 'name'] as const).every(key => a[key] === b[key]);
}
export function sameMetadata(result: unknown, target: Selection, name: string): result is Record<string, unknown> {
  return bound(result, target) && result.branch_id === target.branch_id && result.tip_id === target.tip_id && result.name === name && validRevision(result.revision) && result.revision >= target.revision;
}

// Presentation retained during refresh is deliberately not part of this input.
export function verifiedSelection(v: {
  operation?: unknown; phase?: string | null; stale?: boolean;
  dialog: {open: boolean} | null; selected: Version | null; preview: Preview | null;
  page: Page | null; snapshot?: {revision?: number} | null;
} | null): Readonly<Selection> | null {
  if (!v || v.operation || v.phase || v.stale || !v.dialog?.open || !v.selected || !v.preview || !v.page || v.preview.revision !== v.page.revision || v.preview.revision !== v.snapshot?.revision || v.preview.branch_id !== v.selected.branch_id || v.preview.tip_id !== v.selected.tip_id) return null;
  return Object.freeze({project_id: v.page.project_id, instance_id: v.page.instance_id, session_id: v.page.session_id, revision: v.preview.revision, current_branch_id: v.page.current_branch_id, current_tip_id: v.page.current_tip_id, branch_id: v.selected.branch_id, tip_id: v.selected.tip_id, name: v.selected.name});
}

export interface HistoryInventory { text: string; rows: {name: string; url: string}[] }
// The app owns inventory reads and scope validation. This is only the public
// presentation shape and exact same-project navigation destination allowlist.
export function validHistoryInventory(value: unknown, projectID: string): value is HistoryInventory {
  if (!record(value) || typeof value.text !== 'string' || value.text.length > 4096 || !Array.isArray(value.rows) || value.rows.length > 1000) return false;
  const prefix = `/?view=projects&project=${encodeURIComponent(projectID)}&session=`;
  return value.rows.every((row: unknown) => {
    if (!record(row) || typeof row.name !== 'string' || row.name.length > 512 || typeof row.url !== 'string' || !row.url.startsWith(prefix)) return false;
    try {
      const session = decodeURIComponent(row.url.slice(prefix.length));
      return !!session && session.length <= 256 && row.url === prefix + encodeURIComponent(session);
    } catch { return false; }
  });
}
