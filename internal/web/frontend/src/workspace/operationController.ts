import { uuid } from './model.ts';
import {
  absolutePath,
  activeStates,
  anonymousRemote,
  boundedJSON,
  destination,
  labels,
  leafName,
  validateFolderPage,
  validateGrant,
  validateInventory,
  validateReceipt,
} from './operationModel.ts';
import type { FolderPage, Grant, Operation } from './operationModel.ts';
export interface OperationDraft {
  parent: string;
  name: string;
  kind: 'create' | 'clone';
  remote: string;
}
export type Action =
  | 'create'
  | 'clone'
  | 'cancel'
  | 'reconcile'
  | 'register'
  | 'dismiss';
export interface Review {
  action: Action;
  id: string;
  revision?: number;
  expires?: number;
  parentPath?: string;
  request: Record<string, string>;
  title: string;
  detail: string;
  effects: string;
  button: string;
}
interface Scope {
  draft: OperationDraft;
  pending: boolean;
  uncertain: string;
  listeners: Set<() => void>;
}
const scopes = new Map<string, Scope>();
function scopeFor(csrf: string) {
  let scope = scopes.get(csrf);
  if (!scope) {
    if (scopes.size >= 8) {
      const old = [...scopes].find(
        ([, s]) => !s.pending && !s.uncertain && !s.listeners.size,
      );
      if (!old) throw Error('Too many pending authorization scopes');
      scopes.delete(old[0]);
    }
    scope = {
      draft: { parent: '', name: '', kind: 'create', remote: '' },
      pending: false,
      uncertain: '',
      listeners: new Set(),
    };
    scopes.set(csrf, scope);
  }
  return scope;
}
interface State {
  draft: OperationDraft;
  reading: boolean;
  pending: boolean;
  uncertain: string;
  grant: Grant | null;
  review: Review | null;
  checked: boolean;
  operations: Operation[];
  folder: FolderPage | null;
  foldersOpen: boolean;
  offset: number;
  next: number;
  more: boolean;
  status: string;
}
export class OperationController {
  private scope: Scope;
  private state: State;
  private listeners = new Set<() => void>();
  private reader: AbortController | null = null;
  private generation = 0;
  private alive = true;
  private csrf: string;
  readonly enabled: boolean;
  constructor(csrf: string, enabled: boolean) {
    this.csrf = csrf;
    this.enabled = enabled;
    this.scope = scopeFor(csrf);
    this.state = {
      draft: { ...this.scope.draft },
      reading: false,
      pending: this.scope.pending,
      uncertain: this.scope.uncertain,
      grant: null,
      review: null,
      checked: false,
      operations: [],
      folder: null,
      foldersOpen: false,
      offset: 0,
      next: 0,
      more: false,
      status:
        'No operation submitted. Closing this panel never cancels accepted work.',
    };
    this.scope.listeners.add(this.scopeChanged);
  }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  getSnapshot = () => this.state;
  private publish(patch: Partial<State>) {
    if (!this.alive) return;
    this.state = { ...this.state, ...patch };
    this.listeners.forEach((f) => f());
  }
  private scopeChanged = () =>
    this.publish({
      pending: this.scope.pending,
      uncertain: this.scope.uncertain,
    });
  private syncScope() {
    this.scope.listeners.forEach((f) => f());
  }
  private current(generation: number) {
    return this.alive && generation === this.generation;
  }
  private canRead() {
    return (
      this.alive && this.enabled && !this.scope.pending && !this.state.reading
    );
  }
  private options(
    signal: AbortSignal,
    fields?: Record<string, string>,
  ): RequestInit {
    return {
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'error',
      signal,
      headers: fields
        ? {
            Accept: 'application/json',
            'Content-Type': 'application/x-www-form-urlencoded',
          }
        : { Accept: 'application/json' },
      ...(fields
        ? {
            method: 'POST',
            body: new URLSearchParams({ csrf: this.csrf, ...fields }),
          }
        : {}),
    };
  }
  private saveDraft(draft: OperationDraft) {
    this.scope.draft = {
      ...draft,
      remote: anonymousRemote(draft.remote) ? draft.remote : '',
    };
  }
  edit(field: keyof OperationDraft, value: string) {
    if (
      !this.alive ||
      this.scope.pending ||
      this.scope.uncertain ||
      this.state.review
    )
      return;
    if (field === 'kind' && value !== 'create' && value !== 'clone') return;
    const draft = { ...this.state.draft, [field]: value };
    this.saveDraft(draft);
    this.publish({
      draft,
      review: null,
      checked: false,
      ...(field === 'parent' ? { grant: null } : {}),
    });
  }
  setParentPath(value: string) {
    if (
      !absolutePath(value) ||
      !this.alive ||
      this.scope.pending ||
      this.scope.uncertain
    )
      return false;
    this.stopRead();
    const draft = { ...this.state.draft, parent: value };
    this.saveDraft(draft);
    this.publish({
      draft,
      grant: null,
      review: null,
      checked: false,
      foldersOpen: false,
    });
    return true;
  }
  private stopRead() {
    this.reader?.abort();
    this.reader = null;
    this.generation++;
    this.publish({ reading: false });
  }
  retire() {
    this.stopRead();
    this.saveDraft({ ...this.state.draft, remote: '' });
    this.publish({
      draft: { ...this.state.draft, remote: '' },
      grant: null,
      review: null,
      checked: false,
      folder: null,
      foldersOpen: false,
    });
  }
  dispose() {
    if (!this.alive) return;
    this.retire();
    this.alive = false;
    this.scope.listeners.delete(this.scopeChanged);
    this.listeners.clear();
  }
  private async read(
    url: string,
    apply: (value: unknown) => void,
    status: string,
    fields?: Record<string, string>,
  ) {
    if (!this.canRead()) return;
    this.stopRead();
    const generation = this.generation,
      controller = new AbortController();
    this.reader = controller;
    this.publish({ reading: true, review: null, checked: false, status });
    try {
      const response = await fetch(
        url,
        this.options(
          AbortSignal.any([controller.signal, AbortSignal.timeout(15000)]),
          fields,
        ),
      );
      const result = await boundedJSON(response);
      if (this.current(generation)) apply(result);
    } catch (error) {
      if (this.current(generation))
        this.publish({
          status:
            error instanceof Error && error.message === 'auth'
              ? 'Browser authorization changed. Pair or reload this manager before continuing.'
              : 'Could not read current host metadata. Refresh explicitly; no operation was retried.',
        });
    } finally {
      if (this.current(generation)) {
        this.reader = null;
        this.publish({ reading: false });
      }
    }
  }
  refresh = (offset = 0) =>
    this.read(
      `/operations?offset=${offset}`,
      (value) => {
        const page = validateInventory(value, offset);
        this.publish({
          ...page,
          offset,
          status:
            'Read current durable operations. Refresh to observe changes; no work was replayed.',
        });
      },
      'Reading durable operation metadata…',
    );
  browse = (path = '', offset = 0) => {
    if (
      !this.canRead() ||
      this.scope.uncertain ||
      (path !== '' && !absolutePath(path))
    )
      return;
    this.publish({ foldersOpen: true });
    return this.read(
      '/projects/folders',
      (value) => {
        const folder = validateFolderPage(value);
        this.publish({
          folder,
          status: folder.limited
            ? 'Folder scan limit reached. Enter an absolute host path if needed.'
            : 'Folder browsing is read only. Use this path, then explicitly select the parent.',
        });
      },
      'Reading folders on the Snow host…',
      { path, offset: String(offset) },
    );
  };
  closeFolders() {
    this.stopRead();
    this.publish({ foldersOpen: false });
  }
  select = async () => {
    if (!this.canRead() || this.scope.uncertain) return;
    const path = this.state.draft.parent;
    if (!absolutePath(path)) {
      this.publish({
        status: 'Enter an absolute parent folder on the Snow host.',
      });
      return;
    }
    this.publish({ grant: null });
    await this.read(
      '/projects/folders/select',
      (value) => {
        const grant = validateGrant(value),
          draft = { ...this.state.draft, parent: grant.path };
        this.saveDraft(draft);
        this.publish({
          grant,
          draft,
          status:
            'Parent selected. Enter a new name, then review before any filesystem change.',
        });
      },
      'Validating the explicitly selected parent…',
      { path },
    );
  };
  reviewCreation = () => {
    const { grant, draft } = this.state;
    if (!this.canRead() || this.scope.uncertain || !grant) return;
    if (Date.now() >= grant.expires_at) {
      this.publish({
        grant: null,
        status:
          'Parent selection expired. Explicitly select it again before reviewing.',
      });
      return;
    }
    if (
      !leafName(draft.name) ||
      (draft.kind === 'clone' && !anonymousRemote(draft.remote))
    ) {
      const next = {
        ...draft,
        remote: anonymousRemote(draft.remote) ? draft.remote : '',
      };
      this.saveDraft(next);
      this.publish({
        draft: next,
        status:
          'Review requires one valid leaf name and, for cloning, an anonymous HTTPS URL. Rejected URLs are not retained.',
      });
      return;
    }
    const clone = draft.kind === 'clone',
      request: Record<string, string> = {
        operation_id: grant.operation_id,
        name: draft.name,
      };
    if (clone) request.remote = draft.remote;
    this.showReview({
      action: draft.kind,
      id: grant.operation_id,
      expires: grant.expires_at,
      parentPath: grant.path,
      request,
      title: clone ? 'Confirm repository clone' : 'Confirm folder creation',
      detail:
        destination(grant.path, draft.name) +
        (clone ? `\nFrom ${draft.remote}` : ''),
      effects: `${clone ? 'Create this destination and download the repository using anonymous HTTPS. ' : 'Create this empty destination. '}The host OS user's permissions apply; no startup-root confinement or disk quota is provided. Partial files are retained on failure or stop. Registration requires a separate explicit review after completion; no agent is activated. Closing this panel does not stop accepted work.`,
      button: clone ? 'Clone repository' : 'Create folder',
    });
  };
  reviewAction = (
    op: Operation,
    action: Exclude<Action, 'create' | 'clone'>,
  ) => {
    if (
      !this.canRead() ||
      this.scope.uncertain ||
      this.state.review ||
      !this.state.operations.some(
        (o) => o.id === op.id && o.revision === op.revision,
      )
    )
      return;
    const active = activeStates.has(op.state);
    if (
      action === 'cancel' ? !active || op.state === 'cancel_requested' : active
    )
      return;
    if (action === 'register' && op.project_id) return;
    const ordinary =
      action === 'register' &&
      !(op.state === 'awaiting_registration' && op.outcome === 'observed');
    const titles = {
      cancel: 'Confirm stop request',
      reconcile: 'Confirm identity observation',
      register: ordinary
        ? 'Review ordinary registration'
        : 'Confirm retained registration',
      dismiss: 'Confirm metadata-only dismissal',
    };
    const effects = {
      cancel:
        'Request stop for this exact operation revision. Stop is not complete until worker cleanup is confirmed. Any destination and partial files remain; this is not rollback.',
      reconcile:
        'Observe the recorded directory identity only. This does not retry mkdir, clone, or registration, and does not stop any process.',
      register: ordinary
        ? 'Explicit ordinary registration of the directory currently at this destination. Its ownership or operation outcome is not established. This does not retry cloning and will not convert unknown or failed work into a created success. No agent is activated.'
        : 'Register only the exact retained child identity. Files and outcome are already recorded. This does not retry cloning, execute project code, or activate an agent.',
      dismiss:
        "Remove this settled operation's manager record only. No files, project registration, or session will be deleted. A dismissed request ID cannot be reused.",
    };
    const request: Record<string, string> = { revision: String(op.revision) };
    if (ordinary) request.review = 'true';
    this.showReview({
      action,
      id: op.id,
      revision: op.revision,
      request,
      title: titles[action],
      detail: `${op.name}\n${op.child?.path || destination(op.parent.path, op.name)}\nReference ${op.id} · revision ${op.revision}`,
      effects: effects[action],
      button: titles[action],
    });
  };
  private showReview(review: Review) {
    this.publish({ review, checked: false });
  }
  back = () => {
    if (!this.scope.pending) this.publish({ review: null, checked: false });
  };
  checkConsent = (checked: boolean) => this.publish({ checked });
  confirm = async () => {
    const review = this.state.review;
    if (
      !review ||
      !this.state.checked ||
      !this.canRead() ||
      this.scope.uncertain
    )
      return;
    const creation = review.action === 'create' || review.action === 'clone';
    if (
      creation &&
      (!this.state.grant ||
        Date.now() >= (review.expires || 0) ||
        this.state.grant.operation_id !== review.id)
    ) {
      this.publish({
        grant: null,
        review: null,
        checked: false,
        status:
          'Parent selection expired. No operation was submitted; select and review again.',
      });
      return;
    }
    this.stopRead();
    const generation = this.generation;
    this.scope.pending = true;
    this.scope.uncertain = review.id;
    this.scope.draft.remote = '';
    this.publish({
      review: null,
      checked: false,
      ...(creation ? { grant: null } : {}),
      draft: { ...this.state.draft, remote: '' },
      status:
        'Submitting the explicit request once. Closing this panel does not cancel accepted work.',
    });
    this.syncScope();
    try {
      const endpoint = creation
        ? `/projects/${review.action}`
        : `/operations/${review.id}/${review.action}`;
      const response = await fetch(
        endpoint,
        this.options(AbortSignal.timeout(15000), review.request),
      );
      if (review.action === 'dismiss' && response.status === 204) {
        if (!this.current(generation)) return;
        this.scope.uncertain = '';
        this.publish({
          operations: this.state.operations.filter((o) => o.id !== review.id),
          status:
            'Operation record dismissed. Files and project registration are unchanged.',
        });
      } else {
        const value = await boundedJSON(response);
        if (!this.current(generation)) return;
        const op = validateReceipt(value, review.id, review.revision);
        if (
          creation &&
          (op.kind !== review.action ||
            op.name !== review.request.name ||
            op.parent.path !== review.parentPath ||
            (op.remote || '') !== (review.request.remote || ''))
        )
          throw Error('Wrong destination receipt');
        this.scope.uncertain = '';
        const exists = this.state.operations.some((o) => o.id === op.id);
        this.publish({
          operations: exists
            ? this.state.operations.map((o) => (o.id === op.id ? op : o))
            : [op],
          ...(!exists ? { offset: 0, more: false } : {}),
          status: `${labels[op.state]}. Refresh to observe durable progress. No navigation or activation was performed.`,
        });
      }
    } catch (error) {
      if (this.current(generation))
        this.publish({
          status:
            error instanceof Error && error.message === 'auth'
              ? 'Browser authorization changed. The request may already have taken effect. Pair or reload, then inspect durable operations; never replay it.'
              : "Could not confirm this request's outcome. Do not retry cloning or creation. Check its durable record; no mutation will be replayed automatically.",
        });
    } finally {
      this.scope.pending = false;
      this.syncScope();
    }
  };
  check = async () => {
    const id = this.scope.uncertain;
    if (!uuid(id) || !this.canRead()) return;
    this.stopRead();
    const generation = this.generation,
      controller = new AbortController();
    this.reader = controller;
    this.publish({
      reading: true,
      status:
        'Checking the existing request ID only; no operation is being retried…',
    });
    try {
      const response = await fetch(
        `/operations/${id}`,
        this.options(
          AbortSignal.any([controller.signal, AbortSignal.timeout(15000)]),
        ),
      );
      if (!this.current(generation)) return;
      if (response.status === 404) {
        this.scope.uncertain = '';
        this.publish({
          status:
            'No retained record exists for that ID. Its grant cannot be reused. Inspect the host before explicitly selecting a parent for any new operation.',
        });
        return;
      }
      const op = validateReceipt(await boundedJSON(response), id);
      if (!this.current(generation)) return;
      this.scope.uncertain = '';
      this.publish({
        operations: [op],
        offset: 0,
        more: false,
        status: `${labels[op.state]}. Existing record observed without retrying work.`,
      });
    } catch {
      if (this.current(generation))
        this.publish({
          status:
            'The request remains unconfirmed. Check again explicitly; never retry the clone to recover its status.',
        });
    } finally {
      if (this.current(generation)) {
        this.reader = null;
        this.publish({ reading: false });
      }
      this.syncScope();
    }
  };
}
