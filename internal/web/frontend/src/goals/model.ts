/** Public goal projections only; provider-private state is neither read nor rendered. */
export type Goal = Readonly<{
  session_id: string; branch_id: string; tip_id: string; goal_id: string; goal_run_id: string;
  objective: string; status: string; blocked_reason: string; deferred: boolean; running: boolean;
  tokens_used: number; token_budget?: number | null; budget_remaining?: number | null;
  estimated_costs?: readonly unknown[] | null;
}>;
export type Snapshot = Readonly<{
  goal?: unknown; revision?: unknown; provider?: unknown; model?: unknown;
  permission_mode?: unknown; mode?: unknown; thinking?: unknown;
}>;
export type Facts = Readonly<{provider: unknown; model: unknown; permission: unknown; mode: unknown; thinking: unknown}>;
export type Inspection = Readonly<{goal: Goal; facts: Facts; revision: number}>;
export type UI = Readonly<{supported: boolean; readable: boolean; safe: boolean; canStop?: boolean; showStop?: boolean; stopLabel?: string; stopTitle?: string}>;
export type Action = 'goal-start' | 'goal-resume';
export type Fields = Readonly<{session_id: string; branch_id: string; expected_tip_id: string; expected_goal_id: string; objective?: string; token_budget?: string}>;
export type API = Readonly<{
  root: HTMLElement | null;
  identity: Readonly<{project_id: string; session_id: string; instance_id?: string}>;
  openDialog: (dialog: HTMLDialogElement, trigger: HTMLElement) => void;
  closeDialog: (dialog: HTMLDialogElement) => void;
  validText: (text: string) => boolean;
  inspect: (branch: string, signal: AbortSignal) => Promise<unknown>;
  reserve: (active: boolean) => boolean;
  changed: () => void;
  focusPrompt: () => void;
  notice: (message: string) => void;
  run: (action: Action, fields: Fields, expectedRevision: number) => Promise<boolean>;
}>;
const statuses = new Set(['none', 'active', 'paused', 'blocked', 'usage_limited', 'budget_limited', 'complete']);
export const record = (value: unknown): value is Readonly<Record<string, unknown>> => value !== null && typeof value === 'object' && !Array.isArray(value);
const id = (value: unknown, empty = false): value is string => typeof value === 'string' && (empty || !!value) && value.length <= 256 && !/[\u0000-\u001f\u007f]/.test(value);
const natural = (value: unknown): value is number => typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
export function validGoal(goal: unknown, session: unknown): goal is Goal {
  return record(goal) && goal.session_id === session && id(goal.branch_id) && id(goal.tip_id, true) && id(goal.goal_id, true) && id(goal.goal_run_id, true) && typeof goal.objective === 'string' && new TextEncoder().encode(goal.objective).length <= 65536 && typeof goal.blocked_reason === 'string' && new TextEncoder().encode(goal.blocked_reason).length <= 32768 && typeof goal.status === 'string' && statuses.has(goal.status) && (goal.goal_id ? goal.status !== 'none' : goal.status === 'none' && !goal.running) && typeof goal.deferred === 'boolean' && typeof goal.running === 'boolean' && (!goal.running || !!goal.goal_run_id) && natural(goal.tokens_used) && (goal.token_budget == null || natural(goal.token_budget) && goal.token_budget > 0) && (goal.budget_remaining == null || natural(goal.budget_remaining)) && (goal.estimated_costs == null || Array.isArray(goal.estimated_costs) && goal.estimated_costs.length <= 32);
}
export const terminal = (goal: Goal | null) => goal?.status === 'complete' || goal?.status === 'budget_limited';
export const facts = (snapshot: Snapshot): Facts => ({provider: snapshot.provider, model: snapshot.model, permission: snapshot.permission_mode, mode: snapshot.mode, thinking: snapshot.thinking});
const scopeKeys = ['session_id', 'branch_id', 'tip_id', 'goal_id', 'goal_run_id', 'running', 'status', 'deferred', 'objective', 'tokens_used', 'token_budget', 'budget_remaining'] as const;
export function sameScope(inspected: Inspection | null, snapshot: Snapshot | null, goal: Goal | null): boolean {
  return !!inspected && Number.isSafeInteger(inspected.revision) && inspected.revision > 0 && inspected.revision === snapshot?.revision && !!goal && scopeKeys.every(key => inspected.goal[key] === goal[key]) && JSON.stringify(inspected.facts) === JSON.stringify(facts(snapshot));
}
export function budget(text: string): string | null | false {
  if (!text) return null;
  return /^[1-9][0-9]*$/.test(text) && Number.isSafeInteger(Number(text)) ? text : false;
}
