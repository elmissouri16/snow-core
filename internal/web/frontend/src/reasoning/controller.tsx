import {createRoot} from "react-dom/client";
import type {KeyboardEvent} from "react";
import type {Root} from "react-dom/client";
import {flushSync} from "react-dom";
import {ReasoningPanel} from "./ReasoningPanel";
import {facts, fields, identityKeys, options, sameScope, valid} from "./model";
import type {API, Field, Inspection, Snapshot, UI} from "./model";
export {valid} from "./model";
export type {API, Identity, Inspection, Snapshot, UI} from "./model";

let view: Controller | null = null;

/** Serial legacy admission state, independent of React scheduling. */
export class Controller {
  readonly api: API;
  readonly root: Root;
  readonly launcher: HTMLElement;
  dialog: HTMLDialogElement | null = null;
  picker: HTMLDivElement | null = null;
  opener: HTMLButtonElement | null = null;
  pickerOpen = false;
  pickerLifetime: AbortController | null = null;
  snapshot: Snapshot | null = null;
  ui: UI | null = null;
  inspected: Inspection | null = null;
  displayed: Inspection | null = null;
  read: AbortController | null = null;
  field: Field | "" = "";
  value = "";
  committing = false;
  uncertain = false;
  composing = false;
  error = "";
  constructor(api: API, container: Element, launcher: HTMLElement) {
    this.launcher = launcher;
    this.api = {...api, identity: Object.freeze({...api.identity})};
    this.root = createRoot(container);
  }
  current = () => view === this;
  ready = () => this.current() && !!this.ui?.safe && sameScope(this.inspected, this.snapshot) && !this.read && !this.committing && !this.uncertain && !this.composing;
  publish = () => {
    if (this.current()) flushSync(() => this.root.render(<ReasoningPanel controller={this} />));
  };
  setDialog = (dialog: HTMLDialogElement | null) => { this.dialog = dialog; };
  setPicker = (picker: HTMLDivElement | null) => {
    this.picker?.removeEventListener("toggle", this.pickerToggled);
    this.picker = picker;
    picker?.addEventListener("toggle", this.pickerToggled);
  };
  pickerToggled = () => {
    if (!this.current() || !this.picker) return;
    this.pickerOpen = this.picker.matches(":popover-open");
    if (!this.pickerOpen) {
      this.pickerLifetime?.abort(); this.pickerLifetime = null;
      // Closing presentation never aborts an already-admitted write.
      if (!this.dialog?.open) this.cancel();
    }
    this.publish();
  };
  positionPicker = () => {
    if (!this.picker || !this.opener) return;
    const rect = this.opener.getBoundingClientRect();
    const width = Math.min(288, window.innerWidth - 16);
    // Header delegation can open this picker while its composer trigger is
    // scrolled out of view. Keep that same anchor identity, but clamp placement.
    const height = window.innerHeight;
    const top = Math.max(8, Math.min(rect.top, height - 8));
    const bottom = Math.max(8, Math.min(rect.bottom, height - 8));
    const above = top > height - bottom;
    Object.assign(this.picker.style, {
      width: `${width}px`, left: `${Math.max(8, Math.min(rect.left, window.innerWidth - width - 8))}px`,
      top: above ? "auto" : `${bottom + 6}px`,
      bottom: above ? `${height - top + 6}px` : "auto",
      maxHeight: `${Math.max(0, Math.min(height - 16, (above ? top : height - bottom) - 14))}px`,
    });
  };
  hidePicker = (restoreFocus = false) => {
    this.pickerLifetime?.abort(); this.pickerLifetime = null;
    if (this.picker?.matches(":popover-open")) this.picker.hidePopover();
    this.pickerOpen = false;
    if (restoreFocus) this.opener?.focus();
  };
  pickerKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); this.hidePicker(true); this.cancel(); return; }
    if (e.key === "Tab") { this.hidePicker(); this.cancel(); return; }
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(e.key)) return;
    e.preventDefault();
    const items = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')];
    const index = items.indexOf(document.activeElement as HTMLButtonElement);
    const next = e.key === "Home" ? 0 : e.key === "End" ? items.length - 1 : (index + (e.key === "ArrowUp" ? -1 : 1) + items.length) % items.length;
    items[next]?.focus();
  };
  choose = (value: string) => {
    if (!this.ready() || !this.pickerOpen || !this.inspected?.thinking_levels?.includes(value)) return;
    if (value === this.inspected.thinking) { this.hidePicker(true); this.cancel(); return; }
    this.field = "thinking"; this.value = value; void this.commit();
  };
  openAdvanced = () => {
    if (!this.current() || this.committing || this.uncertain || !this.dialog || !this.opener) return;
    this.hidePicker();
    if (this.inspected) this.show(this.inspected);
    this.api.openDialog(this.dialog, this.opener);
    this.publish();
  };
  fillValues() {
    const choices = this.field ? this.inspected?.[options[this.field]] || [] : [];
    const value = this.field ? this.inspected?.[this.field] : undefined;
    this.value = value && choices.includes(value) ? value : choices[0] || "";
  }
  show(result: Inspection) {
    this.displayed = result;
    this.field = (Object.keys(fields) as Field[]).find(k => k !== "thinking" && result[options[k]]?.length) || "";
    this.fillValues();
  }
  selectField = (field: string) => {
    if (!this.current() || !(field in fields)) return;
    this.field = field as Field; this.fillValues(); this.publish();
  };
  selectValue = (value: string) => { if (this.current()) { this.value = value; this.publish(); } };
  composition = (active: boolean) => { if (this.current()) { this.composing = active; this.publish(); } };
  open = (button: HTMLButtonElement) => {
    if (!this.current() || !this.ui?.readable || this.committing || this.uncertain || !this.picker) return;
    if (this.picker.matches(":popover-open")) { this.hidePicker(true); this.cancel(); return; }
    this.opener = button; this.pickerOpen = true;
    this.positionPicker(); this.picker.showPopover(); this.picker.focus();
    this.pickerLifetime?.abort(); this.pickerLifetime = new AbortController();
    const signal = this.pickerLifetime.signal;
    window.addEventListener("resize", this.positionPicker, {signal});
    document.addEventListener("scroll", this.positionPicker, {capture: true, signal});
    void this.inspect();
  };
  inspect = async () => {
    if (!this.current() || !this.ui?.readable || this.read || this.committing || this.uncertain) return;
    const read = new AbortController(); this.read = read; this.error = ""; this.inspected = null; this.publish();
    try {
      const result = await this.api.inspect(read.signal);
      if (!this.current() || this.read !== read || read.signal.aborted || !(this.dialog?.open || this.picker?.matches(":popover-open"))) return;
      if (!valid(result, this.api.identity) || (this.snapshot?.revision || 0) > result.revision) throw new Error("Unverified scope");
      this.inspected = result; this.show(result);
    } catch {
      if (this.current() && !read.signal.aborted) this.error = "Inspection failed or changed scope. Nothing was updated. Close and reopen Thinking to inspect again.";
    } finally {
      if (this.current() && this.read === read) {
        this.read = null; this.publish();
        if (this.pickerOpen && document.activeElement === this.picker) {
          (this.picker?.querySelector<HTMLButtonElement>('[aria-checked="true"]:not(:disabled)') || this.picker?.querySelector<HTMLButtonElement>('button:not(:disabled)'))?.focus();
        }
      }
    }
  };
  cancel = () => {
    if (!this.current() || this.committing) return;
    this.read?.abort(); this.read = null;
    this.api.reserve(false); this.publish();
  };
  close = () => {
    if (!this.current() || this.committing) return;
    this.cancel(); if (this.current() && this.dialog) this.api.closeDialog(this.dialog);
  };
  commit = async () => {
    if (!this.ready() || !(this.dialog?.open || this.picker?.matches(":popover-open")) || !this.field || !this.inspected) return;
    const {field, value, inspected: expected} = this;
    if (!Object.hasOwn(fields, field) || !expected.current_session_available || !expected[options[field]]?.includes(value) || expected[field] === value) return;
    const payload: Record<string, string> = {scope: "session", field, value, confirm: "session", expected_revision: String(expected.revision)};
    for (const key of facts) if (key !== "project_id" && key !== "instance_id") payload[key] = expected[key];
    this.committing = true;
    if (this.api.reserve(true) === false) { this.committing = false; this.publish(); return; }
    if (!this.current()) return;
    if (!this.ui?.safe || !(this.dialog?.open || this.picker?.matches(":popover-open")) || this.inspected !== expected || !sameScope(expected, this.snapshot)) {
      this.committing = false; this.api.reserve(false); this.publish(); return;
    }
    this.publish();
    let restorePickerFocus = false;
    try {
      const result = await this.api.set(payload);
      if (!this.current()) return;
      if (!valid(result, this.api.identity) || result.revision <= expected.revision || facts.some(k => result[k] !== (k === field ? value : expected[k]))) throw new Error("Unverified update");
      this.inspected = result; this.show(result);
      if (field === "thinking") { restorePickerFocus = this.pickerOpen; this.hidePicker(); }
      this.error = "The worker confirmed the session-only update and its effective current values. No prompt was sent. Refresh before another change.";
    } catch {
      if (!this.current()) return;
      this.uncertain = true; this.inspected = null; this.displayed = null;
      this.error = "The update outcome is unverified. Do not retry: the runtime preference may have changed. Close this panel and explicitly review runtime recovery. Nothing will be activated or retried automatically.";
    } finally {
      if (this.current()) {
        this.committing = false; this.inspected = null; this.api.reserve(false); this.publish();
        if (restorePickerFocus && !this.uncertain) this.opener?.focus();
      }
    }
  };
}
export function init(api: API): void {
  dispose();
  const container = api.root.querySelector('[data-react-live-panel="reasoning"]');
  const launcher = api.root.querySelector<HTMLElement>('[data-reasoning-launcher]');
  if (!container || !launcher) return;
  view = new Controller(api, container, launcher); view.publish();
}
export function render(snapshot: Snapshot, ui: UI): void {
  const current = view; if (!current) return;
  if (identityKeys.some(k => snapshot[k] !== current.api.identity[k])) { dispose(); return; }
  current.snapshot = snapshot; current.ui = ui;
  if (current.inspected && snapshot.revision > current.inspected.revision && !current.committing) { current.inspected = null; current.cancel(); }
  current.publish();
}
export function dispose(): void {
  const old = view; if (!old) return; view = null;
  old.read?.abort(); old.hidePicker(); old.api.reserve(false);
  if (old.dialog) old.api.closeDialog(old.dialog);
  flushSync(() => old.root.unmount());
}
export const reasoning = Object.freeze({init, render, dispose, valid});
export default reasoning;
