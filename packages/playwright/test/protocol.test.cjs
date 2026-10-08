const {test} = require('node:test');
const assert = require('node:assert/strict');
const {engineIdentity, protocolVersion, skipPinKey} = require('../dist/protocol.js');

test('engine handshake requires supported protocol and all identities', () => {
  assert.throws(() => engineIdentity({}), /requires a supported Go engine/);
  assert.throws(() => engineIdentity({NINELIVES_ENGINE_PROTOCOL: '9l.engine/99'}), /requires a supported Go engine/);
  assert.throws(() => engineIdentity({NINELIVES_ENGINE_PROTOCOL: protocolVersion}), /complete engine attempt identity/);
  const env = {NINELIVES_ENGINE_PROTOCOL: protocolVersion, NINELIVES_RUN_ID: 'run-1', NINELIVES_JOB_ID: 'job-001', NINELIVES_ATTEMPT_ID: 'job-001-attempt-001'};
  assert.deepEqual(engineIdentity(env), {version: protocolVersion, runId: 'run-1', jobId: 'job-001', attemptId: 'job-001-attempt-001'});
  assert.throws(() => engineIdentity({...env, NINELIVES_RUN_ID: 'bad\nidentity'}), /complete engine attempt identity/);
});

test('skip pin key drops root and project suites and normalizes separators', () => {
  // Same digest as SkipPinKey in internal/adapters/playwrightsdk (Go parity test).
  const key = '65ff0be8a3e9f3cb35403edfbc5b19fc47af011c6764abf4b42abead846caedc';
  const pin = skipPinKey(['', 'chromium', 'tests/skipped.spec.ts', 'selected test has no execution evidence'], '/');
  assert.equal(skipPinKey(['', '', 'tests\\skipped.spec.ts', 'selected test has no execution evidence'], '\\'), pin);
  assert.match(pin, /^[a-f0-9]{64}$/);
  assert.equal(pin, key);
});
