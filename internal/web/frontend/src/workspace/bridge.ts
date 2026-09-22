import { useEffect, useSyncExternalStore } from 'react';
import { flushSync } from 'react-dom';
import { openingHeight } from './model';
export interface OpeningProjection {
  visible: boolean;
  minHeight: number;
}
export interface DraftNoticeProjection {
  text: string;
  explanation: string;
  url: string;
  useVisible: boolean;
  useDisabled: boolean;
}
export interface DraftProjection {
  text: string;
  workspaceText: string;
  homeEnabled: boolean;
  workspaceEnabled: boolean;
  name: string;
  pending: boolean;
  privacy: string;
  notice: DraftNoticeProjection | null;
}
export interface FolderProjection {
  path: string;
  parent: string;
  folders: { name: string; path: string }[];
  hasMore: boolean;
  busy: boolean;
  canSelect: boolean;
  error: string;
  status: string;
}
const initialDraft: DraftProjection = {
  text: '',
  workspaceText: '',
  homeEnabled: false,
  workspaceEnabled: false,
  name: '',
  pending: false,
  privacy:
    'Choose a workspace, then start or resume. Your draft stays in this tab until you send it; reloading clears it.',
  notice: null,
};
const initialFolder: FolderProjection = {
  path: '',
  parent: '',
  folders: [],
  hasMore: false,
  busy: false,
  canSelect: false,
  error: '',
  status: '',
};
let snapshot = {
  draft: initialDraft,
  folder: initialFolder,
  projectPath: '',
  flowError: '',
  activation: { busy: false, error: '' },
  opening: { visible: false, minHeight: 0 } as OpeningProjection,
  inspectorOpen: false,
};
const listeners = new Set<() => void>();
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};
const getSnapshot = () => snapshot;
function publish(next: typeof snapshot, sync = true) {
  snapshot = next;
  const notify = () => listeners.forEach((f) => f());
  if (sync) flushSync(notify);
  else notify();
}
export function useWorkspace() {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
}
export function useWorkspaceMounted() {
  useEffect(() => {
    let mounted = true;
    queueMicrotask(() => {
      if (mounted) document.dispatchEvent(new Event('snow:workspace-mounted'));
    });
    return () => {
      mounted = false;
    };
  }, []);
}
function sameNotice(left: DraftNoticeProjection | null, right: DraftNoticeProjection | null | undefined) {
  return left === right || !!left && !!right && left.text === right.text && left.explanation === right.explanation && left.url === right.url &&
    left.useVisible === right.useVisible && left.useDisabled === right.useDisabled;
}
function draftChanged(current: DraftProjection, value: Partial<DraftProjection>) {
  return (Object.keys(value) as (keyof DraftProjection)[]).some(key => key === 'notice' ? !sameNotice(current.notice, value.notice) : current[key] !== value[key]);
}
// This is a view projection, not a second canonical draft or business controller.
// Native input events continue bubbling to app's sole draft/admission owner.
export const workspace = {
  updateDraft(value: Partial<DraftProjection>) {
    if (!draftChanged(snapshot.draft, value)) return;
    publish({ ...snapshot, draft: { ...snapshot.draft, ...value } });
  },
  editDraft(text: string, cold = false) {
    publish(
      {
        ...snapshot,
        draft: {
          ...snapshot.draft,
          ...(cold ? { workspaceText: text } : { text }),
        },
      },
      false,
    );
  },
  updateFolder(value: Partial<FolderProjection>) {
    publish({ ...snapshot, folder: { ...snapshot.folder, ...value } });
  },
  setProjectPath(path: string) {
    if (
      !path.startsWith('/') ||
      path.length > 4096 ||
      /[\u0000-\u001f]/u.test(path)
    )
      return false;
    publish({ ...snapshot, projectPath: path });
    return true;
  },
  editProjectPath(path: string) {
    publish({ ...snapshot, projectPath: path }, false);
  },
  updateActivation(value: Partial<{ busy: boolean; error: string }>) {
    publish({ ...snapshot, activation: { ...snapshot.activation, ...value } });
  },
  updateOpening(value: OpeningProjection) {
    const viewport = typeof window === 'undefined' ? 0 : window.innerHeight;
    publish({
      ...snapshot,
      opening: {
        visible: value.visible === true,
        minHeight: openingHeight(value.minHeight, viewport),
      },
    });
  },
  updateInspector(open: boolean) {
    publish({ ...snapshot, inspectorOpen: open === true });
  },
  updateFlowError(flowError: string) {
    publish({ ...snapshot, flowError });
  },
  reset() {
    publish({
      draft: initialDraft,
      folder: initialFolder,
      projectPath: '',
      flowError: '',
      activation: { busy: false, error: '' },
      opening: { visible: false, minHeight: 0 },
      inspectorOpen: false,
    });
  },
};
