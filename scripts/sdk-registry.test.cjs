const assert = require('node:assert/strict');
const test = require('node:test');
const {registryHas} = require('./sdk-registry.cjs');

test('only an exact registry match counts as published', () => {
  const answer = result => () => result;
  assert.equal(registryHas('0.2.0', answer({status: 0, stdout: '0.2.0\n'})), true);
  assert.equal(registryHas('0.2.0', answer({status: 1, stdout: ''})), false, 'E404 for a missing version');
  assert.equal(registryHas('0.2.0', answer({status: 0, stdout: '0.2.00\n'})), false);
  assert.equal(registryHas('0.2.0', answer({status: 0, stdout: ''})), false);
  assert.equal(registryHas('0.2.0', answer({error: new Error('timeout'), status: null, stdout: ''})), false);
});
