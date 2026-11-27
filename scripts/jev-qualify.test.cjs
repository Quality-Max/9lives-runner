const test = require('node:test');
const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const { loadManifest, plannedRequests, renamed, summarize, percentile, parseArgs, goalScript, goalRow } = require('./jev-qualify.cjs');

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

test('repeat, decision and request counts must be whole numbers', () => {
  // A fractional repeat ran two repeats under a cap computed for 1.5, and NaN
  // passed the cap comparison.
  for (const args of [['--repeat', '1.5'], ['--max-requests', 'NaN'], ['--max-requests', ''], ['--goal-max-decisions', '2.5'], ['--repeat', '1e0'], ['--max-requests', '-1'], ['--max-requests', '99999999999999999999']]) {
    assert.throws(() => parseArgs(args), /whole number/, args.join(' '));
  }
  assert.throws(() => parseArgs(['--repeat', '6']), /invalid options/);
  const options = parseArgs(['--mode', 'jev', '--repeat', '2', '--goal-max-decisions', '3', '--max-requests', '40', '--confirm-paid']);
  assert.deepEqual([options.repeat, options.goalMaxDecisions, options.maxRequests], [2, 3, 40]);
});

test('every goal case is exercised by an oracle or a probe', () => {
  for (const c of loadManifest().cases.filter(x => x.lane === 'goal')) {
    const script = goalScript(c);
    assert.ok(script, `${c.id} would be skipped`);
    const parsed = JSON.parse(fs.readFileSync(path.join(root, 'testdata/jev-corpus', script.file), 'utf8'));
    assert.equal(parsed.version, '9l.goal/1', c.id);
    assert.ok(parsed.steps.length > 0, c.id);
    assert.equal(script.kind, c.label.oracle ? 'oracle' : 'probe', c.id);
  }
});

test('unusable rows fail separately and are not scored', () => {
  const rows = [
    { lane: 'repair', expected: 'verified', unusable: 'no session result (exit 2)', verified: false, falseGreen: false, ms: 5 },
    { lane: 'repair', expected: 'verified', verified: true, verifiedCorrect: true, falseGreen: false, ms: 5, jevRequests: 1 },
    { lane: 'goal', unusable: 'the receipt has no goal record', goal: undefined, test: undefined, falseGreen: false, ms: 5 },
    { lane: 'goal', goal: 'completed', goalCorrect: true, test: 'passed', testCorrect: true, falseGreen: false, ms: 9, jevRequests: null, usage: { inputTokens: 40, outputTokens: 4, available: true } },
  ];
  const s = summarize(rows);
  assert.deepEqual([s.repair.cases, s.repair.unusable, s.repair.recovery], [1, 1, 1]);
  assert.deepEqual([s.goal.cases, s.goal.unusable, s.goal.goalStatusMatched], [1, 1, 1]);
  // Goal receipts do not count Jev requests, so the total stays unknown.
  assert.equal(s.goal.usage.requests, null);
  assert.deepEqual([s.goal.usage.inputTokens, s.goal.usage.outputTokens], [40, 4]);
  assert.equal(s.repair.usage.requests, 1);
});

test('goal rows total the Jev requests and models their decisions report', () => {
  const c = { id: 'G01', lane: 'goal', partition: 'dev', label: { goal: ['completed'], test: 'passed' } };
  const run = decisions => ({ code: 0, ms: 5, json: { receipts: [{ status: 'passed', validated: true, goals: [{ status: 'completed', decisions }] }] } });
  const known = goalRow(c, { mode: 'jev' }, run([
    { action: 'fill', requests: 2, model: 'jev-1.14.0', inputTokens: 900, outputTokens: 40, usageAvailable: true },
    { action: 'complete', requests: 0, inputTokens: 0, outputTokens: 0, usageAvailable: true },
  ]));
  assert.deepEqual([known.jevRequests, known.model, known.usage.inputTokens, known.unusable], [2, 'jev-1.14.0', 900, null]);
  // A decision whose provider call failed has no count: the total is unknown.
  const failed = goalRow(c, { mode: 'jev' }, run([{ action: 'fill', requests: 1, model: 'jev-1.14.0' }, { action: 'none', outcome: 'provider_error', inputTokens: 0, outputTokens: 0, usageAvailable: false }]));
  assert.equal(failed.jevRequests, null);
  assert.equal(failed.model, 'jev-1.14.0');
  const s = summarize([known, failed]);
  assert.equal(s.goal.usage.requests, null);
  assert.equal(summarize([known]).goal.usage.requests, 2);
  // Without Jev, nothing is paid.
  assert.equal(goalRow(c, { mode: 'validate' }, run([{ action: 'complete' }])).jevRequests, 0);
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
