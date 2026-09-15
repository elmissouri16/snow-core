import {useLayoutEffect, useRef} from "react";
import {useBrowserInventory} from "./useBrowserInventory.ts";

export function BrowserInventory({csrf}: {csrf: string}) {
  const root = useRef<HTMLElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  const refresh = useRef<HTMLButtonElement>(null);
  const view = useBrowserInventory(root, csrf);
  useLayoutEffect(() => {
    if (!root.current?.isConnected || view.busy || view.accessEnded) return;
    if (view.focus === "cancel" && view.selected) cancel.current?.focus();
    if (view.focus === "refresh") refresh.current?.focus();
  }, [view.focus, view.selected, view.busy, view.accessEnded]);
  const disabled = view.busy || view.accessEnded;
  const selected = view.selected;
  return <section ref={root} className="settings-panel browser-inventory" data-browser-inventory="" aria-label="Paired browsers" aria-busy={view.busy}>
    <div className="browser-inventory-heading"><h2>Paired browsers</h2><button ref={refresh} className="button quiet" type="button" data-browser-refresh="" disabled={disabled} onClick={view.refresh}>Refresh</button></div>
    <p>Revoke one browser without signing out the others. Labels are approximate browser-family hints, not verified device identities. Use the reference to distinguish similar browsers.</p>
    <p className="fine">Created and last seen times are shown in your local time. Last seen is approximate after a restart: activity is saved when browser access changes. Access expires at most 30 days after pairing. Live event streams recheck access every 5 seconds; revocation does not stop a running agent.</p>
    <input type="hidden" data-browser-csrf="" value={csrf} readOnly />
    <p data-browser-status="" role="status" aria-live="polite">{view.status}</p>
    <ul className="browser-inventory-list" data-browser-list="">
      {view.browsers.map(browser => <li key={browser.id} data-browser-id={browser.id}>
        <div>
          <strong>{browser.label}{browser.current ? " · This browser" : ""}</strong>
          <small>Reference {browser.id.slice(-8)}</small>
          <small>Created {new Date(browser.created).toLocaleString()} · Last seen {new Date(browser.last_used).toLocaleString()} · Expires {new Date(browser.expires).toLocaleString()}</small>
        </div>
        <button type="button" className="button danger" disabled={disabled || !view.ready} onClick={() => view.select(browser)}
          aria-label={`Revoke ${browser.label}, reference ${browser.id.slice(-8)}${browser.current ? ", this browser" : ""}`}>
          {browser.current ? "Revoke this browser…" : "Revoke browser…"}
        </button>
      </li>)}
    </ul>
    <div className="browser-revoke-confirm" data-browser-confirm="" hidden={!selected}>
      <h3>Revoke this browser?</h3>
      <p data-browser-confirm-description="">{selected && `${selected.label} · reference ${selected.id.slice(-8)}. ${selected.current ? "This is your current browser. Confirming signs you out and opens the pairing page." : "This browser will need to pair again."}`}</p>
      <p>This removes only the selected browser's access. The pairing code is unchanged; anyone holding it can pair again. Rotate the pairing code separately if needed.</p>
      <div className="browser-inventory-actions">
        <button ref={cancel} className="button" type="button" data-browser-cancel="" disabled={disabled} onClick={view.cancel}>Cancel</button>
        <button className="button danger" type="button" data-browser-revoke="" disabled={disabled || !selected || !view.ready} onClick={view.revoke}>Confirm revoke browser</button>
      </div>
    </div>
    {view.accessEnded && <p><a className="button" href="/login">Pair this browser again</a></p>}
    <noscript><p>Enable JavaScript to list and individually revoke browsers. The existing Sign out and Revoke all controls remain separate.</p></noscript>
  </section>;
}
