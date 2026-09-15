import {mount} from './controller';

export type ReaderController = NonNullable<ReturnType<typeof mount>>;
let owner: ReaderController | null = null;

function dispose() {
  owner?.dispose();
  owner = null;
}
function init(region: HTMLElement | null, key: unknown = '') {
  dispose();
  if (region) owner = mount(region, String(key || ''));
  return owner;
}

// The parent controls the synchronous message/attention/composer transaction.
// Layout notifications schedule the same bounded reconciliation; they never
// create another turn loop, scroll writer, or transport owner.
export const reader = Object.freeze({
  init, dispose,
  beforeUpdate: () => owner?.beforeUpdate(),
  afterUpdate: () => owner?.afterUpdate(),
  follow: () => owner?.follow(),
  wheelFromHandle: (event: WheelEvent) => owner?.wheelFromHandle(event),
  onLayout: () => owner?.onLayout(),
  userIntent: () => owner?.userIntent(),
});
export default reader;
