/** Public worker facts only; this panel never invents model capabilities. */
export const fields = {thinking: "Thinking", reasoning_summary: "Reasoning summary", text_verbosity: "Text verbosity"} as const;
export type Field = keyof typeof fields;
export const options = {thinking: "thinking_levels", reasoning_summary: "reasoning_summaries", text_verbosity: "text_verbosities"} as const;
export const identityKeys = ["project_id", "instance_id", "session_id"] as const;
export const facts = [...identityKeys, "branch_id", "tip_id", "provider", "model", "mode", "permission_mode", "thinking", "reasoning_summary", "text_verbosity"] as const;
export type Identity = Readonly<Record<typeof identityKeys[number], string>>;
export type Inspection = Readonly<Record<typeof facts[number], string> & {
  revision: number;
  defaults_available: false;
  current_session_available: true;
  thinking_levels?: readonly string[] | null;
  reasoning_summaries?: readonly string[] | null;
  text_verbosities?: readonly string[] | null;
}>;
export type Snapshot = Readonly<Identity & {
  revision: number; provider: string; model: string; mode: string;
  permission_mode: string; thinking: string;
  [key: string]: unknown;
}>;
export type UI = Readonly<{readable: boolean; safe: boolean}>;
export interface API {
  readonly root: ParentNode;
  readonly identity: Identity;
  readonly openDialog: (dialog: HTMLDialogElement, opener: HTMLButtonElement) => void;
  readonly closeDialog: (dialog: HTMLDialogElement) => void;
  readonly reserve: (active: boolean) => unknown;
  readonly inspect: (signal: AbortSignal) => Promise<unknown>;
  readonly set: (payload: Readonly<Record<string, string>>) => Promise<unknown>;
}
const text = (value: unknown): value is string => typeof value === "string" && value.length > 0 && value.length <= 256 && !/[\u0000-\u001f\u007f]/.test(value);
export function valid(result: unknown, identity: Identity): result is Inspection {
  if (!result || typeof result !== "object") return false;
  const r = result as Record<string, unknown>;
  return identityKeys.every(k => r[k] === identity[k]) &&
    facts.every(k => k === "tip_id" ? r[k] === "" || text(r[k]) : text(r[k])) &&
    typeof r.revision === "number" && Number.isSafeInteger(r.revision) && r.revision > 0 &&
    ["default", "plan"].includes(r.mode as string) && ["ask", "deny", "allow"].includes(r.permission_mode as string) &&
    r.defaults_available === false && r.current_session_available === true &&
    Object.values(options).every(k => r[k] == null || Array.isArray(r[k]) && r[k].length <= 16 && r[k].every(text) && new Set(r[k]).size === r[k].length);
}
export function sameScope(a: Inspection | null, s: Snapshot | null): boolean {
  return !!a && !!s && a.revision === s.revision &&
    [...identityKeys, "provider", "model", "mode", "permission_mode", "thinking"].every(k => a[k as keyof Inspection] === s[k]);
}
