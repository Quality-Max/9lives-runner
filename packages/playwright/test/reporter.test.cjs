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

// Playwright sets test.expectedStatus from the worker when an attempt ends, so
// after a runtime test.fail() (in the body or, on 1.61.1 through 1.64.1, in an
// after hook) the TestCase reads "failed" from then on. test_begin must keep
// the status declared before any attempt ran, on every retry, and test_end the
// final one, so Go can tell a declared expected failure from a relabeled one.
test('test_begin carries the expected status declared before the test ran', () => {
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
    const testCase = {id: 'test-1', expectedStatus: 'passed', titlePath: () => ['', '', 'a.spec.ts', 'relabeled'], outcome: () => 'expected'};
    reporter.onBegin({}, {allTests: () => [testCase]});
    reporter.onTestBegin(testCase, {retry: 0});
    testCase.expectedStatus = 'failed'; // the dispatcher's update when the attempt ends
    reporter.onTestEnd(testCase, {retry: 0, status: 'failed', attachments: []});
    reporter.onTestBegin(testCase, {retry: 1});
    reporter.onTestEnd(testCase, {retry: 1, status: 'failed', attachments: []});
    reporter.onEnd({status: 'passed'});
    const frames = fs.readFileSync(events, 'utf8').trim().split('\n').map(line => JSON.parse(line));
    assert.deepEqual(frames.map(frame => frame.type), ['hello', 'test_begin', 'test_end', 'test_begin', 'test_end', 'test_result', 'end']);
    assert.deepEqual(frames[0].capabilities, ['steps', 'artifact-metadata', 'terminal-outcomes', 'declared-outcomes']);
    assert.deepEqual(frames.filter(frame => frame.type === 'test_begin').map(frame => frame.expectedStatus), ['passed', 'passed']);
    assert.deepEqual(frames.filter(frame => frame.type === 'test_end').map(frame => frame.expectedStatus), ['failed', 'failed']);
  } finally {
    for (const key of Object.keys(process.env)) if (!(key in saved)) delete process.env[key];
    Object.assign(process.env, saved);
    fs.rmSync(dir, {recursive: true, force: true});
  }
});

// A native driver's setup classification reaches Go as one closed frame per
// attempt; a web target, a second record or content cannot.
test('a target setup attachment becomes one bounded target_setup frame', () => {
  const run = (target, attachments) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-reporter-'));
    const events = path.join(dir, 'events.ndjson');
    const saved = {...process.env};
    Object.assign(process.env, {
      NINELIVES_ENGINE_PROTOCOL: '9l.engine/1', NINELIVES_RUN_ID: 'run-1', NINELIVES_JOB_ID: 'job-001',
      NINELIVES_ATTEMPT_ID: 'job-001-attempt-001', NINELIVES_ENGINE_EVENTS: events, NINELIVES_TARGET: target,
    });
    try {
      const Reporter = require('../dist/reporter.js').default;
      const reporter = new Reporter();
      reporter.onTestEnd({id: 'test-1', expectedStatus: 'passed'}, {retry: 0, status: 'failed', attachments});
      return fs.readFileSync(events, 'utf8').trim().split('\n').map(line => JSON.parse(line));
    } finally {
      for (const key of Object.keys(process.env)) if (!(key in saved)) delete process.env[key];
      Object.assign(process.env, saved);
      fs.rmSync(dir, {recursive: true, force: true});
    }
  };
  const record = {classification: 'blocked', reason: 'device-locked', stage: 'open'};
  const attachment = body => ({name: '9l.target-setup', contentType: 'application/vnd.9l.target-setup+json', body: Buffer.from(JSON.stringify(body))});
  const frames = run('android-fake:finance', [attachment(record)]);
  assert.deepEqual(frames.map(f => f.type), ['target_setup', 'test_end']);
  assert.deepEqual(frames[0].setup, record);
  assert.equal(frames[0].retry, 0);
  const recovered = {classification: 'dirty', reason: 'extra-window', stage: 'open', recovery: {method: 'back', presses: 2, restored: true}};
  assert.deepEqual(run('android:finance', [attachment(recovered)])[0].setup, recovered);
  // A recovered baseline before the test and a lock found after a failed
  // assertion are two records at distinct stages, in that order.
  const rechecked = {classification: 'blocked', reason: 'device-locked', stage: 'recheck'};
  assert.deepEqual(run('android:finance', [attachment(recovered), attachment(rechecked)]).map(f => f.type), ['target_setup', 'target_setup', 'test_end']);
  for (const [target, attachments] of [
    ['web:finance', [attachment(record)]],
    ['android-fake:finance', [attachment(record), attachment(record)]],
    ['android-fake:finance', [attachment(rechecked), attachment(rechecked)]],
    ['android-fake:finance', [attachment(rechecked), attachment(recovered)]],
    ['android-fake:finance', [attachment(recovered), attachment(rechecked), attachment(rechecked)]],
    ['android-fake:finance', [attachment({...record, reason: 'PIN visible on screen'})]],
    ['android-fake:finance', [attachment({...record, note: 'content'})]],
    ['android-fake:finance', [{...attachment(record), contentType: 'application/json'}]],
    ['android-fake:finance', [{name: '9l.target-setup', contentType: 'application/vnd.9l.target-setup+json', body: Buffer.from('{')}]],
  ]) {
    assert.deepEqual(run(target, attachments).map(f => f.type), ['engine_error'], target);
  }
});

test('a target diagnostic attachment becomes one bounded target_diagnostic frame', () => {
  const run = (target, attachments) => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-reporter-'));
    const events = path.join(dir, 'events.ndjson');
    const saved = {...process.env};
    Object.assign(process.env, {
      NINELIVES_ENGINE_PROTOCOL: '9l.engine/1', NINELIVES_RUN_ID: 'run-1', NINELIVES_JOB_ID: 'job-001',
      NINELIVES_ATTEMPT_ID: 'job-001-attempt-001', NINELIVES_ENGINE_EVENTS: events, NINELIVES_TARGET: target,
    });
    try {
      const Reporter = require('../dist/reporter.js').default;
      const reporter = new Reporter();
      reporter.onTestEnd({id: 'test-1', expectedStatus: 'passed'}, {retry: 0, status: 'failed', attachments});
      return fs.readFileSync(events, 'utf8').trim().split('\n').map(line => JSON.parse(line));
    } finally {
      for (const key of Object.keys(process.env)) if (!(key in saved)) delete process.env[key];
      Object.assign(process.env, saved);
      fs.rmSync(dir, {recursive: true, force: true});
    }
  };
  const record = {operation: 'assert-hidden', reason: 'assertion_failed', elapsedMs: 10000, x: 390, y: 790, point: true, viewportWidth: 1080, viewportHeight: 2340, windowsBefore: 2, windowsAfter: 1, focusBefore: 'app', focusAfter: 'app'};
  const attachment = body => ({name: '9l.target-diagnostic', contentType: 'application/vnd.9l.target-diagnostic+json', body: Buffer.from(JSON.stringify(body))});
  const frames = run('android:finance', [attachment(record)]);
  assert.deepEqual(frames.map(f => f.type), ['target_diagnostic', 'test_end']);
  assert.deepEqual(frames[0].diagnostic, record);
  for (const [target, attachments] of [
    ['web:finance', [attachment(record)]],
    ['android:finance', [attachment(record), attachment(record)]],
    ['android:finance', [attachment({...record, reason: 'Close model picker still displayed'})]],
    ['android:finance', [attachment({...record, hierarchy: '<node/>'})]],
    ['android:finance', [attachment({...record, x: undefined, y: undefined})]],
  ]) {
    assert.deepEqual(run(target, attachments).map(f => f.type), ['engine_error'], target);
  }
});

// A failed typed target step sends the fixture's stop reason token and nothing
// else of its error; other errors, user steps and passed steps send none.
test('a failed target operation carries only its stop reason token', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), '9l-reporter-'));
  const events = path.join(dir, 'events.ndjson');
  const saved = {...process.env};
  Object.assign(process.env, {
    NINELIVES_ENGINE_PROTOCOL: '9l.engine/1', NINELIVES_RUN_ID: 'run-1', NINELIVES_JOB_ID: 'job-001',
    NINELIVES_ATTEMPT_ID: 'job-001-attempt-001', NINELIVES_ENGINE_EVENTS: events, NINELIVES_TARGET: 'web:finance',
  });
  try {
    const Reporter = require('../dist/reporter.js').default;
    const reporter = new Reporter();
    const location = {file: fs.realpathSync(path.join(__dirname, '..', 'dist', 'targets.js')), line: 1, column: 1};
    const typed = (title, error) => ({category: 'test.step', title, location, ...(error === undefined ? {} : {error: {message: error}})});
    const testCase = {id: 'test-1'};
    const result = {retry: 0};
    reporter.onStepEnd(testCase, result, typed('9l target assert-text', 'Error: 9lives target stopped: assertion_failed'));
    reporter.onStepEnd(testCase, result, typed('9l target tap', '9lives target stopped: deadline'));
    reporter.onStepEnd(testCase, result, typed('9l target open', 'Error: 9lives target stopped: setup_blocked\n    at open (targets.js:1:1)'));
    reporter.onStepEnd(testCase, result, typed('9l target fill', 'Error: 9lives target stopped: Card 4242 declined'));
    // The fixed range detail is recognized; only the token crosses, and any
    // other parenthetical is not a reason.
    reporter.onStepEnd(testCase, result, typed('9l target assert-visible', 'Error: 9lives target stopped: invalid_timeout (accepted 1-180000 ms)'));
    reporter.onStepEnd(testCase, result, typed('9l target tap', 'Error: 9lives target stopped: invalid_timeout (accepted 1-60000 ms, card 4242)'));
    reporter.onStepEnd(testCase, result, typed('9l target fill', 'Error: element "Send" is not visible at https://shop.invalid/?token=secret'));
    reporter.onStepEnd(testCase, result, typed('9l target assert-visible'));
    reporter.onStepEnd(testCase, result, {category: 'test.step', title: 'my step', error: {message: 'Error: 9lives target stopped: assertion_failed'}});
    // A failed control operation names its identifier-shaped test ID; a
    // passed one does not, and a title that is not "<operation> <id>" names
    // no control (the whole remainder stays the operation, which Go rejects).
    reporter.onStepEnd(testCase, result, typed('9l target tap login-button.v2', 'Error: 9lives target stopped: missing_test_id'));
    reporter.onStepEnd(testCase, result, typed('9l target assert-text receipt'));
    reporter.onStepEnd(testCase, result, typed('9l target tap pay 4242', 'Error: 9lives target stopped: missing_test_id'));
    const frames = fs.readFileSync(events, 'utf8').trim().split('\n').map(line => JSON.parse(line));
    assert.deepEqual(frames.map(frame => [frame.operation, frame.status, frame.reason]), [
      ['assert-text', 'failed', 'assertion_failed'],
      ['tap', 'failed', 'deadline'],
      ['open', 'failed', undefined],
      ['fill', 'failed', undefined],
      ['assert-visible', 'failed', 'invalid_timeout'],
      ['tap', 'failed', undefined],
      ['fill', 'failed', undefined],
      ['assert-visible', 'passed', undefined],
      [undefined, 'failed', undefined],
      ['tap', 'failed', 'missing_test_id'],
      ['assert-text', 'passed', undefined],
      ['tap pay 4242', 'failed', 'missing_test_id'],
    ]);
    assert.deepEqual(frames.map(frame => frame.control), [undefined, undefined, undefined, undefined, undefined, undefined, undefined, undefined, undefined, 'login-button.v2', undefined, undefined]);
    const raw = fs.readFileSync(events, 'utf8');
    for (const leaked of ['Card', 'card', 'Send', 'https://', 'token', 'targets.js', 'accepted']) assert(!raw.includes(leaked), leaked);
  } finally {
    for (const key of Object.keys(process.env)) if (!(key in saved)) delete process.env[key];
    Object.assign(process.env, saved);
    fs.rmSync(dir, {recursive: true, force: true});
  }
});
