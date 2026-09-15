import test from 'node:test';
import assert from 'node:assert/strict';
import {SHELL_MENU_ID, menuLauncherARIA} from './menu-aria.ts';

test('launcher ARIA belongs only to its canonical popup and stable host ID', () => {
  const menu = {kind: 'session' as const, project: 'project-a', session: 'saved-a'};
  assert.deepEqual(menuLauncherARIA(null, 'session', 'project-a', 'saved-a'), {'aria-expanded': false, 'aria-controls': undefined});
  assert.deepEqual(menuLauncherARIA(menu, 'session', 'project-a', 'saved-a'), {'aria-expanded': true, 'aria-controls': SHELL_MENU_ID});
  assert.equal(menuLauncherARIA(menu, 'session', 'project-b', 'saved-a')['aria-expanded'], false);
  assert.equal(menuLauncherARIA(menu, 'session', 'project-a', 'saved-b')['aria-controls'], undefined);
  assert.equal(menuLauncherARIA(menu, 'project', 'project-a')['aria-expanded'], false);
});

test('revoked empty launcher immediately drops popup ARIA; surviving Rename retains it', () => {
  const menu = {kind: 'session' as const, project: 'project-a', session: 'saved-a'};
  assert.deepEqual(menuLauncherARIA(menu, 'session', 'project-a', 'saved-a', false), {'aria-expanded': false, 'aria-controls': undefined});
  assert.deepEqual(menuLauncherARIA(menu, 'session', 'project-a', 'saved-a', true), {'aria-expanded': true, 'aria-controls': SHELL_MENU_ID});
});

test('view, project and external workspace picker derive the same host reference', () => {
  for (const kind of ['view', 'project', 'workspace'] as const) {
    const project = kind === 'project' ? 'project-a' : '';
    assert.deepEqual(menuLauncherARIA({kind, project, session: ''}, kind, project), {'aria-expanded': true, 'aria-controls': SHELL_MENU_ID});
  }
});
