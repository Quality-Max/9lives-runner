const assert = require('node:assert/strict');

// Called only after the engine has validated the receipt and source provenance.
function assertIsolationEvidence(frames, mode) {
  assert(['isolation', 'isolation-defect'].includes(mode), 'isolation: unknown qualification mode');
  const terminals = frames.filter(f => f.type === 'test_end');
  assert.deepEqual(terminals.map(f => f.status), ['failed', mode === 'isolation' ? 'passed' : 'failed'], 'isolation: exact terminal outcomes');
  const ids = new Set(terminals.map(f => f.testId));
  assert.equal(ids.size, 2, 'isolation: distinct test identities');
  const steps = frames.filter(f => f.type === 'step_end');
  assert(steps.filter(f => !f.operation).every(f => f.status === 'passed'), 'isolation: fixture and hook steps must pass');
  const targetSteps = steps.filter(f => f.operation);
  assert(targetSteps.every(f => ids.has(f.testId)), 'isolation: foreign target steps');
  // Passing controls are redacted by the reporter. The exact per-test order,
  // together with matched source provenance, identifies each prerequisite and
  // the final assertion without retaining expected text or page content.
  const operations = [
    ['open', 'reset', 'assert-text', 'tap', 'assert-text', 'assert-text'],
    ['open', 'reset', 'assert-text', 'tap', 'assert-text', 'terminate', 'activate', 'assert-text', 'reset', 'assert-text'],
  ];
  terminals.forEach((terminal, index) => {
    const actual = targetSteps.filter(f => f.testId === terminal.testId)
      .map(({category, operation, status, control, reason}) => ({category, operation, status, control, reason}));
    const expected = operations[index].map((operation, i, sequence) => {
      const failed = i === sequence.length - 1 && terminal.status === 'failed';
      return {
        category: operation.startsWith('assert-') ? 'assertion' : 'action', operation,
        status: failed ? 'failed' : 'passed',
        control: failed ? 'session.status' : undefined,
        reason: failed ? 'assertion_failed' : undefined,
      };
    });
    assert.deepEqual(actual, expected, `isolation: test ${index + 1} must complete its prerequisite and assertion sequence`);
  });
  return terminals;
}

module.exports = {assertIsolationEvidence};
