import { bytes, record, uuid } from './model.ts';
export const safeText = (v: unknown, max: number): v is string =>
  typeof v === 'string' &&
  bytes(v) <= max &&
  !/[\u0000-\u001f\u007f-\u009f]/u.test(v);
export const absolutePath = (v: unknown): v is string =>
  safeText(v, 4096) && v.startsWith('/');
export const leafName = (v: unknown): v is string =>
  safeText(v, 128) &&
  v.length > 0 &&
  v.trim() === v &&
  v !== '.' &&
  v !== '..' &&
  !/[\/\\]/u.test(v);
export function anonymousRemote(v: unknown): v is string {
  if (
    !safeText(v, 512) ||
    !/^https:\/\/[a-zA-Z0-9.-]+\/[a-zA-Z0-9._/-]+$/.test(v)
  )
    return false;
  const parts = v.slice(v.indexOf('/', 8) + 1).split('/');
  return (
    parts.length >= 2 &&
    parts.length <= 8 &&
    parts.every(
      (p) => p.length > 0 && p.length <= 128 && p !== '.' && p !== '..',
    )
  );
}
export interface Identity {
  path: string;
  device: string;
  inode: string;
}
export const labels = {
  admitted: 'Admitted',
  creating: 'Creating destination',
  running: 'Running',
  cancel_requested: 'Stop requested — cleanup not yet confirmed',
  awaiting_registration: 'Files retained — registration pending',
  succeeded: 'Created and registered',
  failed: 'Failed — destination may be partial',
  canceled: 'Canceled after cleanup — files retained',
  interrupted: 'Interrupted — ownership or outcome may be unknown',
  needs_review: 'Needs review — do not assume completion',
};
export const activeStates = new Set<string>([
  'admitted',
  'creating',
  'running',
  'cancel_requested',
]);
export interface Operation {
  id: string;
  kind: 'create' | 'clone';
  name: string;
  state: keyof typeof labels;
  revision: number;
  parent: Identity;
  child?: Identity;
  outcome: 'observed' | 'not_observed' | 'unknown';
  project_id?: string;
  remote?: string;
  created_at: number;
  updated_at: number;
}
export interface FolderPage {
  path: string;
  parent: string;
  folders: { name: string; path: string }[];
  next_offset: number;
  has_more: boolean;
  limited?: boolean;
}
export interface Grant {
  operation_id: string;
  path: string;
  expires_at: number;
}
export const destination = (parent: string, name: string) =>
  `${parent === '/' ? '' : parent}/${name}`;
function identity(v: unknown): v is Identity {
  if (!v || typeof v !== 'object') return false;
  const x = v as Record<string, unknown>;
  return (
    absolutePath(x.path) &&
    typeof x.device === 'string' &&
    /^[0-9]{1,20}$/.test(x.device) &&
    typeof x.inode === 'string' &&
    /^[0-9]{1,20}$/.test(x.inode)
  );
}
export function validateOperation(value: unknown): Operation {
  const op = record(value);
  if (
    !uuid(op.id) ||
    !['create', 'clone'].includes(String(op.kind)) ||
    !leafName(op.name) ||
    typeof op.state !== 'string' ||
    !Object.hasOwn(labels, op.state) ||
    !Number.isSafeInteger(op.revision) ||
    Number(op.revision) < 1 ||
    !identity(op.parent) ||
    !['observed', 'not_observed', 'unknown'].includes(String(op.outcome))
  )
    throw Error('Invalid operation');
  if (
    (op.project_id && !uuid(op.project_id)) ||
    (op.kind === 'clone' && !anonymousRemote(op.remote)) ||
    (op.kind === 'create' && op.remote)
  )
    throw Error('Invalid remote or project');
  if (op.child !== undefined && op.child !== null) {
    const child = record(op.child);
    if (
      child.path &&
      (!identity(child) || child.path !== destination(op.parent.path, op.name))
    )
      throw Error('Invalid child');
  }
  if (
    ['running', 'succeeded', 'awaiting_registration'].includes(op.state) &&
    (!identity(op.child) || op.outcome !== 'observed')
  )
    throw Error('Invalid observed identity');
  if (
    (op.state === 'succeeded' && !uuid(op.project_id)) ||
    (op.state === 'awaiting_registration' && op.project_id)
  )
    throw Error('Invalid registration');
  if (
    ![op.created_at, op.updated_at].every(
      (v) => Number.isSafeInteger(v) && Number(v) > 0,
    ) ||
    Number(op.updated_at) < Number(op.created_at)
  )
    throw Error('Invalid timestamp');
  return op as unknown as Operation;
}
export function validateInventory(value: unknown, offset: number) {
  const v = record(value);
  if (!Array.isArray(v.operations) || v.operations.length > 32)
    throw Error('Invalid inventory');
  const operations = v.operations.map(validateOperation);
  if (
    new Set(operations.map((o) => o.id)).size !== operations.length ||
    !Number.isSafeInteger(v.next_offset) ||
    v.next_offset !== offset + operations.length ||
    v.next_offset > 128 ||
    typeof v.has_more !== 'boolean' ||
    (v.has_more && operations.length === 0)
  )
    throw Error('Invalid page');
  return { operations, next: v.next_offset, more: v.has_more };
}
export function validateFolderPage(value: unknown): FolderPage {
  const v = record(value);
  if (
    !absolutePath(v.path) ||
    !absolutePath(v.parent) ||
    !Array.isArray(v.folders) ||
    v.folders.length > 256 ||
    !Number.isSafeInteger(v.next_offset) ||
    Number(v.next_offset) < 0 ||
    Number(v.next_offset) > 4096 ||
    typeof v.has_more !== 'boolean'
  )
    throw Error('Invalid folders');
  const folders = v.folders.map((value) => {
    const f = record(value);
    if (!safeText(f.name, 4096) || !absolutePath(f.path))
      throw Error('Invalid folder');
    return { name: f.name, path: f.path };
  });
  return {
    path: v.path,
    parent: v.parent,
    folders,
    next_offset: Number(v.next_offset),
    has_more: v.has_more,
    limited: v.limited === true,
  };
}
export function validateGrant(value: unknown, now = Date.now()): Grant {
  const v = record(value);
  if (
    !uuid(v.operation_id) ||
    !absolutePath(v.path) ||
    !Number.isSafeInteger(v.expires_at) ||
    Number(v.expires_at) <= now ||
    Number(v.expires_at) > now + 301000
  )
    throw Error('Invalid grant');
  return {
    operation_id: v.operation_id,
    path: v.path,
    expires_at: Number(v.expires_at),
  };
}
export function validateReceipt(value: unknown, id: string, revision?: number) {
  const op = validateOperation(value);
  if (op.id !== id || (revision !== undefined && op.revision <= revision))
    throw Error('Wrong receipt');
  return op;
}
export async function boundedJSON(response: Response): Promise<unknown> {
  if (!response.ok)
    throw Error(
      response.status === 401 || response.status === 403
        ? 'auth'
        : response.status === 404
          ? 'missing'
          : 'request',
    );
  if (!response.body) throw Error('Empty response');
  const reader = response.body.getReader(),
    chunks: Uint8Array[] = [];
  let size = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      size += value.length;
      if (size > 65536) {
        await reader.cancel();
        throw Error('Bounded response');
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  const data = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    data.set(chunk, offset);
    offset += chunk.length;
  }
  return JSON.parse(
    new TextDecoder('utf-8', { fatal: true }).decode(data),
  ) as unknown;
}
