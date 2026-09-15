import {useLayoutEffect, useRef, useState} from "react";
import type {RefObject} from "react";
import {boundedJSON, REQUEST_TIMEOUT_MS, validateInventory, validateReceipt} from "./model.ts";
import type {PairedBrowser} from "./model.ts";

interface View {
  browsers: PairedBrowser[];
  selected: PairedBrowser | null;
  busy: boolean;
  ready: boolean;
  accessEnded: boolean;
  status: string;
  focus: "cancel" | "refresh" | null;
}
const initial: View = {browsers: [], selected: null, busy: false, ready: false, accessEnded: false,
  status: "Loading paired browsers…", focus: null};
const unknownOutcome = "Could not confirm revocation. Access storage may be unavailable. Refresh before trying again; this request will not be replayed.";
const accessEndedEvent = "snow:browser-access-ended";
interface Actions {refresh(): void; select(browser: PairedBrowser): void; cancel(): void; revoke(): void}
const noopActions: Actions = {refresh() {}, select() {}, cancel() {}, revoke() {}};

export function useBrowserInventory(root: RefObject<HTMLElement | null>, csrf: string) {
  const [view, setView] = useState<View>(initial);
  const actions = useRef<Actions>(noopActions);
  useLayoutEffect(() => {
    let disposed = false, ended = false, paused = false, version = 0;
    let selected: PairedBrowser | null = null;
    let browsers: PairedBrowser[] = [], ready = false, mutating = false;
    let request: AbortController | null = null;
    let deadline: ReturnType<typeof setTimeout> | undefined;
    let navigationTarget: Element | null = null;
    const connected = () => !disposed && !ended && !paused && !!root.current?.isConnected;
    const active = (epoch: number) => connected() && version === epoch;
    const abort = () => {
      ++version;
      const old = request;
      request = null;
      clearTimeout(deadline);
      old?.abort();
    };
    const clearSelection = () => { selected = null; };
    const endAccess = (message = "Browser access ended. Pair this browser again; host work was not stopped.") => {
      if (disposed || ended) return;
      ended = true;
      abort();
      clearSelection();
      browsers = [];
      ready = false;
      setView({...initial, accessEnded: true, status: message});
    };
    const login = (self = false) => {
      endAccess(self ? "This browser has been revoked. Opening the pairing page…" : undefined);
      document.dispatchEvent(new Event(accessEndedEvent));
      window.location.assign("/login");
    };
    const run = async (target: PairedBrowser | null) => {
      if (!connected() || request || (target && (!ready || selected !== target))) return;
      // Consume confirmation synchronously. React's later render is not the double-click guard.
      clearSelection();
      const epoch = ++version;
      const controller = new AbortController();
      request = controller;
      ready = false;
      setView(previous => ({...previous, selected: null, busy: true, ready: false, focus: null,
        status: target ? "Revoking selected browser…" : "Loading paired browsers…"}));
      deadline = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
      let response: Response | undefined;
      try {
        response = await fetch(target ? `/access/browsers/${encodeURIComponent(target.id)}/revoke` : "/access/browsers", {
          method: target ? "POST" : "GET", credentials: "same-origin", cache: "no-store", redirect: "error", signal: controller.signal,
          headers: target ? {Accept: "application/json", "HX-Request": "true", "Content-Type": "application/x-www-form-urlencoded"} : {Accept: "application/json"},
          ...(target ? {body: new URLSearchParams({csrf, confirm: "revoke"})} : {}),
        });
        if (!active(epoch)) return;
        controller.signal.throwIfAborted();
        if (response.status === 401) { login(); return; }
        if (target && response.status === 404) {
          setView(previous => ({...previous, status: "That browser is no longer paired. Refresh the inventory before choosing another browser."}));
          return;
        }
        if (!response.ok) throw Error("request");
        const data = await boundedJSON(response, controller.signal);
        if (!active(epoch)) return;
        controller.signal.throwIfAborted();
        if (target) {
          const receipt = validateReceipt(data, target.id);
          if (receipt.signed_out) { login(true); return; }
          browsers = browsers.filter(browser => browser.id !== target.id);
          ready = true;
          setView(previous => ({...previous, browsers, ready,
            status: "Browser revoked. Other browsers remain paired. Refresh to see the updated inventory."}));
        } else {
          browsers = validateInventory(data);
          ready = true;
          setView(previous => ({...previous, browsers, ready, status: `${browsers.length} of 8 browser slots used.`}));
        }
      } catch (_) {
        if (!active(epoch)) return;
        if (!target) browsers = [];
        setView(previous => ({...previous, browsers, selected: null, ready: false,
          status: target ? unknownOutcome : "Unable to load browser access. Refresh to retry; no access was changed."}));
      } finally {
        // Responses rejected before stream validation still need their bodies canceled.
        void response?.body?.cancel().catch(() => {});
        if (active(epoch)) {
          clearTimeout(deadline);
          request = null;
          setView(previous => ({...previous, busy: false}));
        }
      }
    };
    const interrupt = () => {
      const unknownMutation = !!request && mutating;
      abort();
      clearSelection();
      ready = false;
      setView(previous => ({...previous, selected: null, busy: false, ready: false, focus: null,
        status: unknownMutation ? unknownOutcome : "Browser inventory paused. Refresh before choosing a browser; no request will be replayed."}));
    };
    actions.current = {
      refresh() { if (connected() && !request) { mutating = false; void run(null); } },
      select(browser) {
        if (!connected() || request || !ready || !browsers.includes(browser)) return;
        selected = browser;
        setView(previous => ({...previous, selected: browser, focus: "cancel"}));
      },
      cancel() {
        if (!connected() || request) return;
        abort();
        clearSelection();
        setView(previous => ({...previous, selected: null, focus: "refresh"}));
      },
      revoke() { if (selected && connected() && !request) { mutating = true; void run(selected); } },
    };
    const accessEnded = () => endAccess();
    const submit = (event: Event) => {
      if (!(event.target instanceof HTMLFormElement)) return;
      const url = new URL(event.target.action, window.location.href);
      if (url.origin === window.location.origin && ["/logout", "/access/revoke-all"].includes(url.pathname)) endAccess();
    };
    type NavigationDetail = {target?: unknown; requestConfig?: {path?: string}};
    const beforeNavigation = (event: Event) => {
      const detail = (event as CustomEvent<NavigationDetail>).detail || {};
      if (["/logout", "/access/revoke-all"].includes(detail.requestConfig?.path || "")) { endAccess(); return; }
      if (detail.target instanceof Element && root.current && detail.target.contains(root.current)) {
        navigationTarget = detail.target;
        paused = true;
        interrupt();
      }
    };
    const afterNavigation = (event: Event) => {
      const target = (event as CustomEvent<NavigationDetail>).detail?.target;
      if (navigationTarget && (!(target instanceof Element) || target === navigationTarget)) {
        navigationTarget = null;
        paused = false; // Only an explicit Refresh may resume a canceled navigation.
      }
    };
    const pagehide = () => { paused = true; interrupt(); };
    const pageshow = () => { paused = false; };
    const observer = new MutationObserver(() => {
      if (!disposed && !root.current?.isConnected) { interrupt(); disposed = true; }
    });
    observer.observe(document.documentElement, {childList: true, subtree: true});
    document.addEventListener("submit", submit, true);
    document.addEventListener(accessEndedEvent, accessEnded);
    document.addEventListener("htmx:beforeRequest", beforeNavigation);
    document.addEventListener("htmx:beforeSwap", beforeNavigation);
    document.addEventListener("htmx:afterRequest", afterNavigation);
    document.addEventListener("htmx:afterSettle", afterNavigation);
    window.addEventListener("pagehide", pagehide);
    window.addEventListener("pageshow", pageshow);
    setView(initial);
    void run(null); // Existing initial read per root; mutations only run from explicit clicks.
    return () => {
      disposed = true;
      abort();
      actions.current = noopActions;
      observer.disconnect();
      document.removeEventListener("submit", submit, true);
      document.removeEventListener(accessEndedEvent, accessEnded);
      document.removeEventListener("htmx:beforeRequest", beforeNavigation);
      document.removeEventListener("htmx:beforeSwap", beforeNavigation);
      document.removeEventListener("htmx:afterRequest", afterNavigation);
      document.removeEventListener("htmx:afterSettle", afterNavigation);
      window.removeEventListener("pagehide", pagehide);
      window.removeEventListener("pageshow", pageshow);
    };
  }, [root, csrf]);
  return {...view, refresh: () => actions.current.refresh(), select: (browser: PairedBrowser) => actions.current.select(browser),
    cancel: () => actions.current.cancel(), revoke: () => actions.current.revoke()};
}
