import type {Controller} from "./controller.tsx";
import {stateLabel} from "./model.ts";

export function MCPPanel({controller: c}: {controller: Controller}) {
  const readable = !!c.ui?.readable;
  const servers = c.inventory?.servers ?? [];
  const notice = c.notice || (c.reading ? "Reading MCP server status from the worker…" : !c.inventory ? "Refresh to read server status." : servers.length === 0 ? "No MCP servers are configured for this workspace. Configure them in the host's mcp settings; browsing does not start a server." : `${servers.filter(server => server.connected).length} of ${servers.length} configured server${servers.length === 1 ? "" : "s"} connected.`);
  return <>
    <button type="button" className="quiet" data-mcp-open="" aria-haspopup="dialog" aria-controls="mcp-dialog" disabled={!readable} onClick={event => c.open(event.currentTarget)}>MCP servers…</button>
    <dialog ref={c.setDialog} id="mcp-dialog" className="mcp-dialog runtime-dialog" aria-labelledby="mcp-heading" aria-describedby="mcp-boundary" onCancel={event => { event.preventDefault(); c.close(); }}>
      <div className="dialog-heading">
        <h2 id="mcp-heading">MCP servers</h2>
        <button type="button" className="quiet" data-mcp-refresh="" disabled={!readable || c.reading} onClick={() => void c.inspect()}>Refresh status</button>
        <button type="button" className="quiet icon-button" data-mcp-close="" aria-label="Close MCP servers" title="Close" onClick={c.close}><svg className="icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg></button>
      </div>
      <div className="runtime-dialog-body">
        <p id="mcp-boundary" className="fine">Status of the MCP servers this worker negotiated, as <code>/mcp</code> shows in the terminal. This is read-only: servers are configured on the host and run with your host account's privileges.</p>
        <p className="fine" data-mcp-notice="" role="status" aria-live="polite">{notice}</p>
        {c.inventory?.limited && <p className="fine">The list was truncated to the first 64 servers.</p>}
        <ul className="auth-providers">{servers.map(server => <li key={server.id} className="auth-provider" data-mcp-server={server.id}>
          <div className="auth-provider-head"><h3>{server.id}</h3><span className="auth-state" data-state={server.connected ? "configured" : server.state === "failed" ? "invalid" : "missing"}>{stateLabel(server)}</span></div>
          <ul className="auth-provider-meta">
            {server.transport && <li>{server.transport}</li>}
            {server.server_name && <li>{server.server_name}{server.server_version ? ` ${server.server_version}` : ""}</li>}
            {server.protocol_version && <li>protocol {server.protocol_version}</li>}
            {server.connected && <li>{server.tool_count} tool{server.tool_count === 1 ? "" : "s"}</li>}
            {server.capabilities.length > 0 && <li>{server.capabilities.join(", ")}</li>}
            {server.cached && <li>cached catalog</li>}
          </ul>
          {server.message && <p className="fine">{server.message}</p>}
        </li>)}</ul>
      </div>
    </dialog>
  </>;
}
