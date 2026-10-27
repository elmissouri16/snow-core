export type Identity = Readonly<{project_id: string; instance_id: string; session_id: string}>;
export type MCPServer = Readonly<{id: string; transport: string; connected: boolean; state: string; server_name: string; server_version: string; protocol_version: string; capabilities: readonly string[]; tool_count: number; message: string; cached: boolean}>;
export type MCPInventory = Identity & Readonly<{servers: readonly MCPServer[]; limited: boolean}>;
export type UI = Readonly<{readable: boolean; safe: boolean}>;

const record = (value: unknown): value is Record<string, unknown> => typeof value === "object" && value !== null;
const text = (value: unknown, limit: number): value is string => typeof value === "string" && value.length <= limit && !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(value);

export function validInventory(value: unknown, identity: Identity): value is MCPInventory {
  if (!record(value) || value.project_id !== identity.project_id || value.instance_id !== identity.instance_id || value.session_id !== identity.session_id) return false;
  if (typeof value.limited !== "boolean" || !Array.isArray(value.servers) || value.servers.length > 64) return false;
  return value.servers.every(server => record(server) && text(server.id, 256) && server.id !== "" && text(server.transport, 64) && typeof server.connected === "boolean" &&
    text(server.state, 64) && text(server.server_name, 256) && text(server.server_version, 64) && text(server.protocol_version, 64) &&
    Array.isArray(server.capabilities) && server.capabilities.every(item => text(item, 64)) && Number.isSafeInteger(server.tool_count) && (server.tool_count as number) >= 0 &&
    text(server.message, 1024) && typeof server.cached === "boolean");
}

export function stateLabel(server: MCPServer): string {
  if (server.connected) return "Connected";
  return {disabled: "Disabled", failed: "Failed", unavailable: "Unavailable", pending: "Not connected yet", lazy: "Connects on first use"}[server.state] ?? (server.state || "Not connected");
}
