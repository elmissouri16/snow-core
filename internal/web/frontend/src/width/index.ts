import {mount} from './controller';

export type WidthController = NonNullable<ReturnType<typeof mount>>;
let owner: WidthController | null = null;
function dispose() { owner?.dispose(); owner = null; }
function init(region: HTMLElement | null) {
  dispose();
  if (region) owner = mount(region);
  return owner;
}
export const width = Object.freeze({init, dispose, reset: () => owner?.reset()});
export default width;
