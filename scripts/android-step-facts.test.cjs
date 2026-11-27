const assert = require('node:assert/strict');
const test = require('node:test');
const {stepFacts} = require('./android-step-facts.cjs');

// A protocol-shaped test: fixture steps around target steps, the last of
// which may stop with a status and reason.
function traced(testId, operations, end = {}) {
  const frames = [{type: 'test_begin', testId}, {type: 'step_end', testId, category: 'fixture', status: 'passed'}];
  operations.forEach((operation, i) => {
    const last = i === operations.length - 1;
    frames.push({type: 'step_end', testId, category: operation.startsWith('assert-') ? 'assertion' : 'action', operation,
      status: last && end.step ? end.step : 'passed', ...(last && end.reason ? {control: 'session.status', reason: end.reason} : {})});
  });
  frames.push({type: 'test_end', testId, status: end.test ?? 'passed'});
  return frames;
}

test('a passing test reports its status and step count only', () => {
  assert.deepEqual(stepFacts(traced('a', ['open', 'reset', 'fill', 'tap', 'assert-text'])), [{status: 'passed', steps: 5}]);
});

test('each test of a stream names its first failed step, in stream order', () => {
  // The 2026-11-26 isolation miss: the second test failed its first read after reset.
  const frames = [
    ...traced('first', ['open', 'reset', 'assert-text', 'tap', 'assert-text', 'assert-text'], {step: 'failed', reason: 'assertion_failed', test: 'failed'}),
    ...traced('second', ['open', 'reset', 'assert-text'], {step: 'failed', reason: 'assertion_failed', test: 'failed'}),
  ];
  assert.deepEqual(stepFacts(frames), [
    {status: 'failed', steps: 6, failed: {step: 6, operation: 'assert-text', reason: 'assertion_failed'}},
    {status: 'failed', steps: 3, failed: {step: 3, operation: 'assert-text', reason: 'assertion_failed'}},
  ]);
});

test('a timeout keeps its own reason and a non-failed status is named', () => {
  const timedOut = traced('a', ['open', 'reset', 'assert-text'], {step: 'failed', reason: 'operation_timeout', test: 'failed'});
  assert.deepEqual(stepFacts(timedOut)[0].failed, {step: 3, operation: 'assert-text', reason: 'operation_timeout'});
  const interrupted = traced('b', ['open', 'tap'], {step: 'interrupted', test: 'interrupted'});
  assert.deepEqual(stepFacts(interrupted), [{status: 'interrupted', steps: 2, failed: {step: 2, operation: 'tap', status: 'interrupted'}}]);
});

test('only fixed words are kept: other text becomes `other` or is dropped', () => {
  const frames = [
    {type: 'step_end', testId: 'a', operation: 'Sent $5 to Ada', status: 'failed', reason: 'expected "Signed in"'},
    {type: 'test_end', testId: 'a', status: 'Signed out'},
  ];
  assert.deepEqual(stepFacts(frames), [{status: 'other', steps: 1, failed: {step: 1, operation: 'other'}}]);
  assert.doesNotMatch(JSON.stringify(stepFacts(frames)), /Sent|Ada|Signed/);
});

test('frames without a test, and a test that never ended, are handled', () => {
  assert.deepEqual(stepFacts([]), []);
  assert.deepEqual(stepFacts([{type: 'run_started'}, null, {type: 'step_end', testId: 'a', operation: 'open', status: 'passed'}]), [{status: 'none', steps: 1}]);
});
