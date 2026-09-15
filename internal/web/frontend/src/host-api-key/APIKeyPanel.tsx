import { useLayoutEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { metadata, providerID, readMetadataResponse, validSecret, writtenReceipt } from './model';
import type { APIKeyMetadata } from './model';

interface PanelState {
  provider: string;
  busy: boolean;
  inspected: APIKeyMetadata | null;
  writable: boolean;
  visible: boolean;
  replace: boolean;
  confirm: boolean;
  status: string;
}
const initialState: PanelState = {
  provider: 'openai-compatible', busy: false, inspected: null, writable: false,
  visible: false, replace: false, confirm: false,
  status: 'Not inspected. Opening Settings does not read credentials or start a worker.',
};
const endpoint = (provider: string) => `/settings/providers/${encodeURIComponent(provider)}/api-key`;

export function APIKeyPanel({ csrf, enabled }: { csrf: string; enabled: boolean }) {
  const root = useRef<HTMLElement>(null);
  const providerInput = useRef<HTMLInputElement>(null);
  const secret = useRef<HTMLInputElement>(null);
  const inspectButton = useRef<HTMLButtonElement>(null);
  const form = useRef<HTMLFormElement>(null);
  const [view, setView] = useState<PanelState>(initialState);
  // Synchronous nonsecret ownership fences reject duplicate events even before
  // React commits a disabled control. The password has no state/ref snapshot.
  const owner = useRef({ live: false, generation: 0, state: initialState, request: null as AbortController | null });
  const focusInspect = useRef(false);

  function update(patch: Partial<PanelState>) {
    const next = { ...owner.current.state, ...patch };
    owner.current.state = next;
    setView(next);
  }
  function clearSecret() {
    if (secret.current) secret.current.value = '';
  }
  function retire() {
    owner.current.generation++;
    owner.current.request?.abort();
    owner.current.request = null;
    clearSecret();
    update({ busy: false, inspected: null, writable: false, visible: false, replace: false, confirm: false });
  }
  function connected() {
    const element = root.current;
    const dialog = element?.closest('dialog');
    return owner.current.live && enabled && element?.isConnected === true && (!dialog || dialog.open);
  }
  function active(generation: number, provider: string) {
    return connected() && owner.current.generation === generation &&
      owner.current.state.provider === provider && providerInput.current?.value === provider;
  }

  useLayoutEffect(() => {
    owner.current.live = true;
    retire();
    const node = root.current;
    const password = secret.current;
    const close = (event: Event) => {
      if (event.target instanceof Element && node && event.target.contains(node)) retire();
    };
    const hide = () => retire();
    document.addEventListener('close', close, true);
    window.addEventListener('pagehide', hide);
    return () => {
      owner.current.live = false;
      owner.current.generation++;
      owner.current.request?.abort();
      owner.current.request = null;
      // Keep the captured input so cleanup still clears a detached DOM node.
      if (password) password.value = '';
      document.removeEventListener('close', close, true);
      window.removeEventListener('pagehide', hide);
    };
    // A changed server capability or CSRF invalidates all previous ownership.
  }, [enabled, csrf]);

  useLayoutEffect(() => {
    if (focusInspect.current && !view.busy) {
      focusInspect.current = false;
      if (connected()) inspectButton.current?.focus();
    }
  });

  function changeProvider(value: string) {
    if (owner.current.state.busy) {
      if (providerInput.current) providerInput.current.value = owner.current.state.provider;
      return;
    }
    retire();
    update({ provider: value, status: 'Provider changed. Explicitly inspect this provider before entering a key.' });
  }

  async function explicitInspect() {
    if (!connected() || owner.current.state.busy) return;
    retire();
    const selected = providerInput.current?.value ?? '';
    update({ provider: selected });
    if (!providerID(selected)) {
      update({ status: 'Enter a supported provider or an existing compatible profile ID.' });
      return;
    }
    const generation = owner.current.generation;
    const controller = new AbortController();
    owner.current.request = controller;
    const timer = window.setTimeout(() => controller.abort(), 4500);
    update({ busy: true, status: 'Inspecting local provider metadata only…' });
    try {
      const response = await fetch(endpoint(selected), {
        credentials: 'same-origin', cache: 'no-store', redirect: 'error',
        signal: controller.signal, headers: { Accept: 'application/json' },
      });
      const data = metadata(await readMetadataResponse(response), selected);
      if (!active(generation, selected)) return;
      if (!data) throw new Error('Invalid API-key metadata');
      if (!data.api_key_supported) {
        update({ status: 'This provider does not support API-key entry. Use interactive snow login on the host; ChatGPT uses snow login chatgpt.' });
        return;
      }
      update({ inspected: data, writable: true, visible: true, status: data.replace_required
        ? 'Inspection complete. Explicit replacement consent is required.'
        : 'Inspection complete. Enter a new key only if you intend to save it on this host.' });
    } catch {
      if (active(generation, selected)) update({ status: 'Inspection unavailable. No key was submitted. Explicitly inspect again to continue.' });
    } finally {
      window.clearTimeout(timer);
      if (active(generation, selected)) {
        owner.current.request = null;
        update({ busy: false });
      }
    }
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    // Clear before guards, validation, fetch, or any await, including failures.
    let submitted = secret.current?.value ?? '';
    clearSecret();
    const current = owner.current.state;
    if (!connected() || current.busy || !current.writable || !current.inspected) { submitted = ''; return; }
    const selected = providerInput.current?.value ?? '';
    const inspected = current.inspected;
    if (!providerID(selected) || selected !== current.provider || selected !== inspected.provider_id ||
        !current.confirm || (inspected.replace_required && !current.replace)) {
      submitted = '';
      update({ status: 'The key field was cleared. Confirm the exact provider, host scope, and any required replacement before a new submission.' });
      return;
    }
    if (!validSecret(submitted)) {
      submitted = '';
      update({ status: 'The key field was cleared. Use a nonempty UTF-8 key up to 4096 bytes without whitespace padding or control characters.' });
      return;
    }
    const body = new URLSearchParams({ csrf, expected_revision: inspected.revision, secret: submitted,
      confirm_replace: String(current.replace), confirm_save: 'host' });
    submitted = '';
    const generation = ++owner.current.generation;
    const controller = new AbortController();
    owner.current.request = controller;
    const timer = window.setTimeout(() => controller.abort(), 6500);
    const hadFormFocus = form.current?.contains(document.activeElement) === true;
    // Consuming the grant before dispatch prohibits retry, regardless of receipt.
    update({ writable: false, busy: true, status: 'Submitting a write-only key to the selected Snow host provider…' });
    try {
      const pending = fetch(endpoint(selected), {
        method: 'POST', credentials: 'same-origin', cache: 'no-store', redirect: 'error',
        signal: controller.signal, headers: { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded' }, body,
      });
      // Fetch has synchronously extracted the body; retain no second key copy
      // while waiting for the receipt. Never enqueue or replay this request.
      body.delete('secret');
      const data = await readMetadataResponse(await pending);
      if (!active(generation, selected)) return;
      if (!writtenReceipt(data, selected)) throw new Error('Invalid API-key receipt');
      update({ visible: false, status: `Key saved locally for ${selected}; not network verified. Restart existing workers to use it. Explicitly inspect again before any further write.` });
    } catch {
      if (active(generation, selected)) update({ status: 'Save outcome is unknown or was rejected. The key field is cleared and will not be replayed. Explicitly inspect local status before deciding whether to enter and submit a new key.' });
    } finally {
      body.delete('secret');
      window.clearTimeout(timer);
      if (active(generation, selected)) {
        owner.current.request = null;
        clearSecret();
        // Do not leave keyboard focus in a disabled/hidden form, or steal it
        // from a reader who moved elsewhere while the request was pending.
        focusInspect.current = hadFormFocus && (document.activeElement === document.body || form.current?.contains(document.activeElement) === true);
        update({ busy: false, confirm: false, replace: false });
      }
    }
  }

  const writable = enabled && !view.busy && view.writable;
  return <section ref={root} className="host-api-key" data-host-api-key="" data-enabled={String(enabled)} aria-busy={view.busy} aria-labelledby="host-api-key-title">
    <h4 id="host-api-key-title">Write-only API key</h4>
    {enabled ? <p>Save a key on the Snow host over this direct HTTPS connection. Keys are never returned to the browser. Local presence is not network verification. Existing workers must be restarted to use changed credentials.</p>
      : <p className="host-api-key-warning">API-key entry is disabled on this connection. It requires direct numeric-loopback HTTPS with the manager’s TLS option enabled and an available host control backend. Plain HTTP and forwarded HTTPS headers cannot enable it. Use interactive <code>snow login</code> on the host instead.</p>}
    <input type="hidden" data-api-key-csrf="" value={csrf} />
    <label>Provider ID <input ref={providerInput} type="text" data-api-key-provider="" value={view.provider} maxLength={64} autoComplete="off" autoCapitalize="none" spellCheck={false} disabled={!enabled || view.busy} onChange={event => changeProvider(event.currentTarget.value)} /></label>
    <p className="fine">Use opencode-go, opencode-zen, openai-compatible, or an existing compatible profile ID. ChatGPT requires interactive <code>snow login chatgpt</code>, not an API key.</p>
    <button ref={inspectButton} type="button" className="button" data-api-key-inspect="" disabled={!enabled || view.busy} onClick={() => void explicitInspect()}>Inspect provider before entering a key</button>
    <p data-api-key-status="" className="fine" role="status" aria-live="polite">{view.status}</p>
    <form ref={form} method="post" data-api-key-form="" autoComplete="off" hidden={!enabled || !view.visible} onSubmit={event => void save(event)}>
      <p data-api-key-target="" className="fine">{view.inspected && `${view.inspected.provider_id} · global credential on the Snow host · future workers only. Local state: ${view.inspected.state}; not network verified.`}</p>
      <label>New API key <input ref={secret} type="password" data-api-key-secret="" autoComplete="off" autoCapitalize="none" spellCheck={false} maxLength={4096} disabled={!writable} /></label>
      <p className="fine">Write only, up to 4096 UTF-8 bytes. The field is cleared immediately when submitted, including failures. It is never saved as a browser draft or automatically retried.</p>
      <label className="checkbox-label" data-api-key-replace-label="" hidden={!view.inspected?.replace_required}><input type="checkbox" data-api-key-replace="" checked={view.replace} disabled={!writable || !view.inspected?.replace_required} onChange={event => update({ replace: event.currentTarget.checked })} /> I explicitly authorize replacing the selected provider’s existing or effective credential.</label>
      <label className="checkbox-label"><input type="checkbox" data-api-key-confirm="" checked={view.confirm} disabled={!writable} onChange={event => update({ confirm: event.currentTarget.checked })} /> <span data-api-key-confirm-label="">{view.inspected ? `I confirm saving a key for ${view.inspected.provider_id} on the Snow host for future workers. Existing workers require restart.` : 'I confirm saving this provider’s key on the Snow host for future workers. Existing workers require restart.'}</span></label>
      <button type="submit" className="button primary" data-api-key-save="" disabled={!writable}>{view.inspected ? `Save ${view.inspected.provider_id} key on host` : 'Save key on host'}</button>
    </form>
  </section>;
}
