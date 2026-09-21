export type Authority = {safe: boolean; editable: boolean; readable: boolean};
export type Suggestions = {controls?: string; expanded: boolean; activeDescendant?: string};
export type API = {
  root: HTMLElement; key: string; instance: string;
  request(kind: 'files' | 'file' | 'skills', fields: {path?: string; offset?: number}): Promise<unknown>;
  changed?(): void; error?(message: string): void;
  /** Parent owns textarea state. Replace synchronously, publish its value/selection,
   * and update draft revision. Do not dispatch input: context inspects afterwards. */
  replaceText?(value: string, start: number, end: number): boolean;
  focusPrompt?(): void;
  suggestions?(state: Suggestions): void;
  command?(name: string): boolean;
};
export type Item = {
  id: number; version: number; label: string; state: 'pending' | 'ready' | 'error'; size: number;
  kind?: 'text' | 'image'; text?: string; mime?: string; data?: string; error?: string;
  previewURL?: string | null; previewElement?: HTMLImageElement | null; previewFailed?: boolean;
};
export type Snapshot = Readonly<{
  revision: number; items: readonly Readonly<{id: number; version: number}>[];
  content: string; text: string; hasContent: boolean; key: string; owner: object;
}>;
export type Controller = {
  render(state: Partial<Authority>): void; capture(text?: string): Snapshot;
  accepted(snapshot: Snapshot): void; hasAttachments(): boolean; pending(): boolean; dispose(): void;
};
export type Draft = {items: Item[]; revision: number; owner?: object | null; skillCatalog?: {instance: string; promise: Promise<unknown>}};
export type Query = {
  marker: string; value: string; start: number; end: number; text: string; caret: number;
  revision: number; path: string; filter: string; loading?: boolean; reading?: boolean; skillsLoading?: boolean;
};
export type Choice = {
  id?: string; label: string; title?: string; folder?: boolean; description?: string;
  fullDescription?: string; disabled?: boolean; retrySkills?: boolean; command?: string; choose(): void;
};
export type Entry = {kind: string; path: string; name: string};
export type Directory = {path: string; entries: Entry[]; offset: number; hasMore: boolean; limited: boolean};
export type ListingResponse = {path: string; entries: Entry[]; next_offset: number; has_more: boolean; limited: boolean};
export type FileResponse = {path: string; text: string; size: number; truncated: boolean};
export type SkillsResponse = {instance_id: string; enabled: boolean; limited: boolean; skills: {name: string; enabled: boolean; description?: string; disabled_by?: string}[]};
