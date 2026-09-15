import {createPortal} from "react-dom";
import type {Controller} from "./controller";
import {fields, options, sameScope} from "./model";
import type {Field} from "./model";

/** Thinking is a direct model-owned picker; response preferences remain secondary. */
export function ReasoningPanel({controller: c}: {controller: Controller}) {
  const enabled = c.ready(), result = !c.uncertain && sameScope(c.displayed, c.snapshot) ? c.displayed : null;
  // Readability governs interaction, not the last reported value. Replacing a
  // level with "Unavailable" during every admission reflows the composer row.
  const thinking = c.uncertain ? "Unverified" : c.snapshot?.thinking || "Unavailable";
  const levels = c.inspected?.thinking_levels || [];
  const changed = !!c.inspected && !!c.field && c.value !== c.inspected[c.field] && c.inspected[options[c.field]]?.includes(c.value);
  const preferences = result ? (Object.keys(fields) as Field[]).filter(k => k !== "thinking" && result[options[k]]?.length) : [];
  const values = c.field ? result?.[options[c.field]] || [] : [];
  const notice = c.error || (c.read ? "Reading authoritative settings and model capabilities…" : c.committing ? "Applying one session-only update…" : !sameScope(c.inspected, c.snapshot) ? "Close and reopen Thinking to inspect the current session." : !c.ui?.safe ? "Controls require an idle connected runtime with no other operation, active goal or queue review." : "Current values verified. Changes apply only to this runtime; host and project defaults remain unchanged.");
  return <>
    {createPortal(<>
      <button type="button" className="composer-context-button reasoning-trigger" data-reasoning-open="" title={c.committing ? `Updating thinking · Last reported: ${thinking}` : c.ui?.readable ? `Thinking: ${thinking}` : `Last reported thinking: ${thinking} · Controls unavailable`} aria-busy={c.committing} aria-haspopup="menu" aria-expanded={c.pickerOpen} aria-controls="reasoning-picker" popoverTarget="reasoning-picker" disabled={!c.ui?.readable || c.committing || c.uncertain}
        onClick={e => { e.preventDefault(); c.open(e.currentTarget); }} onKeyDown={e => { if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); if (!c.pickerOpen) c.open(e.currentTarget); } }}>
        <span>Thinking:</span> <span data-reasoning-current="">{thinking}</span><svg className="icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" aria-hidden="true"><path d="m7 10 5 5 5-5" /></svg>
      </button>
      <div ref={c.setPicker} id="reasoning-picker" className="reasoning-picker" popover="auto" tabIndex={-1} aria-label="Thinking" onKeyDown={c.pickerKey}>
        <p className="reasoning-picker-heading">Thinking</p>
        <p className="fine reasoning-picker-notice" data-reasoning-picker-notice="" role="status">{c.uncertain || c.error || c.read || !enabled ? notice : "Applies to this session only."}</p>
        <div role="menu" aria-label="Thinking level" aria-busy={!!c.read || c.committing}>
          {levels.map(level => <button type="button" role="menuitemradio" aria-checked={result?.thinking === level} data-reasoning-level={level} key={level} disabled={!enabled} onClick={() => c.choose(level)}><span>{level}</span><span aria-hidden="true">{result?.thinking === level ? "✓" : ""}</span></button>)}
        </div>
        {!c.read && c.inspected && !levels.length ? <p className="fine">This model advertises no adjustable thinking levels.</p> : null}
        {preferences.length ? <div className="reasoning-picker-secondary"><button type="button" data-reasoning-advanced="" disabled={!!c.read || c.committing || c.uncertain} onClick={c.openAdvanced}>Response settings…</button></div> : null}
      </div>
    </>, c.launcher)}
    <dialog ref={c.setDialog} id="reasoning-dialog" className="reasoning-dialog runtime-dialog" aria-labelledby="reasoning-heading" aria-describedby="reasoning-boundary"
      onCancel={e => {e.preventDefault(); c.close();}} onClose={c.cancel}
      onCompositionStart={() => c.composition(true)} onCompositionEnd={() => c.composition(false)}>
      <div className="dialog-heading">
        <h2 id="reasoning-heading">Response settings</h2>
        <button type="button" className="quiet" data-reasoning-refresh="" disabled={!c.ui?.readable || !!c.read || c.committing || c.uncertain} onClick={() => void c.inspect()}>Refresh settings</button>
        <button type="button" className="quiet icon-button" data-reasoning-close="" aria-label="Close response settings" title="Close" disabled={c.committing} onClick={c.close}><svg className="icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg></button>
      </div>
      <div className="runtime-dialog-body">
        <p id="reasoning-boundary" className="fine">Inspection does not send a prompt. Available choices come from the worker’s model capabilities, not a model-name guess. Settings changes require an idle session without active goals or pending/review queue items.</p>
        <p data-reasoning-notice="" className="fine" role="status" aria-live="polite">{notice}</p>
        <section aria-labelledby="reasoning-current-heading"><h3 id="reasoning-current-heading">Current session — effective values</h3>
          <dl className="reasoning-facts">
            <dt>Project / session</dt><dd data-reasoning-identity="">{result ? `${result.project_id} / ${result.session_id}` : ""}</dd>
            <dt>Model / permissions</dt><dd data-reasoning-authority="">{result ? `${result.provider} / ${result.model} · permissions: ${result.permission_mode}` : ""}</dd>
            <dt>Collaboration mode</dt><dd data-reasoning-mode="">{result?.mode || ""}</dd>
            <dt>Reasoning summary</dt><dd data-reasoning-summary="">{result?.reasoning_summary || ""}</dd>
            <dt>Text verbosity</dt><dd data-reasoning-verbosity="">{result?.text_verbosity || ""}</dd>
          </dl>
          <p className="fine">These are effective runtime values, not saved defaults. Changes apply only to this runtime: no host or project configuration is written. These overrides are not saved as configuration or new session metadata.</p>
        </section>
        <section aria-labelledby="reasoning-session-heading"><h3 id="reasoning-session-heading">Update this session’s runtime</h3>
          <p className="fine">Only the selected preference is changed; other response preferences, model selection, permissions and extension settings are untouched. No prompt or goal starts.</p>
          <label htmlFor="reasoning-field">Preference</label><select id="reasoning-field" data-reasoning-field="" disabled={!enabled} value={c.field} onChange={e => c.selectField(e.currentTarget.value)}>
            {preferences.map(field => <option key={field} value={field}>{fields[field]}</option>)}
            {result && !preferences.length ? <option value="">No supported mutable preferences advertised</option> : null}
          </select>
          <label htmlFor="reasoning-value">New value</label><select id="reasoning-value" data-reasoning-value="" disabled={!enabled} value={c.value} onChange={e => c.selectValue(e.currentTarget.value)}>{values.map(value => <option key={value} value={value}>{value}</option>)}</select>
          <div className="dialog-actions"><button type="button" className="button primary" data-reasoning-confirm="" disabled={!enabled || !changed || !c.inspected?.current_session_available} onClick={() => void c.commit()}>Apply session-only update</button></div>
        </section>
      </div>
    </dialog>
  </>;
}
