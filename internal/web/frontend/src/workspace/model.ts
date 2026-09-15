// Explicit public presentation DTOs. Never accept a Go page/runtime wholesale.
export interface Project {
  id: string;
  name: string;
  path: string;
  available: boolean;
  trusted: boolean;
  skillsEnabled: boolean;
}
export interface HomeProps {
  projects: Project[];
  error: string;
}
export interface CatalogProps extends HomeProps {
  csrf: string;
  registryEnabled: boolean;
  projectOperationsEnabled: boolean;
}
export interface LoginProps {
  csrf: string;
  error: string;
}
export interface ColdProps extends LoginProps {
  project: Project;
  sessionID: string;
  sessionTitle: string;
  runtimeEnabled: boolean;
  hasHistory: boolean;
  nextURL: string;
  recoveryMessage: string;
  recoveryURL: string;
}
export const uuid = (v: unknown): v is string =>
  typeof v === 'string' &&
  /^[a-f0-9]{8}-(?:[a-f0-9]{4}-){3}[a-f0-9]{12}$/.test(v);
export const bytes = (v: string) => new TextEncoder().encode(v).length;
export function record(v: unknown): Record<string, unknown> {
  if (!v || typeof v !== 'object' || Array.isArray(v))
    throw Error('Invalid presentation');
  return v as Record<string, unknown>;
}
export function string(v: unknown, max: number): string {
  if (typeof v !== 'string' || bytes(v) > max) throw Error('Invalid text');
  return v;
}
function bool(v: unknown): boolean {
  if (typeof v !== 'boolean') throw Error('Invalid flag');
  return v;
}
export function validateProject(value: unknown): Project {
  const v = record(value);
  if (!uuid(v.id)) throw Error('Invalid project');
  return {
    id: v.id,
    name: string(v.name, 128),
    path: string(v.path, 4096),
    available: bool(v.available),
    trusted: bool(v.trusted),
    skillsEnabled: bool(v.skillsEnabled),
  };
}
export function validateHomeProps(value: unknown): HomeProps {
  const v = record(value);
  if (!Array.isArray(v.projects) || v.projects.length > 100)
    throw Error('Invalid projects');
  const projects = v.projects.map(validateProject);
  if (new Set(projects.map((p) => p.id)).size !== projects.length)
    throw Error('Duplicate project');
  return { projects, error: string(v.error, 4096) };
}
export function validateLoginProps(value: unknown): LoginProps {
  const v = record(value);
  return { csrf: string(v.csrf, 512), error: string(v.error, 4096) };
}
export function validateCatalogProps(value: unknown): CatalogProps {
  const v = record(value);
  return {
    ...validateHomeProps(v),
    csrf: string(v.csrf, 512),
    registryEnabled: bool(v.registryEnabled),
    projectOperationsEnabled: bool(v.projectOperationsEnabled),
  };
}
export const projectURL = (id: string) =>
  '/?view=projects&project=' + encodeURIComponent(id);
// Navigation stays on the current manager, never protocol-relative/external.
function navigation(value: unknown): string {
  const v = string(value, 8192);
  if (!v) return v;
  if (!v.startsWith('/?') || /[\u0000-\u0020\\]/u.test(v))
    throw Error('Invalid navigation');
  const url = new URL(v, 'http://snow.invalid');
  const params = url.searchParams;
  if (url.hash || params.get('view') !== 'projects' ||
      [...params.keys()].some(key => !['view', 'project', 'session', 'offset'].includes(key) || params.getAll(key).length !== 1) ||
      (params.has('session') && !/^[A-Za-z0-9_-]{1,128}$/.test(params.get('session')!)) ||
      (params.has('offset') && (!/^(0|[1-9][0-9]{0,4})$/.test(params.get('offset')!) || Number(params.get('offset')) > 10_000)))
    throw Error('Invalid navigation');
  return v;
}
export function validateColdProps(value: unknown): ColdProps {
  const v = record(value),
    project = validateProject(v.project),
    sessionID = string(v.sessionID, 128);
  // Saved sessions use runtimeIdentifier, not the registry's project UUIDs.
  if (sessionID && !/^[A-Za-z0-9_-]{1,128}$/.test(sessionID))
    throw Error('Invalid session');
  const nextURL = navigation(v.nextURL),
    recoveryURL = navigation(v.recoveryURL);
  for (const url of [nextURL, recoveryURL])
    if (
      url &&
      new URL(url, 'http://snow.invalid').searchParams.get('project') !==
        project.id
    )
      throw Error('Wrong project navigation');
  return {
    ...validateLoginProps(v),
    project,
    sessionID,
    sessionTitle: string(v.sessionTitle, 512),
    runtimeEnabled: bool(v.runtimeEnabled),
    hasHistory: bool(v.hasHistory),
    nextURL,
    recoveryURL,
    recoveryMessage: string(v.recoveryMessage, 4096),
  };
}

// Opening placeholders reserve only the visible viewport, never arbitrary CSS.
export function openingHeight(height: number, viewportHeight: number): number {
  if (!Number.isFinite(height) || !Number.isFinite(viewportHeight)) return 0;
  return Math.floor(Math.max(0, Math.min(height, viewportHeight)));
}
