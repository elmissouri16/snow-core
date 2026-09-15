/* Conversation presentation adapted from Harness (MIT); see HARNESS-NOTICE.txt. */
import { createElement, useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { HTML_LIMIT } from './model';

import { markdownTags as tags, safeLink } from './markdownModel';
function CodeBlock({children, content}: {children: ReactNode; content: string}) {
  const [label, setLabel] = useState('Copy code'), generation = useRef(0), timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => { generation.current++; clearTimeout(timer.current); }, []);
  async function copy() {
    const current = ++generation.current;
    let next = 'Copied';
    try { await navigator.clipboard.writeText(content); } catch { next = 'Select and copy manually'; }
    if (current !== generation.current) return;
    clearTimeout(timer.current); setLabel(next);
    timer.current = setTimeout(() => { if (current === generation.current) setLabel('Copy code'); }, 2500);
  }
  return <div className="code-block"><div className="code-banner"><span className="code-language">Code</span><button type="button" className="quiet copy-code" data-copy-code="" aria-label="Copy code to clipboard" onClick={copy}>{label}</button></div><pre>{children}</pre></div>;
}
// Parse only the explicit bounded server HTML projection into React elements.
// No dangerous HTML insertion: an independent allowlist drops active content,
// attributes and remotely loaded resources even if a malformed DTO reaches us.
// Stable positional keys preserve text nodes, selection and copy/disclosure UI.
export function markdownNodes(html: string): ReactNode[] {
  if (!html || html.length > HTML_LIMIT) return [];
  const template = document.createElement('template');
  template.innerHTML = html;
  let count = 0;
  function visit(nodes: NodeListOf<ChildNode>, path: string, depth: number): ReactNode[] {
    if (depth > 64) return [];
    return Array.from(nodes, (node, index): ReactNode => {
      if (++count > 32768) return null;
      const key = `${path}.${index}`;
      if (node.nodeType === Node.TEXT_NODE) return node.textContent;
      if (!(node instanceof HTMLElement)) return null;
      const tag = node.localName;
      if (!tags.has(tag)) return null;
      const children = visit(node.childNodes, key, depth + 1);
      if (tag === 'pre') return <CodeBlock key={key} content={node.textContent || ''}>{children}</CodeBlock>;
      if (tag === 'table') return <div key={key} className="message-table-scroll" tabIndex={0} role="region" aria-label="Scrollable table"><table>{children}</table></div>;
      if (tag === 'a') return <a key={key} href={safeLink(node.getAttribute('href') || '')} rel="nofollow noreferrer">{children}</a>;
      return createElement(tag, {key}, ...children);
    });
  }
  return visit(template.content.childNodes, 'markdown', 0);
}
export function Markdown({text, html}: {text: string; html: string}) {
  const content = useMemo(() => html ? markdownNodes(html) : <CodeBlock content={text}>{text}</CodeBlock>, [html, text]);
  return <>{content}</>;
}
