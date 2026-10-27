import {flushSync} from "react-dom";
import {createRoot} from "react-dom/client";
import type {Root} from "react-dom/client";
import {SubagentsPanel} from "./SubagentsPanel.tsx";
import {active, validInventory} from "./model.ts";
import type {Identity, SubagentInventory, UI} from "./model.ts";

export interface SubagentsAPI {
  root: HTMLElement; identity: Identity;
  openDialog: (dialog: HTMLDialogElement, trigger: HTMLElement | null) => void;
  closeDialog: (dialog: HTMLDialogElement) => void;
  inspect: (signal: AbortSignal) => Promise<unknown>;
}

const REFRESH_MS = 3000;

let view: Controller | null = null;

export class Controller {
  readonly api: SubagentsAPI;
  readonly root: Root;
  readonly lifetime = new AbortController();
  dialog: HTMLDialogElement | null = null;
  ui: UI | null = null;
  inventory: SubagentInventory | null = null;
  reading = false;
  notice = "";
  timer: ReturnType<typeof setTimeout> | null = null;
  constructor(api: SubagentsAPI, container: Element) {
    this.api = {...api, identity: Object.freeze({...api.identity})};
    this.root = createRoot(container);
  }
  current = () => view === this;
  publish = () => { if (this.current()) flushSync(() => this.root.render(<SubagentsPanel controller={this} />)); };
  setDialog = (dialog: HTMLDialogElement | null) => { this.dialog = dialog; };
  open = (trigger: HTMLElement | null) => {
    if (!this.current() || !this.ui?.readable || !this.dialog || this.dialog.open) return;
    this.api.openDialog(this.dialog, trigger);
    this.publish();
    void this.inspect();
  };
  close = () => {
    if (!this.current() || !this.dialog) return;
    if (this.timer) { clearTimeout(this.timer); this.timer = null; }
    this.api.closeDialog(this.dialog); this.publish();
  };
  inspect = async () => {
    if (!this.current() || !this.ui?.readable || this.reading) return;
    this.reading = true; this.notice = ""; this.publish();
    try {
      const result = await this.api.inspect(this.lifetime.signal);
      if (!this.current()) return;
      if (!validInventory(result, this.api.identity)) throw new Error("Unverified subagent inventory");
      this.inventory = result;
    } catch {
      if (this.current() && !this.lifetime.signal.aborted) this.notice = "Subagent status could not be read. Refresh to try again.";
    } finally {
      if (this.current()) {
        this.reading = false; this.publish();
        // Keep a live fleet fresh while the dialog is open; idle fleets stay put.
        if (this.dialog?.open && this.inventory?.agents.some(agent => active(agent.status))) {
          if (this.timer) clearTimeout(this.timer);
          this.timer = setTimeout(() => { this.timer = null; void this.inspect(); }, REFRESH_MS);
        }
      }
    }
  };
}

export function init(api: SubagentsAPI): void {
  dispose();
  const container = api.root.querySelector<HTMLElement>('[data-react-live-panel="subagents"]');
  if (!container) return;
  view = new Controller(api, container);
  view.publish();
}
export function render(snapshot: Record<string, unknown> | null, ui: UI): void {
  const current = view; if (!current) return;
  if (snapshot && ["project_id", "instance_id", "session_id"].some(key => snapshot[key] !== current.api.identity[key as keyof Identity])) { dispose(); return; }
  current.ui = ui;
  current.publish();
}
export function dispose(): void {
  const old = view; if (!old) return; view = null;
  old.lifetime.abort();
  if (old.timer) clearTimeout(old.timer);
  if (old.dialog) old.api.closeDialog(old.dialog);
  flushSync(() => old.root.unmount());
}
export const subagents = Object.freeze({init, render, dispose, validInventory});
export default subagents;
