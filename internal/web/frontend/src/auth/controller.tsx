import {flushSync} from "react-dom";
import {createRoot} from "react-dom/client";
import type {Root} from "react-dom/client";
import {AuthPanel} from "./AuthPanel.tsx";
import {instruction, terminal, validInventory, validJob} from "./model.ts";
import type {AuthInventory, AuthJob, Identity, UI} from "./model.ts";

export interface AuthAPI {
  root: HTMLElement; identity: Identity; localOrigin: boolean;
  openDialog: (dialog: HTMLDialogElement, trigger: HTMLElement | null) => void;
  closeDialog: (dialog: HTMLDialogElement) => void;
  /** Holds the shared panel-mutation slot while a sign-in job runs. */
  reserve: (active: boolean) => unknown;
  request: (action: string, fields: Record<string, string>, signal: AbortSignal) => Promise<unknown>;
  /** Re-runs model discovery after a completed sign-in; may be absent. */
  refreshModels?: () => void;
}

const POLL_MS = 2000;

let view: Controller | null = null;

export class Controller {
  readonly api: AuthAPI;
  readonly root: Root;
  readonly lifetime = new AbortController();
  dialog: HTMLDialogElement | null = null;
  opener: HTMLElement | null = null;
  snapshot: Record<string, unknown> | null = null;
  ui: UI | null = null;
  inventory: AuthInventory | null = null;
  reading = false;
  job: AuthJob | null = null;
  starting = false;
  poll: ReturnType<typeof setTimeout> | null = null;
  confirmLogout = "";
  keyDrafts: Record<string, string> = {};
  notice = "";
  constructor(api: AuthAPI, container: Element) {
    this.api = {...api, identity: Object.freeze({...api.identity})};
    this.root = createRoot(container);
  }
  current = () => view === this;
  running = () => !!this.job && !terminal(this.job);
  /** Reads are allowed whenever the runtime is readable; writes also need the idle gate. */
  canAct = () => this.current() && !!this.ui?.safe && !this.reading && !this.starting && !this.running();
  publish = () => { if (this.current()) flushSync(() => this.root.render(<AuthPanel controller={this} />)); };
  setDialog = (dialog: HTMLDialogElement | null) => { this.dialog = dialog; };
  open = (trigger: HTMLElement | null) => {
    if (!this.current() || !this.ui?.readable || !this.dialog) return;
    if (this.dialog.open) return;
    // A finished job's code or error is stale once the panel was closed.
    if (this.job && terminal(this.job)) this.job = null;
    this.opener = trigger;
    this.api.openDialog(this.dialog, trigger);
    this.publish();
    if (!this.inventory) void this.inspect();
  };
  close = () => {
    if (!this.current() || !this.dialog) return;
    this.confirmLogout = "";
    this.api.closeDialog(this.dialog);
    this.publish();
  };
  inspect = async () => {
    if (!this.current() || !this.ui?.readable || this.reading) return;
    this.reading = true; this.notice = ""; this.publish();
    try {
      const result = await this.api.request("auth-inspect", {}, this.lifetime.signal);
      if (!this.current()) return;
      if (!validInventory(result, this.api.identity)) throw new Error("Unverified provider inventory");
      this.inventory = result;
    } catch (error) {
      if (this.current() && !this.lifetime.signal.aborted) this.notice = failure(error, "Provider status could not be read. Nothing changed.");
    } finally {
      if (this.current()) { this.reading = false; this.publish(); }
    }
  };
  setKeyDraft = (provider: string, value: string) => { if (this.current()) { this.keyDrafts[provider] = value; this.publish(); } };
  start = async (provider: string, method: string) => {
    if (!this.canAct() || !this.inventory?.providers.some(item => item.id === provider && item.methods.some(entry => entry.id === method))) return;
    const kind = this.inventory.providers.find(item => item.id === provider)?.methods.find(entry => entry.id === method)?.kind;
    const fields: Record<string, string> = {provider, method};
    if (kind === "api_key") {
      const secret = (this.keyDrafts[provider] ?? "").trim();
      if (!secret) { this.notice = "Paste an API key first. Nothing was sent."; this.publish(); return; }
      if (!this.api.localOrigin) { this.notice = "API keys can be entered only from the local URL on the Snow host."; this.publish(); return; }
      fields.secret = secret;
    }
    this.starting = true; this.notice = ""; this.confirmLogout = "";
    if (this.api.reserve(true) === false) { this.starting = false; this.notice = "Another operation owns this session. Wait for it to finish."; this.publish(); return; }
    this.publish();
    try {
      const result = await this.api.request("auth-login-start", fields, this.lifetime.signal);
      if (!this.current()) return;
      if (!validJob(result, this.api.identity)) throw new Error("Unverified sign-in receipt");
      delete this.keyDrafts[provider];
      this.job = result;
      if (terminal(result)) this.finished(); else this.schedule();
    } catch (error) {
      if (!this.current()) return;
      this.notice = failure(error, "Sign-in could not start. Nothing was retried; check the host terminal if the provider needs a different login.");
      this.api.reserve(false);
    } finally {
      if (this.current()) { this.starting = false; this.publish(); }
    }
  };
  schedule = () => {
    if (this.poll) clearTimeout(this.poll);
    this.poll = setTimeout(() => { this.poll = null; void this.refreshJob("auth-login-status"); }, POLL_MS);
  };
  cancel = async () => {
    if (!this.current() || !this.running() || this.starting) return;
    await this.refreshJob("auth-login-cancel");
  };
  refreshJob = async (action: "auth-login-status" | "auth-login-cancel") => {
    const job = this.job;
    if (!this.current() || !job || terminal(job)) return;
    try {
      const result = await this.api.request(action, {job_id: job.job_id}, this.lifetime.signal);
      if (!this.current() || this.job !== job) return;
      if (!validJob(result, this.api.identity) || result.job_id !== job.job_id) throw new Error("Unverified sign-in status");
      this.job = result;
      if (terminal(result)) this.finished(); else this.schedule();
    } catch {
      if (!this.current() || this.job !== job) return;
      // The job keeps running inside the worker; a lost poll is not a cancel.
      if (this.lifetime.signal.aborted) return;
      if (this.ui?.readable) this.schedule();
      else { this.notice = "Sign-in status is unavailable while the runtime is disconnected. Reopen this panel to check again."; this.publish(); }
    }
    if (this.current()) this.publish();
  };
  finished = () => {
    this.api.reserve(false);
    const job = this.job;
    if (job?.state === "completed") {
      this.notice = "Signed in. Model discovery has been refreshed; pick a model from the model menu if the conversation still shows a placeholder.";
      this.api.refreshModels?.();
    }
    void this.inspect();
  };
  dismissJob = () => { if (this.current() && !this.running()) { this.job = null; this.publish(); } };
  requestLogout = (provider: string) => { if (this.canAct()) { this.confirmLogout = provider; this.publish(); } };
  cancelLogout = () => { if (this.current()) { this.confirmLogout = ""; this.publish(); } };
  logout = async (provider: string) => {
    if (!this.canAct() || this.confirmLogout !== provider) return;
    this.confirmLogout = ""; this.reading = true; this.notice = ""; this.publish();
    try {
      const result = await this.api.request("auth-logout", {provider}, this.lifetime.signal);
      if (!this.current()) return;
      if (!validInventory(result, this.api.identity)) throw new Error("Unverified sign-out receipt");
      this.inventory = result;
      this.notice = `Signed out of ${provider} on the Snow host. Running work keeps its current session; new provider requests will need a sign-in.`;
    } catch (error) {
      if (this.current()) this.notice = failure(error, "Sign-out outcome is unknown. Refresh status before trying again.");
    } finally {
      if (this.current()) { this.reading = false; this.publish(); }
    }
  };
  get latestInstruction() { return instruction(this.job); }
}

function failure(error: unknown, fallback: string): string {
  const status = typeof error === "object" && error !== null && "status" in error ? (error as {status?: number}).status : undefined;
  if (status === 403) return "API keys can be entered only from the local URL on the Snow host, or with snow login in a terminal.";
  if (status === 409) return "The session is busy or changed. Nothing was sent; refresh status and try again.";
  return fallback;
}

export function init(api: AuthAPI): void {
  dispose();
  const container = api.root.querySelector<HTMLElement>('[data-react-live-panel="auth"]');
  if (!container) return;
  view = new Controller(api, container);
  view.publish();
}
export function render(snapshot: Record<string, unknown> | null, ui: UI): void {
  const current = view; if (!current) return;
  if (snapshot && ["project_id", "instance_id", "session_id"].some(key => snapshot[key] !== current.api.identity[key as keyof Identity])) { dispose(); return; }
  current.snapshot = snapshot; current.ui = ui;
  current.publish();
}
export function dispose(): void {
  const old = view; if (!old) return; view = null;
  old.lifetime.abort();
  if (old.poll) clearTimeout(old.poll);
  old.api.reserve(false);
  if (old.dialog) old.api.closeDialog(old.dialog);
  flushSync(() => old.root.unmount());
}
/** Opens the dialog from an external trigger such as the error banner or a slash command. */
export function show(trigger: HTMLElement | null = null): boolean {
  if (!view?.ui?.readable) return false;
  view.open(trigger); return true;
}
export const auth = Object.freeze({init, render, dispose, show, validInventory, validJob});
export default auth;
