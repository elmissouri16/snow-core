/* Conversation presentation adapted from Harness (MIT); see HARNESS-NOTICE.txt. */
import { useLayoutEffect, useRef, useState } from 'react';
import type { MessageData } from './model';
import type { ActionPresentation } from './actions';
import { Images } from './Images';
import { Markdown } from './Markdown';
import { ToolRow } from './ToolRow';
export type MessageElement = HTMLElement & {_snowCopyText: string; _snowHasImages: boolean; _snowEditable: boolean; _snowRegeneratable: boolean; _snowReusable: boolean};
function CopyIcon({copied}: {copied: boolean}) {
  return <svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round"><path d={copied ? 'm5 12 4 4L19 6' : 'M9 8V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-3M5 8h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-9a2 2 0 0 1 2-2Z'}/></svg>;
}
function CopyMessage({text}: {text: string}) {
  const [feedback, setFeedback] = useState(''), generation = useRef(0), timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useLayoutEffect(() => () => { generation.current++; clearTimeout(timer.current); }, []);
  async function copy() {
    const current = ++generation.current;
    let next = 'Copied';
    try { await navigator.clipboard.writeText(text); } catch { next = 'Could not copy. Select the message and copy manually.'; }
    if (current !== generation.current) return;
    clearTimeout(timer.current); setFeedback(next);
    timer.current = setTimeout(() => { if (current === generation.current) setFeedback(''); }, 2500);
  }
  return <><button type="button" className="message-copy" data-message-copy="" aria-label={feedback === 'Copied' ? 'Message copied' : 'Copy message'} title={feedback === 'Copied' ? 'Copied' : 'Copy message'} onClick={copy}><CopyIcon copied={feedback === 'Copied'}/></button><span className="message-copy-feedback" role="status" aria-live="polite">{feedback}</span></>;
}
export function Message({message, scope, actions}: {message: MessageData; scope: HTMLElement | null; actions: ActionPresentation}) {
  const row = useRef<HTMLElement>(null), {role} = message;
  const live = scope?.id === 'live-session' && scope.dataset.runtime === 'true';
  const editable = scope?.id === 'live-session' && scope.dataset.messageEditEnabled === 'true';
  const regeneratable = scope?.id === 'live-session' && scope.dataset.messageRegenerateEnabled === 'true';
  useLayoutEffect(() => {
    if (!row.current) return;
    const element = row.current as MessageElement;
    element._snowCopyText = message.text; element._snowHasImages = !!message.images.length;
    element._snowEditable = message.editable; element._snowRegeneratable = message.regeneratable; element._snowReusable = message.reusable;
  });
  return <article ref={row} className={`catalog-message conversation-message${role === 'user' ? ' user-message' : ''}`} data-message-id={message.id} data-message-role={role} data-message-editable={String(message.editable)} data-message-has-images={String(!!message.images.length)} data-message-regeneratable={String(message.regeneratable)} data-editing={String(live && role === 'user' && actions.edit.messageID === message.id)} aria-label={role === 'user' ? 'Your message' : role === 'plan' ? 'Assistant plan' : 'Assistant message'}>
    <Images images={message.images} root={scope} messageID={message.id}/>
    <div className={`message-body${role !== 'user' ? ' markdown-body' : ''}`} hidden={!message.text && !!message.images.length}>{role === 'user' ? message.text : <Markdown text={message.text} html={message.html}/>}</div>
    <span className="message-source" hidden>{message.text}</span>
    <p className="fine message-truncated" hidden={!message.truncated}>Message truncated for bounded display.</p>
    {(message.tools.length > 0 || message.toolsOmitted) && <div className="message-tools tool-timeline activity-list" role="group" aria-label="Saved tool history">{message.tools.map(tool => <ToolRow key={tool.id} data={tool} history/>)}<p className="fine history-tool-limit" hidden={!message.toolsOmitted}>Saved tool history truncated for bounded display.</p></div>}
    <div className="message-actions">
      {role === 'user' && !message.images.length && (editable ? message.editable && <button type="button" className="message-edit" data-message-edit="" disabled={!live || actions.edit.disabled || actions.historicalDisabled || !message.editable} hidden={!live || actions.edit.hidden} title="Edit this message and replace the following conversation when you Send.">Edit &amp; resend</button> : <button type="button" className="message-reuse" data-message-reuse="" disabled={!live || actions.reuse.disabled || actions.historicalDisabled || !message.reusable} hidden={!live || actions.reuse.hidden} title={!message.reusable ? "This message is truncated and cannot be reused" : actions.reuse.title}>Use as new prompt</button>)}
      {regeneratable && message.regeneratable && <button type="button" className="message-regenerate" data-message-regenerate="" disabled={!live || actions.regenerate.disabled || !message.regeneratable} hidden={!live || actions.regenerate.hidden} title="Regenerate this reply and replace the following conversation after confirmation.">Regenerate</button>}
      <CopyMessage text={message.text}/>
    </div>
  </article>;
}
