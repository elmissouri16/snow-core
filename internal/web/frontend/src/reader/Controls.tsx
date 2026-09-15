import {flushSync} from 'react-dom';
import {createRoot} from 'react-dom/client';

function Controls({hidden, follow}: {hidden: boolean; follow: () => void}) {
  return <button className="button jump-latest" type="button" id="jump-latest"
    hidden={hidden} aria-label="Jump to latest" title="Jump to latest"
    onClick={event => { event.stopPropagation(); follow(); }}>
    <svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor"
      strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 5v14m-6-6 6 6 6-6"/>
    </svg>
  </button>;
}

export function mountControls(host: HTMLElement, follow: () => void) {
  const root = createRoot(host);
  let previous: boolean | undefined;
  return {
    render(hidden: boolean) {
      // Streaming/resize geometry samples do not reconcile unchanged chrome.
      if (previous === hidden) return;
      previous = hidden;
      flushSync(() => root.render(<Controls hidden={hidden} follow={follow}/>));
    },
    dispose() { flushSync(() => root.unmount()); }
  };
}
