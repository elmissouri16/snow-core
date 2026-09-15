import {useEffect, useRef, useState} from "react";
import type {RefObject} from "react";
import {ActivityAccessEnded, fetchActivity, POLL_INTERVAL_MS, REQUEST_TIMEOUT_MS} from "./summary.ts";
import type {ActivitySummary} from "./summary.ts";

export type Freshness = "loading" | "fresh" | "paused" | "error";
interface ActivityView {
  summary: ActivitySummary | null;
  freshness: Freshness;
  message: string;
  busy: boolean;
  accessEnded: boolean;
}
const loadingMessage = "Connecting this browser to the activity summary…";
const pausedMessage = "Summary paused. Displayed host state may be stale.";
const initialView: ActivityView = {
  summary: null, freshness: "loading", message: loadingMessage, busy: false, accessEnded: false,
};

/** One effect owns all reads and timers. Cleanup invalidates both headers and late body reads. */
export function useActivity(root: RefObject<HTMLElement | null>, registryEnabled: boolean) {
  const [view, setView] = useState<ActivityView>(initialView);
  const refreshRef = useRef<() => void>(() => {});
  useEffect(() => {
    let disposed = false, active = false, accessEnded = false, pageAway = false;
    let navigationTarget: Element | null = null;
    let request: AbortController | null = null;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let deadline: ReturnType<typeof setTimeout> | undefined;
    const visible = () => registryEnabled && !pageAway && !navigationTarget && document.visibilityState !== "hidden" &&
      !!root.current?.isConnected && root.current.closest<HTMLElement>("#workspace")?.dataset.view === "activity" &&
      root.current.getClientRects().length > 0;
    const current = () => !disposed && !accessEnded && active && visible();
    const abort = () => {
      clearTimeout(timer);
      clearTimeout(deadline);
      // Retire ownership before abort callbacks or body completion can run.
      const old = request;
      request = null;
      old?.abort();
    };
    const endAccess = () => {
      accessEnded = true;
      active = false;
      abort();
      setView({summary: null, freshness: "error", busy: false, accessEnded: true,
        message: "Browser access ended. Pair this browser again; host work was not stopped."});
    };
    const poll = async () => {
      if (!current() || request) return;
      clearTimeout(timer);
      const controller = new AbortController();
      request = controller;
      setView(previous => ({...previous, busy: true}));
      deadline = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
      try {
        const summary = await fetchActivity(controller.signal);
        if (!current() || request !== controller || controller.signal.aborted) return;
        setView({summary, freshness: "fresh", busy: false, accessEnded: false,
          message: "Browser connected · summary refreshed. Host work is independent of this connection."});
      } catch (error) {
        if (!current() || request !== controller) return;
        if (error instanceof ActivityAccessEnded) { endAccess(); return; }
        setView(previous => ({...previous, freshness: "error", busy: false,
          message: "Browser could not refresh activity. Displayed host state may be stale; no work was replayed."}));
      } finally {
        if (!disposed && request === controller) {
          clearTimeout(deadline);
          request = null;
          if (current()) timer = setTimeout(() => { void poll(); }, POLL_INTERVAL_MS);
        }
      }
    };
    const syncVisibility = () => {
      if (disposed || accessEnded) return;
      if (!visible()) {
        if (active) {
          active = false;
          abort();
          setView(previous => ({...previous, freshness: "paused", message: pausedMessage, busy: false}));
        }
        return;
      }
      if (active) return;
      active = true;
      // A resumed view must not present a previous visibility epoch as current data.
      setView({...initialView});
      void poll();
    };
    refreshRef.current = () => { if (current() && !request) void poll(); };
    const pagehide = () => { pageAway = true; syncVisibility(); };
    const pageshow = () => { pageAway = false; syncVisibility(); };
    const submit = (event: Event) => {
      if (!(event.target instanceof HTMLFormElement)) return;
      const action = new URL(event.target.action, window.location.href);
      if (action.origin === window.location.origin && ["/logout", "/access/revoke-all"].includes(action.pathname)) endAccess();
    };
    type NavigationDetail = {target?: unknown; requestConfig?: {path?: string}};
    const detailOf = (event: Event): NavigationDetail => (event as CustomEvent<NavigationDetail>).detail || {};
    const beforeNavigation = (event: Event) => {
      const detail = detailOf(event);
      if (["/logout", "/access/revoke-all"].includes(detail.requestConfig?.path || "")) {
        endAccess();
        return;
      }
      // Synchronous fencing starts with request admission, not with a later React
      // cleanup. Never treat an HTMX target inside this island as its owner.
      if (detail.target instanceof Element && root.current && detail.target.contains(root.current)) {
        navigationTarget = detail.target;
        syncVisibility();
      }
    };
    const afterNavigation = (event: Event) => {
      const target = detailOf(event).target;
      if (!navigationTarget || (target instanceof Element && target !== navigationTarget)) return;
      navigationTarget = null;
      syncVisibility();
    };
    document.addEventListener("htmx:beforeRequest", beforeNavigation);
    document.addEventListener("htmx:beforeSwap", beforeNavigation);
    document.addEventListener("htmx:afterRequest", afterNavigation);
    document.addEventListener("htmx:afterSettle", afterNavigation);
    document.addEventListener("visibilitychange", syncVisibility);
    document.addEventListener("submit", submit, true);
    window.addEventListener("pagehide", pagehide);
    window.addEventListener("pageshow", pageshow);
    // Observe visibility/attachment only. React remains the sole owner of descendant DOM.
    const observer = new MutationObserver(syncVisibility);
    observer.observe(document.documentElement, {childList: true, subtree: true, attributes: true, attributeFilter: ["data-view", "hidden"]});
    setView(registryEnabled ? {...initialView} : {...initialView, freshness: "error", message: "The project registry is unavailable. Activity cannot be loaded."});
    syncVisibility();
    return () => {
      disposed = true;
      active = false;
      abort();
      refreshRef.current = () => {};
      observer.disconnect();
      document.removeEventListener("htmx:beforeRequest", beforeNavigation);
      document.removeEventListener("htmx:beforeSwap", beforeNavigation);
      document.removeEventListener("htmx:afterRequest", afterNavigation);
      document.removeEventListener("htmx:afterSettle", afterNavigation);
      document.removeEventListener("visibilitychange", syncVisibility);
      document.removeEventListener("submit", submit, true);
      window.removeEventListener("pagehide", pagehide);
      window.removeEventListener("pageshow", pageshow);
    };
  }, [root, registryEnabled]);
  return {...view, refresh: () => refreshRef.current()};
}
