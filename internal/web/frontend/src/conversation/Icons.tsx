const paths = {
  new: 'M9 18H5l-3 3V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v5M18 14v8m-4-4h8',
  rename: 'm16 3 5 5-12 12H4v-5L16 3Z M13 6l5 5',
  versions: 'M3 4v5h5M3 9a9 9 0 1 1 0 6M12 7v5l3 2',
  goals: 'M20 12a8 8 0 1 1-16 0 8 8 0 0 1 16 0ZM15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0ZM12 2v3M12 19v3M2 12h3M19 12h3',
  processes: 'M4 4h16v16H4V4Z m3 4 4 4-4 4m6 0h4',
  reasoning: 'M9 18h6m-6 3h6M8 14a6 6 0 1 1 8 0c-1 1-1 2-1 4H9c0-2 0-3-1-4Z',
  compaction: 'M10 3v7H3M3 7l7 3M14 21v-7h7M21 17l-7-3M3 14h7v7M7 21l3-7M21 10h-7V3M17 3l-3 7',
  steer: 'M12 21V3m-5 5 5-5 5 5M5 21v-4a5 5 0 0 1 5-5h2',
  auth: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8Zm-7 9a7 7 0 0 1 14 0M17 8l1 1 2-2',
  mcp: 'M4 6h16v5H4zM4 13h16v5H4zM7 8.5h.01M7 15.5h.01',
  subagents: 'M12 3v4M12 7H6v5h12V7h-6M8 12v3M16 12v3M4 15h8v6H4zM12 15h8v6h-8z',
  find: 'M16 10a6 6 0 1 1-12 0 6 6 0 0 1 12 0Zm-2 4 6 6',
  canvas: 'M4 4h16v16H4zM14 4v16M4 10h10',
  refresh: 'M20 7V3m0 4h-4M4 17v4m0-4h4M20 7a9 9 0 0 0-16 1m0 9a9 9 0 0 0 16-1',
  back: 'm14 6-6 6 6 6', next: 'm10 6 6 6-6 6', check: 'm5 12 4 4L19 6',
  chevron: 'm8 10 4 4 4-4', shield: 'M12 3 4 6v6c0 5 8 9 8 9s8-4 8-9V6l-8-3Z',
  model: 'm12 3 10 5-10 5L2 8l10-5Z M2 12l10 5 10-5M2 16l10 5 10-5', close: 'm6 6 12 12M6 18 18 6',
  power: 'M12 3v9m-5-7a8 8 0 1 0 10 0', inspector: 'M3 4h18v16H3zM15 4v16m3-11v2m0 3v2',
};
export type Glyph = keyof typeof paths;
export function Icon({name}: {name: Glyph}) {
  return <svg className={name === 'model' ? 'icon model-trigger-icon' : 'icon'} viewBox="0 0 24 24" width={name === 'chevron' ? 14 : name === 'close' ? 18 : 16} height={name === 'chevron' ? 14 : name === 'close' ? 18 : 16} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false"><path d={paths[name]} /></svg>;
}
