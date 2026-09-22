/** Presentation only. No runtime identities, edit tokens, or request authority. */
export interface ChromeState {
  connection: string; connected: boolean; reload: boolean;
  error: string; unknown: boolean; reviewDisabled: boolean;
  recoveryVisible: boolean; recoveryMessage: string; canceled: boolean;
}
export interface NoticeState {
  visible: boolean; text: string; disabled: boolean;
}
export interface ControlsState {
  sendDisabled: boolean; showStop: boolean; sendLabel: string; sending: boolean; goalMode: boolean;
  canStop: boolean; stopLabel: string; stopTitle: string;
  status: string; statusIdle: boolean; turnVisible: boolean;
  queue: {enabled: boolean; hidden: boolean; disabled: boolean; label: string; title: string; hint: string};
  reuse: NoticeState; edit: NoticeState;
  regenerate: {visible: boolean; text: string; dismiss: boolean};
  dialog: {status: string; confirmDisabled: boolean; cancelDisabled: boolean};
}
export const initialChrome = (): ChromeState => ({
  connection: 'Connecting…', connected: false, reload: false, error: '',
  unknown: false, reviewDisabled: true, recoveryVisible: false,
  recoveryMessage: '', canceled: false,
});
export function sameControlField<K extends keyof ControlsState>(key: K, left: ControlsState[K], right: ControlsState[K]): boolean {
  if (left === right) return true;
  if (left == null || right == null) return false;
  if (key === 'queue') {
    const a = left as ControlsState['queue'], b = right as ControlsState['queue'];
    return a.enabled === b.enabled && a.hidden === b.hidden && a.disabled === b.disabled && a.label === b.label && a.title === b.title && a.hint === b.hint;
  }
  if (key === 'reuse' || key === 'edit') {
    const a = left as NoticeState, b = right as NoticeState;
    return a.visible === b.visible && a.text === b.text && a.disabled === b.disabled;
  }
  if (key === 'regenerate') {
    const a = left as ControlsState['regenerate'], b = right as ControlsState['regenerate'];
    return a.visible === b.visible && a.text === b.text && a.dismiss === b.dismiss;
  }
  if (key === 'dialog') {
    const a = left as ControlsState['dialog'], b = right as ControlsState['dialog'];
    return a.status === b.status && a.confirmDisabled === b.confirmDisabled && a.cancelDisabled === b.cancelDisabled;
  }
  return false;
}
export const initialControls = (): ControlsState => ({
  sendDisabled: true, showStop: false, sendLabel: 'Send message', sending: false, goalMode: false,
  canStop: false, stopLabel: 'Stop', stopTitle: 'Stop generation',
  status: 'Updates unavailable · your draft is kept', statusIdle: false, turnVisible: false,
  queue: {enabled: false, hidden: true, disabled: true, label: 'Queue next', title: '', hint: 'Ctrl / ⌘ + Enter to send · Enter for a new line'},
  reuse: {visible: false, text: '', disabled: false},
  edit: {visible: false, text: '', disabled: false},
  regenerate: {visible: false, text: '', dismiss: false},
  dialog: {status: 'Preparing confirmation…', confirmDisabled: true, cancelDisabled: false},
});
