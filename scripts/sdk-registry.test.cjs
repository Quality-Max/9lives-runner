const assert = require('node:assert/strict');
const test = require('node:test');
const {registryHas, awaitVersion} = require('./sdk-registry.cjs');

test('only an exact registry match counts as published', () => {
  const answer = result => () => result;
  assert.equal(registryHas('0.1.3', answer({status: 0, stdout: '0.1.3\n'})), true);
  assert.equal(registryHas('0.1.3', answer({status: 1, stdout: ''})), false, 'E404 for a missing version');
  assert.equal(registryHas('0.1.3', answer({status: 0, stdout: '0.1.30\n'})), false);
  assert.equal(registryHas('0.1.3', answer({status: 0, stdout: ''})), false);
  assert.equal(registryHas('0.1.3', answer({error: new Error('timeout'), status: null, stdout: ''})), false);
});

test('a version that appears late is still seen', async () => {
  let lookups = 0;
  const sleeps = [];
  const seen = await awaitVersion('0.1.3', {attempts: 5, intervalMs: 15000, has: () => ++lookups === 3, sleep: async ms => sleeps.push(ms)});
  assert.equal(seen, true);
  assert.equal(lookups, 3);
  assert.deepEqual(sleeps, [15000, 15000]);
});

test('a version that never appears is reported after the last attempt, without a trailing wait', async () => {
  let lookups = 0;
  const sleeps = [];
  const seen = await awaitVersion('0.1.3', {attempts: 4, intervalMs: 1, has: () => (lookups++, false), sleep: async ms => sleeps.push(ms)});
  assert.equal(seen, false);
  assert.equal(lookups, 4);
  assert.equal(sleeps.length, 3);
});

test('one attempt looks once and never waits', async () => {
  const sleeps = [];
  assert.equal(await awaitVersion('0.1.3', {attempts: 1, intervalMs: 15000, has: () => false, sleep: async ms => sleeps.push(ms)}), false);
  assert.deepEqual(sleeps, []);
});
