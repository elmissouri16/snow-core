import type {RefObject, CSSProperties, KeyboardEvent} from 'react';
import type {Choice, Item} from './types.ts';

export function MentionIcon({kind}: {kind: string}) {
  return <span className="composer-mention-icon" aria-hidden="true" data-kind={kind}>
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
      {kind === 'skill' ? <path d="m9 5 2 5 5 2-5 2-2 5-2-5-5-2 5-2 2-5Z M19 2l1 3 3 1-3 1-1 3-1-3-3-1 3-1 1-3Z" /> : kind === 'folder' ? <path d="M3 6a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" /> : <><rect x="5" y="3" width="14" height="18" rx="3" /><path d="M8 8h8M8 12h6" /></>}
    </svg>
  </span>;
}
export type Presentation = {
  items: (Item & {url: string})[]; canRemove: boolean; canAttach: boolean; canRead: boolean; canFiles: boolean;
  notice: string; privacy: string; error: boolean; popupVisible: boolean; rows: Choice[];
  selected: number; message: string; heading: boolean; marker: string; menuOpen: boolean;
  popupStyle: CSSProperties; menuStyle: CSSProperties;
};
export type Actions = {
  remove(item: Item): void; previewError(item: Item, url: string, image: HTMLImageElement): void;
  choose(choice: Choice): void; addFiles(files: FileList): void; attach(): void; menu(): void;
  marker(marker: string): void; menuKey(event: KeyboardEvent<HTMLDivElement>): void;
};
export type Refs = {input: RefObject<HTMLInputElement | null>; attach: RefObject<HTMLButtonElement | null>; trigger: RefObject<HTMLButtonElement | null>; popup: RefObject<HTMLDivElement | null>; menu: RefObject<HTMLDivElement | null>};
export function ContextPanel({state: s, actions: a, refs}: {state: Presentation; actions: Actions; refs: Refs}) {
  return <>
    <div className="composer-context-items" data-composer-context-items hidden={!s.items.length}>
      {s.items.map(item => <div key={item.id} className={`composer-context-chip${item.state === 'error' ? ' is-error' : ''}`}>
        {item.url && <span className="composer-context-thumbnail"><img src={item.url} alt="" width={32} height={32} onError={event => a.previewError(item, item.url, event.currentTarget)} /></span>}
        <span className="composer-context-label" title={item.label}>{item.label}</span>
        {item.state !== 'ready' && <span className="composer-context-detail">{item.state === 'pending' ? 'Reading…' : item.error}</span>}
        <button type="button" className="composer-context-remove" aria-label={`Remove attachment ${item.label}`} disabled={!s.canRemove} data-context-item-id={item.id} onClick={() => a.remove(item)}>×</button>
      </div>)}
    </div>
    <div className={`composer-context-status${s.error ? ' is-error' : ''}`} data-composer-context-status role="status" aria-live="polite" hidden={!s.notice} title={s.privacy}>{s.notice}</div>
    <div id="composer-mentions" className="composer-mentions" data-composer-mentions role="listbox" aria-label="Files and installed skills" hidden={!s.popupVisible} ref={refs.popup} style={s.popupStyle}>
      {s.message && <div className={s.heading ? 'composer-mention-heading' : 'composer-mention-note'}>{s.message}</div>}
      {s.rows.map((choice, index) => <div key={choice.id} id={choice.id} className="composer-mention-option" role="option" aria-selected={index === s.selected} aria-disabled={choice.disabled || undefined} data-composer-skills-retry={choice.retrySkills ? '' : undefined}
        title={[choice.title || choice.label, choice.fullDescription ?? choice.description].filter(Boolean).join(' — ')} onMouseDown={event => event.preventDefault()} onClick={() => a.choose(choice)}>
        <MentionIcon kind={choice.folder ? 'folder' : s.marker === '$' ? 'skill' : 'file'} />
        <span className="composer-mention-main"><span className="composer-mention-name" title={choice.label}>{choice.label}</span>
          {choice.description && <span className="composer-mention-description" title={choice.fullDescription ?? choice.description}>{choice.description}</span>}
        </span>
        {choice.folder && <><span className="composer-mention-folder-hint" hidden={index !== s.selected}>Browse folder</span><span className="composer-mention-chevron" aria-hidden="true">›</span></>}
      </div>)}
    </div>
    {s.menuOpen && <div id="composer-context-menu" ref={refs.menu} className="snow-menu composer-context-menu" role="menu" aria-label="Add context" tabIndex={-1} style={s.menuStyle} onKeyDown={a.menuKey}>
      <div className="snow-menu-content" tabIndex={-1}>
        <button type="button" role="menuitem" className="snow-menu-row composer-context-menu-item" data-composer-files disabled={!s.canFiles} onClick={() => a.marker('@')}><MentionIcon kind="folder" /><span className="snow-menu-row-label">Project files</span><span className="snow-menu-row-value" aria-hidden="true">@</span></button>
        <button type="button" role="menuitem" className="snow-menu-row composer-context-menu-item" data-composer-skills disabled={!s.canRead} onClick={() => a.marker('$')}><MentionIcon kind="skill" /><span className="snow-menu-row-label">Installed skills</span><span className="snow-menu-row-value" aria-hidden="true">$</span></button>
      </div>
    </div>}
  </>;
}
export function ContextTools({state: s, actions: a, refs}: {state: Presentation; actions: Actions; refs: Refs}) {
  return <>
    {/* MIME classifications are not authoritative; bounded byte validation happens after explicit selection. */}
    <input type="file" data-composer-file-input multiple hidden disabled={!s.canAttach} ref={refs.input} onChange={event => { if (event.currentTarget.files) a.addFiles(event.currentTarget.files); event.currentTarget.value = ''; }} />
    <div className="composer-context-tools" aria-label="Add context">
      <button type="button" className="composer-context-button" data-composer-context-menu aria-label="Add context" title="Add context" aria-haspopup="menu" aria-expanded={s.menuOpen} aria-controls={s.menuOpen ? 'composer-context-menu' : undefined} disabled={!s.canRead} ref={refs.trigger} onClick={a.menu}>
        <svg className="icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
      </button>
      <button type="button" className="composer-context-button" data-composer-attach aria-label="Attach files" title="Attach images or UTF-8 text files" disabled={!s.canAttach} ref={refs.attach} onClick={a.attach}>
        <svg className="icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m8 12 6-6a3 3 0 0 1 4 4l-8 8a5 5 0 0 1-7-7l8-8m-5 13 8-8" /></svg>
      </button>
    </div>
  </>;
}
