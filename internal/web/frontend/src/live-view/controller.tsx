import {createRef, type ReactNode} from 'react';
import {flushSync} from 'react-dom';
import {createRoot, type Root} from 'react-dom/client';
import {Chrome} from './Chrome';
import {Actions, Editor, Notices, Status, TurnStatus, type DraftHandle, type Suggestions} from './Composer';
import {Regeneration} from './Regeneration';
import {initialChrome, initialControls, sameControlField, type ChromeState, type ControlsState} from './model';

type Part = 'chrome' | 'composer-notices' | 'composer-editor' | 'composer-actions' | 'composer-status' | 'turn-status' | 'regenerate-dialog';
interface Owner {
  region: HTMLElement; roots: Map<Part, Root>; draft: ReturnType<typeof createRef<DraftHandle>>;
  chrome: ChromeState; controls: ControlsState; regeneration: boolean;
}
let owner: Owner | null = null;
function commit(current: Owner, part: Part, node: ReactNode) {
  const root = current.roots.get(part);
  if (root) flushSync(() => root.render(node));
}
function dispose() {
  const previous = owner; owner = null;
  if (!previous) return;
  // app saves drafts and retires captured transport/panel owners first.
  for (const root of previous.roots.values()) flushSync(() => root.unmount());
  previous.roots.clear();
}
function init(region: HTMLElement) {
  if (owner?.region === region) return;
  dispose();
  const current: Owner = {region, roots: new Map(), draft: createRef<DraftHandle>(),
    chrome: initialChrome(), controls: initialControls(),
    regeneration: region.dataset.messageRegenerateEnabled === 'true'};
  current.controls.queue.enabled = region.dataset.queueNextEnabled === 'true';
  const parts: Part[] = ['chrome', 'composer-notices', 'composer-editor', 'composer-actions', 'composer-status', 'turn-status', 'regenerate-dialog'];
  for (const part of parts) {
    const mount = region.querySelector<HTMLElement>(`#live-${part}-view`);
    if (!mount) continue;
    if (part === 'chrome') current.chrome.error = (mount.dataset.initialError || '').slice(0, 65536);
    current.roots.set(part, createRoot(mount));
  }
  owner = current;
  commit(current, 'composer-editor', <Editor ref={current.draft}/>);
  updateChrome({}); updateControls({});
}
function updateChrome(patch: Partial<ChromeState>) {
  const current = owner; if (!current) return;
  const changed = Object.keys(patch).length === 0 || (Object.keys(patch) as (keyof ChromeState)[]).some(key => patch[key] !== current.chrome[key]);
  current.chrome = {...current.chrome, ...patch};
  if (changed) commit(current, 'chrome', <Chrome view={current.chrome}/>);
}
function updateControls(patch: Partial<ControlsState>) {
  const current = owner; if (!current) return;
  const previous = current.controls;
  const initial = Object.keys(patch).length === 0;
  const changed = (...keys: (keyof ControlsState)[]) => initial || keys.some(key => key in patch && !sameControlField(key, patch[key]!, previous[key]));
  current.controls = {...previous, ...patch};
  const view = current.controls;
  // Only an explicit composer-mode change updates the editor's label/placeholder.
  // Streaming status cannot compete with input or IME; the textarea stays mounted.
  if (changed('goalMode')) flushSync(() => current.draft.current?.goalMode(view.goalMode));
  if (changed('reuse', 'edit', 'regenerate')) commit(current, 'composer-notices', <Notices view={view}/>);
  if (changed('sendDisabled', 'showStop', 'sendLabel', 'sending', 'canStop', 'stopLabel', 'stopTitle')) commit(current, 'composer-actions', <Actions view={view}/>);
  if (changed('status', 'statusIdle', 'queue')) commit(current, 'composer-status', <Status view={view}/>);
  if (changed('turnVisible')) commit(current, 'turn-status', <TurnStatus visible={view.turnVisible}/>);
  if (current.regeneration && changed('dialog')) commit(current, 'regenerate-dialog', <Regeneration view={view.dialog}/>);
}
function updateDraft(text: string): boolean {
  const current = owner;
  if (!current?.region.isConnected || !current.draft.current || typeof text !== 'string') return false;
  let accepted = false;
  flushSync(() => { accepted = current.draft.current!.update(text); });
  return accepted;
}
function updateSuggestions(state: Suggestions) {
  const current = owner; if (!current?.draft.current) return;
  flushSync(() => current.draft.current!.suggestions(state));
}
export const LiveViewBridge = {init, mount: init, updateChrome, updateControls, updateDraft, updateSuggestions, dispose, clear: dispose};
