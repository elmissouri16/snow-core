import { SnowClient, SdkError, RemoteRpcError } from '@elmissouri16/snow-sdk';
import type {
  RpcCommand, RpcRequest, RpcResponse, RpcReady, RpcNotification, PromptCompleted,
  RpcRequestFor, RpcResponseFor, Model, Message, SessionSummary,
} from '@elmissouri16/snow-sdk';

const request: RpcRequestFor<'models_list'> = { type: 'models_list' };
const grouped: RpcRequestFor<'follow_up'> = { type: 'follow_up', message: 'next' };
const page: RpcRequestFor<'messages_page'> = { type: 'messages_page', params: { limit: 10 } };
const pageDefault: RpcRequestFor<'messages_page'> = { type: 'messages_page' };
const model: RpcRequestFor<'set_model'> = { type: 'set_model', model: 'example' };
const command: RpcCommand = request.type;
const wire: RpcRequest = { ...grouped, id: 'wire-level-id' };
// @ts-expect-error transport owns correlation IDs
const callerId: RpcRequestFor<'models_list'> = { type: 'models_list', id: 'caller' };
// @ts-expect-error set_model requires the model ID
const missingModel: RpcRequestFor<'set_model'> = { type: 'set_model' };
// @ts-expect-error mode schema enum is preserved
const invalidMode: RpcRequestFor<'set_mode'> = { type: 'set_mode', mode: 'arbitrary' };
// @ts-expect-error commands are eager-schema derived
const invalidCommand: RpcCommand = 'invented_command';

function consumeModels(response: RpcResponseFor<'models_list'>) {
  if (response.success) {
    response.data.models.forEach((entry: Model) => {
      const provider: string = entry.provider;
      const vision: boolean = entry.supports_vision;
      void [provider, vision];
    });
    // @ts-expect-error typed data must not degrade to any or generic fallback
    response.data.nonexistent;
  } else {
    const error: string = response.error;
    void error;
  }
}
function consumeGroupedModels(response: RpcResponseFor<'subagent_models'>) {
  if (response.success) {
    const models: Model[] = response.data.models;
    void models;
  }
}
function consumeSessions(response: RpcResponseFor<'sessions_list'>) {
  if (response.success) {
    const sessions: SessionSummary[] = response.data.sessions;
    void sessions;
  }
}
function consumeGeneric(response: RpcResponseFor<'set_mode'>) {
  if (response.success) {
    const data: unknown = response.data;
    // @ts-expect-error generic success data must remain unknown, not any
    data.arbitrary;
  }
}
async function consumeClient(client: SnowClient) {
  const response = await client.rpc({ type: 'models_list' });
  if (response.success) {
    const models: Model[] = response.data.models;
    const current: string | undefined = response.data.current;
    // @ts-expect-error actual optional current-model field must not degrade to any
    const wrongCurrent: boolean = response.data.current;
    void [models, current, wrongCurrent];
  }
  const created = await client.rpc({ type: 'session_create' });
  if (created.success) {
    const sessionId: string = created.data.session_id;
    // @ts-expect-error session_create uses the typed summary, not a generic success branch
    const wrongSessionId: number = created.data.session_id;
    void [sessionId, wrongSessionId];
  }
  const idBearing = { type: 'models_list' as const, id: 'caller' };
  // @ts-expect-error IDs remain forbidden for variables, not only excess object-literal fields
  client.rpc(idBearing);
  // @ts-expect-error messages_page uses cursor pagination, not offset
  client.rpc({ type: 'messages_page', params: { offset: 0 } });
  // @ts-expect-error thinking schema enum is preserved by generic rpc inference
  client.rpc({ type: 'set_thinking', thinking: 'arbitrary' });
  // @ts-expect-error permission_reply requires an explicit decision
  client.rpc({ type: 'permission_reply', params: { request_id: 'permission-1' } });
  // @ts-expect-error caller-provided IDs are not public rpc input
  client.rpc({ type: 'models_list', id: 'caller' });
  // @ts-expect-error malformed grouped request must not pass through another command
  client.rpc({ type: 'follow_up' });
  const models: Model[] = (await client.models.list()).models;
  const sessions: SessionSummary[] = (await client.sessions.list()).sessions;
  const messages: Message[] = (await client.sessions.messages()).messages;
  const run = await client.prompt('hello');
  const completion: PromptCompleted = await run.result;
  const ready: RpcReady = client.ready;
  const events: AsyncIterable<RpcNotification> = client.events();
  void [models, sessions, messages, completion, ready, events];
}
const future: RpcNotification = { type: 'future_event', additive: { value: 1 } };
declare const rawResponse: RpcResponse;
void [SnowClient, SdkError, RemoteRpcError, request, grouped, page, pageDefault,
  model, command, wire, callerId, missingModel, invalidMode, invalidCommand,
  consumeModels, consumeGroupedModels, consumeSessions, consumeGeneric, consumeClient, future, rawResponse];
