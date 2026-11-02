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
  cases.push({mode, file: path.basename(file), outcome: summary.outcome, statuses: summary.receipts.map(r => r.status), attempts: summary.receipts.map(r => r.attempt)});
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
// A driver's setup classification keeps the test failed, names the reason in
// the receipt and classes the failure as setup, never as an application defect.
const locked = invoke('locked', {extra: ['--failure-details']});
assert.equal(locked.code, 1);
assert.equal(locked.summary.receipts[0].validated, true, 'locked: classified evidence still validates');
assert.equal(locked.summary.receipts[0].status, 'failed');
assert.deepEqual(locked.summary.receipts[0].targetSetup.map(({testId, ...rest}) => rest), [{classification: 'blocked', reason: 'device-locked', stage: 'open'}]);
assert.equal(locked.summary.receipts[0].failures[0].class, 'setup', 'locked: failure class');
assert(locked.summary.receipts[0].failures[0].message.includes('9lives target stopped: setup_blocked'));
assert(locked.summary.receipts[0].validation.includes('1 test(s) carry a driver setup classification'));
assert(!locked.mutations.includes('mutation'), 'locked: nothing ran against the app');
const posture = invoke('posture', {extra: ['--failure-details']});
assert.equal(posture.code, 1);
assert.deepEqual(posture.summary.receipts[0].targetSetup.map(({testId, ...rest}) => rest), [{classification: 'mismatch', reason: 'posture', stage: 'open'}]);
assert.equal(posture.summary.receipts[0].failures[0].class, 'setup', 'posture: a spec needing another posture is a setup mismatch');
const staleBuild = invoke('stale-build', {extra: ['--failure-details']});
assert.equal(staleBuild.code, 1);
assert.deepEqual(staleBuild.summary.receipts[0].targetSetup.map(({testId, ...rest}) => rest), [{classification: 'mismatch', reason: 'build-version', stage: 'open'}]);
assert(!staleBuild.mutations.includes('mutation'), 'stale-build: nothing ran against the wrong build');
const lockRecheck = invoke('lock-recheck', {extra: ['--failure-details']});
assert.equal(lockRecheck.code, 1);
assert.deepEqual(lockRecheck.summary.receipts[0].targetSetup.map(({testId, ...rest}) => rest), [{classification: 'blocked', reason: 'device-locked', stage: 'recheck'}]);
assert.equal(lockRecheck.summary.receipts[0].failures[0].class, 'setup');
const recheckFrames = fs.readFileSync(lockRecheck.summary.receipts[0].evidence.stdoutPath, 'utf8').trim().split('\n').map(JSON.parse);
assert(recheckFrames.some(f => f.operation === 'assert-text' && f.status === 'failed'), 'lock-recheck: the assertion step stays failed');
assert.equal(recheckFrames.filter(f => f.type === 'target_setup').length, 1);
// A recovered baseline before the test and a lock found after the failed
// assertion are both kept, and the test stays failed.
const recoveredThenLock = invoke('recovered-then-lock', {extra: ['--failure-details']});
assert.equal(recoveredThenLock.code, 1);
assert.equal(recoveredThenLock.summary.receipts[0].validated, true, 'recovered-then-lock: two records at distinct stages validate');
assert.deepEqual(recoveredThenLock.summary.receipts[0].targetSetup.map(({testId, ...rest}) => rest), [
  {classification: 'dirty', reason: 'extra-window', stage: 'open', recovery: {method: 'back', presses: 2, restored: true}},
  {classification: 'blocked', reason: 'device-locked', stage: 'recheck'},
]);
const recovered = invoke('dirty-recovered');
assert.equal(recovered.code, 0, 'dirty-recovered: the test ran from the restored baseline');
assert.deepEqual(recovered.summary.receipts[0].targetSetup.map(({testId, ...rest}) => rest), [{classification: 'dirty', reason: 'extra-window', stage: 'open', recovery: {method: 'back', presses: 2, restored: true}}]);
assert.equal(recovered.summary.receipts[0].targetAssertions, 3);
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
// Operations beyond tap/fill: geometry, touch, scrolling, back and hidden
// assertions on both targets; keyboard and process lifecycle on the native
// target; a web call to a native-only operation fails with a stable reason.
const operations = path.join(project, 'targets/operations.spec.ts');
const portable = invoke('operations', {targets: ['web:finance', 'android-fake:finance'], grep: 'portable', file: operations});
assert.equal(portable.code, 0, 'operations: portable operations pass on both targets');
for (const receipt of portable.summary.receipts) {
  assert.equal(receipt.targetAssertions, 4, `${receipt.target}: portable assertions`);
  const frames = fs.readFileSync(receipt.evidence.stdoutPath, 'utf8').trim().split('\n').map(JSON.parse);
  assert.deepEqual(frames.filter(f => f.operation).map(f => f.operation), ['open', 'viewport', 'bounds', 'tap-at', 'assert-text', 'scroll', 'assert-text', 'assert-hidden', 'screen-digest', 'screen-digest', 'back', 'assert-text'], receipt.target);
  assert(!fs.readFileSync(receipt.evidence.stdoutPath, 'utf8').includes('PNG'), `${receipt.target}: no image in evidence`);
}
const lifecycle = invoke('lifecycle', {targets: ['android-fake:finance'], grep: 'native only', file: operations});
assert.equal(lifecycle.code, 0, 'lifecycle: keyboard and restart pass on the native target');
assert.equal(lifecycle.summary.receipts[0].targetAssertions, 4);
const unemulated = invoke('unemulated', {targets: ['web:unemulated'], grep: 'portable', file: operations});
assert.equal(unemulated.code, 0, 'viewport: null still reports a real viewport');
const edge = invoke('edge', {targets: ['web:finance', 'android-fake:finance'], grep: 'outside the viewport', file: operations, extra: ['--failure-details']});
assert.notEqual(edge.code, 0);
for (const receipt of edge.summary.receipts) assert(receipt.failures[0].message.includes('9lives target stopped: invalid_point'), `${receipt.target}: edge touch refused before dispatch`);
assert(!edge.mutations.includes('mutation'));
const malformed = invoke('malformed-visibility', {targets: ['android-fake:finance'], grep: 'malformed visibility', file: operations, extra: ['--failure-details']});
assert.notEqual(malformed.code, 0, 'malformed-visibility: an undefined answer cannot pass toBeHidden');
assert(malformed.summary.receipts[0].failures[0].message.includes('9lives target stopped: invalid_property'), 'malformed-visibility: stable reason');
const unsupported = invoke('unsupported', {targets: ['web:finance'], grep: 'native only', file: operations, extra: ['--failure-details']});
assert.notEqual(unsupported.code, 0);
assert(unsupported.summary.receipts[0].failures[0].message.includes('9lives target stopped: unsupported_operation'), 'unsupported: stable capability error');
// With opt-in diagnostics, a hidden assertion the window manager contradicts
// is classified as observer staleness and the failure carries a metadata record.
const observer = invoke('observer', {targets: ['android-fake:finance'], grep: 'outside touch', file: operations, extra: ['--failure-details']});
assert.equal(observer.code, 1);
assert.equal(observer.summary.receipts[0].status, 'failed');
assert.deepEqual(observer.summary.receipts[0].targetSetup.map(({testId, ...rest}) => rest), [{classification: 'observer', reason: 'window-gone-element-displayed', stage: 'recheck'}]);
assert.equal(observer.summary.receipts[0].failures[0].class, 'setup');
const diagnostic = observer.summary.receipts[0].targetDiagnostics[0];
assert.equal(diagnostic.operation, 'assert-hidden');
assert.equal(diagnostic.reason, 'assertion_failed', 'observer: the operation\'s own failure, before reclassification');
assert.deepEqual([diagnostic.windowsBefore, diagnostic.windowsAfter, diagnostic.viewportWidth], [2, 1, 400]);
assert(diagnostic.elapsedMs >= 250 && diagnostic.elapsedMs < 5000, `observer: poll duration recorded, got ${diagnostic.elapsedMs}`);
assert.equal(observer.summary.receipts[0].targetDiagnostics.length, 1, 'observer: one record per test');
const clean = invoke('healthy-modal', {targets: ['web:finance', 'android-fake:finance'], grep: 'outside touch', file: operations});
assert.equal(clean.code, 0, 'healthy modal dismissal passes on both targets');
for (const receipt of clean.summary.receipts) {
  assert.equal(receipt.targetDiagnostics, undefined, `${receipt.target}: no diagnostics without a failure`);
  assert.equal(receipt.targetAssertions, 2, `${receipt.target}: the modal was seen open before the dismissal was asserted`);
}
// An app that ignores the outside touch fails the same hidden assertion as an
// ordinary assertion failure: no observer classification, no diagnostics.
const ignored = invoke('ignored-touch', {targets: ['android-fake:finance'], grep: 'outside touch', file: operations, extra: ['--failure-details']});
assert.equal(ignored.code, 1);
assert(ignored.summary.receipts[0].failures[0].message.includes('9lives target stopped: assertion_failed'), 'ignored-touch: plain assertion failure');
assert.equal(ignored.summary.receipts[0].targetSetup, undefined);
assert.equal(ignored.summary.receipts[0].targetDiagnostics, undefined);
// A diagnostics hook that never answers is abandoned at its bound: the
// failure is still reported by the worker, without a record.
const stalled = invoke('stalled-diagnose', {targets: ['android-fake:finance'], grep: 'outside touch', file: operations, extra: ['--failure-details']});
assert.equal(stalled.code, 1);
assert.equal(stalled.summary.receipts[0].status, 'failed', 'stalled-diagnose: a worker-side failure, not the engine deadline');
assert(stalled.summary.receipts[0].failures[0].message.includes('9lives target stopped: assertion_failed'), 'stalled-diagnose: plain assertion failure');
assert.equal(stalled.summary.receipts[0].targetDiagnostics, undefined);
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
console.log('Target contract: web + fake native passed; exact-result defect, ambiguous/missing locators, incompatible drivers, caught failures, forged steps, unpinned skips and pending cancellation/timeout/deadline rejected; locked-device and lock-during-assertion classified as setup, recovered dirty baseline passed with its record; pinned skip, re-rendered web control and plain listing passed; one attempt each.');
console.log(`Bounded receipts: ${path.relative(root, work)}`);
