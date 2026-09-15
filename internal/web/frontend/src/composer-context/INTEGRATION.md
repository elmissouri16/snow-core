# Composer context React boundary

Register the named `composerContext` export from `./composer-context/index.ts` as
`window.SnowComposerContext` before `SnowReactReady`. The parent app owns init,
render and disposal; main must not mount a second root here.

## Empty mount points

Inside `#live-session`, use **empty** `display: contents` wrappers:

- `[data-composer-context-root]`: in `#live-composer`, before the external
  `#live-prompt` textarea. Replaces the legacy chips, status and mentions nodes.
- `[data-composer-context-tools-root]`: inside `.composer-leading`. Replaces
  `.composer-context-tools` and the legacy hidden file input.

The first is a React root; the second is its portal. Both must exist before
`init`, as must the stable parent-owned textarea and form. The JSX owns complete
chips, bounded private thumbnails, status, listbox, Add context/Attach controls,
file input and context menu. There was no modal dialog in legacy composer
context: its menu remains `role="menu"`, not a new blocking modal. Its bounded
fixed geometry and native focus/scroll behavior are controller-owned; no legacy
SnowMenus append/wrap/remove/reconcile operations may touch these JSX children.
Existing CSS classes, `#composer-mentions`, data hooks, labels, folder/skill
icons and explicit retry controls are retained. No asset or template files were
changed in this task.

The outer form, hidden HTTP identity/CSRF inputs, textarea, send/stop controls,
notices, composer seat and attention regions remain foreign. This facade never
writes their children, text, values, disabled states or attributes. It reads the
textarea value/selection and listens to its existing native input, composition,
keyboard, blur/click events and the form's drop/paste events.

## API

```ts
composerContext.init({
  root: HTMLElement,
  key: string,
  instance: string,
  request(kind: 'files' | 'file' | 'skills', fields: {path?: string; offset?: number}): Promise<unknown>,
  changed?(): void,
  error?(message: string): void,
  replaceText?(value: string, start: number, end: number): boolean,
  focusPrompt?(): void,
  suggestions?(state: {controls?: string; expanded: boolean; activeDescendant?: string}): void,
}): Controller
```

The first six fields retain their existing meaning and transport authority.
Three presentation callbacks replace formerly imperative textarea writes:

- `replaceText`: replace exactly the supplied range in the parent's canonical
  draft, advance its draft revision and **synchronously publish** value plus
  resulting caret; return true on success. Do **not** dispatch `input`; the
  facade inspects the resulting token once after the callback. Missing/false
  means no replacement; no imperative fallback writes the external textarea.
- `focusPrompt`: parent focuses its stable input with `preventScroll`.
- `suggestions`: parent publishes `aria-autocomplete="list"`, `aria-controls`,
  `aria-expanded`, and `aria-activedescendant` from this deduplicated state.
  Disposal sends `{expanded:false}` with no controls/active descendant. This is
  directly compatible with `SnowLiveView.updateSuggestions`' state shape.

For the final input port, ensure the parent's canonical input/draft update runs
before callbacks from context discovery publish the outer editor; context's
native input listener reads the actual just-edited value and never owns it.

Both the returned controller and facade expose:

```ts
render(state: Partial<{safe: boolean; editable: boolean; readable: boolean}>): void
capture(text?: string): Readonly<{
  revision: number; items: readonly Readonly<{id: number; version: number}>[];
  content: string; text: string; hasContent: boolean; key: string; owner: object;
}>
accepted(snapshot: ReturnType<typeof capture>): void
hasAttachments(): boolean
pending(): boolean
dispose(): void
```

The facade additionally exposes `forget(key: string): void`. `capture` throws
when unavailable/retired, pending, failed or over limits; otherwise content is
attachment-only JSON and text keeps the legacy attachment-only fallback.
`accepted` matches owner, key and individual captured item versions: later
additions survive, and old-owner ACKs do nothing. Parent Send/Queue admission,
HTTP/abort identity fences and exact outer text-draft ACK revision handling stay
unchanged. No request, POST, storage write or initial discovery was introduced.

## State and verification

Controller state is canonical and synchronous; React publishes snapshots through
`flushSync`, never effects or asynchronous state admission. Drafts remain a
16-owner-key LRU in tab memory. Legacy limits remain: eight attachments/reads,
64 KiB attachment text/labels, 128 KiB prompt-plus-label text, 2 MiB image/read
bytes, 4,096 directory/catalog entries and 256 results per discovery page.
Interrupted reads, stale query/selection fences, explicit-only failed-skill
retry caching, Unicode validation, signature/dimension preview checks and exact
private blob URL revocation are preserved. Retired drafts cannot recreate URLs
when subsequently disposed.

Executed checks:

- `npm run typecheck` (frontend): passed during implementation. Latest rerun
  was blocked by in-progress peer imports (`inspection/Panel` and
  `workspace/Operations` missing), not composer-context diagnostics.
- Focused strict check of final composer-context sources passed:
  `tsc --ignoreConfig --noEmit --strict --target ES2023 --module ESNext
  --moduleResolution Bundler --jsx react-jsx --skipLibCheck
  --allowImportingTsExtensions --noUnusedLocals --noUnusedParameters
  --types node,react,react-dom src/composer-context/index.ts
  src/composer-context/model.test.ts`.
- `node --experimental-strip-types --test src/composer-context/model.test.ts`:
  five tests passed (Unicode, byte signatures, image header limits, private URL
  reuse/revocation, non-blocking unsafe/unavailable previews).
- Standalone production Vite bundle + native headless Chrome/CDP smoke:
  29 assertions passed for JSX mounts, explicit discovery, file read/selection,
  keyboard rows, safe filename token escaping, synchronous parent callbacks,
  late additions/ACK ownership, stale query response rejection, explicit skill
  retry, escaped metadata and private image retire/forget lifecycle. Temporary
  harness/bundle files were removed, not added to the product.

The full existing native composer-context and layout suites, integrated asset
build and install-local remain parent-owned release gates. The first scratch
`--dump-dom` Chrome attempt timed out; the bounded CDP run above succeeded.
The legacy VM suite still reads `static/composer-context.js`; running it alone
would not verify this new React implementation. Parent owns retirement/update
of that test entry and asset registration. Add `model.test.ts` to the main
frontend test script if desired; package.json was outside this task's ownership.
