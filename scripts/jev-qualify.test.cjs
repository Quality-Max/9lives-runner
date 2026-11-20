const test = require('node:test');
const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const { loadManifest, plannedRequests, renamed, summarize, percentile } = require('./jev-qualify.cjs');

const root = path.resolve(__dirname, '..');

test('the corpus is frozen and covers every lane', () => {
  const manifest = loadManifest(); // throws when a case or the held-out partition changed
  const lanes = {};
  for (const c of manifest.cases) {
    lanes[c.lane] = lanes[c.lane] || { dev: 0, heldout: 0 };
    lanes[c.lane][c.partition]++;
  }
  for (const lane of ['diagnose', 'repair', 'goal']) assert.ok(lanes[lane].dev > 0 && lanes[lane].heldout > 0, lane);
  assert.equal(new Set(manifest.cases.map(c => c.id)).size, manifest.cases.length);
  assert.ok(manifest.cases.some(c => c.lane === 'diagnose' && c.label.runnerClass === 'assertion'), 'business defects are labeled');
});

test('fault coverage names real offline tests', () => {
  const readme = fs.readFileSync(path.join(root, 'testdata/jev-corpus/README.md'), 'utf8');
  const sources = spawnSync('git', ['grep', '-h', '^func Test', '--', 'internal'], { cwd: root, encoding: 'utf8' }).stdout;
  for (const [, name] of readme.matchAll(/`(Test[A-Za-z]+)`/g)) assert.ok(sources.includes(`func ${name}(`), name);
});

test('paid mode refuses before building or calling anything', () => {
  const env = { ...process.env };
  delete env.TYPESAFE_API_KEY;
  for (const args of [['--mode', 'jev'], ['--mode', 'jev', '--confirm-paid', '--max-requests', '1']]) {
    const r = spawnSync(process.execPath, [path.join(__dirname, 'jev-qualify.cjs'), ...args], { env, encoding: 'utf8' });
    assert.equal(r.status, 2);
    assert.match(r.stderr, /No request was made/);
  }
  const manifest = loadManifest();
  assert.equal(plannedRequests(manifest.cases.filter(c => c.lane !== 'goal'), { goalMaxDecisions: 6, repeat: 1 }), 26);
  assert.equal(plannedRequests(manifest.cases.filter(c => c.lane === 'goal'), { goalMaxDecisions: 6, repeat: 2 }), 7 * 12 * 2);
});

test('false greens and metrics are counted from the rows', () => {
  const rows = [
    { lane: 'diagnose', expected: 'investigate-app', disposition: 'eligible-for-repair', falseGreen: true, ms: 10, jevRequests: 1, usage: { inputTokens: 5, outputTokens: 1 } },
    { lane: 'diagnose', expected: 'unresolved', disposition: 'unresolved', correct: true, falseGreen: false, ms: 30 },
    { lane: 'repair', expected: 'verified', verified: true, verifiedCorrect: true, falseGreen: false, ms: 5 },
    { lane: 'repair', expected: 'not-verified', verified: true, verifiedCorrect: false, falseGreen: true, ms: 5 },
    { lane: 'goal', skipped: 'no goal provider without Jev' },
  ];
  const s = summarize(rows);
  assert.equal(s.diagnose.falseGreens, 1);
  assert.equal(s.diagnose.abstained, 1);
  assert.deepEqual(s.diagnose.confusion['investigate-app'], { 'eligible-for-repair': 1 });
  assert.equal(s.diagnose.usage.requests, 1);
  assert.equal(s.repair.precision, 0.5);
  assert.equal(s.repair.recovery, 1);
  assert.equal(s.repair.falseGreens, 1);
  assert.equal(s.goal.skipped, 1);
  assert.equal(percentile([1, 2, 3, 4, 100], 0.95), 100);
  assert.equal(percentile([], 0.5), null);
});

test('labeled renames change only the locator literal', () => {
  const spec = "await page.getByRole('button', { name: 'Submit', exact: true }).click();\nawait page.getByLabel('E-mail').fill('x');";
  assert.equal(renamed(spec, 'Submit', 'Send'), spec.replace("'Submit'", "'Send'"));
  assert.equal(renamed(spec, 'E-mail', "Email's address"), spec.replace("'E-mail'", "'Email\\'s address'"));
  assert.throws(() => renamed(spec, 'Missing', 'x'));
});
