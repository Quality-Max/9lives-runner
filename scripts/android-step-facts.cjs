'use strict';
// Per test, in stream order: its terminal status, how many target steps it
// ran and its first target step that did not pass (position, operation,
// status when not `failed`, and stop reason). Fixed words and counts from the
// engine-validated protocol stream; controls, titles and messages are not
// kept. A hosted failure often cannot be reproduced, so the Android smoke
// prints these on every result line, qualified or not.
const word = /^[a-z][a-z-]{0,39}$/;
const reasonWord = /^[a-z][a-z_]{0,63}$/;

function stepFacts(frames) {
  const tests = new Map();
  for (const frame of frames) {
    if (!frame || typeof frame.testId !== 'string') continue;
    if (!tests.has(frame.testId)) tests.set(frame.testId, {steps: 0});
    const entry = tests.get(frame.testId);
    if (frame.type === 'step_end' && typeof frame.operation === 'string') {
      entry.steps++;
      if (frame.status !== 'passed' && !entry.failed) {
        entry.failed = {
          step: entry.steps,
          operation: word.test(frame.operation) ? frame.operation : 'other',
          ...(frame.status !== 'failed' ? {status: word.test(String(frame.status)) ? frame.status : 'other'} : {}),
          ...(reasonWord.test(String(frame.reason ?? '')) ? {reason: frame.reason} : {}),
        };
      }
    }
    if (frame.type === 'test_end') entry.status = word.test(String(frame.status)) ? frame.status : 'other';
  }
  return [...tests.values()].map(({status, steps, failed}) => ({status: status ?? 'none', steps, ...(failed ? {failed} : {})}));
}

module.exports = {stepFacts};
