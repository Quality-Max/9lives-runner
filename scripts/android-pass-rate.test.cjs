'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const {resultFiles, passRates, markdown} = require('./android-pass-rate.cjs');

function run(directory, name, cases) {
  const dir = path.join(directory, name, 'smoke-1');
  fs.mkdirSync(dir, {recursive: true});
  fs.writeFileSync(path.join(dir, 'results.json'), JSON.stringify({version: 1, cases}));
}

test('counts qualified runs per case and names why the others did not qualify', () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'android-rate-'));
  run(directory, 'a', [{mode: 'healthy', qualified: true}, {mode: 'focus', qualified: false, reason: 'focus: focus-dependent checks run\nstack'}]);
  // A record without `qualified` is a case that ended as expected, such as misfill.
  run(directory, 'b', [{mode: 'healthy', qualified: true}, {mode: 'focus', outcome: 'failed'}, {mode: 'misfill', outcome: 'failed'}]);
  run(directory, 'c', [{mode: 'focus', qualified: false, reason: 'focus: focus-dependent checks run'}]);
  fs.mkdirSync(path.join(directory, 'd'));
  fs.writeFileSync(path.join(directory, 'd', 'results.json'), 'not json');
  const rates = passRates(resultFiles(directory), ['healthy', 'cancel']);
  assert.equal(rates.files, 4);
  assert.equal(rates.unreadable, 1);
  assert.deepEqual({...rates.cases.get('healthy'), reasons: undefined}, {runs: 2, qualified: 2, reasons: undefined});
  assert.equal(rates.cases.get('focus').runs, 3);
  assert.equal(rates.cases.get('focus').qualified, 1);
  assert.equal(rates.cases.get('focus').reasons.get('focus: focus-dependent checks run'), 2);
  const table = markdown(rates);
  // The worst case comes first, after cases that never ran.
  assert.match(table, /\| cancel \| 0\/0 \| not run \|/);
  assert.match(table, /\| focus \| 1\/3 \| 33% \| 2× focus: focus-dependent checks run \|/);
  assert.match(table, /\| misfill \| 1\/1 \| 100% \|/);
  assert.ok(table.indexOf('| cancel') < table.indexOf('| focus') && table.indexOf('| focus') < table.indexOf('| healthy'));
  assert.match(table, /4 smoke run\(s\) read, 1 unreadable/);
});

test('keeps table cells on one line and ignores malformed case names', () => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), 'android-rate-'));
  run(directory, 'a', [{mode: 'state-defect', qualified: false, reason: 'a | b'}, {mode: '../x', qualified: false}, {mode: 7}]);
  const table = markdown(passRates(resultFiles(directory)));
  assert.match(table, /\| state-defect \| 0\/1 \| 0% \| 1× a {3}b \|/);
  assert.doesNotMatch(table, /\.\.\/x/);
});
