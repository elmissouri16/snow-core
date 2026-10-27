import type {Controller} from './controller.tsx';
import {counter} from './model.ts';

export function FindPanel({controller: c}: {controller: Controller}) {
  return <>
    <button type="button" className="quiet" data-find-open="" hidden onClick={c.show}>Find in conversation</button>
    {c.open && <div className="find-bar" role="search" aria-label="Find in conversation" onKeyDown={event => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); c.close(); }
      else if (event.key === 'Enter') { event.preventDefault(); c.move(event.shiftKey ? -1 : 1); }
    }}>
      <svg className="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="M16 10a6 6 0 1 1-12 0 6 6 0 0 1 12 0Zm-2 4 6 6"/></svg>
      <input type="search" data-find-input="" value={c.query} placeholder="Find in conversation" aria-label="Find in conversation" autoComplete="off" spellCheck={false} maxLength={200} onChange={event => c.setQuery(event.currentTarget.value)} />
      <span className="find-count" role="status" aria-live="polite">{c.query.trim().length >= 2 ? counter(c.index, c.matches.length, c.limited) : ''}</span>
      <button type="button" className="quiet icon-button" data-find-previous="" aria-label="Previous match" title="Previous (Shift+Enter)" disabled={!c.matches.length} onClick={() => c.move(-1)}><svg className="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true"><path d="m6 15 6-6 6 6"/></svg></button>
      <button type="button" className="quiet icon-button" data-find-next="" aria-label="Next match" title="Next (Enter)" disabled={!c.matches.length} onClick={() => c.move(1)}><svg className="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg></button>
      <button type="button" className="quiet icon-button" data-find-close="" aria-label="Close find" title="Close (Esc)" onClick={c.close}><svg className="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6"/></svg></button>
    </div>}
  </>;
}
