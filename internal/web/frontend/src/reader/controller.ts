/* Transcript following adapted from Harness ChatView/ConversationRoot (MIT).
 * See HARNESS-NOTICE.txt. Presentation only: no transport or runtime authority.
 * Geometry/intent algorithm retained from static/scroll.js; JSX owns controls.
 */
import {mountControls} from './Controls';

type Anchor = {top: number; candidates: {key: string; offset: number}[]};
type PromptSize = {node: HTMLTextAreaElement; value: string; width: number; minimum: string; maximum: string};

const positions = new Map<string, {following: boolean; anchor: Anchor | null}>(), memoryLimit = 40, threshold = 24, epsilon = 0.5;
const $ = (selector: string, scope: ParentNode) => scope.querySelector<HTMLElement>(selector);
const clamp = (value: number, max: number) => Math.max(0, Math.min(value, max));

export function mount(region: HTMLElement, key: string) {
  const streamNode = $("#live-stream", region), transcriptNode = $("#live-transcript", region);
  if (!streamNode || !transcriptNode) return null;
  const stream = streamNode, transcript = transcriptNode;
  const host = region.closest(".conversation-pane") || region;
  const seat = $("#live-composer-seat", host);
  const controlsHost = $("[data-react-reader-controls]", region);
  if (!controlsHost) return null;
  const controls = mountControls(controlsHost, follow);
  const cleanup: (() => void)[] = [], saved = positions.get(key);
  let following = saved?.following !== false, anchor = saved?.anchor || null;
  let observedTop = stream.scrollTop, updating = false, disposed = false, frame = 0;
  let resizeObserver: ResizeObserver | undefined, contentObserver: MutationObserver | undefined;
  let geometry: string, promptSize: PromptSize | undefined;
  // Writes update this ledger synchronously. Scroll events carry the current
  // offset, not the historical write, even when multiple deliveries coalesce.
  // Clamp-only deliveries therefore never acquire/release reader ownership.
  const floor = () => Math.max(0, stream.scrollHeight - stream.clientHeight);
  const top = () => clamp(stream.scrollTop, floor());
  function listen<K extends keyof WindowEventMap>(target: EventTarget, event: K,
    handler: (event: WindowEventMap[K]) => void, options?: AddEventListenerOptions) {
    target.addEventListener(event, handler as EventListener, options);
    cleanup.push(() => target.removeEventListener(event, handler as EventListener, options));
  }
  function write(value: number) {
    const next = clamp(value, floor());
    if (Math.abs(stream.scrollTop - next) > epsilon) stream.scrollTop = next;
    observedTop = top();
  }
  function rows() {
    // No selector interpolation of server identities; message/activity keys
    // are separate namespaces. A duplicated identity cannot be an anchor.
    const nodes = [...stream.querySelectorAll<HTMLElement>('[data-message-id],[data-activity-id]')];
    const counts = new Map<string, number>();
    const keyed = nodes.map(node => ({node, key: node.dataset.messageId ?
      `message:${node.dataset.messageId}` : node.dataset.activityId ? `activity:${node.dataset.activityId}` : ""}));
    for (const item of keyed) counts.set(item.key, (counts.get(item.key) || 0) + 1);
    return keyed.filter(item => item.key && counts.get(item.key) === 1 && !item.node.hidden);
  }
  function capture() {
    const view = stream.getBoundingClientRect(), candidates = [];
    // A small chain survives bounded head trimming if the leading row goes
    // away. The next surviving row retains its original viewport position.
    for (const item of rows()) {
      const rect = item.node.getBoundingClientRect();
      if (rect.bottom <= view.top || rect.height === 0) continue;
      // Keep the row at its viewport position even when surrounding chrome
      // moves the scrollport itself; a stream-relative offset would drift.
      candidates.push({key: item.key, offset: rect.top});
      if (candidates.length === 8) break;
    }
    return {top: top(), candidates};
  }
  function remember() {
    if (!key) return;
    positions.delete(key);
    positions.set(key, {following, anchor: following ? null : anchor});
    while (positions.size > memoryLimit) positions.delete(positions.keys().next().value!);
  }
  function chrome() {
    region.dataset.scrollFollowing = String(following);
    // Any deliberate upward movement, even 1px inside the threshold, reveals
    // the escape hatch immediately. Geometry alone never hides reader intent.
    controls.render(following || floor() <= epsilon);
  }
  function restore() {
    if (!anchor) { anchor = capture(); return; }
    const keyed = new Map(rows().map(item => [item.key, item.node]));
    const survivor = anchor.candidates.find(item => keyed.has(item.key));
    if (survivor) {
      const offset = keyed.get(survivor.key)!.getBoundingClientRect().top;
      write(top() + offset - survivor.offset);
    } else write(anchor.top);
    anchor = capture();
  }
  function sampleReader() {
    const current = top(), expected = clamp(observedTop, floor());
    const delta = current - expected;
    if (Math.abs(delta) <= epsilon) { observedTop = current; return false; }
    // Direction matters: tiny upward gestures must not get erased by the
    // next chunk. Only a reader moving DOWN into the tail may auto-rejoin.
    following = delta > 0 && floor() - current <= threshold;
    observedTop = current;
    anchor = following ? null : capture();
    chrome(); remember();
    return true;
  }
  function scroll() {
    if (disposed) return;
    sampleReader();
    chrome();
  }
  function sizePrompt() {
    const prompt = seat?.querySelector<HTMLTextAreaElement>("#live-prompt");
    // Attention can hide the normal card. Measure its existing draft only
    // once it has a real layout box again; never replace/focus/edit the input.
    if (!prompt || !prompt.getClientRects().length) return;
    const style = getComputedStyle(prompt), width = prompt.clientWidth;
    const minimum = style.minHeight, maximum = style.maxHeight;
    if (promptSize?.node === prompt && promptSize.value === prompt.value &&
        promptSize.width === width && promptSize.minimum === minimum && promptSize.maximum === maximum) return;
    const scrollTop = prompt.scrollTop;
    const border = parseFloat(style.borderTopWidth) + parseFloat(style.borderBottomWidth);
    const padding = parseFloat(style.paddingTop) + parseFloat(style.paddingBottom);
    // Reset only presentation to allow shrinking. CSS keeps the normal 36px
    // floor, short-seat floor and measured seat/336px cap authoritative.
    prompt.style.height = "0px";
    const height = prompt.scrollHeight + (style.boxSizing === "border-box" ? border : -padding);
    prompt.style.height = `${Math.max(parseFloat(minimum) || 0,
      Math.min(height, parseFloat(maximum) || Infinity))}px`;
    prompt.scrollTop = scrollTop;
    promptSize = {node: prompt, value: prompt.value, width: prompt.clientWidth, minimum, maximum};
  }
  function measure() {
    if (seat) seat.style.setProperty("--scroll-seat-limit", `${Math.max(64, host.getBoundingClientRect().height * 0.55)}px`);
    sizePrompt();
    const rect = region.getBoundingClientRect(), view = stream.getBoundingClientRect();
    const content = transcript.getBoundingClientRect();
    // The stream ends above the flow-seated card. Measure that actual edge,
    // not a guessed composer height; margins/safe-area/footer are included.
    const seatTop = seat && !seat.hidden ? seat.getBoundingClientRect().top : rect.bottom;
    const bottom = Math.max(0, rect.bottom - Math.min(view.bottom, seatTop)) + 16;
    const right = Math.max(16, rect.right - Math.min(content.right, view.right - 16));
    region.style.setProperty("--scroll-jump-bottom", `${bottom}px`);
    region.style.setProperty("--scroll-jump-right", `${right}px`);
    return [stream.scrollHeight, stream.clientHeight, stream.clientWidth, rect.height, seatTop, content.width].join(":");
  }
  function reconcile(force = false) {
    if (disposed || updating) return;
    // Input can have changed scrollTop before its queued scroll event fires.
    // Settle it before a stream/resize callback gets a chance to write.
    sampleReader();
    const next = measure();
    if (force || next !== geometry) {
      if (following) write(floor());
      else restore();
    }
    geometry = next;
    chrome(); remember();
  }
  function schedule() {
    if (disposed || frame) return;
    frame = requestAnimationFrame(() => { frame = 0; reconcile(true); });
  }
  function beforeUpdate() {
    if (disposed || updating) return;
    sampleReader();
    // Use the previous stable anchor when ResizeObserver has not delivered
    // a reflow yet. Capturing the already-shifted row would lose that anchor.
    if (!following && !anchor) anchor = capture();
    updating = true;
  }
  function afterUpdate() {
    if (disposed) return;
    // Synchronous presentation commits may clamp scrollTop through several
    // intermediate layouts. They are not new reader intent; beforeUpdate
    // already captured that intent and its stable content anchor.
    if (updating) observedTop = top();
    updating = false;
    // Structural changes can move rows without changing total content size.
    // Restoring an unchanged anchor is a no-op, never a near-bottom snap.
    reconcile(true);
    observeContent();
  }
  function follow() {
    if (disposed) return;
    following = true; anchor = null;
    geometry = measure();
    write(floor()); chrome(); remember();
  }
  function observeContent() {
    if (!resizeObserver) return;
    // Rebuild a small bounded observation set after reconciliation; removed
    // activity/message nodes must not stay retained by ResizeObserver.
    resizeObserver.disconnect();
    resizeObserver.observe(host); resizeObserver.observe(region); resizeObserver.observe(stream);
    if (seat) resizeObserver.observe(seat);
    for (const child of stream.children) resizeObserver.observe(child);
  }
  function visualViewportChanged() {
    const viewport = window.visualViewport;
    // Dynamic viewport units handle browser chrome. Use the visual height
    // when an unzoomed keyboard reduces it; don't counteract pinch zoom or
    // invent a keyboard inset. Pan changes trigger measurement only: actual
    // iOS/Android focus/pan must be checked on-device, not promised by CSS.
    if (viewport && Math.abs(viewport.scale - 1) < 0.01 && viewport.height > 0) {
      document.documentElement.style.setProperty("--snow-visual-height", `${viewport.height}px`);
    } else document.documentElement.style.removeProperty("--snow-visual-height");
    schedule();
  }
  listen(stream, "scroll", scroll, {passive: true});
  if (seat) listen(seat, "input", event => {
    if (event.target instanceof Element && event.target.id === "live-prompt") reconcile(true);
  });
  // Stop pending follow writes at the beginning of upward wheel/touch/key
  // intent, not after a racing streaming frame. Do not cancel native input,
  // and do not claim events from nested code/table/form scrollers.
  function nestedScroller(target: EventTarget | null) {
    for (let node = target instanceof Element ? target : null; node && node !== stream; node = node.parentElement) {
      if (node.scrollHeight > node.clientHeight + 1 && /auto|scroll/.test(getComputedStyle(node).overflowY)) return true;
    }
    return false;
  }
  function release() {
    if (disposed) return;
    if (following && floor() > epsilon) {
      following = false; anchor = capture(); chrome(); remember();
    }
  }
  function wheelIntent(event: WheelEvent) {
    if (!event.defaultPrevented && event.deltaY < 0 && !event.ctrlKey && !nestedScroller(event.target)) release();
  }
  listen(stream, "wheel", wheelIntent, {passive: true});
  function wheelFromHandle(event: WheelEvent) {
    // Fixed width handles do not join the native overflow scroll chain,
    // despite DOM ancestry. Keep the narrow bridge here with follow/reader
    // intent, not in a competing scroll controller. Ctrl+wheel stays zoom.
    if (disposed || event.ctrlKey || event.defaultPrevented || !event.deltaY || !(event.target instanceof Element) || !stream.contains(event.target) || !event.target.closest?.("[data-chat-width-handle]")) return;
    wheelIntent(event);
    event.preventDefault();
    const unit = event.deltaMode === 2 ? stream.clientHeight : event.deltaMode === 1 ? parseFloat(getComputedStyle(stream).lineHeight) || 16 : 1;
    stream.scrollTop += event.deltaY * unit;
  }
  let touchY: number | undefined;
  listen(stream, "touchstart", event => { touchY = event.touches.length === 1 ? event.touches[0].clientY : undefined; }, {passive: true});
  listen(stream, "touchmove", event => {
    if (touchY === undefined || event.touches.length !== 1) return;
    const y = event.touches[0].clientY;
    if (y > touchY && !nestedScroller(event.target)) release();
    touchY = y;
  }, {passive: true});
  listen(stream, "keydown", event => {
    if (event.defaultPrevented || event.ctrlKey || event.metaKey || event.altKey || (event.target instanceof Element && event.target.closest('input,textarea,select,[contenteditable="true"]'))) return;
    if (["ArrowUp", "PageUp", "Home"].includes(event.key) || event.key === " " && event.shiftKey) {
      if (!nestedScroller(event.target)) release();
    }
  });
  listen(window, "resize", visualViewportChanged, {passive: true});
  if (window.visualViewport) {
    listen(window.visualViewport, "resize", visualViewportChanged, {passive: true});
    listen(window.visualViewport, "scroll", schedule, {passive: true});
  }
  if (typeof ResizeObserver === "function") {
    resizeObserver = new ResizeObserver(schedule); observeContent();
  }
  // Covers same-height keyed replacements and tool disclosure in addition to
  // resize. Attribute filtering excludes our own chrome/style writes.
  if (typeof MutationObserver === "function") {
    contentObserver = new MutationObserver(() => { observeContent(); schedule(); });
    contentObserver.observe(stream, {childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ["open", "hidden"]});
  }
  geometry = measure();
  if (saved && !following) { write(saved.anchor?.top || 0); restore(); }
  else write(floor());
  chrome(); remember(); visualViewportChanged();
  return {
    beforeUpdate, afterUpdate, follow, wheelFromHandle, onLayout: schedule, userIntent: release,
    dispose() {
      if (disposed) return;
      sampleReader(); remember(); disposed = true;
      if (frame) cancelAnimationFrame(frame);
      resizeObserver?.disconnect(); contentObserver?.disconnect();
      for (const remove of cleanup) remove();
      controls.dispose();
      anchor = null; promptSize = undefined;
      region.removeAttribute("data-scroll-following");
      for (const name of ["--scroll-jump-bottom", "--scroll-jump-right"]) region.style.removeProperty(name);
      seat?.style.removeProperty("--scroll-seat-limit");
      document.documentElement.style.removeProperty("--snow-visual-height");
    }
  };
}
