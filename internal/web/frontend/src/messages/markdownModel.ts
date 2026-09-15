// Mirrors the server's deliberately small public Markdown policy. These are
// presentation primitives, not a browser Markdown engine.
export const markdownTags = new Set('p br hr strong em del blockquote pre code h1 h2 h3 h4 h5 h6 ul ol li table thead tbody tr th td a'.split(' '));
export function safeLink(raw: string): string | undefined {
  try { const url = new URL(raw); return ['http:', 'https:'].includes(url.protocol) && !!url.hostname && !url.username && !url.password ? raw : undefined; }
  catch { return undefined; }
}
