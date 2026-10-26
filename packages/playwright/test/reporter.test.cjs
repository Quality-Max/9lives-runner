const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

// The reporter is constructed under an engine identity and writes one frame
// per step. A matcher nested inside another expect step, as expect.poll
// reports its inner matcher, must not become a second assertion frame.
test('a nested expect step is not counted as a second assertion', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-reporter-'));
  const events = path.join(dir, 'events.ndjson');
  const saved = {...process.env};
  Object.assign(process.env, {
    NINELIVES_ENGINE_PROTOCOL: '9l.engine/1', NINELIVES_RUN_ID: 'run-1', NINELIVES_JOB_ID: 'job-001',
    NINELIVES_ATTEMPT_ID: 'job-001-attempt-001', NINELIVES_ENGINE_EVENTS: events,
  });
  delete process.env.NINELIVES_TARGET;
  try {
    const Reporter = require('../dist/reporter.js').default;
    const reporter = new Reporter();
    const testCase = {id: 'test-1'};
    const result = {retry: 0};
    const poll = {category: 'expect', title: 'expect.poll.toBe'};
    reporter.onStepEnd(testCase, result, {category: 'expect', title: 'expect.toBe'});
    reporter.onStepEnd(testCase, result, poll);
    reporter.onStepEnd(testCase, result, {category: 'expect', title: 'expect.toBe', parent: poll});
    reporter.onStepEnd(testCase, result, {category: 'pw:api', title: 'locator.click', parent: {category: 'test.step', title: 'checkout'}});
    // toPass runs its callback, here a test.step, on every retry: the expect
    // inside it is two levels below the toPass step, and failed retries
    // must not become failed assertion frames either.
    const toPass = {category: 'expect', title: 'expect.toPass'};
    const body = {category: 'test.step', title: 'retry body', parent: toPass};
    reporter.onStepEnd(testCase, result, {category: 'expect', title: 'expect.toBe', parent: body, error: {message: 'retry'}});
    reporter.onStepEnd(testCase, result, {category: 'expect', title: 'expect.toBe', parent: body});
    reporter.onStepEnd(testCase, result, toPass);
    // An expect inside an ordinary test.step is still its own assertion.
    reporter.onStepEnd(testCase, result, {category: 'expect', title: 'expect.toBe', parent: {category: 'test.step', title: 'checkout'}});
    const frames = fs.readFileSync(events, 'utf8').trim().split('\n').map(line => JSON.parse(line));
    assert.deepEqual(frames.map(frame => frame.category), ['assertion', 'assertion', 'browser', 'assertion', 'assertion']);
    assert.deepEqual(frames.map(frame => frame.status), ['passed', 'passed', 'passed', 'passed', 'passed']);
    assert.deepEqual(frames.map(frame => frame.stepId), ['step-1', 'step-2', 'step-3', 'step-4', 'step-5'], 'skipped steps do not consume step ids');
  } finally {
    for (const key of Object.keys(process.env)) if (!(key in saved)) delete process.env[key];
    Object.assign(process.env, saved);
    fs.rmSync(dir, {recursive: true, force: true});
  }
});
