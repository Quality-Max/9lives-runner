const assert = require('node:assert/strict');
const test = require('node:test');
const {assertIsolationEvidence} = require('./android-isolation-evidence.cjs');

// Protocol-shaped traces of the private isolation spec. Passing target steps
// intentionally omit controls, as the real reporter does for privacy.
function trace(mode) {
  let step = 0;
  return [
    ['open', 'reset', 'assert-text', 'tap', 'assert-text', 'assert-text'],
    ['open', 'reset', 'assert-text', 'tap', 'assert-text', 'terminate', 'activate', 'assert-text', 'reset', 'assert-text'],
  ].flatMap((operations, index) => {
    const testId = `test-${index}`;
    const status = index === 0 || mode === 'isolation-defect' ? 'failed' : 'passed';
    const fixture = () => ({type: 'step_end', testId, stepId: `step-${++step}`, category: 'fixture', status: 'passed'});
    return [
      {type: 'test_begin', testId, retry: 0, expectedStatus: 'passed'}, fixture(),
      ...operations.map((operation, i) => ({
        type: 'step_end', testId, stepId: `step-${++step}`,
        category: operation.startsWith('assert-') ? 'assertion' : 'action', operation,
        status: i === operations.length - 1 ? status : 'passed',
        ...(i === operations.length - 1 && status === 'failed' ? {control: 'session.status', reason: 'assertion_failed'} : {}),
      })),
      fixture(), {type: 'test_end', testId, status},
    ];
  });
}

for (const mode of ['isolation', 'isolation-defect']) {
  test(`${mode}: accepts the complete prerequisite and assertion sequence`, () => {
    assert.doesNotThrow(() => assertIsolationEvidence(trace(mode), mode));
  });
  test(`${mode}: rejects a teardown failure in the intentionally failing test`, () => {
    const frames = trace(mode);
    frames.filter(f => f.testId === 'test-0' && f.category === 'fixture').at(-1).status = 'failed';
    assert.throws(() => assertIsolationEvidence(frames, mode), /isolation:/);
  });
}

test('rejects the first test failing before sign-in', () => {
  const frames = trace('isolation');
  const failure = frames.find(f => f.testId === 'test-0' && f.operation === 'assert-text');
  Object.assign(failure, {status: 'failed', control: 'session.status', reason: 'assertion_failed'});
  const truncated = frames.filter(f => f.testId !== 'test-0' || !f.operation || frames.indexOf(f) <= frames.indexOf(failure));
  assert.throws(() => assertIsolationEvidence(truncated, 'isolation'), /isolation:/);
});

test('rejects the defect control failing its persistence assertion before the final reset', () => {
  const frames = trace('isolation-defect');
  const activateIndex = frames.findIndex(f => f.operation === 'activate');
  const failure = frames[activateIndex + 1];
  Object.assign(failure, {status: 'failed', control: 'session.status', reason: 'assertion_failed'});
  const truncated = frames.filter(f => f.testId !== 'test-1' || !f.operation || frames.indexOf(f) <= frames.indexOf(failure));
  assert.throws(() => assertIsolationEvidence(truncated, 'isolation-defect'), /isolation:/);
});

test('rejects an omitted final reset even if all assertion statuses match', () => {
  const frames = trace('isolation');
  const finalReset = frames.filter(f => f.testId === 'test-1' && f.operation === 'reset').at(-1);
  assert.throws(() => assertIsolationEvidence(frames.filter(f => f !== finalReset), 'isolation'), /isolation:/);
});

test('rejects prerequisite steps attributed to the other test', () => {
  const frames = trace('isolation');
  frames.find(f => f.testId === 'test-1' && f.operation === 'tap').testId = 'test-0';
  assert.throws(() => assertIsolationEvidence(frames, 'isolation'), /isolation:/);
});
