import {createPortal} from 'react-dom';
import {Icon} from '../conversation/Icons';
import {budget, sameScope, terminal} from './model.ts';
import {canCompose, ready} from './controller.tsx';
import type {Actions, View} from './controller.tsx';

/** Rendering is passive: only explicit event handlers inspect or admit work. */
export function GoalPanel({view, actions}: {view: View; actions: Actions}) {
  const {goal, ui, draft, confirmation, inspected, snapshot} = view;
  const safe = ready(view), scoped = sameScope(inspected, snapshot, goal);
  const create = !goal?.goal_id || terminal(goal), resume = !!goal?.goal_id && !terminal(goal);
  const notice = view.error || (view.invalid ? 'Could not verify the goal projection. Controls are disabled.'
    : view.read ? 'Inspecting the exact saved goal; no work is being started…'
    : view.committing ? 'Requesting one goal run…'
    : goal?.running ? 'One goal run is active across serial turns. No frontend loop or automatic restart is scheduled.'
    : create && budget(draft.budget) === false ? 'Enter a positive whole-token budget no larger than 9007199254740991, or leave it empty.'
    : snapshot?.mode === 'plan' ? 'Goal execution is unavailable in Plan mode. Inspection is read-only.'
    : !scoped ? 'Refresh the inspected scope before authorizing a goal run.'
    : !ui?.safe ? 'Goal execution needs an idle connected runtime without pending/review queue items or another action.'
    : 'Inspection is read-only. Nothing starts until you explicitly authorize and confirm.');
  const scope = inspected ? `Session: ${inspected.goal.session_id}\nBranch: ${inspected.goal.branch_id}\nTip: ${inspected.goal.tip_id || '(empty history)'}\nGoal: ${inspected.goal.goal_id || '(confirmed absent)'}${scoped ? '' : '\nChanged: refresh inspection before continuing.'}` : 'Not inspected. Refresh before authorizing a goal run.';
  const number = (value: number | null | undefined) => value == null ? 'No token budget' : value.toLocaleString();
  return <>
    {createPortal(<button type="button" className="task-menu-trigger goal-mode-toggle" data-goal-toggle aria-label="Goal" aria-pressed={draft.enabled} title={draft.enabled ? 'Goal mode · Submit this composer to start a goal run. Click to return to a normal message.' : canCompose(view) ? 'Goal mode · Type an objective in the composer, then submit to start.' : goal?.goal_id && !terminal(goal) ? 'An existing goal is saved. Open Goal details to inspect or resume it.' : 'Goal mode requires an idle Default-mode session.'} hidden={!ui?.supported} disabled={view.submitting || view.committing || (!draft.enabled && !canCompose(view))} onClick={() => actions.toggle(view)}>
      <Icon name="goals" /><span>Goal</span>
    </button>, view.launcher)}
    {/* The conversation menu retains a secondary details/resume entry point. */}
    <button type="button" data-goals-open data-goals-available={!!ui?.supported} hidden disabled={!ui?.readable || view.invalid || view.committing || view.submitting} onClick={event => actions.open(view, event.currentTarget)}>Goal details</button>
    {createPortal(<div data-live-goal-status className="goal-inline-status" hidden={!goal?.goal_id || terminal(goal)}>
      <span data-goal-status-badge>{goal?.running ? 'Running' : goal?.deferred ? 'Run stopped' : goal?.status.replaceAll('_', ' ')}</span>
      <span data-goal-summary-label className="goal-inline-objective" title={goal?.objective}>{goal?.objective}</span>
      <span data-goal-summary-usage>{goal?.budget_remaining == null ? '' : `${goal.budget_remaining.toLocaleString()} tokens left`}</span>
      <button type="button" className="quiet" data-goal-details disabled={!ui?.readable || view.invalid || view.committing} onClick={event => actions.open(view, event.currentTarget)}>Details</button>
    </div>, view.statusHost)}
    <dialog ref={view.dialog} id="goals-dialog" className="goals-dialog runtime-dialog" aria-labelledby="goals-heading" aria-describedby="goals-boundary"
      onCancel={event => {event.preventDefault(); actions.close(view);}} onClose={() => actions.cancel(view)}
      onCompositionStart={() => actions.compose(view, true)} onCompositionEnd={() => actions.compose(view, false)}>
      <div className="dialog-heading">
        <h2 id="goals-heading">Thread Goal</h2>
        <button type="button" className="quiet" data-goal-inspect disabled={!ui?.readable || !!view.read || !!confirmation || view.committing} onClick={() => void actions.inspect(view)}>Refresh goal</button>
        {/* Stop belongs to app.js's document delegation: never dispatch a second request here. */}
        <button type="button" className="quiet" data-runtime-abort hidden={!(ui?.showStop ?? ui?.canStop)} disabled={!ui?.canStop} title={ui?.stopTitle || ''}>{ui?.stopLabel || 'Stop goal run'}</button>
        <button ref={view.closeButton} type="button" className="quiet icon-button" data-goals-close aria-label="Close goals" title="Close" onClick={() => actions.close(view)}>
          <svg className="icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg>
        </button>
      </div>
      <div className="runtime-dialog-body">
        <p id="goals-boundary" className="fine">Inspecting a goal is read-only. Starting or resuming is explicit: one goal run may use multiple serial turns, write files, and run commands when permitted. It uses the current model and session permissions. These are permission gates, not a sandbox.</p>
        <p data-goal-notice className="fine" role="status" aria-live="polite">{notice}</p>
        <dl className="goal-facts">
          <dt>Reviewed scope</dt><dd data-goal-scope>{scope}</dd>
          <dt>Objective</dt><dd data-goal-objective>{goal?.objective || 'No saved goal'}</dd>
          <dt>Goal status</dt><dd data-goal-state>{goal ? `${goal.status.replaceAll('_', ' ')}${goal.running ? ' · run active' : ' · no active run'}` : 'Unavailable'}</dd>
          <dt>Deferred</dt><dd data-goal-deferred>{goal?.deferred ? 'Yes · not automatically scheduled' : 'No'}</dd>
          <dt>Tokens used</dt><dd data-goal-tokens>{number(goal?.tokens_used)}</dd>
          <dt>Token budget</dt><dd data-goal-budget>{number(goal?.token_budget)}</dd>
          <dt>Budget remaining</dt><dd data-goal-remaining>{number(goal?.budget_remaining)}</dd>
          <dt>Current model / permissions</dt><dd data-goal-authority>{snapshot ? `${snapshot.provider || 'Unknown'} / ${snapshot.model || 'Unknown'} · ${snapshot.permission_mode || 'unknown permissions'} · ${snapshot.mode || 'unknown mode'}` : 'Unknown'}</dd>
        </dl>
        <p data-goal-blocked className="fine" hidden={!goal?.blocked_reason}>{goal?.blocked_reason || ''}</p>
        <div data-goal-create-fields hidden={!create || !!confirmation}>
          <label htmlFor="goal-token-budget">Optional token budget</label>
          <input id="goal-token-budget" data-goal-token-budget type="text" inputMode="numeric" maxLength={16} autoComplete="off" placeholder="No budget" value={draft.budget} onChange={event => actions.draft(view, 'budget', event.currentTarget.value)} />
          <p className="fine">Optional budget for the next new goal. Write its objective in the composer with Goal enabled; submitting starts the run using the current model and permissions. This panel does not start a new goal.</p>
        </div>
        <div className="dialog-actions">
          <button ref={view.startButton} type="button" className="button" data-goal-write hidden={!create || !!confirmation} disabled={!canCompose(view) || budget(draft.budget) === false} onClick={() => actions.write(view)}>Write goal in composer</button>
          <button ref={view.resumeButton} type="button" className="button" data-goal-resume hidden={!resume || !!confirmation} disabled={!safe || goal?.budget_remaining === 0} onClick={() => actions.prepare(view, 'goal-resume')}>Review resume…</button>
        </div>
        <section data-goal-confirmation className="goal-confirmation" aria-labelledby="goal-confirm-heading" hidden={!confirmation}>
          <h3 id="goal-confirm-heading">Authorize this goal run?</h3>
          <p data-goal-confirm-target>{confirmation?.target || ''}</p>
          <p>This starts one correlated goal run, potentially across multiple serial turns. Files and commands may be changed when the current permissions allow them. Stop cancels the whole run, including gaps between turns. Your unsent message draft is kept. A finished run does not necessarily mean the goal is complete.</p>
          <label className="goal-consent"><input ref={view.consentInput} type="checkbox" data-goal-consent checked={view.consent} onChange={event => actions.consent(view, event.currentTarget.checked)} /> I authorize this goal run with the model and permissions shown above.</label>
          <div className="dialog-actions">
            <button type="button" className="quiet" data-goal-cancel disabled={view.committing} onClick={() => actions.cancel(view)}>Cancel</button>
            <button type="button" className="button primary" data-goal-confirm disabled={!safe || !confirmation || !view.consent} onClick={() => void actions.commit(view)}>{confirmation?.action === 'goal-resume' ? 'Resume goal run' : 'Start goal run'}</button>
          </div>
        </section>
      </div>
    </dialog>
  </>;
}
