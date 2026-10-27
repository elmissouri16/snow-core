import {flushSync} from 'react-dom';
import {createRoot} from 'react-dom/client';
import type {Root} from 'react-dom/client';
import {CanvasPanel} from './CanvasPanel.tsx';
import {FILE_LIMIT, touchedFiles, validFile} from './model.ts';
import type {CanvasFile} from './model.ts';

export interface CanvasAPI {
  /** The canvas aside; the React mount lives inside it. */
  pane: HTMLElement;
  projectPath: string;
  /** Read-only file text through the existing inspection endpoint. */
  readFile: (path: string, signal: AbortSignal) => Promise<unknown>;
  /** Returns focus to the composer when the pane closes. */
  focusComposer: () => void;
}
export type UI = Readonly<{running: boolean}>;

const AUTO_KEY = 'snow-canvas-auto';
let view: Controller | null = null;

export class Controller {
  readonly api: CanvasAPI;
  readonly root: Root;
  readonly lifetime = new AbortController();
  files: CanvasFile[] = [];
  selected = '';
  seen = new Set<string>();
  running = false;
  auto = true;
  reading: AbortController | null = null;
  generation = 0;
  constructor(api: CanvasAPI, container: Element) {
    this.api = api;
    this.root = createRoot(container);
    try { this.auto = localStorage.getItem(AUTO_KEY) !== 'off'; } catch { /* preference is optional */ }
  }
  current = () => view === this;
  get open() { return !this.api.pane.hidden; }
  publish = () => { if (this.current()) flushSync(() => this.root.render(<CanvasPanel controller={this} />)); };
  show = (path = this.selected) => {
    if (!this.current()) return;
    this.api.pane.hidden = false;
    if (path) this.selected = path;
    this.publish();
    // Opening always reads the selected file: it may be new or changed since
    // the pane was last visible, and reads are skipped while hidden.
    if (this.selected) void this.load(this.selected);
    document.dispatchEvent(new CustomEvent('snow:canvas-visibility', {detail: {open: true}}));
  };
  close = () => {
    if (!this.current()) return;
    this.api.pane.hidden = true; this.reading?.abort(); this.reading = null; this.publish();
    document.dispatchEvent(new CustomEvent('snow:canvas-visibility', {detail: {open: false}}));
    this.api.focusComposer();
  };
  setAuto = (auto: boolean) => {
    if (!this.current()) return;
    this.auto = auto; this.publish();
    try { localStorage.setItem(AUTO_KEY, auto ? 'on' : 'off'); } catch { /* preference is optional */ }
  };
  select = (path: string) => {
    if (!this.current() || !this.files.some(file => file.path === path)) return;
    this.selected = path; this.publish();
    void this.load(path);
  };
  remove = (path: string) => {
    if (!this.current()) return;
    this.files = this.files.filter(file => file.path !== path);
    if (this.selected === path) { this.selected = this.files[0]?.path || ''; if (this.selected) void this.load(this.selected); }
    if (!this.files.length) this.close(); else this.publish();
  };
  /** Snapshot activities are the only source of truth for which files changed. */
  observe = (activities: unknown, ui: UI) => {
    if (!this.current()) return;
    this.running = ui.running;
    let newest = '';
    for (const touch of touchedFiles(activities, this.api.projectPath)) {
      if (this.seen.has(touch.activity)) continue;
      this.seen.add(touch.activity);
      const existing = this.files.find(file => file.path === touch.path);
      if (existing) { existing.stale = true; existing.tool = touch.tool; this.files = [existing, ...this.files.filter(file => file !== existing)]; }
      else this.files = [{path: touch.path, text: '', truncated: false, tool: touch.tool, stale: true, error: ''}, ...this.files].slice(0, FILE_LIMIT);
      newest = touch.path;
    }
    if (!newest) { this.publish(); return; }
    if (this.auto && !this.open) { this.selected = newest; this.show(newest); return; }
    if (this.open && (!this.selected || this.selected === newest || this.files.find(file => file.path === this.selected)?.stale)) {
      if (!this.selected || this.selected === newest) this.selected = newest;
      void this.load(this.selected);
    }
    this.publish();
  };
  load = async (path: string) => {
    if (!this.current() || !this.open) return;
    this.reading?.abort();
    const controller = new AbortController(); this.reading = controller;
    const generation = ++this.generation;
    const file = this.files.find(entry => entry.path === path); if (!file) return;
    try {
      const data = await this.api.readFile(path, AbortSignal.any([controller.signal, this.lifetime.signal]));
      if (!this.current() || generation !== this.generation) return;
      if (!validFile(data) || data.path !== path) throw new Error('Unexpected file preview');
      Object.assign(file, {text: data.text, truncated: data.truncated, stale: false, error: ''});
    } catch {
      if (!this.current() || generation !== this.generation || controller.signal.aborted) return;
      file.error = 'This file could not be read. It may be protected, binary, too large, or already gone.';
      file.stale = false;
    } finally {
      if (this.current() && this.reading === controller) this.reading = null;
      if (this.current() && generation === this.generation) this.publish();
    }
  };
}

export function init(api: CanvasAPI): void {
  dispose();
  const container = api.pane.querySelector<HTMLElement>('[data-react-live-panel="canvas"]');
  if (!container) return;
  view = new Controller(api, container);
  view.publish();
}
export function render(snapshot: Record<string, unknown> | null, ui: UI): void { view?.observe(snapshot?.activities, ui); }
export function dispose(): void {
  const old = view; if (!old) return; view = null;
  old.lifetime.abort(); old.reading?.abort();
  old.api.pane.hidden = true;
  flushSync(() => old.root.unmount());
}
/** Opens the pane from the menu or a slash command, even with nothing touched yet. */
export function show(): boolean { if (!view) return false; view.show(); return true; }
export const canvas = Object.freeze({init, render, dispose, show});
export default canvas;
