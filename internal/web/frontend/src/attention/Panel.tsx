import type {FormEvent, KeyboardEvent, MouseEvent, RefObject} from 'react';
import {identifier, permissionBlocked, record, validInput} from './model.ts';
import type {Answer, Draft, PermissionRequest, State} from './model.ts';
export interface Props {
  kind: 'input' | 'permission'; pending: unknown; draft: Draft; state: State; feedback: string;
  formRef: RefObject<HTMLFormElement | null>; bodyRef: RefObject<HTMLDivElement | null>;
  onPage: (delta: number) => void; onCollapse: () => void; onContinue: () => void;
  onAnswer: (id: string, change: Partial<Answer>, advance?: boolean, focusCustom?: boolean) => void;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void; onGuard: (event: MouseEvent<HTMLElement>) => void;
  onKeydown: (event: KeyboardEvent<HTMLTextAreaElement>) => void; onComposition: (active: boolean) => void;
}
const recommendedSuffix = /\s*(?:\((?:recommended|推荐)\)|（(?:recommended|推荐)）)\s*$/i;
function Stop({state}: {state: State}) {
  const label = state.stopping ? 'Stopping…' : 'Stop turn';
  return <button type="button" className="attention-stop quiet" data-runtime-abort="" title="Stop the current turn without answering"
    disabled={!state.canStop} aria-label={label}>{label}</button>;
}
function Permission({pending, state, onGuard}: Props) {
  const p = (record(pending) ? pending : {}) as PermissionRequest, blocked = permissionBlocked(pending);
  const locked = !state.safe || !identifier(p.id);
  const string = (value: unknown) => typeof value === 'string' ? value : '';
  const list = (value: unknown): unknown[] => Array.isArray(value) ? value.slice(0, 64) : [];
  return <section className="attention-card attention-permission" aria-busy={!!state.busy} onClickCapture={onGuard}>
    <div className="attention-warning">{blocked ? 'Incomplete summary · approval blocked' : 'APPROVAL REQUIRED'}</div>
    <div className="attention-body attention-permission-body" tabIndex={0} role="group" aria-label="Permission details">
      <h2 className="attention-title">{string(p.tool) || 'Tool'} · {string(p.risk) || 'unspecified risk'}</h2>
      {p.agent_path && <p>Requested by subagent <code>{string(p.agent_path)}</code>{p.agent_role && <> ({string(p.agent_role)})</>}</p>}
      {p.reason && <p>{string(p.reason)}</p>}{p.scope_label && <p>{string(p.scope_label)}</p>}
      {list(p.paths).map((path, i) => <pre key={i}>{string(path)}</pre>)}
      {!!list(p.capabilities).length && <p>Capabilities: {list(p.capabilities).map(string).join(', ')}</p>}
      {list(p.effects).map((effect, i) => <pre key={i}>{record(effect) ?
        [effect.type, effect.capability, effect.operation, effect.resource, effect.command, effect.reason].map(string).filter(Boolean).join(' · ') : ''}</pre>)}
      {blocked && <p className="attention-authority-warning">This summary is incomplete and cannot be approved. Reject or stop the turn.</p>}
      {(p.unknown || blocked) && <p className="attention-authority-warning">Some effects are unknown or omitted.</p>}
    </div>
    <p className="attention-authority">Grants host authority, not sandboxed execution.</p>
    <footer className="attention-footer attention-permission-actions"><Stop state={state}/>
      <button type="button" className="quiet danger" data-permission="deny" data-request-id={string(p.id)} disabled={locked}>Reject</button>
      <button type="button" className="primary" data-permission="allow" data-request-id={string(p.id)} disabled={locked || blocked}
        data-blocked={blocked ? 'true' : undefined} title={blocked ? 'Incomplete permission summaries cannot be approved. Reject or stop this turn.' : undefined}>Allow once</button>
    </footer>
  </section>;
}
export function Panel(props: Props) {
  if (props.kind === 'permission') return <Permission {...props}/>;
  const {pending, draft, state, feedback, formRef, bodyRef, onPage, onCollapse, onContinue, onAnswer, onSubmit, onGuard, onKeydown, onComposition} = props;
  const valid = validInput(pending), questions = valid ? pending.questions : [], locked = !state.safe;
  const last = draft.page === questions.length - 1;
  return <form ref={formRef} className={'attention-card attention-questions' + (draft.collapsed ? ' attention-collapsed' : '')}
    data-runtime-input="" data-request-id={record(pending) && typeof pending.id === 'string' ? pending.id : ''} noValidate
    aria-busy={!!state.busy} onSubmitCapture={onSubmit} onClickCapture={onGuard}>
    <header className="attention-header"><div className="attention-heading"><div className="attention-eyebrow">YOUR INPUT NEEDED</div>
      <h2 className="attention-title" id="attention-question-title">{valid ? questions[draft.page]?.header || `Question ${draft.page + 1}` : 'Question unavailable'}</h2>
    </div><div className="attention-header-actions">
      <button type="button" className="attention-icon quiet" data-attention-collapse="" disabled={locked} onClick={onCollapse}
        aria-controls="attention-question-body attention-question-footer" aria-expanded={!draft.collapsed}
        aria-label={draft.collapsed ? 'Expand questions' : 'Collapse questions'}>{draft.collapsed ? '+' : '−'}</button><Stop state={state}/>
    </div></header>
    <div ref={bodyRef} className="attention-body" id="attention-question-body" hidden={draft.collapsed}>
      <p className="attention-feedback" id="attention-feedback" role="status" hidden={!feedback}>{feedback}</p>
      {!valid && <p className="attention-detail">This question batch is incomplete or unsupported. Stop the turn or review the workspace; no answers can be sent.</p>}
      {questions.map((q, index) => {
        const options = q.options || [], answer = draft.answers[q.id] || {selected: null, custom: ''};
        const custom = answer.selected === 'custom' || answer.selected === null;
        return <fieldset key={q.id} className="attention-question" data-question-id={q.id} hidden={index !== draft.page} disabled={locked} aria-describedby={`attention-detail-${index}`}>
          <legend className="sr-only">{q.header || `Question ${index + 1}`}</legend><p className="attention-detail" id={`attention-detail-${index}`}>{q.question}</p>
          <div className="attention-options">{options.map((option, i) => <label key={i} className="attention-option checkbox-label">
            <input className="attention-radio" type="radio" name={`question-${index}`} id={`question-${index}-option-${i}`} value={option.label}
              data-option-index={i} checked={answer.selected === i} disabled={locked} onChange={() => onAnswer(q.id, {selected: i}, true)}/>
            <span className="attention-number" aria-hidden="true">{i + 1}</span><span className="attention-option-copy"><span className="attention-option-line">
              <span className="attention-option-label">{option.label.replace(recommendedSuffix, '')}</span>{recommendedSuffix.test(option.label) && <span className="attention-badge">Recommended</span>}
            </span>{option.description && <span className="attention-description">{option.description}</span>}</span>
          </label>)}
          {!q.choices_only && <div className={'attention-custom' + (!options.length ? ' attention-custom-block' : '') + (custom ? ' attention-custom-active' : '')}>
            {!!options.length && <input className="attention-radio" type="radio" name={`question-${index}`} value="" data-other="true"
              checked={answer.selected === 'custom'} aria-label="Other: write your own answer" disabled={locked} onChange={() => onAnswer(q.id, {selected: 'custom'}, false, true)}/>}
            <label className="attention-custom-label" htmlFor={`question-${index}-answer`} title="Your answer">
              {!!options.length && <span className="attention-number" aria-hidden="true">{options.length + 1}</span>}<span className="sr-only">Your answer</span>
            </label><div className="attention-answer-field"><div className="attention-answer-mirror" aria-hidden="true">{answer.custom + '\n'}</div>
              <textarea className="attention-answer" id={`question-${index}-answer`} rows={1} maxLength={8192} disabled={locked}
                placeholder={options.length ? 'Other: write your own answer…' : 'Write your answer…'} value={answer.custom} aria-describedby={`attention-detail-${index}`}
                onChange={() => {}} onInput={event => onAnswer(q.id, {selected: 'custom', custom: event.currentTarget.value})}
                onFocus={() => onAnswer(q.id, {selected: 'custom'})} onKeyDown={onKeydown}
                onCompositionStart={() => onComposition(true)} onCompositionEnd={() => onComposition(false)}/>
            </div>
          </div>}
          </div>
        </fieldset>;
      })}
    </div><footer className="attention-footer" id="attention-question-footer" hidden={draft.collapsed}>
      <div className="attention-pager"><button type="button" className="attention-icon quiet" data-attention-page="-1" aria-label="Previous question"
        disabled={locked || !valid || draft.page === 0} onClick={() => onPage(-1)}>‹</button>
        <span className="attention-progress" aria-live="polite">{valid ? `${draft.page + 1} / ${questions.length}` : ''}</span>
        <button type="button" className="attention-icon quiet" data-attention-page="1" aria-label="Next question" disabled={locked || !valid || last} onClick={() => onPage(1)}>›</button>
      </div><button type={last ? 'submit' : 'button'} className="attention-continue primary" data-attention-continue="" disabled={locked || !valid}
        onClick={last ? undefined : onContinue}>{last ? 'Submit answers' : 'Next'}</button>
    </footer>
  </form>;
}
