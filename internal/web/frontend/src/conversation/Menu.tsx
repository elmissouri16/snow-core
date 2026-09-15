import type {ReactNode} from 'react';
import type {Controller, MenuKind} from './controller';
import {Icon} from './Icons';
import type {Glyph} from './Icons';
import {policies, presentation, telemetryView} from './model';
type Props = {c: Controller; menu: {kind: MenuKind; pane: string; query: string; trigger: HTMLElement}};
function Row({label, rowKey = label, disabled = false, checked, value, glyph, next, action, provider, model}: {
  label: string; rowKey?: string; disabled?: boolean; checked?: boolean; value?: string; glyph?: Glyph; next?: boolean;
  action(): void; provider?: string; model?: string;
}) {
  return <button type="button" className="snow-menu-row" data-menu-key={rowKey} role={checked === undefined ? 'menuitem' : 'menuitemradio'} aria-checked={checked} disabled={disabled} title={provider ? `${label} · ${provider} / ${model}` : label + (value ? ` · ${value}` : '')} data-model-provider={provider} data-model-id={model} onClick={action}>
    {(glyph || rowKey === 'back') && <Icon name={glyph || 'back'} />}<span className="snow-menu-row-label">{label}</span>{value && <span className="snow-menu-row-value">{value}</span>}{next && <Icon name="next" />}{checked !== undefined && <span className="snow-menu-check" aria-hidden="true"><span style={{visibility: checked ? 'visible' : 'hidden'}}><Icon name="check" /></span></span>}
  </button>;
}
const Note = ({children}: {children: ReactNode}) => <p className="snow-menu-note">{children}</p>;
const Separator = () => <div className="snow-menu-separator" role="separator" />;
function Telemetry({c, menu}: Props) {
  const view = telemetryView(c.snapshot), cost = presentation(c.snapshot.telemetry);
  return <><h2>Context &amp; usage</h2>{menu.pane === 'telemetry-details' ? <>
    <Row label="Back" rowKey="back" action={() => c.pane('root')} /><Note>Context estimates are approximate; last reported input is measured. An unknown context window cannot give a usage percentage. Unknown values are not zero.</Note>
    <dl><div className="session-cost"><dt>Recorded cost estimate</dt><dd data-workflow-cost="" data-known={String(cost.known)}>{cost.value}</dd><p className="session-cost-note">May exclude unpriced requests; aggregate currency coverage is not verified. An estimate, not a billing charge. Unknown is not zero.</p></div></dl>
  </> : <>
    <dl><div data-menu-key="workflowContext"><dt data-workflow-context-label="">{view.contextLabel}</dt><dd data-workflow-context="">{view.context}</dd></div><div data-menu-key="workflowUsage"><dt>Usage</dt><dd data-workflow-usage="">{view.usage}</dd></div><div data-menu-key="workflowCost"><dt>Cost estimate</dt><dd data-workflow-cost="" data-known={String(view.knownCost)}>{view.cost}</dd></div></dl>
    <Row label="Details" rowKey="telemetry-details" next action={() => c.pane('telemetry-details')} />
  </>}</>;
}
function PolicyMenu({c, menu}: Props) {
  const instance = c.snapshot.instance_id;
  return <><p className="snow-menu-group-label">{menu.pane === 'permission-help' ? 'Permission details' : 'Session permissions'}</p>{menu.pane === 'permission-help' ? <>
    <Row label="Back" rowKey="back" action={() => c.pane('root')} /><Note>Ask requests approval when required. Deny rejects non-read tools without asking. Allow skips permission prompts. Read-risk tools do not require approval.</Note><Note>These are approval policies, not sandbox modes. Tools run with the host account’s privileges. Changes affect this session only; existing session decisions still apply in Ask.</Note>
  </> : <>
    {Object.entries(policies).map(([mode, label]) => <Row key={mode} rowKey={mode} label={label} checked={c.snapshot.permission_mode === mode} disabled={!c.canSetPolicy()} action={() => c.choosePolicy(mode, menu.trigger, instance)} />)}
    <Note>Session only · not a sandbox.</Note><Row label="Details" rowKey="permission-help" action={() => c.pane('permission-help')} />{!c.canSetPolicy() && <Note>Policy changes require a connected, verified, idle session.</Note>}
  </>}</>;
}
function ModeMenu({c}: Props) {
  const snapshot = c.snapshot, instance = snapshot.instance_id;
  return <><p className="snow-menu-group-label">Collaboration mode</p>{[['default', 'Default'], ['plan', 'Plan Mode']].map(([mode, label]) => <Row key={mode} label={label} rowKey={mode} checked={snapshot.mode === mode} disabled={!c.canChange() || !['default', 'plan'].includes(snapshot.mode || '')} action={() => {
    if (!c.valid(instance) || !c.canChange()) return;
    if (c.snapshot.mode === mode) c.dismiss(); else void c.mutate(instance, 'mode', {mode});
  }} />)}<Note>{snapshot.mode === 'plan' ? 'Plan Mode: investigate and plan, not implement.' : snapshot.mode === 'default' ? 'Default mode is active in the runtime.' : 'Authoritative mode is unavailable.'}</Note></>;
}
function SessionMenu({c, menu}: Props) {
  const instance = c.snapshot.instance_id, choices = c.choices;
  if (menu.pane === 'root') {
    const actions = c.runtimeActions();
    return <>
      <Row label="New conversation" rowKey="new" glyph="new" disabled={!c.canSwitch()} action={() => { if (c.valid(instance)) void c.switchTo('', menu.trigger); }} />
      <Row label="Rename conversation" rowKey="rename" glyph="rename" disabled={!c.canChange()} action={() => { if (c.valid(instance)) c.openRename(menu.trigger); }} />
      <Separator /><Row label="Switch conversation" rowKey="sessions" next disabled={!c.canSwitch()} action={() => c.pane('sessions')} />
      {actions.length > 0 && <Separator />}{actions.map(({key, label, source}) => <Row key={key} rowKey={key} label={label} glyph={key} disabled={source.disabled} action={() => {
        if (!c.valid(instance) || !source.isConnected || source.disabled || !c.runtimeActions().some(action => action.key === key && action.source === source)) return;
        c.dismiss(); source.click();
      }} />)}
    </>;
  }
  return <><Row label="Conversations" rowKey="back" action={() => c.pane('root')} />{!choices ? <>
    <Row label={c.loading ? 'Loading host choices…' : 'Load conversations & models'} rowKey="load" glyph="refresh" disabled={!c.canChange()} action={() => void c.load()} /><p className="snow-menu-note" data-workflow-load-status="" role="status">{c.loadError || (c.loading ? 'Contacting the host…' : 'Loading may contact provider model discovery.')}</p>
  </> : <>
    <div className="snow-menu-groups">{!choices.sessions.some(item => item.session_id === c.snapshot.session_id) && <Row label={c.snapshot.session_name || 'Untitled conversation'} rowKey="current" checked action={() => c.dismiss()} />}
      {choices.sessions.map(item => <Row key={item.session_id} rowKey={item.session_id} label={item.name || 'Untitled conversation'} checked={item.session_id === c.snapshot.session_id} disabled={!c.canSwitch()} action={() => { if (c.valid(instance)) void c.switchTo(item.session_id, menu.trigger); }} />)}
    </div>{choices.sessions_available === false ? <Note>Saved conversations are unavailable on this worker.</Note> : !choices.sessions.length && <Note>No other saved conversations.</Note>}{choices.sessions_truncated && <Note>Some conversations are omitted from this bounded list.</Note>}
  </>}</>;
}
function ModelRows({c, menu}: Props) {
  const choices = c.choices, snapshot = c.snapshot, instance = snapshot.instance_id;
  const query = menu.query.trim().toLocaleLowerCase();
  const models = choices?.models.filter(item => [item.provider, item.id, item.name].some(value => value.toLocaleLowerCase().includes(query))) || [];
  return <>
    <p className="snow-menu-note" data-workflow-load-status="" role="status">{c.loading ? 'Loading host models…' : c.loadError || (!choices ? 'Model discovery requires a connected, verified, idle session.' : '')}</p>
    {choices && <><div className="snow-menu-groups" role="menu" aria-label="Available models">
      {[...new Set(models.map(item => item.provider))].map(provider => <div key={provider} className="snow-menu-group" role="group" aria-label={provider}><p className="snow-menu-group-label" title={provider}>{provider}</p>
        {models.filter(item => item.provider === provider).map(item => <Row key={JSON.stringify([item.provider, item.id])} rowKey={JSON.stringify([item.provider, item.id])} label={item.name || item.id} provider={item.provider} model={item.id} checked={item.provider === snapshot.provider && item.id === snapshot.model} disabled={!c.canChange()} action={() => {
          if (!c.valid(instance) || !c.canChange() || !c.choices?.models.some(model => model.provider === item.provider && model.id === item.id)) return;
          if (c.snapshot.provider === item.provider && c.snapshot.model === item.id) c.dismiss(); else void c.mutate(instance, 'model', {provider: item.provider, model: item.id});
        }} />)}
      </div>)}
    </div>{!models.length && <p className="snow-menu-note" role="status">{query ? 'No models match your search.' : 'No host models discovered.'}</p>}{choices.models_partial && <Note>Some provider discovery was unavailable.</Note>}{choices.models_truncated && <Note>Some models are omitted from this bounded list.</Note>}</>}
  </>;
}
export function Menu(props: Props) {
  const {c, menu} = props;
  return <>
    {menu.kind === 'model' && <div className="snow-menu-header model-search-header"><label className="sr-only" htmlFor="model-picker-search">Search models by name, ID or provider</label><input className="model-search" type="search" id="model-picker-search" placeholder="Search models…" maxLength={256} autoComplete="off" spellCheck={false} data-model-search="" data-menu-autofocus="" value={menu.query} onChange={event => c.search(event.currentTarget.value)} /></div>}
    {/* The host finds this direct wrapper and must never wrap/reconcile JSX children. */}
    <div className="snow-menu-content" tabIndex={-1}>
      {menu.kind === 'telemetry' ? <Telemetry {...props} /> : menu.kind === 'permission-policy' ? <PolicyMenu {...props} /> : menu.kind === 'mode' ? <ModeMenu {...props} /> : menu.kind === 'session' ? <SessionMenu {...props} /> : <ModelRows {...props} />}
    </div>
    {menu.kind === 'model' && <div className="snow-menu-footer" role="menu" aria-label="Model discovery"><Row label={c.loading ? c.choices ? 'Refreshing models…' : 'Loading models…' : c.loadError ? 'Retry loading models' : 'Refresh models'} rowKey="load" glyph="refresh" disabled={!c.canChange()} action={() => void c.load()} /></div>}
  </>;
}
