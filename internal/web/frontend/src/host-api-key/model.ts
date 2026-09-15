// Only allowlisted local metadata may enter React state. Never retain the raw
// response (including unknown extensions), response errors, or submitted keys.
export interface APIKeyMetadata {
  provider_id: string;
  api_key_supported: boolean;
  replace_required: boolean;
  revision: string;
  state: 'configured' | 'expired' | 'unavailable';
  reason: string;
}

export function providerID(value: string): boolean {
  return value.trim() === value && /^[a-z0-9][a-z0-9_.-]{0,63}$/.test(value);
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

export function metadata(value: unknown, provider: string): APIKeyMetadata | null {
  if (!providerID(provider) || !record(value) || value.provider_id !== provider ||
      typeof value.api_key_supported !== 'boolean' || typeof value.replace_required !== 'boolean' ||
      typeof value.revision !== 'string' || !(value.revision === 'missing' || (value.revision.length === 64 && /^[a-f0-9]{64}$/.test(value.revision))) ||
      value.checked_locally !== true || value.applies_to !== 'future_runtime' ||
      !record(value.status) || value.status.provider_id !== provider || value.status.checked_locally !== true ||
      (provider === 'chatgpt' && value.api_key_supported)) return null;
  const { state, reason } = value.status;
  if (!((state === 'configured' && (reason === 'credential_present' || reason === 'anonymous_access')) ||
      (state === 'expired' && reason === 'credential_expired') ||
      (state === 'unavailable' && (reason === 'credential_missing' || reason === 'credential_invalid' || reason === 'auth_store_unavailable')))) return null;
  return {
    provider_id: provider, api_key_supported: value.api_key_supported,
    replace_required: value.replace_required, revision: value.revision, state, reason,
  };
}

export function writtenReceipt(value: unknown, provider: string): boolean {
  const data = metadata(value, provider);
  return data !== null && data.api_key_supported && data.replace_required && data.revision !== 'missing' &&
    data.state === 'configured' && data.reason === 'credential_present';
}

export function validSecret(value: string): boolean {
  // Reject unpaired UTF-16 surrogates instead of silently replacing them when
  // URLSearchParams encodes UTF-8. The server still independently validates.
  return value.length > 0 && !/[\uD800-\uDFFF]/u.test(value) &&
    new TextEncoder().encode(value).byteLength <= 4096 && value.trim() === value && !/\p{Cc}/u.test(value);
}

export async function readMetadataResponse(response: Response): Promise<unknown> {
  if (!response.ok || response.redirected || !response.body) {
    await response.body?.cancel();
    throw new Error('Unconfirmed API-key operation');
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder('utf-8', { fatal: true });
  let bytes = 0;
  let text = '';
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      bytes += value.byteLength;
      if (bytes > 65536) throw new Error('Invalid API-key metadata');
      text += decoder.decode(value, { stream: true });
    }
    text += decoder.decode();
    return JSON.parse(text) as unknown;
  } finally {
    // Cancel oversized, malformed, or otherwise unfinished bodies too.
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}
