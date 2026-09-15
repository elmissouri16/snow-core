# Queue React integration

Import the default facade from `queue/index.ts`: `init`, `dispose`, `render`,
`blocking`, `canEnqueue`, `enqueue`. No legacy queue renderer, action effect,
provider call, optimistic transcript insertion, or automatic mutation retry.

## Mount and ownership

The parent supplies one empty wrapper at the former queue section position:

```html
<div data-react-composer-queue style="display: contents"></div>
```

`display: contents` preserves the existing section's flex-item sizing, including
mobile margins. The queue renders the complete `#live-queue-next` section,
heading, error/review/copy notices, controls, and keyed item/editor list there.
The parent must not also retain the old section or attach its old renderer.

The parent owns `#live-prompt`, `[data-queue-next]`, `#composer-hint`, and
`#composer-state`. The native `[data-queue-next]` click bubbles to the queue's
single listener on `api.root`; do not also call `enqueue` for that button.
The parent retains its explicit keyboard/submit route to `enqueue`.

`render(snapshot, ui, false)` updates canonical admission **synchronously**
without publishing React DOM. `blocking()`/`canEnqueue()` read that canonical
state immediately. `render(snapshot, ui)` commits the queue synchronously with
`flushSync`. Both calls return a plain composer presentation object (or null
when unavailable):

- `hidden`, `disabled`, `label`, `title`: apply to the parent-owned Queue next
  button.
- `hint`: apply to the composer hint.
- `state`: override the parent's composer status only when non-null.

Queue code never writes foreign elements or their values. Its native refs focus
its own Edit/Cancel/editor and surviving panel. If a removed queue action should
focus the composer instead, the **parent** must implement that focus policy;
queue code deliberately cannot reach into the parent's input.

## Existing callbacks

`QueueAPI` is exported. Existing `setupQueue` callbacks are unchanged:

- `root: HTMLElement`, `session: string`, `validText(unknown): boolean`.
- `changed(): void` triggers the parent's canonical controls pass.
- `draft(): {value, revision, start, end, direction}` captures exact text and
  selection; `writeDraft(text, selection?)` is the only draft write path.
- `request(action?, fields?)` is the app's identity-bound HTTP/applySnapshot
  hook. Empty action is a read-only reconciliation, never a mutation retry.

The public RuntimeQueue DTO carries `token`, `revision`, `can_enqueue`, and
`items: [{id, text, state}]`. Fields match `internal/web/queue_http.go` and the
native queue fixture. There are **no** browser-supplied request IDs, predecessor
IDs, or slots in this DTO: nonce/CAS and bound session/instance correlation
remain with the existing app/HTTP/runtime owners. Do not invent extra fields.

`UI` carries connected/status/busy/unknown/stopping/editing. Keep goal-running
and manual compaction nonqueueable through the existing authoritative UI/queue
admission. Held/uncertain Copy/Remove is independent of enqueue eligibility.

Run focused validators with:

```sh
node --experimental-strip-types --test src/queue/model.test.ts
```

The parent owns adding this test to package scripts, rebuilding embedded assets,
and running native queue-next/workflow/composer-context acceptance after wiring.
