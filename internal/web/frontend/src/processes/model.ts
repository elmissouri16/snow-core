export type ProcessRecord = {process_id: string; name: string; status: 'running' | 'stopped' | 'exited'; ready?: boolean};
export type Scope = {project: string; instance: string; session: string};
export const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const bytes = (value: string) => new TextEncoder().encode(value).length;
export const idOK = (value: unknown): value is string => typeof value === 'string' && /^proc_[a-f0-9]{32}$/.test(value);
export const statusOK = (value: unknown): value is ProcessRecord['status'] => value === 'running' || value === 'stopped' || value === 'exited';
export function validRecord(value: unknown): value is ProcessRecord {
  return object(value) && idOK(value.process_id) && typeof value.name === 'string' && bytes(value.name) <= 64 && statusOK(value.status) && (value.ready === undefined || typeof value.ready === 'boolean');
}
export function inventory(value: unknown): {processes: ProcessRecord[]; truncated: boolean} | null {
  if (!object(value) || !Array.isArray(value.processes) || value.processes.length > 128 || typeof value.truncated !== 'boolean' || !value.processes.every(validRecord) || new Set(value.processes.map(p => p.process_id)).size !== value.processes.length) return null;
  return {processes: value.processes, truncated: value.truncated};
}
export function logPage(value: unknown, id: string, cursor = 0): {output: string; next_cursor: number; omitted_bytes: number; eof: boolean} | null {
  if (!object(value) || value.process_id !== id || !statusOK(value.status) || typeof value.output !== 'string' || bytes(value.output) > 32768 || !Number.isSafeInteger(value.next_cursor) || (value.next_cursor as number) < cursor || !Number.isSafeInteger(value.omitted_bytes) || (value.omitted_bytes as number) < 0 || typeof value.eof !== 'boolean') return null;
  return {output: value.output.replace(/\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, '').replace(/[\x00-\x08\x0b-\x1f\x7f-\x9f]/g, ''), next_cursor: value.next_cursor as number, omitted_bytes: value.omitted_bytes as number, eof: value.eof};
}
export function resultForScope(value: unknown, scope: Scope): Record<string, unknown> | null {
  return object(value) && value.project_id === scope.project && value.instance_id === scope.instance && object(value.result) && value.result.session_id === scope.session ? value.result : null;
}
