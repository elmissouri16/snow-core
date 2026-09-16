// Application deletion-event ownership contract; native deletion UI is covered
// separately by browser/session-delete/run.mjs. This uses the existing bounded
// app-prefix VM pattern from workspace_session_flow.test.mjs, not a copied handler.
import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const app = readFileSync(new URL('../../internal/web/static/app.js', import.meta.url), 'utf8');
const template = readFileSync(new URL('../../internal/web/templates/pages.html', import.meta.url), 'utf8');
const cut = app.indexOf('  function setTheme(');
assert.ok(cut > app.indexOf('"snow:session-deleted"'), 'Bounded production app prefix includes the deletion event handler');
const initialDraft = (project = 'a', targetSession = 'saved-one') => ({project, targetSession,
  name: 'Fictional workspace', text: 'Unsent startup draft stays local', pending: true,
  ack: {project, session: targetSession, instance: 'worker-a'}});
function fixture(draft = initialDraft()) {
  const events = new Map(), nodes = new Map(), actions = [];
  const add = (name, callback) => { if (!events.has(name)) events.set(name, []); events.get(name).push(callback); };
  const document = {documentElement: {dataset: {}}, body: {addEventListener: add}, addEventListener: add,
    querySelector: selector => nodes.get(selector) || null, querySelectorAll: () => [],
    createElement() { throw new Error('Unexpected DOM creation in ownership-only fixture'); }};
  // Synchronous React-view projection only; the extracted handler below keeps
  // authority over retained drafts and deletion ownership.
  const window = {SnowWorkspace: {updateDraft(value) {
    const prompt = nodes.get('#workspace-prompt');
    if (prompt && 'workspaceEnabled' in value) prompt.disabled = !value.workspaceEnabled;
    if (prompt && 'workspaceText' in value) prompt.value = value.workspaceText;
  }}, SnowNavigation: {visit: (...args) => { actions.push(args); throw new Error('Deletion event must not navigate or send'); }}};
  const context = vm.createContext({document, window, matchMedia: () => ({}), localStorage: {getItem() {}},
    Element: class {}, TextEncoder, URL, encodeURIComponent,
    projectLocation: project => `/?view=projects&project=${encodeURIComponent(project)}`});
  vm.runInContext(app.slice(0, cut) + `
    window.testDeletion = {setDraft: value => { homeDraft = value; }, home: () => homeDraft,
      drafts, editDrafts, reuseDrafts, visitedSessions, uncertain};
  })();`, context);
  const state = window.testDeletion; state.setDraft(draft);
  for (const [map, prefix] of [[state.drafts, 'ordinary'], [state.editDrafts, 'editing'], [state.reuseDrafts, 'reuse'], [state.uncertain, 'uncertain']]) {
    map.set('a:saved-one', {text: `${prefix} deleted-session draft`});
    map.set('a:saved-two', {text: `${prefix} sibling draft`});
    map.set('b:saved-one', {text: `${prefix} other-workspace draft`});
  }
  state.visitedSessions.set('a', 'saved-one'); state.visitedSessions.set('b', 'saved-one');
  const protectedMaps = () => [state.drafts, state.editDrafts, state.reuseDrafts, state.uncertain].map(map => [...map]);
  const before = protectedMaps();
  function emit(detail) { for (const callback of events.get('snow:session-deleted') || []) callback({detail}); }
  function assertPreserved() { assert.deepEqual(protectedMaps(), before); assert.equal(actions.length, 0); }
  return {state, nodes, emit, assertPreserved, actions};
}

test('shared menus load before the React module and controller readiness handshake', () => {
  const menus = template.indexOf('src="/static/menus.js"');
  const module = template.indexOf('type="module" src="/static/generated/app.js"');
  const controller = template.indexOf('src="/static/app.js" defer');
  assert.ok(menus >= 0 && module > menus && controller > module);
  assert.equal([...template.matchAll(/src="\/static\/menus\.js"/g)].length, 1);
  assert.equal([...template.matchAll(/src="\/static\/generated\/app\.js"/g)].length, 1);
  const main = readFileSync(new URL('../../internal/web/frontend/src/main.tsx', import.meta.url), 'utf8');
  const registration = main.indexOf('SnowSessionActions: sessionActions');
  assert.ok(registration >= 0 && main.indexOf("document.dispatchEvent(new Event('snow:react-ready'))") > registration);
  assert.ok(app.includes('document.addEventListener("snow:react-ready", navigation)'));
});

test('matching deletion retargets only startup ownership to New without losing text or drafts', () => {
  const draft = initialDraft(), f = fixture(draft), originalText = draft.text;
  f.emit({project: 'a', session: 'saved-one', instance: 'worker-a'});
  assert.equal(f.state.home(), draft);
  assert.equal(draft.text, originalText); assert.equal(draft.project, 'a');
  assert.equal(draft.targetSession, ''); assert.equal(draft.pending, true); assert.equal(draft.name, 'Fictional workspace');
  assert.equal('ack' in draft, false, 'Deleted target no longer grants automatic startup transfer');
  assert.equal(f.state.visitedSessions.has('a'), false);
  assert.equal(f.state.visitedSessions.get('b'), 'saved-one');
  f.assertPreserved();
});

test('matching deletion can restore preserved text into already mounted New draft', () => {
  const draft = initialDraft(), f = fixture(draft);
  const prompt = {dataset: {draftProject: 'a', draftSession: '', draftName: 'Fictional workspace'}, value: '', disabled: true};
  f.nodes.set('#workspace-prompt', prompt);
  f.emit({project: 'a', session: 'saved-one', instance: ''});
  assert.equal(prompt.value, draft.text); assert.equal(prompt.disabled, false);
  assert.equal(draft.targetSession, ''); f.assertPreserved();
});

for (const [label, project, session] of [
  ['other workspace with same session ID', 'b', 'saved-one'],
  ['same workspace sibling session', 'a', 'saved-two'],
  ['other workspace and session', 'b', 'saved-two']
]) test(`deletion leaves ${label} startup draft and acknowledgement untouched`, () => {
  const draft = initialDraft(project, session), before = structuredClone(draft), f = fixture(draft);
  const ack = draft.ack;
  f.emit({project: 'a', session: 'saved-one', instance: 'worker-a'});
  assert.deepEqual(draft, before); assert.equal(draft.ack, ack);
  assert.equal(f.state.visitedSessions.get('b'), 'saved-one');
  f.assertPreserved();
});

test('deleting an older sibling does not clear a newer visited-session hint', () => {
  const f = fixture(initialDraft('b', 'saved-one'));
  f.state.visitedSessions.set('a', 'saved-two');
  f.emit({project: 'a', session: 'saved-one', instance: ''});
  assert.equal(f.state.visitedSessions.get('a'), 'saved-two'); f.assertPreserved();
});

test('repeated same-target metadata is idempotent and never sends', () => {
  const draft = initialDraft(), f = fixture(draft);
  const receipt = {project: 'a', session: 'saved-one', instance: 'worker-a'};
  f.emit(receipt); const once = structuredClone(draft); f.emit(receipt);
  assert.deepEqual(draft, once); f.assertPreserved();
});

for (const [label, detail] of [
  ['missing detail', undefined], ['empty detail', {}], ['non-string project', {project: 1, session: 'saved-one'}],
  ['non-string session', {project: 'a', session: 1}], ['empty session', {project: 'a', session: ''}]
]) test(`malformed deletion metadata (${label}) cannot clear draft or visited ownership`, () => {
  const draft = initialDraft(), before = structuredClone(draft), f = fixture(draft);
  f.emit(detail);
  assert.deepEqual(draft, before); assert.equal(f.state.visitedSessions.get('a'), 'saved-one');
  f.assertPreserved();
});
