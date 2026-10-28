// Deterministic shared web/fake-native contract qualification. No device/provider.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {createHash} = require('node:crypto');
const {spawnSync} = require('node:child_process');
const root = path.resolve(__dirname, '..');
const work = path.join(root, '.context', 'target-contract', `smoke-${Date.now()}`);
fs.mkdirSync(work, {recursive: true});
const engine = path.join(work, process.platform === 'win32' ? '9l.exe' : '9l');
function sync(command, args, options = {}) {
  const result = spawnSync(command, args, {cwd: root, encoding: 'utf8', timeout: 60000, maxBuffer: 4 << 20, ...options});
  assert.ifError(result.error);
  return result;
}
assert.equal(sync('go', ['build', '-o', engine, './cmd/9l']).status, 0);
const targetAssertions = sync(process.execPath, [path.join(root, 'node_modules', '@playwright', 'test', 'cli.js'),
  'test', '--config', 'testdata/sdk/target-assertion.config.ts', '--reporter=line']);
assert.equal(targetAssertions.status, 0, targetAssertions.stderr || targetAssertions.stdout);
// A throwaway repository supports provenance even on detached CI checkouts.
const project = path.join(work, 'project');
fs.cpSync(path.join(root, 'testdata/sdk/targets'), path.join(project, 'targets'), {recursive: true});
fs.copyFileSync(path.join(root, 'testdata/sdk/targets.config.ts'), path.join(project, 'targets.config.ts'));
const playwrightVersion = JSON.parse(fs.readFileSync(path.join(root, 'package.json'))).devDependencies['@playwright/test'];
fs.writeFileSync(path.join(project, 'package.json'), JSON.stringify({private: true, devDependencies: {'@playwright/test': playwrightVersion}}));
const git = (...args) => assert.equal(sync('git', args, {cwd: project}).status, 0, `git ${args[0]}`);
git('init', '--quiet', '-b', 'target-qualification');
git('add', 'targets', 'targets.config.ts', 'package.json');
git('-c', 'user.name=9lives', '-c', 'user.email=fixture@9lives.invalid', 'commit', '--quiet', '-m', 'Pin target fixture');
fs.symlinkSync(path.join(root, 'node_modules'), path.join(project, 'node_modules'), process.platform === 'win32' ? 'junction' : 'dir');
const spec = path.join(project, 'targets/shared.spec.ts');
const config = path.join(project, 'targets.config.ts');
const provenance = path.join(work, 'creation.json');
const creation = sync(engine, ['provenance', spec, '--agent', 'codex']);
assert.equal(creation.status, 0);
fs.writeFileSync(provenance, creation.stdout, {mode: 0o600});
const cases = [];
function invoke(mode, {targets = ['android-fake:finance'], grep = 'one deterministic scenario', file = spec, extra = []} = {}) {
  const effects = path.join(work, `${mode}.effects`);
  const result = sync(engine, ['run', file, '--sdk', ...targets.flatMap(target => ['--target', target]), ...extra,
    '--config', config, '--grep', grep, '--attempts', '3', '--timeout', mode === 'deadline' ? '6s' : '20s',
    '--format', 'json', '--receipt-dir', path.join(work, mode),
    '--pass-env', 'NINELIVES_TARGET_CASE', '--pass-env', 'NINELIVES_TARGET_EFFECTS',
    ...(targets.length === 1 && file === spec ? ['--agent-provenance', provenance] : [])], {
    env: {...process.env, NINELIVES_TARGET_CASE: mode, NINELIVES_TARGET_EFFECTS: effects},
  });
  const summary = JSON.parse(result.stdout);
  fs.writeFileSync(path.join(work, `${mode}.json`), result.stdout, {mode: 0o600});
  assert.equal(summary.receipts.length, targets.length, mode);
  for (const [index, receipt] of summary.receipts.entries()) {
    assert.equal(receipt.target, targets[index], mode);
    assert.equal(receipt.attempt, 1, `${mode}: mutation must never retry`);
    if (targets.length === 1 && file === spec) assert.equal(receipt.agentProvenance.status, 'matched', mode);
  }
  const mutations = fs.existsSync(effects) ? fs.readFileSync(effects, 'utf8').trim().split('\n') : [];
  cases.push({mode, outcome: summary.outcome, statuses: summary.receipts.map(r => r.status), attempts: summary.receipts.map(r => r.attempt)});
  return {code: result.status, summary, mutations};
}

const baseline = invoke('healthy', {targets: ['web:finance', 'android-fake:finance']});
assert.equal(baseline.code, 0);
assert.equal(baseline.summary.complete, true);
assert.deepEqual(baseline.mutations, ['mutation']);
for (const receipt of baseline.summary.receipts) {
  assert.equal(receipt.validated, true);
  assert.equal(receipt.executedTests, 1);
  assert.equal(receipt.targetAssertions, 3);
  assert.equal(receipt.verifiedAssertions, 0, 'worker-reported target checks are not independent verification');
  assert.equal(receipt.assertionCoverage, 'unknown');
  const raw = fs.readFileSync(receipt.evidence.stdoutPath, 'utf8');
  assert(!raw.includes('Sent $') && !raw.includes('receipt') && !raw.includes('amount'));
  const frames = raw.trim().split('\n').map(line => JSON.parse(line));
  const targetId = createHash('sha256').update(receipt.target).digest('hex');
  assert(frames.every(f => f.targetId === targetId && f.attemptId === receipt.attemptId && f.jobId === receipt.jobId && f.runId === receipt.runId));
  assert.equal(frames.filter(f => f.type === 'required_test').length, 1);
  assert.equal(frames.filter(f => f.operation).length, 7);
}
const defect = invoke('defect');
assert.equal(defect.code, 1);
assert.equal(defect.summary.outcome, 'failed');
assert.equal(defect.summary.receipts[0].validated, true);
assert.equal(defect.summary.receipts[0].failureCount, 1);
assert.deepEqual(defect.mutations, ['mutation']);
const defectFrames = fs.readFileSync(defect.summary.receipts[0].evidence.stdoutPath, 'utf8').trim().split('\n').map(JSON.parse);
assert(defectFrames.some(f => f.operation === 'assert-text' && f.status === 'failed'), 'wrong finance result must fail the exact assertion');
const reasons = {ambiguous: 'ambiguous_test_id', missing: 'missing_test_id', version: 'unsupported_driver_contract', capabilities: 'unsupported_driver_contract'};
for (const mode of ['ambiguous', 'missing', 'version', 'capabilities', 'cancel', 'deadline', 'timeout', 'late', 'caught', 'forged']) {
  const grep = {caught: 'caught failures', forged: 'named like a target assertion'}[mode];
  const result = invoke(mode, {grep, extra: reasons[mode] ? ['--failure-details'] : []});
  assert.notEqual(result.code, 0, mode);
  assert.notEqual(result.summary.outcome, 'passed', mode);
  assert(!result.mutations.includes('mutation'), `${mode}: no side effect`);
  if (['cancel', 'deadline', 'timeout', 'late'].includes(mode)) assert(result.mutations.includes('started'), `${mode}: mutation was actually pending`);
  // The worker's own operation bound settles a validated failure before Go's deadline.
  if (mode === 'timeout' || mode === 'late') assert.equal(result.summary.receipts[0].status, 'failed', `${mode}: operation timeout is a worker-side failure`);
  if (mode === 'caught' || mode === 'forged') assert.equal(result.summary.receipts[0].validated, false, `${mode}: cannot green`);
  if (reasons[mode]) {
    const message = result.summary.receipts[0].failures?.[0]?.message ?? '';
    assert(message.includes(`9lives target stopped: ${reasons[mode]}`), `${mode}: specific reason, got ${message}`);
  }
}
// A platform skip is complete only when pinned, like any other SDK skip.
const skip = 'one deterministic scenario|skipped on the native target';
const unpinned = invoke('skip-unpinned', {grep: skip});
assert.equal(unpinned.summary.receipts[0].validated, false, 'unpinned target skip cannot green');
const pinned = invoke('skip-pinned', {grep: skip, extra: ['--pin-skip', 'shared.spec.ts › skipped on the native target']});
assert.equal(pinned.code, 0);
assert.equal(pinned.summary.receipts[0].executedTests, 1);
assert.equal(pinned.summary.receipts[0].skippedTests, 1);
// Web actions follow a node replaced by re-rendering instead of a stale handle.
const rerender = invoke('rerender', {targets: ['web:finance'], grep: 're-rendered control', file: path.join(project, 'targets/rerender.spec.ts')});
assert.equal(rerender.code, 0, 'rerender');
assert.equal(rerender.summary.receipts[0].targetAssertions, 1);
// Specs remain listable by plain Playwright and editors without 9l.
const listing = sync(process.execPath, [path.join(root, 'node_modules', '@playwright', 'test', 'cli.js'), 'test', '--list', '--config', 'testdata/sdk/targets.config.ts'],
  {env: Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('NINELIVES_')))});
assert.equal(listing.status, 0, listing.stderr);
// Existing ordinary specs still run without SDK opt-in.
assert.equal(sync(process.execPath, [path.join(root, 'node_modules', '@playwright', 'test', 'cli.js'),
  'test', 'testdata/sdk/tests/nested/config.spec.ts', '--config', 'testdata/sdk/playwright.config.ts', '--reporter=line']).status, 0);
fs.writeFileSync(path.join(work, 'results.json'), JSON.stringify({version: 1, cases, ordinaryPlaywright: 'passed'}, null, 2));
console.log('Target contract: web + fake native passed; exact-result defect, ambiguous/missing locators, incompatible drivers, caught failures, forged steps, unpinned skips and pending cancellation/timeout/deadline rejected; pinned skip, re-rendered web control and plain listing passed; one attempt each.');
console.log(`Bounded receipts: ${path.relative(root, work)}`);
