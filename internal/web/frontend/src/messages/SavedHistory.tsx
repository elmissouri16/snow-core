import { useState } from 'react';
import { defaultActions } from './actions';
import { initialMessages } from './initial';
import { Message } from './Message';
import type { MessageData } from './model';
import { scopeOf } from './imageTransport';

const projectionBrand: unique symbol = Symbol('saved-history');
// Only captureSavedHistory can produce this ephemeral handle. The parent passes
// it through its existing React tree; it is not JSON/bootstrap or caller HTML.
export type SavedHistoryProjection = {readonly [projectionBrand]: true};
type PublicHistory = {project: string; session: string; messages: MessageData[]};
const projections = new WeakMap<SavedHistoryProjection, PublicHistory>();
const identifier = (value: unknown): value is string => typeof value === 'string' && /^[A-Za-z0-9_-]{1,128}$/.test(value);
const actions = defaultActions();

export function captureSavedHistory(source: HTMLElement | null, expected: {project: string; session: string}): SavedHistoryProjection | null {
  if (!source || !identifier(expected.project) || !identifier(expected.session) || source.closest('#live-session, [data-react-saved-history]')) return null;
  const owner = source.closest<HTMLElement>('.catalog-history');
  if (!owner) return null;
  const scope = scopeOf(owner);
  if (scope.live || scope.instance || scope.project !== expected.project || scope.session !== expected.session) return null;
  const host = source.querySelector<HTMLElement>(':scope > [data-react-messages]') || source;
  // Saved history has no live event-step authority. All remaining data comes
  // from the same bounded public SSR projection used by standalone enhancement.
  const messages = initialMessages(host).filter(message => message.role !== 'tool_activity');
  const projection: SavedHistoryProjection = Object.freeze({[projectionBrand]: true});
  projections.set(projection, {project: scope.project, session: scope.session, messages});
  return projection;
}

function Content({history}: {history: PublicHistory}) {
  // Callback-ref state supplies the owned, committed scope for authenticated
  // thumbnails. The message JSX is present on the very first commit; there is
  // no effect-mounted inner root, HTML wrapper, or effect-time flushSync.
  const [scope, setScope] = useState<HTMLDivElement | null>(null);
  return <div ref={setScope} className="catalog-history" data-project={history.project} data-session={history.session} data-react-saved-history="">
    {history.messages.length ? history.messages.map(message => <Message key={message.id} message={message} scope={scope} actions={actions}/>) : <div className="empty-state compact"><h2>No displayable messages</h2><p>This page has no displayable messages.</p></div>}
  </div>;
}
export function SavedHistory({projection}: {projection: SavedHistoryProjection | null}) {
  const history = projection && projections.get(projection);
  if (!history) return null;
  return <Content key={`${history.project}:${history.session}`} history={history}/>;
}
