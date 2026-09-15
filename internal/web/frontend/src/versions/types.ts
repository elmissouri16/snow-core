import type { Root } from 'react-dom/client';
import type { HistoryAction, HistoryInventory, Identity, Page, Preview, Selection, Version } from './model';
export interface Snapshot { revision?: number }
interface BaseAPI { root: HTMLElement; identity: Identity; openDialog(dialog: HTMLDialogElement, opener: HTMLElement): void; closeDialog(dialog: HTMLDialogElement): void }
export type Phase = 'preparing' | 'ready' | 'committing' | null;
export interface VersionsAPI extends BaseAPI {
  list(cursor: string, signal: AbortSignal): Promise<unknown>;
  preview(target: Version, cursor: string, signal: AbortSignal): Promise<unknown>;
  prepare(target: Version, origin: Page, signal: AbortSignal): Promise<unknown>;
  commit(token: string): Promise<unknown>;
  restoreState(phase: Phase): void;
}
export interface HistoryAPI extends BaseAPI {
  reserve(active: boolean): unknown;
  mutate(action: HistoryAction, fields: Record<string, string>): Promise<unknown>;
  refreshInventory?(childSessionID: string): unknown;
}
export interface VersionsUI { supported?: boolean; readable?: boolean; restoreSafe?: boolean; showStop?: boolean; canStop?: boolean; stopLabel?: string; stopTitle?: string }
export interface HistoryUI { supported?: boolean; safe?: boolean }
export interface Operation { kind: string; controller: AbortController }
export interface VersionsView {
  api: VersionsAPI; root: Root; host: HTMLElement; dialog: HTMLDialogElement | null; controller: AbortController;
  page: Page | null; selected: Version | null; preview: Preview | null; listCursors: string[]; previewCursors: string[]; listIndex: number; previewIndex: number;
  operation?: Operation | null; phase?: Phase; token?: string; expires?: number; stale?: boolean; error?: string;
  ui?: VersionsUI; snapshot?: Snapshot | null; restoreTarget?: string;
  retained?: {page: Page; selected?: Version | null; preview?: Preview | null; previewIndex: number} | null;
}
export interface HistoryView {
  api: HistoryAPI; panel: HTMLElement; dialog: HTMLDialogElement; controller: AbortController; draft: {name: string};
  snapshot?: Snapshot | null; ui?: HistoryUI; composing?: boolean; busy?: boolean; invalid?: boolean; error?: string;
  confirmation?: {action: HistoryAction; target: Selection; name: string} | null;
  consent: boolean; targetText?: string; confirmLabel?: string; inventory?: HistoryInventory;
}
