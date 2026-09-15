/* Browser-local conversation width, adapted from Harness ConversationRoot (MIT).
 * See HARNESS-NOTICE.txt. No runtime, provider, or session authority.
 * Geometry and native input algorithm retained from conversation-width.js.
 */
import {mountHandles, type HandleView, type Side} from './Handles';
import type {ReaderController} from '../reader';

type Drag = {handle: HTMLDivElement; pointer: number; base: number; origin: number; direction: number; width: number};
const scrollOwner = () => (window as Window & {SnowScroll?: Pick<ReaderController, 'beforeUpdate' | 'afterUpdate' | 'wheelFromHandle'>}).SnowScroll;

const key = "snow-manager-chat-width", minimum = 640, edgeBudget = 176;
function readPreference() {
  try {
    const raw = localStorage.getItem(key);
    if (raw === null || raw.length > 64 || !raw.trim()) return null;
    const value = Number(raw);
    return Number.isFinite(value) && value > 0 ? value : null;
  } catch (_) { return null; }
}
export function mount(region: HTMLElement) {
  const column = region.closest<HTMLElement>(".conversation-pane") || region;
  const streamNode = region.querySelector<HTMLElement>("#live-stream"), transcriptNode = region.querySelector<HTMLElement>("#live-transcript");
  const controlsHost = region.querySelector<HTMLElement>("[data-react-width-controls]");
  if (!streamNode || !transcriptNode || !controlsHost) return null;
  const stream = streamNode, transcript = transcriptNode;
  const listeners = new AbortController(), options = {signal: listeners.signal};
  const controls = mountHandles(controlsHost), handles = controls.handles;
  let preference = readPreference(), drag: Drag | null = null, frame = 0, disposed = false;
  let observer: ResizeObserver | undefined;
  const columnWidth = () => column.getBoundingClientRect().width;
  const maximum = () => Math.max(minimum, columnWidth() - edgeBudget);
  const clamp = (value: number) => Math.max(minimum, Math.min(value, maximum()));
  const resolved = () => preference === null ? Math.max(680, Math.min(columnWidth() * .64, 920)) : clamp(preference);
  function save(value: number | null) {
    preference = value;
    try { if (value === null) localStorage.removeItem(key); else localStorage.setItem(key, String(value)); }
    catch (_) { /* Storage can be disabled; keep the working tab preference. */ }
  }
  function positions() {
    const view = stream.getBoundingClientRect(), content = transcript.getBoundingClientRect(), host = column.getBoundingClientRect();
    const header = region.querySelector(".live-header")?.getBoundingClientRect();
    const composer = region.querySelector("#live-composer-seat")?.getBoundingClientRect();
    const top = Math.max(0, view.top, header?.bottom || 0);
    const bottom = Math.min(innerHeight, view.bottom, composer?.top ?? innerHeight);
    const views: HandleView[] = [];
    for (const handle of handles) {
      const left = handle.dataset.chatWidthHandle === "left";
      const available = left ? content.left - host.left - 48 : host.right - content.right - 48;
      const width = Math.max(0, Math.min(40, available));
      const hidden = width < 1 || bottom <= top || !stream.getClientRects().length;
      // Fixed children do not extend the scroll range. Their wheel bridge
      // uses the existing scroll owner. Measured bounds keep them below
      // chrome and above the composer; narrow columns have no hit strip.
      handle.style.left = `${left ? content.left - 24 - width : content.right + 24}px`;
      handle.style.top = `${top}px`; handle.style.width = `${width}px`;
      handle.style.height = `${Math.max(0, bottom - top)}px`;
      const value = Math.round(drag ? drag.width : resolved());
      views.push({hidden, minimum, maximum: Math.round(Math.max(maximum(), resolved())), value,
        text: `${value} pixels; ${preference === null && !drag ? "automatic" : "custom"} width`});
    }
    controls.render(views);
  }
  function paint() {
    if (disposed || !region.isConnected) return;
    const width = columnWidth();
    if (!Number.isFinite(width) || width <= 0) return;
    const measured = `${width}px`, content = drag ? `${clamp(drag.width)}px` : preference === null ? "" : `${resolved()}px`;
    if (column.style.getPropertyValue("--conversation-column-width") !== measured || column.style.getPropertyValue("--chat-content-width") !== content) {
      scrollOwner()?.beforeUpdate();
      column.style.setProperty("--conversation-column-width", measured);
      if (content) column.style.setProperty("--chat-content-width", content); else column.style.removeProperty("--chat-content-width");
      scrollOwner()?.afterUpdate();
    }
    positions();
  }
  function schedule() {
    if (disposed || frame) return;
    frame = requestAnimationFrame(() => { frame = 0; paint(); });
  }
  function finish(commit = false, x?: number) {
    if (!drag) return;
    const previous = drag;
    if (commit && x !== undefined && Number.isFinite(x) && x !== previous.origin) save(clamp(previous.base + (x - previous.origin) * previous.direction * 2));
    drag = null;
    controls.dragging(null);
    document.documentElement.classList.remove("chat-width-dragging");
    if (previous.handle.hasPointerCapture(previous.pointer)) previous.handle.releasePointerCapture(previous.pointer);
    paint();
  }
  function reset() { if (disposed) return; finish(); save(null); paint(); }
  for (const handle of handles) {
    const side = handle.dataset.chatWidthHandle as Side;
    handle.addEventListener("wheel", event => scrollOwner()?.wheelFromHandle(event), {...options, passive: false});
    handle.addEventListener("pointerdown", event => {
      if (event.button !== 0 || event.isPrimary === false || drag || handle.hidden) return;
      event.preventDefault();
      handle.setPointerCapture(event.pointerId);
      handle.focus({preventScroll: true});
      drag = {handle, pointer: event.pointerId, base: resolved(), origin: event.clientX, direction: side === "left" ? -1 : 1, width: resolved()};
      controls.dragging(side);
      document.documentElement.classList.add("chat-width-dragging");
    }, options);
    handle.addEventListener("pointermove", event => {
      handle.style.setProperty("--width-pointer-y", `${event.clientY - handle.getBoundingClientRect().top}px`);
      if (!drag || drag.handle !== handle || drag.pointer !== event.pointerId) return;
      drag.width = clamp(drag.base + (event.clientX - drag.origin) * drag.direction * 2);
      schedule();
    }, options);
    handle.addEventListener("pointerup", event => {
      if (drag?.handle === handle && drag.pointer === event.pointerId) finish(true, event.clientX);
    }, options);
    for (const name of ["pointercancel", "lostpointercapture"] as const) handle.addEventListener(name, event => {
      if (drag?.handle === handle && drag.pointer === event.pointerId) finish();
    }, options);
    handle.addEventListener("keydown", event => {
      if (event.altKey || event.ctrlKey || event.metaKey) return;
      if (event.key === "Home") { event.preventDefault(); reset(); }
      else if (["ArrowLeft", "ArrowRight"].includes(event.key)) {
        event.preventDefault(); finish();
        const direction = (event.key === "ArrowRight" ? 1 : -1) * (side === "left" ? -1 : 1);
        const current = resolved(), next = clamp(current + direction * (event.shiftKey ? 40 : 10));
        // The automatic width may exceed the dragged edge budget in a
        // partially fitting column. Widen must not unexpectedly narrow it.
        if (direction > 0 && next < current) return;
        save(next); paint();
      }
    }, options);
  }
  document.addEventListener("keydown", event => {
    if (event.key === "Escape" && drag) { event.preventDefault(); event.stopPropagation(); finish(); }
  }, {...options, capture: true});
  window.addEventListener("blur", () => finish(), options);
  document.addEventListener("visibilitychange", () => { if (document.hidden) finish(); }, options);
  window.addEventListener("resize", () => { finish(); schedule(); }, options);
  document.addEventListener("scroll", schedule, {...options, capture: true, passive: true});
  if (window.visualViewport) {
    window.visualViewport.addEventListener("resize", () => { finish(); schedule(); }, options);
    window.visualViewport.addEventListener("scroll", schedule, options);
  }
  if (typeof ResizeObserver === "function") {
    observer = new ResizeObserver(() => {
      // A changed host width invalidates a captured gesture's coordinate
      // system, but never replaces a wider persisted preference with a clamp.
      if (drag && column.style.getPropertyValue("--conversation-column-width") !== `${columnWidth()}px`) finish();
      schedule();
    });
    observer.observe(column); observer.observe(stream); observer.observe(transcript);
  }
  paint();
  return {reset, dispose() {
    if (disposed) return;
    disposed = true; finish(); listeners.abort(); observer?.disconnect();
    if (frame) cancelAnimationFrame(frame);
    controls.dispose();
    column.style.removeProperty("--conversation-column-width"); column.style.removeProperty("--chat-content-width");
  }};
}
