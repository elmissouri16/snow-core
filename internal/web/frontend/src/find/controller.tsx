import {flushSync} from 'react-dom';
import {createRoot} from 'react-dom/client';
import type {Root} from 'react-dom/client';
import {FindPanel} from './FindPanel.tsx';
import {MATCH_LIMIT, findMatches, step} from './model.ts';
import type {Match} from './model.ts';

type Highlights = {set(name: string, highlight: unknown): void; delete(name: string): void};
const highlights = (): Highlights | null => {
  const css = (globalThis as {CSS?: {highlights?: Highlights}}).CSS;
  return css?.highlights && 'Highlight' in globalThis ? css.highlights : null;
};
const makeHighlight = (ranges: Range[]) => new (globalThis as unknown as {Highlight: new (...ranges: Range[]) => unknown}).Highlight(...ranges);

let view: Controller | null = null;

export class Controller {
  readonly root: Root;
  readonly region: HTMLElement;
  readonly lifetime = new AbortController();
  open = false;
  query = '';
  matches: Match[] = [];
  index = 0;
  scheduled = 0;
  constructor(region: HTMLElement, container: Element) {
    this.region = region;
    this.root = createRoot(container);
  }
  current = () => view === this;
  publish = () => { if (this.current()) flushSync(() => this.root.render(<FindPanel controller={this} />)); };
  show = () => { if (!this.current()) return; this.open = true; this.publish(); this.region.querySelector<HTMLInputElement>('[data-find-input]')?.focus(); };
  close = () => { if (!this.current()) return; this.open = false; this.query = ''; this.matches = []; this.index = 0; this.paint(); this.publish(); this.region.querySelector<HTMLTextAreaElement>('#live-prompt')?.focus({preventScroll: true}); };
  setQuery = (query: string) => { if (!this.current()) return; this.query = query.slice(0, 200); this.index = 0; this.search(); };
  search = () => {
    const transcript = this.region.querySelector('#live-transcript') || this.region;
    const previous = this.matches[this.index];
    this.matches = findMatches(transcript, this.query);
    if (previous) { const same = this.matches.findIndex(m => m.node === previous.node && m.start === previous.start); if (same >= 0) this.index = same; }
    if (this.index >= this.matches.length) this.index = 0;
    this.paint(); this.publish();
  };
  move = (delta: number) => { if (!this.current() || !this.matches.length) return; this.index = step(this.index, this.matches.length, delta); this.paint(); this.publish(); this.reveal(); };
  reveal = () => {
    const match = this.matches[this.index];
    match?.node.parentElement?.scrollIntoView({block: 'center', behavior: matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'});
  };
  paint = () => {
    const api = highlights(); if (!api) return;
    if (!this.open || !this.matches.length) { api.delete('snow-find'); api.delete('snow-find-current'); return; }
    const range = (m: Match) => { const r = new Range(); r.setStart(m.node, m.start); r.setEnd(m.node, m.end); return r; };
    api.set('snow-find', makeHighlight(this.matches.map(range)));
    api.set('snow-find-current', makeHighlight([range(this.matches[this.index])]));
  };
  /** Re-run the search when the transcript changes while the bar is open. */
  refresh = () => {
    if (!this.current() || !this.open || !this.query) return;
    cancelAnimationFrame(this.scheduled);
    this.scheduled = requestAnimationFrame(() => { if (this.current() && this.open) this.search(); });
  };
  get limited() { return this.matches.length >= MATCH_LIMIT; }
}

export function init(region: HTMLElement): void {
  dispose();
  const container = region.querySelector<HTMLElement>('[data-react-live-panel="find"]');
  if (!container) return;
  view = new Controller(region, container);
  view.publish();
}
export function render(): void { view?.refresh(); }
export function dispose(): void {
  const old = view; if (!old) return; view = null;
  cancelAnimationFrame(old.scheduled);
  old.open = false; old.matches = []; old.paint();
  old.lifetime.abort();
  flushSync(() => old.root.unmount());
}
/** Opens the bar from a menu row, a slash command, or a keyboard shortcut. */
export function show(): boolean { if (!view) return false; view.show(); return true; }
export const find = Object.freeze({init, render, dispose, show});
export default find;
