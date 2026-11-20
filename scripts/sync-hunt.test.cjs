const test = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { resolve } = require('node:path');
const { expected, checkReplays, room } = require('./sync-hunt.cjs');

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

test('a planted bug changes the recorded fixture', () => {
  assert.notEqual(room('cats', true), room('cats'));
});
