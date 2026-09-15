import { record, text } from './model.ts';

export const reuseTitle = 'Use a copy as a new prompt. Saved history stays unchanged.';
export type ActionPresentation = {
  reuse: {hidden: boolean; disabled: boolean; title: string};
  edit: {hidden: boolean; disabled: boolean; messageID: string};
  regenerate: {hidden: boolean; disabled: boolean};
  historicalDisabled: boolean;
};
export type ActionPresentationPatch = {
  reuse?: Partial<ActionPresentation['reuse']>;
  edit?: Partial<ActionPresentation['edit']>;
  regenerate?: Partial<ActionPresentation['regenerate']>;
  historicalDisabled?: boolean;
};
export function defaultActions(): ActionPresentation {
  return {reuse: {hidden: true, disabled: true, title: reuseTitle}, edit: {hidden: true, disabled: true, messageID: ''}, regenerate: {hidden: true, disabled: true}, historicalDisabled: false};
}
// Presentation only: no readiness, operation, transport or admission decisions
// belong here. Explicit malformed flags close controls; omitted fields retain
// their prior projection so independently updated parent regions cannot race.
export function mergeActions(current: ActionPresentation, patch: ActionPresentationPatch): ActionPresentation {
  const input = record(patch), reuse = record(input.reuse), edit = record(input.edit), regenerate = record(input.regenerate);
  const flag = (input: Record<string, unknown>, key: string, previous: boolean) => Object.hasOwn(input, key) ? typeof input[key] === 'boolean' ? input[key] : true : previous;
  const next: ActionPresentation = {
    reuse: {hidden: flag(reuse, 'hidden', current.reuse.hidden), disabled: flag(reuse, 'disabled', current.reuse.disabled), title: Object.hasOwn(reuse, 'title') ? text(reuse.title, 512) || reuseTitle : current.reuse.title},
    edit: {hidden: flag(edit, 'hidden', current.edit.hidden), disabled: flag(edit, 'disabled', current.edit.disabled), messageID: Object.hasOwn(edit, 'messageID') ? typeof edit.messageID === 'string' && edit.messageID.length <= 256 ? edit.messageID : '' : current.edit.messageID},
    regenerate: {hidden: flag(regenerate, 'hidden', current.regenerate.hidden), disabled: flag(regenerate, 'disabled', current.regenerate.disabled)},
    historicalDisabled: flag(input, 'historicalDisabled', current.historicalDisabled)
  };
  return JSON.stringify(next) === JSON.stringify(current) ? current : next;
}
