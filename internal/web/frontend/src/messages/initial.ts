import { projectActivities, projectMessages, text } from './model';
import type { Activity, MessageData } from './model';
// One-time import of the server's already bounded public presentation, not a
// generic HTML wrapper and not a second serialized transcript/bootstrap.
export function initialMessages(host: HTMLElement): MessageData[] {
  const input: Record<string, unknown>[] = [];
  for (const row of Array.from(host.children).slice(-100)) {
    if (!(row instanceof HTMLElement)) continue;
    if (row.hasAttribute('data-runtime-activity-group')) { input.push({id: row.dataset.messageId, role: 'tool_activity'}); continue; }
    if (!row.classList.contains('catalog-message')) continue;
    const body = row.querySelector<HTMLElement>('.message-body') || row.querySelector('pre');
    const source = row.querySelector('.message-source');
    const role = row.dataset.messageRole || (row.classList.contains('user-message') ? 'user' : row.querySelector('.message-label')?.textContent?.trim().toLowerCase() || 'assistant');
    const tools = Array.from(row.querySelectorAll<HTMLElement>('.message-tools > .history-tool')).slice(0, 64).map(tool => ({
      id: tool.dataset.historyToolId, tool: tool.dataset.toolName || tool.querySelector('.activity-tool')?.textContent,
      status: tool.dataset.status, output_available: tool.dataset.outputAvailable === 'true', output: tool.querySelector('.activity-output')?.textContent,
      truncated: tool.dataset.truncated === 'true', open: (tool as HTMLDetailsElement).open
    }));
    const images = Array.from(row.querySelectorAll<HTMLElement>('.message-images > .message-image')).slice(0, 8).map(tile => ({index: Number(tile.dataset.imageIndex), mime_type: tile.dataset.imageMime, url: tile.querySelector<HTMLElement>('.message-image-preview')?.dataset.imageUrl || ''}));
    input.push({id: row.dataset.messageId, role, text: source ? source.textContent || '' : body?.textContent || '', html: role !== 'user' ? body?.innerHTML || '' : '',
      can_edit: row.dataset.messageEditable === 'true', can_regenerate: row.dataset.messageRegeneratable === 'true',
      truncated: row.dataset.messageTruncated === 'true' || row.querySelector<HTMLElement>('.message-truncated')?.hidden === false || Array.from(row.querySelectorAll(':scope > p.fine')).some(notice => notice.textContent === 'This message is truncated for bounded display. The original session is unchanged.'), images, tools,
      tools_omitted: row.querySelector<HTMLElement>('.history-tool-limit')?.hidden === false,
      // No exact source means the old visible text must not become resend input.
      missing_source: !source});
  }
  const messages = projectMessages(input);
  for (const message of messages) if (input.find((item, index) => (text(item.id, 256) || `message-${index}`) === message.id)?.missing_source) message.reusable = false;
  return messages;
}
export function initialActivities(region: HTMLElement | null): Activity[] {
  if (!region) return [];
  return projectActivities(Array.from(region.querySelectorAll<HTMLElement>('[data-activity-id]')).slice(-128).map(row => ({
    id: row.dataset.activityId, message_id: row.dataset.messageId, tool: row.dataset.toolName || row.querySelector('.activity-tool')?.textContent,
    status: row.dataset.status, summary: row.querySelector('.activity-summary')?.textContent, output: row.querySelector('.activity-output')?.textContent,
    is_error: row.classList.contains('activity-error'), truncated: row.querySelector<HTMLElement>('.activity-truncated')?.hidden === false, open: (row as HTMLDetailsElement).open
  })));
}
