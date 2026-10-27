export type Activity = Readonly<{id: string; tool: string; status: string; output: string}>;
export type Touch = Readonly<{path: string; tool: string; activity: string}>;
export type CanvasFile = {path: string; text: string; truncated: boolean; tool: string; stale: boolean; error: string};

export const FILE_LIMIT = 12;

const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const str = (v: unknown, max = 4096): v is string => typeof v === 'string' && v.length <= max;

/**
 * Files the agent just wrote, taken only from the public tool result text that
 * the write and edit tools return ("Wrote N bytes to PATH", "Replaced N
 * occurrence(s) in PATH"). Tool arguments themselves never reach the browser.
 */
export function touchedFiles(activities: unknown, projectPath: string): Touch[] {
  if (!Array.isArray(activities)) return [];
  const touches: Touch[] = [];
  for (const raw of activities) {
    if (!object(raw) || !str(raw.id, 256) || !str(raw.tool, 128) || !str(raw.output, 65536) || raw.status !== 'completed') continue;
    if (raw.tool !== 'write' && raw.tool !== 'edit') continue;
    const match = /^(?:Wrote \d+ bytes to|Replaced \d+ occurrence\(s\) in) (.+?)\s*$/.exec(raw.output.split('\n', 1)[0] || '');
    if (!match) continue;
    const path = relativePath(match[1]!, projectPath);
    if (path) touches.push({path, tool: raw.tool, activity: raw.id});
  }
  return touches;
}

/** Project-relative path for the read-only file endpoint, or '' when the path escapes the project. */
export function relativePath(path: string, projectPath: string): string {
  let value = path.trim();
  if (!value || value.includes('\0') || value.length > 4096) return '';
  const root = projectPath.replace(/\/+$/, '');
  if (value.startsWith('/')) {
    if (!root || !(value === root || value.startsWith(root + '/'))) return '';
    value = value.slice(root.length + 1);
  }
  value = value.replace(/^\.\/+/, '').replace(/\/+/g, '/');
  if (!value || value === '.' || value.split('/').some(part => part === '..' || part === '')) return '';
  return value;
}

export function validFile(v: unknown): v is {path: string; text: string; size: number; truncated: boolean} {
  return object(v) && str(v.path) && str(v.text, 65536) && Number.isSafeInteger(v.size) && typeof v.truncated === 'boolean';
}

export function basename(path: string): string { return path.slice(path.lastIndexOf('/') + 1) || path; }

export function language(path: string): string {
  const ext = basename(path).toLowerCase().split('.').pop() || '';
  return {go: 'Go', ts: 'TypeScript', tsx: 'TypeScript', js: 'JavaScript', mjs: 'JavaScript', py: 'Python', rs: 'Rust', md: 'Markdown', json: 'JSON', yml: 'YAML', yaml: 'YAML', toml: 'TOML', css: 'CSS', html: 'HTML', sh: 'Shell', sql: 'SQL'}[ext] || (ext ? ext.toUpperCase() : 'Text');
}
