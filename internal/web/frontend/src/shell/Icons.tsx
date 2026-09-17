export type Glyph = 'panel' | 'close' | 'chat' | 'new' | 'folder' | 'search' | 'list' | 'plus' | 'chevron' | 'check' | 'settings' | 'activity' | 'snow' | 'light' | 'dark';
export function Icon({name, className = 'icon'}: {name: Glyph; className?: string}) {
  const paths: Record<Exclude<Glyph, 'snow'>, string> = {
    panel: 'M9 3v18M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2Z',
    close: 'm6 6 12 12M6 18 18 6', chat: 'M21 11.5a8.5 8.5 0 0 1-8.5 8.5H4l-3 2 1.5-6A8.5 8.5 0 1 1 21 11.5Z',
    new: 'M13 20H4l-3 2 1.5-6A8.5 8.5 0 1 1 21 11M18 15v6m-3-3h6',
    folder: 'M3 7V5a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z',
    search: 'm16 16 5 5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0Z', list: 'M9 6h12M9 12h12M9 18h12M3 6h1M3 12h1M3 18h1',
    plus: 'M12 5v14M5 12h14', chevron: 'm9 5 7 7-7 7', check: 'm5 12 4 4L19 6', settings: 'm9 3-1 3-3 1-2 5 2 5 3 1 1 3h6l1-3 3-1 2-5-2-5-3-1-1-3ZM16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z',
    activity: 'M2 12h5l3-8 4 16 3-8h5', light: 'M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z',
    dark: 'M20.5 14A8.5 8.5 0 0 1 10 3.5 8.5 8.5 0 1 0 20.5 14Z',
  };
  return <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d={name === 'snow' ? 'M12 2v20M3.34 7l17.32 10M3.34 17 20.66 7m-12-3.5L12 6l3.34-2.5M8.66 20.5 12 18l3.34 2.5M3.8 10.9l3.8.4L8 7.5m8 9 .4-3.8 3.8.4M3.8 13.1l3.8-.4.4 3.8m8-9 .4 3.8 3.8-.4' : paths[name]} />
  </svg>;
}
