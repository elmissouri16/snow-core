import assert from 'node:assert/strict';
import {test} from 'node:test';
import {answer, identifier, invalidAnswer, permissionBlocked, requestKey, validInput} from './model.ts';
import type {Draft, InputRequest} from './model.ts';
const input: InputRequest = {id: 'request-1', questions: [{id: 'q-1', header: 'Choose', question: 'Which?', options: [{label: 'One'}, {label: 'Two'}], choices_only: true}, {id: 'q-2', question: 'Explain'}]};
const draft = (): Draft => ({key: 'scope', identity: ['project', 'session', 'instance'], page: 0, collapsed: false, updated: 0, answers: Object.create(null)});
test('bounded input projection rejects incomplete or ambiguous IDs and options', () => {
  assert.equal(validInput(input), true);
  for (const value of [null, {}, {...input, id: 'bad/id'}, {...input, id: 'x'.repeat(129)}, {...input, questions: []},
    {...input, questions: Array(17).fill(input.questions[0])}, {...input, questions: [input.questions[0], input.questions[0]]},
    {...input, questions: [{id: 'q', question: 'why', options: [{label: ''}]}]},
    {...input, questions: [{id: 'q', question: 'why', choices_only: true}]},
    {...input, questions: [{id: 'q', question: 'why', options: [{label: 'x'.repeat(513)}]}]},
    {...input, questions: [{id: 'q', question: '\ud800'}]},
    {...input, questions: [{id: 'q', question: 'why', options: 'not an array'}]}]) assert.equal(validInput(value), false);
  assert.equal(identifier('normal-ID_12'), true);
  assert.equal(identifier('한글'), false);
  assert.equal(validInput({id: 'utf8', questions: [{id: 'q', question: '😀'}]}), true);
  assert.equal(validInput({id: 'utf8', questions: [{id: 'q', question: '😀'.repeat(2049)}]}), false);
  assert.equal(validInput({id: 'total', questions: Array.from({length: 9}, (_, i) => ({id: `q-${i}`, question: 'x'.repeat(8192)}))}), false);
});
test('explicit answers only; choices-only cannot inherit free text or submit blank answers', () => {
  const state = draft();
  assert.equal(invalidAnswer(input.questions, state, true), 0);
  state.answers['q-1'] = {selected: 'custom', custom: 'One'};
  assert.equal(answer(input.questions[0], state.answers['q-1']), '');
  assert.equal(invalidAnswer(input.questions, state, true), 0);
  state.answers['q-1'] = {selected: 1, custom: 'ignored'};
  assert.equal(answer(input.questions[0], state.answers['q-1']), 'Two');
  assert.equal(invalidAnswer(input.questions, state, false), -1);
  assert.equal(invalidAnswer(input.questions, state, true), 1);
  state.answers['q-2'] = {selected: 'custom', custom: ' \n '};
  assert.equal(invalidAnswer(input.questions, state, true), 1);
  state.answers['q-2'].custom = 'Useful answer 😀';
  assert.equal(invalidAnswer(input.questions, state, true), -1);
  state.answers['q-2'].custom = 'invalid\0answer';
  assert.equal(invalidAnswer(input.questions, state, true), 1);
  state.answers['q-2'].custom = '\ud800';
  assert.equal(invalidAnswer(input.questions, state, true), 1);
});
test('answer aggregate is bounded in UTF-8 bytes before app POST extraction', () => {
  const state = draft(), questions = Array.from({length: 9}, (_, i) => ({id: `q-${i}`, question: 'Explain'}));
  for (const q of questions) state.answers[q.id] = {selected: 'custom', custom: '😀'.repeat(2048)};
  assert.equal(invalidAnswer(questions, state, true), 8);
});
test('stable canonical snapshot key retains input state but retires changed question/turn authority', () => {
  const key = requestKey('input', input, 'turn-1');
  const reordered = {questions: input.questions.map(q => ({options: q.options?.map(o => ({description: o.description, label: o.label})), choices_only: q.choices_only, question: q.question, header: q.header, id: q.id})), id: input.id};
  assert.equal(requestKey('input', reordered, 'turn-1'), key);
  assert.notEqual(requestKey('input', input, 'turn-2'), key);
  assert.notEqual(requestKey('input', {...input, id: 'request-2'}, 'turn-1'), key);
  assert.notEqual(requestKey('input', {...input, questions: [{...input.questions[0], id: 'different'}, input.questions[1]]}, 'turn-1'), key);
  assert.notEqual(requestKey('input', {...input, questions: [{...input.questions[0], options: [{label: 'Different'}]}, input.questions[1]]}, 'turn-1'), key);
});
test('permission rejects incomplete summaries and never transfers request/tool scope', () => {
  const permission = {id: 'permission-1', tool: 'read', paths: ['README.md'], effects: [{type: 'filesystem', resource: 'README.md'}]};
  assert.equal(permissionBlocked(permission), false);
  // Unknown effects stay visibly warned, as before; truncation is the hard approval block.
  assert.equal(permissionBlocked({...permission, unknown: true}), false);
  assert.equal(permissionBlocked({...permission, agent_path: '/root/investigator', agent_role: 'explorer'}), false);
  for (const value of [null, {...permission, id: ''}, {...permission, truncated: true}, {...permission, agent_path: 'x'.repeat(513)}, {...permission, paths: Array(65).fill('file')},
    {...permission, effects: Array(65).fill({})}, {...permission, effects: [{resource: {private: 'not public text'}}]}, {...permission, paths: 'not an array'}]) assert.equal(permissionBlocked(value), true);
  const key = requestKey('permission', permission, 'turn');
  assert.equal(requestKey('permission', {...permission, effects: [{resource: 'README.md', type: 'filesystem'}]}, 'turn'), key);
  assert.notEqual(requestKey('permission', {...permission, tool: 'bash'}, 'turn'), key);
  assert.notEqual(requestKey('permission', {...permission, agent_path: '/root/investigator'}, 'turn'), key);
  assert.notEqual(requestKey('permission', {...permission, id: 'permission-2'}, 'turn'), key);
  assert.notEqual(requestKey('permission', permission, 'other-turn'), key);
});
