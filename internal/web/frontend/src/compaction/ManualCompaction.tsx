import {createPortal} from "react-dom";
import {Icon} from "../conversation/Icons";
import type {Compaction} from "./model.ts";
import {description} from "./model.ts";

interface Props {
  launcher: HTMLElement;
  supported: boolean;
  canStart: boolean;
  committing: boolean;
  stopping: boolean;
  invalid: boolean;
  notice: string;
  dismissed: boolean;
  compaction: Compaction | null;
}
/** One explicit click authorizes compaction. Admission stays in the controller. */
export function ManualCompaction(p: Props) {
  const busy = p.committing || p.compaction?.state === "pending" || p.compaction?.state === "running";
  const status = p.invalid ? "Compaction status is unavailable. Reconnect before trying again."
    : p.notice || (p.stopping ? "Stopping compaction…" : p.committing ? "Starting compaction…" : description(p.compaction));
  return <>
    {createPortal(<button type="button" className="composer-context-button" data-compaction-open="" aria-label="Compact context" title={busy ? "Compacting context…" : "Compact context · Uses provider tokens"} aria-busy={busy} hidden={!p.supported} disabled={!p.canStart}><Icon name="compaction" /></button>, p.launcher)}
    <div data-compaction-inline="" className="compaction-inline fine" hidden={p.dismissed || !p.compaction && !p.committing && !p.notice && !p.invalid}>
      <p data-compaction-status="" role="status">{status}</p>
      <button type="button" className="quiet icon-button" data-compaction-dismiss="" aria-label="Dismiss compaction status" title="Dismiss compaction status" disabled={busy}>×</button>
    </div>
  </>;
}
