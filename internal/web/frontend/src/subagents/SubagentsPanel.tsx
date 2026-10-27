import type {Controller} from "./controller.tsx";
import {active, duration, stateTone, statusLabel} from "./model.ts";

export function SubagentsPanel({controller: c}: {controller: Controller}) {
  const readable = !!c.ui?.readable;
  const inventory = c.inventory;
  const agents = inventory?.agents ?? [];
  const now = Date.now() / 1000;
  const notice = c.notice || (c.reading ? "Reading the subagent fleet from the worker…" : !inventory ? "Refresh to read subagent status." : agents.length === 0 ? "No subagents have been spawned in this session. The model starts them itself when a task calls for parallel work; they run with your host account's privileges." : `${inventory.running} running · ${inventory.queued} queued · ${inventory.terminal} finished · limit ${inventory.concurrent_limit} concurrent, ${inventory.agent_limit} total.`);
  return <>
    <button type="button" className="quiet" data-subagents-open="" aria-haspopup="dialog" aria-controls="subagents-dialog" disabled={!readable} onClick={event => c.open(event.currentTarget)}>Subagents…</button>
    <dialog ref={c.setDialog} id="subagents-dialog" className="mcp-dialog runtime-dialog" aria-labelledby="subagents-heading" aria-describedby="subagents-boundary" onCancel={event => { event.preventDefault(); c.close(); }}>
      <div className="dialog-heading">
        <h2 id="subagents-heading">Subagents</h2>
        <button type="button" className="quiet" data-subagents-refresh="" disabled={!readable || c.reading} onClick={() => void c.inspect()}>Refresh</button>
        <button type="button" className="quiet icon-button" data-subagents-close="" aria-label="Close subagents" title="Close" onClick={c.close}><svg className="icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg></button>
      </div>
      <div className="runtime-dialog-body">
        <p id="subagents-boundary" className="fine">Live view of the child agents this worker runs, as <code>/agent</code> shows in the terminal. Read-only: the browser never starts, steers or stops a subagent; Stop on the main turn cancels the whole run.</p>
        <p className="fine" data-subagents-notice="" role="status" aria-live="polite">{notice}</p>
        {inventory?.truncated && <p className="fine">The list was truncated.</p>}
        <ul className="auth-providers">{agents.map(agent => <li key={agent.path} className="auth-provider" data-subagent={agent.path} style={{marginLeft: `${Math.min(agent.depth, 6) * 12}px`}}>
          <div className="auth-provider-head"><h3>{agent.nickname || agent.role || agent.path}</h3><span className="auth-state" data-state={stateTone(agent.status)}>{statusLabel(agent.status)}</span></div>
          <ul className="auth-provider-meta">
            <li>{agent.path}</li>
            {agent.role && agent.nickname && <li>{agent.role}</li>}
            {agent.model && <li>{agent.provider ? `${agent.provider} / ` : ""}{agent.model}{agent.thinking ? ` · thinking ${agent.thinking}` : ""}</li>}
            {duration(agent, now) && <li>{active(agent.status) ? "running for " : "took "}{duration(agent, now)}</li>}
          </ul>
          {agent.error && <p className="error">{agent.error}</p>}
          {agent.result && !agent.error && <p className="fine">{agent.result}</p>}
        </li>)}</ul>
      </div>
    </dialog>
  </>;
}
