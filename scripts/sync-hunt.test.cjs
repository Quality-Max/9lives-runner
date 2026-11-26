const test = require('node:test');
const assert = require('node:assert/strict');
const { cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } = require('node:fs');
const { tmpdir } = require('node:os');
const { join, resolve } = require('node:path');
const { expected, checkReplays, room, assertPortable } = require('./sync-hunt.cjs');

const site = resolve(__dirname, '../website/public');

test('the website hunt matches the local game', () => {
  for (const [file, text] of Object.entries(expected())) assert.equal(readFileSync(resolve(site, file), 'utf8'), text, `${file} is stale; run node scripts/sync-hunt.cjs`);
});

test('the website page replays recordings instead of calling the local server', () => {
  const page = expected()['hunt.html'];
  assert.match(page, /<meta name="demo-mode" content="replay">/);
  assert.doesNotMatch(page, /__DEMO_SESSION__|\/assets\/|src="\/shop/);
});

test('recorded runs are portable, complete and match the current game', () => {
  checkReplays();
});

test('a published receipt or Jev record that is not the replayed run fails the check', t => {
  const copy = mkdtempSync(join(tmpdir(), 'hunt-replays-'));
  t.after(() => rmSync(copy, { recursive: true, force: true }));
  const replays = resolve(site, 'hunt/replays');
  const fresh = () => { rmSync(copy, { recursive: true, force: true }); cpSync(replays, copy, { recursive: true }); };
  const edit = (file, change) => { const path = join(copy, file); const value = JSON.parse(readFileSync(path, 'utf8')); change(value); writeFileSync(path, JSON.stringify(value)); };
  fresh();
  checkReplays(copy);
  for (const [name, mutate] of [
    ['a passing receipt beside a failing replay', () => cpSync(join(copy, 'cats-healthy/receipt.json'), join(copy, 'cats-broken/receipt.json'))],
    ['a Jev record that did not answer', () => edit('cats-broken/jev-record.json', r => { r.status = 'provider_error'; })],
    ['a Jev record from another run', () => edit('dogs-healthy/jev-record.json', r => { r.binding.runId = 'run-elsewhere'; })],
    ['a receipt whose failure is not an assertion', () => edit('mice-broken/receipt.json', r => { r.receipts[0].failures[0].class = 'environment'; })],
  ]) {
    fresh();
    mutate();
    assert.throws(() => checkReplays(copy), undefined, name);
  }
});

test('local paths are refused anywhere in published text, URLs and relative paths are not', () => {
  for (const text of ['/Users/someone/x', 'failed at /private/var/folders/ab/T/x.png', 'see /root/.cache/ms-playwright/chromium', 'at C:\\Users\\someone\\x.spec.mjs:3', 'open "C:/work/x"', 'share \\\\host\\share\\x']) {
    assert.throws(() => assertPortable({ message: text }, 'fixture'), /local path/, text);
  }
  for (const text of ['demo/jev/picture-hunt.spec.mjs:98', '.context/jev-hunt-1/receipts/receipt.json', 'GET http://127.0.0.1:4175/cats', 'Found 9/9 cats', 'expected 8/9']) {
    assert.doesNotThrow(() => assertPortable({ message: text }, 'fixture'), text);
  }
});

test('a planted bug changes the recorded fixture', () => {
  assert.notEqual(room('cats', true), room('cats'));
});
