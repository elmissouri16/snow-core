// Public presentation DTOs only. Never spread provider/session objects into props.
export const TEXT_LIMIT = 65536, HTML_LIMIT = 131072;
export const imageTypes = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp']);
export const text = (value: unknown, limit = TEXT_LIMIT): string => typeof value === 'string' ? value.slice(0, limit) : '';
export const record = (value: unknown): Record<string, unknown> => value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};
export type ImageData = {index: number; mime: string; url: unknown};
export type ToolData = {id: string; tool: string; status: string; label: string; summary: string; output: string; expandable: boolean; error: boolean; truncated: boolean; available?: boolean; rawTruncated?: boolean; open?: boolean};
export type MessageData = {id: string; role: 'user' | 'assistant' | 'plan' | 'tool_activity'; text: string; html: string; truncated: boolean; editable: boolean; regeneratable: boolean; reusable: boolean; images: ImageData[]; tools: ToolData[]; toolsOmitted: boolean};
export function boundedOutput(value: unknown, limit: number) {
  const source = typeof value === 'string' ? value : '';
  let bytes = 0, end = 0;
  for (const point of source) {
    const code = point.codePointAt(0)!;
    const size = code <= 0x7f ? 1 : code <= 0x7ff ? 2 : code <= 0xffff ? 3 : 4;
    if (bytes + size > limit) break;
    bytes += size; end += point.length;
  }
  return {text: source.slice(0, end), bytes, truncated: end < source.length};
}
export function projectMessages(value: unknown): MessageData[] {
  const input = Array.isArray(value) ? value.slice(-100) : [], ids = new Set<string>(), toolIDs = new Set<string>();
  let toolBytes = 0;
  const result: MessageData[] = [];
  for (const [index, item] of input.entries()) {
    const data = record(item), role = data.role;
    if (role !== 'user' && role !== 'assistant' && role !== 'plan' && role !== 'tool_activity') continue;
    const id = role === 'tool_activity' ? typeof data.id === 'string' && data.id.length <= 256 ? data.id : '' : text(data.id, 256) || `message-${index}`;
    if (!id || ids.has(id)) continue;
    ids.add(id);
    const images = (Array.isArray(data.images) ? data.images.slice(0, 8) : []).map(item => {
      const image = record(item);
      return {index: Number.isSafeInteger(image.index) && Number(image.index) >= 0 && Number(image.index) <= 10000 ? Number(image.index) : -1,
        mime: typeof image.mime_type === 'string' && imageTypes.has(image.mime_type) ? image.mime_type : '', url: typeof image.url === 'string' && image.url.length <= 4096 ? image.url : null};
    });
    const rawText = typeof data.text === 'string' ? data.text : '', content = text(rawText);
    const truncated = !!data.truncated || rawText.length > TEXT_LIMIT;
    const tools: ToolData[] = [], inputTools = role === 'assistant' && Array.isArray(data.tools) ? data.tools : [];
    let toolsOmitted = data.tools_omitted === true || inputTools.length > 64;
    for (const item of inputTools.slice(0, 64)) {
      const tool = record(item), toolID = text(tool.id, 256);
      if (!toolID || toolIDs.has(toolID)) continue;
      if (toolIDs.size >= 64) { toolsOmitted = true; break; }
      toolIDs.add(toolID);
      const status = tool.status === 'completed' || tool.status === 'failed' ? tool.status : 'unresolved', available = tool.output_available === true;
      const output = boundedOutput(status !== 'unresolved' && available ? tool.output : '', Math.min(8192, 131072 - toolBytes));
      toolBytes += output.bytes;
      const detail = status === 'unresolved' ? 'No result recorded; execution outcome unknown' : !available ? 'Public output was not recorded' : output.text;
      tools.push({id: toolID, tool: text(tool.tool, 128), status, label: status === 'completed' ? 'Completed' : status === 'failed' ? 'Failed' : 'Outcome unknown', summary: '', output: detail,
        expandable: !!detail || !!tool.truncated || output.truncated, error: status === 'failed', truncated: status !== 'unresolved' && available && (!!tool.truncated || output.truncated), available, rawTruncated: !!tool.truncated || output.truncated, open: tool.open === true});
    }
    result.push({id, role, text: content, html: typeof data.html === 'string' && data.html.length <= HTML_LIMIT ? data.html : '', truncated,
      editable: role === 'user' && !images.length && data.can_edit === true,
      regeneratable: role === 'assistant' && data.can_regenerate === true && !truncated,
      reusable: role === 'user' && !images.length && !truncated && typeof data.text === 'string' && new TextEncoder().encode(rawText).length <= TEXT_LIMIT,
      images, tools, toolsOmitted});
  }
  return result;
}
const statuses: Record<string, string> = {pending: 'Pending', queued: 'Queued', running: 'Running', waiting: 'Waiting', permission: 'Approval needed', completed: 'Completed', complete: 'Completed', success: 'Completed', done: 'Completed', failed: 'Failed', error: 'Failed', denied: 'Rejected', rejected: 'Rejected', failure: 'Failed', cancelled: 'Cancelled', canceled: 'Cancelled', interrupted: 'Interrupted', unknown: 'Outcome unknown', aborted: 'Cancelled'};
export type Activity = {messageID: string; data: ToolData};
export function projectActivities(value: unknown): Activity[] {
  const result: Activity[] = [], ids = new Set<string>();
  for (const [index, item] of (Array.isArray(value) ? value.slice(-128) : []).entries()) {
    if (!item || typeof item !== 'object') continue;
    const data = record(item), id = text(data.id, 256) || `activity-${index}`;
    if (ids.has(id)) continue;
    ids.add(id);
    const status = text(data.status, 32), output = text(data.output, 16384), summary = text(data.summary, 1024);
    result.push({messageID: typeof data.message_id === 'string' && data.message_id.length <= 256 ? data.message_id : '', data: {
      id, tool: text(data.tool, 128), status, output, summary,
      label: status === 'unknown' ? 'Outcome unknown' : data.is_error ? 'Failed' : Object.hasOwn(statuses, status) ? statuses[status] : 'Status unavailable',
      error: !!data.is_error || ['error', 'failed', 'failure', 'denied', 'rejected'].includes(status),
      expandable: !!output || !!data.truncated,
      truncated: !!data.truncated || (typeof data.output === 'string' && data.output.length > 16384) || (typeof data.summary === 'string' && data.summary.length > 1024), open: data.open === true
    }});
  }
  return result;
}
