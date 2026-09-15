import {readFile, writeFile, rm, rename, mkdir, stat} from 'node:fs/promises';
import {join} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
export async function operations({p, ready, check, posts, capture}) {
  const root = ready.directory;
  const count = async () => (await readFile(join(root, 'git-starts'), 'utf8').catch(() => '')).trim().split('\n').filter(Boolean).length;
  const exists = path => stat(path).then(() => true, () => false);
  const waitCount = async n => { const end = Date.now() + 10000; while (Date.now() < end) { if (await count() === n) return; await delay(40); } throw Error('Fictional clone did not reach its durable ACK gate'); };
  const list = async () => (await p.get('/operations')).data?.operations;
  const byName = async name => (await list())?.find(op => op.name === name);
  const awaitState = async (name, state) => {
    const end = Date.now() + 15000;
    while (Date.now() < end) { const op = await byName(name); if (op?.state === state) return op; await delay(45); }
    throw Error('Operation did not reach reviewed state: ' + state);
  };
  await p.navigate('/?view=projects'); await p.wait(`!!${p.q('[data-project-operations]')}`, 'project operations surface');
  await p.click('[data-project-operations] > summary');
  const refresh = async id => { await p.click('[data-op-refresh]'); await p.wait(`!${p.q('[data-op-refresh]')}?.disabled && !!${p.q(`[data-operation-id="${id}"]`)}`, 'durable operation inventory'); };
  const confirm = async () => {
    check(await p.evaluate(`${p.q('[data-op-confirm]')}.disabled`), 'Operation confirmation requires an explicit effects checkbox');
    check(await p.evaluate(`document.activeElement===${p.q('[data-op-back]')}`), 'Review places keyboard focus on the nonmutating Back action');
    await p.click('[data-op-confirm-check]'); await p.click('[data-op-confirm]');
  };
  const admit = async (kind, name) => {
    await p.wait(`!${p.q('[data-op-draft]')}?.disabled && !${p.q('[data-op-parent]')}?.disabled`, 'operation draft becomes available');
    await p.replace('[data-op-parent]', join(root, 'parent'));
    await p.click('[data-op-select]'); await p.wait(`${p.q('[data-op-grant')}?.textContent.startsWith('Selected ')`, 'explicit parent grant');
    await p.select('[data-op-kind]', kind); await p.replace('[data-op-name]', name);
    if (kind === 'clone') await p.replace('[data-op-remote]', 'https://example.invalid/fictional/repo.git');
    await p.click('[data-op-review]'); await p.wait(`!${p.q('[data-op-confirmation]') }?.hidden`, 'operation review');
    check(!await exists(join(root, 'parent', name)), 'Parent selection and review create no destination');
    await confirm();
  };
  const action = async (op, verb) => {
    await refresh(op.id); await p.click(`[data-operation-id="${op.id}"] [data-op-row-action="${verb}"]`); await confirm();
  };
  const activationCount = posts('/runtime/open').length;
  await admit('create', 'browser-created');
  let created = await awaitState('browser-created', 'awaiting_registration');
  await delay(120); created = await byName('browser-created');
  check(created?.state === 'awaiting_registration' && !created.project_id, 'Actual create waits for separate registration without activation');
  await action(created, 'register'); created = await awaitState('browser-created', 'succeeded');
  check(!!created.project_id, 'Explicit revision/identity registration commits create metadata');
  await capture('create-registered', p);
  await admit('clone', 'browser-cloned'); await waitCount(1);
  let cloned = await byName('browser-cloned');
  check(cloned?.state === 'running' && cloned.child?.path && !cloned.project_id, 'Real clone begins only after committed child identity and actual CONTROL ACK');
  await writeFile(join(root, 'release-git'), 'release', {mode: 0o600});
  cloned = await awaitState('browser-cloned', 'awaiting_registration');
  check(!cloned.project_id, 'Successful clone retains files without implicit registration');
  await action(cloned, 'register'); await awaitState('browser-cloned', 'succeeded');
  await rm(join(root, 'release-git'));
  await admit('clone', 'browser-partial'); await waitCount(2);
  let partial = await byName('browser-partial');
  await action(partial, 'cancel'); partial = await awaitState('browser-partial', 'canceled');
  check(await exists(join(root, 'parent', 'browser-partial', 'FICTIONAL_PARTIAL')), 'Native cancellation confirms cleanup and retains partial files');
  // A real host-side replacement changes identity; no browser response is mocked.
  await rename(join(root, 'parent', 'browser-partial'), join(root, 'parent', 'browser-partial-original'));
  await mkdir(join(root, 'parent', 'browser-partial'), {mode: 0o700});
  await action(partial, 'reconcile'); partial = await awaitState('browser-partial', 'needs_review');
  check(partial.outcome === 'unknown' && !partial.project_id, 'Explicit reconciliation observes identity uncertainty without replay or adoption');
  await action(partial, 'dismiss');
  await p.wait(`${p.q('[data-op-status')}?.textContent.includes('record dismissed')`, 'metadata dismissal');
  check(await exists(join(root, 'parent', 'browser-partial-original', 'FICTIONAL_PARTIAL')) && await exists(join(root, 'parent', 'browser-partial')), 'Metadata dismissal preserves both original partial files and replacement directory');
  const before = posts('/projects/clone').length;
  await p.click('[data-project-operations] > summary'); await p.click('[data-project-operations] > summary');
  await p.navigate('/?view=projects'); await p.wait(`!!${p.q('[data-project-operations]')}`, 'reopened projects'); await p.click('[data-project-operations] > summary'); await delay(150);
  check(await count() === 2 && posts('/projects/clone').length === before, 'Panel reopen and full page navigation never replay clone admission');
  check(posts('/runtime/open').length === activationCount, 'Create, clone, registration and reconciliation never activate another worker');
  check(await p.evaluate('document.documentElement.scrollWidth<=innerWidth+1'), 'Project review/inventory fits the narrow and wide viewport');
  await capture('operations-retained', p);
}
