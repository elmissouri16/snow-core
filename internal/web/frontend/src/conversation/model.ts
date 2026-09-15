/** Public snapshot subset only; no provider-private continuity or transport logic. */
export type Telemetry = {
  available?: boolean; context_available?: boolean; estimated?: boolean;
  context_tokens?: number; context_window?: number; input_tokens?: number;
  output_tokens?: number; total_tokens?: number;
  cost?: {known?: boolean; currency?: string; total?: number};
};
export type Snapshot = {
  project_id: string; instance_id: string; session_id: string; session_name?: string;
  status: string; provider?: string; model?: string; mode?: string; permission_mode?: string;
  permission?: unknown; input?: unknown; recovery?: {state?: string}; telemetry?: Telemetry;
  cancel_requested?: boolean; goal?: {running?: boolean};
};
export type Controls = {
  safe?: boolean; verified?: boolean; connected?: boolean; busy?: boolean;
  setting?: boolean; invalid?: boolean; status?: string;
  closeDisabled?: boolean; inspectorExpanded?: boolean;
};
export type ModelChoice = {provider: string; id: string; name: string; context_window?: number};
export type SessionChoice = {session_id: string; name?: string; active?: boolean; updated_at?: number};
export type Choices = {
  instance_id: string; project_id?: string; models: ModelChoice[]; sessions: SessionChoice[];
  sessions_available?: boolean; sessions_truncated?: boolean;
  models_partial?: boolean; models_truncated?: boolean;
};
export type Hooks = {
  action(kind: string, fields: Record<string, string>): Promise<unknown>;
  choices(): Promise<unknown>;
  sessions?(target: string): Promise<unknown>;
  openDialog(dialog: HTMLDialogElement, trigger: HTMLElement): void;
  closeDialog(dialog: HTMLDialogElement): void;
};
export const policies = {ask: 'Ask', deny: 'Deny', allow: 'Allow'} as const;
export type Policy = keyof typeof policies;
export const isPolicy = (value: unknown): value is Policy => typeof value === 'string' && Object.hasOwn(policies, value);
export const activeTurn = (status?: string) => ['running', 'permission', 'input'].includes(status || '');
export const validName = (name: string) => !!name.trim() && new TextEncoder().encode(name.trim()).length <= 256;
export const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const stringID = (value: unknown): value is string => typeof value === 'string' && !!value;
export function sessionChoices(value: unknown): SessionChoice[] | null {
  if (!Array.isArray(value)) return null;
  return value.filter(record).filter(item => stringID(item.session_id)).map(item => ({
    session_id: String(item.session_id), name: typeof item.name === 'string' ? item.name : '',
  }));
}
export function choices(value: unknown, instance: string, project: string): Choices | null {
  if (!record(value) || value.instance_id !== instance || (value.project_id !== undefined && value.project_id !== project) || !Array.isArray(value.models)) return null;
  const sessions = sessionChoices(value.sessions); if (!sessions) return null;
  return {
    instance_id: instance, project_id: project, sessions,
    models: value.models.filter(record).filter(item => stringID(item.provider) && stringID(item.id)).map(item => ({
      provider: String(item.provider), id: String(item.id), name: typeof item.name === 'string' ? item.name : '',
      context_window: typeof item.context_window === 'number' ? item.context_window : undefined,
    })),
    sessions_available: value.sessions_available !== false, sessions_truncated: value.sessions_truncated === true,
    models_partial: value.models_partial === true, models_truncated: value.models_truncated === true,
  };
}
export const number = (value?: number) => typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value.toLocaleString() : 'Unknown';
/** Worker-recorded subtotal only. Unknown never means zero or a billing charge. */
export function presentation(telemetry?: Telemetry) {
  const cost = telemetry?.cost;
  if (!telemetry?.available || cost?.known !== true || !/^[A-Z]{3}$/.test(cost.currency || '') || typeof cost.total !== 'number' || !Number.isFinite(cost.total) || cost.total < 0) return {known: false, value: 'Unknown · cost not available'};
  const total = cost.total;
  const amount = total === 0 ? '0.00' : total < 0.000001 || total >= 1e9 ? total.toExponential(3) : total.toLocaleString(undefined, {minimumFractionDigits: 2, maximumFractionDigits: 6});
  return {known: true, value: `${cost.currency} ${amount}`};
}
export function telemetryView(snapshot: Snapshot) {
  const t = snapshot.telemetry, cost = presentation(t);
  return {
    contextLabel: t?.context_available && t.estimated === false ? 'Last reported input' : 'Estimated context',
    context: t?.context_available ? `${number(t.context_tokens)} tokens / ${(t.context_window || 0) > 0 ? number(t.context_window) : 'unknown window'}` : 'Unknown',
    usage: t?.available ? `${number(t.input_tokens)} in · ${number(t.output_tokens)} out · ${number(t.total_tokens)} total` : 'Unknown',
    cost: cost.known ? cost.value : 'Unknown', knownCost: cost.known,
  };
}
export function contextMeter(t?: Telemetry) {
  const known = !!t?.context_available && typeof t.context_tokens === 'number' && Number.isFinite(t.context_tokens) && t.context_tokens >= 0 && typeof t.context_window === 'number' && Number.isFinite(t.context_window) && t.context_window > 0;
  return {known, percent: known ? Math.max(0, Math.min(100, t!.context_tokens! / t!.context_window! * 100)) : 0};
}
export function statusLabel(snapshot: Snapshot) {
  if (snapshot.goal?.running) return 'Goal running';
  if (snapshot.status === 'idle' && snapshot.cancel_requested) return 'Stopping';
  const labels: Record<string, string> = {idle: 'Ready', running: 'Working', permission: 'Approval needed', input: 'Input needed', opening: 'Opening', closing: 'Closing', failed: 'Worker failed'};
  return labels[snapshot.status] || 'Unavailable';
}
