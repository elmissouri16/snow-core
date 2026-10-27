import type {Controller} from "./controller.tsx";
import {describeStatus, orderedProviders, stateLabel, terminal, usableMethods} from "./model.ts";
import type {AuthJob, AuthProvider} from "./model.ts";

function Close() {
  return <svg className="icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg>;
}

function Job({job, c}: {job: AuthJob; c: Controller}) {
  const step = c.latestInstruction;
  const done = terminal(job);
  const title = job.state === "running" ? `Signing in to ${job.provider_id}…` : job.state === "completed" ? `Signed in to ${job.provider_id}` : job.state === "canceled" ? "Sign-in canceled" : "Sign-in failed";
  return <section className="auth-job" data-state={job.state} aria-live="polite" aria-label="Sign-in progress">
    <div className="auth-provider-head"><h3>{title}</h3>{job.state === "running" && <span className="auth-state">Waiting for the provider</span>}</div>
    {step?.user_code && <>
      <p className="fine">{step.message || "Enter this code at the provider to finish signing in."}</p>
      <p className="auth-job-code" data-auth-user-code="">{step.user_code}</p>
    </>}
    {step?.url && <p className="auth-job-url">{step.user_code ? "Open " : (step.message || "Open this link to continue") + ": "}<a href={step.url} target="_blank" rel="noopener noreferrer">{step.url}</a></p>}
    {!step && job.state === "running" && <p className="fine">Contacting the provider…</p>}
    {job.error && <p className="error" role="alert">{job.error}</p>}
    {job.status && done && <p className="fine">{describeStatus(job.status)}</p>}
    <div className="dialog-actions">
      {!done && <button type="button" className="quiet" data-auth-cancel="" disabled={c.starting} onClick={() => void c.cancel()}>Cancel sign-in</button>}
      {done && <button type="button" className="quiet" data-auth-dismiss-job="" onClick={c.dismissJob}>Dismiss</button>}
    </div>
  </section>;
}

function Provider({provider, c}: {provider: AuthProvider; c: Controller}) {
  const methods = usableMethods(provider, c.api.localOrigin);
  const terminalOnly = provider.methods.filter(method => !methods.some(entry => entry.id === method.id));
  const enabled = c.canAct();
  const signedIn = provider.status.state === "configured" || provider.status.state === "expired" || provider.status.state === "invalid";
  const confirming = c.confirmLogout === provider.id;
  return <li className="auth-provider" data-auth-provider={provider.id}>
    <div className="auth-provider-head"><h3>{provider.display_name || provider.id}{provider.required ? "" : " (optional)"}</h3><span className="auth-state" data-state={provider.status.state}>{stateLabel(provider.status.state)}</span></div>
    <p className="fine" data-auth-status="">{describeStatus(provider.status)}</p>
    {provider.environment.length > 0 && provider.status.state === "missing" && <p className="fine">Also accepted from the host environment: {provider.environment.join(", ")}.</p>}
    <div className="auth-provider-actions">
      {methods.filter(method => method.kind !== "api_key").map(method => <button key={method.id} type="button" className="button" data-auth-start={method.id} disabled={!enabled} onClick={() => void c.start(provider.id, method.id)}>
        {signedIn ? "Sign in again" : "Sign in"}{methods.length > 1 ? ` · ${method.display_name || method.id}` : ""}
      </button>)}
      {signedIn && !confirming && <button type="button" className="quiet" data-auth-logout="" disabled={!enabled} onClick={() => c.requestLogout(provider.id)}>Sign out</button>}
    </div>
    {methods.filter(method => method.kind === "api_key").map(method => <form key={method.id} className="auth-key-form" onSubmit={event => { event.preventDefault(); void c.start(provider.id, method.id); }}>
      <label className="sr-only" htmlFor={`auth-key-${provider.id}`}>{method.display_name || "API key"} for {provider.id}</label>
      <input id={`auth-key-${provider.id}`} type="password" autoComplete="off" spellCheck={false} placeholder={signedIn ? "Replace API key" : "Paste API key"} maxLength={65536} value={c.keyDrafts[provider.id] ?? ""} disabled={!enabled} onChange={event => c.setKeyDraft(provider.id, event.currentTarget.value)} />
      <button type="submit" className="button" data-auth-start={method.id} disabled={!enabled || !(c.keyDrafts[provider.id] ?? "").trim()}>Save key</button>
    </form>)}
    {terminalOnly.length > 0 && <p className="fine">{terminalOnly.map(method => method.display_name || method.id).join(", ")}: available from the local URL on the Snow host, or with <code>snow login {provider.id}</code> in a terminal.</p>}
    {confirming && <div className="auth-provider-actions" role="group" aria-label={`Confirm sign out of ${provider.id}`}>
      <span className="fine">Remove the stored {provider.id} credential from this host?</span>
      <button type="button" className="quiet" onClick={c.cancelLogout}>Keep</button>
      <button type="button" className="button danger" data-auth-logout-confirm="" disabled={!enabled} onClick={() => void c.logout(provider.id)}>Sign out</button>
    </div>}
  </li>;
}

export function AuthPanel({controller: c}: {controller: Controller}) {
  const readable = !!c.ui?.readable;
  const notice = c.notice || (c.reading ? "Checking provider credentials on the Snow host…" : !c.inventory ? "Refresh to read provider status." : !c.ui?.safe && !c.running() ? "Sign-in controls need an idle, connected session." : "Credentials live on the Snow host. Signing in here updates every Snow surface on that host.");
  return <>
    <button type="button" className="quiet" data-auth-open="" aria-haspopup="dialog" aria-controls="auth-dialog" disabled={!readable} onClick={event => c.open(event.currentTarget)}>Accounts &amp; sign-in…</button>
    <dialog ref={c.setDialog} id="auth-dialog" className="auth-dialog runtime-dialog" aria-labelledby="auth-heading" aria-describedby="auth-boundary"
      onCancel={event => { event.preventDefault(); c.close(); }}>
      <div className="dialog-heading">
        <h2 id="auth-heading">Accounts &amp; sign-in</h2>
        <button type="button" className="quiet" data-auth-refresh="" disabled={!readable || c.reading} onClick={() => void c.inspect()}>Refresh status</button>
        <button type="button" className="quiet icon-button" data-auth-close="" aria-label="Close accounts" title="Close" onClick={c.close}><Close /></button>
      </div>
      <div className="runtime-dialog-body">
        <p id="auth-boundary" className="fine">Sign-in runs inside this project’s worker on the Snow host, the same way <code>snow login</code> does. Device codes work from any paired device. Tokens are stored on the host and never shown here.</p>
        <p className="fine" data-auth-notice="" role="status" aria-live="polite">{notice}</p>
        {c.job && <Job job={c.job} c={c} />}
        <ul className="auth-providers">{orderedProviders(c.inventory?.providers ?? [], typeof c.snapshot?.provider === "string" ? c.snapshot.provider : "").map(provider => <Provider key={provider.id} provider={provider} c={c} />)}</ul>
      </div>
    </dialog>
  </>;
}
