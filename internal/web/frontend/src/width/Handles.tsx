import {createRef, type RefObject} from 'react';
import {flushSync} from 'react-dom';
import {createRoot} from 'react-dom/client';

export type Side = 'left' | 'right';
export type HandleView = {hidden: boolean; minimum: number; maximum: number; value: number; text: string};
const sides: Side[] = ['left', 'right'];
const title = 'Drag to resize conversation. Arrow keys adjust; Shift adjusts faster. Home restores automatic width. Escape cancels a drag.';

function Handles({refs, views, dragging}: {
  refs: RefObject<HTMLDivElement | null>[]; views: HandleView[]; dragging: Side | null;
}) {
  return <>{sides.map((side, index) => {
    const view = views[index];
    return <div key={side} ref={refs[index]} className="chat-width-handle"
      data-chat-width-handle={side} role="separator" aria-orientation="vertical"
      aria-label={`Conversation width, ${side} edge`}
      aria-controls="live-transcript live-composer-seat" title={title}
      hidden={view.hidden} tabIndex={view.hidden ? -1 : 0}
      aria-valuemin={view.minimum} aria-valuemax={view.maximum}
      aria-valuenow={view.value} aria-valuetext={view.text}
      data-dragging={dragging === side ? 'true' : undefined}/>;
  })}</>;
}

export function mountHandles(host: HTMLElement) {
  const root = createRoot(host), refs = sides.map(() => createRef<HTMLDivElement>());
  let views: HandleView[] = sides.map(() => ({hidden: true, minimum: 640, maximum: 640, value: 640, text: '640 pixels; automatic width'}));
  let dragging: Side | null = null;
  const commit = () => flushSync(() => root.render(<Handles refs={refs} views={views} dragging={dragging}/>));
  commit();
  return {
    handles: refs.map(ref => ref.current!),
    render(next: HandleView[]) {
      if (next.every((view, index) => {
        const prior = views[index];
        return view.hidden === prior.hidden && view.minimum === prior.minimum && view.maximum === prior.maximum && view.value === prior.value && view.text === prior.text;
      })) return;
      views = next; commit();
    },
    dragging(side: Side | null) {
      if (side === dragging) return;
      dragging = side; commit();
    },
    dispose() { flushSync(() => root.unmount()); }
  };
}
