import {useRef} from 'react';
import type {ReactNode} from 'react';
import type {Controller, MenuKind} from './controller';
import {contextMeter, isPolicy, policies, statusLabel} from './model';
import {Icon} from './Icons';
type Props = {c: Controller};
function Trigger({c, kind, children, className = '', label, title = label, disabled = false}: Props & {kind: MenuKind; children: ReactNode; className?: string; label: string; title?: string; disabled?: boolean}) {
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = c.menu?.trigger === trigger.current ? c.menu : null;
  return <button ref={trigger} type="button" className={`task-menu-trigger ${className}`} {...{[`data-${kind}-menu`]: ''}} aria-haspopup={kind === 'model' || kind === 'telemetry' ? 'dialog' : 'menu'} aria-expanded={!!menu} aria-controls={menu?.panel.id} aria-label={label} title={title} disabled={disabled} onClick={event => c.showMenu(kind, event.currentTarget)}>{children}</button>;
}
export function Heading({c}: Props) {
  return <header className="workspace-heading live-header"><div className="conversation-title"><strong data-live-title="">{c.snapshot.session_name || 'New conversation'}</strong><span className="pill" id="live-status" role="status">{statusLabel(c.snapshot)}</span></div><div className="live-controls">
    {c.workflow && <Trigger c={c} kind="session" className="quiet session-menu-trigger" label="Conversation actions"><svg className="icon" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><circle cx="5" cy="12" r="1.5" /><circle cx="12" cy="12" r="1.5" /><circle cx="19" cy="12" r="1.5" /></svg></Trigger>}
    <button type="button" className="quiet icon-button" data-runtime-close="" aria-label="Close runtime" title="Close runtime" disabled={c.controls.closeDisabled ?? true}><Icon name="power" /></button>
    <button type="button" className="quiet icon-button" data-inspector-toggle="" aria-controls="project-inspector" aria-expanded={!!c.controls.inspectorExpanded} aria-label="Files and changes" title="Files and changes"><Icon name="inspector" /></button>
  </div></header>;
}
export function Leading({c}: Props) {
  const mode = c.snapshot.mode, modeLabel = mode === 'plan' ? 'Plan Mode' : mode === 'default' ? 'Default' : 'Mode unknown';
  const policy = c.controls.verified && ['idle', 'running', 'permission', 'input'].includes(c.snapshot.status) && isPolicy(c.snapshot.permission_mode) ? policies[c.snapshot.permission_mode] : 'Unknown';
  return <>
    {c.policy && <Trigger c={c} kind="permission-policy" className="permission-policy-trigger" label={`Session permissions: ${policy}`} title={`Session permissions: ${policy} · not a sandbox`} disabled={!c.canSetPolicy()}><Icon name="shield" /><span data-permission-policy-label="">{policy}</span><Icon name="chevron" /></Trigger>}
    {c.workflow ? <Trigger c={c} kind="mode" label={`Collaboration mode: ${modeLabel}`}><span data-mode-label="">{modeLabel}</span><span className="mode-short" data-mode-short="" aria-hidden="true">{mode === 'plan' ? 'Plan' : mode === 'default' ? 'Default' : 'Mode'}</span><Icon name="chevron" /></Trigger> : <span className="workspace-chip" title={c.projectPath}>{c.projectName}</span>}
  </>;
}
export function Trailing({c}: Props) {
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = c.menu?.trigger === trigger.current ? c.menu : null;
  const {known, percent} = contextMeter(c.snapshot.telemetry);
  if (!c.workflow) return <span className="composer-model" id="live-model">{[c.snapshot.provider, c.snapshot.model].filter(Boolean).join(' / ')}</span>;
  return <>
    <Trigger c={c} kind="model" className="model-menu-trigger" label={`Choose model: ${c.modelName()}`} title={`${c.snapshot.provider || 'Unknown provider'} / ${c.snapshot.model || 'Unknown model'}`}><Icon name="model" /><span id="live-model">{c.modelName()}</span><Icon name="chevron" /></Trigger>
    <button ref={trigger} type="button" className="task-menu-trigger context-menu-trigger" data-telemetry-menu="" data-unknown={String(!known)} aria-haspopup="dialog" aria-expanded={!!menu} aria-controls={menu?.panel.id} aria-label={known ? `Context and usage: ${Math.round(percent)}% context used` : 'Context and usage: context unknown'} title="Context & usage" onClick={event => c.showMenu('telemetry', event.currentTarget)}><svg className="context-ring" viewBox="0 0 20 20" aria-hidden="true"><circle className="context-ring-track" cx="10" cy="10" r="8" pathLength="100" /><circle className="context-ring-value" cx="10" cy="10" r="8" pathLength="100" style={{strokeDasharray: `${percent} 100`}} /></svg></button>
  </>;
}
export function Dialogs({c}: Props) {
  return <>
    {c.policy && <dialog ref={node => { c.policyDialog = node; }} id="workflow-permission-dialog" className="folder-dialog permission-policy-dialog" aria-labelledby="workflow-permission-title" aria-describedby="workflow-permission-description" onClose={() => { c.policyTarget = null; c.consent = false; c.publish(); }} onCancel={event => { event.preventDefault(); c.cancelPolicy(); }}>
      <div className="dialog-heading"><h2 id="workflow-permission-title">Allow tools without approval?</h2><button type="button" className="quiet" data-policy-cancel="" aria-label="Cancel permission change" onClick={c.cancelPolicy}><Icon name="close" /></button></div>
      <p id="workflow-permission-description">Allow skips permission prompts for this session. Tools can modify files and run commands with your host account’s privileges. Snow does not provide a sandbox. Existing effects cannot be undone by switching back to Ask.</p>
      <label className="checkbox-label"><input type="checkbox" data-policy-ack="" checked={c.consent} onChange={event => { c.consent = event.currentTarget.checked; c.publish(); }} /> I understand the risks and want to enable Allow for this session.</label>
      <div className="dialog-actions"><button type="button" className="quiet" data-policy-cancel="" onClick={c.cancelPolicy}>Keep current policy</button><button type="button" className="button danger" data-policy-confirm="" disabled={!c.consent || !c.canSetPolicy()} onClick={() => void c.confirmPolicy()}>Enable Allow</button></div>
    </dialog>}
    {c.workflow && <>
      <dialog ref={node => { c.switchDialog = node; }} id="workflow-switch-dialog" className="folder-dialog" aria-labelledby="workflow-switch-title" aria-describedby="workflow-switch-description" onClose={() => { c.switchTarget = null; }} onCancel={event => { event.preventDefault(); c.closeWorkflow('switch'); }}>
        <div className="dialog-heading"><h2 id="workflow-switch-title">Stop and switch conversation?</h2><button type="button" className="quiet" data-workflow-cancel="" aria-label="Cancel switching conversation" onClick={() => c.closeWorkflow('switch')}>×</button></div>
        <p id="workflow-switch-description">A turn or approval is active. Switching stops that work and opens the selected conversation in this project. Saved history and your unsent draft remain. Nothing is sent in the new conversation.</p>
        <div className="dialog-actions"><button type="button" className="quiet" data-workflow-cancel="" onClick={() => c.closeWorkflow('switch')}>Keep working</button><button type="button" className="button danger" data-workflow-switch-confirm="" disabled={!c.canSwitch()} onClick={() => void c.confirmSwitch()}>Stop and switch</button></div>
      </dialog>
      <dialog ref={node => { c.renameDialog = node; }} id="workflow-rename-dialog" className="folder-dialog" aria-labelledby="workflow-rename-title" onCancel={event => { event.preventDefault(); c.closeWorkflow('rename'); }}>
        <form data-workflow-rename-form="" onSubmit={event => { event.preventDefault(); void c.rename(); }}>
          <div className="dialog-heading"><h2 id="workflow-rename-title">Rename current conversation</h2><button type="button" className="quiet" data-workflow-cancel="" aria-label="Cancel renaming conversation" onClick={() => c.closeWorkflow('rename')}>×</button></div>
          <label htmlFor="workflow-name">Conversation name</label><input ref={node => { c.nameInput = node; }} id="workflow-name" name="name" maxLength={128} required autoComplete="off" value={c.renameDraft} onChange={event => { event.currentTarget.setCustomValidity(''); c.renameDraft = event.currentTarget.value; c.publish(); }} />
          <p className="fine">Only the current conversation’s display name changes.</p><div className="dialog-actions"><button type="button" className="quiet" data-workflow-cancel="" onClick={() => c.closeWorkflow('rename')}>Cancel</button><button type="submit" className="primary" disabled={!c.canChange()}>Save name</button></div>
        </form>
      </dialog>
    </>}
  </>;
}
