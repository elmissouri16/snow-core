/** Public projections of the canonical RPC v1 schemas. No runtime validation. */
import type { GeneratedRpcTypes } from './generated/rpc.js';

export type RpcRequest = GeneratedRpcTypes['Request'];
export type RpcResponse = GeneratedRpcTypes['Response'];
export type RpcReady = GeneratedRpcTypes['Ready'];
export type PromptCompleted = GeneratedRpcTypes['PromptCompleted'];
export type RpcOutput = GeneratedRpcTypes['Output'];
export type AgentEvent = GeneratedRpcTypes['AgentEvent'];
export type Model = GeneratedRpcTypes['Model'];
export type Message = GeneratedRpcTypes['Message'];
export type SessionSummary = GeneratedRpcTypes['SessionSummary'];
export type SessionInfo = GeneratedRpcTypes['SessionInfo'];
export type RpcCommand = keyof GeneratedRpcTypes['RequestMap'];

// Distribute omission across variants (messages_page, for example), rather
// than collapsing a request union into just its common fields.
type WithoutCallerId<T> = T extends unknown ? Omit<T, 'id'> & { id?: never } : never;
export type RpcRequestFor<C extends RpcCommand> = WithoutCallerId<GeneratedRpcTypes['RequestMap'][C]>;
export type RpcResponseFor<C extends RpcCommand> = GeneratedRpcTypes['ResponseMap'][C];

/** Unknown additive event frames are retained; ready/response are routed separately at runtime. */
export interface UnknownRpcNotification {
  type: string;
  [field: string]: unknown;
}
export type RpcNotification = Exclude<RpcOutput, RpcReady | RpcResponse> | UnknownRpcNotification;
