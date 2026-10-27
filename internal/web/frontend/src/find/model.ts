export type Match = Readonly<{node: Text; start: number; end: number}>;

export const MATCH_LIMIT = 500;

/** Case-insensitive, accent-insensitive-ish literal search across the visible message bodies of a transcript root. */
export function findMatches(root: ParentNode, query: string, limit = MATCH_LIMIT): Match[] {
  const needle = query.trim().toLocaleLowerCase();
  if (needle.length < 2) return [];
  const matches: Match[] = [];
  for (const body of root.querySelectorAll<HTMLElement>('.conversation-message .message-body')) {
    if (body.hidden) continue;
    const walker = document.createTreeWalker(body, NodeFilter.SHOW_TEXT);
    for (let node = walker.nextNode() as Text | null; node; node = walker.nextNode() as Text | null) {
      const text = node.data.toLocaleLowerCase();
      let from = 0;
      while (from <= text.length - needle.length) {
        const at = text.indexOf(needle, from);
        if (at < 0) break;
        matches.push({node, start: at, end: at + needle.length});
        if (matches.length >= limit) return matches;
        from = at + needle.length;
      }
    }
  }
  return matches;
}

export function counter(index: number, total: number, limited: boolean): string {
  if (total === 0) return 'No matches';
  return `${index + 1} of ${total}${limited ? '+' : ''}`;
}

export const step = (index: number, total: number, delta: number) => total === 0 ? 0 : (index + delta + total) % total;
