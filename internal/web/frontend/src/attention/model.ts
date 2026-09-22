/** Public bounded attention DTOs only; no RPC/private continuity or transport. */
export interface Option {label: string; description?: string}
export interface Question {id: string; header?: string; question: string; options?: Option[]; choices_only?: boolean}
export interface InputRequest {id: string; questions: Question[]}
export interface Effect {type?: string; capability?: string; operation?: string; resource?: string; command?: string; reason?: string}
export interface PermissionRequest {id?: string; agent_path?: string; agent_role?: string; tool?: string; risk?: string; reason?: string; scope_label?: string; paths?: string[]; capabilities?: string[]; effects?: Effect[]; unknown?: boolean; truncated?: boolean}
export interface Snapshot {project_id?: string; session_id?: string; instance_id?: string; cancel_token?: string; permission?: unknown; input?: unknown}
export interface State {safe?: boolean; busy?: boolean; canStop?: boolean; stopping?: boolean}
export interface Answer {selected: number | 'custom' | null; custom: string}
export interface Draft {key: string; identity: (string | undefined)[]; page: number; collapsed: boolean; answers: Record<string, Answer>; updated: number}
const encoder = new TextEncoder();
export function record(value: unknown): value is Record<string, unknown> {return !!value && typeof value === 'object' && !Array.isArray(value);}
export function text(value: unknown, max: number): value is string {
  return typeof value === 'string' && !value.includes('\0') && !/[\uD800-\uDFFF]/u.test(value) && encoder.encode(value).length <= max;
}
export function identifier(value: unknown): value is string {return text(value, 128) && /^[a-zA-Z0-9_-]+$/.test(value);}
export function validInput(value: unknown): value is InputRequest {
  if (!record(value) || !identifier(value.id) || !Array.isArray(value.questions) || !value.questions.length || value.questions.length > 16) return false;
  const seen = new Set<string>(); let total = 0;
  for (const q of value.questions) {
    if (!record(q) || !identifier(q.id) || seen.has(q.id) || !text(q.question, 8192) || (q.header != null && !text(q.header, 256)) ||
      (q.choices_only != null && typeof q.choices_only !== 'boolean') || (q.options != null && !Array.isArray(q.options))) return false;
    seen.add(q.id);
    const options = (q.options || []) as unknown[];
    if (options.length > 32 || (q.choices_only && !options.length)) return false;
    total += encoder.encode(q.question).length + encoder.encode((q.header || '') as string).length;
    for (const option of options) {
      if (!record(option) || !text(option.label, 512) || !option.label.trim() || (option.description != null && !text(option.description, 2048))) return false;
      total += encoder.encode(option.label).length + encoder.encode((option.description || '') as string).length;
    }
  }
  return total <= 64 * 1024;
}
export function answer(question: Question, draft?: Answer): string {
  if (typeof draft?.selected === 'number') return question.options?.[draft.selected]?.label || '';
  return question.choices_only ? '' : draft?.custom || '';
}
export function invalidAnswer(questions: Question[], draft: Draft, all: boolean): number {
  let total = 0;
  for (let i = 0; i < questions.length; i++) {
    if (!all && i !== draft.page) continue;
    const value = answer(questions[i], draft.answers[questions[i].id]);
    total += encoder.encode(value).length;
    if (!text(value, 64 * 1024) || !value.trim() || total > 64 * 1024) return i;
  }
  return -1;
}
export function permissionBlocked(value: unknown): boolean {
  if (!record(value) || !identifier(value.id) || value.truncated) return true;
  for (const [name, max] of [['agent_path', 512], ['agent_role', 64], ['tool', 8192], ['risk', 8192], ['reason', 8192], ['scope_label', 8192]] as const)
    if (value[name] != null && !text(value[name], max)) return true;
  for (const name of ['paths', 'capabilities']) {
    const list = value[name];
    if (list != null && (!Array.isArray(list) || list.length > 64 || !list.every(item => text(item, 8192)))) return true;
  }
  const effects = value.effects;
  return effects != null && (!Array.isArray(effects) || effects.length > 64 || !effects.every(effect => record(effect) &&
    ['type', 'capability', 'operation', 'resource', 'command', 'reason'].every(name => effect[name] == null || text(effect[name], 8192))));
}
/** Explicit canonical fields avoid remounts from JSON object-property ordering. */
export function requestKey(kind: 'input' | 'permission', pending: unknown, turn = ''): string {
  if (kind === 'input' && validInput(pending)) return JSON.stringify([turn, kind, pending.id,
    pending.questions.map(q => [q.id, q.header || '', q.question, !!q.choices_only, (q.options || []).map(o => [o.label, o.description || ''])])]);
  if (kind === 'permission' && record(pending)) return JSON.stringify([turn, kind, pending.id, pending.agent_path, pending.agent_role, pending.tool, pending.risk, pending.reason,
    pending.scope_label, pending.paths, pending.capabilities, pending.unknown, pending.truncated,
    Array.isArray(pending.effects) ? pending.effects.map(e => record(e) ? [e.type, e.capability, e.operation, e.resource, e.command, e.reason] : null) : null]);
  return JSON.stringify([turn, kind, record(pending) ? pending.id : null, 'invalid']);
}
